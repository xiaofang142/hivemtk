package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func auditSafetyRow(msgID, status string) *model.MessageHub {
	return &model.MessageHub{
		TenantID: 18, Platform: "douyin", AccountID: "audit-account",
		ConversationID: "audit-conversation", MsgID: msgID,
		Direction: "outbound", Status: status, MsgType: "text",
		Content: "安全回归回复", SentAt: time.Now(),
	}
}

func TestOutboundAuditSafety_ConflictDoesNotReviveTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, status, pushError, incomingStatus string
		attempts                                int
		deleted, claimed                        bool
		extra                                   model.JSONMap
	}{
		{name: "旧重复审计不复活", status: "failed", incomingStatus: "failed", extra: model.JSONMap{"duplicate_of": "original"}},
		{name: "新审计不复活", status: "failed", incomingStatus: "pending", extra: model.JSONMap{"delivery_kind": "audit"}},
		{name: "已尝试失败不自动复活", status: "failed", incomingStatus: "pending", attempts: 3},
		{name: "曾认领失败不自动复活", status: "failed", incomingStatus: "pending", claimed: true},
		{name: "耗尽预算不复活", status: "failed", incomingStatus: "pending", attempts: MaxOutboundPushAttempts, pushError: pushExhaustedError},
		{name: "对账失败不复活", status: "failed", incomingStatus: "pending", pushError: pushReconciledUnknownPrefix + "operator"},
		{name: "未知结果软删不复活", status: "unknown", incomingStatus: "pending", deleted: true, pushError: pushAckUnknownError},
		{name: "送达软删不复活", status: "delivered", incomingStatus: "pending", deleted: true},
		{name: "失败输入不能恢复失败", status: "failed", incomingStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := setupConflictRevivalDB(t)
			repo := &DelayedOutboundRepository{db: g}
			seed := auditSafetyRow("mh:audit-conflict", tc.status)
			seed.Extra, seed.PushAttempts, seed.PushError = tc.extra, tc.attempts, tc.pushError
			if tc.claimed {
				now := time.Now().Add(-time.Minute)
				seed.ClaimedAt = &now
			}
			if err := g.Create(seed).Error; err != nil {
				t.Fatalf("准备终态行失败：%v", err)
			}
			if tc.deleted {
				if err := g.Delete(seed).Error; err != nil {
					t.Fatalf("准备软删行失败：%v", err)
				}
			}
			incoming := auditSafetyRow(seed.MsgID, tc.incomingStatus)
			incoming.Content, incoming.Extra = "不应改写的回复", tc.extra
			got, err := repo.CreateMessageHubIdempotent(context.Background(), incoming)
			if err != nil {
				t.Fatalf("幂等冲突应无错误：%v", err)
			}
			var stored model.MessageHub
			if err := g.Unscoped().First(&stored, seed.ID).Error; err != nil {
				t.Fatalf("读取终态行失败：%v", err)
			}
			if got.ID != 0 || stored.Status != seed.Status || stored.Content != seed.Content ||
				stored.PushAttempts != seed.PushAttempts || stored.PushError != seed.PushError ||
				stored.DeletedAt.Valid != tc.deleted {
				t.Fatalf("终态/审计不得复活、改写或清预算：返回 ID=%d，实际状态=%s 次数=%d 错误=%s 软删=%v",
					got.ID, stored.Status, stored.PushAttempts, stored.PushError, stored.DeletedAt.Valid)
			}
		})
	}
}

func TestOutboundAuditSafety_AllReadPathsExcludeAudit(t *testing.T) {
	for _, path := range []string{"轮询认领", "SSE单行认领", "欠交付补拉", "兼容游标补拉", "超时回收"} {
		t.Run(path, func(t *testing.T) {
			g := setupConflictRevivalDB(t)
			repo := &MessageHubRepository{db: g}
			audits := []*model.MessageHub{
				auditSafetyRow("mh:legacy-audit", "pending"),
				auditSafetyRow("mh:typed-audit", "pending"),
			}
			audits[0].Extra = model.JSONMap{"duplicate_of": "original"}
			audits[1].Extra = model.JSONMap{"delivery_kind": "audit"}
			if path == "超时回收" {
				stale := time.Now().Add(-time.Hour)
				for _, row := range audits {
					row.Status, row.ClaimedAt = "inflight", &stale
				}
			}
			for _, row := range audits {
				if err := g.Create(row).Error; err != nil {
					t.Fatalf("准备审计行失败：%v", err)
				}
			}
			normal := auditSafetyRow("mh:ordinary", "pending")
			if err := g.Create(normal).Error; err != nil {
				t.Fatalf("准备普通待投行失败：%v", err)
			}
			ctx := context.Background()
			var rows []model.MessageHub
			var err error
			switch path {
			case "轮询认领", "超时回收":
				rows, err = repo.ClaimPendingOutbound(ctx, normal.Platform, normal.AccountID, 10, time.Minute)
			case "欠交付补拉":
				rows, err = repo.FetchOutboundUndelivered(ctx, normal.Platform, normal.AccountID, time.Minute, 10)
			case "兼容游标补拉":
				rows, err = repo.FetchOutboundSince(ctx, normal.Platform, normal.AccountID, 0, 10)
			case "SSE单行认领":
				for _, row := range audits {
					ok, e := repo.ClaimOutboundForPush(ctx, uint64(row.ID), time.Minute)
					if e != nil || ok {
						t.Fatalf("SSE 不得认领审计：成功=%v 错误=%v", ok, e)
					}
				}
				ok, e := repo.ClaimOutboundForPush(ctx, uint64(normal.ID), time.Minute)
				if e != nil || !ok {
					t.Fatalf("普通待投仍应可认领：成功=%v 错误=%v", ok, e)
				}
				return
			}
			if err != nil || len(rows) != 1 || rows[0].ID != normal.ID {
				t.Fatalf("只应返回普通待投行，实际数量=%d 错误=%v", len(rows), err)
			}
			for _, row := range audits {
				var stored model.MessageHub
				if err := g.First(&stored, row.ID).Error; err != nil {
					t.Fatalf("读取审计失败：%v", err)
				}
				if stored.Status != row.Status || stored.PushAttempts != 0 {
					t.Fatalf("读路径不得认领或回收审计：状态=%s 次数=%d", stored.Status, stored.PushAttempts)
				}
			}
		})
	}
}

func TestOutboundAuditSafety_FollowRecoveryPreservesBudget(t *testing.T) {
	g := setupConflictRevivalDB(t)
	repo := &MessageHubRepository{db: g}
	for i, tc := range []struct {
		name      string
		attempts  int
		pushError string
		extra     model.JSONMap
	}{
		{name: "正常限额恢复", attempts: 7, pushError: "platform_limit"},
		{name: "旧审计", extra: model.JSONMap{"duplicate_of": "original"}},
		{name: "新审计", extra: model.JSONMap{"delivery_kind": "audit"}},
		{name: "预算耗尽", attempts: MaxOutboundPushAttempts, pushError: pushExhaustedError},
		{name: "未知对账失败", pushError: pushReconciledUnknownPrefix + "operator"},
	} {
		row := auditSafetyRow(fmt.Sprintf("mh:follow-safety-%d", i), "send_failed")
		row.PushAttempts, row.PushError, row.Extra = tc.attempts, tc.pushError, tc.extra
		if err := g.Create(row).Error; err != nil {
			t.Fatalf("准备%s失败：%v", tc.name, err)
		}
	}
	n, err := repo.ReviveSendFailedByConversation(context.Background(), "douyin", "audit-account", "audit-conversation")
	if err != nil || n != 1 {
		t.Fatalf("关注仅可恢复一条合法限额失败，实际=%d 错误=%v", n, err)
	}
	var normal model.MessageHub
	if err := g.Where("msg_id = ?", "mh:follow-safety-0").First(&normal).Error; err != nil {
		t.Fatalf("读取限额恢复行失败：%v", err)
	}
	if normal.Status != "pending" || normal.PushAttempts != 7 {
		t.Fatalf("关注恢复不得赠送预算：状态=%s 已用=%d", normal.Status, normal.PushAttempts)
	}
}

func TestOutboundAuditSafety_AuditWriteIsImmutableAndTenantScoped(t *testing.T) {
	g := setupConflictRevivalDB(t)
	repo := &DelayedOutboundRepository{db: g}
	ctx := context.Background()
	orphan := auditSafetyRow("mh:audit-orphan", "pending")
	orphan.TenantID = 0
	if _, err := repo.CreateMessageHubAudit(ctx, orphan); err == nil {
		t.Fatal("无租户审计必须拒绝落库")
	}
	first := auditSafetyRow("mh:immutable-audit", "pending")
	first.TenantID = 0
	got, err := repo.CreateMessageHubAudit(tenant.WithTenantID(ctx, 18), first)
	if err != nil || got.ID == 0 || got.TenantID != 18 {
		t.Fatalf("审计应继承租户并落库：行=%v 错误=%v", got, err)
	}
	duplicate := auditSafetyRow(first.MsgID, "pending")
	duplicate.Content = "不应覆盖原审计"
	if _, err := repo.CreateMessageHubAudit(ctx, duplicate); err != nil {
		t.Fatalf("重复审计应幂等：%v", err)
	}
	var stored model.MessageHub
	if err := g.First(&stored, first.ID).Error; err != nil {
		t.Fatalf("读取审计失败：%v", err)
	}
	if stored.Content != first.Content || stored.Status != "failed" || stored.Extra["delivery_kind"] != "audit" {
		t.Fatal("重复审计不得改写原内容或失去不可投递标识")
	}
	otherTenant := auditSafetyRow(first.MsgID, "pending")
	otherTenant.TenantID = 19
	other, err := repo.CreateMessageHubAudit(ctx, otherTenant)
	if err != nil || other.ID == 0 || other.ID == first.ID {
		t.Fatalf("不同租户同键应独立审计：行=%v 错误=%v", other, err)
	}
	if _, err := repo.CreateMessageHubAudit(ctx, nil); err == nil {
		t.Fatal("空审计消息必须拒绝")
	}
}
