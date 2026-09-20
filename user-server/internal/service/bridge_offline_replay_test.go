// bridge_offline_replay_test.go 离线回扫服务侧投递与收口口径。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// setupOfflineReplaySvc 造一套「回扫服务 + 已装配出站管道」的最小现场。
func setupOfflineReplaySvc(t *testing.T) (*BridgeOfflineReplayService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{})
	setupBridgeWhitelistForTest(t, "tg")
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	prev := GlobalInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(prev) })
	return NewBridgeOfflineReplayService().WithDB(db), db
}

func seedDelayed(t *testing.T, db *gorm.DB, rec *model.DelayedOutboundReply) uint {
	t.Helper()
	if rec.SendAt.IsZero() {
		rec.SendAt = time.Now().Add(-time.Hour)
	}
	if rec.Status == "" {
		rec.Status = model.DelayedStatusPending
	}
	if err := db.WithContext(context.Background()).Create(rec).Error; err != nil {
		t.Fatalf("造延迟出站记录失败: %v", err)
	}
	return rec.ID
}

func readDelayed(t *testing.T, db *gorm.DB, id uint) model.DelayedOutboundReply {
	t.Helper()
	var got model.DelayedOutboundReply
	if err := db.WithContext(context.Background()).Where("id = ?", id).First(&got).Error; err != nil {
		t.Fatalf("回读延迟出站失败: %v", err)
	}
	return got
}

func hubOutbound(t *testing.T, db *gorm.DB, convID string) []model.MessageHub {
	t.Helper()
	var rows []model.MessageHub
	if err := db.WithContext(context.Background()).
		Where("conversation_id = ? AND direction = ?", convID, "outbound").Find(&rows).Error; err != nil {
		t.Fatalf("查询 message_hub 失败: %v", err)
	}
	return rows
}

// TestOfflineReplayService_DeliversAndCloses 一次回扫：文本入站管道、行收口 sent，重跑不双发。
func TestOfflineReplayService_DeliversAndCloses(t *testing.T) {
	svc, db := setupOfflineReplaySvc(t)
	ctx := context.Background()

	id := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "tg", AccountID: "acc_ok", ConversationID: "conv_ok",
		Content: "免打扰窗口开放后的 AI 回复", Kind: model.DelayedKindQuietHours,
	})

	replayed, failed := svc.ReplayDelayedOutbound(ctx, "tg", "acc_ok", 50)
	if replayed != 1 || failed != 0 {
		t.Fatalf("首轮应重放 1 条, got replayed=%d failed=%d", replayed, failed)
	}
	got := readDelayed(t, db, id)
	if got.Status != model.DelayedStatusSent {
		t.Errorf("投递成功后 status=%q want %q", got.Status, model.DelayedStatusSent)
	}
	if got.SentAt == nil {
		t.Error("成功行未记录 sent_at（终态时间戳是排障证据）")
	}
	hub := hubOutbound(t, db, "conv_ok")
	if len(hub) != 1 {
		t.Fatalf("出站管道应收到 1 条, got %d", len(hub))
	}
	if hub[0].MsgType != "text" {
		t.Errorf("出站 msg_type=%q want text（空 msg_type 会让前端渲染成空气泡）", hub[0].MsgType)
	}
	if hub[0].Content != "免打扰窗口开放后的 AI 回复" {
		t.Errorf("出站内容不符: %q", hub[0].Content)
	}

	// 幂等：行已收口，下一轮回扫不该再投一次
	replayed2, _ := svc.ReplayDelayedOutbound(ctx, "tg", "acc_ok", 50)
	if replayed2 != 0 {
		t.Errorf("重跑不应再次投递, got %d", replayed2)
	}
	if n := len(hubOutbound(t, db, "conv_ok")); n != 1 {
		t.Errorf("重跑后出站 %d 条, want 1（双发）", n)
	}
}

// TestOfflineReplayService_SkipsCardsAndClaimed 富卡行交给主链路、已被抢占的行不得再投。
func TestOfflineReplayService_SkipsCardsAndClaimed(t *testing.T) {
	svc, db := setupOfflineReplaySvc(t)
	ctx := context.Background()

	cardID := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "tg", AccountID: "acc_skip", ConversationID: "conv_card",
		Content: "带卡片的回复", Kind: model.DelayedKindQuietHours,
		Cards: model.JSONMap{"cards": []any{map[string]any{"title": "商品 A"}}},
	})
	claimedID := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "tg", AccountID: "acc_taken", ConversationID: "conv_taken",
		Content: "主链路已抢占的回复", Status: model.DelayedStatusSending,
	})

	replayed, failed := svc.ReplayDelayedOutbound(ctx, "tg", "acc_skip", 50)
	if replayed != 0 || failed != 0 {
		t.Fatalf("富卡行应跳过, got replayed=%d failed=%d", replayed, failed)
	}
	if got := readDelayed(t, db, cardID); got.Status != model.DelayedStatusPending || got.Attempts != 0 {
		t.Errorf("富卡行被回扫改写: status=%q attempts=%d", got.Status, got.Attempts)
	}
	if n := len(hubOutbound(t, db, "conv_card")); n != 0 {
		t.Errorf("富卡行不应由桥接管道投递, got %d 条", n)
	}
	replayed2, _ := svc.ReplayDelayedOutbound(ctx, "tg", "acc_taken", 50)
	if replayed2 != 0 {
		t.Errorf("sending 行不得被回扫再投, got %d", replayed2)
	}
	if got := readDelayed(t, db, claimedID); got.Status != model.DelayedStatusSending {
		t.Errorf("主链路抢占态被改写: status=%q", got.Status)
	}
}

// TestOfflineReplayService_ConvergesAfterRetries 投递持续失败必须收敛，不能每 5 分钟重投到永远。
func TestOfflineReplayService_ConvergesAfterRetries(t *testing.T) {
	svc, db := setupOfflineReplaySvc(t)
	ctx := context.Background()

	// 占位账号会被出站管道拒收 → 每轮都失败
	id := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "tg", AccountID: "tg-unknown", ConversationID: "conv_persist_fail",
		Content: "投不出去的历史回复", Kind: model.DelayedKindSendRetry,
	})

	for round := 1; round <= sendRetryMaxAttempts; round++ {
		if _, failed := svc.ReplayDelayedOutbound(ctx, "tg", "tg-unknown", 50); failed != 1 {
			t.Fatalf("第 %d 轮应有 1 条失败, got %d", round, failed)
		}
		got := readDelayed(t, db, id)
		if got.Status != model.DelayedStatusPending {
			t.Fatalf("第 %d 轮失败后应回到 pending, got %q", round, got.Status)
		}
		if got.Attempts != round {
			t.Fatalf("第 %d 轮 attempts=%d（每轮只能 +1）", round, got.Attempts)
		}
		if got.LastError == "" {
			t.Fatalf("第 %d 轮失败原因未落库", round)
		}
	}

	// 次数用尽的下一轮：判弃收口，且不再尝试投递
	if _, failed := svc.ReplayDelayedOutbound(ctx, "tg", "tg-unknown", 50); failed != 0 {
		t.Errorf("用尽后不应再计失败, got %d", failed)
	}
	got := readDelayed(t, db, id)
	if got.Status != model.DelayedStatusFailed {
		t.Errorf("用尽后 status=%q want %q", got.Status, model.DelayedStatusFailed)
	}
	if got.Attempts != sendRetryMaxAttempts {
		t.Errorf("判弃不应再累计次数: attempts=%d", got.Attempts)
	}
	if n := len(hubOutbound(t, db, "conv_persist_fail")); n != 0 {
		t.Errorf("全程未成功，出站管道不该有落库行, got %d", n)
	}
}

// seedSvcBridgeAccount 造一条渠道账号行（回扫的检测数据源）。
func seedSvcBridgeAccount(t *testing.T, db *gorm.DB, channel, accountID, status string, lastSyncAt *time.Time) {
	t.Helper()
	acc := &model.BridgeAccount{Channel: channel, AccountID: accountID, Status: status, LastSyncAt: lastSyncAt}
	if err := db.WithContext(context.Background()).Create(acc).Error; err != nil {
		t.Fatalf("造渠道账号行失败: %v", err)
	}
}

// TestOfflineReplayService_RunOnce_ActuallyReplays 钉 R14 的后果面：渠道检测的两条 SQL
// 引用了无人建立的列（bridge_metrics 没有渠道维度、bridge_accounts 的渠道列叫 channel），
// 于是 RunOnce 恒在检测这一步拿到空集合 ⇒ 到期 pending 行一条都不投，且只在日志里留一行 Warn。
func TestOfflineReplayService_RunOnce_ActuallyReplays(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{}, &model.BridgeAccount{})
	setupBridgeWhitelistForTest(t, "tg")
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	prev := GlobalInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(prev) })
	svc := NewBridgeOfflineReplayService().WithDB(db)
	ctx := context.Background()
	// 本用例只钉检测腿（R14）：补投门显式全放行，免得探针默认值变化把它变成门的用例。
	useOnlineProbe(t, func(context.Context, string, string) bool { return true })

	now := time.Now()
	stale := now.Add(-time.Hour)
	seedSvcBridgeAccount(t, db, "tg", "acc_run", "online", &now)
	seedSvcBridgeAccount(t, db, "tg", "acc_backlog", "offline", &stale)
	id := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "tg", AccountID: "acc_run", ConversationID: "conv_run",
		Content: "回扫整腿应投递的历史回复", Kind: model.DelayedKindQuietHours,
	})

	stats := svc.RunOnce(ctx)
	if stats.ScannedChannels != 2 {
		t.Fatalf("渠道检测未取到已注册的渠道: scanned=%d（R14：SQL 引用无人建立的列 ⇒ 恒 0）", stats.ScannedChannels)
	}
	if stats.OnlineChannels != 1 || stats.OfflineChannels != 1 {
		t.Errorf("报告未区分两侧: online=%d offline=%d want 1/1", stats.OnlineChannels, stats.OfflineChannels)
	}
	if len(stats.OfflineSnapshots) != 1 || stats.OfflineSnapshots[0].AccountID != "acc_backlog" {
		t.Errorf("离线快照错: %+v", stats.OfflineSnapshots)
	}
	if stats.ReplayedMessages != 1 {
		t.Fatalf("RunOnce 应重放 1 条, got %d", stats.ReplayedMessages)
	}
	if got := readDelayed(t, db, id); got.Status != model.DelayedStatusSent {
		t.Errorf("重放后 status=%q want %q", got.Status, model.DelayedStatusSent)
	}
	if n := len(hubOutbound(t, db, "conv_run")); n != 1 {
		t.Errorf("出站管道应收到 1 条, got %d", n)
	}
}

// TestOfflineReplayService_DetectPartitionsByStatus 在线/离线两侧必须互斥且各归其位：
// 两侧同源于同一份 bridge_accounts 快照，旧实现里"在线"这一侧根本不存在（只有恒报错的检测）。
func TestOfflineReplayService_DetectPartitionsByStatus(t *testing.T) {
	db := testutil.NewTestDB(t, &model.BridgeAccount{})
	svc := NewBridgeOfflineReplayService().WithDB(db)
	ctx := context.Background()

	now := time.Now()
	stale := now.Add(-time.Hour)
	seedSvcBridgeAccount(t, db, "douyin", "acc-on", "online", &now)
	seedSvcBridgeAccount(t, db, "xiaohongshu", "acc-off", "offline", &stale)
	// status 是唯一判据：last_sync_at 陈旧但未被置离线的渠道仍算在线
	// （刷新在线位由 SSE 连接/心跳负责，回扫不得用时间窗自己发明一套判定）。
	seedSvcBridgeAccount(t, db, "tiktok", "acc-stale-online", "online", &stale)

	on, err := svc.DetectOnlineChannels(ctx)
	if err != nil {
		t.Fatalf("DetectOnlineChannels: %v", err)
	}
	off, err := svc.DetectOfflineChannels(ctx)
	if err != nil {
		t.Fatalf("DetectOfflineChannels: %v", err)
	}
	if len(on)+len(off) != 3 {
		t.Fatalf("两侧并集应覆盖全部渠道: on=%d off=%d", len(on), len(off))
	}
	onIDs := map[string]string{}
	for _, c := range on {
		onIDs[c.AccountID] = c.Platform
	}
	for _, c := range off {
		if _, dup := onIDs[c.AccountID]; dup {
			t.Errorf("渠道 %s 同时出现在在线与离线两侧", c.AccountID)
		}
	}
	if onIDs["acc-on"] != "douyin" || onIDs["acc-stale-online"] != "tiktok" {
		t.Errorf("在线侧错: %+v", onIDs)
	}
	if len(off) != 1 || off[0].AccountID != "acc-off" || off[0].Platform != "xiaohongshu" {
		t.Errorf("离线侧错: %+v", off)
	}
}
