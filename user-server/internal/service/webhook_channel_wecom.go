package service

import (
	"context"

	"crypto/aes"

	"crypto/cipher"

	"crypto/subtle"

	"encoding/base64"

	"encoding/binary"

	"encoding/json"

	"errors"

	"fmt"

	"io"

	"strconv"

	"strings"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

func verifyWeCom(token, aesKey string, body []byte, query map[string]string) (bool, error) {
	if token == "" {
		return false, errors.New("missing token")
	}

	var p struct {
		MsgSignature string `json:"msg_signature"`
		Timestamp    string `json:"timestamp"`
		Nonce        string `json:"nonce"`
		Encrypt      string `json:"encrypt"`
	}
	if err := json.Unmarshal(body, &p); err != nil {

		if query != nil {
			p.MsgSignature = query["msg_signature"]
			p.Timestamp = query["timestamp"]
			p.Nonce = query["nonce"]
		}
	}
	if p.MsgSignature == "" && query != nil {
		p.MsgSignature = query["msg_signature"]
		p.Timestamp = query["timestamp"]
		p.Nonce = query["nonce"]
	}
	if p.Timestamp == "" {
		p.Timestamp = query["timestamp"]
	}
	if p.Nonce == "" {
		p.Nonce = query["nonce"]
	}
	if p.MsgSignature == "" || p.Timestamp == "" || p.Nonce == "" {
		return false, errors.New("missing msg_signature/timestamp/nonce")
	}

	fourth := p.Encrypt
	if fourth == "" {
		fourth = query["echostr"]
	}
	parts := []string{token, p.Timestamp, p.Nonce, fourth}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	if subtle.ConstantTimeCompare([]byte(h), []byte(p.MsgSignature)) != 1 {
		return false, errors.New("signature mismatch")
	}
	return true, nil
}

func verifyWechat(token string, body []byte, headers map[string]string) bool {
	if token == "" {
		return false
	}
	ts := headers["X-Wechat-Timestamp"]
	nonce := headers["X-Wechat-Nonce"]
	sig := headers["X-Wechat-Signature"]
	if sig == "" {
		sig = headers["signature"]
	}
	if ts == "" || nonce == "" || sig == "" {
		return false
	}

	parts := []string{token, ts, nonce}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	return subtle.ConstantTimeCompare([]byte(h), []byte(sig)) == 1
}

func DecryptWeComMessage(aesKey, encrypted string) ([]byte, error) {
	if len(aesKey) != 43 {
		return nil, fmt.Errorf("invalid EncodingAESKey length: %d", len(aesKey))
	}

	key := aesKey + "="
	keyB, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	cipherB, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decode cipher: %w", err)
	}
	if len(cipherB) < 16 || len(cipherB)%16 != 0 {
		return nil, fmt.Errorf("invalid cipher length: %d", len(cipherB))
	}
	block, err := aes.NewCipher(keyB)
	if err != nil {
		return nil, err
	}
	iv := cipherB[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(cipherB)-16)
	mode.CryptBlocks(plain, cipherB[16:])

	plen := int(plain[len(plain)-1])
	if plen < 1 || plen > 32 {
		return nil, fmt.Errorf("invalid padding: %d", plen)
	}
	plain = plain[:len(plain)-plen]

	if len(plain) < 20 {
		return nil, fmt.Errorf("plain too short: %d", len(plain))
	}
	msgLen := int(binary.BigEndian.Uint32(plain[16:20]))
	if 20+msgLen > len(plain) {
		return nil, fmt.Errorf("msg_len overflow: %d", msgLen)
	}
	return plain[20 : 20+msgLen], nil
}

func VerifyURL(token, aesKey, msgSignature, timestamp, nonce, echostr string) (string, error) {
	if len(aesKey) != 43 {
		return "", errors.New("invalid EncodingAESKey")
	}

	plain, err := DecryptWeComMessage(aesKey, echostr)
	if err != nil {
		return "", fmt.Errorf("decrypt echostr: %w", err)
	}

	plainStr := strings.TrimRight(string(plain), "\x00")
	if len(plainStr) > 20 {
		msgLen := int(binary.BigEndian.Uint32([]byte(plainStr)[16:20]))
		if 20+msgLen <= len(plainStr) {
			plainStr = plainStr[20 : 20+msgLen]
		}
	}

	parts := []string{token, timestamp, nonce, plainStr}
	sortStrings(parts)
	h := sha1Hex([]byte(strings.Join(parts, "")))
	if subtle.ConstantTimeCompare([]byte(h), []byte(msgSignature)) != 1 {
		return "", errors.New("signature mismatch")
	}

	return plainStr, nil
}

func (s *WebhookService) dispatchWeCom(ctx context.Context, accountID string, p *ParsedPayload, raw []byte, headers map[string]string) (*model.MessageHub, error) {
	if s.integration == nil {
		return nil, nil
	}

	plain := s.parseWeComPlain(ctx, accountID, raw)
	if plain == nil {

		plain = p.Extra
	}
	if plain == nil {
		return nil, fmt.Errorf("wecom plain nil")
	}

	fromUser := getString(plain, "FromUserName", "from")
	fromName := getString(plain, "FromUserName")
	msgType := strings.ToLower(getString(plain, "MsgType", "msg_type"))
	content := getString(plain, "Content", "content", "Text", "text")
	if content == "" {

		switch msgType {
		case "image":
			content = "[图片]"
		case "voice":
			content = "[语音]"
		case "video":
			content = "[视频]"
		case "file":
			content = "[文件]"
		case "location":
			content = "[位置]"
		case "link":
			content = getString(plain, "Title", "title") + " " + getString(plain, "Url", "url")
		default:
			content = getString(plain, "Content", "content", "Text", "text")
		}
	}
	mediaID := getString(plain, "MediaId", "media_id")
	chatID := getString(plain, "ChatId", "chat_id")
	chatType := getString(plain, "ChatType", "chat_type")
	event := getString(plain, "Event", "event")
	msgID := getString(plain, "MsgId", "msg_id")

	if msgType == "event" {
		logger.Infof("[Webhook] wecom event=%s from=%s", event, fromUser)
		return nil, nil
	}

	var accID uint64
	if v, err := strconv.ParseUint(accountID, 10, 64); err == nil && v > 0 {
		accID = v
	} else {

		acc, gerr := s.wecomRepo.GetByMerchant(ctx)
		if gerr != nil || len(acc) == 0 {
			return nil, fmt.Errorf("invalid account_id")
		}
		accID = uint64(acc[0].ID)
	}

	hubMsg, _, err := s.integration.ReceiveCallback(ctx, &ReceiveCallbackRequest{
		AccountID: uint(accID),
		FromUser:  fromUser,
		FromName:  fromName,
		MsgType:   msgType,
		Content:   content,
		MsgID:     msgID,
		MediaID:   mediaID,
		ChatID:    chatID,
		ChatType:  chatType,
	})

	if err == nil {
		p.Content = content
		p.Sender = fromUser
		p.ChatID = chatID

		if hubMsg != nil && content != "" && fromUser != "" && msgType != "event" {
			MineUnifiedLead(ctx, s, hubMsg, WeComLeadAdapter{}, accountID, chatID, "", fromUser, fromName, "", content)
		}
		// 媒体消息转存（best-effort）：企微 media_id 仅 3 天有效，异步下载回填长期 URL
		if mediaID != "" && hubMsg != nil && isWeComMediaMsgType(msgType) {
			s.persistWeComMediaAsync(ctx, accountID, hubMsg.MsgID, mediaID, msgType)
		}
	}
	return hubMsg, err
}

// isWeComMediaMsgType 企微携带 MediaId 的消息类型。
func isWeComMediaMsgType(msgType string) bool {
	switch msgType {
	case "image", "voice", "video", "file":
		return true
	}
	return false
}

// persistWeComMediaAsync 异步下载企微媒体并转存，按 msg_id 回填 message_hub.media_url。
func (s *WebhookService) persistWeComMediaAsync(ctx context.Context, accountID, msgID, mediaID, msgType string) {
	if s.wecomRepo == nil {
		return
	}
	utils.SafeGo(ctx, "wecom.media_persist", func(gctx context.Context) {
		accID, _ := strconv.ParseUint(accountID, 10, 64)
		if accID == 0 {
			if accs, gerr := s.wecomRepo.GetByMerchant(gctx); gerr == nil && len(accs) > 0 {
				accID = uint64(accs[0].ID)
			}
		}
		if accID == 0 {
			return
		}
		token, terr := s.wecomAccessToken(gctx, uint(accID))
		if terr != nil || token == "" {
			logger.Ctx(gctx).Warn().Err(terr).Str("account_id", accountID).Msg("[WeCom] 媒体转存跳过：access_token 获取失败")
			return
		}
		rc, contentType, derr := FetchWeComMedia(gctx, token, mediaID)
		if derr != nil {
			logger.Ctx(gctx).Warn().Err(derr).Str("media_id", mediaID).Msg("[WeCom] 媒体下载失败（占位符保留）")
			return
		}
		defer rc.Close()
		data, rerr := io.ReadAll(io.LimitReader(rc, maxInboundMediaBytes))
		if rerr != nil {
			logger.Ctx(gctx).Warn().Err(rerr).Str("media_id", mediaID).Msg("[WeCom] 媒体读取失败")
			return
		}
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = wecomDefaultContentType(msgType)
		}
		publicURL, serr := channelMediaPersist(gctx, "wecom", mediaID, data, contentType, "")
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_id", mediaID).Msg("[WeCom] 媒体转存失败")
			return
		}
		if s.messageHubRepo == nil {
			s.ensureReposFromDB(gctx)
		}
		EnrichHubMediaURLByMsgID(gctx, s.messageHubRepo, "wecom", accountID, msgID, publicURL)
		logger.Ctx(gctx).Info().Str("media_id", mediaID).Str("url", publicURL).Msg("[WeCom] 媒体已转存")
	})
}

// wecomDefaultContentType 企微 media/get 不回 Content-Type 时按 MsgType 推断。
func wecomDefaultContentType(msgType string) string {
	switch msgType {
	case "image":
		return "image/jpeg"
	case "voice":
		return "audio/amr"
	case "video":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

// wecomAccessToken 取企微 access_token（复用 WeComService 缓存逻辑）。
func (s *WebhookService) wecomAccessToken(ctx context.Context, accountID uint) (string, error) {
	acc, err := s.wecomRepo.GetByID(ctx, accountID)
	if err != nil || acc == nil {
		return "", fmt.Errorf("wecom account %d not found", accountID)
	}
	svc := NewWeComServiceWithDB(s.db)
	return svc.GetAccessToken(ctx, acc)
}

func (s *WebhookService) parseWeComPlain(ctx context.Context, accountID string, raw []byte) map[string]any {
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	enc, _ := p["encrypt"].(string)
	if enc == "" {
		return p
	}

	if s.wecomRepo == nil {
		return nil
	}

	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		if acc, gerr := s.wecomRepo.GetByID(ctx, uint(id)); gerr == nil && acc != nil && acc.EncodingAESKey != "" {
			if out := decryptWeComPayload(acc.EncodingAESKey, enc); out != nil {
				if validateWeComAgentID(out, acc.AgentID) {
					return out
				}
				logger.Warnf("[Webhook] wecom AgentID 校验失败(精确路由) account=%d payload_agent=%v expected=%d，回退全量遍历",
					id, out["AgentID"], acc.AgentID)
			} else {
				logger.Warnf("[Webhook] wecom 解密失败(精确路由) account=%d，回退全量遍历", id)
			}
		}
	} else {
		logger.Warnf("[Webhook] wecom 解密缺少有效 account 标识 account=%q，回退全量遍历", accountID)
	}

	accs, err := s.wecomRepo.GetByMerchant(ctx)
	if err != nil || len(accs) == 0 {
		return nil
	}
	for _, a := range accs {
		if a.EncodingAESKey == "" {
			continue
		}
		if out := decryptWeComPayload(a.EncodingAESKey, enc); out != nil {
			if validateWeComAgentID(out, a.AgentID) {
				return out
			}
		}
	}
	logger.Warnf("[Webhook] wecom 全量遍历未能匹配到有效账号 account_id=%s", accountID)
	return nil
}

func decryptWeComPayload(aesKey, enc string) map[string]any {
	if aesKey == "" {
		return nil
	}
	plain, err := DecryptWeComMessage(aesKey, enc)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil
	}
	return out
}

func validateWeComAgentID(payload map[string]any, expected int) bool {
	if payload == nil || expected == 0 {
		return true
	}
	raw, ok := payload["AgentID"]
	if !ok {
		return true
	}
	var got int
	switch v := raw.(type) {
	case float64:
		got = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			got = n
		} else {
			return true
		}
	default:
		return true
	}
	return got == expected
}

func (s *WebhookService) getWechatSecrets(ctx context.Context, accountID string) (string, string) {
	if s.wechatIntegration == nil {
		if s.db == nil {
			return "", ""
		}
		s.wechatIntegration = NewWechatService(s.db)
	}
	var acc *model.WechatAccount
	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		if a, gerr := s.wechatIntegration.GetAccount(ctx, uint(id)); gerr == nil {
			acc = a
		}
	}
	if acc == nil || acc.Token == "" {
		if a, gerr := s.wechatIntegration.GetFirstActiveAccount(ctx); gerr == nil {
			acc = a
		}
	}
	if acc == nil {
		return "", ""
	}
	return acc.Token, acc.EncodingAESKey
}

// GetWeComSecrets 公开方法：供 controller 层 URL 验证使用
func (s *WebhookService) GetWeComSecrets(ctx context.Context, accountID string) (string, string, error) {
	return s.getWeComSecrets(ctx, accountID)
}

func (s *WebhookService) getWeComSecrets(ctx context.Context, accountID string) (string, string, error) {
	if s.wecomRepo == nil {
		return "", "", errors.New("wecomRepo nil")
	}
	if s.db == nil {
		return "", "", nil
	}

	if id, err := strconv.ParseUint(accountID, 10, 64); err == nil && id > 0 {
		acc, err := s.wecomRepo.GetByID(ctx, uint(id))
		if err == nil && acc != nil {
			return acc.CallbackToken, acc.EncodingAESKey, nil
		}
	}

	accs, err := s.wecomRepo.GetByMerchant(ctx)
	if err != nil {
		return "", "", err
	}
	for _, a := range accs {
		if a.WebhookEnabled {
			return a.CallbackToken, a.EncodingAESKey, nil
		}
	}
	if len(accs) > 0 {
		return accs[0].CallbackToken, accs[0].EncodingAESKey, nil
	}
	return "", "", errors.New("wecom account not found")
}
