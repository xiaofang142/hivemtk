package service

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// WechatService 微信公众号服务
//
// 提供：
//   - 服务器配置验证（signature check）
//   - 消息接收（XML 解析）
//   - 客服消息发送（AccessToken 自动管理）
//   - 账号管理
type WechatService struct {
	repo         *repository.WechatAccountRepository
	mu           sync.RWMutex
	tokenClients map[uint]*wechatTokenClient
}

type wechatTokenClient struct {
	appID       string
	appSecret   string
	accessToken string
	expiresAt   time.Time
	mu          sync.Mutex
}

// NewWechatService 创建微信公众号服务
func NewWechatService(db *gorm.DB) *WechatService {
	return &WechatService{
		repo:         repository.NewWechatAccountRepository(db),
		tokenClients: make(map[uint]*wechatTokenClient),
	}
}

// ListAccounts 列出所有公众号账号
func (s *WechatService) ListAccounts(ctx context.Context) ([]model.WechatAccount, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.List(ctx)
}

// GetAccount 获取单个公众号账号
func (s *WechatService) GetAccount(ctx context.Context, id uint) (*model.WechatAccount, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.GetByID(ctx, id)
}

// GetFirstActiveAccount 查找第一个 active 公众号账号（用于智能选渠道兜底）
func (s *WechatService) GetFirstActiveAccount(ctx context.Context) (*model.WechatAccount, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.GetFirstActive(ctx)
}

// CreateAccount 创建公众号账号
func (s *WechatService) CreateAccount(ctx context.Context, acc *model.WechatAccount) error {
	if s.repo == nil {
		return fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.Create(ctx, acc)
}

// UpdateAccount 更新公众号账号
func (s *WechatService) UpdateAccount(ctx context.Context, acc *model.WechatAccount) error {
	if s.repo == nil {
		return fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.UpdateCredentials(ctx, acc)
}

// DeleteAccount 删除公众号账号
func (s *WechatService) DeleteAccount(ctx context.Context, id uint) error {
	if s.repo == nil {
		return fmt.Errorf("wechat: db not initialized")
	}
	return s.repo.Delete(ctx, id)
}

// VerifySignature 验证微信服务器签名
// 微信服务器在配置 URL 时用 GET 请求带 signature/timestamp/nonce/echostr 参数
func (s *WechatService) VerifySignature(token, signature, timestamp, nonce string) bool {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	hash := sha1.Sum([]byte(strings.Join(parts, "")))
	expected := fmt.Sprintf("%x", hash)
	return expected == signature
}

// WechatIncomingMessage 微信推送的 XML 消息结构
type WechatIncomingMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content,omitempty"`
	MsgID        string   `xml:"MsgId,omitempty"`
	// 图片/语音/视频
	MediaID string `xml:"MediaId,omitempty"`
	PicURL  string `xml:"PicUrl,omitempty"`
	Format  string `xml:"Format,omitempty"`
	// 位置
	LocationX float64 `xml:"Location_X,omitempty"`
	LocationY float64 `xml:"Location_Y,omitempty"`
	Scale     int     `xml:"Scale,omitempty"`
	Label     string  `xml:"Label,omitempty"`
	// 链接
	Title       string `xml:"Title,omitempty"`
	Description string `xml:"Description,omitempty"`
	URL         string `xml:"Url,omitempty"`
	// 事件
	Event    string `xml:"Event,omitempty"`
	EventKey string `xml:"EventKey,omitempty"`
}

// ParseIncomingMessage 解析微信推送的 XML 消息
func (s *WechatService) ParseIncomingMessage(body []byte) (*WechatIncomingMessage, error) {
	var msg WechatIncomingMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("parse wechat xml: %w", err)
	}
	return &msg, nil
}

func (s *WechatService) getTokenClient(ctx context.Context, accountID uint) (*wechatTokenClient, error) {
	s.mu.RLock()
	client, ok := s.tokenClients[accountID]
	s.mu.RUnlock()
	if ok {
		return client, nil
	}

	acc, err := s.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	if acc.AppID == "" || acc.AppSecret == "" {
		return nil, fmt.Errorf("wechat account %d: app_id or app_secret is empty", accountID)
	}

	client = &wechatTokenClient{
		appID:     acc.AppID,
		appSecret: acc.AppSecret,
	}
	s.mu.Lock()
	s.tokenClients[accountID] = client
	s.mu.Unlock()
	return client, nil
}

func (c *wechatTokenClient) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Before(c.expiresAt) {
		return c.accessToken, nil
	}

	token, expiresIn, err := c.fetchAccessToken(ctx)
	if err != nil {
		return "", err
	}

	c.accessToken = token
	c.expiresAt = time.Now().Add(time.Duration(expiresIn-300) * time.Second)
	return c.accessToken, nil
}

var wechatAPIBase = "https://api.weixin.qq.com"

func (c *wechatTokenClient) fetchAccessToken(ctx context.Context) (string, int, error) {
	url := fmt.Sprintf("%s/cgi-bin/token?grant_type=client_credential&appid=%s&secret=%s",
		wechatAPIBase, c.appID, c.appSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetch wechat token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Errcode     int    `json:"errcode"`
		Errmsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("parse wechat token response: %w", err)
	}
	if result.Errcode != 0 {
		return "", 0, fmt.Errorf("wechat api error: %d %s", result.Errcode, result.Errmsg)
	}

	return result.AccessToken, result.ExpiresIn, nil
}

// SendCustomMessage 发送客服消息
// 支持 msgType: text / image / news / template
//
// accountID=0 表示"自动选择第一个 active 公众号账号"，其他渠道（Telegram/Feishu）也支持类似语义
func (s *WechatService) SendCustomMessage(ctx context.Context, accountID uint, openID, msgType, content string) (string, error) {
	if accountID == 0 {
		acc, err := s.GetFirstActiveAccount(ctx)
		if err != nil {
			return "", fmt.Errorf("wechat: no active account available: %w", err)
		}
		accountID = acc.ID
	}
	client, err := s.getTokenClient(ctx, accountID)
	if err != nil {
		return "", err
	}

	token, err := client.getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("wechat token: %w", err)
	}

	payload := map[string]any{
		"touser":  openID,
		"msgtype": msgType,
	}
	switch msgType {
	case "text":
		payload["text"] = map[string]string{"content": content}
	case "image":

		payload["image"] = map[string]string{"media_id": content}
	default:
		payload["text"] = map[string]string{"content": content}
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/cgi-bin/message/custom/send?access_token=%s", wechatAPIBase, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("wechat send msg: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse wechat send response: %w", err)
	}
	if result.Errcode != 0 {
		return "", fmt.Errorf("wechat send error: %d %s", result.Errcode, result.Errmsg)
	}

	msg := &model.WechatMessage{
		AccountID:  accountID,
		FromUser:   "SYSTEM",
		ToUser:     openID,
		MsgType:    msgType,
		Content:    content,
		MsgID:      fmt.Sprintf("out_%d", time.Now().UnixNano()),
		IsOutgoing: true,
	}
	if err := s.repo.CreateMessage(ctx, msg); err != nil {
		logger.Warnf("[Wechat] 记录消息失败: %v", err)
	}

	return msg.MsgID, nil
}

// SaveIncomingMessage 保存收到的微信消息
func (s *WechatService) SaveIncomingMessage(ctx context.Context, accountID uint, msg *WechatIncomingMessage, rawXML []byte) error {
	record := &model.WechatMessage{
		AccountID:  accountID,
		FromUser:   msg.FromUserName,
		ToUser:     msg.ToUserName,
		MsgType:    msg.MsgType,
		Content:    msg.Content,
		MsgID:      msg.MsgID,
		RawXML:     string(rawXML),
		IsOutgoing: false,
	}
	return s.repo.CreateMessage(ctx, record)
}

// BuildTextReply 构建文本回复 XML
func (s *WechatService) BuildTextReply(toUser, fromUser, content string) string {
	tmpl := `<xml>
<ToUserName><![CDATA[%s]]></ToUserName>
<FromUserName><![CDATA[%s]]></FromUserName>
<CreateTime>%d</CreateTime>
<MsgType><![CDATA[text]]></MsgType>
<Content><![CDATA[%s]]></Content>
</xml>`
	return fmt.Sprintf(tmpl, toUser, fromUser, time.Now().Unix(), content)
}

// BuildEmptyReply 构建空回复（微信不重试）
func (s *WechatService) BuildEmptyReply() string {
	return "success"
}
