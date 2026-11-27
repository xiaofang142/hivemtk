package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type durableLeadNotifySender struct {
	mu    sync.Mutex
	calls []*model.LeadNotifyOutbox
	fn    func(*model.LeadNotifyOutbox) error
}

func (s *durableLeadNotifySender) Send(_ context.Context, item *model.LeadNotifyOutbox) error {
	s.mu.Lock()
	copyItem := *item
	s.calls = append(s.calls, &copyItem)
	s.mu.Unlock()
	if s.fn != nil {
		return s.fn(item)
	}
	return nil
}

func (s *durableLeadNotifySender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type durableLeadNotifyFixture struct {
	ctx      context.Context
	db       *gorm.DB
	outbox   *repository.LeadNotifyRepository
	users    *repository.LeadNotifyUserRepository
	accounts repository.ChannelAccountRepository
	sender   *durableLeadNotifySender
	svc      *LeadNotifyService
	worker   *LeadNotifyWorker
}

func newDurableLeadNotifyFixture(t *testing.T) *durableLeadNotifyFixture {
	t.Helper()
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctx := tenant.WithTenantID(context.Background(), 11)
	db := testutil.NewTestDB(t,
		&model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{}, &model.LeadMiningNotifyRoute{},
		&model.SystemUser{}, &model.ChannelAccount{}, &model.Channel{},
	)
	outbox := repository.NewLeadNotifyRepository(db)
	users := repository.NewLeadNotifyUserRepository(db)
	accounts := repository.NewChannelAccountRepositoryWithDB(db)
	sender := &durableLeadNotifySender{}
	svc := NewLeadNotifyServiceWithDeps(repository.NewLeadMiningNotifyRouteRepository(), accounts, users, outbox, sender)
	return &durableLeadNotifyFixture{
		ctx: ctx, db: db, outbox: outbox, users: users, accounts: accounts,
		sender: sender, svc: svc, worker: NewLeadNotifyWorker(svc),
	}
}

func (f *durableLeadNotifyFixture) addUser(t *testing.T, id uint, tenantID uint, enabled bool, status int) {
	t.Helper()
	row := &model.SystemUser{TenantID: tenantID, ID: id, Username: fmt.Sprintf("notify-%d-%d", tenantID, id), Password: "x", Role: model.SystemUserRoleUser, Status: status, Enabled: enabled, DataScope: model.DataScopeSelf}
	if err := f.db.Session(&gorm.Session{SkipHooks: true}).WithContext(tenant.OnlyTenant(f.ctx, tenantID)).Create(row).Error; err != nil {
		t.Fatalf("创建系统用户失败：%v", err)
	}
}

func (f *durableLeadNotifyFixture) addRoute(t *testing.T, route *model.LeadMiningNotifyRoute) {
	t.Helper()
	route.TenantID = 11
	if err := f.db.WithContext(f.ctx).Create(route).Error; err != nil {
		t.Fatalf("创建通知路由失败：%v", err)
	}
}

func (f *durableLeadNotifyFixture) histories(t *testing.T, clueID string) []*model.LeadNotifyDelivery {
	t.Helper()
	rows, err := f.svc.ListDeliveries(f.ctx, model.LeadNotifyDeliveryQuery{ClueID: clueID, Limit: 50})
	if err != nil {
		t.Fatalf("查询独立历史失败：%v", err)
	}
	return rows
}

// TestLeadNotifyDurableNoRouteAndNoRecipientArePersisted 无路由/空收件人必须留独立持久审计。
func TestLeadNotifyDurableNoRouteAndNoRecipientArePersisted(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-no-route"}, "customer-no-route"); err == nil {
		t.Fatal("无路由不得返回成功")
	}
	rows := f.histories(t, "clue-no-route")
	if len(rows) != 1 || rows[0].Status != model.LeadNotifyStatusNoRoute || rows[0].Success {
		t.Fatalf("无路由审计不正确：%+v", rows)
	}

	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-empty", Scope: "tenant", NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-no-recipient"}, "customer-no-recipient"); err == nil {
		t.Fatal("空收件人不得返回成功")
	}
	rows = f.histories(t, "clue-no-recipient")
	if len(rows) != 1 || rows[0].Status != model.LeadNotifyStatusNoRecipient || rows[0].Success {
		t.Fatalf("空收件人审计不正确：%+v", rows)
	}
}

// TestLeadNotifyDurableIdempotentImmediateAndWorkerSharePath 重复事件只入队/发送一次，worker 不得二次投递。
func TestLeadNotifyDurableIdempotentImmediateAndWorkerSharePath(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	f.addUser(t, 42, 11, true, 1)
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-idem", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	input := ArchiveClueInput{ClueID: "clue-idem", Summary: "same event"}
	if err := f.svc.NotifyOwner(f.ctx, input, "customer-idem"); err != nil {
		t.Fatalf("首次通知失败：%v", err)
	}
	if err := f.svc.NotifyOwner(f.ctx, input, "customer-idem"); err != nil {
		t.Fatalf("重复通知应幂等成功：%v", err)
	}
	if _, err := f.worker.RunOnce(f.ctx); err != nil {
		t.Fatalf("worker 扫描失败：%v", err)
	}
	if f.sender.count() != 1 {
		t.Fatalf("即时路径与 worker 重复发送，calls=%d", f.sender.count())
	}
	items, err := f.outbox.ListOutboxByEvent(f.ctx, 11, "clue-idem")
	if err != nil || len(items) != 1 || items[0].Status != model.LeadNotifyStatusDelivered {
		t.Fatalf("幂等 outbox 状态不正确：items=%+v err=%v", items, err)
	}
	rows := f.histories(t, "clue-idem")
	if len(rows) != 1 || !rows[0].Success || rows[0].Status != model.LeadNotifyStatusDelivered {
		t.Fatalf("独立历史不正确：%+v", rows)
	}
}

// TestLeadNotifyDurableRestartRecoversRetryFromPostgres 重建 service/worker 后从 PG 恢复重试并完成投递。
func TestLeadNotifyDurableRestartRecoversRetryFromPostgres(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	f.addUser(t, 42, 11, true, 1)
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-restart", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	f.sender.fn = func(*model.LeadNotifyOutbox) error { return errors.New("temporary send failure") }
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-restart"}, "customer-restart"); err == nil {
		t.Fatal("首次全部失败应返回错误")
	}
	items, err := f.outbox.ListOutboxByEvent(f.ctx, 11, "clue-restart")
	if err != nil || len(items) != 1 || items[0].Status != model.LeadNotifyStatusRetry {
		t.Fatalf("失败意图应持久为 retry：items=%+v err=%v", items, err)
	}
	if err := f.db.WithContext(f.ctx).Model(&model.LeadNotifyOutbox{}).Where("id = ?", items[0].ID).Update("next_attempt_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("推进重试时间失败：%v", err)
	}

	restartedSender := &durableLeadNotifySender{}
	restarted := NewLeadNotifyServiceWithDeps(repository.NewLeadMiningNotifyRouteRepository(), f.accounts, f.users, repository.NewLeadNotifyRepository(f.db), restartedSender)
	if _, err := NewLeadNotifyWorker(restarted).RunOnce(f.ctx); err != nil {
		t.Fatalf("重建 worker 恢复失败：%v", err)
	}
	if restartedSender.count() != 1 {
		t.Fatalf("重建后应只投递一次，calls=%d", restartedSender.count())
	}
	items, err = restarted.ListOutboxByEvent(f.ctx, 11, "clue-restart")
	if err != nil || len(items) != 1 || items[0].Status != model.LeadNotifyStatusDelivered {
		t.Fatalf("重建后终态不正确：items=%+v err=%v", items, err)
	}
}

// TestLeadNotifyDurableReauthorizesRecipientBeforeRetry 撤权/禁用/跨租户收件人在重试前必须 fail-closed。
func TestLeadNotifyDurableReauthorizesRecipientBeforeRetry(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	f.addUser(t, 42, 11, true, 1)
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-reauth", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	f.sender.fn = func(*model.LeadNotifyOutbox) error { return errors.New("temporary send failure") }
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-reauth"}, "customer-reauth"); err == nil {
		t.Fatal("首次全部失败应返回错误")
	}
	items, err := f.outbox.ListOutboxByEvent(f.ctx, 11, "clue-reauth")
	if err != nil || len(items) != 1 {
		t.Fatalf("读取重试意图失败：items=%+v err=%v", items, err)
	}
	if err := f.db.WithContext(f.ctx).Model(&model.LeadNotifyOutbox{}).Where("id = ?", items[0].ID).Update("next_attempt_at", time.Now().UTC()).Error; err != nil {
		t.Fatalf("推进重试时间失败：%v", err)
	}
	if err := f.db.WithContext(f.ctx).Model(&model.SystemUser{}).Where("id = ?", 42).Updates(map[string]any{"enabled": false, "status": 0}).Error; err != nil {
		t.Fatalf("撤销收件人失败：%v", err)
	}
	if _, err := f.worker.RunOnce(f.ctx); err != nil {
		t.Fatalf("worker 扫描失败：%v", err)
	}
	if f.sender.count() != 1 {
		t.Fatalf("撤权后不得再次发送，calls=%d", f.sender.count())
	}
	items, err = f.outbox.ListOutboxByEvent(f.ctx, 11, "clue-reauth")
	if err != nil || items[0].Status != model.LeadNotifyStatusExhausted {
		t.Fatalf("撤权意图应 exhausted：items=%+v err=%v", items, err)
	}
	rows := f.histories(t, "clue-reauth")
	if len(rows) != 2 {
		t.Fatalf("撤权历史应有首次失败与授权拒绝两条：%+v", rows)
	}
	seenRetry, seenDenied := false, false
	for _, row := range rows {
		if row.Success {
			t.Fatalf("撤权历史不得出现成功记录：%+v", row)
		}
		switch row.Status {
		case model.LeadNotifyStatusRetry:
			seenRetry = true
		case "authorization_denied":
			seenDenied = true
		}
	}
	if !seenRetry || !seenDenied {
		t.Fatalf("撤权历史状态不正确：%+v", rows)
	}
}

// TestLeadNotifyDurableCrossTenantRecipientAndDisabledExternalAccountFailClosed 跨租户员工与禁用渠道账号均不得外发。
func TestLeadNotifyDurableCrossTenantRecipientAndDisabledExternalAccountFailClosed(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	f.addUser(t, 42, 22, true, 1)
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-cross", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-cross"}, "customer-cross"); err == nil {
		t.Fatal("跨租户收件人不得成功")
	}
	rows := f.histories(t, "clue-cross")
	if len(rows) != 1 || rows[0].Success || rows[0].Status != "authorization_denied" || f.sender.count() != 0 {
		t.Fatalf("跨租户未 fail-closed：rows=%+v calls=%d", rows, f.sender.count())
	}

	channel := &model.Channel{TenantID: 11, Name: "telegram", Code: "telegram", Type: "dm", Enabled: true}
	if err := f.db.WithContext(f.ctx).Create(channel).Error; err != nil {
		t.Fatalf("创建渠道失败：%v", err)
	}
	account := &model.ChannelAccount{TenantID: 11, ChannelID: channel.ID, Name: "disabled", Status: 0, DataScope: "{}"}
	if err := f.db.WithContext(f.ctx).Create(account).Error; err != nil {
		t.Fatalf("创建禁用账号失败：%v", err)
	}
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-disabled", Scope: "tenant", NotifyTypes: model.JSONStrings{model.LeadNotifyChannelTelegram}, Target: fmt.Sprintf("%d:123", account.ID)})
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-disabled"}, "customer-disabled"); err == nil {
		t.Fatal("禁用渠道账号不得成功")
	}
	rows = f.histories(t, "clue-disabled")
	if len(rows) != 1 || rows[0].Success || rows[0].Status != "authorization_denied" || f.sender.count() != 0 {
		t.Fatalf("禁用账号未 fail-closed：rows=%+v calls=%d", rows, f.sender.count())
	}
}

// TestLeadNotifyDurableExternalCluePathUsesSameOutbox 另一条线索发现外发路径必须复用同一耐久 outbox/幂等出口。
func TestLeadNotifyDurableExternalCluePathUsesSameOutbox(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	channel := &model.Channel{TenantID: 11, Name: "telegram", Code: "telegram", Type: "dm", Enabled: true}
	if err := f.db.WithContext(f.ctx).Create(channel).Error; err != nil {
		t.Fatalf("创建渠道失败：%v", err)
	}
	account := &model.ChannelAccount{TenantID: 11, ChannelID: channel.ID, Name: "external", Status: 1, DataScope: "{}"}
	if err := f.db.WithContext(f.ctx).Create(account).Error; err != nil {
		t.Fatalf("创建渠道账号失败：%v", err)
	}
	f.addRoute(t, &model.LeadMiningNotifyRoute{
		RouteID: "route-external-clue", Scope: "tenant", NotifyChannel: model.LeadNotifyChannelTelegram,
		Target: fmt.Sprintf("%d:123", account.ID), Enabled: true,
	})
	clue := &model.Clue{TenantID: 11, ID: "clue-external-path", SourceChannel: "telegram", Summary: "external clue"}
	previous := GlobalLeadNotifyService()
	SetGlobalLeadNotifyService(f.svc)
	t.Cleanup(func() { SetGlobalLeadNotifyService(previous) })
	dispatchLeadMiningExternalNotifications(f.ctx, clue)
	dispatchLeadMiningExternalNotifications(f.ctx, clue)
	if f.sender.count() != 1 {
		t.Fatalf("重复外发路径不得重复发送，calls=%d", f.sender.count())
	}
	items, err := f.outbox.ListOutboxByEvent(f.ctx, 11, clue.ID)
	if err != nil || len(items) != 1 || items[0].Status != model.LeadNotifyStatusDelivered {
		t.Fatalf("外部路径未进入同一耐久 outbox：items=%+v err=%v", items, err)
	}
	rows := f.histories(t, clue.ID)
	if len(rows) != 1 || !rows[0].Success || rows[0].Recipient == fmt.Sprintf("%d:123", account.ID) {
		t.Fatalf("外部历史应成功且目标脱敏：%+v", rows)
	}
}
func TestLeadNotifyDurablePartialSuccessSummaryAndReceiptUnknown(t *testing.T) {
	f := newDurableLeadNotifyFixture(t)
	f.addUser(t, 42, 11, true, 1)
	f.addUser(t, 43, 11, true, 1)
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-partial", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42", "43"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	f.sender.fn = func(item *model.LeadNotifyOutbox) error {
		if item.Recipient == "43" {
			return errors.New("send failed")
		}
		return nil
	}
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-partial"}, "customer-partial"); err != nil {
		t.Fatalf("部分成功不得整体报错：%v", err)
	}
	summary, err := f.svc.SummarizeDeliveries(f.ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-partial"})
	if err != nil {
		t.Fatalf("汇总失败：%v", err)
	}
	total, succeeded := 0, 0
	for _, row := range summary {
		total += row.Total
		succeeded += row.Succeeded
	}
	if total != 2 || succeeded != 1 {
		t.Fatalf("部分成功汇总不正确：total=%d succeeded=%d rows=%+v", total, succeeded, summary)
	}

	f.sender.fn = func(*model.LeadNotifyOutbox) error {
		return &LeadNotifyReceiptUnknownError{Reason: "timeout after send"}
	}
	if err := f.db.WithContext(f.ctx).Model(&model.LeadMiningNotifyRoute{}).Where("route_id = ?", "route-partial").Update("enabled", false).Error; err != nil {
		t.Fatalf("关闭部分成功路由失败：%v", err)
	}
	f.addRoute(t, &model.LeadMiningNotifyRoute{RouteID: "route-unknown", Scope: "tenant", NotifyUserIDs: model.JSONStrings{"42"}, NotifyTypes: model.JSONStrings{model.LeadNotifyChannelInApp}})
	if err := f.svc.NotifyOwner(f.ctx, ArchiveClueInput{ClueID: "clue-unknown"}, "customer-unknown"); err == nil {
		t.Fatal("receipt_unknown 首次尝试不得报成功")
	}
	items, err := f.outbox.ListOutboxByEvent(f.ctx, 11, "clue-unknown")
	if err != nil || len(items) != 1 || items[0].Status != model.LeadNotifyStatusReceiptUnknown {
		t.Fatalf("receipt_unknown 状态不正确：items=%+v err=%v", items, err)
	}
	before := f.sender.count()
	if _, err := f.worker.RunOnce(f.ctx); err != nil {
		t.Fatalf("worker 扫描失败：%v", err)
	}
	if f.sender.count() != before {
		t.Fatalf("receipt_unknown 不得自动重发：before=%d after=%d", before, f.sender.count())
	}
}
