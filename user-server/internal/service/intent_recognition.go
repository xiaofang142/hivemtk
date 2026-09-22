package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// IntentEnabled 意图识别总开关（内存态）
//
// 重构：业务链路统一使用一个总开关管控意图识别流程
//
// 行为：
//   - true:  进入意图识别流程（规则匹配 → LLM 识别 → 兜底）
//   - false: 跳过意图识别流程，直接返回 IntentUnknown 兜底
//
// 不管 LLM 是本地 API 还是云端 API，都由本开关统一控制；
// 本开关关闭时，规则匹配和 LLM 识别都不会被调用，业务链路步骤 3 直接走兜底，
// 后续 SOP 匹配/RAG 召回/LLM 生成等步骤仍可继续。
//
// 持久化：存于 system_config_kv 表，key=intent_recognition_config；
// 由 InitIntentRecognizer 启动加载、UpdateIntentConfig 写入更新。
// 前端 user-web 意图识别页面可在线开关，无需重启服务。
//
// 读写只走紧随其后的那对 accessor（intentEnabledMu 守，包内与包外其余位置直读 0 处）：
// Recognize / RecognizeIntent / RecognizeSpeculative 三条都在入站与恢复协程上读它，
// 而 router 的热更新口与用例的开关用例都在写它 —— 它是这批里唯一一个导出全局，
// 所以"包外零直读"这条由 scripts/check-seam-guard.py 扫整棵 user-server 树来兜。
var (
	intentEnabledMu sync.RWMutex

	IntentEnabled = true
)

func loadIntentEnabled() bool {
	intentEnabledMu.RLock()
	defer intentEnabledMu.RUnlock()
	return IntentEnabled
}

func storeIntentEnabled(enabled bool) {
	intentEnabledMu.Lock()
	defer intentEnabledMu.Unlock()
	IntentEnabled = enabled
}

// IntentConfigKey system_config_kv 表的存储 key
const IntentConfigKey = "intent_recognition_config"

// IntentConfig 意图识别配置（DB 持久化结构）
type IntentConfig struct {
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// IntentRecognizer 销售意图识别器
type IntentRecognizer struct {
	recordRepo       *repository.IntentRecordRepository
	logRepo          *repository.IntentLogRepository
	sopExecutionRepo *repository.SopExecutionRepository
	dispatcher       *llm.Dispatcher
	cache            *redis.Client
	sopService       *SOPService

	embedSvc          llm.EmbeddingServiceInterface
	exampleRepo       *repository.IntentExampleRepository
	anchorMu          sync.RWMutex
	anchorVecs        map[string][][]float32
	embDisabled       bool
	embFailCount      int
	embLastPrecompute time.Time
}

// NewIntentRecognizer 创建意图识别器
func NewIntentRecognizer(db *gorm.DB, dispatcher *llm.Dispatcher, cache *redis.Client) *IntentRecognizer {
	var recordRepo *repository.IntentRecordRepository
	var logRepo *repository.IntentLogRepository
	if db != nil {
		recordRepo = repository.NewIntentRecordRepository()
		recordRepo.SetDB(context.Background(), db)
		logRepo = repository.NewIntentLogRepository()
		logRepo.SetDB(context.Background(), db)
	}
	rec := &IntentRecognizer{
		recordRepo:       recordRepo,
		logRepo:          logRepo,
		sopExecutionRepo: repository.NewSopExecutionRepository(db),
		dispatcher:       dispatcher,
		cache:            cache,
	}
	if db != nil {
		rec.exampleRepo = repository.NewIntentExampleRepository()
		rec.exampleRepo.SetDB(context.Background(), db)
	}
	return rec
}

// SetSOPService 注入 SOP 服务用于意图→SOP 联动
func (s *IntentRecognizer) SetSOPService(ctx context.Context, svc *SOPService) {
	s.sopService = svc
}

// SetEmbeddingService 注入 Embedding 服务并后台预计算意图锚点向量，
// 启用三级级联的中间层（规则未命中 → Embedding → LLM）。
// 预计算异步执行不阻塞启动；失败时中间层静默停用，行为退回原两级级联（fail-open，零破坏）。
func (s *IntentRecognizer) SetEmbeddingService(svc llm.EmbeddingServiceInterface) {
	if svc == nil {
		return
	}
	s.embedSvc = svc
	go s.safePrecomputeAnchors()
}

func (s *IntentRecognizer) safePrecomputeAnchors() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[Intent] 锚点预计算 panic 已隔离: %v", r)
		}
	}()
	s.precomputeAnchors()
}

const intentEmbeddingTop1 = 0.75

const intentEmbeddingGap = 0.05

const intentTopKExamples = 3

func (s *IntentRecognizer) precomputeAnchors() {
	ctx := context.Background()
	if s.loadAnchorsFromDB(ctx) {
		return
	}
	vecs := make(map[string][][]float32, len(DefaultIntents))
	var dbRows []*model.IntentExample
	for _, def := range DefaultIntents {
		for _, ex := range def.Examples {
			v, err := s.embedSvc.EmbedOne(ctx, s.embedSvc.DefaultConfig(), ex)
			if err != nil || len(v) == 0 {
				logger.Warnf("[Intent] 锚点预计算失败 intent=%s example=%s: %v", def.Type, ex, err)
				continue
			}
			vecs[def.Type] = append(vecs[def.Type], v)
			dbRows = append(dbRows, &model.IntentExample{Intent: def.Type, Text: ex, Vector: vecToPGLiteral(v)})
		}
	}
	s.anchorMu.Lock()
	s.anchorVecs = vecs

	s.embDisabled = len(vecs) == 0
	if len(vecs) > 0 {
		s.embFailCount = 0
	}
	s.embLastPrecompute = time.Now()
	s.anchorMu.Unlock()
	total := 0
	for _, v := range vecs {
		total += len(v)
	}
	logger.Infof("[Intent] Embedding 中间层就绪: intents=%d anchors=%d disabled=%v", len(vecs), total, len(vecs) == 0)

	if len(dbRows) > 0 && s.exampleRepo != nil {
		if err := s.exampleRepo.UpsertBatch(ctx, dbRows); err != nil {
			logger.Warnf("[Intent] 示例向量落库失败（下次启动将重算）: %v", err)
		}
	}
}

func (s *IntentRecognizer) loadAnchorsFromDB(ctx context.Context) bool {
	if s.exampleRepo == nil || s.embedSvc == nil {
		return false
	}
	rows, err := s.exampleRepo.ListAll(ctx)
	if err != nil || len(rows) == 0 {
		return false
	}
	vecs := make(map[string][][]float32, len(DefaultIntents))
	dim := s.embedSvc.DefaultConfig().Dimension
	loaded := 0
	for _, row := range rows {
		v, perr := parsePGVectorLiteral(row.Vector, dim)
		if perr != nil || len(v) == 0 {
			continue
		}
		vecs[row.Intent] = append(vecs[row.Intent], v)
		loaded++
	}
	if loaded == 0 {
		return false
	}
	s.anchorMu.Lock()
	s.anchorVecs = vecs
	s.embDisabled = false
	s.embFailCount = 0
	s.embLastPrecompute = time.Now()
	s.anchorMu.Unlock()
	logger.Infof("[Intent] Embedding 中间层从 intent_examples 表就绪: intents=%d anchors=%d", len(vecs), loaded)
	return true
}

func (s *IntentRecognizer) EnsureIntentExamplesIndexed(ctx context.Context) (int, error) {
	if s.embedSvc == nil {
		return 0, fmt.Errorf("embedding service not injected")
	}
	if s.exampleRepo == nil {
		return 0, fmt.Errorf("intent_examples repository unavailable (db is nil)")
	}
	existing, err := s.exampleRepo.ListAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("list intent_examples: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, r := range existing {
		have[r.Text] = true
	}
	imported := 0
	var batch []*model.IntentExample
	for _, def := range DefaultIntents {
		for _, ex := range def.Examples {
			if have[ex] {
				continue
			}
			v, err := s.embedSvc.EmbedOne(ctx, s.embedSvc.DefaultConfig(), ex)
			if err != nil || len(v) == 0 {
				return imported, fmt.Errorf("embed example %q: %w", ex, err)
			}
			batch = append(batch, &model.IntentExample{Intent: def.Type, Text: ex, Vector: vecToPGLiteral(v)})
			imported++
		}
	}
	if err := s.exampleRepo.UpsertBatch(ctx, batch); err != nil {
		return imported, fmt.Errorf("upsert intent_examples: %w", err)
	}
	return imported, nil
}

func vecToPGLiteral(v []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "%g", x)
	}
	sb.WriteByte(']')
	return sb.String()
}

func parsePGVectorLiteral(s string, expectDim int) ([]float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil, fmt.Errorf("empty vector literal")
	}
	parts := strings.Split(s, ",")
	if expectDim > 0 && len(parts) != expectDim {
		return nil, fmt.Errorf("vector dim mismatch: got %d want %d", len(parts), expectDim)
	}
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector element %d: %w", i, err)
		}
		out[i] = float32(f)
	}
	return out, nil
}

const embRetryCooldown = 60 * time.Second

func (s *IntentRecognizer) recognizeByEmbedding(ctx context.Context, text string) *dto.RecognizeResult {
	if s.embedSvc == nil {
		return nil
	}
	s.anchorMu.RLock()
	anchors := s.anchorVecs
	disabled := s.embDisabled
	lastTry := s.embLastPrecompute
	s.anchorMu.RUnlock()

	if len(anchors) == 0 || disabled {
		if time.Since(lastTry) < embRetryCooldown {
			return nil
		}
		s.anchorMu.Lock()
		shouldRetry := !s.embLastPrecompute.After(time.Now().Add(-embRetryCooldown))
		if shouldRetry {
			s.embLastPrecompute = time.Now()
		}
		s.anchorMu.Unlock()
		if shouldRetry {
			logger.Info("[Intent] Embedding 锚点为空，冷却到期触发后台重试预计算")
			go s.safePrecomputeAnchors()
		}
		return nil
	}
	qvec, err := s.embedSvc.EmbedOne(ctx, s.embedSvc.DefaultConfig(), text)
	if err != nil || len(qvec) == 0 {
		s.anchorMu.Lock()
		s.embFailCount++
		if s.embFailCount >= 3 {
			s.embDisabled = true
			logger.Warnf("[Intent] Embedding 连续失败 %d 次，中间层熔断降级为两级级联", s.embFailCount)
		}
		s.anchorMu.Unlock()
		return nil
	}
	s.anchorMu.Lock()
	s.embFailCount = 0
	s.anchorMu.Unlock()
	top1Type, top1Sim, top2Sim, top2Type := "", -1.0, -1.0, ""
	for _, def := range DefaultIntents {
		vecs := anchors[def.Type]
		best := -1.0
		for _, v := range vecs {
			if sim := cosineSimilarity(qvec, v); sim > best {
				best = sim
			}
		}
		if best < 0 {
			continue
		}
		if best > top1Sim {
			top2Sim = top1Sim
			top2Type = top1Type
			top1Sim = best
			top1Type = def.Type
		} else if best > top2Sim {
			top2Sim = best
			top2Type = def.Type
		}
	}
	if top1Type == "" || top1Sim < intentEmbeddingTop1 {
		return nil
	}

	if top1Sim-top2Sim < intentEmbeddingGap && top2Sim >= intentEmbeddingTop1 {
		conf := 0.55 + top1Sim*0.4
		if conf > 0.92 {
			conf = 0.92
		}
		logger.Infof("[Intent] embedding 歧义缺口 top1=%.3f(%s) top2=%.3f(%s) gap<%.2f → clarify",
			top1Sim, top1Type, top2Sim, top2Type, intentEmbeddingGap)
		return &dto.RecognizeResult{
			IntentType:      IntentClarify,
			IntentName:      "歧义澄清",
			Confidence:      conf,
			ConfidenceLevel: "medium",
			Sentiment:       "neutral",
			Method:          "embedding",
			Entities: map[string]any{
				"top1_intent": top1Type,
				"top2_intent": top2Type,
				"gap":         fmt.Sprintf("%.3f", top1Sim-top2Sim),
			},
		}
	}

	var def IntentDef
	for _, d := range DefaultIntents {
		if d.Type == top1Type {
			def = d
			break
		}
	}
	conf := 0.55 + top1Sim*0.4
	if conf > 0.92 {
		conf = 0.92
	}
	level := "medium"
	if conf >= 0.85 {
		level = "high"
	}
	return &dto.RecognizeResult{
		IntentType:      def.Type,
		IntentName:      def.Name,
		Confidence:      conf,
		ConfidenceLevel: level,
		Sentiment:       inferSentiment(def.Type),
		Method:          "embedding",
	}
}

// IntentType 销售意图类型常量
const (
	IntentPriceInquiry        = "price_inquiry"
	IntentObjectionPrice      = "objection_price"
	IntentObjectionNeed       = "objection_need"
	IntentObjectionTrust      = "objection_trust"
	IntentObjectionCompetitor = "objection_competitor"
	IntentObjectionTiming     = "objection_timing"
	IntentPurchase            = "purchase"
	IntentAskProduct          = "ask_product"
	IntentAskService          = "ask_service"
	IntentAfterSale           = "after_sale"
	IntentChurn               = "churn"
	IntentSocial              = "social"
	IntentGreeting            = "greeting"
	IntentComplaint           = "complaint"
	IntentUnknown             = "unknown"
	IntentClarify             = "clarify"

	IntentStall         = IntentObjectionTiming
	IntentAskTrust      = IntentObjectionTrust
	IntentAskCompetitor = IntentObjectionCompetitor
)

// IntentDef 意图定义
type IntentDef struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Keywords    []string `json:"keywords"`
	Examples    []string `json:"examples"`
	Description string   `json:"description"`
}

// DefaultIntents 默认意图词典
var DefaultIntents = []IntentDef{
	{
		Type: IntentPriceInquiry, Name: "价格咨询",
		Keywords:    []string{"多少钱", "价格", "怎么卖", "怎么收费", "报价", "便宜", "优惠", "折扣", "多少钱一套", "什么价位"},
		Examples:    []string{"这个多少钱？", "你们价格怎么样？", "能不能便宜点？", "有优惠活动吗？"},
		Description: "客户询问价格或优惠",
	},
	{
		Type: IntentObjectionPrice, Name: "价格异议",
		Keywords:    []string{"太贵了", "价格高", "价格有点高", "有点高", "太贵", "不值", "不值这个价", "不划算", "太贵啦", "贵了"},
		Examples:    []string{"这个东西太贵了", "价格有点高啊", "比别家贵好多", "我感觉不值这个价"},
		Description: "客户对价格有异议",
	},
	{
		Type: IntentObjectionNeed, Name: "需求异议",
		Keywords:    []string{"不需要", "用不上", "没需求", "暂时不用", "再说吧", "再考虑", "看看吧", "再想想"},
		Examples:    []string{"我先看看吧", "暂时不需要", "我再考虑一下"},
		Description: "客户表示暂无需求",
	},
	{
		Type: IntentObjectionTrust, Name: "信任异议",
		Keywords:    []string{"不放心", "骗人", "假的", "靠谱吗", "真的吗", "不会是骗子", "信不过", "能信吗"},
		Examples:    []string{"你们是正规公司吗？", "能信吗？", "不会跑路吧？"},
		Description: "客户对品牌/服务存在信任问题",
	},
	{
		Type: IntentObjectionCompetitor, Name: "竞品异议",
		Keywords:    []string{"别的家", "别家", "竞品", "其他品牌", "友商", "对比一下", "比别家贵", "别家也是"},
		Examples:    []string{"别家比你们便宜", "我用别家用了几年了", "你们跟XX有什么区别？"},
		Description: "客户提到竞品对比",
	},
	{
		Type: IntentObjectionTiming, Name: "时机异议",
		Keywords:    []string{"再说", "过段时间", "等以后", "不急", "等几天", "最近忙", "没时间", "等下"},
		Examples:    []string{"过段时间再说吧", "最近比较忙", "等下再联系"},
		Description: "客户以时间为由推脱",
	},
	{
		Type: IntentPurchase, Name: "购买意向",
		Keywords:    []string{"怎么买", "怎么付款", "怎么下单", "下单", "购买", "要", "来一个", "买一个", "要了", "付了", "拍了", "怎么付"},
		Examples:    []string{"我要买，怎么付款？", "怎么下单？", "我直接拍了啊"},
		Description: "客户明确购买意向",
	},
	{
		Type: IntentAskProduct, Name: "产品咨询",
		Keywords:    []string{"功能", "特点", "效果", "怎么用", "如何使用", "参数", "规格", "材质", "质量", "能做什么"},
		Examples:    []string{"这个产品有什么功能？", "效果怎么样？", "怎么用？"},
		Description: "客户咨询产品细节",
	},
	{
		Type: IntentAskService, Name: "服务咨询",
		Keywords:    []string{"售后", "保修", "维修", "包邮", "多久到", "发货", "物流", "客服"},
		Examples:    []string{"售后怎么保障？", "多久能到？", "包邮吗？"},
		Description: "客户咨询售后服务",
	},
	{
		Type: IntentAfterSale, Name: "售后问题",
		Keywords:    []string{"坏了", "有问题", "故障", "用不了", "不能用了", "退货", "退换", "换货", "投诉", "退款", "怎么办", "要退货"},
		Examples:    []string{"我买的东西坏了", "想退货怎么办？", "有问题需要处理"},
		Description: "客户售后诉求",
	},
	{
		Type: IntentChurn, Name: "流失倾向",
		Keywords:    []string{"拉黑", "删除", "别发了", "别再发了", "烦", "退订", "屏蔽", "再发举报"},
		Examples:    []string{"别再发了", "我要拉黑你了", "再发我举报了"},
		Description: "客户明确表示流失/反感",
	},
	{

		Type: IntentGreeting, Name: "打招呼问候",
		Keywords:    []string{"你好", "您好", "hi", "hello", "哈喽", "早上好", "晚上好", "下午好", "嗨"},
		Examples:    []string{"你好", "hello", "hi", "您好"},
		Description: "客户打招呼/开场问候",
	},
	{
		Type: IntentSocial, Name: "社交寒暄",
		Keywords:    []string{"在吗", "在么", "在?", "你叫什么", "在不在"},
		Examples:    []string{"在吗？", "在么", "你叫什么名字"},
		Description: "客户社交寒暄/追问开场",
	},
	{
		Type: IntentComplaint, Name: "投诉",
		Keywords:    []string{"投诉", "差评", "垃圾", "气死", "愤怒", "过分", "太过分", "不负责", "解决不了"},
		Examples:    []string{"我要投诉你", "差评！", "太过分了"},
		Description: "客户投诉",
	},
}

// Recognize 识别意图
//
// 流程（重构）：
//  1. 文本为空 → 直接返回 IntentUnknown
//  2. 总开关 IntentEnabled 关闭 → 直接返回 IntentUnknown（不进入流程）
//  3. 总开关开启：
//     a. 规则匹配 → 命中即返回
//     b. 规则未命中 → LLM 识别（本地/云端 API 统一）
//     c. 全部失败 → IntentUnknown 兜底
//  4. 意图→SOP 联动
func (s *IntentRecognizer) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	if text == "" {
		return &dto.RecognizeResult{IntentType: IntentUnknown, Confidence: 0, Method: "rule"}, nil
	}

	if !loadIntentEnabled() {
		return &dto.RecognizeResult{
			IntentType:      IntentUnknown,
			IntentName:      "未知",
			Confidence:      0.3,
			ConfidenceLevel: "low",
			Sentiment:       "neutral",
			Method:          "disabled",
		}, nil
	}

	var result *dto.RecognizeResult

	if r := s.recognizeByRule(ctx, text); r != nil {
		s.saveRecord(ctx, sessionID, customerID, text, r, "", 0, 0)
		result = r
	} else if r := s.recognizeByEmbedding(ctx, text); r != nil {

		s.saveRecord(ctx, sessionID, customerID, text, r, "", 0, 0)
		result = r
	} else if s.dispatcher != nil {
		if r, err := s.recognizeByLLM(ctx, text); err == nil && r != nil {
			s.saveRecord(ctx, sessionID, customerID, text, r, r.LLMModel, r.CostTokens, r.LatencyMs)
			result = r
		}
	}

	if result == nil {
		result = &dto.RecognizeResult{
			IntentType:      IntentUnknown,
			IntentName:      "未知",
			Confidence:      0.3,
			ConfidenceLevel: "low",
			Sentiment:       "neutral",
			Method:          "rule",
		}
	}

	RecordIntentWeakLabel(result)

	if customerID != "" {
		s.triggerSOPByIntent(ctx, customerID, sessionID, result.IntentType, result.Confidence)
	}

	s.fillTopKExamplesDynamic(ctx, text, result)

	return result, nil
}

func (s *IntentRecognizer) triggerSOPByIntent(ctx context.Context, customerID, sessionID, intentType string, confidence float64) {
	if s.sopService == nil {
		return
	}
	if confidence < 0.7 {
		return
	}

	if intentType == IntentClarify || intentType == IntentUnknown {
		return
	}
	matches, err := s.sopService.MatchByIntent(ctx, intentType)
	if err != nil || len(matches) == 0 {
		return
	}
	for _, agent := range matches {
		if !agent.IsActive {
			continue
		}

		if s.sopExecutionRepo != nil {
			count, err := s.sopExecutionRepo.CountRunningBySOPAndCustomer(ctx, agent.ID, customerID, SOPStatusRunning)
			if err != nil || count > 0 {
				continue
			}
		}
		exec, err := s.sopService.Execute(ctx, &dto.ExecuteRequest{
			SOPID:      agent.ID,
			CustomerID: customerID,
			SessionID:  sessionID,
			Input: map[string]any{
				"_trigger":     "intent",
				"_intent_type": intentType,
				"_confidence":  confidence,
			},
		})
		if err != nil {
			logger.Errorf("[Intent→SOP] 启动失败 intent=%s sop=%d: %v", intentType, agent.ID, err)
			continue
		}
		logger.Infof("[Intent→SOP] 触发成功 intent=%s sop=%d exec=%d", intentType, agent.ID, exec.ID)
	}
}

func (s *IntentRecognizer) recognizeByRule(ctx context.Context, text string) *dto.RecognizeResult {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	activeIntents := intentsForRule()
	for _, def := range activeIntents {
		for _, ex := range def.Examples {
			if strings.EqualFold(text, ex) {
				return &dto.RecognizeResult{
					IntentType:      def.Type,
					IntentName:      def.Name,
					Confidence:      0.95,
					ConfidenceLevel: "high",
					Sentiment:       inferSentiment(def.Type),
					Method:          "rule",
				}
			}
		}
	}

	var bestMatch *IntentDef
	bestScore := 0
	for _, def := range activeIntents {
		score := 0
		for _, kw := range def.Keywords {
			if strings.Contains(text, kw) {
				score += len(kw)
			}
		}
		if score > bestScore {
			bestScore = score
			bestMatch = &def
		}
	}
	if bestMatch != nil && bestScore > 0 {
		conf := 0.7 + float64(bestScore)*0.02
		if conf > 0.92 {
			conf = 0.92
		}
		level := "medium"
		if conf >= 0.85 {
			level = "high"
		}
		return &dto.RecognizeResult{
			IntentType:      bestMatch.Type,
			IntentName:      bestMatch.Name,
			Confidence:      conf,
			ConfidenceLevel: level,
			Sentiment:       inferSentiment(bestMatch.Type),
			Method:          "rule",
		}
	}
	return nil
}

func (s *IntentRecognizer) recognizeByLLM(ctx context.Context, text string) (*dto.RecognizeResult, error) {
	intentList := make([]string, 0, len(DefaultIntents))
	for _, def := range DefaultIntents {
		intentList = append(intentList, fmt.Sprintf("%s: %s", def.Type, def.Description))
	}
	prompt := fmt.Sprintf(`你是销售对话意图识别专家。分析以下客户消息，从给定意图列表中选择最匹配的 1 个意图。

【客户消息】: %s

【意图列表】:
%s
unknown: 无法确定或消息不属于以上任何意图（这是合法答案，信息不足时必须选择它）

【输出要求】(严格按 JSON 格式输出):
{
  "intent_type": "从列表中选择最匹配的 type，无法确定时填 unknown",
  "intent_subtype": "细分类型(可填空字符串)",
  "confidence": 0.0-1.0 的置信度,
  "entities": {
    "product": "涉及的产品(可空)",
    "price": "涉及的价格(可空)",
    "quantity": "涉及的数量(可空)",
    "time": "涉及的时间(可空)"
  },
  "sentiment": "positive/negative/neutral"
}`, text, strings.Join(intentList, "\n"))

	start := time.Now()
	result, err := s.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:    llm.ScenarioIntentRecognize,
		Prompt:      prompt,
		JSONMode:    true,
		MaxTokens:   500,
		Temperature: 0,
	})
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return nil, err
	}

	var parsed struct {
		IntentType    string         `json:"intent_type"`
		IntentSubtype string         `json:"intent_subtype"`
		Confidence    float64        `json:"confidence"`
		Entities      map[string]any `json:"entities"`
		Sentiment     string         `json:"sentiment"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return nil, fmt.Errorf("parse intent: %w", err)
	}
	intentName := parsed.IntentType
	known := false
	for _, def := range DefaultIntents {
		if def.Type == parsed.IntentType {
			intentName = def.Name
			known = true
			break
		}
	}
	if !known {
		parsed.IntentType = "unknown"
		intentName = "未知意图"
	}

	if parsed.IntentType != IntentUnknown && parsed.Confidence < 0.7 {
		logger.Infof("[Intent] LLM 低置信强选降级 unknown: type=%s conf=%.2f", parsed.IntentType, parsed.Confidence)
		parsed.IntentType = "unknown"
		intentName = "未知意图"
		parsed.Sentiment = "neutral"
	}
	level := "medium"
	if parsed.Confidence >= 0.85 {
		level = "high"
	} else if parsed.Confidence < 0.5 {
		level = "low"
	}
	return &dto.RecognizeResult{
		IntentType:      parsed.IntentType,
		IntentName:      intentName,
		Confidence:      parsed.Confidence,
		ConfidenceLevel: level,
		IntentSubtype:   parsed.IntentSubtype,
		Entities:        parsed.Entities,
		Sentiment:       parsed.Sentiment,
		Method:          "llm",
		LLMModel:        result.Model,
		CostTokens:      result.TotalTokens,
		LatencyMs:       latency,
	}, nil
}

func fillTopKExamples(r *dto.RecognizeResult) {
	if r == nil || r.IntentType == "" || r.IntentType == IntentUnknown || r.IntentType == IntentClarify {
		return
	}
	for _, def := range DefaultIntents {
		if def.Type == r.IntentType && len(def.Examples) > 0 {
			k := intentTopKExamples
			if len(def.Examples) < k {
				k = len(def.Examples)
			}
			r.TopKExamples = append([]string(nil), def.Examples[:k]...)
			return
		}
	}
}

func BuildClarifyReply(r *dto.RecognizeResult) string {
	if r == nil || r.IntentType != IntentClarify {
		return ""
	}
	nameOf := func(t string) string {
		for _, def := range DefaultIntents {
			if def.Type == t {
				return def.Name
			}
		}
		return t
	}
	top1, _ := r.Entities["top1_intent"].(string)
	top2, _ := r.Entities["top2_intent"].(string)
	if top1 == "" || top2 == "" {
		return "不好意思，我没太理解您的意思，您是想咨询产品价格，还是在使用上遇到了问题呢？"
	}
	return fmt.Sprintf("不好意思确认一下：您是想了解【%s】，还是想说说【%s】方面的事呢？", nameOf(top1), nameOf(top2))
}

func inferSentiment(intentType string) string {
	switch intentType {
	case IntentChurn, IntentComplaint, IntentObjectionPrice, IntentObjectionNeed, IntentObjectionTrust:
		return "negative"
	case IntentPurchase, IntentPriceInquiry:
		return "positive"
	default:
		return "neutral"
	}
}

func (s *IntentRecognizer) saveRecord(ctx context.Context, sessionID, customerID, text string, result *dto.RecognizeResult, llmModel string, costTokens, latencyMs int) {
	if s.recordRepo == nil {
		return
	}
	entitiesJSON, _ := json.Marshal(result.Entities)
	rec := &model.IntentRecord{
		SessionID:       sessionID,
		CustomerID:      customerID,
		RawText:         text,
		IntentType:      result.IntentType,
		IntentSubtype:   result.IntentSubtype,
		Confidence:      result.Confidence,
		ConfidenceLevel: result.ConfidenceLevel,
		Entities:        model.JSONMap(entitiesToMap(entitiesJSON)),
		Sentiment:       result.Sentiment,
		LLMModel:        llmModel,
		CostTokens:      costTokens,
		LatencyMs:       latencyMs,
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("intent_recognition: async persist recovered from panic: %v", r)
			}
		}()
		if err := s.recordRepo.Create(context.Background(), rec); err != nil {
			logger.Errorf("intent_recognition: async persist intent record failed: %v", err)
		}
	}()
}

func entitiesToMap(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func extractJSONFromStr(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// GetIntentStats 获取意图统计
func (s *IntentRecognizer) GetIntentStats(ctx context.Context, days int) (map[string]int, error) {
	if s.recordRepo == nil {
		return map[string]int{}, nil
	}
	since := time.Now().AddDate(0, 0, -days)
	records, err := s.recordRepo.ListSince(ctx, since)
	if err != nil {
		return nil, err
	}
	stats := make(map[string]int)
	for _, r := range records {
		stats[r.IntentType]++
	}
	return stats, nil
}

// GetMethodLevelStats 获取 method/level 维度统计
//
//   - by_method:  {"rule": 12, "llm": 3}
//   - by_level:   {"high": 5, "medium": 8, "low": 2}
//
// 当 DB 不可用或无记录时返回空 map,前端可降级为不显示该卡片
func (s *IntentRecognizer) GetMethodLevelStats(ctx context.Context, days int) (map[string]int, map[string]int) {
	byMethod := map[string]int{}
	byLevel := map[string]int{}
	if s.recordRepo == nil {
		return byMethod, byLevel
	}
	since := time.Now().AddDate(0, 0, -days)
	rows, err := s.recordRepo.GetMethodLevelStatsSince(ctx, since)
	if err != nil {
		return byMethod, byLevel
	}
	for _, r := range rows {
		byMethod[r.Method]++
		byLevel[r.ConfidenceLevel]++
	}
	return byMethod, byLevel
}

// GetRecentIntents 客户近期意图历史
func (s *IntentRecognizer) GetRecentIntents(ctx context.Context, customerID string, limit int) ([]model.IntentRecord, error) {
	if s.recordRepo == nil {
		return nil, nil
	}
	return s.recordRepo.ListByCustomerID(ctx, customerID, limit)
}

// GetRecentIntentsPaged 分页查询最近意图历史,支持意图类型筛选
//
//   - customerID: 空表示全量
//   - intentType: 空表示不筛选
//   - page:       1-based
//   - pageSize:   每页条数
//
// 返回 (records, total, error)
func (s *IntentRecognizer) GetRecentIntentsPaged(ctx context.Context, customerID, intentType string, page, pageSize int) ([]model.IntentRecord, int64, error) {
	if s.recordRepo == nil {
		return nil, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	return s.recordRepo.ListPaged(ctx, customerID, intentType, page, pageSize)
}

var (
	intentRecognizerOnce sync.Once
	intentRecognizer     *IntentRecognizer
)

// GetIntentRecognizer 获取意图识别器单例
func GetIntentRecognizer() *IntentRecognizer {
	return intentRecognizer
}

// SetIntentEnabled 设置意图识别开关（仅更新内存态，供 router/main 注入或热更新）
func SetIntentEnabled(enabled bool) {
	storeIntentEnabled(enabled)
}

// LoadIntentConfig 从 system_config_kv 表加载意图识别配置
//
// 行为：
//   - DB 不可用 / 无记录：返回默认配置（Enabled=true）+ nil
//   - DB 有记录但 JSON 损坏：返回错误（由调用方决定兜底）
func LoadIntentConfig(ctx context.Context) (*IntentConfig, error) {
	repo := repository.NewSystemConfigKVRepository()
	val, err := repo.Get(ctx, IntentConfigKey)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[intent] 加载意图识别配置失败，使用默认配置（Enabled=true）")
		return &IntentConfig{Enabled: true}, nil
	}
	if val == "" {
		return &IntentConfig{Enabled: true}, nil
	}
	var cfg IntentConfig
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return nil, fmt.Errorf("parse intent config: %w", err)
	}
	return &cfg, nil
}

// SaveIntentConfig 保存意图识别配置到 system_config_kv 表（仅持久化，不更新内存）
//
// 由 controller UpdateConfig 调用持久化后再调用 SetIntentEnabled 更新内存态。
func SaveIntentConfig(ctx context.Context, cfg *IntentConfig) error {
	if cfg == nil {
		return fmt.Errorf("intent config is nil")
	}
	repo := repository.NewSystemConfigKVRepository()
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal intent config: %w", err)
	}
	if _, err := repo.Upsert(ctx, IntentConfigKey, string(data)); err != nil {
		return fmt.Errorf("upsert intent config: %w", err)
	}
	return nil
}

// UpdateIntentConfig 一站式：保存到 DB + 更新内存态 IntentEnabled
//
// 用于 controller UpdateConfig 接口，原子化保证前端开关变化立即生效。
func UpdateIntentConfig(ctx context.Context, cfg *IntentConfig) error {
	if err := SaveIntentConfig(ctx, cfg); err != nil {
		return err
	}
	SetIntentEnabled(cfg.Enabled)
	logger.Infof("[intent] 意图识别配置已更新：Enabled=%v", cfg.Enabled)
	return nil
}

// InitIntentRecognizer 初始化意图识别器
//
// 启动时从 system_config_kv 表加载意图识别开关：
//   - 表无记录：使用默认 Enabled=true
//   - 表有记录：以 DB 配置为准
//   - DB 不可用：降级为默认 Enabled=true（不阻断服务启动）
func InitIntentRecognizer(db *gorm.DB, dispatcher *llm.Dispatcher, cache *redis.Client) *IntentRecognizer {
	intentRecognizerOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
		defer cancel()
		cfg, err := LoadIntentConfig(ctx)
		if err != nil {
			logger.Errorf("[intent] 加载意图识别配置失败：%v，使用默认 Enabled=true", err)
			storeIntentEnabled(true)
		} else {
			storeIntentEnabled(cfg.Enabled)
			logger.Infof("[intent] 已从 DB 加载意图识别配置：Enabled=%v", cfg.Enabled)
		}

		intentRecognizer = NewIntentRecognizer(db, dispatcher, cache)

		intentRecognizer.SetEmbeddingService(llm.NewEmbeddingService())

		InitIntentKeywordOverride(ctx)
	})
	return intentRecognizer
}

// IntentKeywordsOverrideKey 覆盖词表存储 key
const IntentKeywordsOverrideKey = "intent_keywords_override"

var intentOverrideMu sync.RWMutex

var intentKeywordOverride map[string][]string

// LoadIntentKeywordOverride 从 system_config_kv 加载覆盖词表
func LoadIntentKeywordOverride(ctx context.Context) (map[string][]string, error) {
	repo := repository.NewSystemConfigKVRepository()
	val, err := repo.Get(ctx, IntentKeywordsOverrideKey)
	if err != nil || val == "" {
		return nil, err
	}
	out := make(map[string][]string)
	if err := json.Unmarshal([]byte(val), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveIntentKeywordOverride 持久化覆盖词表 + 立即生效（热更新）
func SaveIntentKeywordOverride(ctx context.Context, override map[string][]string) error {
	data, err := json.Marshal(override)
	if err != nil {
		return fmt.Errorf("marshal intent override: %w", err)
	}
	repo := repository.NewSystemConfigKVRepository()
	if _, err := repo.Upsert(ctx, IntentKeywordsOverrideKey, string(data)); err != nil {
		return fmt.Errorf("upsert intent override: %w", err)
	}
	intentOverrideMu.Lock()
	intentKeywordOverride = override
	intentOverrideMu.Unlock()
	logger.Infof("[intent] 意图词表覆盖已更新：%d 类", len(override))
	return nil
}

// GetIntentKeywordOverride 读取当前覆盖词表快照（controller 用）
func GetIntentKeywordOverride() map[string][]string {
	intentOverrideMu.RLock()
	defer intentOverrideMu.RUnlock()
	out := make(map[string][]string, len(intentKeywordOverride))
	for k, v := range intentKeywordOverride {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// InitIntentKeywordOverride 启动时加载覆盖词表（失败静默降级默认词表）
func InitIntentKeywordOverride(ctx context.Context) {
	m, err := LoadIntentKeywordOverride(ctx)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Msg("[intent] 意图词表覆盖加载失败，使用默认词表")
		return
	}
	intentOverrideMu.Lock()
	intentKeywordOverride = m
	intentOverrideMu.Unlock()
	if len(m) > 0 {
		logger.Infof("[intent] 意图词表覆盖已加载：%d 类", len(m))
	}
}

func intentsForRule() []IntentDef {
	intentOverrideMu.RLock()
	override := intentKeywordOverride
	intentOverrideMu.RUnlock()
	if len(override) == 0 {
		return DefaultIntents
	}
	merged := make([]IntentDef, 0, len(DefaultIntents))
	for _, def := range DefaultIntents {
		extra, ok := override[def.Type]
		if ok && len(extra) > 0 {
			seen := make(map[string]bool, len(def.Keywords)+len(extra))
			for _, kw := range def.Keywords {
				seen[kw] = true
			}
			appended := append([]string(nil), def.Keywords...)
			for _, kw := range extra {
				kw = strings.TrimSpace(kw)
				if kw != "" && !seen[kw] {
					appended = append(appended, kw)
					seen[kw] = true
				}
			}
			def.Keywords = appended
		}
		merged = append(merged, def)
	}
	return merged
}
