package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 创建后内存时间精度高于PostgreSQL，合法来源仍应通过，真实代际变化仍拒绝。
func TestToolRuntime_CreatedAtDatabasePrecision(t *testing.T) {
	database := testutil.NewTestDB(t, &model.CustomTool{}, &model.MCPServer{}, &model.AIToolConfig{})
	ctx := tenant.WithTenantID(context.Background(), 1)
	repo := NewToolRuntimeRepositoryWithDB(database)
	created := time.Unix(1700000000, 123456789).UTC()
	custom := model.CustomTool{TenantID: 1, Name: "precision-custom", IsActive: true, BaseURL: "https://example.com", SchemaJSON: "{}", CreatedAt: created}
	mcp := model.MCPServer{TenantID: 1, Name: "precision-mcp", Enabled: true, Endpoint: "https://example.com", CreatedAt: created}
	if err := database.WithContext(ctx).Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(&mcp).Error; err != nil {
		t.Fatal(err)
	}

	if err := repo.CheckCustomTool(ctx, custom, "precision.custom"); err != nil {
		t.Fatalf("数据库微秒精度不应撤销合法自定义工具：%v", err)
	}
	if err := repo.CheckMCPServer(ctx, mcp, "precision.mcp"); err != nil {
		t.Fatalf("数据库微秒精度不应撤销合法MCP：%v", err)
	}
	custom.CreatedAt = custom.CreatedAt.Add(time.Microsecond)
	mcp.CreatedAt = mcp.CreatedAt.Add(time.Microsecond)
	if err := repo.CheckCustomTool(ctx, custom, "precision.custom"); !errors.Is(err, ErrToolRuntimeChanged) {
		t.Fatalf("真实自定义来源时间变化必须拒绝，实际%v", err)
	}
	if err := repo.CheckMCPServer(ctx, mcp, "precision.mcp"); !errors.Is(err, ErrToolRuntimeChanged) {
		t.Fatalf("真实MCP来源时间变化必须拒绝，实际%v", err)
	}
}

func TestToolRuntime_AdmissionFailureBoundaries(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	if err := NewToolRuntimeRepositoryWithDB(nil).CheckToolAdmission(ctx, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("缺少权威数据库必须拒绝: %v", err)
	}
	database := testutil.NewTestDB(t, &model.CustomTool{})
	repo := NewToolRuntimeRepositoryWithDB(database)
	// 独立指定不存在的表，不依赖同包前序测试是否已经迁移配置表。
	missingTableRepo := NewToolRuntimeRepositoryWithDB(database.Table("tool_runtime_config_missing_fixture"))
	if err := missingTableRepo.CheckToolAdmission(ctx, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("缺表不能等同正常无配置: %v", err)
	}
	if err := database.AutoMigrate(&model.AIToolConfig{}, &model.MCPServer{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckToolAdmission(ctx, "runtime.boundary"); err != nil {
		t.Fatalf("正常无配置应保留默认准入策略: %v", err)
	}
	if err := repo.CheckToolAdmission(nil, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("空上下文必须拒绝: %v", err)
	}
	mode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(mode) })
	if err := repo.CheckToolAdmission(context.Background(), "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("多租户缺上下文必须拒绝: %v", err)
	}
	custom := model.CustomTool{TenantID: 1, Name: "runtime-boundary", IsActive: true, BaseURL: "https://example.com", SchemaJSON: "{}"}
	mcp := model.MCPServer{TenantID: 1, Name: "runtime-boundary", Enabled: true, Endpoint: "https://example.com"}
	if err := database.WithContext(ctx).Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctx).Create(&mcp).Error; err != nil {
		t.Fatal(err)
	}
	other := tenant.WithTenantID(context.Background(), 2)
	if err := repo.CheckCustomTool(other, custom, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeRevoked) {
		t.Fatalf("自定义源跨租户必须拒绝: %v", err)
	}
	if err := repo.CheckMCPServer(other, mcp, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeRevoked) {
		t.Fatalf("MCP 源跨租户必须拒绝: %v", err)
	}
	faultTable := ""
	if err := database.Callback().Query().Before("gorm:query").Register("test:tool-runtime-source-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == faultTable {
			tx.AddError(context.DeadlineExceeded)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Callback().Query().Remove("test:tool-runtime-source-failure") })
	faultTable = "custom_tools"
	if err := repo.CheckCustomTool(ctx, custom, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("自定义来源查询失败必须拒绝: %v", err)
	}
	faultTable = "mcp_servers"
	if err := repo.CheckMCPServer(ctx, mcp, "runtime.boundary"); !errors.Is(err, ErrToolRuntimeUnavailable) {
		t.Fatalf("MCP 来源查询失败必须拒绝: %v", err)
	}
	faultTable = ""
	if err := repo.CheckCustomTool(ctx, custom, "runtime.boundary"); err != nil {
		t.Fatalf("查询恢复后应按真实源状态恢复: %v", err)
	}
	if err := repo.CheckMCPServer(ctx, mcp, "runtime.boundary"); err != nil {
		t.Fatalf("MCP 查询恢复后应恢复: %v", err)
	}
}
