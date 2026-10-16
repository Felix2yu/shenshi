package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHistoryHandlers 撤销槽位与操作历史的读写。
func TestHistoryHandlers(t *testing.T) {
	s, st := newTestServer(t)

	// 初始：无槽位。
	w := httptest.NewRecorder()
	if err := s.undoState(w, httptest.NewRequest("GET", "/api/history/undo", nil)); err != nil {
		t.Fatalf("undoState: %v", err)
	}
	var us struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &us); err != nil || us.Available {
		t.Errorf("初始槽位 = %+v err=%v", us, err)
	}

	// 空槽位撤销 → 校验错误（「没有可撤销的操作」）。
	w = httptest.NewRecorder()
	if err := s.undo(w, httptest.NewRequest("POST", "/api/history/undo", nil)); err == nil {
		t.Error("空槽位撤销应报错")
	}
	// 空槽位放弃 → 仍成功（幂等）。
	w = httptest.NewRecorder()
	if err := s.dropUndo(w, httptest.NewRequest("POST", "/api/history/undo/drop", nil)); err != nil {
		t.Errorf("dropUndo 空槽位: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("code = %d", w.Code)
	}

	// 删一条任务后：状态可用 → 撤销恢复 → 槽位清空。
	inbox, err := st.InboxListID()
	if err != nil {
		t.Fatalf("InboxListID: %v", err)
	}
	task, err := st.CreateTask(taskInputTitle("待撤销任务"), inbox)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := st.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	w = httptest.NewRecorder()
	if err := s.undoState(w, httptest.NewRequest("GET", "/api/history/undo", nil)); err != nil {
		t.Fatalf("undoState(有槽位): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &us); err != nil || !us.Available {
		t.Fatalf("槽位应可用: %+v err=%v", us, err)
	}

	w = httptest.NewRecorder()
	if err := s.undo(w, httptest.NewRequest("POST", "/api/history/undo", nil)); err != nil {
		t.Fatalf("undo: %v", err)
	}
	var res struct {
		Restored int  `json:"restored"`
		OK       bool `json:"ok"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || !res.OK || res.Restored != 1 {
		t.Errorf("undo 结果 = %+v err=%v", res, err)
	}
	w = httptest.NewRecorder()
	if err := s.undoState(w, httptest.NewRequest("GET", "/api/history/undo", nil)); err != nil {
		t.Fatalf("undoState(恢复后): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &us); err != nil || us.Available {
		t.Errorf("恢复后槽位应清空: %+v", us)
	}
	// 再撤销 → 报错。
	w = httptest.NewRecorder()
	if err := s.undo(w, httptest.NewRequest("POST", "/api/history/undo", nil)); err == nil {
		t.Error("再次撤销应报错")
	}
}

// TestActivityHandlers 操作历史的读取、limit 与清空。
func TestActivityHandlers(t *testing.T) {
	s, st := newTestServer(t)

	// 制造一条历史（完成任务会记 log）。
	inbox, _ := st.InboxListID()
	task, err := st.CreateTask(taskInputTitle("历史任务"), inbox)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := st.ToggleTask(task.ID); err != nil {
		t.Fatalf("ToggleTask: %v", err)
	}

	w := httptest.NewRecorder()
	if err := s.listActivities(w, httptest.NewRequest("GET", "/api/history/activities?limit=5", nil)); err != nil {
		t.Fatalf("listActivities: %v", err)
	}
	var out struct {
		Activities []struct {
			Kind string `json:"kind"`
		} `json:"activities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(out.Activities) == 0 {
		t.Fatal("应至少有一条历史")
	}

	// clear。
	w = httptest.NewRecorder()
	if err := s.clearActivities(w, httptest.NewRequest("DELETE", "/api/history/activities", nil)); err != nil {
		t.Fatalf("clearActivities: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.listActivities(w, httptest.NewRequest("GET", "/api/history/activities", nil)); err != nil {
		t.Fatalf("listActivities(清空后): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(out.Activities) != 0 {
		t.Errorf("清空后仍有 %d 条", len(out.Activities))
	}
}

// TestReviewAndFocusHandlers 复盘的日期校验与专注时长的边界。
func TestReviewAndFocusHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	// getReview 坏日期 → 校验错误。
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/reviews/2026-99-99", nil)
	req.SetPathValue("date", "2026-99-99")
	if err := s.getReview(w, req); err == nil {
		t.Error("非法日期应报错")
	}
	// getReview 合法日期（可能 404，由 store 决定；handler 不崩即可）。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/reviews/2026-09-23", nil)
	req.SetPathValue("date", "2026-09-23")
	_ = s.getReview(w, req) // 无记录时返回 ErrNotFound 属正常

	// listReviews 默认 limit。
	w = httptest.NewRecorder()
	if err := s.listReviews(w, httptest.NewRequest("GET", "/api/reviews", nil)); err != nil {
		t.Fatalf("listReviews: %v", err)
	}
	if !strings.Contains(w.Body.String(), "reviews") {
		t.Errorf("body = %s", w.Body.String())
	}

	// putReview 写入。
	w = httptest.NewRecorder()
	body := `{"date":"2026-09-23","mood":"好","wins":"完成了测试","blockers":"无","tomorrow":"继续"}`
	if err := s.putReview(w, httptest.NewRequest("PUT", "/api/reviews", strings.NewReader(body))); err != nil {
		t.Fatalf("putReview: %v", err)
	}
	// putReview 坏 JSON。
	w = httptest.NewRecorder()
	if err := s.putReview(w, httptest.NewRequest("PUT", "/api/reviews", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}

	// focus：分钟越界拒绝。
	w = httptest.NewRecorder()
	if err := s.addFocus(w, httptest.NewRequest("POST", "/api/focus", strings.NewReader(`{"minutes":601}`))); err == nil {
		t.Error("601 分钟应被拒绝")
	}
	w = httptest.NewRecorder()
	if err := s.addFocus(w, httptest.NewRequest("POST", "/api/focus", strings.NewReader(`{"minutes":0}`))); err == nil {
		t.Error("0 分钟应被拒绝")
	}
	w = httptest.NewRecorder()
	if err := s.addFocus(w, httptest.NewRequest("POST", "/api/focus", strings.NewReader(`{"minutes":30}`))); err != nil {
		t.Fatalf("addFocus: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	// listFocus。
	w = httptest.NewRecorder()
	if err := s.listFocus(w, httptest.NewRequest("GET", "/api/focus", nil)); err != nil {
		t.Fatalf("listFocus: %v", err)
	}
	if !strings.Contains(w.Body.String(), "sessions") {
		t.Errorf("body = %s", w.Body.String())
	}
}

// TestStatsAndRepeatMeta 统计的 days 钳制与重复规则元数据。
func TestStatsAndRepeatMeta(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.stats(w, httptest.NewRequest("GET", "/api/stats?days=9999", nil)); err != nil {
		t.Fatalf("stats(days 超界): %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("code = %d（超界 days 应钳制而非报错）", w.Code)
	}
	w = httptest.NewRecorder()
	if err := s.stats(w, httptest.NewRequest("GET", "/api/stats", nil)); err != nil {
		t.Fatalf("stats(默认): %v", err)
	}

	w = httptest.NewRecorder()
	if err := s.repeatMeta(w, httptest.NewRequest("GET", "/api/repeat-meta", nil)); err != nil {
		t.Fatalf("repeatMeta: %v", err)
	}
	var meta struct {
		Presets []struct {
			Value string `json:"value"`
		} `json:"presets"`
		EbbinghausOffsets []int `json:"ebbinghausOffsets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatalf("解析 repeatMeta: %v", err)
	}
	if len(meta.Presets) < 10 {
		t.Errorf("presets = %d，应含全套规则", len(meta.Presets))
	}
	if len(meta.EbbinghausOffsets) == 0 {
		t.Error("艾宾浩斯间隔不应为空")
	}
}

// TestReminderHandlers 提醒台账的回执、稍后与重置。
func TestReminderHandlers(t *testing.T) {
	s, st := newTestServer(t)

	// due 列表。
	w := httptest.NewRecorder()
	if err := s.dueReminders(w, httptest.NewRequest("GET", "/api/reminders/due", nil)); err != nil {
		t.Fatalf("dueReminders: %v", err)
	}
	if !strings.Contains(w.Body.String(), "reminders") || !strings.Contains(w.Body.String(), "serverTime") {
		t.Errorf("body = %s", w.Body.String())
	}

	// ack 缺参。
	w = httptest.NewRecorder()
	if err := s.ackReminder(w, httptest.NewRequest("POST", "/api/reminders/ack", strings.NewReader(`{}`))); err == nil {
		t.Error("缺参应被拒绝")
	}
	// ack taskId=0。
	w = httptest.NewRecorder()
	if err := s.ackReminder(w, httptest.NewRequest("POST", "/api/reminders/ack", strings.NewReader(`{"taskId":0,"fireAt":"x"}`))); err == nil {
		t.Error("taskId=0 应被拒绝（负数子任务 id 放行）")
	}
	// ack 正常（回执不存在的 fireAt 也允许：先记台账，DueReminders 再跳过）。
	w = httptest.NewRecorder()
	body := `{"taskId":-42,"fireAt":"2026-09-23T10:00:00+08:00"}`
	if err := s.ackReminder(w, httptest.NewRequest("POST", "/api/reminders/ack", strings.NewReader(body))); err != nil {
		t.Fatalf("ackReminder(负 id): %v", err)
	}

	// snooze 分钟边界。
	w = httptest.NewRecorder()
	sb := `{"taskId":1,"fireAt":"2026-09-23T10:00:00+08:00","minutes":0}`
	if err := s.snoozeReminder(w, httptest.NewRequest("POST", "/api/reminders/snooze", strings.NewReader(sb))); err == nil {
		t.Error("0 分钟应被拒绝")
	}
	sb = `{"taskId":1,"fireAt":"2026-09-23T10:00:00+08:00","minutes":1441}`
	if err := s.snoozeReminder(w, httptest.NewRequest("POST", "/api/reminders/snooze", strings.NewReader(sb))); err == nil {
		t.Error("1441 分钟应被拒绝")
	}
	w = httptest.NewRecorder()
	sb = `{"taskId":1,"fireAt":"2026-09-23T10:00:00+08:00","minutes":15}`
	if err := s.snoozeReminder(w, httptest.NewRequest("POST", "/api/reminders/snooze", strings.NewReader(sb))); err != nil {
		t.Fatalf("snoozeReminder: %v", err)
	}
	// snooze 缺 fireAt。
	w = httptest.NewRecorder()
	if err := s.snoozeReminder(w, httptest.NewRequest("POST", "/api/reminders/snooze", strings.NewReader(`{"taskId":1,"minutes":5}`))); err == nil {
		t.Error("缺 fireAt 应被拒绝")
	}

	// reset：带 body 精确清；无 body 全清。
	_ = st
	w = httptest.NewRecorder()
	if err := s.resetReminders(w, httptest.NewRequest("POST", "/api/reminders/reset", strings.NewReader(`{"taskId":-42}`))); err != nil {
		t.Fatalf("resetReminders(指定): %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.resetReminders(w, httptest.NewRequest("POST", "/api/reminders/reset", nil)); err != nil {
		t.Fatalf("resetReminders(全清): %v", err)
	}
	// reset 坏 JSON（ContentLength>0 才解析）。
	w = httptest.NewRecorder()
	if err := s.resetReminders(w, httptest.NewRequest("POST", "/api/reminders/reset", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}
}
