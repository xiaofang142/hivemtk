package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"
	"reflect"
	"time"
)

// BlacklistRequest 拉黑请求
type BlacklistRequest struct {
	SessionID    uint   `json:"session_id" binding:"required"`
	Reason       string `json:"reason"`
	OperatorID   uint   `json:"operator_id"`
	OperatorName string `json:"operator_name"`
	TTLHours     int    `json:"ttl_hours"`
}

// BlacklistSource 黑名单来源枚举
type BlacklistSource string

const (
	BlacklistSourceManual BlacklistSource = "manual"
	BlacklistSourceAuto   BlacklistSource = "auto"
	BlacklistSourceRisk   BlacklistSource = "risk"
)

// BlacklistUser 拉黑当前会话对应的访客（user_id 维度）
//
// 行为：
//  1. 会话必须存在且有 user_id，否则拒绝。
//  2. 幂等：若该 user_id+platform 已存在 active 记录，更新 reason/expires_at。
//  3. 拉黑后立即关闭该会话（status=closed）以防继续对话。
//  4. 推 WebSocket 给前端（type=handler_changed, event=blacklisted）。
//
// 错误：返回业务语义化错误（含中文 message 供前端直接展示）。
func (s *CustomerSessionService) BlacklistUser(ctx context.Context, req *BlacklistRequest) error {
	_ = ctx
	if req == nil {
		return errors.New("请求体不能为空")
	}
	if req.SessionID == 0 {
		return errors.New("session_id 必填")
	}
	session, err := s.sessionRepo.GetByID(ctx, req.SessionID)
	if err != nil {
		return errors.New("会话不存在")
	}
	if session.UserID == "" {
		return errors.New("该会话无 user_id，无法拉黑")
	}

	var expiresAt *time.Time
	if req.TTLHours > 0 {
		t := time.Now().Add(time.Duration(req.TTLHours) * time.Hour)
		expiresAt = &t
	}

	blacklistRecord := &model.UserBlacklist{
		UserID:       session.UserID,
		Platform:     session.Platform,
		Reason:       req.Reason,
		Source:       string(BlacklistSourceManual),
		OperatorID:   req.OperatorID,
		OperatorName: req.OperatorName,
		SessionID:    session.SessionID,
		Active:       true,
		ExpiresAt:    expiresAt,
	}
	if err := s.blacklistRepo.Add(ctx, blacklistRecord); err != nil {
		return fmt.Errorf("写入黑名单失败: %w", err)
	}

	// 黑名单已生效，但会话未关闭则访客仍停留在原会话收发——必须记日志并反馈给调用方
	if err := s.sessionRepo.UpdateStatus(ctx, req.SessionID, model.SessionStatusClosed); err != nil {
		logger.Errorf("blacklist: 会话关闭失败 session=%d: %v", req.SessionID, err)
		return fmt.Errorf("黑名单已写入，但关闭会话失败: %w", err)
	}

	if err := s.notifySessionUpdate(ctx, session, "blacklisted", "human"); err != nil {
		logger.Errorf("blacklist: 会话通知失败 session=%d: %v", req.SessionID, err)
	}
	if err := websocket.SendToVisitor(websocket.TypeAgentJoined, map[string]any{
		"session_id":  session.SessionID,
		"handler":     "human",
		"reason":      "因违反服务条款，该访客已被加入黑名单",
		"blacklisted": true,
	}, session.SessionID); err != nil {
		logger.Errorf("blacklist: WS 通知访客失败 session=%d: %v", req.SessionID, err)
	}

	return nil
}

// UnblacklistUser 解除拉黑
func (s *CustomerSessionService) UnblacklistUser(ctx context.Context, userID string, platform model.Platform) error {
	if userID == "" {
		return errors.New("user_id 必填")
	}
	if err := s.blacklistRepo.Remove(ctx, userID, platform); err != nil {
		return fmt.Errorf("解除拉黑失败: %w", err)
	}
	return nil
}

// IsUserBlacklisted 判断访客是否在黑名单
func (s *CustomerSessionService) IsUserBlacklisted(ctx context.Context, userID string, platform model.Platform) (bool, error) {
	return s.blacklistRepo.IsBlacklisted(ctx, userID, platform)
}

// ListActiveBlacklist 分页查询生效中的黑名单
//
// 命名统一：service 层用业务语义「黑名单」，repository 用实现语义「Active」。
// 这样 controller 调 service.ListActiveBlacklist 时更直观。
func (s *CustomerSessionService) ListActiveBlacklist(ctx context.Context, page, pageSize int) ([]*model.UserBlacklist, int64, error) {
	return s.blacklistRepo.ListActive(ctx, page, pageSize)
}

var _ = func() error {
	t := reflect.TypeOf(CustomerSessionService{})
	field, ok := t.FieldByName("blacklistRepo")
	if !ok {
		return errors.New("blacklistRepo field is missing on CustomerSessionService")
	}
	want := reflect.TypeOf((*repository.UserBlacklistRepository)(nil))
	if field.Type != want {
		return fmt.Errorf("blacklistRepo field type changed: got %v, want %v", field.Type, want)
	}
	return nil
}()

func (s *CustomerSessionService) preCreateBlacklistGuard(ctx context.Context, req *CreateSessionRequest) error {
	if req == nil {
		return errors.New("请求体不能为空")
	}
	if req.UserID == "" {
		return nil
	}
	banned, err := s.blacklistRepo.IsBlacklisted(ctx, req.UserID, req.Platform)
	if err != nil {
		return fmt.Errorf("黑名单校验失败: %w", err)
	}
	if banned {
		return errors.New("该访客已被加入黑名单，无法创建新会话")
	}
	return nil
}
