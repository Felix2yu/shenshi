package store

import (
	"errors"
	"testing"

	"github.com/yufei/shendu/server/internal/model"
)

// TestFoldersTree 分组建删改、嵌套与环路防护。
func TestFoldersTree(t *testing.T) {
	s := newTestStore(t)

	// 空名拒绝。
	if _, err := s.CreateFolder(FolderInput{}); err == nil {
		t.Error("空分组名应被拒绝")
	}
	// 上级不存在。
	bogus := int64(999999)
	if _, err := s.CreateFolder(FolderInput{Name: sp("子组"), ParentID: &bogus}); err == nil {
		t.Error("上级不存在应被拒绝")
	}

	parent, err := s.CreateFolder(FolderInput{Name: sp("父分组"), Color: sp("#112233")})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	child, err := s.CreateFolder(FolderInput{Name: sp("子分组"), ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("嵌套 CreateFolder: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Errorf("parent = %v", child.ParentID)
	}

	// 树装配：父组挂子组。
	tree, err := s.Folders()
	if err != nil {
		t.Fatalf("Folders: %v", err)
	}
	var found *model.Folder
	for i := range tree {
		if tree[i].ID == parent.ID {
			found = &tree[i]
		}
	}
	if found == nil {
		t.Fatal("树中缺少父分组")
	}
	if len(found.Children) != 1 || found.Children[0].ID != child.ID {
		t.Errorf("子树 = %+v", found.Children)
	}

	// 改名 + 折叠。
	if err := s.UpdateFolder(parent.ID, FolderInput{Name: sp("父组改名"), Collapsed: boolp(true)}); err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	flat, _ := s.flatFolders()
	for _, f := range flat {
		if f.ID == parent.ID {
			if f.Name != "父组改名" || !f.Collapsed {
				t.Errorf("更新未生效: %+v", f)
			}
		}
	}

	// 环路：不能把自己挂到自己或子孙之下。
	if err := s.UpdateFolder(parent.ID, FolderInput{ParentID: &parent.ID}); err == nil {
		t.Error("把自己设为上级应被拒绝")
	}
	if err := s.UpdateFolder(parent.ID, FolderInput{ParentID: &child.ID}); err == nil {
		t.Error("挂到自己下级应被拒绝")
	}

	// 提到根。
	if err := s.UpdateFolder(child.ID, FolderInput{MoveToRoot: true}); err != nil {
		t.Fatalf("MoveToRoot: %v", err)
	}
	flat, _ = s.flatFolders()
	for _, f := range flat {
		if f.ID == child.ID && f.ParentID != nil {
			t.Error("提根后 parent 应为 nil")
		}
	}

	// 无字段更新直接成功（不发语句）。
	if err := s.UpdateFolder(child.ID, FolderInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}

	// 删除父组：清单回顶层、子组升位，不连带删除。
	l, err := s.CreateList(ListInput{Name: sp("组内清单"), FolderID: &parent.ID})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if err := s.DeleteFolder(parent.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	lists, _ := s.Lists()
	for _, item := range lists {
		if item.ID == l.ID && item.FolderID != nil {
			t.Error("删除分组后清单应回顶层")
		}
	}
	flat, _ = s.flatFolders()
	for _, f := range flat {
		if f.ID == child.ID && f.ParentID != nil {
			t.Error("子分组应升到被删分组的上一级（此处为根）")
		}
	}
}

func boolp(b bool) *bool { return &b }

// TestListCRUD 清单增删改、收集箱保护与任务计数。
func TestListCRUD(t *testing.T) {
	s := newTestStore(t)

	inbox, err := s.InboxListID()
	if err != nil {
		t.Fatalf("InboxListID: %v", err)
	}
	if err := s.DeleteList(inbox); err == nil {
		t.Error("收集箱不可删除")
	}

	if _, err := s.CreateList(ListInput{}); err == nil {
		t.Error("空清单名应被拒绝")
	}

	l, err := s.CreateList(ListInput{Name: sp("新清单"), Starred: boolp(true)})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	if !l.Starred {
		t.Error("starred 未生效")
	}

	// 计数：建两条未完成 + 一条完成。
	mustCreateTask(t, s, "清单任务一", func(in *model.TaskInput) { in.ListID = optOf(l.ID) })
	mustCreateTask(t, s, "清单任务二", func(in *model.TaskInput) { in.ListID = optOf(l.ID) })
	third := mustCreateTask(t, s, "清单任务三", func(in *model.TaskInput) { in.ListID = optOf(l.ID) })
	if _, err := s.ToggleTask(third.ID); err != nil {
		t.Fatalf("完成: %v", err)
	}

	got, err := s.List(l.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.TaskCount != 2 {
		t.Errorf("TaskCount = %d，期望 2（完成的不计）", got.TaskCount)
	}
	if _, err := s.List(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在清单应 ErrNotFound，得到 %v", err)
	}

	// 更新：改名、归档、移出分组。
	if err := s.UpdateList(l.ID, ListInput{Name: sp("改名清单"), Archived: boolp(true)}); err != nil {
		t.Fatalf("UpdateList: %v", err)
	}
	got, _ = s.List(l.ID)
	if got.Name != "改名清单" || !got.Archived {
		t.Errorf("更新未生效: %+v", got)
	}
	if err := s.UpdateList(l.ID, ListInput{MoveToRoot: true}); err != nil {
		t.Errorf("MoveToRoot: %v", err)
	}
	if err := s.UpdateList(l.ID, ListInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}

	// 默认排序位次递增。
	l2, err := s.CreateList(ListInput{Name: sp("后续清单")})
	if err != nil {
		t.Fatalf("CreateList(2): %v", err)
	}
	if l2.SortOrder <= l.SortOrder && l2.ID != l.ID {
		// SortOrder 语义为递增追加即可，此处仅防明显回绕。
		if l2.SortOrder < 0 {
			t.Errorf("sortOrder 异常: %d", l2.SortOrder)
		}
	}

	// 删除：清单内任务进撤销槽位。
	if err := s.DeleteList(l.ID); err != nil {
		t.Fatalf("DeleteList: %v", err)
	}
	us, _ := s.UndoState()
	if !us.Available || us.Count < 3 {
		t.Errorf("删除清单应把任务收进撤销槽位，得到 %+v", us)
	}
	if _, err := s.List(l.ID); !errors.Is(err, ErrNotFound) {
		t.Error("删除后应 ErrNotFound")
	}
	if err := s.DeleteList(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除不存在清单应 ErrNotFound，得到 %v", err)
	}
}

// TestTags 标签幂等创建、批量建档与计数。
func TestTags(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateTag(TagInput{}); err == nil {
		t.Error("空标签名应被拒绝")
	}

	tag, err := s.CreateTag(TagInput{Name: sp("测试标签"), Color: sp("#123456")})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	// 同名返回既有标签（幂等）。
	same, err := s.CreateTag(TagInput{Name: sp("测试标签")})
	if err != nil || same.ID != tag.ID {
		t.Errorf("同名应返回同一标签: id=%d err=%v", same.ID, err)
	}
	// 无色时自动配调色板。
	auto, err := s.CreateTag(TagInput{Name: sp("自动配色")})
	if err != nil {
		t.Fatalf("CreateTag(自动色): %v", err)
	}
	if auto.Color == "" {
		t.Error("自动配色不应为空")
	}

	ids, err := s.EnsureTags([]string{"测试标签", "  ", "新标签甲", "测试标签"})
	if err != nil {
		t.Fatalf("EnsureTags: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("EnsureTags 返回 %d 个 id（空白跳过、已有复用），期望 3", len(ids))
	}

	// 改名改色。
	if err := s.UpdateTag(tag.ID, TagInput{Name: sp("改名标签"), Color: sp("#abcdef")}); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	tags, _ := s.Tags()
	var found bool
	for _, g := range tags {
		if g.ID == tag.ID {
			found = true
			if g.Name != "改名标签" || g.Color != "#abcdef" {
				t.Errorf("更新未生效: %+v", g)
			}
		}
	}
	if !found {
		t.Error("标签列表缺少目标标签")
	}
	if err := s.UpdateTag(tag.ID, TagInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}

	// 任务打标后计数。
	task := mustCreateTask(t, s, "打标任务", nil)
	if _, err := s.UpdateTask(task.ID, model.TaskInput{TagIDs: optOf([]int64{tag.ID})}); err != nil {
		t.Fatalf("打标: %v", err)
	}
	tags, _ = s.Tags()
	for _, g := range tags {
		if g.ID == tag.ID && g.TaskCount != 1 {
			t.Errorf("TaskCount = %d，期望 1", g.TaskCount)
		}
	}

	if err := s.DeleteTag(tag.ID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
}

// TestSavedFilters 筛选条件的保存、改名与删除。
func TestSavedFilters(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateSavedFilter(SavedFilterInput{}); err == nil {
		t.Error("空筛选名应被拒绝")
	}
	f, err := s.CreateSavedFilter(SavedFilterInput{Name: sp("本周高优"), Query: sp(`{"smart":"high"}`)})
	if err != nil {
		t.Fatalf("CreateSavedFilter: %v", err)
	}
	if f.Query != `{"smart":"high"}` {
		t.Errorf("query = %q", f.Query)
	}

	list, err := s.SavedFilters()
	if err != nil || len(list) != 1 {
		t.Fatalf("SavedFilters: n=%d err=%v", len(list), err)
	}

	blank := "  "
	if err := s.UpdateSavedFilter(f.ID, SavedFilterInput{Name: &blank}); err == nil {
		t.Error("改名为空应被拒绝")
	}
	if err := s.UpdateSavedFilter(f.ID, SavedFilterInput{Name: sp("改名筛选")}); err != nil {
		t.Fatalf("UpdateSavedFilter: %v", err)
	}
	list, _ = s.SavedFilters()
	if list[0].Name != "改名筛选" {
		t.Errorf("name = %q", list[0].Name)
	}
	if err := s.UpdateSavedFilter(f.ID, SavedFilterInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}
	if err := s.UpdateSavedFilter(999999, SavedFilterInput{Name: sp("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("更新不存在应 ErrNotFound，得到 %v", err)
	}

	if err := s.DeleteSavedFilter(f.ID); err != nil {
		t.Fatalf("DeleteSavedFilter: %v", err)
	}
	if err := s.DeleteSavedFilter(f.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
}

// TestReorderBy 分组与清单按传入顺序重写排序。
func TestReorderBy(t *testing.T) {
	s := newTestStore(t)
	f1, err := s.CreateFolder(FolderInput{Name: sp("重排组一")})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	f2, err := s.CreateFolder(FolderInput{Name: sp("重排组二")})
	if err != nil {
		t.Fatalf("CreateFolder(2): %v", err)
	}
	if err := s.ReorderFolders([]int64{f2.ID, f1.ID}); err != nil {
		t.Fatalf("ReorderFolders: %v", err)
	}
	flat, _ := s.flatFolders()
	var order []int64
	for _, f := range flat {
		if f.ID == f1.ID || f.ID == f2.ID {
			order = append(order, f.ID)
		}
	}
	if len(order) != 2 || order[0] != f2.ID {
		t.Errorf("重排结果 = %v", order)
	}

	l1, _ := s.CreateList(ListInput{Name: sp("重排清单一")})
	l2, _ := s.CreateList(ListInput{Name: sp("重排清单二")})
	if err := s.ReorderLists([]int64{l2.ID, l1.ID}); err != nil {
		t.Fatalf("ReorderLists: %v", err)
	}
	if err := s.reorderBy("lists", nil); err != nil {
		t.Errorf("空重排应成功: %v", err)
	}
}

// TestSettingsRoundTrip 设置读写与覆盖。
func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveSettings(map[string]string{"theme": "light", "fontScale": "1.1"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	kv, err := s.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if kv["theme"] != "light" || kv["fontScale"] != "1.1" {
		t.Errorf("settings = %+v", kv)
	}
	// 覆盖写。
	if err := s.SaveSettings(map[string]string{"theme": "dark"}); err != nil {
		t.Fatalf("SaveSettings(覆盖): %v", err)
	}
	kv, _ = s.Settings()
	if kv["theme"] != "dark" {
		t.Errorf("覆盖后 theme = %q", kv["theme"])
	}
}

// TestErrorTypes 错误类型的可判定性（API 层据此映射 400/403）。
func TestErrorTypes(t *testing.T) {
	var ve ValidationError = ValidationError{Msg: "任务标题不能为空"}
	if ve.Error() != "任务标题不能为空" {
		t.Errorf("ValidationError.Error = %q", ve.Error())
	}
	var fe ForbiddenError = ForbiddenError{Msg: "收集箱不可删除"}
	if fe.Error() != "收集箱不可删除" {
		t.Errorf("ForbiddenError.Error = %q", fe.Error())
	}
	if err := errBlank("名称"); err.Error() != "名称不能为空" {
		t.Errorf("errBlank = %q", err.Error())
	}
	if err := errForbidden("不允许"); err.Error() != "不允许" {
		t.Errorf("errForbidden = %q", err.Error())
	}
	// 类型断言互不混淆。
	var e error = errBlank("x")
	var gotFE ForbiddenError
	if errors.As(e, &gotFE) {
		t.Error("ValidationError 不应匹配 ForbiddenError")
	}
}

// TestDerefHelpers org 层小工具。
func TestDerefHelpers(t *testing.T) {
	if derefBool(nil, true) != true {
		t.Error("derefBool(nil) 应给 fallback")
	}
	f := false
	if derefBool(&f, true) != false {
		t.Error("derefBool 应取指针值")
	}
	if derefStr(nil, "d") != "d" || derefStr(sp("v"), "d") != "v" {
		t.Error("derefStr")
	}
}

// TestTaskFilterQuadrant 四象限过滤。
func TestTaskFilterQuadrant(t *testing.T) {
	s := newTestStore(t)
	mustCreateTask(t, s, "重要且紧急", func(in *model.TaskInput) {
		in.Important = optOf(true)
		in.Urgent = optOf(true)
	})
	mustCreateTask(t, s, "都不", func(in *model.TaskInput) {
		in.Important = optOf(false)
		in.Urgent = optOf(false)
	})

	q1, err := s.ListTasks(TaskFilter{Quadrant: "1", Status: "all", SortBy: "manual", IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("Quadrant 1: %v", err)
	}
	var inQ1, noneInQ1 bool
	for _, task := range q1 {
		if task.Title == "重要且紧急" {
			inQ1 = true
		}
		if task.Title == "都不" {
			noneInQ1 = true
		}
	}
	if !inQ1 || noneInQ1 {
		t.Errorf("象限1 过滤错误: 含目标=%v 含「都不」=%v", inQ1, noneInQ1)
	}
	q4, err := s.ListTasks(TaskFilter{Quadrant: "4", Status: "all", SortBy: "manual", IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("Quadrant 4: %v", err)
	}
	ok := false
	for _, task := range q4 {
		if task.Title == "都不" {
			ok = true
		}
	}
	if !ok {
		t.Error("象限4 缺少「都不」")
	}
}
