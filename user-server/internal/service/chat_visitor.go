package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/security"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"
)

// VisitorChatService 访客端客服会话服务
//
// 职责：
//   - 接收访客（匿名）通过 Web Widget 发送的消息
//   - 查找/创建会话（基于 visitor_id + channel_id）
//   - 调用 SmartCSOrchestrator 走 RAG + AI 决策 + 人工接管
//   - 通过 WebSocket 推送 AI/坐席消息给访客
//   - 提供历史消息、关闭会话、评分等访客侧 API
//
// 与 SmartCSOrchestrator.HandleIncomingWithAgent 的关系：
//   - 薄包装：构造 IncomingContext，复用已有的 9 步编排
//   - 差异：Platform=WebEmbed / AccountID=channel_id / SenderID=visitor_id
type VisitorChatService struct {
	channelSvc      *ChatChannelService
	orchestrator    *SmartCSOrchestrator
	sessionSvc      *CustomerSessionService
	sessionRepo     *repository.CustomerSessionRepository
	messageRepo     *repository.SessionMessageRepository
	customerRepo    repository.CustomerRepository
	agentStatusRepo *repository.AgentStatusRepository
	agentBindingSvc *ChannelAgentBindingService
	inboxConvRepo   *repository.InboxConversationRepository
	leadMiningSvc   *Service
}

// NewVisitorChatService 构造访客会话服务
// agentBindingSvc 可为 nil（测试/降级场景）：nil 时网页客服回退默认编排（与历史行为一致）
//
// 五层架构 §三.5：service 层禁止直接持有 *gorm.DB，db 仅在构造期用于绑定各 repository；
// 测试场景下调用方应先 db.SetTestDB(database)，再传入 db=database 以保证 repository 走测试库。
func NewVisitorChatService(ctx context.Context, db *gorm.DB, channelSvc *ChatChannelService, orchestrator *SmartCSOrchestrator, agentBindingSvc *ChannelAgentBindingService) *VisitorChatService {
	if db == nil {
		db = repository.NewCustomerSessionRepository().GetDB(ctx)
	}
	return &VisitorChatService{
		channelSvc:      channelSvc,
		orchestrator:    orchestrator,
		sessionSvc:      NewCustomerSessionService(),
		sessionRepo:     repository.NewCustomerSessionRepositoryWithDB(db),
		messageRepo:     repository.NewSessionMessageRepositoryWithDB(db),
		customerRepo:    repository.NewCustomerRepository(),
		agentStatusRepo: repository.NewAgentStatusRepositoryWithDB(db),
		agentBindingSvc: agentBindingSvc,
		inboxConvRepo:   newInboxConversationRepo(db),
	}
}

func newInboxConversationRepo(db *gorm.DB) *repository.InboxConversationRepository {
	repo := repository.NewInboxConversationRepository()
	if db != nil {
		repo.SetDB(context.Background(), db)
	}
	return repo
}

func (s *VisitorChatService) ensureVisitorCustomer(ctx context.Context, req *VisitorSendMessageRequest, session *model.CustomerSession) error {
	_ = ctx
	if s.customerRepo == nil {
		return nil
	}
	unifiedID := "visitor:" + req.ChannelID + ":" + req.VisitorID
	if existing, _ := s.customerRepo.GetByUnifiedID(ctx, unifiedID); existing != nil {
		return nil
	}
	cust := &model.Customer{
		UnifiedID: unifiedID,
		Tags:      "[\"web_visitor\"]",
	}
	if err := s.customerRepo.Create(ctx, cust); err != nil {
		return fmt.Errorf("建档失败: %w", err)
	}
	return nil
}

// SetOrchestrator 注入编排器（用于循环依赖解耦）
func (s *VisitorChatService) SetOrchestrator(ctx context.Context, o *SmartCSOrchestrator) {
	s.orchestrator = o
}

// SetLeadMining 注入线索发掘服务（用于循环依赖解耦）
func (s *VisitorChatService) SetLeadMining(svc *Service) {
	s.leadMiningSvc = svc
}

// VisitorOpenSessionRequest 访客打开会话请求
//
// 注意（私域部署修复）：ChannelID/VisitorID 不再用 binding:"required"。
// 原因：私域部署模式下，channel_id 由中间件 AppKeyResolve 从 X-Chat-Channel-Id header
// 或 X-Chat-App-Key 反查注入 ctx；visitor_id 从 X-Chat-Visitor-Id header 注入。
// controller 层 OpenSession 会在 binding 之后做软解析兜底，所以 binding 不能 required。
// 保留 json 字段以便兼容显式传 body 的调用方。
type VisitorOpenSessionRequest struct {
	ChannelID    string `json:"channel_id"`
	VisitorID    string `json:"visitor_id"`
	VisitorName  string `json:"visitor_name"`
	VisitorPhone string `json:"visitor_phone"`
	VisitorEmail string `json:"visitor_email"`
	VisitorMeta  string `json:"visitor_meta"`
	Resume       bool   `json:"resume"`
}

// VisitorOpenSessionResult 访客打开会话结果
type VisitorOpenSessionResult struct {
	Session        *model.CustomerSession `json:"session"`
	IsNewSession   bool                   `json:"is_new_session"`
	OnlineAgentNum int                    `json:"online_agent_num"`
	WelcomeMessage string                 `json:"welcome_message"`
	VisitorToken   string                 `json:"visitor_token,omitempty"`
}

func visitorTokenSecret() []byte {
	if cfg := config.GetAppConfig(); cfg.Security.VisitorTokenSecret != "" {
		return []byte(cfg.Security.VisitorTokenSecret)
	}

	return nil
}

// GenerateVisitorToken 为访客生成 HMAC-SHA256 签名 token（委托给 security 包）
func GenerateVisitorToken(channelID, visitorID, sessionID string) (string, error) {
	return security.GenerateVisitorToken(string(visitorTokenSecret()), channelID, visitorID, sessionID, 0)
}

// ValidateVisitorToken 验证 visitor token（委托给 security 包）
func ValidateVisitorToken(token, channelID, visitorID, sessionID string) error {
	return security.ValidateVisitorToken(string(visitorTokenSecret()), token, channelID, visitorID, sessionID)
}

func (s *VisitorChatService) resolveChannel(ctx context.Context, channelRef string) (*model.ChatChannel, error) {
	ref := strings.TrimSpace(channelRef)
	if ref == "" {
		return s.channelSvc.GetOrCreateDefaultChannel(ctx)
	}
	if channel, err := s.channelSvc.GetByChannelID(ctx, ref); err == nil {
		return channel, nil
	} else if !strings.Contains(err.Error(), "渠道不存在") && !strings.Contains(err.Error(), "channel_id 不能为空") {
		return nil, err
	}
	if channel, err := s.channelSvc.GetByAppKey(ctx, ref); err == nil {
		return channel, nil
	} else if !strings.Contains(err.Error(), "AppKey 无效") {
		return nil, err
	}
	if ref == "default" {
		return s.channelSvc.GetOrCreateDefaultChannel(ctx)
	}
	if platform, ok := IsCardChannelRef(ref); ok {
		return s.channelSvc.GetOrCreateCardChannel(ctx, platform)
	}
	return nil, fmt.Errorf("渠道不存在: %s", ref)
}

// OpenSession 打开（或续接）会话
func (s *VisitorChatService) OpenSession(ctx context.Context, req *VisitorOpenSessionRequest) (*VisitorOpenSessionResult, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if strings.TrimSpace(req.ChannelID) == "" || strings.TrimSpace(req.VisitorID) == "" {
		return nil, errors.New("channel_id 和 visitor_id 必填")
	}

	channel, err := s.resolveChannel(ctx, req.ChannelID)
	if err != nil {
		return nil, err
	}
	if !ChatChannelIsActive(channel) {
		return nil, errors.New("渠道已禁用")
	}

	if req.Resume {
		existing, err := s.sessionRepo.GetLatestActiveByPlatformAccountUser(ctx,
			model.PlatformWebEmbed, channel.ChannelID, req.VisitorID,
			[]model.SessionStatus{model.SessionStatusResolved, model.SessionStatusClosed},
		)
		if err == nil && existing != nil {
			online, _ := s.countOnlineAgents(ctx)

			token, terr := GenerateVisitorToken(channel.ChannelID, req.VisitorID, existing.SessionID)
			if terr != nil {
				return nil, fmt.Errorf("生成 visitor_token 失败: %w", terr)
			}
			return &VisitorOpenSessionResult{
				Session:        existing,
				IsNewSession:   false,
				OnlineAgentNum: online,
				WelcomeMessage: channel.WelcomeMessage,
				VisitorToken:   token,
			}, nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("查询历史会话失败: %w", err)
		}
	}

	sessionTags := []string{"web_visitor"}
	if meta := strings.TrimSpace(req.VisitorMeta); meta != "" {
		var mv map[string]string
		if err := json.Unmarshal([]byte(meta), &mv); err == nil {
			if src := strings.TrimSpace(mv["source"]); src != "" {
				sessionTags = append(sessionTags, "source:"+src)
			}
			if cid := strings.TrimSpace(mv["card_id"]); cid != "" {
				sessionTags = append(sessionTags, "card_id:"+cid)
			}
		}
	}
	tagsJSON, _ := json.Marshal(sessionTags)
	session := &model.CustomerSession{
		SessionID:   generateSessionID(),
		Platform:    model.PlatformWebEmbed,
		AccountID:   channel.ChannelID,
		UserID:      req.VisitorID,
		UserName:    defaultIfEmpty(req.VisitorName, "访客"),
		UserPhone:   req.VisitorPhone,
		UserEmail:   req.VisitorEmail,
		Status:      model.SessionStatusPending,
		HandlerType: model.HandlerTypeAI,
		Priority:    0,
		Tags:        string(tagsJSON),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	_ = s.channelSvc.IncrementVisitorCount(ctx, channel.ChannelID)
	_ = s.channelSvc.IncrementSessionCount(ctx, channel.ChannelID)

	online, _ := s.countOnlineAgents(ctx)
	if online > 0 {
		_ = websocket.BroadcastToAgents(websocket.TypeNewSession, map[string]any{
			"session": session,
			"channel": map[string]any{
				"channel_id":   channel.ChannelID,
				"channel_name": channel.ChannelName,
			},
		})
	}

	_ = session

	token, err := GenerateVisitorToken(channel.ChannelID, req.VisitorID, session.SessionID)
	if err != nil {

		return nil, fmt.Errorf("生成 visitor_token 失败: %w", err)
	}
	return &VisitorOpenSessionResult{
		Session:        session,
		IsNewSession:   true,
		OnlineAgentNum: online,
		WelcomeMessage: channel.WelcomeMessage,
		VisitorToken:   token,
	}, nil
}

// GetSessionByVisitorSessionID 访客通过 session_id 获取会话（仅校验归属）
//
// channelID 入参既可能是 channel_id 也可能是 app_key
// （前端 embed 路由把 app_key 作为 path 透传）。这里把入参归一化为 channel_id 后再查。
func (s *VisitorChatService) GetSessionByVisitorSessionID(ctx context.Context, channelID, visitorID, sessionID string) (*model.CustomerSession, error) {
	channel, err := s.resolveChannel(ctx, channelID)
	if err != nil {
		return nil, errors.New("会话不存在或无权访问")
	}
	session, err := s.sessionRepo.GetBySessionIDPlatformAccountUser(ctx,
		sessionID, model.PlatformWebEmbed, channel.ChannelID, visitorID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("会话不存在或无权访问")
		}
		return nil, err
	}
	return session, nil
}

// VisitorSendMessageRequest 访客发送消息请求
type VisitorSendMessageRequest struct {
	ChannelID   string `json:"channel_id" binding:"required"`
	VisitorID   string `json:"visitor_id" binding:"required"`
	SessionID   string `json:"session_id" binding:"required"`
	Content     string `json:"content" binding:"required"`
	ContentType string `json:"content_type"`
	MediaURL    string `json:"media_url"`
	MediaType   string `json:"media_type"`
	MediaName   string `json:"media_name"`
	MediaSize   int64  `json:"media_size"`
}

// VisitorSendMessageResult 访客发送消息结果
type VisitorSendMessageResult struct {
	UserMessage    *model.SessionMessage `json:"user_message"`
	AIReplied      bool                  `json:"ai_replied"`
	AIResponse     *model.SessionMessage `json:"ai_response,omitempty"`
	AICards        []model.RichCard      `json:"ai_cards,omitempty"`
	Transferred    bool                  `json:"transferred"`
	TransferReason string                `json:"transfer_reason,omitempty"`
	Confidence     float64               `json:"confidence"`
	HandlerType    string                `json:"handler_type"`
	SuggestionID   uint                  `json:"suggestion_id,omitempty"`
}

// SendMessage 访客发送消息（核心入口）
func (s *VisitorChatService) SendMessage(ctx context.Context, req *VisitorSendMessageRequest) (*VisitorSendMessageResult, error) {
	if req == nil {
		return nil, errors.New("请求不能为空")
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, errors.New("消息内容不能为空")
	}

	session, err := s.GetSessionByVisitorSessionID(ctx, req.ChannelID, req.VisitorID, req.SessionID)
	if err != nil {
		return nil, err
	}

	// 会话状态守卫：resolved/closed 会话不允许访客继续追加消息（与坐席侧
	// CustomerSessionService.SendMessage 的领域判断保持同一口径），
	// 防止会话关闭后旧 session_id/token 仍可写入并意外触发 AI 回复。
	if !CustomerSessionCanSendMessage(session) {
		return nil, fmt.Errorf("会话状态 %s 不允许发送消息，请开启新会话", session.Status)
	}

	channel, err := s.resolveChannel(ctx, req.ChannelID)
	if err != nil {
		return nil, err
	}

	contentType := model.MessageTypeText
	if req.ContentType != "" {
		contentType = model.MessageType(req.ContentType)
	}
	if req.MediaURL != "" {
		switch req.MediaType {
		case "image":
			contentType = model.MessageTypeImage
		case "file":
			contentType = model.MessageTypeFile
		case "audio":
			contentType = model.MessageTypeAudio
		case "video":
			contentType = model.MessageTypeVideo
		}
	}
	userMsg := &model.SessionMessage{
		SessionID:   session.SessionID,
		Content:     req.Content,
		ContentType: contentType,
		MediaURL:    req.MediaURL,
		SenderType:  "user",
		SenderID:    req.VisitorID,
		SenderName:  session.UserName,
	}
	if err := s.messageRepo.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("保存访客消息失败: %w", err)
	}
	_ = s.sessionRepo.UpdateLastMessage(ctx, session.ID, req.Content, "user")

	if err := s.ensureVisitorCustomer(ctx, req, session); err != nil {
		logger.Warnf("[Visitor] 访客建档失败（不影响消息发送）: %v", err)
	}

	s.syncToInbox(ctx, session, req.Content)

	if s.leadMiningSvc != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			hub := &model.MessageHub{
				MsgID:          fmt.Sprintf("visitor:%s:%d", req.VisitorID, userMsg.ID),
				Platform:       string(model.PlatformWebEmbed),
				AccountID:      req.ChannelID,
				Direction:      "inbound",
				Status:         "pending",
				MsgType:        string(contentType),
				SenderID:       req.VisitorID,
				SenderName:     session.UserName,
				ConversationID: session.SessionID,
				Content:        req.Content,
				MediaURL:       req.MediaURL,
				IsGroup:        false,
				SentAt:         time.Now(),
			}
			s.leadMiningSvc.Enqueue(hub)
		}()
	}

	if session.AgentID > 0 {
		_ = websocket.SendToAgent(websocket.TypeNewMessage, map[string]any{
			"session_id": session.SessionID,
			"message":    userMsg,
		}, session.AgentID)
	}

	if s.orchestrator == nil {
		return &VisitorSendMessageResult{
			UserMessage: userMsg,
			HandlerType: string(session.HandlerType),
		}, nil
	}

	if shouldForceTransferByKeywords(req.Content) {
		if err := s.sessionSvc.AutoAssign(ctx, session.ID); err != nil {
			logger.Errorf("chat_visitor: AutoAssign 失败 session=%d: %v", session.ID, err)
			if uErr := s.sessionRepo.UpdateStatus(ctx, session.ID, model.SessionStatusWaiting); uErr != nil {
				logger.Errorf("chat_visitor: 转等待状态失败 session=%d: %v", session.ID, uErr)
			}
			if hErr := s.sessionRepo.UpdateHandlerType(ctx, session.ID, model.HandlerTypeHuman); hErr != nil {
				logger.Errorf("chat_visitor: 更新处理人类型失败 session=%d: %v", session.ID, hErr)
				// 状态持久化失败时如实告知访客，AI 继续兜底，避免"已转人工"的假承诺
				return &VisitorSendMessageResult{
					UserMessage:    userMsg,
					AIReplied:      false,
					Transferred:    false,
					TransferReason: "人工坐席暂不可用，AI 将继续为您服务",
					HandlerType:    string(session.HandlerType),
				}, nil
			}
		}
		_ = websocket.BroadcastToAgents(websocket.TypeNewSession, map[string]any{
			"session":    session,
			"need_human": true,
			"reason":     "关键词命中自动转人工：" + req.Content,
		})
		sysMsg := &model.SessionMessage{
			SessionID:  session.SessionID,
			Content:    "【系统】访客请求人工客服，正在为您接入...",
			SenderType: "system",
			SenderID:   "system",
			SenderName: "系统",
		}
		_ = s.messageRepo.Create(ctx, sysMsg)
		return &VisitorSendMessageResult{
			UserMessage:    userMsg,
			AIReplied:      false,
			Transferred:    true,
			TransferReason: "关键词命中自动转人工",
			HandlerType:    string(model.HandlerTypeHuman),
		}, nil
	}

	_ = websocket.SendToVisitor(websocket.TypeAITyping, map[string]any{"typing": true}, session.SessionID)
	defer websocket.SendToVisitor(websocket.TypeAITyping, map[string]any{"typing": false}, session.SessionID)

	in := &IncomingContext{
		Platform:   model.PlatformWebEmbed,
		AccountID:  channel.ChannelID,
		SenderID:   req.VisitorID,
		SenderName: session.UserName,
		Content:    req.Content,
		MessageID:  strconv.FormatUint(uint64(userMsg.ID), 10),
	}

	var agentCtxFromChannel *AgentContext
	if s.agentBindingSvc != nil {
		agentCtxFromChannel, _ = s.agentBindingSvc.LoadAgentForChannel(ctx, NormalizeChannelType(string(model.PlatformWebEmbed)), channel.ChannelID)
	}

	const webChatReplyTimeout = 60 * time.Second
	aiCtx, aiCancel := context.WithTimeout(ctx, webChatReplyTimeout)
	defer aiCancel()
	handleResult, err := s.orchestrator.HandleIncomingWithAgent(aiCtx, in, agentCtxFromChannel)
	if err != nil {
		return &VisitorSendMessageResult{
			UserMessage: userMsg,
			HandlerType: string(model.HandlerTypeAI),
		}, nil
	}

	result := &VisitorSendMessageResult{
		UserMessage:    userMsg,
		AIReplied:      handleResult.AIReplied,
		Confidence:     handleResult.Confidence,
		Transferred:    handleResult.Transferred,
		TransferReason: handleResult.TransferReason,
		HandlerType:    string(handleResult.HandlerType),
		SuggestionID:   handleResult.SuggestionID,
	}

	if handleResult.AIReplied && handleResult.Reply != "" {
		now := time.Now()
		existing, _ := s.messageRepo.FindRecentDuplicate(ctx, session.SessionID, "ai", "ai_assistant", handleResult.Reply, 5*time.Second)
		if existing != nil {
			_ = s.messageRepo.MarkDelivered(ctx, session.SessionID, []uint{existing.ID}, now)
			result.AIResponse = existing
		} else {
			aiMsg := &model.SessionMessage{
				SessionID:    session.SessionID,
				Content:      handleResult.Reply,
				ContentType:  model.MessageTypeText,
				SenderType:   "ai",
				SenderID:     "ai_assistant",
				SenderName:   "智能助手",
				AIConfidence: handleResult.Confidence,
				AISource:     "rag",
				DeliveredAt:  &now,
			}
			_ = s.messageRepo.Create(ctx, aiMsg)
			_ = s.sessionRepo.IncrementAIReplyCount(ctx, session.ID)
			result.AIResponse = aiMsg
		}
		_ = s.sessionRepo.UpdateLastMessage(ctx, session.ID, handleResult.Reply, "ai")
	}

	if len(handleResult.Cards) > 0 {
		result.AICards = handleResult.Cards
		now := time.Now()
		for i := range handleResult.Cards {
			card := handleResult.Cards[i]
			cardData, err := model.MarshalRichCard(&card)
			if err != nil {
				logger.Warnf("[visitor] 卡片序列化失败，跳过: %v", err)
				continue
			}
			cardMsg := &model.SessionMessage{
				SessionID:    session.SessionID,
				Content:      card.Title,
				ContentType:  model.MessageTypeCard,
				SenderType:   "ai",
				SenderID:     "ai_assistant",
				SenderName:   "智能助手",
				AIConfidence: handleResult.Confidence,
				AISource:     "rag",
				CardData:     cardData,
				CardType:     card.Type,
				DeliveredAt:  &now,
			}
			_ = s.messageRepo.Create(ctx, cardMsg)
		}
	}

	if session.AgentID > 0 {
		_ = websocket.SendToAgent(websocket.TypeSessionUpdate, map[string]any{
			"session_id":   session.SessionID,
			"handler_type": handleResult.HandlerType,
			"transferred":  handleResult.Transferred,
		}, session.AgentID)
	}

	_ = time.Now
	return result, nil
}

func (s *VisitorChatService) syncToInbox(ctx context.Context, session *model.CustomerSession, content string) {
	if s.inboxConvRepo == nil {
		return
	}
	now := time.Now()
	conv, err := s.inboxConvRepo.FindByPlatformAccountCustomer(ctx, string(model.PlatformWebEmbed), session.AccountID, session.UserID)
	if err == nil && conv != nil {
		_ = s.inboxConvRepo.UpdateLastMessage(ctx, conv.ID, content, now, 1)
		return
	}
	newConv := &model.InboxConversation{
		Platform:           string(model.PlatformWebEmbed),
		AccountID:          session.AccountID,
		CustomerID:         session.UserID,
		CustomerName:       session.UserName,
		ConversationID:     session.SessionID,
		LastMessagePreview: content,
		LastMessageAt:      &now,
		UnreadCount:        1,
		TotalCount:         1,
		Status:             "unread",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = s.inboxConvRepo.Create(ctx, newConv)
}

// GetMessages 访客获取历史消息
func (s *VisitorChatService) GetMessages(ctx context.Context, channelID, visitorID, sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error) {
	if _, err := s.GetSessionByVisitorSessionID(ctx, channelID, visitorID, sessionID); err != nil {
		return nil, 0, err
	}
	return s.messageRepo.GetBySessionID(ctx, sessionID, page, pageSize)
}

// GetLatestActiveSession 获取访客最近一次未结束会话（用于离线消息续接）
//
// channelID 入参既可能是 channel_id 也可能是 app_key，
// 这里统一通过 resolveChannel 归一化为 channel_id 后再查询。
func (s *VisitorChatService) GetLatestActiveSession(ctx context.Context, channelID, visitorID string) (*model.CustomerSession, error) {
	channel, err := s.resolveChannel(ctx, channelID)
	if err != nil {
		return nil, nil
	}
	session, err := s.sessionRepo.GetLatestActiveByPlatformAccountUser(ctx,
		model.PlatformWebEmbed, channel.ChannelID, visitorID,
		[]model.SessionStatus{model.SessionStatusResolved, model.SessionStatusClosed},
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return session, nil
}

// GetRecentClosedSessions 获取访客最近 7 天已结束会话（离线消息列表）
//
// channelID 入参既可能是 channel_id 也可能是 app_key。
func (s *VisitorChatService) GetRecentClosedSessions(ctx context.Context, channelID, visitorID string, limit int) ([]*model.CustomerSession, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	channel, err := s.resolveChannel(ctx, channelID)
	if err != nil {
		return []*model.CustomerSession{}, nil
	}
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	return s.sessionRepo.ListRecentClosedByPlatformAccountUser(ctx,
		model.PlatformWebEmbed, channel.ChannelID, visitorID,
		[]model.SessionStatus{model.SessionStatusResolved, model.SessionStatusClosed},
		sevenDaysAgo, limit,
	)
}

// GetOfflineMessages 拉取访客离线期间未投递的坐席/AI 回复消息
//
// 离线消息定义：sender_type IN ('ai', 'agent') AND delivered_at IS NULL
// 用户自己发的消息不算"离线消息"（用户自己当然知道）
//
// 调用方应在拉取后调用 MarkMessagesDelivered 标记为已投递，避免重复拉取。
func (s *VisitorChatService) GetOfflineMessages(ctx context.Context, channelID, visitorID, sessionID string) ([]*model.SessionMessage, error) {
	if _, err := s.GetSessionByVisitorSessionID(ctx, channelID, visitorID, sessionID); err != nil {
		return nil, err
	}
	return s.messageRepo.ListOfflineBySessionID(ctx, sessionID)
}

// MarkMessagesDelivered 批量标记消息已投递（WebSocket 拉到离线消息后调用）
func (s *VisitorChatService) MarkMessagesDelivered(ctx context.Context, sessionID string, messageIDs []uint) error {
	if len(messageIDs) == 0 {
		return nil
	}
	now := time.Now()
	return s.messageRepo.MarkDelivered(ctx, sessionID, messageIDs, now)
}

// RequestHumanTransfer 访客主动转人工
func (s *VisitorChatService) RequestHumanTransfer(ctx context.Context, channelID, visitorID, sessionID, reason string) error {
	session, err := s.GetSessionByVisitorSessionID(ctx, channelID, visitorID, sessionID)
	if err != nil {
		return err
	}

	transferMsg := &model.SessionMessage{
		SessionID:  session.SessionID,
		Content:    "【访客请求转人工】" + defaultIfEmpty(reason, ""),
		SenderType: "user",
		SenderID:   visitorID,
		SenderName: session.UserName,
	}
	_ = s.messageRepo.Create(ctx, transferMsg)

	if err := s.sessionSvc.AutoAssign(ctx, session.ID); err != nil {
		_ = s.sessionRepo.UpdateStatus(ctx, session.ID, model.SessionStatusWaiting)
		_ = s.sessionRepo.UpdateHandlerType(ctx, session.ID, model.HandlerTypeHuman)
		_ = websocket.SendToVisitor(websocket.TypeMessage, map[string]any{
			"session_id": session.SessionID,
			"system_msg": "正在为您转接人工客服，请稍候...",
		}, session.SessionID)
	}

	return nil
}

// CloseSession 访客主动关闭会话
func (s *VisitorChatService) CloseSession(ctx context.Context, channelID, visitorID, sessionID string) error {
	session, err := s.GetSessionByVisitorSessionID(ctx, channelID, visitorID, sessionID)
	if err != nil {
		return err
	}
	return s.sessionSvc.UpdateSessionStatus(ctx, session.ID, model.SessionStatusClosed)
}

// RateSession 访客评分
func (s *VisitorChatService) RateSession(ctx context.Context, channelID, visitorID, sessionID string, rating int, comment string) error {
	session, err := s.GetSessionByVisitorSessionID(ctx, channelID, visitorID, sessionID)
	if err != nil {
		return err
	}
	if rating < 1 || rating > 5 {
		return errors.New("评分必须在 1-5 之间")
	}
	if err := s.sessionSvc.RateSession(ctx, session.ID, rating, comment); err != nil {
		return err
	}

	// 回流 CSAT 调查单：会话关闭时 SessionChainService.TriggerCSATOnClose 会自动
	// 创建 csat_surveys（status=sent），访客在 embed 窗口提交的评分需要同步写回该
	// 调查单（status→responded），否则管理端 CSAT 看板（只统计 csat_surveys）永远
	// 读不到 embed 访客评分，调查单停留在 sent 无人闭环。无调查单时静默跳过
	// （best-effort：访客可能直接评分而未走自动调查流程，此时 customer_sessions.rating
	// 已落地，不阻断）。
	csatRepo := repository.NewCSATSurveyRepository()
	if _, err := csatRepo.SubmitResponse(ctx, session.SessionID, rating, comment); err != nil {
		logger.Ctx(ctx).Info().
			Str("session_id", session.SessionID).
			Int("rating", rating).
			Msg("[VisitorChat] 无进行中的 CSAT 调查单，评分仅落 customer_sessions.rating")
	}
	return nil
}

func (s *VisitorChatService) countOnlineAgents(ctx context.Context) (int, error) {
	return s.agentStatusRepo.CountOnlineAgents(ctx)
}

// CountAvailableAgents 公开方法：可用坐席数
func (s *VisitorChatService) CountAvailableAgents(ctx context.Context) (int, error) {
	return s.countOnlineAgents(ctx)
}

func shouldForceTransferByKeywords(content string) bool {
	return MatchTransferKeywords(content)
}
