package store

import "github.com/yufei/shendu/server/internal/model"

// activityKeep 是操作历史的保留条数。这是一份「回看用的清单」，不是审计日志，
// 无限增长只会让弹窗越来越难读。
const activityKeep = 500

// 活动来源：这条改动是谁做的。
//
// 单用户自托管，「谁改的」听起来是多用户才需要的东西——但真正费时间的恰恰是
// 「我明明没动它，怎么变了」。答案通常就在来源里：网页上点的、日历订阅同步过来的、
// 导入文件带进来的、快捷指令发起的。有了这一列，排查从「猜」变成「看」。
//
// 记不准就归 web（默认）：把来源错标成别的，比不标更误导。
const (
	SrcWeb    = "web"    // 网页 / PWA 界面
	SrcCalDAV = "caldav" // 日历订阅同步（Apple 提醒事项、系统日历等）
	SrcImport = "import" // 备份或第三方格式导入
	SrcAPI    = "api"    // 直接调接口（快捷指令、脚本）
)

// 活动来源的中文名，供前端展示。
var ActivitySourceLabel = map[string]string{
	SrcWeb:    "网页",
	SrcCalDAV: "日历同步",
	SrcImport: "导入",
	SrcAPI:    "接口",
}

// SetActivitySource 设定后续活动记录的来源，返回一个恢复函数。
//
// 用「设置-恢复」而不是把来源一路透传成参数：写路径有二十几处 logActivity 调用，
// 逐个加参数会让每条链路都变长，而来源只需要在**入口**处声明一次。
// defer 恢复保证嵌套与 panic 都不会污染后续请求——
// 否则一次日历同步失败就可能把后面所有网页操作都标成 caldav。
func (s *Store) SetActivitySource(src string) func() {
	if src == "" {
		src = SrcWeb
	}
	s.srcMu.Lock()
	prev := s.srcCur
	s.srcCur = src
	s.srcMu.Unlock()
	return func() {
		s.srcMu.Lock()
		s.srcCur = prev
		s.srcMu.Unlock()
	}
}

func (s *Store) activeSource() string {
	s.srcMu.RLock()
	defer s.srcMu.RUnlock()
	if s.srcCur == "" {
		return SrcWeb
	}
	return s.srcCur
}

// logActivity 记一笔操作历史。
//
// 刻意只记「值得回看」的动作：建了、做完了、删了、挪了清单。
// 改标题、调优先级这类高频微调不记 —— 记了也只是把真正重要的几笔冲掉。
// 写失败不改变调用方的结果：历史是附加品，不该让主流程失败。
func (s *Store) logActivity(kind string, taskID int64, title, detail string) {
	if _, err := s.db.Exec(
		`INSERT INTO activities(kind, task_id, title, detail, source, created_at) VALUES(?,?,?,?,?,?)`,
		kind, taskID, title, detail, s.activeSource(), model.Now(),
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
		`SELECT id, kind, task_id, title, detail, source, created_at FROM activities ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Activity{}
	for rows.Next() {
		var a model.Activity
		if err := rows.Scan(&a.ID, &a.Kind, &a.TaskID, &a.Title, &a.Detail, &a.Source, &a.CreatedAt); err != nil {
			return nil, err
		}
		// 老记录没有 source（迁移时补的默认值），别让它显示成空白。
		if a.Source == "" {
			a.Source = SrcWeb
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