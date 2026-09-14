package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	IntentMajorConsult      = "consult"
	IntentMajorPriceInquiry = "price_inquiry"
	IntentMajorObjection    = "objection"
	IntentMajorAfterSale    = "after_sale"
	IntentMajorComplaint    = "complaint"
	IntentMajorChurn        = "churn"
	IntentMajorIntentBuy    = "intent_buy"
	IntentMajorAskProduct   = "ask_product"
)

// consult 子类
const (
	IntentMinorConsultGeneral         = "general"
	IntentMinorConsultProductSpecific = "product_specific"
	IntentMinorConsultComparison      = "comparison"
)

// price_inquiry 子类
const (
	IntentMinorPriceBudgetCheck  = "budget_check"
	IntentMinorPriceDiscountReq  = "discount_request"
	IntentMinorPricePaymentTerms = "payment_terms"
)

// objection 子类
const (
	IntentMinorObjectionPriceHigh     = "price_too_high"
	IntentMinorObjectionTrustIssue    = "trust_issue"
	IntentMinorObjectionTimingBad     = "timing_bad"
	IntentMinorObjectionCompetitorCmp = "competitor_comparison"
)

// after_sale 子类
const (
	IntentMinorAfterSaleQuality  = "quality_issue"
	IntentMinorAfterSaleDelivery = "delivery_issue"
	IntentMinorAfterSaleRefund   = "refund_request"
	IntentMinorAfterSaleWarranty = "warranty"
)

// complaint 子类
const (
	IntentMinorComplaintService = "service_complaint"
	IntentMinorComplaintProduct = "product_complaint"
	IntentMinorComplaintBilling = "billing_complaint"
)

// churn 子类
const (
	IntentMinorChurnCancelSub  = "cancel_subscription"
	IntentMinorChurnSwitchComp = "switch_competitor"
	IntentMinorChurnStopUsing  = "stop_using"
)

// intent_buy 子类
const (
	IntentMinorIntentBuyReady    = "ready_to_buy"
	IntentMinorIntentBuyNeedInfo = "need_more_info"
	IntentMinorIntentBuyApproval = "need_approval"
)

// ask_product 子类
const (
	IntentMinorAskProductFeature = "feature_query"
	IntentMinorAskProductAvail   = "availability"
	IntentMinorAskProductSpec    = "spec_query"
)

// IntentResult 精细意图识别结果
type IntentResult struct {
	Major      string  `json:"major"`
	Minor      string  `json:"minor"`
	Confidence float64 `json:"confidence"`
	Method     string  `json:"method"`
	Reasoning  string  `json:"reasoning,omitempty"`
	LatencyMs  int     `json:"latency_ms,omitempty"`
	LLMModel   string  `json:"llm_model,omitempty"`
}

type minorRule struct {
	minor    string
	keywords []string
	weight   int
}

type majorRule struct {
	major    string
	minors   []minorRule
	keywords []string
}

var fineIntentRules = []majorRule{
	{
		major:    IntentMajorConsult,
		keywords: []string{"咨询", "了解一下", "请问", "想问", "请问一下", "想了解", "怎么处理", "怎么回事"},
		minors: []minorRule{
			{minor: IntentMinorConsultGeneral, keywords: []string{"咨询", "了解一下", "想了解", "请问"}, weight: 1},
			{minor: IntentMinorConsultProductSpecific, keywords: []string{"这个产品", "这个功能", "这个服务", "这个方案", "你们的产品"}, weight: 2},
			{minor: IntentMinorConsultComparison, keywords: []string{"对比", "比较", "区别", "哪个好", "差异", "优缺点", "对比一下", "两款"}, weight: 4},
		},
	},
	{
		major:    IntentMajorPriceInquiry,
		keywords: []string{"多少钱", "价格", "报价", "价位", "怎么卖", "怎么收费", "费用"},
		minors: []minorRule{
			{minor: IntentMinorPriceBudgetCheck, keywords: []string{"预算", "多少钱", "价位", "什么价位"}, weight: 2},
			{minor: IntentMinorPriceDiscountReq, keywords: []string{"优惠", "折扣", "便宜", "便宜点", "减免", "满减", "活动"}, weight: 3},
			{minor: IntentMinorPricePaymentTerms, keywords: []string{"付款", "分期", "月付", "年付", "支付方式", "结算", "结账"}, weight: 3},
		},
	},
	{
		major:    IntentMajorObjection,
		keywords: []string{"不需要", "太贵", "再考虑", "再说", "不放心", "别家"},
		minors: []minorRule{
			{minor: IntentMinorObjectionPriceHigh, keywords: []string{"太贵", "价格高", "有点高", "不值", "不划算", "贵了"}, weight: 3},
			{minor: IntentMinorObjectionTrustIssue, keywords: []string{"不放心", "骗人", "假的", "靠谱吗", "真的吗", "信不过", "能信吗"}, weight: 3},
			{minor: IntentMinorObjectionTimingBad, keywords: []string{"再说", "过段时间", "等以后", "不急", "等几天", "最近忙", "没时间"}, weight: 3},
			{minor: IntentMinorObjectionCompetitorCmp, keywords: []string{"别的家", "别家", "竞品", "其他品牌", "友商", "对比一下", "比别家", "他家"}, weight: 4},
		},
	},
	{
		major:    IntentMajorAfterSale,
		keywords: []string{"售后", "坏了", "有问题", "故障", "退货", "退款"},
		minors: []minorRule{
			{minor: IntentMinorAfterSaleQuality, keywords: []string{"坏了", "故障", "用不了", "不能用了", "质量问题", "有问题", "出问题"}, weight: 3},
			{minor: IntentMinorAfterSaleDelivery, keywords: []string{"发货", "物流", "快递", "多久到", "没收到", "配送", "运送"}, weight: 3},
			{minor: IntentMinorAfterSaleRefund, keywords: []string{"退货", "退款", "退钱", "退换", "换货", "要退货"}, weight: 3},
			{minor: IntentMinorAfterSaleWarranty, keywords: []string{"保修", "维修", "保养", "质保", "三包"}, weight: 3},
		},
	},
	{
		major:    IntentMajorComplaint,
		keywords: []string{"投诉", "差评", "举报", "气愤", "愤怒"},
		minors: []minorRule{
			{minor: IntentMinorComplaintService, keywords: []string{"服务态度", "客服态度", "处理慢", "没人理", "不负责", "服务差"}, weight: 3},
			{minor: IntentMinorComplaintProduct, keywords: []string{"产品差", "垃圾产品", "质量差", "不能用", "假货", "次品"}, weight: 3},
			{minor: IntentMinorComplaintBilling, keywords: []string{"多扣钱", "乱扣费", "账单错", "费用不对", "计费错", "重复扣款"}, weight: 3},
		},
	},
	{
		major:    IntentMajorChurn,
		keywords: []string{"拉黑", "删除", "退订", "别再发", "取消"},
		minors: []minorRule{
			{minor: IntentMinorChurnCancelSub, keywords: []string{"取消订阅", "取消会员", "退订", "取消续费", "解约"}, weight: 3},
			{minor: IntentMinorChurnSwitchComp, keywords: []string{"换别家", "用竞品", "转其他", "投奔", "换品牌", "别家更好"}, weight: 3},
			{minor: IntentMinorChurnStopUsing, keywords: []string{"不用了", "停用", "拉黑", "删除", "别再发", "再发举报", "屏蔽"}, weight: 3},
		},
	},
	{
		major:    IntentMajorIntentBuy,
		keywords: []string{"怎么买", "下单", "购买", "付款", "要了"},
		minors: []minorRule{
			{minor: IntentMinorIntentBuyReady, keywords: []string{"下单", "购买", "怎么买", "怎么付款", "要了", "来一个", "买了"}, weight: 3},
			{minor: IntentMinorIntentBuyNeedInfo, keywords: []string{"还需要", "想确认", "再了解一下", "更多信息", "具体情况"}, weight: 2},
			{minor: IntentMinorIntentBuyApproval, keywords: []string{"审批", "请示", "汇报", "老板同意", "领导同意", "走流程"}, weight: 3},
		},
	},
	{
		major:    IntentMajorAskProduct,
		keywords: []string{"功能", "效果", "怎么用", "规格", "参数"},
		minors: []minorRule{
			{minor: IntentMinorAskProductFeature, keywords: []string{"功能", "特点", "能做什么", "支持什么", "效果"}, weight: 3},
			{minor: IntentMinorAskProductAvail, keywords: []string{"有货", "现货", "什么时候有", "能买吗", "可售", "库存"}, weight: 3},
			{minor: IntentMinorAskProductSpec, keywords: []string{"规格", "参数", "型号", "尺寸", "容量", "配置"}, weight: 3},
		},
	},
}

func minorLLMThreshold() float64 {
	return GlobalConfigParam().GetFloat(context.Background(), "agent_llm", "minor_llm_threshold", 0.6)
}

// RecognizeIntent 精细意图识别入口
//
// 流程：
//  1. 规则匹配（快速）：返回 IntentResult（method=rule）
//  2. 若 confidence < 0.6 且 dispatcher 可用：触发 LLM 二次识别（method=hybrid）
//  3. 若规则匹配为空：直接走 LLM 识别（method=llm）
//  4. 持久化 IntentLog
//  5. 返回最终结果
func (s *IntentRecognizer) RecognizeIntent(ctx context.Context, message, customerID, sessionID string) (*IntentResult, error) {
	start := time.Now()
	if message == "" {
		return &IntentResult{
			Major:      IntentMajorConsult,
			Minor:      IntentMinorConsultGeneral,
			Confidence: 0.3,
			Method:     "rule",
		}, nil
	}

	if !IntentEnabled {
		return &IntentResult{
			Major:      IntentMajorConsult,
			Minor:      IntentMinorConsultGeneral,
			Confidence: 0.3,
			Method:     "disabled",
		}, nil
	}

	ruleResult := s.recognizeFineByRule(ctx, message)

	var final *IntentResult
	if ruleResult != nil {
		final = ruleResult
		if ruleResult.Confidence < minorLLMThreshold() && s.dispatcher != nil {
			if llmResult, err := s.recognizeFineByLLM(ctx, message); err == nil && llmResult != nil {
				llmResult.Method = "hybrid"
				final = llmResult
			}
		}
	} else if s.dispatcher != nil {
		if llmResult, err := s.recognizeFineByLLM(ctx, message); err == nil && llmResult != nil {
			final = llmResult
		}
	}

	if final == nil {
		final = &IntentResult{
			Major:      IntentMajorConsult,
			Minor:      IntentMinorConsultGeneral,
			Confidence: 0.3,
			Method:     "rule",
		}
	}

	final.LatencyMs = int(time.Since(start).Milliseconds())

	s.saveIntentLog(ctx, customerID, sessionID, message, final)

	if customerID != "" {
		s.triggerSOPByIntent(ctx, customerID, sessionID, final.Major, final.Confidence)
	}

	return final, nil
}

func (s *IntentRecognizer) recognizeFineByRule(ctx context.Context, text string) *IntentResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var (
		bestMajor string
		bestMinor string
		bestScore int
	)

	for _, mr := range fineIntentRules {
		for _, mi := range mr.minors {
			score := 0
			for _, kw := range mi.keywords {
				if strings.Contains(text, kw) {
					score += mi.weight * len(kw)
				}
			}
			if score > bestScore {
				bestScore = score
				bestMajor = mr.major
				bestMinor = mi.minor
			}
		}
		if bestMajor == "" {
			majorScore := 0
			for _, kw := range mr.keywords {
				if strings.Contains(text, kw) {
					majorScore += len(kw)
				}
			}
			if majorScore > bestScore {
				bestScore = majorScore
				bestMajor = mr.major
				if len(mr.minors) > 0 {
					bestMinor = mr.minors[0].minor
				}
			}
		}
	}

	if bestMajor == "" || bestScore == 0 {
		return nil
	}

	conf := 0.5 + float64(bestScore)*0.03
	if conf > 0.92 {
		conf = 0.92
	}

	return &IntentResult{
		Major:      bestMajor,
		Minor:      bestMinor,
		Confidence: conf,
		Method:     "rule",
	}
}

func (s *IntentRecognizer) recognizeFineByLLM(ctx context.Context, text string) (*IntentResult, error) {
	if s.dispatcher == nil {
		return nil, fmt.Errorf("dispatcher is nil")
	}

	var sb strings.Builder
	for _, mr := range fineIntentRules {
		sb.WriteString(fmt.Sprintf("- %s\n", mr.major))
	}

	prompt := fmt.Sprintf(`你是销售对话意图识别专家。分析以下客户消息，从给定意图列表中选择最匹配的 1 个大类。

【客户消息】: %s

【意图列表】:
%s
unknown: 无法确定或消息不属于以上任何意图（这是合法答案，信息不足时必须选择它）

【输出要求】(严格按 JSON 格式输出，不要添加任何其他内容):
{
  "major": "8 大类之一或 unknown",
  "confidence": 0.0-1.0 的置信度,
  "reasoning": "简短解释为何选择此意图（不超过 50 字）"
}`, text, sb.String())

	result, err := s.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:    llm.ScenarioIntentRecognize,
		Prompt:      prompt,
		JSONMode:    true,
		MaxTokens:   300,
		Temperature: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("llm dispatch: %w", err)
	}

	var parsed struct {
		Major      string  `json:"major"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return nil, fmt.Errorf("parse llm intent: %w", err)
	}

	if !isValidMajor(parsed.Major) || parsed.Confidence < 0.7 {
		parsed.Major = "unknown"
	}
	if parsed.Confidence <= 0 {
		parsed.Confidence = 0.5
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}

	minor := refineMinorByKeywords(parsed.Major, text)
	return &IntentResult{
		Major:      parsed.Major,
		Minor:      minor,
		Confidence: parsed.Confidence,
		Method:     "llm",
		Reasoning:  parsed.Reasoning,
		LLMModel:   result.Model,
	}, nil
}

func refineMinorByKeywords(major, text string) string {
	if !isValidMajor(major) {
		return ""
	}
	bestMinor := ""
	bestScore := -1
	for _, mr := range fineIntentRules {
		if mr.major != major {
			continue
		}
		for _, mi := range mr.minors {
			score := 0
			for _, kw := range mi.keywords {
				if strings.Contains(text, kw) {
					score += mi.weight * len(kw)
				}
			}
			if score > bestScore {
				bestScore = score
				bestMinor = mi.minor
			}
		}
		if bestScore > 0 {
			return bestMinor
		}

		if len(mr.minors) > 0 {
			return mr.minors[0].minor
		}
	}
	return IntentMinorConsultGeneral
}

func isValidMajor(major string) bool {
	for _, mr := range fineIntentRules {
		if mr.major == major {
			return true
		}
	}
	return false
}

func isValidMinor(major, minor string) bool {
	for _, mr := range fineIntentRules {
		if mr.major == major {
			for _, mi := range mr.minors {
				if mi.minor == minor {
					return true
				}
			}
		}
	}
	return false
}

func getDefaultMinor(major string) string {
	for _, mr := range fineIntentRules {
		if mr.major == major && len(mr.minors) > 0 {
			return mr.minors[0].minor
		}
	}
	return IntentMinorConsultGeneral
}

func (s *IntentRecognizer) saveIntentLog(ctx context.Context, customerID, sessionID, message string, result *IntentResult) {
	if s.logRepo == nil || result == nil {
		return
	}
	log := &model.IntentLog{
		CustomerID:  customerID,
		SessionID:   sessionID,
		Message:     message,
		IntentMajor: result.Major,
		IntentMinor: result.Minor,
		Confidence:  result.Confidence,
		Method:      result.Method,
		LatencyMs:   result.LatencyMs,
		Reasoning:   result.Reasoning,
		Timestamp:   time.Now(),
		Source:      model.IntentLogSource,
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("intent_recognition_fine: async persist recovered from panic: %v", r)
			}
		}()
		if err := s.logRepo.Create(context.Background(), log); err != nil {
			logger.Errorf("intent_recognition_fine: async persist intent log failed: %v", err)
		}
	}()
}

// GetIntentLogs 查询意图识别日志
//
// 参数：
//   - customerID: 客户 ID（可选，空表示不限）
//   - major: 大类过滤（可选，空表示不限）
//   - limit: 返回条数上限
func (s *IntentRecognizer) GetIntentLogs(ctx context.Context, customerID, major string, limit int) ([]model.IntentLog, error) {
	if s.logRepo == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return s.logRepo.List(ctx, customerID, major, limit)
}

// GetIntentLogStats 意图识别统计（按 major + minor 聚合）
//
// 参数：
//   - days: 统计最近 N 天的数据
func (s *IntentRecognizer) GetIntentLogStats(ctx context.Context, days int) (map[string]any, error) {
	if s.logRepo == nil {
		return nil, nil
	}
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	majorStats, err := s.logRepo.GetMajorStatsSince(ctx, since)
	if err != nil {
		return nil, err
	}

	minorStats, err := s.logRepo.GetMinorStatsSince(ctx, since)
	if err != nil {
		return nil, err
	}

	methodStats, err := s.logRepo.GetMethodStatsSince(ctx, since)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"days":         days,
		"by_major":     majorStats,
		"by_minor":     minorStats,
		"by_method":    methodStats,
		"generated_at": time.Now(),
	}, nil
}

// QueryIntentLogsByTraceID 通过 trace_id 查询该 trace 关联的所有 IntentLog
// 供 trace API 使用，由 controller 调用
func (s *IntentRecognizer) QueryIntentLogsByTraceID(ctx context.Context, traceID string) ([]model.IntentLog, error) {
	if s.logRepo == nil || traceID == "" {
		return nil, nil
	}
	return s.logRepo.ListByTraceID(ctx, traceID)
}
