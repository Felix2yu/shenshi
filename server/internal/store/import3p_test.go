package store

import (
	"testing"

	"github.com/yufei/shendu/server/internal/model"
)

// TestParseTodoistCSV Todoist 导出解析。
func TestParseTodoistCSV(t *testing.T) {
	csv := `"content","description","project","due date","labels","priority","checked"
"写季度总结","给老板过一版","工作","2026-10-08","汇报, 重要","p4","0"
"买牛奶","","生活","","","","1"
"没有清单也没有日期","","","","","p2","0"
`
	b, prev, err := ParseThirdParty(FormatTodoistCSV, []byte(csv))
	if err != nil {
		t.Fatalf("ParseThirdParty: %v", err)
	}
	if prev.Tasks != 3 {
		t.Errorf("解析任务数 = %d，期望 3", prev.Tasks)
	}
	if prev.Completed != 1 {
		t.Errorf("已完成数 = %d，期望 1", prev.Completed)
	}
	// 清单与标签要被收集出来，好让用户知道会新建什么。
	if len(prev.Lists) != 2 {
		t.Errorf("清单 = %v，期望 2 个", prev.Lists)
	}
	if len(prev.Tags) != 2 {
		t.Errorf("标签 = %v，期望 2 个", prev.Tags)
	}
	if len(b.Tasks) != 3 {
		t.Fatalf("bundle 任务数 = %d", len(b.Tasks))
	}

	// 第一条：日期、优先级、标签。
	first := b.Tasks[0]
	if first.Title != "写季度总结" {
		t.Errorf("标题 = %q", first.Title)
	}
	if first.DueDate == nil || *first.DueDate != "2026-10-08" {
		t.Errorf("到期日 = %v，期望 2026-10-08", first.DueDate)
	}
	if first.Priority != 3 {
		t.Errorf("优先级 = %d，期望 3（p4 是最高档）", first.Priority)
	}
	if first.Notes != "给老板过一版" {
		t.Errorf("备注 = %q", first.Notes)
	}
	if len(first.Tags) != 2 {
		t.Errorf("标签数 = %d，期望 2", len(first.Tags))
	}
	if first.Status == "done" {
		t.Error("第一条未勾选，不该是已完成")
	}

	// 第二条：已勾选。
	if b.Tasks[1].Status != "done" {
		t.Errorf("已勾选的任务状态 = %q，期望 done", b.Tasks[1].Status)
	}

	// 第三条：既没清单也没日期——必须落到收件箱，不能被静默丢掉。
	third := b.Tasks[2]
	if third.ListID != inboxPlaceholderID {
		t.Errorf("无归属任务的清单 id = %d，期望收件箱占位 %d", third.ListID, inboxPlaceholderID)
	}
	if b.InboxListID != inboxPlaceholderID {
		t.Error("bundle 未声明收件箱占位，无归属任务会被整条跳过")
	}
}

// TestDetectFormat 靠内容识别格式：用户从邮件里存下来的文件常常没有正确后缀。
func TestDetectFormat(t *testing.T) {
	if got := DetectFormat("todoist.csv", nil); got != FormatTodoistCSV {
		t.Errorf("按后缀识别 CSV = %q", got)
	}
	if got := DetectFormat("export.ics", nil); got != FormatICS {
		t.Errorf("按后缀识别 ICS = %q", got)
	}
	// 无后缀：看内容。
	if got := DetectFormat("download", []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")); got != FormatICS {
		t.Errorf("按内容识别 ICS = %q", got)
	}
	if got := DetectFormat("download", []byte("\"content\",\"project\"\n\"a\",\"b\"\n")); got != FormatTodoistCSV {
		t.Errorf("按内容识别 CSV = %q", got)
	}
	if got := DetectFormat("random.bin", []byte("nonsense")); got != "" {
		t.Errorf("认不出的文件应返回空，得到 %q", got)
	}
}

// TestParseICS iCalendar 解析：VTODO 与 VEVENT 都收，状态与分类要认。
func TestParseICS(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//test//EN
BEGIN:VTODO
SUMMARY:交周报
DESCRIPTION:发给团队
DTSTART;VALUE=DATE:20261007
CATEGORIES:工作,例行
STATUS:NEEDS-ACTION
END:VTODO
BEGIN:VTODO
SUMMARY:已交的
DTSTART;VALUE=DATE:20261001
STATUS:COMPLETED
END:VTODO
BEGIN:VEVENT
SUMMARY:周会
DTSTART:20261009T100000Z
END:VEVENT
END:VCALENDAR
`
	b, prev, err := ParseThirdParty(FormatICS, []byte(ics))
	if err != nil {
		t.Fatalf("ParseThirdParty: %v", err)
	}
	// VTODO 两条 + VEVENT 一条 = 3。丢掉会议会让很多用户的导入结果莫名其妙空一半。
	if prev.Tasks != 3 {
		t.Errorf("解析任务数 = %d，期望 3", prev.Tasks)
	}
	if prev.Completed != 1 {
		t.Errorf("已完成数 = %d，期望 1", prev.Completed)
	}
	if b.Tasks[0].Title != "交周报" {
		t.Errorf("标题 = %q", b.Tasks[0].Title)
	}
	if b.Tasks[0].DueDate == nil || *b.Tasks[0].DueDate != "2026-10-07" {
		t.Errorf("VALUE=DATE 的 DTSTART 应解析出 2026-10-07，得到 %v", b.Tasks[0].DueDate)
	}
	if len(b.Tasks[0].Tags) != 2 {
		t.Errorf("CATEGORIES 应拆成 2 个标签，得到 %d", len(b.Tasks[0].Tags))
	}
	// 带时刻的 DTSTART 也要能取出日期。
	if b.Tasks[2].DueDate == nil || *b.Tasks[2].DueDate != "2026-10-09" {
		t.Errorf("VEVENT 的 DTSTART 应解析出 2026-10-09，得到 %v", b.Tasks[2].DueDate)
	}
}

// TestImportThirdParty 端到端：解析出来的东西要真的能落库。
func TestImportThirdParty(t *testing.T) {
	s := newTestStore(t)
	clearTasks(t, s)
	if err := s.ClearActivities(); err != nil {
		t.Fatalf("ClearActivities: %v", err)
	}

	csv := `"content","project","labels"
"导入的事一","工作","重要"
"导入的事二","",""
`
	res, prev, err := s.ImportThirdParty("todoist.csv", []byte(csv))
	if err != nil {
		t.Fatalf("ImportThirdParty: %v", err)
	}
	if prev.Tasks != 2 {
		t.Errorf("预览任务数 = %d", prev.Tasks)
	}
	if res.Tasks != 2 {
		t.Errorf("实际导入任务数 = %d，期望 2", res.Tasks)
	}

	// 两条都得在库里 —— 无归属的那条要落进收件箱而不是被丢掉。
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("库里任务数 = %d，期望 2", n)
	}

	// 清单按名字建出来。
	var listName string
	if err := s.db.QueryRow(`SELECT l.name FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.title = '导入的事一'`).Scan(&listName); err != nil {
		t.Fatalf("查清单: %v", err)
	}
	if listName != "工作" {
		t.Errorf("清单名 = %q，期望「工作」", listName)
	}

	// 标签关系挂上了。
	var tagName string
	if err := s.db.QueryRow(`SELECT g.name FROM task_tags tt JOIN tags g ON g.id = tt.tag_id
		JOIN tasks t ON t.id = tt.task_id WHERE t.title = '导入的事一'`).Scan(&tagName); err != nil {
		t.Fatalf("查标签: %v", err)
	}
	if tagName != "重要" {
		t.Errorf("标签 = %q，期望「重要」", tagName)
	}

	// 活动留一条汇总，来源标成导入：事后回看时能分清这批不是自己在界面上建的，
	// 也能想起来「上周导进来那批东西是哪来的」。
	var kind, src, detail string
	if err := s.db.QueryRow(`SELECT kind, source, detail FROM activities ORDER BY id DESC LIMIT 1`).
		Scan(&kind, &src, &detail); err != nil {
		t.Fatalf("查活动: %v", err)
	}
	if kind != model.ActImported {
		t.Errorf("活动 kind = %q，期望 %q", kind, model.ActImported)
	}
	if src != SrcImport {
		t.Errorf("活动来源 = %q，期望 %q", src, SrcImport)
	}
	if detail == "" {
		t.Error("汇总活动该说明导入了多少")
	}
	// 逐条不记：一次导三条不该产生三条历史（导三百条会把历史冲干净）。
	var nAct int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM activities`).Scan(&nAct); err != nil {
		t.Fatalf("count activities: %v", err)
	}
	if nAct != 1 {
		t.Errorf("活动条数 = %d，期望 1（只留汇总）", nAct)
	}
}

// TestThirdPartyRejectsGarbage 认不出或没内容时要明确报错，不能静默导入 0 条。
func TestThirdPartyRejectsGarbage(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.ImportThirdParty("random.bin", []byte("nonsense")); err == nil {
		t.Error("认不出的格式应报错")
	}
	if _, _, err := s.ImportThirdParty("todoist.csv", []byte(`"content","project"`+"\n")); err == nil {
		t.Error("没有任务的 CSV 应报错，而不是「成功导入 0 条」")
	}
	if _, err := s.PreviewThirdParty("random.bin", []byte("nonsense")); err == nil {
		t.Error("预览认不出的格式也应报错")
	}
}