package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ProactiveReachRequest 主动触达请求（按 OneID 智能选渠道）
type ProactiveReachRequest struct {
	// CustomerID 客户主键 ID（从线索/客户库中获取）
	CustomerID string `json:"customer_id,omitempty"`
	// OneID 客户 OneID（与 CustomerID 二选一）
	OneID string `json:"one_id,omitempty"`
	// Phone 手动指定手机号（最高优先级，绕过智能选渠道）
	Phone string `json:"phone,omitempty"`
	// Email 手动指定邮箱
	Email string `json:"email,omitempty"`
	// AccountID 渠道账号ID（用于出站发送）
	AccountID string `json:"account_id,omitempty"`
	// PreferredChannels 客户偏好渠道（按顺序）
	PreferredChannels []string `json:"preferred_channels,omitempty"`
	// Content 消息内容
	Content string `json:"content" binding:"required"`
	// Subject 邮件主题
	Subject string `json:"subject,omitempty"`
	// TemplateID 模板ID（邮件/短信/WA 模板）
	TemplateID string `json:"template_id,omitempty"`
	// Params 模板参数
	Params map[string]string `json:"params,omitempty"`
	// DryRun 试运行
	DryRun bool `json:"dry_run,omitempty"`
}

// ProactiveReachResponse 主动触达响应
type ProactiveReachResponse struct {
	// MessageID 消息ID
	MessageID string `json:"message_id"`
	// Channel 实际使用的渠道
	Channel string `json:"channel"`
	// RecipientID 实际接收方 ID
	RecipientID string `json:"recipient_id"`
	// AccountID 渠道账号ID
	AccountID string `json:"account_id"`
	// Status 状态
	Status string `json:"status"`
	// SentAt 发送时间
	SentAt time.Time `json:"sent_at"`
	// Strategy 选渠道策略说明（透明化）
	Strategy string `json:"strategy"`
}

type ProactiveReachService struct {
	repo          *repository.ProactiveReachRepository
	customerRepo  *customerRepo
	accountLookup AccountLookup

	dnc *DoNotContactService

	smsRegistry      func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error)
	emailRegistry    func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error)
	telegramRegistry func(ctx context.Context, accountID uint, chatID int64, content string) error
	whatsAppRegistry func(ctx context.Context, accountID uint, toPhone, content string) error
	weComRegistry    func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error)
	feishuRegistry   func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error
	dingTalkRegistry func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error)
	wechatRegistry   func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error)
}

// SetSMSRegistry 设置 SMS 发送器
func (s *ProactiveReachService) SetSMSRegistry(fn func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error)) {
	s.smsRegistry = fn
}

// SetEmailRegistry 设置 Email 发送器
func (s *ProactiveReachService) SetEmailRegistry(fn func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error)) {
	s.emailRegistry = fn
}

// SetTelegramRegistry 设置 Telegram 发送器
func (s *ProactiveReachService) SetTelegramRegistry(fn func(ctx context.Context, accountID uint, chatID int64, content string) error) {
	s.telegramRegistry = fn
}

// SetWhatsAppRegistry 设置 WhatsApp 发送器
func (s *ProactiveReachService) SetWhatsAppRegistry(fn func(ctx context.Context, accountID uint, toPhone, content string) error) {
	s.whatsAppRegistry = fn
}

// SetWeComRegistry 设置企微发送器
func (s *ProactiveReachService) SetWeComRegistry(fn func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error)) {
	s.weComRegistry = fn
}

// SetFeishuRegistry 设置飞书发送器
func (s *ProactiveReachService) SetFeishuRegistry(fn func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error) {
	s.feishuRegistry = fn
}

// SetDingTalkRegistry 设置钉钉发送器
func (s *ProactiveReachService) SetDingTalkRegistry(fn func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error)) {
	s.dingTalkRegistry = fn
}

// SetWechatRegistry 设置微信公众号发送器
func (s *ProactiveReachService) SetWechatRegistry(fn func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error)) {
	s.wechatRegistry = fn
}

// AccountLookup 渠道账号查找接口
type AccountLookup interface {
	// FindActiveAccount 查找某渠道下第一个 active 账号
	FindActiveAccount(ctx context.Context, channel string) (accountID string, err error)
}

// NewProactiveReachService 创建主动触达服务
func NewProactiveReachService(db *gorm.DB, lookup AccountLookup) *ProactiveReachService {
	if lookup == nil {
		lookup = &defaultAccountLookup{repo: repository.NewProactiveReachRepository(db)}
	}
	return &ProactiveReachService{
		repo:          repository.NewProactiveReachRepository(db),
		customerRepo:  newCustomerRepo(db),
		accountLookup: lookup,
		dnc:           NewDoNotContactService(nil),
	}
}

// SetDoNotContact 注入全局退订标志位服务（测试或自定义装配时使用）
func (s *ProactiveReachService) SetDoNotContact(dnc *DoNotContactService) {
	s.dnc = dnc
}

func (s *ProactiveReachService) dncService() *DoNotContactService {
	if s.dnc == nil {
		s.dnc = NewDoNotContactService(nil)
	}
	return s.dnc
}

// customerRepo 客户读取的进程内适配（gorm 收敛到 repository.ProactiveReachRepository）
type customerRepo struct {
	repo *repository.ProactiveReachRepository
}

func newCustomerRepo(db *gorm.DB) *customerRepo {
	return &customerRepo{repo: repository.NewProactiveReachRepository(db)}
}

func (r *customerRepo) GetByID(ctx context.Context, id string) (*model.Customer, error) {
	return r.repo.GetCustomerByID(ctx, id)
}

func (r *customerRepo) GetByUnifiedID(ctx context.Context, oneID string) (*model.Customer, error) {
	return r.repo.GetCustomerByUnifiedID(ctx, oneID)
}

// ReachByCustomer 按客户主键或 OneID 智能选择渠道发送
//
// 选渠道策略（不猜测）：
//  1. 显式指定 Phone/Email → 直发该渠道
//  2. 从 Customer 读取所有渠道身份字段，列出有完整身份的渠道
//  3. 客户偏好渠道（CustomerChannels.preferred_rank）排在最前
//  4. 默认顺序：SMS > Email > TG > WhatsApp > 企微 > 微信 > 飞书 > 抖音 > ...
//  5. 第一个有 active 账号 + 完整身份的渠道即为最终选择
//  6. 全部不可用 → 返回明确错误（带详细原因）
func (s *ProactiveReachService) ReachByCustomer(ctx context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error) {
	if req == nil || req.Content == "" {
		return nil, errors.New("content is required")
	}

	if req.Phone != "" {
		if s.checkDoNotContact(ctx, "", "sms", req.Phone) {
			return nil, fmt.Errorf("do-not-contact: phone %s has opted out globally, send skipped", req.Phone)
		}
		return s.sendSMS(ctx, req, req.Phone)
	}
	if req.Email != "" {
		if s.checkDoNotContact(ctx, "", "email", "email:"+NormalizeEmail(req.Email)) {
			return nil, fmt.Errorf("do-not-contact: email %s has opted out globally, send skipped", req.Email)
		}
		return s.sendEmail(ctx, req, req.Email)
	}

	customer, err := s.loadCustomer(ctx, req.CustomerID, req.OneID)
	if err != nil {
		return nil, fmt.Errorf("load customer: %w", err)
	}
	if customer == nil {
		return nil, errors.New("customer not found: provide customer_id, one_id, phone, or email")
	}

	available := CustomerAvailableChannels(customer, req.PreferredChannels)
	if len(available) == 0 {
		return nil, fmt.Errorf("customer %s has no channel identity on file, please bind at least one channel", customer.UnifiedID)
	}

	available = s.filterDoNotContactChannels(ctx, customer.UnifiedID, available)
	if len(available) == 0 {
		return nil, fmt.Errorf("do-not-contact: customer %s has opted out on all available channels", customer.UnifiedID)
	}

	preferred, _ := s.loadCustomerPreferredOrder(ctx, customer.UnifiedID, available)
	if len(preferred) > 0 {
		available = preferred
	}

	if req.DryRun {
		channel, recipient, accountID, err := s.pickChannelDryRun(available, customer)
		if err != nil {
			return nil, err
		}
		return &ProactiveReachResponse{
			MessageID:   "dry_run",
			Channel:     channel,
			RecipientID: recipient,
			AccountID:   accountID,
			Status:      "dry_run",
			SentAt:      time.Now(),
			Strategy:    fmt.Sprintf("available=[%s], picked=%s", strings.Join(available, ","), channel),
		}, nil
	}

	if !s.checkCooldown(ctx, customer.UnifiedID) {
		return nil, fmt.Errorf("cooldown: customer %s recently received a message, please wait", customer.UnifiedID)
	}

	channel, recipient, accountID, err := s.pickChannel(ctx, available, customer)
	if err != nil {
		return nil, err
	}

	switch channel {
	case "sms":
		return s.sendSMS(ctx, req, recipient)
	case "email":
		return s.sendEmail(ctx, req, recipient)
	case "telegram":
		return s.sendTelegram(ctx, req, recipient, accountID)
	case "whatsapp":
		return s.sendWhatsApp(ctx, req, recipient, accountID)
	case "wecom":
		return s.sendWeCom(ctx, req, recipient, accountID)
	case "wechat":
		return s.sendWeChat(ctx, req, recipient)
	case "feishu":
		return s.sendFeishu(ctx, req, recipient, accountID)
	case "douyin", "tiktok", "kuaishou", "xiaohongshu", "xianyu":
		return s.sendBridge(ctx, channel, req, recipient, accountID)
	case "dingtalk":
		return s.sendDingTalk(ctx, req, recipient)
	}

	return nil, fmt.Errorf("unsupported channel: %s", channel)
}

func (s *ProactiveReachService) loadCustomer(ctx context.Context, customerID, oneID string) (*model.Customer, error) {
	if s.customerRepo == nil {
		return nil, nil
	}
	if customerID != "" {
		return s.customerRepo.GetByID(ctx, customerID)
	}
	if oneID != "" {
		return s.customerRepo.GetByUnifiedID(ctx, oneID)
	}
	return nil, nil
}

// LoadCustomer 公开方法（供 controller 调用）
func (s *ProactiveReachService) LoadCustomer(ctx context.Context, customerID, oneID string) (*model.Customer, error) {
	return s.loadCustomer(ctx, customerID, oneID)
}

func (s *ProactiveReachService) loadCustomerPreferredOrder(ctx context.Context, oneID string, fallback []string) ([]string, error) {
	if s.repo == nil {
		return nil, nil
	}
	rows, err := s.repo.ListCustomerChannelsByOneID(ctx, oneID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ordered := make([]string, 0, len(rows))
	for _, r := range rows {
		for _, f := range fallback {
			if r.Channel == f {
				ordered = append(ordered, r.Channel)
				break
			}
		}
	}
	return ordered, nil
}

func (s *ProactiveReachService) pickChannelDryRun(candidates []string, customer *model.Customer) (channel, recipient, accountID string, err error) {
	for _, ch := range candidates {
		recipient = CustomerChannelIdentity(customer, ch)
		if recipient == "" {
			continue
		}
		if ch == "sms" || ch == "email" || ch == "dingtalk" {
			return ch, recipient, "", nil
		}
		return ch, recipient, "dry_run_account", nil
	}
	return "", "", "", fmt.Errorf("no channel identity for customer %s", customer.UnifiedID)
}

func (s *ProactiveReachService) pickChannel(ctx context.Context, candidates []string, customer *model.Customer) (channel, recipient, accountID string, err error) {
	var tried []string
	for _, ch := range candidates {
		tried = append(tried, ch)
		recipient = CustomerChannelIdentity(customer, ch)
		if recipient == "" {
			continue
		}

		if ch == "sms" || ch == "email" || ch == "dingtalk" {
			return ch, recipient, "", nil
		}

		acc, err := s.accountLookup.FindActiveAccount(ctx, ch)
		if err != nil {
			logger.Warnf("[ProactiveReach] 查找 %s 账号失败: %v", ch, err)
			continue
		}
		if acc == "" {
			continue
		}
		return ch, recipient, acc, nil
	}
	return "", "", "", fmt.Errorf("no active account for customer %s, tried: %s", customer.UnifiedID, strings.Join(tried, ","))
}

func (s *ProactiveReachService) checkCooldown(ctx context.Context, oneID string) bool {
	if oneID == "" {
		return true
	}
	key := "mtk:reach:cooldown:" + oneID
	set, err := cache.GetGlobalCache().SetNX(ctx, key, "1", 60*time.Minute)
	if err != nil {
		return true
	}
	return set
}

func (s *ProactiveReachService) checkDoNotContact(ctx context.Context, oneID, channel, fallbackKey string) bool {
	key := oneID
	if key == "" {
		key = fallbackKey
	}
	blocked := s.dncService().IsBlocked(ctx, key, channel)
	if blocked {

		logger.Warnf("[DNC] 跳过发送 one_id=%s channel=%s（命中全局退订标志位）", key, channel)
	}
	return blocked
}

func (s *ProactiveReachService) filterDoNotContactChannels(ctx context.Context, oneID string, candidates []string) []string {
	dnc := s.dncService()
	kept := make([]string, 0, len(candidates))
	for _, ch := range candidates {
		if dnc.IsBlocked(ctx, oneID, ch) {
			logger.Warnf("[DNC] 跳过渠道 one_id=%s channel=%s（命中全局退订标志位）", oneID, ch)
			continue
		}
		kept = append(kept, ch)
	}
	return kept
}

func (s *ProactiveReachService) sendSMS(ctx context.Context, req *ProactiveReachRequest, phone string) (*ProactiveReachResponse, error) {
	registry := s.smsRegistry
	if registry == nil {
		return nil, errors.New("sms service not registered")
	}
	svc, err := registry()
	if err != nil {
		return nil, err
	}
	msgID, err := svc(ctx, phone, req.Content, req.TemplateID, req.Params)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "sms",
		RecipientID: phone,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "explicit phone",
	}, nil
}

func (s *ProactiveReachService) sendEmail(ctx context.Context, req *ProactiveReachRequest, to string) (*ProactiveReachResponse, error) {
	sender := s.emailRegistry
	if sender == nil {
		return nil, errors.New("email service not registered")
	}
	subject := req.Subject
	if subject == "" {
		subject = "来自 HiveMtk 的消息"
	}
	msgID, err := sender(ctx, 0, to, subject, req.Content, nil)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "email",
		RecipientID: to,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "explicit email",
	}, nil
}

func (s *ProactiveReachService) sendTelegram(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.telegramRegistry
	if sender == nil {
		return nil, errors.New("telegram service not registered")
	}
	accID := parseUint(accountID)
	chatID := parseInt64(recipientID)
	if chatID == 0 {
		return nil, fmt.Errorf("telegram: invalid chat_id %s", recipientID)
	}
	if err := sender(ctx, accID, chatID, req.Content); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("tg_%d", time.Now().UnixNano()),
		Channel:     "telegram",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has telegram_chat_id",
	}, nil
}

func (s *ProactiveReachService) sendWhatsApp(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.whatsAppRegistry
	if sender == nil {
		return nil, errors.New("whatsapp service not registered")
	}
	accID := parseUint(accountID)
	if err := sender(ctx, accID, recipientID, req.Content); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("wa_%d", time.Now().UnixNano()),
		Channel:     "whatsapp",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has whatsapp_phone",
	}, nil
}

func (s *ProactiveReachService) sendWeCom(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.weComRegistry
	if sender == nil {
		return nil, errors.New("wecom service not registered")
	}
	accID := parseUint(accountID)
	msgID, err := sender(ctx, accID, recipientID, "text", req.Content, true, "ai_agent")
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "wecom",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has wecom_external_id",
	}, nil
}

func (s *ProactiveReachService) sendWeChat(ctx context.Context, req *ProactiveReachRequest, recipientID string) (*ProactiveReachResponse, error) {
	sender := s.wechatRegistry
	if sender == nil {
		return nil, errors.New("wechat service not registered")
	}
	accountID := parseUint(req.AccountID)
	msgID, err := sender(ctx, accountID, recipientID, "text", req.Content)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "wechat",
		RecipientID: recipientID,
		AccountID:   fmt.Sprintf("%d", accountID),
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has wechat_open_id",
	}, nil
}

func (s *ProactiveReachService) sendFeishu(ctx context.Context, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	sender := s.feishuRegistry
	if sender == nil {
		return nil, errors.New("feishu service not registered")
	}
	accID := parseUint(accountID)
	if err := sender(ctx, accID, recipientID, req.Content, "open_id"); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("feishu_%d", time.Now().UnixNano()),
		Channel:     "feishu",
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "customer has feishu_open_id",
	}, nil
}

func (s *ProactiveReachService) sendBridge(ctx context.Context, channel string, req *ProactiveReachRequest, recipientID, accountID string) (*ProactiveReachResponse, error) {
	if err := DeliverBridgeOutbound(ctx, channel, accountID, recipientID, "text", req.Content, ""); err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   fmt.Sprintf("%s_%d", channel, time.Now().UnixNano()),
		Channel:     channel,
		RecipientID: recipientID,
		AccountID:   accountID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    fmt.Sprintf("customer has %s open_id", channel),
	}, nil
}

func (s *ProactiveReachService) sendDingTalk(ctx context.Context, req *ProactiveReachRequest, chatID string) (*ProactiveReachResponse, error) {
	sender := s.dingTalkRegistry
	if sender == nil {
		return nil, errors.New("dingtalk service not registered")
	}
	msgID, err := sender(ctx, chatID, "", "text", req.Content)
	if err != nil {
		return nil, err
	}
	return &ProactiveReachResponse{
		MessageID:   msgID,
		Channel:     "dingtalk",
		RecipientID: chatID,
		Status:      "sent",
		SentAt:      time.Now(),
		Strategy:    "dingtalk webhook",
	}, nil
}

func parseUint(s string) uint {
	var n uint
	fmt.Sscanf(s, "%d", &n)
	return n
}

func parseInt64(s string) int64 {
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

type defaultAccountLookup struct {
	repo *repository.ProactiveReachRepository
}

func (l *defaultAccountLookup) FindActiveAccount(ctx context.Context, channel string) (string, error) {
	if l.repo == nil {
		return "", errors.New("db not available")
	}
	return l.repo.FindActiveAccountID(ctx, channel)
}

// BindProactiveReachSenders 把 Reach Sender 注册到 ProactiveReachService
//
//	在 router 启动时调用，把真实的 SMS/Email/TG/... 发送器注入。
func BindProactiveReachSenders(svc *ProactiveReachService, db *gorm.DB) {
	if svc == nil {
		return
	}

	svc.SetSMSRegistry(func() (func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error), error) {
		smsSvc := NewSmsService(repository.NewSmsRepository())
		return func(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
			if err := smsSvc.SendSms(ctx, &dto.SmsSendRequest{Phone: phone, Content: content}); err != nil {
				return "", err
			}
			return "sms_out", nil
		}, nil
	})

	svc.SetEmailRegistry(func(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error) {
		emailSvc := NewEmailService(db)
		return emailSvc.Send(ctx, accountID, to, subject, content, attachments)
	})

	svc.SetTelegramRegistry(func(ctx context.Context, accountID uint, chatID int64, content string) error {
		return NewTelegramIntegrationService(db).SendMessage(ctx, accountID, chatID, content)
	})

	svc.SetWhatsAppRegistry(func(ctx context.Context, accountID uint, toPhone, content string) error {
		return NewWhatsAppCloudIntegrationService(db).SendMessage(ctx, accountID, toPhone, content)
	})

	svc.SetWeComRegistry(func(ctx context.Context, accountID uint, externalUserID, msgType, content string, isAIReply bool, agent string) (string, error) {
		_, err := NewWeComIntegrationService(db).SendMessage(ctx, &WeComSendRequest{
			AccountID:      accountID,
			ExternalUserID: externalUserID,
			MsgType:        msgType,
			Content:        content,
			IsAIReply:      isAIReply,
			AIAgent:        agent,
		})
		if err != nil {
			return "", err
		}
		return "wecom_out", nil
	})

	svc.SetFeishuRegistry(func(ctx context.Context, accountID uint, openID, content, receiveIDType string) error {
		return NewFeishuIntegrationService(db).SendMessage(ctx, accountID, openID, content, receiveIDType, "")
	})

	svc.SetDingTalkRegistry(func(ctx context.Context, webhookOrToken, secret, msgType, content string) (string, error) {
		return NewDingTalkService().SendRobot(ctx, webhookOrToken, secret, msgType, content)
	})

	svc.SetWechatRegistry(func(ctx context.Context, accountID uint, openID, msgType, content string) (string, error) {
		return NewWechatService(db).SendCustomMessage(ctx, accountID, openID, msgType, content)
	})
}
