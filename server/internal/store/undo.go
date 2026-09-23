package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// undoTTL 是撤销槽位的保鲜期。过期之后被删任务的文件才真正从磁盘清理干净。
//
// 为什么不是「立刻删文件」：删除是唯一不可逆的动作，留一个能反悔的窗口，
// 比事后从备份里翻要实际得多。附件内容一旦删掉就找不回来，所以文件删得比记录晚一步。
const undoTTL = 10 * time.Minute

// UndoState 描述当前是否还有可撤销的删除。
type UndoState struct {
	Available bool   `json:"available"`
	Label     string `json:"label"`
	Count     int    `json:"count"`
	At        string `json:"at"`
}

// stageUndo 把即将被删除的任务存进撤销槽位（只保留最近一次）。
//
// 调用的时机必须是「记录还没删、文件还没动」之前。被顶替的旧槽位会在这里清理掉文件，
// 于是磁盘占用是恒定的 —— 最多留一次删除的附件。
func (s *Store) stageUndo(label string, tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	// 先清掉上一份，连同它引用的文件。
	if err := s.dropUndo(true); err != nil {
		return err
	}
	payload, err := json.Marshal(tasks)
	if err != nil {
		return err
	}
	files := map[string]bool{}
	for _, t := range tasks {
		for _, a := range t.Attachments {
			if a.File != "" {
				files[a.File] = true
			}
		}
	}
	names := make([]string, 0, len(files))
	for f := range files {
		names = append(names, f)
	}
	blob, err := json.Marshal(names)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO undo_slot(id, label, payload, files, created_at) VALUES(1,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET label = excluded.label, payload = excluded.payload,
		   files = excluded.files, created_at = excluded.created_at`,
		label, string(payload), string(blob), model.Now(),
	)
	return err
}

// UndoState 返回撤销槽位的现状。
func (s *Store) UndoState() (*UndoState, error) {
	if err := s.expireUndo(); err != nil {
		return nil, err
	}
	var label, payload, at string
	err := s.db.QueryRow(`SELECT label, payload, created_at FROM undo_slot WHERE id = 1`).Scan(&label, &payload, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return &UndoState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []model.Task
	if err := json.Unmarshal([]byte(payload), &tasks); err != nil {
		return &UndoState{}, nil
	}
	return &UndoState{Available: len(tasks) > 0, Label: label, Count: len(tasks), At: at}, nil
}

// Undo 恢复撤销槽位里的任务，返回恢复的条数。
//
// 恢复出来的是新记录（id 会变），但内容按原样还原：日期、标签、子任务、
// 完成状态与重复规则都在，附件记录也按原存储名挂回去。
func (s *Store) Undo() (int, error) {
	if err := s.expireUndo(); err != nil {
		return 0, err
	}
	var payload string
	if err := s.db.QueryRow(`SELECT payload FROM undo_slot WHERE id = 1`).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ValidationError{Msg: "没有可撤销的操作"}
		}
		return 0, err
	}
	var tasks []model.Task
	if err := json.Unmarshal([]byte(payload), &tasks); err != nil {
		return 0, ValidationError{Msg: "撤销内容已损坏，无法恢复"}
	}
	if len(tasks) == 0 {
		return 0, ValidationError{Msg: "没有可撤销的操作"}
	}

	inbox, err := s.InboxListID()
	if err != nil {
		return 0, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	restored := make([]int64, 0, len(tasks))
	idMap := make(map[int64]int64, len(tasks))
	for _, t := range tasks {
		listID := t.ListID
		// 清单若在删除之后也被删掉了，退回收件箱，而不是让整次撤销失败。
		var present int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE id = ?`, listID).Scan(&present); err != nil {
			return 0, err
		}
		if present == 0 {
			listID = inbox
		}
		newID, err := insertTaskCopy(tx, t, listID)
		if err != nil {
			return 0, err
		}
		idMap[t.ID] = newID
		ids := make([]int64, 0, len(t.Tags))
		for _, g := range t.Tags {
			var ok int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, g.ID).Scan(&ok); err != nil {
				return 0, err
			}
			if ok == 1 {
				ids = append(ids, g.ID)
			}
		}
		if err := syncTaskTags(tx, newID, ids); err != nil {
			return 0, err
		}
		for _, a := range t.Attachments {
			if a.File == "" {
				continue
			}
			// 文件还在才挂记录 —— 撤销窗口之外被清理过的附件，不要留成死链。
			if _, err := os.Stat(s.filePath(a.File)); err != nil {
				continue
			}
			if _, err := tx.Exec(
				`INSERT INTO attachments(task_id, name, file, size, mime, created_at) VALUES(?,?,?,?,?,?)`,
				newID, a.Name, a.File, a.Size, a.Mime, stamp(a.CreatedAt),
			); err != nil {
				return 0, err
			}
		}
		restored = append(restored, newID)
	}

	// 依赖/关联边原样回放。对端可能没被删（单任务删除），此时用原 id 接回；
	// attachLinks 的视角视图会把对向边也挂上，related 是对称关系，反向补一条，
	// UNIQUE 约束保证重复插入无害。指向的对端已不存在时整条跳过。
	for _, t := range tasks {
		from, okFrom := idMap[t.ID]
		if !okFrom {
			continue
		}
		for _, l := range t.Links {
			if l.Kind == "" || l.LinkedTaskID <= 0 {
				continue
			}
			to := l.LinkedTaskID
			if mapped, ok := idMap[l.LinkedTaskID]; ok {
				to = mapped
			} else {
				var exists int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, l.LinkedTaskID).Scan(&exists); err != nil {
					return 0, err
				}
				if exists == 0 {
					continue
				}
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO task_links(task_id, linked_task_id, kind, created_at) VALUES(?,?,?,?)`,
				from, to, l.Kind, model.Now()); err != nil {
				return 0, err
			}
			if l.Kind == model.LinkRelated && to != from {
				if _, err := tx.Exec(`INSERT OR IGNORE INTO task_links(task_id, linked_task_id, kind, created_at) VALUES(?,?,?,?)`,
					to, from, l.Kind, model.Now()); err != nil {
					return 0, err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// 记录已经回来，引用的文件自然不能删：清槽位时保留文件。
	if _, err := s.db.Exec(`DELETE FROM undo_slot WHERE id = 1`); err != nil {
		return 0, err
	}
	for _, id := range restored {
		if t, err := s.GetTask(id); err == nil {
			s.emit(model.EventTaskCreated, t)
		}
	}
	if restored[0] > 0 {
		title := tasks[0].Title
		detail := "撤销删除，已恢复"
		if len(restored) > 1 {
			detail = "撤销删除，已恢复 " + itoa(len(restored)) + " 件"
		}
		s.logActivity(model.ActUndone, restored[0], title, detail)
	}
	return len(restored), nil
}

// filePath 由存储名拼出磁盘路径（拒绝带目录成分的名字）。
func (s *Store) filePath(stored string) string {
	return s.storedPath(stored)
}

func itoa(n int) string { return strconv.Itoa(n) }

// DropUndo 显式放弃撤销机会（例如用户点了「知道了」）。文件会被真正删除。
func (s *Store) DropUndo() error { return s.dropUndo(true) }

// expireUndo 惰性清理过期槽位：没有任何后台协程，读到时发现过期就顺手清掉。
func (s *Store) expireUndo() error {
	var at string
	err := s.db.QueryRow(`SELECT created_at FROM undo_slot WHERE id = 1`).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil || time.Since(ts) > undoTTL {
		return s.dropUndo(true)
	}
	return nil
}

// dropUndo 清空槽位。removeFiles 为 false 时保留附件文件（撤销成功后走这条路）。
func (s *Store) dropUndo(removeFiles bool) error {
	if removeFiles {
		var files string
		err := s.db.QueryRow(`SELECT files FROM undo_slot WHERE id = 1`).Scan(&files)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var names []string
		if strings.TrimSpace(files) != "" {
			_ = json.Unmarshal([]byte(files), &names)
		}
		for _, name := range names {
			_ = os.Remove(s.filePath(name))
		}
	}
	_, err := s.db.Exec(`DELETE FROM undo_slot WHERE id = 1`)
	return err
}
