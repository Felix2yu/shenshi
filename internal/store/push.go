package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// pushMaxAttempts 单条推送的最大尝试次数。失败会随下一个轮询周期重试，
// 但次数用尽就放弃——外部渠道持续不可用时不能让台账无限积压重试。
const pushMaxAttempts = 3

// PushState 是一条推送在台账里的状态。
type PushState struct {
	Attempts int  // 已尝试次数（含失败的）
	OK       bool // 最近一次是否成功
	Done     bool // 已成功送达，或重试次数用尽——两者都不再重试
}

// PushState 查询某个推送键的状态。查不到视为从未推过。
// 键由推送器自己拼：提醒是 "R|<ackId>|<fireAt>"，每日概览是 "D|<日期>"。
func (s *Store) PushState(key string) (PushState, error) {
	var attempts, ok int
	err := s.db.QueryRow(`SELECT attempts, ok FROM push_log WHERE key = ?`, key).Scan(&attempts, &ok)
	if errors.Is(err, sql.ErrNoRows) {
		return PushState{}, nil
	}
	if err != nil {
		return PushState{}, err
	}
	return PushState{Attempts: attempts, OK: ok == 1, Done: ok == 1 || attempts >= pushMaxAttempts}, nil
}

// RecordPush 落一条推送结果。成功即永久去重；失败累加尝试次数，
// 由调用方结合 PushState 决定是否还要重试。
func (s *Store) RecordPush(key string, ok bool, errMsg string) error {
	if ok {
		_, err := s.db.Exec(`INSERT INTO push_log(key, attempts, ok, error, sent_at) VALUES(?, 1, 1, '', ?)
			ON CONFLICT(key) DO UPDATE SET ok = 1, error = '', sent_at = excluded.sent_at`,
			key, model.Now())
		return err
	}
	_, err := s.db.Exec(`INSERT INTO push_log(key, attempts, ok, error, sent_at) VALUES(?, 1, 0, ?, ?)
		ON CONFLICT(key) DO UPDATE SET attempts = attempts + 1, ok = 0, error = excluded.error, sent_at = excluded.sent_at`,
		key, errMsg, model.Now())
	return err
}

// PrunePushLog 清掉 sent_at 早于 cutoff 的台账。推送键都带日期/时间戳，
// 老条目不会再被命中，留着只是占地方。
func (s *Store) PrunePushLog(cutoff time.Time) error {
	_, err := s.db.Exec(`DELETE FROM push_log WHERE sent_at < ?`, cutoff.Format(time.RFC3339))
	return err
}

// PushEntry 是台账里的一条记录，供「推送自检」查看最近发生了什么。
type PushEntry struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"` // reminder（提醒）/ daily（每日概览）
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	SentAt  string `json:"sentAt"`
	Attempts int   `json:"attempts"`
}

// RecentPushes 返回最近若干条推送记录，新的在前。
// 排障用：推送「没收到」时，先要看的是服务端到底有没有发出去、发出去是什么结果。
func (s *Store) RecentPushes(limit int) ([]PushEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.db.Query(`SELECT key, attempts, ok, error, sent_at FROM push_log ORDER BY sent_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushEntry{}
	for rows.Next() {
		var e PushEntry
		var ok, attempts int
		if err := rows.Scan(&e.Key, &attempts, &ok, &e.Error, &e.SentAt); err != nil {
			return nil, err
		}
		e.OK = ok == 1
		e.Attempts = attempts
		e.Kind = "reminder"
		if len(e.Key) > 1 && e.Key[0] == 'D' {
			e.Kind = "daily"
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DailySummary 汇总「今天要做多少、已经逾期多少」，供每日概览推送使用。
// 口径与提醒/欠账一致：待办与进行中都算；归档（任务级/清单级）按收起语义不计。
type DailySummary struct {
	Today   int      `json:"today"`   // 到期日是今天的未完成任务数
	Overdue int      `json:"overdue"` // 到期日早于今天的未完成任务数
	Sample  []string `json:"sample"`  // 今日前几件任务的标题，推送正文里点个名
}

// DailySummary 计算某时刻的每日概览。
func (s *Store) DailySummary(now time.Time) (DailySummary, error) {
	day := now.Format("2006-01-02")
	var sum DailySummary
	err := s.db.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN t.due_date = ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.due_date < ? THEN 1 ELSE 0 END), 0)
		FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.status IN ('todo','in_progress') AND t.archived = 0 AND l.archived = 0
		  AND t.due_date IS NOT NULL`, day, day).Scan(&sum.Today, &sum.Overdue)
	if err != nil {
		return sum, err
	}
	if sum.Today == 0 {
		return sum, nil
	}
	rows, err := s.db.Query(`SELECT t.title FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.status IN ('todo','in_progress') AND t.archived = 0 AND l.archived = 0 AND t.due_date = ?
		ORDER BY COALESCE(t.due_time, '99') ASC, t.id ASC LIMIT 3`, day)
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return sum, err
		}
		sum.Sample = append(sum.Sample, title)
	}
	return sum, rows.Err()
}
