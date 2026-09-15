// seed_llm_logs.go 模块 I：LLM 路由日志种子数据
//
// 覆盖表（DDL 由 migrations/llm_routing_logs_migration.go 创建，无 GORM 模型，使用 raw SQL）：
// llm_routing_logs (120) 每次 Dispatch 调用一条，覆盖本地/云端 × 7 场景 × 三档 token_source
// llm_routing_audit (15) 路由变更审计，覆盖 create/update/rollback/gray_switch 等 action
//
// 关键约束（来自 project_memory）：
// trace_id 全链路追踪；scenario 7 个业务场景
// model_type=local/cloud；vendor 由 base_url 推断
// token_source=actual/estimated/missing；失败调用 is_fallback=true
// 缓存命中 from_cache=true；每次 dispatch 必须落库
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"
)

type llmLogsSeeder struct{}

func (s *llmLogsSeeder) Name() string        { return "llm_logs" }
func (s *llmLogsSeeder) Description() string { return "路由日志(120)+路由审计(15)" }

func (s *llmLogsSeeder) Clean(database *gorm.DB) error {
	if err := database.Exec("DELETE FROM llm_routing_logs WHERE trace_id LIKE 'seed-llm-%'").Error; err != nil {
		if !isTableMissingErr(err) {
			return fmt.Errorf("清空 llm_routing_logs 失败: %w", err)
		}
		log.Printf("  ! llm_routing_logs 表不存在，跳过清理（请先执行迁移）")
	}
	if err := database.Exec("DELETE FROM llm_routing_audit WHERE trace_id LIKE 'seed-llm-%'").Error; err != nil {
		if !isTableMissingErr(err) {
			return fmt.Errorf("清空 llm_routing_audit 失败: %w", err)
		}
	}
	return nil
}

func (s *llmLogsSeeder) Seed(database *gorm.DB, ctx *SeedContext) error {
	// 检查基础表是否已建立
	var tableExists int64
	if err := database.Raw("SELECT COUNT(1) FROM information_schema.tables WHERE table_name = 'llm_routing_logs'").Scan(&tableExists).Error; err != nil || tableExists == 0 {
		return fmt.Errorf("llm_routing_logs 表不存在，请先执行迁移 migrations/llm_routing_logs_migration.go")
	}
	// 检查扩展字段是否存在（v3.7.0 扩展迁移）
	var extColExists int64
	if err := database.Raw("SELECT COUNT(1) FROM information_schema.columns WHERE table_name='llm_routing_logs' AND column_name='model_type'").Scan(&extColExists).Error; err != nil || extColExists == 0 {
		return fmt.Errorf("llm_routing_logs 缺少扩展字段（model_type 等），请先执行迁移 migrations/llm_routing_logs_extend_migration.go")
	}

	logs := s.buildRoutingLogs()
	if err := s.insertRoutingLogs(database, logs); err != nil {
		return fmt.Errorf("写入 llm_routing_logs 失败: %w", err)
	}

	audits := s.buildRoutingAudits()
	if err := s.insertRoutingAudits(database, audits); err != nil {
		return fmt.Errorf("写入 llm_routing_audit 失败: %w", err)
	}

	// 收集日志 ID（最新插入的）供后续统计模块使用
	var lastLogID int64
	_ = database.Raw("SELECT MAX(id) FROM llm_routing_logs WHERE trace_id LIKE 'seed-llm-%'").Scan(&lastLogID).Error
	if lastLogID > 0 {
		ctx.LLMRoutingLogIDs = append(ctx.LLMRoutingLogIDs, lastLogID)
	}

	log.Printf("  ✓ 已写入 路由日志%d条+路由审计%d条", len(logs), len(audits))
	return nil
}

// routingLogSpec 单条 dispatch 日志规格
type routingLogSpec struct {
	Scenario         string
	Provider         string
	Model            string
	ModelType        string
	Vendor           string
	BaseURL          string
	IsFallback       bool
	FromCache        bool
	Success          bool
	TokenSource      string
	Estimator        string
	Source           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	PromptCost       float64
	CompletionCost   float64
	TotalCost        float64
	LatencyMs        int
	ErrorMsg         string
	HoursAgo         int
}

// 7 个业务场景（来自 project_memory：intent/sop/objection/…）
var llmScenarios = []string{
	"intent",
	"sop",
	"objection",
	"greeting",
	"closing",
	"faq",
	"follow_up",
}

func (s *llmLogsSeeder) buildRoutingLogs() []routingLogSpec {
	localProviders := []struct {
		Provider, Model, Vendor, BaseURL string
	}{
		{"ollama", "llama3:8b", "ollama", "http://localhost:11434"},
		{"ollama", "qwen2.5:7b", "ollama", "http://localhost:11434"},
		{"vllm", "qwen2.5-14b-instruct", "vllm", "http://localhost:8000"},
		{"lmstudio", "deepseek-r1:7b", "lmstudio", "http://localhost:1234"},
		{"local", "internal-chatglm3", "self-hosted", "http://gpu-internal:8080"},
	}
	cloudProviders := []struct {
		Provider, Model, Vendor, BaseURL string
	}{
		{"openai", "gpt-4o-mini", "openai", "https://api.openai.com/v1"},
		{"openai", "gpt-4o", "openai", "https://api.openai.com/v1"},
		{"deepseek", "deepseek-chat", "deepseek", "https://api.deepseek.com/v1"},
		{"anthropic", "claude-3-5-sonnet", "anthropic", "https://api.anthropic.com/v1"},
		{"zhipu", "glm-4-plus", "zhipu", "https://open.bigmodel.cn/api/paas/v4"},
		{"moonshot", "moonshot-v1-8k", "moonshot", "https://api.moonshot.cn/v1"},
		{"dashscope", "qwen-max", "alibaba", "https://dashscope.aliyuncs.com/api/v1"},
	}

	specs := make([]routingLogSpec, 0, 120)
	idx := 0

	for _, scenario := range llmScenarios {
		for j, p := range localProviders {
			idx++
			spec := routingLogSpec{
				Scenario:  scenario,
				Provider:  p.Provider,
				Model:     p.Model,
				ModelType: "local",
				Vendor:    p.Vendor,
				BaseURL:   p.BaseURL,
				Source:    "dispatch",
				Success:   true,
				LatencyMs: randInt(80, 800),
				HoursAgo:  randInt(1, 168),
			}
			switch j {
			case 0:
				spec.TokenSource = "actual"
				spec.Estimator = "api_usage"
				spec.PromptTokens = randInt(200, 1200)
				spec.CompletionTokens = randInt(50, 500)
			case 1, 2:
				spec.TokenSource = "estimated"
				spec.Estimator = "char_weight"
				spec.PromptTokens = randInt(180, 1000)
				spec.CompletionTokens = randInt(40, 400)
			case 3:
				spec.TokenSource = "missing"
				spec.Estimator = "empty_fallback"
				spec.PromptTokens = 0
				spec.CompletionTokens = 0
			case 4:
				spec.TokenSource = "estimated"
				spec.Estimator = "char_weight"
				spec.PromptTokens = randInt(250, 1500)
				spec.CompletionTokens = randInt(60, 600)
			}
			spec.TotalTokens = spec.PromptTokens + spec.CompletionTokens
			spec.PromptCost = float64(spec.PromptTokens) * 0.000001
			spec.CompletionCost = float64(spec.CompletionTokens) * 0.000002
			spec.TotalCost = spec.PromptCost + spec.CompletionCost
			specs = append(specs, spec)
		}
	}

	for _, scenario := range llmScenarios {
		for j, p := range cloudProviders {
			if j >= 5 {
				break
			}
			idx++
			spec := routingLogSpec{
				Scenario:         scenario,
				Provider:         p.Provider,
				Model:            p.Model,
				ModelType:        "cloud",
				Vendor:           p.Vendor,
				BaseURL:          p.BaseURL,
				Source:           "dispatch",
				Success:          true,
				LatencyMs:        randInt(500, 3000),
				HoursAgo:         randInt(1, 168),
				TokenSource:      "actual",
				Estimator:        "api_usage",
				PromptTokens:     randInt(150, 800),
				CompletionTokens: randInt(40, 350),
			}
			if j == 2 {
				spec.TokenSource = "estimated"
				spec.Estimator = "char_weight"
			}
			spec.TotalTokens = spec.PromptTokens + spec.CompletionTokens
			switch p.Vendor {
			case "openai":
				if strings.Contains(p.Model, "gpt-4o-mini") {
					spec.PromptCost = float64(spec.PromptTokens) * 0.00000015
					spec.CompletionCost = float64(spec.CompletionTokens) * 0.0000006
				} else {
					spec.PromptCost = float64(spec.PromptTokens) * 0.000005
					spec.CompletionCost = float64(spec.CompletionTokens) * 0.000015
				}
			case "deepseek":
				spec.PromptCost = float64(spec.PromptTokens) * 0.00000014
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.00000028
			case "anthropic":
				spec.PromptCost = float64(spec.PromptTokens) * 0.000003
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.000015
			case "zhipu":
				spec.PromptCost = float64(spec.PromptTokens) * 0.0000005
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.0000005
			case "moonshot":
				spec.PromptCost = float64(spec.PromptTokens) * 0.00000017
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.00000024
			case "alibaba":
				spec.PromptCost = float64(spec.PromptTokens) * 0.0000025
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.00001
			}
			spec.TotalCost = spec.PromptCost + spec.CompletionCost
			specs = append(specs, spec)
		}
	}

	for i := 0; i < 50; i++ {
		idx++
		scenario := llmScenarios[i%len(llmScenarios)]
		isLocal := i%2 == 0
		var p struct {
			Provider, Model, Vendor, BaseURL string
		}
		if isLocal {
			lp := localProviders[i%len(localProviders)]
			p.Provider, p.Model, p.Vendor, p.BaseURL = lp.Provider, lp.Model, lp.Vendor, lp.BaseURL
		} else {
			cp := cloudProviders[i%len(cloudProviders)]
			p.Provider, p.Model, p.Vendor, p.BaseURL = cp.Provider, cp.Model, cp.Vendor, cp.BaseURL
		}
		modelType := "local"
		if !isLocal {
			modelType = "cloud"
		}
		spec := routingLogSpec{
			Scenario:  scenario,
			Provider:  p.Provider,
			Model:     p.Model,
			ModelType: modelType,
			Vendor:    p.Vendor,
			BaseURL:   p.BaseURL,
			LatencyMs: randInt(100, 2500),
			HoursAgo:  randInt(1, 168),
		}
		switch i % 5 {
		case 0:
			spec.FromCache = true
			spec.Source = "cache"
			spec.Success = true
			spec.TokenSource = "estimated"
			spec.Estimator = "char_weight"
			spec.PromptTokens = randInt(100, 600)
			spec.CompletionTokens = randInt(30, 200)
		case 1:
			spec.IsFallback = true
			spec.Source = "fallback"
			spec.Success = true
			spec.TokenSource = "estimated"
			spec.Estimator = "char_weight"
			spec.PromptTokens = randInt(200, 800)
			spec.CompletionTokens = randInt(50, 300)
		case 2:
			spec.IsFallback = true
			spec.Source = "fallback"
			spec.Success = false
			spec.ErrorMsg = "演示：上游 LLM 网关超时"
			spec.TokenSource = "missing"
			spec.Estimator = "empty_fallback"
		case 3:
			spec.IsFallback = false
			spec.Source = "dispatch"
			spec.Success = true
			spec.TokenSource = "actual"
			spec.Estimator = "api_usage"
			spec.PromptTokens = randInt(200, 1000)
			spec.CompletionTokens = randInt(50, 400)
		case 4:
			spec.Success = true
			spec.Source = "dispatch"
			spec.TokenSource = "missing"
			spec.Estimator = "empty_fallback"
		}
		spec.TotalTokens = spec.PromptTokens + spec.CompletionTokens
		if spec.Success {
			if isLocal {
				spec.PromptCost = float64(spec.PromptTokens) * 0.000001
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.000002
			} else {
				spec.PromptCost = float64(spec.PromptTokens) * 0.000001
				spec.CompletionCost = float64(spec.CompletionTokens) * 0.000002
			}
		}
		spec.TotalCost = spec.PromptCost + spec.CompletionCost
		specs = append(specs, spec)
	}

	return specs
}

// routingAuditSpec 单条路由变更审计规格
type routingAuditSpec struct {
	Scenario      string
	Version       int
	PrevProvider  string
	NewProvider   string
	PrevFallbacks string
	NewFallbacks  string
	Action        string
	Operator      string
	HoursAgo      int
}

func (s *llmLogsSeeder) buildRoutingAudits() []routingAuditSpec {
	actions := []string{"create", "update", "rollback", "gray_switch", "update"}
	operators := []string{"admin", "ops-manager", "sre-oncall", "product-manager"}
	providers := []string{"ollama", "openai", "deepseek", "vllm", "zhipu", "anthropic"}

	audits := make([]routingAuditSpec, 0, 15)
	for i := 0; i < 15; i++ {
		scenario := llmScenarios[i%len(llmScenarios)]
		prev := providers[i%len(providers)]
		next := providers[(i+3)%len(providers)]
		if i%3 == 0 {
			prev = ""
		}
		a := routingAuditSpec{
			Scenario:      scenario,
			Version:       (i / 3) + 1,
			PrevProvider:  prev,
			NewProvider:   next,
			PrevFallbacks: fmt.Sprintf(`["%s","%s"]`, providers[(i+1)%len(providers)], providers[(i+2)%len(providers)]),
			NewFallbacks:  fmt.Sprintf(`["%s","%s","%s"]`, providers[(i+4)%len(providers)], providers[(i+5)%len(providers)], providers[(i+6)%len(providers)]),
			Action:        actions[i%len(actions)],
			Operator:      operators[i%len(operators)],
			HoursAgo:      randInt(1, 336),
		}
		audits = append(audits, a)
	}
	return audits
}

func (s *llmLogsSeeder) insertRoutingLogs(database *gorm.DB, specs []routingLogSpec) error {
	if len(specs) == 0 {
		return nil
	}
	const insertSQL = `INSERT INTO llm_routing_logs (
		trace_id, scenario, provider, model,
		prompt_tokens, completion_tokens, total_tokens,
		cost, latency_ms, success, error_msg, from_cache,
		model_type, vendor, base_url, is_fallback,
		prompt_cost, completion_cost, token_source, estimator, source, scenario_provider,
		created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`

	for i, spec := range specs {
		traceID := fmt.Sprintf("seed-llm-trace-%d-%s-%d", i+1, spec.Scenario, spec.HoursAgo)
		createdAt := hoursAgo(spec.HoursAgo)
		scenarioProvider := spec.Scenario + ":" + spec.Provider
		if err := database.Exec(insertSQL,
			traceID, spec.Scenario, spec.Provider, spec.Model,
			spec.PromptTokens, spec.CompletionTokens, spec.TotalTokens,
			spec.TotalCost, spec.LatencyMs, spec.Success, spec.ErrorMsg, spec.FromCache,
			spec.ModelType, spec.Vendor, spec.BaseURL, spec.IsFallback,
			spec.PromptCost, spec.CompletionCost, spec.TokenSource, spec.Estimator, spec.Source, scenarioProvider,
			createdAt,
		).Error; err != nil {
			return fmt.Errorf("insert llm_routing_logs[%d]: %w", i+1, err)
		}
	}
	return nil
}

func (s *llmLogsSeeder) insertRoutingAudits(database *gorm.DB, specs []routingAuditSpec) error {
	if len(specs) == 0 {
		return nil
	}
	const insertSQL = `INSERT INTO llm_routing_audit (
		scenario, version, prev_provider, new_provider,
		prev_fallbacks, new_fallbacks, action, operator, trace_id, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	for i, spec := range specs {
		traceID := fmt.Sprintf("seed-llm-audit-%d-%s", i+1, spec.Scenario)
		createdAt := hoursAgo(spec.HoursAgo)
		if err := database.Exec(insertSQL,
			spec.Scenario, spec.Version, spec.PrevProvider, spec.NewProvider,
			spec.PrevFallbacks, spec.NewFallbacks, spec.Action, spec.Operator, traceID, createdAt,
		).Error; err != nil {
			return fmt.Errorf("insert llm_routing_audit[%d]: %w", i+1, err)
		}
	}
	return nil
}

// isTableMissingErr 判断是否为表不存在的错误（容错）
func isTableMissingErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "does not exist") || strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "relation") && strings.Contains(msg, "does not exist")
}

// 防止 time 未使用警告（time 在 future 扩展中使用）
var _ = time.Now
