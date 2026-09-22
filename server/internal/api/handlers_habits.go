package api

import (
	"net/http"

	"github.com/yufei/shendu/server/internal/store"
)

// listHabits 返回习惯视图所需的全部数据：习惯、区间流水与统计。
// 默认区间为最近 12 周，正好铺满一张热力图。
func (s *Server) listHabits(w http.ResponseWriter, r *http.Request) error {
	board, err := s.st.HabitBoard(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, board)
	return nil
}

func (s *Server) createHabit(w http.ResponseWriter, r *http.Request) error {
	var in store.HabitInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	h, err := s.st.CreateHabit(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, h)
	return nil
}

func (s *Server) updateHabit(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.HabitInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	h, err := s.st.UpdateHabit(id, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, h)
	return nil
}

func (s *Server) deleteHabit(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteHabit(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) reorderHabits(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if err := s.st.ReorderHabits(body.IDs); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": len(body.IDs)})
	return nil
}

// checkHabit 打卡。请求体缺省 day 时按今天计。
func (s *Server) checkHabit(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in store.CheckInput
	// 允许空请求体：不传即表示「今天加一次」。
	if err := decodeOptional(w, r, &in); err != nil {
		return err
	}
	if in.Day == "" {
		in.Day = today()
	}
	log, err := s.st.CheckIn(id, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"log": log})
	return nil
}

// uncheckHabit 撤销打卡，日期取查询参数 day（缺省今天）。
func (s *Server) uncheckHabit(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = today()
	}
	if err := s.st.Uncheck(id, day); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"day": day})
	return nil
}
