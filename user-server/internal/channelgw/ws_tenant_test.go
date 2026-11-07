package channelgw

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// 渠道 WS 这一口的归属只能由 register 帧的 (channel, account_id) 自证。
// 双泵里的错误全部只落 Warn，连接与心跳看起来一切正常——所以"泵是否带着租户跑"
// 必须能被直接读出来，否则 on 模式下的静默零下发在测试里长得和成功一模一样。

type recordingPipeline struct {
	mu        sync.Mutex
	ingestCtx []uint
	claimCtx  []uint
	ackCtx    []uint
	history   []*model.MessageEvent
	claimQue  []*model.MessageHub
}

func (p *recordingPipeline) record(sink *[]uint, ctx context.Context) {
	tid, _ := tenant.FromContext(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	*sink = append(*sink, tid)
}

func (p *recordingPipeline) IngestBatch(ctx context.Context, events []*model.MessageEvent) (*service.InboxIngressBatchResult, error) {
	p.record(&p.ingestCtx, ctx)
	res := &service.InboxIngressBatchResult{PerEvent: make([]*service.InboxIngressResult, len(events))}
	for i := range events {
		res.PerEvent[i] = &service.InboxIngressResult{Accepted: true}
	}
	return res, nil
}

func (p *recordingPipeline) PersistHistory(ctx context.Context, ev *model.MessageEvent, _ string) error {
	p.record(&p.ingestCtx, ctx)
	p.mu.Lock()
	p.history = append(p.history, ev)
	p.mu.Unlock()
	return nil
}

func (p *recordingPipeline) ClaimOutbound(ctx context.Context, _, _ string, limit int) ([]*model.MessageHub, error) {
	p.record(&p.claimCtx, ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.claimQue) == 0 {
		return nil, nil
	}
	n := min(len(p.claimQue), limit)
	out := p.claimQue[:n]
	p.claimQue = p.claimQue[n:]
	return out, nil
}

func (p *recordingPipeline) AckOutbound(ctx context.Context, _ string, _ string, msgIDs []string) (int, error) {
	p.record(&p.ackCtx, ctx)
	return len(msgIDs), nil
}

// AckOutboundDetailed CH-37b 刀2 接口新增方法：租户透传语义与 AckOutbound 同源（同 ctx 记账），
// 本 fake 不承载业务分类，只保证接口可实现且 ctx 带租户。
func (p *recordingPipeline) AckOutboundDetailed(ctx context.Context, _ string, _ string, msgIDs []string, _, _ string, _ map[string]service.BridgeOutboundAckInput, _ service.AckOutboundClaimFence) (*service.AckOutboundResult, error) {
	p.record(&p.ackCtx, ctx)
	res := &service.AckOutboundResult{Items: make([]service.AckOutboundItem, 0, len(msgIDs))}
	for _, id := range msgIDs {
		res.Items = append(res.Items, service.AckOutboundItem{MsgID: id, Status: model.BridgeAckStatusAcked})
	}
	res.AckedItemsCount = len(msgIDs)
	return res, nil
}

func (p *recordingPipeline) snapshot() (ingest, claim, ack []uint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]uint(nil), p.ingestCtx...), append([]uint(nil), p.claimCtx...), append([]uint(nil), p.ackCtx...)
}

func newTenantAwareServer(t *testing.T, fp IngressPipeline, binder func(*http.Request, string, string) (context.Context, error)) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	tr := NewWSTransport(fp, nil)
	tr.pushInterval = 60 * time.Millisecond
	tr.TenantBinder = binder
	r.GET("/api/ws/channel", tr.HandleWS)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func dialAndRegister(t *testing.T, srv *httptest.Server, channel, accountID string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/channel"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("WS 拨号失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(registerFrame(channel, accountID)); err != nil {
		t.Fatalf("发送 register 帧失败: %v", err)
	}
	return conn
}

// binder 判不清归属 ⇒ 连接必须被显式拒绝（register_reject 帧），且双泵一次都不启动。
func TestWS_TenantBinderRejectionRejectsHandshake(t *testing.T) {
	fp := &recordingPipeline{}
	wantErr := errors.New("该渠道账号尚未归属任何租户，首次上报请携带 tenant_code")
	srv := newTenantAwareServer(t, fp, func(_ *http.Request, _, _ string) (context.Context, error) {
		return nil, wantErr
	})

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws/channel"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("WS 拨号失败: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteJSON(registerFrame("douyin", "acc-new")); err != nil {
		t.Fatalf("发送 register 帧失败: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f Frame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("应收到 register_reject，实际读帧失败: %v", err)
	}
	if f.Type != FrameRegisterReject {
		t.Errorf("帧类型应为 register_reject，实际 %s（reason=%s）", f.Type, f.Reason)
	}
	if !strings.Contains(f.Reason, "tenant_code") {
		t.Errorf("拒绝原因必须带上可自救的提示，实际 %q", f.Reason)
	}

	// 泵没启动的硬证据：拒绝之后发一帧 inbound，管道不会被调用。
	_ = conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{EventID: "ev-after-reject", ConversationID: "cv-1", Content: "hi"}})
	time.Sleep(200 * time.Millisecond)
	ingest, claim, ack := fp.snapshot()
	if len(ingest)+len(claim)+len(ack) != 0 {
		t.Errorf("拒绝后双泵仍打了管道：ingest=%v claim=%v ack=%v", ingest, claim, ack)
	}
}

// binder 绑定成功 ⇒ 入站、下发认领、ack 回写三条链路都必须看到同一个租户。
func TestWS_PumpsCarryBoundTenant(t *testing.T) {
	fp := &recordingPipeline{}
	const wantTenant uint = 7

	var gotChannel, gotAccount string
	var mu sync.Mutex
	srv := newTenantAwareServer(t, fp, func(_ *http.Request, channel, accountID string) (context.Context, error) {
		mu.Lock()
		gotChannel, gotAccount = channel, accountID
		mu.Unlock()
		return tenant.WithTenantID(context.Background(), wantTenant), nil
	})

	conn := dialAndRegister(t, srv, "douyin", "acc-7")

	// ① 入站
	if err := conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{EventID: "ev-in-1", ConversationID: "cv-1", Content: "你好"}}); err != nil {
		t.Fatalf("发送 inbound 失败: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		var f Frame
		if err := conn.ReadJSON(&f); err != nil {
			t.Fatalf("等 inbound ack 失败: %v", err)
		}
		if f.Type == FrameAck && len(f.Results) > 0 {
			break
		}
	}

	// ② ack 回写
	if err := conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameAck, MsgIDs: []string{"m-1"}}); err != nil {
		t.Fatalf("发送 ack 失败: %v", err)
	}

	// ③ 下发认领（pushPump 每 60ms 打一次）
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	chanGot, accGot := gotChannel, gotAccount
	mu.Unlock()
	if chanGot != "douyin" || accGot != "acc-7" {
		t.Errorf("binder 收到的自证入参不符：channel=%q account=%q", chanGot, accGot)
	}

	ingest, claim, ack := fp.snapshot()
	if len(ingest) == 0 || len(claim) == 0 || len(ack) == 0 {
		t.Fatalf("三条链路未全部打到管道：ingest=%d claim=%d ack=%d", len(ingest), len(claim), len(ack))
	}
	for i, tid := range ingest {
		if tid != wantTenant {
			t.Errorf("入站第 %d 次调用没带租户 %d，实际 %d（无归属的写在 on 模式被隔离 callback 判错，只落一行 Warn）", i, wantTenant, tid)
		}
	}
	for i, tid := range claim {
		if tid != wantTenant {
			t.Errorf("下发认领第 %d 次调用没带租户，实际 %d——这条正是「静默零下发」的落点", i, tid)
		}
	}
	for i, tid := range ack {
		if tid != wantTenant {
			t.Errorf("ack 回写第 %d 次调用没带租户，实际 %d", i, tid)
		}
	}
}

// 未注入 binder（off 模式部署、以及历史构造方式）必须保持改造前行为：
// 泵用 context.Background()，不绑任何租户。
func TestWS_NilBinderKeepsLegacyContext(t *testing.T) {
	fp := &recordingPipeline{}
	srv := newTenantAwareServer(t, fp, nil)

	conn := dialAndRegister(t, srv, "douyin", "acc-legacy")
	if err := conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{EventID: "ev-legacy", ConversationID: "cv-2", Content: "hi"}}); err != nil {
		t.Fatalf("发送 inbound 失败: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var f Frame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	if f.Type != FrameAck {
		t.Fatalf("期望 ack 帧，实际 %s", f.Type)
	}

	ingest, _, _ := fp.snapshot()
	if len(ingest) == 0 {
		t.Fatal("入站链路没被打到")
	}
	for i, tid := range ingest {
		if tid != 0 {
			t.Errorf("未注入 binder 时不应带租户，第 %d 次调用 tid=%d", i, tid)
		}
	}
}

// binder 返回 nil ctx 时不得把 nil 传进泵（nil ctx 会 panic 并带走整个进程）。
func TestWS_BinderNilCtxFallsBackToBackground(t *testing.T) {
	fp := &recordingPipeline{}
	srv := newTenantAwareServer(t, fp, func(_ *http.Request, _, _ string) (context.Context, error) {
		return nil, nil
	})

	conn := dialAndRegister(t, srv, "douyin", "acc-nil")
	if err := conn.WriteJSON(&Frame{V: CurrentProtocolVersion, Type: FrameInbound,
		Message: &IngestMessage{EventID: "ev-nil", ConversationID: "cv-3", Content: "hi"}}); err != nil {
		t.Fatalf("发送 inbound 失败: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var f Frame
	if err := conn.ReadJSON(&f); err != nil {
		t.Fatalf("读帧失败（泵可能 panic）: %v", err)
	}
	if f.Type != FrameAck {
		t.Fatalf("期望 ack 帧，实际 %s", f.Type)
	}
	ingest, _, _ := fp.snapshot()
	if len(ingest) == 0 {
		t.Fatal("入站链路没被打到")
	}
}
