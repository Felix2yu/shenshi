package api

import (
	"net/http"

	"github.com/yufei/shendu/server/internal/push"
	"github.com/yufei/shendu/server/internal/store"
)

// pushStatus 返回推送的自检信息：开关、渠道条数、最近几条投递结果。
//
// 「我明明开了推送却没收到」是最难自证的问题 —— 可能是没配地址、可能是页面
// 关着而服务端其实推成功了、也可能是渠道地址写错。这里把服务端侧的真相摊开，
// 让人至少能分清「没发」和「发了但渠道拒收」。
func (s *Server) pushStatus(w http.ResponseWriter, r *http.Request) error {
	kv, err := s.st.Settings()
	if err != nil {
		return err
	}
	cfg := push.LoadConfig(kv)
	recent, err := s.st.RecentPushes(10)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   cfg.Enabled,
		"channels":  len(cfg.URLs),
		"daily":     cfg.DailyEnabled,
		"dailyTime": cfg.DailyTime,
		"baseUrl":   cfg.BaseURL,
		"recent":    recent,
	})
	return nil
}

// pushTest 立即向推送渠道发一条测试消息。
//
// 请求体可为空（用已保存的配置），也可带 {urls: [...]} 直接测未保存的输入——
// 设置面板里「填完就测、满意再存」比「先存再测」顺手得多。
// 逐地址返回结果：哪条配错了一眼可见，而不是笼统一句失败。
func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		URLs []string `json:"urls"`
	}
	if err := decodeOptional(w, r, &body); err != nil {
		return err
	}
	urls := body.URLs
	if len(urls) == 0 {
		kv, err := s.st.Settings()
		if err != nil {
			return err
		}
		urls = push.LoadConfig(kv).URLs
	}
	if len(urls) == 0 {
		return store.ValidationError{Msg: "尚未配置推送地址，请先填写至少一条 Apprise URL"}
	}
	results := make([]push.TestResult, 0, len(urls))
	for _, u := range urls {
		results = append(results, push.SendTest(u, "慎始 · 推送测试", "收到这条说明推送链路已就绪，提醒到期时会把消息推到这里。"))
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
	return nil
}
