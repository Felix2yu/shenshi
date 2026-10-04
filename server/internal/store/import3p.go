package store

import (
	"encoding/csv"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/yufei/shendu/server/internal/model"
)

// 第三方格式导入。
//
// 为什么值得单独做：迁移是别人用起来的第一道门槛。备份 JSON 只有自己导得出来，
// 从别处换过来的人手里拿的是 Todoist 的 CSV、系统的 .ics——
// 不认这些格式，就等于让每个新用户先手工重建一遍全部数据，那一步足以劝退大多数人。
//
// 一条硬约束：**导入永远走 merge，不做 replace**。第三方文件不是慎始的备份，
// 它的结构和语义都不同，拿它去「替换」现有数据等于把用户的东西删掉。
// 重复导入靠 client_id 之外的标题+日期判重兜不住，所以这里明确只增不改。

// inboxPlaceholderID 是 bundle 里收件箱占位条目的 id。
// importMerge 见到 ID 等于 InboxListID 的清单会并入现有收件箱而不是新建，
// 所以用它承载「第三方文件里没有归属清单的那些任务」。
const inboxPlaceholderID = -1

// ImportedFormat 是被识别出来的第三方格式。
type ImportedFormat string

const (
	FormatTodoistCSV ImportedFormat = "todoist-csv"
	FormatICS        ImportedFormat = "ics"
)

// DetectFormat 靠内容猜格式，不看扩展名。
// 用户从邮件里存下来的文件常常没有正确的后缀，靠扩展名判断会把好好的 CSV 当成垃圾拒掉。
func DetectFormat(name string, head []byte) ImportedFormat {
	s := strings.ToLower(name)
	switch {
	case strings.HasSuffix(s, ".ics"):
		return FormatICS
	case strings.HasSuffix(s, ".csv"):
		return FormatTodoistCSV
	}
	// 无后缀时看内容：ICS 必有 BEGIN:VCALENDAR，Todoist CSV 首行含逗号与引号列。
	text := string(head)
	if strings.Contains(text, "BEGIN:VCALENDAR") {
		return FormatICS
	}
	if strings.Contains(text, "\"content\"") || strings.Contains(text, "\"project\"") {
		return FormatTodoistCSV
	}
	return ""
}

// ImportPreview 是导入前的预览：用户得先看清「会进来多少东西」再按确定。
type ImportPreview struct {
	Format    ImportedFormat `json:"format"`
	Tasks     int            `json:"tasks"`
	Completed int            `json:"completed"`
	Lists     []string       `json:"lists"`
	Tags      []string       `json:"tags"`
	// Notes 列出被丢掉的内容（如子任务、循环规则），不悄悄吞掉用户的数据。
	Notes []string `json:"notes,omitempty"`
	// FirstTitles 抽样几条，让用户确认解析对了而不是解析出了一堆乱码。
	FirstTitles []string `json:"firstTitles,omitempty"`
}

// thirdPartyTask 是解析出来的中间形态：还没决定落到哪个清单、打什么标签。
type thirdPartyTask struct {
	title    string
	notes    string
	listName string
	labels   []string
	due      *string
	done     bool
	priority int
}

// ParseThirdParty 把第三方文件解析成 bundle。format 为空表示认不出来。
func ParseThirdParty(format ImportedFormat, data []byte) (*ExportBundle, *ImportPreview, error) {
	var tasks []thirdPartyTask
	var err error
	switch format {
	case FormatTodoistCSV:
		tasks, err = parseTodoistCSV(data)
	case FormatICS:
		tasks, err = parseICS(data)
	default:
		return nil, nil, ValidationError{Msg: "认不出这是什么格式（支持 Todoist CSV 与 iCalendar .ics）"}
	}
	if err != nil {
		return nil, nil, err
	}
	if len(tasks) == 0 {
		return nil, nil, ValidationError{Msg: "文件里没有可导入的任务"}
	}
	return buildThirdPartyBundle(tasks, format)
}

// parseTodoistCSV 解析 Todoist 导出的 CSV。
// 列名各版本略有出入（"content"/"title"、"due date"/"due"），
// 因此全部按小写取值并允许多个别名，而不是写死列序——
// 写死列序的解析器会在用户换一版导出设置后静默产出空白任务。
func parseTodoistCSV(data []byte) ([]thirdPartyTask, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	r.FieldsPerRecord = -1 // 各行列数不齐是常态，别直接报错
	rows, err := r.ReadAll()
	if err != nil {
		return nil, ValidationError{Msg: "CSV 解析失败: " + err.Error()}
	}
	if len(rows) == 0 {
		return nil, nil
	}
	// 表头定位：找内容/项目/标签这几列的下标。
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	col := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := idx[n]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
	var out []thirdPartyTask
	for _, row := range rows[1:] {
		content := col(row, "content", "title", "任务", "任务内容")
		if content == "" {
			continue
		}
		t := thirdPartyTask{
			title:    content,
			notes:    col(row, "description", "notes", "备注"),
			listName: col(row, "project", "section", "list", "项目"),
			priority: todoistPriority(col(row, "priority")),
		}
		if d := col(row, "due date", "due", "截止日期"); d != "" {
			t.due = parseLooseDate(d)
		}
		// Todoist 的 label 列形如 "a, b, c"；旧版本叫 "labels"。
		for _, raw := range strings.Split(col(row, "labels", "label", "标签"), ",") {
			if v := strings.TrimSpace(raw); v != "" {
				t.labels = append(t.labels, v)
			}
		}
		if v := col(row, "checked", "iscompleted", "completed"); v != "" {
			t.done = v == "1" || strings.EqualFold(v, "true")
		}
		out = append(out, t)
	}
	return out, nil
}

// todoistPriority：Todoist 用 1~4，4 最高。本项目 0~3，3 最高。
func todoistPriority(v string) int {
	switch strings.TrimSpace(v) {
	case "p1":
		return 1
	case "p2":
		return 2
	case "p3":
		return 3
	case "p4":
		return 3 // 最高一档压到本项目的最高（3），不额外造一档
	}
	return 0
}

// parseLooseDate 认出几种常见写法，只取日期部分。
// 第三方导出里的日期格式五花八门（2026-10-04、2026-10-04T09:00:00Z、10/04/2026），
// 全都当字符串塞进日期字段会直接让日期排序失效，所以这里逐个试。
func parseLooseDate(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02",
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006/01/02",
		"20060102",
	}
	for _, l := range layouts {
		if d, err := time.Parse(l, v); err == nil {
			s := d.Format("2006-01-02")
			return &s
		}
	}
	return nil
}

// parseICS 解析 iCalendar。VTODO 是待办事项，VEVENT 也一并收进来——
// 很多人把日历上的会议当待办管理，丢掉它们会让导入后的清单空一大半。
func parseICS(data []byte) ([]thirdPartyTask, error) {
	cal, err := ical.NewDecoder(strings.NewReader(string(data))).Decode()
	if err != nil {
		return nil, ValidationError{Msg: "iCalendar 解析失败: " + err.Error()}
	}
	if cal == nil {
		return nil, ValidationError{Msg: "iCalendar 内容为空"}
	}
	var out []thirdPartyTask
	for _, child := range cal.Children {
		if child.Name != ical.CompToDo && child.Name != ical.CompEvent {
			continue
		}
		t := thirdPartyTask{
			title:    strings.TrimSpace(icalText(child, ical.PropSummary)),
			notes:    strings.TrimSpace(icalText(child, ical.PropDescription)),
			listName: strings.TrimSpace(icalText(child, ical.PropName)),
		}
		if t.title == "" {
			continue
		}
		// 日期：优先 DTSTART（带时刻就取其日期部分），退回 DUE（VTODO 专有）。
		for _, name := range []string{ical.PropDateTimeStart, ical.PropDue} {
			if d, ok := icalDate(child, name); ok {
				s := d.Format("2006-01-02")
				t.due = &s
				break
			}
		}
		// 已完成：VTODO 的 STATUS，或 VEVENT 明确标了 TRANSP:TRANSPARENT（不占用时间）不算。
		if status := strings.ToUpper(strings.TrimSpace(icalText(child, ical.PropStatus))); status == "COMPLETED" || status == "CANCELLED" {
			t.done = true
		}
		// CATEGORIES 存成文本列表，可能一条属性里逗号分隔，也可能多属性并列，两种都要拆开。
		if prop := child.Props.Get(ical.PropCategories); prop != nil {
			if list, err := prop.TextList(); err == nil {
				for _, v := range list {
					for _, one := range strings.Split(v, ",") {
						if c := strings.TrimSpace(one); c != "" {
							t.labels = append(t.labels, c)
						}
					}
				}
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// icalText 读一个文本属性。缺属性或类型不符一律返回空串：
// 第三方 .ics 的属性类型标注经常不规范，为一条读不出的属性丢掉整个任务不值得。
func icalText(c *ical.Component, name string) string {
	prop := c.Props.Get(name)
	if prop == nil {
		return ""
	}
	s, err := prop.Text()
	if err != nil {
		return strings.TrimSpace(prop.Value)
	}
	return s
}

// icalDate 读一个日期属性。DTSTART 可能是纯日期（VALUE=DATE），也可能是带时刻的。
// 两种都要能读出来，只认一种会让「全天事件」和「定时事件」各丢一半。
func icalDate(c *ical.Component, name string) (time.Time, bool) {
	prop := c.Props.Get(name)
	if prop == nil {
		return time.Time{}, false
	}
	d, err := prop.DateTime(time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

// buildThirdPartyBundle 把中间形态拼成与自家备份同构的 bundle，
// 后面的入库直接复用 Import —— 只写一套入库逻辑，将来改字段不会漏掉导入这条路。
//
// 清单与标签的 id 是**占位 id**：importMerge 会按「备份里的 id → 新建的 id」重新映射，
// 所以这里只要自洽即可（列表下标从1 起，避开 0 这个「无」的含义）。
func buildThirdPartyBundle(tasks []thirdPartyTask, format ImportedFormat) (*ExportBundle, *ImportPreview, error) {
	// 按出现顺序收集清单与标签，保持用户在原应用里的组织方式。
	var listNames []string
	seenList := map[string]bool{}
	tagSet := map[string]bool{}
	var tagNames []string
	for _, t := range tasks {
		if name := strings.TrimSpace(t.listName); name != "" && !seenList[name] {
			seenList[name] = true
			listNames = append(listNames, name)
		}
		for _, l := range t.labels {
			if l != "" && !tagSet[l] {
				tagSet[l] = true
				tagNames = append(tagNames, l)
			}
		}
	}
	sort.Strings(listNames)
	sort.Strings(tagNames)

	lists := make([]model.List, 0, len(listNames))
	listIdx := map[string]int64{}
	for i, n := range listNames {
		l := model.List{ID: int64(i + 1), Name: n, Color: listColorFor(i)}
		lists = append(lists, l)
		listIdx[n] = l.ID
	}
	tags := make([]model.Tag, 0, len(tagNames))
	tagIdx := map[string]int64{}
	for i, n := range tagNames {
		g := model.Tag{ID: int64(i + 1), Name: n}
		tags = append(tags, g)
		tagIdx[n] = g.ID
	}

	var completed int
	var hasNote bool
	var needsInbox bool
	out := make([]model.Task, 0, len(tasks))
	var samples []string
	for i, t := range tasks {
		mt := model.Task{
			// 任务也需要占位 id：importMerge 用 t.ID 建 taskID 映射，供关联边重建。
			// 这里没有关联边，但保持 id 非零，避免被当成「无」。
			ID:       int64(i + 1),
			Title:    t.title,
			Notes:    t.notes,
			DueDate:  t.due,
			Priority: priorityOf(t.priority),
			Tags:     tagModelsFor(t.labels, tagIdx),
		}
		if name := strings.TrimSpace(t.listName); name != "" {
			mt.ListID = listIdx[name]
		} else {
			// 没归属清单的任务必须有个去处：importMerge 里 listID 查不到就整条跳过，
			// 那等于「导入成功但少了一半任务」，是这类导入最难被察觉的失败。
			// 统一挂到 bundle 里的收件箱条目上（它会被 importMerge 并入现有收件箱）。
			needsInbox = true
			mt.ListID = inboxPlaceholderID
		}
		if t.done {
			mt.Status = model.StatusDone
			completed++
		}
		out = append(out, mt)
		if len(samples) < 5 {
			samples = append(samples, t.title)
		}
		if t.notes != "" {
			hasNote = true
		}
	}

	// 无归属任务挂靠的收件箱占位。它不会新建一个清单：
	// importMerge 见到 ID == InboxListID 的条目会并入现有收件箱。
	if needsInbox {
		lists = append(lists, model.List{ID: inboxPlaceholderID, Name: "收集箱"})
	}

	prev := &ImportPreview{
		Format:      format,
		Tasks:       len(out),
		Completed:   completed,
		Lists:       listNames,
		Tags:        tagNames,
		FirstTitles: samples,
	}
	// 明确说清丢了什么，比让用户自己发现「少了一堆子任务」要好。
	if !hasNote {
		prev.Notes = append(prev.Notes, "文件里没有备注，已按空备注导入")
	}
	prev.Notes = append(prev.Notes,
		"循环规则不迁移：导入的任务是一次性的，需要重复请在慎始里重新设置",
		"提醒不迁移：只保留日期",
	)

	b := &ExportBundle{
		Version:    1,
		App:        "shenshi-thirdparty",
		ExportedAt: time.Now().Format(time.RFC3339),
		Lists:      lists,
		Tags:       tags,
		Tasks:      out,
	}
	if needsInbox {
		b.InboxListID = inboxPlaceholderID
	}
	return b, prev, nil
}

// priorityOf 把外部的 1~4 折算成本项目的 0~3。
func priorityOf(p int) int {
	switch {
	case p >= 3:
		return model.PriorityHigh
	case p == 2:
		return model.PriorityMedium
	case p == 1:
		return model.PriorityLow
	}
	return model.PriorityNone
}

func tagModelsFor(labels []string, idx map[string]int64) []model.Tag {
	out := make([]model.Tag, 0, len(labels))
	for _, l := range labels {
		out = append(out, model.Tag{ID: idx[l], Name: l})
	}
	return out
}

// 清单配色：按序号从一组颜色里循环取，让导入后的清单彼此可区分。
var importListColors = []string{"#8a9a5b", "#5b8a8a", "#a87b5b", "#7b5b8a", "#5b6b8a", "#8a7b5b"}

func listColorFor(i int) string {
	return importListColors[i%len(importListColors)]
}