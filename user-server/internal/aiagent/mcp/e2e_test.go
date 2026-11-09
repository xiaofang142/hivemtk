package mcp

// e2e_test.go P1-3 验收：真 HTTP 往返的「发现 → 注册 → 经 ToolExecutor 调用」
// ＋「不可达 server 只跳过自己不拖垮别人」。
//
// 被测面是真实协议面：httptest 里跑的是**本仓 mcp.Server**（真实 JSON-RPC 实现），
// 客户端走真实 HTTP + JSON-RPC；不是 mock 掉网络层——AC 说的是「tools/list 发现并
// 可在 Agent 调用链中执行」，mock 掉传输就等于没验协议。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// echoTool 最小可调用工具：回显参数 + 名字。
type echoTool struct{}

func (e *echoTool) Name() string                   { return "echo" }
func (e *echoTool) Category() tooluse.ToolCategory { return tooluse.CategoryBusiness }
func (e *echoTool) Description() string            { return "回显文本（测试用）" }
func (e *echoTool) Parameters() tooluse.ToolParameters {
	return tooluse.ToolParameters{
		Type:       "object",
		Properties: map[string]tooluse.ToolParam{"text": {Type: "string", Description: "要回显的文本"}},
		Required:   []string{"text"},
	}
}
func (e *echoTool) Execute(_ context.Context, args map[string]any) (tooluse.ToolResult, error) {
	return tooluse.SuccessResult("echo", map[string]any{
		"echo": fmt.Sprintf("hello %v", args["text"]),
	}), nil
}

// newUpstreamMCPServer 起一个真 MCP server（本仓实现）挂在 httptest 上。
func newUpstreamMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	src := tooluse.NewToolRegistry()
	if err := src.Register(&echoTool{}); err != nil {
		t.Fatalf("源注册表注册失败: %v", err)
	}
	srv := NewServer(src)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := srv.HandleRequest(r.Context(), body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
}

// recordingLimiter 记录限流键（验证装饰器链真的包住了外部工具）。
type recordingLimiter struct{ keys []string }

func (l *recordingLimiter) Acquire(_ context.Context, key string) error {
	l.keys = append(l.keys, key)
	return nil
}

func TestE2E_DiscoverRegisterExecuteAndIsolation(t *testing.T) {
	withTestTransport(t)
	upstream := newUpstreamMCPServer(t)
	defer upstream.Close()

	db := testutil.NewTestDB(t, &model.MCPServer{})
	live := model.MCPServer{Name: "fs", Endpoint: upstream.URL, TimeoutSeconds: 5}
	if err := db.Create(&live).Error; err != nil {
		t.Fatalf("种 live 行失败: %v", err)
	}
	// gorm 列默认值会把零值 false 覆盖回 true，停用/启用显式 UPDATE（同 custom_tools 的坑）
	dead := model.MCPServer{Name: "dead", Endpoint: "http://127.0.0.1:1", TimeoutSeconds: 2}
	if err := db.Create(&dead).Error; err != nil {
		t.Fatalf("种 dead 行失败: %v", err)
	}

	reg := tooluse.NewToolRegistry()
	servers, tools := LoadConfiguredServers(context.Background(), db, reg)
	if servers != 1 || tools != 1 {
		t.Fatalf("接入 server=%d tools=%d want 1/1（不可达 server 必须被跳过而非拖垮整批）", servers, tools)
	}

	// AC②：不可达 server 只在自己那行留 error，不影响已接入的工具
	var deadRow model.MCPServer
	if err := db.First(&deadRow, dead.ID).Error; err != nil {
		t.Fatalf("读 dead 行失败: %v", err)
	}
	if deadRow.LastStatus != "error" || deadRow.LastError == "" {
		t.Errorf("dead 行应记 error：status=%q err=%q", deadRow.LastStatus, deadRow.LastError)
	}

	toolName := "mcp_fs_echo"
	if _, err := reg.Get(toolName); err != nil {
		t.Fatalf("发现的工具 %s 未注册: %v", toolName, err)
	}

	// AC①：经 ToolExecutor（Agent 调用链的真实执行面）调用，且过审计+限流装饰器
	audit := tooluse.NewMemoryAuditLogger(100)
	limiter := &recordingLimiter{}
	exec := tooluse.NewToolExecutor(reg, tooluse.ToolExecutorConfig{
		DefaultTimeout: 5 * time.Second,
		AuditLogger:    audit,
		RateLimiter:    limiter,
	})
	res := exec.Execute(context.Background(), tooluse.ExecuteRequest{
		ToolName: toolName,
		Args:     map[string]any{"text": "world"},
	})
	if res.Err != nil || !res.Success {
		t.Fatalf("经 executor 调用失败: err=%v result=%+v", res.Err, res)
	}
	if !strings.Contains(fmt.Sprint(res.Data), "hello world") {
		t.Errorf("回显内容不对: %v", res.Data)
	}
	if len(limiter.keys) == 0 || limiter.keys[0] != toolName {
		t.Errorf("限流装饰器未按工具名开火: %v", limiter.keys)
	}
	if audit.Count() == 0 {
		t.Error("审计装饰器未记录调用")
	}
}

func TestE2E_CircuitBreakerOnDeadServer(t *testing.T) {
	withTestTransport(t)
	db := testutil.NewTestDB(t, &model.MCPServer{})
	dead := model.MCPServer{Name: "gone", Endpoint: "http://127.0.0.1:1", TimeoutSeconds: 1, FailLimit: 1, CooldownSeconds: 30}
	if err := db.Create(&dead).Error; err != nil {
		t.Fatalf("种行失败: %v", err)
	}
	client := NewClient(ServerConfigFromRow(&dead))
	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatal("不可达 server 应报错")
	}
	// FailLimit=1 ⇒ 第一次失败后进入冷却；第二次直接熔断报错（不再发起网络请求）
	_, err := client.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "熔断") {
		t.Errorf("第二次调用应命中熔断，实际: %v", err)
	}
}
