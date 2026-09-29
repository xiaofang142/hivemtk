package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// WeComIntegrationService 企微与消息中台/统一收件箱集成
type WeComIntegrationService struct {
	repo      *repository.WeComAccountRepository
	hub       *MessageHubService
	inbox     *InboxService
	healthSvc *WeComAccountHealthService
	wecom     *WeComService
}

// NewWeComIntegrationService 创建集成服务
func NewWeComIntegrationService(db *gorm.DB) *WeComIntegrationService {
	repo := repository.NewWeComAccountRepository()
	if db != nil {
		repo.SetDB(context.Background(), db)
	}
	return &WeComIntegrationService{
		repo:      repo,
		hub:       NewMessageHubServiceWithDB(db, nil),
		inbox:     NewInboxServiceWithDB(db),
		healthSvc: NewWeComAccountHealthService(db),
		wecom:     NewWeComServiceWithDB(db),
	}
}

// IngestRequest 接入请求
type IngestRequest struct {
	AccountID      uint      `json:"account_id"`
	ExternalUserID string    `json:"external_user_id"`
	Name           string    `json:"name"`
	MsgType        string    `json:"msg_type"`
	Content        string    `json:"content"`
	MediaURL       string    `json:"media_url"`
	MsgID          string    `json:"msg_id"`
	ConversationID string    `json:"conversation_id"`
	IsGroup        bool      `json:"is_group"`
	GroupID        string    `json:"group_id"`
	ReceivedAt     time.Time `json:"received_at"`
}

// IngestMessage 将企微消息接入消息中台与统一收件箱
func (s *WeComIntegrationService) IngestMessage(ctx context.Context, req *IngestRequest) (*model.MessageHub, *model.InboxConversation, error) {
	if s.repo == nil {
		return nil, nil, fmt.Errorf("db is nil")
	}
	if req.AccountID == 0 || req.ExternalUserID == "" {
		return nil, nil, fmt.Errorf(" account_id, external_user_id are required")
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}
	if req.ReceivedAt.IsZero() {
		req.ReceivedAt = time.Now()
	}
	if req.MsgID == "" {
		req.MsgID = fmt.Sprintf("wecom-%s", uuid.NewString())
	}
	if req.ConversationID == "" {
		req.ConversationID = fmt.Sprintf("wecom-%d-%s", req.AccountID, req.ExternalUserID)
	}

	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{

		Platform:       "wecom",
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          req.MsgID,
		Direction:      "inbound",
		MsgType:        req.MsgType,
		SenderID:       req.ExternalUserID,
		SenderName:     req.Name,
		ReceiverID:     fmt.Sprintf("%d", req.AccountID),
		Content:        req.Content,
		MediaURL:       req.MediaURL,
		ConversationID: req.ConversationID,
		IsGroup:        req.IsGroup,
		GroupID:        req.GroupID,
		SentAt:         &req.ReceivedAt,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("hub push: %w", err)
	}

	var conv *model.InboxConversation
	if hubMsg != nil {
		conv, err = s.inbox.UpsertFromHubMessage(ctx, hubMsg)
		if err != nil {
			return hubMsg, nil, fmt.Errorf("inbox upsert: %w", err)
		}
	}

	_ = s.healthSvc.ConsumeQuota(ctx, req.AccountID, 0)

	return hubMsg, conv, nil
}

// WeComSendRequest 发送消息请求
type WeComSendRequest struct {
	AccountID      uint   `json:"account_id"`
	ExternalUserID string `json:"external_user_id"`
	MsgType        string `json:"msg_type"`
	Content        string `json:"content"`
	MediaID        string `json:"media_id"`
	IsAIReply      bool   `json:"is_ai_reply"`
	AIAgent        string `json:"ai_agent"`
}

// SendMessage 通过消息中台发送并写入收件箱
func (s *WeComIntegrationService) SendMessage(ctx context.Context, req *WeComSendRequest) (*model.MessageHub, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if false || req.AccountID == 0 || req.ExternalUserID == "" {
		return nil, fmt.Errorf(" account_id, external_user_id are required")
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}

	acc, err := s.healthSvc.SelectHealthyAccount(ctx)
	if err != nil || acc == nil {
		return nil, ErrWeComAccountNotFound
	}
	if acc.LoginState == WeComLoginBanned {
		return nil, ErrWeComAccountBanned
	}
	if acc.DailyMsgUsed+1 > acc.DailyMsgQuota {
		return nil, ErrWeComQuotaExceeded
	}

	convID := fmt.Sprintf("wecom-%d-%s", req.AccountID, req.ExternalUserID)

	now := time.Now()
	// 这里不声明投递状态：本函数是「先落库、后发送」的形状，且发送段在 WECOM_DISABLE_OUTBOUND
	// 或缺 CorpID/CorpSecret 时整块跳过。落库即写 delivered 等于把没发出去的消息记成已送达。
	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{

		Platform:       "wecom",
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          fmt.Sprintf("wecom-out-%s", uuid.NewString()),
		Direction:      "outbound",
		MsgType:        req.MsgType,
		SenderID:       fmt.Sprintf("%d", req.AccountID),
		ReceiverID:     req.ExternalUserID,
		Content:        req.Content,
		MediaURL:       req.MediaID,
		ConversationID: convID,
		IsAIReply:      req.IsAIReply,
		AIAgent:        req.AIAgent,
		SentAt:         &now,
	})
	if err != nil {
		return nil, fmt.Errorf("hub push: %w", err)
	}

	if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
		return hubMsg, fmt.Errorf("inbox upsert: %w", err)
	}

	disableOutbound := os.Getenv("WECOM_DISABLE_OUTBOUND") == "1" ||
		(os.Getenv("IS_TEST_MODE") == "1" && os.Getenv("WECOM_ALLOW_OUTBOUND") != "1")
	if !disableOutbound {
		if wa, err := s.repo.GetByID(ctx, req.AccountID); err == nil && wa != nil && wa.CorpID != "" && wa.CorpSecret != "" {
			if _, serr := s.wecom.SendMessage(ctx, wa, &WeComSendMessageRequest{
				ToUser:  req.ExternalUserID,
				MsgType: req.MsgType,
				Content: req.Content,
				MediaID: req.MediaID,
			}); serr != nil {
				return hubMsg, fmt.Errorf("wecom outbound api: %w", serr)
			}
		}
	}

	_ = s.healthSvc.ConsumeQuota(ctx, req.AccountID, 1)

	return hubMsg, nil
}

// UpdateAccountStatus 更新账号状态（如：登录/掉线/封禁）
func (s *WeComIntegrationService) UpdateAccountStatus(ctx context.Context, accountID uint, loginState, risk string) error {
	if s.repo == nil {
		return nil
	}
	updates := map[string]any{
		"login_state":    loginState,
		"last_active_at": time.Now(),
	}
	if risk != "" {
		updates["risk_level"] = risk
	}
	if loginState == WeComLoginBanned {
		updates["risk_level"] = WeComRiskBanned
		updates["weight"] = 0
	} else if loginState == WeComLoginOffline {
		updates["weight"] = 50
	}
	return s.repo.UpdateFields(ctx, accountID, updates)
}

// AccountWithHealth 账号+健康度组合返回
type AccountWithHealth struct {
	Account *model.WeComAccount       `json:"account"`
	Health  *model.WeComAccountHealth `json:"health,omitempty"`
}

// ListAccountsWithHealth 列出账号并附带最新健康度
func (s *WeComIntegrationService) ListAccountsWithHealth(ctx context.Context) ([]AccountWithHealth, error) {
	if s.repo == nil {
		return nil, nil
	}
	accounts, err := s.repo.ListAllOrderByIDDesc(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountWithHealth, 0, len(accounts))
	for i := range accounts {
		health, _ := s.healthSvc.GetLatestHealth(ctx, accounts[i].ID)
		out = append(out, AccountWithHealth{
			Account: &accounts[i],
			Health:  health,
		})
	}
	return out, nil
}

// ReceiveCallbackRequest 企微回调请求（webhook 入口）
type ReceiveCallbackRequest struct {
	AccountID uint   `json:"account_id"`
	FromUser  string `json:"from_user"`
	FromName  string `json:"from_name"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
	MsgID     string `json:"msg_id"`
	MediaID   string `json:"media_id"`
	ChatID    string `json:"chat_id"`
	ChatType  string `json:"chat_type"`
}

// ReceiveCallback 处理企微回调
func (s *WeComIntegrationService) ReceiveCallback(ctx context.Context, req *ReceiveCallbackRequest) (*model.MessageHub, *model.InboxConversation, error) {
	convID := req.ChatID
	if convID == "" {
		convID = fmt.Sprintf("wecom-%d-%s", req.AccountID, req.FromUser)
	}
	ingestReq := &IngestRequest{

		AccountID:      req.AccountID,
		ExternalUserID: req.FromUser,
		Name:           req.FromName,
		MsgType:        req.MsgType,
		Content:        req.Content,
		MediaURL:       req.MediaID,
		MsgID:          req.MsgID,
		ConversationID: convID,
		IsGroup:        strings.EqualFold(req.ChatType, "group"),
		GroupID:        req.ChatID,
		ReceivedAt:     time.Now(),
	}
	return s.IngestMessage(ctx, ingestReq)
}
