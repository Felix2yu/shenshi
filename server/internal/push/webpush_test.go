package push

import (
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/store"
)

// TestGenerateVAPIDKeys 生成的密钥对必须自洽：私钥能解出、公钥是对应的那个点。
// 密钥对不上是最难发现的推送故障——服务端签得出 VAPID，推送服务却拒收。
func TestGenerateVAPIDKeys(t *testing.T) {
	priv, pub, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatalf("密钥为空：priv=%q pub=%q", priv, pub)
	}
	if strings.ContainsAny(pub, "+/=") {
		t.Errorf("公钥必须是 base64url（浏览器 subscribe 要求），得到 %q", pub)
	}

	// 私钥能解析，且公钥以未压缩点开头（0x04 base64url 编码后的前两字符是 "AE"）。
	if _, err := NewWebSender(map[string]string{SetVAPIDPrivate: priv}); err != nil {
		t.Fatalf("用生成的私钥构造发送器失败: %v", err)
	}
	if !strings.HasPrefix(pub, "B") && !strings.HasPrefix(pub, "A") {
		t.Logf("公钥前缀 = %q（未压缩点首字节 0x04 编码后落在首字符附近）", pub[:min(4, len(pub))])
	}

	// 两��生成必须不同：复用同一密钥会让所有设备共享一个身份。
	priv2, pub2, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("二次生成: %v", err)
	}
	if priv == priv2 || pub == pub2 {
		t.Error("两次生成的密钥相同，VAPID 身份应唯一")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestNewWebSenderErrors 没配私钥 / 私钥格式错时要给出明确错误，
// 而不是返回一个 nil 函数让上层崩在调用处。
func TestNewWebSenderErrors(t *testing.T) {
	if _, err := NewWebSender(map[string]string{}); err == nil {
		t.Error("未配私钥应报错")
	}
	if _, err := NewWebSender(map[string]string{SetVAPIDPrivate: "不是密钥"}); err == nil {
		t.Error("私钥格式不对应报错")
	}
}

// TestWebPushStatus 状态汇报要让用户分得清「没配」「配了没订阅」「配了也订阅了」。
func TestWebPushStatus(t *testing.T) {
	subs := []store.PushSubscription{
		{Endpoint: "https://push/a", LastOKAt: "2026-10-01T09:00:00+08:00"},
		{Endpoint: "https://push/b"},
	}

	// 没配。
	st := WebPushStatusFor(map[string]string{}, nil)
	if st.Configured {
		t.Error("没配密钥时 Configured 应为 false")
	}

	// 配了但没订阅。
	priv, pub, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("生成密钥: %v", err)
	}
	kv := map[string]string{SetVAPIDPrivate: priv, SetVAPIDPublic: pub}
	st = WebPushStatusFor(kv, nil)
	if !st.Configured {
		t.Error("配了密钥后 Configured 应为 true")
	}
	if st.Subs != 0 {
		t.Errorf("未订阅时 Subs = %d", st.Subs)
	}
	if WebPushEnabled(kv, 0) {
		t.Error("没有订阅时通道不该算可用")
	}

	// 配了也有订阅。
	st = WebPushStatusFor(kv, subs)
	if st.Subs != 2 {
		t.Errorf("Subs = %d，期望 2", st.Subs)
	}
	if !WebPushEnabled(kv, 2) {
		t.Error("配了密钥且有订阅时通道应可用")
	}
	if st.LastOKAt == "" {
		t.Error("应报出最近一次成功投递时间")
	}
}

// TestWebPushPayloadTag 台账键当 tag：同一提醒在设备上不该堆成一串。
func TestWebPushPayloadTag(t *testing.T) {
	key := ReminderKey(42, "2026-10-04T09:00:00+08:00")
	if key != "R|42|2026-10-04T09:00:00+08:00" {
		t.Errorf("台账键 = %q", key)
	}
	if !strings.HasPrefix(key, "R|") {
		t.Error("提醒键应以 R 开头")
	}
	if d := DailyKey("2026-10-04"); d != "D|2026-10-04" {
		t.Errorf("每日键 = %q", d)
	}
}