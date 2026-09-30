package service

// 超时未验证成员的处置口径（对真人动手的那一条，判据全部落在台账的送达状态上）：
//  1. 提示从未送达 → 不处置：没被告知的人不为"没做的事"受罚，留在禁言里等补偿；
//  2. 送达过、但刚过 TTL → 再宽限一个 verify_ttl_min：点链接 → 跳 Bot 私聊 → 发 /start
//     这条链路本来就慢，一个窗口没点不等于不想入群；
//  3. 送达过、宽限期也过了 → 移出群（banChatMember 过去时间 + 立刻 unban，仍可重新入群）。
//
// 方案 A（join_request）不受 1/2 约束：decline 只把申请退回"可以再次申请"，
// 没有人被移走、也没有人被拉黑，代价不足以要求"先证明被告知过"。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// r66Seed 按给定状态种一行台账（r65SeedMember 固定 restricted/mute_unlock，覆盖不到
// join_request 与"已过审"这两格）。
func r66Seed(t *testing.T, db *gorm.DB, accID uint, userID, mode, status string, sentAt *time.Time, resends int, expiresAt time.Time, authorized bool) {
	t.Helper()
	exp := expiresAt
	m := &model.TelegramGroupMember{
		AccountID:      accID,
		ChatID:         r65ChatID,
		UserID:         userID,
		FullName:       "成员" + userID,
		JoinStatus:     status,
		JoinMode:       mode,
		VerifyToken:    "tok_" + userID,
		WelcomeSentAt:  sentAt,
		WelcomeResends: resends,
		ExpiresAt:      &exp,
		Authorized:     authorized,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("种台账 %s: %v", userID, err)
	}
}

// 判据 1：从未送达的人超时也不动他。补发计数一并打到上限：窗口已走完时补偿循环本来
// 就不捞他，这一格不该有任何 TG 请求，两个免罚理由叠在一起才算"零动作"。
func TestSweepExpiredLeavesNeverNotifiedMember(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)

	r66Seed(t, db, accID, "7001", TGGateModeMuteUnlock, model.TGMemberRestricted,
		nil, tgGateWelcomeResendMax, time.Now().Add(-30*time.Minute), false)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 0 {
		t.Fatalf("未送达的成员不该被计成处置, swept=%d", swept)
	}
	if stub.total() != 0 {
		t.Fatalf("未送达的成员不该收到任何 TG 请求, 实际=%v", stub.calls)
	}
	if got := r65ReadMember(t, db, accID, "7001"); got.JoinStatus != model.TGMemberRestricted {
		t.Fatalf("台账应保持 restricted（等待补偿/人工）, 实际=%s", got.JoinStatus)
	}
}

// 判据 2：刚过 TTL 的人在宽限期内不动。
func TestSweepExpiredGivesOneTTLGrace(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)

	// 群里 TTL=10 分钟：提示 11 分钟前送达、1 分钟前到期 ⇒ 宽限期（10 分钟）还没走完。
	r66Seed(t, db, accID, "7002", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-11*time.Minute)), 0, time.Now().Add(-1*time.Minute), false)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 0 {
		t.Fatalf("宽限期内不该处置, swept=%d", swept)
	}
	if stub.total() != 0 {
		t.Fatalf("宽限期内不该有任何 TG 请求, 实际=%v", stub.calls)
	}
	if got := r65ReadMember(t, db, accID, "7002"); got.JoinStatus != model.TGMemberRestricted {
		t.Fatalf("宽限期内台账状态不该变, 实际=%s", got.JoinStatus)
	}
}

// 判据 3：送达过 + 宽限期也走完 → 移出且"只踢不拉黑"（ban 之后必须补 unban，
// 否则按 TG 的实际语义他就再也进不了这个群），并把台账落成 kicked。
func TestSweepExpiredRemovesAfterGraceAndKeepsRejoinOpen(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)

	r66Seed(t, db, accID, "7003", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), false)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 1 {
		t.Fatalf("送达过且过了宽限期应处置 1 人, 实际=%d, 全部调用=%v", swept, stub.calls)
	}
	if n := stub.count("banChatMember"); n != 1 {
		t.Fatalf("应移出群一次, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if n := stub.count("unbanChatMember"); n != 1 {
		t.Fatalf("移出后必须解除拉黑（保留可重新入群）, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if n := stub.count("sendMessage"); n != 0 {
		t.Fatalf("清扫器不播报, 实际=%d", n)
	}
	got := r65ReadMember(t, db, accID, "7003")
	if got.JoinStatus != model.TGMemberKicked {
		t.Fatalf("应落 kicked, 实际=%s", got.JoinStatus)
	}
	if got.Authorized {
		t.Fatal("被移出的人不该是已验证状态")
	}
}

// 方案 A：decline 与"是否送达"无关，也不走宽限期。
func TestSweepExpiredDeclinesJoinRequest(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)

	r66Seed(t, db, accID, "7004", TGGateModeJoinRequest, model.TGMemberPending,
		nil, 0, time.Now().Add(-1*time.Minute), false)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 1 {
		t.Fatalf("超时的入群申请应被退回, swept=%d, 全部调用=%v", swept, stub.calls)
	}
	if n := stub.count("declineChatJoinRequest"); n != 1 {
		t.Fatalf("应 decline 一次, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	if n := stub.count("banChatMember"); n != 0 {
		t.Fatalf("方案 A 不该动 ban, 实际=%d", n)
	}
	if got := r65ReadMember(t, db, accID, "7004"); got.JoinStatus != model.TGMemberKicked {
		t.Fatalf("申请退回落台账 kicked, 实际=%s", got.JoinStatus)
	}
}

// TG 侧没动成功就不许把台账写成 kicked：否则下一轮他不在过期集合里（ListExpired 只认
// pending/restricted），这个人在群里既没被移出也没人再管。
func TestSweepExpiredKeepsRetryWhenBanFails(t *testing.T) {
	stub := &r65GateStub{failMethods: map[string]bool{"banChatMember": true}}
	svc, db, accID := r65SetupGate(t, stub)

	r66Seed(t, db, accID, "7005", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), false)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 0 {
		t.Fatalf("移出失败不该计入处置, swept=%d", swept)
	}
	if n := stub.count("unbanChatMember"); n != 0 {
		t.Fatalf("ban 失败后不该再补 unban, 实际=%d, 全部调用=%v", n, stub.calls)
	}
	got := r65ReadMember(t, db, accID, "7005")
	if got.JoinStatus != model.TGMemberRestricted {
		t.Fatalf("TG 侧没动成功时台账必须保持可重试状态, 实际=%s", got.JoinStatus)
	}
}

// 已过审的人不在过期集合里：他可能刚在到期那一瞬点了链接，此时任何"踢人"都是误伤。
func TestSweepExpiredSkipsAuthorizedMember(t *testing.T) {
	stub := &r65GateStub{}
	svc, db, accID := r65SetupGate(t, stub)

	r66Seed(t, db, accID, "7006", TGGateModeMuteUnlock, model.TGMemberRestricted,
		timePtr(time.Now().Add(-25*time.Minute)), 0, time.Now().Add(-15*time.Minute), true)

	swept, err := svc.SweepExpired(context.Background(), 100)
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if swept != 0 || stub.total() != 0 {
		t.Fatalf("已过审成员不该被清扫, swept=%d, 调用=%v", swept, stub.calls)
	}
}
