package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/pkg/httpclient"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// 钉钉机器人入站媒体（M-01）：官方口径为
// open.dingtalk.com/document/development/download-the-file-content-of-the-robot-receiving-message
// —— POST https://api.dingtalk.com/v1.0/robot/messageFiles/download，header
// x-acs-dingtalk-access-token，body {downloadCode, robotCode}（两个都必填），
// 返回的是**预签名临时链接** downloadUrl，不是文件字节，需要再 GET 一次。
// accessToken 走新版 POST /v1.0/oauth2/accessToken（body {appKey, appSecret}，有效期 7200s）。
// downloadCode 的时限官方没有公布数字，只在错误码里出现「400 下载码有误或者已经过期」
// ⇒ 入站当场换取是唯一安全设计，不能延后到工作台点开时再取。

var (
	dtMediaFetchFn = FetchDingTalkRobotMedia
	dtMediaStoreFn = channelMediaPersist
)

// dingtalkSeamMu 守住 dingtalkOpenAPIBase 的每一次读和每一次写（包内其余位置直读 0 处）。
// 理由同 telegram_media.go 的 tgSeamMu：机器人入站媒体在协程里调默认实现
// FetchDingTalkRobotMedia → dingTalkNewAccessToken，两处拼的都是这个全局。
var (
	dingtalkSeamMu sync.RWMutex

	// dingtalkOpenAPIBase 是钉钉 v1.0 开放接口的域名根（取凭证 + 机器人接收文件下载两条腿都拼它）。
	// 必须是变量：这两条腿原先把主机名写死在调用点上，"HTTP 200 但 accessToken 为空"那一格
	// 只能真打外网才碰得到 ⇒ 全仓零用例（批J 实测：既有下载用例是整条换掉 dtMediaFetchFn，
	// 从没执行过这两个 URL 上的任何解析代码）。测试把它指向 httptest。
	dingtalkOpenAPIBase = "https://api.dingtalk.com"
)

func loadDingtalkOpenAPIBase() string {
	dingtalkSeamMu.RLock()
	defer dingtalkSeamMu.RUnlock()
	return dingtalkOpenAPIBase
}

// FetchDingTalkRobotMedia 用凭证换回机器人接收文件的字节流与 Content-Type。
func FetchDingTalkRobotMedia(ctx context.Context, appKey, appSecret, robotCode, downloadCode string) ([]byte, string, error) {
	if appKey == "" || appSecret == "" {
		return nil, "", errors.New("dingtalk app credentials missing")
	}
	if robotCode == "" || downloadCode == "" {
		return nil, "", errors.New("dingtalk robotCode/downloadCode missing")
	}
	token, err := dingTalkNewAccessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, "", err
	}
	body, _ := json.Marshal(map[string]string{"downloadCode": downloadCode, "robotCode": robotCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		loadDingtalkOpenAPIBase()+"/v1.0/robot/messageFiles/download", strings.NewReader(string(body)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-acs-dingtalk-access-token", token)
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("dingtalk media download status %d: %s", resp.StatusCode, dtErrSnippet(respBody))
	}
	var dl struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := json.Unmarshal(respBody, &dl); err != nil {
		return nil, "", fmt.Errorf("dingtalk media download parse: %w body=%s", err, dtErrSnippet(respBody))
	}
	if dl.DownloadURL == "" {
		return nil, "", fmt.Errorf("dingtalk media download empty url: %s", dtErrSnippet(respBody))
	}
	return fetchDingTalkTemporaryFile(ctx, dl.DownloadURL)
}

// fetchDingTalkTemporaryFile GET 预签名链接。链接本身已带签名，不能再附加任何 header，
// 否则 OSS/COS 风格的签名校验会失败。
func fetchDingTalkTemporaryFile(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return nil, "", fmt.Errorf("dingtalk download url rejected: %s", dtErrSnippet([]byte(rawURL)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("dingtalk file download status %d", resp.StatusCode)
	}
	data, err := readInboundMedia(resp.Body, maxInboundMediaBytes)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// dingTalkNewAccessToken 取新版企业内部应用 accessToken（同一 appKey 重复获取返回相同值并自动续期，
// 因此不必自建缓存；每条媒体消息多一次调用，换不来凭证落库与失效竞态的复杂度）。
func dingTalkNewAccessToken(ctx context.Context, appKey, appSecret string) (string, error) {
	body, _ := json.Marshal(map[string]string{"appKey": appKey, "appSecret": appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		loadDingtalkOpenAPIBase()+"/v1.0/oauth2/accessToken", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		AccessToken string `json:"accessToken"`
		ExpireIn    int64  `json:"expireIn"`
	}
	if err := json.Unmarshal(respBody, &tr); err != nil {
		// 片段必须带上：中间层（网关/登录页）把请求换成了 HTML 时，只有 json 报错的话，
		// 现场分不出是"钉钉答了"还是"我们根本没打到钉钉"（与抖音 token 腿同口径）。
		return "", fmt.Errorf("dingtalk token parse status %d: %w body=%s",
			resp.StatusCode, err, dtErrSnippet(respBody))
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("dingtalk token empty status %d: %s", resp.StatusCode, dtErrSnippet(respBody))
	}
	return tr.AccessToken, nil
}

// persistDingTalkMediaAsync 逐条下载码各转存一次（富文本可以带多张图），成功后把长期 URL
// 回填到本行 message_hub：media_url 放首个成功转存的链接（该列只有一个位置），全量 URL 进 Extra.media_urls。
// 失败仅告警：占位符正文已经入库，媒体不可用不该拖注入站链路。
func (s *DingTalkAppService) persistDingTalkMediaAsync(ctx context.Context, accountID uint, hubMsgID, conversationID, appKey, appSecret, robotCode string, codes []string, fileName string) {
	if len(codes) == 0 {
		return
	}
	if robotCode == "" {
		logger.Ctx(ctx).Warn().Uint("account_id", accountID).Str("msg_id", hubMsgID).
			Msg("[DingTalk] 媒体转存跳过：回调未携带 robotCode（换取下载链接的必填项）")
		return
	}
	// SafeGoDetached 而不是 SafeGo：钉钉回调是同步链路，请求 ctx 在 handler 写完响应时就 Done()，
	// 沿用它会把手头这次下载立刻取消掉（媒体永久丢失，而用例传 Background() 时看不出来）。
	// 解耦取消链但保留 ctx.Value（trace_id 等），用 5 分钟硬上限防泄漏。
	// 包级注入点进协程前先快照成本地值：上一条用例残留的协程若直读包级变量，会和下一条用例装替身的写撞成 DATA RACE。
	mediaFetchFn, mediaStoreFn := dtMediaFetchFn, dtMediaStoreFn
	utils.SafeGoDetached(ctx, "dingtalk.media_persist", 5*time.Minute, func(gctx context.Context) {
		urls := make([]string, 0, len(codes))
		for _, code := range codes {
			data, contentType, ferr := mediaFetchFn(gctx, appKey, appSecret, robotCode, code)
			if ferr != nil {
				logger.Ctx(gctx).Warn().Err(ferr).Str("media_code", code).
					Msg("[DingTalk] 媒体下载失败（占位符保留）")
				continue
			}
			publicURL, serr := mediaStoreFn(gctx, "dingtalk", code, data, contentType, fileName)
			if serr != nil {
				logger.Ctx(gctx).Warn().Err(serr).Str("media_code", code).Msg("[DingTalk] 媒体转存失败")
				continue
			}
			urls = append(urls, publicURL)
		}
		if len(urls) == 0 {
			return
		}
		s.backfillDingTalkMedia(gctx, accountID, hubMsgID, conversationID, urls)
		logger.Ctx(gctx).Info().Uint("account_id", accountID).Str("msg_id", hubMsgID).
			Strs("urls", urls).Msg("[DingTalk] 入站媒体已转存")
	})
}

// backfillDingTalkMedia 回填 media_url 与 Extra.media_urls，按四键定位入站行。
// 不走 EnrichHubMediaURLByMsgID：那条只写单值列，富文本的多张图片会在最后一次写里互相覆盖。
func (s *DingTalkAppService) backfillDingTalkMedia(ctx context.Context, accountID uint, hubMsgID, conversationID string, urls []string) {
	if s.hubRepo == nil || hubMsgID == "" {
		return
	}
	accID := strconv.FormatUint(uint64(accountID), 10)
	found, err := s.hubRepo.SetInboundMediaURLs(ctx, "dingtalk", accID, conversationID, hubMsgID, urls)
	switch {
	case err != nil:
		logger.Ctx(ctx).Warn().Err(err).Str("msg_id", hubMsgID).Str("conv_id", conversationID).
			Msg("[DingTalk] 媒体回填失败：hub 行读取或写入出错")
	case !found:
		logger.Ctx(ctx).Warn().Str("msg_id", hubMsgID).Str("conv_id", conversationID).
			Msg("[DingTalk] 媒体回填失败：按 (platform, account_id, conversation_id, msg_id) 找不到 hub 行")
	}
}

// dtErrSnippet 错误响应片段进日志前的两道处理：截断到 200 字符 + 抹掉可用凭证。
//
// 只截断不够：这两条腿的响应体里天然带着凭证——凭证腿的 accessToken（有效期 7200s）、
// 下载腿的 downloadUrl（预签名链接，等价于一把有时效的临时凭证），而它们所在的那一行
// 会被异步转存原样打进 warn 日志、长期留存。空值必须原样带出（批J 的「200 带空 accessToken」
// 那一格靠 `accessToken":""` 判因），所以抹的是**非空值**。
func dtErrSnippet(b []byte) string {
	s := dtQueryRe.ReplaceAllString(string(b), "?<redacted>")
	s = dtSecretValueRe.ReplaceAllStringFunc(s, func(m string) string {
		g := dtSecretValueRe.FindStringSubmatch(m)
		if g[2] == "" {
			return m
		}
		return g[1] + "<redacted>" + g[3]
	})
	r := []rune(s)
	if len(r) <= 200 {
		return string(r)
	}
	return string(r[:200]) + "…"
}

// dtSecretValueRe 只认这几个键的值：键名之外的正文（网关 HTML 里的 title、钉钉的
// code/message/requestId）要留着判因，否则"钉钉答了"和"我们根本没打到钉钉"又分不出来了。
var (
	dtSecretValueRe = regexp.MustCompile(`(?i)("(?:access_?token|appsecret|app_secret|security_?token|download_?url)"\s*:\s*")((?:[^"\\]|\\.)*)(")`)
	dtQueryRe       = regexp.MustCompile(`\?[^"\\\s]*`)
)
