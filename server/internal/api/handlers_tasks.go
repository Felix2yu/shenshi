package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// smartCounts 计算侧边栏各智能清单的角标数量。
//
// 「最近修改」与「最近完成」刻意不算：它们的数量就是「全部」与「已完成的全部」，
// 挂在侧栏只会变成一个永远很大的数字，没有信息量。
func (s *Server) smartCounts() (map[string]int, error) {
	smarts := []string{
		model.SmartInbox, model.SmartToday, model.SmartTomorrow, model.SmartWeek,
		model.SmartNext7, model.SmartOverdue, model.SmartNoDate, model.SmartAll,
		model.SmartDone, model.SmartHigh, model.SmartStarred,
	}
	out := map[string]int{}
	for _, name := range smarts {
		n, err := s.st.CountTasks(store.TaskFilter{Smart: name})
		if err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{
		"app":   "慎始",
		"motto": "慎始而敬终，行稳致远",
		"time":  model.Now(),
	})
	return nil
}

// bootstrap 一次性返回前端启动所需的全部基础数据，避免首屏多次往返。
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) error {
	folders, err := s.st.Folders()
	if err != nil {
		return err
	}
	lists, err := s.st.Lists()
	if err != nil {
		return err
	}
	tags, err := s.st.Tags()
	if err != nil {
		return err
	}
	settings, err := s.st.Settings()
	if err != nil {
		return err
	}
	inboxID, err := s.st.InboxListID()
	if err != nil {
		return err
	}
	counts, err := s.smartCounts()
	if err != nil {
		return err
	}
	savedFilters, err := s.st.SavedFilters()
	if err != nil {
		return err
	}
	undo, err := s.st.UndoState()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app":          "慎始",
		"motto":        "慎始而敬终，行稳致远",
		"today":        time.Now().Format("2006-01-02"),
		"folders":      folders,
		"lists":        lists,
		"tags":         tags,
		"settings":     settings,
		"inboxListId":  inboxID,
		"counts":       counts,
		"savedFilters": savedFilters,
		"undo":         undo,
	})
	return nil
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) error {
	f := store.TaskFilter{
		Smart:    r.URL.Query().Get("smart"),
		ListID:   queryInt64(r, "listId"),
		FolderID: queryInt64(r, "folderId"),
		TagID:    queryInt64(r, "tagId"),
		Status:   r.URL.Query().Get("status"),
		Search:   r.URL.Query().Get("q"),
		Quadrant: r.URL.Query().Get("quadrant"),
		SortBy:   r.URL.Query().Get("sortBy"),
		Limit:    queryInt(r, "limit", 0),
	}
	if v := queryStr(r, "date"); v != nil {
		f.From, f.To = v, v
	}
	if v := queryStr(r, "from"); v != nil {
		f.From = v
	}
	if v := queryStr(r, "to"); v != nil {
		f.To = v
	}
	if r.URL.Query().Has("priority") {
		p := queryInt(r, "priority", 0)
		f.Priority = &p
	}
	tasks, err := s.st.ListTasks(f)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks, "count": len(tasks)})
	return nil
}

var tagPattern = regexp.MustCompile(`#([\p{Han}\w\-]{1,20})`)

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) error {
	var in model.TaskInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	inbox, err := s.st.InboxListID()
	if err != nil {
		return err
	}
	// 便捷能力：标题中的 #标签 自动建档（REST 直接调用时也生效）。
	if !in.TagIDs.Set && in.Title.Set {
		if names := extractTags(in.Title.Value); len(names) > 0 {
			ids, err := s.st.EnsureTags(names)
			if err != nil {
				return err
			}
			in.TagIDs = model.Opt[[]int64]{Set: true, Value: ids}
		}
	}
	t, err := s.st.CreateTask(in, inbox)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, t)
	return nil
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	t, err := s.st.GetTask(id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in model.TaskInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	t, err := s.st.UpdateTask(id, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteTask(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) toggleTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	res, err := s.st.ToggleTask(id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

func (s *Server) moveTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ListID  *int64             `json:"listId"`
		DueDate model.Opt[*string] `json:"dueDate"`
		DueTime model.Opt[*string] `json:"dueTime"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	var dueDate, dueTime *string
	if body.DueDate.Set {
		dueDate = body.DueDate.Value
		if dueDate == nil {
			// 显式置 null 表示清空日期；用空串标记以便与「未传」区分。
			empty := ""
			dueDate = &empty
		}
	}
	if body.DueTime.Set {
		dueTime = body.DueTime.Value
		if dueTime == nil {
			empty := ""
			dueTime = &empty
		}
	}
	t, err := s.st.MoveTask(id, body.ListID, dueDate, dueTime)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) batchTasks(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs     []int64 `json:"ids"`
		Action  string  `json:"action"`
		ListID  *int64  `json:"listId"`
		DueDate *string `json:"dueDate"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 {
		return store.ValidationError{Msg: "未选择任何任务"}
	}
	n, err := s.st.BatchAction(body.IDs, body.Action, body.ListID, body.DueDate)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": n})
	return nil
}

// reorderTasks 按传入顺序重写 sort_order，供列表手动拖拽排序落库。
func (s *Server) reorderTasks(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 {
		return store.ValidationError{Msg: "未提供排序序列"}
	}
	if err := s.st.ReorderTasks(body.IDs); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.IDs)})
	return nil
}

// duplicateTask 复制一条任务：结构照搬，状态归零。用于「同一件事再做一遍」。
func (s *Server) duplicateTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	t, err := s.st.DuplicateTask(id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, t)
	return nil
}

// purgeCompleted 清空已完成任务。带 listId 时只清该清单内的。
func (s *Server) purgeCompleted(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		ListID *int64 `json:"listId"`
	}
	if err := decodeOptional(w, r, &body); err != nil {
		return err
	}
	n, err := s.st.PurgeCompleted(body.ListID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "affected": n})
	return nil
}

// skipTask 跳过重复任务的本次发生，只把日期推进到下一次。
func (s *Server) skipTask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	t, err := s.st.SkipTask(id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) addSubtask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if strings.TrimSpace(body.Title) == "" {
		return store.ValidationError{Msg: "子任务标题不能为空"}
	}
	sub, err := s.st.AddSubtask(id, body.Title, -1)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, sub)
	return nil
}

func (s *Server) updateSubtask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		Title     *string `json:"title"`
		Done      *bool   `json:"done"`
		SortOrder *int    `json:"sortOrder"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if err := s.st.UpdateSubtask(id, body.Title, body.Done, body.SortOrder); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) deleteSubtask(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteSubtask(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func extractTags(title string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range tagPattern.FindAllStringSubmatch(title, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
