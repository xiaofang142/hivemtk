package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// QQService QQ 机器人账号 CRUD 服务
type QQService struct {
	accRepo *repository.QQAccountRepository
}

// NewQQService 创建 QQ 账号服务
func NewQQService(db *gorm.DB) *QQService {
	r := repository.NewQQAccountRepository()
	if db != nil {
		r.SetDB(context.Background(), db)
	}
	return &QQService{accRepo: r}
}

// CreateAccount 创建账号
func (s *QQService) CreateAccount(ctx context.Context, acc *model.QQAccount) (*model.QQAccount, error) {
	if acc.AccountName == "" || acc.AppID == "" || acc.AppSecret == "" {
		return nil, errors.New("account_name, app_id and app_secret are required")
	}
	if acc.Status == 0 {
		acc.Status = 1
	}
	if err := s.accRepo.Create(ctx, acc); err != nil {
		return nil, err
	}
	return acc, nil
}

// GetAccount 查询账号
func (s *QQService) GetAccount(ctx context.Context, id uint) (*model.QQAccount, error) {
	return s.accRepo.GetByID(ctx, id)
}

// ListAccounts 列出账号
func (s *QQService) ListAccounts(ctx context.Context) ([]*model.QQAccount, error) {
	return s.accRepo.GetAll(ctx)
}

// UpdateAccount 更新账号
func (s *QQService) UpdateAccount(ctx context.Context, acc *model.QQAccount) error {
	return s.accRepo.Update(ctx, acc)
}

// DeleteAccount 删除账号
func (s *QQService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accRepo.Delete(ctx, id)
}

// getWebhookSecret 取账号的 webhook 验签 secret（供 WebhookService.Verify 使用）
func (s *QQService) getWebhookSecret(ctx context.Context, accountID string) string {
	if s.accRepo == nil {
		return ""
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	acc, err := s.accRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return ""
	}
	return acc.WebhookSecret
}

// VerifyQQCallbackChallenge Op13 回调地址验证：用派生私钥签名 event_ts + plain_token。
// 由 webhook controller 在验签阶段同步调用，直接返回官方要求的应答 JSON。
func (s *QQService) VerifyQQCallbackChallenge(ctx context.Context, accountID string, e *qq.Event) (plainToken, signature string, ok bool) {
	secret := s.getWebhookSecret(ctx, accountID)
	if secret == "" || e.PlainToken == "" {
		return "", "", false
	}
	sig, err := qq.GenerateCallbackTestSignature(secret, e.EventTS, e.PlainToken)
	if err != nil {
		logger.Errorf("[QQ] op13 callback verify failed account=%s: %v", accountID, err)
		return "", "", false
	}
	return e.PlainToken, sig, true
}

// ---------------------------------------------------------------------------
// QQIntegrationService 入站 Ingest + 出站 SendMessage
// ---------------------------------------------------------------------------

// QQIntegrationService QQ 消息集成服务（与 TelegramIntegrationService 同构）
type QQIntegrationService struct {
	qqSvc *QQService
	hub   *MessageHubService
	inbox *InboxService

	// msgSeqMu/msgSeqMap 同一 msg_id 的被动回复 msg_seq 递增（QQ 被动回复 5 分钟限 5 条，
	// 同一 msg_id 重发必须递增 msg_seq，否则平台丢弃）
	msgSeqMu  sync.Mutex
	msgSeqMap map[string]int
}

// NewQQIntegrationService 创建 QQ 集成服务
func NewQQIntegrationService(db *gorm.DB) *QQIntegrationService {
	return &QQIntegrationService{
		qqSvc:     NewQQService(db),
		hub:       NewMessageHubServiceWithDB(db, nil),
		inbox:     NewInboxServiceWithDB(db),
		msgSeqMap: make(map[string]int),
	}
}

// QQIngestRequest 入站消息落库请求
type QQIngestRequest struct {
	AccountID uint
	ConvID    string // 群 openid / 单聊 openid
	SenderID  string
	MsgID     string
	Content   string
	IsGroup   bool
}

// IngestMessage 入站消息落 message_hub + upsert inbox（与 TG IngestMessage 同构）
func (s *QQIntegrationService) IngestMessage(ctx context.Context, req *QQIngestRequest) (*model.MessageHub, error) {
	if req.MsgID == "" {
		return nil, errors.New("qq ingest: empty msg id")
	}
	hubMsg, err := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       model.ChannelQQ,
		AccountID:      fmt.Sprintf("%d", req.AccountID),
		MsgID:          req.MsgID,
		Direction:      "inbound",
		MsgType:        model.MsgTypeText,
		SenderID:       req.SenderID,
		Content:        req.Content,
		ConversationID: req.ConvID,
		IsGroup:        req.IsGroup,
		GroupID:        req.ConvID,
		SentAt:         timePtr(time.Now()),
	})
	if err != nil {
		return nil, fmt.Errorf("qq hub push: %w", err)
	}
	if hubMsg != nil && s.inbox != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[QQ] upsert inbox failed conv=%s: %v", req.ConvID, err)
		}
	}
	return hubMsg, nil
}

// nextMsgSeq 同一 msg_id 的 msg_seq 自增
func (s *QQIntegrationService) nextMsgSeq(msgID string) int {
	if msgID == "" {
		return 1 + int(time.Now().UnixNano()%1000) // 主动消息随机 seq
	}
	s.msgSeqMu.Lock()
	defer s.msgSeqMu.Unlock()
	s.msgSeqMap[msgID]++
	return s.msgSeqMap[msgID]
}

// qqAPIBaseOverride 测试/代理环境覆盖官方 API 域名（QQ_API_BASE_URL）。
// 生产留空走官方 https://api.bot.qq.com；测试指向 httptest 模拟平台。
func qqAPIBaseOverride() string {
	return strings.TrimSpace(os.Getenv("QQ_API_BASE_URL"))
}

// SendMessage 出站发送 AI 回复。
// convID 为群 openid（群聊回复）或单聊 openid；msgID 为被回复的原消息 ID（被动回复关联）。
func (s *QQIntegrationService) SendMessage(ctx context.Context, accountID uint, convID, msgID, content string) error {
	acc, err := s.qqSvc.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get qq account: %w", err)
	}
	opts := []core.ClientOption{core.WithHTTPClient(httpclient.Client)}
	if base := qqAPIBaseOverride(); base != "" {
		opts = append(opts, core.WithBaseURL(base))
	}
	cli := qq.NewClient(acc.AppID, acc.AppSecret, opts...)
	target := qq.SendTarget{MsgID: msgID, MsgSeq: s.nextMsgSeq(msgID)}
	if isQQGroupConversation(convID) {
		target.GroupOpenID = convID
	} else {
		target.UserOpenID = convID
	}
	if _, err := cli.SendMessage(ctx, target, content); err != nil {
		now := time.Now()
		acc.LastErrorAt = &now
		acc.LastErrorMsg = err.Error()
		_ = s.qqSvc.UpdateAccount(ctx, acc)
		return fmt.Errorf("send qq msg: %w", err)
	}
	// 成功后记录出站消息到 message_hub + inbox（与 TG 出站落库一致）
	outMsgID := "qq_out_" + fmt.Sprintf("%d_%d", accountID, time.Now().UnixNano())
	hubMsg, _ := s.hub.Push(ctx, &PushMessageRequest{
		Platform:       model.ChannelQQ,
		AccountID:      fmt.Sprintf("%d", accountID),
		MsgID:          outMsgID,
		Direction:      "outbound",
		MsgType:        model.MsgTypeText,
		SenderID:       fmt.Sprintf("%d", accountID),
		ReceiverID:     convID,
		Content:        content,
		ConversationID: convID,
		IsGroup:        isQQGroupConversation(convID),
		GroupID:        convID,
		IsAIReply:      true,
		AIAgent:        "sales_engine",
		SentAt:         timePtr(time.Now()),
	})
	if hubMsg != nil && s.inbox != nil {
		if _, err := s.inbox.UpsertFromHubMessage(ctx, hubMsg); err != nil {
			logger.Warnf("[QQ] upsert outbound inbox failed conv=%s: %v", convID, err)
		}
	}
	return nil
}

// isQQGroupConversation 判断会话 ID 是否为群 openid。
// 群 openid 以 "G" 开头、单聊/用户 openid 以 "C"/其他开头（官方命名惯例，仅作启发式）。
func isQQGroupConversation(convID string) bool {
	return len(convID) > 0 && (convID[0] == 'G')
}

// GenQQWebhookSecret 生成随机 webhook secret 备用（官方 BotSecret 由管理端下发，本函数仅测试用）
func GenQQWebhookSecret() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
