package service

import (
	"context"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

// SessionService 会话查询 + 手动中断
type SessionService struct {
	sessionRepo repository.BrowserSessionRepository
	stepRepo    repository.BrowserStepRepository
	executor    *Executor
}

func NewSessionService(sessionRepo repository.BrowserSessionRepository, stepRepo repository.BrowserStepRepository, executor *Executor) *SessionService {
	return &SessionService{sessionRepo: sessionRepo, stepRepo: stepRepo, executor: executor}
}

func (s *SessionService) Get(ctx context.Context, id, userID uint) (*model.BrowserSession, error) {
	return s.sessionRepo.GetByID(ctx, id, userID)
}

func (s *SessionService) ListByUser(ctx context.Context, userID uint, status string, page, limit int) ([]*model.BrowserSession, int64, error) {
	return s.sessionRepo.ListByUser(ctx, userID, status, page, limit)
}

func (s *SessionService) ListByTask(ctx context.Context, taskID, userID uint, page, limit int) ([]*model.BrowserSession, int64, error) {
	return s.sessionRepo.ListByTaskID(ctx, taskID, userID, page, limit)
}

func (s *SessionService) ListSteps(ctx context.Context, sessionID, userID uint) ([]*model.BrowserStep, error) {
	// 归属校验：session 必须属于该用户
	if _, err := s.sessionRepo.GetByID(ctx, sessionID, userID); err != nil {
		return nil, err
	}
	return s.stepRepo.ListBySessionID(ctx, sessionID)
}

// Stop 手动中断（session 置 stopped 由 Executor 收口；此处仅发信号 + 返回是否命中运行中）
func (s *SessionService) Stop(ctx context.Context, sessionID, userID uint, reason string) (bool, error) {
	sess, err := s.sessionRepo.GetByID(ctx, sessionID, userID)
	if err != nil {
		return false, err
	}
	if sess.Status != "created" && sess.Status != "active" {
		return false, nil
	}
	ok := s.executor.SignalStop(sessionID)
	return ok, nil
}
