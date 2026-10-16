package store

import (
	"database/sql"
	"errors"
	"fmt"
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
	Archived  *bool   `json:"archived"`
	ParentID  *int64  `json:"parentId"`
	// MoveToRoot 为 true 时把分组提到最外层。
	MoveToRoot bool `json:"moveToRoot"`
}

// ListInput 清单入参。
type ListInput struct {
	Name      *string `json:"name"`
	Color     *string `json:"color"`
	Icon      *string `json:"icon"`
	FolderID  *int64  `json:"folderId"`
	SortOrder *int    `json:"sortOrder"`
	Archived  *bool   `json:"archived"`
	Starred   *bool   `json:"starred"`
	// MoveToRoot 为 true 时把清单移出分组。
	MoveToRoot bool `json:"moveToRoot"`
}

// Folders 返回分组树。分组可以嵌套（parent_id），Children 是它的子树。
// 归档的分组仍然返回：侧栏把它们收进「已归档」里，前端才有得可展开。
func (s *Store) Folders() ([]model.Folder, error) {
	flat, err := s.flatFolders()
	if err != nil {
		return nil, err
	}
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	idx := map[int64]int{}
	for i := range flat {
		idx[flat[i].ID] = i
	}

	listsOf := map[int64][]model.List{}
	for _, l := range lists {
		if l.FolderID != nil {
			listsOf[*l.FolderID] = append(listsOf[*l.FolderID], l)
		}
	}

	// seen 兼作环路护栏：数据被手工改出「A 是 B 的父亲、B 又是 A 的父亲」时，
	// 递归会直接爆栈，宁可在某一层截断。
	var build func(id int64, seen map[int64]bool) model.Folder
	build = func(id int64, seen map[int64]bool) model.Folder {
		f := flat[idx[id]]
		out := model.Folder{
			ID: f.ID, ParentID: f.ParentID, Name: f.Name, Color: f.Color, Icon: f.Icon,
			SortOrder: f.SortOrder, Collapsed: f.Collapsed, Archived: f.Archived, CreatedAt: f.CreatedAt,
			Lists:    []model.List{},
			Children: []model.Folder{},
		}
		if ls, ok := listsOf[id]; ok {
			out.Lists = ls
		}
		next := map[int64]bool{}
		for k := range seen {
			next[k] = true
		}
		next[id] = true
		for _, cand := range flat {
			if cand.ParentID == nil || *cand.ParentID != id || next[cand.ID] {
				continue
			}
			out.Children = append(out.Children, build(cand.ID, next))
		}
		return out
	}

	roots := []model.Folder{}
	for _, f := range flat {
		// 父分组缺失时按顶层处理，不让它凭空消失。
		if f.ParentID != nil {
			if _, ok := idx[*f.ParentID]; ok {
				continue
			}
		}
		roots = append(roots, build(f.ID, map[int64]bool{}))
	}
	return roots, nil
}

// flatFolders 返回不带嵌套的分组列表，供树装配、导出与环路校验共用。
func (s *Store) flatFolders() ([]model.Folder, error) {
	rows, err := s.db.Query(`SELECT id, parent_id, name, color, icon, sort_order, collapsed, archived, created_at
		FROM folders ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	out := []model.Folder{}
	for rows.Next() {
		var f model.Folder
		var collapsed, archived int
		var parent sql.NullInt64
		if err := rows.Scan(&f.ID, &parent, &f.Name, &f.Color, &f.Icon, &f.SortOrder, &collapsed, &archived, &f.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if parent.Valid {
			v := parent.Int64
			f.ParentID = &v
		}
		f.Collapsed = collapsed == 1
		f.Archived = archived == 1
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return out, nil
}

// Lists 返回全部清单（含收集箱与已归档），附带未完成任务数。
func (s *Store) Lists() ([]model.List, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.folder_id, l.name, l.color, l.icon, l.sort_order, l.is_inbox, l.archived, l.starred, l.created_at,
		       (SELECT COUNT(*) FROM tasks t WHERE t.list_id = l.id AND t.status IN ('todo','in_progress'))
		FROM lists l ORDER BY l.is_inbox DESC, l.sort_order, l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.List{}
	for rows.Next() {
		var l model.List
		var fid *int64
		var inbox, archived, starred int
		if err := rows.Scan(&l.ID, &fid, &l.Name, &l.Color, &l.Icon, &l.SortOrder, &inbox, &archived, &starred, &l.CreatedAt, &l.TaskCount); err != nil {
			return nil, err
		}
		l.FolderID = fid
		l.Archived = archived == 1
		l.Starred = starred == 1
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

// CreateFolder 新建分组。ParentID 非空时建在另一个分组之下，形成多级分组。
func (s *Store) CreateFolder(in FolderInput) (*model.Folder, error) {
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("分组名称")
	}
	if in.ParentID != nil {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE id = ?`, *in.ParentID).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ValidationError{Msg: "分组不存在"}
		}
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
	color := derefStr(in.Color, "#8a7c66")
	icon := derefStr(in.Icon, "folder")
	res, err := s.db.Exec(
		`INSERT INTO folders(parent_id, name, color, icon, sort_order, collapsed, archived, created_at) VALUES(?,?,?,?,?,0,?,?)`,
		in.ParentID, name, color, icon, order, boolInt(derefBool(in.Archived, false)), model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Folder{
		ID: id, ParentID: in.ParentID, Name: name, Color: color, Icon: icon, SortOrder: order,
		Archived: derefBool(in.Archived, false), Lists: []model.List{}, Children: []model.Folder{},
	}, nil
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
	if in.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*in.Archived))
	}
	if in.MoveToRoot {
		sets = append(sets, "parent_id = NULL")
	} else if in.ParentID != nil {
		if *in.ParentID == id {
			return ValidationError{Msg: "分组不能成为自己的上级"}
		}
		descendant, err := s.folderDescendsFrom(*in.ParentID, id)
		if err != nil {
			return err
		}
		if descendant {
			// 否则会在树里绕成一个环，渲染与统计都会失控。
			return ValidationError{Msg: "不能把分组移动到它自己的下级里"}
		}
		sets = append(sets, "parent_id = ?")
		args = append(args, *in.ParentID)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE folders SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// folderDescendsFrom 判断 candidate 是否位于 ancestor 的子树里（含自身）。
func (s *Store) folderDescendsFrom(candidate, ancestor int64) (bool, error) {
	flat, err := s.flatFolders()
	if err != nil {
		return false, err
	}
	parent := map[int64]*int64{}
	for i := range flat {
		parent[flat[i].ID] = flat[i].ParentID
	}
	cur := candidate
	for i := 0; i < 64; i++ {
		if cur == ancestor {
			return true, nil
		}
		p, ok := parent[cur]
		if !ok || p == nil {
			return false, nil
		}
		cur = *p
	}
	return false, nil
}

// DeleteFolder 删除分组。分组内的清单回到顶层，子分组升到被删分组的上一级，
// 两者都不会被连带删除。
func (s *Store) DeleteFolder(id int64) error {
	var parentID *int64
	if err := s.db.QueryRow(`SELECT parent_id FROM folders WHERE id = ?`, id).Scan(&parentID); err != nil {
		return ErrNotFound
	}
	// 三条语句同生共死：中间失败若留下「清单已升空、子分组还挂着」的半截状态，
	// 再点一次删除会因分组已不存在而回 ErrNotFound，脏状态永远修不回来。
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE lists SET folder_id = NULL WHERE folder_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE folders SET parent_id = ? WHERE parent_id = ?`, parentID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM folders WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
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
	color := derefStr(in.Color, "#b4553d")
	icon := derefStr(in.Icon, "list")
	res, err := s.db.Exec(
		`INSERT INTO lists(folder_id, name, color, icon, sort_order, is_inbox, archived, starred, created_at) VALUES(?,?,?,?,?,0,?,?,?)`,
		in.FolderID, name, color, icon, order,
		boolInt(derefBool(in.Archived, false)), boolInt(derefBool(in.Starred, false)), model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.List{
		ID: id, FolderID: in.FolderID, Name: name, Color: color, Icon: icon, SortOrder: order,
		Archived: derefBool(in.Archived, false), Starred: derefBool(in.Starred, false),
	}, nil
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
	if in.Archived != nil {
		sets = append(sets, "archived = ?")
		args = append(args, boolInt(*in.Archived))
	}
	if in.Starred != nil {
		sets = append(sets, "starred = ?")
		args = append(args, boolInt(*in.Starred))
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

// DeleteList 删除清单；收集箱不可删除。
//
// 清单内的任务不能靠外键级联「顺手删掉」：那会绕过撤销槽位（删了没法反悔）、
// 不发事件（CalDAV / Webhook 对这次删除一无所知）、也不清理附件文件（磁盘上留孤儿）。
// 这里先按单删的口径把任务收进撤销槽位并逐条走 deleteTaskRows，最后才删清单本身。
func (s *Store) DeleteList(id int64) error {
	var inbox int
	var name string
	if err := s.db.QueryRow(`SELECT is_inbox, name FROM lists WHERE id = ?`, id).Scan(&inbox, &name); err != nil {
		return ErrNotFound
	}
	if inbox == 1 {
		return errForbidden("收集箱不可删除")
	}

	// 删清单是把整个清单端掉：归档的、已完成的一并带走，不留「幸存者」。
	listID := id
	tasks, err := s.ListTasks(TaskFilter{
		ListID:              &listID,
		Status:              "all",
		IncludeArchived:     true,
		IncludeTaskArchived: true,
		SortBy:              "manual",
	})
	if err != nil {
		return err
	}
	if err := s.stageUndo("删除清单「"+name+"」", tasks); err != nil {
		return err
	}
	deleted := 0
	for i := range tasks {
		err := s.deleteTaskRows(tasks[i].ID, &tasks[i], true)
		if errors.Is(err, ErrNotFound) {
			continue // 并发下已被别的路径删掉，不值得让整次删除失败
		}
		if err != nil {
			return err
		}
		deleted++
	}
	if deleted > 0 {
		s.logActivity(model.ActDeleted, tasks[0].ID, name,
			fmt.Sprintf("删除清单「%s」，连同 %d 件任务", name, deleted))
	}
	if _, err := s.db.Exec(`DELETE FROM lists WHERE id = ?`, id); err != nil {
		return err
	}
	return nil
}

func derefBool(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
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
		         WHERE tt.tag_id = g.id AND t.status IN ('todo','in_progress'))
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

// ---------- 保存的筛选条件 ----------

// SavedFilterInput 保存筛选条件的入参。
//
// Query 是一段不透明字符串（前端 TaskFilter 的 JSON 原文）。服务端刻意不解析它：
// 筛选维度会随版本增删，两端各解析一份迟早会不一致，保管原样反而更耐用。
type SavedFilterInput struct {
	Name      *string  `json:"name"`
	Query     *string  `json:"query"`
	SortOrder *float64 `json:"sortOrder"`
}

// SavedFilters 返回全部保存的筛选条件。
func (s *Store) SavedFilters() ([]model.SavedFilter, error) {
	rows, err := s.db.Query(`SELECT id, name, query, sort_order, created_at FROM saved_filters ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SavedFilter{}
	for rows.Next() {
		var f model.SavedFilter
		if err := rows.Scan(&f.ID, &f.Name, &f.Query, &f.SortOrder, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateSavedFilter 保存一个筛选条件。
func (s *Store) CreateSavedFilter(in SavedFilterInput) (*model.SavedFilter, error) {
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("筛选名称")
	}
	order := 0.0
	if in.SortOrder != nil {
		order = *in.SortOrder
	} else {
		var max *float64
		_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM saved_filters`).Scan(&max)
		if max != nil {
			order = *max + 1024
		}
	}
	query := derefStr(in.Query, "{}")
	res, err := s.db.Exec(`INSERT INTO saved_filters(name, query, sort_order, created_at) VALUES(?,?,?,?)`,
		name, query, order, model.Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.SavedFilter{ID: id, Name: name, Query: query, SortOrder: order}, nil
}

// UpdateSavedFilter 更新保存的筛选条件（改名或覆盖条件）。
func (s *Store) UpdateSavedFilter(id int64, in SavedFilterInput) error {
	sets, args := []string{}, []any{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return errBlank("筛选名称")
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if in.Query != nil {
		sets = append(sets, "query = ?")
		args = append(args, *in.Query)
	}
	if in.SortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *in.SortOrder)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.db.Exec("UPDATE saved_filters SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSavedFilter 删除保存的筛选条件。
func (s *Store) DeleteSavedFilter(id int64) error {
	res, err := s.db.Exec(`DELETE FROM saved_filters WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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

// SaveSettings 覆盖写入设置项。内部键（autoBackupLastAt 等）照写不误——
// 自动备份等内部路径经此落库；对外部请求的内部键拦截在 API 层做。
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
