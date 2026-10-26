package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"

	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/channelbot/telegram"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

type IntegrationReachAdapter struct {
	// db 只用于账号读侧（reach.account.list / reach.health 的渠道账号表查询）。
	// 发送路径一律经各渠道 IntegrationService，不经这一句柄——两条路的账号身份口径
	// 不同（发送用平台凭证、读侧用配置行），混用会让列表与实发结果互相打脸。
	db       *gorm.DB
	tg       *service.TelegramIntegrationService
	wa       *service.WhatsAppCloudIntegrationService
	feishu   *service.FeishuIntegrationService
	web      *service.CustomerSessionService
	wecom    *service.WeComIntegrationService
	dingtalk *service.DingTalkService
	sms      service.SmsService
	email    *email.EmailSendService
}

// Sentinel errors
//
// 使用 sentinel error 替代字符串比较（MASTER_RULES 5.2）：
//   - 渠道未实现：ErrChannelNotImplemented
//   - 渠道 IntegrationService 未注入：ErrIntegrationServiceNotConfigured
//   - 参数解析失败：ErrInvalidAccountID / ErrInvalidInt64
//   - 工具调用参数错误：复用 tooluse 包内已有 sentinel
var (
	ErrChannelNotImplemented           = errors.New("channel not implemented in IntegrationReachAdapter")
	ErrIntegrationServiceNotConfigured = errors.New("integration service not configured for channel")
	ErrInvalidAccountID                = errors.New("invalid account_id")
	ErrInvalidInt64                    = errors.New("invalid int64 value")
	// ErrChannelNoAccountDimension 该渠道没有"账号"这一层可查：短信按签名发、邮件按 SMTP
	// 配置发、card 是消息形态、web 的身份是会话。回空列表会被读成"这个渠道一个账号都没有"，
	// 与"这个问题在这个渠道不成立"是两种事实，所以给错误而不是空集。
	ErrChannelNoAccountDimension = errors.New("channel has no account dimension")
	// ErrUnknownReachChannel 渠道名不在触达支持集合内。拼错的名字必须当场响，
	// 静默返回 0 条会让调用方把"名字写错"排查成"账号没配"。
	ErrUnknownReachChannel = errors.New("unknown reach channel")
	// ErrRecallNotSupported 该渠道没有平台主动撤回通道（WA 无主动撤回 API，
	// 桥接渠道的 msgID 是合成键而非平台消息标识）。
	ErrRecallNotSupported = errors.New("recall not supported for channel")
	// ErrRecallWindowExpired 已超过平台撤回时间窗口（TG 48h、飞书/企微 24h）。
	ErrRecallWindowExpired = errors.New("recall window expired")
	// ErrHubKeyNotFound message_hub 无 msgID 对应行，无法反查平台消息标识。
	ErrHubKeyNotFound = errors.New("hub message not found for recall")
)

// NewIntegrationReachAdapter 创建集成服务适配器
//
// 三个参数均可为 nil（nil 时对应渠道发送返回 ErrIntegrationServiceNotConfigured）
func NewIntegrationReachAdapter(tg *service.TelegramIntegrationService, wa *service.WhatsAppCloudIntegrationService, feishu *service.FeishuIntegrationService) *IntegrationReachAdapter {
	return &IntegrationReachAdapter{tg: tg, wa: wa, feishu: feishu}
}

// NewIntegrationReachAdapterFromDB 通过 db 一站式创建（推荐用法）
//
// 真正实例化 3 个 IntegrationService，让 智能体的 reach.telegram.send / reach.whatsapp.send /
// reach.feishu.send 工具在生产中可以真正发送消息。
func NewIntegrationReachAdapterFromDB(db *gorm.DB) *IntegrationReachAdapter {
	if db == nil {
		return &IntegrationReachAdapter{}
	}
	return &IntegrationReachAdapter{
		db: db,
		// 仓储在装配点构造（service 层不再接收 DB 句柄，见五层架构 §3.2）
		tg: service.NewTelegramIntegrationService(repository.NewTelegramAccountRepositoryWithDB(db)),
		wa: service.NewWhatsAppCloudIntegrationService(repository.NewWhatsAppCloudAccountRepositoryWithDB(db)),
		feishu: service.NewFeishuIntegrationService(
			repository.NewFeishuAccountRepositoryWithDB(db),
			repository.NewFeishuMessageRepositoryWithDB(db),
		),
		web:      service.NewCustomerSessionService(),
		wecom:    service.NewWeComIntegrationService(),
		dingtalk: service.NewDingTalkService(),
		sms:      service.NewSmsService(repository.NewSmsRepository()),
		email:    email.NewEmailSendService(),
	}
}

// SendTelegram 通过 TelegramIntegrationService 发送消息
//
// 参数：accountID 数字字符串，chatID 数字字符串（私聊为正、群组为负），content 消息文本
// 返回：msgID 为出站回执键 `tg-out-{accountID}-{平台消息号}`（与 message_hub 行同键，
// 可 join、可被 Recall 反查；不再返回查无此行的纳秒占位假号）
// 错误：IntegrationService 透传（网络错误、Bot Token 无效、chat 限流等）
func (a *IntegrationReachAdapter) SendTelegram(ctx context.Context, accountID, chatID, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "telegram").Str("account_id", accountID).Str("chat_id", chatID).Int("content_len", len(content)).Msg("reach send start")
	if a.tg == nil {
		return "", fmt.Errorf("telegram: %w", ErrIntegrationServiceNotConfigured)
	}
	accID, err := parseAccountID(accountID)
	if err != nil {
		return "", fmt.Errorf("telegram: %w", err)
	}
	cid, err := parseInt64(chatID)
	if err != nil {
		return "", fmt.Errorf("telegram: %w", err)
	}
	msgID, err := a.tg.SendMessageWithReceipt(ctx, accID, cid, content, telegram.SendMessageOptions{})
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "telegram").Str("account_id", accountID).Msg("reach send failed")
		return "", fmt.Errorf("telegram send: %w", err)
	}
	return msgID, nil
}

// SendWhatsApp 通过 WhatsAppCloudIntegrationService 发送消息
//
// 参数：accountID 数字字符串，toPhone E.164 格式，content 消息文本
// 返回：msgID 为 message_hub 键 = 平台 wamid（I29 起不再自造 wa-{账号}-{纳秒} 占位）
// 错误：透传 IntegrationService（401 token 失效、403 模板未审批、429 限流等）
func (a *IntegrationReachAdapter) SendWhatsApp(ctx context.Context, accountID, toPhone, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "whatsapp").Str("account_id", accountID).Int("content_len", len(content)).Msg("reach send start")
	if a.wa == nil {
		return "", fmt.Errorf("whatsapp: %w", ErrIntegrationServiceNotConfigured)
	}
	accID, err := parseAccountID(accountID)
	if err != nil {
		return "", fmt.Errorf("whatsapp: %w", err)
	}
	msgID, err := a.wa.SendMessageWithReceipt(ctx, accID, toPhone, content, "", nil)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "whatsapp").Str("account_id", accountID).Msg("reach send failed")
		return "", fmt.Errorf("whatsapp send: %w", err)
	}
	return msgID, nil
}

// SendFeishu 通过 FeishuIntegrationService 发送消息
//
// 参数：accountID 数字字符串，openID 飞书 open_id，content 消息文本
// 返回：msgID 为 message_hub 键 = feishu-out-{账号}-{平台message_id}（I29 起不再自造占位）
// 错误：透传 IntegrationService（token 过期、app_id 无效、用户不在可见范围等）
func (a *IntegrationReachAdapter) SendFeishu(ctx context.Context, accountID, openID, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "feishu").Str("account_id", accountID).Int("content_len", len(content)).Msg("reach send start")
	if a.feishu == nil {
		return "", fmt.Errorf("feishu: %w", ErrIntegrationServiceNotConfigured)
	}
	accID, err := parseAccountID(accountID)
	if err != nil {
		return "", fmt.Errorf("feishu: %w", err)
	}
	msgID, err := a.feishu.SendMessageWithReceipt(ctx, accID, openID, content, "open_id", "")
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "feishu").Str("account_id", accountID).Msg("reach send failed")
		return "", fmt.Errorf("feishu send: %w", err)
	}
	return msgID, nil
}

// SendWeb 通过网页客服渠道（WebSocket）向访客会话推送消息。
//
// 完整业务闭环（区别于其他渠道的纯 API 转发）：
//  1. 校验会话存在（CustomerSessionService.SendMessage 内部校验，不存在返回错误）
//  2. 落库 SessionMessage（sender_type=agent, sender_name=客服）并更新会话最后消息与回复计数
//  3. 实时经 WebSocket 推送给在线访客（SendMessage 内部 pushToVisitor 已实现）
//  4. 访客在线则标记 delivered_at，避免 WebSocket 重连后离线补发重复展示
//
// 为遵守「客服页面不对用户显示 AI」，网页客服主动触达统一以「客服」身份下发，不暴露 AI 标识。
// 错误：服务未注入 / session_id 为空 / content 为空 / 会话不存在。
func (a *IntegrationReachAdapter) SendWeb(ctx context.Context, sessionID, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "web").Str("session_id", sessionID).Int("content_len", len(content)).Msg("reach send start")
	if a.web == nil {
		return "", fmt.Errorf("web: %w", ErrIntegrationServiceNotConfigured)
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("web: session_id required")
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("web: content required")
	}
	msg, err := a.web.SendMessage(ctx, &service.SendMessageRequest{
		SessionID:   sessionID,
		Content:     content,
		ContentType: model.MessageTypeText,
		SenderType:  "agent",
		SenderName:  "客服",
		SenderID:    "reach_web",
	})
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "web").Str("session_id", sessionID).Msg("reach send failed")
		return "", fmt.Errorf("web send: %w", err)
	}
	return fmt.Sprintf("web-%s-%d", sessionID, msg.ID), nil
}

// WithWeb 注入网页客服会话服务（实现 reach.web.send 完整业务）
func (a *IntegrationReachAdapter) WithWeb(svc *service.CustomerSessionService) *IntegrationReachAdapter {
	a.web = svc
	return a
}

// WithWeCom 注入企微集成服务（实现 reach.wecom.send 完整业务， 收敛统一企微出站入口）
func (a *IntegrationReachAdapter) WithWeCom(svc *service.WeComIntegrationService) *IntegrationReachAdapter {
	a.wecom = svc
	return a
}

// WebReachAdapter 网页客服触达适配器类型别名。
// 通过注入 CustomerSessionService 实现 reach.web.send 的完整业务（落库 + 实时推访客），
// 可直接作为 ReachToolDeps.Adapter 注入，独立承载网页客服渠道。
type WebReachAdapter = IntegrationReachAdapter

// NewWebReachAdapter 创建仅聚焦网页客服渠道的触达适配器
func NewWebReachAdapter(svc *service.CustomerSessionService) *WebReachAdapter {
	return &WebReachAdapter{web: svc}
}

// SendSMS 通过 SmsService 发送短信（补 reach.sms.send 真实出站）
//
// 备注：SmsSendRequest 仅含 Phone/Content（明文发送），适配器签名中的
// templateID/params 为模板发送预留，当前透传至 service 时忽略（service 暂不含模板分支）。
func (a *IntegrationReachAdapter) SendSMS(ctx context.Context, phone, content, templateID string, params map[string]string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "sms").Str("phone", phone).Int("content_len", len(content)).Msg("reach send start")
	if a.sms == nil {
		return "", fmt.Errorf("sms: %w", ErrIntegrationServiceNotConfigured)
	}
	if strings.TrimSpace(phone) == "" {
		return "", errors.New("sms: phone required")
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("sms: content required")
	}
	if err := a.sms.SendSms(ctx, &dto.SmsSendRequest{Phone: phone, Content: content}); err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "sms").Str("phone", phone).Msg("reach sms send failed")
		return "", fmt.Errorf("sms send: %w", err)
	}
	return fmt.Sprintf("sms-%s", phone), nil
}

// SendEmail 通过 EmailSendService 发送邮件（补 reach.email.send 真实出站）
//
// ImmediateSend=true：落库后立即异步发送（SMTP 配置缺失时仅记录失败状态，不阻塞主流程）。
func (a *IntegrationReachAdapter) SendEmail(ctx context.Context, to, subject, content string, attachments []string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "email").Str("to", to).Int("content_len", len(content)).Msg("reach send start")
	if a.email == nil {
		return "", fmt.Errorf("email: %w", ErrIntegrationServiceNotConfigured)
	}
	if strings.TrimSpace(to) == "" {
		return "", errors.New("email: to required")
	}
	log, err := a.email.SendEmail(ctx, dto.SendEmailRequest{
		To:            to,
		Subject:       subject,
		Content:       content,
		Attachments:   attachments,
		ImmediateSend: true,
	})
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "email").Str("to", to).Msg("reach email send failed")
		return "", fmt.Errorf("email send: %w", err)
	}
	return fmt.Sprintf("email-%s", log.ID), nil
}

// SendWeCom 通过 WeComIntegrationService 发送企微消息（收敛：统一企微出站入口）
//
// 此前为 NoOp（ErrChannelNotImplemented），企微出站独立在 WeComIntegrationService，
// 与 ReachAdapter 接口重叠（缺陷）。现委托 WeComIntegrationService.SendMessage，
// 使 IntegrationReachAdapter 成为覆盖 TG/WA/Feishu/Web/WeCom 的单一出站入口。
//
// 底层语义与 WeComIntegrationService.SendMessage 既有行为一致：
//   - 账号健康度/配额检查后推消息中台 + 收件箱
//
// 配置了真实企微凭证（CorpID/CorpSecret）时真实调企微 API；无凭证安全跳过
//   - SelectHealthyAccount 自动选健康账号（忽略传入 AccountID，与既有出站一致）
func (a *IntegrationReachAdapter) SendWeCom(ctx context.Context, accountID, externalUserID, msgType, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "wecom").Str("account_id", accountID).Str("external_user_id", externalUserID).Int("content_len", len(content)).Msg("reach send start")
	if a.wecom == nil {
		return "", fmt.Errorf("wecom: %w", ErrIntegrationServiceNotConfigured)
	}
	accID, err := parseAccountID(accountID)
	if err != nil {
		return "", fmt.Errorf("wecom: %w", err)
	}
	if externalUserID == "" {
		return "", errors.New("wecom: external_user_id required")
	}
	mt := msgType
	if mt == "" {
		mt = "text"
	}
	hubMsg, err := a.wecom.SendMessage(ctx, &service.WeComSendRequest{
		AccountID:      accID,
		ExternalUserID: externalUserID,
		MsgType:        mt,
		Content:        content,
	})
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "wecom").Str("account_id", accountID).Msg("reach send failed")
		return "", fmt.Errorf("wecom send: %w", err)
	}
	if hubMsg == nil {
		return "", nil
	}
	return hubMsg.MsgID, nil
}

// SendWeixin 微信公众号（客服消息，通过 GlobalServiceRegistry 获取 WechatService）
func (a *IntegrationReachAdapter) SendWeixin(ctx context.Context, openID, msgType, content string) (string, error) {
	svc, err := tooluse.GlobalServiceRegistry().Wechat()
	if err != nil {
		return "", fmt.Errorf("weixin: %w", err)
	}
	return svc.SendCustomMessage(ctx, 0, openID, msgType, content)
}

// 下面五个方法是 ReachAdapter 接口要求的形状，但生产装配路径上到不了这里：
// 五个桥接渠道的出站由 BridgeReachAdapter.deliverToOutbox 承担（它覆盖同名方法，
// 不再向下委托），本包只有 ListAccounts/AccountHealth/SendCard 会被委派进来。
// 保留返回错误是为了 fail-closed：谁绕过桥接层直接用本适配器，就该当场听到
// "这条路上没有直连 API"，而不是拿到一个假的成功 id。
// （抖音/快手/小红书/tiktok/闲鱼的私信都没有官方开放接口，只能经浏览器扩展桥接。）

// SendDouyin 见上方说明：桥接渠道无直连出站，此处是 fail-closed 兜底。
func (a *IntegrationReachAdapter) SendDouyin(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", fmt.Errorf("douyin: %w", ErrChannelNotImplemented)
}

// SendKuaishou 见上方说明：桥接渠道无直连出站，此处是 fail-closed 兜底。
func (a *IntegrationReachAdapter) SendKuaishou(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", fmt.Errorf("kuaishou: %w", ErrChannelNotImplemented)
}

// SendXHS 见上方说明：桥接渠道无直连出站，此处是 fail-closed 兜底。
func (a *IntegrationReachAdapter) SendXHS(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", fmt.Errorf("xhs: %w", ErrChannelNotImplemented)
}

// SendTikTok 见上方说明：桥接渠道无直连出站，此处是 fail-closed 兜底。
func (a *IntegrationReachAdapter) SendTikTok(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", fmt.Errorf("tiktok: %w", ErrChannelNotImplemented)
}

// SendXianyu 见上方说明：桥接渠道无直连出站，此处是 fail-closed 兜底。
func (a *IntegrationReachAdapter) SendXianyu(ctx context.Context, accountID, openID, msgType, content string) (string, error) {
	return "", fmt.Errorf("xianyu: %w", ErrChannelNotImplemented)
}

func (a *IntegrationReachAdapter) SendDingTalk(ctx context.Context, chatID, msgType, content string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", "dingtalk").Str("chat_id", chatID).Int("content_len", len(content)).Msg("reach send start")
	if a.dingtalk == nil {
		return "", fmt.Errorf("dingtalk: %w", ErrIntegrationServiceNotConfigured)
	}
	if chatID == "" {
		return "", errors.New("dingtalk: chat_id (webhook or access_token) required")
	}
	mt := msgType
	if mt == "" {
		mt = "text"
	}
	msgID, err := a.dingtalk.SendRobot(ctx, chatID, "", mt, content)
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", "dingtalk").Str("chat_id", chatID).Msg("reach send failed")
		return "", fmt.Errorf("dingtalk send: %w", err)
	}
	return msgID, nil
}

// cardTemplate 五张卡片模板表读出来的公共形状：字段名逐张一致，
// 差别只在 short_link_id 是可空指针还是 0 值哨兵。
type cardTemplate struct {
	Title       string
	Description string
	ShortLinkID uint
	IsActive    bool
}

// SendCard 把后台配好的卡片发到某个桥接渠道的会话里（reach.card.send 的实现）。
//
// 此前恒返回 ErrChannelNotImplemented：工具在 AI 的清单里可见、审批与限流都走完了，
// 最后一段却是空的，客户什么都收不到。这里补齐的是运营今天在人工工作台上做的同一件事
// ——把卡片的 /s/ 短链当消息发出去，因此消息体是"标题+描述+链接"的文本，
// 与全部 12 处桥接出站一样用 msg_type=text（桥接客户端只认文本，回 "card" 会被读成
// "这条消息该渲染成卡片"，而那件事本服务无法验证）。
//
// 只支持有卡片模板表的五个桥接渠道：企微/公众号没有"卡片模板"这一层，
// 显式拒绝比拿 card_id 去撞一张不存在的表更省事。
func (a *IntegrationReachAdapter) SendCard(ctx context.Context, channel, accountID, externalUserID, cardID string) (string, error) {
	ctx = logger.WithModule(ctx, "reach")
	if a.db == nil {
		return "", fmt.Errorf("card: %w", ErrIntegrationServiceNotConfigured)
	}
	if accountID == "" || externalUserID == "" {
		return "", errors.New("card: account_id 与 external_user_id 必填（卡片要发到具体会话）")
	}
	target := channel
	if bridge.IsBridgeChannel(channel) {
		target = bridge.NormalizeBridgeChannel(channel)
	}
	id, err := strconv.ParseUint(cardID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("card(%s): card_id 须是卡片后台的数字 id，收到 %q: %w", target, cardID, ErrInvalidInt64)
	}
	card, err := a.cardTemplateByID(ctx, target, uint(id))
	if err != nil {
		return "", err
	}
	if !card.IsActive {
		return "", fmt.Errorf("card(%s#%d): %w（卡片已停用）", target, id, ErrCardNotDeliverable)
	}
	link, err := a.cardShareURL(ctx, target, uint(id), card.ShortLinkID)
	if err != nil {
		return "", err
	}
	content := composeCardMessage(card.Title, card.Description, link)
	if err := service.DeliverBridgeOutbound(ctx, target, accountID, externalUserID, "text", content, ""); err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", target).Str("account_id", accountID).Msg("reach card send failed")
		return "", fmt.Errorf("card(%s): %w", target, err)
	}
	return "bridge:" + target + ":" + accountID + ":" + externalUserID, nil
}

// ErrCardNotDeliverable 卡片行存在、但这一刻发不出去：停用、没短链、短链已停用/已过期、
// 或服务端不知道对外域名。和"卡片不存在"（gorm.ErrRecordNotFound）是两种事实，
// 前者要人去补配置，后者是 id 写错了。
var ErrCardNotDeliverable = errors.New("card template not deliverable")

func (a *IntegrationReachAdapter) cardTemplateByID(ctx context.Context, channel string, id uint) (*cardTemplate, error) {
	switch channel {
	case bridge.ChannelDouyinWeb:
		var row model.DouyinCard
		if err := a.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, fmt.Errorf("card(douyin#%d) 读取失败: %w", id, err)
		}
		return &cardTemplate{Title: row.Title, Description: row.Description, ShortLinkID: row.ShortLinkID, IsActive: row.IsActive}, nil
	case bridge.ChannelKuaishouWeb:
		var row model.KuaishouCard
		if err := a.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, fmt.Errorf("card(kuaishou#%d) 读取失败: %w", id, err)
		}
		return &cardTemplate{Title: row.Title, Description: row.Description, ShortLinkID: derefUint(row.ShortLinkID), IsActive: row.IsActive}, nil
	case bridge.ChannelXHSWeb:
		var row model.XiaohongshuCard
		if err := a.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, fmt.Errorf("card(xiaohongshu#%d) 读取失败: %w", id, err)
		}
		return &cardTemplate{Title: row.Title, Description: row.Description, ShortLinkID: derefUint(row.ShortLinkID), IsActive: row.IsActive}, nil
	case bridge.ChannelTikTok:
		var row model.TikTokCard
		if err := a.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, fmt.Errorf("card(tiktok#%d) 读取失败: %w", id, err)
		}
		return &cardTemplate{Title: row.Title, Description: row.Description, ShortLinkID: row.ShortLinkID, IsActive: row.IsActive}, nil
	case bridge.ChannelXianyuWeb:
		var row model.XianyuCard
		if err := a.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, fmt.Errorf("card(xianyu#%d) 读取失败: %w", id, err)
		}
		return &cardTemplate{Title: row.Title, Description: row.Description, ShortLinkID: row.ShortLinkID, IsActive: row.IsActive}, nil
	default:
		return nil, fmt.Errorf("card(%s): 该渠道没有卡片模板这一层，可发的渠道为 douyin/kuaishou/xiaohongshu/tiktok/xianyu: %w", channel, ErrChannelNotImplemented)
	}
}

// cardShareURL 拼出客户能点开的绝对短链。
//
// 短链行上的 code 在库里是相对的（/s/xxxxxx），后台分享时由前端补 window.location.origin；
// 服务端没有"浏览器 origin"这个东西，只能用部署期声明的 PUBLIC_BASE_URL。没配它就把相对路径
// 发出去，等于给客户一条点不开的死链，所以当场报错。
// 短链的停用/过期判据与 redirect 控制器逐条对齐：库里活着但客户打开是 410 的链接，不发。
func (a *IntegrationReachAdapter) cardShareURL(ctx context.Context, channel string, cardID, shortLinkID uint) (string, error) {
	if shortLinkID == 0 {
		return "", fmt.Errorf("card(%s#%d): %w（这张卡片还没生成 /s/ 短链，先在卡片后台分享一次）", channel, cardID, ErrCardNotDeliverable)
	}
	var link model.ShortLink
	if err := a.db.WithContext(ctx).First(&link, shortLinkID).Error; err != nil {
		return "", fmt.Errorf("card(%s#%d) 读短链记录失败: %w", channel, cardID, err)
	}
	if link.Status == shortLinkStatusDisabled {
		return "", fmt.Errorf("card(%s#%d): %w（短链 %s 已停用，发出去客户只会看到 410）", channel, cardID, ErrCardNotDeliverable, link.ShortCode)
	}
	if link.ExpireTime != nil && link.ExpireTime.Before(time.Now()) {
		return "", fmt.Errorf("card(%s#%d): %w（短链 %s 已于 %s 过期）", channel, cardID, ErrCardNotDeliverable, link.ShortCode,
			link.ExpireTime.Format(time.RFC3339))
	}
	base := config.GetPublicBaseURLFor(ctx)
	if base == "" {
		return "", fmt.Errorf("card(%s#%d): %w（未配置 PUBLIC_BASE_URL，服务端拼不出绝对链接）", channel, cardID, ErrCardNotDeliverable)
	}
	return base + "/s/" + link.ShortCode, nil
}

// shortLinkStatusDisabled 与 internal/controller/redirect.go 的 410 判据同一个值：
// 短链状态里 2 才是"停用"，0 是存量行的默认空值，不能当停用处理。
const shortLinkStatusDisabled = 2

func derefUint(v *uint) uint {
	if v == nil {
		return 0
	}
	return *v
}

// composeCardMessage 卡片消息体：标题 + 描述 + 链接，和富卡片出站的既有文案同一形状
// （见 DouyinIntegrationService.SendCard），运营在会话里看到的仍是认得出的那段话。
func composeCardMessage(title, description, link string) string {
	var b strings.Builder
	b.WriteString("[卡片] ")
	b.WriteString(title)
	if description != "" {
		b.WriteString("\n")
		b.WriteString(description)
	}
	b.WriteString("\n链接: ")
	b.WriteString(link)
	return b.String()
}

// Recall 平台撤回：按渠道分派到已接的平台撤回 API（不是"忘了写"）。
//
//   - TG：POST /bot{token}/deleteMessage，平台窗口 48h
//   - 飞书：DELETE /open-apis/im/v1/messages/{message_id}，平台窗口 24h
//   - 企微：POST /cgi-bin/message/recall，平台窗口 24h；平台 msgid 取自 hub 行
//     Extra["msgid"]（发送侧回填），旧行没有这个字段时如实报 missing msgid 而不是假装撤掉
//   - WhatsApp：Cloud API 无主动撤回接口（只有用户侧 revoke 通知）⇒ ErrRecallNotSupported
//   - 桥接渠道（抖音/快手/小红书/tiktok/闲鱼）：msgID 是 BridgeReachAdapter 生成的
//     合成键（或 ContentHash 总线键），不是平台消息标识 ⇒ ErrRecallNotSupported
//
// 成败一律以平台**业务码**为准，不看 HTTP 状态码：飞书与企微都在 HTTP 200 里回业务码，
// 只判状态码会把"平台没撤掉"谎报成成功。各渠道窗口见 recallWindow。
func (a *IntegrationReachAdapter) Recall(ctx context.Context, channel, msgID string) error {
	ctx = logger.WithModule(ctx, "reach")
	logger.Ctx(ctx).Debug().Str("channel", channel).Str("msg_id", msgID).Msg("reach recall start")
	if bridge.IsBridgeChannel(channel) {
		return fmt.Errorf("recall(%s): %w（桥接渠道 msgID 是合成键，不是平台消息标识）", channel, ErrRecallNotSupported)
	}
	if a.db == nil {
		return ErrIntegrationServiceNotConfigured
	}
	var hub model.MessageHub
	if err := a.db.WithContext(ctx).Where("msg_id = ?", msgID).First(&hub).Error; err != nil {
		return fmt.Errorf("recall(%s): 查 message_hub 无此行: %w", channel, ErrHubKeyNotFound)
	}
	return a.RecallHub(ctx, &hub)
}

// RecallHub 按**已定位的 hub 行**执行平台撤回（B2 的端口实现）。
//
// 与 Recall(ctx, channel, msgID) 的分工：那个入口按 msg_id 自己回查一行，是给工具
// （reach.recall，参数只有渠道与消息键）用的；这个入口直接吃行，是给
// /api/message-hub/:id/recall 用的 —— 撤回端点已经按主键定位到具体一行，
// 再按 msg_id 回查一次是**多一次读且可能读错行**：msg_id 的唯一索引是
// (platform, msg_id, conversation_id, direction) 四元组，单按 msg_id 查
// 在有同键异方向/异会话行时会挑到任意一条。
//
// 失败一律回 *service.RecallFailure（带 Kind），因为调用方（service 层）不能
// import 本包：分类在这里做，翻译成 HTTP 码的那一步在 service/controller。
func (a *IntegrationReachAdapter) RecallHub(ctx context.Context, hub *model.MessageHub) error {
	if hub == nil {
		return &service.RecallFailure{Kind: service.RecallKindChannelError, Err: errors.New("nil hub row")}
	}
	if bridge.IsBridgeChannel(hub.Platform) {
		return &service.RecallFailure{
			Kind: service.RecallKindNotSupported,
			Err:  fmt.Errorf("%w（桥接渠道 msgID 是合成键，不是平台消息标识）", ErrRecallNotSupported),
		}
	}
	if a.db == nil {
		return &service.RecallFailure{Kind: service.RecallKindChannelError, Err: ErrIntegrationServiceNotConfigured}
	}
	win, ok := recallWindow(hub.Platform)
	if !ok {
		return &service.RecallFailure{
			Kind: service.RecallKindNotSupported,
			Err:  fmt.Errorf("%w（渠道 %s 无平台主动撤回）", ErrRecallNotSupported, hub.Platform),
		}
	}
	if time.Since(hub.SentAt) > win {
		return &service.RecallFailure{
			Kind: service.RecallKindWindowExpired,
			Err:  fmt.Errorf("%w（窗口 %s）", ErrRecallWindowExpired, win),
		}
	}
	var err error
	switch hub.Platform {
	case "telegram":
		err = a.recallTelegram(ctx, hub)
	case "feishu":
		err = a.recallFeishu(ctx, hub)
	case "wecom":
		err = a.recallWeCom(ctx, hub)
	case "whatsapp":
		return &service.RecallFailure{
			Kind: service.RecallKindNotSupported,
			Err:  fmt.Errorf("%w（WhatsApp Cloud API 无主动撤回接口，仅用户侧 revoke 通知）", ErrRecallNotSupported),
		}
	default:
		return &service.RecallFailure{
			Kind: service.RecallKindNotSupported,
			Err:  fmt.Errorf("%w（渠道 %s）", ErrChannelNotImplemented, hub.Platform),
		}
	}
	if err != nil {
		logger.Ctx(ctx).Error().Err(err).Str("channel", hub.Platform).Str("msg_id", hub.MsgID).Msg("reach recall failed")
		return &service.RecallFailure{Kind: service.RecallKindChannelError, Err: err}
	}
	logger.Ctx(ctx).Info().Str("channel", hub.Platform).Str("msg_id", hub.MsgID).Msg("reach recall ok")
	return nil
}

// recallWindow 各渠道平台主动撤回的时间窗口（超过则平台拒绝）。
func recallWindow(platform string) (time.Duration, bool) {
	switch platform {
	case "telegram":
		return 48 * time.Hour, true
	case "feishu":
		return 24 * time.Hour, true
	case "wecom":
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}

// recallTelegram 平台撤回（Telegram deleteMessage）。
func (a *IntegrationReachAdapter) recallTelegram(ctx context.Context, hub *model.MessageHub) error {
	if a.tg == nil {
		return fmt.Errorf("recallTelegram: %w", ErrIntegrationServiceNotConfigured)
	}

	platformMsgID, err := platformMessageID(hub)
	if err != nil {
		return err
	}

	var acc model.TelegramAccount
	if err := a.db.WithContext(ctx).Where("id = ?", hub.AccountID).First(&acc).Error; err != nil {
		return fmt.Errorf("recallTelegram: 查 TelegramAccount 无此行: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage", acc.BotToken)
	reqBody := map[string]interface{}{
		"chat_id":    hub.ConversationID,
		"message_id": platformMsgID,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("recallTelegram: 构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("recallTelegram: 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("recallTelegram: TG 返回 %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// recallFeishu 平台撤回（飞书撤回消息 API）。
func (a *IntegrationReachAdapter) recallFeishu(ctx context.Context, hub *model.MessageHub) error {
	if a.feishu == nil {
		return fmt.Errorf("recallFeishu: %w", ErrIntegrationServiceNotConfigured)
	}

	platformMsgID, err := platformMessageID(hub)
	if err != nil {
		return err
	}

	var acc model.FeishuAccount
	if err := a.db.WithContext(ctx).Where("id = ?", hub.AccountID).First(&acc).Error; err != nil {
		return fmt.Errorf("recallFeishu: 查 FeishuAccount 无此行: %w", err)
	}

	token, err := a.getFeishuTenantToken(ctx, &acc)
	if err != nil {
		return fmt.Errorf("recallFeishu: 获取 tenant_access_token 失败: %w", err)
	}

	url := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages/%s", platformMsgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("recallFeishu: 构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("recallFeishu: 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("recallFeishu: 飞书返回 %d: %s", resp.StatusCode, string(body))
	}

	// 飞书的成败不在状态码上：成功与失败都是 HTTP 200，业务码在 body 的 `code`。
	// 只看 StatusCode 的话，平台拒绝了这次撤回也会 return nil（消息其实还在飞书里），
	// 而 Recall 的调用方会把"没撤掉"当成"撤掉了"报给操作者。口径对齐 recallWeCom
	// 与 internal/service/feishu.go 的 `code != 0` 判定。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("recallFeishu: 读取响应失败: %w", err)
	}
	return feishuRecallAPIError(body)
}

// feishuRecallAPIError 把飞书撤回接口的响应体翻译成 Go error：业务码非 0 即失败。
//
// 单独拆成纯函数是为了能不起网络就单测这条判定——它正是原先漏掉的那一步。
func feishuRecallAPIError(body []byte) error {
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("recallFeishu: 解析响应失败: %w", err)
	}
	if r.Code != 0 {
		return fmt.Errorf("recallFeishu: 飞书 errcode=%d errmsg=%s", r.Code, r.Msg)
	}
	return nil
}

// recallWeCom 平台撤回（企微撤回消息 API）。
func (a *IntegrationReachAdapter) recallWeCom(ctx context.Context, hub *model.MessageHub) error {
	if a.wecom == nil {
		return fmt.Errorf("recallWeCom: %w", ErrIntegrationServiceNotConfigured)
	}

	platformMsgID, err := platformMessageID(hub)
	if err != nil {
		return err
	}

	var acc model.WeComAccount
	if err := a.db.WithContext(ctx).Where("id = ?", hub.AccountID).First(&acc).Error; err != nil {
		return fmt.Errorf("recallWeCom: 查 WeComAccount 无此行: %w", err)
	}

	token, err := a.getWeComAccessToken(ctx, &acc)
	if err != nil {
		return fmt.Errorf("recallWeCom: 获取 access_token 失败: %w", err)
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/recall?access_token=%s", token)
	reqBody := map[string]interface{}{
		"msgid": platformMsgID,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("recallWeCom: 构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("recallWeCom: 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("recallWeCom: 企微返回 %d: %s", resp.StatusCode, string(body))
	}

	var respBody struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return fmt.Errorf("recallWeCom: 解析响应失败: %w", err)
	}
	if respBody.ErrCode != 0 {
		return fmt.Errorf("recallWeCom: 企微 errcode=%d errmsg=%s", respBody.ErrCode, respBody.ErrMsg)
	}

	return nil
}

// platformMessageID 从 hub 行反查平台消息标识。
// D-9（2026-10-24）：platform_msg_id 一等列（v3.82.0 迁移 + 发送侧回填）优先；
// 列为空时按原解析口径兜底（存量行 / 迁移不能无歧义解析的行）。
func platformMessageID(hub *model.MessageHub) (string, error) {
	if hub.PlatformMsgID != "" {
		return hub.PlatformMsgID, nil
	}
	switch hub.Platform {
	case "telegram", "feishu":
		parts := strings.Split(hub.MsgID, "-")
		if len(parts) < 3 {
			return "", fmt.Errorf("invalid hub key format: %s", hub.MsgID)
		}
		return parts[len(parts)-1], nil
	case "wecom":
		if hub.Extra != nil {
			if msgid, ok := hub.Extra["msgid"].(string); ok && msgid != "" {
				return msgid, nil
			}
		}
		return "", fmt.Errorf("wecom hub extra missing msgid")
	default:
		return "", fmt.Errorf("unsupported platform: %s", hub.Platform)
	}
}

// getFeishuTenantToken 获取飞书 tenant_access_token（带缓存）。
func (a *IntegrationReachAdapter) getFeishuTenantToken(ctx context.Context, acc *model.FeishuAccount) (string, error) {
	if acc.AccessToken != "" && acc.TokenExpires != nil && time.Now().Before(*acc.TokenExpires) {
		return acc.AccessToken, nil
	}

	url := "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"
	reqBody := map[string]interface{}{
		"app_id":     acc.AppID,
		"app_secret": acc.AppSecret,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var tokenResp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	if tokenResp.Code != 0 {
		return "", fmt.Errorf("feishu auth errcode=%d errmsg=%s", tokenResp.Code, tokenResp.Msg)
	}

	acc.AccessToken = tokenResp.TenantAccessToken
	expiresAt := time.Now().Add(time.Duration(tokenResp.Expire-300) * time.Second)
	acc.TokenExpires = &expiresAt
	a.db.WithContext(ctx).Model(acc).Updates(map[string]interface{}{
		"access_token":  acc.AccessToken,
		"token_expires": acc.TokenExpires,
	})

	return acc.AccessToken, nil
}

// getWeComAccessToken 获取企微 access_token（带缓存）。
func (a *IntegrationReachAdapter) getWeComAccessToken(ctx context.Context, acc *model.WeComAccount) (string, error) {
	if acc.AccessToken != "" && time.Now().Before(acc.TokenExpires) {
		return acc.AccessToken, nil
	}

	url := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s", acc.CorpID, acc.CorpSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var tokenResp struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	if tokenResp.ErrCode != 0 {
		return "", fmt.Errorf("wecom gettoken errcode=%d errmsg=%s", tokenResp.ErrCode, tokenResp.ErrMsg)
	}

	acc.AccessToken = tokenResp.AccessToken
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn-300) * time.Second)
	acc.TokenExpires = expiresAt
	a.db.WithContext(ctx).Model(acc).Updates(map[string]interface{}{
		"access_token":  acc.AccessToken,
		"token_expires": acc.TokenExpires,
	})

	return acc.AccessToken, nil
}

// platformMessageID 从 hub 行反查平台消息标识。
//
// TG/飞书的行键内嵌平台号：tg-out-{acc}-{平台号} / feishu-out-{acc}-{平台id}，
// 取末段即可；企微是 wecom-out-{uuid}（uuid 随机），平台 msgid 落在 Extra；
// WA 的行键就是 wamid 本体，但 WA 无主动撤回，到不了这里。

// AccountHealth 读单个账号的健康度。
//
// 配额与风险只有企微有来源（wecom_account_health 是全仓唯一带配额的账号健康表），
// 其余渠道如实回 not_tracked 与 0，而不是把"没测过"写成"没问题"。
func (a *IntegrationReachAdapter) AccountHealth(ctx context.Context, channel, accountID string) (*tooluse.AccountHealthInfo, error) {
	if accountID == "" {
		return nil, fmt.Errorf("account_health(%s): account_id 必填；整渠道的账号清单走 reach.account.list", channel)
	}
	// 单账号健康只能问一个渠道：channel 传空在 reachAccountChannels 里是"全部渠道"，
	// 让同一个空值在两个工具里一个是"全量"、一个是"取第一个"，AI 拿到的是同一句话的两种答案。
	if channel == "" {
		return nil, errors.New("account_health: channel 必填")
	}
	// 先问"这个渠道有没有账号这一层"，再问"这台实例配没配底座"：sms 没有账号维度是渠道的
	// 永久事实，而 db 句柄缺席只是本机装配状态。反过来排会让同一个问题在配好底座的实例上
	// 回「这个渠道没有账号这一层」、在没配的实例上回「服务未配置」，两个答案指向不同的修法。
	channels, err := reachAccountChannels(channel)
	if err != nil {
		return nil, fmt.Errorf("account_health(%s): %w", channel, err)
	}
	// 显式渠道名只会落进一个 target（多 target 只发生在 channel 传空的那条路径）
	target := channels[0]

	if target == "wecom" {
		return a.wecomHealth(ctx, accountID)
	}
	if a.db == nil {
		return nil, ErrIntegrationServiceNotConfigured
	}
	if bridge.IsBridgeChannel(target) {
		return a.bridgeHealth(ctx, target, accountID)
	}
	return a.apiAccountHealth(ctx, target, accountID)
}

// ListAccounts 列出渠道下已配置的账号（reach.account.list 的实现）。
//
// channel 为空 = 覆盖全部有账号维度的渠道；sms/email/card/web 没有账号这一层，
// 报错而不是回空列表——空列表会被读成"这个渠道一个账号都没有"。
func (a *IntegrationReachAdapter) ListAccounts(ctx context.Context, channel string) ([]tooluse.AccountInfo, error) {
	channels, err := reachAccountChannels(channel)
	if err != nil {
		return nil, err
	}
	if a.db == nil {
		return nil, ErrIntegrationServiceNotConfigured
	}
	out := make([]tooluse.AccountInfo, 0, 16)
	for _, target := range channels {
		var (
			list []tooluse.AccountInfo
			lerr error
		)
		switch {
		case target == "wecom":
			list, lerr = a.listWeComAccounts(ctx)
		case bridge.IsBridgeChannel(target):
			list, lerr = a.listBridgeAccounts(ctx, target)
		default:
			list, lerr = a.listAPIAccounts(ctx, target)
		}
		if lerr != nil {
			return nil, fmt.Errorf("list_accounts(%s): %w", target, lerr)
		}
		out = append(out, list...)
	}
	return out, nil
}

// reachAccountChannels 把工具入参的渠道名收敛成"要读哪几张表"。
// 返回的每一项都必须真的能读，所以未收录的名字直接判错，不静默返回 0 条。
func reachAccountChannels(channel string) ([]string, error) {
	switch channel {
	case "":
		return []string{"telegram", "whatsapp", "feishu", "wecom", "weixin", "dingtalk",
			bridge.ChannelDouyinWeb, bridge.ChannelKuaishouWeb, bridge.ChannelXHSWeb,
			bridge.ChannelTikTok, bridge.ChannelXianyuWeb}, nil
	case "sms", "email", "card", "web":
		return nil, ErrChannelNoAccountDimension
	}
	if bridge.IsBridgeChannel(channel) {
		return []string{bridge.NormalizeBridgeChannel(channel)}, nil
	}
	if isPlainReachChannel(channel) {
		return []string{channel}, nil
	}
	return nil, ErrUnknownReachChannel
}

func isPlainReachChannel(channel string) bool {
	switch channel {
	case "telegram", "whatsapp", "feishu", "wecom", "weixin", "dingtalk":
		return true
	default:
		return false
	}
}

// intStatusAccount 是四张"status 为整数、1=启用"的渠道账号表的公共形状。
type intStatusAccount struct {
	AccountID string
	Nickname  string
	Enabled   bool
}

func (a *IntegrationReachAdapter) listAPIAccounts(ctx context.Context, channel string) ([]tooluse.AccountInfo, error) {
	var rows []intStatusAccount
	switch channel {
	case "telegram":
		var list []model.TelegramAccount
		if err := a.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, intStatusAccount{
				AccountID: strconv.FormatUint(uint64(r.ID), 10),
				Nickname:  r.AccountName,
				Enabled:   r.Status == 1,
			})
		}
	case "whatsapp":
		var list []model.WhatsAppCloudAccount
		if err := a.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, intStatusAccount{
				AccountID: strconv.FormatUint(uint64(r.ID), 10),
				Nickname:  r.AccountName,
				Enabled:   r.Status == 1,
			})
		}
	case "feishu":
		var list []model.FeishuAccount
		if err := a.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, intStatusAccount{
				AccountID: strconv.FormatUint(uint64(r.ID), 10),
				Nickname:  r.AccountName,
				Enabled:   r.Status == 1,
			})
		}
	case "dingtalk":
		var list []model.DingTalkAppAccount
		if err := a.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
			return nil, err
		}
		for _, r := range list {
			rows = append(rows, intStatusAccount{
				AccountID: strconv.FormatUint(uint64(r.ID), 10),
				Nickname:  r.AccountName,
				Enabled:   r.Status == 1,
			})
		}
	case "weixin":
		var list []model.WechatAccount
		if err := a.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
			return nil, err
		}
		out := make([]tooluse.AccountInfo, 0, len(list))
		for _, r := range list {
			out = append(out, tooluse.AccountInfo{
				AccountID: strconv.FormatUint(uint64(r.ID), 10),
				Channel:   channel,
				Nickname:  r.AppID,
				Status:    r.Status,
				IsHealthy: r.Status == "active",
			})
		}
		return out, nil
	default:
		return nil, ErrUnknownReachChannel
	}

	out := make([]tooluse.AccountInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, tooluse.AccountInfo{
			AccountID: r.AccountID,
			Channel:   channel,
			Nickname:  r.Nickname,
			Status:    intStatusText(r.Enabled),
			IsHealthy: r.Enabled,
		})
	}
	return out, nil
}

func intStatusText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// apiAccountHealth 读一张整数状态表的单行。
// 这些渠道的凭证是否真的还能发，只有发的那一刻才知道，所以 RiskLevel 固定 not_tracked。
func (a *IntegrationReachAdapter) apiAccountHealth(ctx context.Context, channel, accountID string) (*tooluse.AccountHealthInfo, error) {
	id, err := parseAccountID(accountID)
	if err != nil {
		return nil, err
	}
	var dest model.TelegramAccount
	q := a.db.WithContext(ctx)
	switch channel {
	case "telegram":
		if err := q.First(&dest, id).Error; err != nil {
			return nil, err
		}
		return &tooluse.AccountHealthInfo{
			AccountID: accountID, Channel: channel,
			Status: intStatusText(dest.Status == 1), RiskLevel: reachRiskNotTracked,
			LastCheckAt: formatReachCheckTime(dest.LastSyncAt),
		}, nil
	case "whatsapp":
		var row model.WhatsAppCloudAccount
		if err := q.First(&row, id).Error; err != nil {
			return nil, err
		}
		return &tooluse.AccountHealthInfo{
			AccountID: accountID, Channel: channel,
			Status: intStatusText(row.Status == 1), RiskLevel: reachRiskNotTracked,
			LastCheckAt: formatReachCheckTime(row.LastSyncAt),
		}, nil
	case "feishu":
		var row model.FeishuAccount
		if err := q.First(&row, id).Error; err != nil {
			return nil, err
		}
		return &tooluse.AccountHealthInfo{
			AccountID: accountID, Channel: channel,
			Status: intStatusText(row.Status == 1), RiskLevel: reachRiskNotTracked,
			LastCheckAt: formatReachCheckTime(row.LastSyncAt),
		}, nil
	case "dingtalk":
		var row model.DingTalkAppAccount
		if err := q.First(&row, id).Error; err != nil {
			return nil, err
		}
		return &tooluse.AccountHealthInfo{
			AccountID: accountID, Channel: channel,
			Status: intStatusText(row.Status == 1), RiskLevel: reachRiskNotTracked,
			LastCheckAt: formatReachCheckTime(row.LastErrorAt),
		}, nil
	case "weixin":
		var row model.WechatAccount
		if err := q.First(&row, id).Error; err != nil {
			return nil, err
		}
		return &tooluse.AccountHealthInfo{
			AccountID: accountID, Channel: channel,
			Status: row.Status, RiskLevel: reachRiskNotTracked,
			LastCheckAt: row.UpdatedAt.UTC().Format(time.RFC3339),
		}, nil
	}
	return nil, ErrUnknownReachChannel
}

// listBridgeAccounts 读 bridge_accounts 里该渠道的账号，在线与否用桥接侧同一份判据。
func (a *IntegrationReachAdapter) listBridgeAccounts(ctx context.Context, channel string) ([]tooluse.AccountInfo, error) {
	var list []model.BridgeAccount
	if err := a.db.WithContext(ctx).Where("channel = ?", channel).Order("id").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]tooluse.AccountInfo, 0, len(list))
	for _, r := range list {
		online := bridge.IsAccountRowOnline(ctx, r.LastSyncAt, r.Status)
		out = append(out, tooluse.AccountInfo{
			AccountID: r.AccountID,
			Channel:   channel,
			Nickname:  r.AccountName,
			Status:    bridgeStatusText(online),
			IsHealthy: online,
		})
	}
	return out, nil
}

func (a *IntegrationReachAdapter) bridgeHealth(ctx context.Context, channel, accountID string) (*tooluse.AccountHealthInfo, error) {
	var row model.BridgeAccount
	err := a.db.WithContext(ctx).
		Where("channel = ? AND account_id = ?", channel, accountID).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	online := bridge.IsAccountRowOnline(ctx, row.LastSyncAt, row.Status)
	return &tooluse.AccountHealthInfo{
		AccountID:   accountID,
		Channel:     channel,
		Status:      bridgeStatusText(online),
		RiskLevel:   reachRiskNotTracked,
		LastCheckAt: formatReachCheckTime(row.LastSyncAt),
	}, nil
}

func bridgeStatusText(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

// listWeComAccounts / wecomHealth 走企微那套已有的账号+健康读口，不再自己拼一次 SQL。
func (a *IntegrationReachAdapter) listWeComAccounts(ctx context.Context) ([]tooluse.AccountInfo, error) {
	if a.wecom == nil {
		return nil, ErrIntegrationServiceNotConfigured
	}
	list, err := a.wecom.ListAccountsWithHealth(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tooluse.AccountInfo, 0, len(list))
	for _, item := range list {
		if item.Account == nil {
			continue
		}
		out = append(out, tooluse.AccountInfo{
			AccountID: strconv.FormatUint(uint64(item.Account.ID), 10),
			Channel:   "wecom",
			Nickname:  item.Account.CorpID,
			Status:    item.Account.LoginState,
			IsHealthy: item.Account.LoginState == service.WeComLoginOnline && item.Account.Status == 1,
		})
	}
	return out, nil
}

func (a *IntegrationReachAdapter) wecomHealth(ctx context.Context, accountID string) (*tooluse.AccountHealthInfo, error) {
	if a.wecom == nil {
		return nil, ErrIntegrationServiceNotConfigured
	}
	list, err := a.wecom.ListAccountsWithHealth(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range list {
		if item.Account == nil || strconv.FormatUint(uint64(item.Account.ID), 10) != accountID {
			continue
		}
		info := &tooluse.AccountHealthInfo{
			AccountID:   accountID,
			Channel:     "wecom",
			Status:      item.Account.LoginState,
			DailyQuota:  item.Account.DailyMsgQuota,
			DailyUsed:   item.Account.DailyMsgUsed,
			RiskLevel:   item.Account.RiskLevel,
			LastCheckAt: formatReachCheckTime(item.Account.LastSyncAt),
		}
		info.DailyRemain = item.Account.DailyMsgQuota - item.Account.DailyMsgUsed
		if info.DailyRemain < 0 {
			info.DailyRemain = 0
		}
		if item.Health != nil {
			info.RiskLevel = item.Health.RiskLevel
			info.DailyUsed = item.Health.QuotaUsed
			if item.Health.QuotaTotal > 0 {
				info.DailyQuota = item.Health.QuotaTotal
				info.DailyRemain = item.Health.QuotaTotal - item.Health.QuotaUsed
				if info.DailyRemain < 0 {
					info.DailyRemain = 0
				}
			}
			info.LastCheckAt = item.Health.ReportedAt.UTC().Format(time.RFC3339)
		}
		if info.RiskLevel == "" {
			info.RiskLevel = reachRiskNotTracked
		}
		return info, nil
	}
	return nil, fmt.Errorf("account_health(wecom): %s: %w", accountID, gorm.ErrRecordNotFound)
}

const reachRiskNotTracked = "not_tracked"

func formatReachCheckTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseAccountID(s string) (uint, error) {
	if s == "" {
		return 0, fmt.Errorf("empty: %w", ErrInvalidAccountID)
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, ErrInvalidAccountID)
	}
	if id == 0 {
		return 0, fmt.Errorf("zero: %w", ErrInvalidAccountID)
	}
	return uint(id), nil
}

func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty: %w", ErrInvalidInt64)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, ErrInvalidInt64)
	}
	if v == 0 {
		return 0, fmt.Errorf("zero: %w", ErrInvalidInt64)
	}
	return v, nil
}
