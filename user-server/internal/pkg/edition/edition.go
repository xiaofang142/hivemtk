// Package edition 提供"版本类型"（开源版/定制版）的统一控制方案。
//
// 设计要点（统一控制方案，供后续类似功能复用）：
//   - 版本类型是"商户/实例"级别的控制键，取值 opensource | custom。
//   - 本地兜底：环境变量 EDITION / VERSION_TYPE（默认 opensource）。
//   - 平台侧可覆盖：平台商户管理设置 version_type 后，通过心跳响应下发，
//     user-server 调用 SetCurrent 应用；后续所有基于 edition 的功能统一走
//     edition.Current() / edition.Allows(feature) 判定，避免散落的 if/else。
//   - 功能 entitlement 映射集中在此包：新增功能只需在 featureEntitlements
//     登记，消费方调用 edition.Allows(featureKey)。
package edition

import (
	"context"
	"os"
	"strings"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	TypeOpensource = "opensource"
	TypeCustom     = "custom"

	// FeatureDomesticChannels 开源版不包含国内渠道（减少传播风险）。
	FeatureDomesticChannels = "channels.domestic"
)

// featureEntitlements 各版本类型允许的功能集合。
// custom（定制版）全量开放；opensource 仅开放国外渠道相关能力。
var featureEntitlements = map[string]map[string]bool{
	TypeOpensource: {
		FeatureDomesticChannels: false,
	},
	TypeCustom: {
		FeatureDomesticChannels: true,
	},
}

var (
	mu      sync.RWMutex
	current = TypeOpensource
)

func init() {
	if v := strings.TrimSpace(os.Getenv("EDITION")); v != "" {
		current = normalize(v)
	}
	if v := strings.TrimSpace(os.Getenv("VERSION_TYPE")); v != "" {
		current = normalize(v)
	}
}

func normalize(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "custom", "enterprise", "定制版":
		return TypeCustom
	case "opensource", "community", "开源版", "":
		return TypeOpensource
	default:
		// 未知值按开源版保守处理（fail-closed：不开国内渠道）。
		return TypeOpensource
	}
}

// Current 返回当前生效的版本类型。
// 支持 Current() 与 Current(ctx) 两种调用形式。
func Current(ctx ...context.Context) string {
	if len(ctx) > 0 && ctx[0] != nil {
		if v, ok := FromContext(ctx[0]); ok {
			return v
		}
	}
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// SetCurrent 由平台心跳响应等外部来源覆盖本地版本类型（线程安全）。
// 空值/非法值忽略，保持当前值（fail-closed）。
// 变更时输出审计日志，便于追踪平台下发与本地生效的漂移。
func SetCurrent(v string) {
	n := normalize(v)
	if n == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if current != n {
		logger.Infof("[edition] version type changed: %s -> %s", current, n)
	}
	current = n
}

// contextKey 用于在请求上下文中携带版本类型覆盖值（多租户/测试场景）。
type contextKey struct{}

var editionContextKey = contextKey{}

// WithContext 返回携带版本类型覆盖值的新 context。
// 传入空/非法值时原样返回 ctx（保持全局兜底）。
func WithContext(ctx context.Context, v string) context.Context {
	if ctx == nil {
		return ctx
	}
	n := normalize(v)
	if n == "" {
		return ctx
	}
	return context.WithValue(ctx, editionContextKey, n)
}

// FromContext 从 context 读取版本类型覆盖值。
func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(editionContextKey).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// CurrentContext 返回当前生效的版本类型：优先 context 覆盖，其次全局值。
func CurrentContext(ctx context.Context) string {
	if v, ok := FromContext(ctx); ok {
		return v
	}
	return Current()
}

// AllowsContext 判断 context 对应版本是否允许某功能。
func AllowsContext(ctx context.Context, feature string) bool {
	cur := CurrentContext(ctx)
	mu.RLock()
	defer mu.RUnlock()
	if m, ok := featureEntitlements[cur]; ok {
		if v, ok2 := m[feature]; ok2 {
			return v
		}
	}
	return true
}

// ResetForTest 仅测试使用：重置全局版本类型（不记录审计）。
func ResetForTest(v string) {
	n := normalize(v)
	if n == "" {
		n = TypeOpensource
	}
	mu.Lock()
	defer mu.Unlock()
	current = n
}

// IsCustom 当前是否为定制版。
func IsCustom() bool { return Current() == TypeCustom }

// Allows 判断当前版本是否允许某功能。
//
// 为兼容不同调用方，支持以下调用形式：
//   - Allows(feature)：使用全局版本类型
//   - Allows(ctx, feature) / Allows(feature, ctx)：使用 context 覆盖
//
// 未登记的功能默认允许（向后兼容），避免新增功能被意外屏蔽。
func Allows(first interface{}, rest ...interface{}) bool {
	var ctx context.Context
	feature := ""
	for _, a := range append([]interface{}{first}, rest...) {
		switch v := a.(type) {
		case context.Context:
			ctx = v
		case string:
			if feature == "" {
				feature = v
			}
		}
	}
	if feature == "" {
		return true
	}
	if ctx != nil {
		return AllowsContext(ctx, feature)
	}
	mu.RLock()
	defer mu.RUnlock()
	cur := current
	if m, ok := featureEntitlements[cur]; ok {
		if v, ok2 := m[feature]; ok2 {
			return v
		}
	}
	return true
}

var domesticChannelCodes = []string{
	"douyin", "xiaohongshu", "kuaishou", "xianyu", "wecom", "qq", "feishu",
}

// DomesticChannelCodes 返回国内渠道码集合的副本，供 repository 构造参数化
// 版本门。调用方修改返回值不得影响 edition 单一事实源。
func DomesticChannelCodes() []string {
	return append([]string(nil), domesticChannelCodes...)
}

// IsDomesticChannel 判断渠道码是否属于国内渠道（开源版需过滤）。
func IsDomesticChannel(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, domestic := range domesticChannelCodes {
		if code == domestic {
			return true
		}
	}
	return false
}
