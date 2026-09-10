package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/aiagent/llm"
	rag_core "hivemtk-user/internal/aiagent/rag/core"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SessionAssignmentService 会话分配服务
type SessionAssignmentService struct {
	sessionRepo         *repository.CustomerSessionRepository
	messageRepo         *repository.SessionMessageRepository
	agentRepo           *repository.AgentStatusRepository
	ragEngine           *rag_core.RAGEngine
	llmService          *llm.LLMService
	confidenceThreshold float64
}

// NewSessionAssignmentService 创建会话分配服务
func NewSessionAssignmentService() *SessionAssignmentService {
	return &SessionAssignmentService{
		sessionRepo:         repository.NewCustomerSessionRepository(),
		messageRepo:         repository.NewSessionMessageRepository(),
		agentRepo:           repository.NewAgentStatusRepository(),
		ragEngine:           rag_core.NewRAGEngine(nil),
		llmService:          llm.NewLLMService(),
		confidenceThreshold: 0.5,
	}
}

// SetConfidenceThreshold 设置置信度阈值
func (s *SessionAssignmentService) SetConfidenceThreshold(ctx context.Context, threshold float64) {
	s.confidenceThreshold = threshold
}

// ProcessIncomingMessage 处理 incoming 消息，决定是否创建/更新会话并分配
func (s *SessionAssignmentService) ProcessIncomingMessage(ctx context.Context, msg *model.UnifiedMessage) error {
	session, err := s.findActiveSession(ctx, msg.SenderID, msg.ChatID)
	if err != nil && !errors.Is(err, ErrSessionNotFound) {
		return err
	}

	if session == nil {
		session, err = s.createSession(ctx, msg)
		if err != nil {
			return err
		}
	}

	err = s.saveMessageToSession(ctx, session, msg)
	if err != nil {
		return err
	}

	decision, err := s.decideHandler(ctx, session, msg)
	if err != nil {
		return err
	}

	return s.executeDecision(ctx, session, decision)
}

func (s *SessionAssignmentService) findActiveSession(ctx context.Context, userID, chatID string) (*model.CustomerSession, error) {
	if chatID != "" {
		if session, err := s.sessionRepo.GetBySessionID(ctx, chatID); err == nil && session != nil && isActiveSession(session) {
			return session, nil
		}
	}
	if userID == "" {
		return nil, ErrSessionNotFound
	}
	sessions, err := s.sessionRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if isActiveSession(session) {
			return session, nil
		}
	}

	return nil, ErrSessionNotFound
}

func isActiveSession(session *model.CustomerSession) bool {
	return session.Status == model.SessionStatusPending ||
		session.Status == model.SessionStatusAIHandling ||
		session.Status == model.SessionStatusHumanHandling ||
		session.Status == model.SessionStatusWaiting
}

// ErrSessionNotFound 会话未找到错误
var ErrSessionNotFound = errors.New("活跃会话未找到")

func (s *SessionAssignmentService) createSession(ctx context.Context, msg *model.UnifiedMessage) (*model.CustomerSession, error) {
	sessionID := fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), msg.MessageID[:8])

	session := &model.CustomerSession{
		SessionID:       sessionID,
		Platform:        msg.Platform,
		AccountID:       msg.AccountID,
		UserID:          msg.SenderID,
		UserName:        msg.SenderName,
		UserAvatar:      msg.SenderAvatar,
		Status:          model.SessionStatusPending,
		Priority:        0,
		LastMessage:     msg.Content,
		LastMessageAt:   &msg.ReceivedAt,
		LastMessageBy:   "user",
		MessageCount:    0,
		AIReplyCount:    0,
		HumanReplyCount: 0,
	}

	err := s.sessionRepo.Create(ctx, session)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (s *SessionAssignmentService) saveMessageToSession(ctx context.Context, session *model.CustomerSession, msg *model.UnifiedMessage) error {
	message := &model.SessionMessage{
		SessionID:    session.SessionID,
		Content:      msg.Content,
		ContentType:  model.MessageTypeText,
		MediaURL:     msg.MediaURL,
		SenderType:   "user",
		SenderID:     msg.SenderID,
		SenderName:   msg.SenderName,
		SenderAvatar: msg.SenderAvatar,
	}

	return s.messageRepo.Create(ctx, message)
}

// HandlerDecision 处理者决策
type HandlerDecision struct {
	HandlerType    model.HandlerType
	Confidence     float64
	ShouldTransfer bool
	TransferReason string
	AIResponse     string
	Priority       int
}

func (s *SessionAssignmentService) decideHandler(ctx context.Context, session *model.CustomerSession, msg *model.UnifiedMessage) (*HandlerDecision, error) {
	decision := &HandlerDecision{
		HandlerType: model.HandlerTypeAI,
		Confidence:  0,
	}

	if session.HandlerType == model.HandlerTypeHuman && session.AgentID > 0 {
		agent, err := s.agentRepo.GetByAgentID(ctx, session.AgentID)
		if err == nil && agent.Status != "offline" {
			decision.HandlerType = model.HandlerTypeHuman
			return decision, nil
		}
	}

	if s.isUrgentOrComplaint(ctx, msg.Content) {
		decision.HandlerType = model.HandlerTypeHuman
		decision.TransferReason = "检测到紧急或投诉内容"
		decision.Priority = 2
		return decision, nil
	}

	ragResults, err := s.ragEngine.Search(ctx, msg.Content, 3)
	if err == nil && len(ragResults) > 0 {
		decision.Confidence = ragResults[0].Score
		if decision.Confidence >= s.confidenceThreshold {
			decision.AIResponse = ragResults[0].Content
			decision.HandlerType = model.HandlerTypeAI
			return decision, nil
		}
	}

	llmResponse, confidence, err := s.generateLLMResponse(ctx, msg.Content)
	if err == nil {
		if confidence >= s.confidenceThreshold {
			decision.AIResponse = llmResponse
			decision.Confidence = confidence
			decision.HandlerType = model.HandlerTypeAI
			return decision, nil
		}
	}

	decision.HandlerType = model.HandlerTypeHuman
	decision.TransferReason = fmt.Sprintf("AI 置信度不足 (%.2f < %.2f)", decision.Confidence, s.confidenceThreshold)

	return decision, nil
}

func (s *SessionAssignmentService) isUrgentOrComplaint(ctx context.Context, content string) bool {
	urgentKeywords := []string{
		"投诉", "举报", "曝光", "315", "消协", "工商局",
		"紧急", "着急", "马上", "立刻", "赶紧", "快点",
		"骗子", "假货", "垃圾", "垃圾东西", "再也不买",
		"退钱", "退款", "赔钱", "赔偿",
	}

	content = strings.ToLower(content)
	for _, keyword := range urgentKeywords {
		if strings.Contains(content, keyword) {
			return true
		}
	}

	return false
}

func (s *SessionAssignmentService) generateLLMResponse(ctx context.Context, content string) (string, float64, error) {
	prompt := fmt.Sprintf(`作为专业客服助手，请针对以下用户消息生成友好、专业的回复：

用户消息：%s

要求：
1. 语气友好专业
2. 回复简洁（不超过 200 字）
3. 如有必要，询问更多细节
4. 不要编造信息，不确定的内容请说明需要核实

回复：`, content)

	config := &llm.LLMConfig{
		Model:          "gpt-3.5-turbo",
		APIType:        "openai",
		Temperature:    0.7,
		MaxTokens:      500,
		ResponseFormat: "text",
	}

	output, err := s.llmService.Generate(ctx, config, prompt)
	if err != nil {
		return "", 0.5, err
	}

	return output, s.calibratedConfidence(ctx, content), nil
}

func (s *SessionAssignmentService) calibratedConfidence(ctx context.Context, content string) float64 {
	agg := GetConfidenceAggregator()
	if agg == nil {
		return 0.5
	}
	dec, err := agg.Aggregate(ctx, &dto.SignalCollectionInput{
		Text:          content,
		RawIntentConf: 0.5,
	})
	if err != nil || dec == nil {
		return 0.5
	}
	return dec.AggregatedConf
}

func (s *SessionAssignmentService) executeDecision(ctx context.Context, session *model.CustomerSession, decision *HandlerDecision) error {
	if decision.Priority > session.Priority {
		session.Priority = decision.Priority
	}

	switch decision.HandlerType {
	case model.HandlerTypeAI:
		return s.handleByAI(ctx, session, decision.AIResponse)
	case model.HandlerTypeHuman:
		return s.handleByHuman(ctx, session, decision.TransferReason)
	default:
		return nil
	}
}

func (s *SessionAssignmentService) handleByAI(ctx context.Context, session *model.CustomerSession, response string) error {
	err := s.sessionRepo.UpdateStatus(ctx, session.ID, model.SessionStatusAIHandling)
	if err != nil {
		return err
	}

	message := &model.SessionMessage{
		SessionID:    session.SessionID,
		Content:      response,
		ContentType:  model.MessageTypeText,
		SenderType:   "ai",
		SenderID:     "system",
		SenderName:   "AI 助手",
		AIConfidence: 0.8,
		AISource:     "llm",
	}

	err = s.messageRepo.Create(ctx, message)
	if err != nil {
		return err
	}

	err = s.sessionRepo.UpdateLastMessage(ctx, session.ID, response, "ai")
	if err != nil {
		return err
	}

	err = s.sessionRepo.IncrementAIReplyCount(ctx, session.ID)
	if err != nil {
		return err
	}

	if session.AgentID > 0 {
		websocket.NotifySessionUpdate(strconv.FormatUint(uint64(session.AgentID), 10), map[string]any{
			"session_id": session.SessionID,
			"status":     model.SessionStatusAIHandling,
			"ai_replied": true,
		})
	}

	return s.sessionRepo.UpdateStatus(ctx, session.ID, model.SessionStatusWaiting)
}

func (s *SessionAssignmentService) handleByHuman(ctx context.Context, session *model.CustomerSession, reason string) error {
	err := s.sessionRepo.UpdateStatus(ctx, session.ID, model.SessionStatusPending)
	if err != nil {
		return err
	}

	if session.AgentID == 0 {
		err = s.autoAssignToAgent(ctx, session, reason)
		if err != nil {
			return nil
		}
	}

	return nil
}

func (s *SessionAssignmentService) autoAssignToAgent(ctx context.Context, session *model.CustomerSession, reason string) error {
	bestAgent, err := s.agentRepo.AutoAssignAgentTx(ctx, s.sessionRepo, session.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("无在线客服")
		}
		return err
	}

	websocket.NotifyNewSession(strconv.FormatUint(uint64(bestAgent.AgentID), 10), map[string]any{
		"session_id":      session.SessionID,
		"user_name":       session.UserName,
		"last_message":    session.LastMessage,
		"transfer_reason": reason,
		"priority":        session.Priority,
	})

	_ = websocket.SendToVisitor(websocket.TypeAgentJoined, map[string]any{
		"session_id": session.SessionID,
		"handler":    "human",
		"agent_name": bestAgent.AgentName,
		"reason":     "正在为您接入人工客服，请稍候...",
	}, session.SessionID)

	return nil
}

// TransferToHuman 转人工（当 AI 处理过程中用户要求转人工时）
//
// Bug 修复 ：
//
//	nil 检查 + 幂等分配 + 通知新客服 + 解锁 Redis 接管锁。
func (s *SessionAssignmentService) TransferToHuman(ctx context.Context, sessionID uint, agentID uint, reason string) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("session not found")
	}

	if session.AgentID > 0 {
		_ = s.agentRepo.DecrementActiveSessions(ctx, session.AgentID)
	}

	if agentID > 0 {
		agent, err := s.agentRepo.GetByAgentID(ctx, agentID)
		if err != nil || agent == nil {
			return errors.New("agent not found")
		}
		if agent.Status == "offline" {
			return errors.New("agent is offline")
		}
		if err := s.sessionRepo.AssignAgent(ctx, sessionID, agentID, agent.AgentName); err != nil {
			return err
		}
		if err := s.agentRepo.IncrementActiveSessions(ctx, agentID); err != nil {
			return err
		}
		websocket.NotifyNewSession(strconv.FormatUint(uint64(agentID), 10), map[string]any{
			"session_id":      session.SessionID,
			"user_name":       session.UserName,
			"last_message":    session.LastMessage,
			"transfer_reason": reason,
		})
		_ = websocket.SendToVisitor(websocket.TypeAgentJoined, map[string]any{
			"session_id": session.SessionID,
			"handler":    "human",
			"reason":     "已为您转接客服，请稍候",
		}, session.SessionID)
	}

	if err := s.sessionRepo.UpdateStatus(ctx, sessionID, model.SessionStatusHumanHandling); err != nil {
		return err
	}
	return nil
}

// GetPendingSessions 获取待分配会话
func (s *SessionAssignmentService) GetPendingSessions(ctx context.Context) ([]*model.CustomerSession, error) {
	return s.sessionRepo.GetPendingSessions(context.Background())
}

// AutoAssignAllPending 自动分配所有待处理会话
func (s *SessionAssignmentService) AutoAssignAllPending(ctx context.Context) (int, error) {
	sessions, err := s.GetPendingSessions(ctx)
	if err != nil {
		return 0, err
	}

	assignedCount := 0
	for _, session := range sessions {
		err := s.autoAssignToAgent(ctx, session, "系统自动分配")
		if err != nil {
			continue
		}
		assignedCount++
	}

	return assignedCount, nil
}
