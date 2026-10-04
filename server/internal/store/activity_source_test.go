package store

import "testing"

// TestActivitySource 活动历史要能说清「这条是谁改的」。
//
// 单用户场景下这不是多用户审计，而是排查工具：「我明明没动它，怎么变了」——
// 答案得在来源列里。默认走网页，日历同步与导入各自标自己的来源，
// 且恢复函数必须真的恢复，否则一次同步失败会污染后面所有请求。
func TestActivitySource(t *testing.T) {
	s := newTestStore(t)
	if err := s.ClearActivities(); err != nil {
		t.Fatalf("ClearActivities: %v", err)
	}

	// 默认来源：网页。
	s.logActivity("created", 1, "界面上建的", "")
	// 标成日历同步。
	restore := s.SetActivitySource(SrcCalDAV)
	s.logActivity("completed", 2, "日历里勾掉的", "")
	restore()
	// 恢复之后应回到网页。
	s.logActivity("created", 3, "再建一条", "")

	// 嵌套：内层恢复后应回到外层的值，不是回到默认值。
	outer := s.SetActivitySource(SrcImport)
	inner := s.SetActivitySource(SrcAPI)
	s.logActivity("created", 4, "接口建的", "")
	inner()
	s.logActivity("created", 5, "导入建的", "")
	outer()
	s.logActivity("created", 6, "最后这条回网页", "")

	list, err := s.Activities(10)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	// Activities 是新的在前。
	want := []struct {
		title  string
		source string
	}{
		{"最后这条回网页", SrcWeb},
		{"导入建的", SrcImport},
		{"接口建的", SrcAPI},
		{"再建一条", SrcWeb},
		{"日历里勾掉的", SrcCalDAV},
		{"界面上建的", SrcWeb},
	}
	if len(list) != len(want) {
		t.Fatalf("活动条数 = %d，期望 %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i].Title != w.title {
			t.Errorf("[%d] title = %q，期望 %q", i, list[i].Title, w.title)
		}
		if list[i].Source != w.source {
			t.Errorf("[%d] %q 的来源 = %q，期望 %q", i, list[i].Title, list[i].Source, w.source)
		}
	}
}

// TestActivitySourceEmptyFallback 空来源按网页兜底：老记录不该显示成空白标签。
func TestActivitySourceEmptyFallback(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`INSERT INTO activities(kind, task_id, title, detail, source, created_at)
		VALUES('created', 1, '老记录', '', '', '2026-01-01T00:00:00')`); err != nil {
		t.Fatalf("插入老记录: %v", err)
	}
	list, err := s.Activities(10)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	found := false
	for _, a := range list {
		if a.Title == "老记录" {
			found = true
			if a.Source != SrcWeb {
				t.Errorf("空来源应回退为 web，得到 %q", a.Source)
			}
		}
	}
	if !found {
		t.Error("老记录没查出来")
	}
}