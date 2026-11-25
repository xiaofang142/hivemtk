package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestQuietReplay_OnlyConfirmedHandoffIsSent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       string
		account    string
		duplicate  bool
		longConv   bool
		staleToken bool
		want       string
	}{
		{name: "已入出库队列", kind: model.DelayedKindQuietHours, account: "quiet-test-account", want: model.DelayedStatusSent},
		{name: "确定不可达", kind: model.DelayedKindQuietHours, account: "douyin-unknown", want: model.DelayedStatusFailed},
		{name: "旧空类别也不能伪报成功", account: "douyin-unknown", want: model.DelayedStatusFailed},
		{name: "重复认领没有投递事实", kind: model.DelayedKindQuietHours, account: "quiet-test-account", duplicate: true, want: model.DelayedStatusUnknown},
		{name: "可重试写库错误也不增加首发重投", kind: model.DelayedKindQuietHours, account: "quiet-test-account", longConv: true, want: model.DelayedStatusFailed},
		{name: "旧租约不能覆盖当前认领", kind: model.DelayedKindQuietHours, account: "quiet-test-account", staleToken: true, want: model.DelayedStatusSending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newBridgeSentTestService(t)
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			defer svc.Stop(ctx)
			database := svc.eventRepo.GetDB()
			account := tc.account
			if account == "quiet-test-account" {
				account = seedCanonicalOutboundAccount(t, database, tenant.DefaultTenantID, "douyin", account)
			}
			id := uint(900_000_000 + time.Now().UnixNano()%90_000_000)
			now := time.Now()
			lease := now.Add(time.Minute)
			rec := &DelayedOutboundReply{
				ID: id, TenantID: tenant.DefaultTenantID, Platform: "douyin",
				AccountID: account, ConversationID: fmt.Sprintf("quiet-conv-%d", id),
				SenderID: fmt.Sprintf("quiet-customer-%d", id), Content: "合成夜间回复，不发送真实消息",
				SendAt: now.Add(time.Hour), CreatedAt: now, Status: model.DelayedStatusSending,
				Kind: tc.kind, ClaimToken: fmt.Sprintf("quiet-token-%d", id), ClaimedAt: &now, LeaseUntil: &lease,
			}
			useLegacyBridgeFixture(t)
			ctx = localBridgeSubmitContext(t, ctx, rec.Platform, rec.AccountID, "text")
			if tc.longConv {
				rec.ConversationID = strings.Repeat("q", 119)
			}
			if err := database.WithContext(ctx).Create(rec).Error; err != nil {
				t.Fatal(err)
			}
			seedReplayAISession(t, database, ctx, rec)
			if tc.staleToken {
				// 数据库仍由当前token持有，传给旧worker的是过时副本。
				rec.ClaimToken = "obsolete-quiet-token"
			}
			// 默认值会把空kind补齐；明确复现历史空值行。
			if tc.kind == "" {
				if err := database.WithContext(ctx).Model(rec).Update("kind", "").Error; err != nil {
					t.Fatal(err)
				}
				rec.Kind = ""
			}
			eventID := fmt.Sprintf("delayed-%d", id)
			releaseReplyClaimForTest(t, eventID)
			if tc.duplicate && !agent_runtime.ClaimReply(eventID) {
				t.Fatal("重复认领前置必须成立")
			}
			svc.replayDelayedOutbound(ctx, rec)
			var got DelayedOutboundReply
			if err := database.WithContext(ctx).First(&got, id).Error; err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("没有成功事实不能标已发送：得到%s，期望%s", got.Status, tc.want)
			}
			if got.Attempts != 0 {
				t.Fatal("夜间一次首发不能增加自动重试预算")
			}
			if (tc.want == model.DelayedStatusFailed || tc.want == model.DelayedStatusUnknown) && got.LastError == "" {
				t.Fatal("非成功结果必须留下可核对原因")
			}
			if tc.staleToken && (got.SentAt != nil || got.ClaimToken == rec.ClaimToken) {
				t.Fatal("过期worker不得改写当前token或成功时间")
			}
			var retries int64
			if err := database.WithContext(ctx).Model(&DelayedOutboundReply{}).Where("kind = ?", model.DelayedKindSendRetry).Count(&retries).Error; err != nil {
				t.Fatal(err)
			}
			if retries != 0 {
				t.Fatal("免打扰首发失败或未知不能创建新的自动重投")
			}
		})
	}
}
