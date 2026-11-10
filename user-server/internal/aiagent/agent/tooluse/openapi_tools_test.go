// openapi_tools_test.go P1-2：解析/命名/SSRF 防线/执行转发 单测。
package tooluse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/urlguard"
)

const petstoreMini = `{
  "openapi": "3.0.0",
  "paths": {
    "/pets/{petId}": {
      "get": {
        "operationId": "getPetById",
        "summary": "查询宠物",
        "parameters": [
          {"name": "petId", "in": "path", "required": true, "schema": {"type": "integer"}},
          {"name": "verbose", "in": "query", "schema": {"type": "boolean"}}
        ]
      },
      "delete": {
        "summary": "删除宠物",
        "parameters": [{"name": "petId", "in": "path", "required": true, "schema": {"type": "integer"}}]
      }
    },
    "/pets": {
      "post": {"summary": "新建宠物", "requestBody": {"required": true}}
    }
  }
}`

func TestParseOpenAPISchema(t *testing.T) {
	ops, err := ParseOpenAPISchema(petstoreMini)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 {
		t.Fatalf("应解析出 3 个操作，实际 %d", len(ops))
	}
	byName := map[string]openAPIOperation{}
	for _, op := range ops {
		byName[op.ToolName] = op
	}
	if op, ok := byName["getpetbyid"]; !ok || op.Method != "get" || len(op.Params) != 2 {
		t.Errorf("getPetById 解析错: %+v", byName)
	}
	// 无 operationId 的 delete 落到 method+path 生成名
	if _, ok := byName["delete_pets_petid"]; !ok {
		t.Errorf("delete 应生成名 delete_pets_petid，实际 keys: %v", keys(byName))
	}
	// post 带 body
	if op := byName["post_pets"]; op.ToolName == "" || !op.HasBody {
		t.Errorf("post_pets 应存在且 HasBody: %+v", op)
	}
}

func TestParseOpenAPISchema_BadInput(t *testing.T) {
	if _, err := ParseOpenAPISchema("not json"); err == nil {
		t.Error("非法 JSON 应报错")
	}
	if _, err := ParseOpenAPISchema(`{"openapi":"3.0.0"}`); err == nil {
		t.Error("无 paths 应报错")
	}
}

func TestValidateOpenAPIBaseURL_SSRF(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1:8080/api",
		"http://localhost/api",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/api",
		"http://192.168.1.1/api",
		"file:///etc/passwd",
		"http://172.16.0.9/v1",
	}
	for _, u := range blocked {
		if err := ValidateOpenAPIBaseURL(u); err == nil {
			t.Errorf("应拒绝: %s", u)
		}
	}
}

func TestOpenAPITool_ExecutePathAndQuery(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ops, err := ParseOpenAPISchema(petstoreMini)
	if err != nil {
		t.Fatal(err)
	}
	var getOp openAPIOperation
	for _, op := range ops {
		if op.ToolName == "getpetbyid" {
			getOp = op
		}
	}
	tool := &openAPITool{op: getOp, spec: OpenAPIImportSpec{Name: "pets", BaseURL: srv.URL}, client: srv.Client()}
	res, err := tool.Execute(context.Background(), map[string]any{"petId": 42, "verbose": true})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "GET" || gotPath != "/pets/42" || !strings.Contains(gotQuery, "verbose=true") {
		t.Errorf("转发错: %s %s?%s", gotMethod, gotPath, gotQuery)
	}
	if res.Error != "" {
		t.Errorf("不应报错: %s", res.Error)
	}
}

func TestOpenAPITool_ExecuteQueryEncoding(t *testing.T) {
	var gotQuery, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotPath = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ops, err := ParseOpenAPISchema(petstoreMini)
	if err != nil {
		t.Fatal(err)
	}
	var getOp openAPIOperation
	for _, op := range ops {
		if op.ToolName == "getpetbyid" {
			getOp = op
		}
	}
	tool := &openAPITool{op: getOp, spec: OpenAPIImportSpec{Name: "pets", BaseURL: srv.URL}, client: srv.Client()}
	// G-2：值里带 & # 空格 = —— 修复前会把它们裸拼进 query，污染/注入上游参数
	if _, err := tool.Execute(context.Background(), map[string]any{"petId": 42, "verbose": "a&b=c d#e"}); err != nil {
		t.Fatal(err)
	}
	vals, err := url.ParseQuery(gotQuery)
	if err != nil {
		t.Fatalf("上游收到的 query 非法: %q", gotQuery)
	}
	if vals.Get("verbose") != "a&b=c d#e" {
		t.Errorf("query 值应原样往返，实际 %q (raw=%q)", vals.Get("verbose"), gotQuery)
	}
	// petId 是 path 参数（替换进 /pets/{petId}），不应出现在 query 里
	if gotPath != "/pets/42" {
		t.Errorf("path 参数应替换进路径，实际 %q", gotPath)
	}
	if len(vals) != 1 {
		t.Errorf("query 应只有 verbose 一个参数，实际 %v", vals)
	}
}

func TestOpenAPITool_ExecuteBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(201)
	}))
	defer srv.Close()

	tool := &openAPITool{op: openAPIOperation{ToolName: "post_pets", Method: "post", Path: "/pets", HasBody: true},
		spec: OpenAPIImportSpec{Name: "pets", BaseURL: srv.URL}, client: srv.Client()}
	if _, err := tool.Execute(context.Background(), map[string]any{"body": map[string]any{"name": "rex"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "rex") {
		t.Errorf("body 未转发: %q", gotBody)
	}
}

func TestOpenAPIToolProviderRegistration(t *testing.T) {
	p, err := NewOpenAPIToolProvider(OpenAPIImportSpec{
		Name: "Pets API", Description: "宠物服务", BaseURL: "https://api.example.com",
		SchemaJSON: petstoreMini,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openapi_pets_api" {
		t.Errorf("Provider 名: %q", p.Name())
	}
	tools, err := p.Provide(ProviderContext{})
	if err != nil || len(tools) != 3 {
		t.Fatalf("Provide: %v tools=%d", err, len(tools))
	}
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name(), "openapi.") {
			t.Errorf("工具名应带 openapi. 前缀: %s", tool.Name())
		}
		// 治理前提：参数 schema 非空
		if tool.Parameters().Type == "" {
			t.Errorf("%s 参数 schema 空", tool.Name())
		}
	}
}

func keys(m map[string]openAPIOperation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ===== MA-01：运行期 SSRF 第二层（GuardedTransport 拨号期复校）=====

// TestOpenAPIRunDialControlRejections 钉 urlguard.DialControl 的拒绝清单：
// 环回/云元数据/CGNAT 一律拒绝，公网形态放行。重定向跳内网与 DNS rebinding
// 最终都落到这一把拨号期闸门上（对每一次 dial 生效），本用例即该闸的在位证明。
func TestOpenAPIRunDialControlRejections(t *testing.T) {
	rejected := []string{
		"127.0.0.1:8080",
		"[::1]:8080",
		"169.254.169.254:80",
		"10.0.0.5:443",
		"192.168.1.10:443",
		"172.16.0.9:443",
		"100.64.0.7:443",
	}
	for _, addr := range rejected {
		if err := urlguard.DialControl("tcp", addr, nil); err == nil {
			t.Errorf("DialControl(%q) 应拒绝，实际放行", addr)
		}
	}
	// 公网形态：example.com 解析出的地址不在保留段（CI 无外网时该断言退化为
	// 解析失败也算放行路径以外的分支——故改用字面公网 IP，不做 DNS）。
	for _, addr := range []string{"93.184.216.34:443", "1.1.1.1:53"} {
		if err := urlguard.DialControl("tcp", addr, nil); err != nil {
			t.Errorf("DialControl(%q) 应放行公网字面地址，实际拒绝: %v", addr, err)
		}
	}
}

// TestOpenAPITool_BuildOpsClientIsGuarded 结构性断言：buildOps 的 client 挂了
// 带 DialContext（拨号期复校）的传输，且 Timeout 语义保留。
func TestOpenAPITool_BuildOpsClientIsGuarded(t *testing.T) {
	ops, err := ParseOpenAPISchema(petstoreMini)
	if err != nil {
		t.Fatal(err)
	}
	spec := OpenAPIImportSpec{Name: "pets", BaseURL: "https://api.example.com"}
	tools := buildOps(ops, spec, 3*time.Second)
	if len(tools) == 0 {
		t.Fatal("无工具")
	}
	tool := tools[0].(*openAPITool)
	if tool.client.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v want 3s", tool.client.Timeout)
	}
	tr, ok := tool.client.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatalf("client.Transport 应为带 DialContext 的守卫传输，实际 %T", tool.client.Transport)
	}
}

// TestOpenAPITool_ExecuteRefusesPrivateDial 负例（红转绿锚点）：修前裸
// http.Client 会把请求打到环回 httptest 上游并返回 200；修后拨号期闸门
// 必须在 Execute 阶段拒绝（模拟「导入时公网、调用时已指向内网」的
// rebinding/重定向终态）。
func TestOpenAPITool_ExecuteRefusesPrivateDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ops, err := ParseOpenAPISchema(petstoreMini)
	if err != nil {
		t.Fatal(err)
	}
	spec := OpenAPIImportSpec{Name: "pets", BaseURL: srv.URL}
	tool := buildOps(ops, spec, 3*time.Second)[0].(*openAPITool)
	_, err = tool.Execute(context.Background(), map[string]any{"petId": 42})
	if err == nil {
		t.Fatal("对环回上游的执行应被拨号期闸门拒绝（MA-01 运行期层未生效）")
	}
	if !strings.Contains(err.Error(), "拒绝") {
		t.Errorf("拒绝原因应来自 SSRF 复校，实际: %v", err)
	}
}
