package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
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

// TestLeadNotifyOutboxIdempotency 同一 event/recipient/channel 在真实 PG 中只占一个 durable 意图。
func TestLeadNotifyOutboxIdempotency(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	store := repository.NewLeadNotifyRepository(db)
	key := leadNotifyIdempotencyKey(11, "clue-1", "event-1", "42", "telegram")
	first := &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: key, ClueID: "clue-1", EventID: "event-1",
		Recipient: "42", Channel: "telegram", NextAttemptAt: time.Now().UTC(),
	}
	created, err := store.Enqueue(ctx, first)
	if err != nil || !created || first.Status != model.LeadNotifyStatusPending {
		t.Fatalf("首次入队应为 pending，实际 item=%+v created=%v err=%v", first, created, err)
	}
	duplicate := &model.LeadNotifyOutbox{TenantID: 11, IdempotencyKey: key}
	created, err = store.Enqueue(ctx, duplicate)
	if err != nil || created || duplicate.ID != first.ID || duplicate.Status != model.LeadNotifyStatusPending {
		t.Fatalf("重复事件不得新建投递意图：created=%v item=%+v err=%v", created, duplicate, err)
	}
	rows, err := store.ListOutboxByEvent(ctx, 11, "event-1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("幂等键重复占用了 %d 条记录，err=%v", len(rows), err)
	}
}

// TestLeadNotifyOutboxBackoffAndManualRetry 真实仓储按退避进入 retry/exhausted，人工重试重置 pending；回执丢失单独标记。
func TestLeadNotifyOutboxBackoffAndManualRetry(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	store := repository.NewLeadNotifyRepository(db)
	now := time.Now().UTC()
	item := &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: "backoff-key", ClueID: "clue-1", EventID: "event-1",
		Recipient: "42", Channel: "in_app", NextAttemptAt: now, MaxAttempts: 2,
	}
	if _, err := store.Enqueue(ctx, item); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	claimed, ok, err := store.ClaimByIDForDelivery(ctx, item.ID, now)
	if err != nil || !ok {
		t.Fatalf("首次认领失败：ok=%v err=%v", ok, err)
	}
	next := now.Add(time.Minute)
	if err := store.ScheduleRetry(ctx, item.ID, claimed.ClaimToken, next, "timeout"); err != nil {
		t.Fatalf("退避重试失败：%v", err)
	}
	got, err := store.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusRetry || got.NextAttemptAt.Before(next) {
		t.Fatalf("失败应按退避进入 retry：got=%+v err=%v", got, err)
	}
	rows, err := store.ClaimDueForUpdate(tenant.WithSystemScope(context.Background()), next, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("到期重试认领失败：rows=%d err=%v", len(rows), err)
	}
	claimed = rows[0]
	if err := store.ScheduleRetry(ctx, item.ID, claimed.ClaimToken, next.Add(time.Minute), "timeout"); err != nil {
		t.Fatalf("耗尽重试失败：%v", err)
	}
	got, err = store.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusExhausted {
		t.Fatalf("超过最大重试应进入 exhausted：got=%+v err=%v", got, err)
	}
	if err := store.RetryManually(ctx, item.ID); err != nil {
		t.Fatalf("人工重试失败：%v", err)
	}
	got, err = store.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusPending || got.Attempts != 0 {
		t.Fatalf("人工重试应重置为 pending：got=%+v err=%v", got, err)
	}
	claimed, ok, err = store.ClaimByIDForDelivery(ctx, item.ID, time.Now().UTC())
	if err != nil || !ok {
		t.Fatalf("人工重试后认领失败：ok=%v err=%v", ok, err)
	}
	if err := store.MarkReceiptUnknown(ctx, item.ID, claimed.ClaimToken, "receipt lost"); err != nil {
		t.Fatalf("标记回执丢失失败：%v", err)
	}
	got, err = store.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusReceiptUnknown {
		t.Fatalf("回执丢失必须单独标记：got=%+v err=%v", got, err)
	}
}

// TestLeadNotifyOutboxSurvivesRebuild 重建 repository 后，真实 PG 中的投递意图仍可恢复。
func TestLeadNotifyOutboxSurvivesRebuild(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	key := leadNotifyIdempotencyKey(11, "clue-2", "event-2", "42", "in_app")
	if _, err := repository.NewLeadNotifyRepository(db).Enqueue(ctx, &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: key, ClueID: "clue-2", EventID: "event-2",
		Recipient: "42", Channel: "in_app", NextAttemptAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	rebuilt := repository.NewLeadNotifyRepository(db)
	rows, err := rebuilt.ListOutboxByEvent(ctx, 11, "event-2")
	if err != nil || len(rows) != 1 || rows[0].Status != model.LeadNotifyStatusPending {
		t.Fatalf("重建后投递意图丢失：rows=%+v err=%v", rows, err)
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
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	return tenant.WithTenantID(context.Background(), 11)
}

func newLeadNotifyDeliveryTestService(t *testing.T, store *leadNotifyRouteStoreStub) (*LeadNotifyService, *repository.LeadNotifyRepository, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{}, &model.SystemUser{},
		&model.ChannelAccount{}, &model.Channel{},
	)
	outbox := repository.NewLeadNotifyRepository(db)
	svc := NewLeadNotifyServiceWithDeps(
		store,
		nil,
		repository.NewLeadNotifyUserRepository(db),
		outbox,
		&durableLeadNotifySender{},
	)
	return svc, outbox, db
}

// TestLeadNotifyNotifyOwner_NoRouteFails 三级路由与账号负责人都缺席时必须报错，并把 no_route 写入独立历史。
func TestLeadNotifyNotifyOwner_NoRouteFails(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{}
	svc, outbox, _ := newLeadNotifyDeliveryTestService(t, store)

	err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-no-route"}, "customer-no-route")
	if err == nil || !strings.Contains(err.Error(), "无可用通知路由") {
		t.Fatalf("无路由应返回明确错误，实际 err=%v", err)
	}
	if store.appendCalls != 0 {
		t.Fatalf("独立历史模式下不应写路由 JSONB，实际调用 %d 次", store.appendCalls)
	}
	rows, err := outbox.ListDeliveries(ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-no-route"})
	if err != nil || len(rows) != 1 || rows[0].Status != model.LeadNotifyStatusNoRoute || rows[0].Success {
		t.Fatalf("无路由必须落独立持久审计：rows=%+v err=%v", rows, err)
	}
}

// TestLeadNotifyNotifyOwner_NoRecipientsFailsAndRecords 空收件人必须把 no_recipient 写入独立历史并报错。
func TestLeadNotifyNotifyOwner_NoRecipientsFailsAndRecords(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{routes: []model.LeadMiningNotifyRoute{{
		RouteID: "route-no-recipient", Scope: "tenant", Enabled: true,
	}}}
	svc, outbox, _ := newLeadNotifyDeliveryTestService(t, store)

	err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-no-recipient"}, "customer-no-recipient")
	if err == nil {
		t.Fatal("空收件人不得返回成功")
	}
	if store.appendCalls != 0 {
		t.Fatalf("独立历史模式下不应写路由 JSONB，实际调用 %d 次", store.appendCalls)
	}
	rows, err := outbox.ListDeliveries(ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-no-recipient"})
	if err != nil || len(rows) != 1 || rows[0].Status != model.LeadNotifyStatusNoRecipient || rows[0].Success || !strings.Contains(rows[0].Error, "no_recipient") {
		t.Fatalf("空收件人应留下 no_recipient 独立历史，实际 rows=%+v err=%v", rows, err)
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

// TestLeadNotifyNotifyOwner_RouteLedgerFailureDoesNotBlockDurableDelivery 路由 JSONB 写失败不得阻断独立 outbox/历史投递。
func TestLeadNotifyNotifyOwner_RouteLedgerFailureDoesNotBlockDurableDelivery(t *testing.T) {
	ctx := leadNotifyTestContext(t)
	store := &leadNotifyRouteStoreStub{
		routes: []model.LeadMiningNotifyRoute{{
			RouteID: "route-independent-ledger", Scope: "tenant", Enabled: true,
			NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp},
		}},
		appendErr: gorm.ErrRecordNotFound,
	}
	svc, outbox, db := newLeadNotifyDeliveryTestService(t, store)
	user := &model.SystemUser{
		TenantID: 11, ID: 42, Username: "notify-ledger", Password: "x",
		Role: model.SystemUserRoleUser, Status: 1, Enabled: true, DataScope: model.DataScopeSelf,
	}
	if err := db.Session(&gorm.Session{SkipHooks: true}).WithContext(ctx).Create(user).Error; err != nil {
		t.Fatalf("创建系统用户失败：%v", err)
	}

	if err := svc.NotifyOwner(ctx, ArchiveClueInput{ClueID: "clue-fallback"}, "customer-fallback"); err != nil {
		t.Fatalf("路由 JSONB 已不是权威台账，不得阻断耐久投递：%v", err)
	}
	if store.appendCalls != 0 {
		t.Fatalf("生产投递不得再写路由 JSONB，实际调用 %d 次", store.appendCalls)
	}
	rows, err := outbox.ListDeliveries(ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-fallback"})
	if err != nil || len(rows) != 1 || !rows[0].Success || rows[0].RouteID != "route-independent-ledger" {
		t.Fatalf("独立送达历史不正确：rows=%+v err=%v", rows, err)
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
