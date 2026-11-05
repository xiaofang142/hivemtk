package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
)

// publishWebhook 通用 Webhook 发布：POST JSON，可选 HMAC-SHA256 签名。
// 凭证：url（必填）、secret（可选，填写后带 X-GEO-Signature）。
func (s *PlatformService) publishWebhook(ctx context.Context, article *model.GeoArticle, req *dto.PublishRequest) (*dto.PublishResponse, error) {
	creds := s.latestAccountCredentials(ctx, "webhook")
	if creds == nil {
		return nil, fmt.Errorf("未配置 Webhook 账号：请先保存 url（和可选 secret）")
	}
	endpoint := firstNonEmpty(creds["url"], creds["endpoint"])
	if endpoint == "" {
		return nil, fmt.Errorf("webhook 缺少 url")
	}
	if err := validateOutboundURL(endpoint, true); err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(creds["secret"])

	payload := map[string]any{
		"event":    "geo.article.publish",
		"platform": req.Platform,
		"article": map[string]any{
			"id":      article.ID,
			"title":   article.Title,
			"keyword": article.Keyword,
			"content": article.Content,
		},
		"published_at": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 Webhook 负载失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造 Webhook 请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "HiveMtk-GEO/1.0")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		httpReq.Header.Set("X-GEO-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.saveFailedRecord(ctx, article.ID, req.Platform, err.Error())
		return nil, fmt.Errorf("webhook 请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		s.saveFailedRecord(ctx, article.ID, req.Platform, msg)
		return nil, fmt.Errorf("webhook %d: %s", resp.StatusCode, msg)
	}

	publishedURL := endpoint
	var out struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(respBody, &out) == nil && strings.TrimSpace(out.URL) != "" {
		publishedURL = strings.TrimSpace(out.URL)
	}
	record := &model.GeoPublishRecord{
		ArticleID:    article.ID,
		Platform:     req.Platform,
		Status:       PublishStatusCopied,
		PublishedURL: publishedURL,
		PublishedAt:  time.Now(),
	}
	if err := s.recordRepo.Create(ctx, record); err != nil {
		return nil, err
	}
	return &dto.PublishResponse{ID: record.ID, Status: PublishStatusCopied, PublishedURL: publishedURL}, nil
}
