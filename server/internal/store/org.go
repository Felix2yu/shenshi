package store

import (
	"strings"

	"github.com/yufei/shendu/server/internal/model"
)

// ---------- 清单分组 ----------

// FolderInput 分组入参。
type FolderInput struct {
	Name      *string `json:"name"`
	Color     *string `json:"color"`
	Icon      *string `json:"icon"`
	SortOrder *int    `json:"sortOrder"`
	Collapsed *bool   `json:"collapsed"`
}

// ListInput 清单入参。
type ListInput struct {
	Name      *string `json:"name"`
	Color     *string `json:"color"`
	Icon      *string `json:"icon"`
	FolderID  *int64  `json:"folderId"`
	SortOrder *int    `json:"sortOrder"`
	// MoveToRoot 为 true 时把清单移出分组。
	MoveToRoot bool `json:"moveToRoot"`
}

// Folders 返回分组树，分组内嵌套其清单。
func (s *Store) Folders() ([]model.Folder, error) {
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, name, color, icon, sort_order, collapsed, created_at FROM folders ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	folders := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		var collapsed int
		if err := rows.Scan(&f.ID, &f.Name, &f.Color, &f.Icon, &f.SortOrder, &collapsed, &f.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		f.Collapsed = collapsed == 1
		f.Lists = []model.List{}
		folders = append(folders, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	for i := range folders {
		for _, l := range lists {
			if l.FolderID != nil && *l.FolderID == folders[i].ID {
				folders[i].Lists = append(folders[i].Lists, l)
			}
		}
	}
	return folders, nil
}

// Lists 返回全部清单（含收集箱），附带未完成任务数。
func (s *Store) Lists() ([]model.List, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.folder_id, l.name, l.color, l.icon, l.sort_order, l.is_inbox, l.created_at,
		       (SELECT COUNT(*) FROM tasks t WHERE t.list_id = l.id AND t.status = 'todo')
		FROM lists l ORDER BY l.is_inbox DESC, l.sort_order, l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.List{}
	for rows.Next() {
		var l model.List
		var fid *int64
		var inbox int
		if err := rows.Scan(&l.ID, &fid, &l.Name, &l.Color, &l.Icon, &l.SortOrder, &inbox, &l.CreatedAt, &l.TaskCount); err != nil {
			return nil, err
		}
		l.FolderID = fid
		out = append(out, l)
	}
	return out, rows.Err()
}

// InboxListID 返回收集箱清单 ID。
func (s *Store) InboxListID() (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM lists WHERE is_inbox = 1 ORDER BY id LIMIT 1`).Scan(&id)
	if err != nil {
		// 兜底：若收集箱被删除，退化为最早创建的清单。
		if err2 := s.db.QueryRow(`SELECT id FROM lists ORDER BY id LIMIT 1`).Scan(&id); err2 != nil {
			return 0, err2
		}
	}
	return id, nil
}

// CreateFolder 新建分组。
func (s *Store) CreateFolder(in FolderInput) (*model.Folder, error) {
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("分组名称")
	}
	order := 0
	if in.SortOrder != nil {
		order = *in.SortOrder
	} else {
		var max *int
		_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM folders`).Scan(&max)
		if max != nil {
			order = *max + 1
		}
	}
	res, err := s.db.Exec(`INSERT INTO folders(name, color, icon, sort_order, collapsed, created_at) VALUES(?,?,?,?,0,?)`,
		name, derefStr(in.Color, "#8a7c66"), derefStr(in.Icon, "folder"), order, model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Folder{ID: id, Name: name, Color: derefStr(in.Color, "#8a7c66"), Icon: derefStr(in.Icon, "folder"), SortOrder: order, Lists: []model.List{}}, nil
}

// UpdateFolder 更新分组。
func (s *Store) UpdateFolder(id int64, in FolderInput) error {
	sets, args := []string{}, []any{}
	if in.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*in.Name))
	}
	if in.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *in.Color)
	}
	if in.Icon != nil {
		sets = append(sets, "icon = ?")
		args = append(args, *in.Icon)
	}
	if in.SortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *in.SortOrder)
	}
	if in.Collapsed != nil {
		sets = append(sets, "collapsed = ?")
		args = append(args, boolInt(*in.Collapsed))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE folders SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// DeleteFolder 删除分组。分组内的清单会回到顶层，不会被连带删除。
func (s *Store) DeleteFolder(id int64) error {
	if _, err := s.db.Exec(`UPDATE lists SET folder_id = NULL WHERE folder_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM folders WHERE id = ?`, id)
	return err
}

// CreateList 新建清单。
func (s *Store) CreateList(in ListInput) (*model.List, error) {
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("清单名称")
	}
	order := 0
	if in.SortOrder != nil {
		order = *in.SortOrder
	} else {
		var max *int
		_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM lists WHERE is_inbox = 0`).Scan(&max)
		if max != nil {
			order = *max + 1
		}
	}
	res, err := s.db.Exec(`INSERT INTO lists(folder_id, name, color, icon, sort_order, is_inbox, created_at) VALUES(?,?,?,?,?,0,?)`,
		in.FolderID, name, derefStr(in.Color, "#b4553d"), derefStr(in.Icon, "list"), order, model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.List{ID: id, FolderID: in.FolderID, Name: name, Color: derefStr(in.Color, "#b4553d"), Icon: derefStr(in.Icon, "list"), SortOrder: order}, nil
}

// UpdateList 更新清单。
func (s *Store) UpdateList(id int64, in ListInput) error {
	sets, args := []string{}, []any{}
	if in.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*in.Name))
	}
	if in.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *in.Color)
	}
	if in.Icon != nil {
		sets = append(sets, "icon = ?")
		args = append(args, *in.Icon)
	}
	if in.MoveToRoot {
		sets = append(sets, "folder_id = NULL")
	} else if in.FolderID != nil {
		sets = append(sets, "folder_id = ?")
		args = append(args, *in.FolderID)
	}
	if in.SortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *in.SortOrder)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE lists SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// DeleteList 删除清单；收集箱不可删除。清单内的任务会一并删除（外键级联）。
func (s *Store) DeleteList(id int64) error {
	var inbox int
	if err := s.db.QueryRow(`SELECT is_inbox FROM lists WHERE id = ?`, id).Scan(&inbox); err != nil {
		return ErrNotFound
	}
	if inbox == 1 {
		return errForbidden("收集箱不可删除")
	}
	_, err := s.db.Exec(`DELETE FROM lists WHERE id = ?`, id)
	return err
}

// ---------- 标签 ----------

// TagInput 标签入参。
type TagInput struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

// Tags 返回全部标签及未完成任务数。
func (s *Store) Tags() ([]model.Tag, error) {
	rows, err := s.db.Query(`
		SELECT g.id, g.name, g.color, g.created_at,
		       (SELECT COUNT(*) FROM task_tags tt JOIN tasks t ON t.id = tt.task_id
		         WHERE tt.tag_id = g.id AND t.status = 'todo')
		FROM tags g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Tag{}
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt, &t.TaskCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTag 新建标签（同名返回既有标签）。
func (s *Store) CreateTag(in TagInput) (*model.Tag, error) {
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("标签名称")
	}
	return s.ensureTag(name, derefStr(in.Color, ""))
}

func (s *Store) ensureTag(name, color string) (*model.Tag, error) {
	var existing model.Tag
	err := s.db.QueryRow(`SELECT id, name, color, created_at FROM tags WHERE name = ?`, name).
		Scan(&existing.ID, &existing.Name, &existing.Color, &existing.CreatedAt)
	if err == nil {
		return &existing, nil
	}
	if color == "" {
		palette := []string{"#b4553d", "#6b7f6e", "#8a6d3b", "#5c6b8a", "#7a5c7a", "#3f7a7a"}
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&n)
		color = palette[n%len(palette)]
	}
	res, err := s.db.Exec(`INSERT INTO tags(name, color, created_at) VALUES(?,?,?)`, name, color, model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Tag{ID: id, Name: name, Color: color}, nil
}

// EnsureTags 批量保证标签存在，返回标签 ID 列表。用于从标题中的 #标签 自动建档。
func (s *Store) EnsureTags(names []string) ([]int64, error) {
	ids := []int64{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		tag, err := s.ensureTag(n, "")
		if err != nil {
			return nil, err
		}
		ids = append(ids, tag.ID)
	}
	return ids, nil
}

// UpdateTag 更新标签。
func (s *Store) UpdateTag(id int64, in TagInput) error {
	sets, args := []string{}, []any{}
	if in.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*in.Name))
	}
	if in.Color != nil {
		sets = append(sets, "color = ?")
		args = append(args, *in.Color)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE tags SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// DeleteTag 删除标签。
func (s *Store) DeleteTag(id int64) error {
	_, err := s.db.Exec(`DELETE FROM tags WHERE id = ?`, id)
	return err
}

// ReorderFolders 按传入顺序重写分组排序。
func (s *Store) ReorderFolders(ids []int64) error { return s.reorderBy("folders", ids) }

// ReorderLists 按传入顺序重写清单排序。
func (s *Store) ReorderLists(ids []int64) error { return s.reorderBy("lists", ids) }

// reorderBy 是分组与清单共用的排序落库逻辑。表名只接受白名单取值，
// 因为 SQLite 不支持把表名作为绑定参数，只能拼接。
func (s *Store) reorderBy(table string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if table != "folders" && table != "lists" {
		return ValidationError{Msg: "未知的排序目标"}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`UPDATE ` + table + ` SET sort_order = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, id := range ids {
		if _, err := stmt.Exec((i+1)*1024, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- 设置 ----------

// Settings 返回全部设置项。
func (s *Store) Settings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SaveSettings 覆盖写入设置项。
func (s *Store) SaveSettings(kv map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, v := range kv {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- 小工具 ----------

func derefStr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

// ValidationError 表示入参不合法（HTTP 400）。
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// ForbiddenError 表示不允许的操作（HTTP 403）。
type ForbiddenError struct{ Msg string }

func (e ForbiddenError) Error() string { return e.Msg }

func errBlank(field string) error { return ValidationError{Msg: field + "不能为空"} }

func errForbidden(msg string) error { return ForbiddenError{Msg: msg} }
