package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestReplayLease_InvalidExecutionCannotCreateOutbound(t *testing.T) {
	for _, tc := range []struct {
		name         string
		changeStored func(*DelayedOutboundReply)
		changeCopy   func(*DelayedOutboundReply)
	}{
		{name: "合法租约"},
		{name: "过期令牌", changeCopy: func(r *DelayedOutboundReply) { r.ClaimToken = "obsolete" }},
		{name: "过期租约", changeStored: func(r *DelayedOutboundReply) { past := time.Now().Add(-time.Minute); r.LeaseUntil = &past }},
		{name: "缺期限", changeStored: func(r *DelayedOutboundReply) { r.LeaseUntil = nil }},
		{name: "缺认领时间", changeStored: func(r *DelayedOutboundReply) { r.ClaimedAt = nil }},
		{name: "未认领记录", changeStored: func(r *DelayedOutboundReply) { r.Status = model.DelayedStatusPending }},
		{name: "缺令牌", changeCopy: func(r *DelayedOutboundReply) { r.ClaimToken = "" }},
		{name: "已终态", changeStored: func(r *DelayedOutboundReply) { r.Status = model.DelayedStatusSent }},
		{name: "跨租户副本", changeCopy: func(r *DelayedOutboundReply) { r.TenantID = 19 }},
		{name: "错误账号", changeCopy: func(r *DelayedOutboundReply) { r.AccountID = "other-account" }},
		{name: "错误渠道", changeCopy: func(r *DelayedOutboundReply) { r.Platform = "xiaohongshu" }},
		{name: "错误会话", changeCopy: func(r *DelayedOutboundReply) { r.ConversationID = "other-conversation" }},
		{name: "错误发送者", changeCopy: func(r *DelayedOutboundReply) { r.SenderID = "other-customer" }},
		{name: "陈旧载荷", changeCopy: func(r *DelayedOutboundReply) { r.Content = "陈旧副本，不应发送" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newBridgeSentTestService(t)
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			defer svc.Stop(ctx)
			database := svc.eventRepo.GetDB()
			account := seedCanonicalOutboundAccount(t, database, tenant.DefaultTenantID, "douyin", "lease-test-account")
			now := time.Now()
			until := now.Add(time.Minute)
			id := uint(700_000_000 + time.Now().UnixNano()%90_000_000)
			stored := DelayedOutboundReply{ID: id, TenantID: tenant.DefaultTenantID,
				Platform: "douyin", AccountID: account, ConversationID: fmt.Sprintf("lease-conv-%d", id),
				SenderID: fmt.Sprintf("lease-customer-%d", id), Content: "数据库当前合成答复", Kind: model.DelayedKindQuietHours,
				Status: model.DelayedStatusSending, SendAt: now.Add(time.Hour), CreatedAt: now,
				ClaimToken: fmt.Sprintf("lease-token-%d", id), ClaimedAt: &now, LeaseUntil: &until}
			if tc.changeStored != nil {
				tc.changeStored(&stored)
			}
			if err := database.WithContext(ctx).Create(&stored).Error; err != nil {
				t.Fatal(err)
			}
			seedReplayAISession(t, database, ctx, &stored)
			useLegacyBridgeFixture(t)
			ctx = localBridgeSubmitContext(t, ctx, stored.Platform, stored.AccountID, "text")
			snapshot := stored
			if tc.changeCopy != nil {
				tc.changeCopy(&snapshot)
			}
			releaseReplyClaimForTest(t, fmt.Sprintf("delayed-%d", id))
			svc.replayDelayedOutbound(ctx, &snapshot)
			var out []model.MessageHub
			if err := database.WithContext(ctx).Where("direction = ?", "outbound").Find(&out).Error; err != nil {
				t.Fatal(err)
			}
			valid := tc.name == "合法租约" || tc.name == "陈旧载荷"
			if valid {
				if len(out) != 1 || out[0].Content != stored.Content {
					t.Fatalf("仅当前有效租约的权威载荷可入队：%+v", out)
				}
			} else {
				if len(out) != 0 {
					t.Fatalf("无效执行不得先产生出站：%d条", len(out))
				}
				var got DelayedOutboundReply
				if err := database.WithContext(ctx).First(&got, id).Error; err != nil {
					t.Fatal(err)
				}
				if got.Status != stored.Status || got.ClaimToken != stored.ClaimToken || got.Attempts != stored.Attempts || got.SentAt != nil {
					t.Fatal("拒绝旧执行不得改写当前持有者或终态")
				}
			}
			var retries int64
			if err := database.WithContext(ctx).Model(&DelayedOutboundReply{}).Where("kind = ?", model.DelayedKindSendRetry).Count(&retries).Error; err != nil {
				t.Fatal(err)
			}
			if retries != 0 {
				t.Fatal("拒绝租约不得启动新的自动重试")
			}
		})
	}
}

func TestReplayLease_ReadMustRemainValidAtExecution(t *testing.T) {
	for _, fault := range []bool{false, true} {
		name := "查询期间租约到期"
		if fault {
			name = "租约查询故障"
		}
		t.Run(name, func(t *testing.T) {
			svc, _ := newBridgeSentTestService(t)
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			defer svc.Stop(ctx)
			database := svc.eventRepo.GetDB()
			now := time.Now()
			until := now.Add(time.Minute)
			rec := &DelayedOutboundReply{TenantID: tenant.DefaultTenantID,
				Platform: "xiaohongshu", AccountID: "lease-read-account", ConversationID: "lease-read-conversation",
				SenderID: fmt.Sprintf("lease-read-customer-%d", now.UnixNano()), Content: "合成答复，不能在核验失效后投递",
				Kind: model.DelayedKindQuietHours, Status: model.DelayedStatusSending,
				SendAt: now.Add(time.Hour), ClaimToken: "lease-read-token", ClaimedAt: &now, LeaseUntil: &until}
			if err := database.WithContext(ctx).Create(rec).Error; err != nil {
				t.Fatal(err)
			}
			seedReplayAISession(t, database, ctx, rec)
			calls := 0
			callbackName := "test:replay-lease-read"
			if err := database.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement.Table != "reach_delayed_outbound" {
					return
				}
				calls++
				if fault {
					tx.AddError(errors.New("合成读取故障"))
					return
				}
				// 模拟查询刚返回时租约已到期，不靠延长测试等待或放宽生产预算。
				if current, ok := tx.Statement.Dest.(*model.DelayedOutboundReply); ok {
					expired := time.Now().Add(-time.Nanosecond)
					current.LeaseUntil = &expired
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Callback().Query().Remove(callbackName) })
			releaseReplyClaimForTest(t, fmt.Sprintf("delayed-%d", rec.ID))
			svc.replayDelayedOutbound(ctx, rec)
			if err := database.Callback().Query().Remove(callbackName); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("必须实际经过一次租约查询：%d", calls)
			}
			var count int64
			if err := database.WithContext(ctx).Model(&model.MessageHub{}).Where("direction = ?", "outbound").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatal("读取失败或返回时失效的租约不能创建出站")
			}
			var stored DelayedOutboundReply
			if err := database.WithContext(ctx).First(&stored, rec.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != model.DelayedStatusSending || stored.SentAt != nil || stored.Attempts != 0 {
				t.Fatal("拒绝执行不得改写任务或追加预算")
			}
		})
	}
}
