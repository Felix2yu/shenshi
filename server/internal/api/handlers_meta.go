package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// ---------- 分组 / 清单 ----------

func (s *Server) listFolders(w http.ResponseWriter, r *http.Request) error {
	folders, err := s.st.Folders()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
	return nil
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) error {
	var in store.FolderInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	f, err := s.st.CreateFolder(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, f)
	return nil
}

func (s *Server) updateFolder(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.FolderInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.st.UpdateFolder(id, in); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteFolder(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// reorderFolders / reorderLists：接收 id 数组，按数组顺序重写侧边栏排序。
func (s *Server) reorderFolders(w http.ResponseWriter, r *http.Request) error {
	return s.reorderOrg(w, r, s.st.ReorderFolders)
}

func (s *Server) reorderLists(w http.ResponseWriter, r *http.Request) error {
	return s.reorderOrg(w, r, s.st.ReorderLists)
}

// reorderOrg 抽取分组与清单共用的入参解析与响应。
func (s *Server) reorderOrg(w http.ResponseWriter, r *http.Request, apply func([]int64) error) error {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if len(body.IDs) == 0 {
		return store.ValidationError{Msg: "未提供排序序列"}
	}
	if err := apply(body.IDs); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(body.IDs)})
	return nil
}

func (s *Server) listLists(w http.ResponseWriter, r *http.Request) error {
	lists, err := s.st.Lists()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": lists})
	return nil
}

func (s *Server) createList(w http.ResponseWriter, r *http.Request) error {
	var in store.ListInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	l, err := s.st.CreateList(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, l)
	return nil
}

func (s *Server) updateList(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.ListInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.st.UpdateList(id, in); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) deleteList(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteList(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// ---------- 标签 ----------

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) error {
	tags, err := s.st.Tags()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
	return nil
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) error {
	var in store.TagInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	tag, err := s.st.CreateTag(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, tag)
	return nil
}

// ensureTags 供快速添加时把标题中的 #标签 就地建档。
func (s *Server) ensureTags(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Names []string `json:"names"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	ids, err := s.st.EnsureTags(body.Names)
	if err != nil {
		return err
	}
	tags, err := s.st.Tags()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": ids, "tags": tags})
	return nil
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.TagInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.st.UpdateTag(id, in); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteTag(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// ---------- 保存的筛选条件 ----------

func (s *Server) listSavedFilters(w http.ResponseWriter, r *http.Request) error {
	items, err := s.st.SavedFilters()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"savedFilters": items})
	return nil
}

func (s *Server) createSavedFilter(w http.ResponseWriter, r *http.Request) error {
	var in store.SavedFilterInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	f, err := s.st.CreateSavedFilter(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, f)
	return nil
}

func (s *Server) updateSavedFilter(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.SavedFilterInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := s.st.UpdateSavedFilter(id, in); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) deleteSavedFilter(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteSavedFilter(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// ---------- 设置 ----------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) error {
	kv, err := s.st.Settings()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, kv)
	return nil
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) error {
	var kv map[string]string
	if err := decode(w, r, &kv); err != nil {
		return err
	}
	if err := s.st.SaveSettings(kv); err != nil {
		return err
	}
	out, err := s.st.Settings()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// ---------- 统计 / 复盘 / 专注 ----------

func (s *Server) stats(w http.ResponseWriter, r *http.Request) error {
	st, err := s.st.Stats(queryInt(r, "days", 30))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) error {
	rs, err := s.st.ListReviews(queryInt(r, "limit", 30))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": rs})
	return nil
}

func (s *Server) getReview(w http.ResponseWriter, r *http.Request) error {
	date := r.PathValue("date")
	if _, err := time.ParseInLocation("2006-01-02", date, time.Local); err != nil {
		return store.ValidationError{Msg: "日期格式应为 YYYY-MM-DD"}
	}
	rev, err := s.st.GetReview(date)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rev)
	return nil
}

func (s *Server) putReview(w http.ResponseWriter, r *http.Request) error {
	var in store.ReviewInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	rev, err := s.st.UpsertReview(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, rev)
	return nil
}

func (s *Server) listFocus(w http.ResponseWriter, r *http.Request) error {
	fs, err := s.st.ListFocus(queryInt(r, "limit", 20))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": fs})
	return nil
}

func (s *Server) addFocus(w http.ResponseWriter, r *http.Request) error {
	var in store.FocusInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Minutes <= 0 || in.Minutes > 600 {
		return store.ValidationError{Msg: "专注时长应在 1 ~ 600 分钟之间"}
	}
	sess, err := s.st.AddFocus(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, sess)
	return nil
}

// ---------- 提醒 ----------

func (s *Server) dueReminders(w http.ResponseWriter, r *http.Request) error {
	lookahead := time.Duration(queryInt(r, "lookahead", 120)) * time.Minute
	hits, err := s.st.DueReminders(time.Now(), lookahead, 72*time.Hour)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"reminders": hits, "serverTime": model.Now()})
	return nil
}

func (s *Server) ackReminder(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		TaskID int64  `json:"taskId"`
		FireAt string `json:"fireAt"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if body.TaskID <= 0 || body.FireAt == "" {
		return store.ValidationError{Msg: "taskId 与 fireAt 均为必填"}
	}
	if err := s.st.AckReminder(body.TaskID, body.FireAt); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// resetReminders 清空提醒台账：用户在设置中点击「重置全部提醒」时使用。
func (s *Server) resetReminders(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		TaskID int64 `json:"taskId"`
	}
	if r.ContentLength > 0 {
		if err := decode(w, r, &body); err != nil {
			return err
		}
	}
	if err := s.st.ClearReminderLog(body.TaskID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// ---------- 重复规则元数据 ----------

// repeatPreset 描述一个可选的重复规则。
type repeatPreset struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Group string `json:"group"`
}

// repeatMeta 把重复规则的选项与艾宾浩斯间隔交给前端，
// 前端只负责渲染，规则语义由后端统一解释，避免两端理解不一致。
func (s *Server) repeatMeta(w http.ResponseWriter, r *http.Request) error {
	presets := []repeatPreset{
		{Value: "", Label: "不重复", Group: "基础"},
		{Value: "daily", Label: "每天", Group: "基础"},
		{Value: "weekdays", Label: "每个工作日", Group: "基础"},
		{Value: "weekly", Label: "每周", Group: "基础"},
		{Value: "monthly", Label: "每月", Group: "基础"},
		{Value: "yearly", Label: "每年", Group: "基础"},
		{Value: "every:2:day", Label: "每 2 天", Group: "间隔"},
		{Value: "every:3:day", Label: "每 3 天", Group: "间隔"},
		{Value: "every:2:week", Label: "每 2 周", Group: "间隔"},
		{Value: "every:6:month", Label: "每 6 个月", Group: "间隔"},
		{Value: "monthly:last", Label: "每月最后一天", Group: "星期与月末"},
		{Value: "monthly:lastworkday", Label: "每月最后一个工作日", Group: "星期与月末"},
		{Value: "monthly:nth:1:1", Label: "每月第一个周一", Group: "星期与月末"},
		{Value: "monthly:nth:3:0", Label: "每月第三个周日", Group: "星期与月末"},
		{Value: "weekly:1", Label: "每周一", Group: "星期与月末"},
		{Value: "weekly:5", Label: "每周五", Group: "星期与月末"},
		{Value: "weekly:1,3,5", Label: "每周一、三、五", Group: "星期与月末"},
		{Value: "ebbinghaus:0", Label: "艾宾浩斯记忆曲线", Group: "记忆"},
	}
	offsets := store.EbbinghausOffsets()
	strOffsets := make([]string, 0, len(offsets))
	for _, o := range offsets {
		strOffsets = append(strOffsets, strconv.Itoa(o))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"presets":           presets,
		"ebbinghausOffsets": offsets,
		"ebbinghausDays":    strOffsets,
	})
	return nil
}
