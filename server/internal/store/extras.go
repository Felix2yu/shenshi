// 本文件承载「慎始」的三组扩展能力：任务附件、出站 Webhook、模板任务，
// 以及给 CalDAV 增量同步用的变更日志。它们都围绕任务展开，但各自独立成体系。
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// maxAttachmentSize 单个附件的上限。再大的文件不该进任务详情，挂个网盘链接更合适。
const maxAttachmentSize = 32 << 20

// ---------- 附件 ----------

// ListAttachments 返回一个任务上的全部附件，按创建先后排列。
func (s *Store) ListAttachments(taskID int64) ([]model.Attachment, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, name, file, size, mime, created_at FROM attachments WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Attachment{}
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Name, &a.File, &a.Size, &a.Mime, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAttachment 落盘一个附件并登记元数据。
// 文件名用随机串而非原名，避免同名覆盖与路径穿越；原名只用于展示。
func (s *Store) SaveAttachment(taskID int64, name, mime string, r io.Reader) (*model.Attachment, error) {
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM tasks WHERE id = ?`, taskID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var salt [8]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, fmt.Errorf("生成随机文件名失败: %w", err)
	}
	stored := fmt.Sprintf("%d-%s%s", taskID, hex.EncodeToString(salt[:]), safeExt(name))
	dst := filepath.Join(s.attachmentDir, stored)

	tmp, err := os.CreateTemp(s.attachmentDir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	// 多读一个字节，用来判断是否超限——否则超限时会被静默截断。
	n, err := io.Copy(tmp, io.LimitReader(r, maxAttachmentSize+1))
	if err != nil {
		tmp.Close()
		return nil, fmt.Errorf("写入附件失败: %w", err)
	}
	if n > maxAttachmentSize {
		tmp.Close()
		return nil, ValidationError{Msg: "附件超过 32MB 上限"}
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return nil, fmt.Errorf("保存附件失败: %w", err)
	}

	ts := model.Now()
	res, err := s.db.Exec(
		`INSERT INTO attachments(task_id, name, file, size, mime, created_at) VALUES(?,?,?,?,?,?)`,
		taskID, safeDisplayName(name), stored, n, mime, ts)
	if err != nil {
		_ = os.Remove(dst)
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Attachment{
		ID: id, TaskID: taskID, Name: safeDisplayName(name), File: stored,
		Size: n, Mime: mime, CreatedAt: ts,
	}, nil
}

// GetAttachment 读取一条附件记录。
func (s *Store) GetAttachment(id int64) (*model.Attachment, error) {
	var a model.Attachment
	err := s.db.QueryRow(
		`SELECT id, task_id, name, file, size, mime, created_at FROM attachments WHERE id = ?`, id,
	).Scan(&a.ID, &a.TaskID, &a.Name, &a.File, &a.Size, &a.Mime, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AttachmentPath 返回附件在磁盘上的绝对路径。
func (s *Store) AttachmentPath(a *model.Attachment) string {
	return filepath.Join(s.attachmentDir, a.File)
}

// DeleteAttachment 删除一条附件，连同磁盘文件。
func (s *Store) DeleteAttachment(id int64) error {
	a, err := s.GetAttachment(id)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM attachments WHERE id = ?`, id); err != nil {
		return err
	}
	// 记录已删，文件删不掉只是留了点垃圾，不影响一致性。
	_ = os.Remove(s.AttachmentPath(a))
	return nil
}

// deleteTaskAttachments 清空一个任务上的全部附件（删除任务前调用）。
func (s *Store) deleteTaskAttachments(taskID int64) error {
	return s.dropTaskAttachments(taskID, true)
}

// dropTaskAttachments 清掉任务名下的附件记录。
//
// removeFiles 为 false 时只删库里的行、把文件留在磁盘上 —— 这是「可撤销删除」需要的行为：
// 撤销时记录会按原存储名挂回来，文件还在就还能打开；直到撤销槽位被顶替才真正删文件。
func (s *Store) dropTaskAttachments(taskID int64, removeFiles bool) error {
	list, err := s.ListAttachments(taskID)
	if err != nil {
		return err
	}
	if removeFiles {
		for i := range list {
			_ = os.Remove(s.AttachmentPath(&list[i]))
		}
	}
	_, err = s.db.Exec(`DELETE FROM attachments WHERE task_id = ?`, taskID)
	return err
}

// safeDisplayName 只保留文件名部分，并压掉控制字符与路径分隔符。
func safeDisplayName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0, '\n', '\r', '\t':
			return -1
		}
		if r < 32 {
			return -1
		}
		return r
	}, base)
	if base == "" || base == "." || base == ".." {
		return "附件"
	}
	if len([]rune(base)) > 120 {
		return string([]rune(base)[:120])
	}
	return base
}

// safeExt 取出扩展名并做白名单收敛：只保留常见的安全后缀，其余一律不带，
// 避免把 .exe / .html 之类存上去后被当成可执行内容打开。
func safeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".bmp", ".svg",
		".pdf", ".txt", ".md", ".csv", ".json", ".zip", ".log",
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".mp3", ".m4a", ".wav", ".mp4", ".mov":
		return ext
	default:
		return ".bin"
	}
}

// attachAttachments 给一批任务装配附件列表。
func (s *Store) attachAttachments(tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	idx := make(map[int64]int, len(tasks))
	ids := make([]any, 0, len(tasks))
	for i, t := range tasks {
		idx[t.ID] = i
		ids = append(ids, t.ID)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	rows, err := s.db.Query(
		`SELECT id, task_id, name, file, size, mime, created_at FROM attachments
		 WHERE task_id IN (`+placeholders+`) ORDER BY id`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a model.Attachment
		if err := rows.Scan(&a.ID, &a.TaskID, &a.Name, &a.File, &a.Size, &a.Mime, &a.CreatedAt); err != nil {
			return err
		}
		if i, ok := idx[a.TaskID]; ok {
			tasks[i].Attachments = append(tasks[i].Attachments, a)
		}
	}
	return rows.Err()
}

// ---------- Webhook ----------

// ListWebhooks 返回全部 Webhook，密钥一律脱敏。
func (s *Store) ListWebhooks() ([]model.Webhook, error) {
	rows, err := s.db.Query(
		`SELECT id, name, url, secret, events, enabled, created_at, updated_at FROM webhooks ORDER BY id`)
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
		redactWebhook(&w)
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWebhook 读取一条 Webhook（含真实密钥，供投递签名使用）。
func (s *Store) GetWebhook(id int64) (*model.Webhook, error) {
	row := s.db.QueryRow(
		`SELECT id, name, url, secret, events, enabled, created_at, updated_at FROM webhooks WHERE id = ?`, id)
	w, err := scanWebhook(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// WebhooksForEvent 返回订阅了某事件且已启用的 Webhook（含真实密钥）。
func (s *Store) WebhooksForEvent(event string) ([]model.Webhook, error) {
	rows, err := s.db.Query(
		`SELECT id, name, url, secret, events, enabled, created_at, updated_at FROM webhooks WHERE enabled = 1 ORDER BY id`)
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
		if !w.Enabled {
			continue
		}
		for _, e := range w.Events {
			if e == event {
				out = append(out, w)
				break
			}
		}
	}
	return out, rows.Err()
}

type webhookScanner interface {
	Scan(dest ...any) error
}

func scanWebhook(row webhookScanner) (model.Webhook, error) {
	var w model.Webhook
	var events string
	var enabled int
	if err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &events, &enabled, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return w, err
	}
	w.Enabled = enabled != 0
	w.Events = jsonStrings(events)
	return w, nil
}

// redactWebhook 抹掉密钥，只以 HasSecret 示意是否配过。
func redactWebhook(w *model.Webhook) {
	w.HasSecret = w.Secret != ""
	w.Secret = ""
}

// CreateWebhook 新增一个 Webhook。URL 必须是 http(s)，事件名必须已知。
func (s *Store) CreateWebhook(in model.WebhookInput) (*model.Webhook, error) {
	url := strings.TrimSpace(deref(in.URL, ""))
	if url == "" {
		return nil, ValidationError{Msg: "回调地址不能为空"}
	}
	if !isHTTPURL(url) {
		return nil, ValidationError{Msg: "回调地址必须是 http:// 或 https:// 开头"}
	}
	events := normalizeEvents(deref(in.Events, nil))
	if len(events) == 0 {
		events = []string{model.EventTaskCreated, model.EventTaskCompleted}
	}
	enabled := 1
	if in.Enabled.Set && !in.Enabled.Value {
		enabled = 0
	}
	ts := model.Now()
	res, err := s.db.Exec(
		`INSERT INTO webhooks(name, url, secret, events, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
		strings.TrimSpace(deref(in.Name, "")), url, deref(in.Secret, ""), mustJSON(events), enabled, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	w, err := s.GetWebhook(id)
	if err != nil {
		return nil, err
	}
	redactWebhook(w)
	return w, nil
}

// UpdateWebhook 按 PATCH 语义更新 Webhook。
func (s *Store) UpdateWebhook(id int64, in model.WebhookInput) (*model.Webhook, error) {
	cur, err := s.GetWebhook(id)
	if err != nil {
		return nil, err
	}
	sets := []string{}
	args := []any{}
	if in.Name.Set {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(in.Name.Value))
	}
	if in.URL.Set {
		u := strings.TrimSpace(in.URL.Value)
		if u == "" {
			return nil, ValidationError{Msg: "回调地址不能为空"}
		}
		if !isHTTPURL(u) {
			return nil, ValidationError{Msg: "回调地址必须是 http:// 或 https:// 开头"}
		}
		sets = append(sets, "url = ?")
		args = append(args, u)
	}
	if in.Secret.Set {
		sets = append(sets, "secret = ?")
		args = append(args, in.Secret.Value)
	}
	if in.Events.Set {
		ev := normalizeEvents(in.Events.Value)
		if len(ev) == 0 {
			return nil, ValidationError{Msg: "至少要订阅一个事件"}
		}
		sets = append(sets, "events = ?")
		args = append(args, mustJSON(ev))
	}
	if in.Enabled.Set {
		sets = append(sets, "enabled = ?")
		args = append(args, boolInt(in.Enabled.Value))
	}
	if len(sets) == 0 {
		redactWebhook(cur)
		return cur, nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, model.Now(), id)
	if _, err := s.db.Exec(`UPDATE webhooks SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return nil, err
	}
	w, err := s.GetWebhook(id)
	if err != nil {
		return nil, err
	}
	redactWebhook(w)
	return w, nil
}

// DeleteWebhook 删除一个 Webhook。
func (s *Store) DeleteWebhook(id int64) error {
	res, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordDelivery 记一次投递结果，并顺带裁掉多余的历史，避免台账无限增长。
func (s *Store) RecordDelivery(webhookID int64, event string, code int, ok bool, errMsg string) error {
	if _, err := s.db.Exec(
		`INSERT INTO webhook_deliveries(webhook_id, event, code, ok, error, created_at) VALUES(?,?,?,?,?,?)`,
		webhookID, event, code, boolInt(ok), truncate(errMsg, 300), model.Now()); err != nil {
		return err
	}
	// 每个 Webhook 只留最近 20 条：够排查问题，又不至于让台账成为负担。
	_, err := s.db.Exec(`DELETE FROM webhook_deliveries WHERE webhook_id = ? AND id NOT IN (
		SELECT id FROM webhook_deliveries WHERE webhook_id = ? ORDER BY id DESC LIMIT 20)`, webhookID, webhookID)
	return err
}

// RecentDeliveries 返回某 Webhook 最近的投递记录。
func (s *Store) RecentDeliveries(webhookID int64, limit int) ([]model.WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, webhook_id, event, code, ok, error, created_at FROM webhook_deliveries
		 WHERE webhook_id = ? ORDER BY id DESC LIMIT ?`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WebhookDelivery{}
	for rows.Next() {
		var d model.WebhookDelivery
		var ok int
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &d.Code, &ok, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.OK = ok != 0
		out = append(out, d)
	}
	return out, rows.Err()
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// normalizeEvents 过滤掉未知事件名，保留顺序并去重。
func normalizeEvents(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range in {
		e = strings.TrimSpace(e)
		if e == "" || seen[e] {
			continue
		}
		for _, known := range model.WebhookEvents {
			if e == known {
				seen[e] = true
				out = append(out, e)
				break
			}
		}
	}
	return out
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// ---------- 模板任务 ----------

// ListTemplates 返回全部模板，按排序位次排列。
func (s *Store) ListTemplates() ([]model.TaskTemplate, error) {
	rows, err := s.db.Query(
		`SELECT id, name, title, notes, list_id, priority, due_offset, due_time, reminders, repeat_rule,
		        important, urgent, tag_ids, subtasks, sort_order, created_at, updated_at
		 FROM task_templates ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TaskTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTemplate 读取一个模板。
func (s *Store) GetTemplate(id int64) (*model.TaskTemplate, error) {
	row := s.db.QueryRow(
		`SELECT id, name, title, notes, list_id, priority, due_offset, due_time, reminders, repeat_rule,
		        important, urgent, tag_ids, subtasks, sort_order, created_at, updated_at
		 FROM task_templates WHERE id = ?`, id)
	t, err := scanTemplate(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTemplate(row webhookScanner) (model.TaskTemplate, error) {
	var t model.TaskTemplate
	var reminders, tagIDs, subtasks string
	var important, urgent int
	err := row.Scan(&t.ID, &t.Name, &t.Title, &t.Notes, &t.ListID, &t.Priority, &t.DueOffset, &t.DueTime,
		&reminders, &t.RepeatRule, &important, &urgent, &tagIDs, &subtasks, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	t.Important = important != 0
	t.Urgent = urgent != 0
	t.Reminders = jsonInts(reminders)
	t.TagIDs = jsonInt64s(tagIDs)
	t.Subtasks = jsonStrings(subtasks)
	return t, nil
}

// CreateTemplate 新增模板。名称与标题必填。
func (s *Store) CreateTemplate(in model.TemplateInput) (*model.TaskTemplate, error) {
	name := strings.TrimSpace(deref(in.Name, ""))
	title := strings.TrimSpace(deref(in.Title, ""))
	if name == "" {
		return nil, ValidationError{Msg: "模板名称不能为空"}
	}
	if title == "" {
		return nil, ValidationError{Msg: "任务标题不能为空"}
	}
	sortOrder := deref(in.SortOrder, 0)
	if !in.SortOrder.Set {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1024 FROM task_templates`).Scan(&sortOrder); err != nil {
			return nil, err
		}
	}
	ts := model.Now()
	res, err := s.db.Exec(
		`INSERT INTO task_templates(name, title, notes, list_id, priority, due_offset, due_time, reminders,
			repeat_rule, important, urgent, tag_ids, subtasks, sort_order, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		name, title, deref(in.Notes, ""), nullableInt64(derefPtr(in.ListID, nil)), deref(in.Priority, 0),
		derefPtr(in.DueOffset, nil), derefPtr(in.DueTime, nil), mustJSON(nonNilInts(deref(in.Reminders, nil))),
		derefPtr(in.RepeatRule, nil), boolInt(deref(in.Important, false)), boolInt(deref(in.Urgent, false)),
		mustJSON(nonNilInt64s(deref(in.TagIDs, nil))), mustJSON(nonNilStrings(deref(in.Subtasks, nil))),
		sortOrder, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetTemplate(id)
}

// UpdateTemplate 按 PATCH 语义更新模板。
func (s *Store) UpdateTemplate(id int64, in model.TemplateInput) (*model.TaskTemplate, error) {
	if _, err := s.GetTemplate(id); err != nil {
		return nil, err
	}
	sets := []string{}
	args := []any{}
	add := func(expr string, v ...any) {
		sets = append(sets, expr)
		args = append(args, v...)
	}
	if in.Name.Set {
		if strings.TrimSpace(in.Name.Value) == "" {
			return nil, ValidationError{Msg: "模板名称不能为空"}
		}
		add("name = ?", strings.TrimSpace(in.Name.Value))
	}
	if in.Title.Set {
		if strings.TrimSpace(in.Title.Value) == "" {
			return nil, ValidationError{Msg: "任务标题不能为空"}
		}
		add("title = ?", strings.TrimSpace(in.Title.Value))
	}
	if in.Notes.Set {
		add("notes = ?", in.Notes.Value)
	}
	if in.ListID.Set {
		add("list_id = ?", nullableInt64(in.ListID.Value))
	}
	if in.Priority.Set {
		add("priority = ?", in.Priority.Value)
	}
	if in.DueOffset.Set {
		add("due_offset = ?", nullableInt(in.DueOffset.Value))
	}
	if in.DueTime.Set {
		add("due_time = ?", nullableStrPtr(in.DueTime.Value))
	}
	if in.Reminders.Set {
		add("reminders = ?", mustJSON(nonNilInts(in.Reminders.Value)))
	}
	if in.RepeatRule.Set {
		add("repeat_rule = ?", nullableStrPtr(in.RepeatRule.Value))
	}
	if in.Important.Set {
		add("important = ?", boolInt(in.Important.Value))
	}
	if in.Urgent.Set {
		add("urgent = ?", boolInt(in.Urgent.Value))
	}
	if in.TagIDs.Set {
		add("tag_ids = ?", mustJSON(nonNilInt64s(in.TagIDs.Value)))
	}
	if in.Subtasks.Set {
		add("subtasks = ?", mustJSON(nonNilStrings(in.Subtasks.Value)))
	}
	if in.SortOrder.Set {
		add("sort_order = ?", in.SortOrder.Value)
	}
	if len(sets) == 0 {
		return s.GetTemplate(id)
	}
	add("updated_at = ?", model.Now())
	args = append(args, id)
	if _, err := s.db.Exec(`UPDATE task_templates SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return nil, err
	}
	return s.GetTemplate(id)
}

// DeleteTemplate 删除模板。
func (s *Store) DeleteTemplate(id int64) error {
	res, err := s.db.Exec(`DELETE FROM task_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// InstantiateTemplate 按模板生成一条真实任务。
// listID / dueDate 可覆盖模板里的默认值：不传即按模板（dueOffset 相对今天推算）。
func (s *Store) InstantiateTemplate(id int64, listID *int64, dueDate *string) (*model.Task, error) {
	t, err := s.GetTemplate(id)
	if err != nil {
		return nil, err
	}
	in := model.TaskInput{
		Title:      optOf(t.Title),
		Notes:      optOf(t.Notes),
		Priority:   optOf(t.Priority),
		DueTime:    optOfPtr(t.DueTime),
		Reminders:  optOf(t.Reminders),
		RepeatRule: optOfPtr(t.RepeatRule),
		Important:  optOf(t.Important),
		Urgent:     optOf(t.Urgent),
		TagIDs:     optOf(t.TagIDs),
	}
	subs := make([]model.Subtask, 0, len(t.Subtasks))
	for i, s := range t.Subtasks {
		if strings.TrimSpace(s) == "" {
			continue
		}
		subs = append(subs, model.Subtask{Title: strings.TrimSpace(s), Done: false, SortOrder: (i + 1) * 1024})
	}
	if len(subs) > 0 {
		in.Subtasks = optOf(subs)
	}

	// 日期：显式传入优先，否则按模板的偏移相对今天推算。
	var due *string
	if dueDate != nil {
		if strings.TrimSpace(*dueDate) == "" {
			due = nil
		} else {
			v := strings.TrimSpace(*dueDate)
			due = &v
		}
	} else if t.DueOffset != nil {
		d := time.Now().AddDate(0, 0, *t.DueOffset).Format("2006-01-02")
		due = &d
	}
	if due != nil {
		in.DueDate = optOfPtr(due)
		in.Urgent = optOf(true) // 带了日期就算紧急，与新建任务的口径一致
	}

	target := int64(0)
	if listID != nil && *listID > 0 {
		target = *listID
	} else if t.ListID != nil {
		target = *t.ListID
	} else {
		target, err = s.InboxListID()
		if err != nil {
			return nil, err
		}
	}
	in.ListID = optOf(target)
	return s.CreateTask(in, target)
}

// ---------- CalDAV 变更日志 ----------

// NoteCalDAVChange 记一行变更。collection 为 events（日历）或 todos（提醒事项）。
func (s *Store) NoteCalDAVChange(collection, uid string, taskID int64, deleted bool) error {
	_, err := s.db.Exec(
		`INSERT INTO caldav_changes(collection, uid, task_id, deleted, changed_at) VALUES(?,?,?,?,?)`,
		collection, uid, nullableTaskID(taskID), boolInt(deleted), model.Now())
	return err
}

// CalDAVChangesSince 返回序号大于 seq 的变更，以及当前最大序号。
// limit 为 0 或负表示不限。
func (s *Store) CalDAVChangesSince(collection string, seq int64, limit int) ([]model.CalDAVChange, int64, error) {
	maxSeq, err := s.CalDAVMaxSeq(collection)
	if err != nil {
		return nil, 0, err
	}
	q := `SELECT seq, collection, uid, task_id, deleted, changed_at FROM caldav_changes
	      WHERE collection = ? AND seq > ? ORDER BY seq`
	args := []any{collection, seq}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.CalDAVChange{}
	for rows.Next() {
		var c model.CalDAVChange
		var deleted int
		if err := rows.Scan(&c.Seq, &c.Collection, &c.UID, &c.TaskID, &deleted, &c.ChangedAt); err != nil {
			return nil, 0, err
		}
		c.Deleted = deleted != 0
		out = append(out, c)
	}
	return out, maxSeq, rows.Err()
}

// CalDAVMaxSeq 返回某集合当前的变更序号上界。
func (s *Store) CalDAVMaxSeq(collection string) (int64, error) {
	var n sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM caldav_changes WHERE collection = ?`, collection).Scan(&n); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

func nullableTaskID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func nullableInt64(p *int64) any {
	if p == nil || *p <= 0 {
		return nil
	}
	return *p
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableStrPtr(p *string) any {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	return strings.TrimSpace(*p)
}

// ---------- JSON 小工具 ----------

func jsonStrings(s string) []string {
	out := []string{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func jsonInts(s string) []int {
	out := []int{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func jsonInt64s(s string) []int64 {
	out := []int64{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func nonNilInts(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}

func nonNilInt64s(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
