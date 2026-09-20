package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// withContributorGlobals 隔离本文件用到的三处进程级状态：
// merchantKey（sync.go 包级变量）、contribToken/contribExpireAt（贡献者 token 缓存）。
// 用例结束后必须还原，否则同包后续用例会被这里的登录结果污染。
func withContributorGlobals(t *testing.T, mk string) {
	t.Helper()
	oldKey := merchantKey
	oldTok, oldExp := contribToken, contribExpireAt
	t.Cleanup(func() {
		merchantKey = oldKey
		contribToken, contribExpireAt = oldTok, oldExp
	})
	merchantKey = mk
	contribToken = ""
	contribExpireAt = time.Time{}
}

// TestContributorIdentityDerivation 贡献者身份完全由 (merchantKey, platform.secret) 派生：
// 平台侧不存明文口令，商户端重装后必须得到同一身份，故派生式必须钉死。
func TestContributorIdentityDerivation(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	auth, err := contributorIdentity()
	if err != nil {
		t.Fatalf("contributorIdentity: %v", err)
	}
	if auth.username != "mtk_mk-abc" {
		t.Errorf("username=%q", auth.username)
	}
	if auth.email != "mtk_mk-abc@mtk.local" {
		t.Errorf("email=%q", auth.email)
	}
	if auth.displayName != "商户-mk-abc" {
		t.Errorf("displayName=%q", auth.displayName)
	}
	sum := sha256.Sum256([]byte("mk-abc|s3cr3t"))
	if want := hex.EncodeToString(sum[:])[:16]; auth.password != want {
		t.Errorf("password 派生式变了: %q want %q", auth.password, want)
	}

	// merchantKey 为空说明本机身份没落地。此时任何派生结果都是拿别人的号：
	// 回落 "anonymous" 会让所有坏实例共用一个平台账号，口令还能被任何知道 secret 的人复现。
	withContributorGlobals(t, "")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})
	if _, err := contributorIdentity(); err == nil || !strings.Contains(err.Error(), "merchant key 未初始化") {
		t.Errorf("空 merchantKey 应报错, got %v", err)
	}

	// 口令必须随 secret 变化，否则派生身份失去意义
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "another"})
	other, err := contributorIdentity()
	if err != nil {
		t.Fatalf("contributorIdentity(another): %v", err)
	}
	if other.password == auth.password {
		t.Error("不同 platform.secret 应派生出口令不同的身份")
	}
}

// TestContributorIdentityEmptySecret 缺 secret 时 fail-closed：报包装了 ErrPlatformNotConfigured
// 的 error，只有 CONTRIBUTOR_DEV=1 才允许占位口令。
func TestContributorIdentityEmptySecret(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	t.Setenv("CONTRIBUTOR_DEV", "")

	for _, tc := range []struct {
		name string
		cfg  *config.PlatformConfig
		want string
	}{
		// 平台配置整体缺失时必须报可读错误，而不是对 nil 指针取字段
		{"配置未加载", nil, "请先加载平台配置"},
		{"secret 为空", &config.PlatformConfig{Secret: ""}, "platform.secret 为空"},
	} {
		withPlatformConfig(t, tc.cfg)
		_, err := contributorIdentity()
		if !errors.Is(err, ErrPlatformNotConfigured) {
			t.Errorf("%s: err=%v，应包装 ErrPlatformNotConfigured", tc.name, err)
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: 错误信息应含 %q, got %v", tc.name, tc.want, err)
		}
	}

	t.Setenv("CONTRIBUTOR_DEV", "1")
	auth, err := contributorIdentity() // 不应 panic，也不应报错
	if err != nil {
		t.Fatalf("CONTRIBUTOR_DEV=1 时应允许占位派生: %v", err)
	}
	sum := sha256.Sum256([]byte("mk-abc|" + devPlaceholderSecret))
	if got := hex.EncodeToString(sum[:])[:16]; auth.password != got {
		t.Errorf("CONTRIBUTOR_DEV 占位口令派生错: %q want %q", auth.password, got)
	}
}

// TestEnsureContributorTokenFailsLoudly 派生失败必须止于 error：这条链路的调用方是资产上传的
// 请求协程（CreateAsset/SubmitAudit → ensureContributorToken），panic 会直接把请求打挂。
func TestEnsureContributorTokenFailsLoudly(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, nil)

	cc := &ContributorClient{baseURL: "http://127.0.0.1:1", httpClient: NewPlatformClient("k").httpClient}
	var tok string
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("取 token 链路 panic(%v)，应返回 error", r)
			}
		}()
		tok, err = ensureContributorToken(cc)
	}()
	if !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("err=%v，应包装 ErrPlatformNotConfigured", err)
	}
	if tok != "" {
		t.Errorf("失败时不应返回 token: %q", tok)
	}
	if contribToken != "" {
		t.Errorf("失败后不应写入 token 缓存: %q", contribToken)
	}
}

func TestNewContributorClientReadsConfig(t *testing.T) {
	withPlatformConfig(t, nil)
	if got := NewContributorClient(); got.baseURL != "" {
		t.Errorf("nil 配置应得到空 baseURL, got %q", got.baseURL)
	}
	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://127.0.0.1:8899"})
	if got := NewContributorClient(); got.baseURL != "http://127.0.0.1:8899" {
		t.Errorf("baseURL=%q", got.baseURL)
	}
}

// loginEnvelope 按平台侧真实信封回登录结果：token 嵌在 data.token。
func loginEnvelope(w io.Writer, token string) {
	_, _ = fmt.Fprintf(w, `{"code":200,"msg":"登录成功","data":{"token":%q}}`, token)
}

// TestEnsureContributorTokenPaths 覆盖：登录即中 / 缓存命中 / 缓存过期。
// 三条路径共用一个 httptest 实例，因此 logins 计数就是「有没有白白重登」的证据。
func TestEnsureContributorTokenPaths(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	var logins, registers int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/auth/login":
			logins++
			loginEnvelope(w, fmt.Sprintf("ct-%d", logins))
		case "/contributor-api/v1/auth/register":
			registers++
			_, _ = w.Write([]byte(`{"code":500,"msg":"注册未开放"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cc := &ContributorClient{baseURL: srv.URL, httpClient: srv.Client()}

	// 1) 登录即中：不应触发注册
	tok, err := ensureContributorToken(cc)
	if err != nil {
		t.Fatalf("ensureContributorToken: %v", err)
	}
	if tok != "ct-1" || logins != 1 || registers != 0 {
		t.Fatalf("首登路径异常: tok=%q logins=%d registers=%d", tok, logins, registers)
	}

	// 2) 缓存命中：再次调用不得再打服务端
	if tok2, err := ensureContributorToken(cc); err != nil || tok2 != tok {
		t.Fatalf("缓存应直接复用: tok=%q err=%v", tok2, err)
	}
	if logins != 1 {
		t.Errorf("有效期内重复登录: logins=%d want 1", logins)
	}

	// 3) 缓存过期 → 再登一次
	contribExpireAt = time.Now().Add(-time.Hour)
	if _, err := ensureContributorToken(cc); err != nil {
		t.Fatalf("过期后重新登录失败: %v", err)
	}
	if logins != 2 {
		t.Errorf("过期后应重新登录: logins=%d want 2", logins)
	}
	if registers != 0 {
		t.Errorf("重登成功时不应注册: registers=%d", registers)
	}
}

// TestEnsureContributorTokenProvisioning 首次使用本身份时的两条开户兜底路径：
// 登录失败 → 注册即签发 token；注册也被拒（同号已由别处建好）→ 再登一次。
func TestEnsureContributorTokenProvisioning(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	for _, tc := range []struct {
		name string
		// 首登是否失败；注册是否失败
		loginFails, registerFails bool
		wantTok                   string
	}{
		{"注册签发", true, false, "ct-from-register"},
		{"注册被拒后重登", true, true, "ct-on-retry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withContributorGlobals(t, "mk-abc")
			var logins, registers int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/contributor-api/v1/auth/login":
					logins++
					if logins == 1 && tc.loginFails {
						w.WriteHeader(http.StatusUnauthorized) // 账号还不存在：平台 JWT 层给真 401
						return
					}
					loginEnvelope(w, "ct-on-retry")
				case "/contributor-api/v1/auth/register":
					registers++
					if tc.registerFails {
						_, _ = w.Write([]byte(`{"code":400,"msg":"用户名已存在"}`))
						return
					}
					_, _ = w.Write([]byte(`{"code":200,"msg":"注册成功","data":{"token":"ct-from-register"}}`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			tok, err := ensureContributorToken(&ContributorClient{baseURL: srv.URL, httpClient: srv.Client()})
			if err != nil {
				t.Fatalf("开户链路应成功: %v", err)
			}
			if tok != tc.wantTok {
				t.Errorf("tok=%q want %q", tok, tc.wantTok)
			}
			if registers != 1 {
				t.Errorf("registers=%d want 1", registers)
			}
			if contribToken != tok {
				t.Errorf("开户成功应写入缓存: %q", contribToken)
			}
		})
	}
}

// TestEnsureContributorTokenTotalFailure 两条腿都失败时返回聚合错误（登录/注册原因都要留痕），
// 且不得留下半截缓存。
func TestEnsureContributorTokenTotalFailure(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contributor-api/v1/auth/register" {
			_, _ = w.Write([]byte(`{"code":400,"msg":"用户名已存在"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ensureContributorToken(&ContributorClient{baseURL: srv.URL, httpClient: srv.Client()})
	if err == nil {
		t.Fatal("全失败应报错")
	}
	for _, want := range []string{"获取平台贡献者 token 失败", "登录=", "注册=", "用户名已存在"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("聚合错误应含 %q, got %v", want, err)
		}
	}
	if contribToken != "" {
		t.Errorf("失败后不应写入 token 缓存: %q", contribToken)
	}
}

func TestContributorCreateAssetAndSubmitAudit(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	type capture struct{ auth, path, body string }
	businessCh := make(chan capture, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contributor-api/v1/auth/login" {
			loginEnvelope(w, "ct")
			return
		}
		b, _ := io.ReadAll(r.Body)
		select {
		case businessCh <- capture{auth: r.Header.Get("Authorization"), path: r.URL.Path, body: string(b)}:
		default:
		}
		switch r.URL.Path {
		case "/contributor-api/v1/assets":
			_, _ = w.Write([]byte(`{"code":200,"msg":"创建成功","data":{"id":77,"name":"n1"}}`))
		default: // submit 等：成功信封，data 为空
			_, _ = w.Write([]byte(`{"code":200,"msg":"提交成功"}`))
		}
	}))
	defer srv.Close()

	// baseURL 末尾带斜杠：doAuth 必须 TrimRight 后再拼，否则出现 //contributor-api
	cc := &ContributorClient{baseURL: srv.URL + "/", httpClient: srv.Client()}
	id, err := cc.CreateAsset(CreateAssetPayload{AssetType: "prompt", Name: "n1"})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if id != 77 {
		t.Errorf("assetID=%d want 77", id)
	}
	if got := <-businessCh; got.auth != "Bearer ct" || !strings.Contains(got.body, `"asset_type":"prompt"`) {
		t.Errorf("创建资产请求异常: %+v", got)
	}
	if err := cc.SubmitAudit(77); err != nil {
		t.Fatalf("SubmitAudit: %v", err)
	}
	if got := <-businessCh; got.path != "/contributor-api/v1/assets/77/submit" {
		t.Errorf("submit 路径=%q", got.path)
	}

	// 平台返回成功信封但 data.id 为 0：必须报错，不能把「0 号资产」当成功交回调用方
	zeroSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contributor-api/v1/auth/login" {
			loginEnvelope(w, "ct")
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"创建成功","data":{"id":0}}`))
	}))
	defer zeroSrv.Close()
	contribToken = ""
	if _, err := (&ContributorClient{baseURL: zeroSrv.URL, httpClient: zeroSrv.Client()}).
		CreateAsset(CreateAssetPayload{}); err == nil || !strings.Contains(err.Error(), "平台创建资产返回空 ID") {
		t.Errorf("空 ID 应报错, got %v", err)
	}

	// 取不到 token 时应在发起业务请求前失败
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()
	contribToken = ""
	ccBad := &ContributorClient{baseURL: badSrv.URL, httpClient: badSrv.Client()}
	if _, err := ccBad.CreateAsset(CreateAssetPayload{}); err == nil ||
		!strings.Contains(err.Error(), "获取平台贡献者 token 失败") {
		t.Errorf("token 获取失败应前置报错, got %v", err)
	}
	if err := ccBad.SubmitAudit(1); err == nil || !strings.Contains(err.Error(), "获取平台贡献者 token 失败") {
		t.Errorf("SubmitAudit token 失败应前置报错, got %v", err)
	}
}

// readAuth 带超时地取一条 Authorization。
// 直接 <-ch 在用例本就没发第二次请求时会挂到 go test 默认 10min 超时，变异电池会被拖成假红/假挂。
func readAuth(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("等待平台侧收到的请求头超时")
		return ""
	}
}

// TestContributorTokenSelfHealOn401 平台轮换 JWT 密钥后旧 token 会被中间件打成真 401。
// 客户端缓存 24h，若不清缓存重登，本实例的提交链路会一直坏到缓存自然过期。
func TestContributorTokenSelfHealOn401(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	authCh := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contributor-api/v1/auth/login" {
			loginEnvelope(w, "ct-fresh")
			return
		}
		auth := r.Header.Get("Authorization")
		authCh <- auth
		if auth != "Bearer ct-fresh" { // 缓存里的 stale token
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"提交成功"}`))
	}))
	defer srv.Close()

	contribToken, contribExpireAt = "ct-stale", time.Now().Add(time.Hour) // 已缓存但平台侧已失效
	if err := (&ContributorClient{baseURL: srv.URL, httpClient: srv.Client()}).SubmitAudit(77); err != nil {
		t.Fatalf("401 后应自愈重登并成功: %v", err)
	}
	first, second := readAuth(t, authCh), readAuth(t, authCh)
	if first != "Bearer ct-stale" || second != "Bearer ct-fresh" {
		t.Errorf("应先带旧 token 再带新 token 重试: %q, %q", first, second)
	}
	if contribToken != "ct-fresh" {
		t.Errorf("自愈后缓存应换成新 token: %q", contribToken)
	}
}

// TestContributorDoAuthBranches 覆盖 doAuth 的分支：空体 / code 非 200 / 真 401 / 非法 JSON / 传输失败。
func TestContributorDoAuthBranches(t *testing.T) {
	authCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/empty200":
			select {
			case authCh <- r.Header.Get("Authorization"):
			default:
			}
			// 200 + 空体：不得对空字节流调用 Unmarshal，应视为成功信封
		case "/contributor-api/v1/rejected":
			_, _ = w.Write([]byte(`{"code":403,"msg":"无权限操作该资产"}`))
		case "/contributor-api/v1/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
		case "/contributor-api/v1/boom":
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(`nope`))
		case "/contributor-api/v1/badjson":
			_, _ = w.Write([]byte(`{oops`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cc := &ContributorClient{baseURL: srv.URL, httpClient: srv.Client()}

	env, err := cc.doAuth("GET", "/contributor-api/v1/empty200", nil, "tok-1")
	if err != nil {
		t.Fatalf("200 空体应成功: %v", err)
	}
	if !env.succeeded() {
		t.Error("200 空体应视为成功信封")
	}
	if got := <-authCh; got != "Bearer tok-1" {
		t.Errorf("token 非空时应设置 Authorization, got %q", got)
	}
	// 成功但无 data：取值必须失败，而不是解出零值骗过调用方
	var out struct {
		ID int64 `json:"id"`
	}
	if err := env.decodeData(&out); err == nil || !strings.Contains(err.Error(), "缺少 data") {
		t.Errorf("空体解码应报错, got %v", err)
	}

	// 无 body 且空 token：不设 Authorization 也能通
	if _, err := cc.doAuth("GET", "/contributor-api/v1/empty200", nil, ""); err != nil {
		t.Errorf("空 token 应成功: %v", err)
	}
	if got := <-authCh; got != "" {
		t.Errorf("token 为空时不应设置 Authorization, got %q", got)
	}

	// HTTP 200 + code 403：平台的拒绝必须回错（这是「假绿灯」的根因）
	env, err = cc.doAuth("POST", "/contributor-api/v1/rejected", []byte(`{}`), "tok")
	if err == nil || !strings.Contains(err.Error(), "平台贡献者接口拒绝(code=403): 无权限操作该资产") {
		t.Errorf("code 非 200 应报错并带出 msg, got %v", err)
	}
	if env == nil || env.succeeded() {
		t.Errorf("拒绝时仍应把信封交回调用方供判定, got %+v", env)
	}

	if _, err := cc.doAuth("GET", "/contributor-api/v1/unauthorized", nil, "tok"); !errors.Is(err, ErrContributorUnauthorized) {
		t.Errorf("真 401 应回 ErrContributorUnauthorized, got %v", err)
	}

	if _, err := cc.doAuth("POST", "/contributor-api/v1/boom", []byte(`{}`), ""); err == nil ||
		!strings.Contains(err.Error(), "平台贡献者接口返回 418: nope") {
		t.Errorf("非 200 应带状态码与响应体, got %v", err)
	}
	if _, err := cc.doAuth("POST", "/contributor-api/v1/badjson", nil, ""); err == nil ||
		!strings.Contains(err.Error(), "解析平台贡献者响应失败") {
		t.Errorf("非法 JSON 应报解析错误, got %v", err)
	}

	// 端口不可达：包装成传输层错误
	ccDead := &ContributorClient{baseURL: "http://127.0.0.1:1", httpClient: NewPlatformClient("k").httpClient}
	if _, err := ccDead.doAuth("GET", "/contributor-api/v1/x", nil, ""); err == nil ||
		!strings.Contains(err.Error(), "调用平台贡献者接口失败") {
		t.Errorf("连接失败应包装, got %v", err)
	}
}

// TestContributorClientSpeaksPlatformEnvelope 商户端必须按平台侧的真实信封说话。
//
// 契约取证自平台实现（hivemtk-platform/platform-server）：
//   - internal/utils/response/response.go:17-23 成功回 {code:200,msg,data}，
//     业务失败同样回 HTTP 200、真值写在 code 里（Error 也是 c.JSON(200,...)）；
//   - internal/controller/asset_market_controller.go:411-437,582-590 登录 token 在 data.token、
//     创建资产在 data.id、提交审核失败回 {code:400,msg}。
//
// 只看 HTTP 状态码 / 只读顶层字段的两处后果：
//   - 永远取不到 token，开发者资产提交链路 100% 走不通；
//   - 平台明确拒绝（code 400）被当成「提交成功」，审核环节假绿灯。
func TestContributorClientSpeaksPlatformEnvelope(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	envelopes := map[string]string{
		"/contributor-api/v1/auth/login":       `{"code":200,"msg":"登录成功","data":{"token":"ct-env","contributor":{"id":5}}}`,
		"/contributor-api/v1/assets":           `{"code":200,"msg":"创建成功","data":{"id":77,"name":"n1"}}`,
		"/contributor-api/v1/assets/77/submit": `{"code":400,"msg":"资产状态不允许提交审核"}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := envelopes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cc := &ContributorClient{baseURL: srv.URL, httpClient: srv.Client()}
	id, err := cc.CreateAsset(CreateAssetPayload{AssetType: "prompt", Name: "n1"})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if id != 77 {
		t.Errorf("assetID=%d want 77（id 在 data 里，顶层没有）", id)
	}
	if contribToken != "ct-env" {
		t.Errorf("缓存 token=%q want ct-env（token 在 data.token）", contribToken)
	}
	if err := cc.SubmitAudit(77); err == nil || !strings.Contains(err.Error(), "资产状态不允许提交审核") {
		t.Errorf("HTTP 200 + code 400 必须回错, got %v", err)
	}
}
