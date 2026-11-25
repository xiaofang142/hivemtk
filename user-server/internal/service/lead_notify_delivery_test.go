package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestLeadNotifyAppendDeliveryRecordsDoesNotOverwriteHistory 追加写不能覆盖路由上既有送达历史。
func TestLeadNotifyAppendDeliveryRecordsDoesNotOverwriteHistory(t *testing.T) {
	route := modelLeadNotifyRouteWithHistory(`[{"clue_id":"old","success":true}]`)
	added := []map[string]any{{"clue_id": "new", "success": false, "error": "no_recipient"}}
	raw := appendLeadNotifyDeliveryRecords(route, added)
	var got []map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("送达记录不是合法 JSON: %v raw=%s", err, raw)
	}
	if len(got) != 2 || got[0]["clue_id"] != "old" || got[1]["clue_id"] != "new" {
		t.Fatalf("历史被覆盖或新记录丢失：%s", raw)
	}
}

// TestLeadNotifyDeliveryStatusMatrix 无收件人/无目标/部分成功/失败都有明确状态。
func TestLeadNotifyDeliveryStatusMatrix(t *testing.T) {
	cases := []struct {
		name       string
		records    []map[string]any
		wantStatus string
	}{
		{"no_recipient", []map[string]any{{"success": false, "error": "no_recipient"}}, "no_recipient"},
		{"no_target", []map[string]any{{"success": false, "error": "no_target"}}, "no_target"},
		{"partial", []map[string]any{{"success": true}, {"success": false, "error": "SendMessage failed"}}, "partial"},
		{"failed", []map[string]any{{"success": false}, {"success": false}}, "failed"},
		{"empty", nil, "no_recipient"},
	}
	for _, c := range cases {
		if got := leadNotifyDeliveryStatus(c.records); got != c.wantStatus {
			t.Errorf("%s: 状态 = %q, want %q", c.name, got, c.wantStatus)
		}
	}
}

// TestLeadNotifyOutboxIdempotency 同一 event/recipient/channel 重复投递只占一个 durable 意图。
func TestLeadNotifyOutboxIdempotency(t *testing.T) {
	store := newMemoryLeadNotifyOutbox()
	key := leadNotifyIdempotencyKey(11, "clue-1", "event-1", "42", "telegram")
	first, created := store.Enqueue(context.Background(), key, leadNotifyOutboxItem{
		TenantID: 11, ClueID: "clue-1", EventID: "event-1", Recipient: "42", Channel: "telegram",
	})
	if !created || first.Status != "pending" {
		t.Fatalf("首次入队应为 pending，实际 %+v", first)
	}
	second, created := store.Enqueue(context.Background(), key, leadNotifyOutboxItem{})
	if created || second.Status != "pending" {
		t.Fatalf("重复事件不得新建投递意图：created=%v item=%+v", created, second)
	}
	if len(store.All()) != 1 {
		t.Fatalf("幂等键重复占用了 %d 条记录", len(store.All()))
	}
}

// TestLeadNotifyOutboxBackoffAndManualRetry 失败按退避进入 retry，人工重试可重置 pending；回执丢失单独标记。
func TestLeadNotifyOutboxBackoffAndManualRetry(t *testing.T) {
	store := newMemoryLeadNotifyOutbox()
	key := leadNotifyIdempotencyKey(1, "clue-1", "event-1", "u-1", "in_app")
	store.Enqueue(context.Background(), key, leadNotifyOutboxItem{TenantID: 1, ClueID: "clue-1", EventID: "event-1", Recipient: "u-1", Channel: "in_app"})
	item, _ := store.Fail(context.Background(), key, errors.New("timeout"), 1)
	if item.Status != "retry" || !item.NextAttemptAt.After(item.LastAttemptAt) {
		t.Fatalf("失败应按退避进入 retry：%+v", item)
	}
	item, _ = store.Fail(context.Background(), key, errors.New("timeout"), 6)
	if item.Status != "exhausted" {
		t.Fatalf("超过最大重试应进入 exhausted：%+v", item)
	}
	item, _ = store.RetryManually(context.Background(), key, 1)
	if item.Status != "pending" || item.Attempts != 0 {
		t.Fatalf("人工重试应重置为 pending：%+v", item)
	}
	item, _ = store.MarkReceiptUnknown(context.Background(), key)
	if item.Status != "receipt_unknown" || item.ReceiptLost != true {
		t.Fatalf("回执丢失必须单独标记：%+v", item)
	}
}

// TestLeadNotifyOutboxSurvivesRebuild 重启后（新建同 store 实例、保留共享状态）投递意图仍可恢复。
func TestLeadNotifyOutboxSurvivesRebuild(t *testing.T) {
	shared := map[string]leadNotifyOutboxItem{}
	s1 := &memoryLeadNotifyOutbox{items: shared}
	key := leadNotifyIdempotencyKey(1, "clue-2", "event-2", "u-2", "in_app")
	s1.Enqueue(context.Background(), key, leadNotifyOutboxItem{TenantID: 1, ClueID: "clue-2", EventID: "event-2", Recipient: "u-2", Channel: "in_app"})
	s2 := &memoryLeadNotifyOutbox{items: shared}
	got, ok := s2.Get(context.Background(), key)
	if !ok || got.Status != "pending" {
		t.Fatalf("重启后投递意图丢失：ok=%v item=%+v", ok, got)
	}
}

// TestLeadNotifyExternalSummaryMasked 手机/邮箱/聊天敏感片段必须屏蔽，站内详情引用优先且需登录再鉴权。
func TestLeadNotifyExternalSummaryMasked(t *testing.T) {
	raw := "客户 13812345678 邮箱 buyer@example.com 微信 wxid_abc123 咨询报价"
	masked, policy := maskLeadNotifySummary(raw, "telegram")
	if strings.Contains(masked, "13812345678") || strings.Contains(masked, "buyer@example.com") || strings.Contains(masked, "wxid_abc123") {
		t.Fatalf("外部摘要仍含敏感片段：%s", masked)
	}
	if !strings.Contains(policy, "phone") || !strings.Contains(policy, "email") || !strings.Contains(policy, "chat_id") {
		t.Fatalf("未记录实际脱敏策略：%s", policy)
	}
	if !strings.Contains(masked, "/lead-mining") {
		t.Fatalf("应给需登录再鉴权的站内详情引用：%s", masked)
	}
}

// TestLeadNotifyRecipientAuthorization 撤权、跨租户目标与外部负责人失效一律拒发并留原因。
func TestLeadNotifyRecipientAuthorization(t *testing.T) {
	cases := []struct {
		name      string
		target    leadNotifyRecipient
		wantAllow bool
	}{
		{"authorized_inapp", leadNotifyRecipient{TenantID: 1, UserID: 42, Channel: "in_app", Authorized: true}, true},
		{"revoked", leadNotifyRecipient{TenantID: 1, UserID: 42, Channel: "telegram", Authorized: false}, false},
		{"cross_tenant", leadNotifyRecipient{TenantID: 2, UserID: 42, Channel: "telegram", Authorized: true}, false},
		{"invalid_owner", leadNotifyRecipient{TenantID: 1, UserID: 0, Channel: "telegram", Authorized: true}, false},
	}
	for _, c := range cases {
		allow, reason := checkLeadNotifyRecipient(1, c.target)
		if allow != c.wantAllow || (allow && reason != "") || (!allow && reason == "") {
			t.Errorf("%s: allow=%v reason=%q want=%v", c.name, allow, reason, c.wantAllow)
		}
	}
}
