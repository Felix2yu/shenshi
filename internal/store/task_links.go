package store

import (
	"fmt"
	"strings"

	"github.com/yufei/shendu/server/internal/model"
)

// attachLinks 为一批任务装配「关联 / 依赖」。
//
// related 是对称的：存一条 (A, B, related)，A、B 两边都能看到对方；
// blocked_by 是有向的：(A, B, blocked_by) 表示 A 被 B 阻塞。装配时如果
// 视角站在阻塞者一侧，把 kind 翻成 blocks，前端就不必再做方向推算。
// 一条 SQL 带出全部相关行与对方的标题、状态、清单名，避免逐任务往返。
func (s *Store) attachLinks(tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	idx := map[int64]int{}
	ids := make([]any, 0, len(tasks))
	ph := make([]string, 0, len(tasks))
	for i := range tasks {
		tasks[i].Links = []model.TaskLink{}
		idx[tasks[i].ID] = i
		ids = append(ids, tasks[i].ID)
		ph = append(ph, "?")
	}
	idList := strings.Join(ph, ",")
	q := `SELECT tl.id, tl.task_id, tl.linked_task_id, tl.kind,
	             ta.title, ta.status, la.name,
	             tb.title, tb.status, lb.name
	      FROM task_links tl
	      JOIN tasks ta ON ta.id = tl.task_id
	      JOIN lists la ON la.id = ta.list_id
	      JOIN tasks tb ON tb.id = tl.linked_task_id
	      JOIN lists lb ON lb.id = tb.list_id
	      WHERE tl.task_id IN (` + idList + `) OR tl.linked_task_id IN (` + idList + `)`
	rows, err := s.db.Query(q, append(append([]any{}, ids...), ids...)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var link model.TaskLink
		var sideA, sideB int64
		var infoA, infoB model.Task
		if err := rows.Scan(&link.ID, &sideA, &sideB, &link.Kind,
			&infoA.Title, &infoA.Status, &infoA.ListName,
			&infoB.Title, &infoB.Status, &infoB.ListName); err != nil {
			return err
		}
		// 每条边最多给两端各装配一份视图：
		// task_id 一侧按原方向看对方（B）；linked_task_id 一侧只看 related（对称），
		// blocked_by 的对方则翻成 blocks ——「它阻塞着我」与「我阻塞着它」是两种读法。
		if i, ok := idx[sideA]; ok {
			l := link
			l.TaskID, l.LinkedTaskID = sideA, sideB
			l.Title, l.Status, l.ListName = infoB.Title, infoB.Status, infoB.ListName
			tasks[i].Links = append(tasks[i].Links, l)
		}
		if j, ok := idx[sideB]; ok {
			switch link.Kind {
			case model.LinkRelated:
				l := link
				l.TaskID, l.LinkedTaskID = sideB, sideA
				l.Title, l.Status, l.ListName = infoA.Title, infoA.Status, infoA.ListName
				tasks[j].Links = append(tasks[j].Links, l)
			case model.LinkBlocked:
				l := link
				l.TaskID, l.LinkedTaskID = sideB, sideA
				l.Kind = model.LinkBlocking
				l.Title, l.Status, l.ListName = infoA.Title, infoA.Status, infoA.ListName
				tasks[j].Links = append(tasks[j].Links, l)
			}
		}
	}
	return rows.Err()
}

// AddTaskLink 建立任务间关联。拒绝自引用、重复边与依赖环路。
func (s *Store) AddTaskLink(taskID, linkedID int64, kind string) (*model.TaskLink, error) {
	if kind != model.LinkRelated && kind != model.LinkBlocked {
		return nil, ValidationError{Msg: "关联类型只能是 related 或 blocked_by"}
	}
	if taskID == linkedID {
		return nil, ValidationError{Msg: "任务不能与自己建立关联"}
	}
	if _, err := s.GetTask(linkedID); err != nil {
		return nil, ValidationError{Msg: "要关联的任务不存在"}
	}
	// 环检测、查重、写入同事务：SQLite 单写者串行，tx 内做完即封死
	// 「两个并发请求各自过检后插入、合起来成环」的窗口。
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if kind == model.LinkBlocked {
		// 依赖环会让「先做哪件」永远无解：顺着对方的依赖链往下走，走回自己即成环。
		blockedBy, err := blockedByIDs(tx, linkedID)
		if err != nil {
			return nil, err
		}
		seen := map[int64]bool{}
		for len(blockedBy) > 0 {
			cur := blockedBy[0]
			blockedBy = blockedBy[1:]
			if cur == taskID {
				return nil, ValidationError{Msg: "建立该依赖会形成环路，对方已（直接或间接）依赖本任务"}
			}
			if seen[cur] {
				continue
			}
			seen[cur] = true
			more, err := blockedByIDs(tx, cur)
			if err != nil {
				return nil, err
			}
			blockedBy = append(blockedBy, more...)
		}
	}
	// 同方向的边不允许重复；related 是对称的，反向（B→A）也算重复。
	dupQ := `SELECT COUNT(*) FROM task_links WHERE kind = ? AND task_id = ? AND linked_task_id = ?`
	dupArgs := []any{kind, taskID, linkedID}
	if kind == model.LinkRelated {
		dupQ = `SELECT COUNT(*) FROM task_links WHERE kind = ? AND
			((task_id = ? AND linked_task_id = ?) OR (task_id = ? AND linked_task_id = ?))`
		dupArgs = []any{kind, taskID, linkedID, linkedID, taskID}
	}
	exists := 0
	if err := tx.QueryRow(dupQ, dupArgs...).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		return nil, ValidationError{Msg: "这两个任务之间已存在同类关联"}
	}
	ts := model.Now()
	res, err := tx.Exec(`INSERT INTO task_links(task_id, linked_task_id, kind, created_at) VALUES(?,?,?,?)`,
		taskID, linkedID, kind, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE tasks SET updated_at = ? WHERE id IN (?, ?)`, ts, taskID, linkedID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	link := &model.TaskLink{
		ID:           id,
		TaskID:       taskID,
		LinkedTaskID: linkedID,
		Kind:         kind,
	}
	if t, err := s.GetTask(linkedID); err == nil {
		link.Title = t.Title
		link.Status = t.Status
		link.ListName = t.ListName
	}
	return link, nil
}

// blockedByIDs 返回 taskID 直接依赖的全部任务 id（即「谁的完成才能解锁它」）。
// 入参用 txer，让环检测可以在 AddTaskLink 的事务内执行。
func blockedByIDs(q txer, taskID int64) ([]int64, error) {
	rows, err := q.Query(`SELECT linked_task_id FROM task_links WHERE task_id = ? AND kind = ?`, taskID, model.LinkBlocked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteTaskLink 删除一条关联。
func (s *Store) DeleteTaskLink(id int64) error {
	res, err := s.db.Exec(`DELETE FROM task_links WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TaskIsBlocked 判断任务是否被未完成的依赖阻塞。UI 用它提示「先完成对方」，
// 不作为硬约束 —— 事务里的顺序只有做事的人自己清楚。
func (s *Store) TaskIsBlocked(taskID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM task_links tl JOIN tasks t ON t.id = tl.linked_task_id
		WHERE tl.task_id = ? AND tl.kind = ? AND t.status <> 'done'`, taskID, model.LinkBlocked).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("查询依赖状态失败: %w", err)
	}
	return n > 0, nil
}

// Blockers 返回「挡在 taskID 前面」的未完成依赖（id、标题、状态），
// 供详情面板直接渲染「被什么挡着」，不必让前端再翻全量 links 自己算。
func (s *Store) Blockers(taskID int64) ([]model.TaskLink, error) {
	return s.linksOfKind(taskID, model.LinkBlocked, true)
}

// linksOfKind 列出任务的某种关联边。unblockedOnly 时只保留对端未完成的
// （blocked_by 语义下「已完成」等于路已让开）。
func (s *Store) linksOfKind(taskID int64, kind string, openOnly bool) ([]model.TaskLink, error) {
	q := `SELECT tl.id, tl.task_id, tl.linked_task_id, tl.kind, tb.title, tb.status, lb.name
	      FROM task_links tl
	      JOIN tasks tb ON tb.id = tl.linked_task_id
	      JOIN lists lb ON lb.id = tb.list_id
	      WHERE tl.task_id = ? AND tl.kind = ?`
	if openOnly {
		q += ` AND tb.status <> 'done'`
	}
	q += ` ORDER BY tl.id`
	rows, err := s.db.Query(q, taskID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TaskLink{}
	for rows.Next() {
		var l model.TaskLink
		if err := rows.Scan(&l.ID, &l.TaskID, &l.LinkedTaskID, &l.Kind, &l.Title, &l.Status, &l.ListName); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
