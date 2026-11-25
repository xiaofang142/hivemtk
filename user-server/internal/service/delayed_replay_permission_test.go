package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func seedReplayAISession(t *testing.T, database *gorm.DB, ctx context.Context, rec *model.DelayedOutboundReply) *model.CustomerSession {
	t.Helper()
	if err := database.AutoMigrate(&model.CustomerSession{}); err != nil {
		t.Fatal(err)
	}
	session := &model.CustomerSession{
		TenantID: rec.TenantID, SessionID: fmt.Sprintf("review-session-%d-%d", rec.ID, time.Now().UnixNano()),
		Platform: model.Platform(rec.Platform), AccountID: rec.AccountID, UserID: rec.SenderID,
		HandlerType: model.HandlerTypeAI, Status: model.SessionStatusAIHandling,
	}
	if err := database.WithContext(ctx).Create(session).Error; err != nil {
		t.Fatal(err)
	}
	return session
}

func TestDelayedReplayPermission_BothPathsRespectHumanAndNewReply(t *testing.T) {
	for _, path := range []string{"主重放", "离线回扫"} {
		for _, scenario := range []string{"合法AI会话", "人工锁", "人工会话", "较新已答", "会话缺失"} {
			t.Run(path+"/"+scenario, func(t *testing.T) {
				svc, _ := newBridgeSentTestService(t)
				setupBridgeWhitelistForTest(t, "douyin")
				ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
				defer svc.Stop(ctx)
				database := svc.eventRepo.GetDB()
				now := time.Now()
				accountID := seedCanonicalBridgeOutboundAccount(t, database, tenant.DefaultTenantID, "douyin", fmt.Sprintf("permission-account-%d", now.UnixNano()))
				until := now.Add(time.Minute)
				rec := &model.DelayedOutboundReply{
					TenantID: tenant.DefaultTenantID, Platform: "douyin",
					AccountID: accountID, ConversationID: "permission-conversation",
					SenderID: fmt.Sprintf("permission-customer-%d", now.UnixNano()), Content: "合成待投答案",
					Kind: model.DelayedKindQuietHours, Status: model.DelayedStatusSending,
					SendAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour),
					ClaimToken: "permission-token", ClaimedAt: &now, LeaseUntil: &until,
				}
				ctx = localBridgeSubmitContextCustomEdition(t, ctx, rec.Platform, rec.AccountID, "text")
				if path == "离线回扫" {
					rec.Status, rec.ClaimToken, rec.ClaimedAt, rec.LeaseUntil = model.DelayedStatusPending, "", nil, nil
				}
				if err := database.WithContext(ctx).Create(rec).Error; err != nil {
					t.Fatal(err)
				}
				if scenario != "会话缺失" {
					session := seedReplayAISession(t, database, ctx, rec)
					if scenario == "人工锁" {
						if err := svc.ingressSvc.LockSessionForHuman(ctx, session.SessionID, "合成接管"); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = svc.ingressSvc.UnlockSessionForHuman(ctx, session.SessionID) })
					}
					if scenario == "人工会话" {
						if err := database.WithContext(ctx).Model(session).Update("handler_type", model.HandlerTypeHuman).Error; err != nil {
							t.Fatal(err)
						}
					}
				}
				if scenario == "较新已答" {
					answered := &model.MessageHub{
						TenantID: rec.TenantID, Platform: rec.Platform, AccountID: rec.AccountID,
						ConversationID: rec.ConversationID, MsgID: "permission-human-answer", Direction: "outbound",
						Status: "delivered", Content: "合成人工已解决", SentAt: now,
					}
					if err := database.WithContext(ctx).Create(answered).Error; err != nil {
						t.Fatal(err)
					}
				}
				releaseReplyClaimForTest(t, fmt.Sprintf("delayed-%d", rec.ID))
				if path == "主重放" {
					useLegacyBridgeFixture(t)
					svc.replayDelayedOutbound(ctx, rec)
				} else {
					previous := GlobalInboxIngressService()
					svc.ingressSvc.outboundAuthorizer = capregistry.AuthorizeBridgeSubmission
					SetGlobalInboxIngressService(svc.ingressSvc)
					t.Cleanup(func() { SetGlobalInboxIngressService(previous) })
					offline := NewBridgeOfflineReplayService().WithDB(database)
					offline.ReplayDelayedOutbound(ctx, rec.Platform, rec.AccountID, 10)
				}
				var count int64
				if err := database.WithContext(ctx).Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				want := int64(0)
				if scenario == "合法AI会话" || scenario == "较新已答" {
					want = 1
				}
				if count != want {
					t.Fatalf("只有合法未接管未被新答案替代的会话可投递：出站%d，期望%d", count, want)
				}
			})
		}
	}
}
