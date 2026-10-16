package push

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/daaku/webpush"

	"github.com/yufei/shendu/server/internal/store"
)

// Web Push 通道：页面完全关掉也能把提醒送到。
//
// 与 Apprise 的分工：Apprise 覆盖上百种服务（ntfy / Bark / 邮件…），已在用，
// 但它要求用户另外装一个 App、配一个地址；Web Push 只要在主屏幕上装了这个应用就够了，
// 不依赖任何第三方。两条通道并存，投递台账共用一套去重逻辑（见 store.PushState）。
//
// iOS 的硬前提：16.4 起才支持，且**必须先添加到主屏幕**——Safari 标签页里
// `PushManager.subscribe()` 会直接失败。这不是可以绕过的限制，所以设置里要说清。

const (
	SetVAPIDSubject = "push.vapidSubject" // mailto:xxx 或 https://域名，RFC 8292 要求
	SetVAPIDPrivate = "push.vapidPrivate" // 私钥，base64url
	SetVAPIDPublic  = "push.vapidPublic"  // 公钥，给前端 subscribe 用
)

// webPushTTL 推送服务的保存时长。给 24 小时：iOS 会按自己的节奏投递，
// 设成几分钟的话，用户在锁屏前打开就会发现提醒已经过期消失了。
const webPushTTL = 24 * time.Hour

// sendTimeout 单次投递的超时。推送服务是外部依赖，10 秒连不上就该放弃这一轮，
// 不然会拖住整个 30 秒的巡检周期。
const sendTimeout = 10 * time.Second

// VAPIDPublicKey 返回给前端 subscribe 用的公钥。没配就是空串。
func VAPIDPublicKey(kv map[string]string) string {
	return strings.TrimSpace(kv[SetVAPIDPublic])
}

// GenerateVAPIDKeys 生成一对 VAPID 密钥。设置面板的「生成密钥」按钮用。
//
// 只返回私钥与公钥，**不落库** —— 由调用方决定写不写。密钥生成本身没有副作用，
// 这样测试可以随便调，设置面板也能先给用户看密钥再让他确认。
func GenerateVAPIDKeys() (private, public string, err error) {
	private, err = webpush.GenerateVAPIDKey()
	if err != nil {
		return "", "", err
	}
	key, err := webpush.ParseVAPIDKey(private)
	if err != nil {
		return "", "", err
	}
	// 公钥用未压缩点，与浏览器 PushManager.subscribe() 期望的格式一致。
	pub := ellipticMarshal(key)
	return private, base64.RawURLEncoding.EncodeToString(pub), nil
}

// WebPushPayload 是发给 Service Worker 的内容。
// 字段名与前端 sw.js 里的读取保持一致，改一处必须改另一处。
type WebPushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Tag 让同一任务的通知互相覆盖而不是堆叠。
	Tag string `json:"tag,omitempty"`
	// URL 点通知后打开的深链。
	URL string `json:"url,omitempty"`
	// AckID 回执 id：页面据此直接落回提醒中心。
	AckID int64 `json:"ackId,omitempty"`
}

// WebSender 向一条订阅投递。抽成函数是为了测试能替换。
type WebSender func(sub store.PushSubscription, payload WebPushPayload) error

// WebPushEnabled 判断 Web Push 通道是否可用：公私钥齐了、且至少有一条订阅。
func WebPushEnabled(kv map[string]string, subs int) bool {
	return VAPIDPublicKey(kv) != "" && strings.TrimSpace(kv[SetVAPIDPrivate]) != "" && subs > 0
}

// NewWebSender 构造投递函数。没配好密钥时返回一个明确的错误而不是 nil：
// 调用方会把它当成「这轮跳过 Web Push」而不是崩掉。
func NewWebSender(kv map[string]string) (WebSender, error) {
	priv := strings.TrimSpace(kv[SetVAPIDPrivate])
	if priv == "" {
		return nil, errors.New("尚未配置 VAPID 私钥")
	}
	key, err := webpush.ParseVAPIDKey(priv)
	if err != nil {
		return nil, errors.New("VAPID 私钥格式不对")
	}
	subject := strings.TrimSpace(kv[SetVAPIDSubject])
	if subject == "" {
		subject = "https://github.com/yufei/shendu"
	}
	client := &webpush.Client{
		Client:     &http.Client{Timeout: sendTimeout},
		VAPIDKey:   key,
		Subscriber: subject,
		TTL:        webPushTTL,
		Urgency:    webpush.UrgencyHigh,
	}
	return func(sub store.PushSubscription, payload WebPushPayload) error {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		err = client.Send(context.Background(), body, &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth},
		})
		// 订阅作废要能和服务端错误区分开：这一种必须删订阅、不能重试。
		if err != nil {
			var we *webpush.Error
			if errors.As(err, &we) && (we.Permanent || we.StatusCode == http.StatusGone || we.StatusCode == http.StatusNotFound) {
				return store.ErrSubscriptionGone
			}
		}
		return err
	}, nil
}

// WebPushStatus 是「Web Push 自检」要展示的东西。
type WebPushStatus struct {
	Configured bool   `json:"configured"`
	PublicKey  string `json:"publicKey"`
	Subject    string `json:"subject"`
	Subs       int    `json:"subs"`
	// LastEndpoint 与 LastOK 只取最近一条，便于确认「上次到底送到了没有」。
	LastEndpoint string `json:"lastEndpoint,omitempty"`
	LastOKAt     string `json:"lastOkAt,omitempty"`
}

// WebPushStatusFor 汇总当前 Web Push 状态。
func WebPushStatusFor(kv map[string]string, subs []store.PushSubscription) WebPushStatus {
	st := WebPushStatus{
		PublicKey: VAPIDPublicKey(kv),
		Subject:   strings.TrimSpace(kv[SetVAPIDSubject]),
		Subs:      len(subs),
	}
	st.Configured = st.PublicKey != "" && strings.TrimSpace(kv[SetVAPIDPrivate]) != ""
	// 「上次送到没有」要取**最近一次成功**的那台，而不是列表里最后一个 ——
	// 订阅按创建时间排序，最后一条很可能从没成功投递过（比如刚装上还没到点），
	// 拿它当答案会让用户以为推送一直是坏的。
	for _, sub := range subs {
		if sub.LastOKAt == "" {
			continue
		}
		if sub.LastOKAt > st.LastOKAt {
			st.LastOKAt = sub.LastOKAt
			st.LastEndpoint = sub.Endpoint
		}
	}
	return st
}

// ---------------------------------------------------------------------------
// 以下是 webpush 库没提供的小工具：密钥的编码与解码。
// 放在这里而不是让调用方自己转，是为了让「密钥怎么存」只有一个答案。
// ---------------------------------------------------------------------------

func ellipticMarshal(key *ecdsa.PrivateKey) []byte {
	// 未压缩点：0x04 || X(32) || Y(32)
	size := (key.Curve.Params().BitSize + 7) / 8
	out := make([]byte, 1+2*size)
	out[0] = 4
	key.X.FillBytes(out[1 : 1+size])
	key.Y.FillBytes(out[1+size:])
	return out
}
