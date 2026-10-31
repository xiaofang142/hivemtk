// Package service — custom_tool_service.go P1-2：OpenAPI 导入服务层。
// 职责：校验（SSRF 防线在 tooluse.ValidateOpenAPIBaseURL）→ 加密落凭据 →
// 注册 Provider 进全局注册中心（自动进入装饰链接线）→ 列表/删除。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	bizerr "hivemtk-user/internal/domain/errors"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// CustomToolService OpenAPI 自定义工具服务。
// 2026-10-25：DB 访问下沉 repository（L3 铁律，P1-2 收口）。
type CustomToolService struct {
	repo *repository.CustomToolRepository
}

// NewCustomToolService 构造。
func NewCustomToolService() *CustomToolService {
	return &CustomToolService{repo: repository.NewCustomToolRepository()}
}

// ImportInput 导入请求。
type ImportInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	BaseURL     string `json:"base_url"`
	SchemaJSON  string `json:"schema_json"`
	AuthHeader  string `json:"auth_header"`
	AuthValue   string `json:"auth_value"`
	TimeoutMS   int    `json:"timeout_ms"`
}

// Import 解析 schema、SSRF 校验、落库并注册 Provider。返回导入的工具数。
func (s *CustomToolService) Import(ctx context.Context, in ImportInput) (int, *model.CustomTool, error) {
	if strings.TrimSpace(in.Name) == "" {
		return 0, nil, bizerr.New(bizerr.CodeParamInvalid, "name 不能为空")
	}
	provider, err := tooluse.NewOpenAPIToolProvider(tooluse.OpenAPIImportSpec{
		Name: in.Name, Description: in.Description, BaseURL: in.BaseURL,
		SchemaJSON: in.SchemaJSON, AuthHeader: in.AuthHeader, AuthValue: in.AuthValue,
		TimeoutMS: in.TimeoutMS,
	})
	if err != nil {
		// schema/必填项问题 ⇒ 400（原文已是中文可读原因）
		return 0, nil, bizerr.New(bizerr.CodeParamInvalid, err.Error())
	}
	// SSRF：导入时校验一次（运行时转发由 client 直连该基址，导入即锁定）
	if err := tooluse.ValidateOpenAPIBaseURL(in.BaseURL); err != nil {
		return 0, nil, bizerr.New(bizerr.CodeParamInvalid, err.Error())
	}

	// 去重
	if _, err := s.repo.FindByName(ctx, in.Name); err == nil {
		return 0, nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("工具集 %q 已存在，请先删除再导入", in.Name))
	}

	rec := &model.CustomTool{
		Name: in.Name, Description: in.Description, BaseURL: in.BaseURL,
		AuthHeader: in.AuthHeader, SchemaJSON: in.SchemaJSON,
		TimeoutMS: in.TimeoutMS, IsActive: true,
	}
	if in.AuthValue != "" {
		rec.AuthValueEnc = dbencrypt.Encrypt(in.AuthValue)
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		// 并发同名时 FindByName 拦不住，唯一索引兜底；不要把 PG 原文回给前端。
		if repository.IsDuplicateKeyErr(err) {
			return 0, nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("工具集 %q 已存在，请先删除再导入", in.Name))
		}
		return 0, nil, errors.New("保存工具集失败")
	}

	// 即时注册：导入成功后本进程立刻可调。**重启后的恢复不靠这里**——
	// 自注册切片是进程内的，重启即空；启动期由 tooluse.LoadCustomToolProviders
	// 从 custom_tools 表重建（装配点见 app/tool_provider_wiring.go）。
	tooluse.RegisterToolProvider(provider)
	// 登记到**当前租户的叠加层**（不是全局注册表）：多租户下工具是租户级资源，
	// 进全局会让其他租户的 Agent 也能调到它（见 tooluse/tenant_registry.go）。
	ownerTenant := tenant.WriteTenantID(ctx)
	if tools, perr := provider.Provide(tooluse.ProviderContext{}); perr == nil {
		for _, t := range tools {
			if err := tooluse.RegisterTenantTool(ownerTenant, t); err != nil {
				logger.Ctx(ctx).Warn().Err(err).Str("tool", t.Name()).Msg("[custom-tool] 工具登记失败")
			}
		}
	}
	n := countOps(in.SchemaJSON)
	_ = s.repo.UpdateToolCount(ctx, rec.ID, n)
	rec.ToolCount = n
	logger.Ctx(ctx).Info().Str("name", in.Name).Int("tools", n).Msg("[custom-tool] OpenAPI 工具集已导入并注册")
	return n, rec, nil
}

// List 导入记录列表（不含凭据明文/密文）。
// 隔离由 tenancyidx 全局 callback 负责（见 mcp_client_service.List 同注），不手写过滤。
func (s *CustomToolService) List(ctx context.Context) ([]*model.CustomTool, error) {
	return s.repo.List(ctx)
}

// Delete 删除记录。已注册工具的卸载交给重启装配（tooluse.UnregisterProvider
// 注释口径：注销 Provider 不删除已注册工具，同步清理由调用方处理；重启后
// auto_register 列表已无此项，自然收敛）。
func (s *CustomToolService) Delete(ctx context.Context, id uint) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return bizerr.New(bizerr.CodeNotFound, fmt.Sprintf("工具集不存在（id=%d）", id))
		}
		return errors.New("删除工具集失败")
	}
	return nil
}

// countOps 统计 schema 中的操作数（ToolCount 展示用）。
func countOps(schemaJSON string) int {
	ops, err := tooluse.ParseOpenAPISchema(schemaJSON)
	if err != nil {
		return 0
	}
	return len(ops)
}
