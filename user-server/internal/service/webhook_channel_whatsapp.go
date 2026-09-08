package service

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"hivemtk-user/internal/channelbot/whatsapp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

func waMessageContent(msgType string, body string) string {
	if msgType == "text" {
		return body
	}
	switch msgType {
	case "image":
		return "[图片]"
	case "audio":
		return "[语音]"
	case "video":
		return "[视频]"
	case "document":
		return "[文件]"
	default:
		return "[" + msgType + "]"
	}
}

func (s *WebhookService) dispatchWhatsApp(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if s.db == nil {
		return nil, nil
	}
	s.ensureReposFromDB(ctx)

	if handled, err := s.dispatchWhatsAppStatuses(ctx, accountID, raw); handled {
		return nil, err
	}

	var bufSessionID, bufMsgID string
	var bufTimestampMs int64
	if waPre, err := whatsapp.ParseWebhook(raw); err == nil {
		for _, ent := range waPre.Entry {
			for _, ch := range ent.Changes {
				for _, m := range ch.Value.Messages {
					tsSec, _ := strconv.ParseInt(m.Timestamp, 10, 64)
					if tsSec > 0 && bufSessionID == "" {
						bufSessionID = m.From
						bufMsgID = m.ID
						bufTimestampMs = tsSec * 1000
						break
					}
				}
				if bufSessionID != "" {
					break
				}
			}
			if bufSessionID != "" {
				break
			}
		}
	}
	if bufSessionID != "" {
		_, delayed := globalReorderBuffer.Offer(accountID, bufSessionID, bufMsgID, bufTimestampMs, raw)
		if delayed {
			logger.Infof("[Webhook] WhatsApp session=%s delayed by reorder buffer", bufSessionID)
			return nil, nil
		}
	}

	waPayload, err := whatsapp.ParseWebhook(raw)
	if err != nil {
		return nil, fmt.Errorf("whatsapp parse: %w", err)
	}

	if err := waPayload.Ingress(ctx, s.ingressHandler(ctx), accountID); err != nil {
		return nil, err
	}

	// 媒体消息转存（best-effort）：media id 仅 7 天有效，异步下载并把长期 URL 回填 message_hub.media_url
	if s.messageHubRepo != nil {
		if mediaID, _, filename, ok := waPayload.MediaRef(); ok {
			s.ensureReposFromDB(ctx)
			s.persistWhatsAppMediaAsync(ctx, accountID, mediaID, filename)
		}
	}

	var firstHub *model.MessageHub
	for _, e := range waPayload.Entry {
		for _, c := range e.Changes {
			for _, msg := range c.Value.Messages {
				content := waMessageContent(msg.Type, msg.Text.Body)
				// 非文本消息：保留渠道原生引用（media_id/mime/filename），供展示与 AI 理解
				var mediaExtra map[string]any
				if msg.Type != "text" {
					if r, fname, mok := mediaRefOfMsg(&msg); mok {
						mediaExtra = map[string]any{
							"media_id":  r.MediaID,
							"mime_type": r.MimeType,
						}
						if fname != "" {
							mediaExtra["filename"] = fname
						}
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
					MsgType:        msg.Type,
					Content:        content,
					SentAt:         time.Now(),
				}
				if mediaExtra != nil {
					hub.Extra = mediaExtra
				}

				s.upsertInboxFromHub(ctx, hub, name)

				MineUnifiedLead(ctx, s, hub, WhatsAppLeadAdapter{}, accountID, "", "", msg.From, name, "", content)

				if firstHub == nil {
					firstHub = hub
					p.Content = content
					p.Sender = msg.From
					p.ChatID = msg.From
				}
			}
		}
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

// mediaRefOfMsg 从单条 WA webhook 消息提取媒体引用（供 hub.Extra 落库）。
// 直接接收匿名结构体指针，避免暴露 channelbot 内部类型映射。
func mediaRefOfMsg(msg *struct {
	From      string `json:"from"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Text      struct {
		Body string `json:"body"`
	} `json:"text"`
	Image    *whatsapp.WAMedia `json:"image,omitempty"`
	Audio    *whatsapp.WAMedia `json:"audio,omitempty"`
	Video    *whatsapp.WAMedia `json:"video,omitempty"`
	Document *struct {
		whatsapp.WAMedia
		Filename string `json:"filename"`
	} `json:"document,omitempty"`
	Sticker *whatsapp.WAMedia `json:"sticker,omitempty"`
}) (whatsapp.WAMedia, string, bool) {
	var empty whatsapp.WAMedia
	switch msg.Type {
	case "image":
		if msg.Image != nil {
			return *msg.Image, "", true
		}
	case "audio":
		if msg.Audio != nil {
			return *msg.Audio, "", true
		}
	case "video":
		if msg.Video != nil {
			return *msg.Video, "", true
		}
	case "sticker":
		if msg.Sticker != nil {
			return *msg.Sticker, "", true
		}
	case "document":
		if msg.Document != nil {
			return msg.Document.WAMedia, msg.Document.Filename, true
		}
	}
	return empty, "", false
}

// persistWhatsAppMediaAsync 异步下载 WA 媒体并转存，成功后按 media_id 定位 hub 行回填 media_url。
// 失败仅告警（占位符文本已入库，不影响主链路）。
func (s *WebhookService) persistWhatsAppMediaAsync(ctx context.Context, accountID, mediaID, filename string) {
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
		rc, contentType, ferr := FetchWhatsAppMedia(gctx, token, acc.PhoneNumberID, mediaID)
		if ferr != nil {
			logger.Ctx(gctx).Warn().Err(ferr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体下载失败（占位符保留）")
			return
		}
		defer rc.Close()
		data, rerr := io.ReadAll(io.LimitReader(rc, maxInboundMediaBytes))
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体读取失败")
			return
		}
		publicURL, serr := channelMediaPersist(gctx, "whatsapp", mediaID, data, contentType, filename)
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[WhatsApp] 媒体转存失败")
			return
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "whatsapp", accountID, mediaID, publicURL)
		logger.Ctx(gctx).Info().Str("media_id", mediaID).Str("url", publicURL).Msg("[WhatsApp] 媒体已转存")
	})
}

// waCloudSecrets 取 WA Cloud 账号 token（复用 WhatsAppCloudService）。
func (s *WebhookService) waCloudSecrets(ctx context.Context, accountID string) (token, appSecret string, err error) {
	svc := NewWhatsAppCloudService(s.db)
	return svc.GetSecretsByAccountID(ctx, accountID)
}

// waCloudAccount 取 WA Cloud 账号（需要 PhoneNumberID 拼下载 URL）。
func (s *WebhookService) waCloudAccount(ctx context.Context, id uint64) (*model.WhatsAppCloudAccount, error) {
	svc := NewWhatsAppCloudService(s.db)
	return svc.GetAccount(ctx, uint(id))
}
