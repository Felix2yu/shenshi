package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/model"
)

// TestTemplateHandlers 模板的接口层 CRUD 与实例化。
func TestTemplateHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	// list 初始。
	w := httptest.NewRecorder()
	if err := s.listTemplates(w, httptest.NewRequest("GET", "/api/templates", nil)); err != nil {
		t.Fatalf("listTemplates: %v", err)
	}
	var list []model.TaskTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析模板列表: %v", err)
	}

	// create 空名 → 拒绝。
	w = httptest.NewRecorder()
	if err := s.createTemplate(w, httptest.NewRequest("POST", "/api/templates", strings.NewReader(`{"name":""}`))); err == nil {
		t.Error("空名应被拒绝")
	}
	// create 成功。
	w = httptest.NewRecorder()
	body := `{"name":"接口模板","title":"模板任务"}`
	if err := s.createTemplate(w, httptest.NewRequest("POST", "/api/templates", strings.NewReader(body))); err != nil {
		t.Fatalf("createTemplate: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var tpl model.TaskTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &tpl); err != nil || tpl.ID == 0 {
		t.Fatalf("template = %+v err=%v", tpl, err)
	}

	// update。
	id := itoa(tpl.ID)
	w = httptest.NewRecorder()
	ub := `{"name":"接口模板v2"}`
	if err := s.updateTemplate(w, reqWithID("PATCH", "/api/templates/"+id, id, ub)); err != nil {
		t.Fatalf("updateTemplate: %v", err)
	}
	// update 非法 id。
	w = httptest.NewRecorder()
	if err := s.updateTemplate(w, reqWithID("PATCH", "/api/templates/x", "x", `{}`)); err == nil {
		t.Error("非法 id 应报错")
	}
	// update 不存在。
	w = httptest.NewRecorder()
	if err := s.updateTemplate(w, reqWithID("PATCH", "/api/templates/999999", "999999", `{}`)); err == nil {
		t.Error("不存在应报错")
	}

	// instantiate：空体直接按模板。
	w = httptest.NewRecorder()
	if err := s.instantiateTemplate(w, reqWithID("POST", "/api/templates/"+id+"/instantiate", id, "")); err != nil {
		t.Fatalf("instantiateTemplate: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var task model.Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil || task.Title != "模板任务" {
		t.Errorf("任务 = %+v err=%v", task, err)
	}
	// instantiate 非法日期。
	w = httptest.NewRecorder()
	ib := `{"dueDate":"2026-99-99"}`
	if err := s.instantiateTemplate(w, reqWithID("POST", "/api/templates/"+id+"/instantiate", id, ib)); err == nil {
		t.Error("非法日期应被拒绝")
	}
	// instantiate 空串日期 = 清空。
	w = httptest.NewRecorder()
	ib = `{"dueDate":""}`
	if err := s.instantiateTemplate(w, reqWithID("POST", "/api/templates/"+id+"/instantiate", id, ib)); err != nil {
		t.Errorf("空串日期应放行: %v", err)
	}

	// delete。
	w = httptest.NewRecorder()
	if err := s.deleteTemplate(w, reqWithID("DELETE", "/api/templates/"+id, id, "")); err != nil {
		t.Fatalf("deleteTemplate: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteTemplate(w, reqWithID("DELETE", "/api/templates/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}
}

// TestWebhookHandlers 回调配置的接口层 CRUD 与投递台账。
func TestWebhookHandlers(t *testing.T) {
	s, _ := newTestServer(t)

	w := httptest.NewRecorder()
	if err := s.listWebhooks(w, httptest.NewRequest("GET", "/api/webhooks", nil)); err != nil {
		t.Fatalf("listWebhooks: %v", err)
	}
	var hooks []model.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &hooks); err != nil {
		t.Fatalf("解析: %v", err)
	}

	// 非法 URL → 拒绝。
	w = httptest.NewRecorder()
	if err := s.createWebhook(w, httptest.NewRequest("POST", "/api/webhooks", strings.NewReader(`{"url":"ftp://x"}`))); err == nil {
		t.Error("非 http(s) 应被拒绝")
	}
	// create 成功，密钥脱敏。
	w = httptest.NewRecorder()
	cb := `{"name":"接口回调","url":"https://example.com/hook","secret":"s3cr3t"}`
	if err := s.createWebhook(w, httptest.NewRequest("POST", "/api/webhooks", strings.NewReader(cb))); err != nil {
		t.Fatalf("createWebhook: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var h model.Webhook
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil || h.ID == 0 {
		t.Fatalf("webhook = %+v err=%v", h, err)
	}
	if h.Secret != "" || !h.HasSecret {
		t.Errorf("响应应脱敏: secret=%q has=%v", h.Secret, h.HasSecret)
	}

	id := itoa(h.ID)
	// update。
	w = httptest.NewRecorder()
	ub := `{"name":"接口回调2"}`
	if err := s.updateWebhook(w, reqWithID("PATCH", "/api/webhooks/"+id, id, ub)); err != nil {
		t.Fatalf("updateWebhook: %v", err)
	}
	// 非法 update。
	w = httptest.NewRecorder()
	if err := s.updateWebhook(w, reqWithID("PATCH", "/api/webhooks/999999", "999999", `{}`)); err == nil {
		t.Error("不存在应报错")
	}

	// 台账（初始为空数组）。
	w = httptest.NewRecorder()
	if err := s.listDeliveries(w, reqWithID("GET", "/api/webhooks/"+id+"/deliveries", id, "")); err != nil {
		t.Fatalf("listDeliveries: %v", err)
	}
	var dels []model.WebhookDelivery
	if err := json.Unmarshal(w.Body.Bytes(), &dels); err != nil {
		t.Fatalf("解析台账: %v", err)
	}

	// 测试投递：URL 指向本机（异步发送，不等待结果）。
	w = httptest.NewRecorder()
	if err := s.testWebhookHandler(w, reqWithID("POST", "/api/webhooks/"+id+"/test", id, "")); err != nil {
		t.Fatalf("testWebhookHandler: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("code = %d", w.Code)
	}
	// 不存在的测试。
	w = httptest.NewRecorder()
	if err := s.testWebhookHandler(w, reqWithID("POST", "/api/webhooks/999999/test", "999999", "")); err == nil {
		t.Error("不存在应回 404 语义")
	}

	// delete。
	w = httptest.NewRecorder()
	if err := s.deleteWebhook(w, reqWithID("DELETE", "/api/webhooks/"+id, id, "")); err != nil {
		t.Fatalf("deleteWebhook: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.deleteWebhook(w, reqWithID("DELETE", "/api/webhooks/"+id, id, "")); err == nil {
		t.Error("重复删除应报错")
	}
}

// TestSignature 幂等的 HMAC 签名格式。
func TestSignature(t *testing.T) {
	s1 := signature("k", []byte("hello"))
	s2 := signature("k", []byte("hello"))
	if s1 != s2 {
		t.Error("同一输入签名应一致")
	}
	if !strings.HasPrefix(s1, "sha256=") {
		t.Errorf("签名 = %q，应带 sha256= 前缀", s1)
	}
	if signature("k2", []byte("hello")) == s1 {
		t.Error("不同密钥签名应不同")
	}
}
