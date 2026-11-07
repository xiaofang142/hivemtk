package service

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
)

// ErrInboxForbidden 非超管访问了未分配给自己的会话（数据权限拦截）
var ErrInboxForbidden = errors.New("无权访问该会话")

// InboxScope 收件箱数据权限上下文（由 controller 从 JWT 注入）
//
// 规则（最小权限）：
//   - Role == "admin" 或 Role 为空（旧调用/内部任务）→ 不隔离
//   - 其余角色（customer_service/staff）→ 只能见到 assigned_to == Username 的会话
type InboxScope struct {
	Role     string
	Username string
}

// restricted 判定是否需要行级隔离
func (sc InboxScope) restricted() bool {
	return sc.Role != "" && sc.Role != model.SystemRoleCodeAdmin
}

// visible 会话归属是否对当前 scope 可见
func (sc InboxScope) visible(conv *model.InboxConversation) bool {
	if !sc.restricted() || conv == nil {
		return true
	}
	return conv.AssignedTo != "" && conv.AssignedTo == sc.Username
}

// applyScope 非超管强制覆盖查询条件为“只看自己”（忽略入参 assigned_to）
func (sc InboxScope) applyScope(q *InboxQuery) {
	if sc.restricted() {
		q.AssignedTo = sc.Username
	}
}

// ensureVisible 取会话并校验归属；不可见返回 ErrInboxForbidden
func (s *InboxService) ensureVisible(ctx context.Context, conversationID uint, sc InboxScope) (*model.InboxConversation, error) {
	conv, err := s.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if !sc.visible(conv) {
		return nil, ErrInboxForbidden
	}
	return conv, nil
}

// ListScoped 带数据权限的会话列表（非超管强制 assigned_to=自己）
func (s *InboxService) ListScoped(ctx context.Context, q InboxQuery, sc InboxScope) ([]*model.InboxConversation, int64, error) {
	sc.applyScope(&q)
	return s.List(ctx, q)
}

// GetByIDScoped 带归属校验的会话详情
func (s *InboxService) GetByIDScoped(ctx context.Context, id uint, sc InboxScope) (*model.InboxConversation, error) {
	return s.ensureVisible(ctx, id, sc)
}

// GetStatsScoped 带数据权限的统计（非超管只统计 assigned_to=自己的会话）
func (s *InboxService) GetStatsScoped(ctx context.Context, sc InboxScope) (*InboxStats, error) {
	if !sc.restricted() || s.inboxRepo == nil {
		return s.GetStats(ctx)
	}
	validStatuses := []string{InboxStatusUnread, InboxStatusOpen, InboxStatusAssigned, InboxStatusClosed}
	activeStatuses := []string{InboxStatusAssigned, InboxStatusOpen}
	threshold := time.Now().Add(-InboxOverdueThreshold)
	res, err := s.inboxRepo.GetStatsByAssignedTo(ctx, validStatuses, activeStatuses, InboxFromCustomer, threshold, sc.Username)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &InboxStats{ByPlatform: map[string]int64{}, ByAssignedTo: map[string]int64{}}, nil
	}
	return &InboxStats{
		Total:        res.Total,
		Unread:       res.Unread,
		Open:         res.Open,
		Assigned:     res.Assigned,
		Closed:       res.Closed,
		ByPlatform:   res.ByPlatform,
		ByAssignedTo: res.ByAssignedTo,
		OverdueCount: res.OverdueCount,
	}, nil
}

// GetMessagesByConversationScoped 带归属校验的会话消息
func (s *InboxService) GetMessagesByConversationScoped(ctx context.Context, conversationID uint, page, pageSize int, sc InboxScope) ([]map[string]any, int64, error) {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return nil, 0, err
	}
	return s.GetMessagesByConversation(ctx, conversationID, page, pageSize)
}

// MarkReadScoped 带归属校验的已读标记
func (s *InboxService) MarkReadScoped(ctx context.Context, conversationID uint, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.MarkRead(ctx, conversationID)
}

// PinScoped 带归属校验的置顶
func (s *InboxService) PinScoped(ctx context.Context, conversationID uint, pinned bool, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.Pin(ctx, conversationID, pinned)
}

// StarScoped 带归属校验的星标
func (s *InboxService) StarScoped(ctx context.Context, conversationID uint, starred bool, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.Star(ctx, conversationID, starred)
}

// MuteScoped 带归属校验的静音
func (s *InboxService) MuteScoped(ctx context.Context, conversationID uint, muted bool, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.Mute(ctx, conversationID, muted)
}

// AddTagScoped 带归属校验的加标签
func (s *InboxService) AddTagScoped(ctx context.Context, conversationID uint, tag string, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.AddTag(ctx, conversationID, tag)
}

// RemoveTagScoped 带归属校验的删标签
func (s *InboxService) RemoveTagScoped(ctx context.Context, conversationID uint, tag string, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.RemoveTag(ctx, conversationID, tag)
}

// DeleteMessageScoped 带归属校验的删消息
func (s *InboxService) DeleteMessageScoped(ctx context.Context, conversationID, messageID uint, source string, sc InboxScope) error {
	if _, err := s.ensureVisible(ctx, conversationID, sc); err != nil {
		return err
	}
	return s.DeleteMessage(ctx, conversationID, messageID, source)
}

// InboxAgent 可分配坐席的脱敏视图：只有分配下拉框要用的四列。
type InboxAgent struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	RealName string `json:"real_name"`
	Role     string `json:"role"`
}

// ListAssignableAgents 列出能接手会话的在职坐席（客服／员工两档）。
//
// 坐席选择器原先打的是 /api/system/users：那是仅超管口，非超管永远 403，而收件箱工作台
// 是可以授予坐席的——于是"页面打得开、分配下拉永远空"。这里单独出一条口，只出四列
// （不含 email/phone/data_scope/口令形态这类只有账号管理页才该看见的东西），
// 且只出 enabled 且 status=1 的行：停用账号继续出现在候选名单里，等于把会话分配给没人接的坐席。
//
// AV-04c 批次6：agent_statuses⋈system_users 的四列投影查询下沉
// InboxConversationRepository.ListAssignableAgents（SQL 逐字保留），本层只做
// 脱敏视图搬运；controller 响应契约（json 字段、空名单为 null）不变。
func (s *InboxService) ListAssignableAgents(ctx context.Context) ([]InboxAgent, error) {
	if s.inboxRepo == nil {
		// 与其余读路径同一套"无库回空"语义（全局句柄未注入时不报错、不查库）。
		return nil, nil
	}
	// 口径修正（2026-11-04 T1 线上验收 BUG-2）：名单从「角色白名单」改为
	// 「有坐席档案的在职账号」。原白名单（customer_service/staff 两档）在 SaaS
	// 新租户上永远为空——租户唯一账号是 admin 角色，而坐席档案（agent_statuses）
	// 在注册/首次上线时就已建好。按档案 join 后：admin 老板兼客服出现在名单里；
	// 反过来，仅账号域的 admin（无坐席档案）依旧不会混进脱敏投影。
	rows, err := s.inboxRepo.ListAssignableAgents(ctx)
	if err != nil {
		return nil, err
	}
	var agents []InboxAgent
	for _, row := range rows {
		agents = append(agents, InboxAgent{
			ID:       row.ID,
			Username: row.Username,
			RealName: row.RealName,
			Role:     row.Role,
		})
	}
	return agents, nil
}
