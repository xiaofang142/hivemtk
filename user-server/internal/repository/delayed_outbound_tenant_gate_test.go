package repository

// CreateMessageHubIdempotent 租户 fail-closed 门禁（2026-11-14 生产事故回归钉）。
//
// 事故链：AI 出站重试路径落库无租户 → gorm default:1 写进 tenant 1 → 行成为
// 认领/管理台双盲孤儿（douyin 4610 实证），AI 侧 ai_replied=true 而客户永收不到。
// 三条断言：无租户 ctx 拒绝；ctx 带租户自动补列；显式租户原样保留。
// DB 门禁：缺 POSTGRES_TEST_PASSWORD 时 fail-closed 跳过（环境问题非代码问题）。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupHubTenantGateDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	return database
}

func TestCreateMessageHubIdempotent_TenantGate(t *testing.T) {
	gormDB := setupHubTenantGateDB(t)
	r := &DelayedOutboundRepository{db: gormDB}
	ctx := context.Background()

	// ① 无租户 ctx + 行无租户 → 拒绝（fail-closed），不落库
	orphan := &model.MessageHub{Platform: "douyin", MsgID: "mh:tgate_a", ConversationID: "conv:a", Direction: "outbound", MsgType: "text", Content: "x"}
	if _, err := r.CreateMessageHubIdempotent(ctx, orphan); err == nil {
		t.Fatalf("无租户上下文应拒绝落库（fail-closed），实际成功")
	}
	var n int64
	gormDB.Model(&model.MessageHub{}).Where("msg_id = ?", "mh:tgate_a").Count(&n)
	if n != 0 {
		t.Fatalf("被拒绝的行不应落库，实际 %d 行", n)
	}

	// ② ctx 带租户 + 行无租户 → 自动补列
	withT := tenant.OnlyTenant(ctx, 18)
	filled := &model.MessageHub{Platform: "douyin", MsgID: "mh:tgate_b", ConversationID: "conv:b", Direction: "outbound", MsgType: "text", Content: "x"}
	got, err := r.CreateMessageHubIdempotent(withT, filled)
	if err != nil {
		t.Fatalf("ctx 带租户应成功: %v", err)
	}
	if got.TenantID != 18 || got.ID == 0 {
		t.Fatalf("应自动补租户 18 且回填 ID，实际 tenant=%d id=%d", got.TenantID, got.ID)
	}

	// ③ 显式租户原样保留 + 同键幂等（DO NOTHING 不产生第二行）
	explicit := &model.MessageHub{TenantID: 7, Platform: "douyin", MsgID: "mh:tgate_c", ConversationID: "conv:c", Direction: "outbound", MsgType: "text", Content: "x"}
	got2, err := r.CreateMessageHubIdempotent(ctx, explicit)
	if err != nil {
		t.Fatalf("显式租户应成功: %v", err)
	}
	if got2.TenantID != 7 {
		t.Fatalf("显式租户应原样保留，实际 %d", got2.TenantID)
	}
	dup := &model.MessageHub{TenantID: 7, Platform: "douyin", MsgID: "mh:tgate_c", ConversationID: "conv:c", Direction: "outbound", MsgType: "text", Content: "x"}
	if _, err := r.CreateMessageHubIdempotent(ctx, dup); err != nil {
		t.Fatalf("幂等重复应无错: %v", err)
	}
	gormDB.Model(&model.MessageHub{}).Where("msg_id = ?", "mh:tgate_c").Count(&n)
	if n != 1 {
		t.Fatalf("同键幂等应仅 1 行，实际 %d", n)
	}
}
