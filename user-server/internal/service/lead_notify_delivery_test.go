package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
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

// leadNotifyRouteStoreStub 只替代生产通知服务的路由读写边界，不模拟发送结果。
type leadNotifyRouteStoreStub struct {
	routes          []model.LeadMiningNotifyRoute
	listErr         error
	appendErr       error
	appendCalls     int
	appendedRouteID string
	appendedRecords string
}

func (s *leadNotifyRouteStoreStub) ListByChannelAccount(_ context.Context, _ uint, _ string) ([]model.LeadMiningNotifyRoute, error) {
	return s.routes, s.listErr
}

func (s *leadNotifyRouteStoreStub) AppendDeliveryRecords(_ context.Context, routeID, records string) error {
	s.appendCalls++
	s.appendedRouteID = routeID
	s.appendedRecords = records
	return s.appendErr
}

func leadNotifyTestContext(t *testing.T) context.Context {
	t.Helper()
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })
	return context.Background()
}

// TestLeadNotifyNotifyOwner_NoRouteFails 三级路由与账号负责人都缺席时必须报错，不能假成功。
func TestLeadNotifyNotifyOwner_NoRouteFails(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{}
	svc := NewLeadNotifyServiceWith(store, nil)

	err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-no-route"}, "customer-no-route")
	if err == nil || !strings.Contains(err.Error(), "无可用通知路由") {
		t.Fatalf("无路由应返回明确错误，实际 err=%v", err)
	}
	if store.appendCalls != 0 {
		t.Fatalf("无路由时不应尝试写入不存在的送达记录，实际调用 %d 次", store.appendCalls)
	}
}

// TestLeadNotifyNotifyOwner_NoRecipientsFailsAndRecords 空收件人必须留失败记录并报错。
func TestLeadNotifyNotifyOwner_NoRecipientsFailsAndRecords(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{routes: []model.LeadMiningNotifyRoute{{
		RouteID: "route-no-recipient", Scope: "tenant", Enabled: true,
	}}}
	svc := NewLeadNotifyServiceWith(store, nil)

	err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-no-recipient"}, "customer-no-recipient")
	if err == nil {
		t.Fatal("空收件人不得返回成功")
	}
	if store.appendCalls != 1 || store.appendedRouteID != "route-no-recipient" {
		t.Fatalf("空收件人结果必须落库：calls=%d route=%q", store.appendCalls, store.appendedRouteID)
	}
	var records []map[string]any
	if rawErr := json.Unmarshal([]byte(store.appendedRecords), &records); rawErr != nil {
		t.Fatalf("送达记录不是合法 JSON：%v raw=%s", rawErr, store.appendedRecords)
	}
	if len(records) != 1 || records[0]["success"] != false || !strings.Contains(fmt.Sprint(records[0]["error"]), "no_recipient") {
		t.Fatalf("空收件人应留下 no_recipient 失败记录，实际 %s", store.appendedRecords)
	}
}

// TestLeadNotifyMarshalDeliveryRecords_FailureVisible 序列化失败必须上抛，不能静默写成空数组。
func TestLeadNotifyMarshalDeliveryRecords_FailureVisible(t *testing.T) {
	raw, err := marshalLeadNotifyDeliveryRecords([]model.JSONPayload{{"bad": math.NaN()}})
	if err == nil {
		t.Fatalf("含 NaN 的送达记录应返回序列化错误，实际 raw=%q", raw)
	}
	if raw != "" {
		t.Fatalf("序列化失败不得返回可落库的空数组，实际 raw=%q", raw)
	}
}

// TestLeadNotifyNotifyOwner_VirtualRouteMissingRecordFails 虚拟账号负责人路由写 0 行必须显式失败。
func TestLeadNotifyNotifyOwner_VirtualRouteMissingRecordFails(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{
		routes: []model.LeadMiningNotifyRoute{{
			RouteID: "account-owner-fallback", Scope: "account", Enabled: true,
			NotifyUserIDs: model.JSONStrings{"not-a-user-id"},
		}},
		appendErr: gorm.ErrRecordNotFound,
	}
	svc := NewLeadNotifyServiceWith(store, nil)

	err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-fallback"}, "customer-fallback")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("虚拟路由送达记录写 0 行应返回 ErrRecordNotFound，实际 err=%v", err)
	}
	if store.appendedRouteID != "account-owner-fallback" {
		t.Fatalf("应写回虚拟账号负责人路由标识，实际 route=%q", store.appendedRouteID)
	}
}

type archiveOwnerNotifierStub struct {
	err   error
	calls int
}

func (n *archiveOwnerNotifierStub) NotifyOwner(_ context.Context, _ ArchiveClueInput, _ string) error {
	n.calls++
	return n.err
}

type archiveCustomerRepoStub struct {
	repository.CustomerRepository
	customer *model.Customer
}

func (r *archiveCustomerRepoStub) GetByID(_ context.Context, _ string) (*model.Customer, error) {
	return r.customer, nil
}

// TestClueArchive_NotifiedReflectsNotifierResult 客户侧归档只在通知出口返回 nil 时标记已通知。
func TestClueArchive_NotifiedReflectsNotifierResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: errors.New("通知写入失败")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := &archiveOwnerNotifierStub{err: tc.err}
			svc := &ClueArchiveService{
				customers: &archiveCustomerRepoStub{customer: &model.Customer{ID: "customer-archive"}},
				notifier:  notifier,
			}
			result, err := svc.ArchiveClueAndLinkCustomer(context.Background(), ArchiveClueInput{
				ClueID: "clue-archive", CustomerID: "customer-archive",
			})
			if err != nil {
				t.Fatalf("通知失败不应回滚已完成的客户归档：%v", err)
			}
			if notifier.calls != 1 {
				t.Fatalf("通知出口应调用一次，实际 %d 次", notifier.calls)
			}
			if result.Notified != (tc.err == nil) {
				t.Fatalf("Notified=%v，通知错误=%v", result.Notified, tc.err)
			}
		})
	}
}

type archiveClueRepoStub struct {
	repository.ClueRepository
	clue        *model.Clue
	updateCalls int
}

func (r *archiveClueRepoStub) GetByID(_ context.Context, _ string) (*model.Clue, error) {
	return r.clue, nil
}

func (r *archiveClueRepoStub) UpdateByID(_ context.Context, _ string, _ map[string]any) error {
	r.updateCalls++
	return nil
}

// TestLeadClueArchive_NotifiedReflectsNotifierResult 线索终态归档后同样按通知错误决定 Notified。
func TestLeadClueArchive_NotifiedReflectsNotifierResult(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: errors.New("通知写入失败")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifier := &archiveOwnerNotifierStub{err: tc.err}
			archive := &ClueArchiveService{
				customers: &archiveCustomerRepoStub{customer: &model.Customer{ID: "customer-lead-archive"}},
				notifier:  notifier,
			}
			clues := &archiveClueRepoStub{clue: &model.Clue{ID: "clue-lead-archive"}}
			svc := NewLeadClueArchiveServiceWith(clues, archive)
			result, err := svc.Archive(context.Background(), LeadClueArchiveInput{
				ClueID: "clue-lead-archive", CustomerID: "customer-lead-archive",
			})
			if err != nil {
				t.Fatalf("通知失败不应回滚已完成的线索归档：%v", err)
			}
			if clues.updateCalls != 1 || notifier.calls != 1 {
				t.Fatalf("归档终态与通知应各执行一次：updates=%d notifications=%d", clues.updateCalls, notifier.calls)
			}
			if result.Notified != (tc.err == nil) {
				t.Fatalf("Notified=%v，通知错误=%v", result.Notified, tc.err)
			}
		})
	}
}
