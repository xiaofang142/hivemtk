package sop

// deps.go：SOP 域（按域拆包第四刀）对根 service 包的全部外依赖接缝。
//
// 方向：sop 只声明它真正需要的方法，根包 sop_compat.go 提供适配器并在 init 装配。
// 审批域已独立成 internal/service/approval，sop 直接 import，不需要适配器。
// 默认值一律安全：参数中心回落调用方兜底值、模块闸对未映射名放行（与根包同口径）、
// 资产未装配回落内置默认、圈选源未装配返回显式错误（fail-closed）。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// ── 客户侧出域（reach）──

// ReachRequest 出域请求：只含 SOP 节点会填的字段。
type ReachRequest struct {
	CustomerID        string
	OneID             string
	Content           string
	PreferredChannels []string
}

// ReachResult 出域结果。
type ReachResult struct {
	Channel   string
	MessageID string
}

// 出域三哨兵，与根包 ErrDoNotContact / ErrReachCooldown / ErrReachApprovalDenied 对应。
var (
	ErrDoNotContact        = errors.New("dnc")
	ErrReachCooldown       = errors.New("cooldown")
	ErrReachApprovalDenied = errors.New("reach_approval_denied")
)

// ── 参数中心 ──

// ParamGetter 参数中心只读面（根包 *ConfigParamService 天然满足）。
type ParamGetter interface {
	GetInt(ctx context.Context, group, key string, fallback int) int
	GetDuration(ctx context.Context, group, key string, fallback time.Duration) time.Duration
}

type fallbackParams struct{}

func (fallbackParams) GetInt(_ context.Context, _, _ string, fallback int) int { return fallback }

func (fallbackParams) GetDuration(_ context.Context, _, _ string, fallback time.Duration) time.Duration {
	return fallback
}

var (
	paramMu     sync.RWMutex
	paramGetter ParamGetter = fallbackParams{}
)

// SetParamGetter 装配参数中心（根包 init 调用）。
func SetParamGetter(g ParamGetter) {
	paramMu.Lock()
	paramGetter = g
	paramMu.Unlock()
}

func globalParams() ParamGetter {
	paramMu.RLock()
	defer paramMu.RUnlock()
	return paramGetter
}

// ── 模块闸 ──

var (
	workerGateMu sync.RWMutex
	// 默认恒放行：与根包 AllowWorker 对「映射不到模块 key」的口径一致。
	workerAllowed = func(context.Context, string) bool { return true }
)

// SetWorkerAllowed 装配模块闸（根包 init 调用）。
func SetWorkerAllowed(f func(context.Context, string) bool) {
	workerGateMu.Lock()
	workerAllowed = f
	workerGateMu.Unlock()
}

func allowWorker(ctx context.Context, nameOrKey string) bool {
	workerGateMu.RLock()
	f := workerAllowed
	workerGateMu.RUnlock()
	return f(ctx, nameOrKey)
}

// ── 资产 ──

// SOPTemplate 生效 SOP 资产转创建请求。
type SOPTemplate interface {
	ToCreateRequest(ctx context.Context, scenario string) *dto.CreateRequest
}

// ABPlan 生效 AB 实验资产转配置。
type ABPlan interface {
	ToSOPABTestConfig(ctx context.Context) dto.SOPABTestConfig
}

// AssetProvider 资产覆盖层只读面（根包 *AssetResolver 适配后满足）。
type AssetProvider interface {
	GetActiveSOP(ctx context.Context) (SOPTemplate, bool)
	GetActiveABPlan(ctx context.Context) (ABPlan, bool)
}

var (
	assetMu         sync.RWMutex
	assetProvider   func() AssetProvider
	assetDataLoader = func(context.Context, string, string) ([]byte, bool) { return nil, false }
)

// SetAssetProvider 装配资产覆盖层读取器（根包 init 调用）。
func SetAssetProvider(f func() AssetProvider) {
	assetMu.Lock()
	assetProvider = f
	assetMu.Unlock()
}

func getAssetProvider() AssetProvider {
	assetMu.RLock()
	f := assetProvider
	assetMu.RUnlock()
	if f == nil {
		return nil
	}
	return f()
}

// SetAssetDataLoader 装配资产明细读取器（根包 init 调用）。
func SetAssetDataLoader(f func(ctx context.Context, assetType, assetID string) ([]byte, bool)) {
	assetMu.Lock()
	assetDataLoader = f
	assetMu.Unlock()
}

func loadAssetFromDB(ctx context.Context, assetType, assetID string) ([]byte, bool) {
	assetMu.RLock()
	f := assetDataLoader
	assetMu.RUnlock()
	return f(ctx, assetType, assetID)
}

// ── 随域迁入的小工具（原根包 helper）──

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func toJSONMap(m map[string]any) model.JSONMap {
	out := make(model.JSONMap, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toJSONMapBytes(data []byte) model.JSONMap {
	out := model.JSONMap{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func envFlagEnabled(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if v == "" {
		return false
	}
	if ok, err := strconv.ParseBool(v); err == nil {
		return ok
	}
	switch v {
	case "yes", "y", "on":
		return true
	}
	return false
}
