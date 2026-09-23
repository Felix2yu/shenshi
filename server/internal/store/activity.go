package store

import "github.com/yufei/shendu/server/internal/model"

// activityKeep 是操作历史的保留条数。这是一份「回看用的清单」，不是审计日志，
// 无限增长只会让弹窗越来越难读。
const activityKeep = 500

// logActivity 记一笔操作历史。
//
// 刻意只记「值得回看」的动作：建了、做完了、删了、挪了清单。
// 改标题、调优先级这类高频微调不记 —— 记了也只是把真正重要的几笔冲掉。
// 写失败不改变调用方的结果：历史是附加品，不该让主流程失败。
func (s *Store) logActivity(kind string, taskID int64, title, detail string) {
	if _, err := s.db.Exec(
		`INSERT INTO activities(kind, task_id, title, detail, created_at) VALUES(?,?,?,?,?)`,
		kind, taskID, title, detail, model.Now(),
	); err != nil {
		return
	}
	_, _ = s.db.Exec(`DELETE FROM activities WHERE id NOT IN (SELECT id FROM activities ORDER BY id DESC LIMIT ?)`, activityKeep)
}

// Activities 返回最近的操作历史。
func (s *Store) Activities(limit int) ([]model.Activity, error) {
	if limit <= 0 || limit > activityKeep {
		limit = activityKeep
	}
	rows, err := s.db.Query(
		`SELECT id, kind, task_id, title, detail, created_at FROM activities ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Activity{}
	for rows.Next() {
		var a model.Activity
		if err := rows.Scan(&a.ID, &a.Kind, &a.TaskID, &a.Title, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ClearActivities 清空操作历史。
func (s *Store) ClearActivities() error {
	_, err := s.db.Exec(`DELETE FROM activities`)
	return err
}
