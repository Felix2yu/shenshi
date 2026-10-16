package store

import (
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// RollOverdue 把逾期未完成的任务改到某一天（默认今天）。
//
// 用途：逾期清单会越攒越厚，越厚越没人看，于是所有欠账一起烂在原地。
// 一次顺延等于给它们一个新的开始——但这是**破坏性**的批量写入，
// 所以只由服务端显式接口触发，绝不在后台悄悄跑：
// 用户对「昨天没做完」的真实意图可能是改期、删除，也可能就是今天继续做。
// 悄悄替他决定，就等于替他撒谎。
//
// 口径与智能清单 SmartOverdue 一致：未完成（todo/in_progress）、任务与清单都未归档。
// 已归档的事不该被顺延——归档的语义就是「收起来，别再管它」。
func (s *Store) RollOverdue(to string) (int64, error) {
	day := to
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	// 取 id 是为了逐条 emit：日历与其它设备靠事件对账，
	// 静默批量 UPDATE 的话，手机上的日期会一直停在旧值。
	rows, err := s.db.Query(`SELECT t.id FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.status IN ('todo','in_progress') AND t.archived = 0 AND l.archived = 0
		  AND t.due_date IS NOT NULL AND t.due_date < ?`, day)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	now := model.Now()
	urgent := deriveUrgent(&day, false)
	for _, id := range ids {
		// 逐条写而不是一条 UPDATE：urgent 要按新日期逐条重算，
		// 且每条都要 emit。批量 UPDATE 只对纯机械改字段才有优势，这里两样都要。
		if _, err := s.db.Exec(
			`UPDATE tasks SET due_date = ?, urgent = ?, updated_at = ? WHERE id = ?`,
			day, urgent, now, id); err != nil {
			return int64(len(ids)), err
		}
	}
	for _, id := range ids {
		if t, err := s.GetTask(id); err == nil {
			s.emit(model.EventTaskUpdated, t)
		}
	}
	return int64(len(ids)), nil
}

// CountOverdue 数一数今天有几件事逾期了，供 UI 在按钮上说明「会影响多少条」。
func (s *Store) CountOverdue(today string) (int64, error) {
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.status IN ('todo','in_progress') AND t.archived = 0 AND l.archived = 0
		  AND t.due_date IS NOT NULL AND t.due_date < ?`, today).Scan(&n)
	return n, err
}