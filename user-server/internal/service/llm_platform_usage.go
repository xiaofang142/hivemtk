package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
)

// 平台垫资模型的本地镜像与用量落库。
//
// ============================================================
// 扣费的权威在平台侧（代理转发时实时扣），这里**不做第二次扣减**。
//
// 为什么不做：两次扣账是预付系统最贵的 bug —— 商户充 100 元、用 10 元，
// 平台扣 10、本地也扣 10，看着"对得上"；但两边各自还有一份余额，
// 一旦其中一边重试（网络抖动导致的自动重发）就会二次扣，用户看到余额掉得比实际快。
//
// 本地这一层只负责三件事：
//  1. 落用量明细 → 让商户在本地也能看到"谁在什么场景用了多少"；
//  2. 更新余额镜像 → 页面不用为看一眼余额多打一次平台；
//  3. 余额不足时**提前阻断** → 少一次注定失败的往返。
// ============================================================

// PlatformLLMUsage 一次调用的用量（由 dispatcher 在收尾时回填）
type PlatformLLMUsage struct {
	MerchantKey string
	Provider    string
	Model       string
	PlanCode    string
	Scenario    string
	TraceID     string
	AgentID     string
	UserID      string
	// TenantCode 本次调用归属的租户短码（与代理调用 query 的 tenant_code 同源，
	// 供明细字段完整性/排障定位用）。镜像表已按 tenant_id 归属，本轮不加列。
	TenantCode       string
	PromptTokens     int64
	CompletionTokens int64
	// InputCostCents / OutputCostCents 输入/输出分列成本（分）。dispatcher 在
	// 写入时点按平台套餐单价快照整数换算（tokens*price/1000，与平台 PriceTokens
	// 同式同截断），随事件原样下发 —— 这里**只消费不重算**：读侧不重算不回填，
	// 单价调整后历史明细不漂移。
	InputCostCents  int64
	OutputCostCents int64
}

// recordPlatformUsage 落本地用量明细并更新余额镜像
func recordPlatformUsage(ctx context.Context, u PlatformLLMUsage) {
	if u.PromptTokens <= 0 && u.CompletionTokens <= 0 {
		return
	}
	// 请求 ID 缺 trace_id 时用「provider+模型+时刻」兜底：宁可偶尔重复，
	// 也不能因为没有 trace 而一条都不记（计量断档会让成本核算整体失去依据）。
	requestID := strings.TrimSpace(u.TraceID)
	if requestID == "" {
		requestID = "no-trace:" + u.Provider + ":" + u.Model + ":" + time.Now().Format("20060102150405.000")
	}
	// 成本三列直接消费事件携带的整数分（写入时点快照，见 PlatformLLMUsage 注释）。
	// 取不到价的调用 dispatcher 已按 0 分下发 —— 这里如实落 0，不回填不编价。
	inc, outc := u.InputCostCents, u.OutputCostCents
	repo := repository.NewPlatformLLMUsageRepository()
	rec := &model.PlatformLLMUsageRecord{
		RequestID:        requestID,
		Provider:         u.Provider,
		Model:            u.Model,
		PlanCode:         u.PlanCode,
		Scenario:         u.Scenario,
		TraceID:          u.TraceID,
		AgentID:          u.AgentID,
		UserID:           u.UserID,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		InputCostCents:   inc,
		OutputCostCents:  outc,
		TotalCostCents:   inc + outc,
		OccurredAt:       time.Now(),
	}
	// 幂等：trace_id 撞唯一索引就跳过（重试/重放不重复记账）
	if err := repo.InsertUsageRecord(ctx, rec); err != nil {
		if errors.Is(err, repository.ErrDBNotReady) {
			logger.Warnf("[llm-usage] db 未就绪，跳过本地用量落库 provider=%s tenant=%s", u.Provider, u.TenantCode)
			return
		}
		logger.Warnf("[llm-usage] 本地用量落库失败 provider=%s tenant=%s trace=%s: %v",
			u.Provider, u.TenantCode, u.TraceID, err)
		return
	}
}

// syncPlatformProviders 拉取平台下发的 provider 元信息并落库
//
// **不下发 API Key**（见 plan.md ADR-M7）。落库时按 provider_name upsert，
// 已存在的行只更新元信息与 source，**不动商户自己填的 APIKey** ——
// 万一将来某个 provider 从 platform 转回 self，密钥还在。
//
// 单价落地（PLT-01 刀1）：Providers() 与 Plans() 各拉一次，按 plan_code 关联
// 把套餐单价快照（分/1k token）落到 provider 镜像行上 —— 零协议变化（平台
// provider 视图不带价，套餐接口带）。join 不上（套餐下架/不可见）= 价列 0，
// 调用侧如实降级 cost 0，不编价。
func syncPlatformProviders(ctx context.Context) (int, error) {
	if !config.PlatformEnabled() {
		return 0, nil
	}
	client := platform.NewModuleClient()
	rows, err := client.Providers(ctx)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	// 价目拉取失败 ≠ 元信息失败：provider 元信息照落（价列缺省 0），本趟降级，
	// 下一趟同步补上。已落的历史明细不回填（写入时点快照口径）。
	priceByPlan, err := platformPlanPriceMap(ctx, client)
	if err != nil {
		logger.Warnf("[llm-provider] 平台套餐价拉取失败，本趟单价快照缺省 0（元信息照常落库）: %v", err)
	}
	sysCtx := tenant.WithSystemScope(context.Background())
	n, err := repository.NewPlatformLLMUsageRepository().UpsertProviders(sysCtx, platformProviderRowsToModels(rows, priceByPlan))
	if err != nil {
		logger.Warnf("[llm-provider] 平台 provider 落库失败: %v", err)
	}
	logger.Infof("[llm-provider] 平台下发 provider 同步完成：%d 条（含单价快照 %d 个套餐）", n, len(priceByPlan))
	return n, nil
}

// platformPlanPriceMap 拉取可见套餐并整理成 plan_code → 单价快照。
// 平台价唯一来源（不新增第二定价源）：本地价列只从这里来。
func platformPlanPriceMap(ctx context.Context, client *platform.ModuleClient) (map[string]platform.PlatformLLMPlan, error) {
	plans, err := client.Plans(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]platform.PlatformLLMPlan, len(plans))
	for _, p := range plans {
		code := strings.TrimSpace(p.PlanCode)
		if code == "" {
			continue
		}
		m[code] = p
	}
	return m, nil
}

// platformProviderRowsToModels 平台行 → 本地模型行（业务默认值归业务层；
// 落库归仓储）。归属默认租户：平台下发的是全局 provider 定义，dispatcher 的
// LoadProvidersFromDB 只装 tenant_id=1，写到别的租户会「写进去但读不出来」。
//
// priceByPlan 为 nil（价目拉取失败）时价列落 0：宁可如实记 0 也不编价，
// 调用侧的 cost 降级与「套餐下架 join 不上」同格。
func platformProviderRowsToModels(rows []platform.PlatformLLMProvider, priceByPlan map[string]platform.PlatformLLMPlan) []model.LLMProvider {
	mapped := make([]model.LLMProvider, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.ProviderName) == "" {
			continue
		}
		row := model.LLMProvider{
			TenantID:    tenant.DefaultTenantID,
			Name:        r.ProviderName,
			DisplayName: r.DisplayName,
			Model:       r.Model,
			APIType:     llmDefaultIfEmpty(r.APIType, "openai"),
			Vendor:      r.Vendor,
			Source:      model.LLMSourcePlatform,
			PlanCode:    r.PlanCode,
			Enabled:     true,
		}
		if p, ok := priceByPlan[strings.TrimSpace(r.PlanCode)]; ok {
			row.InputPricePer1kCents = p.InputPricePer1kCents
			row.OutputPricePer1kCents = p.OutputPricePer1kCents
		}
		mapped = append(mapped, row)
	}
	return mapped
}

func llmDefaultIfEmpty(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// InstallPlatformUsageSink 装配期接线：把 dispatcher 的用量回填接到本地落库。
//
// 装配点放在 cmd/api/main.go（见该文件），理由与 ADR-M1 一致：
// llm 是底层包，它不能反过来 import service，所以接线必须在能同时看到两侧的地方做。
//
// 同一处一并注入平台套餐单价解析（PLT-01 刀1）：dispatcher 在写入时点按
// plan_code 取本地镜像价列、整数换算后随事件下发 —— 同一份快照同时喂
// routing_logs 与 platform_llm_usage_records，两表成本口径恒一致。
func InstallPlatformUsageSink() {
	llm.SetPlatformUsageSink(func(e llm.PlatformUsageEvent) {
		// 用量明细是租户表：sink 在调用链之外执行，只能用事件里带的租户
		// 重建上下文；没带租户就不落（on 模式下 callback 也会拒绝）。
		ctx := context.Background()
		if e.TenantID != 0 {
			ctx = tenant.WithTenantID(ctx, e.TenantID)
		}
		recordPlatformUsage(ctx, PlatformLLMUsage{
			Provider:         e.Provider,
			Model:            e.Model,
			PlanCode:         e.PlanCode,
			Scenario:         e.Scenario,
			TraceID:          e.TraceID,
			AgentID:          e.AgentID,
			UserID:           e.UserID,
			TenantCode:       e.TenantCode,
			PromptTokens:     e.PromptTokens,
			CompletionTokens: e.CompletionTokens,
			InputCostCents:   e.InputCostCents,
			OutputCostCents:  e.OutputCostCents,
		})
	})
	llm.SetPlatformPlanPriceResolver(platformPlanPriceResolver)
}

// platformPlanPriceResolver 按 plan_code 读本地 provider 镜像上的套餐单价快照。
//
// 读走 system scope：平台价是商户级配置（镜像行归属默认租户），带租户 ctx 的
// 隔离过滤会让非默认租户永远读不到价 → 平台调用成本全部降级 0。
// 查不到/未同步/库未就绪 = (0,0)：调用侧如实降级，不报错、不编价。
func platformPlanPriceResolver(planCode string) (int64, int64) {
	ctx := tenant.WithSystemScope(context.Background())
	in, out, err := repository.NewPlatformLLMUsageRepository().GetPlatformPlanPriceByCode(ctx, planCode)
	if err != nil {
		logger.Warnf("[llm-usage] 平台套餐单价读取失败 plan=%s（本调用成本按 0 记）: %v", planCode, err)
		return 0, 0
	}
	return in, out
}

// SyncPlatformProviders 导出给装配层调用（同步平台下发的 provider）
func SyncPlatformProviders(ctx context.Context) (int, error) { return syncPlatformProviders(ctx) }
