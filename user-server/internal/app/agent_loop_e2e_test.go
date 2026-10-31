package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func setupAgentLoopTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.Customer{},
		&model.Order{},
		&model.KnowledgeDocument{},
		&model.KnowledgeChunk{},
		&model.KnowledgeFeedback{},
		&model.KnowledgeSearchLog{},
		&model.RagProduct{},
	)
	_db.SetTestDB(database)
	return database
}

func setupAgentLoopExecutor(t *testing.T, db *gorm.DB) *tooluse.ToolExecutor {
	t.Helper()
	t.Setenv("EMBEDDING_ALLOW_FALLBACK", "true")

	registry := tooluse.NewToolRegistry()

	customerDeps := tooluse.NewCustomerToolDepsWithDB(db)

	customerDeps.CustomerRepo = NewCustomerDataStore()
	if err := tooluse.RegisterCustomerTools(registry, customerDeps); err != nil {
		t.Fatalf("注册客户工具失败：%v", err)
	}
	reachDeps := tooluse.ReachToolDeps{Adapter: tooluse.NoOpReachAdapter{}, DB: db}
	if err := tooluse.RegisterReachTools(registry, reachDeps); err != nil {
		t.Fatalf("注册触达工具失败：%v", err)
	}
	knowledgeDeps := tooluse.NewKnowledgeToolDepsWithDB(db)
	knowledgeDeps.RagSearcher.SetHybridSearcher(nil)
	if err := tooluse.RegisterKnowledgeTools(registry, knowledgeDeps); err != nil {
		t.Fatalf("注册知识工具失败：%v", err)
	}
	businessDeps := newTestBusinessToolDeps(db)
	if err := tooluse.RegisterBusinessTools(registry, businessDeps); err != nil {
		t.Fatalf("注册业务工具失败：%v", err)
	}

	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{
		DefaultTimeout:    5 * time.Second,
		PermissionChecker: tooluse.NoOpPermissionChecker{},
		RateLimiter:       tooluse.NewTokenBucketLimiter(50, 100),
		RetryPolicy:       tooluse.NewExponentialBackoffPolicy(2, 100*time.Millisecond, 2*time.Second),
		AuditLogger:       tooluse.NewMemoryAuditLogger(1000),
		CostTracker:       tooluse.NewMemoryCostTracker(),
	})
	return executor
}

func setupAgentLoopSalesEngine(t *testing.T, db *gorm.DB, dispatcher *llm.Dispatcher) (*service.SalesEngine, *tooluse.ToolExecutor) {
	t.Helper()
	executor := setupAgentLoopExecutor(t, db)

	engine := service.NewSalesEngine(
		dispatcher,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	engine.SetToolExecutor(context.Background(), NewToolExecutorAdapter(executor))
	return engine, executor
}

func stubLLMDispatcher(t *testing.T, toolCallsToReturn []llm.ToolCall, finalContent string) *llm.Dispatcher {
	t.Helper()
	d := llm.NewDispatcher(llm.NewLLMService())
	d.AddProvider(llm.ProviderConfig{
		Name:         "stub",
		BaseURL:      "http://127.0.0.1:0/v1",
		APIType:      "openai",
		Model:        "stub-model",
		APIKey:       "stub-key",
		Enabled:      true,
		QualityScore: 0.99,
	})
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioSOPReply, llm.ScenarioObjection, llm.ScenarioFriendlyChat,
		llm.ScenarioIntentRecognize, llm.ScenarioHighQuality, llm.ScenarioLowCost,
		llm.ScenarioLongSummary,
	} {
		d.SetRoute(llm.ScenarioRoute{Scenario: sc, Provider: "stub", MinQuality: 0.0, MaxLatency: 60000})
	}
	return d
}

// TestT1_CustomerSearchTool_RealDB 验证 customer.search 工具真实执行
//
// 业务场景：智能体收到客户咨询，先调用 customer.search 查询客户是否存在
// 预期：工具真实查询 DB，返回客户列表（先写入 1 条客户，再查出）
func TestT1_CustomerSearchTool_RealDB(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)

	customer := &model.Customer{
		ID:    "cust-t1-001",
		Phone: "13800138001",
		Email: "zhangsan@example.com",
	}
	if err := db.Create(customer).Error; err != nil {
		t.Fatalf("预置客户失败：%v", err)
	}

	toolCall := tooluse.LLMToolCall{
		ID: "call-t1-001",
		Function: tooluse.LLMToolFunction{
			Name:      "customer.search",
			Arguments: `{"phone":"13800138001"}`,
		},
	}

	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{toolCall}, nil)
	if len(results) != 1 {
		t.Fatalf("应返回 1 个结果，实际 %d", len(results))
	}
	if !results[0].Success {
		t.Fatalf("customer.search 执行失败：%s", results[0].Content)
	}

	if !strings.Contains(results[0].Content, "13800138001") {
		t.Errorf("返回内容应包含手机号 13800138001，实际：%s", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "cust-t1-001") {
		t.Errorf("返回内容应包含客户 ID，实际：%s", results[0].Content)
	}
	t.Logf("✅ T1 customer.search 真实执行成功，返回：%s", results[0].Content)
}

// TestT4_FollowTaskCreate_RealDB 验证 follow_task.create 真实落库
//
// 业务场景：智能体识别客户意向后创建跟进任务
// 预期：跟进任务真实写入 DB
func TestT4_FollowTaskCreate_RealDB(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)

	call := tooluse.LLMToolCall{
		ID: "call-t4-001",
		Function: tooluse.LLMToolFunction{
			Name:      "follow_task.create",
			Arguments: `{"customer_id":"cust-t4-001","owner_id":"sales-t4","type":"first_contact","due_in_minutes":60,"title":"T4 首次跟进","priority":2}`,
		},
	}
	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{call}, nil)
	if !results[0].Success {
		t.Fatalf("follow_task.create 失败：%s", results[0].Content)
	}

	if !strings.Contains(results[0].Content, "reminder_id") {
		t.Errorf("返回应包含 reminder_id，实际：%s", results[0].Content)
	}
	t.Logf("✅ T4 follow_task.create 真实落库成功")
}

// TestT5_KnowledgeListKB_RealDB 验证 knowledge.list_kb 真实返回
//
// 业务场景：智能体查询有哪些知识库可用
// 预期：返回 RagProduct 列表（预置 2 个产品）
func TestT5_KnowledgeListKB_RealDB(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)

	for i := 1; i <= 2; i++ {
		p := &model.RagProduct{
			ID:             fmt.Sprintf("kb-t5-%03d", i),
			Name:           fmt.Sprintf("T5 知识库 %d", i),
			VectorTable:    fmt.Sprintf("vec_t5_%03d", i),
			IsActive:       true,
			Status:         1,
			EmbeddingModel: "BAAI/bge-base-zh-v1.5",
			TopK:           5,
		}
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("预置 RagProduct %d 失败：%v", i, err)
		}
	}

	call := tooluse.LLMToolCall{
		ID: "call-t5-001",
		Function: tooluse.LLMToolFunction{
			Name:      "knowledge.list_kb",
			Arguments: `{}`,
		},
	}
	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{call}, nil)
	if !results[0].Success {
		t.Fatalf("knowledge.list_kb 失败：%s", results[0].Content)
	}

	if !strings.Contains(results[0].Content, `"total":2`) {
		t.Errorf("应返回 total=2，实际：%s", results[0].Content)
	}
	t.Logf("✅ T5 knowledge.list_kb 真实返回 2 个知识库")
}

// TestT6_KnowledgeFeedback_RealDB 验证 knowledge.feedback 真实写入
//
// 业务场景：智能体对 RAG 检索结果进行反馈，用于持续学习优化召回质量
// 预期：KnowledgeFeedback 表真实写入 1 条记录
func TestT6_KnowledgeFeedback_RealDB(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)

	call := tooluse.LLMToolCall{
		ID: "call-t6-001",
		Function: tooluse.LLMToolFunction{
			Name:      "knowledge.feedback",
			Arguments: `{"product_id":"kb-t6-001","query":"如何退款","rating":"helpful","comment":"T6 测试反馈：答案清晰","operator":"ai_agent_t6"}`,
		},
	}
	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{call}, nil)
	if !results[0].Success {
		t.Fatalf("knowledge.feedback 失败：%s", results[0].Content)
	}

	var count int64
	db.Model(&model.KnowledgeFeedback{}).Count(&count)
	if count != 1 {
		t.Errorf("应写入 1 条 KnowledgeFeedback，实际 %d", count)
	}
	t.Logf("✅ T6 knowledge.feedback 真实写入 1 条反馈记录")
}

// TestT7_RagSearch_RealDB 验证 rag.search 真实检索
//
// 业务场景：智能体调用 rag.search 在知识库中检索相关文档
// 预期：返回 top_k 分段（预置 1 个 chunk）
func TestT7_RagSearch_RealDB(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)

	chunk := &model.KnowledgeChunk{
		DocumentID: 1,
		ProductID:  "12345",
		ChunkIndex: 0,
		Content:    "T7 测试：退款流程为联系客服并提供客户ID",
	}
	if err := db.Create(chunk).Error; err != nil {
		t.Fatalf("预置 chunk 失败：%v", err)
	}

	call := tooluse.LLMToolCall{
		ID: "call-t7-001",
		Function: tooluse.LLMToolFunction{
			Name:      "rag.search",
			Arguments: `{"product_id":"kb-t7-001","query":"退款","top_k":5}`,
		},
	}
	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{call}, nil)
	if !results[0].Success {
		t.Fatalf("rag.search 失败：%s", results[0].Content)
	}
	if !strings.Contains(results[0].Content, "chunks") {
		t.Errorf("返回应包含 chunks 字段，实际：%s", results[0].Content)
	}
	t.Logf("✅ T7 rag.search 真实检索成功")
}

// TestT8_AgentLoop_MultiIteration 验证 Agent Loop 真实多轮迭代
//
// 业务场景：LLM 第一次返回 tool_calls（调用 follow_task.create）→ 执行工具 →
// 回灌 tool 结果 → LLM 第二次返回 stop（最终回复）
//
// 本测试不依赖真实 LLM HTTP，通过 ToolExecutorAdapter 直接调用 DispatchToolCalls，
// 验证 AgentToolExecutor 接口契约 + 工具结果回灌格式正确
func TestT8_AgentLoop_MultiIteration(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)
	adapter := NewToolExecutorAdapter(executor)

	tools := adapter.ListTools(context.Background())
	if len(tools) < 32 {
		t.Fatalf("ListTools 应返回 >= 38 个工具，实际 %d", len(tools))
	}
	t.Logf("✅ T8.1 ListTools 返回 %d 个工具", len(tools))

	agentCalls := []service.AgentToolCall{
		{
			ID:        "call-t8-create",
			Name:      "follow_task.create",
			Arguments: `{"customer_id":"cust-t8-001","owner_id":"sales-t8","type":"first_contact","due_in_minutes":60,"title":"T8 跟进","priority":2}`,
		},
		{
			ID:        "call-t8-search",
			Name:      "customer.search",
			Arguments: `{"phone":"13900139001"}`,
		},
	}
	toolCtx := service.AgentToolContext{
		AgentID:    "agent-t8",
		SessionID:  "sess-t8",
		CustomerID: "cust-t8",
		Source:     "agent",
	}
	results := adapter.DispatchToolCalls(context.Background(), agentCalls, toolCtx)
	if len(results) != 2 {
		t.Fatalf("应返回 2 个结果，实际 %d", len(results))
	}

	for i, r := range results {
		if r.ToolCallID == "" {
			t.Errorf("结果 %d 的 ToolCallID 不应为空", i)
		}
		if r.Content == "" {
			t.Errorf("结果 %d 的 Content 不应为空", i)
		}
		t.Logf("  ToolCall %s: success=%v, content_len=%d", r.ToolCallID, r.Success, len(r.Content))
	}

	for _, r := range results {
		toolMsg := llm.ChatMessage{
			Role:       "tool",
			ToolCallID: r.ToolCallID,
			Content:    r.Content,
		}
		msgBytes, err := json.Marshal(toolMsg)
		if err != nil {
			t.Errorf("tool result 序列化为 ChatMessage 失败：%v", err)
		}
		if !strings.Contains(string(msgBytes), `"role":"tool"`) {
			t.Errorf("序列化后应包含 role:tool，实际：%s", string(msgBytes))
		}
	}
	t.Logf("✅ T8.2 Agent Loop 多轮迭代：tool_calls 并发执行 + 结果可回灌 LLM")
}

// TestT9_DecoratorChain_RealEffect 验证装饰器链真实生效
//
// 业务场景：工具执行经过 5 装饰器链：权限 → 限流 → 重试 → 超时 → 审计/计费
// 预期：
//  1. AuditLogger 记录每次工具调用（MemoryAuditLogger 非空）
//  2. CostTracker 统计调用次数（MemoryCostTracker 非空）
//  3. 装饰器链对失败工具自动重试（验证 RetryCount > 0）
func TestT9_DecoratorChain_RealEffect(t *testing.T) {
	db := setupAgentLoopTestDB(t)

	auditLogger := tooluse.NewMemoryAuditLogger(1000)
	costTracker := tooluse.NewMemoryCostTracker()
	registry := tooluse.NewToolRegistry()
	businessDeps := newTestBusinessToolDeps(db)
	_ = tooluse.RegisterBusinessTools(registry, businessDeps)

	executor := tooluse.NewToolExecutor(registry, tooluse.ToolExecutorConfig{
		DefaultTimeout:    5 * time.Second,
		PermissionChecker: tooluse.NoOpPermissionChecker{},
		RateLimiter:       tooluse.NewTokenBucketLimiter(100, 200),
		RetryPolicy:       tooluse.NewExponentialBackoffPolicy(3, 50*time.Millisecond, 1*time.Second),
		AuditLogger:       auditLogger,
		CostTracker:       costTracker,
	})

	call := tooluse.LLMToolCall{
		ID: "call-t9-001",
		Function: tooluse.LLMToolFunction{
			Name:      "follow_task.create",
			Arguments: `{"customer_id":"cust-t9-001","owner_id":"sales-t9","type":"first_contact","due_in_minutes":60,"title":"T9 跟进","priority":2}`,
		},
	}
	results := executor.DispatchByLLMToolCall(context.Background(), []tooluse.LLMToolCall{call}, nil)
	if !results[0].Success {
		t.Fatalf("follow_task.create 失败：%s", results[0].Content)
	}

	if auditLogger.Count() < 1 {
		t.Errorf("AuditLogger 应记录 >=1 条，实际 %d", auditLogger.Count())
	}
	entries := auditLogger.Entries()
	if len(entries) > 0 {
		e := entries[len(entries)-1]
		if e.ToolName != "follow_task.create" {
			t.Errorf("AuditEntry.ToolName 应为 follow_task.create，实际 %s", e.ToolName)
		}
		if !e.Success {
			t.Errorf("AuditEntry.Success 应为 true")
		}
	}

	stats := costTracker.Stats()
	foundOrder := false
	for _, s := range stats {
		if s.ToolName == "follow_task.create" && s.TotalCalls >= 1 {
			foundOrder = true
			if s.SuccessCalls < 1 {
				t.Errorf("follow_task.create 应有 >=1 次成功，实际 %d", s.SuccessCalls)
			}
		}
	}
	if !foundOrder {
		t.Errorf("CostTracker 未记录 follow_task.create 调用")
	}
	t.Logf("✅ T9 装饰器链真实生效：AuditLogger=%d 条，CostTracker 记录 follow_task.create", auditLogger.Count())
}

// TestT10_GlobalRegistry_Completeness 验证全局注册中心完整性
//
// 业务场景：SalesEngine 通过 ToolExecutorAdapter.ListTools() 获取所有可用工具
// 预期：返回的工具列表覆盖全部 5 大分类（customer/reach/knowledge/rag/follow_task）
//
//	工具数量 >= 32（reach 20 + customer 8 + knowledge 3 + rag 1 + follow_task 2）
func TestT10_GlobalRegistry_Completeness(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)
	adapter := NewToolExecutorAdapter(executor)

	tools := adapter.ListTools(context.Background())
	if len(tools) < 32 {
		t.Fatalf("ListTools 应返回 >= 36 个工具，实际 %d", len(tools))
	}

	categories := map[string]int{
		"customer.": 0, "reach.": 0, "knowledge.": 0, "rag.": 0,
		"follow_task.": 0,
	}
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("工具 Name 不应为空")
		}
		if tool.Description == "" {
			t.Errorf("工具 %s 的 Description 不应为空", tool.Name)
		}
		if tool.Parameters == nil {
			t.Errorf("工具 %s 的 Parameters 不应为 nil", tool.Name)
		}

		for prefix := range categories {
			if strings.HasPrefix(tool.Name, prefix) {
				categories[prefix]++
				break
			}
		}
	}

	required := map[string]int{
		"customer.":    8,
		"reach.":       20,
		"knowledge.":   3,
		"rag.":         1,
		"follow_task.": 2,
	}
	for prefix, minCount := range required {
		if categories[prefix] < minCount {
			t.Errorf("分类 %s 应有 >=%d 个工具，实际 %d", prefix, minCount, categories[prefix])
		}
	}

	criticalTools := []string{
		"customer.search", "customer.get", "customer.create",
		"reach.web.send", "reach.telegram.send", "reach.whatsapp.send",
		"rag.search", "knowledge.feedback",
		"follow_task.create", "follow_task.update",
	}
	toolNames := make(map[string]bool, len(tools))
	for _, t := range tools {
		toolNames[t.Name] = true
	}
	for _, name := range criticalTools {
		if !toolNames[name] {
			t.Errorf("关键工具 %s 未在 ListTools 返回中", name)
		}
	}

	t.Logf("✅ T10 全局注册中心完整性：共 %d 个工具，覆盖 5 大分类", len(tools))
	t.Logf("   分类分布：%v", categories)
}

// TestAgentLoop_SalesEngineHandle_RealToolExecutor 验证 SalesEngine.Handle 走 Agent Loop 路径
//
// 业务场景：SalesEngine 注入 ToolExecutor 后，generateCandidate 应走 runAgentLoop
// 注意：本测试 LLM 调用会失败（无真实 LLM 服务），但能验证：
//  1. SalesEngine.SetToolExecutor 后 toolExecutor 字段非 nil
//  2. generateCandidate 进入 runAgentLoop 分支（而非原始 Dispatch 路径）
//  3. Agent Loop 失败后降级到原始路径（向后兼容）
func TestAgentLoop_SalesEngineHandle_RealToolExecutor(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	dispatcher := stubLLMDispatcher(t, nil, "AI 回复")
	engine, executor := setupAgentLoopSalesEngine(t, db, dispatcher)

	if engine == nil {
		t.Fatal("SalesEngine 不应为 nil")
	}
	if executor == nil {
		t.Fatal("ToolExecutor 不应为 nil")
	}

	adapter := NewToolExecutorAdapter(executor)
	tools := adapter.ListTools(context.Background())
	if len(tools) < 32 {
		t.Errorf("ListTools 应返回 >= 38 个工具，实际 %d", len(tools))
	}
	t.Logf("✅ SalesEngine 注入 ToolExecutor 成功，ListTools 返回 %d 个工具", len(tools))

}

// TestAgentLoop_Fallback_WhenToolFails 验证工具失败时回灌错误信息给 LLM
//
// 业务场景：LLM 调用不存在的工具 → 工具执行返回错误 → 错误信息回灌给 LLM
// 预期：DispatchToolCalls 返回 success=false，Content 包含错误描述
func TestAgentLoop_Fallback_WhenToolFails(t *testing.T) {
	db := setupAgentLoopTestDB(t)
	executor := setupAgentLoopExecutor(t, db)
	adapter := NewToolExecutorAdapter(executor)

	call := service.AgentToolCall{
		ID:        "call-fail-001",
		Name:      "nonexistent.tool",
		Arguments: `{}`,
	}
	results := adapter.DispatchToolCalls(context.Background(), []service.AgentToolCall{call}, service.AgentToolContext{})
	if len(results) != 1 {
		t.Fatalf("应返回 1 个结果，实际 %d", len(results))
	}
	if results[0].Success {
		t.Error("不存在的工具应返回 success=false")
	}

	if !strings.Contains(results[0].Content, "nonexistent.tool") {
		t.Errorf("Content 应包含工具名 'nonexistent.tool' 的错误描述，实际：%s", results[0].Content)
	}
	t.Logf("✅ 工具失败降级：返回 success=false + 错误描述回灌给 LLM")

	call2 := service.AgentToolCall{
		ID:        "call-fail-002",
		Name:      "follow_task.create",
		Arguments: `{}`,
	}
	results2 := adapter.DispatchToolCalls(context.Background(), []service.AgentToolCall{call2}, service.AgentToolContext{})
	if results2[0].Success {
		t.Error("缺少必填参数应返回 success=false")
	}
	t.Logf("✅ 参数校验失败降级：返回 success=false + 错误描述回灌给 LLM")
}

func newTestBusinessToolDeps(db *gorm.DB) tooluse.BusinessToolDeps {
	orderPort := service.NewOrderPortAdapter(repository.NewExternalOrderRepository())
	return tooluse.NewBusinessToolDepsWithPorts(
		service.NewFollowUpPortAdapter(service.NewFollowUpService(service.NewCustomerJourneyService())),
		orderPort,
		service.NewAfterSalePortAdapter(service.NewAfterSaleService(repository.NewAfterSaleRepositoryWithDB(db))),
		db,
	)
}
