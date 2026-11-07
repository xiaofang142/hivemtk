package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// FeedbackLearningService 反馈学习闭环服务
type FeedbackLearningService struct {
	feedbackRepo *repository.FeedbackLearningRepository
	sopAgentRepo *repository.SopAgentRepository
	sopExecRepo  *repository.SopExecutionRepository
}

// NewFeedbackLearningService 创建反馈学习服务
//
// repo 为空时 repos 保持 nil（service 各方法会返回 "db not configured" 错误）。
// AV-04c 批次5 刀2：两个 SOP 兄弟仓储改经包级 Shared 构造共享注入连接，
// service 不再经 repo.GetDB() 取句柄转手（批次4 NewTraceLearningRepositoryShared
// 先例）。
func NewFeedbackLearningService(repo *repository.FeedbackLearningRepository) *FeedbackLearningService {
	s := &FeedbackLearningService{}
	if repo != nil {
		s.feedbackRepo = repo
		s.sopAgentRepo = repository.NewSopAgentRepositoryShared(repo)
		s.sopExecRepo = repository.NewSopExecutionRepositoryShared(repo)
	}
	return s
}

// ChampionDimensionScore 单维度得分
type ChampionDimensionScore struct {
	Dimension     model.SalesChampionDimension `json:"dimension"`
	Name          string                       `json:"name"`
	Score         float64                      `json:"score"`
	SampleCount   int                          `json:"sample_count"`
	PositiveCount int                          `json:"positive_count"`
	NegativeCount int                          `json:"negative_count"`
	EvidenceTags  []string                     `json:"evidence_tags"`
}

// ChampionProfileReport 销冠画像报告（5 维度雷达）
type ChampionProfileReport struct {
	StaffID      uint                     `json:"staff_id"`
	StaffName    string                   `json:"staff_name"`
	Scenario     string                   `json:"scenario"`
	Dimensions   []ChampionDimensionScore `json:"dimensions"`
	OverallScore float64                  `json:"overall_score"`
	PeriodStart  time.Time                `json:"period_start"`
	PeriodEnd    time.Time                `json:"period_end"`
	GeneratedAt  time.Time                `json:"generated_at"`
}

var dimensionNameMap = map[model.SalesChampionDimension]string{
	model.DimensionObjectionHandling:   "异议处理能力",
	model.DimensionClosingInvitation:   "逼单邀约能力",
	model.DimensionFollowupActivation:  "跟进激活能力",
	model.DimensionNurturingConversion: "培育转化能力",
	model.DimensionRepurchaseOperation: "复购运营能力",
}

// DimensionName 维度中文名
func DimensionName(d model.SalesChampionDimension) string {
	if name, ok := dimensionNameMap[d]; ok {
		return name
	}
	return string(d)
}

// ExtractProfile 提取销冠画像（从对话记录）
// staffID=0 表示系统级 智能体
// scenario 标识场景，如 "ai_champion" 或 "staff_123"
func (s *FeedbackLearningService) ExtractProfile(ctx context.Context, staffID uint, staffName, scenario string, periodStart, periodEnd time.Time) (*ChampionProfileReport, error) {
	if s.feedbackRepo == nil {
		return nil, fmt.Errorf("db not configured")
	}
	if periodEnd.IsZero() {
		periodEnd = time.Now()
	}
	if periodStart.IsZero() {
		periodStart = periodEnd.AddDate(0, 0, -30)
	}

	messages, err := s.feedbackRepo.ListAIMessagesByPeriod(ctx, periodStart, periodEnd, 5000)
	if err != nil {
		return nil, fmt.Errorf("query ai messages: %w", err)
	}

	sessionIDs := extractSessionIDs(messages)
	customerMsgMap, _ := s.queryCustomerMessages(ctx, sessionIDs, periodStart, periodEnd)

	report := &ChampionProfileReport{
		StaffID:     staffID,
		StaffName:   staffName,
		Scenario:    scenario,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		GeneratedAt: time.Now(),
	}

	for _, dim := range model.AllSalesChampionDimensions {
		score := s.extractDimension(ctx, dim, messages, customerMsgMap)
		score.Name = DimensionName(dim)
		report.Dimensions = append(report.Dimensions, score)
	}

	total := 0.0
	for _, d := range report.Dimensions {
		total += d.Score
	}
	if len(report.Dimensions) > 0 {
		report.OverallScore = total / float64(len(report.Dimensions))
	}

	if err := s.persistProfileSnapshot(ctx, report); err != nil {
		logger.Errorf("feedback_learning: persistProfileSnapshot failed: %v", err)
	}

	return report, nil
}

func (s *FeedbackLearningService) extractDimension(ctx context.Context, dim model.SalesChampionDimension, messages []model.SessionMessage, customerMsgs map[string][]model.SessionMessage) ChampionDimensionScore {
	score := ChampionDimensionScore{
		Dimension:    dim,
		EvidenceTags: []string{},
	}

	var positive, negative int
	var evidences []string

	for _, msg := range messages {
		ctx := ""
		if msgs, ok := customerMsgs[msg.SessionID]; ok {
			ctx = latestCustomerMessage(msgs, msg.CreatedAt)
		}

		hit, evidence := classifyDimensionHit(dim, msg.Content, ctx)
		if !hit {
			continue
		}
		score.SampleCount++
		if evidence.positive {
			positive++
			evidences = append(evidences, evidence.tag+"✓")
		} else {
			negative++
			evidences = append(evidences, evidence.tag+"✗")
		}
	}

	score.PositiveCount = positive
	score.NegativeCount = negative

	score.EvidenceTags = dedupeTags(evidences, 5)

	if score.SampleCount == 0 {
		score.Score = 50.0
	} else {
		score.Score = float64(positive) / float64(score.SampleCount) * 100
	}
	score.Score = roundTo2(score.Score)

	return score
}

type dimensionEvidence struct {
	positive bool
	tag      string
}

func classifyDimensionHit(dim model.SalesChampionDimension, aiReply, customerMsg string) (bool, dimensionEvidence) {
	switch dim {
	case model.DimensionObjectionHandling:
		return classifyObjectionHandling(aiReply, customerMsg)
	case model.DimensionClosingInvitation:
		return classifyClosingInvitation(aiReply, customerMsg)
	case model.DimensionFollowupActivation:
		return classifyFollowupActivation(aiReply, customerMsg)
	case model.DimensionNurturingConversion:
		return classifyNurturingConversion(aiReply, customerMsg)
	case model.DimensionRepurchaseOperation:
		return classifyRepurchaseOperation(aiReply, customerMsg)
	}
	return false, dimensionEvidence{}
}

func classifyObjectionHandling(aiReply, customerMsg string) (bool, dimensionEvidence) {
	objectionWords := []string{"太贵", "贵了", "不需要", "不用了", "考虑一下", "考虑下",
		"再说吧", "算了", "别人家", "其他家", "竞品", "对比", "不值", "不划算"}
	hasObjection := false
	for _, w := range objectionWords {
		if strings.Contains(customerMsg, w) {
			hasObjection = true
			break
		}
	}
	if !hasObjection {
		return false, dimensionEvidence{}
	}

	positiveWords := []string{"理解", "明白", "确实", "其实", "性价比", "价值",
		"活动", "优惠", "划算", "对比", "适合", "帮您", "值得", "保障", "服务"}
	negativeWords := []string{"好的，再见", "打扰了", "抱歉打扰", "那算了"}
	for _, w := range positiveWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: true, tag: "异议已回应"}
		}
	}
	for _, w := range negativeWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: false, tag: "异议放弃"}
		}
	}
	if len([]rune(aiReply)) < 10 {
		return true, dimensionEvidence{positive: false, tag: "回应过短"}
	}
	return true, dimensionEvidence{positive: true, tag: "异议已回应"}
}

func classifyClosingInvitation(aiReply, customerMsg string) (bool, dimensionEvidence) {
	closingWords := []string{"下单", "预约", "到店", "体验", "试试", "下单", "购买",
		"付款", "订金", "锁定", "名额", "限时", "今天", "马上", "安排", "帮您下单"}
	hasClosing := false
	for _, w := range closingWords {
		if strings.Contains(aiReply, w) {
			hasClosing = true
			break
		}
	}
	if !hasClosing {
		return false, dimensionEvidence{}
	}

	positiveSignals := []string{"好的", "可以", "行", "试试", "预约", "下单", "安排",
		"怎么付", "多少钱", "可以试试", "好"}
	negativeSignals := []string{"不用", "算了", "再看看", "考虑", "不了", "下次"}
	for _, w := range positiveSignals {
		if strings.Contains(customerMsg, w) {
			return true, dimensionEvidence{positive: true, tag: "逼单成功"}
		}
	}
	for _, w := range negativeSignals {
		if strings.Contains(customerMsg, w) {
			return true, dimensionEvidence{positive: false, tag: "逼单被拒"}
		}
	}
	return true, dimensionEvidence{positive: true, tag: "逼单发起"}
}

func classifyFollowupActivation(aiReply, customerMsg string) (bool, dimensionEvidence) {
	followupWords := []string{"好久没", "最近", "在吗", "打扰了", "想您", "想到您",
		"更新", "新品", "到货", "上架", "活动", "优惠", "福利",
		"怎么不回", "怎么不", "赶紧", "还不", "为什么不回"}
	hasFollowup := false
	for _, w := range followupWords {
		if strings.Contains(aiReply, w) {
			hasFollowup = true
			break
		}
	}
	if !hasFollowup {
		return false, dimensionEvidence{}
	}

	positiveWords := []string{"活动", "优惠", "新品", "到货", "福利", "适合", "建议", "帮您"}
	negativeWords := []string{"催", "怎么不回", "为什么不", "还不", "赶紧"}
	for _, w := range positiveWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: true, tag: "价值跟进"}
		}
	}
	for _, w := range negativeWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: false, tag: "催促式跟进"}
		}
	}
	return true, dimensionEvidence{positive: true, tag: "常规跟进"}
}

func classifyNurturingConversion(aiReply, customerMsg string) (bool, dimensionEvidence) {
	nurturingWords := []string{"之前", "上次", "记得", "了解您", "根据您", "为您",
		"推荐", "适合", "专属", "定制", "方案"}
	hasNurturing := false
	for _, w := range nurturingWords {
		if strings.Contains(aiReply, w) {
			hasNurturing = true
			break
		}
	}
	if !hasNurturing {
		return false, dimensionEvidence{}
	}

	positiveWords := []string{"推荐", "适合", "专属", "方案", "建议", "帮您", "安排"}
	for _, w := range positiveWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: true, tag: "个性化培育"}
		}
	}
	return true, dimensionEvidence{positive: false, tag: "培育无推进"}
}

func classifyRepurchaseOperation(aiReply, customerMsg string) (bool, dimensionEvidence) {
	repurchaseWords := []string{"老客", "老朋友", "回购", "复购", "再次", "回头",
		"推荐", "分享", "老用户", "会员", "积分", "专属"}
	hasRepurchase := false
	for _, w := range repurchaseWords {
		if strings.Contains(aiReply, w) {
			hasRepurchase = true
			break
		}
	}
	if !hasRepurchase {
		return false, dimensionEvidence{}
	}

	positiveWords := []string{"会员", "积分", "专属", "优惠", "福利", "老朋友", "感谢"}
	for _, w := range positiveWords {
		if strings.Contains(aiReply, w) {
			return true, dimensionEvidence{positive: true, tag: "老客权益"}
		}
	}
	return true, dimensionEvidence{positive: true, tag: "复购触达"}
}

// SOPNodeConversionStats SOP 节点转化率统计
type SOPNodeConversionStats struct {
	SOPID             uint                   `json:"sop_id"`
	SOPName           string                 `json:"sop_name"`
	Variant           string                 `json:"variant"`
	Nodes             []NodeConversionDetail `json:"nodes"`
	TotalExecutions   int64                  `json:"total_executions"`
	OverallConversion float64                `json:"overall_conversion"`
	GeneratedAt       time.Time              `json:"generated_at"`
}

// NodeConversionDetail 节点转化详情
type NodeConversionDetail struct {
	NodeID         string  `json:"node_id"`
	NodeName       string  `json:"node_name"`
	NodeType       string  `json:"node_type"`
	EnteredCount   int     `json:"entered_count"`
	SuccessCount   int     `json:"success_count"`
	AbandonedCount int     `json:"abandoned_count"`
	FailedCount    int     `json:"failed_count"`
	ConversionRate float64 `json:"conversion_rate"`
	DropRate       float64 `json:"drop_rate"`
	AvgDurationMs  int     `json:"avg_duration_ms"`
	IsBottleneck   bool    `json:"is_bottleneck"`
}

// AnalyzeNodeConversion 分析 SOP 节点转化率
func (s *FeedbackLearningService) AnalyzeNodeConversion(ctx context.Context, sopID uint, variant string) (*SOPNodeConversionStats, error) {
	if s.feedbackRepo == nil {
		return nil, fmt.Errorf("db not configured")
	}

	agent, err := s.sopAgentRepo.GetByID(ctx, sopID)
	if err != nil {
		return nil, fmt.Errorf("query sop: %w", err)
	}

	transitions, err := s.feedbackRepo.ListNodeTransitionsBySOPAndVariant(ctx, sopID, variant)
	if err != nil {
		return nil, fmt.Errorf("query transitions: %w", err)
	}

	stats := &SOPNodeConversionStats{
		SOPID:       sopID,
		SOPName:     agent.Name,
		Variant:     variant,
		GeneratedAt: time.Now(),
	}

	nodeMap := make(map[string]*NodeConversionDetail)
	for _, t := range transitions {
		node := nodeMap[t.ToNode]
		if node == nil {
			node = &NodeConversionDetail{
				NodeID:   t.ToNode,
				NodeType: t.NodeType,
			}
			nodeMap[t.ToNode] = node
		}
		node.EnteredCount++
		switch t.Outcome {
		case model.NodeOutcomeSuccess:
			node.SuccessCount++
		case model.NodeOutcomeAbandoned:
			node.AbandonedCount++
		case model.NodeOutcomeFailed:
			node.FailedCount++
		}
		node.AvgDurationMs = (node.AvgDurationMs*(node.EnteredCount-1) + t.DurationMs) / node.EnteredCount
	}

	for _, node := range nodeMap {
		if node.EnteredCount > 0 {
			node.ConversionRate = float64(node.SuccessCount) / float64(node.EnteredCount) * 100
			node.DropRate = float64(node.AbandonedCount) / float64(node.EnteredCount) * 100
		}
		if node.ConversionRate < 50 && node.EnteredCount >= 5 {
			node.IsBottleneck = true
		}
		node.ConversionRate = roundTo2(node.ConversionRate)
		node.DropRate = roundTo2(node.DropRate)
		stats.Nodes = append(stats.Nodes, *node)
	}

	stats.TotalExecutions, _ = s.sopExecRepo.CountBySOPID(ctx, sopID)

	if stats.TotalExecutions > 0 {
		successCount, _ := s.sopExecRepo.CountBySOPIDAndStatus(ctx, sopID, SOPStatusSuccess)
		stats.OverallConversion = float64(successCount) / float64(stats.TotalExecutions) * 100
		stats.OverallConversion = roundTo2(stats.OverallConversion)
	}

	return stats, nil
}

// OptimizationSuggestionInput 优化建议生成输入
type OptimizationSuggestionInput struct {
	SOPID             uint
	SOPName           string
	NodeConversion    *SOPNodeConversionStats
	LowScoreThreshold float64
	MinSampleCount    int
}

// GenerateOptimizationSuggestions 为低转化节点生成优化建议
func (s *FeedbackLearningService) GenerateOptimizationSuggestions(ctx context.Context, input OptimizationSuggestionInput) ([]model.OptimizationSuggestion, error) {
	if s.feedbackRepo == nil {
		return nil, fmt.Errorf("db not configured")
	}
	if input.NodeConversion == nil {
		return nil, fmt.Errorf("node conversion stats required")
	}
	threshold := input.LowScoreThreshold
	if threshold <= 0 {
		threshold = 50
	}
	minSample := input.MinSampleCount
	if minSample <= 0 {
		minSample = 5
	}

	var suggestions []model.OptimizationSuggestion
	for _, node := range input.NodeConversion.Nodes {
		if node.ConversionRate >= threshold || node.EnteredCount < minSample {
			continue
		}
		sug := buildOptimizationSuggestion(input.SOPID, input.SOPName, node, threshold)
		suggestions = append(suggestions, sug)
	}

	if len(suggestions) > 0 {
		if err := s.feedbackRepo.CreateSuggestionsInBatches(ctx, suggestions, 100); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Int("count", len(suggestions)).
				Msg("[feedback] batch insert suggestions failed")
		}
	}

	return suggestions, nil
}

func buildOptimizationSuggestion(sopID uint, sopName string, node NodeConversionDetail, threshold float64) model.OptimizationSuggestion {
	sug := model.OptimizationSuggestion{
		SOPID:        sopID,
		SOPName:      sopName,
		NodeID:       node.NodeID,
		NodeName:     node.NodeName,
		NodeType:     node.NodeType,
		CurrentScore: node.ConversionRate,
		Threshold:    threshold,
		SampleCount:  node.EnteredCount,
		Status:       model.SuggestionStatusPending,
	}

	switch node.NodeType {
	case "llm":
		sug.SuggestionType = model.SuggestionTypePromptRewrite
		sug.Priority = 2
		sug.SuggestionText = fmt.Sprintf(
			"LLM 节点 [%s] 转化率 %.1f%%（低于阈值 %.0f%%），流失 %d 人。"+
				"建议优化 prompt：1) 增加共情话术；2) 明确下一步行动号召；3) 精简回复长度。",
			node.NodeName, node.ConversionRate, threshold, node.AbandonedCount)
		sug.ExpectedImpact = "预计提升转化率 15-25%"
	case "condition":
		sug.SuggestionType = model.SuggestionTypeBranchPrune
		sug.Priority = 1
		sug.SuggestionText = fmt.Sprintf(
			"条件节点 [%s] 转化率 %.1f%%（低于阈值 %.0f%%），流失 %d 人。"+
				"建议审查分支条件：1) 简化条件判断；2) 增加兜底分支；3) 调整分支优先级。",
			node.NodeName, node.ConversionRate, threshold, node.AbandonedCount)
		sug.ExpectedImpact = "预计减少流失 20-30%"
	case "action":
		if node.AbandonedCount > node.FailedCount {
			sug.SuggestionType = model.SuggestionTypeTimingAdjust
			sug.Priority = 1
			sug.SuggestionText = fmt.Sprintf(
				"动作节点 [%s] 流失率 %.1f%%。建议调整触达时机或频率，避免过度打扰。",
				node.NodeName, node.DropRate)
		} else {
			sug.SuggestionType = model.SuggestionTypeNodeMerge
			sug.Priority = 0
			sug.SuggestionText = fmt.Sprintf(
				"动作节点 [%s] 失败 %d 次。建议检查动作配置或合并相邻动作节点。",
				node.NodeName, node.FailedCount)
		}
		sug.ExpectedImpact = "预计提升稳定性 10-20%"
	default:
		sug.SuggestionType = model.SuggestionTypeAddObjection
		sug.Priority = 0
		sug.SuggestionText = fmt.Sprintf(
			"节点 [%s] 转化率 %.1f%%。建议补充异议处理分支和共情话术。",
			node.NodeName, node.ConversionRate)
		sug.ExpectedImpact = "预计提升转化率 5-15%"
	}

	sug.EvidenceData = map[string]any{
		"entered_count":   node.EnteredCount,
		"success_count":   node.SuccessCount,
		"abandoned_count": node.AbandonedCount,
		"failed_count":    node.FailedCount,
		"conversion_rate": node.ConversionRate,
		"drop_rate":       node.DropRate,
		"is_bottleneck":   node.IsBottleneck,
	}

	return sug
}

// RecordNodeTransition 记录节点流转
// 供 SalesEngine / SOPService 在执行 SOP 时调用
func (s *FeedbackLearningService) RecordNodeTransition(ctx context.Context, t *model.SOPNodeTransition) error {
	if s.feedbackRepo == nil {
		return nil
	}
	return s.feedbackRepo.CreateNodeTransition(ctx, t)
}

// ListPendingSuggestions 列出待审核建议
func (s *FeedbackLearningService) ListPendingSuggestions(ctx context.Context, sopID uint, limit int) ([]model.OptimizationSuggestion, error) {
	if s.feedbackRepo == nil {
		return nil, fmt.Errorf("db not configured")
	}
	list, err := s.feedbackRepo.ListPendingSuggestions(ctx, sopID, limit)
	if err != nil {
		return nil, fmt.Errorf("list suggestions: %w", err)
	}
	return list, nil
}

// ReviewSuggestion 审核建议
func (s *FeedbackLearningService) ReviewSuggestion(ctx context.Context, suggestionID uint, reviewerID uint, action string) error {
	if s.feedbackRepo == nil {
		return fmt.Errorf("db not configured")
	}
	now := time.Now()
	updates := map[string]any{
		"reviewed_by": reviewerID,
		"reviewed_at": &now,
	}
	switch action {
	case "approve":
		updates["status"] = model.SuggestionStatusApproved
	case "reject":
		updates["status"] = model.SuggestionStatusRejected
	case "apply":
		updates["status"] = model.SuggestionStatusApplied
		updates["applied_at"] = &now
	default:
		return fmt.Errorf("invalid action: %s (expected approve/reject/apply)", action)
	}
	return s.feedbackRepo.UpdateSuggestionFields(ctx, suggestionID, updates)
}

// GetLatestProfile 获取最新画像快照
func (s *FeedbackLearningService) GetLatestProfile(ctx context.Context, staffID uint, scenario string) ([]model.SalesChampionProfileSnapshot, error) {
	if s.feedbackRepo == nil {
		return nil, fmt.Errorf("db not configured")
	}
	list, err := s.feedbackRepo.ListLatestProfileSnapshots(ctx, staffID, scenario, 5)
	if err != nil {
		return nil, fmt.Errorf("query profile: %w", err)
	}
	return list, nil
}

func (s *FeedbackLearningService) persistProfileSnapshot(ctx context.Context, report *ChampionProfileReport) error {
	for _, dim := range report.Dimensions {
		snapshot := model.SalesChampionProfileSnapshot{
			StaffID:       report.StaffID,
			StaffName:     report.StaffName,
			Scenario:      report.Scenario,
			Dimension:     string(dim.Dimension),
			Score:         dim.Score,
			SampleCount:   dim.SampleCount,
			PositiveCount: dim.PositiveCount,
			NegativeCount: dim.NegativeCount,
			EvidenceTags:  toStringArray(dim.EvidenceTags),
			PeriodStart:   report.PeriodStart,
			PeriodEnd:     report.PeriodEnd,
		}
		if err := s.feedbackRepo.CreateProfileSnapshot(ctx, &snapshot); err != nil {
			return fmt.Errorf("persist snapshot: %w", err)
		}
	}
	return nil
}

func (s *FeedbackLearningService) queryCustomerMessages(ctx context.Context, sessionIDs []string, start, end time.Time) (map[string][]model.SessionMessage, error) {
	if len(sessionIDs) == 0 {
		return map[string][]model.SessionMessage{}, nil
	}
	msgs, err := s.feedbackRepo.ListCustomerMessagesBySessions(ctx, sessionIDs, start, end)
	if err != nil {
		return nil, err
	}
	m := make(map[string][]model.SessionMessage)
	for _, msg := range msgs {
		m[msg.SessionID] = append(m[msg.SessionID], msg)
	}
	return m, nil
}

func extractSessionIDs(messages []model.SessionMessage) []string {
	seen := map[string]bool{}
	var ids []string
	for _, m := range messages {
		if !seen[m.SessionID] {
			seen[m.SessionID] = true
			ids = append(ids, m.SessionID)
		}
	}
	return ids
}

func latestCustomerMessage(msgs []model.SessionMessage, before time.Time) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].CreatedAt.Before(before) || msgs[i].CreatedAt.Equal(before) {
			return msgs[i].Content
		}
	}
	return ""
}

func dedupeTags(tags []string, max int) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

func toStringArray(items []string) model.JSONArray {
	if len(items) == 0 {
		return model.JSONArray{}
	}
	out := make(model.JSONArray, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func roundTo2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
