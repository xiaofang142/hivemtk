package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/websocket"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CustomerSessionService 客服会话服务
type CustomerSessionService struct {
	sessionRepo    *repository.CustomerSessionRepository
	messageRepo    *repository.SessionMessageRepository
	agentRepo      *repository.AgentStatusRepository
	suggestionRepo *repository.AISuggestionRepository
	blacklistRepo  *repository.UserBlacklistRepository
}

// CustomerSessionActiveTTL 客服会话活跃 TTL（与 repository.DefaultSessionActiveTTL 保持一致）
//
// 24h 内有消息互动的会话视为「活跃」；超过 24h 自动关闭（由 AutoCloseStaleSessions
// 定时任务驱动）。这是 GetActiveByUserID 的隐含语义边界。
//
// 单一源：repository.DefaultSessionActiveTTL；调整需同步更新两边。
const CustomerSessionActiveTTL = repository.DefaultSessionActiveTTL

const autoCloseStaleBatchSize = 500

// AutoCloseStaleSessions 自动关闭超过 TTL 的活跃会话
//
// 设计：
//   - 定时任务每小时跑一次（启动期由 system_init.go 注册）
//   - 单批最多 500 条；分批 UPDATE 避免长时间锁表
//   - 不返回 error（仅日志）；任务失败不阻塞下一次调度
//
// 行为：
//   - last_message_at 为空 → 用 created_at 作为活跃度判断依据（COALESCE）
//   - 只关闭「活跃」状态（pending/ai_handling/waiting/human_handling）
//   - 已 resolved/closed 的会话不会被重复关闭
func (s *CustomerSessionService) AutoCloseStaleSessions(ctx context.Context) (int64, error) {
	total, err := s.sessionRepo.AutoCloseStaleSessions(ctx, CustomerSessionActiveTTL, autoCloseStaleBatchSize)
	if err != nil {
		return 0, err
	}
	if total > 0 {
		logger.Infof("auto-close stale sessions: closed %d (TTL=%s)", total, CustomerSessionActiveTTL)
	}
	return total, nil
}

// NewCustomerSessionService 创建客服会话服务实例
func NewCustomerSessionService() *CustomerSessionService {
	return &CustomerSessionService{
		sessionRepo:    repository.NewCustomerSessionRepository(),
		messageRepo:    repository.NewSessionMessageRepository(),
		agentRepo:      repository.NewAgentStatusRepository(),
		suggestionRepo: repository.NewAISuggestionRepository(),
		blacklistRepo:  repository.NewUserBlacklistRepository(),
	}
}

// NewCustomerSessionServiceWithDB 创建指定数据库连接的客服会话服务实例
//
// 用于：① 测试注入独立真实测试库（testutil.NewTestDB）② 多库/独立事务场景。
// 避免依赖全局数据库句柄，保证 reach.web.send 等触达链路使用调用方真实 DB。
func NewCustomerSessionServiceWithDB(db *gorm.DB) *CustomerSessionService {
	return &CustomerSessionService{
		sessionRepo:    repository.NewCustomerSessionRepositoryWithDB(db),
		messageRepo:    repository.NewSessionMessageRepositoryWithDB(db),
		agentRepo:      repository.NewAgentStatusRepositoryWithDB(db),
		suggestionRepo: repository.NewAISuggestionRepositoryWithDB(db),
		blacklistRepo:  repository.NewUserBlacklistRepositoryWithDB(db),
	}
}

// CreateSessionRequest 创建会话请求
type CreateSessionRequest struct {
	Platform   model.Platform `json:"platform" binding:"required"`
	AccountID  string         `json:"account_id" binding:"required"`
	UserID     string         `json:"user_id" binding:"required"`
	OneID      string         `json:"one_id"`
	UserName   string         `json:"user_name"`
	UserAvatar string         `json:"user_avatar"`
	UserPhone  string         `json:"user_phone"`
	UserEmail  string         `json:"user_email"`
}

// CreateSession 创建会话
//
// 进入时先校验 IsUserBlacklisted(userID, platform)，
// 命中即拒绝创建（避免已被拉黑的访客通过新会话绕过黑名单）。
//
// 具体校验逻辑见 customer_session_blacklist.go 中的 preCreateBlacklistGuard。
func (s *CustomerSessionService) CreateSession(ctx context.Context, req *CreateSessionRequest) (*model.CustomerSession, error) {
	if err := s.preCreateBlacklistGuard(ctx, req); err != nil {
		return nil, err
	}

	sessionID := generateSessionID()

	session := &model.CustomerSession{
		SessionID:  sessionID,
		Platform:   req.Platform,
		AccountID:  req.AccountID,
		UserID:     req.UserID,
		OneID:      req.OneID,
		UserName:   req.UserName,
		UserAvatar: req.UserAvatar,
		UserPhone:  req.UserPhone,
		UserEmail:  req.UserEmail,
		Status:     model.SessionStatusPending,
		Priority:   0,
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	MaybeSendAwayReply(session.SessionID, session.SessionID, string(session.Platform), session.AccountID)

	DispatchSessionEventAsync(RuleEventConversationCreated, session.SessionID, session)

	return session, nil
}

// GetSessions 获取会话列表
func (s *CustomerSessionService) GetSessions(ctx context.Context, status model.SessionStatus, page, pageSize int) ([]*model.CustomerSession, int64, error) {
	return s.sessionRepo.GetByMerchant(ctx, status, page, pageSize)
}

// GetPendingSessions 获取待处理会话（pending / AI handling / waiting）
func (s *CustomerSessionService) GetPendingSessions(ctx context.Context, page, pageSize int) ([]*model.CustomerSession, error) {
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
	var sessions []*model.CustomerSession
	if err := s.sessionRepo.GetDB(ctx).Where("status IN ?", statuses).
		Order("priority DESC, COALESCE(last_message_at, created_at) ASC").
		Offset(offset).Limit(pageSize).Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

// CountPendingSessions 统计待处理会话数
func (s *CustomerSessionService) CountPendingSessions(ctx context.Context) (int64, error) {
	statuses := []model.SessionStatus{
		model.SessionStatusPending,
		model.SessionStatusAIHandling,
		model.SessionStatusWaiting,
	}
	var count int64
	if err := s.sessionRepo.GetDB(ctx).Model(&model.CustomerSession{}).
		Where("status IN ?", statuses).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// GetSessionByID 获取会话详情
func (s *CustomerSessionService) GetSessionByID(ctx context.Context, id uint) (*model.CustomerSession, error) {
	session, err := s.sessionRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// SendMessageRequest 发送消息请求
type SendMessageRequest struct {
	SessionID    string            `json:"session_id"`
	Content      string            `json:"content" binding:"required"`
	ContentType  model.MessageType `json:"content_type"`
	MediaURL     string            `json:"media_url"`
	SenderType   string            `json:"sender_type"`
	SenderID     string            `json:"sender_id"`
	SenderName   string            `json:"sender_name"`
	SenderAvatar string            `json:"sender_avatar"`
	AIConfidence float64           `json:"ai_confidence"`
	AISource     string            `json:"ai_source"`
}

// UnmarshalJSON 自定义反序列化，兼容 sender_id 为数字或字符串
func (r *SendMessageRequest) UnmarshalJSON(data []byte) error {

	type Alias SendMessageRequest
	aux := &struct {
		SenderID interface{} `json:"sender_id"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	switch v := aux.SenderID.(type) {
	case float64:
		r.SenderID = fmt.Sprintf("%d", int64(v))
	case string:
		r.SenderID = v
	}
	return nil
}

// SendMessage 发送消息
//
// 状态校验: 只有 pending/ai_handling/human_handling/waiting 状态的会话可发送消息,
// resolved/closed 状态不允许追加消息, 防止历史会话被意外修改.
func (s *CustomerSessionService) SendMessage(ctx context.Context, req *SendMessageRequest) (*model.SessionMessage, error) {
	session, err := s.sessionRepo.GetBySessionID(ctx, req.SessionID)
	if err != nil {
		return nil, errors.New("会话不存在")
	}
	if !CustomerSessionCanSendMessage(session) {
		return nil, fmt.Errorf("会话状态 %s 不允许发送消息", session.Status)
	}

	message := &model.SessionMessage{
		SessionID:    req.SessionID,
		Content:      req.Content,
		ContentType:  req.ContentType,
		MediaURL:     req.MediaURL,
		SenderType:   req.SenderType,
		SenderID:     req.SenderID,
		SenderName:   req.SenderName,
		SenderAvatar: req.SenderAvatar,
		AIConfidence: req.AIConfidence,
		AISource:     req.AISource,
	}

	if req.ContentType == "" {
		message.ContentType = model.MessageTypeText
	}

	if err := s.messageRepo.Create(ctx, message); err != nil {
		return nil, err
	}

	if err := s.sessionRepo.UpdateLastMessage(ctx, session.ID, req.Content, req.SenderType); err != nil {
		return nil, err
	}

	if req.SenderType == "ai" {
		if e := s.sessionRepo.IncrementAIReplyCount(ctx, session.ID); e != nil {
			logger.Warnf("[Session] AI 回复计数失败 sessionID=%d: %v", session.ID, e)
		}
	} else if req.SenderType == "agent" {
		if e := s.sessionRepo.IncrementHumanReplyCount(ctx, session.ID); e != nil {
			logger.Warnf("[Session] 人工回复计数失败 sessionID=%d: %v", session.ID, e)
		}

		if session.HandoffAt != nil && session.FirstHumanReplyAt == nil {
			now := time.Now()
			if err := s.sessionRepo.UpdateFirstHumanReplyAt(ctx, session.ID, now); err != nil {

			} else {
				session.FirstHumanReplyAt = &now
			}
		}
		if session.AgentID > 0 {
			if e := s.agentRepo.IncrementTodayMessages(ctx, session.AgentID); e != nil {
				logger.Warnf("[Session] 坐席今日消息计数失败 agentID=%d: %v", session.AgentID, e)
			}
		}
	}

	if session.AgentID > 0 && req.SenderType == "user" {
		if e := websocket.NotifyNewMessage(strconv.FormatUint(uint64(session.AgentID), 10), message); e != nil {
			logger.Warnf("[Session] 新消息推送坐席失败 agentID=%d: %v", session.AgentID, e)
		}
	}

	if req.SenderType == "agent" || req.SenderType == "ai" {
		s.pushToVisitor(ctx, session.SessionID, message)
	}

	return message, nil
}

func (s *CustomerSessionService) pushToVisitor(ctx context.Context, sessionID string, message *model.SessionMessage) {
	payload := map[string]any{
		"session_id":   sessionID,
		"id":           message.ID,
		"content":      message.Content,
		"content_type": message.ContentType,
		"media_url":    message.MediaURL,
		"sender_type":  message.SenderType,
		"sender_name":  message.SenderName,
		"sender_id":    message.SenderID,
		"created_at":   message.CreatedAt,
	}
	if message.ContentType == model.MessageTypeCard && message.CardData != "" {
		if card, err := model.UnmarshalRichCard(message.CardData); err == nil {
			payload["card"] = card
		}
	}
	if websocket.IsVisitorOnline(sessionID) {
		_ = websocket.SendToVisitor(websocket.TypeMessage, payload, sessionID)
		now := time.Now()
		_ = s.sessionRepo.GetDB(ctx).Model(&model.SessionMessage{}).
			Where("id = ?", message.ID).Update("delivered_at", &now).Error
	}
}

// GetMessages 获取会话消息列表
func (s *CustomerSessionService) GetMessages(ctx context.Context, sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error) {
	session, err := s.sessionRepo.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, 0, errors.New("会话不存在")
	}
	_ = session

	return s.messageRepo.GetBySessionID(ctx, sessionID, page, pageSize)
}

// UpdateSessionStatus 更新会话状态
//
// 幂等保护: 对已处于目标状态的会话重复调用不会产生副作用
// (如 agent active_sessions 不会被重复扣减)
func (s *CustomerSessionService) UpdateSessionStatus(ctx context.Context, sessionID uint, status model.SessionStatus) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("会话不存在")
	}

	if err := s.sessionRepo.UpdateStatus(ctx, sessionID, status); err != nil {
		return err
	}

	if (status == model.SessionStatusResolved || status == model.SessionStatusClosed) &&
		session.AgentID > 0 && session.Status != status {
		if e := s.agentRepo.DecrementActiveSessions(ctx, session.AgentID); e != nil {
			logger.Warnf("[Session] 坐席活跃会话计数扣减失败 agentID=%d: %v", session.AgentID, e)
		}
	}

	if session.AgentID > 0 && session.Status != status {
		if e := websocket.NotifySessionUpdate(strconv.FormatUint(uint64(session.AgentID), 10), map[string]any{
			"session_id": session.SessionID,
			"status":     status,
		}); e != nil {
			logger.Warnf("[Session] 会话状态推送坐席失败 agentID=%d: %v", session.AgentID, e)
		}
	}

	if (status == model.SessionStatusResolved || status == model.SessionStatusClosed) && session.Status != status {
		// 统一待办收口（T-P3-03）：会话都结束了，池子里那条"这条会话等人工"必须跟着落定。
		cancelOpenHumanTaskForSession(ctx, session.SessionID, status)

		chain := NewSessionChainServiceFromGlobal()
		chain.TriggerCSATOnClose(session)
		DispatchSessionEventAsync(RuleEventSessionResolved, session.SessionID, session)
	}

	return nil
}

// cancelOpenHumanTaskForSession 会话结束时撤销它那条开放的人工待办（非致命旁路）。
//
// 三处刻意的取舍：
//   - 底座未装配（全局服务为 nil）时什么都不做、也不告警：那是本卡天然的关闸形态，
//     每关一次会话喊一行 WARN 会把日志刷成噪声；
//   - 只撤销开放那一条（服务侧按 subject 定位 + 跃迁判据），已 done 的原样留着 —— 坐席
//     工作量与"这轮转人工处理了多久"都读那条记录，覆盖掉就等于把历史改了；
//   - 失败只 Warn、不改变本函数返回值：会话状态此刻已经落库，撤销失败不能让它退回
//     "没结束"，而漏掉的那一行坐席在收件箱里手动撤销即可（/cancel 端点已可用）。
func cancelOpenHumanTaskForSession(ctx context.Context, sessionID string, status model.SessionStatus) {
	svc := GlobalHumanTaskService()
	if svc == nil || sessionID == "" {
		return
	}
	what := "结束"
	if status == model.SessionStatusClosed {
		what = "关闭"
	}
	if _, _, err := svc.CancelOpenBySubject(ctx, HumanTaskSubjectCustomerSession, sessionID,
		"系统自动撤销：会话已"+what); err != nil {
		logger.Warnf("[Session] 撤销人工待办失败 session_id=%s: %v", sessionID, err)
	}
}

// RateSession 评价会话
func (s *CustomerSessionService) RateSession(ctx context.Context, sessionID uint, rating int, comment string) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("会话不存在")
	}

	session.Rating = rating
	session.RatingComment = comment
	return s.sessionRepo.Update(ctx, session)
}

// TagSession 标记会话
func (s *CustomerSessionService) TagSession(ctx context.Context, sessionID uint, tags []string) error {
	session, err := s.sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return errors.New("会话不存在")
	}

	tagsJSON, _ := json.Marshal(tags)
	session.Tags = string(tagsJSON)
	return s.sessionRepo.Update(ctx, session)
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d_%s", time.Now().UnixNano(), uuid.New().String()[:8])
}

// CustomerSessionCanSendMessage 会话当前状态是否允许发送消息
// 领域判断自 (*model.CustomerSession).CanSendMessage 迁入。
func CustomerSessionCanSendMessage(s *model.CustomerSession) bool {
	switch s.Status {
	case model.SessionStatusPending, model.SessionStatusAIHandling, model.SessionStatusHumanHandling, model.SessionStatusWaiting:
		return true
	default:
		return false
	}
}

func DispatchSessionEventAsync(event, sessionID string, session *model.CustomerSession) {
	// 引擎必须在 spawning 之前构造：NewRuleEngineService 一路要读 pkg/db 的包级全局句柄十余次
	// （repository.NewAutomationRuleRepository、NewCustomerServicePlusService 名下 8 个子仓储等），
	// 放进下面的异步体就会与"另一条用例改写全局句柄"的 db.SetTestDB() 撞在同一地址上——
	// 整包 -race 实测到 TestCreateSession_AllowDifferentPlatform / TestCreateSession_AnonymousUser
	// 两条红（单跑不复现）。同形状先例见 session_chain.go:TriggerCSATOnClose、
	// objection_handler.go:fallbackVersionOf；回归探针见 async_db_handle_probe_test.go。
	engine := NewRuleEngineServiceFromGlobal()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), utils.DefaultHTTPTimeout)
		defer cancel()
		engine.DispatchWithText(ctx, event, sessionID, "", session)
	}()
}
