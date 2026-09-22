package store

import (
	"strconv"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// ReminderHit 是一条待投递的提醒。
type ReminderHit struct {
	Task     model.Task `json:"task"`
	FireAt   string     `json:"fireAt"`   // RFC3339
	Offset   int        `json:"offset"`   // 提前分钟数，0 表示准点
	Overdue  bool       `json:"overdue"`  // 触发时刻已过
	DueLabel string     `json:"dueLabel"` // 人类可读的到期描述
}

// resolveDueTime 把「日期 + 可选时间」解析为本地时间点。
// 未指定时间时约定为当天 09:00 —— 让提醒落在人清醒的时段，而不是凌晨。
func resolveDueTime(dueDate string, dueTime *string) (time.Time, bool) {
	d, err := time.ParseInLocation("2006-01-02", dueDate, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	if dueTime != nil && len(*dueTime) == 5 {
		if t, err := time.ParseInLocation("15:04", *dueTime, time.Local); err == nil {
			return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, time.Local), true
		}
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 9, 0, 0, 0, time.Local), true
}

// DueReminders 返回当前应当投递的提醒。窗口内已投递过的会被排除，
// 因此前端可以放心地按固定间隔轮询而不产生重复打扰。
func (s *Store) DueReminders(now time.Time, lookahead, lookback time.Duration) ([]ReminderHit, error) {
	rows, err := s.db.Query(taskSelect + ` WHERE t.status = 'todo' AND t.due_date IS NOT NULL AND t.reminders <> '[]'`)
	if err != nil {
		return nil, err
	}
	tasks := []model.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// 已投递台账
	logRows, err := s.db.Query(`SELECT task_id, fire_at FROM reminder_log`)
	if err != nil {
		return nil, err
	}
	fired := map[string]bool{}
	for logRows.Next() {
		var tid int64
		var fireAt string
		if err := logRows.Scan(&tid, &fireAt); err != nil {
			logRows.Close()
			return nil, err
		}
		fired[key(tid, fireAt)] = true
	}
	logRows.Close()

	hits := []ReminderHit{}
	for _, t := range tasks {
		if t.DueDate == nil {
			continue
		}
		due, ok := resolveDueTime(*t.DueDate, t.DueTime)
		if !ok {
			continue
		}
		for _, off := range t.Reminders {
			fireAt := due.Add(-time.Duration(off) * time.Minute)
			if fireAt.After(now.Add(lookahead)) || fireAt.Before(now.Add(-lookback)) {
				continue
			}
			fs := fireAt.Format(time.RFC3339)
			if fired[key(t.ID, fs)] {
				continue
			}
			hits = append(hits, ReminderHit{
				Task:     t,
				FireAt:   fs,
				Offset:   off,
				Overdue:  fireAt.Before(now),
				DueLabel: describeDue(due, now),
			})
		}
	}
	return hits, nil
}

// AckReminder 记录一条提醒已投递，避免重复提醒。
func (s *Store) AckReminder(taskID int64, fireAt string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO reminder_log(task_id, fire_at, fired_at) VALUES(?,?,?)`,
		taskID, fireAt, model.Now())
	return err
}

// ClearReminderLog 清空台账（用于「重置提醒」）。
func (s *Store) ClearReminderLog(taskID int64) error {
	if taskID > 0 {
		_, err := s.db.Exec(`DELETE FROM reminder_log WHERE task_id = ?`, taskID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM reminder_log`)
	return err
}

func key(taskID int64, fireAt string) string {
	return strconv.FormatInt(taskID, 10) + "|" + fireAt
}

func describeDue(due, now time.Time) string {
	d0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	d1 := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.Local)
	days := int(d1.Sub(d0).Hours() / 24)
	prefix := ""
	switch {
	case days < 0:
		prefix = "已逾期 "
	case days == 0:
		prefix = "今天 "
	case days == 1:
		prefix = "明天 "
	case days == 2:
		prefix = "后天 "
	default:
		prefix = due.Format("1月2日") + " "
	}
	return prefix + due.Format("15:04")
}
