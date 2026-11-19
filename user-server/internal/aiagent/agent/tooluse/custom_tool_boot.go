package tooluse

// custom_tool_boot.go P1-2：custom_tools 表的启动期重建。
//
// 为什么必须有这一层：`RegisterToolProvider` 只写进程内切片（auto_register.go），
// 进程重启后为空。导入的 OpenAPI 工具若只靠导入那一刻的注册，重启即全部消失，
// 而 `custom_tools` 行还在——前端列表显示「已导入」，Agent 却调不到，
// 这是最容易骗过验收的形态。本文件把「表里有行」与「注册中心里有工具」
// 在每次启动时重新对齐。

import (
	"context"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/toolruntime"

	"gorm.io/gorm"
)

// LoadCustomToolProviders 读取全部启用中的 custom_tools 行并重建 Provider。
//
// 容错口径：单行失败（schema 损坏/解密异常）只跳过该行并记日志，不影响其余行——
// 一个坏工具集不该让整条工具链装配失败（那会把 Agent 的所有工具一起带走）。
func LoadCustomToolProviders(gormDB *gorm.DB, authority toolruntime.Authority) []ToolProvider {
	if gormDB == nil {
		return nil
	}
	var rows []model.CustomTool
	if err := gormDB.Where("is_active = ?", true).Order("id ASC").Find(&rows).Error; err != nil {
		logger.Warnf("[custom-tool] 启动加载失败（工具集将不可用，其余工具不受影响）: %v", err)
		return nil
	}
	out := make([]ToolProvider, 0, len(rows))
	for i := range rows {
		p, err := CustomToolRowToProvider(&rows[i], authority)
		if err != nil {
			logger.Warnf("[custom-tool] 跳过工具集 %q(id=%d): %v", rows[i].Name, rows[i].ID, err)
			continue
		}
		out = append(out, p)
	}
	if len(out) > 0 {
		logger.Infof("[custom-tool] 启动期重建 %d 个 OpenAPI 工具集（共 %d 行）", len(out), len(rows))
	}
	return out
}

// CustomToolRowToProvider 把一行 custom_tools 转成 Provider（导入与启动重建共用）。
// AuthValueEnc 经 dbencrypt.Decrypt 还原（明文存量行原样返回，见 dbencrypt 头注释）。
func CustomToolRowToProvider(row *model.CustomTool, authority toolruntime.Authority) (ToolProvider, error) {
	if row == nil {
		return nil, fmt.Errorf("row 为空")
	}
	if row.SchemaJSON == "" {
		return nil, fmt.Errorf("schema_json 为空（历史行无法重建，请重新导入）")
	}
	provider, err := NewOpenAPIToolProvider(OpenAPIImportSpec{
		Name:        row.Name,
		Description: row.Description,
		BaseURL:     row.BaseURL,
		SchemaJSON:  row.SchemaJSON,
		AuthHeader:  row.AuthHeader,
		AuthValue:   dbencrypt.Decrypt(row.AuthValueEnc),
		TimeoutMS:   row.TimeoutMS,
	})
	if err != nil {
		return nil, err
	}
	tools, err := provider.Provide(ProviderContext{})
	if err != nil {
		return nil, err
	}
	expected := *row
	for _, tool := range tools {
		if operation, ok := tool.(*openAPITool); ok {
			operation.admit = func(ctx context.Context, name string) error {
				if authority == nil {
					return toolruntime.ErrUnavailable
				}
				return authority.CheckCustomTool(ctx, expected, name)
			}
		}
	}
	return provider, nil
}
