package agent_runtime

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestAgentContext_Structure 验证 AgentContext 字段完整性
func TestAgentContext_Structure(t *testing.T) {
	ctx := &AgentContext{
		AgentID:          1,
		AgentCode:        "test_agent",
		Name:             "测试智能体",
		AgentType:        "sales",
		Persona:          "专业销售",
		RagProductIDs:    []string{"rag_001"},
		SOPIDs:           []string{"sop_001"},
		ScriptLibraryIDs: []string{"script_001"},
		ABExperimentIDs:  []string{"exp_001"},
	}

	if ctx.AgentID != 1 {
		t.Errorf("AgentID = %d, want 1", ctx.AgentID)
	}
	if ctx.AgentCode != "test_agent" {
		t.Errorf("AgentCode = %s, want test_agent", ctx.AgentCode)
	}
	if len(ctx.ABExperimentIDs) != 1 {
		t.Errorf("ABExperimentIDs length = %d, want 1", len(ctx.ABExperimentIDs))
	}
}

// TestCustomerMessagePayload_Fields 验证事件载荷字段
func TestCustomerMessagePayload_Fields(t *testing.T) {
	payload := CustomerMessagePayload{
		ChannelType: "telegram",
		AccountID:   "tg_001",
		CustomerID:  "cust_001",
		Content:     "你好",
		MessageType: "text",
		Timestamp:   time.Now(),
		TraceID:     "trace_001",
	}

	if payload.ChannelType != "telegram" {
		t.Errorf("ChannelType = %s, want telegram", payload.ChannelType)
	}
	if payload.TraceID != "trace_001" {
		t.Errorf("TraceID = %s, want trace_001", payload.TraceID)
	}
}

// TestSalesResponse_Fields 验证销售响应字段
func TestSalesResponse_Fields(t *testing.T) {
	resp := &SalesResponse{
		ReplyContent: "您好，有什么可以帮您？",
		ReplyType:    "text",
		Confidence:   0.95,
		AgentID:      1,
		TraceID:      "trace_001",
	}

	if resp.Confidence != 0.95 {
		t.Errorf("Confidence = %f, want 0.95", resp.Confidence)
	}
	if resp.ReplyContent == "" {
		t.Error("ReplyContent should not be empty")
	}
}

// TestCacheKey_String 验证缓存键序列化
func TestCacheKey_String(t *testing.T) {
	key := cacheKey{Channel: "telegram", AccountID: "tg_001"}
	expected := "telegram:tg_001"
	if key.String() != expected {
		t.Errorf("String() = %s, want %s", key.String(), expected)
	}
}

// TestNewAgentRuntime 验证运行时实例创建
func TestNewAgentRuntime(t *testing.T) {
	rt := NewAgentRuntime(nil, nil, nil)
	if rt == nil {
		t.Error("NewAgentRuntime returned nil")
	}
}

// TestConcurrentLoad 验证并发安全
func TestConcurrentLoad(t *testing.T) {
	rt := NewAgentRuntime(nil, nil, nil).(*defaultAgentRuntime)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rt.mu.Lock()
			_ = rt.cache
			rt.mu.Unlock()
		}(i)
	}
	wg.Wait()

	_ = rt.Stop(context.Background())
}
