package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"sync/atomic"
	"time"

	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// answerCacheNamespace 确认最终答案能否作为稳定纯知识结果复用。
func (o *SmartCSOrchestrator) answerCacheNamespace(
	ctx context.Context,
	in *IncomingContext,
	session *model.CustomerSession,
	agent *AgentContext,
	kbVersion string,
) string {
	if ctx == nil || ctx.Err() != nil || tenant.IsSystemScope(ctx) || o == nil || o.engine == nil ||
		in == nil || session == nil || agent == nil || o.faqEmbedder == nil || kbVersion == "" {
		return ""
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 || session.TenantID != tenantID || agent.AgentID == 0 ||
		in.AccountID == "" || in.AccountID == "default" || in.Platform == "" || session.SessionID == "" ||
		session.Platform != in.Platform || session.AccountID != in.AccountID {
		return ""
	}
	if !o.engine.answerCacheExecutionStable(ctx, agent) || o.confidenceAgg != nil {
		return ""
	}
	// 缓存只复用不含客户事实的知识答案，不能以缓存命中替代上下文读取。
	req := &SalesRequest{SessionID: session.SessionID, CustomerID: session.UserID, OneID: in.OneID,
		UserMessage: in.Content, Platform: string(in.Platform)}
	memory, err := o.engine.recallMemory(ctx, req)
	if err != nil || memory == nil || answerCacheMemoryIsPersonal(memory) {
		return ""
	}
	if o.engine.customerLookup != nil {
		customer, customerErr := o.engine.resolveCustomer(ctx, req)
		if customerErr != nil || customer != nil {
			return ""
		}
	}
	if !o.engine.answerCacheIntentStable(ctx, req, memory) {
		return ""
	}
	config := o.faqEmbedder.DefaultConfig()
	if config == nil || config.Model == "" || config.Dimension <= 0 || ctx.Err() != nil {
		return ""
	}
	dispatch := o.engine.dispatcher.ResponseCacheFingerprint()
	if dispatch == "" {
		return ""
	}
	// 配置（包含凭证变化）只进入摘要计算，不输出原值或可读缓存键。
	payload := struct {
		Version      int
		TenantID     uint
		Channel      string
		AccountID    string
		Agent        *AgentContext
		KBVersion    string
		Embedding    any
		Dispatcher   string
		Threshold    float64
		Compact      string
		CardDelivery string
	}{
		Version: 1, TenantID: tenantID, Channel: string(in.Platform), AccountID: in.AccountID,
		Agent: agent, KBVersion: kbVersion, Embedding: config, Dispatcher: dispatch,
		Threshold: o.answerCacheThreshold(agent), Compact: compactReplyPolicy,
		CardDelivery: channelCardPrompt(string(in.Platform)),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// 命中最终答案前仍执行只读意图与流程检查，不以缓存绕过否决或必要步骤。
func (e *SalesEngine) answerCacheIntentStable(ctx context.Context, req *SalesRequest, memory *model.DialogueMemory) bool {
	if e.intent == nil || GetAssetResolver() != nil {
		return false
	}
	preview, ok := e.intent.(IntentPreviewer)
	if !ok {
		return false
	}
	intent, err := preview.PreviewIntent(ctx, req.UserMessage)
	if err != nil || intent == nil || intent.IntentType == IntentUnknown || intent.IntentType == IntentClarify {
		return false
	}
	if transfer, _ := e.shouldTransferToHuman(ctx, intent, memory, req); transfer {
		return false
	}
	// matchSOP 的兼容路径会吞仓储错误；资格确认直接核对只读接口。
	if e.sop != nil {
		sops, sopErr := e.sop.MatchByIntent(ctx, intent.IntentType)
		if sopErr != nil || len(sops) != 0 {
			return false
		}
	}
	if e.scriptLookup != nil {
		script, scriptErr := e.scriptLookup.MatchScript(ctx, intent.IntentType, intent.IntentName)
		if scriptErr != nil || script != nil {
			return false
		}
	}
	return ctx.Err() == nil
}

func (e *SalesEngine) answerCacheExecutionStable(ctx context.Context, agent *AgentContext) bool {
	dynamicAgent := !agent.EnableRAG || agent.EnableScriptMatch || agent.EnableHumanizePolish ||
		agent.EnableContentAudit || agent.EnablePlaybook || agent.AssetBundleID != "" ||
		len(agent.SOPIDs) > 0 || len(agent.SOPTemplateIDs) > 0 || len(agent.ScriptLibraryIDs) > 0 || len(agent.Tools) > 0
	dynamicEngine := e.layerRouter != nil || e.confidenceAggregator != nil || e.humanizeEvaluator != nil ||
		e.intentChain != nil || e.glossary != nil || e.calibrator != nil || e.playbook != nil ||
		e.faqSvc != nil || (e.behaviorPl != nil && e.behaviorPl.IsEnabled())
	if dynamicAgent || dynamicEngine {
		return false
	}
	// 空代理工具白名单代表默认工具集，不等于没有运行时工具。
	return e.toolExecutor == nil || len(e.toolExecutor.ListTools(ctx)) == 0
}

func answerCacheMemoryIsPersonal(m *model.DialogueMemory) bool {
	return strings.TrimSpace(m.Summary) != "" || len(m.KeyFacts) > 0 || m.CustomerName != "" ||
		m.CustomerPhone != "" || m.CustomerWechat != "" || m.Budget != "" || m.Demand != "" ||
		len(m.Objections) > 0 || m.PurchaseIntent != "" || len(m.IntentTrail) > 0 || len(m.SOPHistory) > 0 ||
		m.LastAction != "" || m.NextActionSuggestion != "" || m.MessageCount > 1
}

func (o *SmartCSOrchestrator) answerCacheThreshold(agent *AgentContext) float64 {
	if agent != nil && agent.ConfidenceThreshold > 0 {
		return agent.ConfidenceThreshold
	}
	return o.confidenceThreshold
}

type answerCacheWrite struct {
	Incoming          *IncomingContext
	Session           *model.CustomerSession
	Agent             *AgentContext
	KBVersion         string
	Namespace         string
	KBID              string
	Vector            []float32
	SourceKBUpdatedAt *time.Time
	Response          *SalesResponse
}

type answerCacheWriteCounters struct {
	completed atomic.Uint64
	failed    atomic.Uint64
	dropped   atomic.Uint64
	inFlight  atomic.Uint64
}

// AnswerCacheWriteStats.Completed 表示后台 Store 调用完成，不证明插入了新行或平台交付。
type AnswerCacheWriteStats struct {
	Completed uint64
	Failed    uint64
	Dropped   uint64
	InFlight  uint64
}

func (o *SmartCSOrchestrator) AnswerCacheWriteStats() AnswerCacheWriteStats {
	if o == nil {
		return AnswerCacheWriteStats{}
	}
	return AnswerCacheWriteStats{
		Completed: o.answerCacheWriteCounters.completed.Load(),
		Failed:    o.answerCacheWriteCounters.failed.Load(),
		Dropped:   o.answerCacheWriteCounters.dropped.Load(),
		InFlight:  o.answerCacheWriteCounters.inFlight.Load(),
	}
}

func answerCacheResultReusable(response *SalesResponse, threshold float64) bool {
	if response == nil || response.Memory == nil || response.Intent == nil ||
		response.Degraded || response.TransferredToHuman || response.DefiniteAnswer ||
		response.Polished || response.Audited || len(response.AuditIssues) != 0 ||
		len(response.Cards) != 0 || response.MatchedSOP != nil || response.ScriptTemplate != nil ||
		len(response.PlaybookSuggestions) != 0 || response.SendPlan != nil ||
		strings.TrimSpace(response.LLMProvider) == "" || strings.TrimSpace(response.LLMModel) == "" ||
		strings.TrimSpace(response.Reply) == "" || ragcache.IsRefusalAnswer(response.Reply) ||
		len(response.RAGChunks) == 0 || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 {
		return false
	}
	if response.Intent.IntentType == IntentUnknown || response.Intent.IntentType == IntentClarify ||
		response.Intent.IntentType == IntentComplaint || response.Intent.IntentType == IntentChurn {
		return false
	}
	for _, step := range response.Steps {
		if step.Status != "ok" && step.Status != "skip" {
			return false
		}
	}
	m := response.Memory
	if answerCacheMemoryIsPersonal(&model.DialogueMemory{
		Summary: m.Summary, KeyFacts: model.JSONMap(m.KeyFacts),
		CustomerName: m.CustomerName, CustomerPhone: m.CustomerPhone, CustomerWechat: m.CustomerWechat,
		Budget: m.Budget, Demand: m.Demand, Objections: model.JSONArray(m.Objections),
		PurchaseIntent: m.PurchaseIntent, IntentTrail: model.JSONArray(m.IntentTrail),
		SOPHistory: model.JSONArray(m.SOPHistory), LastAction: m.LastAction,
		NextActionSuggestion: m.NextActionSuggestion, MessageCount: m.MessageCount,
	}) {
		return false
	}
	score := response.RAGChunks[0].Score
	return !math.IsNaN(score) && !math.IsInf(score, 0) && score >= threshold
}

func (o *SmartCSOrchestrator) queueAnswerCacheWrite(ctx context.Context, request answerCacheWrite) {
	if o == nil || o.faqCache == nil || ctx == nil || ctx.Err() != nil || request.Namespace == "" ||
		request.KBID == "" || len(request.Vector) == 0 || request.SourceKBUpdatedAt == nil ||
		request.SourceKBUpdatedAt.IsZero() || !answerCacheResultReusable(request.Response, o.answerCacheThreshold(request.Agent)) {
		return
	}
	// 同一次生成前后必须具备相同范围证明；未知和变化均不回填。
	if o.answerCacheNamespace(ctx, request.Incoming, request.Session, request.Agent, request.KBVersion) != request.Namespace {
		return
	}
	o.answerCacheWriteOnce.Do(func() { o.answerCacheWriteSlots = make(chan struct{}, 4) })
	select {
	case o.answerCacheWriteSlots <- struct{}{}:
	default:
		o.answerCacheWriteCounters.dropped.Add(1)
		logger.Ctx(ctx).Warn().Msg("answer cache write capacity reached; optional write skipped")
		return
	}
	version := *request.SourceKBUpdatedAt
	storeRequest := ragcache.StoreRequest{
		KBID: request.KBID, PromptVersion: request.Namespace,
		QueryVector: append([]float32(nil), request.Vector...), Answer: request.Response.Reply,
		FromKnowledgeBase: true, SourceKBUpdatedAt: &version,
	}
	store := o.faqCache
	writeCtx := tenant.BackgroundWithTenant(ctx)
	o.answerCacheWriteCounters.inFlight.Add(1)
	utils.SafeGo(writeCtx, "smartcs.answerCacheWrite", func(parent context.Context) {
		defer func() {
			<-o.answerCacheWriteSlots
			o.answerCacheWriteCounters.inFlight.Add(^uint64(0))
			if recover() != nil {
				o.answerCacheWriteCounters.failed.Add(1)
				logger.Ctx(parent).Warn().Msg("answer cache write panicked; optional write abandoned")
			}
		}()
		bounded, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		if err := store.Store(bounded, storeRequest); err != nil {
			o.answerCacheWriteCounters.failed.Add(1)
			logger.Ctx(parent).Warn().Msg("answer cache write failed; normal reply unchanged")
			return
		}
		o.answerCacheWriteCounters.completed.Add(1)
	})
}
