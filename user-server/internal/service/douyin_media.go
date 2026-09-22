package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 抖音私信入站媒体（批G-2b）。
//
// 官方口径两份原文（A 档，含 URL 与字节数登记在审计文档 §16.1）：
//   - .../dop/develop/openapi/account-permission/client-token
//     POST https://open.douyin.com/oauth/client_token/
//     body {grant_type:"client_credential", client_key, client_secret}，content-type 固定 application/json
//     resp {data:{access_token, description, error_code, expires_in}, message}
//     「有效时间为 2 个小时，重复获取 client_token 后会使上次的 client_token 失效（但有 5 分钟的缓冲时间…）」
//     「禁止频繁调用 access-token 接口（频控规则：5 分钟内超过 500 次接口调用，接口报错，错误码 10020）」
//   - .../dop/develop/openapi/search-management/business-tool/get-message-resources
//     GET https://open.douyin.com/api/im/message/resources/
//     header access-token + content-type；query open_id(必填)/conversation_id/message_id
//     resp data{media_type:image/video, url}，「资源访问链接，有效期 30 天」
//     「访问 URL 时，需额外在请求 Header 中携带 Access-Token, OpenID 字段」
//     「URL 中可能包含转义字符 \u0026，需要将其替换为 &」
//     「仅支持…user_local_image 和 user_local_video」
//     业务错误一律 HTTP 200 + err_no≠0
//
// 为什么必须入站当场换：webhook 给的只有 ID，直链要另外签发（且只 30 天），
// 占位符正文不是媒体 —— 拖到工作台点开时再取，取到的机会只会更低。

const (
	douyinOfficialAPIBase     = "https://open.douyin.com"
	douyinClientTokenPath     = "/oauth/client_token/"
	douyinMsgResourcesPath    = "/api/im/message/resources/"
	douyinGrantTypeClientCred = "client_credential"
)

// 官方错误码里与凭证有关的两个（处置都是「重新生成 access_token」）。
//
// 注意：**不能把"其余 err_no 都是终态"当成前提**。status 表里另有三个码官方直接写了重试
// （见 dyErrSysBusy 等），瞬时错误当终态的代价是「这条消息永久没有媒体」，且只有一行 Warn。
const (
	dyErrTokenInvalid = 28001003 // access_token无效 → 重新请求生成
	dyErrTokenExpired = 28001008 // access_token过期,请刷新或重新授权

	dyErrSysBusy  = 28001005 // 系统繁忙/系统内部错误，此时请开发者稍候再试
	dyErrNetCall  = 28001006 // 网络调用错误，请重试（官方处置列直接写「重试即可」）
	dyErrResSign  = 28029014 // 资源签发失败，请重试
	dyErrNoBucket = 28001012 // 用户未授权该 OpenAPI（权限/口径问题，重试无用，只记不试）
	dyErrMismatch = 28001015 // access_token 与 openId 不匹配（同上）
)

// dyMediaRetryBackoff 重试间隔（长度即额外尝试次数）。
// 用例把它改小，免得为瞬时错误真的等近一秒；生产用默认值。
var dyMediaRetryBackoff = []time.Duration{200 * time.Millisecond, 600 * time.Millisecond}

// retryable 官方要我们重试的码：28001005/28001006 写在处置列里，28029014 只写在错误文案里
// （它的处置列是空的）。凭证类的两个不在此列：它们要的是换 token，不是重投。
func (e *douyinAPIError) retryable() bool {
	switch e.ErrNo {
	case dyErrSysBusy, dyErrNetCall, dyErrResSign:
		return true
	}
	return false
}

// douyinTransientError 带外瞬时失败：网关 5xx / 429 / 连接层报错。语义与 28001005 相同，
// 只是没走 err_no 通道 —— 只重投 in-band 码而放过 502，等于把同一个"永久没有媒体"
// 按通道形状区别对待。
type douyinTransientError struct{ msg string }

func (e *douyinTransientError) Error() string { return e.msg }

func douyinStatusRetryable(code int) bool {
	return code >= http.StatusInternalServerError || code == http.StatusTooManyRequests
}

func douyinTransportError(what string, err error) error {
	return &douyinTransientError{fmt.Sprintf("douyin %s transport: %v", what, err)}
}

func douyinStatusError(what string, code int, raw []byte) error {
	msg := fmt.Sprintf("douyin %s status %d: %s", what, code, dyErrSnippet(raw))
	if douyinStatusRetryable(code) {
		return &douyinTransientError{msg}
	}
	return errors.New(msg)
}

// douyinErrRetryable 这次失败值不值得整动作重投。凭证类（28001003/28001008）不在内：
// 它们要的是换 token 重取，那条路在 once 里已经走过一次，重投只会把同一个坏值再送一遍。
func douyinErrRetryable(err error) bool {
	var te *douyinAPIError
	if errors.As(err, &te) {
		return te.retryable()
	}
	var tr *douyinTransientError
	return errors.As(err, &tr)
}

// douyinWaitRetry 退避到下一次尝试；false 表示预算用尽或 ctx 已结束，都不该再重投。
func douyinWaitRetry(ctx context.Context, attempt int) bool {
	if attempt >= len(dyMediaRetryBackoff) {
		return false
	}
	select {
	case <-time.After(dyMediaRetryBackoff[attempt]):
		return true
	case <-ctx.Done():
		return false
	}
}

var (
	dyMediaFetchFn = FetchDouyinMessageResource
	dyMediaStoreFn = channelMediaPersist
)

// dyAPIBaseOverride 只给用例用：把两条腿指向假平台。生产恒为空。
var dyAPIBaseOverride string

func douyinAPIBase() string {
	if s := strings.TrimSpace(dyAPIBaseOverride); s != "" {
		return strings.TrimRight(s, "/")
	}
	return douyinOfficialAPIBase
}

// douyinMediaRef 换取一条媒体所需的三个官方标识（缺一不可，见 complete）。
type douyinMediaRef struct {
	OpenID         string // 发送人 from_user_id（官方：通过 /oauth/access_token/ 获取的用户唯一标志）
	ConversationID string // content.conversation_short_id（长期有效）
	MessageID      string // content.server_message_id
}

func (r douyinMediaRef) complete() bool {
	return r.OpenID != "" && r.ConversationID != "" && r.MessageID != ""
}

// storageKey 存储键。官方两个 ID 都是 base64 形态（含 / + = ），直接当文件名会让
// 存储层把 "/" 拼成子目录、把 ".." 拼成穿越 —— 只有转存成功时才走这条路，日常看不出来。
// 换成 sha1 前 16 位：定长、仅 [0-9a-f]、同一条消息稳定（重投不会多存一份）。
func (r douyinMediaRef) storageKey() string {
	if k := douyinMsgKey(r.MessageID); k != "" {
		return k
	}
	return douyinMsgKey(r.OpenID + "|" + r.ConversationID)
}

// douyinNeedsMedia 官方只给这两类签发资源链接。
// image/emoji 这类报文里本来就带 resource_url 直链，敲过去只会得到 28029016 不支持的消息类型。
func douyinNeedsMedia(messageType string) bool {
	return messageType == "user_local_image" || messageType == "user_local_video"
}

// ─── client_token ───────────────────────────────────────────────

type douyinTokenEntry struct {
	token    string
	expireAt time.Time
}

// 缓存按 client_key 分账号存：一台服务多个抖音应用是常态，共用一把会互相顶号。
// 顶号是**静默**的（旧 token 还有 5 分钟缓冲，之后开始回 28001003），
// 所以这里既是省钱（官方频控 500 次/5 分钟）也是防互相踢。
var (
	douyinTokenMu     sync.Mutex
	douyinTokenCache  = map[string]douyinTokenEntry{}
	douyinTokenSafety = 5 * time.Minute
)

// douyinClientToken 取缓存的 client_token；缺失或临近过期时生成新的。
func douyinClientToken(ctx context.Context, clientKey, clientSecret string) (string, error) {
	return douyinClientTokenForce(ctx, clientKey, clientSecret, false)
}

// douyinClientTokenForce force=true 时无视缓存重新生成（供凭证被判失效后重试）。
func douyinClientTokenForce(ctx context.Context, clientKey, clientSecret string, force bool) (string, error) {
	if clientKey == "" || clientSecret == "" {
		return "", errors.New("douyin client_key/client_secret missing")
	}
	// 取 token 全程持锁：并发各取一次就会互相顶号，而那正是本函数要避免的事。
	douyinTokenMu.Lock()
	defer douyinTokenMu.Unlock()

	if !force {
		if e, ok := douyinTokenCache[clientKey]; ok && time.Now().Before(e.expireAt) {
			return e.token, nil
		}
	}

	body, _ := json.Marshal(map[string]string{
		"grant_type":    douyinGrantTypeClientCred,
		"client_key":    clientKey,
		"client_secret": clientSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		douyinAPIBase()+douyinClientTokenPath, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return "", douyinTransportError("client_token", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", douyinTransportError("client_token", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", douyinStatusError("client_token", resp.StatusCode, raw)
	}
	var parsed struct {
		Data struct {
			AccessToken string `json:"access_token"`
			Description string `json:"description"`
			ErrorCode   int64  `json:"error_code"`
			ExpiresIn   int64  `json:"expires_in"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("douyin client_token parse: %w body=%s", err, dyErrSnippet(raw))
	}
	if parsed.Data.AccessToken == "" {
		return "", fmt.Errorf("douyin client_token empty error_code=%d message=%s body=%s",
			parsed.Data.ErrorCode, parsed.Message, dyErrSnippet(raw))
	}
	ttl := time.Duration(parsed.Data.ExpiresIn) * time.Second
	if ttl <= 0 {
		// 官方示例给 7200；没给就别按「永久」缓存，按一半的保守值重取。
		ttl = douyinDefaultTokenTTL
	}
	if ttl <= douyinTokenSafety {
		ttl = 0 // 有效期本身比提前量还短：不缓存，下次重新取
	} else {
		ttl -= douyinTokenSafety
	}
	douyinTokenCache[clientKey] = douyinTokenEntry{token: parsed.Data.AccessToken, expireAt: time.Now().Add(ttl)}
	return parsed.Data.AccessToken, nil
}

// douyinDefaultTokenTTL 官方「有效时间为 2 个小时」，用于响应没带 expires_in 的场合。
const douyinDefaultTokenTTL = 2 * time.Hour

// ─── 资源链接与下载 ─────────────────────────────────────────────

// FetchDouyinMessageResource 换一条媒体的全部字节：整体动作按 dyMediaRetryBackoff 有界重投。
//
// 重投落在**这一层**而不是单条腿上，是因为代价按整动作算：三条腿（client_token、
// resources、直链下载）任何一条被带外抖动打断，结果都是「这条消息永久没有媒体」+ 一行 Warn。
// 重投复用同一个 client_token（缓存命中 ⇒ 不会多取一次 token、不会顶号），
// 只有官方判凭证失效时才由 once 内部强制重取。
func FetchDouyinMessageResource(ctx context.Context, clientKey, clientSecret string, ref douyinMediaRef) ([]byte, string, error) {
	if !ref.complete() {
		return nil, "", fmt.Errorf("douyin media ref incomplete: open_id/conversation_id/message_id 缺一不可 (%+v)", ref)
	}
	for attempt := 0; ; attempt++ {
		data, contentType, err := douyinFetchMessageResourceOnce(ctx, clientKey, clientSecret, ref)
		if err == nil || !douyinErrRetryable(err) {
			return data, contentType, err
		}
		if !douyinWaitRetry(ctx, attempt) {
			return nil, "", err
		}
		logger.Ctx(ctx).Debug().Err(err).Int("attempt", attempt+1).
			Str("media_id", ref.MessageID).Msg("[Douyin] 媒体腿回瞬时错误，退避后整动作重投")
	}
}

// douyinFetchMessageResourceOnce 一趟两步换链 + 下载：client_token → resources(url) → 带凭证下载字节。
func douyinFetchMessageResourceOnce(ctx context.Context, clientKey, clientSecret string, ref douyinMediaRef) ([]byte, string, error) {
	token, err := douyinClientToken(ctx, clientKey, clientSecret)
	if err != nil {
		return nil, "", err
	}
	meta, err := douyinMessageResource(ctx, token, ref)
	if err != nil {
		var te *douyinAPIError
		if !errors.As(err, &te) || !te.tokenRejected() {
			return nil, "", err
		}
		// 官方对 28001003/28001008 的排查建议都是「重新请求生成 access_token」：
		// 缓存里那个值可能已被别处顶号（或被平台判过期），带着它重试三次只是把同一个坏值再送三遍。
		token, err = douyinClientTokenForce(ctx, clientKey, clientSecret, true)
		if err != nil {
			return nil, "", err
		}
		if meta, err = douyinMessageResource(ctx, token, ref); err != nil {
			return nil, "", err
		}
	}
	return douyinDownloadResource(ctx, token, ref.OpenID, meta.URL)
}

type douyinResourceMeta struct {
	MediaType string `json:"media_type"`
	URL       string `json:"url"`
}

// douyinAPIError resources 接口的业务错误：HTTP 200 + err_no≠0 是官方的失败形态，
// 只看状态码会把它当成功（再去下载一个空 URL）。
type douyinAPIError struct {
	ErrNo  int64
	ErrMsg string
	LogID  string
}

func (e *douyinAPIError) Error() string {
	return fmt.Sprintf("douyin api err_no=%d err_msg=%s log_id=%s", e.ErrNo, e.ErrMsg, e.LogID)
}

func (e *douyinAPIError) tokenRejected() bool {
	return e.ErrNo == dyErrTokenInvalid || e.ErrNo == dyErrTokenExpired
}

func douyinMessageResource(ctx context.Context, token string, ref douyinMediaRef) (*douyinResourceMeta, error) {
	// 官方：conversation_id / message_id 含 + = 等特殊字符，传参时需要编码。
	// 不编码的后果是**静默**的："+" 在 query 里被解成空格 ⇒ 28029020 未获取到资源链接，
	// 而我们只看到「这条消息没有媒体」。
	q := url.Values{}
	q.Set("open_id", ref.OpenID)
	q.Set("conversation_id", ref.ConversationID)
	q.Set("message_id", ref.MessageID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		douyinAPIBase()+douyinMsgResourcesPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("access-token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, douyinTransportError("resources", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, douyinTransportError("resources", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, douyinStatusError("resources", resp.StatusCode, raw)
	}
	var payload struct {
		ErrNo  int64                 `json:"err_no"`
		ErrMsg string                `json:"err_msg"`
		LogID  string                `json:"log_id"`
		Data   douyinResourceMetaRaw `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("douyin resources parse: %w body=%s", err, dyErrSnippet(raw))
	}
	if payload.ErrNo != 0 {
		return nil, &douyinAPIError{ErrNo: payload.ErrNo, ErrMsg: payload.ErrMsg, LogID: payload.LogID}
	}
	u := douyinUnescapeResourceURL(payload.Data.URL)
	if u == "" {
		return nil, fmt.Errorf("douyin resources empty url err_no=0 log_id=%s body=%s",
			payload.LogID, dyErrSnippet(raw))
	}
	return &douyinResourceMeta{MediaType: payload.Data.MediaType, URL: u}, nil
}

// douyinResourceMetaRaw data 的承载位。官方把 data 标成必填 struct，但异常示例里给的是
// {} ⇒ 用值类型解即可，缺失时字段为空串，由上层按「没有链接」处理。
type douyinResourceMetaRaw struct {
	MediaType string `json:"media_type"`
	URL       string `json:"url"`
}

// douyinUnescapeResourceURL 官方：「URL 中可能包含转义字符 \u0026，需要将其替换为 &」。
// 正常 JSON 解码已经会把 \u0026 还原成 &，这里兜的是**双重编码**（整个响应体作为字符串再包一层，
// 或平台侧直接吐字面量）：那时字面量会带着反斜杠进请求，拿到的是 404 而不是「资源不存在」。
func douyinUnescapeResourceURL(u string) string {
	return strings.TrimSpace(strings.ReplaceAll(u, `\u0026`, "&"))
}

// douyinDownloadResource 按官方要求带着 Access-Token + OpenID 取字节（直链本身不鉴权）。
func douyinDownloadResource(ctx context.Context, token, openID, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, "", fmt.Errorf("douyin resource url rejected: %s", dyErrSnippet([]byte(rawURL)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Access-Token", token)
	req.Header.Set("OpenID", openID)
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", douyinTransportError("media download", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 不回显 body：这一条是文件字节流，几 MB 内容进错误信息会淹掉日志。
		msg := fmt.Sprintf("douyin media download status %d", resp.StatusCode)
		if douyinStatusRetryable(resp.StatusCode) {
			return nil, "", &douyinTransientError{msg}
		}
		return nil, "", errors.New(msg)
	}
	data, err := readInboundMedia(resp.Body, maxInboundMediaBytes)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ─── 入站接线 ───────────────────────────────────────────────────

// persistDouyinMediaAsync 转存并回填本行 message_hub。失败只告警：占位符正文已入库，
// 媒体不可用不该拖注入站链路。
func (s *WebhookService) persistDouyinMediaAsync(ctx context.Context, channel WebhookChannel, accountID, hubMsgID string, ref douyinMediaRef) {
	if channel != ChannelDouyin {
		// TikTok 的媒体接口没有同源的 A 档证据（域名/鉴权/路径都不同，审计 §16.3 记为未证项）。
		// 拿抖音的端点去敲 TikTok 的 ID 只会静默失败，这里一次都不发。
		return
	}
	platform, _ := douyinPlatformKeyPrefix(channel)
	if hubMsgID == "" || !ref.complete() {
		return
	}
	clientKey, clientSecret, err := s.douyinClientCreds(ctx, channel, accountID)
	if err != nil || clientKey == "" || clientSecret == "" {
		logger.Ctx(ctx).Warn().Err(err).Str("account_id", accountID).Str("msg_id", hubMsgID).
			Msg("[Douyin] 入站媒体跳过转存：账号未配 client_key/client_secret（只有 webhook 密钥换不到 token）")
		return
	}

	// SafeGoDetached：转存是几 MB 的网络往返，挂在 4 个 webhook worker 上会把后面所有
	// 渠道的事件堵住（队头阻塞）；解耦取消链同时保留 ctx.Value，5 分钟硬上限防泄漏。
	utils.SafeGoDetached(ctx, "douyin.media_persist", 5*time.Minute, func(gctx context.Context) {
		data, contentType, ferr := dyMediaFetchFn(gctx, clientKey, clientSecret, ref)
		if ferr != nil {
			logger.Ctx(gctx).Warn().Err(ferr).Str("media_id", ref.MessageID).
				Msg("[Douyin] 入站媒体下载失败（占位符保留）")
			return
		}
		key := ref.storageKey()
		publicURL, serr := dyMediaStoreFn(gctx, platform, key, data, contentType, "")
		if serr != nil {
			logger.Ctx(gctx).Warn().Err(serr).Str("media_key", key).Msg("[Douyin] 入站媒体转存失败")
			return
		}
		s.backfillDouyinMedia(gctx, accountID, hubMsgID, publicURL)
		logger.Ctx(gctx).Info().Str("account_id", accountID).Str("msg_id", hubMsgID).Str("url", publicURL).
			Msg("[Douyin] 入站媒体已转存")
	})
}

// backfillDouyinMedia 回填 media_url（不抹 Extra：官方 server_message_id 是报障时对号的唯一抓手）。
func (s *WebhookService) backfillDouyinMedia(ctx context.Context, accountID, hubMsgID, publicURL string) {
	db := s.lazyDB()
	if db == nil || hubMsgID == "" || publicURL == "" {
		return
	}
	var hub model.MessageHub
	if err := db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND msg_id = ?", string(ChannelDouyin), accountID, hubMsgID).
		First(&hub).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("msg_id", hubMsgID).
			Msg("[Douyin] 媒体回填失败：找不到 hub 行")
		return
	}
	extra := hub.Extra
	if extra == nil {
		extra = model.JSONMap{}
	}
	extra["media_urls"] = []string{publicURL}
	if err := db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ?", hub.ID).
		Updates(map[string]any{"media_url": publicURL, "extra": extra}).Error; err != nil {
		logger.Ctx(ctx).Warn().Err(err).Uint("hub_id", hub.ID).Msg("[Douyin] 媒体回填写库失败")
	}
}

// douyinClientCreds 取应用的 client_key + client_secret。
// client_key 只认**账号上配的**值：信封里那个 client_key 是调用方的应用标识，
// 拿它配上我们的 secret 去换 token，等于替别的应用生成凭证。
// 平台按**入站渠道**取而不是写死 douyin：写死时那两级查询会把抖音应用的凭证
// 发给任何调用方（包括 TikTok 的账号），两家共用一台服务时就是跨应用取凭证。
func (s *WebhookService) douyinClientCreds(ctx context.Context, channel WebhookChannel, accountID string) (string, string, error) {
	if s.accountRepo == nil {
		return "", "", errors.New("account repo 未接线")
	}
	acc, err := s.accountRepo.GetByPlatformAndAccount(ctx, string(channel), accountID)
	if err != nil || acc == nil {
		if acc, err = s.accountRepo.GetByPlatform(ctx, string(channel)); err != nil || acc == nil {
			return "", "", err
		}
	}
	return acc.APIKey, acc.APISecret, nil
}

// dyErrSnippet 错误响应只取前 200 字节进日志：整段回显会把直链（等价于临时凭证）留在日志里。
func dyErrSnippet(b []byte) string {
	r := []rune(string(b))
	if len(r) <= 200 {
		return string(r)
	}
	return string(r[:200]) + "…"
}
