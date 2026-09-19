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

	// orderDraftProduce 「AI 响应 → 订单意向提取 → 建草稿」的生产者（T-P2-06）。
	// 由 internal/app 的装配层注入；nil = 本进程不产草稿（旗子关着 / 没 DB 句柄）。
	orderDraftProduce func(ctx context.Context, customerID, ownerID string, resp *SalesResponse)

	// humanTaskProduce 「转人工 → 投递一条会话待办」的生产者（T-P3-03）。
	// 同样由 internal/app 注入；nil = 本进程不产待办（没有 DB 句柄时就是这份形态），
	// 此时 transferToHuman 的行为与本卡之前逐字一致。
	humanTaskProduce func(ctx context.Context, session *model.CustomerSession, reason string) error
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

// SetOrderDraftProducer 注入「AI 响应后提取订单意向并建草稿」的生产者（T-P2-06）。
//
// 传 nil 与不调这个效果相同：一条草稿都不会建。之所以做成函数注入而不是让编排器
// 自己去 new 一个 OrderDraftService，是"要不要持久化"这个决策归装配层（读旗子、拿
// DB 句柄），编排器只负责在合适的时机把响应交出去 —— 与 SetDNCChecker 同一分层口径。
func (o *SmartCSOrchestrator) SetOrderDraftProducer(produce func(ctx context.Context, customerID, ownerID string, resp *SalesResponse)) {
	o.orderDraftProduce = produce
}

// SetHumanTaskProducer 注入「转人工 → 投递一条会话待办」的生产者（T-P3-03）。
//
// 传 nil 与不调这个效果相同：一条待办都不会投（= 本卡之前的行为，会话只改状态）。
// 做成函数注入而不是编排器自己去拿全局服务，理由与上面那条一致：有没有 DB 句柄、
// 要不要建底座是装配层的事；这里也顺带成为本卡天然的关闸（没有旗子，不注入即零改动）。
func (o *SmartCSOrchestrator) SetHumanTaskProducer(produce func(ctx context.Context, session *model.CustomerSession, reason string) error) {
	o.humanTaskProduce = produce
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
	// 答案缓存命名空间每次入会话算一次，再透传给 Lookup 与 Store：两处各自现算的话，
	// 中途有人抬灰度比例会把同一会话的读键和写键劈成两个版本（读不到自己刚写的行）。
	// KBAnswerVersionFor 在灰度未开（或该 KB 没 publish 过）时恒返回 faqPromptVersion，
	// 即挂载前的字面量 "v1" ⇒ 这一行本身不改变今天的缓存键。
	faqPromptVer := faqPromptVersion
	if o.faqCache != nil && o.faqEmbedder != nil {
		if kb := o.resolveFAQKB(ctx, finalAgentCtx); kb != nil {
			faqKBID = strconv.FormatUint(uint64(kb.ID), 10)
			faqPromptVer = KBAnswerVersionFor(kb, in.OneID)
			faqVec = o.embedFAQQuery(ctx, in.Content)
		}
	}
	if len(faqVec) > 0 {
		if res, hit := o.lookupFAQAnswerCache(ctx, faqKBID, faqPromptVer, faqVec, result); hit {
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
				PromptVersion:     faqPromptVer,
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

	// 订单意向提取 → 建草稿（T-P2-06 的生产入口）。三点口径：
	//   - 只在 AI 真的给出回复之后跑：转人工/降级链那几条出口上面已经 return 了，
	//     那里没有"谈出来的单"可提取；空回复的判据放在 runOrderDraftProduce 里，
	//     这样这条路径能被单测直接驱动而不必搭完整的引擎 + 会话链；
	//   - 异步 + recover + 独立 ctx：抽取要过正则、建草稿要写库，任何一步慢或炸都不能
	//     把会话响应拖住或带崩（与上面 faqCache.Store 那个 goroutine 同一取舍）；
	//     传进来的 ctx 此刻已随请求返回而取消，所以必须挂新 ctx 而不能沿用；
	//   - produce 为 nil（旗子关/装配未注入）时零动作，本行不改变挂载前的行为。
	if o.orderDraftProduce != nil {
		go o.runOrderDraftProduce(session.UserID, salesResp)
	}

	return result, nil
}

// orderDraftProduceTimeout 给后台提取留的时间窗。
//
// 10s 是按"正则扫描一段回复 + 最多几条 INSERT"给的：远超正常耗时，又能在库卡住时
// 准时把 goroutine 收掉，不至于每次会话失败都留下一个永久挂起的写库协程。
const orderDraftProduceTimeout = 10 * time.Second

// runOrderDraftProduce 在后台跑一次"意向提取 → 建草稿"。
//
// 单独成方法有两个用处：一是 HandleIncomingWithAgent 尾部只留一行、不把 recover 与
// 超时脚手架摊进主流程；二是这条路径可被测试直接驱动（HandleIncomingWithAgent 需要
// 完整的引擎 + 会话链，为了测几个 nil 判断去搭那套不值得）。
func (o *SmartCSOrchestrator) runOrderDraftProduce(customerID string, resp *SalesResponse) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[order-draft] 意向提取 panic 已 recover，不影响会话主链路: %v\n%s",
				r, debug.Stack())
		}
	}()
	produce := o.orderDraftProduce
	// 空回复没有可提取的东西：AI 没说话就建草稿，等于把"库里某产品标价 880"当成客户下了单。
	if produce == nil || resp == nil || strings.TrimSpace(resp.Reply) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), orderDraftProduceTimeout)
	defer cancel()
	produce(ctx, customerID, "", resp)
}

// resolveFAQKB 取本次会话用于答案缓存的知识库行。
//
// 由 resolveFAQKBID 改来：只回 ID 字符串的话，版本/灰度决策就没有输入（要读该行上的
// Version/Canary* 三列），改回整行是这一步的最小形状。选行规则一字未动
// （FAQ 优先、RAG 兜底、按 ListByAgent 原序），T-P2-05 AC③ 的差分对照跑的就是这里。
func (o *SmartCSOrchestrator) resolveFAQKB(ctx context.Context, agentCtx *AgentContext) *model.KnowledgeBase {
	if agentCtx == nil || agentCtx.AgentID == 0 {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
		}
	}()
	if o.kbRepo == nil {
		return nil
	}
	kbs, err := o.kbRepo.ListByAgent(ctx, agentCtx.AgentID)
	if err != nil {
		return nil
	}
	fallbackIdx := -1
	for i := range kbs {
		switch kbs[i].Type {
		case model.KnowledgeBaseTypeFAQ:
			return &kbs[i]
		case model.KnowledgeBaseTypeRAG:
			if fallbackIdx < 0 {
				fallbackIdx = i
			}
		}
	}
	if fallbackIdx < 0 {
		return nil
	}
	return &kbs[fallbackIdx]
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

func (o *SmartCSOrchestrator) lookupFAQAnswerCache(ctx context.Context, kbID, promptVersion string, vec []float32, result *HandleResult) (*HandleResult, bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warnf("[ragcache] lookup panic (kb_id=%s): %v", kbID, r)
		}
	}()
	lr, err := o.faqCache.Lookup(ctx, ragcache.LookupRequest{
		KBID:          kbID,
		PromptVersion: promptVersion,
		QueryVector:   vec,
	})
	if err != nil || lr == nil || lr.Tier == ragcache.TierMiss || strings.TrimSpace(lr.Answer) == "" {
		return nil, false
	}
	logger.Ctx(ctx).Info().
		Str("kb_id", kbID).
		Str("prompt_version", promptVersion).
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

	// 统一待办投递（T-P3-03 AC②）。三点口径：
	//   - 放在会话状态落库**之后**：待办的意义是"池子里有一行等着人去处理这条会话"，
	//     会话还没转过去就先投，坐席会领到一条 status 仍是 ai_handling 的会话；
	//   - 失败只告警不上抛：会话此刻已经是 waiting 了，回滚不了，而上抛会让调用方把
	//     整条消息处理判失败（客户那边其实已经转过去了）。漏投由这条 Error 日志兜底，
	//     它是本卡唯一一处"待办可能缺"的已知缺口；
	//   - 幂等交给服务：同一次转人工被低置信度与情绪策略两个出口各叫一次、以及上一轮
	//     还没处理完就再转，都只会拿到同一条开放待办（用例逐条验过）。
	if err := o.produceHandoffTask(ctx, session, reason); err != nil {
		logger.Ctx(ctx).Error().Err(err).
			Str("session_id", session.SessionID).
			Msg("[transferToHuman] 投递人工待办失败（会话已转人工，但池子里没有这一行，需人工补投）")
	}
	return nil
}

// humanTaskProduceTimeout 给"转人工 → 写一条待办"留的时间窗。
//
// 一次读（查同 subject 的开放待办）+ 最多两次写（INSERT，或撞索引后回读），
// 5s 远超正常耗时；给上界是因为这段跑在请求 ctx 的续命上，库卡住时不能把请求吊死。
const humanTaskProduceTimeout = 5 * time.Second

// produceHandoffTask 投递一次会话待办；未注入生产者时零动作。
//
// ctx 用 WithoutCancel + 新上界，而不是直接沿用请求 ctx（与订单草稿那条**刻意不同**）：
// 草稿丢了可以重建、而且是后台附带产物，所以它异步跑；待办不行 —— 会话此刻已经被判成
// "等人工"，客户端断开、响应已返回都不改变"有人在等"这件事。若沿用请求 ctx，
// 客户端一断就把这条 INSERT 取消掉，结果正是本卡要消灭的那个形态：
// 会话 waiting、池子里空无一物、谁也不知道。
func (o *SmartCSOrchestrator) produceHandoffTask(ctx context.Context, session *model.CustomerSession, reason string) error {
	if o.humanTaskProduce == nil {
		return nil
	}
	if session == nil {
		// 在这里先拒而不是让服务报"subject_id 为空"：那句错误在日志里指不回
		// "哪一次转人工"，而调用链上只有这一处会传 nil 会话。
		return fmt.Errorf("%w: transferToHuman 拿到了空会话，无法定位待办主体", ErrHumanTaskInputInvalid)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), humanTaskProduceTimeout)
	defer cancel()
	return o.humanTaskProduce(ctx, session, reason)
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
