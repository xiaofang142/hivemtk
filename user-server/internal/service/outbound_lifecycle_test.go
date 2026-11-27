package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type lifecycleStoreStub struct {
	mu           sync.Mutex
	rows         map[string]*model.MessageHub
	prepareErr   error
	settleErr    error
	prepareCalls int
	settleCalls  int
}

func newLifecycleStoreStub() *lifecycleStoreStub {
	return &lifecycleStoreStub{rows: map[string]*model.MessageHub{}}
}

func cloneLifecycleRow(row *model.MessageHub) *model.MessageHub {
	if row == nil {
		return nil
	}
	copyRow := *row
	copyRow.AudienceJSON = append(model.JSONStrings{}, row.AudienceJSON...)
	copyRow.Extra = model.JSONMap{}
	for key, value := range row.Extra {
		copyRow.Extra[key] = value
	}
	return &copyRow
}

func (s *lifecycleStoreStub) PrepareOutbound(_ context.Context, row *model.MessageHub) (*model.MessageHub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prepareCalls++
	if s.prepareErr != nil {
		return nil, s.prepareErr
	}
	correlationID := hubCorrelationID(row.Extra)
	if existing := s.rows[correlationID]; existing != nil {
		return cloneLifecycleRow(existing), nil
	}
	copyRow := cloneLifecycleRow(row)
	if copyRow.ID == 0 {
		copyRow.ID = uint(len(s.rows) + 1)
	}
	s.rows[correlationID] = copyRow
	return cloneLifecycleRow(copyRow), nil
}

func (s *lifecycleStoreStub) SettleOutbound(_ context.Context, settlement repository.OutboundSettlement) (*model.MessageHub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settleCalls++
	if s.settleErr != nil {
		return nil, s.settleErr
	}
	for _, row := range s.rows {
		if row.ID == settlement.RowID {
			row.Status = settlement.Status
			row.PlatformMsgID = settlement.PlatformMsgID
			row.Extra["receipts"] = settlement.Receipts
			row.Extra["partial"] = settlement.Partial
			row.Extra["unknown"] = settlement.Unknown
			for key, value := range settlement.Extra {
				row.Extra[key] = value
			}
			if settlement.SentAt != nil {
				row.SentAt = *settlement.SentAt
			}
			return cloneLifecycleRow(row), nil
		}
	}
	return nil, errors.New("lifecycle row not found")
}

func (s *lifecycleStoreStub) AlignOutboundSession(_ context.Context, _ *model.MessageHub, _ *time.Time) error {
	return nil
}

type lifecycleSenderStub struct {
	mu      sync.Mutex
	result  *OutboundResult
	err     error
	calls   int
	content []string
	onSend  func(*OutboundRequest)
}

func (s *lifecycleSenderStub) Channel() WebhookChannel { return ChannelDingTalk }

func (s *lifecycleSenderStub) Send(_ context.Context, req *OutboundRequest) *OutboundResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.content = append(s.content, req.Content)
	if s.onSend != nil {
		s.onSend(req)
	}
	if s.err != nil {
		req.Fail(s.err)
	}
	return s.result
}

func lifecycleTestService(store OutboundLifecycleStore, sender channelSender) *WebhookService {
	return &WebhookService{
		messageHubRepo:         nil,
		outboundLifecycleStore: store,
		senders: map[WebhookChannel]channelSender{
			ChannelDingTalk: sender,
		},
	}
}

func lifecycleSourceHub() *model.MessageHub {
	return &model.MessageHub{
		TenantID:           71,
		Platform:           "dingtalk",
		AccountID:          "107",
		MsgID:              "in-lifecycle",
		Direction:          "inbound",
		MsgType:            "text",
		SenderID:           "customer-71",
		Content:            "question",
		ConversationID:     "conv-lifecycle",
		IsGroup:            true,
		GroupID:            "group-lifecycle",
		Visibility:         model.VisibilityMember,
		AudienceJSON:       model.JSONStrings{"customer-71", "agent-71"},
		VisibilityEvidence: model.JSONMap{"source": "inbound"},
		Extra:              model.JSONMap{"session_webhook": "https://example.invalid/hook", "custom": "keep-me"},
		SentAt:             time.Now().Add(-time.Minute),
	}
}

// canonical pending 行必须在 sender 网络调用前存在，并且发送成功后只结算同一行。
func TestSendOutbound_LifecyclePersistsBeforeTransportAndSettlesSameRow(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{result: &OutboundResult{
		Sent:          true,
		PlatformMsgID: "platform-1",
		Receipts:      []OutboundArtifactReceipt{{ArtifactID: "text", PlatformMsgID: "platform-1", Status: OutboundArtifactSent}},
	}}
	sender.onSend = func(req *OutboundRequest) {
		canonical := outboundCanonicalFromContext(context.Background())
		if canonical != nil {
			t.Errorf("sender 不应从空 ctx 取得 canonical 行")
		}
		if req.Hub == nil || req.Hub.ID == 0 || req.Hub.Status != OutboundStatusPending {
			t.Errorf("sender 收到的必须是已持久化 pending canonical 行，got %+v", req.Hub)
		}
		if store.prepareCalls != 1 {
			t.Errorf("网络调用前 PrepareOutbound 应恰好一次，got %d", store.prepareCalls)
		}
	}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)
	ctx = OutboundCorrelationToContext(ctx, "corr-lifecycle-1")

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: "evt-lifecycle-1", Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if !sent || sendErr != nil {
		t.Fatalf("成功投递应返回 sent=true，got sent=%v err=%v", sent, sendErr)
	}
	if sender.calls != 1 || store.prepareCalls != 1 || store.settleCalls != 1 {
		t.Fatalf("调用次数不符 sender=%d prepare=%d settle=%d", sender.calls, store.prepareCalls, store.settleCalls)
	}
	row := store.rows["corr-lifecycle-1"]
	if row == nil || row.Status != OutboundStatusSent || row.PlatformMsgID != "platform-1" {
		t.Fatalf("同一 canonical 行应结算为 sent 并保留回执，got %+v", row)
	}
	if row.Extra["custom"] != "keep-me" || row.Extra["origin"] != string(OutboundOriginAI) {
		t.Fatalf("原 Extra 与 AI origin 丢失，got %+v", row.Extra)
	}
	if !row.IsGroup || row.GroupID != "group-lifecycle" || row.Visibility != model.VisibilityMember || len(row.AudienceJSON) != 2 {
		t.Fatalf("群聊 scope/visibility/audience 丢失，got %+v", row)
	}
}

// 持久化准备失败必须 fail-closed，sender 一次都不能被调用。
func TestSendOutbound_LifecyclePrepareFailureBlocksTransport(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	store.prepareErr = errors.New("prepare failed")
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true}}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: "evt-lifecycle-blocked", Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if sent || sendErr == nil {
		t.Fatalf("准备失败必须返回未发送与错误，got sent=%v err=%v", sent, sendErr)
	}
	if sender.calls != 0 {
		t.Fatalf("持久化失败后不得发起网络调用，got %d", sender.calls)
	}
}

// 可重试失败只把同一 canonical 行改回 pending；再次投递复用 correlation 与行 ID。
func TestSendOutbound_LifecycleRetryReusesIdentity(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{err: errors.New("connection refused")}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)
	ctx = OutboundCorrelationToContext(ctx, "corr-retry")
	payload := &ParsedPayload{EventID: "evt-lifecycle-retry", Sender: "customer-71"}

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", payload, "hello", lifecycleSourceHub(), nil)
	if sent || sendErr == nil {
		t.Fatalf("首次网络失败应如实返回，got sent=%v err=%v", sent, sendErr)
	}
	first := store.rows["corr-retry"]
	if first == nil || first.Status != OutboundStatusPending {
		t.Fatalf("可重试失败应保留同一行为 pending，got %+v", first)
	}

	sender.err = nil
	sender.result = &OutboundResult{Sent: true, PlatformMsgID: "platform-retry"}
	sent, sendErr = svc.sendOutbound(ctx, ChannelDingTalk, "107", payload, "hello", lifecycleSourceHub(), nil)
	if !sent || sendErr != nil {
		t.Fatalf("重试应成功，got sent=%v err=%v", sent, sendErr)
	}
	if len(store.rows) != 1 || store.rows["corr-retry"].ID != first.ID || store.rows["corr-retry"].Status != OutboundStatusSent {
		t.Fatalf("重试不得新建 canonical 行，rows=%+v", store.rows)
	}
}

// unknown 是独立终态，不能当成 sent，也不能自动进入重试。
func TestSendOutbound_LifecycleUnknownDoesNotBecomeSuccess(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{result: &OutboundResult{Unknown: true}}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: "evt-lifecycle-unknown", Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if sent || sendErr == nil {
		t.Fatalf("unknown 不得伪报成功，got sent=%v err=%v", sent, sendErr)
	}
	row := store.rows[hubCorrelationID(lifecycleSourceHub().Extra)]
	for _, candidate := range store.rows {
		if strings.Contains(candidate.MsgID, "out-") {
			row = candidate
		}
	}
	if row == nil || row.Status != OutboundStatusUnknown {
		t.Fatalf("unknown 应落同一 canonical 行，got %+v", row)
	}
}

// 分块/多 artifact 只产生一条逻辑回复行，回执逐项保留。
func TestSendOutbound_LifecycleChunkReceiptsDoNotDuplicateRows(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{result: &OutboundResult{
		Sent: true,
		Receipts: []OutboundArtifactReceipt{
			{ArtifactID: "chunk-1", PlatformMsgID: "p1", Status: OutboundArtifactSent},
			{ArtifactID: "chunk-2", PlatformMsgID: "p2", Status: OutboundArtifactSent},
			{ArtifactID: "chunk-3", PlatformMsgID: "p3", Status: OutboundArtifactSent},
		},
	}}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)
	ctx = OutboundCorrelationToContext(ctx, "corr-chunks")

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: "evt-lifecycle-chunks", Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if !sent || sendErr != nil || len(store.rows) != 1 {
		t.Fatalf("分块应只有一条 canonical 行，sent=%v err=%v rows=%d", sent, sendErr, len(store.rows))
	}
	receipts, ok := store.rows["corr-chunks"].Extra["receipts"].([]OutboundArtifactReceipt)
	if !ok || len(receipts) != 3 {
		t.Fatalf("应保留逐分块回执，got %#v", store.rows["corr-chunks"].Extra["receipts"])
	}
}

// 人工回复必须显式落 origin=human，不能沿用 AI 默认值。
func TestSendOutbound_LifecycleHumanOrigin(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true}}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)
	ctx = OutboundOriginToContext(ctx, OutboundOriginHuman)
	ctx = OutboundCorrelationToContext(ctx, "corr-human")

	sent, sendErr := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: "evt-lifecycle-human", Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if !sent || sendErr != nil {
		t.Fatalf("人工回复应成功，got sent=%v err=%v", sent, sendErr)
	}
	row := store.rows["corr-human"]
	if row == nil || row.IsAIReply || row.Extra["origin"] != string(OutboundOriginHuman) {
		t.Fatalf("人工 origin 不得硬编码为 AI，got %+v", row)
	}
}

// claim 未取得的 false,nil 只能表示未投递，不能被调用方解释为成功。
func TestSendOutbound_LifecycleClaimFalseNilIsNotSuccess(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	const eventID = "evt-lifecycle-claimed"
	releaseReplyClaimForTest(t, eventID)
	if !agent_runtime.ClaimReply(eventID) {
		t.Fatal("测试前置 claim 未成立")
	}
	defer releaseReplyClaimForTest(t, eventID)
	store := newLifecycleStoreStub()
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true}}
	svc := lifecycleTestService(store, sender)
	ctx := tenant.WithTenantID(context.Background(), 71)

	sent, _ := svc.sendOutbound(ctx, ChannelDingTalk, "107", &ParsedPayload{EventID: eventID, Sender: "customer-71"}, "hello", lifecycleSourceHub(), nil)
	if sent {
		t.Fatal("重复 claim 的 false,nil 不得变成发送成功")
	}
	if sender.calls != 0 {
		t.Fatalf("重复 claim 不得调用 sender，got %d", sender.calls)
	}
}
