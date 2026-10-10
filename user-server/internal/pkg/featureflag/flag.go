// Package featureflag 提供企业级 FeatureFlag 支持
//
// 设计依据: AI 智能体性能优化
//   - 支持灰度发布 + 一键回滚
//   - 6 个核心开关: parallel / stream / layer1 / fallback_chain / debug_log / sse_bridge
//   - 通过 env (FF_XXX=1) 注入, 无需重启即可热加载
//   - 所有 flag 默认关闭 (安全)
//
// 使用方式:
//
//	import "hivemtk-user/internal/pkg/featureflag"
//
//	if featureflag.Flag("parallel").Bool() {
//	    // 启用并行化
//	}
package featureflag

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// FF_ENABLE_SSE_BRIDGE 控制 Bridge 出站是否使用 SSE（true）还是长轮询（false）
	FF_ENABLE_SSE_BRIDGE = "sse_bridge"
)

// DefaultPollInterval 热加载轮询周期的代码兜底（参数中心 telemetry.feature_flag_poll_interval
// 未配置时用）。
// 每 5s 重新读取 env, 实现 "改 env 不重启" 的热加载。
// 5s 平衡了响应延迟和 CPU 开销 (每秒 0.2 次 env 读取, 对 5 个 flag 几乎无负担)。
const DefaultPollInterval = 5 * time.Second

var pollIntervalProvider func() time.Duration

// SetPollIntervalProvider 注入热加载轮询周期；传 nil 视为不注入。
func SetPollIntervalProvider(fn func() time.Duration) { pollIntervalProvider = fn }

// PollInterval 生效的热加载轮询周期。非正值一律回落兜底——0 会让 time.NewTicker(0)
// 直接 panic，把轮询 goroutine 的启动失败变成进程级崩溃。
func PollInterval() time.Duration {
	if p := pollIntervalProvider; p != nil {
		if d := p(); d > 0 {
			return d
		}
	}
	return DefaultPollInterval
}

// Flag 表示一个 FeatureFlag 实例 (线程安全)
type Flag struct {
	name         string
	defaultValue bool
	mu           sync.RWMutex
	lastReload   time.Time

	cachedValue bool
}

// FlagManager 全局 Flag 管理器
type FlagManager struct {
	flags    map[string]*Flag
	mu       sync.RWMutex
	pollOnce sync.Once
	stopCh   chan struct{}
	stopped  bool
}

var (
	defaultManager *FlagManager
	defaultOnce    sync.Once
)

// DefaultManager 获取默认 Flag 管理器
func DefaultManager() *FlagManager {
	defaultOnce.Do(func() {
		defaultManager = &FlagManager{flags: make(map[string]*Flag)}
		defaultManager.register("parallel", false)
		defaultManager.register("stream", false)
		defaultManager.register("layer1", false)
		defaultManager.register("fallback_chain", false)
		defaultManager.register("debug_log", false)
		defaultManager.register(FF_ENABLE_SSE_BRIDGE, true)
		defaultManager.startPoller()
	})
	return defaultManager
}

func (m *FlagManager) register(name string, defaultValue bool) *Flag {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flags[name]
	if !ok {
		f = &Flag{
			name:         name,
			defaultValue: defaultValue,
			cachedValue:  defaultValue,
		}
		f.lastReload = time.Now()
		f.cachedValue = f.readEnv()
		m.flags[name] = f
	}
	return f
}

func (m *FlagManager) startPoller() {
	m.pollOnce.Do(func() {
		m.stopCh = make(chan struct{})
		go func() {
			ticker := time.NewTicker(PollInterval())
			defer ticker.Stop()
			for {
				select {
				case <-m.stopCh:
					return
				case <-ticker.C:
					m.ReloadAll()
				}
			}
		}()
	})
}

// StopPoller 停止后台轮询 (主要用于单测 / 优雅关闭)
func (m *FlagManager) StopPoller() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	m.stopped = true
	if m.stopCh != nil {
		close(m.stopCh)
	}
}

// Flag 获取或创建指定名称的 Flag
func Get(name string) *Flag {
	return DefaultManager().register(name, false)
}

// MustGet 同 Get, 在 flag 不存在时 panic (用于必填 flag)
func MustGet(name string) *Flag {
	f := Get(name)
	if f == nil {
		panic("featureflag: must register flag first: " + name)
	}
	return f
}

// Bool 返回 Flag 的布尔值 (从缓存读取, 后台轮询刷新)
//
// 规则:
//   - env FF_<NAME>=1 / true / yes -> true
//   - env FF_<NAME>=0 / false / no / "" -> false
//   - env 未设置 -> 使用 defaultValue
//
// 不再每次都读 env, 而是返回 cachedValue, 由后台 goroutine 每 5s 刷新。
// 业务代码调用 f.Bool() 数十次/请求, 缓存避免重复系统调用。
func (f *Flag) Bool() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cachedValue
}

// EnvNameOf 返回 flag 对应的环境变量名。
//
// 与 readEnv 同源：告警/运维端点提示"该开哪个 env"时必须用这里算，
// 否则会出现提示写 FF_A_B、代码读 FF_A.B 这种查不出来的漂移。
//
// 点号（以及任何不是 [A-Z0-9_] 的字符）在这里翻成下划线：flag 名里点号是合法的
// （`ai.safety.tool_approval_gate` 这种分层写法），但**环境变量名里不是** ——
// shell 里 `export FF_AI.SAFETY.TOOL_APPROVAL_GATE=1` 直接报 invalid variable name，
// 于是"提示让你开的那个变量"永远开不上：旗子在端点上照旧读作 off，运维反复确认
// 自己写对了名字，白名单却一次也没生效过。提示与读取必须落在同一个可用的名字上。
//
// 不做的事：不把名字压成去重后的同一片（`a.b` 与 `a_b` 会撞名）—— 现有注册表里
// 没有这种成对的名字，而防撞名的做法要么加后缀要么判冲突，都超出"把点号写对"这件事。
// 撞名这件事由测试盯着（见 flag_envname_test.go）。
func EnvNameOf(name string) string {
	var b strings.Builder
	b.WriteString("FF_")
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// legacyEnvName 点号原名直接拼出来的那份（`FF_AI.SAFETY.TOOL_APPROVAL_GATE`）。
// 它不可从 shell 设置，却**可能**已经在 compose/k8s 的 env 映射里被人照着旧提示写过
// （那些地方允许点号键名）。归一化之后如果只认新名字，那种写法会静默变成 off ——
// 一把安全闸门开关从"开着"掉回"关着"，且没有任何一处报错。读的时候两份都看，新名优先。
func legacyEnvName(name string) string {
	return "FF_" + strings.ToUpper(name)
}

func (f *Flag) readEnv() bool {
	return f.readEnvNamed(EnvNameOf(f.name), legacyEnvName(f.name))
}

// readEnvNamed 按优先级读两个候选变量名。
// 拆开是因为"未设置"与"设置了空串"在旧行为里是两种判读（空串回落到默认值），
// 这个区别必须留在同一处，别在两个候选名之间丢失它。
func (f *Flag) readEnvNamed(names ...string) bool {
	v := ""
	for _, n := range names {
		if got, ok := os.LookupEnv(n); ok {
			v = got
			break
		}
	}
	if v == "" {
		return f.defaultValue
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		switch strings.ToLower(v) {
		case "yes", "y", "on":
			return true
		case "no", "n", "off":
			return false
		}
		return f.defaultValue
	}
	return b
}

func (f *Flag) resolve() bool { //nolint:unused //// 仅被 *_test.go 引用，生产路径未用
	f.mu.Lock()
	defer f.mu.Unlock()
	val := f.readEnv()
	f.cachedValue = val
	return val
}

// String 返回 flag 名称 (调试用)
func (f *Flag) String() string {
	return f.name
}

// LastReload 返回最后一次 reload 时间
func (f *Flag) LastReload() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastReload
}

// ReloadAll 重新加载所有 flag (可由 SIGHUP handler / admin API / 后台 poller 触发)
//
// 立即读一次 env 刷新所有 flag 的 cachedValue, 同时更新 lastReload。
// 后台 poller 每 5s 自动调用一次; SIGHUP handler / admin API 触发即时刷新。
func (m *FlagManager) ReloadAll() {
	m.mu.RLock()
	flags := make([]*Flag, 0, len(m.flags))
	for _, f := range m.flags {
		flags = append(flags, f)
	}
	m.mu.RUnlock()

	now := time.Now()
	for _, f := range flags {
		f.mu.Lock()
		f.cachedValue = f.readEnv()
		f.lastReload = now
		f.mu.Unlock()
	}
}

// Snapshot 返回所有 flag 的当前值快照 (用于 /healthz 调试)
func (m *FlagManager) Snapshot() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]bool, len(m.flags))
	for name, f := range m.flags {
		f.mu.RLock()
		out[name] = f.cachedValue
		f.mu.RUnlock()
	}
	return out
}

// AllFlagSnapshot 便捷函数
func AllFlagSnapshot() map[string]bool {
	return DefaultManager().Snapshot()
}
