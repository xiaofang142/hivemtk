package app

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	mcpclient "hivemtk-user/internal/aiagent/mcp"
	"hivemtk-user/internal/bridge"
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

	// P1-2：OpenAPI 导入的工具体现在 custom_tools 表里，进程内注册表重启即空——
	// 必须每次启动从表里重建，否则「前端显示已导入、Agent 调不到」。
	for _, p := range tooluse.LoadCustomToolProviders(gormDB) {
		if err := providerRegistry.RegisterProvider(p); err != nil {
			logger.Errorf("[agent] 注册 OpenAPI 工具集 %s 失败: %v", p.Name(), err)
		}
	}

	// P1-3：外部 MCP server 同理——配置在 mcp_servers 表里，工具要靠启动发现补回。
	// 单个 server 不可达只跳过它自己（LoadConfiguredServers 内部隔离）。
	if nSrv, nTool := mcpclient.LoadConfiguredServers(context.Background(), gormDB, tooluse.GetGlobalRegistry()); nSrv > 0 || nTool > 0 {
		logger.Infof("[agent] ✅ 外部 MCP server 接入 %d 个 / 工具 %d 个", nSrv, nTool)
	}

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
