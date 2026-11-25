// Package wecomcs 封装企业微信客服（微信客服）协议：取 token、增量拉消息、发消息、回调验签。
//
// 纯协议层：只依赖 channelbot/core 与标准库，不 import service/repository。
// 与 wecom（自建应用 XML 回调）是两套协议：客服消息按 cursor 增量拉取，
// 拉取游标由调用方持久化（见 service/webhook_channel_wecomcs.go）。
package wecomcs

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/pkg/protocolhash"
)

const (
	defaultAPIBase = "https://qyapi.weixin.qq.com"
	// tokenExpiryMargin token 提前 5 分钟过期，避免边界上刚好失效。
	tokenExpiryMargin = 300 * time.Second
)

// Error 企业微信业务错误（HTTP 200 但 errcode != 0）。
type Error struct {
	RetCode int
	ErrMsg  string
	Op      string
}

func (e *Error) Error() string {
	return fmt.Sprintf("wecomcs %s: retcode=%d errmsg=%s", e.Op, e.RetCode, e.ErrMsg)
}

// IsWindowExpired 被动回复窗口过期类错误码（48 小时窗口）。
func (e *Error) IsWindowExpired() bool {
	switch e.RetCode {
	case 95021, 95026, 95019:
		return true
	}
	return false
}

// Client 微信客服 API 客户端。
type Client struct {
	core.BaseClient

	mu     sync.Mutex
	tokens map[string]cachedToken
}

type cachedToken struct {
	token  string
	expire time.Time
}

func NewClient(opts ...core.ClientOption) *Client {
	options := append([]core.ClientOption{core.WithBaseURL(defaultAPIBase)}, opts...)
	return &Client{BaseClient: core.NewBaseClient(options...), tokens: map[string]cachedToken{}}
}

type tokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetToken 取 access_token（按 corpID+secret 缓存，提前 5 分钟刷新）。
func (c *Client) GetToken(ctx context.Context, corpID, secret string) (string, error) {
	key := corpID + "\x00" + secret
	c.mu.Lock()
	if t, ok := c.tokens[key]; ok && time.Now().Before(t.expire) {
		c.mu.Unlock()
		return t.token, nil
	}
	c.mu.Unlock()

	q := url.Values{}
	q.Set("corpid", corpID)
	q.Set("corpsecret", secret)
	body, status, err := c.DoJSON(ctx, "GET", c.BaseURL+"/cgi-bin/gettoken?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	var r tokenResp
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("wecomcs gettoken decode: %w (status=%d)", err, status)
	}
	if r.ErrCode != 0 {
		return "", &Error{RetCode: r.ErrCode, ErrMsg: r.ErrMsg, Op: "gettoken"}
	}
	if r.AccessToken == "" {
		return "", fmt.Errorf("wecomcs gettoken: empty access_token (status=%d)", status)
	}
	ttl := time.Duration(r.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	if ttl > tokenExpiryMargin {
		ttl -= tokenExpiryMargin
	}
	c.mu.Lock()
	c.tokens[key] = cachedToken{token: r.AccessToken, expire: time.Now().Add(ttl)}
	c.mu.Unlock()
	return r.AccessToken, nil
}

// TextBody 文本消息体。
type TextBody struct {
	Content string `json:"content"`
}

// SyncMsgReq 增量拉取请求。Cursor 空串表示从最早开始。
type SyncMsgReq struct {
	Cursor   string `json:"cursor,omitempty"`
	Token    string `json:"token,omitempty"`
	OpenKfID string `json:"open_kfid,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// SyncMsg 客服会话消息（只取本项目消费的字段）。
type SyncMsg struct {
	MsgID          string   `json:"msgid"`
	OpenKfID       string   `json:"open_kfid"`
	ExternalUserID string   `json:"external_userid"`
	SendTime       int64    `json:"send_time"`
	Origin         int      `json:"origin"` // 3=客户 4=系统 5=客服
	MsgType        string   `json:"msgtype"`
	Text           TextBody `json:"text"`
	Image          struct {
		MediaID string `json:"media_id"`
	} `json:"image"`
}

// SyncMsgResp 增量拉取响应。
type SyncMsgResp struct {
	ErrCode    int       `json:"errcode"`
	ErrMsg     string    `json:"errmsg"`
	NextCursor string    `json:"next_cursor"`
	HasMore    int       `json:"has_more"`
	MsgList    []SyncMsg `json:"msg_list"`
}

// SyncMsg 拉取增量消息。
func (c *Client) SyncMsg(ctx context.Context, accessToken string, req SyncMsgReq) (*SyncMsgResp, error) {
	if req.Limit <= 0 || req.Limit > 1000 {
		req.Limit = 1000
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw, status, err := c.DoJSONBytes(ctx, "POST",
		c.BaseURL+"/cgi-bin/kf/sync_msg?access_token="+url.QueryEscape(accessToken), body, nil)
	if err != nil {
		return nil, err
	}
	var r SyncMsgResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("wecomcs sync_msg decode: %w (status=%d)", err, status)
	}
	if r.ErrCode != 0 {
		return nil, &Error{RetCode: r.ErrCode, ErrMsg: r.ErrMsg, Op: "sync_msg"}
	}
	return &r, nil
}

// SendMsgReq 发送客服消息请求。
type SendMsgReq struct {
	Touser   string    `json:"touser"`
	OpenKfID string    `json:"open_kfid"`
	MsgType  string    `json:"msgtype"`
	Text     *TextBody `json:"text,omitempty"`
	Image    *struct {
		MediaID string `json:"media_id"`
	} `json:"image,omitempty"`
	Link *struct {
		Title  string `json:"title"`
		Desc   string `json:"desc"`
		URL    string `json:"url"`
		PicURL string `json:"picurl"`
	} `json:"link,omitempty"`
	MiniProgram *struct {
		AppID    string `json:"appid"`
		Title    string `json:"title"`
		PagePath string `json:"pagepath"`
	} `json:"miniprogram,omitempty"`
}

// SendMsg 发送客服消息，返回平台 msgid。
func (c *Client) SendMsg(ctx context.Context, accessToken string, req *SendMsgReq) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	raw, status, err := c.DoJSONBytes(ctx, "POST",
		c.BaseURL+"/cgi-bin/kf/send_msg?access_token="+url.QueryEscape(accessToken), body, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MsgID   string `json:"msgid"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("wecomcs send_msg decode: %w (status=%d)", err, status)
	}
	if r.ErrCode != 0 {
		return "", &Error{RetCode: r.ErrCode, ErrMsg: r.ErrMsg, Op: "send_msg"}
	}
	return r.MsgID, nil
}

// VerifySignature 校验回调签名：sha1(sort(token,timestamp,nonce,echostr) 拼接)。
// 与企微官方回调签名算法一致（字典序排序后直接拼接，无分隔符）。
// SHA-1 由企微客服回调协议强制规定，非本项目自选；仅在受控封装内使用。
func VerifySignature(token, signature, timestamp, nonce, echostr string) bool {
	if token == "" || signature == "" {
		return false
	}
	parts := []string{token, timestamp, nonce, echostr}
	sort.Strings(parts)
	sum := protocolhash.LegacyProtocolSHA1([]byte(strings.Join(parts, "")))
	return core.SecureEqual(strings.ToLower(signature), hex.EncodeToString(sum[:]))
}

// ParseTimestamp 回调时间戳（秒），解析失败返回 0。
func ParseTimestamp(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}
