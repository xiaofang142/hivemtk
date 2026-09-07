package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ErrChannelCustomerNotFound 客户不存在（customer_id 未命中）
var ErrChannelCustomerNotFound = errors.New("customer not found")

type ChannelOverviewService struct {
	repo *repository.ChannelOverviewRepository
}

// NewChannelOverviewService 创建渠道概览服务
func NewChannelOverviewService(repo *repository.ChannelOverviewRepository) *ChannelOverviewService {
	return &ChannelOverviewService{repo: repo}
}

func (s *ChannelOverviewService) safeCount(label string, query func() (int64, error)) int {
	if s.repo == nil {
		return 0
	}
	n, err := query()
	if err != nil {
		logger.Warnf("[ChannelOverview] %s count failed: %v", label, err)
		return 0
	}
	return int(n)
}

// GetOverview 列出所有 13 渠道的当前状态
func (s *ChannelOverviewService) GetOverview(ctx context.Context) dto.ChannelOverview {
	channels := []dto.ChannelStatus{

		{
			Channel:          "telegram",
			ChannelName:      "Telegram Bot",
			Category:         "official_api",
			AccountCount:     s.safeCount("telegram", func() (int64, error) { return s.repo.CountTelegram(ctx) }),
			ActiveCount:      s.safeCount("telegram_active", func() (int64, error) { return s.repo.CountTelegramActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"bot_token", "webhook_secret", "agent_id"},
			ConfigURLs:       []string{"/api/telegram/accounts", "/api/telegram/accounts (POST)"},
			HealthURL:        "/api/webhook/telegram/{account_id}",
		},
		{
			Channel:          "qq",
			ChannelName:      "QQ 机器人",
			Category:         "official_api",
			AccountCount:     s.safeCount("qq", func() (int64, error) { return s.repo.CountQQ(ctx) }),
			ActiveCount:      s.safeCount("qq_active", func() (int64, error) { return s.repo.CountQQActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"app_id", "app_secret", "webhook_secret(BotSecret)"},
			ConfigURLs:       []string{"/api/qq/accounts", "/api/qq/accounts (POST)"},
			HealthURL:        "/api/webhook/qq/{account_id}",
		},
		{
			Channel:          "whatsapp",
			ChannelName:      "WhatsApp Cloud API",
			Category:         "official_api",
			AccountCount:     s.safeCount("whatsapp", func() (int64, error) { return s.repo.CountWhatsApp(ctx) }),
			ActiveCount:      s.safeCount("whatsapp_active", func() (int64, error) { return s.repo.CountWhatsAppActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"phone_id", "access_token", "waba_id"},
			ConfigURLs:       []string{"/api/whatsapp-cloud/accounts", "/api/whatsapp-cloud/accounts (POST)"},
			HealthURL:        "/api/webhook/whatsapp/{account_id}",
		},
		{
			Channel:          "feishu",
			ChannelName:      "飞书机器人",
			Category:         "official_api",
			AccountCount:     s.safeCount("feishu", func() (int64, error) { return s.repo.CountFeishu(ctx) }),
			ActiveCount:      s.safeCount("feishu_active", func() (int64, error) { return s.repo.CountFeishuActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"app_id", "app_secret", "verification_token", "encrypt_key"},
			ConfigURLs:       []string{"/api/feishu/accounts", "/api/feishu/accounts (POST)"},
			HealthURL:        "/api/webhook/feishu/{account_id}",
		},
		{
			Channel:          "wecom",
			ChannelName:      "企业微信",
			Category:         "official_api",
			AccountCount:     s.safeCount("wecom", func() (int64, error) { return s.repo.CountWeCom(ctx) }),
			ActiveCount:      s.safeCount("wecom_online", func() (int64, error) { return s.repo.CountWeComOnline(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"corp_id", "corp_secret", "agent_id"},
			ConfigURLs:       []string{"/api/wecom/accounts", "/api/wecom/accounts (POST)"},
			HealthURL:        "/api/webhook/wecom/{account_id}",
		},
		{
			Channel:          "dingtalk",
			ChannelName:      "钉钉群机器人",
			Category:         "official_api",
			AccountCount:     s.safeCount("dingtalk", func() (int64, error) { return s.repo.CountDingTalk(ctx) }),
			ActiveCount:      s.safeCount("dingtalk_active", func() (int64, error) { return s.repo.CountDingTalkActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"webhook_url", "sign_secret"},
			ConfigURLs:       []string{"/api/dingtalk-app/accounts", "/api/dingtalk-app/accounts (POST)"},
		},
		{
			Channel:          "sms",
			ChannelName:      "阿里云短信",
			Category:         "official_api",
			AccountCount:     s.safeCount("sms", func() (int64, error) { return s.repo.CountSMS(ctx) }),
			ActiveCount:      s.safeCount("sms_active", func() (int64, error) { return s.repo.CountSMSActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"access_key_id", "access_key_secret", "sign_name", "template_id"},
			ConfigURLs:       []string{"/api/sms/config", "/api/sms/config (POST)"},
		},
		{
			Channel:          "email",
			ChannelName:      "SMTP 邮件",
			Category:         "official_api",
			AccountCount:     s.safeCount("email", func() (int64, error) { return s.repo.CountEmail(ctx) }),
			ActiveCount:      s.safeCount("email_active", func() (int64, error) { return s.repo.CountEmailActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"smtp_host", "smtp_port", "username", "password", "from_addr"},
			ConfigURLs:       []string{"POST /api/email/accounts", "GET /api/email/accounts"},
		},

		{
			Channel:          "douyin",
			ChannelName:      "抖音（Bridge）",
			Category:         "bridge",
			AccountCount:     s.bridgeCount(ctx, "douyin"),
			OnlineCount:      s.bridgeOnlineCount(ctx, "douyin"),
			IntegrationReady: true,
			RequiredFields:   []string{"chrome_extension_installed", "user_login_douyin"},
			ConfigURLs:       []string{"Chrome 扩展: ingestBridge.js", "/api/bridge/account (POST)"},
			HealthURL:        "/api/bridge/outbox?channel=douyin&account_id=...",
		},
		{
			Channel:          "kuaishou",
			ChannelName:      "快手（Bridge）",
			Category:         "bridge",
			AccountCount:     s.bridgeCount(ctx, "kuaishou"),
			OnlineCount:      s.bridgeOnlineCount(ctx, "kuaishou"),
			IntegrationReady: true,
			RequiredFields:   []string{"chrome_extension_installed", "user_login_kuaishou"},
			ConfigURLs:       []string{"Chrome 扩展: content-kuaishou.js", "/api/bridge/account (POST)"},
			HealthURL:        "/api/bridge/outbox?channel=kuaishou&account_id=...",
		},
		{
			Channel:          "xiaohongshu",
			ChannelName:      "小红书（Bridge）",
			Category:         "bridge",
			AccountCount:     s.bridgeCount(ctx, "xiaohongshu"),
			OnlineCount:      s.bridgeOnlineCount(ctx, "xiaohongshu"),
			IntegrationReady: true,
			RequiredFields:   []string{"chrome_extension_installed", "user_login_xhs"},
			ConfigURLs:       []string{"Chrome 扩展: ingestBridge.js", "/api/bridge/account (POST)"},
		},
		{
			Channel:          "tiktok",
			ChannelName:      "TikTok（Bridge）",
			Category:         "bridge",
			AccountCount:     s.bridgeCount(ctx, "tiktok"),
			OnlineCount:      s.bridgeOnlineCount(ctx, "tiktok"),
			IntegrationReady: true,
			RequiredFields:   []string{"chrome_extension_installed", "user_login_tiktok"},
			ConfigURLs:       []string{"Chrome 扩展: ingestBridge.js", "/api/bridge/account (POST)"},
		},
		{
			Channel:          "xianyu",
			ChannelName:      "闲鱼（Bridge）",
			Category:         "bridge",
			AccountCount:     s.bridgeCount(ctx, "xianyu"),
			OnlineCount:      s.bridgeOnlineCount(ctx, "xianyu"),
			IntegrationReady: true,
			RequiredFields:   []string{"chrome_extension_installed", "user_login_xianyu"},
			ConfigURLs:       []string{"Chrome 扩展: ingestBridge.js", "/api/bridge/account (POST)"},
		},
		{
			Channel:          "wechat",
			ChannelName:      "微信公众号",
			Category:         "official_api",
			AccountCount:     s.safeCount("wechat", func() (int64, error) { return s.repo.CountWechat(ctx) }),
			ActiveCount:      s.safeCount("wechat_active", func() (int64, error) { return s.repo.CountWechatActive(ctx) }),
			IntegrationReady: true,
			RequiredFields:   []string{"app_id", "app_secret", "原始ID"},
			ConfigURLs:       []string{"/api/wechat/accounts", "/api/wechat/accounts (POST)"},
			HealthURL:        "/api/webhook/wechat/{account_id}",
		},
	}

	return dto.ChannelOverview{
		Channels:         channels,
		TotalChannels:    len(channels),
		RealChannels:     13,
		BridgeChannels:   5,
		OfficialChannels: 8,
	}
}

func (s *ChannelOverviewService) bridgeCount(ctx context.Context, channel string) int {
	return s.safeCount("bridge_"+channel, func() (int64, error) { return s.repo.CountBridge(ctx, channel) })
}

func (s *ChannelOverviewService) bridgeOnlineCount(ctx context.Context, channel string) int {
	return s.safeCount("bridge_online_"+channel, func() (int64, error) { return s.repo.CountBridgeOnline(ctx, channel) })
}

// BindChannel 绑定客户到某渠道（用于主动收集 OneID 信息）
//
// 业务规则:
//   - one_id 缺省时通过 customer_id 反查 unified_id（未命中报错）
//   - customer_channels 按 (one_id, channel) 幂等 upsert
//   - 同步 best-effort 更新 customers 表对应渠道主字段
func (s *ChannelOverviewService) BindChannel(ctx context.Context, req *dto.CustomerChannelBinding) (map[string]any, error) {

	oneID := req.OneID
	if oneID == "" && req.CustomerID != "" {
		resolved, err := s.repo.GetCustomerUnifiedID(ctx, req.CustomerID)
		if err != nil || resolved == "" {
			return nil, ErrChannelCustomerNotFound
		}
		oneID = resolved
	}
	if oneID == "" {
		return nil, fmt.Errorf("one_id or customer_id required")
	}

	cc := &model.CustomerChannel{
		OneID:         oneID,
		Channel:       req.Channel,
		ChannelUserID: req.ChannelUserID,
		ChannelName:   req.ChannelName,
		AccountID:     req.AccountID,
	}
	assignVals := map[string]any{
		"channel_user_id": req.ChannelUserID,
		"channel_name":    req.ChannelName,
		"account_id":      req.AccountID,
		"is_primary":      req.IsPrimary,
		"group_id":        req.GroupID,
		"updated_at":      time.Now(),
	}
	if err := s.repo.UpsertCustomerChannel(ctx, cc, assignVals); err != nil {
		return nil, fmt.Errorf("upsert failed: %w", err)
	}

	updateCustomerField := map[string]string{
		"telegram":    "telegram_chat_id",
		"whatsapp":    "whats_app_phone",
		"wechat":      "wechat_open_id",
		"feishu":      "feishu_open_id",
		"wecom":       "we_com_external_id",
		"douyin":      "douyin_open_id",
		"tiktok":      "tik_tok_open_id",
		"kuaishou":    "kuaishou_open_id",
		"xiaohongshu": "xiaohongshu_id",
		"xianyu":      "xianyu_id",
	}
	if field, ok := updateCustomerField[req.Channel]; ok {
		if err := s.repo.UpdateCustomerChannelField(ctx, oneID, field, req.ChannelUserID); err != nil {
			logger.Warnf("[ChannelOverview] update customer %s field %s failed: %v", oneID, field, err)
		}
	}

	return map[string]any{
		"one_id":   oneID,
		"channel":  req.Channel,
		"identity": req.ChannelUserID,
	}, nil
}

// ListCustomerChannels 列出客户的所有渠道绑定
func (s *ChannelOverviewService) ListCustomerChannels(ctx context.Context, customerID string) (map[string]any, error) {

	n, err := s.repo.CountCustomersByID(ctx, customerID)
	if err != nil {
		logger.Warnf("[ChannelOverview] count err: %v, customer_id=%s", err, customerID)
	}
	logger.Infof("[ChannelOverview] ListCustomerChannels count=%d customer_id=%s", n, customerID)

	cust, err := s.repo.GetCustomerIdentity(ctx, customerID)
	if err != nil {
		logger.Warnf("[ChannelOverview] ListCustomerChannels scan err: %v, customer_id=%s", err, customerID)
		return nil, ErrChannelCustomerNotFound
	}
	logger.Infof("[ChannelOverview] ListCustomerChannels customer_id=%s -> unified_id=%s phone=%s email=%s name=%s",
		customerID, cust.UnifiedID, cust.Phone, cust.Email, cust.Name)

	rows, err := s.repo.ListCustomerChannelsByOneID(ctx, cust.UnifiedID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"customer_id": customerID,
		"one_id":      cust.UnifiedID,
		"name":        cust.Name,
		"phone":       cust.Phone,
		"email":       cust.Email,
		"bindings":    rows,
		"total":       len(rows),
	}, nil
}
