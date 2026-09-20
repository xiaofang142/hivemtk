package platform

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrPlatformNotConfigured 平台客户端未配置哨兵错误（轮询型端点静默降级判定用）
var ErrPlatformNotConfigured = errors.New("平台配置未初始化")

// DegradeReason 把降级原因分类给调用方：
// 「本机根本没接平台」(not_configured) 是私域独立部署的常态，「接了但挂了」(unreachable) 才是故障。
// 二者都只有一个 error 时，消费方要么把常态刷成 Error 日志，要么把故障静默掉，只能二选一错。
func DegradeReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrPlatformNotConfigured):
		return "not_configured"
	default:
		return "unreachable"
	}
}

type Client struct {
	merchantSecret string
	merchantKey    string
	httpClient     *http.Client
	jwtToken       string
	jwtExpireAt    time.Time
	jwtMu          sync.Mutex
}

func NewPlatformClient(merchantKey string) *Client {
	c := &Client{
		merchantKey: merchantKey,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}

	if err := c.loadMerchantSecret(); err != nil {
		logger.Warn(err.Error())
	}
	return c
}

// SetMerchantSecret 注入注册响应下发的每商户独立 HMAC 密钥（契约 §1 落地）。
// 非空时优先于全局 MERCHANT_API_SECRET / PlatformCfg.Secret 参与签名。
func (c *Client) SetMerchantSecret(secret string) {
	if secret != "" {
		c.merchantSecret = secret
	}
}

func (c *Client) sign(method, path string, body []byte) (string, string, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	pathNoQuery := path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		pathNoQuery = path[:i]
	}
	payload := method + "\n" + pathNoQuery + "\n" + timestamp + "\n" + string(body)

	secret := c.merchantSecret
	if secret == "" {
		secret = os.Getenv("MERCHANT_API_SECRET")
	}
	if secret == "" && config.PlatformCfg != nil {
		secret = config.PlatformCfg.Secret
	}
	if secret == "" {
		return "", "", fmt.Errorf("MERCHANT_API_SECRET 未配置: 请设置环境变量或 PlatformCfg.Secret")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return sig, timestamp, nil
}

// Do 公开的 HTTP 请求方法，供 controller 层代理调用平台 API 使用。
// 失败时返回 *PlatformError，调用方可按状态码/业务 code 结构化分支处理。
func (c *Client) Do(method, path string, reqData, respData any) error {
	return c.doRetry(method, path, reqData, respData, false)
}

func (c *Client) ensureJWTToken() error {
	c.jwtMu.Lock()
	defer c.jwtMu.Unlock()

	if c.jwtToken != "" && time.Now().Before(c.jwtExpireAt.Add(-60*time.Second)) {
		return nil
	}

	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 无法获取平台 JWT", ErrPlatformNotConfigured)
	}

	username := config.PlatformCfg.AdminUsername
	password := config.PlatformCfg.AdminPassword
	if username == "" {
		username = "admin"
	}
	if password == "" {
		return fmt.Errorf("平台管理员密码未配置，请设置 config/platform.yaml 中的 admin_password")
	}

	loginBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	loginURL := config.PlatformCfg.APIURL + "/api/auth/login"
	req, _ := http.NewRequest("POST", loginURL, bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("平台登录失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("平台登录返回 %d (读取响应体失败: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("平台登录返回 %d: %s", resp.StatusCode, string(body))
	}

	var loginResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Token   string `json:"token"`
			Expires int64  `json:"expires"`
		} `json:"data"`
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取登录响应失败: %w", err)
	}
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return fmt.Errorf("解析登录响应失败: %w", err)
	}
	if loginResp.Data.Token == "" {
		return fmt.Errorf("平台登录响应缺少 token")
	}

	c.jwtToken = loginResp.Data.Token
	expireAt := loginResp.Data.Expires
	if expireAt > 0 {
		c.jwtExpireAt = time.Unix(expireAt, 0)
	} else {
		c.jwtExpireAt = time.Now().Add(time.Hour)
	}
	logger.Info(fmt.Sprintf("平台 JWT token 获取成功，过期时间: %s", c.jwtExpireAt.Format("2006-01-02 15:04:05")))
	return nil
}

func (c *Client) do(method, path string, reqData, respData any) error {
	return c.doRetry(method, path, reqData, respData, false)
}

func (c *Client) doRetry(method, path string, reqData, respData any, retried bool) error {
	if config.PlatformCfg == nil {
		err := fmt.Errorf("%w: 商户上报请求未发出", ErrPlatformNotConfigured)
		logger.Error(err, "商户上报请求失败")
		return err
	}
	url := config.PlatformCfg.APIURL + path
	var body []byte
	if reqData != nil {
		var err error
		body, err = json.Marshal(reqData)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %w", err)
		}
	}

	reqLog := fmt.Sprintf("商户上报请求: %s %s", method, url)
	if len(body) > 0 {
		reqLog += fmt.Sprintf(" 请求数据: %s", string(body))
	}
	logger.Info(reqLog)

	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if strings.HasPrefix(path, "/platform/") {
		if err := c.ensureJWTToken(); err != nil {
			logger.Error(err, "获取平台 JWT token 失败")
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	} else {
		sig, timestamp, err := c.sign(method, path, body)
		if err != nil {
			logger.Error(err, "商户签名失败")
			return err
		}
		req.Header.Set("X-Merchant-Key", c.merchantKey)
		req.Header.Set("X-Timestamp", timestamp)
		req.Header.Set("X-Signature", sig)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		logger.Error(err, fmt.Sprintf("商户上报请求失败: %s %s, 耗时: %v", method, url, duration))
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized && !retried {
		c.jwtMu.Lock()
		c.jwtToken = ""
		c.jwtExpireAt = time.Time{}
		c.jwtMu.Unlock()
		logger.Debugf("[platform-client] JWT refreshed path=%s", path)
		logger.Warn(fmt.Sprintf("平台 JWT 失效(401)，清空缓存并重试一次: %s %s", method, url))
		return c.doRetry(method, path, reqData, respData, true)
	}

	if resp.StatusCode != http.StatusOK {
		rawBody, readErr := io.ReadAll(resp.Body)
		bodyStr := ""
		if readErr == nil {
			bodyStr = string(rawBody)
		}
		var baseResp BaseResp
		if bodyStr != "" {
			_ = json.Unmarshal([]byte(bodyStr), &baseResp)
		}
		logger.Error(fmt.Errorf("平台接口返回 %d", resp.StatusCode),
			fmt.Sprintf("商户上报请求失败: %s %s, 状态码: %d, 耗时: %v, 响应: %s", method, url, resp.StatusCode, duration, bodyStr))
		return &PlatformError{StatusCode: resp.StatusCode, RawBody: bodyStr, Resp: &baseResp}
	}

	var respBody []byte
	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err, "读取响应体失败")
		return err
	}
	if perr := envelopeRefusal(respBody); perr != nil {
		logger.Error(fmt.Errorf("平台业务码 %d", perr.Resp.Code),
			fmt.Sprintf("商户上报被拒: %s %s, 耗时: %v, 响应: %s", method, url, duration, perr.RawBody))
		return perr
	}
	if respData != nil {
		logger.Info(fmt.Sprintf("商户上报请求成功: %s %s, 状态码: %d, 耗时: %v, 响应数据: %s", method, url, resp.StatusCode, duration, string(respBody)))
		return json.Unmarshal(respBody, respData)
	}
	return nil
}

// envelopeRefusal 认出"平台用 HTTP 200 承载的拒绝"。
//
// 平台侧 response.Success 与 response.Error 都写 HTTP 200，真值只在信封 code 里 ——
// 连 MerchantAuth 的 401/403、注册的 400/409 也一样。所以只看状态码会把
// "该邮箱已被注册""签名错误"读成成功，调用方连失败原因都拿不到。
//
// 两条边界：body 不是信封（非对象 / 无 code 键）时不凭空造失败，原样交回调用方解析；
// code=0 视为无业务码（既有口径，资产市场客户端同此），只有显式的非 200 码才算拒绝。
func envelopeRefusal(body []byte) *PlatformError {
	var probe struct {
		Code *int   `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Code == nil {
		return nil
	}
	switch *probe.Code {
	case 0, http.StatusOK:
		return nil
	}
	return &PlatformError{
		StatusCode: http.StatusOK,
		RawBody:    string(body),
		Resp:       &BaseResp{Code: *probe.Code, Msg: probe.Msg},
	}
}

func (c *Client) RegisterMerchant(req RegisterMerchantReq) error {
	logger.Info(fmt.Sprintf("开始商户注册，请求数据: %+v", req))
	var resp BaseResp
	err := c.do("POST", "/merchant-api/merchant/register", req, &resp)
	if err != nil {
		logger.Error(err, "商户注册失败")
		return err
	}

	if len(resp.Data) > 0 {
		var reg struct {
			Key    string `json:"key"`
			Secret string `json:"secret"`
		}
		if jerr := json.Unmarshal(resp.Data, &reg); jerr == nil && reg.Secret != "" {
			c.SetMerchantSecret(reg.Secret)
			if serr := saveMerchantSecret(reg.Secret); serr != nil {
				logger.Warn(fmt.Sprintf("持久化 per-merchant secret 失败(本次进程内仍生效): %v", serr))
			}
			logger.Info("已接收并启用每商户独立签名密钥")
		}
	}
	logger.Info("商户注册成功")
	return nil
}

func merchantSecretFilePath() string {
	return filepath.Join(merchantStateDir(), ".merchant_api_secret")
}

func saveMerchantSecret(secret string) error {
	p := merchantSecretFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(secret), 0o600)
}

// loadMerchantSecret 读回本商户独立的签名密钥。
// 文件不存在是首次安装的正常态，静默即可；其余读取失败意味着签名会用错密钥，
// 必须上报给调用方记日志——吞掉它会把故障推到平台侧的成片 401 上，事后无从定位。
func (c *Client) loadMerchantSecret() error {
	p := merchantSecretFilePath()
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		c.SetMerchantSecret(strings.TrimSpace(string(b)))
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("读取 per-merchant secret(%s) 失败: %w", p, err)
	}
}

// CheckConnection 探测平台是否可达：只打平台真实存在的存活性端点 GET {APIURL}/health。
//
// 不走 doRetry —— 那条路会做商户签名并可能拉 JWT，把一个连通性探针绑到鉴权链上等于
// 多造两类假故障（签名没配好 / 登录不上都会被判成"平台挂了"）。
// 也不要恢复成"查授权状态"：平台从未实现授权端点（开源版连 License 概念都移除了），
// 拿它做探针会让健康平台恒被判成 unreachable（R12）。
func (c *Client) CheckConnection() error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 未配置平台地址，连通性探测无从发出", ErrPlatformNotConfigured)
	}
	req, err := http.NewRequest(http.MethodGet, config.PlatformCfg.APIURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("平台连通性探测请求构造失败: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("平台连通性探测失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &PlatformError{StatusCode: resp.StatusCode}
	}
	return nil
}

type BaseResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// PlatformError 结构化错误，携带 HTTP 状态码与平台响应体，便于调用方按 code/状态码
// 而非脆弱的字符串匹配(如 strings.Contains(err, "404"))进行分支处理。
type PlatformError struct {
	StatusCode int
	RawBody    string
	Resp       *BaseResp
}

func (e *PlatformError) Error() string {
	if e.Resp != nil && e.Resp.Msg != "" {
		return fmt.Sprintf("platform request failed: status=%d, code=%d, msg=%s", e.StatusCode, e.Resp.Code, e.Resp.Msg)
	}
	return fmt.Sprintf("platform request failed: status=%d, body=%s", e.StatusCode, e.RawBody)
}

// Msg 返回平台返回的业务错误信息（优先 Resp.Msg，其次原始响应体）。
func (e *PlatformError) Msg() string {
	if e.Resp != nil && e.Resp.Msg != "" {
		return e.Resp.Msg
	}
	if e.RawBody != "" {
		return e.RawBody
	}
	return e.Error()
}

type RegisterMerchantReq struct {
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
	ContactPhone string `json:"contact_phone"`
	DeviceInfo   string `json:"device_info"`
}

// ReportInstallReq 安装信息上报请求（开源版：一个安装信息 = 一个商户）
type ReportInstallReq struct {
	InstallID         string `json:"install_id"`
	MerchantName      string `json:"merchant_name"`
	ContactEmail      string `json:"contact_email"`
	ContactPhone      string `json:"contact_phone"`
	ContactName       string `json:"contact_name"`
	DeviceFingerprint string `json:"device_fingerprint"`
	ClientIP          string `json:"client_ip"`
	Version           string `json:"version"`
}

// ReportInstall 上报安装信息到平台（开源版：创建/更新一个商户）
// 该接口为公开统计接口，不要求 JWT / 商户签名。
func (c *Client) ReportInstall(req *ReportInstallReq) error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 上报请求未发出", ErrPlatformNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化安装信息失败: %w", err)
	}
	url := strings.TrimRight(config.PlatformCfg.APIURL, "/") + "/api/platform/install"
	httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("上报安装信息失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("上报安装信息返回 %d: %s", resp.StatusCode, string(raw))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取安装响应失败: %w", err)
	}
	if perr := envelopeRefusal(raw); perr != nil {
		return fmt.Errorf("上报安装信息被拒: code=%d, msg=%s", perr.Resp.Code, perr.Resp.Msg)
	}
	logger.Info("上报安装信息成功")
	return nil
}

// ReportInstallDefault 使用全局配置创建客户端并上报安装信息
// 安装信息上报为公开统计接口，不要求商户签名，故使用空 merchantKey。
func ReportInstallDefault(req *ReportInstallReq) error {
	return NewPlatformClient("").ReportInstall(req)
}

// ReportHeartbeatReq 心跳上报请求（开源版：每 3 分钟上报一次，仅统计用）
type ReportHeartbeatReq struct {
	InstallID         string          `json:"install_id"`
	Version           string          `json:"version"`
	HostInfo          json.RawMessage `json:"host_info"`
	Metrics           json.RawMessage `json:"metrics"`
	DeviceFingerprint string          `json:"device_fingerprint"`
	ClientIP          string          `json:"client_ip"`
	Timestamp         time.Time       `json:"timestamp"`
}

// ReportHeartbeat 上报心跳到平台（公开统计接口，不要求签名/JWT）
func (c *Client) ReportHeartbeat(req *ReportHeartbeatReq) error {
	if config.PlatformCfg == nil {
		return fmt.Errorf("%w: 上报请求未发出", ErrPlatformNotConfigured)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化心跳数据失败: %w", err)
	}
	url := strings.TrimRight(config.PlatformCfg.APIURL, "/") + "/api/platform/heartbeat"
	httpReq, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("上报心跳失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("上报心跳返回 %d: %s", resp.StatusCode, string(raw))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取心跳响应失败: %w", err)
	}
	if perr := envelopeRefusal(raw); perr != nil {
		return fmt.Errorf("上报心跳被拒: code=%d, msg=%s", perr.Resp.Code, perr.Resp.Msg)
	}
	logger.Info("上报心跳成功")
	return nil
}

// ReportHeartbeatDefault 使用全局配置创建客户端并上报心跳
// 心跳上报为公开统计接口，不要求商户签名，故使用空 merchantKey。
func ReportHeartbeatDefault(req *ReportHeartbeatReq) error {
	return NewPlatformClient("").ReportHeartbeat(req)
}
