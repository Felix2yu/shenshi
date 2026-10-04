package api

import (
	"net/http"
	"strings"

	"github.com/yufei/shendu/server/internal/push"
	"github.com/yufei/shendu/server/internal/store"
)

// subscribePush 保存一条浏览器推送订阅。
// 由页面在用户点了「开启通知」之后调用——Notification.requestPermission 与
// PushManager.subscribe 都必须由用户手势触发，iOS 尤其严格，不能放在启动流程里。
func (s *Server) subscribePush(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	// 三项缺一不可：缺任何一项都构造不出加密用的报文，
	// 存下来只会在推送时白等一次超时。
	if strings.TrimSpace(in.Endpoint) == "" || in.Keys.P256DH == "" || in.Keys.Auth == "" {
		return store.ValidationError{Msg: "订阅内容不完整（需要 endpoint、p256dh、auth）"}
	}
	ua := r.Header.Get("User-Agent")
	if len(ua) > 200 {
		ua = ua[:200]
	}
	if err := s.st.UpsertSubscription(store.PushSubscription{
		Endpoint: in.Endpoint,
		P256DH:   in.Keys.P256DH,
		Auth:     in.Keys.Auth,
		UA:       ua,
	}); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// unsubscribePush 移除一条订阅（前端关掉通知权限时调用）。
func (s *Server) unsubscribePush(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Endpoint) == "" {
		return store.ValidationError{Msg: "缺少 endpoint"}
	}
	if err := s.st.DeleteSubscription(in.Endpoint); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

// webPushStatus 报 Web Push 通道的状态：配没配、有几台设备、上次送到没有。
// 与 pushStatus 合并成一个接口会让「Apprise 通道正常但 Web Push 压根没开」这件事
// 混在一堆字段里说不清，所以单独给一个。
func (s *Server) webPushStatus(w http.ResponseWriter, r *http.Request) error {
	kv, err := s.st.Settings()
	if err != nil {
		return err
	}
	subs, err := s.st.Subscriptions()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, push.WebPushStatusFor(kv, subs))
	return nil
}

// webPushKeys 生成一对 VAPID 密钥。
// 只回给页面，不落库：用户得先看到密钥、确认，再由设置面板写进设置键。
// 私钥只在这一刻经过前端，之后一直存在服务端设置里（接口不会回读它）。
func (s *Server) webPushKeys(w http.ResponseWriter, r *http.Request) error {
	private, public, err := push.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"privateKey": private, "publicKey": public})
	return nil
}

// webPushTest 立刻往全部订阅发一条测试通知。
// 「配完了但从没验证过能不能收到」是最难自证的状态，这里给一个能立刻看到结果的按钮。
func (s *Server) webPushTest(w http.ResponseWriter, r *http.Request) error {
	kv, err := s.st.Settings()
	if err != nil {
		return err
	}
	subs, err := s.st.Subscriptions()
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return store.ValidationError{Msg: "还没有任何设备订阅推送"}
	}
	send, err := push.NewWebSender(kv)
	if err != nil {
		return store.ValidationError{Msg: err.Error()}
	}
	ok, failed := 0, 0
	var lastErr string
	for _, sub := range subs {
		err := send(sub, push.WebPushPayload{
			Title: "慎始 · 测试通知",
			Body:  "推送通道已经通了，到点提醒会这样送到。",
			Tag:   "test",
		})
		if err != nil {
			failed++
			lastErr = err.Error()
			// 订阅作废当场清掉：留着只会让之后每一轮都白等超时。
			if store.IsSubscriptionGone(err) {
				_ = s.st.DeleteSubscription(sub.Endpoint)
			}
			continue
		}
		ok++
		_ = s.st.MarkSubscriptionSent(sub.Endpoint)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "failed": failed, "error": lastErr})
	return nil
}