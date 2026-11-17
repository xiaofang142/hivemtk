package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"hivemtk-user/internal/pkg/tenant"
)

// ResponseCacheFingerprint 返回配置摘要，不暴露凭证，也不把运行时延迟采样当配置版本。
// 这里只证明缓存范围，不改变模型选择、质量门槛或实际请求限流。
func (d *Dispatcher) ResponseCacheFingerprint() string {
	if d == nil {
		return ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	providers := make([]ProviderConfig, 0, len(d.providers))
	for _, p := range d.providers {
		if p == nil {
			continue
		}
		cp := *p
		cp.AvgLatencyMs = 0
		providers = append(providers, cp)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	routes := make([]ScenarioRoute, 0, len(d.routes))
	for _, r := range d.routes {
		if r != nil {
			routes = append(routes, *r)
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Scenario < routes[j].Scenario })
	return responseCacheHash(struct {
		Version   int
		Providers []ProviderConfig
		Routes    []ScenarioRoute
	}{Version: 1, Providers: providers, Routes: routes})
}

func responseCacheHash(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (d *Dispatcher) scopedResponseCacheKey(ctx context.Context, req DispatchRequest) string {
	if ctx == nil || ctx.Err() != nil || tenant.IsSystemScope(ctx) || req.CacheKey == "" || req.CacheTTL <= 0 {
		return ""
	}
	// 纯文本缓存不能表示工具调用、消息式执行、投票结果或 token 级观测。
	if len(req.Tools) > 0 || req.ToolChoice != "" || len(req.Messages) > 0 ||
		(req.FanOut != nil && req.FanOut.Enable) || req.ReturnLogprobs || req.TopLogprobs > 0 {
		return ""
	}
	// 高质量场景可在后续分派阶段隐式启用投票；普通单模型答案不能冒充投票结果。
	if req.Scenario == ScenarioHighQuality && fanoutVoteEnabled() {
		return ""
	}
	tenantID := tenant.WriteTenantID(ctx)
	config := d.ResponseCacheFingerprint()
	if tenantID == 0 || config == "" {
		return ""
	}
	key := responseCacheHash(struct {
		TenantID uint
		Config   string
		Request  DispatchRequest
	}{TenantID: tenantID, Config: config, Request: req})
	if key == "" {
		return ""
	}
	return "llm:response:v1:" + key
}
