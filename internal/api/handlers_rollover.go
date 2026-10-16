package api

import (
	"net/http"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// rollOverdue 把所有逾期未完成的任务改到今天（或指定日）。
//
// 破坏性写入，因此：① 必须 POST，② 显式调用，绝不在后台定时悄悄跑。
// 逾期清单越积越厚、厚了就没人看，但「昨天没做完」该怎么办只有用户自己知道——
// 替他默默改期，等于替他做了他未必想要的决定。要顺延，就让他自己按这一下。
func (s *Server) rollOverdue(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		// To 为空即今天。允许传别的日期是给「批量推迟到下周一」这类用法留的口子。
		To string `json:"to"`
	}
	if err := decodeOptional(w, r, &in); err != nil {
		return err
	}
	if in.To != "" {
		if _, err := time.Parse("2006-01-02", in.To); err != nil {
			return store.ValidationError{Msg: "日期格式应为 2006-01-02"}
		}
	}
	n, err := s.st.RollOverdue(in.To)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"rolled": n})
	return nil
}

// overdueCount 报今天有几件事逾期，供 UI 在按钮上说明「这会动多少条」。
func (s *Server) overdueCount(w http.ResponseWriter, r *http.Request) error {
	today := r.URL.Query().Get("today")
	if today != "" {
		if _, err := time.Parse("2006-01-02", today); err != nil {
			return store.ValidationError{Msg: "日期格式应为 2006-01-02"}
		}
	}
	n, err := s.st.CountOverdue(today)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"overdue": n})
	return nil
}