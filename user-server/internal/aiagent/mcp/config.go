package mcp

// config.go P1-3：外部 MCP server 的配置面（表 mcp_servers）与启动期发现。
//
// 与 OpenAPI 工具集共用同一治理面：发现出来的工具进同一个 ToolRegistry，
// 经同一个 ToolExecutor 装配装饰器链（审计/限流/权限/熔断）——这是「声明式
// 接入双通道」的实质：两条接入方式，一条治理链。

import (
	"context"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	dbpkg "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// ServerConfigFromRow 行 → 连接配置（密钥解密；空值走 Client 默认）。
func ServerConfigFromRow(row *model.MCPServer) ClientConfig {
	cfg := ClientConfig{
		Name:      row.Name,
		Endpoint:  row.Endpoint,
		APIKey:    dbencrypt.Decrypt(row.APIKeyEnc),
		FailLimit: row.FailLimit,
	}
	if row.TimeoutSeconds > 0 {
		cfg.Timeout = time.Duration(row.TimeoutSeconds) * time.Second
	}
	if row.CooldownSeconds > 0 {
		cfg.Cooldown = time.Duration(row.CooldownSeconds) * time.Second
	}
	return cfg
}

// DiscoverAndRegisterRowDefaultDB 用全局默认句柄 + 全局注册表的变体。
//
// 2026-10-25：service 层不得取 DB 句柄（架构 L3），repository 包又不能反向依赖
// tooluse（会与 business_tools 的 portcontract→repository 成环），而「发现并落库」
// 本来就住在本包——所以默认句柄的解析也留在这里，调用方只传行。
func DiscoverAndRegisterRowDefaultDB(ctx context.Context, row *model.MCPServer) (int, error) {
	// 登记到该 server 所属租户的叠加层（多租户隔离），而不是全局注册表。
	return DiscoverAndRegisterRow(ctx, dbpkg.GetDB(), row, tooluse.EnsureTenantRegistry(row.TenantID))
}

// DiscoverAndRegisterRow 发现一个已配置 server 并登记结果（tool_count/last_status）。
// 返回注册成功的工具数；server 不可达只记错误、不阻断调用方。
func DiscoverAndRegisterRow(ctx context.Context, db *gorm.DB, row *model.MCPServer, registry *tooluse.ToolRegistry) (int, error) {
	if row == nil {
		return 0, nil
	}
	n, err := DiscoverAndRegister(ctx, ServerConfigFromRow(row), registry)
	now := time.Now()
	updates := map[string]any{"last_synced_at": &now, "tool_count": n}
	if err != nil {
		updates["last_status"] = "error"
		updates["last_error"] = truncateStr(err.Error(), 500)
	} else {
		updates["last_status"] = "ok"
		updates["last_error"] = ""
	}
	if db != nil {
		_ = db.WithContext(ctx).Model(&model.MCPServer{}).Where("id = ?", row.ID).Updates(updates).Error
	}
	return n, err
}

// LoadConfiguredServers 启动期：把所有启用中的 MCP server 发现并注册。
//
// 隔离口径（AC「Server 不可达时熔断降级不影响其他工具」的装配侧）：
// 单个 server 的发现失败只跳过它自己，其余 server 与内置工具照常装配。
func LoadConfiguredServers(ctx context.Context, gormDB *gorm.DB, registry *tooluse.ToolRegistry) (servers int, tools int) {
	if gormDB == nil || registry == nil {
		return 0, 0
	}
	var rows []model.MCPServer
	if err := gormDB.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").Find(&rows).Error; err != nil {
		logger.Warnf("[mcp-client] 读取 mcp_servers 失败（外部 MCP 工具不可用，其余工具不受影响）: %v", err)
		return 0, 0
	}
	for i := range rows {
		row := &rows[i]
		n, err := DiscoverAndRegisterRow(ctx, gormDB, row, registry)
		if err != nil {
			logger.Warnf("[mcp-client] server %q 发现失败（跳过该 server，其余工具不受影响）: %v", row.Name, err)
			continue
		}
		servers++
		tools += n
		logger.Infof("[mcp-client] server %q 已接入 %d 个工具", row.Name, n)
	}
	return servers, tools
}
