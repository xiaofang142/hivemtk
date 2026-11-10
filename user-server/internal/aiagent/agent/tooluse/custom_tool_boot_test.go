package tooluse

// custom_tool_boot_test.go P1-2 验收：重启后导入的工具必须还在。
//
// 缺陷形态（本文件存在的理由）：注册只写在进程内切片里，重启为空，
// 而 custom_tools 行还在 ⇒ 前端「已导入」、Agent 调不到。这里把
// 「表里有行」→「LoadCustomToolProviders 重建出可用工具」这条腿钉住。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/secrets"
)

const bootTestSchema = `{
  "openapi": "3.0.0",
  "info": {"title": "inventory", "version": "1.0.0"},
  "servers": [{"url": "https://erp.example.com"}],
  "paths": {
    "/stock": {
      "get": {
        "operationId": "getStock",
        "summary": "查询库存",
        "parameters": [{"name": "sku", "in": "query", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

func ensureBootSecrets(t *testing.T) {
	t.Helper()
	const testKey = "0123456789abcdef0123456789abcdef"
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", testKey)
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatalf("secrets.InitFromEnv 失败: %v", err)
	}
	t.Cleanup(secrets.ResetForTest)
}

func TestLoadCustomToolProviders_RestoresFromTable(t *testing.T) {
	ensureBootSecrets(t)
	db := testutil.NewTestDB(t, &model.CustomTool{})

	authPlain := "Bearer sk-test-123"
	row := model.CustomTool{
		Name: "erp", Description: "ERP 工具集", BaseURL: "https://erp.example.com",
		AuthHeader: "Authorization", AuthValueEnc: dbencrypt.Encrypt(authPlain),
		SchemaJSON: bootTestSchema, TimeoutMS: 5000, IsActive: true, ToolCount: 1,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("种行失败: %v", err)
	}
	// 凭据不明文（P1-2 AC③）
	if !strings.HasPrefix(row.AuthValueEnc, "enc:v1:") {
		t.Fatalf("落库凭据未加密: %q", row.AuthValueEnc)
	}
	if strings.Contains(row.AuthValueEnc, "sk-test-123") {
		t.Fatal("密文里出现明文片段")
	}

	providers := LoadCustomToolProviders(db)
	if len(providers) != 1 {
		t.Fatalf("重建 Provider 数 = %d want 1", len(providers))
	}
	if providers[0].Name() != "openapi_erp" {
		t.Errorf("Provider 名 = %q want openapi_erp", providers[0].Name())
	}
	tools, err := providers[0].Provide(ProviderContext{})
	if err != nil || len(tools) != 1 {
		t.Fatalf("工具数 = %d err=%v want 1", len(tools), err)
	}

	// 解密腿：上游必须收到解出来的鉴权头（用 httptest 冒充上游）。
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stock": 42}`))
	}))
	defer srv.Close()
	// 重建一个指向本地假上游的 Provider（BaseURL 换掉，其余沿用行内字段）
	local, err := NewOpenAPIToolProvider(OpenAPIImportSpec{
		Name: "erp", BaseURL: srv.URL, SchemaJSON: bootTestSchema,
		AuthHeader: row.AuthHeader, AuthValue: dbencrypt.Decrypt(row.AuthValueEnc),
	})
	if err != nil {
		t.Fatalf("构造本地 Provider 失败: %v", err)
	}
	localTools, _ := local.Provide(ProviderContext{})
	// MA-01：生产 client 挂 GuardedTransport（拨号期拒环回），解密腿验证用
	// httptest 假上游，同包直接把测试客户端换回去（SSRF 闸在位由
	// TestOpenAPITool_ExecuteRefusesPrivateDial 钉死，不在本用例重复验证）。
	if ot, ok := localTools[0].(*openAPITool); ok {
		ot.client = srv.Client()
	}
	if _, err := localTools[0].Execute(context.Background(), map[string]any{"sku": "A-1"}); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if gotAuth != authPlain {
		t.Errorf("上游收到 Authorization = %q want %q（解密链断了）", gotAuth, authPlain)
	}
}

func TestLoadCustomToolProviders_SkipsBrokenRowKeepsGood(t *testing.T) {
	db := testutil.NewTestDB(t, &model.CustomTool{})
	good := model.CustomTool{Name: "good", BaseURL: "https://a.example.com", SchemaJSON: bootTestSchema, IsActive: true}
	bad := model.CustomTool{Name: "bad", BaseURL: "https://b.example.com", SchemaJSON: "{not json", IsActive: true}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("种好行失败: %v", err)
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("种坏行失败: %v", err)
	}
	// 停用行必须建完再显式 UPDATE：gorm 列上有 default:true，结构体零值 false
	// 会被默认值覆盖，直接 Create 落库仍是 true（实测踩过）——那会让本用例
	// 的"停用行被跳过"这条腿测不到。
	disabled := model.CustomTool{Name: "off", BaseURL: "https://c.example.com", SchemaJSON: bootTestSchema}
	if err := db.Create(&disabled).Error; err != nil {
		t.Fatalf("种停用行失败: %v", err)
	}
	if err := db.Model(&disabled).Update("is_active", false).Error; err != nil {
		t.Fatalf("停用失败: %v", err)
	}

	providers := LoadCustomToolProviders(db)
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	if len(providers) != 1 || providers[0].Name() != "openapi_good" {
		t.Fatalf("应只重建启用且可解析的 1 个 Provider，实际 %d 个: %v", len(providers), names)
	}
}
