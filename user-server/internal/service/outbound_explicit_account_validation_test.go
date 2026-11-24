package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

type explicitOutboundFixture struct {
	svc     *WebhookService
	db      *gorm.DB
	channel *model.Channel
	account *model.ChannelAccount
}

func newExplicitOutboundFixture(t *testing.T, tenantID uint, channelCode string) *explicitOutboundFixture {
	t.Helper()
	t.Cleanup(DisableAIQuietHoursForTest(true))
	db := testutil.NewTestDB(t,
		&model.Channel{}, &model.ChannelAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	channel := &model.Channel{TenantID: tenantID, Name: channelCode, Code: channelCode, Type: "dm", Enabled: true}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(channel).Error; err != nil {
		t.Fatalf("写入渠道夹具失败: %v", err)
	}
	account := &model.ChannelAccount{
		TenantID: tenantID, ChannelID: channel.ID, Name: "active-" + channelCode,
		IdentifyCode: fmt.Sprintf("explicit-code-%s-%d", channelCode, tenantID), Status: 1,
		DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(account).Error; err != nil {
		t.Fatalf("写入账号夹具失败: %v", err)
	}
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })
	useLegacyBridgeFixture(t)
	return &explicitOutboundFixture{svc: svc, db: db, channel: channel, account: account}
}

func (f *explicitOutboundFixture) addAccount(t *testing.T, tenantID, channelID uint, name string, status int) *model.ChannelAccount {
	t.Helper()
	account := &model.ChannelAccount{
		TenantID: tenantID, ChannelID: channelID, Name: name,
		IdentifyCode: "explicit-code-" + name, Status: status,
		DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := f.db.WithContext(tenant.WithSystemScope(context.Background())).Create(account).Error; err != nil {
		t.Fatalf("写入额外账号夹具失败: %v", err)
	}
	if status == 0 {
		if err := f.db.Model(&model.ChannelAccount{}).Where("id = ?", account.ID).Update("status", 0).Error; err != nil {
			t.Fatalf("写入禁用状态失败: %v", err)
		}
		account.Status = 0
	}
	return account
}

func (f *explicitOutboundFixture) send(t *testing.T, ctx context.Context, channelCode, accountID, bindingAccountID, conversationID string) (bool, error, int64) {
	t.Helper()
	hub := &model.MessageHub{
		MsgID: "mh-in-" + conversationID, Platform: channelCode, AccountID: bindingAccountID,
		Direction: "inbound", MsgType: "text", SenderID: "customer-" + conversationID,
		Content: "在吗", ConversationID: conversationID, SentAt: time.Now(),
	}
	if tid, ok := tenant.FromContext(ctx); ok {
		hub.TenantID = tid
	}
	if err := f.db.Create(hub).Error; err != nil {
		t.Fatalf("写入入站夹具失败: %v", err)
	}
	eventID := "evt-" + strings.ReplaceAll(t.Name(), "/", "-")
	releaseReplyClaimForTest(t, eventID)
	payload := &ParsedPayload{EventID: eventID, Sender: hub.SenderID, Content: "在吗", ChatID: conversationID}
	if bindingAccountID != "" {
		ctx = localBridgeSubmitContext(t, ctx, channelCode, bindingAccountID, "text")
	}
	sent, sendErr := f.svc.sendOutbound(ctx, WebhookChannel(channelCode), accountID, payload, "AI 回复", hub, nil)
	var count int64
	if err := f.db.Model(&model.MessageHub{}).
		Where("direction = ? AND conversation_id = ?", "outbound", conversationID).
		Count(&count).Error; err != nil {
		t.Fatalf("统计出站行失败: %v", err)
	}
	return sent, sendErr, count
}

func requireExplicitOutboundRejected(t *testing.T, sent bool, sendErr error, count int64) {
	t.Helper()
	if sent || count != 0 {
		t.Fatalf("显式账号无效必须零外发: sent=%v outbox=%d err=%v", sent, count, sendErr)
	}
	if sendErr == nil {
		t.Fatal("显式账号无效必须回传可见错误")
	}
}

// 有效显式账号必须精确使用，即使同渠道还有另一个活跃账号也不能 auto-pick。
func TestSendOutbound_Bridge_ExplicitAccountValidUsesExactAccount(t *testing.T) {
	fixture := newExplicitOutboundFixture(t, 41, "douyin")
	other := fixture.addAccount(t, 41, fixture.channel.ID, "other-active", 1)
	explicitID := strconv.FormatUint(uint64(fixture.account.ID), 10)
	ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
	sent, sendErr, count := fixture.send(t, ctx, "douyin", explicitID, explicitID, "conv-explicit-valid")
	if !sent || sendErr != nil || count != 1 {
		t.Fatalf("有效显式账号应精确出库: sent=%v outbox=%d err=%v", sent, count, sendErr)
	}
	var row model.MessageHub
	if err := fixture.db.Where("direction = ? AND conversation_id = ?", "outbound", "conv-explicit-valid").First(&row).Error; err != nil {
		t.Fatalf("读回显式出站行失败: %v", err)
	}
	if row.AccountID != explicitID || row.AccountID == strconv.FormatUint(uint64(other.ID), 10) {
		t.Fatalf("显式账号被 auto-pick 替换: got=%q want=%q other=%d", row.AccountID, explicitID, other.ID)
	}
}

// 只有空账号才允许自动分派。
func TestSendOutbound_Bridge_EmptyAccountAutoPicks(t *testing.T) {
	fixture := newExplicitOutboundFixture(t, 41, "douyin")
	activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
	ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
	sent, sendErr, count := fixture.send(t, ctx, "douyin", "", activeID, "conv-explicit-empty")
	if !sent || sendErr != nil || count != 1 {
		t.Fatalf("空账号应 auto-pick 后出库: sent=%v outbox=%d err=%v", sent, count, sendErr)
	}
}

func TestSendOutbound_Bridge_ExplicitAccountInvalidFailClosed(t *testing.T) {
	t.Run("not-found", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		missingID := strconv.FormatUint(uint64(fixture.account.ID+1000), 10)
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", missingID, activeID, "conv-explicit-missing")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("disabled", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		disabled := fixture.addAccount(t, 41, fixture.channel.ID, "disabled", 0)
		activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", strconv.FormatUint(uint64(disabled.ID), 10), activeID, "conv-explicit-disabled")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("wrong-channel", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		otherChannel := &model.Channel{TenantID: 41, Name: "weibo", Code: "weibo", Type: "dm", Enabled: true}
		if err := fixture.db.WithContext(tenant.WithSystemScope(context.Background())).Create(otherChannel).Error; err != nil {
			t.Fatalf("写入错渠道夹具失败: %v", err)
		}
		wrong := fixture.addAccount(t, 41, otherChannel.ID, "wrong-channel", 1)
		activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", strconv.FormatUint(uint64(wrong.ID), 10), activeID, "conv-explicit-wrong-channel")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("cross-tenant", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		otherChannel := &model.Channel{TenantID: 42, Name: "kuaishou", Code: "kuaishou", Type: "dm", Enabled: true}
		if err := fixture.db.WithContext(tenant.WithSystemScope(context.Background())).Create(otherChannel).Error; err != nil {
			t.Fatalf("写入跨租户渠道夹具失败: %v", err)
		}
		crossTenant := fixture.addAccount(t, 42, otherChannel.ID, "cross-tenant", 1)
		activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", strconv.FormatUint(uint64(crossTenant.ID), 10), activeID, "conv-explicit-cross-tenant")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("tenant-zero", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		explicitID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		sent, sendErr, count := fixture.send(t, context.Background(), "douyin", explicitID, "", "conv-explicit-tenant-zero")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("edition-denied", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		explicitID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeOpensource)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", explicitID, explicitID, "conv-explicit-edition-denied")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
	})
	t.Run("channel-repository-error-visible", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		explicitID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		queryErr := errors.New("explicit channel repository unavailable")
		const callback = "test:explicit-outbound-channel-query-error"
		if err := fixture.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				tx.AddError(queryErr)
			}
		}); err != nil {
			t.Fatalf("注册渠道查询故障失败: %v", err)
		}
		t.Cleanup(func() { _ = fixture.db.Callback().Query().Remove(callback) })
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", explicitID, explicitID, "conv-explicit-channel-repo-error")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
		if !strings.Contains(sendErr.Error(), queryErr.Error()) {
			t.Fatalf("渠道仓储运行错误被降级/吞掉: want=%q got=%q", queryErr.Error(), sendErr.Error())
		}
	})
	t.Run("repository-error-visible", func(t *testing.T) {
		fixture := newExplicitOutboundFixture(t, 41, "douyin")
		activeID := strconv.FormatUint(uint64(fixture.account.ID), 10)
		queryErr := errors.New("explicit account repository unavailable")
		const callback = "test:explicit-outbound-account-query-error"
		if err := fixture.db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "channel_accounts" {
				tx.AddError(queryErr)
			}
		}); err != nil {
			t.Fatalf("注册账号查询故障失败: %v", err)
		}
		t.Cleanup(func() { _ = fixture.db.Callback().Query().Remove(callback) })
		ctx := edition.WithContext(tenant.WithTenantID(context.Background(), 41), edition.TypeCustom)
		sent, sendErr, count := fixture.send(t, ctx, "douyin", activeID, activeID, "conv-explicit-repo-error")
		requireExplicitOutboundRejected(t, sent, sendErr, count)
		if !strings.Contains(sendErr.Error(), queryErr.Error()) {
			t.Fatalf("仓储运行错误被降级/吞掉: want=%q got=%q", queryErr.Error(), sendErr.Error())
		}
	})
}
