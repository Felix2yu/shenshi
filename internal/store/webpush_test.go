package store

import (
	"errors"
	"testing"
	"time"
)

// TestPushSubscriptions 订阅的存取与作废判定。
func TestPushSubscriptions(t *testing.T) {
	s := newTestStore(t)

	n, err := s.SubscriptionCount()
	if err != nil {
		t.Fatalf("SubscriptionCount: %v", err)
	}
	if n != 0 {
		t.Errorf("初始订阅数 = %d，期望 0", n)
	}

	sub := PushSubscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/abc",
		P256DH:   "key1",
		Auth:     "auth1",
		UA:       "iPhone",
	}
	if err := s.UpsertSubscription(sub); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	// 同一 endpoint 重复订阅（用户重新授权 / 应用升级）应覆盖而不是新增。
	sub.P256DH = "key2"
	if err := s.UpsertSubscription(sub); err != nil {
		t.Fatalf("重复 UpsertSubscription: %v", err)
	}
	subs, err := s.Subscriptions()
	if err != nil {
		t.Fatalf("Subscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("订阅数 = %d，期望 1（重复订阅应覆盖）", len(subs))
	}
	if subs[0].P256DH != "key2" {
		t.Errorf("p256dh = %q，期望被覆盖为 key2", subs[0].P256DH)
	}

	// 成功投递要记时间，供设置页看「上次送到没有」。
	if err := s.MarkSubscriptionSent(sub.Endpoint); err != nil {
		t.Fatalf("MarkSubscriptionSent: %v", err)
	}
	subs, _ = s.Subscriptions()
	if subs[0].LastOKAt == "" {
		t.Error("成功投递后 last_ok_at 应有值")
	}

	// 主动退订。
	if err := s.DeleteSubscription(sub.Endpoint); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}
	n, _ = s.SubscriptionCount()
	if n != 0 {
		t.Errorf("退订后订阅数 = %d，期望 0", n)
	}
}

// TestIsSubscriptionGone 订阅作废必须能与普通失败区分开。
// 混为一谈的后果是：作废的订阅会被重试三次、每轮白等一次超时，还一直留在库里。
func TestIsSubscriptionGone(t *testing.T) {
	if IsSubscriptionGone(nil) {
		t.Error("nil 不该判为订阅失效")
	}
	if !IsSubscriptionGone(ErrSubscriptionGone) {
		t.Error("ErrSubscriptionGone 应判为失效")
	}
	if !IsSubscriptionGone(errors.New("webpush: fcm.googleapis.com: gone (410)")) {
		t.Error("410 应判为失效")
	}
	if !IsSubscriptionGone(errors.New("request failed: 404")) {
		t.Error("404 应判为失效")
	}
	if IsSubscriptionGone(errors.New("connection refused")) {
		t.Error("连不上是临时故障，不该判为失效（否则会误删订阅）")
	}
}

// TestPruneStaleSubscriptions 长期不成功的订阅要主动清掉。
func TestPruneStaleSubscriptions(t *testing.T) {
	s := newTestStore(t)

	old := PushSubscription{Endpoint: "https://push.example/old", P256DH: "k", Auth: "a"}
	fresh := PushSubscription{Endpoint: "https://push.example/fresh", P256DH: "k", Auth: "a"}
	never := PushSubscription{Endpoint: "https://push.example/never", P256DH: "k", Auth: "a"}
	for _, sub := range []PushSubscription{old, fresh, never} {
		if err := s.UpsertSubscription(sub); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}
	// old 成功过但很久以前；fresh 刚成功过；never 从没成功过。
	oldAt := time.Now().AddDate(0, 0, -200).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE push_subscriptions SET last_ok_at = ? WHERE endpoint = ?`, oldAt, old.Endpoint); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := s.MarkSubscriptionSent(fresh.Endpoint); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	removed, err := s.PruneStaleSubscriptions(90)
	if err != nil {
		t.Fatalf("PruneStaleSubscriptions: %v", err)
	}
	if removed != 1 {
		t.Errorf("清理条数 = %d，期望 1", removed)
	}
	subs, _ := s.Subscriptions()
	endpoints := map[string]bool{}
	for _, sub := range subs {
		endpoints[sub.Endpoint] = true
	}
	if endpoints[old.Endpoint] {
		t.Error("久未成功的订阅应被清掉")
	}
	if !endpoints[fresh.Endpoint] {
		t.Error("刚成功过的订阅不该被清掉")
	}
	// 从没成功过的暂时留着：它可能只是刚装上，还没到点推送过。
	if !endpoints[never.Endpoint] {
		t.Error("从未成功过的订阅不该被清理（last_ok_at 为空）")
	}
}