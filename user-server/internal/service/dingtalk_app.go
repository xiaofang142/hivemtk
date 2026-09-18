package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// DingTalkAppService 钉钉企业内部应用账号服务（CRUD + 回调验签 + 入站收消息）
type DingTalkAppService struct {
	repo       repository.DingTalkAppRepository
	webhookSvc *WebhookService
}

// NewDingTalkAppService 创建钉钉应用账号服务。
// webhookSvc 复用 WebhookService（其 ingressSvc 已在 NewWebhookService 中注入），
// 以便入站消息经 HandleIngressMessage 进入统一 AI 派发管线。
//
// 注：保留 db *gorm.DB 入参以维持向后兼容（router 装配不改动），
// 内部在构造函数中实例化 repository，service struct 不直接持有 *gorm.DB。
func NewDingTalkAppService(db *gorm.DB, webhookSvc *WebhookService) *DingTalkAppService {
	return &DingTalkAppService{repo: repository.NewDingTalkAppRepository(db), webhookSvc: webhookSvc}
}

// CreateAccount 创建账号
func (s *DingTalkAppService) CreateAccount(ctx context.Context, acc *model.DingTalkAppAccount) error {
	return s.repo.Create(ctx, acc)
}

// GetAccount 查询账号
func (s *DingTalkAppService) GetAccount(ctx context.Context, id uint) (*model.DingTalkAppAccount, error) {
	return s.repo.FindByID(ctx, id)
}

// UpdateAccount 更新账号
func (s *DingTalkAppService) UpdateAccount(ctx context.Context, acc *model.DingTalkAppAccount) error {
	return s.repo.Update(ctx, acc)
}

// ListAccounts 列出账号
func (s *DingTalkAppService) ListAccounts(ctx context.Context) ([]model.DingTalkAppAccount, error) {
	return s.repo.ListAll(ctx)
}

// DeleteAccount 删除账号
func (s *DingTalkAppService) DeleteAccount(ctx context.Context, id uint) error {
	return s.repo.DeleteByID(ctx, id)
}

// VerifyCallback 钉钉回调 URL 验证（GET）。
// 钉钉对配置的回调地址发起 GET，携带 signature/timestamp/nonce/echostr。
// 本地用 token 计算 signature 比对；一致则回显 echostr（配置了 AESKey 时先解密）。
func (s *DingTalkAppService) VerifyCallback(ctx context.Context, accountID uint, signature, timestamp, nonce, echostr string) (string, error) {
	acc, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return "", errors.New("account not found")
	}
	if acc.Token == "" {
		return "", errors.New("callback token not configured")
	}

	mac := hmac.New(sha256.New, []byte(acc.Token))
	mac.Write([]byte(timestamp + "\n" + nonce))
	expect := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expect), []byte(signature)) != 1 {
		return "", errors.New("signature verify failed")
	}
	if acc.AESKey == "" {
		return echostr, nil
	}
	plain, err := dingTalkDecrypt(acc.AESKey, echostr)
	if err != nil {
		return "", fmt.Errorf("decrypt echostr: %w", err)
	}
	return plain, nil
}

func (s *DingTalkAppService) ReceiveMessage(ctx context.Context, accountID uint, raw []byte) error {
	acc, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return errors.New("account not found")
	}
	if !acc.InboundEnabled {
		return errors.New("inbound not enabled")
	}
	if acc.AESKey == "" {
		return errors.New("dingtalk aes_key not configured; plaintext webhook rejected")
	}
	var payload []byte // 解密块内所有存活路径都会赋值
	{
		var env struct {
			Encrypt string `json:"encrypt"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("parse envelope: %w", err)
		}
		if env.Encrypt == "" {
			return errors.New("missing encrypt field")
		}
		plain, derr := dingTalkDecrypt(acc.AESKey, env.Encrypt)
		if derr != nil {
			return fmt.Errorf("decrypt message: %w", derr)
		}
		payload = []byte(plain)
	}
	var msg struct {
		MsgType        string `json:"msgtype"`
		SenderID       string `json:"senderId"`
		SenderStaffID  string `json:"senderStaffId"`
		ConversationID string `json:"conversationId"`
		MsgID          string `json:"msgId"`
		CreateAt       int64  `json:"createAt"`

		SessionWebhook            string `json:"sessionWebhook"`
		SessionWebhookExpiredTime int64  `json:"sessionWebhookExpiredTime"`
		Content                   struct {
			Content string `json:"content"`
		} `json:"content"`
		Text struct {
			Content string `json:"content"`
		} `json:"text"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return fmt.Errorf("parse message: %w", err)
	}
	content := msg.Content.Content
	if content == "" {
		content = msg.Text.Content
	}
	if content == "" {
		content = "[" + msg.MsgType + "]"
	}

	sender := msg.SenderStaffID
	if sender == "" {
		sender = msg.SenderID
	}
	if sender == "" {
		sender = msg.ConversationID
	}
	agent := acc.AIAgentID
	if agent == "" {
		agent = "sales_engine"
	}
	event := &model.MessageEvent{
		EventID:        fmt.Sprintf("dt-%d-%s", accountID, msg.MsgID),
		SessionID:      fmt.Sprintf("dt-%d-%s", accountID, msg.ConversationID),
		Channel:        model.ChannelDingTalk,
		SenderID:       sender,
		MsgType:        model.MsgTypeText,
		Content:        content,
		ConversationID: msg.ConversationID,
		Timestamp:      time.Now(),
		AIAgent:        agent,
		Extra: map[string]any{
			"account_id": fmt.Sprintf("%d", accountID),

			"session_webhook":            msg.SessionWebhook,
			"session_webhook_expired_at": msg.SessionWebhookExpiredTime,
		},
	}
	if s.webhookSvc == nil {
		return errors.New("webhook service not configured")
	}

	if err := s.webhookSvc.ingressHandler(ctx).HandleIngressMessage(ctx, event); err != nil {
		logger.Errorf("[dingtalk] 入站消息处理失败 accountID=%d eventID=%s: %v", accountID, event.EventID, err)
		return err
	}
	return nil
}

func dingTalkDecrypt(aesKey, cipherText string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(aesKey + "=")
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(aesKey)
		if err != nil {
			return "", fmt.Errorf("decode aes key: %w", err)
		}
	}
	if len(key) > 32 {
		key = key[:32]
	} else if len(key) < 32 {
		padded := make([]byte, 32)
		copy(padded, key)
		key = padded
	}
	ct, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(ct)%aes.BlockSize != 0 {
		return "", errors.New("ciphertext is not a multiple of block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := key[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)
	plain := make([]byte, len(ct))
	mode.CryptBlocks(plain, ct)
	n := len(plain)
	if n == 0 {
		return "", errors.New("empty plaintext")
	}
	pad := int(plain[n-1])
	if pad < 1 || pad > aes.BlockSize || pad > n {
		return "", errors.New("invalid padding")
	}
	for _, b := range plain[n-pad:] {
		if int(b) != pad {
			return "", errors.New("invalid padding")
		}
	}
	return string(plain[:n-pad]), nil
}
