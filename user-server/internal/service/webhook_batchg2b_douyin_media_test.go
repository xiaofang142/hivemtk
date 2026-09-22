package service

// 批G-2b：抖音入站媒体转存（user_local_image / user_local_video）。
//
// 现场：私信里的「用户本地图片/本地视频」在官方报文里**只有两个 ID**
// （content.conversation_short_id + content.server_message_id），既没有直链也没有字节。
// 批G-1 之后这类消息会落一行 [图片] 占位符，但占位符不是媒体 —— 工作台点开仍然什么都没有。
//
// 官方取证（A 档原文与 URL 登记在审计文档 §16.1）：
//   - .../dop/develop/openapi/account-permission/client-token
//     POST https://open.douyin.com/oauth/client_token/ ，content-type 固定 application/json，
//     body {grant_type:"client_credential", client_key, client_secret}；
//     响应 {data:{access_token, description, error_code, expires_in:7200}, message:"success"}。
//     「client_token 的有效时间为 2 个小时，重复获取 client_token 后会使上次的 client_token 失效
//      （但有 5 分钟的缓冲时间…）」「禁止频繁调用 access-token 接口
//      （频控规则：5 分钟内超过 500 次接口调用，接口报错，错误码 10020）」
//   - .../dop/develop/openapi/search-management/business-tool/get-message-resources
//     GET https://open.douyin.com/api/im/message/resources/ ，header access-token + content-type，
//     query open_id(必填)/conversation_id/message_id；
//     「仅支持在接收消息 webhook 返回的消息类型为 user_local_image 和 user_local_video 时返回相关 url 资源」
//     「由于 conversation_id 包含 + = 等特殊字符，传参时需要进行编码」（message_id 同）
//     响应 data{media_type:image/video, url}，「资源访问链接，有效期 30 天」，
//     「访问 URL 时，需额外在请求 Header 中携带 Access-Token, OpenID 字段」
//     「注意：URL 中可能包含转义字符 \u0026，需要将其替换为 & 才能正常访问资源」
//     错误一律 HTTP 200 + err_no≠0（28001003 access_token无效 / 28029020 未获取到资源链接 …），
//     本页处置列另给 28029014「资源签发失败，请重试」
//   - .../dop/develop/openapi/status-code（状态码排查工具页，全量码表）
//     28001005「系统繁忙，此时请开发者稍候再试」→ 请求重试；28001006「网络调用错误，请重试」→ 重试即可
//     ⇒ 与上一条合起来三个码官方直接要求重试（不是终态）。
//     28001012「用户未授权该OpenAPI」→ 获取含对应 scope 的 access_token；
//     28001015「请求参数 access_token 和 openId 不匹配」→ 核对是否同一用户。
//
// 一条**推读**（官方没写，别当作取证）：官方只给了 in-band err_no 的重试口径，从没写过
// 网关回 502/503/504、或连接层直接断链时该怎么办。本实现把带外 5xx/429/EOF 与 28001005
// 同权重投 —— 依据不是原话，而是后果逐字相同（这条消息永久没有媒体，只留一行 Warn）。
// 4xx 除 429 仍判终态：403 在官方口径里是「否则无法访问相关资源」，重投救不了。
//
// 一处**未证项**（不能靠文档关掉，登记在审计 §16.1 第 8 条）：resources 的 access-token 头
// 官方示例值是 act.…（用户级 token 的形态），而 client_token 文档的示例值是 clt.…（应用级），
// 两处都不是硬约束。本实现取 clt.…（私信发送方从未授权过本应用，拿不到它的 act. token）；
// 真机若回 28001015/28001012/28001003/28001008，**每一条都可能是这条口径错了的后果**（平台落在
// 哪一格不由我们决定，判据的读法见审计 §16.1 第 8 条③）—— 四格都要求原样带出 err_no。
//
// 本文件的假平台按上面两份原文应答，不与被测实现共享任何常量或解析代码。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// g2bAPI 一个逐字段按官方口径应答的假开放平台：同时扮演 client_token 服务、
// resources 服务和媒体文件服务器（返回的 url 指回自己）。
type g2bAPI struct {
	srv *httptest.Server

	mu         sync.Mutex
	tokenCalls int
	resCalls   int
	dlCalls    int

	tokenSeenBody string
	tokenSeenCT   string
	resSeenRaw    string
	resSeenQuery  url.Values
	resSeenHdr    http.Header
	dlSeenHdr     http.Header
	dlSeenURI     string

	expiresIn  int
	tokenSeq   int
	resReplies []string // 每次 resources 调用弹出一个响应体；空则回默认成功
	resSeq     []int    // 逐次弹出的 resources 状态码（非 200 时不回 body 里的成功体）
	resDrop    []int    // 逐次"连接被掐"（hijack 后 close）：制造连接层错误，与状态码两回事
	dlBody     []byte
	dlStatus   int
	dlSeq      []int // 逐次弹出的下载状态码；非空时优先于 dlStatus
	tokStatus  int
	tokSeq     []int    // 逐次弹出的 client_token 状态码
	tokBodies  []string // 逐次弹出的 client_token 响应体（HTTP 200；空则回默认成功体）
}

func newG2BAPI(t *testing.T, clientKey string) *g2bAPI {
	t.Helper()
	a := &g2bAPI{expiresIn: 7200, dlBody: []byte("dy-media-bytes-of-" + clientKey),
		dlStatus: http.StatusOK, tokStatus: http.StatusOK}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/client_token/":
			body, _ := io.ReadAll(r.Body)
			a.mu.Lock()
			a.tokenCalls++
			a.tokenSeq++
			a.tokenSeenBody = string(body)
			a.tokenSeenCT = r.Header.Get("Content-Type")
			seq := a.tokenSeq
			exp := a.expiresIn
			tstatus := a.tokStatus
			if len(a.tokSeq) > 0 {
				tstatus, a.tokSeq = a.tokSeq[0], a.tokSeq[1:]
			}
			tbody := ""
			if len(a.tokBodies) > 0 {
				tbody, a.tokBodies = a.tokBodies[0], a.tokBodies[1:]
			}
			a.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if tstatus != http.StatusOK {
				w.WriteHeader(tstatus)
				fmt.Fprintf(w, `{"data":{},"message":"token status %d"}`, tstatus)
				return
			}
			if tbody != "" {
				// HTTP 200 但内容由用例指定：官方对"凭证不对/被频控"就是这一格形状。
				_, _ = io.WriteString(w, tbody)
				return
			}
			fmt.Fprintf(w, `{"data":{"access_token":"clt.test-%d","description":"","error_code":0,"expires_in":%d},"message":"success"}`,
				seq, exp)
		case "/api/im/message/resources/":
			a.mu.Lock()
			a.resCalls++
			a.resSeenRaw = r.URL.RawQuery
			a.resSeenQuery = r.URL.Query()
			a.resSeenHdr = r.Header.Clone()
			reply := ""
			if len(a.resReplies) > 0 {
				reply = a.resReplies[0]
				a.resReplies = a.resReplies[1:]
			}
			dl := a.dlURL()
			rstatus := http.StatusOK
			if len(a.resSeq) > 0 {
				rstatus, a.resSeq = a.resSeq[0], a.resSeq[1:]
			}
			drop := len(a.resDrop) > 0
			if drop {
				a.resDrop = a.resDrop[1:]
			}
			a.mu.Unlock()
			if drop {
				// 一个字节都不回就断链：客户端拿到的是 EOF，不是某个 HTTP 状态码。
				hj, ok := w.(http.Hijacker)
				if !ok {
					return
				}
				conn, _, herr := hj.Hijack()
				if herr == nil {
					_ = conn.Close()
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if rstatus != http.StatusOK {
				w.WriteHeader(rstatus)
				fmt.Fprintf(w, `{"err_no":0,"data":{},"message":"resources status %d"}`, rstatus)
				return
			}
			if reply == "" {
				reply = `{"err_no":0,"err_msg":"","log_id":"lg-ok","data":{"media_type":"image","url":"` + dl + `"}}`
			}
			_, _ = io.WriteString(w, reply)
		default: // 媒体直链落在 /im/media
			body, _ := io.ReadAll(r.Body)
			_ = body
			a.mu.Lock()
			a.dlCalls++
			a.dlSeenHdr = r.Header.Clone()
			a.dlSeenURI = r.URL.RequestURI()
			status, payload := a.dlStatus, a.dlBody
			if len(a.dlSeq) > 0 {
				status, a.dlSeq = a.dlSeq[0], a.dlSeq[1:]
			}
			a.mu.Unlock()
			if status == http.StatusOK {
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write(payload)
				return
			}
			w.WriteHeader(status)
			// 回一行短文本而不是空 body：官方失败应答也带内容，"有内容 ⇒ 已消费"
			// 这条链不能被一个真空响应替掉。
			fmt.Fprintf(w, "download status %d", status)
		}
	}))
	t.Cleanup(a.srv.Close)

	prev := dyAPIBaseOverride
	dyAPIBaseOverride = a.srv.URL
	t.Cleanup(func() { dyAPIBaseOverride = prev })
	return a
}

// dlURL 资源直链（query 里已带一个转义过的 = ，逼近官方示例的形态）。
func (a *g2bAPI) dlURL() string { return a.srv.URL + "/im/media?secret=skv2Z1fhiKw1gGBsGa4%3D" }

func (a *g2bAPI) counts() (token, res, dl int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tokenCalls, a.resCalls, a.dlCalls
}

func (a *g2bAPI) snapshot() (tokenBody, rawQuery, dlURI string, resQ url.Values, resH, dlH http.Header) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tokenSeenBody, a.resSeenRaw, a.dlSeenURI, a.resSeenQuery, a.resSeenHdr, a.dlSeenHdr
}

// g2bSetup 装一个 client_key/client_secret 齐备的抖音账号。
//
// 本 helper **不碰** dyMediaFetchFn/dyMediaStoreFn：钉钉侧的 f4DtSetup 在 helper 里装了
// 报错替身，把调用方刚装的替身静默覆盖掉（三个下载用例红、一个计数用例假绿），
// 这里不再复制那个坑 —— 装替身一律用 g2bSeams，它自带还原。
func g2bSetup(t *testing.T, clientKey string) (*WebhookService, context.Context, *gorm.DB) {
	t.Helper()
	db := newD03DispatchDB(t)
	acc := &model.IntegrationAccount{
		Platform: string(ChannelDouyin), APIKey: clientKey, APISecret: gDySecret, Status: 1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed douyin account: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })
	return svc, context.Background(), db
}

type g2bFetchFn = func(context.Context, string, string, douyinMediaRef) ([]byte, string, error)
type g2bStoreFn = func(context.Context, string, string, []byte, string, string) (string, error)

// g2bSeams 装上本用例的替身并登记还原。
func g2bSeams(t *testing.T, fetch g2bFetchFn, store g2bStoreFn) {
	t.Helper()
	prevFetch, prevStore := dyMediaFetchFn, dyMediaStoreFn
	t.Cleanup(func() { dyMediaFetchFn, dyMediaStoreFn = prevFetch, prevStore })
	dyMediaFetchFn, dyMediaStoreFn = fetch, store
}

// g2bNoSeamsMustRun 断言真 HTTP 腿一次都没被走（替身失效时这里会 nonzero）。
func g2bNoRealCall(t *testing.T, api *g2bAPI, what string) {
	t.Helper()
	token, res, dl := api.counts()
	if token+res+dl != 0 {
		t.Errorf("%s：替身没接住，真实打了抖音接口 token=%d res=%d dl=%d", what, token, res, dl)
	}
}

// g2bLocalMediaRaw 官方 user_local_image/video 报文（ID 用官方形态：@ 开头的 base64，含 + / =）。
func g2bLocalMediaRaw(msgID, sender, convID, serverMsgID, messageType string) string {
	return `{"event":"im_receive_msg","client_key":"kk","from_user_id":"` + sender +
		`","to_user_id":"bot","log_id":"lg-1","content":{"conversation_short_id":"` + convID +
		`","server_message_id":"` + serverMsgID +
		`","conversation_type":1,"create_time":1681303285997,"message_type":"` + messageType +
		`","index":"1"}}`
}

// g2bWaitHubMediaURL 轮询等异步转存回填：替身返回的那一刻 UPDATE 还没跑，直读会把竞态读成缺陷。
func g2bWaitHubMediaURL(t *testing.T, db *gorm.DB, msgID string) model.MessageHub {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var hub model.MessageHub
	var err error
	for {
		hub = model.MessageHub{}
		err = db.Where("platform = ? AND msg_id = ?", "douyin", msgID).First(&hub).Error
		if err == nil && hub.MediaURL != "" {
			return hub
		}
		if time.Now().After(deadline) {
			t.Fatalf("10s 内 media_url 未回填 msg_id=%s err=%v", msgID, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestG2B_FetchWalksOfficialTwoLegContract 逐字段核两条腿的请求形状：
// token 请求的三个必填 body 键与固定 content-type，resources 的两个必填头与三个 query。
func TestG2B_FetchWalksOfficialTwoLegContract(t *testing.T) {
	const clientKey = "tt_g2b_contract"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	data, contentType, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-open-1", ConversationID: "@conv1", MessageID: "@msg1",
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(data) != "dy-media-bytes-of-"+clientKey {
		t.Errorf("媒体字节错位，got %q", data)
	}
	if contentType != "image/jpeg" {
		t.Errorf("Content-Type 应取下载响应的头，got %q", contentType)
	}

	tokenBody, rawQuery, _, resQ, resH, _ := api.snapshot()
	var tb map[string]any
	if uerr := json.Unmarshal([]byte(tokenBody), &tb); uerr != nil {
		t.Fatalf("token 请求体不是 JSON: %v (%s)", uerr, tokenBody)
	}
	if tb["grant_type"] != "client_credential" {
		t.Errorf("grant_type 官方固定值 client_credential，got %v", tb["grant_type"])
	}
	if tb["client_key"] != clientKey || tb["client_secret"] != gDySecret {
		t.Errorf("token 请求要带 client_key+client_secret，got %v", tb)
	}
	if api.tokenSeenCT != "application/json" {
		t.Errorf("token 的 content-type 官方标固定值 application/json，got %q", api.tokenSeenCT)
	}

	// 这里刻意钉**值**而不是"非空"：官方 access-token 的示例值是 act.…（用户级 token），
	// 而 client_token 文档的示例值是 clt.…（应用级），两处都没写成硬约束（A 档原文第 8 条，
	// 见审计 §16.1）。本实现取的是 clt.…：私信发送方从未授权过本应用，拿不到它的 act. token。
	// 「非空」断言换不成这一层证据 —— 改成任何别的 token 它照样绿，所以按形态判。
	// 若将来按平台答复改成用户级 token，这条必须一起改，等于把口径钉在测试里。
	if got := resH.Get("access-token"); !strings.HasPrefix(got, "clt.test-") {
		t.Errorf("resources 的 access-token 必须是 /oauth/client_token/ 签发的应用级 token（clt. 形态），got %q", got)
	}
	if ct := resH.Get("Content-Type"); ct != "application/json" {
		t.Errorf("resources 的 content-type 官方标固定值 application/json，got %q", ct)
	}
	for k, want := range map[string]string{
		"open_id": "u-open-1", "conversation_id": "@conv1", "message_id": "@msg1",
	} {
		if got := resQ.Get(k); got != want {
			t.Errorf("query %s = %q，want %q（RawQuery=%s）", k, got, want, rawQuery)
		}
	}
}

// TestG2B_ClientTokenIsCachedAcrossBurst 官方频控「5 分钟内超过 500 次接口调用报错 10020」
// 且「重复获取会使上次的 client_token 失效（5 分钟缓冲）」⇒ 每条媒体消息各取一次 token
// 既不省钱也会互相顶号。三次取用必须只打一次 token 接口。
func TestG2B_ClientTokenIsCachedAcrossBurst(t *testing.T) {
	const clientKey = "tt_g2b_cache"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	for i := 0; i < 3; i++ {
		if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
			OpenID: "u-cache", ConversationID: "@c", MessageID: fmt.Sprintf("@m%d", i),
		}); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	token, res, dl := api.counts()
	if token != 1 {
		t.Errorf("3 次媒体取用应只生成 1 次 client_token（官方频控+顶号），实际 %d 次", token)
	}
	if res != 3 || dl != 3 {
		t.Errorf("每条消息各一次 resources+下载，got res=%d dl=%d", res, dl)
	}
}

// TestG2B_StaleTokenSurvivesExpiryMargin expires_in 用满 7200s 会卡在「官方说还有效、
// 平台侧已失效」的窗口里。缓存必须留出提前量，并且在 token 到点前主动重取。
func TestG2B_StaleTokenRefreshesOnceAndRetries(t *testing.T) {
	const clientKey = "tt_g2b_refresh"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	// 先暖一次缓存，再让 resources 首次回 28001003（模拟被别处顶号 / 缓存里的值已失效）。
	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-re", ConversationID: "@c", MessageID: "@m0",
	}); err != nil {
		t.Fatalf("warm fetch: %v", err)
	}
	api.resReplies = []string{
		`{"err_no":28001003,"err_msg":"access_token无效","log_id":"lg-bad","data":{}}`,
	}

	data, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-re", ConversationID: "@c", MessageID: "@m1",
	})
	if err != nil {
		t.Fatalf("坏 token 后应刷新重试并成功，got %v", err)
	}
	if !strings.HasPrefix(string(data), "dy-media-bytes-") {
		t.Errorf("重试后字节错位，got %q", data)
	}
	token, res, _ := api.counts()
	if token != 2 {
		t.Errorf("遇 28001003 必须重新生成一次 token（含首暖共 2 次），got %d", token)
	}
	if res != 3 {
		t.Errorf("resources 应重投 1 次（首暖 1 + 本次 2），got %d", res)
	}
}

// TestG2B_RefreshThatDoesNotHelpIsTerminalWithErrNo 上一条只覆盖"刷新就救回来"，这里补另一格：
// 强刷之后仍是 28001003。审计 §16.1 第 8 条③ 的现场判据全靠这一格的终态应答——
// 真机要拿 err_no 分清「token 类填错」和「别处顶号」，继续重投会把这一格冲销成重试噪声。
func TestG2B_RefreshThatDoesNotHelpIsTerminalWithErrNo(t *testing.T) {
	const clientKey = "tt_g2b_refresh_dead"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	bad := `{"err_no":28001003,"err_msg":"access_token无效","log_id":"lg-dead","data":{}}`
	// 只铺两格（首取 + 强刷后）。实现若多敲一次，第三格拿到的是假平台的默认成功体 ⇒ 本用例转红，
	// 所以"凭证码不进出站重投预算"是被跑出来的，不是读码读出来的。
	api.resReplies = []string{bad, bad}

	_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-dead"))
	if err == nil {
		t.Fatal("token 一直无效必须报错，不能「这轮算成功、等下轮再试」")
	}
	var aerr *douyinAPIError
	if !errors.As(err, &aerr) || aerr.ErrNo != 28001003 {
		t.Errorf("判据要靠 err_no 原样落到日志里，got %T %v", err, err)
	}
	if douyinErrRetryable(err) {
		t.Error("28001003 判成可重试 ⇒ 每次坏凭证都多吃一遍退避预算，且把判据洗成噪声")
	}

	token, res, dl := api.counts()
	if token != 2 || res != 2 {
		t.Errorf("应只强刷一次：token 2 次、resources 2 次，got token=%d res=%d", token, res)
	}
	if dl != 0 {
		t.Errorf("资源没换到就不该发起下载，got dl=%d", dl)
	}
	// 只数次数不比对值，等于没证明「刷」这件事真发生：末次必须带出**新**那一把。
	if _, _, _, _, resH, _ := api.snapshot(); resH.Get("access-token") != "clt.test-2" {
		t.Errorf("resources 末次要带强刷后的新 token，got %q", resH.Get("access-token"))
	}
}

// TestG2B_EmptyTokenResponseIsTerminalAndUncached token 腿的第三格形状：HTTP 200、
// 但 data.access_token 是空的。client_token 页的响应结构里之所以带 `error_code`/`description`
// 两个字段，就是为了这一格（credential 不匹配、以及官方明写的频控 10020）。
//
// 这一格此前全文件没铺过：假平台只会回 "clt.test-N"。少了它，删掉空值判定的后果是
// 把 "" 缓存住两小时、再拿它去敲 resources —— 现场只剩一个看不出根因的 28001003，
// 而根因（我们的 client_key/secret 不对）本来就在上一跳的响应体里。
func TestG2B_EmptyTokenResponseIsTerminalAndUncached(t *testing.T) {
	const clientKey = "tt_g2b_tok_empty"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	empty := `{"data":{"access_token":"","description":"client_key或者client_secret不正确","error_code":10002,"expires_in":0},"message":"bad request"}`
	// 两把都是空的：第二次取用若不再打 token 腿，就说明空值被缓存了（那时 token 计数会停在 1）。
	api.tokBodies = []string{empty, empty}

	for i, msgID := range []string{"@m-empty-1", "@m-empty-2"} {
		if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef(msgID)); err == nil {
			t.Fatalf("第 %d 次取用：空 access_token 必须报错", i+1)
		} else if !strings.Contains(err.Error(), "error_code=10002") {
			t.Errorf("第 %d 次取用：官方根因码必须落在错误里，got %q", i+1, err)
		} else if douyinErrRetryable(err) {
			t.Errorf("第 %d 次取用：token 腿空应答判成可重试 ⇒ 频控现场（10020）会被 200ms/600ms 连打三遍", i+1)
		}
	}
	token, res, dl := api.counts()
	if token != 2 {
		t.Errorf("空应答不得进缓存：两次取用应各打一次 token 腿，got token=%d", token)
	}
	if res != 0 || dl != 0 {
		t.Errorf("换不到 token 就不该再敲后两条腿，got res=%d dl=%d", res, dl)
	}
}

// TestG2B_TokenLegNonJSONKeepsBodySnippet 200 但响应体不是 JSON（中间层把请求换成了登录页/
// 错误页）。判终态是**保守**选择而不是官方口径：这一格没有任何原文可依，猜它是"抖一下"还是
// "配置错了"都无据 ⇒ 只要求它把 body 片段带出来，让现场分得清是谁在应答。
func TestG2B_TokenLegNonJSONKeepsBodySnippet(t *testing.T) {
	const clientKey = "tt_g2b_tok_html"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	api.tokBodies = []string{"<html><head><title>502 Bad Gateway</title></head></html>"}
	_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-html"))
	if err == nil {
		t.Fatal("200 + 非 JSON 必须报错")
	}
	if !strings.Contains(err.Error(), "client_token parse") {
		t.Errorf("应报在解析这一步（而不是含糊的 empty token），got %q", err)
	}
	if !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Errorf("响应体片段必须留在错误里，否则无法判定是谁应答的，got %q", err)
	}
	if _, res, dl := api.counts(); res != 0 || dl != 0 {
		t.Errorf("解析失败后不该继续敲腿，got res=%d dl=%d", res, dl)
	}
}

// TestG2B_OfficialIDsAreQueryEscaped 官方明写「conversation_id 包含 + = 等特殊字符，
// 传参时需要进行编码」。不编码的后果很安静：`+` 在 query 里被解成空格，
// 于是 conversation_id 变成 "@conv grp" —— 平台回 28029020 未获取到资源链接，
// 而我们看到的只是「这条消息没有媒体」。
func TestG2B_OfficialIDsAreQueryEscaped(t *testing.T) {
	const clientKey = "tt_g2b_escape"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	const conv = "@conv+AB/cd=="
	const msg = "@msg+XY/zw=="
	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-esc", ConversationID: conv, MessageID: msg,
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	_, rawQuery, _, resQ, _, _ := api.snapshot()
	if got := resQ.Get("conversation_id"); got != conv {
		t.Errorf("服务端解出的 conversation_id 走样（+ 被当空格？）：got %q want %q", got, conv)
	}
	if got := resQ.Get("message_id"); got != msg {
		t.Errorf("服务端解出的 message_id 走样：got %q want %q", got, msg)
	}
	if !strings.Contains(rawQuery, "%2B") || !strings.Contains(rawQuery, "%2F") {
		t.Errorf("特殊字符必须百分号编码后再发，got RawQuery=%q", rawQuery)
	}
}

// TestG2B_ResourceURLUnescuesAmpersand 官方「URL 中可能包含转义字符 \u0026，
// 需要将其替换为 & 才能正常访问资源」。JSON 解码本身会把 \u0026 还原成 &，
// 但响应双重编码时会剩下字面量 \u0026 ⇒ 直链带着反斜杠去请求，
// 拿到的是 404 而不是「资源不存在」。
func TestG2B_ResourceURLUnescuesAmpersand(t *testing.T) {
	const clientKey = "tt_g2b_u0026"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	// 响应里刻意写双重转义：JSON 解出来正是字面量 \u0026。
	api.resReplies = []string{`{"err_no":0,"err_msg":"","log_id":"lg","data":{"media_type":"image","url":"` +
		api.srv.URL + `/im/media?secret=abc\\u0026type=image"}}`}

	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-u0026", ConversationID: "@c", MessageID: "@m",
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	_, _, dlURI, _, _, _ := api.snapshot()
	if !strings.Contains(dlURI, "secret=abc&type=image") {
		t.Errorf("下载请求必须把 \\u0026 还原成 &，got URI=%q", dlURI)
	}
	if strings.Contains(dlURI, "u0026") {
		t.Errorf("字面量 \\u0026 被原样带进了下载请求：got URI=%q", dlURI)
	}
}

// TestG2B_NonZeroErrNoIsNotASuccess 官方所有业务错误都是 HTTP 200 + err_no≠0。
// 只看 HTTP 状态码会把「access_token无效」当成成功、再去下载一个空 URL。
func TestG2B_NonZeroErrNoIsNotASuccess(t *testing.T) {
	const clientKey = "tt_g2b_errno"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	// data 里**带一条真实可下的直链**：否则「err_no≠0 时不得下载」是空断言 ——
	// 空 URL 本来就下不了（被形状守卫拦住），把 err_no 检查整个删掉这条也不会红。
	api.resReplies = []string{`{"err_no":28029020,"err_msg":"未获取到资源链接","log_id":"lg-e","data":{"media_type":"image","url":"` +
		api.dlURL() + `"}}`}
	_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-e", ConversationID: "@c", MessageID: "@m",
	})
	if err == nil {
		t.Fatal("err_no=28029020 必须当失败（HTTP 200 不代表成功）")
	}
	if !strings.Contains(err.Error(), "28029020") {
		t.Errorf("错误里要带官方 err_no 便于报障对号，got %v", err)
	}
	if _, _, dlCalls := api.counts(); dlCalls != 0 {
		t.Errorf("err_no≠0 时不得去下载，got %d 次", dlCalls)
	}

	// 正例腿：同一条直链在 err_no=0 时**必须**真下一次，否则上面那个 0 是空转出来的。
	if _, _, err = FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-e", ConversationID: "@c", MessageID: "@m-ok",
	}); err != nil {
		t.Fatalf("默认成功应答下应取到媒体: %v", err)
	}
	if _, _, dlCalls := api.counts(); dlCalls != 1 {
		t.Errorf("计数器没被好直链推动过（dl=%d），前面的 0 次断言不成立", dlCalls)
	}
}

// g2bFastRetry 把重试间隔换成用例尺度（返回还原函数）。
// 生产是 200ms/600ms，不为瞬时错误在测试里真等近一秒。
func g2bFastRetry(t *testing.T) func() {
	t.Helper()
	prev := dyMediaRetryBackoff
	dyMediaRetryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	return func() { dyMediaRetryBackoff = prev }
}

// TestG2B_RetryableCodesRetryWithSameToken 官方对 28001005（系统繁忙）/28001006（网络调用错误）
// /28029014（资源签发失败）的处置列直接写「请重试」「重试即可」。
// 我们这条腿是入站当场的一次性动作：当成终态 = 瞬时抖动一次就永久没有媒体。
// 同时盯住「重投不得重取 token」——那三个码不是凭证问题，为此刷新 token 反而去踩
// 官方「重复获取使上次 token 失效」的顶号坑。
func TestG2B_RetryableCodesRetryWithSameToken(t *testing.T) {
	defer g2bFastRetry(t)()
	const clientKey = "tt_g2b_retry"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	for _, no := range []int64{dyErrSysBusy, dyErrNetCall, dyErrResSign} {
		api.resReplies = []string{fmt.Sprintf(
			`{"err_no":%d,"err_msg":"官方标请重试","log_id":"lg-r","data":{}}`, no)}
		data, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
			OpenID: "u-r", ConversationID: "@c", MessageID: fmt.Sprintf("@m-%d", no),
		})
		if err != nil {
			t.Fatalf("err_no=%d 官方写明重试，第一次就放弃是漏判: %v", no, err)
		}
		if !strings.HasPrefix(string(data), "dy-media-bytes-") {
			t.Errorf("err_no=%d 重试后字节错位，got %q", no, data)
		}
	}
	token, res, dl := api.counts()
	if token != 1 {
		t.Errorf("可重试码不得重取 token（顶号坑），3 次换取共生成 %d 次", token)
	}
	if res != 6 || dl != 3 {
		t.Errorf("每个可重试码应重投一次后成功，got res=%d dl=%d（want 6/3）", res, dl)
	}
}

// TestG2B_RetryBudgetIsBoundedAndTerminal 重试必须有上界：官方频控是「5 分钟 500 次」，
// 无界重试会在平台抖动时把三个接口的配额一起烧掉。到点必须把最后一个官方 err_no 报出来。
func TestG2B_RetryBudgetIsBoundedAndTerminal(t *testing.T) {
	defer g2bFastRetry(t)()
	const clientKey = "tt_g2b_budget"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	replies := make([]string, 0, len(dyMediaRetryBackoff)+1)
	for i := 0; i <= len(dyMediaRetryBackoff); i++ {
		replies = append(replies, `{"err_no":28001006,"err_msg":"网络调用错误,请重试","log_id":"lg-b","data":{}}`)
	}
	api.resReplies = replies
	_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-b", ConversationID: "@c", MessageID: "@m-b",
	})
	if err == nil {
		t.Fatal("可重试码连续失败必须最终报错（不能无限重试后假装成功）")
	}
	if !strings.Contains(err.Error(), "28001006") {
		t.Errorf("报错要带官方 err_no 便于对 log_id 报障，got %v", err)
	}
	token, res, dl := api.counts()
	if want := len(dyMediaRetryBackoff) + 1; res != want {
		t.Errorf("resources 尝试次数应等于 1+退避表长度=%d，got %d", want, res)
	}
	if token != 1 || dl != 0 {
		t.Errorf("耗尽预算既不该重取 token 也不该下载，got token=%d dl=%d", token, dl)
	}
}

// TestG2B_PermissionCodesAreTerminal 28001012（未授权该 OpenAPI）/28001015（token 与 openId
// 不匹配）官方给的是「换含对应 scope 的 token / 核对同一用户」，重试与刷新都救不了。
// 这两个码同时是「access-token 到底该填应用级还是用户级」这条未证项**唯一会露头的信号**
// （见审计 §16.1 第 8 条），所以要求它们原样进错误、一次都不重试。
func TestG2B_PermissionCodesAreTerminal(t *testing.T) {
	defer g2bFastRetry(t)()
	const clientKey = "tt_g2b_perm"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	for _, no := range []int64{dyErrNoBucket, dyErrMismatch} {
		_, res0, _ := api.counts()
		api.resReplies = []string{fmt.Sprintf(
			`{"err_no":%d,"err_msg":"权限/口径不匹配","log_id":"lg-p","data":{}}`, no)}
		_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
			OpenID: "u-p", ConversationID: "@c", MessageID: fmt.Sprintf("@m-%d", no),
		})
		if err == nil {
			t.Fatalf("err_no=%d 是权限/口径问题，不得当成功", no)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("err_no=%d", no)) {
			t.Errorf("错误里要原样带上 err_no=%d（它是 token 口径的判据），got %v", no, err)
		}
		_, res1, dl := api.counts()
		if res1-res0 != 1 {
			t.Errorf("err_no=%d 不该重试（多打了 %d 次）", no, res1-res0-1)
		}
		if dl != 0 {
			t.Errorf("err_no=%d 后不得下载，got %d 次", no, dl)
		}
	}
	if token, _, _ := api.counts(); token != 1 {
		t.Errorf("权限类错误刷新 token 无用，却重取了 %d 次（会顶掉线上 token）", token)
	}
}

// TestG2B_Gateway5xxRetriesEveryLeg 带外瞬时态（网关 5xx）与 28001005 同义：都是"平台抖了一下"。
// 只在 in-band err_no 上重投、放过 502/503，等于按通道形状区别对待同一个永久丢媒体的后果。
// 三条腿各来一次"先 5xx 后正常"，证明重投落在整动作而不是某一条腿上。
func TestG2B_Gateway5xxRetriesEveryLeg(t *testing.T) {
	defer g2bFastRetry(t)()

	t.Run("client_token 腿", func(t *testing.T) {
		const clientKey = "tt_g2b_5xx_token"
		api := newG2BAPI(t, clientKey)
		api.tokSeq = []int{http.StatusBadGateway}
		_, ctx, _ := g2bSetup(t, clientKey)
		data, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-5xx-t"))
		if err != nil {
			t.Fatalf("502 之后官方没说要放弃：整动作应重投，got %v", err)
		}
		if !strings.HasPrefix(string(data), "dy-media-bytes-") {
			t.Errorf("重投后取到的不是这条媒体的字节，got %q", data)
		}
		token, res, dl := api.counts()
		if token != 2 || res != 1 || dl != 1 {
			t.Errorf("应只在 token 腿上多重投一次，got token=%d res=%d dl=%d（want 2/1/1）", token, res, dl)
		}
	})

	t.Run("resources 腿", func(t *testing.T) {
		const clientKey = "tt_g2b_5xx_res"
		api := newG2BAPI(t, clientKey)
		api.resSeq = []int{http.StatusServiceUnavailable}
		_, ctx, _ := g2bSetup(t, clientKey)
		if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-5xx-r")); err != nil {
			t.Fatalf("503 应退避重投后成功，got %v", err)
		}
		token, res, dl := api.counts()
		if token != 1 || res != 2 || dl != 1 {
			t.Errorf("重投不得顺带重取 token，got token=%d res=%d dl=%d（want 1/2/1）", token, res, dl)
		}
	})

	t.Run("resources 腿_连接被掐", func(t *testing.T) {
		const clientKey = "tt_g2b_eof_res"
		api := newG2BAPI(t, clientKey)
		// 掐两次：一次分不清是"我们的退避重投"还是 net/http 对复用连接的内部重试
		// （后者最多兜一次，且只发生在 reused conn 上，首次拨号不会）。三次里前两次都断、
		// 第三次才成，只有整动作预算（1+2）能解释 3 次连接。
		api.resDrop = []int{1, 1}
		_, ctx, _ := g2bSetup(t, clientKey)
		if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-eof-r")); err != nil {
			t.Fatalf("连接层 EOF 与 5xx 同属瞬时态，应重投后成功，got %v", err)
		}
		token, res, dl := api.counts()
		if token != 1 || res != 3 || dl != 1 {
			t.Errorf("应用满整动作预算重投，got token=%d res=%d dl=%d（want 1/3/1）", token, res, dl)
		}
	})

	t.Run("下载腿", func(t *testing.T) {
		const clientKey = "tt_g2b_5xx_dl"
		api := newG2BAPI(t, clientKey)
		api.dlSeq = []int{http.StatusGatewayTimeout}
		_, ctx, _ := g2bSetup(t, clientKey)
		data, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-5xx-d"))
		if err != nil {
			t.Fatalf("直链 504 是瞬时态（官方 url 30 天有效，重投还在窗口内），got %v", err)
		}
		if !strings.HasPrefix(string(data), "dy-media-bytes-") {
			t.Errorf("重投后字节错位，got %q", data)
		}
		token, res, dl := api.counts()
		// res=2 是"整动作重投"的代价：下载失败后前面已成功的腿会再走一遍。
		// token 命中缓存不发 HTTP（仍为 1），resources 会再签一次直链 —— 多花一次 JSON 调用，
		// 换来的是重投带的是**新签**的 url，不用赌旧直链还在不在。
		if token != 1 || res != 2 || dl != 2 {
			t.Errorf("应只在下载腿上多重投一次，got token=%d res=%d dl=%d（want 1/2/2）", token, res, dl)
		}
	})
}

// TestG2B_Client4xxIsTerminalOnEveryLeg 4xx（除 429）是凭证/权限判定，重投救不了；
// 而 429 是官方频控（10020 那条 5 分钟窗口），退避一次可能就过 —— 两类必须分得开。
func TestG2B_Client4xxIsTerminalOnEveryLeg(t *testing.T) {
	defer g2bFastRetry(t)()

	t.Run("下载腿_403", func(t *testing.T) {
		const clientKey = "tt_g2b_4xx_dl"
		api := newG2BAPI(t, clientKey)
		api.dlSeq = []int{http.StatusForbidden, http.StatusForbidden, http.StatusForbidden}
		_, ctx, _ := g2bSetup(t, clientKey)
		_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-4xx-d"))
		if err == nil {
			t.Fatal("403 不得当成功")
		}
		if !strings.Contains(err.Error(), "403") {
			t.Errorf("错误里要带状态码便于报障，got %v", err)
		}
		if _, _, dl := api.counts(); dl != 1 {
			t.Errorf("403 后不该重投（多打了 %d 次），官方口径是「否则无法访问相关资源」", dl-1)
		}
	})

	t.Run("resources 腿_429 反过来必须重投", func(t *testing.T) {
		const clientKey = "tt_g2b_429_res"
		api := newG2BAPI(t, clientKey)
		api.resSeq = []int{http.StatusTooManyRequests}
		_, ctx, _ := g2bSetup(t, clientKey)
		if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, g2bRef("@m-429-r")); err != nil {
			t.Fatalf("429 是可退避的频控，不是拒绝服务，got %v", err)
		}
		if _, res, _ := api.counts(); res != 2 {
			t.Errorf("429 应重投一次后成功，got res=%d", res)
		}
	})
}

func g2bRef(msgID string) douyinMediaRef {
	return douyinMediaRef{OpenID: "u-5xx", ConversationID: "@c", MessageID: msgID}
}

// TestG2B_DownloadCarriesAccessTokenAndOpenID 官方：「访问 URL 时，需额外在请求 Header 中
// 携带 Access-Token, OpenID 字段，字段的值与调用本接口的 AccessToken, OpenID 相同，否则无法访问」。
func TestG2B_DownloadCarriesAccessTokenAndOpenID(t *testing.T) {
	const clientKey = "tt_g2b_dlhdr"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-hdr", ConversationID: "@c", MessageID: "@m",
	}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	_, _, _, _, resH, dlH := api.snapshot()
	// 官方：「字段的值与调用本接口的 AccessToken, OpenID 相同，否则无法访问」⇒ 两腿必须是同一个值。
	if got := dlH.Get("Access-Token"); got == "" || got != resH.Get("access-token") {
		t.Errorf("下载直链的 Access-Token 应与取资源时同一个，got %q / resources %q", got, resH.Get("access-token"))
	}
	if got := dlH.Get("OpenID"); got != "u-hdr" {
		t.Errorf("下载直链必须带 OpenID 头（与取资源时同一个）: got %q", got)
	}
}

// TestG2B_InboundUserLocalImageBackfillsMediaURL 端到端：入站一条 user_local_image，
// hub 行先落占位符，转存完成后 media_url 回填成长期 URL，官方两个 ID 留在 Extra。
func TestG2B_InboundUserLocalImageBackfillsMediaURL(t *testing.T) {
	const clientKey = "tt_g2b_e2e"
	api := newG2BAPI(t, clientKey)
	svc, ctx, db := g2bSetup(t, clientKey)

	fetched := make(chan douyinMediaRef, 4)
	g2bSeams(t,
		func(_ context.Context, key, secret string, ref douyinMediaRef) ([]byte, string, error) {
			if key == "" || secret == "" {
				return nil, "", fmt.Errorf("stub: 抖音 client 凭证没传下来")
			}
			fetched <- ref
			return []byte("img-bytes-" + ref.MessageID), "image/jpeg", nil
		},
		func(_ context.Context, channel, mediaID string, data []byte, contentType, hint string) (string, error) {
			if !strings.Contains(string(data), "img-bytes-") {
				return "", fmt.Errorf("stub: 字节不属于 %s", mediaID)
			}
			return "/files/" + channel + "/" + mediaID, nil
		})

	const sender = "u-local-1"
	const srvMsgID = "@msgLocalQ+abc=="
	raw := g2bLocalMediaRaw("e2e-1", sender, "@convLocal+1==", srvMsgID, "user_local_image")
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-e2e", &ParsedPayload{EventID: "g2b-e2e-1"}, []byte(raw))
	if err != nil || hub == nil {
		t.Fatalf("dispatch: hub=%v err=%v", hub, err)
	}

	select {
	case ref := <-fetched:
		if ref.OpenID != sender {
			t.Errorf("open_id 官方必填且是发送人，got %q", ref.OpenID)
		}
		if ref.ConversationID != "@convLocal+1==" || ref.MessageID != srvMsgID {
			t.Errorf("取资源要用官方两个 ID，got %+v", ref)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("15s 内没有起过一次抖音媒体下载")
	}

	row := g2bWaitHubMediaURL(t, db, hub.MsgID)
	if !strings.HasPrefix(row.MediaURL, "/files/douyin/") {
		t.Errorf("media_url 应是转存后的长期 URL，got %q", row.MediaURL)
	}
	if got, _ := row.Extra["server_message_id"].(string); got != srvMsgID {
		t.Errorf("回填不得抹掉官方 ID（报障要能对上）：got %v", row.Extra["server_message_id"])
	}
	g2bNoRealCall(t, api, "入站媒体腿")
}

// TestG2B_MediaKeyHasNoPathSeparators 存储键取自官方 ID：官方 ID 是 base64，里面就有 `/`。
// 直接当 filename 传下去，路径拼接会把它做成子目录（`..` 更是穿越），
// 而这条路只有转存成功时才会走到，日常用例看不出来。
func TestG2B_MediaKeyHasNoPathSeparators(t *testing.T) {
	const clientKey = "tt_g2b_key"
	svc, ctx, _ := g2bSetup(t, clientKey)

	stored := make(chan string, 4)
	g2bSeams(t,
		func(context.Context, string, string, douyinMediaRef) ([]byte, string, error) {
			return []byte("x"), "image/png", nil
		},
		func(_ context.Context, channel, mediaID string, _ []byte, _ string, _ string) (string, error) {
			stored <- mediaID
			return "/files/" + channel + "/" + mediaID, nil
		})

	raw := g2bLocalMediaRaw("k-1", "u-key", "@c/../../etc", "@msg/../pass+word==", "user_local_image")
	if _, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-key", &ParsedPayload{EventID: "g2b-key-1"}, []byte(raw)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	select {
	case id := <-stored:
		if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
			t.Errorf("存储键 %q 含路径分隔符/穿越段（官方 ID 不能直接当文件名）", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("没有发生转存，无法判定存储键形状")
	}
}

// TestG2B_OnlyUserLocalTypesFetch 官方「仅支持 …user_local_image 和 user_local_video」。
// image/emoji 这类本来就带 resource_url 直链，去敲 resources 只会拿到 28029016 不支持的消息类型。
func TestG2B_OnlyUserLocalTypesFetch(t *testing.T) {
	const clientKey = "tt_g2b_types"
	svc, ctx, _ := g2bSetup(t, clientKey)

	var mu sync.Mutex
	var evlog []string // 只追加、不清空：清空会把「正例刚发生的 store」一起吃掉
	record := func(ev string) { mu.Lock(); evlog = append(evlog, ev); mu.Unlock() }
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), evlog...)
	}
	g2bSeams(t,
		func(_ context.Context, _, _ string, ref douyinMediaRef) ([]byte, string, error) {
			record("fetch:" + ref.MessageID)
			return []byte("x"), "image/png", nil
		},
		func(_ context.Context, _, mediaID string, _ []byte, _ string, _ string) (string, error) {
			record("store:" + mediaID)
			return "", nil
		})

	waitForLen := func(n int) []string {
		deadline := time.Now().Add(10 * time.Second)
		for {
			got := snapshot()
			if len(got) >= n {
				return got
			}
			if time.Now().After(deadline) {
				t.Fatalf("10s 内只看到 %d 个事件，want %d（seen=%v）", len(got), n, got)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// 正例腿**先**跑：它证明这套替身真的能推动计数（否则下面那个 0 是空转出来的），
	// 也让转存协程先被调度一次。
	rawOK := g2bLocalMediaRaw("t-ok", "u-type", "@c-type", "@m-type-ok", "user_local_video")
	if _, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-types",
		&ParsedPayload{EventID: "g2b-types-ok"}, []byte(rawOK)); err != nil {
		t.Fatalf("dispatch positive: %v", err)
	}
	seen := waitForLen(2) // 一次换取 + 一次转存，顺序即契约
	if seen[0] != "fetch:@m-type-ok" {
		t.Errorf("应先换取再转存，got %v", seen)
	}
	if !strings.HasPrefix(seen[1], "store:") {
		t.Errorf("换取后必须落到转存，got %v", seen)
	}

	for _, mt := range []string{"image", "emoji", "video", "text", "retain_consult_card"} {
		raw := g2bLocalMediaRaw("t-"+mt, "u-type", "@c-type", "@m-type-"+mt, mt)
		if _, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-types",
			&ParsedPayload{EventID: "g2b-types-" + mt}, []byte(raw)); err != nil {
			t.Fatalf("dispatch %s: %v", mt, err)
		}
	}
	// 排空窗口：转存腿是 SafeGoDetached 起的协程，紧接着读长度会读到「还没被调度」而不是
	// 「不该发生」，那条 0 断言就成了碰运气（同文件其余 0 断言一律给 800ms）。
	time.Sleep(800 * time.Millisecond)
	if got := snapshot(); len(got) != len(seen) {
		t.Errorf("非 user_local_* 类型却起了媒体换取，got %v", got[len(seen):])
	}
}

// TestG2B_IncompleteOfficialIDsSkip open_id/conversation_id/message_id 任缺一都不会成功，
// 与其发一次注定 28001007 参数不合法的请求，不如就地跳过。
func TestG2B_IncompleteOfficialIDsSkip(t *testing.T) {
	const clientKey = "tt_g2b_incomplete"
	svc, ctx, _ := g2bSetup(t, clientKey)

	called := 0
	g2bSeams(t,
		func(context.Context, string, string, douyinMediaRef) ([]byte, string, error) {
			called++
			return nil, "", fmt.Errorf("stub: 不该被调用")
		},
		func(context.Context, string, string, []byte, string, string) (string, error) {
			called++
			return "", nil
		})

	noMsgID := `{"event":"im_receive_msg","client_key":"kk","from_user_id":"u-noid","content":{` +
		`"conversation_short_id":"@c-noid","create_time":1681303285997,"message_type":"user_local_image"}}`
	if _, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-inc", &ParsedPayload{EventID: "g2b-inc-1"}, []byte(noMsgID)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if called != 0 {
		t.Errorf("缺 server_message_id 却起了 %d 次资源换取，want 0", called)
	}
}

// TestG2B_TikTokNeverCallsDouyinEndpoint TikTok 的媒体接口没有同源的 A 档证据
// （域名、鉴权头、路径都不同，审计 §16.3 记为未证项）。拿抖音的端点去敲 TikTok 的 ID，
// 最坏情况是「静默失败 + 永久没有媒体」，所以这里要求一次都不发。
//
// 两条腿缺一不可，且是这条被变异电池打出来的（M14 拆掉渠道守卫，本文件仍 17/17 全绿）：
//   - 反证腿（正例）：同形状的**抖音**消息必须真的起 1 次下载并回填 media_url。
//     只断言"0 次"是守不住守卫的 —— 早先那版 0 是"账号查不到 ⇒ 凭证为空 ⇒ 守卫之后那道
//     if 里返回"凑出来的，守卫拆了照样绿。
//   - 负例：TikTok 侧一次都不许多起来。这里把**两家账号都配齐凭证**，还额外留一把抖音应用
//     的 key 当诱饵：守卫一拆，抖音那把凭证就会被配到 TikTok 的消息上，计数立刻从 1 变 2。
func TestG2B_TikTokNeverCallsDouyinEndpoint(t *testing.T) {
	db := newD03DispatchDB(t)
	for _, acc := range []*model.IntegrationAccount{
		{Platform: string(ChannelTiktok), APIKey: "tt_g2b_tiktok", APISecret: gDySecret, Status: 1},
		// 诱饵：douyinClientCreds 若按平台写死/回退取"任意一家"，TikTok 就会拿到这把。
		{Platform: string(ChannelDouyin), APIKey: "tt_g2b_douyin_bait", APISecret: gDySecret, Status: 1},
	} {
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("seed %s account: %v", acc.Platform, err)
		}
	}
	svc := NewWebhookService(db)
	ctx := context.Background()
	t.Cleanup(func() { svc.Stop(ctx) })

	api := newG2BAPI(t, "tt_g2b_douyin_bait")
	var mu sync.Mutex
	fetched := 0
	seenKey := ""
	g2bSeams(t,
		func(_ context.Context, clientKey, _ string, _ douyinMediaRef) ([]byte, string, error) {
			mu.Lock()
			fetched++
			seenKey = clientKey
			mu.Unlock()
			return []byte("stub-bytes"), "image/png", nil
		},
		func(context.Context, string, string, []byte, string, string) (string, error) {
			return "/files/g2b-stub.png", nil
		})
	count := func() (int, string) { mu.Lock(); defer mu.Unlock(); return fetched, seenKey }

	// 反证腿：正例必须先证明这条路真的会起下载。
	dyRaw := g2bLocalMediaRaw("g2b-pos", "u-dy", "@c-dy", "@m-dy", "user_local_image")
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-dy-acct", &ParsedPayload{EventID: "g2b-dy-pos"}, []byte(dyRaw))
	if err != nil || hub == nil {
		t.Fatalf("抖音正例必须落库: hub=%v err=%v", hub, err)
	}
	if got := g2bWaitHubMediaURL(t, db, hub.MsgID); got.MediaURL == "" {
		t.Fatalf("抖音正例 media_url 未回填")
	}
	if n, key := count(); n != 1 || key != "tt_g2b_douyin_bait" {
		t.Fatalf("抖音正例应恰好起 1 次换取（用 bait key），got n=%d key=%q ⇒ 正例立不住，下面的 0 就没有反证力", n, key)
	}

	// 负例：换成 TikTok，同形状报文一条都不许发。
	ttRaw := g2bLocalMediaRaw("g2b-neg", "u-tt", "@c-tt", "@m-tt", "user_local_image")
	if _, _, err := svc.dispatchDouyin(ctx, ChannelTiktok, "g2b-tt-acct", &ParsedPayload{EventID: "g2b-tt-neg"}, []byte(ttRaw)); err != nil {
		t.Fatalf("dispatch tiktok: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if n, _ := count(); n != 1 {
		t.Errorf("TikTok 起了抖音媒体换取（累计 %d 次，正例应为 1 次）：渠道守卫失效，且会把抖音应用的凭证配到 TikTok 的 ID 上", n)
	}
	g2bNoRealCall(t, api, "TikTok 腿")
}

// TestG2B_MissingClientKeySkips 只配了 webhook 密钥（client_secret）没配 client_key 的账号
// 取不到 token；这时不能拿信封里带的 client_key 去换 —— 那是**另一个应用**的标识，
// 用它等于用我们的 secret 去换别人应用的 token。
func TestG2B_MissingClientKeySkips(t *testing.T) {
	db := newD03DispatchDB(t)
	if err := db.Create(&model.IntegrationAccount{
		Platform: string(ChannelDouyin), APISecret: gDySecret, Status: 1,
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	svc := NewWebhookService(db)
	t.Cleanup(func() { svc.Stop(context.Background()) })

	called := 0
	g2bSeams(t,
		func(context.Context, string, string, douyinMediaRef) ([]byte, string, error) {
			called++
			return nil, "", fmt.Errorf("stub: 不该被调用")
		},
		func(context.Context, string, string, []byte, string, string) (string, error) {
			called++
			return "", nil
		})

	raw := g2bLocalMediaRaw("ck-1", "u-ck", "@c-ck", "@m-ck", "user_local_image")
	if _, _, err := svc.dispatchDouyin(context.Background(), ChannelDouyin,
		"g2b-nck", &ParsedPayload{EventID: "g2b-nck-1"}, []byte(raw)); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	time.Sleep(800 * time.Millisecond)
	if called != 0 {
		t.Errorf("账号未配 client_key 却起了 %d 次换取，want 0", called)
	}
}

// TestG2B_OutboundEchoNeverFetches 官方「仅支持在**接收消息 webhook** 返回的消息类型为
// user_local_image 和 user_local_video 时返回相关 url 资源」。im_send_msg 是我方发出的回声，
// 按回声去敲 resources 只会拿到 28029016 不支持的消息类型 —— 白烧频控（官方 500 次/5 分钟），
// 所以回声一条都不能发。
func TestG2B_OutboundEchoNeverFetches(t *testing.T) {
	const clientKey = "tt_g2b_echo"
	svc, ctx, db := g2bSetup(t, clientKey)

	called := 0
	g2bSeams(t,
		func(context.Context, string, string, douyinMediaRef) ([]byte, string, error) {
			called++
			return nil, "", fmt.Errorf("stub: 不该被调用")
		},
		func(context.Context, string, string, []byte, string, string) (string, error) {
			called++
			return "", nil
		})

	raw := `{"event":"im_send_msg","client_key":"kk","from_user_id":"bot_self","to_user_id":"@u-echo",` +
		`"log_id":"lg-echo","content":{"conversation_short_id":"@c-echo","server_message_id":"@m-echo",` +
		`"create_time":1681303285997,"message_type":"user_local_image"}}`
	hub, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "g2b-echo", &ParsedPayload{EventID: "g2b-echo-1"}, []byte(raw))
	if err != nil || hub == nil {
		t.Fatalf("dispatch: hub=%v err=%v", hub, err)
	}
	if hub.Direction != "outbound" {
		t.Fatalf("前置条件走样：回声行应是 outbound，got %q", hub.Direction)
	}
	time.Sleep(800 * time.Millisecond)
	if called != 0 {
		t.Errorf("出站回声起了 %d 次资源换取（官方只支持接收消息），want 0", called)
	}
	var row model.MessageHub
	if err := db.Where("platform = ? AND msg_id = ?", "douyin", hub.MsgID).First(&row).Error; err != nil {
		t.Fatalf("读回回声行: %v", err)
	}
	if row.MediaURL != "" {
		t.Errorf("回声行不该被回填媒体，got %q", row.MediaURL)
	}
}

// TestG2B_TokenCacheKeepsExpiryMargin 官方「有效时间 2 小时」「重复获取会使上次的失效
// （5 分钟缓冲）」。按 expires_in 用满缓存，会卡在「我们以为还有效、平台已作废」的窗口里，
// 而那窗口的表现是**静默**拿到 28001003。缓存必须留提前量；有效期本身比提前量还短时的
// 唯一安全选择是不缓存（下一腿重新取）。
func TestG2B_TokenCacheKeepsExpiryMargin(t *testing.T) {
	const clientKey = "tt_g2b_margin"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	ref := douyinMediaRef{OpenID: "u-mg", ConversationID: "@c", MessageID: "@m1"}
	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, ref); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if token, _, _ := api.counts(); token != 1 {
		t.Fatalf("首腿应恰好生成一次 token，got %d", token)
	}
	douyinTokenMu.Lock()
	entry, ok := douyinTokenCache[clientKey]
	douyinTokenMu.Unlock()
	if !ok {
		t.Fatal("取过 token 却没进缓存（每条媒体各打一次会撞官方频控 10020）")
	}
	// expires_in=7200：留出的提前量应在 4–6 分钟之间（官方 5 分钟缓冲的同量级）。
	left := time.Until(entry.expireAt)
	if left > 7200*time.Second-4*time.Minute || left < 7200*time.Second-6*time.Minute {
		t.Errorf("7200s 的 token 缓存了 %v，未按要求留出 ~5 分钟提前量", 7200*time.Second-left)
	}

	// 有效期短于提前量 ⇒ 缓存等于「知道它快死了还留着」，必须不缓存。
	const shortKey = "tt_g2b_margin_short"
	short := newG2BAPI(t, shortKey)
	short.expiresIn = 120
	for i := 0; i < 2; i++ {
		if _, _, err := FetchDouyinMessageResource(ctx, shortKey, gDySecret, douyinMediaRef{
			OpenID: "u-mg2", ConversationID: "@c", MessageID: fmt.Sprintf("@m%d", i),
		}); err != nil {
			t.Fatalf("short fetch %d: %v", i, err)
		}
	}
	if token, _, _ := short.counts(); token != 2 {
		t.Errorf("expires_in=120s 低于提前量，应每次都重新生成 token，got %d 次", token)
	}
}

// TestG2B_ExpiredTokenCodeAlsoRefreshes 官方对 28001003「无效」和 28001008「过期」的处置
// 都是「重新请求生成 access_token」。只认前一个的话：缓存里那个值被别处顶号后我们会
// 一路失败到 2 小时到点（顶号是静默的，不会告诉我们）。
func TestG2B_ExpiredTokenCodeAlsoRefreshes(t *testing.T) {
	const clientKey = "tt_g2b_expired"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-ex", ConversationID: "@c", MessageID: "@m0",
	}); err != nil {
		t.Fatalf("warm fetch: %v", err)
	}
	api.resReplies = []string{
		`{"err_no":28001008,"err_msg":"access_token过期,请刷新或重新授权","log_id":"lg-exp","data":{}}`,
	}
	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-ex", ConversationID: "@c", MessageID: "@m1",
	}); err != nil {
		t.Fatalf("遇 28001008 应刷新后重试并成功，got %v", err)
	}
	token, res, _ := api.counts()
	if token != 2 {
		t.Errorf("28001008 也必须重取 token（含首暖共 2 次），got %d", token)
	}
	if res != 3 {
		t.Errorf("28001008 后必须重投 resources（首暖 1 + 本次 2），got %d", res)
	}
}

// TestG2B_ResourceURLMustBeAbsoluteHTTP 直链来自渠道响应，随后会被我们**带着凭证**去请求。
// 相对地址、非 http 协议（file:// 等）一旦照发，就是拿我们的 token 去开本地文件或内网端口。
// 下载腿必须先判形状。
func TestG2B_ResourceURLMustBeAbsoluteHTTP(t *testing.T) {
	const clientKey = "tt_g2b_scheme"
	api := newG2BAPI(t, clientKey)
	_, ctx, _ := g2bSetup(t, clientKey)

	for i, bad := range []string{"file:///etc/passwd", "http:///no-host", "/im/media?secret=x"} {
		api.resReplies = []string{`{"err_no":0,"err_msg":"","log_id":"lg","data":{"media_type":"image","url":"` + bad + `"}}`}
		_, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
			OpenID: "u-sch", ConversationID: "@c", MessageID: fmt.Sprintf("@m%d", i),
		})
		if err == nil {
			t.Errorf("非法直链 %q 竟然下载成功", bad)
		} else if !strings.Contains(err.Error(), "douyin resource url rejected") {
			t.Errorf("非法直链 %q 的报错没说明是形状被拒，got %v", bad, err)
		}
	}
	if _, _, dlCalls := api.counts(); dlCalls != 0 {
		t.Errorf("被拒的直链仍然发起了 %d 次下载（带着凭证）", dlCalls)
	}

	// 正例腿：假平台只听得见真正发出去的 HTTP 请求，file:// 与相对地址本来就够不着它 ——
	// 上面那个 0 只有在「好直链确实会让 dl 计数 +1」成立时才是证据。
	if _, _, err := FetchDouyinMessageResource(ctx, clientKey, gDySecret, douyinMediaRef{
		OpenID: "u-sch", ConversationID: "@c", MessageID: "@m-good",
	}); err != nil {
		t.Fatalf("默认应答（合法直链）应下载成功: %v", err)
	}
	if _, _, dlCalls := api.counts(); dlCalls != 1 {
		t.Errorf("合法直链没推动下载计数器（dl=%d），前面的 0 次断言不成立", dlCalls)
	}
}
