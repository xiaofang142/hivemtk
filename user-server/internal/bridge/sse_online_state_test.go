// sse_online_state_test.go 桥接在线信号的口径：总线订阅态是在线的唯一权威来源。
package bridge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestSSEBus_HasSubscribers 补投门的真值来源：账号级订阅在/不在必须判得出来，
// 且取消订阅后立刻转 false —— 否则回扫会永远以为扩展还挂着，把门变成没加。
func TestSSEBus_HasSubscribers(t *testing.T) {
	b := NewSSEBus()
	if b.HasSubscribers("douyin", "acc1") {
		t.Fatal("无人订阅却判在线")
	}
	_, cancel := b.Subscribe("douyin", "acc1")
	if !b.HasSubscribers("douyin", "acc1") {
		t.Fatal("有活订阅者却判离线")
	}
	if b.HasSubscribers("douyin", "other") {
		t.Error("别的账号订阅不得让 acc1 看起来在线")
	}
	if b.HasSubscribers("xiaohongshu", "acc1") {
		t.Error("别的渠道订阅不得让 acc1 看起来在线")
	}
	cancel()
	if b.HasSubscribers("douyin", "acc1") {
		t.Error("取消订阅后仍判在线 ⇒ 门会永久放行离线渠道")
	}
	// 空渠道/空账号是缺参而不是可寻址账号：缺参键不得被认成"有人在线"，
	// 否则一条拼错的 ":acc" 订阅就能让真实渠道通过补投门。
	if b.HasSubscribers("", "acc1") || b.HasSubscribers("douyin", "") {
		t.Error("缺参调用被判在线")
	}
	_, cancelBlank := b.Subscribe("", "acc-blank")
	if b.HasSubscribers("", "acc-blank") {
		t.Error("空渠道订阅不得构成在线位")
	}
	cancelBlank()
}

// TestSSEBus_HasSubscribers_MultiSubscriberLastOneOut 同一账号多条并发连接时，
// 只有最后一条退出才算离线。
func TestSSEBus_HasSubscribers_MultiSubscriberLastOneOut(t *testing.T) {
	b := NewSSEBus()
	_, cancelA := b.Subscribe("douyin", "acc-multi")
	_, cancelB := b.Subscribe("douyin", "acc-multi")

	cancelA()
	if !b.HasSubscribers("douyin", "acc-multi") {
		t.Fatal("还有一条活连接却判离线（会误置 status=offline）")
	}
	cancelB()
	if b.HasSubscribers("douyin", "acc-multi") {
		t.Error("两条都退了还判在线")
	}
}

// --- SSE 生命周期在线信号（R15）：连接/心跳刷新，最后一条流退出才置离线 ---

type recordingBridgeAccountRepo struct {
	touches     atomic.Int64
	offlines    atomic.Int64
	touchKeys   chan string
	offlineKeys chan string
}

func newRecordingBridgeAccountRepo() *recordingBridgeAccountRepo {
	return &recordingBridgeAccountRepo{
		touchKeys:   make(chan string, 64),
		offlineKeys: make(chan string, 8),
	}
}

func (f *recordingBridgeAccountRepo) Upsert(context.Context, BridgeAccountUpsert) error { return nil }
func (f *recordingBridgeAccountRepo) SetOffline(_ context.Context, channel, accountID string) error {
	f.offlines.Add(1)
	f.offlineKeys <- channel + ":" + accountID
	return nil
}
func (f *recordingBridgeAccountRepo) TouchLastSync(_ context.Context, channel, accountID string) error {
	f.touches.Add(1)
	f.touchKeys <- channel + ":" + accountID
	return nil
}
func (f *recordingBridgeAccountRepo) ListByUser(context.Context, uint) ([]BridgeAccountView, error) {
	return nil, nil
}
func (f *recordingBridgeAccountRepo) IsOnline(context.Context, string, string) (bool, error) {
	return false, nil
}

// startOnlineSignalSSEServer 起一条短命 SSE 流：心跳 40ms、最长 240ms，跑完自行收尾。
func startOnlineSignalSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	return startSSEServerWithWindow(t, 40*time.Millisecond, 240*time.Millisecond)
}

func startSSEServerWithWindow(t *testing.T, heartbeat, maxDuration time.Duration) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewBridgeIngestHandler(nil)
	h.sseHandler.SetHeartbeat(heartbeat)
	h.sseHandler.SetMaxDuration(maxDuration)
	r := gin.New()
	r.GET("/api/bridge/outbox/sse", h.HandleOutboxSSE)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// openSSEStream 在后台 goroutine 里把流读完，返回「流已结束」的信号通道
// （不在 goroutine 里调 t.Fatalf）。
func openSSEStream(url string) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(url)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
	}()
	return done
}

// nextKey 在限定时间内取一个被记录的键；取不到就是失败，绝不能阻塞等测试总超时。
func nextKey(t *testing.T, ch chan string, within time.Duration) string {
	t.Helper()
	select {
	case k := <-ch:
		return k
	case <-time.After(within):
		t.Fatalf("等待记录键超时（%s）⇒ 该写的一次都没发生", within)
		return ""
	}
}

// waitFor 轮询 cond 直到成立或超时，返回是否成立。
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// drainSSEStream 打开流并读到服务端结束，返回收到的心跳帧数。
func drainSSEStream(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("SSE 请求失败: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读 SSE 流失败: %v", err)
	}
	return strings.Count(string(body), ": ping")
}

func useRecordingRepo(t *testing.T) *recordingBridgeAccountRepo {
	t.Helper()
	fake := newRecordingBridgeAccountRepo()
	prev := GlobalBridgeAccountRepo
	GlobalBridgeAccountRepo = fake
	t.Cleanup(func() { GlobalBridgeAccountRepo = prev })
	return fake
}

// TestHandleOutboxSSE_RefreshesOnlineOnConnectAndHeartbeat 在线信号必须随连接与每次心跳刷新：
// 修之前 TouchLastSync 零调用 ⇒ 注册后 status 恒为 online，断开也读不出来。
func TestHandleOutboxSSE_RefreshesOnlineOnConnectAndHeartbeat(t *testing.T) {
	fake := useRecordingRepo(t)
	srv := startOnlineSignalSSEServer(t)

	pings := drainSSEStream(t, srv.URL+"/api/bridge/outbox/sse?channel=douyin&account_id=acc-life")
	if pings == 0 {
		t.Fatal("流里一个心跳帧都没有 ⇒ 用例没真跑到心跳分支，下面的计数不作数")
	}
	if got := fake.touches.Load(); got < 2 {
		t.Fatalf("connect+heartbeat 应至少刷新 2 次在线位, got %d（pings=%d）", got, pings)
	}
	key := <-fake.touchKeys
	if key != "douyin:acc-life" {
		t.Errorf("在线位刷新写错账号: %q", key)
	}
	if got := fake.offlines.Load(); got != 1 {
		t.Errorf("流结束应置离线 1 次, got %d", got)
	}
	if k := nextKey(t, fake.offlineKeys, 2*time.Second); k != "douyin:acc-life" {
		t.Errorf("离线置错账号: %q", k)
	}
}

// TestHandleOutboxSSE_KeepsOnlineWhileAnotherStreamLives 同一账号还有别的活连接时，
// 先结束的那条不得把整个账号判离线。
func TestHandleOutboxSSE_KeepsOnlineWhileAnotherStreamLives(t *testing.T) {
	fake := useRecordingRepo(t)
	_, cancelOther := GlobalSSEBus.Subscribe("douyin", "acc-share")
	t.Cleanup(cancelOther)

	srv := startOnlineSignalSSEServer(t)
	drainSSEStream(t, srv.URL+"/api/bridge/outbox/sse?channel=douyin&account_id=acc-share")

	if got := fake.offlines.Load(); got != 0 {
		t.Errorf("另一条流仍在线却置了离线: %d 次", got)
	}
	if got := fake.touches.Load(); got == 0 {
		t.Error("在线位一次都没刷新")
	}
}

// TestHandleOutboxSSE_SkipsAccountWritesWhenRepoMissing 未装配账号仓储（单进程局部装配）时
// 不得 panic、也不得因写不了状态而断流。
func TestHandleOutboxSSE_SkipsAccountWritesWhenRepoMissing(t *testing.T) {
	prev := GlobalBridgeAccountRepo
	GlobalBridgeAccountRepo = nil
	t.Cleanup(func() { GlobalBridgeAccountRepo = prev })

	srv := startOnlineSignalSSEServer(t)
	if pings := drainSSEStream(t, srv.URL+"/api/bridge/outbox/sse?channel=douyin&account_id=acc-norepo"); pings == 0 {
		t.Error("仓储未注入时流被提前中断")
	}
}

// TestHandleOutboxSSE_QueryChannelAliasSharesCanonicalKey 扩展用渠道别名（douyin_web）建流时，
// 订阅键与在线位都必须落在归一后的渠道上。
//
// 修之前只有 ingest 侧归一、SSE 侧原样透传：别名客户端挂在 "douyin_web:acc" 键上，
// 而 Publish 用规范渠道 "douyin:acc" 广播 ⇒ 低延迟路永远投不到它，且 TouchLastSync
// 找不到账号行（bridge_accounts 存的是规范渠道）而静默 no-op；
// 补投门按规范渠道问「谁在线」时，这个明显在线的扩展会被判离线、消息闷在待办里。
func TestHandleOutboxSSE_QueryChannelAliasSharesCanonicalKey(t *testing.T) {
	fake := useRecordingRepo(t)
	srv := startSSEServerWithWindow(t, 40*time.Millisecond, time.Second)

	done := openSSEStream(srv.URL + "/api/bridge/outbox/sse?channel=douyin_web&account_id=acc-alias")

	if !waitFor(800*time.Millisecond, func() bool {
		return GlobalSSEBus.HasSubscribers("douyin", "acc-alias")
	}) {
		t.Error("别名建流后规范键上查不到订阅 ⇒ Publish 广播投不到该客户端")
	}
	if GlobalSSEBus.HasSubscribers("douyin_web", "acc-alias") {
		t.Error("订阅仍挂在未归一的别名键上")
	}

	<-done
	if !waitFor(2*time.Second, func() bool { return fake.offlines.Load() == 1 }) {
		t.Fatalf("流结束未置离线（touches=%d）", fake.touches.Load())
	}
	if k := nextKey(t, fake.touchKeys, 2*time.Second); k != "douyin:acc-alias" {
		t.Errorf("在线位落在别名渠道上，bridge_accounts 无此行 ⇒ 刷新静默丢失: %q", k)
	}
	if k := nextKey(t, fake.offlineKeys, 2*time.Second); k != "douyin:acc-alias" {
		t.Errorf("离线位落在别名渠道上: %q", k)
	}
}
