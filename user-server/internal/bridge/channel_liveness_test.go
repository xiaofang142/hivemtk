// channel_liveness_test.go 补投门可达性真值：订阅 OR 最近同步，渠道先归一，读不到真值放行。
package bridge

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- 补投门的可达性真值：订阅 OR 最近同步（轮询下发不留订阅） ----------------

// livenessRepo 装上可编排的在线位 fake。
func livenessRepo(t *testing.T) *recordingBridgeAccountRepo {
	t.Helper()
	return useRecordingRepo(t)
}

// TestBridgeChannelOnline_LiveSubscriberSkipsDB 有活 SSE 订阅时直接放行，不得再读库：
// 回扫每轮每个渠道都要问一次，命中订阅是常态，多一次 SELECT 就是白付的往返。
func TestBridgeChannelOnline_LiveSubscriberSkipsDB(t *testing.T) {
	fake := livenessRepo(t)
	fake.onlineDefault = true
	_, cancel := GlobalSSEBus.Subscribe("douyin", "acc-push")
	t.Cleanup(cancel)

	if !BridgeChannelOnline(context.Background(), "douyin", "acc-push") {
		t.Fatal("有活订阅却判不可达")
	}
	if n := fake.onlineCalls.Load(); n != 0 {
		t.Errorf("订阅命中后仍读了 %d 次库（真值顺序应为订阅优先）", n)
	}
}

// TestBridgeChannelOnline_NormalizesChannelBeforeAsking 渠道别名入参必须先归一再查订阅与账号行。
//
// bridge_accounts 与订阅键都只存规范渠道（v3.17.1 把旧值改成 *_web、v3.18.0 又统一回规范值，
// 现网 70 行实测全为规范渠道），而回扫读的是表里的历史值：不归一时一条别名行
// 永远问不到真值，等价于把这串渠道永久判离线。
func TestBridgeChannelOnline_NormalizesChannelBeforeAsking(t *testing.T) {
	fake := livenessRepo(t)
	fake.onlineByChannel["douyin:acc-alias"] = true
	_, cancel := GlobalSSEBus.Subscribe("douyin", "acc-alias-push")
	t.Cleanup(cancel)

	if !BridgeChannelOnline(context.Background(), "douyin_web", "acc-alias") {
		t.Error("别名入参未归一 ⇒ 查不到账号行的在线位")
	}
	if k := nextKey(t, fake.onlineKeys, 2*time.Second); k != "douyin:acc-alias" {
		t.Errorf("在线位查询用了别名键: %q", k)
	}
	if !BridgeChannelOnline(context.Background(), "douyin_web", "acc-alias-push") {
		t.Error("别名入参未归一 ⇒ 查不到规范键上的订阅")
	}
	if n := fake.onlineCalls.Load(); n != 1 {
		t.Errorf("订阅命中后仍读了库: %d 次", n)
	}
}

// TestBridgeChannelOnline_PollingClientPassesOnRecentSync 长轮询下发（通道C）不建 SSE 订阅，
// 可达性只能来自「最近同步过」——否则 FF_SSE_BRIDGE=0 或 SSE 起不来回退轮询时，
// 延后出站会被整轮跳过、永不补投。
func TestBridgeChannelOnline_PollingClientPassesOnRecentSync(t *testing.T) {
	fake := livenessRepo(t)
	fake.onlineByChannel["douyin:acc-poller"] = true

	if !BridgeChannelOnline(context.Background(), "douyin", "acc-poller") {
		t.Error("刚轮询过的账号被判不可达（补投门会永久扣住它的延后出站）")
	}
}

// TestBridgeChannelOnline_NoSubscriberNoSyncIsOffline 两个信号都没有才算不可达：
// 这是这道门存在的理由——浏览器关掉的这段时间里，行必须留在 pending 而不是被烧进判弃。
func TestBridgeChannelOnline_NoSubscriberNoSyncIsOffline(t *testing.T) {
	fake := livenessRepo(t)
	fake.onlineByChannel["douyin:acc-gone"] = false

	if BridgeChannelOnline(context.Background(), "douyin", "acc-gone") {
		t.Error("无订阅且从未同步却被判可达（门等于没加）")
	}
}

// TestBridgeChannelOnline_FailsOpenWhenTruthUnavailable 在线真值读不到时必须放行，
// 与「探针缺件放行」同一口径：门建不起来只能退化成照旧补投，不能变成谁都不投。
func TestBridgeChannelOnline_FailsOpenWhenTruthUnavailable(t *testing.T) {
	fake := livenessRepo(t)
	fake.onlineErr = errors.New("db down")
	if !BridgeChannelOnline(context.Background(), "douyin", "acc-err") {
		t.Error("读库失败被判离线")
	}

	prev := GlobalBridgeAccountRepo
	GlobalBridgeAccountRepo = nil
	t.Cleanup(func() { GlobalBridgeAccountRepo = prev })
	if !BridgeChannelOnline(context.Background(), "douyin", "acc-norepo") {
		t.Error("账号仓储未装配被判离线")
	}
}

// TestBridgeChannelOnline_EmptyParamIsNotReachable 缺参不是「可寻址的账号」：
// 空账号会命中所有渠道的历史行，放行等于把门拆了。
func TestBridgeChannelOnline_EmptyParamIsNotReachable(t *testing.T) {
	livenessRepo(t).onlineDefault = true
	if BridgeChannelOnline(context.Background(), "douyin", "") || BridgeChannelOnline(context.Background(), "", "acc") {
		t.Error("缺参调用被判可达")
	}
}
