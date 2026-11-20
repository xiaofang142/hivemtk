package agent_runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/delivery"
)

// TestDataFlow_DirectReply 场景1: 寒暄直答
func TestDataFlow_DirectReply(t *testing.T) {
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), nil)
	result, err := orch.Process(context.Background(), CustomerMessagePayload{
		ChannelType: "telegram",
		CustomerID:  "d8_1",
		SessionID:   "d8_session_1",
		Content:     "你好",
	}, &AgentContext{AgentCode: "default", EnableRAG: true})

	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if result.HandoffToHuman {
		t.Error("greeting should not handoff")
	}
	if result.SessionID != "d8_session_1" {
		t.Errorf("sessionID = %s, want d8_session_1", result.SessionID)
	}
	if result.AssetContext == nil {
		t.Error("asset context should be loaded")
	}
	if result.CrisisLevel >= CrisisHigh {
		t.Errorf("greeting crisis level = %d, want < High", result.CrisisLevel)
	}
}

// TestDataFlow_HandoffTriggered 场景2: 强制转人工
func TestDataFlow_HandoffTriggered(t *testing.T) {
	escalated := make(chan struct{}, 1)
	escalate := EscalationAdapter{
		Fn: func(_ context.Context, sessionID, reason string) error {
			if sessionID == "" || reason == "" {
				t.Errorf("invalid escalation args: sessionID=%s reason=%s", sessionID, reason)
			}
			select {
			case escalated <- struct{}{}:
			default:
			}
			return nil
		},
	}
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), escalate)
	orch.SetDeliveryGateway(newExecutionDeliveryFixture(func(ctx context.Context, i delivery.Intent) error {
		return escalate.Trigger(ctx, i.SessionID, i.Content)
	}))
	orch.SetExecutionGate(executionGateFunc(func(context.Context, ExecutionRequest) (ExecutionAuthorization, error) { return ExecutionApproved, nil }))
	result, err := orch.ProcessWithMode(executionTestContext(), CustomerMessagePayload{
		MessageID: "handoff-test", AccountID: "test-account",
		ChannelType: "telegram",
		CustomerID:  "d8_2",
		SessionID:   "d8_session_2",
		Content:     "你们是骗子，赶紧退款！",
	}, &AgentContext{AgentCode: "default"}, ExecutionModeExecution)

	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if !result.HandoffToHuman {
		t.Error("expected handoff to human")
	}
	if result.HandoffReason == "" {
		t.Error("expected handoff reason")
	}
	if result.CrisisLevel < CrisisHigh {
		t.Errorf("crisis level = %d, want >= High", result.CrisisLevel)
	}

	select {
	case <-escalated:
	case <-time.After(2 * time.Second):
		t.Fatal("escalation not triggered within 2s")
	}
}

// TestDataFlow_AssetLoader 场景3: 资产加载器
func TestDataFlow_AssetLoader(t *testing.T) {
	loader := &captureAssetLoader{
		captured: make(chan *CustomerMessagePayload, 1),
		assetCtx: &AssetContext{
			L1ShortTerm: map[string]string{"user": "张三"},
			L2Profile:   map[string]string{"level": "VIP"},
			PromptText:  "你是专业客服",
			SystemTools: []string{"order.query"},
		},
	}
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), nil)
	orch.SetAssetLoader(loader)

	result, err := orch.Process(context.Background(), CustomerMessagePayload{
		ChannelType: "telegram",
		CustomerID:  "d8_3",
		SessionID:   "d8_session_3",
		Content:     "查询订单",
	}, &AgentContext{AgentCode: "default", EnableRAG: true})

	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if result.AssetContext == nil {
		t.Fatal("expected asset context")
	}
	if result.AssetContext.L1ShortTerm["user"] != "张三" {
		t.Errorf("L1 user = %s, want 张三", result.AssetContext.L1ShortTerm["user"])
	}
	if result.AssetContext.L2Profile["level"] != "VIP" {
		t.Errorf("L2 level = %s, want VIP", result.AssetContext.L2Profile["level"])
	}
	if result.AssetContext.PromptText != "你是专业客服" {
		t.Errorf("PromptText = %s, want 你是专业客服", result.AssetContext.PromptText)
	}
}

// TestDataFlow_Trimmer 场景4: 裁剪器
func TestDataFlow_Trimmer(t *testing.T) {
	trimmer := defaultTrimmer{}
	tests := []struct {
		input    string
		expected string
	}{
		{"你好```json\n{\"intent\":\"x\"}\n```", "你好"},
		{"已回复```json\n{\"tool\":\"x\"}\n```", "已回复"},
		{"正常文本", "正常文本"},
		{"", ""},
		{"前面```json\n{\"a\":1}\n```后面", "前面后面"},
	}
	for _, tt := range tests {
		got := trimmer.Trim(tt.input)
		if got != tt.expected {
			t.Errorf("Trim(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestDataFlow_Publisher 场景5: 发布器
func TestDataFlow_Publisher(t *testing.T) {
	pub := &capturePublisher{ch: make(chan string, 1)}
	orch, _, _ := executionFixture(t, 0)
	orch.SetDeliveryGateway(newExecutionDeliveryFixture(func(ctx context.Context, i delivery.Intent) error {
		return pub.Publish(ctx, i.Channel, i.CustomerID, i.Content)
	}))

	_, err := orch.ProcessWithMode(executionTestContext(), CustomerMessagePayload{
		MessageID: "publisher-test", AccountID: "test-account",
		ChannelType: "telegram",
		CustomerID:  "d8_5",
		SessionID:   "d8_session_5",
		Content:     "你好",
	}, &AgentContext{AgentCode: "default"}, ExecutionModeExecution)

	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	select {
	case reply := <-pub.ch:
		if reply != "订单正在运输中" {
			t.Fatalf("发布内容未经生成和复核：%q", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("已批准执行没有发布复核后的正文")
	}
}

// TestDataFlow_Stats 统计
func TestDataFlow_Stats(t *testing.T) {
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), nil)
	for i := 0; i < 5; i++ {
		_, _ = orch.Process(context.Background(), CustomerMessagePayload{
			ChannelType: "telegram",
			CustomerID:  "d8_stats",
			SessionID:   "d8_session_stats",
			Content:     "hello",
		}, &AgentContext{AgentCode: "default"})
	}
	stats := orch.GetStats()
	if stats.TotalTasks != 5 {
		t.Errorf("TotalTasks = %d, want 5", stats.TotalTasks)
	}
}

// TestDataFlow_NoSessionID 边界：无 sessionID
func TestDataFlow_NoSessionID(t *testing.T) {
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), nil)
	result, err := orch.Process(context.Background(), CustomerMessagePayload{
		ChannelType: "telegram",
		CustomerID:  "user_no_session",
		Content:     "hello",
	}, &AgentContext{AgentCode: "default"})

	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if result.SessionID == "" {
		t.Error("sessionID should be auto-generated from channel:customer")
	}
	if !strings.HasPrefix(result.SessionID, "telegram:") {
		t.Errorf("sessionID = %s, want prefix 'telegram:'", result.SessionID)
	}
}

// TestDataFlow_Concurrent 并发
func TestDataFlow_Concurrent(t *testing.T) {
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = orch.Process(context.Background(), CustomerMessagePayload{
				ChannelType: "telegram",
				CustomerID:  "user_concurrent",
				SessionID:   "d8_session_concurrent",
				Content:     "hello",
			}, &AgentContext{AgentCode: "default"})
		}(i)
	}
	wg.Wait()
	stats := orch.GetStats()
	if stats.TotalTasks != 20 {
		t.Errorf("TotalTasks = %d, want 20", stats.TotalTasks)
	}
}

// TestDataFlow_EndToEndPipeline 端到端: 多场景跑通
func TestDataFlow_EndToEndPipeline(t *testing.T) {
	escalated := make(chan string, 10)
	orch := NewCoreDataFlowOrchestrator(NewInferenceCycle(), EscalationAdapter{
		Fn: func(_ context.Context, sessionID, reason string) error {
			escalated <- sessionID
			return nil
		},
	})
	orch.SetAssetLoader(&captureAssetLoader{
		assetCtx: &AssetContext{
			L1ShortTerm: map[string]string{"user": "test"},
			L2Profile:   map[string]string{"level": "VIP"},
			PromptText:  "你是专业客服",
		},
	})

	scenarios := []struct {
		name        string
		content     string
		wantHandoff bool
	}{
		{"greeting", "你好", false},
		{"inquiry", "多少钱？", false},
		{"angry", "骗子！退款！", true},
		{"handoff", "转人工", true},
		{"normal", "谢谢", false},
	}
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			result, err := orch.Process(context.Background(), CustomerMessagePayload{
				ChannelType: "telegram",
				CustomerID:  "d8_e2e_" + s.name,
				SessionID:   "d8_session_e2e_" + s.name,
				Content:     s.content,
			}, &AgentContext{AgentCode: "default", EnableRAG: true})

			if err != nil {
				t.Fatalf("process error: %v", err)
			}
			if result.HandoffToHuman != s.wantHandoff {
				t.Errorf("HandoffToHuman=%v, want %v (reason=%s)",
					result.HandoffToHuman, s.wantHandoff, result.HandoffReason)
			}
		})
	}
}

type captureAssetLoader struct {
	captured chan *CustomerMessagePayload
	assetCtx *AssetContext
}

func (c *captureAssetLoader) LoadContext(_ context.Context, p CustomerMessagePayload, _ *AgentContext) (*AssetContext, error) {
	select {
	case c.captured <- &p:
	default:
	}
	if c.assetCtx != nil {
		return c.assetCtx, nil
	}
	return &AssetContext{
		L1ShortTerm: map[string]string{},
		L2Profile:   map[string]string{},
	}, nil
}

type capturePublisher struct {
	ch chan string
}

func (p *capturePublisher) Publish(_ context.Context, _, _, content string) error {
	select {
	case p.ch <- content:
	default:
	}
	return nil
}
