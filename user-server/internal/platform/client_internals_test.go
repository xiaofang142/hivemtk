package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/config"
)

// withPlatformConfig 替换全局平台配置并在用例结束时还原。
// 本包所有用例都必须走它：config.PlatformCfg 是进程级变量，
// 既有用例（client_test.go）直接赋值不还原，靠字母序侥幸排在后面才没串味。
func withPlatformConfig(t *testing.T, cfg *config.PlatformConfig) {
	t.Helper()
	orig := config.PlatformCfg
	config.PlatformCfg = cfg
	t.Cleanup(func() { config.PlatformCfg = orig })
}

// TestSignSecretPrecedence 签名密钥优先级：per-merchant > env > PlatformCfg.Secret > 报错。
func TestSignSecretPrecedence(t *testing.T) {
	t.Setenv("MERCHANT_API_SECRET", "env-secret")
	withPlatformConfig(t, &config.PlatformConfig{Secret: "cfg-secret"})

	c := NewPlatformClient("mk")
	c.SetMerchantSecret("per-merchant")
	sigA, tsA, err := c.sign("GET", "/x", nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	c2 := NewPlatformClient("mk")
	c2.merchantSecret = "" // 绕过构造期文件加载
	sigB, _, err := c2.sign("GET", "/x", nil)
	if err != nil {
		t.Fatalf("sign(env): %v", err)
	}
	if sigA == sigB {
		t.Error("per-merchant 密钥应与 env 密钥产生不同签名")
	}
	if tsA == "" {
		t.Error("timestamp 不应为空")
	}

	t.Setenv("MERCHANT_API_SECRET", "")
	c3 := NewPlatformClient("mk")
	c3.merchantSecret = ""
	if _, _, err := c3.sign("GET", "/x", nil); err != nil {
		t.Fatalf("env 为空时应回落 PlatformCfg.Secret: %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{Secret: ""})
	if _, _, err := c3.sign("GET", "/x", nil); err == nil || !strings.Contains(err.Error(), "MERCHANT_API_SECRET 未配置") {
		t.Errorf("三处皆空应报未配置, got %v", err)
	}

	// SetMerchantSecret("") 必须被忽略，否则注册失败路径会把已有密钥清空。
	c.SetMerchantSecret("")
	if c.merchantSecret != "per-merchant" {
		t.Errorf("空串不应覆盖已有密钥: %q", c.merchantSecret)
	}

	// sign 的签名串含 unix 秒时间戳（`method\npath\ntimestamp\nbody`）：跨秒的两次调用必然得到
	// 不同签名，直接比较会偶发红（跨秒时「未参与签名的字段」也会显示成不同）。
	// 故一律取「同一秒内的两次签名」才可比。
	sameSecond := func(a, b func() (string, string, error)) (string, string) {
		t.Helper()
		for i := 0; i < 50; i++ {
			sa, tsa, err := a()
			if err != nil {
				t.Fatalf("sign A: %v", err)
			}
			sb, tsb, err := b()
			if err != nil {
				t.Fatalf("sign B: %v", err)
			}
			if tsa == tsb {
				return sa, sb
			}
		}
		t.Fatal("重试 50 次仍未取到同一秒内的两次签名")
		return "", ""
	}
	sigOf := func(method, path string, body []byte) func() (string, string, error) {
		return func() (string, string, error) { return c.sign(method, path, body) }
	}

	// 带 query 的 path 只对 path 部分签名
	if a, b := sameSecond(sigOf("POST", "/api/a?b=1", []byte("{}")), sigOf("POST", "/api/a?b=2", []byte("{}"))); a != b {
		t.Error("query 部分不应参与签名串")
	}
	if a, b := sameSecond(sigOf("POST", "/api/a?b=1", []byte("{}")), sigOf("POST", "/api/b?b=1", []byte("{}"))); a == b {
		t.Error("不同 path 签名不应相同")
	}
	// method 与 body 都必须进签名串
	if a, b := sameSecond(sigOf("GET", "/api/a", []byte("{}")), sigOf("POST", "/api/a", []byte("{}"))); a == b {
		t.Error("不同 method 签名不应相同")
	}
	if a, b := sameSecond(sigOf("POST", "/api/a", []byte("{}")), sigOf("POST", "/api/a", []byte(`{"x":1}`))); a == b {
		t.Error("不同 body 签名不应相同")
	}
	// 同 method/path/body 必须确定性一致（串里不得混入 nonce）
	if a, b := sameSecond(sigOf("POST", "/api/a", []byte("{}")), sigOf("POST", "/api/a", []byte("{}"))); a != b {
		t.Error("同参两次签名应一致")
	}
}

func TestEnsureJWTTokenBranches(t *testing.T) {
	var loginHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		loginHits++
		switch loginHits {
		case 1: // 缺 token
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":""}}`))
		case 2: // 非法 JSON
			_, _ = w.Write([]byte(`{not json`))
		case 3: // 非 200
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`denied`))
		case 4: // 成功且带 expires
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"token": "jwt-1", "expires": time.Now().Add(time.Hour).Unix()},
			})
		default: // 成功但无 expires → 回落 1h
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt-2"}})
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")

	// 配置未初始化
	withPlatformConfig(t, nil)
	c := NewPlatformClient("mk")
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("nil 配置应报未初始化, got %v", err)
	}

	// 管理员密码未配置
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL})
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "admin_password") {
		t.Errorf("空密码应报错并提示配置项, got %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, AdminPassword: "p", Secret: "s"})
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "缺少 token") {
		t.Errorf("响应无 token 应报错, got %v", err)
	}
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "解析登录响应失败") {
		t.Errorf("非法 JSON 应报错, got %v", err)
	}
	if err := c.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台登录返回 403") {
		t.Errorf("非 200 应带状态码, got %v", err)
	}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("第 4 次应成功: %v", err)
	}
	if c.jwtToken != "jwt-1" || time.Until(c.jwtExpireAt) < 50*time.Minute {
		t.Errorf("expires 未正确解析: token=%q expire=%v", c.jwtToken, c.jwtExpireAt)
	}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("token 有效期内应复用不报错: %v", err)
	}
	if loginHits != 4 {
		t.Errorf("有效期内不应重复登录: loginHits=%d want 4", loginHits)
	}

	// 清空缓存后第 5 次登录走「响应无 expires」分支 → 回落 now+1h
	c.jwtToken = ""
	c.jwtExpireAt = time.Time{}
	if err := c.ensureJWTToken(); err != nil {
		t.Fatalf("无 expires 响应应成功并回落 1h: %v", err)
	}
	if c.jwtToken != "jwt-2" {
		t.Errorf("token=%q want jwt-2", c.jwtToken)
	}
	if d := time.Until(c.jwtExpireAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("无 expires 时应回落 1h, got %v", d)
	}

	// 服务端不可达
	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://127.0.0.1:1", AdminPassword: "p", Secret: "s"})
	c2 := NewPlatformClient("mk")
	if err := c2.ensureJWTToken(); err == nil || !strings.Contains(err.Error(), "平台登录失败") {
		t.Errorf("连接失败应包装为登录失败, got %v", err)
	}
}

func TestDoRetryPlatformPrefixUsesJWTAndNilRespDataIsOK(t *testing.T) {
	// 响应头经 channel 回传：handler 在另一个 goroutine 里写变量，直接读会构成数据竞争。
	authCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
		case "/platform/anything":
			select {
			case authCh <- r.Header.Get("Authorization"):
			default:
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/merchant-api/empty200":
			// 200 且响应体为空：走 respData != nil 的 io.ReadAll + json.Unmarshal 分支
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, AdminPassword: "p", Secret: "s"})

	c := NewPlatformClient("mk")
	if err := c.Do("GET", "/platform/anything", nil, nil); err != nil {
		t.Fatalf("Do(nil respData) 应成功: %v", err)
	}
	if got := <-authCh; got != "Bearer jwt" {
		t.Errorf("JWT 未随 /platform/ 前缀下发: %q", got)
	}
	if err := c.Do("POST", "/merchant-api/x", map[string]any{"bad": make(chan int)}, nil); err == nil ||
		!strings.Contains(err.Error(), "序列化请求数据失败") {
		t.Errorf("请求体不可序列化应报错, got %v", err)
	}

	// 204 属于「非 200」，返回结构化 *PlatformError（不是反序列化错误）
	var pe *PlatformError
	if err := c.do("GET", "/merchant-api/nobody", nil, &BaseResp{}); !errors.As(err, &pe) ||
		pe.StatusCode != http.StatusNoContent {
		t.Errorf("204 应返回 *PlatformError{StatusCode:204}, got %v", err)
	}
	// 200 + 空体 + 需要反序列化：json.Unmarshal("") 必然报错
	if err := c.do("GET", "/merchant-api/empty200", nil, &BaseResp{}); err == nil ||
		!strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("200 空响应体应报反序列化错误, got %v", err)
	}
	// 配置缺失时 doRetry 直接短路报错（不发出任何请求）
	withPlatformConfig(t, nil)
	if err := c.do("GET", "/merchant-api/ping", nil, &BaseResp{}); err == nil ||
		!strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("nil 配置应短路报错, got %v", err)
	}

	// /merchant-api/ 前缀走签名分支：X-Merchant-Key 与 X-Signature 必须都在
	sigCh := make(chan [2]string, 4)
	sigSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			select {
			case sigCh <- [2]string{r.Header.Get("X-Signature"), r.Header.Get("X-Merchant-Key")}:
			default:
			}
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer sigSrv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: sigSrv.URL, Secret: "s"})
	if err := NewPlatformClient("mk-key").do("GET", "/merchant-api/ping", nil, &BaseResp{}); err != nil {
		t.Fatalf("签名分支请求失败: %v", err)
	}
	hdr := <-sigCh
	if len(hdr[0]) != 64 {
		t.Errorf("X-Signature 应为 64 位 hex, got %q", hdr[0])
	}
	if hdr[1] != "mk-key" {
		t.Errorf("X-Merchant-Key=%q", hdr[1])
	}
}

func TestRegisterMerchantPersistsSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(dir) // loadMerchantSecret/saveMerchantSecret 用相对路径 config/.merchant_api_secret

	regBodyCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
			return
		}
		b, _ := io.ReadAll(r.Body)
		select {
		case regBodyCh <- string(b):
		default:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0, "msg": "ok", "data": map[string]any{"key": "k-1", "secret": "persisted-secret"},
		})
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "env-secret")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	// 文件不存在时构造：secret 保持空
	c := NewPlatformClient("mk")
	if c.merchantSecret != "" {
		t.Fatalf("起始应无 per-merchant secret, got %q", c.merchantSecret)
	}
	if err := c.RegisterMerchant(RegisterMerchantReq{Name: "商户A", ContactEmail: "a@b.c"}); err != nil {
		t.Fatalf("RegisterMerchant: %v", err)
	}
	if regBody := <-regBodyCh; !strings.Contains(regBody, `"name":"商户A"`) {
		t.Errorf("注册请求体异常: %s", regBody)
	}
	if c.merchantSecret != "persisted-secret" {
		t.Errorf("注册响应 secret 未生效: %q", c.merchantSecret)
	}
	secretPath := filepath.Join(dir, "config", ".merchant_api_secret")
	b, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("secret 未落盘: %v", err)
	}
	if string(b) != "persisted-secret" {
		t.Errorf("落盘内容=%q", string(b))
	}
	if fi, sErr := os.Stat(secretPath); sErr != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("secret 文件权限应为 0600, got %v %v", fi.Mode(), sErr)
	}

	c2 := NewPlatformClient("mk") // 从盘上加载
	if c2.merchantSecret != "persisted-secret" {
		t.Errorf("loadMerchantSecret 未读回: %q", c2.merchantSecret)
	}

	if p := merchantSecretFilePath(); p != filepath.Join("config", ".merchant_api_secret") {
		t.Errorf("secret 文件路径变了: %s", p)
	}
	// 注册响应 data 里无 secret 时不得覆盖已有密钥，也不得落盘
	if err := os.Remove(secretPath); err != nil {
		t.Fatalf("rm: %v", err)
	}
	c3 := NewPlatformClient("mk")
	c3.SetMerchantSecret("keep")
	withConfigNoData(t)
	if err := c3.RegisterMerchant(RegisterMerchantReq{}); err != nil {
		t.Fatalf("无 data 的注册响应应视为成功: %v", err)
	}
	if c3.merchantSecret != "keep" {
		t.Errorf("空 secret 不应覆盖已有密钥: %q", c3.merchantSecret)
	}
	if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
		t.Errorf("无 secret 时不应落盘, stat err=%v", err)
	}
	if err := saveMerchantSecret("x"); err != nil {
		t.Errorf("saveMerchantSecret 正常路径不应报错: %v", err)
	}
	if _, err := os.Stat(secretPath); err != nil {
		t.Errorf("saveMerchantSecret 未建文件: %v", err)
	}
}

// withConfigNoData 指向一个注册端点返回 code=0 但无 data 字段的平台。
func withConfigNoData(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})
}

// TestRegisterMerchantFailureKeepsSecret 注册失败返回结构化错误，且不得改写已持有的密钥。
func TestRegisterMerchantFailureKeepsSecret(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"token": "jwt"}})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"msg":"duplicate"}`))
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	c := NewPlatformClient("mk")
	c.SetMerchantSecret("keep-me")
	if err := c.RegisterMerchant(RegisterMerchantReq{}); err == nil {
		t.Fatal("400 应报错")
	} else if pe, ok := err.(*PlatformError); !ok || pe.Resp.Msg != "duplicate" {
		t.Errorf("应返回结构化错误: %T %v", err, err)
	}
	if c.merchantSecret != "keep-me" {
		t.Error("失败响应不应改写已有 secret")
	}
	if _, err := os.Stat(filepath.Join(dir, "config", ".merchant_api_secret")); !os.IsNotExist(err) {
		t.Errorf("注册失败不应落盘密钥, err=%v", err)
	}
}

func TestGetLicenseStatusParsesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/merchant-api/license/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"status": "active", "expire_at": "2027-01-02T03:04:05Z", "remaining_days": 200,
		}})
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	c := NewPlatformClient("mk")
	lic, err := c.GetLicenseStatus()
	if err != nil {
		t.Fatalf("GetLicenseStatus: %v", err)
	}
	if lic.Status != "active" || lic.Remaining != 200 || lic.ExpireAt.Year() != 2027 {
		t.Errorf("授权状态解析错: %+v", lic)
	}

	// 平台侧业务失败：非 200 → 原样返回 *PlatformError，不吞成解析错误
	brokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":403,"msg":"license expired"}`))
	}))
	defer brokenSrv.Close()
	withPlatformConfig(t, &config.PlatformConfig{APIURL: brokenSrv.URL, Secret: "s"})
	if _, err := NewPlatformClient("mk").GetLicenseStatus(); err == nil {
		t.Error("403 应返回错误")
	} else {
		var pe *PlatformError
		if !errors.As(err, &pe) || pe.Resp.Msg != "license expired" {
			t.Errorf("应透传结构化错误, got %T %v", err, err)
		}
	}
}

// TestGetLicenseStatusUnmarshalFailure 覆盖 GetLicenseStatus 内部的
// `json.Unmarshal(resp.Data, &LicenseStatusResp{})` 失败分支：请求路径固定为
// /merchant-api/license/status，无法靠改 path 触发，故用第二个 server 让该路径返回
// data:"不是对象"（合法 JSON、但类型不是对象）。
func TestGetLicenseStatusUnmarshalFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": "不是对象"})
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL, Secret: "s"})

	if _, err := NewPlatformClient("mk").GetLicenseStatus(); err == nil ||
		!strings.Contains(err.Error(), "cannot unmarshal string into Go value of type") {
		t.Errorf("data 非对象时应返回类型错误, got %v", err)
	}
}

func TestReportInstallAndHeartbeatBranches(t *testing.T) {
	installCh, heartbeatCh := make(chan string, 4), make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/install":
			b, _ := io.ReadAll(r.Body)
			installBody := string(b)
			select {
			case installCh <- installBody:
			default:
			}
			if strings.Contains(installBody, "boom-install") {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`server blew up`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		case "/api/platform/heartbeat":
			b, _ := io.ReadAll(r.Body)
			hbBody := string(b)
			select {
			case heartbeatCh <- hbBody:
			default:
			}
			if strings.Contains(hbBody, "boom-heart") {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("MERCHANT_API_SECRET", "s")

	// 两个上报端点都是公开统计接口：既不签名也不带 JWT，故 nil 配置时必须自己短路报错。
	withPlatformConfig(t, nil)
	if err := ReportInstallDefault(&ReportInstallReq{}); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("install 无配置应报错, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{}); err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("heartbeat 无配置应报错, got %v", err)
	}

	// APIURL 末尾带斜杠：拼 URL 前须 TrimRight，否则得到 //api/platform/install
	withPlatformConfig(t, &config.PlatformConfig{APIURL: srv.URL + "/", Secret: "s"})
	if err := ReportInstallDefault(&ReportInstallReq{InstallID: "ok-install", Version: "v1"}); err != nil {
		t.Fatalf("install 成功路径: %v", err)
	}
	if installBody := <-installCh; !strings.Contains(installBody, `"install_id":"ok-install"`) {
		t.Errorf("install 请求体异常: %s", installBody)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "ok-heart", HostInfo: json.RawMessage(`{"os":"darwin"}`)}); err != nil {
		t.Fatalf("heartbeat 成功路径: %v", err)
	}
	if hbBody := <-heartbeatCh; !strings.Contains(hbBody, `"os":"darwin"`) {
		t.Errorf("heartbeat 请求体异常: %s", hbBody)
	}

	if err := ReportInstallDefault(&ReportInstallReq{InstallID: "boom-install"}); err == nil ||
		!strings.Contains(err.Error(), "上报安装信息返回 500") {
		t.Errorf("install 非 200 应带状态码, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{InstallID: "boom-heart"}); err == nil ||
		!strings.Contains(err.Error(), "上报心跳返回 502") {
		t.Errorf("heartbeat 非 200 应带状态码, got %v", err)
	}

	withPlatformConfig(t, &config.PlatformConfig{APIURL: "http://127.0.0.1:1", Secret: "s"})
	if err := ReportInstallDefault(&ReportInstallReq{}); err == nil || !strings.Contains(err.Error(), "上报安装信息失败") {
		t.Errorf("install 连接失败应包装, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{}); err == nil || !strings.Contains(err.Error(), "上报心跳失败") {
		t.Errorf("heartbeat 连接失败应包装, got %v", err)
	}
}

func TestPlatformErrorFormatting(t *testing.T) {
	full := &PlatformError{StatusCode: 404, RawBody: `{"code":4040,"msg":"下架"}`, Resp: &BaseResp{Code: 4040, Msg: "下架"}}
	if got := full.Error(); !strings.Contains(got, "code=4040") || !strings.Contains(got, "msg=下架") {
		t.Errorf("Error()=%q", got)
	}
	if full.Msg() != "下架" {
		t.Errorf("Msg()=%q", full.Msg())
	}

	bare := &PlatformError{StatusCode: 500, RawBody: "plain-text-body"}
	if got := bare.Error(); !strings.Contains(got, "body=plain-text-body") {
		t.Errorf("无 Resp 时 Error()=%q", got)
	}
	if bare.Msg() != "plain-text-body" {
		t.Errorf("无 Resp 时 Msg()=%q", bare.Msg())
	}

	// Resp 存在但 Msg 为空 → 仍应回落原始 body（否则界面只能看到一句英文模板）
	blank := &PlatformError{StatusCode: 400, RawBody: `{"code":9,"msg":""}`}
	if blank.Msg() != `{"code":9,"msg":""}` {
		t.Errorf("Resp.Msg 为空时 Msg() 应回落 RawBody, got %q", blank.Msg())
	}

	empty := &PlatformError{StatusCode: 502}
	if empty.Msg() != empty.Error() {
		t.Errorf("皆空时 Msg 应回落 Error(): %q vs %q", empty.Msg(), empty.Error())
	}
	if !strings.Contains(fmt.Sprintf("%v", empty), "status=502") {
		t.Error("Error 应含状态码")
	}
}
