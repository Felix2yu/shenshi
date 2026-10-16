package store

import (
	"errors"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// Web Push 订阅的存取。
//
// 为什么与 Apprise 通道并存而不是取代它：Apprise 覆盖上百种服务且已在用，
// Web Push 的独有价值只有一个 —— 页面完全关掉也能到，而且不依赖任何第三方服务。
// 两者成本极不对称（订阅表就一张），所以并存，不做二选一。

// PushSubscription 是一条浏览器推送订阅。
type PushSubscription struct {
	Endpoint  string `json:"endpoint"`
	P256DH    string `json:"p256dh"`
	Auth      string `json:"auth"`
	UA        string `json:"ua"`
	CreatedAt string `json:"createdAt"`
	LastOKAt  string `json:"lastOkAt"`
}

// UpsertSubscription 写入或更新一条订阅。
// 按 endpoint 判重：浏览器重订阅（换了应用版本、重新授权）会生成新 endpoint，
// 而「同一个人同一台设备」这件事只能靠 endpoint 近似——本就是单用户自托管，够用。
func (s *Store) UpsertSubscription(sub PushSubscription) error {
	_, err := s.db.Exec(`INSERT INTO push_subscriptions(endpoint, p256dh, auth, ua, created_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, ua = excluded.ua`,
		sub.Endpoint, sub.P256DH, sub.Auth, sub.UA, model.Now())
	return err
}

// Subscriptions 返回全部订阅。没有订阅时返回空切片而非 nil，
// 上层遍历时不必判空。
func (s *Store) Subscriptions() ([]PushSubscription, error) {
	rows, err := s.db.Query(`SELECT endpoint, p256dh, auth, ua, created_at, last_ok_at
		FROM push_subscriptions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		var sub PushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256DH, &sub.Auth, &sub.UA, &sub.CreatedAt, &sub.LastOKAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// SubscriptionCount 数一数当前有几台设备订阅了。
func (s *Store) SubscriptionCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions`).Scan(&n)
	return n, err
}

// DeleteSubscription 按 endpoint 移除一条。
func (s *Store) DeleteSubscription(endpoint string) error {
	_, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}

// MarkSubscriptionSent 记下最近一次成功投递的时间。
// 只在成功时更新：用户看设置页想知道「上次真的送到没有」，失败的次数没有意义。
func (s *Store) MarkSubscriptionSent(endpoint string) error {
	_, err := s.db.Exec(`UPDATE push_subscriptions SET last_ok_at = ? WHERE endpoint = ?`, model.Now(), endpoint)
	return err
}

// ErrSubscriptionGone 表示订阅已被推送服务作废（HTTP 410）。
// 这是唯一一种「重试也没用」的失败：必须删掉，而不是像普通失败那样留到三次上限。
var ErrSubscriptionGone = errors.New("订阅已失效")

// IsSubscriptionGone 判断错误是不是「订阅作废」。
// daaku/webpush 把 410 与 404 都算作订阅没了：两者都意味着这个 endpoint 不会再被投递，
// 留着只会让每一轮推送都白等一次超时。
func IsSubscriptionGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSubscriptionGone) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "410") || strings.Contains(msg, "404")
}

// PruneStaleSubscriptions 清掉太久没收到成功投递的订阅。
// 判据是 last_ok_at 而不是 created_at：长期不点通知的设备其推送服务可能早就把订阅作废了，
// 但只要还没报 410，我们就不知道；给个宽限期主动清掉，比一直白试便宜。
func (s *Store) PruneStaleSubscriptions(days int) (int, error) {
	if days <= 0 {
		days = 90
	}
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE last_ok_at <> '' AND last_ok_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}