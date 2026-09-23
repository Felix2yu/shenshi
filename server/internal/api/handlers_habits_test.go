package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/model"
)

// reqWithID 构造带路径参数的请求。
func reqWithID(method, target, id, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.SetPathValue("id", id)
	return r
}

// TestHabitsHandlers 习惯的增删改查、重排、打卡与撤销打卡。
func TestHabitsHandlers(t *testing.T) {
	s, st := newTestServer(t)

	// list 默认区间 12 周。
	w := httptest.NewRecorder()
	if err := s.listHabits(w, httptest.NewRequest("GET", "/api/habits", nil)); err != nil {
		t.Fatalf("listHabits: %v", err)
	}
	var board struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Today string `json:"today"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatalf("解析 board: %v", err)
	}
	if board.From == "" || board.To == "" || board.Today == "" {
		t.Errorf("board 区间 = %+v", board)
	}

	// 非法区间 → 错误。
	w = httptest.NewRecorder()
	if err := s.listHabits(w, httptest.NewRequest("GET", "/api/habits?from=nope", nil)); err == nil {
		t.Error("非法 from 应报错")
	}

	// create：空名 → 校验错误。
	w = httptest.NewRecorder()
	if err := s.createHabit(w, httptest.NewRequest("POST", "/api/habits", strings.NewReader(`{"name":""}`))); err == nil {
		t.Error("空名称应被拒绝")
	}
	// create 成功。
	w = httptest.NewRecorder()
	if err := s.createHabit(w, httptest.NewRequest("POST", "/api/habits", strings.NewReader(`{"name":"喝水"}`))); err != nil {
		t.Fatalf("createHabit: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("created = %+v err=%v", created, err)
	}

	// create 坏 JSON。
	w = httptest.NewRecorder()
	if err := s.createHabit(w, httptest.NewRequest("POST", "/api/habits", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}

	// update 改名。
	w = httptest.NewRecorder()
	id := itoa(created.ID)
	if err := s.updateHabit(w, reqWithID("PATCH", "/api/habits/"+id, id, `{"note":"备注"}`)); err != nil {
		t.Fatalf("updateHabit: %v", err)
	}
	got, err := st.Habit(created.ID)
	if err != nil || got.Note != "备注" {
		t.Errorf("habit = %+v err=%v", got, err)
	}
	// update 路径参数非法。
	w = httptest.NewRecorder()
	if err := s.updateHabit(w, reqWithID("PATCH", "/api/habits/x", "x", `{}`)); err == nil {
		t.Error("非法 id 应报错")
	}
	// update 不存在。
	w = httptest.NewRecorder()
	if err := s.updateHabit(w, reqWithID("PATCH", "/api/habits/999999", "999999", `{}`)); err == nil {
		t.Error("不存在习惯应报错")
	}

	// check：空 body → 今天打卡。
	w = httptest.NewRecorder()
	if err := s.checkHabit(w, reqWithID("POST", "/api/habits/"+id+"/check", id, "")); err != nil {
		t.Fatalf("checkHabit(空体): %v", err)
	}
	var chk struct {
		Log model.HabitLog `json:"log"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &chk); err != nil || chk.Log.Count != 1 {
		t.Errorf("check = %+v err=%v", chk, err)
	}
	// check 指定日期。
	w = httptest.NewRecorder()
	if err := s.checkHabit(w, reqWithID("POST", "/api/habits/"+id+"/check", id, `{"day":"2026-09-20"}`)); err != nil {
		t.Fatalf("checkHabit(指定日期): %v", err)
	}
	// check 非法日期。
	w = httptest.NewRecorder()
	if err := s.checkHabit(w, reqWithID("POST", "/api/habits/"+id+"/check", id, `{"day":"bad"}`)); err == nil {
		t.Error("非法日期应报错")
	}

	// uncheck 默认今天。
	w = httptest.NewRecorder()
	if err := s.uncheckHabit(w, reqWithID("POST", "/api/habits/"+id+"/uncheck", id, "")); err != nil {
		t.Fatalf("uncheckHabit: %v", err)
	}

	// reorder。
	w = httptest.NewRecorder()
	if err := s.reorderHabits(w, httptest.NewRequest("POST", "/api/habits/reorder", strings.NewReader(`{"ids":[`+id+`]}`))); err != nil {
		t.Fatalf("reorderHabits: %v", err)
	}
	// reorder 空 id 也成功（len 0）。
	w = httptest.NewRecorder()
	if err := s.reorderHabits(w, httptest.NewRequest("POST", "/api/habits/reorder", strings.NewReader(`{"ids":[]}`))); err != nil {
		t.Errorf("空 ids 应成功: %v", err)
	}
	// reorder 坏 JSON。
	w = httptest.NewRecorder()
	if err := s.reorderHabits(w, httptest.NewRequest("POST", "/api/habits/reorder", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}

	// delete。
	w = httptest.NewRecorder()
	if err := s.deleteHabit(w, reqWithID("DELETE", "/api/habits/"+id, id, "")); err != nil {
		t.Fatalf("deleteHabit: %v", err)
	}
	// delete 不存在。
	w = httptest.NewRecorder()
	if err := s.deleteHabit(w, reqWithID("DELETE", "/api/habits/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}
	// delete 非法 id。
	w = httptest.NewRecorder()
	if err := s.deleteHabit(w, reqWithID("DELETE", "/api/habits/abc", "abc", "")); err == nil {
		t.Error("非法 id 应报错")
	}
}

// itoa 测试侧的简单转换（api 包内无同名冲突）。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
