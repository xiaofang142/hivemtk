package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestDelayedTerminalFence_RequiresCurrentExecution(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	for _, operation := range []struct {
		name string
		call func(*DelayedOutboundRepository, context.Context, uint, string, time.Time) error
	}{
		{name: "完成", call: func(r *DelayedOutboundRepository, ctx context.Context, id uint, token string, now time.Time) error {
			return r.MarkSent(ctx, id, token, now)
		}},
		{name: "改排", call: func(r *DelayedOutboundRepository, ctx context.Context, id uint, token string, now time.Time) error {
			return r.ScheduleRetry(ctx, id, token, now.Add(time.Minute), "合成失败")
		}},
		{name: "失败收口", call: func(r *DelayedOutboundRepository, ctx context.Context, id uint, token string, now time.Time) error {
			return r.FinishReplay(ctx, id, token, model.DelayedStatusFailed, now, "合成失败")
		}},
	} {
		for _, scenario := range []string{"合法", "未知", "过期", "缺令牌", "缺认领时间", "缺上下文", "系统豁免", "已取消", "默认租户不能读18"} {
			t.Run(operation.name+"/"+scenario, func(t *testing.T) {
				database := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
				repo := NewDelayedOutboundRepository(database)
				now := time.Now()
				until := now.Add(time.Minute)
				rec := &model.DelayedOutboundReply{TenantID: 18, Platform: "xiaohongshu", AccountID: "fence-account", ConversationID: "fence-conversation", SenderID: "fence-customer", Content: "合成待投答案", Status: model.DelayedStatusSending, ClaimToken: "fence-token", ClaimedAt: &now, LeaseUntil: &until, Attempts: 2}
				switch scenario {
				case "未知":
					rec.Status = model.DelayedStatusUnknown
				case "过期":
					expired := now.Add(-time.Second)
					rec.LeaseUntil = &expired
				case "缺认领时间":
					rec.ClaimedAt = nil
				}
				if err := database.Create(rec).Error; err != nil {
					t.Fatal(err)
				}
				ctx := tenant.OnlyTenant(context.Background(), 18)
				token := rec.ClaimToken
				switch scenario {
				case "缺令牌":
					token = ""
				case "缺上下文":
					ctx = nil
				case "系统豁免":
					ctx = tenant.WithSystemScope(ctx)
				case "已取消":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "默认租户不能读18":
					ctx = context.Background()
				}
				err := operation.call(repo, ctx, rec.ID, token, now)
				if (err == nil) != (scenario == "合法") {
					t.Errorf("转换许可错误：%v", err)
				}
				after := readReply(t, database, rec.ID)
				if scenario != "合法" && (after.Status != rec.Status || after.Attempts != rec.Attempts || after.SentAt != nil) {
					t.Fatalf("被拒执行仍修改任务：%s / %d / %v", after.Status, after.Attempts, after.SentAt)
				}
				if scenario == "合法" && operation.name == "失败收口" && after.SentAt != nil {
					t.Fatal("失败不能写成已发送时间")
				}
			})
		}
	}
}

func TestDelayedTerminalFence_UnknownCannotBeOverwritten(t *testing.T) {
	database := testutil.NewTestDB(t, &model.DelayedOutboundReply{})
	repo := NewDelayedOutboundRepository(database)
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	now := time.Now()
	until := now.Add(time.Minute)
	rec := &model.DelayedOutboundReply{TenantID: tenant.DefaultTenantID, Status: model.DelayedStatusSending, ClaimToken: "old-execution", ClaimedAt: &now, LeaseUntil: &until}
	if err := database.WithContext(ctx).Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSendingUnknown(ctx, rec.ID, rec.ClaimToken, "合成结果未知"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkSent(ctx, rec.ID, rec.ClaimToken, now); err == nil {
		t.Fatal("unknown不能被同一旧令牌覆盖")
	}
	if after := readReply(t, database, rec.ID); after.Status != model.DelayedStatusUnknown {
		t.Fatal("核查状态必须保留")
	}
}
