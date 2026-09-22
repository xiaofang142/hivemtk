package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/channelbot/core"
	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ChannelErrorCategory 渠道错误类别
type ChannelErrorCategory string

const (
	CategoryRateLimited ChannelErrorCategory = "rate_limited"
	CategoryAuth        ChannelErrorCategory = "auth"
	CategoryNetwork     ChannelErrorCategory = "network"
	CategoryBadRequest  ChannelErrorCategory = "bad_request"
	// CategoryQuotaExhausted 配额到顶（公众号 45009 daily quota 一类）：
	// 官方口径是"次日/整点刷新"，分钟级退避不会再成功，重试只是把队列灌满无效请求。
	CategoryQuotaExhausted ChannelErrorCategory = "quota_exhausted"
	// CategoryWindowExpired 被动回复窗口/次数超限（QQ 40034128、304103）：
	// 窗口已经关了，同一份内容重投必然再失败。
	CategoryWindowExpired ChannelErrorCategory = "window_expired"
	CategoryUnknown       ChannelErrorCategory = "unknown"
)

// ChannelError 结构化渠道错误。实现 error 接口，可无损穿过现有 error 链路。
type ChannelError struct {
	// Channel 决定业务码怎么解释：45009 在企业微信是"1 分钟后自动解除"（可重试），
	// 在公众号是"reach max api daily quota limit"（不可重试），同号不同义。
	Channel    string
	Category   ChannelErrorCategory
	Retryable  bool
	StatusCode int
	// Code 渠道自带的业务码原文（errcode / code / error.code）。
	Code       string
	RetryAfter time.Duration
	Raw        string
}

func (e *ChannelError) Error() string { return "channel error [" + string(e.Category) + "]: " + e.Raw }

// AsChannelError 把任意 error 归一为 *ChannelError（已是则原样返回）。
func AsChannelError(err error) *ChannelError {
	if err == nil {
		return nil
	}
	var ce *ChannelError
	if errors.As(err, &ce) {
		return completeChannelError(ce)
	}
	// 客户端在解析响应的当场构造的结构化事实（状态码/业务码/等待值），
	// 跨 fmt.Errorf("%w") 链到这里仍然完整 —— 这是"判据按码不按文案"的落点。
	var api *core.APIError
	if errors.As(err, &api) {
		return completeChannelError(&ChannelError{
			Channel:    api.Channel,
			StatusCode: api.StatusCode,
			Code:       api.Code,
			RetryAfter: api.RetryAfter,
			Raw:        api.Error(),
		})
	}
	return classifyChannelError(err.Error())
}

// completeChannelError 只补构造点没判定的部分：类别缺失时按"业务码表 → 状态码/文案"的顺序判，
// 构造点带来的状态码、业务码、等待值一律保留（渠道没在串里重写一遍的机会）。
func completeChannelError(ce *ChannelError) *ChannelError {
	if ce.Category != "" && ce.Category != CategoryUnknown {
		return ce
	}
	base := classifyChannelError(ce.Raw)
	if ce.Channel == "" {
		ce.Channel = base.Channel
	}
	if ce.Code == "" {
		ce.Code = base.Code
	}
	if ce.StatusCode == 0 {
		ce.StatusCode = base.StatusCode
	}
	if ce.RetryAfter == 0 {
		ce.RetryAfter = base.RetryAfter
	}
	ce.Category, ce.Retryable = base.Category, base.Retryable
	if rule, ok := lookupCodeRule(ce.Channel, ce.Code); ok {
		ce.Category, ce.Retryable = rule.category, rule.retryable
		if ce.RetryAfter == 0 {
			ce.RetryAfter = rule.delay
		}
	}
	// 等待值也是事实，而 base 只读过 Raw：渠道把"还要等多久"放在响应头（飞书
	// x-ogw-ratelimit-reset）时，Raw 里天然没有这句话，只按文案判就会落 unknown。
	if ce.Category == CategoryUnknown && ce.RetryAfter > 0 {
		ce.Category, ce.Retryable = CategoryRateLimited, true
	}
	return ce
}

var (
	reHTTPStatus = regexp.MustCompile(`status[ =:]+(\d{3})\b`)
	// retry_after 在本仓有三种真实形态：`retry_after=30s`（渠道客户端拼的错误串）、
	// `"retry_after":31`（Telegram 响应体原样）、`retry_after=1m0s`（pacing 直接拼 Go Duration）。
	// 字符类少了 `=` 就前两种都取不到，token 少了单位段就第三种只剩 1 秒。
	reRetryAfter = regexp.MustCompile(`retry_after["': =]{1,3}([0-9][0-9.]*[0-9a-zA-Z.]*)`)
	// 渠道标识：错误串前缀是本仓自己拼的（"tg send …"、"wa send …"），别名一并认。
	reChannelWord = regexp.MustCompile(`(^|[^a-z0-9])(telegram|tg|wecom|wechat|weixin|feishu|qq|whatsapp|wa|dingtalk)([^a-z0-9]|$)`)
	channelAlias  = map[string]string{"tg": "telegram", "wa": "whatsapp", "weixin": "wechat"}
	// 各渠道错误串里业务码所在的位置同样由本仓的拼接格式决定，按渠道各取各的形状，
	// 不做"看见一串数字就当业务码"的通用匹配（会把 HTTP 码、消息 id 误当码值）。
	reChannelCode = map[string]*regexp.Regexp{
		"wecom":    regexp.MustCompile(`errcode=(-?\d+)`),
		"dingtalk": regexp.MustCompile(`errcode=(-?\d+)`),
		"wechat":   regexp.MustCompile(`(?:send error:|errcode=) ?(-?\d+)`),
		"feishu":   regexp.MustCompile(`(?:api code|errcode=) ?(-?\d+)`),
		"qq":       regexp.MustCompile(`code=(-?\d+)`),
		"whatsapp": regexp.MustCompile(`"code": ?(-?\d+)`),
	}
)

// parseRetryAfterToken 纯数字按秒（Telegram 官方单位是秒），其余交给 time.ParseDuration
// （本仓 pacing 拼的是 Go Duration）。解不出就返回 0，即"渠道没说等多久"。
func parseRetryAfterToken(tok string) time.Duration {
	if n, err := strconv.ParseInt(tok, 10, 64); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if d, err := time.ParseDuration(tok); err == nil && d > 0 {
		return d
	}
	return 0
}

// codeRule 一条渠道业务码的判读结果。判据来自各渠道官方返回码页原文，
// 取证分档见审计 §10（只有 A 档进这张表）。
type codeRule struct {
	category  ChannelErrorCategory
	retryable bool
	delay     time.Duration // 官方写明的解除时长；没写则 0，由上层退避表决定
}

// channelCodeRules 业务码 → 判读。按渠道分表，因为同号不同义：
// 45009 在企业微信是"接口调用超过限制…1分钟后自动解除"（可重试），
// 在公众号是 "reach max api daily quota limit"（当天不会再成功）。
// Telegram 不进表：官方明写 "An Integer 'error_code' field is also returned,
// but its contents are subject to change in the future"，只认 parameters.retry_after。
var channelCodeRules = map[string]map[string]codeRule{
	"wecom": {
		// 企微《全局错误码》原文：40001 不合法的secret参数、40014 不合法的access_token、
		// 42001 access_token已过期（说明列："access_token有时效性，需要重新获取一次"）
		// —— 凭证类失败重投不会自己变好，同一份 token 再撞一次墙。
		"40001": {CategoryAuth, false, 0},
		"40014": {CategoryAuth, false, 0},
		"42001": {CategoryAuth, false, 0},
		"45009": {CategoryRateLimited, true, time.Minute},
		"45033": {CategoryRateLimited, true, time.Minute},
	},
	"wechat": {
		"-1":    {CategoryNetwork, true, 0}, // system error 系统繁忙，此时请开发者稍候再试
		"45009": {CategoryQuotaExhausted, false, 0},
		"45011": {CategoryRateLimited, true, time.Minute}, // must slower, retry next minute
		// 同页原文：40001 invalid credential, access_token is invalid or not latest、
		// 40014 invalid access_token、41001 access_token missing、42001 access_token expired
		"40001": {CategoryAuth, false, 0},
		"40014": {CategoryAuth, false, 0},
		"41001": {CategoryAuth, false, 0},
		"42001": {CategoryAuth, false, 0},
	},
	"feishu": {
		// request trigger frequency limit；确切等待值在响应头 x-ogw-ratelimit-reset，由客户端带入 RetryAfter
		"99991400": {CategoryRateLimited, true, 0},
	},
	"qq": {
		"40034100": {CategoryRateLimited, true, 0},    // 主动消息发送超过频控限制
		"40034128": {CategoryWindowExpired, false, 0}, // 被动回复时间或次数超限：窗口已关
		"304103":   {CategoryWindowExpired, false, 0}, // msg_id 已过期，不能回复
		// 取凭证接口的四个业务码。官方两条硬事实：①「即使调用失败，HTTP 返回码仍为 200，
		// 请优先依据 code 判断」⇒ 状态码这一路永远看不到失败；② message「仅用于人工排查，
		// 内容可能随时调整」⇒ 文案判据不可依赖。取证 A 档：
		// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html，
		// 批J 2026-09-20 curl 直连 http=200 / 24,598 B / md5 f1acdd7dc218e41635259d0fa7fafea0。
		// 补表前这四个码在文本判据里全落空 ⇒ CategoryUnknown + Retryable=true：
		// AppID/Secret 错这类"重投不会自己变好"的失败要白撞三轮退避，
		// 且永不触发 CategoryAuth 那条运维事件（凭证坏了没人知道）。
		"100001": {CategoryRateLimited, true, 0}, // Too many requests：只说降频，未给解除时长
		"100007": {CategoryAuth, false, 0},       // appid invalid（AppID 无效或机器人被封禁/删除）
		"100016": {CategoryAuth, false, 0},       // invalid appid or secret
		"10004":  {CategoryAuth, false, 0},       // 机器人不存在（官方码值是 5 位 10004，不是 1000xx 一族）
	},
	"whatsapp": {
		"130429": {CategoryRateLimited, true, 0}, // (#130429) Rate limit hit
		"131048": {CategoryRateLimited, true, 0},
		"131056": {CategoryRateLimited, true, 0}, // 同一收方过快：1 message / 6s
		"131047": {CategoryAuth, false, 0},       // re-engagement：窗口外的会话不该重投
	},
}

func lookupCodeRule(channel, code string) (codeRule, bool) {
	if channel == "" || code == "" {
		return codeRule{}, false
	}
	r, ok := channelCodeRules[channel][code]
	return r, ok
}

func detectChannel(lower string) string {
	m := reChannelWord.FindStringSubmatch(lower)
	if m == nil {
		return ""
	}
	if mapped, ok := channelAlias[m[2]]; ok {
		return mapped
	}
	return m[2]
}

func classifyChannelError(raw string) *ChannelError {
	lower := strings.ToLower(raw)
	ce := &ChannelError{Raw: raw, Category: CategoryUnknown}
	ce.Channel = detectChannel(lower)
	if re := reChannelCode[ce.Channel]; re != nil {
		if m := re.FindStringSubmatch(lower); m != nil {
			ce.Code = m[1]
		}
	}
	if m := reHTTPStatus.FindStringSubmatch(lower); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n > 0 {
			ce.StatusCode = n
		}
	}
	if m := reRetryAfter.FindStringSubmatch(lower); m != nil {
		ce.RetryAfter = parseRetryAfterToken(m[1])
	}

	// 判据顺序：渠道业务码（稳定字段）优先于 HTTP 状态码，状态码优先于文案。
	if rule, ok := lookupCodeRule(ce.Channel, ce.Code); ok {
		ce.Category, ce.Retryable = rule.category, rule.retryable
		if ce.RetryAfter == 0 {
			ce.RetryAfter = rule.delay
		}
		return ce
	}

	switch {

	// 429 单独一档先判：它就是"被限流挡下"的官方口径。
	case ce.StatusCode == 429,
		strings.Contains(lower, "131048"),
		strings.Contains(lower, "131056"),
		strings.Contains(lower, "rate limit"),
		strings.Contains(lower, "too many requests"),
		strings.Contains(lower, "frequency limit"):
		ce.Category = CategoryRateLimited
		ce.Retryable = true

	case ce.StatusCode == 401, ce.StatusCode == 403,
		strings.Contains(lower, "131047"),
		strings.Contains(lower, "invalid access token"),
		// 微信系渠道（公众号/企微）在 errmsg 里写的就是这几句原文；码值缺失时（错误串
		// 只留了 errmsg）它们是凭证失效的唯一线索。下划线形态与空格形态都要认。
		strings.Contains(lower, "invalid credential"),
		strings.Contains(lower, "invalid access_token"),
		strings.Contains(lower, "access_token expired"),
		strings.Contains(lower, "access_token missing"),
		// 本仓自有常量（service/feishu.go 取 token 失败对外只返回这一句），不是渠道文案
		strings.Contains(lower, "access token failed"),
		strings.Contains(lower, "unauthorized"),
		strings.Contains(lower, "forbidden"):
		ce.Category = CategoryAuth
		ce.Retryable = false

	// 渠道开口让等多久，本身就说明这次是被限流挡下的（这条串里可能既没有 "status"
	// 字样也没有 "rate limit" 文案，比如本仓 pacing 只拼了 retry_after=1m0s）。
	// 排在 auth 之后：403 同时带 retry_after 属自相矛盾的响应，凭证类一律 fail-fast，
	// 不能被等待值带进重试通道。
	case ce.RetryAfter > 0:
		ce.Category = CategoryRateLimited
		ce.Retryable = true

	case ce.StatusCode == 400, ce.StatusCode == 404, ce.StatusCode == 422:
		ce.Category = CategoryBadRequest
		ce.Retryable = false

	case ce.StatusCode >= 500,
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "context deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "no such host"):
		ce.Category = CategoryNetwork
		ce.Retryable = true

	default:

		ce.Category = CategoryUnknown
		ce.Retryable = true
	}
	return ce
}

func retryDelaysFor(ce *ChannelError) []time.Duration {
	if ce == nil {
		return []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
	}
	switch ce.Category {
	case CategoryRateLimited:
		if ce.RetryAfter > 0 {
			return []time.Duration{ce.RetryAfter}
		}
		return []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
	case CategoryNetwork, CategoryUnknown:
		return []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
	default:
		return nil
	}
}

// outboundSendFailed 统一收口一次"投递失败"：结构化日志 → 授权告警 → 终态失败轨迹。
//
// content 是被丢弃的那条回复正文，只在需要另造轨迹行时用到（见下）。
func (s *WebhookService) outboundSendFailed(ctx context.Context, channel WebhookChannel, accountID string, hubMsg *model.MessageHub, content string, sendErr error) {
	ce := AsChannelError(sendErr)
	logger.Ctx(ctx).Error().
		Str("channel", string(channel)).
		Str("account_id", accountID).
		Str("category", string(ce.Category)).
		Int("status_code", ce.StatusCode).
		Bool("retryable", ce.Retryable).
		Msg("outbound send failed: " + ce.Raw)

	if ce.Category == CategoryAuth {
		if bus := event.GetGlobalBus(); bus != nil {
			bus.Publish(event.Event{
				Topic:     event.TopicOperationLog,
				Source:    "webhook_outbound",
				Timestamp: time.Now(),
				Payload: event.OperationLogPayload{
					Action:     "outbound_auth_failure",
					Module:     "channel",
					Resource:   string(channel),
					ResourceID: accountID,
				},
			})
		}
	}

	if hubMsg == nil || s.messageHubRepo == nil {
		return
	}

	if ce.Retryable {
		return
	}
	extra := model.JSONMap{}
	for k, v := range hubMsg.Extra {
		extra[k] = v
	}
	extra["send_failed_category"] = string(ce.Category)
	extra["send_failed_reason"] = ce.Raw

	// 只有"本来就有一行出站记录"时才能就地改状态。AI 出站链路实际拿到的 hubMsg 是入站
	// 消息的内存副本（ID==0、Direction=="inbound"），而 MarkOutboundSendFailed 的 WHERE
	// 要求 direction='outbound' ⇒ 命中 0 行，失败在库里没有任何反证。落不到就另造一行
	// send_failed 轨迹（与 TG 集成层 pushTelegramSendFailureTrace 同一口径）。
	if hubMsg.ID != 0 && hubMsg.Direction == "outbound" {
		_ = s.messageHubRepo.MarkOutboundSendFailed(ctx, hubMsg.ID, extra)
		return
	}
	s.pushUndeliveredReplyTrace(ctx, channel, accountID, hubMsg, content, ce)
}

// pushUndeliveredReplyTrace 给"生成过但没能投出去"的 AI 回复补一行出站轨迹。
//
// 走 MessageHubService.PushSendFailureTrace 而不是自己 repo.Create：那条路径带平台/类型/
// 正文长度校验，且与 TG 失败轨迹写法逐字同源，两处口径不会各自漂移。这里刻意用结构字面量
// 而不是 NewMessageHubServiceWithDB：构造函数会从全局句柄另开一个库连接，绕过用例注入的 repo。
func (s *WebhookService) pushUndeliveredReplyTrace(ctx context.Context, channel WebhookChannel, accountID string, hubMsg *model.MessageHub, content string, ce *ChannelError) {
	if content == "" || hubMsg.ConversationID == "" {
		return
	}
	hub := &MessageHubService{repo: s.messageHubRepo, maxContent: MessageHubDefaultMaxContent}
	receiver := hubMsg.SenderID
	if receiver == "" {
		receiver = hubMsg.ConversationID
	}
	agentID := extractAgentIDFromCtx(ctx)
	if agentID == "" {
		agentID = "sales_engine"
	}
	now := time.Now()
	_, err := hub.PushSendFailureTrace(ctx, &PushMessageRequest{
		Platform:       string(channel),
		AccountID:      accountID,
		MsgID:          fmt.Sprintf("%s-fail-%d", channel, now.UnixNano()),
		Direction:      "outbound",
		MsgType:        "text",
		SenderID:       accountID,
		ReceiverID:     receiver,
		Content:        content,
		ConversationID: hubMsg.ConversationID,
		IsGroup:        hubMsg.IsGroup,
		GroupID:        hubMsg.GroupID,
		IsAIReply:      true,
		AIAgent:        agentID,
		Extra:          map[string]any{"send_failed_category": string(ce.Category)},
		SentAt:         &now,
	}, ce.Raw)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("channel", string(channel)).Str("account_id", accountID).
			Str("conv_id", hubMsg.ConversationID).Msg("出站失败轨迹落库失败，本次投递在库内无痕")
	}
}
