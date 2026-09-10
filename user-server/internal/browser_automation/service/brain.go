package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"strings"
	"sync"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// BrainService Brain 层：观察快照 → LLM reflect+plan → 翻译为 steps → 闭环评估。
// 架构对标 browser-use 单 agent 大循环（AGENT_BASE_PROOF.md §1.3/§3）：
//
//	结构化 reflect 输出（thinking/evaluation/memory/next_goal）+ 循环检测 nudge + 独立 judge 验收。
type BrainService struct {
	planRepo repository.BrowserLLMPlanRepository

	mu             sync.Mutex
	lastPlanTokens int // P1-1 最近一次 plan token 消耗（计量信号；精确审计看 plans 表）
}

func NewBrainService(planRepo repository.BrowserLLMPlanRepository) *BrainService {
	return &BrainService{planRepo: planRepo}
}

// planSchema LLM 结构化 reflect 输出（对标 browser-use AgentOutput；旧字段兼容）
type planSchema struct {
	Thinking           string          `json:"thinking"`
	EvaluationPrevGoal string          `json:"evaluation_previous_goal"`
	Memory             string          `json:"memory"`
	NextGoal           string          `json:"next_goal"`
	Done               bool            `json:"done"`
	Reasoning          string          `json:"reasoning"` // 旧字段兼容（thinking 为空时回退展示）
	Steps              json.RawMessage `json:"steps"`
}

// reflectState 跨轮记忆状态（MessageManager 语义：每轮由 LLM 维护、执行器回喂）
type reflectState struct {
	PrevEvaluation string   // 上轮 evaluation_previous_goal（回喂给下轮）
	Memory         string   // 累积 memory（LLM 每轮重写，执行器透传）
	History        []string // 动作历史（含结果状态）
}

// loopFingerprint 近 3 轮动作序列指纹（循环检测用）
func loopFingerprint(history []string) (bool, string) {
	n := len(history)
	if n < 6 {
		return false, ""
	}
	last3 := strings.Join(history[n-3:], "|")
	prev3 := strings.Join(history[n-6:n-3], "|")
	if last3 == prev3 && last3 != "" {
		return true, last3
	}
	return false, ""
}

// GeneratePlan 生成执行计划并落库，返回解析后的步骤。
func (s *BrainService) GeneratePlan(ctx context.Context, taskID uint, goal, snapshot string) (stepsJSON []byte, done bool, err error) {
	return s.GeneratePlanWithHistory(ctx, taskID, goal, snapshot, nil)
}

// GeneratePlanWithHistory 兼容旧签名（无平台知识注入）
func (s *BrainService) GeneratePlanWithHistory(ctx context.Context, taskID uint, goal, snapshot string, history []string) (stepsJSON []byte, done bool, err error) {
	st := &reflectState{History: history}
	return s.plan(ctx, taskID, goal, "", snapshot, st, "")
}

// GeneratePlanWithPlatform 带平台知识注入 + reflect 状态的编排。
func (s *BrainService) GeneratePlanWithPlatform(ctx context.Context, taskID uint, goal, snapshot string, history []string, platformID string) (stepsJSON []byte, done bool, err error) {
	st := &reflectState{History: history}
	return s.plan(ctx, taskID, goal, platformID, snapshot, st, "")
}

// GeneratePlanReflect 完整 reflect 编排：执行器传入跨轮状态（PrevEvaluation/Memory/History），
// 返回计划的同时把本轮 evaluation/memory 写回 state（执行器下一轮透传）。
func (s *BrainService) GeneratePlanReflect(ctx context.Context, taskID uint, goal, platformID, snapshot string, st *reflectState) (stepsJSON []byte, done bool, err error) {
	return s.plan(ctx, taskID, goal, platformID, snapshot, st, "")
}

// JudgeDone done 的独立验收（对标 browser-use judge）：approve=false 则执行器继续循环。
func (s *BrainService) JudgeDone(ctx context.Context, goal, finalState string) (approve bool, reason string) {
	dispatcher := llm.GetGlobalDispatcher()
	var out struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		SystemPrompt: "你是严格的验收员，只输出 JSON。",
		Prompt:       BuildJudgePrompt(goal, finalState),
		JSONMode:     true,
		MaxTokens:    256,
	}
	if _, err := dispatcher.DispatchStructured(ctx, req, &out); err != nil {
		// judge 失败不阻断（fail-open）：验收增强挂了不能卡死主流程
		logger.Warnf("[BrowserBrain] judge 失败（fail-open 放行）: %v", err)
		return true, "judge_unavailable"
	}
	return out.Approve, out.Reason
}

// plan 核心：模板工厂拼 prompt → LLM → 落库。
// 重试分类（P0-3）：可重试错误（429/5xx/网络/超时）按退避重试；不可重试（401/403 鉴权类）立即快败。
func (s *BrainService) plan(ctx context.Context, taskID uint, goal, platformID, snapshot string, st *reflectState, extraSuffix string) (stepsJSON []byte, done bool, err error) {
	dispatcher := llm.GetGlobalDispatcher()
	sys := BuildPlanSystemPrompt(platformID)
	snap, stBudgeted := budgetInput(snapshot, st) // P0-1 输入预算
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		// 硬闸（铁律 4 全自动）：execCtx 超时后 LLM HTTP 调用可能不响应 cancel
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if attempt > 0 {
			if !isRetryableLLMError(lastErr) {
				return nil, false, lastErr // 不可重试错误快败，不烧次数
			}
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		sysAttempt := sys + extraSuffix
		if attempt == 2 {
			sysAttempt += BuildRecoveryPrompt()
		}
		stepsJSON, done, err = s.planOnce(ctx, dispatcher, taskID, goal, snap, sysAttempt, stBudgeted)
		if err == nil {
			return stepsJSON, done, nil
		}
		lastErr = err
		logger.Warnf("[BrowserBrain] plan 尝试 %d/3 失败 task=%d retryable=%v: %v", attempt+1, taskID, isRetryableLLMError(err), err)
	}
	return nil, false, lastErr
}

// planOnce 单次 LLM reflect+plan
func (s *BrainService) planOnce(ctx context.Context, dispatcher *llm.Dispatcher, taskID uint, goal, snapshot, systemPrompt string, st *reflectState) (stepsJSON []byte, done bool, err error) {
	var (
		planModel  string
		planTokIn  int
		planTokOut int
	)
	var b strings.Builder
	b.WriteString("目标：" + goal + "\n\n页面快照：\n" + snapshot)
	if st != nil {
		if st.PrevEvaluation != "" {
			b.WriteString("\n\n上一步评估：" + st.PrevEvaluation)
		}
		if st.Memory != "" {
			b.WriteString("\n\n累积记忆（跨轮关键事实）：" + st.Memory)
		}
		if len(st.History) > 0 {
			b.WriteString("\n\n动作历史（勿重复无效动作）：\n- " + strings.Join(st.History, "\n- "))
		}
	}
	req := llm.DispatchRequest{
		Scenario:     llm.ScenarioHighQuality,
		SystemPrompt: systemPrompt,
		Prompt:       b.String(),
		JSONMode:     true,
		MaxTokens:    4096,
	}
	var p planSchema
	// 调用级看门狗（R22 实测：execCtx 取消在部分调用栈不生效，session 卡 active）——
	// 真实时钟强返；被泄漏的内部 goroutine 由 HTTP client 180s 自然终结，可接受
	type planResult struct {
		steps  []byte
		done   bool
		model  string
		tokIn  int
		tokOut int
		err    error
	}
	ch := make(chan planResult, 1)
	go func() {
		result, err := dispatcher.DispatchStructured(ctx, req, &p)
		if err != nil {
			ch <- planResult{err: fmt.Errorf("LLM 编排失败: %w", err)}
			return
		}
		ch <- planResult{
			done:   p.Done,
			model:  result.Model,
			tokIn:  result.Usage.PromptTokens,
			tokOut: result.Usage.CompletionTokens,
		}
	}()
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, false, r.err
		}
		planModel, planTokIn, planTokOut = r.model, r.tokIn, r.tokOut
	}
	done = p.Done
	steps := p.Steps
	if len(steps) == 0 || string(steps) == "null" {
		steps = []byte("[]")
	}
	// reflect 状态写回（执行器下一轮透传）：evaluation/memory 以新字段为准，缺省回退旧 reasoning
	if st != nil {
		if ev := strings.TrimSpace(p.EvaluationPrevGoal); ev != "" {
			st.PrevEvaluation = ev
		} else if strings.TrimSpace(p.Reasoning) != "" {
			st.PrevEvaluation = p.Reasoning
		}
		if m := strings.TrimSpace(p.Memory); m != "" {
			st.Memory = m
		}
	}

	// 落库：reasoning 列存 reflect 全文（thinking+evaluation+next_goal），审计可回放
	reasoning := buildReasoningText(&p)
	if len(reasoning) > 4096 {
		reasoning = reasoning[:4096]
	}
	row := &model.BrowserLLMPlan{
		TaskID:    taskID,
		Goal:      goal,
		Snapshot:  truncate(snapshot, 64*1024),
		Steps:     datatypes.JSON(steps),
		Reasoning: reasoning,
		Model:     planModel,
		TokenIn:   planTokIn,
		TokenOut:  planTokOut,
	}
	if err := s.planRepo.Create(ctx, row); err != nil {
		// P2-3：plan 落库失败升级 error（审计断链必须可见），不阻断执行
		logger.Errorf("[BrowserBrain] plan 落库失败 task=%d: %v", taskID, err)
	}
	s.mu.Lock()
	s.lastPlanTokens = planTokIn + planTokOut
	s.mu.Unlock()
	return steps, p.Done, nil
}

func buildReasoningText(p *planSchema) string {
	parts := make([]string, 0, 4)
	if s := strings.TrimSpace(p.Thinking); s != "" {
		parts = append(parts, "thinking: "+s)
	}
	if s := strings.TrimSpace(p.EvaluationPrevGoal); s != "" {
		parts = append(parts, "eval: "+s)
	}
	if s := strings.TrimSpace(p.NextGoal); s != "" {
		parts = append(parts, "next: "+s)
	}
	if s := strings.TrimSpace(p.Reasoning); s != "" {
		parts = append(parts, "reasoning: "+s)
	}
	return strings.Join(parts, " | ")
}

// SummarizeSession 执行结束后 LLM 总结
func (s *BrainService) SummarizeSession(ctx context.Context, goal string, success, total int, extracts, consoleErrors string) string {
	if total <= 0 {
		return ""
	}
	dispatcher := llm.GetGlobalDispatcher()
	prompt := fmt.Sprintf(
		"以下是浏览器自动化任务的执行结果，请用不超过 120 字总结关键发现。\n目标：%s\n成功率：%d/%d\n提取数据：%s\n控制台错误：%s",
		goal, success, total, truncate(extracts, 8192), truncate(consoleErrors, 2048),
	)
	result, err := dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:  llm.ScenarioLongSummary,
		Prompt:    prompt,
		MaxTokens: 512,
	})
	if err != nil {
		logger.Warnf("[BrowserBrain] 总结失败: %v", err)
		return ""
	}
	return strings.TrimSpace(result.Content)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// maxBrainIterations Brain 模式防无限循环上限（judge 可能追加轮次，上限保持保守）
const maxBrainIterations = 40

// brainMaxPlanFailures Brain 编排连续失败容忍度
const brainMaxPlanFailures = 3

// LastPlanTokens 最近一次 planOnce 的 token 消耗（P1-1 session 级计量）。
// BrainService 无状态多 session 并发安全：值仅作为计量信号，非精确审计（精确值看 browser_llm_plans）。
func (s *BrainService) LastPlanTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPlanTokens
}
