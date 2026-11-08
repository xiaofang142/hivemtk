// cs05_outbound_tenant_scope_test.go CS-05 刀B：出站认领/确认链的租户越界矩阵。
//
// 审计（2026-11-08）结论：message_hub 出站的账号越界有既有格（AckOutboundScopeIsolation，
// (platform, account_id) 谓词），unknown 对账有租户作用域格（ReconcileUnknownOutbound_TenantScope），
// 但**认领与确认**两条链只有账号维度、没有租户维度格——多租户自建渠道账号撞同一
// account_id 字符串是真实场景（各租户独立授权自己的账号，ID 由各自桥端自报），
// 隔离 callback（on 模式 ctx 未绑租户 fail-closed、绑了只放本租户行）是唯一防线，
// 没有格守着它就等于没有保障。
//
// 本文件钉住三接口 × 跨租户同 account_id：
//
//	① 认领（ClaimPendingOutbound）：ctx=A 只认领 A 的 pending，B 同 account 同 msg 语义的行不可见；
//	② 三值确认（AckOutboundDeliveredDetailed）：ctx=A 的 ack 谓词对 B 行 0 命中（不翻 B 的终态）；
//	③ fenced 确认：同上且 claimed_at 栅栏不因越界而意外匹配。
//
// 预置/回读走 system scope（隔离 callback 对无租户 ctx fail-closed，见 unknown 套件同一惯例）。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const cs05Platform = "cs05_scope"

func cs05SeedOutbound(t *testing.T, db *gorm.DB, msgID, conv, status string, tenantID uint) uint {
	t.Helper()
	row := &model.MessageHub{
		TenantID:       tenantID,
		Platform:       cs05Platform,
		AccountID:      "acc-shared",
		ConversationID: conv,
		MsgID:          msgID,
		MsgType:        "text",
		Content:        "c-" + msgID,
		Direction:      "outbound",
		Status:         status,
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("seed %s 失败: %v", msgID, err)
	}
	return row.ID
}

func TestCS05_OutboundClaimAndAck_TenantScope(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := NewInboxIngressService()
	const (
		tenantA = uint(801)
		tenantB = uint(802)
	)
	ctxA := tenant.WithTenantID(context.Background(), tenantA)
	ctxB := tenant.WithTenantID(context.Background(), tenantB)

	// —— ① 认领租户作用域：A/B 各一条 pending，同 platform/account ——
	cs05SeedOutbound(t, db, "cs05-claim-a", "conv-a", "pending", tenantA)
	cs05SeedOutbound(t, db, "cs05-claim-b", "conv-b", "pending", tenantB)

	claimedA, err := svc.ClaimPendingOutbound(ctxA, cs05Platform, "acc-shared", 10)
	if err != nil {
		t.Fatalf("A 认领失败: %v", err)
	}
	if len(claimedA) != 1 || claimedA[0].MsgID != "cs05-claim-a" {
		t.Fatalf("A 应恰认领本租户 1 条(cs05-claim-a)，实际 %+v", claimedA)
	}
	// B 同参认领：A 已翻 inflight 的行对 B 不可见（inflight 且非本租户），B 只见自己那条。
	claimedB, err := svc.ClaimPendingOutbound(ctxB, cs05Platform, "acc-shared", 10)
	if err != nil {
		t.Fatalf("B 认领失败: %v", err)
	}
	if len(claimedB) != 1 || claimedB[0].MsgID != "cs05-claim-b" {
		t.Fatalf("B 应恰认领本租户 1 条(cs05-claim-b)，实际 %+v", claimedB)
	}

	// —— ② 三值确认租户作用域：ctx=A 的 ack 谓词对 B 行 0 命中 ——
	repo := repository.NewMessageHubRepositoryWithDB(db)
	// A 确认自己的 inflight 行 → delivered；同批夹一个 B 的 msg_id：B 行必须 0 翻转。
	gotA, _, err := repo.AckOutboundDeliveredBatchReturningWithStatus(ctxA, cs05Platform, "acc-shared", "",
		model.BridgeAckStatusDelivered, []string{"cs05-claim-a", "cs05-claim-b"})
	if err != nil {
		t.Fatalf("A 三值确认失败: %v", err)
	}
	if len(gotA) != 1 || gotA[0] != "cs05-claim-a" {
		t.Fatalf("A 确认应恰翻本租户 1 行(cs05-claim-a)，实际 %v", gotA)
	}
	cs05AssertStatus(t, db, "cs05-claim-a", model.BridgeAckStatusDelivered)
	cs05AssertStatus(t, db, "cs05-claim-b", "inflight")

	// —— ③ fenced 确认租户作用域：B 拿自己的 fence 值，批里夹 A 的 msg_id 不许翻 A ——
	// B 行的 claimed_at 取认领返回值（fence 语义：值必须来自 Claim 返回行）。
	if claimedB[0].ClaimedAt == nil {
		t.Fatal("认领返回行应带 claimed_at（fence 输入）")
	}
	gotB, _, err := repo.AckOutboundDeliveredBatchReturningWithStatusFenced(ctxB, cs05Platform, "acc-shared", "",
		model.BridgeAckStatusDelivered,
		[]string{"cs05-claim-b", "cs05-claim-a"},
		map[string]time.Time{"cs05-claim-b": *claimedB[0].ClaimedAt, "cs05-claim-a": *claimedB[0].ClaimedAt})
	if err != nil {
		t.Fatalf("B fenced 确认失败: %v", err)
	}
	if len(gotB) != 1 || gotB[0] != "cs05-claim-b" {
		t.Fatalf("B fenced 确认应恰翻本租户 1 行，实际 %v", gotB)
	}
	cs05AssertStatus(t, db, "cs05-claim-a", model.BridgeAckStatusDelivered) // A 行不被 B 触碰
	cs05AssertStatus(t, db, "cs05-claim-b", model.BridgeAckStatusDelivered)

	// —— ④ legacy ack 租户作用域：ctx=A 确认批里夹 B 的 msg_id，B 行 0 翻转 ——
	cs05SeedOutbound(t, db, "cs05-legacy-b", "conv-b2", "pending", tenantB)
	n, err := svc.AckOutboundDelivered(ctxA, cs05Platform, "acc-shared", []string{"cs05-legacy-b"})
	if err != nil {
		t.Fatalf("legacy ack 失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("A 的 legacy ack 对 B 行应 0 生效，实际 %d", n)
	}
	cs05AssertStatus(t, db, "cs05-legacy-b", "pending")
}

// cs05AssertStatus system scope 直读终态（预置/回读同 scope，被测方法才带租户 ctx）。
func cs05AssertStatus(t *testing.T, db *gorm.DB, msgID, want string) {
	t.Helper()
	var got string
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Model(&model.MessageHub{}).
		Select("status").Where("platform = ? AND msg_id = ?", cs05Platform, msgID).
		Scan(&got).Error; err != nil {
		t.Fatalf("回读 %s 失败: %v", msgID, err)
	}
	if got != want {
		t.Errorf("行 %s status=%q，期望 %q", msgID, got, want)
	}
}
