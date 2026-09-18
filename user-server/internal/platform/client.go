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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrPlatformNotConfigured 平台客户端未配置哨兵错误（轮询型端点静默降级判定用）
var ErrPlatformNotConfigured = errors.New("平台配置未初始化")

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

	c.loadMerchantSecret()
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
		return fmt.Errorf("平台配置未初始化")
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
		logger.Error(fmt.Errorf("平台配置未初始化"), "商户上报请求失败")
		return fmt.Errorf("平台配置未初始化")
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
	if respData != nil {
		var err error
		respBody, err = io.ReadAll(resp.Body)
		if err != nil {
			logger.Error(err, "读取响应体失败")
			return err
		}
		logger.Info(fmt.Sprintf("商户上报请求成功: %s %s, 状态码: %d, 耗时: %v, 响应数据: %s", method, url, resp.StatusCode, duration, string(respBody)))
		return json.Unmarshal(respBody, respData)
	}
	return nil
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
	return filepath.Join("config", ".merchant_api_secret")
}

func saveMerchantSecret(secret string) error {
	p := merchantSecretFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(secret), 0o600)
}

func (c *Client) loadMerchantSecret() {
	b, err := os.ReadFile(merchantSecretFilePath())
	if err == nil {
		c.SetMerchantSecret(strings.TrimSpace(string(b)))
	}
}

// GetLicenseStatus 获取授权状态
func (c *Client) GetLicenseStatus() (*LicenseStatusResp, error) {
	logger.Info("开始获取授权状态")
	var resp BaseResp
	if err := c.do("GET", "/merchant-api/license/status", nil, &resp); err != nil {
		logger.Error(err, "获取授权状态失败")
		return nil, err
	}
	var data LicenseStatusResp
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		logger.Error(err, "解析授权状态响应失败")
		return nil, err
	}
	logger.Info(fmt.Sprintf("获取授权状态成功: 状态=%s, 到期时间=%s, 剩余天数=%d",
		data.Status, data.ExpireAt.Format("2006-01-02 15:04:05"), data.Remaining))
	return &data, nil
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

type LicenseStatusResp struct {
	Status    string    `json:"status"`
	ExpireAt  time.Time `json:"expire_at"`
	Remaining int       `json:"remaining_days"`
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
		return fmt.Errorf("平台配置未初始化")
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
		return fmt.Errorf("平台配置未初始化")
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
	logger.Info("上报心跳成功")
	return nil
}

// ReportHeartbeatDefault 使用全局配置创建客户端并上报心跳
// 心跳上报为公开统计接口，不要求商户签名，故使用空 merchantKey。
func ReportHeartbeatDefault(req *ReportHeartbeatReq) error {
	return NewPlatformClient("").ReportHeartbeat(req)
}
