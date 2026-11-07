package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
)

// ErrSessionForbidden 数据隔离越权：会话不属于当前身份且不是未分配池。
var ErrSessionForbidden = errors.New("无权访问该会话")

// SessionScope 客服会话读侧的数据隔离身份（由 controller 从登录态组装）。
//
// 与 inbox_scope.go 的 InboxScope 同形：Role 为空或 "admin" 不限制，
// 其余角色（customer_service/staff）只可见 agent_id = 自己 或 agent_id = 0（未分配池）。
// 未分配池必须可见：pending 会话靠认领进入人工，隔离把池关掉等于把工作流关掉。
type SessionScope struct {
	Role   string
	UserID uint
}

func sessionScopeRestricted(s SessionScope) bool {
	return s.Role != "" && s.Role != "admin"
}

// sessionVisible 判定一条会话对该身份是否可见（纯函数，便于单测）。
func sessionVisible(sess *model.CustomerSession, scope SessionScope) bool {
	if !sessionScopeRestricted(scope) {
		return true
	}
	return sess.AgentID == 0 || sess.AgentID == scope.UserID
}

func (s *CustomerSessionService) ensureSessionVisible(sess *model.CustomerSession, scope SessionScope) error {
	if !sessionVisible(sess, scope) {
		return ErrSessionForbidden
	}
	return nil
}

// GetSessionsScoped 列表读口（数据隔离版）。
func (s *CustomerSessionService) GetSessionsScoped(ctx context.Context, status model.SessionStatus, page, pageSize int, scope SessionScope) ([]*model.CustomerSession, int64, error) {
	if sessionScopeRestricted(scope) {
		return s.sessionRepo.GetByMerchantForAgent(ctx, status, page, pageSize, scope.UserID)
	}
	return s.GetSessions(ctx, status, page, pageSize)
}

// GetPendingSessionsScoped 待处理池读口（数据隔离版）：自己名下 + 未分配。
func (s *CustomerSessionService) GetPendingSessionsScoped(ctx context.Context, page, pageSize int, scope SessionScope) ([]*model.CustomerSession, error) {
	if !sessionScopeRestricted(scope) {
		return s.GetPendingSessions(ctx, page, pageSize)
	}
	statuses := []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	// AV-04c 批次5 刀1：隔离查询下沉 sessionRepo.ListPendingForAgent
	// （agent_id 过滤、排序、分页逐字保留），service 不再经 GetDB 取句柄。
	return s.sessionRepo.ListPendingForAgent(ctx, statuses, scope.UserID, offset, pageSize)
}

// CountPendingSessionsScoped 待处理池计数（数据隔离版，与 GetPendingSessionsScoped 同口径）。
func (s *CustomerSessionService) CountPendingSessionsScoped(ctx context.Context, scope SessionScope) (int64, error) {
	if !sessionScopeRestricted(scope) {
		return s.CountPendingSessions(ctx)
	}
	statuses := []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
	}
	// AV-04c 批次5 刀1：隔离计数下沉 sessionRepo.CountByStatusesForAgent。
	return s.sessionRepo.CountByStatusesForAgent(ctx, statuses, scope.UserID)
}

// GetSessionByIDScoped 详情读口（数据隔离版）：不可见返回 ErrSessionForbidden。
func (s *CustomerSessionService) GetSessionByIDScoped(ctx context.Context, id uint, scope SessionScope) (*model.CustomerSession, error) {
	session, err := s.GetSessionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSessionVisible(session, scope); err != nil {
		return nil, err
	}
	return session, nil
}

// GetSessionByIDStringScoped 按业务 session_id 的详情读口（数据隔离版）。
// SendMessage/GetMessages 的入口拿到的可能是字符串 id，数字 id 那条路挡住了
// 字符串这条路就绕过去了 —— 两条都要过同一道可见性检查。
func (s *CustomerSessionService) GetSessionByIDStringScoped(ctx context.Context, sessionID string, scope SessionScope) (*model.CustomerSession, error) {
	session, err := s.sessionRepo.GetByIDString(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSessionVisible(session, scope); err != nil {
		return nil, err
	}
	return session, nil
}
