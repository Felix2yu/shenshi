package store

import (
	"errors"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// TestHabitCRUD 习惯的建改删与入参校验。
func TestHabitCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateHabit(HabitInput{}); err == nil {
		t.Error("空名称应被拒绝")
	}
	badCadence := "hourly"
	if _, err := s.CreateHabit(HabitInput{Name: sp("坏节奏"), Cadence: &badCadence}); err == nil {
		t.Error("非法节奏应被拒绝")
	}
	zero := 0
	if _, err := s.CreateHabit(HabitInput{Name: sp("零目标"), Target: &zero}); err == nil {
		t.Error("target<1 应被拒绝")
	}
	badDay := "2026-99-99"
	if _, err := s.CreateHabit(HabitInput{Name: sp("坏日期"), StartDate: &badDay}); err == nil {
		t.Error("非法开始日期应被拒绝")
	}

	today := time.Now().Format("2006-01-02")
	h, err := s.CreateHabit(HabitInput{Name: sp("晨读"), Cadence: sp(model.CadenceDaily), Target: intp(3), StartDate: sp(today)})
	if err != nil {
		t.Fatalf("CreateHabit: %v", err)
	}
	if h.Target != 3 || h.StartDate != today || h.Cadence != model.CadenceDaily {
		t.Errorf("习惯字段 = %+v", h)
	}

	// 每周习惯未指定星期 → 默认全周。
	weekly, err := s.CreateHabit(HabitInput{Name: sp("周练"), Cadence: sp(model.CadenceWeekly)})
	if err != nil {
		t.Fatalf("CreateHabit(weekly): %v", err)
	}
	if weekly.Weekdays == "" {
		t.Error("每周习惯应有默认 weekdays")
	}

	got, err := s.Habit(h.ID)
	if err != nil {
		t.Fatalf("Habit: %v", err)
	}
	if got.Name != "晨读" {
		t.Errorf("name = %q", got.Name)
	}
	if _, err := s.Habit(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在习惯应 ErrNotFound，得到 %v", err)
	}

	// 更新。
	upd, err := s.UpdateHabit(h.ID, HabitInput{Name: sp("晨读改名"), Note: sp("笔记")})
	if err != nil {
		t.Fatalf("UpdateHabit: %v", err)
	}
	if upd.Name != "晨读改名" || upd.Note != "笔记" {
		t.Errorf("更新未生效: %+v", upd)
	}
	// 空更新直接回读。
	if _, err := s.UpdateHabit(h.ID, HabitInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}
	// 归档。
	arch, err := s.UpdateHabit(h.ID, HabitInput{Archived: boolp(true)})
	if err != nil || !arch.Archived {
		t.Errorf("归档失败: %+v err=%v", arch, err)
	}

	// 列表：默认排除归档，显式包含。
	open, err := s.Habits(false)
	if err != nil {
		t.Fatalf("Habits: %v", err)
	}
	for _, item := range open {
		if item.ID == h.ID {
			t.Error("默认列表不应含归档习惯")
		}
	}
	all, err := s.Habits(true)
	if err != nil {
		t.Fatalf("Habits(含归档): %v", err)
	}
	found := false
	for _, item := range all {
		if item.ID == h.ID {
			found = true
		}
	}
	if !found {
		t.Error("含归档列表应有目标习惯")
	}

	if err := s.DeleteHabit(h.ID); err != nil {
		t.Fatalf("DeleteHabit: %v", err)
	}
	if err := s.DeleteHabit(h.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
	_ = weekly
}

func intp(n int) *int { return &n }

// TestHabitCheckIn 打卡自增、显式设值、撤销与流水。
func TestHabitCheckIn(t *testing.T) {
	s := newTestStore(t)
	h, err := s.CreateHabit(HabitInput{Name: sp("打卡习惯")})
	if err != nil {
		t.Fatalf("CreateHabit: %v", err)
	}
	today := time.Now().Format("2006-01-02")

	// 非法日期。
	if _, err := s.CheckIn(h.ID, CheckInput{Day: "nope"}); err == nil {
		t.Error("非法日期应被拒绝")
	}
	// 不存在的习惯。
	if _, err := s.CheckIn(999999, CheckInput{Day: today}); err == nil {
		t.Error("不存在习惯应被拒绝")
	}

	// 加一次 → 再加一次：自增到 2。
	log1, err := s.CheckIn(h.ID, CheckInput{Day: today})
	if err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if log1.Count != 1 {
		t.Errorf("首次 count = %d", log1.Count)
	}
	log2, err := s.CheckIn(h.ID, CheckInput{Day: today, Note: sp("备注")})
	if err != nil {
		t.Fatalf("CheckIn(2): %v", err)
	}
	if log2.Count != 2 {
		t.Errorf("再打卡 count = %d，期望 2（SQL 自增）", log2.Count)
	}
	if log2.Note != "备注" {
		t.Errorf("note = %q", log2.Note)
	}

	// 显式设值覆盖。
	five := 5
	if _, err := s.CheckIn(h.ID, CheckInput{Day: today, Count: &five}); err != nil {
		t.Fatalf("CheckIn(设值): %v", err)
	}
	logs, err := s.AllHabitLogs()
	if err != nil {
		t.Fatalf("AllHabitLogs: %v", err)
	}
	var got int
	for _, item := range logs {
		if item.HabitID == h.ID && item.Day == today {
			got = item.Count
		}
	}
	if got != 5 {
		t.Errorf("显式设值后 count = %d，期望 5", got)
	}

	// count<=0 → 等同撤销。
	zero := 0
	if _, err := s.CheckIn(h.ID, CheckInput{Day: today, Count: &zero}); err != nil {
		t.Fatalf("CheckIn(0): %v", err)
	}
	logs, _ = s.AllHabitLogs()
	for _, item := range logs {
		if item.HabitID == h.ID && item.Day == today {
			t.Error("count=0 应删除当日流水")
		}
	}

	// 再打一次然后 Uncheck。
	if _, err := s.CheckIn(h.ID, CheckInput{Day: today}); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if err := s.Uncheck(h.ID, today); err != nil {
		t.Fatalf("Uncheck: %v", err)
	}
	if err := s.Uncheck(h.ID, "bad-day"); err == nil {
		t.Error("Uncheck 非法日期应被拒绝")
	}
}

// TestHabitBoard 看板区间、统计与排序。
func TestHabitBoard(t *testing.T) {
	s := newTestStore(t)
	h, err := s.CreateHabit(HabitInput{Name: sp("看板习惯")})
	if err != nil {
		t.Fatalf("CreateHabit: %v", err)
	}
	today := time.Now().Format("2006-01-02")
	from := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	if _, err := s.CheckIn(h.ID, CheckInput{Day: today}); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}

	// 空日期用默认区间。
	board, err := s.HabitBoard("", "", false)
	if err != nil {
		t.Fatalf("HabitBoard(默认): %v", err)
	}
	if board.From == "" || board.To == "" || board.Today != today {
		t.Errorf("区间 = %+v", board)
	}
	if len(board.Habits) == 0 || len(board.Stats) == 0 {
		t.Error("看板应含习惯与统计")
	}
	if len(board.Logs) == 0 {
		t.Error("看板应含区间内流水")
	}

	// 非法区间。
	if _, err := s.HabitBoard("nope", "", false); err == nil {
		t.Error("非法 from 应被拒绝")
	}
	// from > to 自动交换。
	swapped, err := s.HabitBoard(today, from, false)
	if err != nil {
		t.Fatalf("HabitBoard(交换): %v", err)
	}
	if swapped.From > swapped.To {
		t.Errorf("应交换区间: %+v", swapped)
	}
	_ = h
}

// TestHabitScheduleAndWeekdays 排期判断与星期规范化纯函数。
func TestHabitScheduleAndWeekdays(t *testing.T) {
	// daily：开始日之后每天都排。
	daily := model.Habit{Cadence: model.CadenceDaily, StartDate: "2026-09-01"}
	if !HabitScheduled(daily, "2026-09-23") {
		t.Error("daily 应每天排期")
	}
	if HabitScheduled(daily, "2026-08-31") {
		t.Error("开始日之前不排期")
	}

	// weekly 只排指定星期。2026-09-23 是周三（3）。
	weekly := model.Habit{Cadence: model.CadenceWeekly, Weekdays: "3", StartDate: "2026-09-01"}
	if !HabitScheduled(weekly, "2026-09-23") {
		t.Error("周三的习惯周三应排期")
	}
	if HabitScheduled(weekly, "2026-09-24") {
		t.Error("周四不应排期")
	}
	// 未列星期 → 视为全排。
	open := model.Habit{Cadence: model.CadenceWeekly, Weekdays: "", StartDate: "2026-09-01"}
	if !HabitScheduled(open, "2026-09-23") {
		t.Error("空 weekdays 应全排")
	}

	// weekdayOf / nextDay / prevDay。
	if got := weekdayOf("2026-09-23"); got != 3 {
		t.Errorf("2026-09-23 是周三，weekdayOf = %d", got)
	}
	if weekdayOf("bad") != 0 {
		t.Error("非法日期回落 0")
	}
	if nextDay("2026-09-30") != "2026-10-01" {
		t.Errorf("nextDay = %s", nextDay("2026-09-30"))
	}
	if prevDay("2026-10-01") != "2026-09-30" {
		t.Errorf("prevDay = %s", prevDay("2026-10-01"))
	}

	// parseWeekdays 容忍中文逗号与空格，丢弃越界。
	set := parseWeekdays("1， 3 , 9 , x, 5")
	if len(set) != 3 {
		t.Errorf("parseWeekdays = %v，应含 1/3/5", set)
	}

	// normalizeWeekdays 去重排序。
	got, err := normalizeWeekdays("5,1,3,1")
	if err != nil || got != "1,3,5" {
		t.Errorf("normalizeWeekdays = %q, %v", got, err)
	}
	if got, err := normalizeWeekdays("  "); err != nil || got != "" {
		t.Errorf("空白应得空串: %q %v", got, err)
	}
	if _, err := normalizeWeekdays("9"); err == nil {
		t.Error("全越界应报错")
	}
	// 每周习惯配空星期在归一化路径里拒绝。
	emptyWD := ""
	badInput := HabitInput{Weekdays: &emptyWD, Cadence: sp(model.CadenceWeekly)}
	if err := normalizeHabitIn(&badInput, false); err == nil {
		t.Error("weekly + 空 weekdays 应被拒绝")
	}
	_ = ternary(true, 1, 2)
}

// TestReorderHabits 按传入顺序重写习惯排序。
func TestReorderHabits(t *testing.T) {
	s := newTestStore(t)
	h1, _ := s.CreateHabit(HabitInput{Name: sp("习惯一")})
	h2, _ := s.CreateHabit(HabitInput{Name: sp("习惯二")})
	if err := s.ReorderHabits([]int64{h2.ID, h1.ID}); err != nil {
		t.Fatalf("ReorderHabits: %v", err)
	}
	habits, err := s.Habits(false)
	if err != nil {
		t.Fatalf("Habits: %v", err)
	}
	var order []int64
	for _, h := range habits {
		if h.ID == h1.ID || h.ID == h2.ID {
			order = append(order, h.ID)
		}
	}
	if len(order) == 2 && order[0] != h2.ID {
		t.Errorf("排序结果 = %v", order)
	}
}
