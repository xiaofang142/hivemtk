package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// TestEnsureContributorTokenPaths 覆盖：登录即中 / 缓存命中 / 缓存过期 / 登录失败→注册→再登录。
func TestEnsureContributorTokenPaths(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	var logins, registers int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/auth/login":
			logins++
			_ = json.NewEncoder(w).Encode(map[string]any{"token": fmt.Sprintf("ct-%d", logins)})
		case "/contributor-api/v1/auth/register":
			registers++
			_, _ = w.Write([]byte(`{}`))
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

	// 4) 登录失败 → 自动注册 → 再登录成功
	contribToken = ""
	var firstLoginFailed bool
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/auth/login":
			if !firstLoginFailed {
				firstLoginFailed = true
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ct-after-register"})
		case "/contributor-api/v1/auth/register":
			registers++
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv2.Close()

	tok2, err := ensureContributorToken(&ContributorClient{baseURL: srv2.URL, httpClient: srv2.Client()})
	if err != nil {
		t.Fatalf("注册后重登应成功: %v", err)
	}
	if tok2 != "ct-after-register" {
		t.Errorf("tok=%q", tok2)
	}
	if registers == 0 {
		t.Error("首登失败后应触发自动注册")
	}
}

// TestEnsureContributorTokenTotalFailure 两条腿都失败时返回聚合错误，且不得留下半截缓存。
func TestEnsureContributorTokenTotalFailure(t *testing.T) {
	withContributorGlobals(t, "mk-abc")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "s3cr3t"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := ensureContributorToken(&ContributorClient{baseURL: srv.URL, httpClient: srv.Client()}); err == nil ||
		!strings.Contains(err.Error(), "获取平台贡献者 token 失败") {
		t.Errorf("全失败应报聚合错误, got %v", err)
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
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ct"})
			return
		}
		b, _ := io.ReadAll(r.Body)
		select {
		case businessCh <- capture{auth: r.Header.Get("Authorization"), path: r.URL.Path, body: string(b)}:
		default:
		}
		switch r.URL.Path {
		case "/contributor-api/v1/assets":
			_, _ = w.Write([]byte(`{"id":77}`))
		default: // submit 等：200 空体即可
			_, _ = w.Write([]byte(`{}`))
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

	// 平台返回 200 但 id 为 0：必须报错，不能把「0 号资产」当成功交回调用方
	zeroSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/contributor-api/v1/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ct"})
			return
		}
		_, _ = w.Write([]byte(`{"id":0}`))
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

// TestContributorDoAuthBranches 覆盖 doAuth 的空体 / 非 200 / 非法 JSON / 传输失败四类分支。
func TestContributorDoAuthBranches(t *testing.T) {
	authCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/empty200":
			select {
			case authCh <- r.Header.Get("Authorization"):
			default:
			}
			// 200 + 空体 + out != nil：不得对空字节流调用 Unmarshal
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

	var out map[string]any
	if err := cc.doAuth("GET", "/contributor-api/v1/empty200", nil, &out, "tok-1"); err != nil {
		t.Fatalf("200 空体应成功: %v", err)
	}
	if got := <-authCh; got != "Bearer tok-1" {
		t.Errorf("token 非空时应设置 Authorization, got %q", got)
	}
	if err := cc.doAuth("GET", "/contributor-api/v1/empty200", nil, nil, ""); err != nil {
		t.Errorf("无 out 且空 token 应成功: %v", err)
	}
	if err := cc.doAuth("POST", "/contributor-api/v1/boom", []byte(`{}`), nil, ""); err == nil ||
		!strings.Contains(err.Error(), "平台贡献者接口返回 418: nope") {
		t.Errorf("非 200 应带状态码与响应体, got %v", err)
	}
	if err := cc.doAuth("POST", "/contributor-api/v1/badjson", nil, &out, ""); err == nil ||
		!strings.Contains(err.Error(), "invalid character") {
		t.Errorf("非法 JSON 应报反序列化错误, got %v", err)
	}

	// 端口不可达：包装成传输层错误
	ccDead := &ContributorClient{baseURL: "http://127.0.0.1:1", httpClient: NewPlatformClient("k").httpClient}
	if err := ccDead.doAuth("GET", "/contributor-api/v1/x", nil, nil, ""); err == nil ||
		!strings.Contains(err.Error(), "调用平台贡献者接口失败") {
		t.Errorf("连接失败应包装, got %v", err)
	}
}
