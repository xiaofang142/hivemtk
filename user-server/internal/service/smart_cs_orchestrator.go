package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	ragcache "hivemtk-user/internal/aiagent/rag/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	confidencesvc "hivemtk-user/internal/service/confidence"
)

const faqPromptVersion = "v1"

var (
	globalFAQCache    *ragcache.FAQAnswerCacheService
	globalFAQEmbedder llm.EmbeddingServiceInterface
)

func SetGlobalFAQAnswerCache(svc *ragcache.FAQAnswerCacheService, embedder llm.EmbeddingServiceInterface) {
	globalFAQCache = svc
	globalFAQEmbedder = embedder
}

// SmartCSOrchestrator 智能体编排器
type SmartCSOrchestrator struct {
	engine         *SalesEngine
	sessionSvc     *CustomerSessionService
	assignmentSvc  *SessionAssignmentService
	suggestionRepo *repository.AISuggestionRepository
	sessionRepo    *repository.CustomerSessionRepository
	messageRepo    *repository.SessionMessageRepository
	agentRepo      *repository.AgentStatusRepository
	kbRepo         *repository.KnowledgeBaseRepository

	csAgentSvc  *CustomerServiceAgentService
	identitySvc *CustomerIdentityService

	confidenceThreshold float64
	enableAutoReply     bool
	maxAIConsecutive    int

	confidenceAgg *confidencesvc.ConfidenceAggregator

	faqCache    *ragcache.FAQAnswerCacheService
	faqEmbedder llm.EmbeddingServiceInterface

	dncChecker DoNotContactChecker
}

// OrchestratorConfig 编排器配置
type OrchestratorConfig struct {
	ConfidenceThreshold float64
	EnableAutoReply     bool
	MaxAIConsecutive    int
}

// DefaultOrchestratorConfig 默认配置（DB 驱动读取）
// seed: smart_cs.confidence_threshold, smart_cs.max_ai_consecutive
func DefaultOrchestratorConfig() *OrchestratorConfig {
	cp := GlobalConfigParam()
	return &OrchestratorConfig{
		ConfidenceThreshold: cp.GetFloat(context.Background(), "smart_cs", "confidence_threshold", 0.7),
		EnableAutoReply:     cp.GetBool(context.Background(), "smart_cs", "enable_auto_reply", true),
		MaxAIConsecutive:    cp.GetInt(context.Background(), "smart_cs", "max_ai_consecutive", 10),
	}
}

// SetConfidenceAggregator 注入五信号置信度聚合器（D01；factory 层从 engine 取同实例）
func (o *SmartCSOrchestrator) SetConfidenceAggregator(agg *confidencesvc.ConfidenceAggregator) {
	o.confidenceAgg = agg
}

// NewSmartCSOrchestrator 创建智能体编排器
func NewSmartCSOrchestrator(engine *SalesEngine, cfg *OrchestratorConfig, kbRepo *repository.KnowledgeBaseRepository) *SmartCSOrchestrator {
	if cfg == nil {
		cfg = DefaultOrchestratorConfig()
	}
	sessionSvc := NewCustomerSessionService()
	assignmentSvc := NewSessionAssignmentService()
	assignmentSvc.SetConfidenceThreshold(context.Background(), cfg.ConfidenceThreshold)
	return &SmartCSOrchestrator{
		engine:              engine,
		sessionSvc:          sessionSvc,
		assignmentSvc:       assignmentSvc,
		suggestionRepo:      repository.NewAISuggestionRepository(),
		sessionRepo:         repository.NewCustomerSessionRepository(),
		messageRepo:         repository.NewSessionMessageRepository(),
		agentRepo:           repository.NewAgentStatusRepository(),
		kbRepo:              kbRepo,
		confidenceThreshold: cfg.ConfidenceThreshold,
		enableAutoReply:     cfg.EnableAutoReply,
		maxAIConsecutive:    cfg.MaxAIConsecutive,
		faqCache:            globalFAQCache,
		faqEmbedder:         globalFAQEmbedder,
	}
}

// SetCustomerServiceAgentService 注入客服座席智能体挂载服务
// 注入后 HandleIncomingWithAgent 会按座席挂载的智能体覆盖渠道默认智能体
// 优先级：座席挂载 > 渠道绑定 > 默认配置
func (o *SmartCSOrchestrator) SetCustomerServiceAgentService(ctx context.Context, svc *CustomerServiceAgentService) {
	o.csAgentSvc = svc
}

// SetIdentityService 注入 CustomerIdentityService。
// 注入后 findOrCreateSession 在创建新会话时会自动 IdentifyOrCreate
// 补建 customer 档案（按平台 open_id 查找/创建），确保 session ↔ customer
// 关联不断裂；nil 时零影响（向后兼容）。
func (o *SmartCSOrchestrator) SetIdentityService(svc *CustomerIdentityService) {
	o.identitySvc = svc
}

func (o *SmartCSOrchestrator) SetDNCChecker(checker DoNotContactChecker) {
	o.dncChecker = checker
}

func (o *SmartCSOrchestrator) ensureCustomerForSession(ctx context.Context, platform model.Platform, senderID, userName string) {
	if o.identitySvc == nil || senderID == "" {
		return
	}
	var identifiers identity.Identifiers
	switch platform {
	case model.PlatformWeChat:
		identifiers.WechatOpenID = senderID
	case model.PlatformDouyin:
		identifiers.DouyinOpenID = senderID
	case model.PlatformXiaohongshu:
		identifiers.XiaohongshuID = senderID
	}
	if !HasAnyIdentifier(identifiers) {
		return
	}
	if _, err := o.identitySvc.IdentifyOrCreate(ctx, identifiers); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("platform", string(platform)).
			Str("sender", senderID).Msg("[Orchestrator] IdentifyOrCreate best-effort 失败，不阻断 session")
	}
}

// Mode 返回本编排器作为智能体生命周期的工作模式：被动（passive）。
// SmartCSOrchestrator 即 agent/lifecycle 体系下的「被动模式」实现——
// 消息/事件进入系统后由它调用智能体完成对话并返回（对话域主路径）。
// 主动模式（active）由后续主动触达引擎落地（详见）。
func (o *SmartCSOrchestrator) Mode(ctx context.Context) string { return string(model.AgentModePassive) }

// IncomingContext 入站消息上下文
type IncomingContext struct {
	Platform   model.Platform
	AccountID  string
	SenderID   string
	SenderName string
	Content    string
	MessageID  string
	MediaURL   string
	OneID      string
	IsGroup    bool
	GroupID    string
	GroupName  string
}

// HandleResult 处理结果
type HandleResult struct {
	SessionID      string            `json:"session_id"`
	HandlerType    model.HandlerType `json:"handler_type"`
	AIReplied      bool              `json:"ai_replied"`
	Reply          string            `json:"reply,omitempty"`
	Confidence     float64           `json:"confidence"`
	Transferred    bool              `json:"transferred"`
	TransferReason string            `json:"transfer_reason,omitempty"`
	Cards          []model.RichCard  `json:"cards,omitempty"`
	SuggestionID   uint              `json:"suggestion_id,omitempty"`
	SalesResponse  *SalesResponse    `json:"sales_response,omitempty"`
}

// HandleIncoming 处理入站消息（智能体主入口，默认配置）
// 调用方：WebhookController 收到渠道消息后调用
// 等价于 HandleIncomingWithAgent(ctx, in, nil)
func (o *SmartCSOrchestrator) HandleIncoming(ctx context.Context, in *IncomingContext) (*HandleResult, error) {
	return o.HandleIncomingWithAgent(ctx, in, nil)
}

// HandleIncomingWithAgent 处理入站消息（按指定智能体编排）
// 多 AI 智能体路由核心入口：
//   - agentCtxFromChannel：渠道账号绑定的智能体上下文（由 WebhookService.loadAgentForChannel 加载）
//   - 若会话已分配座席，按座席挂载的智能体覆盖（座席挂载 > 渠道绑定 > 默认）
//   - agentCtx == nil 时回退到默认配置（engine.HandleWithAgent 内部回退到 Handle）
func (o *SmartCSOrchestrator) HandleIncomingWithAgent(ctx context.Context, in *IncomingContext, agentCtxFromChannel *AgentContext) (*HandleResult, error) {
	if o == nil || in == nil {
		return nil, errors.New("orchestrator or incoming context is nil")
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, errors.New("content is empty")
	}

	ctx = logger.WithModule(ctx, "orchestrator")
	start := time.Now()
	result := &HandleResult{HandlerType: model.HandlerTypeAI}
	logger.Ctx(ctx).Info().
		Str("platform", string(in.Platform)).
		Str("account_id", in.AccountID).
		Str("sender_id", in.SenderID).
		Str("message_id", in.MessageID).
		Int("content_len", len(in.Content)).
		Msg("[1] orchestrator start")
	defer func() {
		logger.Ctx(ctx).Info().
			Dur("cost", time.Since(start)).
			Str("handler", string(result.HandlerType)).
			Bool("transferred", result.Transferred).
			Bool("ai_replied", result.AIReplied).
			Msg("[9] orchestrator done")
	}()

	session, err := o.findOrCreateSession(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("find/create session failed: %w", err)
	}
	result.SessionID = session.SessionID

	o.ensureCustomerForSession(ctx, in.Platform, in.SenderID, in.SenderName)

	if err := o.saveInboundMessage(ctx, session, in); err != nil {
		return nil, fmt.Errorf("save inbound message failed: %w", err)
	}

	if err := o.sessionRepo.UpdateLastMessage(ctx, session.ID, in.Content, "user"); err != nil {
		logger.Ctx(ctx).Warn().Err(err).
			Str("session_id", session.SessionID).
			Msg("[Orchestrator] UpdateLastMessage(user) failed — message_count 可能不准")
	}

	if session.HandlerType == model.HandlerTypeHuman && session.AgentID > 0 {
		if o.isAgentOnline(ctx, session.AgentID) {
			result.HandlerType = model.HandlerTypeHuman
			result.Transferred = true
			result.TransferReason = "会话已分配给在线座席"
			return result, nil
		}
	}

	if o.maxAIConsecutive > 0 && session.AIReplyCount >= o.maxAIConsecutive {
		result.HandlerType = model.HandlerTypeHuman
		result.Transferred = true
		result.TransferReason = fmt.Sprintf("AI 连续回复已达上限 (%d 次)，转人工跟进", o.maxAIConsecutive)
		utils.WarnErrKV("smartcs.transferToHuman.upperLimit", o.transferToHuman(ctx, session, result.TransferReason), "session_id", session.SessionID, "agent_id", strconv.FormatUint(uint64(session.AgentID), 10))
		return result, nil
	}

	emotionHint := ""
	if o.isUrgentOrComplaint(ctx, in.Content) {
		emoStrat := StrategyForEmotion(ClassifyEmotion(in.Content))
		if emoStrat.TransferToHuman {
			result.HandlerType = model.HandlerTypeHuman
			result.Transferred = true
			result.TransferReason = emoStrat.TransferReason
			utils.WarnErrKV("smartcs.transferToHuman.emotion", o.transferToHuman(ctx, session, result.TransferReason), "session_id", session.SessionID, "reason", emoStrat.TransferReason)
			return result, nil
		}
		emotionHint = emoStrat.ReplyHint
	}

	if o.engine == nil {
		result.HandlerType = model.HandlerTypeHuman
		result.Transferred = true
		result.TransferReason = "AI 引擎未就绪，转人工"
		_ = o.transferToHuman(ctx, session, result.TransferReason)
		return result, nil
	}

	finalAgentCtx := agentCtxFromChannel
	if session.AgentID > 0 && o.csAgentSvc != nil {
		seatAgentCtx, err := o.csAgentSvc.LoadAgentForSeat(ctx, session.AgentID)
		if err != nil {
			logger.Ctx(ctx).Warn().
				Err(err).
				Uint("agent_id", session.AgentID).
				Msg("[6.1] load seat agent failed, fallback to channel binding")
		} else if seatAgentCtx != nil {
			finalAgentCtx = seatAgentCtx
		}
	}

	faqKBID, faqVec := "", []float32(nil)
	if o.faqCache != nil && o.faqEmbedder != nil {
		if faqKBID = o.resolveFAQKBID(ctx, finalAgentCtx); faqKBID != "" {
			faqVec = o.embedFAQQuery(ctx, in.Content)
		}
	}
	if len(faqVec) > 0 {
		if res, hit := o.lookupFAQAnswerCache(ctx, faqKBID, faqVec, result); hit {
			return res, nil
		}
	}

	salesReq := &SalesRequest{
		SessionID:   session.SessionID,
		CustomerID:  session.UserID,
		OneID:       in.OneID,
		UserMessage: in.Content,
		Platform:    string(in.Platform),
		AutoExecute: o.enableAutoReply,
		EmotionHint: emotionHint,
	}
	salesResp, err := o.engine.HandleWithAgent(ctx, salesReq, finalAgentCtx)
	if err != nil || salesResp == nil {

		logger.Ctx(ctx).Warn().Err(err).Str("session_id", session.SessionID).
			Msg("[Orchestrator] HandleWithAgent 失败，进入降级链")

		if err != nil {
			logger.Ctx(ctx).Info().Str("session_id", session.SessionID).
				Msg("[Orchestrator] 降级链 Level 1: 备用 Engine.Handle() 重试")
			salesResp, err = o.engine.Handle(ctx, salesReq)
		}
		if err == nil && salesResp != nil && salesResp.Reply != "" {
			logger.Ctx(ctx).Info().Str("session_id", session.SessionID).
				Msg("[Orchestrator] 降级链 Level 1 成功: 备用 Engine.Handle() 返回有效回复")
		} else {

			logger.Ctx(ctx).Info().Str("session_id", session.SessionID).
				Msg("[Orchestrator] 降级链 Level 2: RuleEngine 规则引擎触发")
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Ctx(ctx).Warn().Interface("panic", r).
							Msg("[Orchestrator] RuleEngine 触发 panic，已 recover 不影响主链路")
					}
				}()
				NewRuleEngineServiceFromGlobal().DispatchWithText(
					ctx, "ai_fallback", session.SessionID, in.Content, session,
				)
			}()

			logger.Ctx(ctx).Info().Str("session_id", session.SessionID).
				Msg("[Orchestrator] 降级链 Level 3: 最终兜底 → 转人工")
			result.HandlerType = model.HandlerTypeHuman
			result.Transferred = true
			result.TransferReason = "AI 引擎处理失败，降级链 Level 3 兜底转人工"
			_ = o.transferToHuman(ctx, session, result.TransferReason)
			return result, nil
		}
	}
	result.SalesResponse = salesResp
	result.Confidence = o.extractConfidence(ctx, salesResp, session.SessionID, in.Content)
	result.Cards = RichCardsFromDTO(salesResp.Cards)

	suggestionID := o.saveAISuggestion(ctx, session.SessionID, salesResp, in.Content)
	result.SuggestionID = suggestionID

	threshold := o.confidenceThreshold
	if finalAgentCtx != nil && finalAgentCtx.ConfidenceThreshold > 0 {
		threshold = finalAgentCtx.ConfidenceThreshold
	}
	effectiveConf := result.Confidence
	if len(salesResp.Cards) > 0 && effectiveConf < threshold {
		effectiveConf = threshold
	}
	result.Confidence = effectiveConf
	knownIntent := salesResp.Intent != nil && salesResp.Intent.IntentType != IntentUnknown
	safeIntent := salesResp.Intent != nil &&
		(salesResp.Intent.IntentType == IntentGreeting || salesResp.Intent.IntentType == IntentSocial)
	shouldTransfer := salesResp.TransferredToHuman ||
		(knownIntent && !safeIntent && effectiveConf < threshold)
	if shouldTransfer {
		result.HandlerType = model.HandlerTypeHuman
		result.Transferred = true
		if salesResp.TransferReason != "" {
			result.TransferReason = salesResp.TransferReason
		} else {
			result.TransferReason = fmt.Sprintf("AI 置信度不足 (%.2f < %.2f)", result.Confidence, threshold)
		}
		utils.WarnErrKV("smartcs.transferToHuman.lowConfidence", o.transferToHuman(ctx, session, result.TransferReason), "session_id", session.SessionID, "confidence", strconv.FormatFloat(result.Confidence, 'f', 4, 64), "threshold", strconv.FormatFloat(threshold, 'f', 4, 64))
		return result, nil
	}

	result.HandlerType = model.HandlerTypeAI
	result.AIReplied = true
	result.Reply = salesResp.Reply

	// FAQ 答案缓存写入守卫：只有当 top1 RAG 召回分数达到置信度阈值时才缓存。
	// 无门槛写入会让低分召回（甚至幻觉拼接）的回复长期留在缓存里，
	// 后续相似问题直接命中并绕过置信度→转人工判断（缓存投毒）。
	faqStoreAllowed := false
	if faqKBID != "" && len(faqVec) > 0 && len(salesResp.RAGChunks) > 0 && salesResp.Reply != "" {
		top1Score := salesResp.RAGChunks[0].Score
		faqStoreAllowed = top1Score >= o.confidenceThreshold
		if !faqStoreAllowed {
			logger.Infof("[ragcache] skip store answer: top1 rag score %.3f < threshold %.3f (kb_id=%s)",
				top1Score, o.confidenceThreshold, faqKBID)
		}
	}
	if faqStoreAllowed {
		go func(answer string, vec []float32) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			if err := o.faqCache.Store(context.Background(), ragcache.StoreRequest{
				KBID:              faqKBID,
				PromptVersion:     faqPromptVersion,
				QueryVector:       vec,
				Answer:            answer,
				FromKnowledgeBase: true,
			}); err != nil {
				logger.Warnf("[ragcache] store answer failed (kb_id=%s): %v", faqKBID, err)
			}
		}(salesResp.Reply, faqVec)
	}

	if o.enableAutoReply && salesResp.Reply != "" {
		if err := o.saveOutboundMessage(ctx, session, salesResp.Reply, true); err != nil {
			return nil, fmt.Errorf("save outbound message failed: %w", err)
		}
		utils.WarnErrKV("smartcs.markSuggestionUsed", o.markSuggestionUsed(ctx, suggestionID), "session_id", session.SessionID, "suggestion_id", strconv.FormatUint(uint64(suggestionID), 10))
		utils.WarnErrKV("smartcs.incrementAIReplyCount", o.incrementAIReplyCount(ctx, session), "session_id", session.SessionID, "ai_reply_count", strconv.Itoa(session.AIReplyCount+1))

		if err := o.sessionRepo.UpdateLastMessage(ctx, session.ID, salesResp.Reply, "ai"); err != nil {
			logger.Ctx(ctx).Warn().Err(err).
				Str("session_id", session.SessionID).
				Msg("[Orchestrator] UpdateLastMessage(ai) failed — message_count 可能不准")
		}
	}

	return result, nil
}

func (o *SmartCSOrchestrator) resolveFAQKBID(ctx context.Context, agentCtx *AgentContext) string {
	if agentCtx == nil || agentCtx.AgentID == 0 {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()
	if o.kbRepo == nil {
		return ""
	}
	kbs, err := o.kbRepo.ListByAgent(ctx, agentCtx.AgentID)
	if err != nil {
		return ""
	}
	fallback := ""
	for _, kb := range kbs {
		switch kb.Type {
		case model.KnowledgeBaseTypeFAQ:
			return strconv.FormatUint(uint64(kb.ID), 10)
		case model.KnowledgeBaseTypeRAG:
			if fallback == "" {
				fallback = strconv.FormatUint(uint64(kb.ID), 10)
			}
		}
	}
	return fallback
}

func (o *SmartCSOrchestrator) embedFAQQuery(ctx context.Context, text string) []float32 {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()
	vec, err := o.faqEmbedder.EmbedOne(ctx, o.faqEmbedder.DefaultConfig(), text)
	if err != nil || len(vec) == 0 {
		return nil
	}
	return vec
}

func (o *SmartCSOrchestrator) lookupFAQAnswerCache(ctx context.Context, kbID string, vec []float32, result *HandleResult) (*HandleResult, bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[ragcache] lookup panic (kb_id=%s): %v", kbID, r)
		}
	}()
	lr, err := o.faqCache.Lookup(ctx, ragcache.LookupRequest{
		KBID:          kbID,
		PromptVersion: faqPromptVersion,
		QueryVector:   vec,
	})
	if err != nil || lr == nil || lr.Tier == ragcache.TierMiss || strings.TrimSpace(lr.Answer) == "" {
		return nil, false
	}
	logger.Ctx(ctx).Info().
		Str("kb_id", kbID).
		Str("tier", string(lr.Tier)).
		Float64("similarity", lr.Similarity).
		Msg("[ragcache] FAQ answer cache HIT, skip LLM generation")
	result.HandlerType = model.HandlerTypeAI
	result.AIReplied = true
	result.Reply = lr.Answer
	// 置信度用实际召回相似度而非恒 1.0：恒 1.0 会绕过置信度阈值→转人工的
	// 下游判断，缓存命中变成"免检通道"。相似度低于阈值时仍走正常降级。
	if lr.Similarity < o.confidenceThreshold {
		logger.Ctx(ctx).Info().
			Str("kb_id", kbID).
			Float64("similarity", lr.Similarity).
			Float64("threshold", o.confidenceThreshold).
			Msg("[ragcache] similarity below confidence threshold, skip cache hit")
		return nil, false
	}
	result.Confidence = lr.Similarity
	if o.enableAutoReply {
		if session := o.sessionOfResult(result); session != nil {
			utils.WarnErrKV("smartcs.saveOutboundMessage.hit", o.saveOutboundMessage(ctx, session, lr.Answer, true), "session_id", session.SessionID, "source", "ragcache")
			utils.WarnErrKV("smartcs.incrementAIReplyCount.hit", o.incrementAIReplyCount(ctx, session), "session_id", session.SessionID, "source", "ragcache")

			utils.WarnErrKV("smartcs.UpdateLastMessage.hit", o.sessionRepo.UpdateLastMessage(ctx, session.ID, lr.Answer, "ai"), "session_id", session.SessionID, "source", "ragcache")
		}
	}
	return result, true
}

func (o *SmartCSOrchestrator) sessionOfResult(result *HandleResult) *model.CustomerSession {
	if result == nil || result.SessionID == "" {
		return nil
	}
	session, err := o.sessionRepo.GetByIDString(context.Background(), result.SessionID)
	if err != nil || session == nil {
		return nil
	}
	return session
}

func (o *SmartCSOrchestrator) findOrCreateSession(ctx context.Context, in *IncomingContext) (*model.CustomerSession, error) {
	if in.IsGroup {
		groupKey := in.GroupID
		if groupKey == "" {
			groupKey = in.SenderID
		}
		derivedOneID := "group:" + groupKey
		now := time.Now()
		dncBlocked := o.checkDNCBlocked(ctx, derivedOneID)
		stableSessionID := fmt.Sprintf("sess_%s_%s_%s", string(in.Platform), in.AccountID, derivedOneID)
		if id, err := o.sessionRepo.UpsertByOneID(ctx, string(in.Platform), in.AccountID, derivedOneID, derivedOneID, in.GroupName, in.Content, &now, dncBlocked); err != nil {
			staleID := fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), safeMessageID(in.MessageID))
			logger.Ctx(ctx).Warn().Err(err).Str("stable_id", stableSessionID).Str("stale_id", staleID).
				Msg("[Orchestrator] 群聊 UpsertByOneID 失败，降级用 stale session_id")
			session := &model.CustomerSession{
				SessionID:     staleID,
				Platform:      in.Platform,
				AccountID:     in.AccountID,
				UserID:        derivedOneID,
				OneID:         derivedOneID,
				UserName:      in.GroupName,
				Status:        model.SessionStatusPending,
				Priority:      0,
				LastMessage:   in.Content,
				LastMessageAt: &now,
				LastMessageBy: "user",
				HandlerType:   model.HandlerTypeAI,
				DNCBlocked:    dncBlocked,
			}
			if err := o.sessionRepo.Create(ctx, session); err != nil {
				return nil, err
			}
			return session, nil
		} else if id != "" {
			return o.sessionRepo.GetByIDString(ctx, id)
		}
		staleID := fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), safeMessageID(in.MessageID))
		session := &model.CustomerSession{
			SessionID:     staleID,
			Platform:      in.Platform,
			AccountID:     in.AccountID,
			UserID:        derivedOneID,
			OneID:         derivedOneID,
			UserName:      in.GroupName,
			Status:        model.SessionStatusPending,
			Priority:      0,
			LastMessage:   in.Content,
			LastMessageAt: &now,
			LastMessageBy: "user",
			HandlerType:   model.HandlerTypeAI,
			DNCBlocked:    dncBlocked,
		}
		if err := o.sessionRepo.Create(ctx, session); err != nil {
			return nil, err
		}
		return session, nil
	}

	if in.OneID != "" {
		if existing, err := o.sessionRepo.GetActiveByOneID(ctx, in.OneID); err == nil && existing != nil {
			return existing, nil
		}
	}

	if existing, err := o.sessionRepo.GetActiveByUserID(ctx, in.SenderID); err == nil && existing != nil {
		return existing, nil
	}

	derivedOneID := in.OneID
	if derivedOneID == "" {
		derivedOneID = fmt.Sprintf("%s:%s", in.Platform, in.SenderID)
	}
	dncBlocked := o.checkDNCBlocked(ctx, derivedOneID)
	now := time.Now()
	stableSessionID := fmt.Sprintf("sess_%s_%s_%s", string(in.Platform), in.AccountID, derivedOneID)
	if id, err := o.sessionRepo.UpsertByOneID(ctx, string(in.Platform), in.AccountID, derivedOneID, in.SenderID, in.SenderName, in.Content, &now, dncBlocked); err != nil {
		staleID := fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), safeMessageID(in.MessageID))
		logger.Ctx(ctx).Warn().Err(err).Str("stable_id", stableSessionID).Str("stale_id", staleID).Msg("[Orchestrator] UpsertByOneID 失败，降级用 stale session_id")
		session := &model.CustomerSession{
			SessionID:     staleID,
			Platform:      in.Platform,
			AccountID:     in.AccountID,
			UserID:        in.SenderID,
			OneID:         derivedOneID,
			UserName:      in.SenderName,
			Status:        model.SessionStatusPending,
			Priority:      0,
			LastMessage:   in.Content,
			LastMessageAt: &now,
			LastMessageBy: "user",
			HandlerType:   model.HandlerTypeAI,
			DNCBlocked:    dncBlocked,
		}
		if err := o.sessionRepo.Create(ctx, session); err != nil {
			return nil, err
		}
		return session, nil
	} else if id != "" {
		return o.sessionRepo.GetByIDString(ctx, id)
	}
	staleID := fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), safeMessageID(in.MessageID))
	session := &model.CustomerSession{
		SessionID:     staleID,
		Platform:      in.Platform,
		AccountID:     in.AccountID,
		UserID:        in.SenderID,
		OneID:         derivedOneID,
		UserName:      in.SenderName,
		Status:        model.SessionStatusPending,
		Priority:      0,
		LastMessage:   in.Content,
		LastMessageAt: &now,
		LastMessageBy: "user",
		HandlerType:   model.HandlerTypeAI,
		DNCBlocked:    dncBlocked,
	}
	if err := o.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (o *SmartCSOrchestrator) checkDNCBlocked(ctx context.Context, oneID string) bool {
	if o.dncChecker == nil || oneID == "" {
		return false
	}
	if o.dncChecker.IsBlocked(ctx, oneID, "") {
		logger.Ctx(ctx).Warn().
			Str("one_id", oneID).
			Msg("[CS-P0-1] Orchestrator 创建会话时命中全局退订，标记 DNCBlocked=true 但允许创建")
		return true
	}
	return false
}

func (o *SmartCSOrchestrator) saveInboundMessage(ctx context.Context, session *model.CustomerSession, in *IncomingContext) error {
	if existing, _ := o.messageRepo.FindRecentDuplicate(ctx, session.SessionID, "user", in.SenderID, in.Content, 5*time.Second); existing != nil {
		return nil
	}
	msg := &model.SessionMessage{
		SessionID:   session.SessionID,
		Content:     in.Content,
		ContentType: model.MessageTypeText,
		MediaURL:    in.MediaURL,
		SenderType:  "user",
		SenderID:    in.SenderID,
		SenderName:  in.SenderName,
	}
	return o.messageRepo.Create(ctx, msg)
}

func (o *SmartCSOrchestrator) saveOutboundMessage(ctx context.Context, session *model.CustomerSession, content string, aiGenerated bool) error {
	senderType := "agent"
	if aiGenerated {
		senderType = "ai"
	}
	if existing, _ := o.messageRepo.FindRecentDuplicate(ctx, session.SessionID, senderType, "ai_assistant", content, 5*time.Second); existing != nil {
		return nil
	}
	msg := &model.SessionMessage{
		SessionID:   session.SessionID,
		Content:     content,
		ContentType: model.MessageTypeText,
		SenderType:  senderType,
		SenderID:    "ai_assistant",
		SenderName:  "AI 助手",
	}
	return o.messageRepo.Create(ctx, msg)
}

func (o *SmartCSOrchestrator) saveAISuggestion(ctx context.Context, sessionID string, resp *SalesResponse, userText string) uint {
	if o.suggestionRepo == nil || resp == nil || resp.Reply == "" {
		return 0
	}
	confidence := o.extractConfidence(ctx, resp, sessionID, userText)
	suggestion := &model.AISuggestion{
		SessionID:  sessionID,
		Suggestion: resp.Reply,
		Confidence: confidence,
		Source:     "sales_engine",
	}
	if err := o.suggestionRepo.Create(ctx, suggestion); err != nil {
		return 0
	}
	return suggestion.ID
}

func (o *SmartCSOrchestrator) markSuggestionUsed(ctx context.Context, id uint) error {
	if id == 0 || o.suggestionRepo == nil {
		return nil
	}
	return o.suggestionRepo.MarkAsUsed(ctx, id, 0)
}

func (o *SmartCSOrchestrator) transferToHuman(ctx context.Context, session *model.CustomerSession, reason string) error {
	session.Status = model.SessionStatusWaiting
	session.HandlerType = model.HandlerTypeHuman
	session.LastMessage = reason

	if session.HandoffAt == nil {
		now := time.Now()
		session.HandoffAt = &now
		session.HandoffReason = reason
	}
	now := time.Now()
	session.LastMessageAt = &now
	session.AIReplyCount = 0
	if err := o.sessionRepo.Update(ctx, session); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[transferToHuman] update session status failed")
		return err
	}

	sysMsg := &model.SessionMessage{
		SessionID:   session.SessionID,
		Content:     "【系统】" + reason,
		ContentType: model.MessageTypeText,
		SenderType:  "system",
		SenderID:    "system",
		SenderName:  "系统",
	}
	if err := o.messageRepo.Create(ctx, sysMsg); err != nil {
		logger.Ctx(ctx).Error().Err(err).Msg("[transferToHuman] create system message failed")
	}

	if o.assignmentSvc != nil {
		if err := o.assignmentSvc.autoAssignToAgent(ctx, session, reason); err != nil {
			logger.Ctx(ctx).Error().Err(err).Msg("[transferToHuman] autoAssignToAgent failed")
		}
	}
	return nil
}

func (o *SmartCSOrchestrator) incrementAIReplyCount(ctx context.Context, session *model.CustomerSession) error {
	now := time.Now()
	if err := o.sessionRepo.IncrementAIReplyCount(ctx, session.ID); err != nil {
		return fmt.Errorf("increment ai_reply_count: %w", err)
	}
	if err := o.sessionRepo.UpdateFields(ctx, session.ID, map[string]any{
		"status":          model.SessionStatusAIHandling,
		"last_message_at": &now,
	}); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("id", session.ID).Msg("[Orchestrator] UpdateFields after AI increment failed (non-fatal)")
	}

	session.AIReplyCount++
	session.Status = model.SessionStatusAIHandling
	session.LastMessageAt = &now
	return nil
}

func (o *SmartCSOrchestrator) isAgentOnline(ctx context.Context, agentID uint) (online bool) {
	defer func() {
		if r := recover(); r != nil {
			online = false
		}
	}()
	if o.agentRepo == nil {
		return false
	}
	agent, err := o.agentRepo.GetByAgentID(ctx, agentID)
	if err != nil || agent == nil {
		return false
	}
	return agent.Status == "online" || agent.Status == "busy"
}

func (o *SmartCSOrchestrator) isUrgentOrComplaint(ctx context.Context, content string) bool {
	return MatchUrgentKeywords(content)
}

func (o *SmartCSOrchestrator) extractConfidence(ctx context.Context, resp *SalesResponse, sessionID, userText string) float64 {
	if resp == nil {
		return 0
	}
	if o.confidenceAgg != nil {
		in := &dto.SignalCollectionInput{
			SessionID:   sessionID,
			Text:        userText,
			RAGExecuted: len(resp.RAGChunks) > 0,
			RAGChunks:   resp.RAGChunks,
		}
		if resp.Intent != nil {
			in.IntentType = resp.Intent.IntentType
			in.RawIntentConf = resp.Intent.Confidence
		}
		if dec, err := o.confidenceAgg.Aggregate(ctx, in); err == nil && dec != nil {
			return dec.AggregatedConf
		} else if err != nil {
			logger.Ctx(ctx).Warn().Err(err).Msg("[Orchestrator] confidence aggregate failed, fallback to heuristic")
		}
	}
	return o.fallbackConfidence(resp)
}

func (o *SmartCSOrchestrator) fallbackConfidence(resp *SalesResponse) float64 {
	if resp.Intent != nil && resp.Intent.Confidence > 0 {
		return resp.Intent.Confidence
	}
	score := 0.5
	if resp.Reply != "" {
		score += 0.1
	}
	if resp.Polished {
		score += 0.05
	}
	if resp.Audited && len(resp.AuditIssues) == 0 {
		score += 0.1
	}
	if len(resp.RAGChunks) > 0 {
		score += 0.05
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func safeMessageID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	if id == "" {
		return "nomsgid"
	}
	return id
}

// AgentTakeover 座席接管 AI 会话
// 当座席认为 AI 回复不合适时，可主动接管会话
func (o *SmartCSOrchestrator) AgentTakeover(ctx context.Context, sessionID string, agentID uint) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("session not found (repo panic): %s, %v", sessionID, r)
		}
	}()
	session, err := o.sessionRepo.GetBySessionID(ctx, sessionID)
	if err != nil || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	session.HandlerType = model.HandlerTypeHuman
	session.AgentID = agentID
	session.Status = model.SessionStatusHumanHandling
	now := time.Now()
	session.LastMessageAt = &now
	return o.sessionRepo.Update(ctx, session)
}

// AgentReply 座席手动回复（覆盖 AI 建议）
func (o *SmartCSOrchestrator) AgentReply(ctx context.Context, sessionID string, agentID uint, content string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("session not found (repo panic): %s, %v", sessionID, r)
		}
	}()
	session, err := o.sessionRepo.GetBySessionID(ctx, sessionID)
	if err != nil || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	msg := &model.SessionMessage{
		SessionID:   sessionID,
		Content:     content,
		ContentType: model.MessageTypeText,
		SenderType:  "agent",
		SenderID:    fmt.Sprintf("%d", agentID),
	}
	if err := o.messageRepo.Create(ctx, msg); err != nil {
		return err
	}
	session.HumanReplyCount++
	session.LastMessage = content
	now := time.Now()
	session.LastMessageAt = &now
	session.LastMessageBy = "agent"

	if session.HandoffAt != nil && session.FirstHumanReplyAt == nil {
		session.FirstHumanReplyAt = &now
	}
	return o.sessionRepo.Update(ctx, session)
}
