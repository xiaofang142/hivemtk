package app

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	mcpclient "hivemtk-user/internal/aiagent/mcp"
	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// ReachToolProvider 提供 20 个多渠道触达工具
type ReachToolProvider struct{}

func (p *ReachToolProvider) Name() string                   { return "reach" }
func (p *ReachToolProvider) Category() tooluse.ToolCategory { return tooluse.CategoryReach }
func (p *ReachToolProvider) Description() string {
	return "多渠道触达工具（短信/邮件/微信/抖音/小红书/Telegram/WhatsApp/飞书等 20 个）"
}

func (p *ReachToolProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if ctx.DB == nil {
		return nil, errProviderDBRequired
	}
	adapter := bridge.NewBridgeReachAdapter(NewIntegrationReachAdapterFromDB(ctx.DB), GetBridgeIngressSvc())
	deps := NewReachToolDepsWithAdapter(ctx.DB, adapter)
	return tooluse.BuildReachTools(deps), nil
}

// PrivateMessageToolProvider 提供 3 个私信工具
type PrivateMessageToolProvider struct{}

func (p *PrivateMessageToolProvider) Name() string { return "pm" }
func (p *PrivateMessageToolProvider) Category() tooluse.ToolCategory {
	return tooluse.CategoryPrivateMessage
}
func (p *PrivateMessageToolProvider) Description() string {
	return "私信工具（pm.session.open/read/message.send）"
}

func (p *PrivateMessageToolProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if ctx.DB == nil {
		return nil, errProviderDBRequired
	}
	sessionSvc := service.NewCustomerSessionService()
	deps := tooluse.NewPrivateMessageToolDepsWithPort(service.NewSessionPortAdapter(sessionSvc))
	return tooluse.BuildPrivateMessageTools(deps), nil
}

// CustomerToolProvider 提供 8 个客户工具
type CustomerToolProvider struct{}

func (p *CustomerToolProvider) Name() string                   { return "customer" }
func (p *CustomerToolProvider) Category() tooluse.ToolCategory { return tooluse.CategoryCustomer }
func (p *CustomerToolProvider) Description() string {
	return "客户工具（search/get/create/update/merge/add_tag/remove_tag/segment）"
}

func (p *CustomerToolProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if ctx.DB == nil {
		return nil, errProviderDBRequired
	}
	customerPort := service.NewCustomerPortAdapter(service.NewCustomerService())
	deps := tooluse.NewCustomerToolDepsWithPort(customerPort, ctx.DB, NewCustomerDataStore())
	return tooluse.BuildCustomerTools(deps), nil
}

// KnowledgeToolProvider 提供 4 个知识工具
type KnowledgeToolProvider struct{}

func (p *KnowledgeToolProvider) Name() string                   { return "knowledge" }
func (p *KnowledgeToolProvider) Category() tooluse.ToolCategory { return tooluse.CategoryKnowledge }
func (p *KnowledgeToolProvider) Description() string {
	return "知识工具（rag.search/knowledge.feedback/add_doc/list_kb）"
}

func (p *KnowledgeToolProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if ctx.DB == nil {
		return nil, errProviderDBRequired
	}
	deps := tooluse.NewKnowledgeToolDepsWithDB(ctx.DB)
	return tooluse.BuildKnowledgeTools(deps), nil
}

// BusinessToolProvider 提供 5 个业务工具
type BusinessToolProvider struct{}

func (p *BusinessToolProvider) Name() string                   { return "business" }
func (p *BusinessToolProvider) Category() tooluse.ToolCategory { return tooluse.CategoryBusiness }
func (p *BusinessToolProvider) Description() string {
	return "业务工具（follow_task.create/update、order.lookup、aftersale.create/query）"
}

func (p *BusinessToolProvider) Provide(ctx tooluse.ProviderContext) ([]tooluse.Tool, error) {
	if ctx.DB == nil {
		return nil, errProviderDBRequired
	}
	orderPort := service.NewOrderPortAdapter(repository.NewExternalOrderRepository())
	afterSalePort := service.NewAfterSalePortAdapter(
		service.NewAfterSaleService(repository.NewAfterSaleRepositoryWithDB(ctx.DB)),
	)
	deps := tooluse.NewBusinessToolDepsWithPorts(
		service.NewFollowUpPortAdapter(service.NewFollowUpService(service.NewCustomerJourneyService())),
		orderPort,
		afterSalePort,
		ctx.DB,
	)
	return tooluse.BuildBusinessTools(deps), nil
}

var errProviderDBRequired = &providerError{"provider requires DB in ProviderContext"}

type permissionGuardedTool struct {
	tooluse.Tool
	checker tooluse.PermissionChecker
}

func (g *permissionGuardedTool) Execute(ctx context.Context, args map[string]any) (tooluse.ToolResult, error) {
	if g.checker != nil {
		name := g.Tool.Name()
		if tooluse.GetToolName(ctx) == "" {
			ctx = tooluse.WithToolName(ctx, name)
		}
		if err := g.checker.Check(ctx, name, tooluse.GetToolContext(ctx)); err != nil {
			return tooluse.ErrorResult(name, fmt.Errorf("%w: %v", tooluse.ErrPermissionDenied, err)), tooluse.ErrPermissionDenied
		}
	}
	return g.Tool.Execute(ctx, args)
}

func (g *permissionGuardedTool) RiskLevel() tooluse.ToolRiskLevel {
	return tooluse.DeclaredRisk(g.Tool)
}

func rewirePermissionDecorators(registry *tooluse.ToolRegistry) {
	if registry == nil {
		return
	}
	checker := GetGlobalPermissionChecker()
	if checker == nil {
		logger.Warn("[agent] ⚠️ 全局 PermissionChecker 未就绪，跳过 PermissionDecorator 接线")
		return
	}
	wired := 0
	for _, t := range registry.List() {
		if _, ok := t.(*permissionGuardedTool); ok {
			continue
		}
		if err := registry.Unregister(t.Name()); err != nil {
			continue
		}
		if err := registry.Register(&permissionGuardedTool{Tool: t, checker: checker}); err != nil {
			logger.Errorf("[agent] ❌ 工具 %s 权限装饰回注失败: %v", t.Name(), err)
			continue
		}
		wired++
	}
	logger.Infof("[agent] ✅ PermissionDecorator 已接线：%d 个工具挂载权限钩子（defaultAllow=true 语义不变）", wired)
}

type providerError struct{ msg string }

func (e *providerError) Error() string { return e.msg }

var globalProviderRegistry *tooluse.ProviderRegistry

// GetGlobalProviderRegistry 读取全局 ProviderRegistry（未装配时为 nil）
func GetGlobalProviderRegistry() *tooluse.ProviderRegistry { return globalProviderRegistry }

// SetGlobalProviderRegistryForTest 仅用于测试：临时替换/清空全局 ProviderRegistry
func SetGlobalProviderRegistryForTest(r *tooluse.ProviderRegistry) { globalProviderRegistry = r }

func initBuiltinToolProviders(registry *tooluse.ProviderRegistry) {
	providers := []tooluse.ToolProvider{
		&ReachToolProvider{},
		&PrivateMessageToolProvider{},
		&CustomerToolProvider{},
		&KnowledgeToolProvider{},
		&BusinessToolProvider{},
	}
	for _, p := range providers {
		if err := registry.RegisterProvider(p); err != nil {
			logger.Errorf("[agent] 注册内置 Provider %s 失败: %v", p.Name(), err)
		}
	}
}

func registerAllAgentToolsViaProviders(gormDB *gorm.DB) {
	providerRegistry := tooluse.NewProviderRegistry()
	globalProviderRegistry = providerRegistry

	initBuiltinToolProviders(providerRegistry)

	// P1-2 / P1-3：custom_tools（OpenAPI 导入）与 mcp_servers（外部 MCP）都是**租户表**，
	// 进程内注册表重启即空，必须每次启动从表里重建；但重建的目标不是全局注册表，
	// 而是按租户隔离的叠加层（tooluse.EnsureTenantRegistry）——否则多租户下
	// A 租户的 Agent 能调到 B 租户的工具（工具背后带 B 的凭据）。
	loadTenantScopedToolProviders(context.Background(), gormDB)

	autoProviders := tooluse.GetAutoRegisteredProviders()
	for _, p := range autoProviders {
		if err := providerRegistry.RegisterProvider(p); err != nil {
			logger.Errorf("[agent] 注册第三方 Provider %s 失败: %v", p.Name(), err)
		} else {
			logger.Infof("[agent] ✅ 第三方 Provider %s 已注册", p.Name())
		}
	}

	ctx := tooluse.ProviderContext{
		DB:     gormDB,
		Config: tooluse.ProviderConfig{Enabled: true},
	}
	results, err := providerRegistry.RegisterAll(ctx, tooluse.GetGlobalRegistry())
	if err != nil {
		logger.Errorf("[agent] ⚠️ Provider 批量装配存在失败: %v", err)
	}

	tooluse.RegisterCardTools(tooluse.GetGlobalRegistry())
	logger.Info("[agent] ✅ 会话内卡片工具（card.show）已接入全局注册中心")

	// browser 自动化工具（browser_open_task / browser_task_status / browser_task_list）
	if err := tooluse.RegisterBrowserTools(tooluse.GetGlobalRegistry(), tooluse.NewBrowserToolDeps(gormDB)); err != nil {
		logger.Errorf("[agent] ⚠️ browser 自动化工具注册失败: %v", err)
	} else {
		logger.Info("[agent] ✅ browser 自动化工具（browser_open_task/browser_task_status/browser_task_list）已接入全局注册中心")
	}

	// ego-lite 腿工具（egolite_run，specs/016 S11）：执行器经 SetEgoLiteRunner 注入
	// （egolite_routes.go 装配），这里只登记工具面
	if err := tooluse.RegisterEgoLiteTools(tooluse.GetGlobalRegistry(), tooluse.NewBrowserToolDeps(gormDB)); err != nil {
		logger.Errorf("[agent] ⚠️ ego-lite 工具注册失败: %v", err)
	} else {
		logger.Info("[agent] ✅ ego-lite 工具（egolite_run）已接入全局注册中心")
	}

	// 通用浏览器执行能力（browser_session_open/act/observe/finish）：端口由
	// egolite_routes.go 装配（SetBrowserSessionPort）注入，这里只登记工具面。
	if err := tooluse.RegisterBrowserSessionTools(tooluse.GetGlobalRegistry(), tooluse.NewBrowserToolDeps(gormDB)); err != nil {
		logger.Errorf("[agent] ⚠️ 通用浏览器会话工具注册失败: %v", err)
	} else {
		logger.Info("[agent] ✅ 通用浏览器会话工具（open/act/observe/finish）已接入全局注册中心")
	}

	rewirePermissionDecorators(tooluse.GetGlobalRegistry())

	applyTenantDisabledTools(tooluse.GetGlobalRegistry())

	totalTools := 0
	totalProviders := 0
	failedProviders := 0
	for _, r := range results {
		if r.Skipped {
			logger.Infof("[agent] ⏭️  Provider %s 跳过: %s", r.ProviderName, r.SkippedReason)
			continue
		}
		if r.Err != "" {
			failedProviders++
			logger.Errorf("[agent] ❌ Provider %s 失败: %s", r.ProviderName, r.Err)
			continue
		}
		totalProviders++
		totalTools += r.ToolCount
		logger.Infof("[agent] ✅ Provider %s 装配完成: %d 个工具 (耗时 %v)",
			r.ProviderName, r.ToolCount, r.Duration)
	}
	logger.Infof("[agent] 🎯 工具链装配总结: providers=%d tools=%d failed=%d",
		totalProviders, totalTools, failedProviders)

	InitGlobalToolRouter()
}

func applyTenantDisabledTools(registry *tooluse.ToolRegistry) {
	// 工具注册表是进程级共享的：on 模式下把默认租户的停用清单在装配期剔掉会波及
	// 所有租户。租户级过滤改在 agent loop 每次派发时按请求 ctx 执行（resolveAgentSettings）。
	if tenant.IsMultiTenant() {
		return
	}
	cfg, err := service.LoadAgentSettingsConfig(context.Background())
	if err != nil || cfg == nil {
		return
	}
	applyDisabledTools(registry, cfg.DisabledTools)
}

func applyDisabledTools(registry *tooluse.ToolRegistry, disabled []string) int {
	if registry == nil || len(disabled) == 0 {
		return 0
	}
	removed := 0
	for _, name := range disabled {
		if name == "" {
			continue
		}
		if err := registry.Unregister(name); err != nil {

			continue
		}
		removed++
		logger.Infof("[agent] 🚫 工具 %s 已按 agent_settings.disabled_tools 停用", name)
	}
	return removed
}

// loadTenantScopedToolProviders 按租户重建 custom_tools / mcp_servers 的工具。
//
// 枚举行用 SystemScope（跨租户读），工具只登记到各自租户的叠加层；
// 单行/单 server 失败只跳过自己。单租户（off）模式全部行属于默认租户，
// ScopeFor 会把默认租户的工具合并给所有请求，行为与改造前一致。
func loadTenantScopedToolProviders(ctx context.Context, gormDB *gorm.DB) {
	if gormDB == nil {
		return
	}
	sysCtx := tenant.WithSystemScope(ctx)

	// 1) OpenAPI 导入工具集
	var rows []model.CustomTool
	if err := gormDB.WithContext(sysCtx).Where("is_active = ?", true).Order("id ASC").Find(&rows).Error; err != nil {
		logger.Warnf("[custom-tool] 启动加载失败（租户自定义工具将不可用，其余工具不受影响）: %v", err)
	} else {
		byTenant := map[uint][]model.CustomTool{}
		for _, r := range rows {
			byTenant[r.TenantID] = append(byTenant[r.TenantID], r)
		}
		for tid, list := range byTenant {
			n := 0
			for i := range list {
				rec := list[i]
				p, err := tooluse.CustomToolRowToProvider(&rec)
				if err != nil {
					logger.Warnf("[custom-tool] 跳过工具集 %q(id=%d,tenant=%d): %v", rec.Name, rec.ID, tid, err)
					continue
				}
				tools, perr := p.Provide(tooluse.ProviderContext{})
				if perr != nil {
					logger.Warnf("[custom-tool] 工具集 %q(id=%d,tenant=%d) 装配失败: %v", rec.Name, rec.ID, tid, perr)
					continue
				}
				for _, t := range tools {
					if err := tooluse.RegisterTenantTool(tid, t); err != nil {
						logger.Warnf("[custom-tool] 工具 %q(tenant=%d) 登记失败: %v", t.Name(), tid, err)
						continue
					}
					n++
				}
			}
			if n > 0 {
				logger.Infof("[custom-tool] 租户 %d 重建 %d 个 OpenAPI 工具", tid, n)
			}
		}
	}

	// 2) 外部 MCP server
	var servers []model.MCPServer
	if err := gormDB.WithContext(sysCtx).Where("enabled = ?", true).Order("id ASC").Find(&servers).Error; err != nil {
		logger.Warnf("[mcp-client] 读取 mcp_servers 失败（外部 MCP 工具不可用，其余工具不受影响）: %v", err)
		return
	}
	byTenant := map[uint][]model.MCPServer{}
	for _, r := range servers {
		byTenant[r.TenantID] = append(byTenant[r.TenantID], r)
	}
	for tid, list := range byTenant {
		tctx := tenant.WithTenantID(ctx, tid)
		reg := tooluse.EnsureTenantRegistry(tid)
		nSrv, nTool := 0, 0
		for i := range list {
			n, err := mcpclient.DiscoverAndRegisterRow(tctx, gormDB, &list[i], reg)
			if err != nil {
				logger.Warnf("[mcp-client] server %q(tenant=%d) 发现失败（跳过该 server）: %v", list[i].Name, tid, err)
				continue
			}
			nSrv++
			nTool += n
		}
		if nSrv > 0 {
			logger.Infof("[mcp-client] 租户 %d 接入 %d 个 server / %d 个工具", tid, nSrv, nTool)
		}
	}
}
