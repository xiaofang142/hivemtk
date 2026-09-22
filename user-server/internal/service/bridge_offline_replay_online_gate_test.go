// bridge_offline_replay_online_gate_test.go 补投门：在线位为真的渠道才回扫延后出站。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// useOnlineProbe 换掉进程级在线探针，跑完还原（探针是全局态，不清理会串到别的用例）。
func useOnlineProbe(t *testing.T, fn func(ctx context.Context, channel, accountID string) bool) {
	t.Helper()
	prev := loadBridgeChannelOnlineProbe()
	storeBridgeChannelOnlineProbe(fn)
	probeWarned.Store(false)
	t.Cleanup(func() {
		storeBridgeChannelOnlineProbe(prev)
		probeWarned.Store(false)
	})
}

// TestBridgeChannelOnline_UninjectedProbeFailsOpen 探针未装配（单进程局部装配 / 测试现场）
// 时必须放行并留一次 Warn：门缺件只能退化成"照旧补投"，不能变成"谁都不投"。
func TestBridgeChannelOnline_UninjectedProbeFailsOpen(t *testing.T) {
	useOnlineProbe(t, nil)
	if !bridgeChannelOnline(context.Background(), "douyin", "acc-any") {
		t.Error("探针缺件时判离线（延后出站会被永久扣住）")
	}
	if !probeWarned.Load() {
		t.Error("探针缺件未在日志留痕（退化会被无声吞掉）")
	}
	if !bridgeChannelOnline(context.Background(), "douyin", "acc-again") {
		t.Error("已告警过一次后不再放行")
	}
}

// TestBridgeChannelOnline_ReportsProbeVerbatim 探针在场时逐字转达订阅真值，
// 且渠道/账号必须原样透传（回扫按渠道遍历，串了键就判错人）。
func TestBridgeChannelOnline_ReportsProbeVerbatim(t *testing.T) {
	var gotChannel, gotAccount string
	useOnlineProbe(t, func(_ context.Context, channel, accountID string) bool {
		gotChannel, gotAccount = channel, accountID
		return channel == "douyin" && accountID == "acc-live"
	})

	if !bridgeChannelOnline(context.Background(), "douyin", "acc-live") {
		t.Fatal("在线渠道被判离线")
	}
	if gotChannel != "douyin" || gotAccount != "acc-live" {
		t.Fatalf("透传错账号: %s/%s", gotChannel, gotAccount)
	}
	if bridgeChannelOnline(context.Background(), "xiaohongshu", "acc-live") {
		t.Error("非在线渠道被判在线")
	}
	if probeWarned.Load() {
		t.Error("探针在场却报缺件")
	}
}

// TestOfflineReplay_RunOnce_SkipsChannelsWithoutLiveSubscriber 扩展没连 SSE 时，
// 回扫必须把这串渠道整条跳过、一行都不碰。
//
// 不加这道门的后果不是"白跑一趟"：每条到期行会被置 sending、投递必然找不到连接、
// 回到 pending 并 attempts+1，三轮之后判弃——**只是用户暂时关着浏览器**的回复被永久丢掉。
// 跳过即整条渠道不进入状态机，行留在 pending，等重连后的那一轮补投。
func TestOfflineReplay_RunOnce_SkipsChannelsWithoutLiveSubscriber(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{}, &model.BridgeAccount{})
	setupBridgeWhitelistForTest(t, "douyin")
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	prev := GlobalInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(prev) })
	svc := NewBridgeOfflineReplayService().WithDB(db)
	ctx := context.Background()

	seedSvcBridgeAccount(t, db, "douyin", "acc-dark", "online", nil)
	id := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "douyin", AccountID: "acc-dark", ConversationID: "conv_dark",
		Content: "扩展掉线期间攒下的回复", Kind: model.DelayedKindQuietHours,
	})
	useOnlineProbe(t, func(context.Context, string, string) bool { return false })

	stats := svc.RunOnce(ctx)
	if stats.ScannedChannels != 1 || stats.SkippedOffline != 1 {
		t.Fatalf("1 个渠道应全部因无订阅被跳过: %+v", stats)
	}
	if stats.ReplayedMessages != 0 || stats.FailedMessages != 0 {
		t.Errorf("无在线渠道仍投递: replayed=%d failed=%d", stats.ReplayedMessages, stats.FailedMessages)
	}
	if n := len(hubOutbound(t, db, "conv_dark")); n != 0 {
		t.Errorf("无在线订阅仍落出站 %d 行", n)
	}
	if got := readDelayed(t, db, id); got.Status != model.DelayedStatusPending || got.Attempts != 0 {
		t.Errorf("跳过的渠道把行改写了: status=%q attempts=%d（应为待投、零失败计数）", got.Status, got.Attempts)
	}
}

// TestOfflineReplay_RunOnce_ReplaysWhenSubscriberLive 有活着的 SSE 订阅时正常补投，
// 门不得把可达渠道也一起挡掉（挡掉＝消息永久闷在待办集合里，没有任何人再来取）。
func TestOfflineReplay_RunOnce_ReplaysWhenSubscriberLive(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{}, &model.BridgeAccount{})
	setupBridgeWhitelistForTest(t, "douyin")
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	prev := GlobalInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(prev) })
	svc := NewBridgeOfflineReplayService().WithDB(db)
	ctx := context.Background()

	seedSvcBridgeAccount(t, db, "douyin", "acc-dark", "online", nil)
	seedSvcBridgeAccount(t, db, "douyin", "acc-live", "online", nil)
	darkID := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "douyin", AccountID: "acc-dark", ConversationID: "conv_dark2",
		Content: "这条还没轮到", Kind: model.DelayedKindQuietHours,
	})
	liveID := seedDelayed(t, db, &model.DelayedOutboundReply{
		Platform: "douyin", AccountID: "acc-live", ConversationID: "conv_live",
		Content: "重连后补投的回复", Kind: model.DelayedKindQuietHours,
	})
	useOnlineProbe(t, func(_ context.Context, _, accountID string) bool { return accountID == "acc-live" })

	stats := svc.RunOnce(ctx)
	if stats.SkippedOffline != 1 {
		t.Errorf("掉线渠道应计 1 次跳过: %+v", stats)
	}
	if stats.ReplayedMessages != 1 {
		t.Errorf("在线渠道未被补投: %+v", stats)
	}
	if got := readDelayed(t, db, liveID); got.Status != model.DelayedStatusSent {
		t.Errorf("在线渠道行未收口: status=%q", got.Status)
	}
	if got := readDelayed(t, db, darkID); got.Status != model.DelayedStatusPending {
		t.Errorf("掉线渠道行被误投: status=%q", got.Status)
	}
	if n := len(hubOutbound(t, db, "conv_live")); n != 1 {
		t.Errorf("在线渠道补投落库 %d 行, want 1", n)
	}
}

// TestOfflineReplay_RunOnce_ProbeReadOncePerChannel 一轮回扫里每个渠道只问一次在线位：
// 逐条行都问一次会在几十万历史行规模下把订阅表锁读成热点。
func TestOfflineReplay_RunOnce_ProbeReadOncePerChannel(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{}, &model.BridgeAccount{})
	setupBridgeWhitelistForTest(t, "douyin")
	ingress := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	prev := GlobalInboxIngressService()
	SetGlobalInboxIngressService(ingress)
	t.Cleanup(func() { SetGlobalInboxIngressService(prev) })
	svc := NewBridgeOfflineReplayService().WithDB(db)

	for i := 0; i < 3; i++ {
		seedDelayed(t, db, &model.DelayedOutboundReply{
			Platform: "douyin", AccountID: "acc-count", ConversationID: "conv_count",
			Content: time.Now().String(), Kind: model.DelayedKindQuietHours,
		})
	}
	seedSvcBridgeAccount(t, db, "douyin", "acc-count", "online", nil)

	var calls int
	useOnlineProbe(t, func(context.Context, string, string) bool {
		calls++
		return false
	})
	svc.RunOnce(context.Background())
	if calls != 1 {
		t.Errorf("每渠道应只问一次在线位, got %d（整轮 3 条历史行）", calls)
	}
}

type replayProbeCtxKey struct{}

// TestOfflineReplay_RunOnce_ForwardsCtxToProbe 回扫自己的 ctx 必须原样交给探针：
// 探针如今要读账号行（轮询模式的在线位落在 DB 里），中途换成 context.Background()
// 就同时丢掉调用方的截止时间与 trace 链路——停用信号传不进去，这轮读会跑完才回。
func TestOfflineReplay_RunOnce_ForwardsCtxToProbe(t *testing.T) {
	db := testutil.NewTestDB(t, &model.DelayedOutboundReply{}, &model.MessageHub{}, &model.BridgeAccount{})
	svc := NewBridgeOfflineReplayService().WithDB(db)
	seedSvcBridgeAccount(t, db, "douyin", "acc-ctx", "online", nil)

	var gotCtx context.Context
	useOnlineProbe(t, func(ctx context.Context, _, _ string) bool {
		gotCtx = ctx
		return false
	})
	svc.RunOnce(context.WithValue(context.Background(), replayProbeCtxKey{}, "marker"))

	if gotCtx == nil {
		t.Fatal("探针没拿到 ctx")
	}
	if gotCtx.Value(replayProbeCtxKey{}) != "marker" {
		t.Error("回扫的 ctx 未透传给探针（读库会脱离调用方的截止与链路）")
	}
}
