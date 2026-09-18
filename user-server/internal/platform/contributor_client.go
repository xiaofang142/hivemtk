package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func contributorIdentity() (username, password, email, displayName string) {
	mk := GetMerchantKey()
	if mk == "" {
		mk = "anonymous"
	}
	username = "mtk_" + mk

	secret := config.PlatformCfg.Secret
	if secret == "" {
		if os.Getenv("CONTRIBUTOR_DEV") == "1" {
			secret = "dev-only-placeholder-secret-do-not-use-in-prod"
			logger.Warn("contributor_identity: CONTRIBUTOR_DEV=1 detected, using placeholder secret. NEVER set this in production.")
		} else {
			panic("contributor_identity: platform.secret is empty. Set platform.secret in config or CONTRIBUTOR_DEV=1 for local dev only.")
		}
	}
	sum := sha256.Sum256([]byte(mk + "|" + secret))
	password = hex.EncodeToString(sum[:])[:16]
	email = username + "@mtk.local"
	displayName = "商户-" + mk
	return
}

func ensureContributorToken(cc *ContributorClient) (string, error) {
	contribMu.Lock()
	defer contribMu.Unlock()
	if contribToken != "" && time.Now().Before(contribExpireAt.Add(-60*time.Second)) {
		return contribToken, nil
	}
	username, password, email, displayName := contributorIdentity()
	if tok, err := cc.login(username, password); err == nil && tok != "" {
		contribToken, contribExpireAt = tok, time.Now().Add(24*time.Hour)
		return tok, nil
	}
	if err := cc.register(username, password, email, displayName); err != nil {
		logger.Warn(fmt.Sprintf("contributor 自动注册失败(可忽略，登录重试): %v", err))
	}
	tok, err := cc.login(username, password)
	if err != nil || tok == "" {
		return "", fmt.Errorf("获取平台贡献者 token 失败: %v", err)
	}
	contribToken, contribExpireAt = tok, time.Now().Add(24*time.Hour)
	return tok, nil
}

func (cc *ContributorClient) login(username, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	var out struct {
		Token string `json:"token"`
	}
	if err := cc.doAuth("POST", "/contributor-api/v1/auth/login", body, &out, ""); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (cc *ContributorClient) register(username, password, email, displayName string) error {
	body, _ := json.Marshal(map[string]string{
		"username":     username,
		"password":     password,
		"email":        email,
		"display_name": displayName,
	})
	return cc.doAuth("POST", "/contributor-api/v1/auth/register", body, nil, "")
}

// CreateAsset 以贡献者身份在平台创建资产（data 为 OpenAI 兼容 messages 数组），返回平台资产 ID
func (cc *ContributorClient) CreateAsset(p CreateAssetPayload) (int64, error) {
	tok, err := ensureContributorToken(cc)
	if err != nil {
		return 0, err
	}
	body, _ := json.Marshal(p)
	var out struct {
		ID int64 `json:"id"`
	}
	if err := cc.doAuth("POST", "/contributor-api/v1/assets", body, &out, tok); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, fmt.Errorf("平台创建资产返回空 ID")
	}
	return out.ID, nil
}

// SubmitAudit 将平台资产提交审核上架
func (cc *ContributorClient) SubmitAudit(assetID int64) error {
	tok, err := ensureContributorToken(cc)
	if err != nil {
		return err
	}
	return cc.doAuth("POST", fmt.Sprintf("/contributor-api/v1/assets/%d/submit", assetID), nil, nil, tok)
}

func (cc *ContributorClient) doAuth(method, path string, body []byte, out any, token string) error {
	url := strings.TrimRight(cc.baseURL, "/") + path
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := cc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("调用平台贡献者接口失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("平台贡献者接口返回 %d: %s", resp.StatusCode, string(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}
