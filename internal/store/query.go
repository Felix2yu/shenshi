package store

import (
	"strconv"
	"strings"
	"time"
)

// 查询语法：在搜索框里直接打 #标签 @清单 p:高 due:today。
//
// 为什么不把这些做成 UI 上的独立控件：Todoist 那一类应用的杀手锏是把「筛选」压缩成
// 一行可复制的文本。做成控件，用户每次都得重新点一遍；做成文本，点一下侧栏的保存视图
// 就带走了一整套条件。这里实现的是其中最容易记住、也最常用的一小撮算子，
// 剩下的文本仍旧走多词分词——语法不认识的部分永远退化成普通搜索，不会搜不到东西。
//
// 一条硬原则：**任何无法识别的算子都当普通文本处理**，绝不报错、绝不静默丢弃。
// 搜「C#」不该因为有个 # 就丢掉半截词。

// querySpec 是解析后的查询条件：结构化算子 + 剩余的自由文本。
type querySpec struct {
	// AND 生效的条件（每项自带占位符）。同一算子出现多次时取交集。
	where []string
	args  []any

	// 结构化命中的标签 / 清单名与优先级，供上层解释与提示使用。
	tags     []string
	lists    []string
	priority *int

	// 剩余文本（已剔除所有识别出的算子），走多词分词。
	text string

	// 排除项（- 前缀）。
	notTags  []string
	notLists []string
}

// dueKeyword 把 due: 的取值翻译成到期日区间。
// today/overdue/nodate 是三种互斥的语义，week 与具体的日期区间则要算出边界。
type dueKeyword struct {
	from, to *string
	// nodate 表示「没有到期日」，无法用区间表达，单独一个开关。
	nodate bool
}

// priorityKeywords 认得两种写法：p:高 / p:3 与 p1 / 1。
// 用户从 Todoist 迁过来时写的是 p1，照着「优先级 1」的中文界面写又是「高」——都该认。
var priorityKeywords = map[string]int{
	"0": 0, "1": 1, "2": 2, "3": 3,
	"p0": 0, "p1": 1, "p2": 2, "p3": 3,
	"无": 0, "低": 1, "中": 2, "高": 3,
	"none": 0, "low": 1, "mid": 2, "medium": 2, "high": 3,
}

// ParseQuery 解析查询串。返回结构化条件与剩余自由文本。
func ParseQuery(q string, now time.Time) querySpec {
	var spec querySpec
	var rest []string

	for _, tok := range splitQueryTokens(q) {
		if tok.text == "" {
			continue
		}
		// 引号包住的整块一律当普通文本：用户打了引号就是在说「这几个字，别当算子」。
		if tok.quoted {
			rest = append(rest, tok.text)
			continue
		}
		switch {
		case strings.HasPrefix(tok.text, "#") && len(tok.text) > 1:
			spec.tags = append(spec.tags, strings.TrimPrefix(tok.text, "#"))
		case strings.HasPrefix(tok.text, "@") && len(tok.text) > 1:
			spec.lists = append(spec.lists, strings.TrimPrefix(tok.text, "@"))
		case strings.HasPrefix(tok.text, "-@") && len(tok.text) > 2:
			spec.notLists = append(spec.notLists, strings.TrimPrefix(tok.text, "-@"))
		case strings.HasPrefix(tok.text, "-#") && len(tok.text) > 2:
			spec.notTags = append(spec.notTags, strings.TrimPrefix(tok.text, "-#"))
		case strings.HasPrefix(tok.text, "p:"):
			if v, ok := priorityKeywords[strings.ToLower(strings.TrimPrefix(tok.text, "p:"))]; ok && spec.priority == nil {
				p := v
				spec.priority = &p
			} else {
				rest = append(rest, tok.text)
			}
		case strings.HasPrefix(tok.text, "due:"):
			if d, ok := parseDue(strings.TrimPrefix(tok.text, "due:"), now); ok {
				w, a := d.clause()
				spec.where = append(spec.where, w...)
				spec.args = append(spec.args, a...)
			} else {
				rest = append(rest, tok.text)
			}
		default:
			rest = append(rest, tok.text)
		}
	}
	spec.text = strings.Join(rest, " ")
	return spec
}

// queryToken 是切出来的一个词，带上它是否被引号包住。
type queryToken struct {
	text   string
	quoted bool
}

// splitQueryTokens 与 splitSearchTerms 同构，但额外记住引号边界。
// 引号在查询语法里有特殊含义——它说「这一段是字面量」，不能与切词用同一份信息。
//
// quoted 标志在**词的首字符**处取一次，而不是 flush 时才取：
// `"#重要 项目"` 的收尾引号会在 flush 之前把状态翻回 false，
// 那样整块就被误判成裸文本，算子照拆不误。
func splitQueryTokens(s string) []queryToken {
	var out []queryToken
	var cur strings.Builder
	quoted := false  // 当前是否在引号内
	has := false     // 当前是否已攒到字符
	wasQuoted := false // 本词是否始于引号内
	flush := func() {
		if has {
			if v := strings.TrimSpace(cur.String()); v != "" {
				out = append(out, queryToken{text: v, quoted: wasQuoted})
			}
		}
		cur.Reset()
		has = false
		wasQuoted = false
	}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			if !has {
				// 引号开头的词：记下它自始至终都在引号里。
				wasQuoted = true
			}
			has = true
		case (r == ' ' || r == '\t' || r == '\n') && !quoted:
			flush()
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	return out
}

// parseDue 解析 due: 的取值。不认识的取值返回 ok=false，调用方把它退回普通文本。
func parseDue(v string, now time.Time) (dueKeyword, bool) {
	day := func(offset int) *string {
		s := now.AddDate(0, 0, offset).Format("2006-01-02")
		return &s
	}
	switch strings.ToLower(v) {
	case "today":
		return dueKeyword{from: day(0), to: day(0)}, true
	case "tomorrow":
		return dueKeyword{from: day(1), to: day(1)}, true
	case "yesterday":
		return dueKeyword{from: day(-1), to: day(-1)}, true
	case "overdue":
		// 逾期没有下界，用一个极早的日期占位；上界是「昨天」（今天不算逾期）。
		early := "1970-01-01"
		return dueKeyword{from: &early, to: day(-1)}, true
	case "week":
		// 「本周」与智能清单口径一致：按自然周，周一或周日起（看 weekStart 设置，
		// 这里用服务器本地时区的周一，跨时区用户顶多差一天，可接受）。
		wd := (int(now.Weekday()) + 6) % 7 // 周一=0
		return dueKeyword{from: day(-wd), to: day(6 - wd)}, true
	case "next7":
		return dueKeyword{from: day(0), to: day(7)}, true
	case "nodate":
		return dueKeyword{nodate: true}, true
	}
	// due:2026-10-04 或 due:2026-10-04..2026-10-10
	if strings.Contains(v, "..") {
		parts := strings.SplitN(v, "..", 2)
		f, ok1 := parseDay(parts[0])
		t, ok2 := parseDay(parts[1])
		if ok1 && ok2 {
			return dueKeyword{from: &f, to: &t}, true
		}
		return dueKeyword{}, false
	}
	if d, ok := parseDay(v); ok {
		return dueKeyword{from: &d, to: &d}, true
	}
	return dueKeyword{}, false
}

func parseDay(s string) (string, bool) {
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err != nil {
		return "", false
	}
	return strings.TrimSpace(s), true
}

// clause 返回该条件对应的 WHERE 片段与参数。
func (d dueKeyword) clause() ([]string, []any) {
	if d.nodate {
		return []string{"t.due_date IS NULL"}, nil
	}
	var w []string
	var a []any
	if d.from != nil {
		w = append(w, "t.due_date >= ?")
		a = append(a, *d.from)
	}
	if d.to != nil {
		w = append(w, "t.due_date <= ?")
		a = append(a, *d.to)
	}
	return w, a
}

// apply 把解析结果翻译成 WHERE 片段，接到已有条件之后。
//
// 标签与清单按**名字**匹配而不是 id：查询语法是给人手打的，
// 让人先去别处查出 id 再回来拼 `#12` 毫无意义。名字不存在时该条件匹配不到任何任务，
// 结果就是空的——这与用户「拼错了名字」的预期一致，比静默忽略更容易被发现。
func (s querySpec) apply() ([]string, []any) {
	where := append([]string{}, s.where...)
	args := append([]any{}, s.args...)

	for _, name := range s.tags {
		where = append(where, `EXISTS (SELECT 1 FROM task_tags tt JOIN tags g ON g.id = tt.tag_id
			WHERE tt.task_id = t.id AND g.name = ?)`)
		args = append(args, name)
	}
	for _, name := range s.lists {
		where = append(where, "l.name = ?")
		args = append(args, name)
	}
	for _, name := range s.notTags {
		where = append(where, `NOT EXISTS (SELECT 1 FROM task_tags tt JOIN tags g ON g.id = tt.tag_id
			WHERE tt.task_id = t.id AND g.name = ?)`)
		args = append(args, name)
	}
	for _, name := range s.notLists {
		where = append(where, "l.name <> ?")
		args = append(args, name)
	}
	if s.priority != nil {
		where = append(where, "t.priority = ?")
		args = append(args, *s.priority)
	}
	return where, args
}

// IsEmpty 结构化部分没有任何条件，且剩余文本为空。
func (s querySpec) IsEmpty() bool {
	return len(s.where) == 0 && len(s.tags) == 0 && len(s.lists) == 0 &&
		len(s.notTags) == 0 && len(s.notLists) == 0 && s.priority == nil &&
		strings.TrimSpace(s.text) == ""
}

// Describe 人类可读地描述解析结果，用于搜索框下方的提示。
// 只描述真正落到结构化条件上的算子——剩下的普通文本不必复述，用户自己知道敲了什么。
func (s querySpec) Describe() string {
	var parts []string
	for _, t := range s.tags {
		parts = append(parts, "#"+t)
	}
	for _, l := range s.lists {
		parts = append(parts, "@"+l)
	}
	for _, t := range s.notTags {
		parts = append(parts, "-#"+t)
	}
	for _, l := range s.notLists {
		parts = append(parts, "-@"+l)
	}
	if s.priority != nil {
		parts = append(parts, "p:"+strconv.Itoa(*s.priority))
	}
	return strings.Join(parts, " ")
}