package service

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

// ReportUsageBestEffort best-effort 异步上报资产使用到平台（闭环使用统计），
// 失败静默忽略，绝不阻塞/影响运行时主流程。
func ReportUsageBestEffort(assetID string) {
	defer func() { _ = recover() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := platform.NewAssetMarketClient()
	_ = client.ReportUsage(ctx, assetID, 1)
}

// AssetResolver 资产市场运行时覆盖层（M2：运行时覆盖默认）。
//
// 设计意图：业务子系统在构建默认配置时，优先读取「生效中」(is_active=true)
// 的已购买/已同步资产；若不存在，则回退到各 Loader 内置的代码默认。
//
// 五种资产类型分别由对应的 Loader 提供 DB 优先、代码默认兜底的读取能力，
// 本 resolver 在其之上封装「取生效资产」语义，供业务运行时调用。
type AssetResolver struct {
	assetRepo repository.LocalAssetRepository
	agent     *AgentLoader
	script    *ScriptLoader
	sop       *SOPLoader
	abtest    *ABTestLoader
	workflow  *WorkflowLoader
}

var assetResolverInstance *AssetResolver

// InitAssetResolver 初始化全局运行时覆盖层，应在服务启动时调用一次。
func InitAssetResolver(db *gorm.DB) {
	assetResolverInstance = &AssetResolver{
		assetRepo: repository.NewLocalAssetRepository(db),
		agent:     NewAgentLoader(),
		script:    NewScriptLoader(),
		sop:       NewSOPLoader(),
		abtest:    NewABTestLoader(),
		workflow:  NewWorkflowLoader(),
	}
}

// GetAssetResolver 获取全局运行时覆盖层实例（未初始化时返回 nil，调用方需 nil 保护）。
func GetAssetResolver() *AssetResolver {
	return assetResolverInstance
}

func (r *AssetResolver) activeAssetID(ctx context.Context, assetType string) (string, bool) {
	if r == nil || r.assetRepo == nil {
		return "", false
	}
	aid, err := r.assetRepo.FindActiveAssetIDByType(ctx, assetType)
	if err != nil || aid == "" {
		return "", false
	}

	if la, findErr := r.assetRepo.FindByAssetID(ctx, aid); findErr == nil && la != nil {
		_ = r.assetRepo.IncrementUseCount(ctx, la.ID, 1)
		go ReportUsageBestEffort(aid)
	}
	return aid, true
}

func (r *AssetResolver) GetActivePersona(ctx context.Context) (*AgentPersona, bool) {
	if aid, ok := r.activeAssetID(ctx, "agent_persona"); ok {
		if p, err := r.agent.LoadPersona(ctx, aid); err == nil {
			return p, true
		}
	}
	return nil, false
}

func (r *AssetResolver) GetActiveScript(ctx context.Context) (*SalesScript, bool) {
	if aid, ok := r.activeAssetID(ctx, "sales_script"); ok {
		if s, err := r.script.LoadScript(ctx, aid); err == nil {
			return s, true
		}
	}
	return nil, false
}

func (r *AssetResolver) GetActiveSOP(ctx context.Context) (*IndustrySOP, bool) {
	if aid, ok := r.activeAssetID(ctx, "industry_sop"); ok {
		if s, err := r.sop.LoadSOP(ctx, aid); err == nil {
			return s, true
		}
	}
	return nil, false
}

func (r *AssetResolver) GetActiveABPlan(ctx context.Context) (*ABTestPlan, bool) {
	if aid, ok := r.activeAssetID(ctx, "ab_test_plan"); ok {
		if p, err := r.abtest.LoadPlan(ctx, aid); err == nil {
			return p, true
		}
	}
	return nil, false
}

func (r *AssetResolver) GetActiveWorkflow(ctx context.Context) (*MarketingWorkflow, bool) {
	if aid, ok := r.activeAssetID(ctx, "marketing_workflow"); ok {
		if w, err := r.workflow.LoadWorkflow(ctx, aid); err == nil {
			return w, true
		}
	}
	return nil, false
}

func (r *AssetResolver) LoadPersona(ctx context.Context, assetID string) (*AgentPersona, error) {
	return r.agent.LoadPersona(ctx, assetID)
}

func (r *AssetResolver) LoadScript(ctx context.Context, assetID string) (*SalesScript, error) {
	return r.script.LoadScript(ctx, assetID)
}

func (r *AssetResolver) LoadSOP(ctx context.Context, assetID string) (*IndustrySOP, error) {
	return r.sop.LoadSOP(ctx, assetID)
}

func (r *AssetResolver) LoadPlan(ctx context.Context, assetID string) (*ABTestPlan, error) {
	return r.abtest.LoadPlan(ctx, assetID)
}

func (r *AssetResolver) LoadWorkflow(ctx context.Context, assetID string) (*MarketingWorkflow, error) {
	return r.workflow.LoadWorkflow(ctx, assetID)
}

func (r *AssetResolver) ListPersonas(ctx context.Context) ([]*AgentPersona, error) {
	return r.agent.ListAllPersonas(ctx)
}

func (r *AssetResolver) ListScripts(ctx context.Context) ([]*SalesScript, error) {
	return r.script.ListAllScripts(ctx)
}

func (r *AssetResolver) ListSOPs(ctx context.Context) ([]*IndustrySOP, error) {
	return r.sop.ListAllSOPs(ctx)
}

func (r *AssetResolver) ListPlans(ctx context.Context) ([]*ABTestPlan, error) {
	return r.abtest.ListAllPlans(ctx)
}

func (r *AssetResolver) ListWorkflows(ctx context.Context) ([]*MarketingWorkflow, error) {
	return r.workflow.ListAllWorkflows(ctx)
}
