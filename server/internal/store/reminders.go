package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// ReminderHit 是一条待投递的提醒。
type ReminderHit struct {
	Task     model.Task    `json:"task"`
	Subtask  *model.Subtask `json:"subtask,omitempty"` // 非空表示这是子任务自己的提醒
	AckID    int64         `json:"ackId"`             // 回执 id：任务为正 id，子任务为 -子任务ID（与台账 key 一致）
	FireAt   string        `json:"fireAt"`            // RFC3339
	Offset   int           `json:"offset"`            // 提前分钟数，0 表示准点
	Overdue  bool          `json:"overdue"`           // 触发时刻已过
	DueLabel string        `json:"dueLabel"`          // 人类可读的到期描述
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

// DueReminders 返回当前应当投递的提醒。
// 台账三态：已回执（fired_at 非空）永久静默；「稍后提醒」窗口内（snoozed_until >
// now）暂不投递；推迟到期或从未处理的照常返回——前端据此全量对齐本地状态，
// 刷新与跨标签页都能恢复未处理的提醒。
// 口径与「欠账」一致：进行中同样要提醒；归档（任务级/清单级）按「收起」语义不再打扰。
func (s *Store) DueReminders(now time.Time, lookahead, lookback time.Duration) ([]ReminderHit, error) {
	rows, err := s.db.Query(taskSelect + ` WHERE t.status IN ('todo','in_progress')
		AND t.archived = 0 AND l.archived = 0
		AND t.due_date IS NOT NULL AND t.reminders <> '[]'`)
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

	// 台账：fired 永久静默，snoozed 记「推迟到期」。
	logRows, err := s.db.Query(`SELECT task_id, fire_at, fired_at, snoozed_until FROM reminder_log`)
	if err != nil {
		return nil, err
	}
	fired := map[string]bool{}
	snoozed := map[string]time.Time{}
	for logRows.Next() {
		var tid int64
		var fireAt, firedAt string
		var snoozeUntil sql.NullString
		if err := logRows.Scan(&tid, &fireAt, &firedAt, &snoozeUntil); err != nil {
			logRows.Close()
			return nil, err
		}
		k := key(tid, fireAt)
		if firedAt != "" {
			fired[k] = true
		}
		if snoozeUntil.Valid && snoozeUntil.String != "" {
			if t, err := time.Parse(time.RFC3339, snoozeUntil.String); err == nil {
				snoozed[k] = t
			}
		}
	}
	logRows.Close()
	// suppressed：已回执的永久静默；推迟窗口内的暂不投递；
	// 推迟已到期的视同未处理，重新出现在结果里。
	suppressed := func(k string) bool {
		if fired[k] {
			return true
		}
		if until, ok := snoozed[k]; ok && until.After(now) {
			return true
		}
		return false
	}

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
			if suppressed(key(t.ID, fs)) {
				continue
			}
			hits = append(hits, ReminderHit{
				Task:     t,
				AckID:    t.ID,
				FireAt:   fs,
				Offset:   off,
				Overdue:  fireAt.Before(now),
				DueLabel: describeDue(due, now),
			})
		}
	}

	// 子任务自己的提醒：父任务未收尾、子步骤未勾选、且单独设了日期与提醒点。
	// 台账 key 用负数 id（-子任务ID），与任务 ID 空间隔离，避免 (id, fire_at) 撞车；
	// AckID 沿用同一约定，回执接口据此落账。父任务口径与主查询一致（含进行中、排除归档）。
	subRows, err := s.db.Query(`SELECT sb.id, sb.task_id, sb.title, sb.due_date, sb.reminders, sb.sort_order, t.title
	      FROM subtasks sb JOIN tasks t ON t.id = sb.task_id JOIN lists l ON l.id = t.list_id
	      WHERE sb.done = 0 AND sb.due_date IS NOT NULL AND sb.reminders <> '[]'
	        AND t.status IN ('todo','in_progress') AND t.archived = 0 AND l.archived = 0`)
	if err != nil {
		return nil, err
	}
	defer subRows.Close()
	for subRows.Next() {
		var sb model.Subtask
		var reminders, parentTitle string
		if err := subRows.Scan(&sb.ID, &sb.TaskID, &sb.Title, &sb.DueDate, &reminders, &sb.SortOrder, &parentTitle); err != nil {
			return nil, err
		}
		sb.Reminders = []int{}
		if reminders != "" {
			_ = json.Unmarshal([]byte(reminders), &sb.Reminders)
		}
		due, ok := resolveDueTime(*sb.DueDate, nil)
		if !ok {
			continue
		}
		for _, off := range sb.Reminders {
			fireAt := due.Add(-time.Duration(off) * time.Minute)
			if fireAt.After(now.Add(lookahead)) || fireAt.Before(now.Add(-lookback)) {
				continue
			}
			fs := fireAt.Format(time.RFC3339)
			if suppressed(key(-sb.ID, fs)) {
				continue
			}
			hits = append(hits, ReminderHit{
				// 回填父任务的 id 与标题：前端「查看」要能跳到任务，通知也要有上下文。
				Task:     model.Task{ID: sb.TaskID, Title: parentTitle},
				Subtask:  &sb,
				AckID:    -sb.ID,
				FireAt:   fs,
				Offset:   off,
				Overdue:  fireAt.Before(now),
				DueLabel: describeDue(due, now),
			})
		}
	}
	if err := subRows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// AckReminder 回执一条提醒：用户明确处理过（勾掉/查看/关闭）即永久静默。
// upsert 而非 INSERT OR IGNORE：「稍后提醒」到期重弹后若再回执，
// 必须把旧行的 fired_at 补上，否则推迟记录会让它永远关不掉。
func (s *Store) AckReminder(taskID int64, fireAt string) error {
	_, err := s.db.Exec(`INSERT INTO reminder_log(task_id, fire_at, fired_at, snoozed_until) VALUES(?,?,?,'')
		ON CONFLICT(task_id, fire_at) DO UPDATE SET fired_at = excluded.fired_at, snoozed_until = ''`,
		taskID, fireAt, model.Now())
	return err
}

// SnoozeReminder 记一条「稍后提醒」：窗口内 DueReminders 跳过该键，
// 到期后重新投递。fired_at 留空表示尚未回执，仍属未处理。
func (s *Store) SnoozeReminder(taskID int64, fireAt string, until time.Time) error {
	_, err := s.db.Exec(`INSERT INTO reminder_log(task_id, fire_at, fired_at, snoozed_until) VALUES(?,?, '', ?)
		ON CONFLICT(task_id, fire_at) DO UPDATE SET snoozed_until = excluded.snoozed_until`,
		taskID, fireAt, until.Format(time.RFC3339))
	return err
}

// ClearReminderLog 清空台账（用于「重置提醒」）。0 表示全清，负数精确清子任务。
func (s *Store) ClearReminderLog(taskID int64) error {
	if taskID != 0 {
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
