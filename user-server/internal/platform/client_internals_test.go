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
	// 「未配置」必须是哨兵错误：调用方（轮询型端点、独立部署的降级判定）只能靠 errors.Is 分支，
	// 字符串匹配会在改文案的那一刻静默失效。
	if err := c.ensureJWTToken(); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("nil 配置应报 ErrPlatformNotConfigured, got %v", err)
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
	// 配置缺失时 doRetry 直接短路报错（不发出任何请求），且必须是哨兵错误：
	// 调用方靠 errors.Is 区分「本机没接平台」与「接了但挂了」，裸 fmt.Errorf 同文案也匹配不上。
	withPlatformConfig(t, nil)
	err := c.do("GET", "/merchant-api/ping", nil, &BaseResp{})
	if !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("nil 配置应回 ErrPlatformNotConfigured, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "平台配置未初始化") {
		t.Errorf("错误文案应保留可读原因, got %v", err)
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
	t.Chdir(dir) // 未设 MERCHANT_STATE_DIR 时，两份身份状态默认落在 CWD 下的 config/

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

	// 未设 MERCHANT_STATE_DIR 时的默认落点：钉住它，已有安装升级后不必重新注册拿密钥
	if p := merchantSecretFilePath(); p != filepath.Join("config", ".merchant_api_secret") {
		t.Errorf("secret 默认路径变了: %s", p)
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

// TestMerchantSecretFollowsStateDir 两份本机身份锚点（merchant key 与 per-merchant 签名密钥）
// 必须共用 MERCHANT_STATE_DIR 落盘。否则换工作目录启动（systemd 的 WorkingDirectory、
// 容器 WORKDIR、从别的目录跑迁移命令）时，key 找得回、secret 找不到，
// 所有平台调用会静默退回 env/全局 secret 签名而被平台判 401。
func TestMerchantSecretFollowsStateDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MERCHANT_STATE_DIR", dir)

	want := filepath.Join(dir, ".merchant_api_secret")
	if p := merchantSecretFilePath(); p != want {
		t.Fatalf("secret 路径未跟随状态目录: got %s want %s", p, want)
	}
	if err := saveMerchantSecret("state-dir-secret"); err != nil {
		t.Fatalf("saveMerchantSecret: %v", err)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("secret 未写进状态目录: %v", err)
	}
	if string(b) != "state-dir-secret" {
		t.Errorf("落盘内容=%q", string(b))
	}
	if fi, sErr := os.Stat(want); sErr != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("secret 文件权限应为 0600, got %v %v", fi.Mode(), sErr)
	}
	// 同一目录构造的客户端要读得回来（不依赖 CWD）
	if got := NewPlatformClient("mk").merchantSecret; got != "state-dir-secret" {
		t.Errorf("构造时未从状态目录读回 secret: %q", got)
	}
}

// TestLoadMerchantSecretFailsLoudOnUnreadable 状态目录里的 secret 文件读不了（被换成目录、
// 权限错、IO 故障）不是「首次安装还没密钥」：静默吞掉会让实例带着错误身份继续签名，
// 表现为平台侧成片 401，事后无从定位。ENOENT 才是可静默的正常态。
func TestLoadMerchantSecretFailsLoudOnUnreadable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MERCHANT_STATE_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".merchant_api_secret"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	c := NewPlatformClient("mk")
	if err := c.loadMerchantSecret(); err == nil {
		t.Error("secret 路径不可读时应上报错误（调用方记日志），不得静默")
	}

	empty := t.TempDir()
	t.Setenv("MERCHANT_STATE_DIR", empty)
	if err := NewPlatformClient("mk").loadMerchantSecret(); err != nil {
		t.Errorf("文件不存在属首次安装，应静默返回 nil, got %v", err)
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

	// 两个上报端点都是公开统计接口：既不签名也不带 JWT，故 nil 配置时必须自己短路报错，
	// 且报的是同一个哨兵错误——cron 每 3 分钟打一次心跳，调用方要能区分「没接平台」与「平台挂了」，
	// 否则独立部署的实例会周期性地为一条并不存在的故障刷 Error 日志。
	withPlatformConfig(t, nil)
	if err := ReportInstallDefault(&ReportInstallReq{}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("install 无配置应报 ErrPlatformNotConfigured, got %v", err)
	}
	if err := ReportHeartbeatDefault(&ReportHeartbeatReq{}); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Errorf("heartbeat 无配置应报 ErrPlatformNotConfigured, got %v", err)
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

// TestDegradeReason 分类必须只看哨兵：传输错误、平台 401、裸 error 都不是"未配置"。
// 判据若退回字符串匹配，改文案即静默错分类。
func TestDegradeReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"无错", nil, ""},
		{"裸哨兵", ErrPlatformNotConfigured, "not_configured"},
		{"包装后的哨兵", fmt.Errorf("%w: 商户上报请求未发出", ErrPlatformNotConfigured), "not_configured"},
		{"传输失败", errors.New("dial tcp 127.0.0.1:1: connect: connection refused"), "unreachable"},
		{"平台拒绝", &PlatformError{StatusCode: 401}, "unreachable"},
		{"同文案但非哨兵", errors.New("平台配置未初始化"), "unreachable"},
	} {
		if got := DegradeReason(tc.err); got != tc.want {
			t.Errorf("%s: DegradeReason=%q want %q", tc.name, got, tc.want)
		}
	}
}
