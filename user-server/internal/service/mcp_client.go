package service

// mcp_client.go P1-3：外部 MCP server 配置面（运营增删/测试/发现）。
//
// 保存即发现：与 OpenAPI 工具集同口径（导入即注册），避免「配好了但要重启才生效」。
// 工具进同一个 ToolRegistry ⇒ 自动过装饰器链；server 不可达只把该行标 error。
// 2026-10-25：DB 访问下沉 repository（L3 铁律），发现注册经 repo 包装，句柄不出仓储。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	bizerr "hivemtk-user/internal/domain/errors"

	"hivemtk-user/internal/aiagent/mcp"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// MCPClientService 外部 MCP server 管理。
type MCPClientService struct {
	repo *repository.MCPServerRepository
}

// NewMCPClientService 构造。
func NewMCPClientService() *MCPClientService {
	return &MCPClientService{repo: repository.NewMCPServerRepository()}
}

// MCPImportInput 新增/更新配置。
type MCPImportInput struct {
	Name            string `json:"name"`
	Endpoint        string `json:"endpoint"`
	APIKey          string `json:"api_key"`
	Enabled         *bool  `json:"enabled"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	FailLimit       int    `json:"fail_limit"`
	CooldownSeconds int    `json:"cooldown_seconds"`
}

// Create 落库并立即发现注册。
func (s *MCPClientService) Create(ctx context.Context, in MCPImportInput) (*model.MCPServer, int, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, 0, bizerr.New(bizerr.CodeParamInvalid, "name 不能为空")
	}
	if strings.TrimSpace(in.Endpoint) == "" {
		return nil, 0, bizerr.New(bizerr.CodeParamInvalid, "endpoint 不能为空")
	}
	if _, err := s.repo.FindByName(ctx, in.Name); err == nil {
		return nil, 0, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("MCP server %q 已存在，请先删除", in.Name))
	}
	row := &model.MCPServer{
		Name: in.Name, Endpoint: in.Endpoint,
		APIKeyEnc:      dbencrypt.Encrypt(in.APIKey),
		TimeoutSeconds: in.TimeoutSeconds, FailLimit: in.FailLimit, CooldownSeconds: in.CooldownSeconds,
	}
	row.Enabled = in.Enabled == nil || *in.Enabled
	if err := s.repo.Create(ctx, row); err != nil {
		// 唯一索引兜底：并发同名时上面的 FindByName 拦不住，不能把 PG 原文回给前端。
		if repository.IsDuplicateKeyErr(err) {
			return nil, 0, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("MCP server %q 已存在，请先删除", in.Name))
		}
		return nil, 0, errors.New("保存 MCP 配置失败")
	}
	// model.MCPServer.Enabled 带 `gorm:"default:true"`，GORM 会把显式 false 当零值换成默认值
	// 并回填结构体 —— 创建时「先不启用」会静默变成启用。这里显式回写。
	if in.Enabled != nil && !*in.Enabled {
		if err := s.repo.SetEnabled(ctx, row.ID, false); err != nil {
			return nil, 0, errors.New("保存 MCP 配置失败")
		}
		row.Enabled = false
	}
	if row.Enabled {
		n, err := mcp.DiscoverAndRegisterRowDefaultDB(ctx, row)
		if err != nil {
			// 不当作失败：配置已存下，失败留痕在 last_status/last_error，运维可见。
			logger.Ctx(ctx).Warn().Err(err).Str("name", in.Name).Msg("[mcp-client] 加入时发现失败（配置已保存）")
			return row, 0, nil
		}
		row.ToolCount = n
	}
	return row, row.ToolCount, nil
}

// List 配置列表（不含密钥）。
//
// 隔离靠 tenancyidx 的全局 callback（模型读路径自动注入 WHERE tenant_id = ?，
// 写路径自动打戳）——这里**不要**再手写过滤：重复过滤会掩盖"登记表漏了新表"，
// 让真正该红的那次静默通过。守这条的是 mcp_client_test 的多租户用例。
func (s *MCPClientService) List(ctx context.Context) ([]*model.MCPServer, error) {
	return s.repo.List(ctx)
}

// Delete 删除配置。
//
// 已注册的工具在重启后自然消失（启动发现只拉现存行）；进程内工具名保留到重启，
// 与 custom_tools 的注销口径一致（tooluse.UnregisterProvider 注释：注销不等于删工具）。
func (s *MCPClientService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// SetEnabled 启停。
func (s *MCPClientService) SetEnabled(ctx context.Context, id uint, enabled bool) (*model.MCPServer, int, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, bizerr.New(bizerr.CodeNotFound, fmt.Sprintf("MCP 配置不存在（id=%d）", id))
		}
		return nil, 0, errors.New("读取 MCP 配置失败")
	}
	if err := s.repo.SetEnabled(ctx, id, enabled); err != nil {
		return nil, 0, err
	}
	row.Enabled = enabled
	if !enabled {
		return row, 0, nil
	}
	n, err := mcp.DiscoverAndRegisterRowDefaultDB(ctx, row)
	if err != nil {
		return row, 0, nil
	}
	row.ToolCount = n
	return row, n, nil
}

// Test 连通性/发现自检（不注册，仅回工具名清单供运营确认）。
func (s *MCPClientService) Test(ctx context.Context, id uint) ([]string, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, bizerr.New(bizerr.CodeNotFound, fmt.Sprintf("MCP 配置不存在（id=%d）", id))
		}
		return nil, errors.New("读取 MCP 配置失败")
	}
	client := mcp.NewClient(mcp.ServerConfigFromRow(row))
	tools, err := client.ListTools(ctx)
	if err != nil {
		// 上游不可达 ⇒ 5xxx ⇒ 502：与"配置不存在"的 404 分开，运维才知道该改地址还是改记录。
		return nil, bizerr.Wrap(bizerr.CodePlatformUnavail, "连通性测试失败", err)
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	now := time.Now()
	_ = s.repo.UpdateSyncResult(ctx, id, "ok", "", len(names), &now)
	return names, nil
}
