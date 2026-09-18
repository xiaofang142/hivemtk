package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
)

// AfterSaleExternalResult 电商侧回写结果
type AfterSaleExternalResult struct {
	ExternalID string
	Status     string
}

// AfterSaleExternalClient 售后回写电商平台客户端接口
type AfterSaleExternalClient interface {
	Create(ctx context.Context, req *portcontract.AfterSaleRequest) (*AfterSaleExternalResult, error)
	Configured() bool
}

// HTTPAfterSaleClient 标准售后回写 HTTP 客户端，实现 AfterSaleExternalClient
type HTTPAfterSaleClient struct {
	baseURL string
	key     string
	secret  string
	http    *http.Client
}

// NewAfterSaleExternalClientFromConfig 按数据库中的售后集成配置构造。baseURL 为空表示未配置回写接口。
func NewAfterSaleExternalClientFromConfig(cfg AfterSaleIntegration) *HTTPAfterSaleClient {
	return &HTTPAfterSaleClient{
		baseURL: cfg.BaseURL,
		key:     cfg.Key,
		secret:  cfg.Secret,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured 回写接口是否已配置
func (c *HTTPAfterSaleClient) Configured() bool { return c.baseURL != "" }

// Create 把售后请求推给电商执行落地。未配置时返回 (nil, nil)。
func (c *HTTPAfterSaleClient) Create(ctx context.Context, req *portcontract.AfterSaleRequest) (*AfterSaleExternalResult, error) {
	if !c.Configured() {
		return nil, nil
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	url := c.baseURL + "/api/refund"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.secret != "" {
		httpReq.Header.Set("X-Api-Secret", c.secret)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aftersale api status %d", resp.StatusCode)
	}
	var out struct {
		ExternalID string `json:"external_id"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &AfterSaleExternalResult{ExternalID: out.ExternalID, Status: out.Status}, nil
}
