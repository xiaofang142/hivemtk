package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// http 动作的受控边界（FA-01）：
//   - 仅 http/https 且必须显式配置 url，缺 URL/非法 URL 直接失败，不再回退 placeholder；
//   - 方法白名单；单请求超时节点可配（默认 10s，硬顶 30s）；
//   - 响应体读取限长防内存放大；落库输出再截断防节点记录膨胀；
//   - 仅 2xx 记"已执行"副作用：失败路径不落副作用键，dispatcher 的重试=真实重发；
//   - 信任边界：工作流由租户管理员配置，URL 可达内网服务属产品既有信任域内，
//     本执行器不做内网 IP 段封禁（会切断合法内部集成）。
const (
	defaultHTTPActionTimeout    = 10 * time.Second
	maxHTTPActionTimeout        = 30 * time.Second
	httpActionResponseReadLimit = 64 << 10
	httpActionOutputBodyLimit   = 4096
)

var workflowHTTPClient = &http.Client{Timeout: defaultHTTPActionTimeout}

var allowedHTTPActionMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodHead:    true,
}

// executeHTTPAction 执行 action_type=http 的真实出站请求。
// 返回值语义：completed=平台侧已完成请求且 2xx；failed=未送达或上游拒绝，
// ErrorMessage 写明原因、Retryable 标注 dispatcher 是否值得重试。
func (e *ActionNodeExecutor) executeHTTPAction(ctx context.Context, wctx *WorkflowExecContext, sideEffectKey string) *WorkflowNodeExecResult {
	cfg := wctx.NodeConfig

	rawURL := configString(cfg, "url")
	if rawURL == "" {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: "http action requires url in node config (placeholder fallback removed)",
			Retryable:    false,
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action url must be an absolute http(s) URL, got %q", rawURL),
			Retryable:    false,
		}
	}

	method := strings.ToUpper(configString(cfg, "method"))
	if method == "" {
		method = http.MethodPost
	}
	if !allowedHTTPActionMethods[method] {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action method %q not allowed (get/post/put/patch/delete/head)", method),
			Retryable:    false,
		}
	}

	body, contentType, err := buildHTTPActionBody(cfg)
	if err != nil {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action body invalid: %v", err),
			Retryable:    false,
		}
	}

	timeout := defaultHTTPActionTimeout
	if ms := configInt(cfg, "timeout_ms"); ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
		if timeout > maxHTTPActionTimeout {
			timeout = maxHTTPActionTimeout
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action build request failed: %v", err),
			Retryable:    false,
		}
	}
	for k, v := range configStringMap(cfg, "headers") {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && len(body) > 0 && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", contentType)
	}

	logger.Ctx(ctx).Info().Str("workflow_action", "http").Str("method", method).Str("url", rawURL).
		Dur("timeout", timeout).Msg("ActionNodeExecutor: http dispatch")

	resp, err := workflowHTTPClient.Do(req)
	if err != nil {
		// ctx 已取消（调度器停止/请求被外部打断）时重试无意义；网络类错误按可重试处理。
		if ctx.Err() != nil || reqCtx.Err() == context.Canceled {
			return &WorkflowNodeExecResult{
				Status:       NodeStatusFailed,
				ErrorMessage: fmt.Sprintf("http action cancelled: %v", err),
				Retryable:    false,
			}
		}
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action request failed: %v", err),
			Retryable:    true,
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, httpActionResponseReadLimit))
	if readErr != nil && len(respBody) == 0 {
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			ErrorMessage: fmt.Sprintf("http action read response failed: %v", readErr),
			Retryable:    true,
		}
	}

	output := model.JSONMap{
		"_action_type":  "http",
		"url":           rawURL,
		"method":        method,
		"status_code":   resp.StatusCode,
		"response_body": truncateHTTPActionBody(respBody),
	}
	if readErr != nil {
		output["response_truncated"] = true
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		output["retryable"] = resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return &WorkflowNodeExecResult{
			Status:       NodeStatusFailed,
			Output:       output,
			ErrorMessage: fmt.Sprintf("http action got non-2xx status %d from %s", resp.StatusCode, rawURL),
			Retryable:    resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests,
		}
	}

	// 仅真实成功才标记已执行：失败重试时本键不存在，节点会被再次执行（真实重发）。
	AppendWorkflowSideEffect(wctx, sideEffectKey)
	return &WorkflowNodeExecResult{
		Status:      NodeStatusCompleted,
		Output:      output,
		NextNodeID:  "",
		SideEffects: []string{sideEffectKey, "http_called:" + rawURL},
	}
}

// buildHTTPActionBody 从节点配置取请求体：body（原样字符串）或 body_json（对象序列化）。
func buildHTTPActionBody(cfg model.JSONMap) (body []byte, contentType string, err error) {
	contentType = "application/json"
	if cfg == nil {
		return nil, contentType, nil
	}
	if v, ok := cfg["body_json"]; ok && v != nil {
		raw, mErr := json.Marshal(v)
		if mErr != nil {
			return nil, contentType, fmt.Errorf("marshal body_json: %w", mErr)
		}
		return raw, contentType, nil
	}
	if s, ok := cfg["body"].(string); ok && s != "" {
		return []byte(s), contentType, nil
	}
	return nil, contentType, nil
}

func truncateHTTPActionBody(b []byte) string {
	if len(b) <= httpActionOutputBodyLimit {
		return string(b)
	}
	return string(b[:httpActionOutputBodyLimit]) + "…(truncated)"
}

func configString(cfg model.JSONMap, key string) string {
	if cfg == nil {
		return ""
	}
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

func configInt(cfg model.JSONMap, key string) int {
	if cfg == nil {
		return 0
	}
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func configStringMap(cfg model.JSONMap, key string) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
