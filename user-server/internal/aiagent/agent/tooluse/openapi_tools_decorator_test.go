package tooluse

// openapi_tools_decorator_test.go P1-2 AC②：导入的工具必须与内置工具同权治理。
//
// 判据走**真实执行面**（ToolExecutor → 装饰器链 → 上游 HTTP），不是直接调
// tool.Execute：直接调会绕过整条链，「自动套装饰器」这句承诺就无人验证。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const decoratorTestSchema = `{
  "openapi": "3.0.0",
  "info": {"title": "stock", "version": "1.0.0"},
  "paths": {
    "/stock/{sku}": {
      "get": {
        "operationId": "getStock",
        "summary": "查询库存",
        "parameters": [{"name": "sku", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

func TestOpenAPITool_ExecutesThroughDecoratorChain(t *testing.T) {
	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		if r.URL.Path != "/stock/A-1" {
			t.Errorf("上游路径 = %q want /stock/A-1（路径参数未替换）", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stock": 7}`))
	}))
	defer upstream.Close()

	// MA-01：buildOps 的生产客户端已挂 GuardedTransport（拨号期拒内网），
	// 对环回 httptest 上游执行必须注入测试客户端——本用例验证的是装饰器链
	// 与上游转发的接缝，不是 SSRF 闸（闸由 TestOpenAPITool_ExecuteRefusesPrivateDial
	// 与 TestOpenAPIRunDialControlRejections 承载），故同包直接构造 openAPITool。
	ops, err := ParseOpenAPISchema(decoratorTestSchema)
	if err != nil {
		t.Fatalf("解析 schema 失败: %v", err)
	}
	spec := OpenAPIImportSpec{Name: "stock", BaseURL: upstream.URL, TimeoutMS: 3000}
	tools := []Tool{&openAPITool{op: ops[0], spec: spec, client: upstream.Client()}}
	if len(tools) != 1 {
		t.Fatalf("工具数 = %d", len(tools))
	}

	reg := NewToolRegistry()
	for _, tl := range tools {
		if err := reg.Register(tl); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
	}
	audit := NewMemoryAuditLogger(10)
	limiter := &keyRecordingLimiter{}
	exec := NewToolExecutor(reg, ToolExecutorConfig{
		DefaultTimeout: 3 * time.Second,
		AuditLogger:    audit,
		RateLimiter:    limiter,
	})

	res := exec.Execute(context.Background(), ExecuteRequest{
		ToolName: tools[0].Name(),
		Args:     map[string]any{"sku": "A-1"},
	})
	if res.Err != nil || !res.Success {
		t.Fatalf("执行失败: err=%v result=%+v", res.Err, res)
	}
	if upstreamHits != 1 {
		t.Errorf("上游命中 %d 次 want 1（重试策略不得在成功后重发）", upstreamHits)
	}
	if got := limiter.snapshot(); len(got) != 1 || got[0] != tools[0].Name() {
		t.Errorf("限流装饰器未按工具名开火: %v", got)
	}
	if audit.Count() == 0 {
		t.Error("审计装饰器未记录本次调用")
	}
	entries := audit.Entries()
	if len(entries) > 0 && entries[len(entries)-1].ToolName != tools[0].Name() {
		t.Errorf("审计条目工具名 = %q want %q", entries[len(entries)-1].ToolName, tools[0].Name())
	}
}
