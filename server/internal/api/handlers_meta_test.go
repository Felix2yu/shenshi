package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// taskInputTitle 构造一条最小任务输入。
func taskInputTitle(title string) model.TaskInput {
	return model.TaskInput{Title: model.Opt[string]{Set: true, Value: title}}
}

// TestFolderHandlers 分组建删改与重排。
func TestFolderHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.listFolders(w, httptest.NewRequest("GET", "/api/folders", nil)); err != nil {
		t.Fatalf("listFolders: %v", err)
	}
	if !strings.Contains(w.Body.String(), "folders") {
		t.Errorf("body = %s", w.Body.String())
	}

	// create 空名 → 校验错误。
	w = httptest.NewRecorder()
	if err := s.createFolder(w, httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":""}`))); err == nil {
		t.Error("空名应被拒绝")
	}
	// create 成功。
	w = httptest.NewRecorder()
	if err := s.createFolder(w, httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":"测试分组"}`))); err != nil {
		t.Fatalf("createFolder: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var f model.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.ID == 0 {
		t.Fatalf("folder = %+v err=%v", f, err)
	}

	// update / delete。
	id := itoa(f.ID)
	w = httptest.NewRecorder()
	if err := s.updateFolder(w, reqWithID("PATCH", "/api/folders/"+id, id, `{"name":"改名分组"}`)); err != nil {
		t.Fatalf("updateFolder: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteFolder(w, reqWithID("DELETE", "/api/folders/"+id, id, "")); err != nil {
		t.Fatalf("deleteFolder: %v", err)
	}
	// 重复删除。
	w = httptest.NewRecorder()
	if err := s.deleteFolder(w, reqWithID("DELETE", "/api/folders/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}

	// reorder：空序列 → 校验错误。
	w = httptest.NewRecorder()
	if err := s.reorderFolders(w, httptest.NewRequest("POST", "/api/folders/reorder", strings.NewReader(`{"ids":[]}`))); err == nil {
		t.Error("空序列应被拒绝")
	}
	w = httptest.NewRecorder()
	if err := s.reorderFolders(w, httptest.NewRequest("POST", "/api/folders/reorder", strings.NewReader(`{"ids":[1,2]}`))); err != nil {
		t.Errorf("reorderFolders: %v", err)
	}
	// reorder 坏 JSON。
	w = httptest.NewRecorder()
	if err := s.reorderFolders(w, httptest.NewRequest("POST", "/api/folders/reorder", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

// TestListHandlers 清单 CRUD 与收集箱保护。
func TestListHandlers(t *testing.T) {
	s, st := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.listLists(w, httptest.NewRequest("GET", "/api/lists", nil)); err != nil {
		t.Fatalf("listLists: %v", err)
	}
	if !strings.Contains(w.Body.String(), "lists") {
		t.Errorf("body = %s", w.Body.String())
	}

	// create。
	w = httptest.NewRecorder()
	if err := s.createList(w, httptest.NewRequest("POST", "/api/lists", strings.NewReader(`{"name":"接口清单"}`))); err != nil {
		t.Fatalf("createList: %v", err)
	}
	var l model.List
	if err := json.Unmarshal(w.Body.Bytes(), &l); err != nil || l.ID == 0 {
		t.Fatalf("list = %+v err=%v", l, err)
	}

	// update。
	id := itoa(l.ID)
	w = httptest.NewRecorder()
	if err := s.updateList(w, reqWithID("PATCH", "/api/lists/"+id, id, `{"name":"接口清单2"}`)); err != nil {
		t.Fatalf("updateList: %v", err)
	}
	got, err := st.List(l.ID)
	if err != nil || got.Name != "接口清单2" {
		t.Errorf("list = %+v err=%v", got, err)
	}

	// delete。
	w = httptest.NewRecorder()
	if err := s.deleteList(w, reqWithID("DELETE", "/api/lists/"+id, id, "")); err != nil {
		t.Fatalf("deleteList: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteList(w, reqWithID("DELETE", "/api/lists/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}

	// 收集箱删除 → Forbidden。
	inbox, _ := st.InboxListID()
	inboxID := itoa(inbox)
	w = httptest.NewRecorder()
	if err := s.deleteList(w, reqWithID("DELETE", "/api/lists/"+inboxID, inboxID, "")); err == nil {
		t.Error("收集箱应不可删除")
	} else {
		var fe store.ForbiddenError
		if !errorsAs(err, &fe) {
			t.Errorf("应为 ForbiddenError，得到 %T", err)
		}
	}

	// reorder lists。
	w = httptest.NewRecorder()
	if err := s.reorderLists(w, httptest.NewRequest("POST", "/api/lists/reorder", strings.NewReader(`{"ids":[1]}`))); err != nil {
		t.Errorf("reorderLists: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.reorderLists(w, httptest.NewRequest("POST", "/api/lists/reorder", strings.NewReader(`{"ids":[]}`))); err == nil {
		t.Error("空序列应被拒绝")
	}
}

func errorsAs(err error, target any) bool {
	switch t := target.(type) {
	case *store.ForbiddenError:
		if fe, ok := err.(store.ForbiddenError); ok {
			*t = fe
			return true
		}
	}
	return false
}

// TestTagHandlers 标签 CRUD 与批量建档。
func TestTagHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.listTags(w, httptest.NewRequest("GET", "/api/tags", nil)); err != nil {
		t.Fatalf("listTags: %v", err)
	}
	if !strings.Contains(w.Body.String(), "tags") {
		t.Errorf("body = %s", w.Body.String())
	}

	// create 空名 → 拒绝。
	w = httptest.NewRecorder()
	if err := s.createTag(w, httptest.NewRequest("POST", "/api/tags", strings.NewReader(`{"name":""}`))); err == nil {
		t.Error("空名应被拒绝")
	}
	w = httptest.NewRecorder()
	if err := s.createTag(w, httptest.NewRequest("POST", "/api/tags", strings.NewReader(`{"name":"接口标签"}`))); err != nil {
		t.Fatalf("createTag: %v", err)
	}
	var tag model.Tag
	if err := json.Unmarshal(w.Body.Bytes(), &tag); err != nil || tag.ID == 0 {
		t.Fatalf("tag = %+v err=%v", tag, err)
	}

	// ensureTags：已有复用 + 新建 + 空白跳过。
	w = httptest.NewRecorder()
	eb := `{"names":["接口标签","  ","新标签X"]}`
	if err := s.ensureTags(w, httptest.NewRequest("POST", "/api/tags/ensure", strings.NewReader(eb))); err != nil {
		t.Fatalf("ensureTags: %v", err)
	}
	var er struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil || len(er.IDs) != 2 {
		t.Errorf("ensure = %+v err=%v", er, err)
	}

	// update / delete。
	id := itoa(tag.ID)
	w = httptest.NewRecorder()
	if err := s.updateTag(w, reqWithID("PATCH", "/api/tags/"+id, id, `{"name":"改名标签"}`)); err != nil {
		t.Fatalf("updateTag: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteTag(w, reqWithID("DELETE", "/api/tags/"+id, id, "")); err != nil {
		t.Fatalf("deleteTag: %v", err)
	}
	// DeleteTag 不查 RowsAffected：重复删除幂等成功（与 DeleteFolder/DeleteList 不同）。
	w = httptest.NewRecorder()
	if err := s.deleteTag(w, reqWithID("DELETE", "/api/tags/"+id, id, "")); err != nil {
		t.Errorf("重复删除应幂等成功，得到 %v", err)
	}
	// update 非法 id。
	w = httptest.NewRecorder()
	if err := s.updateTag(w, reqWithID("PATCH", "/api/tags/x", "x", `{}`)); err == nil {
		t.Error("非法 id 应报错")
	}
}

// TestSavedFilterHandlers 筛选条件 CRUD。
func TestSavedFilterHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.listSavedFilters(w, httptest.NewRequest("GET", "/api/saved-filters", nil)); err != nil {
		t.Fatalf("listSavedFilters: %v", err)
	}

	// create 空名 → 拒绝。
	w = httptest.NewRecorder()
	if err := s.createSavedFilter(w, httptest.NewRequest("POST", "/api/saved-filters", strings.NewReader(`{"name":""}`))); err == nil {
		t.Error("空名应被拒绝")
	}
	w = httptest.NewRecorder()
	cb := `{"name":"本周","query":"{\"smart\":\"today\"}"}`
	if err := s.createSavedFilter(w, httptest.NewRequest("POST", "/api/saved-filters", strings.NewReader(cb))); err != nil {
		t.Fatalf("createSavedFilter: %v", err)
	}
	var f model.SavedFilter
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.ID == 0 {
		t.Fatalf("filter = %+v err=%v", f, err)
	}

	id := itoa(f.ID)
	w = httptest.NewRecorder()
	if err := s.updateSavedFilter(w, reqWithID("PATCH", "/api/saved-filters/"+id, id, `{"name":"本周改"}`)); err != nil {
		t.Fatalf("updateSavedFilter: %v", err)
	}
	// 空白改名 → 拒绝。
	w = httptest.NewRecorder()
	if err := s.updateSavedFilter(w, reqWithID("PATCH", "/api/saved-filters/"+id, id, `{"name":"  "}`)); err == nil {
		t.Error("空白改名应被拒绝")
	}
	w = httptest.NewRecorder()
	if err := s.deleteSavedFilter(w, reqWithID("DELETE", "/api/saved-filters/"+id, id, "")); err != nil {
		t.Fatalf("deleteSavedFilter: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteSavedFilter(w, reqWithID("DELETE", "/api/saved-filters/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}
}

// TestGetPutSettings 读写往返。
func TestGetPutSettings(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.putSettings(w, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(`{"fontScale":"1.2"}`))); err != nil {
		t.Fatalf("putSettings: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.getSettings(w, httptest.NewRequest("GET", "/api/settings", nil)); err != nil {
		t.Fatalf("getSettings: %v", err)
	}
	var kv map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &kv); err != nil {
		t.Fatalf("解析 settings: %v", err)
	}
	if kv["fontScale"] != "1.2" {
		t.Errorf("settings = %+v", kv)
	}
	// getSettings 把内部键挡在外面。
	for _, k := range []string{"autoBackupLastAt", "autoBackupLastFile", "autoBackupLastError"} {
		if v, ok := kv[k]; ok {
			t.Errorf("内部键 %s 不应出现在响应: %q", k, v)
		}
	}
}
