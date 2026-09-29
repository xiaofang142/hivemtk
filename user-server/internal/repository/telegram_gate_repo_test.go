// ListStalledRestricted 的取数口径（补偿循环的唯一入口，SQL 在仓储、策略在 service）。
//
// 两类条件各拦一种线上故障：
//   - welcome_sent_at IS NULL：提示从未送达才需要补；已送达的人由 SweepExpired 按到期
//     口径处置。旧版把"临近到期"当"提示没送达"的代理，于是每个 TTL 给同一个人重播一次
//     入群提示，重播还顺手把 expires_at 顶回去（实测一名成员一天 60+ 条）。
//   - 窗口没到期（expires_at 为空视作待补）：窗口走完就不再补，避免与到期处置抢同一个人。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func TestTelegramMember_ListStalledRestricted(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	now := time.Now()
	delivered := now.Add(-time.Minute)
	restrictedSoon := now.Add(1 * time.Minute) // 临近到期
	restrictedFar := now.Add(24 * time.Hour)   // 到期还早
	expiredAgo := now.Add(-time.Hour)          // 窗口已走完
	members := []*model.TelegramGroupMember{
		// 已送达 + 临近到期 → 不命中（旧版正是在这一格重播并续期的）
		{AccountID: 1, ChatID: "c1", UserID: "u1", FullName: "delivered", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedSoon, WelcomeSentAt: &delivered},
		// 已送达 + 到期还早 → 不命中
		{AccountID: 1, ChatID: "c1", UserID: "u2", FullName: "far", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedFar, WelcomeSentAt: &delivered},
		// 非 restricted → 不命中
		{AccountID: 1, ChatID: "c1", UserID: "u3", FullName: "pending", JoinStatus: model.TGMemberPending, Authorized: false, ExpiresAt: &restrictedSoon, WelcomeSentAt: &delivered},
		// 已过审 → 两条判据都不该把他捞回来
		{AccountID: 1, ChatID: "c1", UserID: "u4", FullName: "authed", JoinStatus: model.TGMemberRestricted, Authorized: true, ExpiresAt: &restrictedSoon, WelcomeSentAt: &delivered},
		// 从未送达 + 没有到期时间 → 命中（窗口都没开过，更要补）
		{AccountID: 1, ChatID: "c1", UserID: "u5", FullName: "noexp", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: nil},
		// 从未送达 + 到期还早 → 命中（入群时发失败，下一拍就该补，不用等到临近到期）
		{AccountID: 2, ChatID: "c2", UserID: "u6", FullName: "undelivered", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &restrictedFar},
		// 从未送达 + 窗口已走完 → 不命中（此时该由到期处置说话，补偿循环不再空转）
		{AccountID: 2, ChatID: "c2", UserID: "u7", FullName: "window-closed", JoinStatus: model.TGMemberRestricted, Authorized: false, ExpiresAt: &expiredAgo},
	}
	for _, m := range members {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	got, err := repo.ListStalledRestricted(ctx, now, 0)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.UserID] = true
	}
	if len(got) != 2 || !seen["u5"] || !seen["u6"] {
		t.Fatalf("命中集合应={u5,u6}, 实际=%v", seen)
	}

	// limit 生效
	limited, err := repo.ListStalledRestricted(ctx, now, 1)
	if err != nil {
		t.Fatalf("limit 查询失败: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit=1 应返回 1 条, 实际 %d", len(limited))
	}

	// 全部补上送达登记后关火：空结果不报错
	if err := db.Model(&model.TelegramGroupMember{}).
		Where("welcome_sent_at IS NULL").
		Update("welcome_sent_at", now).Error; err != nil {
		t.Fatalf("补记送达状态失败: %v", err)
	}
	empty, err := repo.ListStalledRestricted(ctx, now.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("空结果查询失败: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("应返回空, 实际 %d", len(empty))
	}
}

// 重新入群＝一段新的成员关系：Upsert 必须把上一段的送达状态覆盖回未送达，
// 否则新 token 的提示永远补不出去（清扫器只认 welcome_sent_at IS NULL）。
func TestTelegramMember_UpsertResetsWelcomeState(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	sent := time.Now().Add(-time.Hour).Truncate(time.Microsecond) // PG 只存到微秒，比对前必须同精度
	seed := &model.TelegramGroupMember{
		AccountID: 7, ChatID: "c7", UserID: "u7", JoinStatus: model.TGMemberRestricted,
		JoinMode: "mute_unlock", VerifyToken: "old", ExpiresAt: &sent,
		WelcomeSentAt: &sent, WelcomeResends: 2,
	}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	fresh := &model.TelegramGroupMember{
		AccountID: 7, ChatID: "c7", UserID: "u7", JoinStatus: model.TGMemberRestricted,
		JoinMode: "mute_unlock", VerifyToken: "new", ExpiresAt: &sent,
	}
	if err := repo.Upsert(ctx, fresh); err != nil {
		t.Fatalf("Upsert 失败: %v", err)
	}

	var got model.TelegramGroupMember
	if err := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", 7, "c7", "u7").First(&got).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got.VerifyToken != "new" {
		t.Fatalf("重新入群应换发验证 token, 实际=%q", got.VerifyToken)
	}
	if got.WelcomeSentAt != nil {
		t.Fatalf("重新入群后送达状态应清空, 实际=%v", got.WelcomeSentAt)
	}
	if got.WelcomeResends != 0 {
		t.Fatalf("重新入群后补发计数应清零, 实际=%d", got.WelcomeResends)
	}
}

// 这些窄更新（MarkWelcomeSent / BumpWelcomeResend 按 key 定位，RetimeVerification /
// MarkKicked 按主键）只 SET 自己的列，且都带 authorized=false 守卫：
// 成员可能在 read→write 窗口内刚好 /start 过审，整行 Save 会把 authorized 覆盖回 false，
// 过审的人因此既丢了激活状态、又会被 TTL 清扫踢出。
func TestTelegramMember_WelcomeStateGuards(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	exp := time.Now().Add(time.Minute).Truncate(time.Microsecond) // PG 只存微秒精度，等值比对前要对齐
	m := &model.TelegramGroupMember{
		AccountID: 8, ChatID: "c8", UserID: "u8", JoinStatus: model.TGMemberRestricted,
		JoinMode: "mute_unlock", VerifyToken: "tok", ExpiresAt: &exp, WelcomeResends: 2,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	sentAt := time.Now().Truncate(time.Microsecond)
	if err := repo.MarkWelcomeSent(ctx, 8, "c8", "u8", sentAt); err != nil {
		t.Fatalf("MarkWelcomeSent 失败: %v", err)
	}
	got := rereadMember(t, db, 8, "c8", "u8")
	if got.WelcomeSentAt == nil || got.WelcomeResends != 0 {
		t.Fatalf("送达登记应记时间并清零计数, 实际 sent=%v resends=%d", got.WelcomeSentAt, got.WelcomeResends)
	}
	if got.VerifyToken != "tok" {
		t.Fatalf("按 key 定位的窄更新不该改其他列, 实际 token=%q", got.VerifyToken)
	}
	if err := repo.BumpWelcomeResend(ctx, 8, "c8", "u8"); err != nil {
		t.Fatalf("BumpWelcomeResend（未过审）失败: %v", err)
	}
	if bumped := rereadMember(t, db, 8, "c8", "u8"); bumped.WelcomeResends != 1 {
		t.Fatalf("未过审成员的补发计数应 +1, 实际=%d", bumped.WelcomeResends)
	}

	// 未过审时重新计时必须真的生效——否则下面"过审后不该生效"是恒真的空判据。
	currentExp := time.Now().Add(30 * time.Minute).Truncate(time.Microsecond)
	if err := repo.RetimeVerification(ctx, got.ID, currentExp); err != nil {
		t.Fatalf("RetimeVerification（未过审）失败: %v", err)
	}
	if timed := rereadMember(t, db, 8, "c8", "u8"); timed.ExpiresAt == nil || !timed.ExpiresAt.Equal(currentExp) {
		t.Fatalf("未过审成员应被重新计时到 %v, 实际=%v", currentExp, timed.ExpiresAt)
	}

	// 未过审时 MarkKicked 必须真能把状态落成 kicked——否则下面"过审后不落状态"又是恒真空判据。
	if err := repo.MarkKicked(ctx, got.ID); err != nil {
		t.Fatalf("MarkKicked（未过审）失败: %v", err)
	}
	if kicked := rereadMember(t, db, 8, "c8", "u8"); kicked.JoinStatus != model.TGMemberKicked {
		t.Fatalf("未过审成员应落成 kicked, 实际=%s", kicked.JoinStatus)
	}
	if err := db.Model(&model.TelegramGroupMember{}).Where("id = ?", got.ID).
		Update("join_status", model.TGMemberRestricted).Error; err != nil {
		t.Fatalf("复位 join_status 失败: %v", err)
	}

	// 过审之后：三个写方法都该变成空操作（read→write 窗口内刚 /start 的人不该被回滚）
	if err := db.Model(&model.TelegramGroupMember{}).Where("id = ?", got.ID).
		Update("authorized", true).Error; err != nil {
		t.Fatalf("置 authorized 失败: %v", err)
	}
	if err := repo.BumpWelcomeResend(ctx, 8, "c8", "u8"); err != nil {
		t.Fatalf("BumpWelcomeResend 失败: %v", err)
	}
	if err := repo.MarkWelcomeSent(ctx, 8, "c8", "u8", time.Now()); err != nil {
		t.Fatalf("MarkWelcomeSent（已过审）失败: %v", err)
	}
	if err := repo.RetimeVerification(ctx, got.ID, time.Now().Add(90*time.Minute)); err != nil {
		t.Fatalf("RetimeVerification（已过审）失败: %v", err)
	}
	// 踢人的状态回写同样得受这条守卫保护：TG 侧那两次调用之间他刚好过审的话，
	// 无守卫的 UPDATE 会把一个已经解禁的人打成 kicked，台账与群内状态从此永久错位。
	if err := repo.MarkKicked(ctx, got.ID); err != nil {
		t.Fatalf("MarkKicked（已过审）失败: %v", err)
	}

	after := rereadMember(t, db, 8, "c8", "u8")
	if !after.Authorized {
		t.Fatal("窄更新把 authorized 覆盖回了 false")
	}
	if after.WelcomeResends != 1 || after.WelcomeSentAt == nil || !after.WelcomeSentAt.Equal(sentAt) {
		t.Fatalf("已过审成员的补发状态不该被动过, 实际 sent=%v resends=%d", after.WelcomeSentAt, after.WelcomeResends)
	}
	if after.ExpiresAt == nil || !after.ExpiresAt.Equal(currentExp) {
		t.Fatalf("已过审成员仍被重新计时, 到期时间应停在 %v, 实际=%v", currentExp, after.ExpiresAt)
	}
	if after.JoinStatus != model.TGMemberRestricted {
		t.Fatalf("已过审成员不该被落成 kicked, 实际=%s", after.JoinStatus)
	}
}

func rereadMember(t *testing.T, db *gorm.DB, accountID uint, chatID, userID string) model.TelegramGroupMember {
	t.Helper()
	var m model.TelegramGroupMember
	if err := db.Where("account_id = ? AND chat_id = ? AND user_id = ?", accountID, chatID, userID).
		First(&m).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	return m
}

// ClaimStalledResend 是补偿循环的并发刹车：带乐观锁的单条 UPDATE，同一行
// 一轮只能被认领一次；认领即计一次尝试（调用方失败支路不再 bump）；已送达/
// 到上限的行认领失败（任务不丢，下轮再来）。
func TestTelegramMember_ClaimStalledResend(t *testing.T) {
	db := testutil.NewTestDB(t, &model.TelegramGroupMember{})
	repo := NewTelegramGroupMemberRepositoryWithDB(db)
	ctx := context.Background()

	seed := &model.TelegramGroupMember{
		AccountID: 9, ChatID: "c9", UserID: "u9", JoinStatus: model.TGMemberRestricted,
		JoinMode: "mute_unlock", VerifyToken: "tok", Authorized: false,
	}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("seed 失败: %v", err)
	}

	// 第一次认领成功，且尝试计数 +1
	ok, err := repo.ClaimStalledResend(ctx, seed.ID, 0, 3)
	if err != nil || !ok {
		t.Fatalf("首次认领应成功, ok=%v err=%v", ok, err)
	}
	if got := rereadMember(t, db, 9, "c9", "u9"); got.WelcomeResends != 1 {
		t.Fatalf("认领应计一次尝试, 实际=%d", got.WelcomeResends)
	}

	// 并发第二个认领者拿着过期期望值（List 出来时的 0）→ 必须失败，否则两条提示齐飞
	if ok, err := repo.ClaimStalledResend(ctx, seed.ID, 0, 3); err != nil || ok {
		t.Fatalf("过期期望值的认领必须失败, ok=%v err=%v", ok, err)
	}
	if got := rereadMember(t, db, 9, "c9", "u9"); got.WelcomeResends != 1 {
		t.Fatalf("失败的认领不应计数, 实际=%d", got.WelcomeResends)
	}

	// 下一轮拿着新期望值（1）可认领
	if ok, err := repo.ClaimStalledResend(ctx, seed.ID, 1, 3); err != nil || !ok {
		t.Fatalf("新期望值的认领应成功, ok=%v err=%v", ok, err)
	}

	// 到上限后认领失败（2→3 已是最后一次，resends=3 时不再认领）
	if ok, err := repo.ClaimStalledResend(ctx, seed.ID, 2, 3); err != nil || !ok {
		t.Fatalf("resends=2、max=3 时应还能认领最后一次, ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ClaimStalledResend(ctx, seed.ID, 3, 3); err != nil || ok {
		t.Fatalf("到上限后认领必须失败, ok=%v err=%v", ok, err)
	}

	// 已送达的行认领失败
	now := time.Now()
	if err := db.Model(&model.TelegramGroupMember{}).Where("id = ?", seed.ID).
		Update("welcome_sent_at", now).Error; err != nil {
		t.Fatalf("补记送达失败: %v", err)
	}
	if ok, err := repo.ClaimStalledResend(ctx, seed.ID, 3, 3); err != nil || ok {
		t.Fatalf("已送达行的认领必须失败, ok=%v err=%v", ok, err)
	}
}
