package api

import (
	"net/http"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// listTemplates 返回全部模板。
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) error {
	list, err := s.st.ListTemplates()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) error {
	var in model.TemplateInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	t, err := s.st.CreateTemplate(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, t)
	return nil
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in model.TemplateInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	t, err := s.st.UpdateTemplate(id, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, t)
	return nil
}

func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteTemplate(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// instantiateTemplate 按模板生成一条任务。
// 请求体可覆盖清单与日期：{ "listId": 3, "dueDate": "2026-10-01" }。
func (s *Server) instantiateTemplate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ListID  *int64  `json:"listId"`
		DueDate *string `json:"dueDate"`
	}
	if err := decodeOptional(w, r, &body); err != nil {
		return err
	}
	if body.DueDate != nil {
		if v := *body.DueDate; v != "" && store.CheckDay(v) != nil {
			return store.ValidationError{Msg: "日期格式应为 YYYY-MM-DD"}
		}
	}
	t, err := s.st.InstantiateTemplate(id, body.ListID, body.DueDate)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, t)
	return nil
}
