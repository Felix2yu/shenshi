package api

import (
	"net/http"
)

// ---------- 撤销 ----------

// undoState 查询当前是否有可撤销的删除。
func (s *Server) undoState(w http.ResponseWriter, r *http.Request) error {
	st, err := s.st.UndoState()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, st)
	return nil
}

// undo 恢复最近一次删除掉的任务。
func (s *Server) undo(w http.ResponseWriter, r *http.Request) error {
	n, err := s.st.Undo()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": n})
	return nil
}

// dropUndo 放弃撤销机会。放弃了就等于确认删除，附件文件这时才真正清掉。
func (s *Server) dropUndo(w http.ResponseWriter, r *http.Request) error {
	if err := s.st.DropUndo(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// ---------- 操作历史 ----------

func (s *Server) listActivities(w http.ResponseWriter, r *http.Request) error {
	items, err := s.st.Activities(queryInt(r, "limit", 120))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": items})
	return nil
}

func (s *Server) clearActivities(w http.ResponseWriter, r *http.Request) error {
	if err := s.st.ClearActivities(); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}
