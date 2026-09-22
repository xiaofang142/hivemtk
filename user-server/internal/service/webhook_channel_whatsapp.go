package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/whatsapp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// waMessageContent 非文本消息的正文。占位符表由适配器导出（whatsapp.InboundPlaceholder）：
// 落库那一行由 Ingress 写，这里只是 dispatch 的内存视图，两处各一份表迟早分叉。
func waMessageContent(msgType string, body string) string {
	if msgType == "text" {
		return body
	}
	return whatsapp.InboundPlaceholder(msgType)
}

func (s *WebhookService) dispatchWhatsApp(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if s.lazyDB() == nil {
		return nil, nil
	}
	s.ensureReposFromDB(ctx)

	if handled, err := s.dispatchWhatsAppStatuses(ctx, accountID, raw); handled {
		return nil, err
	}

	// 审计 N-04：这里原先过一次 globalReorderBuffer「乱序缓冲」，已删除——它的
	// delayed 分支不可达（Offer 只在缓冲区内 >=2 条时建定时器，而每次 flush 都会
	// 删掉会话条目，长度恒为 1），实为纯直通。删除依据与等价性由
	// TestDispatchWhatsApp_OutOfOrderArrivalsAreNotHeld 在删除前后各跑一次坐实。
	// WhatsApp 官方只承诺「并发投递 + 可能重复」，从未承诺顺序（见审计 §6），
	// 真正的防护是 wamid 幂等去重，而不是客户端重排。

	waPayload, err := whatsapp.ParseWebhook(raw)
	if err != nil {
		return nil, fmt.Errorf("whatsapp parse: %w", err)
	}

	// 与 Telegram 同构：本渠道的 AI 触发由 handleJob 末尾按账号 AI 开关
	// （shouldTriggerAI → triggerSalesEngine）负责，中台 Ingress 只落库。
	// 不打标记时两条路径会对同一条 wamid 各跑一次推理（去重键不同：Ingress 用 wamid，
	// triggerSalesEngine 用 webhook job 的 EventID），客户收到两条回复。
	if err := waPayload.Ingress(WithChannelOwnedAITrigger(ctx), s.ingressHandler(ctx), accountID); err != nil {
		return nil, err
	}

	// 媒体消息按 wamid 建索引：一条推送可以带多条媒体，只取首条会漏存（N-10）。
	mediaByWAMID := waPayload.MediaByMsgID()

	var firstHub *model.MessageHub
	var contents []string
	for _, e := range waPayload.Entry {
		for _, c := range e.Changes {
			for _, msg := range c.Value.Messages {
				content := waMessageContent(msg.Type, msg.Text.Body)
				// 非文本消息：保留渠道原生引用（media_id/mime/filename），供展示与 AI 理解
				var mediaExtra map[string]any
				var mediaID, mediaFilename string
				if ref, ok := mediaByWAMID[msg.ID]; ok {
					if id, mime, fname, ok := ref.Media(); ok {
						mediaExtra = map[string]any{
							"media_id":  id,
							"mime_type": mime,
						}
						if fname != "" {
							mediaExtra["filename"] = fname
						}
						mediaID, mediaFilename = id, fname
					}
				}

				name := msg.From
				for _, ct := range c.Value.Contacts {
					if ct.WAID == msg.From {
						name = ct.Profile.Name
						break
					}
				}

				hub := &model.MessageHub{
					Platform:       "whatsapp",
					AccountID:      accountID,
					MsgID:          msg.ID,
					Direction:      "inbound",
					SenderID:       msg.From,
					ConversationID: msg.From,
					MsgType:        InboundHubMsgType(msg.Type),
					Content:        content,
					SentAt:         time.Now(),
				}
				if mediaExtra != nil {
					hub.Extra = mediaExtra
				}

				s.upsertInboxFromHub(ctx, hub, name)

				// 媒体消息转存（best-effort）：media id 仅 7 天有效，异步下载并把长期 URL
				// 回填到**本行** message_hub.media_url（键 = wamid，不是 media_id）。
				if s.messageHubRepo != nil && mediaID != "" {
					s.persistWhatsAppMediaAsync(ctx, accountID, msg.ID, mediaID, mediaFilename)
				}

				MineUnifiedLead(ctx, s, hub, WhatsAppLeadAdapter{}, accountID, "", "", msg.From, name, "", content)

				if firstHub == nil {
					firstHub = hub
					p.Sender = msg.From
					p.ChatID = msg.From
				}
				contents = append(contents, content)
			}
		}
	}
	// M-02：一次推送可以带多条消息，handleJob 拿 p.Content 当 AI 的 UserMessage
	// （webhook_ai.go 的 PublishCustomerMessage / SalesRequest.UserMessage 同源），
	// 只带首条会让连发的"多少钱 / 有现货吗 / 能开发票吗"只得到第一个问题的回答。
	// 口径与中台批量入口一致：N 条合成一份输入、一次回复。
	if len(contents) > 0 {
		p.Content = strings.Join(contents, "\n")
	}
	return firstHub, nil
}

func (s *WebhookService) dispatchWhatsAppStatuses(ctx context.Context, accountID string, raw []byte) (bool, error) {
	s.ensureReposFromDB(ctx)
	payload, err := whatsapp.ParseWebhook(raw)
	if err != nil {
		return false, nil
	}
	hasStatuses := false
	hasMessages := false
	for _, e := range payload.Entry {
		for _, c := range e.Changes {
			if len(c.Value.Messages) > 0 {
				hasMessages = true
			}
			for _, st := range c.Value.Statuses {
				hasStatuses = true
				if s.messageHubRepo == nil {
					break
				}
				reason := ""
				if len(st.Errors) > 0 {
					reason = st.Errors[0].Title + ": " + st.Errors[0].Message
				}
				if uerr := s.messageHubRepo.UpdateDeliveryStatus(ctx, "whatsapp", accountID, st.ID, st.Status, reason); uerr != nil {

					logger.Infof("[WhatsApp] status writeback miss wamid=%s status=%s: %v", st.ID, st.Status, uerr)
				}
			}
		}
	}
	if !hasStatuses {
		return false, nil
	}
	if hasMessages {

		return false, nil
	}
	logger.Infof("[Webhook] whatsapp statuses consumed account=%s", accountID)
	return true, nil
}

// 媒体转存的两个外部 IO 边界以函数变量注入：真实下载要访问 graph.facebook.com，
// 转存要经 obs_config/存储驱动，测试环境两者都不可达，用替身跑通
// 「取凭证 → 下载 → 转存 → 回填」全链（生产指向实现本身）。
var (
	waMediaFetchFn = FetchWhatsAppMedia
	waMediaStoreFn = channelMediaPersist
)

// persistWhatsAppMediaAsync 异步下载 WA 媒体并转存，成功后按 msgID（wamid，即本条消息
// 落 message_hub 时的 msg_id）回填该行的 media_url。
// 失败仅告警（占位符文本已入库，不影响主链路）。
func (s *WebhookService) persistWhatsAppMediaAsync(ctx context.Context, accountID, msgID, mediaID, filename string) {
	accID, _ := strconv.ParseUint(accountID, 10, 64)
	utils.SafeGo(ctx, "whatsapp.media_persist", func(gctx context.Context) {
		token, _, err := s.waCloudSecrets(gctx, accountID)
		if err != nil || token == "" {
			logger.Ctx(gctx).Warn().Str("account_id", accountID).Msg("[WhatsApp] 媒体转存跳过：账号凭证缺失")
			return
		}
		acc, gerr := s.waCloudAccount(gctx, accID)
		if gerr != nil || acc == nil {
			logger.Ctx(gctx).Warn().Str("account_id", accountID).Msg("[WhatsApp] 媒体转存跳过：账号不存在")
			return
		}
		rc, contentType, ferr := waMediaFetchFn(gctx, token, acc.PhoneNumberID, mediaID)
		if ferr != nil {
			logger.Ctx(gctx).Warn().Err(ferr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体下载失败（占位符保留）")
			return
		}
		defer func() { _ = rc.Close() }()
		data, rerr := readInboundMedia(rc, maxInboundMediaBytes)
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体读取失败（占位符保留）")
			return
		}
		publicURL, serr := waMediaStoreFn(gctx, "whatsapp", mediaID, data, contentType, filename)
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体转存失败")
			return
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "whatsapp", accountID, msgID, publicURL)
		logger.Ctx(gctx).Info().Str("msg_id", msgID).Str("media_id", mediaID).Str("url", publicURL).Msg("[WhatsApp] 媒体已转存")
	})
}

// waCloudSecrets 取 WA Cloud 账号 token（复用 WhatsAppCloudService）。
func (s *WebhookService) waCloudSecrets(ctx context.Context, accountID string) (token, appSecret string, err error) {
	svc := NewWhatsAppCloudService(s.lazyDB())
	return svc.GetSecretsByAccountID(ctx, accountID)
}

// waCloudAccount 取 WA Cloud 账号（需要 PhoneNumberID 拼下载 URL）。
func (s *WebhookService) waCloudAccount(ctx context.Context, id uint64) (*model.WhatsAppCloudAccount, error) {
	svc := NewWhatsAppCloudService(s.lazyDB())
	return svc.GetAccount(ctx, uint(id))
}
