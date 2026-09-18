package service

import (
	"context"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"
)

// AISuggestionService AI建议服务
type AISuggestionService struct {
	suggestionRepo *repository.AISuggestionRepository
	sessionRepo    *repository.CustomerSessionRepository
}

// NewAISuggestionService 创建AI建议服务实例
func NewAISuggestionService() *AISuggestionService {
	return &AISuggestionService{
		suggestionRepo: repository.NewAISuggestionRepository(),
		sessionRepo:    repository.NewCustomerSessionRepository(),
	}
}

// CreateSuggestion 创建AI建议
func (s *AISuggestionService) CreateSuggestion(ctx context.Context, sessionID string, messageID uint, suggestion string, confidence float64, source string) (*model.AISuggestion, error) {
	ais := &model.AISuggestion{
		SessionID:  sessionID,
		MessageID:  messageID,
		Suggestion: suggestion,
		Confidence: confidence,
		Source:     source,
	}

	if err := s.suggestionRepo.Create(ctx, ais); err != nil {
		return nil, err
	}

	session, _ := s.sessionRepo.GetBySessionID(ctx, sessionID)
	if session != nil && session.AgentID > 0 {
		if err := websocket.NotifyAISuggestion(strconv.FormatUint(uint64(session.AgentID), 10), ais); err != nil {
			logger.Warnf("[AISuggestion] 推送建议通知失败 agentID=%d: %v", session.AgentID, err)
		}
	}

	return ais, nil
}

// GetSuggestions 获取会话的AI建议
func (s *AISuggestionService) GetSuggestions(ctx context.Context, sessionID string) ([]*model.AISuggestion, error) {
	return s.suggestionRepo.GetBySessionID(ctx, sessionID)
}

// UseSuggestion 使用AI建议
func (s *AISuggestionService) UseSuggestion(ctx context.Context, id uint, agentID uint) error {
	return s.suggestionRepo.MarkAsUsed(ctx, id, agentID)
}
