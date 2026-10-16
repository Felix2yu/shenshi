package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// webhookTimeout 单次投递的超时。外部系统卡住不该拖住任务写入的返回。
const webhookTimeout = 8 * time.Second

// dispatcher 把库内的变更推给订阅方。
//
// 投递是「尽力而为」的：失败只记台账、不重试到底，也不回滚任务写入。
// 任务本身已经落库，外部系统没收到是外部系统的事——把写操作变成分布式事务
// 只会让本地自用场景变得脆弱。真需要可靠投递，订阅方应该反过来轮询 API。
type dispatcher struct {
	st     *store.Store
	client *http.Client
	logf   func(format string, v ...any)
}

func newDispatcher(st *store.Store, logf func(format string, v ...any)) *dispatcher {
	if logf == nil {
		logf = log.Printf
	}
	return &dispatcher{st: st, client: &http.Client{Timeout: webhookTimeout}, logf: logf}
}

// hook 返回注册到 store 上的事件钩子。
func (d *dispatcher) hook() store.EventHook {
	return func(kind string, payload any) {
		t, ok := payload.(*model.Task)
		if !ok || t == nil {
			return
		}
		hooks, err := d.st.WebhooksForEvent(kind)
		if err != nil {
			d.logf("webhook: 读取订阅失败: %v", err)
			return
		}
		for _, h := range hooks {
			go d.send(h, kind, t)
		}
	}
}

type webhookPayload struct {
	Event     string      `json:"event"`
	Timestamp string      `json:"timestamp"`
	Task      *model.Task `json:"task"`
	Test      bool        `json:"test,omitempty"`
}

func (d *dispatcher) send(h model.Webhook, kind string, task *model.Task) {
	body, err := json.Marshal(webhookPayload{Event: kind, Timestamp: model.Now(), Task: task})
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		d.record(h, kind, 0, false, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "shenshi-webhook/1")
	req.Header.Set("X-Shenshi-Event", kind)
	req.Header.Set("X-Shenshi-Timestamp", time.Now().UTC().Format(time.RFC3339))
	if h.Secret != "" {
		req.Header.Set("X-Shenshi-Signature", signature(h.Secret, body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	resp, err := d.client.Do(req.WithContext(ctx))
	if err != nil {
		// 超时/连接失败都归为一类，统一记 0 并提示重试语义。
		d.record(h, kind, 0, false, err.Error())
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	msg := ""
	if !ok {
		msg = fmt.Sprintf("响应 %d", resp.StatusCode)
	}
	d.record(h, kind, resp.StatusCode, ok, msg)
	if !ok {
		d.logf("webhook: 投递 %s 到 %s 失败：%s", kind, h.URL, msg)
	}
}

// signature 生成 HMAC-SHA256 签名，供订阅方校验请求确实来自本服务。
func signature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d *dispatcher) record(h model.Webhook, kind string, code int, ok bool, msg string) {
	if err := d.st.RecordDelivery(h.ID, kind, code, ok, msg); err != nil {
		d.logf("webhook: 记录投递结果失败: %v", err)
	}
}

// test 立即投递一条测试事件，用于验证地址与签名是否配对了。
func (d *dispatcher) test(h model.Webhook) {
	d.send(h, "webhook.test", &model.Task{
		ID:    0,
		Title: "慎始 · Webhook 测试",
		Notes: "这是一条测试投递，不代表真实任务变更。",
		Status: model.StatusTodo,
	})
}

// ---------- 接口 ----------

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) error {
	list, err := s.st.ListWebhooks()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) error {
	var in model.WebhookInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	h, err := s.st.CreateWebhook(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, h)
	return nil
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	var in model.WebhookInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	h, err := s.st.UpdateWebhook(id, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, h)
	return nil
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	if err := s.st.DeleteWebhook(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

// testWebhookHandler 触发一次测试投递。异步执行：不必等外部系统响应才返回。
func (s *Server) testWebhookHandler(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	h, err := s.st.GetWebhook(id)
	if err != nil {
		return err
	}
	go s.hooks.test(*h)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	list, err := s.st.RecentDeliveries(id, 20)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

// ---------- 注册 ----------

// registerWebhooks 把投递器挂到数据层的事件上。在 New 里调用一次。
func (s *Server) registerWebhooks() {
	s.hooks = newDispatcher(s.st, log.Printf)
	s.st.OnEvent(s.hooks.hook())
}
