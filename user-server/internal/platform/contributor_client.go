package platform

import (
	"bytes"
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
	"strings"
	"sync"
	"time"
)

var (
	contribMu       sync.Mutex
	contribToken    string
	contribExpireAt time.Time
)

// CreateAssetPayload 平台贡献者端创建资产请求体（资产 data 为 OpenAI 兼容 messages 数组）
type CreateAssetPayload struct {
	AssetType   string          `json:"asset_type"`
	Industry    string          `json:"industry"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Version     string          `json:"version"`
	Changelog   string          `json:"changelog"`
	Data        json.RawMessage `json:"data"`
}

// ContributorClient 以"贡献者"身份调用平台端 contributor-api，用于商户端开发者
// 将本地调试好的资产包提交平台审核上架。凭证基于商户标识自动注册/登录，幂等。
type ContributorClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewContributorClient() *ContributorClient {
	apiURL := ""
	if config.PlatformCfg != nil {
		apiURL = config.PlatformCfg.APIURL
	}
	return &ContributorClient{baseURL: apiURL, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// contributorAuth 派生出的贡献者账号凭证。
type contributorAuth struct {
	username    string
	password    string
	email       string
	displayName string
}

const devPlaceholderSecret = "dev-only-placeholder-secret-do-not-use-in-prod"

// contributorIdentity 由 (merchantKey, platform.secret) 派生贡献者身份。
//
// 派生式必须可复现：同一台商户端重启/重装后要落回平台上的同一个账号，否则每次提交都是新用户。
// 因此「拿不到锚点」的三种情况（无 merchantKey / 无平台配置 / secret 为空）一律回 error，
// 而不是带着空值继续派生出一个跨实例撞号、或拿到配置后静默换人的身份。
// 调用方是资产上传的请求协程，这里 panic 会直接把请求打挂，必须走 error。
func contributorIdentity() (contributorAuth, error) {
	var a contributorAuth
	mk := GetMerchantKey()
	if mk == "" {
		return a, errors.New("贡献者身份无法派生: merchant key 未初始化（platform.InitSync 未执行或状态目录不可用）")
	}
	a.username = "mtk_" + mk

	secret := ""
	if config.PlatformCfg != nil {
		secret = config.PlatformCfg.Secret
	}
	if secret == "" {
		if os.Getenv("CONTRIBUTOR_DEV") == "1" {
			secret = devPlaceholderSecret
			logger.Warn("contributor_identity: CONTRIBUTOR_DEV=1 detected, using placeholder secret. NEVER set this in production.")
		} else if config.PlatformCfg == nil {
			return a, fmt.Errorf("%w: 无法派生贡献者身份，请先加载平台配置", ErrPlatformNotConfigured)
		} else {
			return a, fmt.Errorf("%w: platform.secret 为空，无法派生贡献者身份（本地调试可设 CONTRIBUTOR_DEV=1）", ErrPlatformNotConfigured)
		}
	}

	sum := sha256.Sum256([]byte(mk + "|" + secret))
	a.password = hex.EncodeToString(sum[:])[:16]
	a.email = a.username + "@mtk.local"
	a.displayName = "商户-" + mk
	return a, nil
}

// ErrContributorUnauthorized 平台侧判定贡献者 JWT 无效（真 HTTP 401，来自 JWT 中间件）。
// 与「业务失败」不同：后者平台也用 HTTP 200 + code 表达，见 contributorResp。
var ErrContributorUnauthorized = errors.New("平台贡献者 token 未通过校验")

// contributorResp 平台贡献者接口的统一信封。
//
// 契约取证自平台实现（hivemtk-platform/platform-server/internal/utils/response/response.go:17-23）：
// 成功回 {code:200,msg,data}，业务失败同样回 HTTP 200、真值写在 code 里，成功数据一律嵌在 data 下。
// 因此判定必须看 code、取值必须进 data —— 只看 HTTP 状态码会把平台的拒绝读成成功，
// 只读顶层字段会把平台的成功读成「拿不到 token」。
type contributorResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (r *contributorResp) succeeded() bool { return r.Code == http.StatusOK }

// decodeData 把信封里的 data 解到 out。
func (r *contributorResp) decodeData(out any) error {
	if len(r.Data) == 0 || string(r.Data) == "null" {
		return fmt.Errorf("平台响应缺少 data")
	}
	return json.Unmarshal(r.Data, out)
}

// ensureContributorToken 拿到可用的贡献者 token：登录 → 失败则自动注册（注册即签发 token）→ 再登录一次。
func ensureContributorToken(cc *ContributorClient) (string, error) {
	contribMu.Lock()
	defer contribMu.Unlock()
	if contribToken != "" && time.Now().Before(contribExpireAt.Add(-60*time.Second)) {
		return contribToken, nil
	}
	auth, err := contributorIdentity()
	if err != nil {
		return "", err
	}
	tok, loginErr := cc.login(auth.username, auth.password)
	if loginErr == nil {
		contribToken, contribExpireAt = tok, time.Now().Add(24*time.Hour)
		return tok, nil
	}
	registered, regErr := cc.register(auth.username, auth.password, auth.email, auth.displayName)
	if regErr == nil {
		contribToken, contribExpireAt = registered, time.Now().Add(24*time.Hour)
		return registered, nil
	}
	logger.Warn(fmt.Sprintf("contributor 自动注册失败(可忽略，登录重试): %v", regErr))
	// 注册被拒最常见的原因是同号已由另一实例建好，再登一次
	if tok, err2 := cc.login(auth.username, auth.password); err2 == nil {
		contribToken, contribExpireAt = tok, time.Now().Add(24*time.Hour)
		return tok, nil
	}
	return "", fmt.Errorf("获取平台贡献者 token 失败: 登录=%v, 注册=%v", loginErr, regErr)
}

// login 用派生身份换 token；平台把 token 放在 data.token。
func (cc *ContributorClient) login(username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	env, err := cc.doAuth("POST", "/contributor-api/v1/auth/login", body, "")
	if err != nil {
		return "", err
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := env.decodeData(&data); err != nil {
		return "", err
	}
	if data.Token == "" {
		return "", errors.New("平台登录成功但未返回 data.token")
	}
	return data.Token, nil
}

// register 首次使用本身份时自动开户；平台注册成功即签发 token（签发失败会回滚账号）。
func (cc *ContributorClient) register(username, password, email, displayName string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username":     username,
		"password":     password,
		"email":        email,
		"display_name": displayName,
	})
	env, err := cc.doAuth("POST", "/contributor-api/v1/auth/register", body, "")
	if err != nil {
		return "", err
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := env.decodeData(&data); err != nil {
		return "", err
	}
	if data.Token == "" {
		return "", errors.New("平台注册成功但未返回 data.token")
	}
	return data.Token, nil
}

// doAuthorized 带贡献者 token 调平台接口；401 时清掉缓存重登一次再试。
// 没有这条自愈，平台轮换 JWT 密钥后本实例会卡在坏 token 里直到 24h 缓存自然过期。
func (cc *ContributorClient) doAuthorized(method, path string, body []byte) (*contributorResp, error) {
	tok, err := ensureContributorToken(cc)
	if err != nil {
		return nil, err
	}
	env, err := cc.doAuth(method, path, body, tok)
	if !errors.Is(err, ErrContributorUnauthorized) {
		return env, err
	}
	contribMu.Lock()
	contribToken, contribExpireAt = "", time.Time{}
	contribMu.Unlock()
	if tok, err = ensureContributorToken(cc); err != nil {
		return nil, err
	}
	return cc.doAuth(method, path, body, tok)
}

// CreateAsset 以贡献者身份在平台创建资产（data 为 OpenAI 兼容 messages 数组），返回平台资产 ID
func (cc *ContributorClient) CreateAsset(p CreateAssetPayload) (int64, error) {
	body, _ := json.Marshal(p)
	env, err := cc.doAuthorized("POST", "/contributor-api/v1/assets", body)
	if err != nil {
		return 0, err
	}
	var data struct {
		ID int64 `json:"id"`
	}
	if err := env.decodeData(&data); err != nil {
		return 0, err
	}
	if data.ID == 0 {
		return 0, fmt.Errorf("平台创建资产返回空 ID")
	}
	return data.ID, nil
}

// SubmitAudit 将平台资产提交审核上架
func (cc *ContributorClient) SubmitAudit(assetID int64) error {
	_, err := cc.doAuthorized("POST", fmt.Sprintf("/contributor-api/v1/assets/%d/submit", assetID), nil)
	return err
}

func (cc *ContributorClient) doAuth(method, path string, body []byte, token string) (*contributorResp, error) {
	url := strings.TrimRight(cc.baseURL, "/") + path
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用平台贡献者接口失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrContributorUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("平台贡献者接口返回 %d: %s", resp.StatusCode, string(raw))
	}
	env := &contributorResp{Code: http.StatusOK}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, env); err != nil {
			return nil, fmt.Errorf("解析平台贡献者响应失败: %w, body=%s", err, string(raw))
		}
	}
	if !env.succeeded() {
		return env, fmt.Errorf("平台贡献者接口拒绝(code=%d): %s", env.Code, env.Msg)
	}
	return env, nil
}
