// Package qq 封装 QQ 机器人开放平台（q.qq.com）的主动发消息与被动收消息（Webhook Ed25519 验签/事件解析）。
//
// 官方文档：https://bot.q.qq.com/wiki/develop/api-v2/
// 纯协议层，零外部依赖（仅依赖 channelbot/core 与标准库），与 telegram/whatsapp 子包同构。
//
// 鉴权链路：
//   - AppID + AppSecret → POST /app/getAppAccessToken 换 access_token（7200s，服务端缓存临期刷新）
//   - API 请求头 Authorization: QQBot {access_token}
//   - 事件推送域名 api.bot.qq.com（2026-08-10 起统一，沙箱 sandbox.api.sgroup.qq.com）
//
// 回调验签（Ed25519，官方《签名生成》）：
//   - 由 BotSecret 派生 seed：repeat 补齐后截取 32 字节，ed25519.GenerateKey 生成密钥对
//   - 请求头 X-Signature-Ed25519（hex，128 字符）+ X-Signature-Timestamp
//   - 验签消息 = timestamp + body
//   - Op 13 回调地址验证：对 event_ts + plain_token 签名，hex 返回 {"plain_token","signature"}
package qq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/channelbot/core"
)

const (
	defaultAPIBase   = "https://api.bot.qq.com"
	defaultSandbox   = "https://sandbox.api.sgroup.qq.com"
	QQMessageMaxLen  = 2000 // 官方单条消息内容长度上限（保守值，超长分段）
	qqTokenRefreshLe = 300 * time.Second
)

// Client QQ 机器人 API 客户端
type Client struct {
	core.BaseClient
	appID     string
	appSecret string
	apiBase   string

	mu          sync.Mutex
	accessToken string
	tokenExpAt  time.Time
}

// NewClient 创建 QQ 客户端（appID/appSecret 用于 access_token 管理）
func NewClient(appID, appSecret string, opts ...core.ClientOption) *Client {
	c := &Client{appID: appID, appSecret: appSecret, apiBase: defaultAPIBase}
	c.BaseClient = core.NewBaseClient(opts...)
	// WithBaseURL 注入的测试/代理地址优先于默认 apiBase
	if c.BaseURL != "" {
		c.apiBase = c.BaseURL
	}
	return c
}

// WithSandbox 切换沙箱环境（须在首个 API 调用前设置）
func WithSandbox(c *Client) { c.apiBase = defaultSandbox }

// tokenResp getAppAccessToken 响应
// 注意：官方文档 expires_in 标注 number 但示例返回字符串 "7200"（自相矛盾），
// 解析须兼容两种类型（json.RawMessage 手动判定）。
type tokenResp struct {
	AccessToken string          `json:"access_token"`
	ExpiresIn   json.RawMessage `json:"expires_in"`
	Code        int             `json:"code"`
	Message     string          `json:"message"`
}

// expiresInSeconds 兼容 expires_in 为字符串或数字两种返回
func (t *tokenResp) expiresInSeconds() int {
	if len(t.ExpiresIn) == 0 {
		return 0
	}
	raw := strings.TrimSpace(string(t.ExpiresIn))
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	var f float64
	if err := json.Unmarshal(t.ExpiresIn, &f); err == nil && f > 0 {
		return int(f)
	}
	return 0
}

// GetAccessToken 获取 access_token（缓存期内直接复用，临期 300s 内刷新；
// 官方机制：有效期内重复获取返回相同值，距过期 60s 内获取返回新 token）
func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpAt.Add(-qqTokenRefreshLe)) {
		return c.accessToken, nil
	}
	payload, err := json.Marshal(map[string]string{"appId": c.appID, "clientSecret": c.appSecret})
	if err != nil {
		return "", fmt.Errorf("qq token marshal: %w", err)
	}
	url := c.apiBase + "/app/getAppAccessToken"
	body, status, err := c.DoJSON(ctx, "POST", url, bytes.NewReader(payload), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return "", fmt.Errorf("qq token request: %w", err)
	}
	var tr tokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("qq token parse (status %d): %w", status, err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("qq token empty (status %d code=%d msg=%s)", status, tr.Code, tr.Message)
	}
	expire := 7200 * time.Second
	if n := tr.expiresInSeconds(); n > 0 {
		expire = time.Duration(n) * time.Second
	}
	c.accessToken = tr.AccessToken
	c.tokenExpAt = time.Now().Add(expire)
	return c.accessToken, nil
}

// InvalidateToken 清空缓存的 token（401 时强制刷新）
func (c *Client) InvalidateToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = ""
	c.tokenExpAt = time.Time{}
}

func (c *Client) authHeaders(ctx context.Context) (map[string]string, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"Authorization": "QQBot " + token,
		"Content-Type":  "application/json",
	}, nil
}

// SendTarget 发送目标：群或单聊
type SendTarget struct {
	GroupOpenID string // 群场景
	UserOpenID  string // 单聊场景
	MsgID       string // 被动回复关联的原消息 ID（空=主动消息）
	MsgSeq      int    // 同一 msg_id 的回复序号（从 1 起；主动消息随机即可）
}

func (t SendTarget) apiPath() string {
	if t.GroupOpenID != "" {
		return "/v2/groups/" + t.GroupOpenID + "/messages"
	}
	return "/v2/users/" + t.UserOpenID + "/messages"
}

// SendMessage 发送文本消息（自动分段；返回首段 msg_id）
//
// msg_type=0 文本。被动回复带 msg_id + msg_seq（同一 msg_id 限 5 分钟 5 条），
// 主动消息不带 msg_id（走平台主动消息频控且需用户允许）。
func (c *Client) SendMessage(ctx context.Context, target SendTarget, text string) (string, error) {
	if target.GroupOpenID == "" && target.UserOpenID == "" {
		return "", errors.New("qq send: empty target")
	}
	chunks := splitQQMessage(text, QQMessageMaxLen)
	if len(chunks) == 0 {
		return "", errors.New("qq send: empty text")
	}
	var firstID string
	for i, chunk := range chunks {
		seq := target.MsgSeq
		if target.MsgID == "" {
			seq = 1 + int(time.Now().UnixNano()%1000)
		} else {
			seq += i
		}
		msgID, err := c.sendSingle(ctx, target, chunk, seq)
		if err != nil {
			return firstID, fmt.Errorf("qq send chunk %d/%d failed: %w", i+1, len(chunks), err)
		}
		if i == 0 {
			firstID = msgID
		}
	}
	return firstID, nil
}

type sendResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		MsgID string `json:"msg_id"`
	} `json:"data"`
}

func (c *Client) sendSingle(ctx context.Context, target SendTarget, text string, seq int) (string, error) {
	payload := map[string]any{
		"content":  text,
		"msg_type": 0,
		"msg_seq":  seq,
	}
	if target.MsgID != "" {
		payload["msg_id"] = target.MsgID
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("qq send marshal: %w", err)
	}
	headers, err := c.authHeaders(ctx)
	if err != nil {
		return "", err
	}
	body, status, err := c.DoJSON(ctx, "POST", c.apiBase+target.apiPath(), bytes.NewReader(b), headers)
	if err != nil {
		return "", fmt.Errorf("qq send: %w", err)
	}
	// 401：token 失效，刷新后重试一次（官方 access_token 临期机制；官方无显式 401 流程文档，
	// 刷新重试为工程实践——避免分段消息中途 token 过期导致整批失败）
	if status == 401 {
		c.InvalidateToken()
		headers, herr := c.authHeaders(ctx)
		if herr != nil {
			return "", herr
		}
		body, status, err = c.DoJSON(ctx, "POST", c.apiBase+target.apiPath(), bytes.NewReader(b), headers)
		if err != nil {
			return "", fmt.Errorf("qq send retry after 401: %w", err)
		}
	}
	var sr sendResp
	if uerr := json.Unmarshal(body, &sr); uerr != nil {
		// 非 JSON 响应不能当作成功（2xx + 空 body 场景）
		if status >= 200 && status < 300 {
			return "", fmt.Errorf("qq send non-json response (status %d): %s", status, truncateForLog(body))
		}
		return "", fmt.Errorf("qq send status %d body=%s", status, truncateForLog(body))
	}
	if status < 200 || status >= 300 || sr.Code != 0 {
		return "", fmt.Errorf("qq send status %d code=%d msg=%s body=%s", status, sr.Code, sr.Message, truncateForLog(body))
	}
	return sr.Data.MsgID, nil
}

func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 256 {
		s = s[:256]
	}
	return s
}

func splitQQMessage(text string, limit int) []string {
	if limit <= 0 {
		limit = QQMessageMaxLen
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	var out []string
	for len(runes) > limit {
		split := -1
		for i := limit - 1; i > 0; i-- {
			if runes[i] == '\n' {
				split = i + 1
				break
			}
		}
		if split == -1 {
			for i := limit - 1; i > 0; i-- {
				if runes[i] == '。' || runes[i] == '！' || runes[i] == '？' || runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
					split = i + 1
					break
				}
			}
		}
		if split == -1 {
			for i := limit - 1; i > 0; i-- {
				if runes[i] == ' ' {
					split = i + 1
					break
				}
			}
		}
		if split <= 0 {
			split = limit
		}
		head := strings.TrimRight(string(runes[:split]), " \n")
		if head == "" {
			head = string(runes[:split])
		}
		out = append(out, head)
		runes = runes[split:]
	}
	if len(runes) > 0 {
		out = append(out, strings.TrimRight(string(runes), " \n"))
	}
	return out
}

// ---------------------------------------------------------------------------
// Webhook 验签（Ed25519）
// ---------------------------------------------------------------------------

// DerivePrivateKey 官方验签密钥派生：对 secret repeat 至 >=32 字节后截取，作为 ed25519 seed。
func DerivePrivateKey(secret string) ed25519.PrivateKey {
	if secret == "" {
		return nil
	}
	seed := make([]byte, 0, 32)
	for len(seed) < 32 {
		seed = append(seed, []byte(secret)...)
	}
	return ed25519.NewKeyFromSeed(seed[:32])
}

// VerifySignature Ed25519 验签
//
// sigHex: X-Signature-Ed25519 头（hex）；timestamp: X-Signature-Timestamp 头；body: 原始请求体。
// 官方约束：签名 hex 解码后 64 字节且 sig[63]&224==0（Ed25519 高位约束），消息 = timestamp + body。
func VerifySignature(secret, sigHex, timestamp string, body []byte) bool {
	if secret == "" || sigHex == "" || timestamp == "" {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize || sig[ed25519.SignatureSize-1]&224 != 0 {
		return false
	}
	pub, ok := DerivePrivateKey(secret).Public().(ed25519.PublicKey)
	if !ok {
		return false
	}
	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(pub, msg, sig)
}

// GenerateCallbackTestSignature 生成回调地址验证（Op 13）应答签名。
// 官方流程：对 event_ts + plain_token 用派生私钥签名，hex 编码。
func GenerateCallbackTestSignature(secret, eventTS, plainToken string) (signature string, err error) {
	priv := DerivePrivateKey(secret)
	if priv == nil {
		return "", errors.New("qq callback verify: empty secret")
	}
	sig := ed25519.Sign(priv, []byte(eventTS+plainToken))
	return hex.EncodeToString(sig), nil
}

// op13 限制：plain_token 长度约束（官方为随机串，64 字符足够宽松），
// event_ts 距今 ±5 分钟内有效——防止端点被滥用为任意消息的签名预言机。
const (
	op13PlainTokenMaxLen = 64
	op13EventTSWindow    = 5 * time.Minute
)

// IsValidChallenge 校验 Op13 挑战参数合法性（plain_token 格式 + event_ts 新鲜度）
func IsValidChallenge(plainToken, eventTS string) bool {
	if plainToken == "" || len(plainToken) > op13PlainTokenMaxLen ||
		strings.ContainsAny(plainToken, "{}[]\"\\ \n\r\t") {
		return false
	}
	ts, err := strconv.ParseInt(eventTS, 10, 64)
	if err != nil || ts <= 0 {
		return false
	}
	diff := time.Since(time.Unix(ts, 0))
	if diff < 0 {
		diff = -diff
	}
	return diff <= op13EventTSWindow
}

// ---------------------------------------------------------------------------
// 事件解析
// ---------------------------------------------------------------------------

// 事件类型常量（官方事件名）
const (
	EventGroupAtMessage = "GROUP_AT_MESSAGE_CREATE"
	EventC2CAtMessage   = "C2C_AT_MESSAGE_CREATE"
	EventDirectMessage  = "DIRECT_MESSAGE_CREATE"
)

// CallbackOp 回调操作码（webhook 通道关注 op=13 地址验证）
const CallbackOpVerify = 13

// Event webhook 推送事件统一结构（op 0=事件分发 / 13=回调地址验证）
type Event struct {
	ID         string          `json:"id"`
	Op         int             `json:"op"`
	T          string          `json:"t"`
	S          int64           `json:"s"`
	PlainToken string          `json:"plain_token"`
	EventTS    string          `json:"event_ts"`
	D          json.RawMessage `json:"d"`
}

// GroupMessageData 群 @机器人 消息事件体（GROUP_AT_MESSAGE_CREATE）
type GroupMessageData struct {
	ID          string `json:"id"`           // 事件 ID（幂等键）
	GroupOpenID string `json:"group_openid"` // 群 openid
	Content     string `json:"content"`      // 消息内容（官方已去除 @机器人 前缀）
	Author      struct {
		MemberOpenID string `json:"member_openid"` // 发送者 openid
	} `json:"author"`
	Timestamp string `json:"timestamp"`
}

// C2CMessageData 单聊消息事件体（C2C_AT_MESSAGE_CREATE）
type C2CMessageData struct {
	ID         string `json:"id"`
	UserOpenID string `json:"user_openid"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

// ParseEvent 解析 webhook 原始 body
func ParseEvent(body []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("qq parse event: %w", err)
	}
	return &e, nil
}

// IsCallbackVerify 是否为 Op13 回调地址验证请求
func (e *Event) IsCallbackVerify() bool { return e.Op == CallbackOpVerify }

// ToInbound 归一化为 core.InboundMessage（accountID 由调用方填充）。
// 仅处理群 @ 与单聊消息事件；其他事件（如 DIRECT_MESSAGE_CREATE 频道私信）返回 nil 忽略。
func (e *Event) ToInbound(accountID string) *core.InboundMessage {
	switch e.T {
	case EventGroupAtMessage:
		var d GroupMessageData
		if err := json.Unmarshal(e.D, &d); err != nil {
			return nil
		}
		return &core.InboundMessage{
			Platform:       "qq",
			AccountID:      accountID,
			MessageID:      "qq_" + d.ID,
			ConversationID: d.GroupOpenID,
			SenderID:       d.Author.MemberOpenID,
			Content:        d.Content,
			MsgType:        "text",
			IsGroup:        true,
			GroupID:        d.GroupOpenID,
			Timestamp:      parseQQTimestamp(d.Timestamp),
		}
	case EventC2CAtMessage:
		var d C2CMessageData
		if err := json.Unmarshal(e.D, &d); err != nil {
			return nil
		}
		return &core.InboundMessage{
			Platform:       "qq",
			AccountID:      accountID,
			MessageID:      "qq_" + d.ID,
			ConversationID: d.UserOpenID,
			SenderID:       d.UserOpenID,
			Content:        d.Content,
			MsgType:        "text",
			Timestamp:      parseQQTimestamp(d.Timestamp),
		}
	default:
		return nil
	}
}

// Ingress 把解析后的 QQ 入站消息经消息中台统一处理。
// EventID 取 "qq_{事件ID}" 作幂等键（官方事件 id 重投不变）。
// 调用方负责在调用前完成 webhook 验签。
func (e *Event) Ingress(ctx context.Context, h core.IngressHandler, accountID string) error {
	if h == nil {
		return nil
	}
	inbound := e.ToInbound(accountID)
	if inbound == nil {
		return nil
	}
	event := inbound.ToMessageEvent(accountID)
	if e.ID != "" {
		event.EventID = "qq_evt_" + e.ID
	}
	return h.HandleIngressMessage(ctx, event)
}

func parseQQTimestamp(ts string) int64 {
	if ts == "" {
		return 0
	}
	// 官方 timestamp 为 RFC3339/ISO8601；解析失败返回 0（中台 NormalizeEvent 会补 now），
	// 另兼容常见日期时间格式（部分事件文档示例为空格分隔格式）
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Unix()
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", ts, time.Local); err == nil {
		return t.Unix()
	}
	return 0
}
