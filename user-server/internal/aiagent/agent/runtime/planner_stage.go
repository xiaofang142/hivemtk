package agent_runtime

import (
	"context"
	"time"
)

// TB-01 数据驱动意图→工具链：类型/白名单/渲染/构造在 planner_chain.go；
// 本文件只保留规划器对绑定层的集成钩子（ChainLookup 注入 + Plan 查询点）。

// DefaultTaskPlanner 默认任务规划器
type DefaultTaskPlanner struct {
	FAQLibrary func(ctx context.Context, content string) (bool, string)

	// ChainLookup 意图→工具链绑定数据源（TB-01）；nil = 未装配，行为与硬编码规划完全一致。
	ChainLookup IntentChainLookup
}

// SetChainLookup 注入绑定数据源（装配层调用；运行期只读）。
func (p *DefaultTaskPlanner) SetChainLookup(l IntentChainLookup) {
	p.ChainLookup = l
}

// NewDefaultTaskPlanner 构造默认任务规划器
func NewDefaultTaskPlanner() *DefaultTaskPlanner {
	return &DefaultTaskPlanner{}
}

// NewDefaultTaskPlannerWithFAQ 自定义 FAQ
func NewDefaultTaskPlannerWithFAQ(faq func(ctx context.Context, content string) (bool, string)) *DefaultTaskPlanner {
	return &DefaultTaskPlanner{FAQLibrary: faq}
}

// Name 阶段名
func (p *DefaultTaskPlanner) Name() string {
	return "planner"
}

// Execute 执行规划
func (p *DefaultTaskPlanner) Execute(ctx context.Context, ic *InferenceContext) StageResult {
	start := time.Now()
	plan, err := p.Plan(ctx, ic)
	if err != nil {
		ic.Stages = append(ic.Stages, StageDecision{
			Stage:    p.Name(),
			Action:   "plan_failed",
			Reason:   err.Error(),
			Duration: time.Since(start),
			Success:  false,
			Error:    err.Error(),
		})
		return FailResult(err)
	}
	ic.Plan = plan

	ic.Decision.Plan = plan
	ic.Decision.Confidence = plan.Confidence
	ic.Decision.StopReason = "plan_ready"

	ic.Stages = append(ic.Stages, StageDecision{
		Stage:    p.Name(),
		Action:   "plan",
		Reason:   "type=" + plan.PlanType + " tools=" + itoa(len(plan.ToolCalls)) + " skip_llm=" + boolStr(plan.SkipLLM) + chainSourceSuffix(plan.PlanSource),
		Duration: time.Since(start),
		Success:  true,
	})
	return ContinueResult()
}

// Plan 生成行动方案
func (p *DefaultTaskPlanner) Plan(ctx context.Context, ic *InferenceContext) (*ActionPlan, error) {
	plan := &ActionPlan{
		PlanType:   "customer_service",
		Confidence: 0.5,
		ToolCalls:  []PlannedToolCall{},
	}

	if p.FAQLibrary != nil {
		if matched, reply := p.FAQLibrary(ctx, ic.Payload.Content); matched {
			plan.SkipLLM = true
			plan.SkipReason = "faq_matched"
			plan.ReplyHint = reply
			plan.Confidence = 0.95
			return plan, nil
		}
	}

	// TB-01 绑定层：先查数据驱动的意图→工具链；命中即按数据规划，
	// 未命中/禁用/渲染失败/查询错误一律回落下方既有硬编码 case（零行为变化）。
	if p.ChainLookup != nil {
		chainSrc := "miss"
		if spec, err := p.ChainLookup.LookupIntentChain(ctx, string(ic.Intent.Primary)); err != nil {
			chainSrc = "error"
		} else if spec != nil {
			if chainPlan, ok := buildChainPlan(spec, ic); ok {
				return chainPlan, nil
			}
			chainSrc = "rejected"
		}
		plan.PlanSource = "builtin:" + chainSrc
	}

	switch ic.Intent.Primary {
	case IntentGreeting, IntentFarewell, IntentChitchat:
		plan.PlanType = "customer_service"
		plan.ReplyHint = buildChitchatHint(ic)
		plan.Confidence = 0.85

	case IntentInquiry:
		plan.PlanType = "sales"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "knowledge.search", Args: map[string]any{"query": ic.Payload.Content, "top_k": 3}, Priority: 1},
			PlannedToolCall{ToolName: "product.price", Args: map[string]any{}, Priority: 2},
		)
		plan.ReplyHint = buildSalesHint(ic)
		plan.Confidence = 0.75

	case IntentOrderStatus:
		plan.PlanType = "customer_service"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "order.lookup", Args: map[string]any{"customer_id": ic.Payload.CustomerID}, Priority: 1},
		)
		plan.ReplyHint = buildOrderHint(ic)
		plan.Confidence = 0.80

	case IntentRefund, IntentComplaint:
		plan.PlanType = "customer_service"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "order.lookup", Args: map[string]any{"customer_id": ic.Payload.CustomerID}, Priority: 1},
		)
		plan.ReplyHint = buildApologizeHint(ic)
		plan.Confidence = 0.70

	case IntentAfterSales:
		plan.PlanType = "customer_service"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "aftersales.policy", Args: map[string]any{}, Priority: 1},
		)
		plan.ReplyHint = buildAfterSalesHint(ic)
		plan.Confidence = 0.70

	case IntentSalesLead:
		plan.PlanType = "sales"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "knowledge.search", Args: map[string]any{"query": ic.Payload.Content, "top_k": 5}, Priority: 1},
		)
		plan.ReplyHint = buildSalesHint(ic)
		plan.Confidence = 0.78

	default:
		plan.PlanType = "customer_service"
		plan.ToolCalls = append(plan.ToolCalls,
			PlannedToolCall{ToolName: "knowledge.search", Args: map[string]any{"query": ic.Payload.Content, "top_k": 3}, Priority: 1},
		)
		plan.ReplyHint = buildDefaultHint(ic)
		plan.Confidence = 0.55
	}

	if ic.AgentCtx != nil && !ic.AgentCtx.EnableRAG {
		filtered := []PlannedToolCall{}
		for _, tc := range plan.ToolCalls {
			if tc.ToolName != "knowledge.search" {
				filtered = append(filtered, tc)
			}
		}
		plan.ToolCalls = filtered
	}

	if ic.EpisodicMemory != "" {
		plan.ReplyHint = plan.ReplyHint + "\n\n【跨会话情境记忆】\n" + ic.EpisodicMemory
	}

	return plan, nil
}

func buildChitchatHint(ic *InferenceContext) string {
	persona := ""
	if ic.AgentCtx != nil {
		persona = ic.AgentCtx.Persona
	}
	if persona == "" {
		persona = "友善、专业、温暖"
	}
	return "用" + persona + "的语气回应客户的闲聊。简短、有温度、不啰嗦。"
}

func buildSalesHint(ic *InferenceContext) string {
	hint := "客户正在询价或表达购买意向。请：\n"
	hint += "1. 热情回应（6维-热情度优先）\n"
	hint += "2. 展示产品价值与差异化\n"
	hint += "3. 引导下一步（询单/试用/加购）\n"
	if ic.AgentCtx != nil && ic.AgentCtx.Persona != "" {
		hint += "4. 人设：" + ic.AgentCtx.Persona + "\n"
	}
	return hint
}

func buildOrderHint(ic *InferenceContext) string {
	hint := "客户在查订单。请：\n"
	hint += "1. 调 order.lookup 拿到订单详情\n"
	hint += "2. 用清晰、简洁的格式展示（订单号/商品/状态/预计到达）\n"
	hint += "3. 如有物流单号，附上查询链接\n"
	return hint
}

func buildApologizeHint(ic *InferenceContext) string {
	hint := "客户在投诉/退款。请：\n"
	hint += "1. 先真诚道歉（不要辩解）\n"
	hint += "2. 表达同理心（6维-同理心优先）\n"
	hint += "3. 调 order.lookup 查订单\n"
	hint += "4. 提供明确的下一步（如：1小时内退款/补发/转专员）\n"
	return hint
}

func buildAfterSalesHint(ic *InferenceContext) string {
	return "客户在咨询售后。请：\n1. 调 aftersales.policy 查询售后政策\n2. 给出明确步骤（如何申请/需要哪些凭证/处理时长）\n3. 表达同理心"
}

func buildDefaultHint(ic *InferenceContext) string {
	return "意图不明时：\n1. 礼貌确认客户问题\n2. 调知识库检索（若有）\n3. 如知识库无答案，引导客户换种方式描述"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// chainSourceSuffix 阶段观测用规划来源后缀（未装配绑定层时为空串，观测文本零变化）。
func chainSourceSuffix(src string) string {
	if src == "" {
		return ""
	}
	return " src=" + src
}
