package store

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// 导入模式。
const (
	ImportReplace = "replace" // 清空现有数据后按备份重建
	ImportMerge   = "merge"   // 保留现有数据，把备份作为副本追加进来
)

const exportVersion = 1

// ExportBundle 是全量备份的结构。Version 预留给将来做格式升级。
type ExportBundle struct {
	Version      int                  `json:"version"`
	App          string               `json:"app"`
	ExportedAt   string               `json:"exportedAt"`
	InboxListID  int64                `json:"inboxListId"`
	Folders      []model.Folder       `json:"folders"`
	Lists        []model.List         `json:"lists"`
	Tasks        []model.Task         `json:"tasks"`
	TaskLinks    []model.TaskLink     `json:"taskLinks,omitempty"`
	Tags         []model.Tag          `json:"tags"`
	Reviews      []model.Review       `json:"reviews"`
	Focus        []model.FocusSession `json:"focus"`
	Habits       []model.Habit        `json:"habits"`
	HabitLogs    []model.HabitLog     `json:"habitLogs"`
	SavedFilters []model.SavedFilter  `json:"savedFilters"`
	Templates    []model.TaskTemplate `json:"templates,omitempty"`
	Webhooks     []model.Webhook      `json:"webhooks,omitempty"`
	Settings     map[string]string    `json:"settings"`
}

// ImportResult 汇报各类数据的导入条数。
type ImportResult struct {
	Mode         string `json:"mode"`
	Folders      int    `json:"folders"`
	Lists        int    `json:"lists"`
	Tasks        int    `json:"tasks"`
	Tags         int    `json:"tags"`
	Reviews      int    `json:"reviews"`
	Focus        int    `json:"focus"`
	Habits       int    `json:"habits"`
	HabitLogs    int    `json:"habitLogs"`
	SavedFilters int    `json:"savedFilters"`
	Templates    int    `json:"templates"`
	Webhooks     int    `json:"webhooks"`
	// 附件单独报数：裸 JSON 备份不带文件，恢复不出来是预期内的，
	// 但不说一声用户会以为已经一并恢复了。
	Attachments       int `json:"attachments"`
	AttachmentsMissed int `json:"attachmentsMissed"`
}

// txer 是 *sql.DB 与 *sql.Tx 共有的方法集，让同一套导入逻辑在事务内外都能复用。
// 方法签名必须与 database/sql 完全一致，否则 *sql.Tx 不满足该接口。
type txer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Export 导出全量数据。任务一律含子任务与标签，保证备份是自洽的。
func (s *Store) Export() (*ExportBundle, error) {
	// 用扁平分组而非树：备份是「一张表一份数据」，嵌套结构在导入时还要再拆一次。
	folders, err := s.flatFolders()
	if err != nil {
		return nil, err
	}
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	// IncludeArchived：归档清单里的任务同样是数据，备份漏掉它们就等于悄悄丢东西。
	// IncludeTaskArchived 同理：已归档的任务也要原样带走。
	tasks, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual", IncludeArchived: true, IncludeTaskArchived: true})
	if err != nil {
		return nil, err
	}
	tags, err := s.Tags()
	if err != nil {
		return nil, err
	}
	// limit<0 表示取全部，否则备份会静默截断到默认条数。
	reviews, err := s.ListReviews(-1)
	if err != nil {
		return nil, err
	}
	focus, err := s.ListFocus(-1)
	if err != nil {
		return nil, err
	}
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	habits, err := s.Habits(true) // 含已归档：备份要能完整还原
	if err != nil {
		return nil, err
	}
	habitLogs, err := s.AllHabitLogs()
	if err != nil {
		return nil, err
	}
	savedFilters, err := s.SavedFilters()
	if err != nil {
		return nil, err
	}
	inbox, err := s.InboxListID()
	if err != nil {
		return nil, err
	}
	// 任务间关联按原始边导出。装配到任务上的 Links 是「视角视图」，
	// 同一条 related 边会在两端各出现一次，直接落库会翻倍。
	links, err := s.rawTaskLinks()
	if err != nil {
		return nil, err
	}
	// 模板与 Webhook 也要能换机带走：配置类数据丢了等于让人重配一遍。
	templates, err := s.ListTemplates()
	if err != nil {
		return nil, err
	}
	webhooks, err := s.rawWebhooks()
	if err != nil {
		return nil, err
	}

	// 分组不再嵌套子节点：父子关系由 ParentID 表达，导出两份会互相打架。
	for i := range folders {
		folders[i].Lists = nil
		folders[i].Children = nil
	}

	return &ExportBundle{
		Version:      exportVersion,
		App:          "慎始",
		ExportedAt:   model.Now(),
		InboxListID:  inbox,
		Folders:      folders,
		Lists:        lists,
		Tasks:        tasks,
		TaskLinks:    links,
		Tags:         tags,
		Reviews:      reviews,
		Focus:        focus,
		Habits:       habits,
		HabitLogs:    habitLogs,
		SavedFilters: savedFilters,
		Templates:    templates,
		Webhooks:     webhooks,
		Settings:     settings,
	}, nil
}

// rawWebhooks 导出全部 Webhook（含真实密钥）。备份是给本人换机用的，
// 密钥抹掉就等于换机后所有回调签名校验集体失效。
func (s *Store) rawWebhooks() ([]model.Webhook, error) {
	rows, err := s.db.Query(`SELECT id, name, url, secret, events, enabled, created_at, updated_at FROM webhooks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		w.HasSecret = w.Secret != ""
		out = append(out, w)
	}
	return out, rows.Err()
}

// rawTaskLinks 导出任务间关联的原始边（不含装配来的展示字段）。
func (s *Store) rawTaskLinks() ([]model.TaskLink, error) {
	rows, err := s.db.Query(`SELECT id, task_id, linked_task_id, kind FROM task_links`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TaskLink{}
	for rows.Next() {
		var l model.TaskLink
		if err := rows.Scan(&l.ID, &l.TaskID, &l.LinkedTaskID, &l.Kind); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ExportJSON 返回带缩进的 JSON，便于人工查看与 diff。
func (s *Store) ExportJSON() ([]byte, error) {
	b, err := s.Export()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(b, "", "  ")
}

var priorityLabel = map[int]string{0: "无", 1: "低", 2: "中", 3: "高"}

// ExportCSV 把任务摊平成一张表，供 Excel 或其它工具消费。
// 只导出任务本身——CSV 往返会丢失子任务与重复规则等结构，需要完整备份请用 JSON。
func (s *Store) ExportCSV() ([]byte, error) {
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	folders, err := s.Folders()
	if err != nil {
		return nil, err
	}
	tasks, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual", IncludeArchived: true})
	if err != nil {
		return nil, err
	}

	folderOf := map[int64]string{}
	for _, f := range folders {
		folderOf[f.ID] = f.Name
	}
	listOf := map[int64]model.List{}
	for _, l := range lists {
		listOf[l.ID] = l
	}

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM：让 Excel 正确识别 UTF-8
	w := csv.NewWriter(&buf)

	if err := w.Write([]string{
		"id", "标题", "清单", "分组", "状态", "优先级", "置顶", "收藏",
		"开始日期", "日期", "开始时间", "结束时间", "链接", "重复规则", "重要", "紧急",
		"标签", "子任务已完成", "子任务总数", "备注", "创建时间", "完成时间",
	}); err != nil {
		return nil, err
	}

	for _, t := range tasks {
		l := listOf[t.ListID]
		folder := ""
		if l.FolderID != nil {
			folder = folderOf[*l.FolderID]
		}
		status := "未完成"
		if t.Status == model.StatusDone {
			status = "已完成"
		}
		names := make([]string, 0, len(t.Tags))
		for _, g := range t.Tags {
			names = append(names, g.Name)
		}
		if err := w.Write([]string{
			strconv.FormatInt(t.ID, 10),
			t.Title,
			l.Name,
			folder,
			status,
			priorityLabel[t.Priority],
			boolWord(t.Pinned),
			boolWord(t.Starred),
			derefStr(t.StartDate, ""),
			derefStr(t.DueDate, ""),
			derefStr(t.DueTime, ""),
			derefStr(t.EndTime, ""),
			t.URL,
			derefStr(t.RepeatRule, ""),
			boolWord(t.Important),
			boolWord(t.Urgent),
			strings.Join(names, " "),
			strconv.Itoa(t.SubtaskDone),
			strconv.Itoa(len(t.Subtasks)),
			t.Notes,
			t.CreatedAt,
			derefStr(t.CompletedAt, ""),
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func boolWord(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// Import 导入备份。merge 重建 id 映射并把数据作为副本追加；
// replace 则在清空后按原 id 精确复原，用于灾难恢复。
//
// src 提供随备份一起带来的附件内容（压缩包导入时非 nil）；
// 裸 JSON 备份传 nil，此时只有磁盘上原本就在的附件能被挂回去。
func (s *Store) Import(b *ExportBundle, mode string, src AttachmentSource) (*ImportResult, error) {
	if mode != ImportMerge && mode != ImportReplace {
		return nil, ValidationError{Msg: "导入模式只能是 merge 或 replace"}
	}
	if b == nil || (len(b.Lists) == 0 && len(b.Tasks) == 0 && len(b.Habits) == 0) {
		return nil, ValidationError{Msg: "备份内容为空，无法导入"}
	}

	// 先落盘再入库：反过来的话，事务提交后写文件失败会在库里留下指向空文件的记录。
	// 反过来失败只留几个没人引用的随机名文件，无害。
	files, missed, err := s.stageAttachments(b, mode, src)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res := &ImportResult{Mode: mode, AttachmentsMissed: missed}
	if mode == ImportReplace {
		err = importReplace(tx, b, res, files)
	} else {
		err = importMerge(tx, b, res, files)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// stageAttachments 把备份里的附件写进附件目录，返回「备份中的存储名 → 磁盘上的存储名」。
//
// replace 模式沿用原名：任务 id 不变，重复导入不会堆出一串副本。
// merge 模式必须换名：任务会重新编号，两条记录共用同一个文件的话，
// 删掉其中一条会连带把另一条的文件带走。
func (s *Store) stageAttachments(b *ExportBundle, mode string, src AttachmentSource) (map[string]string, int, error) {
	out := map[string]string{}
	missed := 0
	done := map[string]bool{}
	for _, t := range b.Tasks {
		for i := range t.Attachments {
			a := t.Attachments[i]
			if a.File == "" || done[a.File] {
				continue
			}
			done[a.File] = true

			// 存储名来自备份、不可信：带目录成分的一律拒收（不落盘、不入库），
			// 按缺失计数如实上报，别让「../shenshi.db」读走或写出附件目录之外的东西。
			if _, valid := safeStoredName(a.File); !valid {
				missed++
				continue
			}

			data, ok, err := attachmentBytes(src, s.attachmentDir, a.File, mode)
			if err != nil {
				return nil, 0, err
			}
			if !ok {
				missed++
				continue
			}
			stored := a.File
			// data 为 nil 表示文件已在位，只需把记录挂上去，不必重写。
			if data != nil {
				if mode == ImportMerge {
					stored = newStoredName(a.File)
				}
				if err := s.writeAttachmentFile(stored, data); err != nil {
					return nil, 0, err
				}
			}
			out[a.File] = stored
		}
	}
	return out, missed, nil
}

// attachmentBytes 取出一个附件的内容，三种来源按优先级：
// ① 备份自带（压缩包） ② 磁盘上原本就在 ③ 都没有 —— 返回 ok=false。
//
// 返回 nil 的 data 且 ok 为真，表示「文件已在位、沿用原名即可，不必重写」，
// 只在 replace 模式下出现。
func attachmentBytes(src AttachmentSource, dir, stored string, mode string) (data []byte, ok bool, err error) {
	if src != nil {
		rc, err := src.Open(stored)
		switch {
		case err == nil:
			defer rc.Close()
			// 与上传同一个上限，避免畸形备份塞进一个巨大文件。
			b, err := io.ReadAll(io.LimitReader(rc, maxAttachmentSize+1))
			if err != nil {
				return nil, false, fmt.Errorf("读取备份里的附件失败: %w", err)
			}
			if int64(len(b)) > maxAttachmentSize {
				return nil, false, ValidationError{Msg: "备份里的附件超过 32MB 上限"}
			}
			return b, true, nil
		case notProvided(err):
			// 这次备份没带它，往下看磁盘。
		default:
			return nil, false, fmt.Errorf("读取备份里的附件失败: %w", err)
		}
	}

	// 防御纵深：进入拼接前再验一次，别让这个独立函数依赖调用方已校验。
	clean, valid := safeStoredName(stored)
	if !valid {
		return nil, false, nil
	}
	disk := filepath.Join(dir, clean)
	if _, err := os.Stat(disk); err != nil {
		return nil, false, nil // 到处都没有：记一笔缺失，任务本身照常导入
	}
	if mode == ImportReplace {
		return nil, true, nil // 沿用原名，重复导入不会堆副本
	}
	f, err := os.Open(disk)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxAttachmentSize+1))
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// newStoredName 生成一个不冲突的存储名，保留原扩展名。
func newStoredName(old string) string {
	var salt [8]byte
	if _, err := rand.Read(salt[:]); err != nil {
		// 随机数取不到时退回时间戳，唯一性略弱但不至于撞车。
		return fmt.Sprintf("imp-%x%s", time.Now().UnixNano(), safeExt(old))
	}
	return fmt.Sprintf("imp-%s%s", hex.EncodeToString(salt[:]), safeExt(old))
}

// writeAttachmentFile 把附件内容落到磁盘：先写临时文件再改名，
// 中途失败不会留下半截文件被当成完整附件。
func (s *Store) writeAttachmentFile(stored string, data []byte) error {
	clean, valid := safeStoredName(stored)
	if !valid {
		return fmt.Errorf("非法的附件存储名: %q", stored)
	}
	if err := os.MkdirAll(s.attachmentDir, 0o755); err != nil {
		return fmt.Errorf("创建附件目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(s.attachmentDir, ".import-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写入附件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(s.attachmentDir, clean)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func importReplace(tx txer, b *ExportBundle, res *ImportResult, files map[string]string) error {
	// 按外键层级自上而下清空；reminder_log 一并清掉，免得旧台账挡住新数据的提醒。
	for _, stmt := range []string{
		`DELETE FROM task_tags`,
		`DELETE FROM task_links`,
		`DELETE FROM subtasks`,
		`DELETE FROM attachments`,
		`DELETE FROM tasks`,
		`DELETE FROM lists`,
		`DELETE FROM folders`,
		`DELETE FROM tags`,
		`DELETE FROM reviews`,
		`DELETE FROM focus_sessions`,
		`DELETE FROM habit_logs`,
		`DELETE FROM habits`,
		`DELETE FROM reminder_log`,
		`DELETE FROM saved_filters`,
		`DELETE FROM webhook_deliveries`,
		`DELETE FROM webhooks`,
		`DELETE FROM task_templates`,
		// 旧撤销槽位引用的是被清掉的附件，留着会在过期时误删刚导入的文件。
		`DELETE FROM undo_slot`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	// 分组分两趟写：parent_id 可能指向后面才插入的分组，而外键是即时校验的，
	// 一趟写完会在「子先父后」的顺序上直接失败。
	for _, f := range b.Folders {
		if _, err := tx.Exec(
			`INSERT INTO folders(id, parent_id, name, color, icon, sort_order, collapsed, archived, created_at) VALUES(?,NULL,?,?,?,?,?,?,?)`,
			f.ID, f.Name, f.Color, f.Icon, f.SortOrder, boolInt(f.Collapsed), boolInt(f.Archived), stamp(f.CreatedAt),
		); err != nil {
			return err
		}
		res.Folders++
	}
	for _, f := range b.Folders {
		if f.ParentID == nil {
			continue
		}
		if _, err := tx.Exec(`UPDATE folders SET parent_id = ? WHERE id = ?`, f.ParentID, f.ID); err != nil {
			return err
		}
	}

	valid := map[int64]bool{}
	for _, l := range b.Lists {
		if _, err := tx.Exec(
			`INSERT INTO lists(id, folder_id, name, color, icon, sort_order, is_inbox, archived, starred, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.FolderID, l.Name, l.Color, l.Icon, l.SortOrder, boolInt(l.ID == b.InboxListID),
			boolInt(l.Archived), boolInt(l.Starred), stamp(l.CreatedAt),
		); err != nil {
			return err
		}
		valid[l.ID] = true
		res.Lists++
	}

	for _, g := range b.Tags {
		if _, err := tx.Exec(
			`INSERT INTO tags(id, name, color, created_at) VALUES(?,?,?,?)`,
			g.ID, g.Name, g.Color, stamp(g.CreatedAt),
		); err != nil {
			return err
		}
		res.Tags++
	}

	for _, t := range b.Tasks {
		// 备份若有损坏（任务指向不存在的清单），跳过它而不是让整次导入失败。
		if !valid[t.ListID] {
			continue
		}
		if err := insertTaskWithID(tx, t); err != nil {
			return err
		}
		if err := insertAttachments(tx, t, t.ID, files, res); err != nil {
			return err
		}
		res.Tasks++
	}

	// replace 模式保留原始任务 id，关联边可以原样回放。
	if err := restoreTaskLinks(tx, b.TaskLinks, func(id int64) (int64, bool) { return id, valid[id] }, res); err != nil {
		return err
	}

	for _, r := range b.Reviews {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO reviews(id, date, mood, wins, blockers, tomorrow, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Date, r.Mood, r.Wins, r.Blockers, r.Tomorrow, stamp(r.CreatedAt), stamp(r.UpdatedAt),
		); err != nil {
			return err
		}
		res.Reviews++
	}

	for _, f := range b.Focus {
		// 任务 id 可能指向备份外的任务（截断的备份、手改过的 JSON），
		// 挂不上就记为无关联，而不是让整批导入被外键打回。
		var taskID *int64
		if f.TaskID != nil && valid[*f.TaskID] {
			taskID = f.TaskID
		}
		if _, err := tx.Exec(
			`INSERT INTO focus_sessions(id, task_id, minutes, started_at, ended_at) VALUES(?,?,?,?,?)`,
			f.ID, taskID, f.Minutes, stamp(f.StartedAt), stamp(f.EndedAt),
		); err != nil {
			return err
		}
		res.Focus++
	}

	for k, v := range b.Settings {
		if _, err := tx.Exec(
			`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v,
		); err != nil {
			return err
		}
	}

	for _, f := range b.SavedFilters {
		if _, err := tx.Exec(
			`INSERT INTO saved_filters(id, name, query, sort_order, created_at) VALUES(?,?,?,?,?)`,
			f.ID, f.Name, f.Query, f.SortOrder, stamp(f.CreatedAt),
		); err != nil {
			return err
		}
		res.SavedFilters++
	}

	// 模板与 Webhook 原样恢复（表已清空，可带原 id）。模板的 list_id 指向的
	// 清单若不存在，置 NULL 而不是让整批导入失败。
	for _, t := range b.Templates {
		var listID *int64
		if t.ListID != nil {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE id = ?`, *t.ListID).Scan(&exists); err != nil {
				return err
			}
			if exists == 1 {
				listID = t.ListID
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO task_templates(id, name, title, notes, list_id, priority, due_offset, due_time, reminders,
				repeat_rule, important, urgent, tag_ids, subtasks, sort_order, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.Name, t.Title, t.Notes, listID, t.Priority, t.DueOffset, t.DueTime,
			mustJSON(nonNilInts(t.Reminders)), t.RepeatRule, boolInt(t.Important), boolInt(t.Urgent),
			mustJSON(nonNilInt64s(t.TagIDs)), mustJSON(nonNilStrings(t.Subtasks)),
			t.SortOrder, stamp(t.CreatedAt), stamp(t.UpdatedAt),
		); err != nil {
			return err
		}
		res.Templates++
	}
	for _, w := range b.Webhooks {
		events := w.Events
		if events == nil {
			events = []string{}
		}
		if _, err := tx.Exec(
			`INSERT INTO webhooks(id, name, url, secret, events, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			w.ID, w.Name, w.URL, w.Secret, mustJSON(events), boolInt(w.Enabled), stamp(w.CreatedAt), stamp(w.UpdatedAt),
		); err != nil {
			return err
		}
		res.Webhooks++
	}

	habitOK := map[int64]bool{}
	for _, h := range b.Habits {
		if _, err := tx.Exec(`INSERT INTO habits(id, name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, h.Name, h.Icon, h.Color, habitCadenceOr(h.Cadence), h.Weekdays, habitTargetOr(h.Target),
			habitDayOr(h.StartDate), h.Note, boolInt(h.Archived), h.SortOrder, stamp(h.CreatedAt), stamp(h.UpdatedAt),
		); err != nil {
			return err
		}
		habitOK[h.ID] = true
		res.Habits++
	}
	for _, l := range b.HabitLogs {
		if !habitOK[l.HabitID] || l.Count <= 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO habit_logs(id, habit_id, day, count, note, created_at) VALUES(?,?,?,?,?,?)`,
			l.ID, l.HabitID, l.Day, l.Count, l.Note, stamp(l.CreatedAt),
		); err != nil {
			return err
		}
		res.HabitLogs++
	}

	// 收件箱必须有且仅有一个，否则新建任务会找不到落点。
	return ensureInbox(tx)
}

func importMerge(tx txer, b *ExportBundle, res *ImportResult, files map[string]string) error {
	// 分组与标签按名称合并到已有记录上；清单与任务一律作为新纪录追加。
	folderID, err := mergeFolders(tx, b.Folders, res)
	if err != nil {
		return err
	}
	tagID, err := mergeTags(tx, b.Tags, res)
	if err != nil {
		return err
	}

	listID := map[int64]int64{}
	for _, l := range b.Lists {
		var fid *int64
		if l.FolderID != nil {
			if mapped, ok := folderID[*l.FolderID]; ok {
				fid = &mapped
			}
		}
		// 备份里的收件箱不重复创建，直接并入现有收件箱。
		if l.ID == b.InboxListID {
			if existing, err := firstInboxID(tx); err == nil {
				listID[l.ID] = existing
				continue
			}
		}
		r, err := tx.Exec(
			`INSERT INTO lists(folder_id, name, color, icon, sort_order, is_inbox, archived, starred, created_at) VALUES(?,?,?,?,?,0,?,?,?)`,
			fid, l.Name, l.Color, l.Icon, l.SortOrder, boolInt(l.Archived), boolInt(l.Starred), stamp(l.CreatedAt),
		)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		listID[l.ID] = id
		res.Lists++
	}

	if err := ensureInbox(tx); err != nil {
		return err
	}

	taskID := map[int64]int64{}
	for _, t := range b.Tasks {
		target, ok := listID[t.ListID]
		if !ok {
			continue // 备份里的清单缺失时跳过该任务，而不是整批失败
		}
		newID, err := insertTaskCopy(tx, t, target)
		if err != nil {
			return err
		}
		taskID[t.ID] = newID
		ids := make([]int64, 0, len(t.Tags))
		for _, g := range t.Tags {
			if mapped, ok := tagID[g.ID]; ok {
				ids = append(ids, mapped)
			}
		}
		if err := syncTaskTags(tx, newID, ids); err != nil {
			return err
		}
		if err := insertAttachments(tx, t, newID, files, res); err != nil {
			return err
		}
		res.Tasks++
	}

	// merge 模式任务全部重新编号，关联边按新旧 id 映射重建；断边的跳过。
	if err := restoreTaskLinks(tx, b.TaskLinks, func(id int64) (int64, bool) {
		n, ok := taskID[id]
		return n, ok
	}, res); err != nil {
		return err
	}

	for _, r := range b.Reviews {
		// 复盘按日期唯一，同一天保留现有内容。
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO reviews(date, mood, wins, blockers, tomorrow, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
			r.Date, r.Mood, r.Wins, r.Blockers, r.Tomorrow, stamp(r.CreatedAt), stamp(r.UpdatedAt),
		); err != nil {
			return err
		}
		res.Reviews++
	}

	for _, f := range b.Focus {
		// 专注记录原本指向的任务已被重新编号，不强行关联，避免挂到错误的任务上。
		if _, err := tx.Exec(
			`INSERT INTO focus_sessions(task_id, minutes, started_at, ended_at) VALUES(NULL,?,?,?)`,
			f.Minutes, stamp(f.StartedAt), stamp(f.EndedAt),
		); err != nil {
			return err
		}
		res.Focus++
	}

	// 设置与 replace 对称：merge 也写回备份里的 settings，换机合并后外观等配置不丢。
	for k, v := range b.Settings {
		if _, err := tx.Exec(
			`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v,
		); err != nil {
			return err
		}
	}

	// 习惯一律作为新纪录追加，并把打卡流水挂到新 id 上。
	habitID := map[int64]int64{}
	for _, h := range b.Habits {
		r, err := tx.Exec(`INSERT INTO habits(name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.Name, h.Icon, h.Color, habitCadenceOr(h.Cadence), h.Weekdays, habitTargetOr(h.Target),
			habitDayOr(h.StartDate), h.Note, boolInt(h.Archived), h.SortOrder, stamp(h.CreatedAt), stamp(h.UpdatedAt),
		)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		habitID[h.ID] = id
		res.Habits++
	}
	for _, l := range b.HabitLogs {
		target, ok := habitID[l.HabitID]
		if !ok || l.Count <= 0 {
			continue
		}
		// 同一天已有打卡则保留次数较多的一次，避免合并后进度倒退。
		if _, err := tx.Exec(`INSERT INTO habit_logs(habit_id, day, count, note, created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(habit_id, day) DO UPDATE SET count = MAX(habit_logs.count, excluded.count)`,
			target, l.Day, l.Count, l.Note, stamp(l.CreatedAt)); err != nil {
			return err
		}
		res.HabitLogs++
	}

	// 保存的筛选条件按名称去重：同名的保留本地那一份，避免合并后侧栏出现两条一样的。
	for _, f := range b.SavedFilters {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM saved_filters WHERE name = ?`, f.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO saved_filters(name, query, sort_order, created_at) VALUES(?,?,?,?)`,
			f.Name, f.Query, f.SortOrder, stamp(f.CreatedAt),
		); err != nil {
			return err
		}
		res.SavedFilters++
	}

	// 模板与 Webhook 作为新纪录追加；同名模板保留本地那份（与筛选同策略）。
	for _, t := range b.Templates {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM task_templates WHERE name = ?`, t.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		var listID2 *int64
		if t.ListID != nil {
			if mapped, ok := listID[*t.ListID]; ok {
				listID2 = &mapped
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO task_templates(name, title, notes, list_id, priority, due_offset, due_time, reminders,
				repeat_rule, important, urgent, tag_ids, subtasks, sort_order, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.Name, t.Title, t.Notes, listID2, t.Priority, t.DueOffset, t.DueTime,
			mustJSON(nonNilInts(t.Reminders)), t.RepeatRule, boolInt(t.Important), boolInt(t.Urgent),
			mustJSON(nonNilInt64s(t.TagIDs)), mustJSON(nonNilStrings(t.Subtasks)),
			t.SortOrder, stamp(t.CreatedAt), stamp(t.UpdatedAt),
		); err != nil {
			return err
		}
		res.Templates++
	}
	for _, w := range b.Webhooks {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM webhooks WHERE url = ?`, w.URL).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		events := w.Events
		if events == nil {
			events = []string{}
		}
		if _, err := tx.Exec(
			`INSERT INTO webhooks(name, url, secret, events, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
			w.Name, w.URL, w.Secret, mustJSON(events), boolInt(w.Enabled), stamp(w.CreatedAt), stamp(w.UpdatedAt),
		); err != nil {
			return err
		}
		res.Webhooks++
	}

	return nil
}

func habitCadenceOr(s string) string {
	if s == model.CadenceWeekly {
		return model.CadenceWeekly
	}
	return model.CadenceDaily
}

func habitTargetOr(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func habitDayOr(s string) string {
	if err := checkDay(s); err != nil {
		return model.Now()[:10]
	}
	return s
}

func mergeFolders(tx txer, folders []model.Folder, res *ImportResult) (map[int64]int64, error) {
	existing := map[string]int64{}
	rows, err := tx.Query(`SELECT id, name FROM folders`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		existing[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mapped := map[int64]int64{}
	// 第一趟：分组先都插成顶层（parent_id 为空），并把待接的父子关系记下来。
	type pending struct {
		id     int64
		parent int64
	}
	todo := []pending{}
	for _, f := range folders {
		if id, ok := existing[f.Name]; ok {
			mapped[f.ID] = id
			continue
		}
		r, err := tx.Exec(
			`INSERT INTO folders(parent_id, name, color, icon, sort_order, collapsed, archived, created_at) VALUES(NULL,?,?,?,?,?,?,?)`,
			f.Name, f.Color, f.Icon, f.SortOrder, boolInt(f.Collapsed), boolInt(f.Archived), stamp(f.CreatedAt),
		)
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		mapped[f.ID] = id
		existing[f.Name] = id
		res.Folders++
		if f.ParentID != nil {
			todo = append(todo, pending{id: id, parent: *f.ParentID})
		}
	}
	// 第二趟：父子关系补齐。按名称合并过的上级会走 mapped 映射，指向真正落库的那一条。
	for _, p := range todo {
		if parent, ok := mapped[p.parent]; ok {
			if _, err := tx.Exec(`UPDATE folders SET parent_id = ? WHERE id = ?`, parent, p.id); err != nil {
				return nil, err
			}
		}
	}
	return mapped, nil
}

func mergeTags(tx txer, tags []model.Tag, res *ImportResult) (map[int64]int64, error) {
	existing := map[string]int64{}
	rows, err := tx.Query(`SELECT id, name FROM tags`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		existing[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mapped := map[int64]int64{}
	for _, g := range tags {
		if id, ok := existing[g.Name]; ok {
			mapped[g.ID] = id
			continue
		}
		r, err := tx.Exec(`INSERT INTO tags(name, color, created_at) VALUES(?,?,?)`, g.Name, g.Color, stamp(g.CreatedAt))
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		mapped[g.ID] = id
		existing[g.Name] = id
		res.Tags++
	}
	return mapped, nil
}

// insertAttachments 把任务上的附件记录重新登记到库里。
//
// files 里查不到的存储名，说明文件这次没随备份带来（裸 JSON 导入常见），
// 那就只记一笔缺失，不建指向空文件的记录 —— 宁可少一条附件，
// 也不要在详情里摆一个点开就 410 的死链。
func insertAttachments(tx txer, t model.Task, taskID int64, files map[string]string, res *ImportResult) error {
	for i := range t.Attachments {
		a := t.Attachments[i]
		stored, ok := files[a.File]
		if !ok {
			continue // 缺失数已由 stageAttachments 统计，这里不重复计
		}
		// 双保险：files 的值也过一遍，恶意存储名进不了库。
		clean, valid := safeStoredName(stored)
		if !valid {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO attachments(task_id, name, file, size, mime, created_at) VALUES(?,?,?,?,?,?)`,
			taskID, safeDisplayName(a.Name), clean, a.Size, a.Mime, stamp(a.CreatedAt),
		); err != nil {
			return err
		}
		res.Attachments++
	}
	return nil
}

// insertTaskWithID 按原 id 精确复原一条任务（replace 模式）。
func insertTaskWithID(tx txer, t model.Task) error {
	if _, err := tx.Exec(`INSERT INTO tasks(id, list_id, title, notes, status, priority, start_date, due_date, due_time, end_time, url, reminders, repeat_rule, repeat_from, important, urgent, pinned, starred, archived, estimate_minutes, progress, completed_at, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ListID, t.Title, t.Notes, statusOr(t.Status), t.Priority,
		t.StartDate, t.DueDate, t.DueTime, t.EndTime, t.URL,
		mustJSON(nonNil(t.Reminders)), t.RepeatRule, normalizeRepeatFrom(t.RepeatFrom),
		boolInt(t.Important), boolInt(t.Urgent), boolInt(t.Pinned), boolInt(t.Starred), boolInt(t.Archived),
		t.EstimateMinutes, t.Progress, t.CompletedAt, t.SortOrder,
		stamp(t.CreatedAt), stamp(t.UpdatedAt),
	); err != nil {
		return err
	}
	if err := insertSubtasks(tx, t, t.ID); err != nil {
		return err
	}
	for _, g := range t.Tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_tags(task_id, tag_id) VALUES(?,?)`, t.ID, g.ID); err != nil {
			return err
		}
	}
	return nil
}

// insertTaskCopy 追加一条任务副本，id 由数据库重新分配（merge 模式 / 撤销恢复复用）。
func insertTaskCopy(tx txer, t model.Task, listID int64) (int64, error) {
	r, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, start_date, due_date, due_time, end_time, url, reminders, repeat_rule, repeat_from, important, urgent, pinned, starred, archived, estimate_minutes, progress, completed_at, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		listID, t.Title, t.Notes, statusOr(t.Status), t.Priority,
		t.StartDate, t.DueDate, t.DueTime, t.EndTime, t.URL,
		mustJSON(nonNil(t.Reminders)), t.RepeatRule, normalizeRepeatFrom(t.RepeatFrom),
		boolInt(t.Important), boolInt(t.Urgent), boolInt(t.Pinned), boolInt(t.Starred), boolInt(t.Archived),
		t.EstimateMinutes, t.Progress, t.CompletedAt, t.SortOrder,
		stamp(t.CreatedAt), stamp(t.UpdatedAt),
	)
	if err != nil {
		return 0, err
	}
	id, _ := r.LastInsertId()
	// 标签由调用方按 id 映射写入，这里只处理子任务。
	return id, insertSubtasks(tx, t, id)
}

func insertSubtasks(tx txer, t model.Task, taskID int64) error {
	// 递归插入：先父后子，用新分配的 id 接续 parent_id，树的形状原样保留。
	var insert func(subs []model.Subtask, parent *int64) error
	insert = func(subs []model.Subtask, parent *int64) error {
		for i, s := range subs {
			order := s.SortOrder
			if order == 0 {
				order = (i + 1) * 1024
			}
			r, err := tx.Exec(
				`INSERT INTO subtasks(task_id, parent_id, title, due_date, reminders, done, sort_order) VALUES(?,?,?,?,?,?,?)`,
				taskID, parent, s.Title, s.DueDate, mustJSON(nonNil(s.Reminders)), boolInt(s.Done), order,
			)
			if err != nil {
				return err
			}
			if len(s.Children) > 0 {
				id, _ := r.LastInsertId()
				if err := insert(s.Children, &id); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return insert(t.Subtasks, nil)
}

// ensureInbox 保证系统里有且仅有一个收件箱。
func ensureInbox(tx txer) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE is_inbox = 1`).Scan(&n); err != nil {
		return err
	}
	switch {
	case n == 0:
		var first int64
		err := tx.QueryRow(`SELECT id FROM lists ORDER BY id LIMIT 1`).Scan(&first)
		if err != nil {
			// 一个清单都没有：补一个空的收集箱，否则无处新建任务。
			_, err = tx.Exec(
				`INSERT INTO lists(name, color, icon, sort_order, is_inbox, created_at) VALUES('收集箱','#b4553d','inbox',0,1,?)`,
				model.Now(),
			)
			return err
		}
		_, err = tx.Exec(`UPDATE lists SET is_inbox = 1 WHERE id = ?`, first)
		return err
	case n > 1:
		var keep int64
		if err := tx.QueryRow(`SELECT id FROM lists WHERE is_inbox = 1 ORDER BY id LIMIT 1`).Scan(&keep); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE lists SET is_inbox = 0 WHERE is_inbox = 1 AND id <> ?`, keep)
		return err
	}
	return nil
}

func firstInboxID(tx txer) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM lists WHERE is_inbox = 1 ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

func statusOr(s string) string {
	switch s {
	case model.StatusDone:
		return model.StatusDone
	case model.StatusInProgress:
		return model.StatusInProgress
	}
	return model.StatusTodo
}

// restoreTaskLinks 回放任务间关联边。mapID 把备份里的任务 id 换算成目标库的 id，
// 返回 false 表示该任务没进来（清单缺失等），相关的边随之丢弃。
func restoreTaskLinks(tx txer, links []model.TaskLink, mapID func(int64) (int64, bool), res *ImportResult) error {
	for _, l := range links {
		a, okA := mapID(l.TaskID)
		b, okB := mapID(l.LinkedTaskID)
		if !okA || !okB || a == b {
			continue
		}
		if l.Kind != model.LinkRelated && l.Kind != model.LinkBlocked {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_links(task_id, linked_task_id, kind, created_at) VALUES(?,?,?,?)`,
			a, b, l.Kind, model.Now()); err != nil {
			return err
		}
	}
	return nil
}

// stamp 补全缺失的时间戳，避免导入的空值污染排序与展示。
func stamp(s string) string {
	if strings.TrimSpace(s) == "" {
		return model.Now()
	}
	return s
}

func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
