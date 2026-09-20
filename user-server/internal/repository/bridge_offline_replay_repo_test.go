// bridge_offline_replay_repo_test.go 离线回扫对 reach_delayed_outbound 的读写口径。
//
// 建表统一走 model.AutoMigrate（与 v3.26.0 迁移建的列一致），
// 这样"SQL 引用了无人建立的列"会在真库上直接暴露，而不是靠肉眼比对 DDL。
package repository

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupOfflineReplayTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.DelayedOutboundReply{})
}

// seedPendingDelayedAt 造一条指定到期时间的 pending 延迟出站，返回其 ID。
func seedPendingDelayedAt(t *testing.T, database *gorm.DB, accountID, convID string, sendAt time.Time, attempts int) uint {
	t.Helper()
	rec := &model.DelayedOutboundReply{
		Platform:       "tg",
		AccountID:      accountID,
		ConversationID: convID,
		SenderID:       "sender-" + convID,
		Content:        "离线期间积压的 AI 回复",
		SendAt:         sendAt,
		Status:         model.DelayedStatusPending,
		Attempts:       attempts,
	}
	if err := database.WithContext(context.Background()).Create(rec).Error; err != nil {
		t.Fatalf("造延迟出站记录失败: %v", err)
	}
	return rec.ID
}

// seedPendingDelayed 造一条到期的 pending 延迟出站，返回其 ID。
func seedPendingDelayed(t *testing.T, database *gorm.DB, accountID, convID string) uint {
	t.Helper()
	return seedPendingDelayedAt(t, database, accountID, convID, time.Now().Add(-time.Hour), 0)
}

func readDelayedStatus(t *testing.T, database *gorm.DB, id uint) (string, int) {
	t.Helper()
	var got struct {
		Status   string
		Attempts int
	}
	if err := database.WithContext(context.Background()).Model(&model.DelayedOutboundReply{}).
		Where("id = ?", id).Select("status, attempts").Scan(&got).Error; err != nil {
		t.Fatalf("回读状态失败: %v", err)
	}
	return got.Status, got.Attempts
}

// seedBridgeAccount 造一条桥接渠道账号行（R14：检测的数据源就是这张表）。
func seedBridgeAccount(t *testing.T, database *gorm.DB, channel, accountID, status string, lastSyncAt *time.Time) {
	t.Helper()
	acc := &model.BridgeAccount{
		Channel:    channel,
		AccountID:  accountID,
		Status:     status,
		LastSyncAt: lastSyncAt,
	}
	if err := database.WithContext(context.Background()).Create(acc).Error; err != nil {
		t.Fatalf("造渠道账号行失败 channel=%s account=%s: %v", channel, accountID, err)
	}
}

// TestListBridgeAccounts_ReadsRealColumns 渠道快照必须取自 bridge_accounts 的实有列。
//
// 旧实现的两条渠道查询分别引用 bridge_metrics（指标时间序列，没有 platform /
// account_id / updated_at）与 bridge_accounts.platform（该表的渠道列叫 channel），
// 在真库上必然 42703 ⇒ DetectOfflineChannels 恒报错返回 ⇒ 离线回扫从未投过一条。
func TestListBridgeAccounts_ReadsRealColumns(t *testing.T) {
	database := testutil.NewTestDB(t, &model.BridgeAccount{})
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	now := time.Now()
	stale := now.Add(-time.Hour)
	seedBridgeAccount(t, database, "douyin", "acc-online", "online", &now)
	seedBridgeAccount(t, database, "xiaohongshu", "acc-off", "offline", &stale)

	rows, err := repo.ListBridgeAccounts(ctx)
	if err != nil {
		t.Fatalf("读渠道快照失败（R14 复现：SQL 引用的列无人建立）: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("渠道快照=%d 行，want 2：%+v", len(rows), rows)
	}
	byAcc := make(map[string]BridgeChannelRow, len(rows))
	for _, r := range rows {
		byAcc[r.AccountID] = r
	}
	online := byAcc["acc-online"]
	if online.Channel != "douyin" || online.Status != "online" {
		t.Errorf("acc-online 快照错：channel=%q status=%q", online.Channel, online.Status)
	}
	off := byAcc["acc-off"]
	if off.Channel != "xiaohongshu" {
		t.Errorf("acc-off channel=%q want xiaohongshu", off.Channel)
	}
	if off.LastSyncAt == nil {
		t.Fatal("acc-off last_sync_at 为空 ⇒ 报告里的离线时刻无从计算")
	}
	if !off.LastSyncAt.Equal(stale.Truncate(time.Microsecond)) {
		t.Errorf("acc-off last_sync_at=%v，want 造出的离线时刻 %v", off.LastSyncAt, stale)
	}
}

// TestListBridgeAccounts_NoDBGuardReturnsNil 无库时必须返回空快照而非 panic：
// 回扫由 cron 调用，DB 未装配的进程里它应当静默无事可做。
func TestListBridgeAccounts_NoDBGuardReturnsNil(t *testing.T) {
	rows, err := NewBridgeOfflineReplayRepositoryWithDB(nil).ListBridgeAccounts(context.Background())
	if err != nil || rows != nil {
		t.Errorf("无库守卫失效: rows=%v err=%v", rows, err)
	}
}

// TestOfflineReplayListReadsRealColumns 列表查询必须落在真实列上：
// 引用不存在的列时 gorm 的 SELECT * 不会报错，只会把那几个字段永远留成零值，
// 于是重放带着空 msg_type / 空 event_id 出站，且没有任何报警。
func TestOfflineReplayListReadsRealColumns(t *testing.T) {
	database := setupOfflineReplayTestDB(t)
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	id := seedPendingDelayed(t, database, "acc-list", "conv-list")
	other := seedPendingDelayed(t, database, "acc-other", "conv-other")
	defer func() { _ = other }()

	rows, err := repo.ListPendingDelayedOutbound(ctx, "tg", "acc-list", 10)
	if err != nil {
		t.Fatalf("ListPendingDelayedOutbound: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("只应取到本渠道的 pending 行, got %d", len(rows))
	}
	m := rows[0]
	if m.ID != id || m.ConversationID != "conv-list" || m.Content == "" {
		t.Errorf("行内容异常: %+v", m)
	}
	if m.Attempts != 0 || m.Kind != model.DelayedKindQuietHours {
		t.Errorf("未把真实列读进行里: attempts=%d kind=%q", m.Attempts, m.Kind)
	}
}

// TestOfflineReplayOnlyPicksDueRows 未到期的行不得被离线回扫提前投递。
//
// quiet_hours 行的 send_at 就是"次日窗口开放"的时刻；回扫不看 send_at 等于
// 在客户免打扰时段把这条回复推出去，把窗口语义整个抹掉。
func TestOfflineReplayOnlyPicksDueRows(t *testing.T) {
	database := setupOfflineReplayTestDB(t)
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	dueID := seedPendingDelayedAt(t, database, "acc-due", "conv-due", time.Now().Add(-time.Minute), 0)
	futureID := seedPendingDelayedAt(t, database, "acc-due", "conv-future", time.Now().Add(6*time.Hour), 0)

	rows, err := repo.ListPendingDelayedOutbound(ctx, "tg", "acc-due", 10)
	if err != nil {
		t.Fatalf("ListPendingDelayedOutbound: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != dueID {
		t.Fatalf("只应取到已到期行, got %+v", rows)
	}
	if rows[0].ID == futureID {
		t.Error("未到期的 quiet_hours 行被提前投递")
	}
}

// TestOfflineReplayStatusWritesLandOnTable 重放收口必须真的改到行上。
//
// 旧实现的两条 UPDATE 写的是 retry_count / replayed_at —— 表和模型里都没有这两列，
// 在 Postgres 上直接 42703，而调用方用 `_ =` 吞掉错误：行永远停在 pending，
// 这条链路每 5 分钟把同一条消息重投一次。
func TestOfflineReplayStatusWritesLandOnTable(t *testing.T) {
	database := setupOfflineReplayTestDB(t)
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	// 1) 重放成功：收口到 sent，且不再被 pending 查询取到
	okID := seedPendingDelayed(t, database, "acc-ok", "conv-ok")
	if err := repo.MarkDelayedOutboundReplayed(ctx, okID); err != nil {
		t.Fatalf("MarkDelayedOutboundReplayed: %v", err)
	}
	if st, _ := readDelayedStatus(t, database, okID); st != model.DelayedStatusSent {
		t.Errorf("重放成功后 status=%q want %q", st, model.DelayedStatusSent)
	}

	// 2) 重放失败：回到 pending 并累计 attempts、留下失败原因，交给主链路按上限收敛
	badID := seedPendingDelayed(t, database, "acc-bad", "conv-bad")
	if err := repo.MarkDelayedOutboundReplayFailed(ctx, badID, "渠道未就绪"); err != nil {
		t.Fatalf("MarkDelayedOutboundReplayFailed: %v", err)
	}
	st, attempts := readDelayedStatus(t, database, badID)
	if st != model.DelayedStatusPending || attempts != 1 {
		t.Errorf("失败回写后 status=%q attempts=%d want pending/1", st, attempts)
	}
	var lastErr string
	if err := database.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ?", badID).Pluck("last_error", &lastErr).Error; err != nil || lastErr != "渠道未就绪" {
		t.Errorf("失败原因未落库: %q err=%v", lastErr, err)
	}

	// 3) 只有成功路径该被摘出 pending：失败行仍会被下一轮取到（且只多算一次 attempts）
	rows, err := repo.ListPendingDelayedOutbound(ctx, "tg", "acc-bad", 10)
	if err != nil || len(rows) != 1 {
		t.Errorf("失败行应仍在待发队列, got %d 条 err=%v", len(rows), err)
	}
	if _, err := repo.ListPendingDelayedOutbound(ctx, "tg", "acc-ok", 10); err != nil {
		t.Errorf("已成功渠道查询失败: %v", err)
	}
}

// TestOfflineReplayClaimPreventsDoubleSend 抢占必须是原子的：
// 离线回扫与 H-3 主链路 drain 同一张表的 pending 行，若两边都直接投递，
// 同一份内容会在一次周期里发给客户两次。只有把 pending→sending 的更新当入场券才安全。
func TestOfflineReplayClaimPreventsDoubleSend(t *testing.T) {
	database := setupOfflineReplayTestDB(t)
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	id := seedPendingDelayed(t, database, "acc-claim", "conv-claim")
	first, err := repo.ClaimDelayedOutboundForReplay(ctx, id)
	if err != nil || !first {
		t.Fatalf("首次抢占应成功: won=%v err=%v", first, err)
	}
	second, err := repo.ClaimDelayedOutboundForReplay(ctx, id)
	if err != nil {
		t.Fatalf("二次抢占查询失败: %v", err)
	}
	if second {
		t.Error("同一行不得被抢占两次（双发入口）")
	}
	if st, _ := readDelayedStatus(t, database, id); st != model.DelayedStatusSending {
		t.Errorf("抢占后 status=%q want %q", st, model.DelayedStatusSending)
	}
}

// TestOfflineReplayAbandonConverges 重投次数用尽的行必须收口到终态 failed，
// 否则一条永远投不出的行会以 5 分钟一次的节奏永久占用回扫。
func TestOfflineReplayAbandonConverges(t *testing.T) {
	database := setupOfflineReplayTestDB(t)
	repo := NewBridgeOfflineReplayRepositoryWithDB(database)
	ctx := context.Background()

	id := seedPendingDelayedAt(t, database, "acc-ex", "conv-ex", time.Now().Add(-time.Hour), 3)
	if err := repo.MarkDelayedOutboundAbandoned(ctx, id, "重投次数用尽"); err != nil {
		t.Fatalf("MarkDelayedOutboundAbandoned: %v", err)
	}
	st, attempts := readDelayedStatus(t, database, id)
	if st != model.DelayedStatusFailed {
		t.Errorf("放弃后 status=%q want %q", st, model.DelayedStatusFailed)
	}
	if attempts != 3 {
		t.Errorf("放弃不应改动已投次数: attempts=%d", attempts)
	}
	rows, err := repo.ListPendingDelayedOutbound(ctx, "tg", "acc-ex", 10)
	if err != nil || len(rows) != 0 {
		t.Errorf("终态行不得再被回扫取到, got %d 条 err=%v", len(rows), err)
	}
	var lastErr string
	if err := database.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ?", id).Pluck("last_error", &lastErr).Error; err != nil || lastErr != "重投次数用尽" {
		t.Errorf("放弃原因未落库: %q err=%v", lastErr, err)
	}
}
