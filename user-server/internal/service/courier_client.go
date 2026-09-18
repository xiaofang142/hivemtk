package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
)

// CourierClient 标准快递轨迹 HTTP 客户端，实现 portcontract.CourierClient
type CourierClient struct {
	baseURL string
	key     string
	secret  string
	http    *http.Client
}

// NewCourierClientFromConfig 按数据库中的物流集成配置构造。baseURL 为空表示未配置实时物流接口。
func NewCourierClientFromConfig(cfg LogisticsIntegration) *CourierClient {
	return &CourierClient{
		baseURL: cfg.BaseURL,
		key:     cfg.Key,
		secret:  cfg.Secret,
		http:    &http.Client{Timeout: 8 * time.Second},
	}
}

// Configured 实时快递接口是否已配置
func (c *CourierClient) Configured() bool { return c.baseURL != "" }

// Query 按 快递公司 + 运单号 查询实时轨迹。未配置或运单号为空时返回 (nil, nil)。
func (c *CourierClient) Query(ctx context.Context, carrier, trackingNo string) ([]*portcontract.LogisticsTrackView, error) {
	if !c.Configured() || trackingNo == "" {
		return nil, nil
	}
	reqURL := fmt.Sprintf("%s/api/track?carrier=%s&no=%s", c.baseURL, url.QueryEscape(carrier), url.QueryEscape(trackingNo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	if c.secret != "" {
		req.Header.Set("X-Api-Secret", c.secret)
	}
	resp, err := c.http.Do(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("courier api status %d", resp.StatusCode)
	}
	var out struct {
		Tracks []struct {
			Time        string `json:"time"`
			Status      string `json:"status"`
			Location    string `json:"location"`
			Description string `json:"description"`
		} `json:"tracks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	tracks := make([]*portcontract.LogisticsTrackView, 0, len(out.Tracks))
	for _, t := range out.Tracks {
		tracks = append(tracks, &portcontract.LogisticsTrackView{
			Time:        t.Time,
			Status:      t.Status,
			Location:    t.Location,
			Description: t.Description,
		})
	}
	return tracks, nil
}
