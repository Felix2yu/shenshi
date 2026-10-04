package store

import (
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// TestParseQueryOperators 算子解析：结构化的部分被摘走，剩下的仍是自由文本。
func TestParseQueryOperators(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.Local) // 周日

	cases := []struct {
		in        string
		wantTags  []string
		wantLists []string
		wantPri   *int
		wantText  string
	}{
		{"#重要", []string{"重要"}, nil, nil, ""},
		{"@工作", nil, []string{"工作"}, nil, ""},
		{"p:高", nil, nil, ptrOf(3), ""},
		{"p:1", nil, nil, ptrOf(1), ""},
		{"#重要 #紧急", []string{"重要", "紧急"}, nil, nil, ""},
		{"季度 材料", nil, nil, nil, "季度 材料"},
		{"#重要 季度", []string{"重要"}, nil, nil, "季度"},
		{"-#归档", nil, nil, nil, ""},
		{"-@工作", nil, nil, nil, ""},
		// 识别不出的算子必须原样退回文本，绝不吞掉。
		{"due:下周三", nil, nil, nil, "due:下周三"},
		{"p:urgent", nil, nil, nil, "p:urgent"},
		// 光一个 # 不构成算子（用户可能搜 C# 之类）。
		{"C#", nil, nil, nil, "C#"},
		// 引号内整块当普通文本。
		{`"#重要 项目"`, nil, nil, nil, "#重要 项目"},
	}
	for _, c := range cases {
		spec := ParseQuery(c.in, now)
		if len(spec.tags) != len(c.wantTags) {
			t.Errorf("%q: tags = %v，期望 %v", c.in, spec.tags, c.wantTags)
			continue
		}
		for i, v := range c.wantTags {
			if spec.tags[i] != v {
				t.Errorf("%q: tags[%d] = %q，期望 %q", c.in, i, spec.tags[i], v)
			}
		}
		if len(spec.lists) != len(c.wantLists) {
			t.Errorf("%q: lists = %v，期望 %v", c.in, spec.lists, c.wantLists)
		}
		switch {
		case c.wantPri == nil && spec.priority != nil:
			t.Errorf("%q: 不该解析出优先级，得到 %d", c.in, *spec.priority)
		case c.wantPri != nil && spec.priority == nil:
			t.Errorf("%q: 应解析出优先级 %d", c.in, *c.wantPri)
		case c.wantPri != nil && *spec.priority != *c.wantPri:
			t.Errorf("%q: priority = %d，期望 %d", c.in, *spec.priority, *c.wantPri)
		}
		if spec.text != c.wantText {
			t.Errorf("%q: text = %q，期望 %q", c.in, spec.text, c.wantText)
		}
	}
}

// TestParseQueryDue due: 的取值要翻成正确的日期区间。
func TestParseQueryDue(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 0, 0, 0, time.Local)
	s := ParseQuery("due:today", now)
	if len(s.where) != 2 {
		t.Fatalf("due:today 应产出上下界两个条件，得到 %v", s.where)
	}
	if s.args[0] != "2026-10-04" || s.args[1] != "2026-10-04" {
		t.Errorf("due:today 区间 = %v，期望 2026-10-04 ~ 2026-10-04", s.args)
	}

	// 逾期：上界是昨天。
	s = ParseQuery("due:overdue", now)
	if s.args[len(s.args)-1] != "2026-10-03" {
		t.Errorf("due:overdue 上界 = %v，期望 2026-10-03", s.args[len(s.args)-1])
	}

	// 无日期：一个 IS NULL 就够，不该产出区间。
	s = ParseQuery("due:nodate", now)
	if len(s.where) != 1 || s.where[0] != "t.due_date IS NULL" {
		t.Errorf("due:nodate 条件 = %v", s.where)
	}

	// 显式区间。
	s = ParseQuery("due:2026-10-01..2026-10-03", now)
	if len(s.args) != 2 || s.args[0] != "2026-10-01" || s.args[1] != "2026-10-03" {
		t.Errorf("区间解析 = %v", s.args)
	}

	// 本周：周日应回溯到周一（10-04 是周日，周一是 09-28）。
	s = ParseQuery("due:week", now)
	if s.args[0] != "2026-09-28" || s.args[1] != "2026-10-04" {
		t.Errorf("due:week = %v，期望 2026-09-28 ~ 2026-10-04", s.args)
	}
}

func ptrOf[T any](v T) *T { return &v }

// clearTasks 清空任务表。Open 会 seed 两条示例任务，计数与命中类断言必须先清干净。
func clearTasks(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec(`DELETE FROM tasks`); err != nil {
		t.Fatalf("清空任务: %v", err)
	}
}

// TestQuerySyntaxEndToEnd 结构化算子要真的筛出对的任务，并与多词文本搜索共存。
func TestQuerySyntaxEndToEnd(t *testing.T) {
	s := newTestStore(t)
	clearTasks(t, s)

	tag, err := s.CreateTag(TagInput{Name: sp("重要")})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	list, err := s.CreateList(ListInput{Name: sp("工作")})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	// A：带 #重要 标签、在「工作」清单、今天到期、优先级高。
	a := mustCreateTask(t, s, "季度材料汇总", func(in *model.TaskInput) {
		in.ListID = optOf(list.ID)
		in.TagIDs = optOf([]int64{tag.ID})
		in.DueDate = optOf(&today)
		in.Priority = optOf(model.PriorityHigh)
	})
	_ = a
	// B：同样在「工作」清单，但没打标签、优先级低、明天到期。
	mustCreateTask(t, s, "无关杂事", func(in *model.TaskInput) {
		in.ListID = optOf(list.ID)
		in.DueDate = optOf(&tomorrow)
		in.Priority = optOf(model.PriorityLow)
	})
	// C：打了标签但不在那个清单。
	mustCreateTask(t, s, "别处的重要事", func(in *model.TaskInput) {
		in.TagIDs = optOf([]int64{tag.ID})
		in.DueDate = optOf(&today)
		in.Priority = optOf(model.PriorityHigh)
	})

	ids := func(f TaskFilter) map[string]bool {
		t.Helper()
		tasks, err := s.ListTasks(f)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		out := map[string]bool{}
		for _, task := range tasks {
			out[task.Title] = true
		}
		return out
	}

	// #重要 只命中打了标签的两条。
	got := ids(TaskFilter{Search: "#重要", Status: "all"})
	if len(got) != 2 || !got["季度材料汇总"] || !got["别处的重要事"] {
		t.Errorf("#重要 命中 %v，期望两条", got)
	}

	// 三个算子取交集：只剩 A。
	got = ids(TaskFilter{Search: "#重要 @工作 p:高 due:today", Status: "all"})
	if len(got) != 1 || !got["季度材料汇总"] {
		t.Errorf("组合算子命中 %v，期望只剩「季度材料汇总」", got)
	}

	// 算子与自由文本共存：#重要 加上「季度」两词。
	got = ids(TaskFilter{Search: "#重要 季度", Status: "all"})
	if len(got) != 1 || !got["季度材料汇总"] {
		t.Errorf("#重要 + 文本命中 %v", got)
	}

	// 纯文本搜索（无算子）仍按老口径工作。
	got = ids(TaskFilter{Search: "杂事", Status: "all"})
	if len(got) != 1 || !got["无关杂事"] {
		t.Errorf("纯文本搜索命中 %v", got)
	}

	// 排除：#重要 -@工作 去掉在「工作」清单里的那条。
	got = ids(TaskFilter{Search: "#重要 -@工作", Status: "all"})
	if len(got) != 1 || !got["别处的重要事"] {
		t.Errorf("排除命中 %v，期望只剩「别处的重要事」", got)
	}

	// due:today 只看今天到期。
	got = ids(TaskFilter{Search: "due:today", Status: "all"})
	if len(got) != 2 {
		t.Errorf("due:today 命中 %v，期望两条", got)
	}
}