package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// HTTPAfterSaleClient 标准售后回写 HTTP 客户端，实现 AfterSaleExternalClient。
// 注意：当前为通用回调客户端（默认 POST {baseURL}/api/refund），不覆盖 EC-14 中的逆向物流、
// 仓库验收/质检、换货发运等分步流程；其“成功/失败”语义仍需结合业务错误解析与外部对账。
type HTTPAfterSaleClient struct {
	baseURL    string
	key        string
	secret     string
	http       *http.Client
	authorizer func(context.Context, *portcontract.AfterSaleRequest) error
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
	check := c.authorizer
	if check == nil {
		check = authorizeAfterSaleSubmit
	}
	if err := check(ctx, req); err != nil {
		return nil, err
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
	// EC-13 (M13): HTTP 200 仍需解析业务错误，避免“看似成功但实为失败”的泛化回写。
	var out struct {
		ExternalID  string      `json:"external_id"`
		Status      string      `json:"status"`
		Code        interface{} `json:"code"`
		Success     *bool       `json:"success"`
		Message     string      `json:"message"`
		Error       string      `json:"error"`
		ErrorMsg    string      `json:"error_msg"`
		ErrMsg      string      `json:"errmsg"`
		ErrCode     interface{} `json:"errcode"`
		BusinessErr string      `json:"business_error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
<<<<<<< HEAD
	// Heuristic business error detection (best-effort; keep backward compatible).
	if out.Success != nil && !*out.Success {
		msg := out.Message
		if msg == "" {
			msg = out.Error
		}
		if msg == "" {
			msg = out.ErrorMsg
		}
		if msg == "" {
			msg = out.ErrMsg
		}
		if msg == "" {
			msg = out.BusinessErr
		}
		if msg == "" {
			msg = "business error (success=false)"
		}
		return nil, fmt.Errorf("aftersale business error: %s", msg)
	}
	if out.BusinessErr != "" {
		return nil, fmt.Errorf("aftersale business error: %s", out.BusinessErr)
	}
	if out.ErrCode != nil {
		// Common in some platforms: errcode != 0 means error
		switch v := out.ErrCode.(type) {
		case float64:
			if v != 0 {
				return nil, fmt.Errorf("aftersale business error: errcode=%.0f %s", v, out.ErrMsg)
			}
		case int:
			if v != 0 {
				return nil, fmt.Errorf("aftersale business error: errcode=%d %s", v, out.ErrMsg)
			}
		case string:
			if v != "" && v != "0" {
				return nil, fmt.Errorf("aftersale business error: errcode=%s %s", v, out.ErrMsg)
			}
		}
	}
	if out.Code != nil {
		switch v := out.Code.(type) {
		case string:
			vs := strings.ToUpper(strings.TrimSpace(v))
			if vs != "" && vs != "0" && vs != "200" && vs != "SUCCESS" && vs != "OK" && vs != "TRUE" {
				msg := out.Message
				if msg == "" {
					msg = out.Error
				}
				if msg == "" {
					msg = out.ErrorMsg
				}
				if msg == "" {
					msg = v
				}
				return nil, fmt.Errorf("aftersale business error: code=%s %s", v, msg)
			}
		case float64:
			if v != 0 {
				return nil, fmt.Errorf("aftersale business error: code=%.0f", v)
			}
		}
	}
	// Feature-branch parity: reject if no external reference.
	if out.ExternalID == "" {
		return nil, fmt.Errorf("aftersale provider rejected or missing external reference")
	}
	return &AfterSaleExternalResult{ExternalID: out.ExternalID, Status: out.Status}, nil
}
