package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hivemtk-user/internal/repository"
)

// KBConnectorService 连接器服务
type KBConnectorService struct {
	kv      repository.SystemConfigKVRepository
	httpCli *http.Client
}

// NewKBConnectorService 构造
func NewKBConnectorService() *KBConnectorService {
	return &KBConnectorService{
		kv: repository.NewSystemConfigKVRepository(),
		httpCli: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

var kbConnectorSources = map[string]bool{
	"notion": true, "feishu": true, "dingtalk": true, "crm": true,
}

var kbConnectorSecretFields = []string{"token", "secret", "app_secret", "api_key", "password"}

func connectorKVKey(source string) string { return fmt.Sprintf("kb_connector.%s", source) }

// SaveConnectorRequest 保存凭据请求
type SaveConnectorRequest struct {
	Enabled bool           `json:"enabled"`
	Config  map[string]any `json:"config"`
}

// ConnectorStatus 连接器状态（读取视图：密钥脱敏）
type ConnectorStatus struct {
	Source      string         `json:"source"`
	Name        string         `json:"name"`
	Enabled     bool           `json:"enabled"`
	Configured  bool           `json:"configured"`
	Config      map[string]any `json:"config"`
	LastTestAt  *string        `json:"last_test_at,omitempty"`
	LastTestOK  *bool          `json:"last_test_ok,omitempty"`
	LastTestMsg string         `json:"last_test_msg,omitempty"`
}

var kbConnectorNames = map[string]string{
	"notion": "Notion", "feishu": "飞书", "dingtalk": "钉钉", "crm": "自定义 CRM Webhook",
}

// List 列出全部连接器状态
func (s *KBConnectorService) List(ctx context.Context) []ConnectorStatus {
	out := make([]ConnectorStatus, 0, len(kbConnectorSources))
	for source := range kbConnectorSources {
		out = append(out, s.Get(ctx, source))
	}
	return out
}

// Get 单个连接器状态（凭据脱敏）
func (s *KBConnectorService) Get(ctx context.Context, source string) ConnectorStatus {
	st := ConnectorStatus{Source: source, Name: kbConnectorNames[source]}
	raw, err := s.kv.Get(ctx, connectorKVKey(source))
	if err != nil || raw == "" {
		return st
	}
	var saved SaveConnectorRequest
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return st
	}
	st.Enabled = saved.Enabled
	st.Configured = len(saved.Config) > 0
	st.Config = maskConnectorConfig(saved.Config)

	var testRes struct {
		At  string `json:"at"`
		OK  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(raw), &testRes); err == nil && testRes.At != "" {
		st.LastTestAt = &testRes.At
		st.LastTestOK = &testRes.OK
		st.LastTestMsg = testRes.Msg
	}
	return st
}

func (s *KBConnectorService) Save(ctx context.Context, source string, req *SaveConnectorRequest) error {
	if !kbConnectorSources[source] {
		return fmt.Errorf("不支持的连接器: %s", source)
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	if raw, err := s.kv.Get(ctx, connectorKVKey(source)); err == nil && raw != "" {
		var prev SaveConnectorRequest
		if json.Unmarshal([]byte(raw), &prev) == nil && prev.Config != nil {
			for k, v := range prev.Config {
				lower := strings.ToLower(k)
				isSecret := false
				for _, sf := range kbConnectorSecretFields {
					if strings.Contains(lower, sf) {
						isSecret = true
						break
					}
				}
				if !isSecret {
					continue
				}
				cur, ok := req.Config[k]
				if !ok {
					req.Config[k] = v
				} else if sv, isStr := cur.(string); isStr && strings.TrimSpace(sv) == "" {
					req.Config[k] = v
				}
			}
		}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = s.kv.Upsert(ctx, connectorKVKey(source), string(raw))
	return err
}

// TestResult 连通测试结果
type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Latency int64  `json:"latency_ms"`
}

// Test 连通性测试（各源官方探测端点；测试结果连同时间戳写回 KV）
func (s *KBConnectorService) Test(ctx context.Context, source string) (*TestResult, error) {
	if !kbConnectorSources[source] {
		return nil, fmt.Errorf("不支持的连接器: %s", source)
	}
	raw, err := s.kv.Get(ctx, connectorKVKey(source))
	if err != nil || raw == "" {
		return nil, fmt.Errorf("连接器 %s 未配置凭据", source)
	}
	var saved SaveConnectorRequest
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return nil, fmt.Errorf("凭据损坏，请重新保存")
	}
	res := s.probe(ctx, source, saved.Config)

	merged, _ := json.Marshal(struct {
		SaveConnectorRequest
		TestAt  string `json:"test_at"`
		TestOK  bool   `json:"test_ok"`
		TestMsg string `json:"test_msg"`
	}{saved, time.Now().Format(time.RFC3339), res.OK, res.Message})
	_, _ = s.kv.Upsert(ctx, connectorKVKey(source), string(merged))
	return res, nil
}

func (s *KBConnectorService) probe(ctx context.Context, source string, cfg map[string]any) *TestResult {
	start := time.Now()
	res := &TestResult{}
	str := func(k string) string { v, _ := cfg[k].(string); return strings.TrimSpace(v) }

	var url, method string
	var headers map[string]string
	var body string
	switch source {
	case "notion":
		if str("token") == "" {
			res.Message = "缺少 token"
			return res
		}
		url, method = "https://api.notion.com/v1/users/me", http.MethodGet
		headers = map[string]string{"Authorization": "Bearer " + str("token"), "Notion-Version": "2022-06-28"}
	case "feishu":
		if str("app_id") == "" || str("app_secret") == "" {
			res.Message = "缺少 app_id / app_secret"
			return res
		}
		url, method = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal", http.MethodPost
		body = fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, str("app_id"), str("app_secret"))
		headers = map[string]string{"Content-Type": "application/json"}
	case "dingtalk":
		if str("app_key") == "" || str("app_secret") == "" {
			res.Message = "缺少 app_key / app_secret"
			return res
		}
		url = fmt.Sprintf("https://oapi.dingtalk.com/gettoken?appkey=%s&appsecret=%s", str("app_key"), str("app_secret"))
		method = http.MethodGet
	case "crm":
		if str("webhook_url") == "" {
			res.Message = "缺少 webhook_url"
			return res
		}
		if !strings.HasPrefix(str("webhook_url"), "https://") && !strings.HasPrefix(str("webhook_url"), "http://") {
			res.Message = "webhook_url 必须以 http(s):// 开头"
			return res
		}
		url, method = str("webhook_url"), http.MethodGet
		headers = map[string]string{}
		if str("api_key") != "" {
			headers["Authorization"] = "Bearer " + str("api_key")
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		res.Message = "请求构建失败: " + err.Error()
		return res
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		res.Message = "网络不可达: " + err.Error()
		return res
	}
	defer func() { _ = resp.Body.Close() }()
	res.Latency = time.Since(start).Milliseconds()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		res.OK = true
		res.Message = fmt.Sprintf("连接成功 (HTTP %d)", resp.StatusCode)
	} else {
		res.Message = fmt.Sprintf("远端返回 HTTP %d", resp.StatusCode)
	}
	return res
}

func maskConnectorConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		lower := strings.ToLower(k)
		isSecret := false
		for _, sf := range kbConnectorSecretFields {
			if strings.Contains(lower, sf) {
				isSecret = true
				break
			}
		}
		if isSecret {
			if sv, ok := v.(string); ok && len(sv) > 4 {
				out[k] = "****" + sv[len(sv)-4:]
				continue
			}
			out[k] = "****"
			continue
		}
		out[k] = v
	}
	return out
}
