// Package core 提供 channelbot 跨渠道复用能力：HTTP 客户端、安全比较、入站消息标准化。
//
// 拆分动机：telegram/whatsapp 等子包均为纯协议层（零外部依赖，独立可开源），
// 把它们共享的 HTTP/BaseClient/常量时间比较/入站消息结构提到本包，避免每个子包重复实现。
//
// 设计原则：
//   - 依赖标准库与 model.MessageEvent（消息中台统一事件协议）
//   - 不依赖业务侧 service/repository（与 channelbot 子包同侧，service 经适配器实现 IngressHandler）
//   - 暴露的方法与字段保持稳定（被 telegram.NewClient / whatsapp.NewCloudClient 使用）
package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"hivemtk-user/internal/model"
)

// DefaultHTTPTimeout 默认 HTTP 超时（外部依赖默认 30s）
const DefaultHTTPTimeout = 30 * time.Second

// httpTimeoutProvider 由 internal/app 层在启动时注入（读 config_params
// group=channelbot, key=http_timeout）。nil 即未注入，走兜底常量。
var httpTimeoutProvider func() time.Duration

// SetHTTPTimeoutProvider 注入外部依赖默认 HTTP 超时读取函数。
// fn 传 nil 视为「不注入」，保留兜底值。
func SetHTTPTimeoutProvider(fn func() time.Duration) {
	if fn != nil {
		httpTimeoutProvider = fn
	}
}

// HTTPTimeoutEffective 返回实际生效的外部依赖默认 HTTP 超时。
// 非正值一律回落兜底：超时 0 在 net/http 语义里是「不设超时」，
// 一次外部依赖挂起就能把整个请求 goroutine 挂死。
func HTTPTimeoutEffective() time.Duration {
	if httpTimeoutProvider != nil {
		if v := httpTimeoutProvider(); v > 0 {
			return v
		}
	}
	return DefaultHTTPTimeout
}

// ProbeHTTPTimeout 导出读口，供装配层测试断言注入是否真的生效。
func ProbeHTTPTimeout() time.Duration { return HTTPTimeoutEffective() }

// ClientOption BaseClient 配置项
type ClientOption func(*BaseClient)

// WithTimeout 覆盖默认超时
func WithTimeout(d time.Duration) ClientOption {
	return func(c *BaseClient) { c.Timeout = d }
}

// WithHTTPClient 注入自定义 http.Client（用于测试或复用连接池）
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *BaseClient) { c.HTTPClient = hc }
}

// WithBaseURL 覆盖默认 base URL（用于测试或代理）
func WithBaseURL(u string) ClientOption {
	return func(c *BaseClient) { c.BaseURL = u }
}

// WithProxyTransport 注入自定义代理 Transport
//
// 用于在需要代理的环境中（如中国访问 Telegram API）注入配置好的 Transport。
// 传入 nil 等价于直连。
func WithProxyTransport(t *http.Transport) ClientOption {
	return func(c *BaseClient) {
		if t != nil {
			c.HTTPClient.Transport = t
		}
	}
}

// BaseClient 跨渠道共享的 HTTP 客户端封装
//
// 复用项目既有 httpclient 模式（如 internal/pkg/utils/httpclient 不可见时独立维护）：
//   - Timeout 默认 30s，可由 WithTimeout 覆盖
//   - HTTPClient 复用同一实例以复用连接池（避免每次新建 transport）
//   - DoJSON 统一 JSON 编解码 + 状态码校验透传业务侧
//
// 子包通过嵌入 BaseClient 获得 DoJSON 能力：
//
//	type Client struct {
//	    core.BaseClient
//	    token string
//	}
type BaseClient struct {
	HTTPClient *http.Client
	Timeout    time.Duration
	BaseURL    string
}

// NewBaseClient 创建 BaseClient
func NewBaseClient(opts ...ClientOption) BaseClient {
	bc := BaseClient{
		HTTPClient: &http.Client{
			Timeout: HTTPTimeoutEffective(),
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				DisableCompression:    false,
				DisableKeepAlives:     false,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
		Timeout: HTTPTimeoutEffective(),
	}
	for _, opt := range opts {
		opt(&bc)
	}
	if bc.HTTPClient.Timeout == 0 {
		bc.HTTPClient.Timeout = bc.Timeout
	}
	return bc
}

// DoJSON 发送 JSON 请求并读取响应
//
// 返回：响应体（已读完整 body，可由调用方丢弃），HTTP 状态码，错误
//   - 网络/IO 错误返回 error
//   - 业务侧 HTTP 4xx/5xx 仍返回 body 和 status，由调用方决定如何处理（典型场景：WA Cloud 200/201 为成功，其他为业务失败）
//   - body io.ReadCloser 由本方法负责关闭
func (c *BaseClient) DoJSON(ctx context.Context, method, url string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	if c.HTTPClient == nil {
		return nil, 0, fmt.Errorf("core: HTTPClient not initialized")
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("core: new request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("core: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("core: read body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// DoJSONBytes 便捷方法：把 []body 包装为 bytes.NewReader
func (c *BaseClient) DoJSONBytes(ctx context.Context, method, url string, body []byte, headers map[string]string) ([]byte, int, error) {
	return c.DoJSON(ctx, method, url, bytes.NewReader(body), headers)
}

// SecureEqual 常量时间字符串比较（防时序攻击）
//
// 使用 crypto/subtle 实现。空字符串与任何非空字符串比较均返回 false。
// 适用于：webhook secret / signature / verify_token 等敏感字段比较。
func SecureEqual(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// InboundMessage 归一化入站消息（跨渠道统一结构）
//
// 由各渠道子包的 ToInbound() 方法产出，service 层无差别消费。
// 字段命名遵循：Platform / AccountID / MessageID / ConversationID / SenderID / SenderName /
// Content / MsgType / IsGroup / GroupID / GroupName / Timestamp。
type InboundMessage struct {
	Platform       string
	AccountID      string
	MessageID      string
	ConversationID string
	SenderID       string
	SenderName     string
	Content        string
	MsgType        string
	IsGroup        bool
	GroupID        string
	GroupName      string
	Timestamp      int64
}

// IngressHandler 渠道入站消息中台接口。
// 由 service.InboxIngressService 经适配器实现（避免 channelbot 反向依赖 service 层，
// 适配器把 (*InboxIngressResult, error) 收敛为 error，并对 message_hub 唯一冲突做幂等容忍）。
//
// 渠道适配器解析完原始 webhook 后，构造 model.MessageEvent 并调用本接口，
// 由中台统一完成：标准化 → 人工锁判定 → AI 串行锁 → 落库 message_hub → 触发 AgentRuntime。
type IngressHandler interface {
	HandleIngressMessage(ctx context.Context, event *model.MessageEvent) error
}

// ToMessageEvent 把归一化入站消息转换为消息中台的 MessageEvent。
// accountID 由调用方（webhook 层）填充；EventID/SessionID 缺失时由中台 NormalizeEvent 补齐。
// 渠道特有字段（如 TG update_id 幂等键、WA 非文本内容映射）由各渠道 Ingress 方法在调用前覆写。
func (m InboundMessage) ToMessageEvent(accountID string) *model.MessageEvent {
	event := &model.MessageEvent{
		EventID:        m.MessageID,
		Channel:        m.Platform,
		SenderID:       m.SenderID,
		SenderName:     m.SenderName,
		ReceiverID:     accountID,
		ConversationID: m.ConversationID,
		MsgType:        m.MsgType,
		Content:        m.Content,
		IsGroup:        m.IsGroup,
		GroupID:        m.GroupID,
		Extra: map[string]any{
			"account_id":     accountID,
			"channel_msg_id": m.MessageID,
			"group_name":     m.GroupName,
		},
	}
	if m.Timestamp != 0 {
		event.Timestamp = time.Unix(m.Timestamp, 0)
	}
	if m.ConversationID != "" {
		event.SessionID = m.Platform + ":" + m.ConversationID
	}
	return event
}

// TemplateComponent WhatsApp 模板消息组件
//
// 文档参考：https://developers.facebook.com/docs/whatsapp/cloud-api/reference/messages#template
// 业务侧只关心 parameters（header/body/button 子段），结构化封装以便序列化。
type TemplateComponent struct {
	Type       string              `json:"type"`
	Parameters []TemplateParameter `json:"parameters"`
	SubType    string              `json:"sub_type,omitempty"`
	Index      string              `json:"index,omitempty"`
}

// TemplateParameter 模板参数
type TemplateParameter struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Currency *TemplateMoney `json:"currency,omitempty"`
	DateTime *TemplateTime  `json:"date_time,omitempty"`
	Image    *TemplateMedia `json:"image,omitempty"`
}

// TemplateMoney 货币参数
type TemplateMoney struct {
	FallbackValue string `json:"fallback_value"`
	Code          string `json:"code"`
	Amount1000    int64  `json:"amount_1000"`
}

// TemplateTime 时间参数
type TemplateTime struct {
	FallbackValue string `json:"fallback_value"`
}

// TemplateMedia 媒体参数
type TemplateMedia struct {
	Link string `json:"link,omitempty"`
	ID   string `json:"id,omitempty"`
}
