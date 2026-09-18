package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
)

type apiPublisher struct {
	platform, endpoint, tokenEnv, userIDEnv string
}

func (a *apiPublisher) PlatformName() string { return a.platform }

func (a *apiPublisher) Publish(ctx context.Context, account *model.GeoPlatformAccount, content AdaptedContent) (string, error) {
	token := os.Getenv(a.tokenEnv)
	if token == "" && account != nil && account.Config != "" {

		var creds map[string]string
		if json.Unmarshal([]byte(account.Config), &creds) == nil {
			token = creds["access_token"]
			if token == "" {
				token = creds["api_key"]
			}
		}
	}
	if token == "" {
		return "", fmt.Errorf("%s 平台需要配置 access_token 或设置环境变量 %s", a.platform, a.tokenEnv)
	}

	var body map[string]any
	switch a.platform {
	case PlatformMedium:

		userID := os.Getenv(a.userIDEnv)
		if userID == "" && account != nil {
			userID = account.AccountID
		}
		if userID == "" {
			return "", fmt.Errorf("medium 平台需要 user_id（环境变量 MEDIUM_USER_ID 或账号 AccountID）")
		}
		endpoint := strings.ReplaceAll(a.endpoint, "{userId}", userID)
		body = map[string]any{
			"title":           content.Title,
			"contentFormat":   "markdown",
			"content":         content.Body,
			"publishStatus":   "public",
			"notifyFollowers": true,
		}
		return a.doPost(ctx, endpoint, token, body)

	case PlatformDevTo:

		body = map[string]any{
			"article": map[string]any{
				"title":         content.Title,
				"body_markdown": content.Body,
				"published":     true,
				"tags":          []string{"geoptimization", "ai", "content"},
			},
		}
		return a.doPost(ctx, a.endpoint, token, body)

	default:
		return "", fmt.Errorf("未实现的 API 平台: %s", a.platform)
	}
}

func (a *apiPublisher) doPost(ctx context.Context, endpoint, token string, body map[string]any) (string, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("序列化请求体: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s HTTP 请求失败: %w", a.platform, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s API %d: %s", a.platform, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("%s 解析响应失败: %w", a.platform, err)
	}

	if data, ok := result["data"].(map[string]any); ok {
		if url, ok := data["url"].(string); ok && url != "" {
			return url, nil
		}
	}
	if url, ok := result["url"].(string); ok && url != "" {
		return url, nil
	}
	return fmt.Sprintf("https://%s.com/", a.platform), nil
}

// NewDefaultPublishers 返回真实可发布的平台发布器
// GitHub 平台由 PlatformService.publishGitHub 直接处理（不经过 PublishPipeline）
// 掘金/小红书/知乎/CSDN 等无公开 API 平台已移除 stub，需要时可接 Playwright
func NewDefaultPublishers() []PlatformPublisher {
	return []PlatformPublisher{
		&apiPublisher{
			platform:  PlatformMedium,
			endpoint:  "https://api.medium.com/v1/users/{userId}/posts",
			tokenEnv:  "MEDIUM_TOKEN",
			userIDEnv: "MEDIUM_USER_ID",
		},
		&apiPublisher{
			platform: PlatformDevTo,
			endpoint: "https://dev.to/api/articles",
			tokenEnv: "DEVTO_TOKEN",
		},
	}
}
