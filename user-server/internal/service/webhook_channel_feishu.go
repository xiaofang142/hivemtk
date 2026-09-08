package service

import (
	"context"

	"crypto/subtle"

	"encoding/json"

	"errors"

	"fmt"

	"io"

	"strconv"

	"strings"

	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

func (s *WebhookService) getFeishuEncryptKey(ctx context.Context, accountID string) string {
	if s.db == nil || s.feishuRepo == nil {
		return ""
	}
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil || id == 0 {
		return ""
	}
	acc, err := s.feishuRepo.GetByID(ctx, uint(id))
	if err != nil || acc == nil {
		return ""
	}
	return acc.EncryptKey
}

// HandleFeishuURLVerification 处理飞书事件订阅的 POST url_verification 挑战。
// 官方流程：配置回调 URL 时飞书 POST {"challenge":"...","token":"...","type":"url_verification"}
// （开启 Encrypt Key 时为 {"encrypt":"..."} 信封），服务端必须原样回显 {"challenge": "..."}。
// 返回 (challenge, true, nil) 表示已处理；(…, false, nil) 表示非验证请求（调用方继续常规处理）；
// 校验失败返回 err（token 与账号 VerificationToken 不匹配等，防任意第三方伪造绑定）。
func (s *WebhookService) HandleFeishuURLVerification(ctx context.Context, accountID string, raw []byte) (string, bool, error) {
	payload := raw
	var probe struct {
		Encrypt   string `json:"encrypt"`
		Challenge string `json:"challenge"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return "", false, nil
	}
	isVerification := probe.Type == "url_verification"
	if !isVerification && probe.Encrypt == "" {
		return "", false, nil
	}
	if probe.Encrypt != "" {
		key := s.getFeishuEncryptKey(ctx, accountID)
		if key == "" {
			return "", true, errors.New("feishu encrypt_key not configured")
		}
		plain, derr := DecryptFeishuEvent(key, probe.Encrypt)
		if derr != nil {
			return "", true, fmt.Errorf("decrypt url_verification: %w", derr)
		}
		payload = plain
	}
	var req struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return "", true, fmt.Errorf("parse url_verification: %w", err)
	}
	if req.Type != "url_verification" || req.Challenge == "" {
		if isVerification {

			return "", true, fmt.Errorf("url_verification missing challenge")
		}
		return "", false, nil
	}
	id, perr := strconv.ParseUint(accountID, 10, 64)
	if perr != nil || id == 0 || s.feishuRepo == nil {
		return "", true, errors.New("invalid feishu account_id")
	}
	acc, gerr := s.feishuRepo.GetByID(ctx, uint(id))
	if gerr != nil || acc == nil {
		return "", true, errors.New("feishu account not found")
	}

	if acc.VerificationToken == "" || subtle.ConstantTimeCompare([]byte(req.Token), []byte(acc.VerificationToken)) != 1 {
		return "", true, errors.New("feishu verification token mismatch")
	}
	return req.Challenge, true, nil
}

func (s *WebhookService) dispatchFeishu(ctx context.Context, accountID string, p *ParsedPayload, raw []byte) (*model.MessageHub, error) {
	if s.db == nil {
		return nil, nil
	}
	s.ensureReposFromDB(ctx)

	var envProbe struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &envProbe); err == nil && envProbe.Encrypt != "" {
		key := s.getFeishuEncryptKey(ctx, accountID)
		if key == "" {
			return nil, fmt.Errorf("feishu encrypted event but encrypt_key not configured (account=%s)", accountID)
		}
		plain, derr := DecryptFeishuEvent(key, envProbe.Encrypt)
		if derr != nil {
			return nil, fmt.Errorf("feishu decrypt event: %w", derr)
		}
		raw = plain
	}

	var fsPayload struct {
		Challenge string `json:"challenge"`
		Type      string `json:"type"`
		Header    *struct {
			EventType    string `json:"event_type"`
			AppID        string `json:"app_id"`
			TenantKey    string `json:"tenant_key"`
			EventID      string `json:"event_id"`
			Token        string `json:"token"`
			CreateTime   int64  `json:"create_time"`
			AppSecretVer int    `json:"app_secret_ver"`
		} `json:"header,omitempty"`
		Event *struct {
			Sender *struct {
				SenderID *struct {
					UnionID string `json:"union_id"`
					UserID  string `json:"user_id"`
					OpenID  string `json:"open_id"`
				} `json:"sender_id"`
				SenderType string `json:"sender_type"`
				TenantKey  string `json:"tenant_key"`
			} `json:"sender"`
			Message *struct {
				MessageID   string `json:"message_id"`
				ChatID      string `json:"chat_id"`
				ChatType    string `json:"chat_type"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
				CreateTime  int64  `json:"create_time"`
			} `json:"message"`
		} `json:"event,omitempty"`
	}
	if err := json.Unmarshal(raw, &fsPayload); err != nil {
		return nil, fmt.Errorf("feishu parse: %w", err)
	}

	if fsPayload.Challenge != "" && (fsPayload.Type == "url_verification" || fsPayload.Header == nil) {

		return nil, nil
	}
	if fsPayload.Event == nil || fsPayload.Event.Message == nil {
		return nil, nil
	}
	m := fsPayload.Event.Message

	var contentObj struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal([]byte(m.Content), &contentObj)
	content := contentObj.Text
	if content == "" {
		content = "[" + m.MessageType + "]"
	}
	// 飞书媒体消息：content JSON 带 image_key/file_key，保留到 hub.Extra 供转存与展示
	mediaFileKey := ""
	switch m.MessageType {
	case "image":
		var imgObj struct {
			ImageKey string `json:"image_key"`
		}
		_ = json.Unmarshal([]byte(m.Content), &imgObj)
		mediaFileKey = imgObj.ImageKey
	case "file", "audio", "media":
		var fileObj struct {
			FileKey string `json:"file_key"`
		}
		_ = json.Unmarshal([]byte(m.Content), &fileObj)
		mediaFileKey = fileObj.FileKey
	}
	senderID := ""
	if fsPayload.Event.Sender != nil && fsPayload.Event.Sender.SenderID != nil {
		senderID = fsPayload.Event.Sender.SenderID.OpenID
		if senderID == "" {
			senderID = fsPayload.Event.Sender.SenderID.UserID
		}
		if senderID == "" {
			senderID = fsPayload.Event.Sender.SenderID.UnionID
		}
	}
	hub := &model.MessageHub{
		Platform:       "feishu",
		AccountID:      accountID,
		MsgID:          m.MessageID,
		Direction:      "inbound",
		SenderID:       senderID,
		ConversationID: m.ChatID,
		MsgType:        m.MessageType,
		Content:        content,
		SentAt:         time.Now(),
		IsGroup:        m.ChatType == "group",
		GroupID:        m.ChatID,
	}
	if mediaFileKey != "" {
		hub.Extra = model.JSONMap{"file_key": mediaFileKey, "message_id": m.MessageID}
	}
	if err := s.messageHubRepo.Create(ctx, hub); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "duplicate") {
			return nil, err
		}
	}
	// 媒体转存（best-effort）：异步下载飞书资源并回填长期 URL
	if mediaFileKey != "" {
		s.persistFeishuMediaAsync(ctx, accountID, m.MessageID, mediaFileKey, m.MessageType)
	}
	s.upsertInboxFromHub(ctx, hub, "")

	MineUnifiedLead(ctx, s, hub, FeishuLeadAdapter{}, accountID, m.ChatID, "", senderID, "", "", content)

	p.Content = content
	p.Sender = senderID
	p.ChatID = m.ChatID
	return hub, nil
}

// persistFeishuMediaAsync 异步下载飞书消息资源并转存，按 msg_id 回填 message_hub.media_url。
func (s *WebhookService) persistFeishuMediaAsync(ctx context.Context, accountID, messageID, fileKey, msgType string) {
	utils.SafeGo(ctx, "feishu.media_persist", func(gctx context.Context) {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID == 0 {
			return
		}
		acc, gerr := NewFeishuService(s.db).GetAccount(gctx, uint(accID))
		if gerr != nil || acc == nil {
			logger.Ctx(gctx).Warn().Str("account_id", accountID).Msg("[Feishu] 媒体转存跳过：账号不存在")
			return
		}
		integration := NewFeishuIntegrationService(s.db)
		tenantToken, tkerr := integration.getAccessToken(gctx, acc)
		if tkerr != nil || tenantToken == "" {
			logger.Ctx(gctx).Warn().Err(tkerr).Str("account_id", accountID).Msg("[Feishu] 媒体转存跳过：tenant_access_token 获取失败")
			return
		}
		resType := "file"
		if msgType == "image" {
			resType = "image"
		}
		rc, contentType, derr := FetchFeishuMedia(gctx, tenantToken, messageID, fileKey, resType)
		if derr != nil {
			logger.Ctx(gctx).Warn().Err(derr).Str("file_key", fileKey).Msg("[Feishu] 媒体下载失败（占位符保留）")
			return
		}
		defer rc.Close()
		data, rerr := io.ReadAll(io.LimitReader(rc, maxInboundMediaBytes))
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("file_key", fileKey).Msg("[Feishu] 媒体读取失败")
			return
		}
		publicURL, serr := channelMediaPersist(gctx, "feishu", fileKey, data, contentType, "")
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("file_key", fileKey).Msg("[Feishu] 媒体转存失败")
			return
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "feishu", accountID, messageID, publicURL)
		logger.Ctx(gctx).Info().Str("file_key", fileKey).Str("url", publicURL).Msg("[Feishu] 媒体已转存")
	})
}
