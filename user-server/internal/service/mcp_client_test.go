package service

// mcp_client_service_test.go P1-3：配置面 CRUD 与隔离口径。
//
// 三格：
//  1. 建不可达 server：配置必须落库（运维有据可查）、last_status=error、不 panic；
//  2. 列表带 last_error 供排障（不含密钥字段——model 上 json:"-"）；
//  3. 多租户模式下列表只出本租户的行 —— 机制是 tenancyidx 全局 callback 的自动注入
//     （不是本服务手写的 Where：手写会掩盖"登记表漏了新表"），本格守的是
//     「mcp_servers 确实被隔离机制覆盖」。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func newMCPTestDB(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t, &model.MCPServer{})
}

func TestMCPClient_CreateUnreachableSavesRowWithError(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()
	// G-1 后 127.0.0.1 在 Create 期即被拒；这里改用公网 TEST-NET-1 文档地址（不可达但合法）
	row, n, err := svc.Create(context.Background(), MCPImportInput{
		Name: "gone", Endpoint: "http://192.0.2.1:1", TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("配置应落库成功（不可达只影响发现）: %v", err)
	}
	if row == nil || row.ID == 0 {
		t.Fatalf("行未落库: %+v", row)
	}
	if n != 0 {
		t.Errorf("不可达 server 工具数 = %d want 0", n)
	}
	rows, err := svc.List(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatalf("列表 = %d err=%v want 1", len(rows), err)
	}
	if rows[0].LastStatus != "error" || rows[0].LastError == "" {
		// TEST-NET-1 不可达错误文关怀环境（timeout/unreachable/refused），只钉非空
		t.Errorf("应记连接错误：status=%q err=%q", rows[0].LastStatus, rows[0].LastError)
	}
}

func TestMCPClient_CreateRejectsDuplicate(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()
	if _, _, err := svc.Create(context.Background(), MCPImportInput{Name: "dup", Endpoint: "http://192.0.2.1:1", TimeoutSeconds: 1}); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, _, err := svc.Create(context.Background(), MCPImportInput{Name: "dup", Endpoint: "http://192.0.2.1:1", TimeoutSeconds: 1}); err == nil {
		t.Fatal("重名应拒绝")
	}
}

func TestMCPClient_ListTenantScopedInMultiTenantMode(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()

	// 直插两行（绕过 Create 的网络发现）：租户 1 与租户 2 各一条
	db := testutil.NewTestDB(t, &model.MCPServer{})
	if err := db.Create(&model.MCPServer{TenantID: 1, Name: "t1", Endpoint: "http://127.0.0.1:1"}).Error; err != nil {
		t.Fatalf("种 t1 失败: %v", err)
	}
	if err := db.Create(&model.MCPServer{TenantID: 2, Name: "t2", Endpoint: "http://127.0.0.1:1"}).Error; err != nil {
		t.Fatalf("种 t2 失败: %v", err)
	}

	// 单机模式（默认 off）：不过滤，两条都出
	rows, err := svc.List(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("单机模式应看到 2 行，实际 %d err=%v", len(rows), err)
	}

	// 多租户模式 + ctx 绑定租户 2：只出 t2（由 callback 注入，服务层无过滤代码）
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	ctx2 := tenant.WithTenantID(context.Background(), 2)
	rows, err = svc.List(ctx2)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "t2" {
		names := make([]string, 0, len(rows))
		for _, r := range rows {
			names = append(names, r.Name)
		}
		t.Fatalf("租户 2 只应看到 t2，实际 %v", names)
	}
}

// —— G-1 stored-SSRF 防线：配置期三入口（Create/SetEnabled/Test）统一拒绝内网端点 ——

func TestMCPClient_CreateRejectsInternalEndpoint(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()
	for _, raw := range []string{
		"http://127.0.0.1:1", "http://localhost:1", "http://192.168.1.1",
		"http://169.254.169.254/latest/meta-data", "http://100.64.0.1",
	} {
		_, _, err := svc.Create(context.Background(), MCPImportInput{Name: "ssrf-" + raw, Endpoint: raw})
		if err == nil || !strings.Contains(err.Error(), "SSRF") {
			t.Errorf("内网端点 %q 应拒绝且报 SSRF，实际 err=%v", raw, err)
		}
	}
	rows, err := svc.List(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("拒绝的用例不应落库，实际 %d 行 err=%v", len(rows), err)
	}
}

func TestMCPClient_SetEnabledRejectsLegacyInternalRow(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()
	db := testutil.NewTestDB(t, &model.MCPServer{})
	if err := db.Create(&model.MCPServer{TenantID: 1, Name: "legacy", Endpoint: "http://[::1]:8080"}).Error; err != nil {
		t.Fatalf("种 legacy 行失败: %v", err)
	}
	// 显式置停用：Enabled 带 default:true 且零值省略，Create 无法表达"禁用出生"，
	// 必须用 Update 落 false，否则"拒绝后保持停用"断言失去前提。
	if err := db.Model(&model.MCPServer{}).Where("name = ?", "legacy").Update("enabled", false).Error; err != nil {
		t.Fatalf("置 legacy 行为停用失败: %v", err)
	}
	row, err := svc.List(context.Background())
	if err != nil || len(row) != 1 {
		t.Fatalf("种品行应可列出: %+v err=%v", row, err)
	}
	if row[0].Enabled {
		t.Fatalf("种子行应为停用态（Update 置 false 后），实际 enabled=%v", row[0].Enabled)
	}
	if _, _, err := svc.SetEnabled(context.Background(), row[0].ID, true); err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("启用内网 legacy 行应拒绝，实际 err=%v", err)
	}
	after, err := svc.List(context.Background())
	if err != nil || after[0].Enabled {
		t.Fatalf("拒绝后行应保持停用: %+v", after)
	}
}

func TestMCPClient_TestRejectsInternalEndpoint(t *testing.T) {
	newMCPTestDB(t)
	svc := NewMCPClientService()
	db := testutil.NewTestDB(t, &model.MCPServer{})
	if err := db.Create(&model.MCPServer{TenantID: 1, Name: "t-ssrf", Endpoint: "http://10.0.0.1:9200"}).Error; err != nil {
		t.Fatalf("种行失败: %v", err)
	}
	row, err := svc.List(context.Background())
	if err != nil || len(row) != 1 {
		t.Fatalf("种品行应可列出: %+v err=%v", row, err)
	}
	if _, err := svc.Test(context.Background(), row[0].ID); err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("Test 内网端点应拒绝，实际 err=%v", err)
	}
}
