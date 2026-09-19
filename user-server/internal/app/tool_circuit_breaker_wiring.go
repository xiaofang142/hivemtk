package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/pkg/utils/logger"
)

// W-3 工具熔断接线（新规划任务清单 T-P1-04）
//
// tooluse 里的 CircuitBreakerRegistry 早就实现完整（状态机 + 指数退避 + 半开配额，
// 单测覆盖），但生产从未构造过它：`ToolExecutorConfig.CircuitBreaker` 一直是零值 nil，
// `BuildChainWithCircuitBreaker` 走 nil 分支退化成 `BuildDefaultChain` ⇒ 熔断永不生效。
// 本文件补的就是那一行赋值，外加"先观察、后生效"的灰度形态。
//
// 三态而非开关：
//
//	off（默认）    不构造熔断器，装饰链与接线前逐字节一致
//	shadow         照常记账照常判定，但绝不拦下请求，只把"本会被拦"计数 + 打日志
//	enforce        判定拒绝即返回 ErrCircuitOpen
//
// shadow 是这一卡的正确交付形态，理由：熔断是 🔴行为变更（会把工具调用变成错误返回，
// 上游 LLM 会看到"工具不可用"并改变行为），而在给出"这一周有多少调用会被拦"的实测对比
// 之前，没人知道阈值 5/冷却 30s 对这个系统是不是合适的量级。T-P1-06 转阻断的前置条件
// 就是本文件累计出的那份对比报告。
//
// 注意与 ToolRouter 的自带熔断区分：`RouterConfig.FailThreshold=5/Cooldown=30s` 是
// ToolRouter 内部的**工具切换**用的短路（失败即换同类工具），`/agent/tools/circuit/reset`
// 重置的是它。本文件挂的是 executor 装饰链上的按工具熔断，两者独立计量、互不共享状态。

// 环境变量名集中在此，避免"文档写一个、代码读另一个"。
const (
	circuitFlagEnv          = "FF_TOOL_CIRCUIT_BREAKER"
	circuitThresholdEnv     = "TOOL_CIRCUIT_FAILURE_THRESHOLD"
	circuitBaseCooldownEnv  = "TOOL_CIRCUIT_BASE_COOLDOWN"
	circuitMaxCooldownEnv   = "TOOL_CIRCUIT_MAX_COOLDOWN"
	circuitBackoffEnv       = "TOOL_CIRCUIT_BACKOFF_MULTIPLIER"
	circuitHalfOpenEnv      = "TOOL_CIRCUIT_HALF_OPEN_ATTEMPTS"
	circuitThresholdMax     = 1000
	circuitHalfOpenMaxLimit = 100
)

type toolCircuitMode string

const (
	toolCircuitOff     toolCircuitMode = "off"
	toolCircuitShadow  toolCircuitMode = "shadow"
	toolCircuitEnforce toolCircuitMode = "enforce"
)

// circuitStateRef 当前生效的熔断接线状态；由 applyToolCircuitBreaker 在装配期写一次。
//
// 读写无锁的前提：写入发生在 router.Setup() 里、executor 与 HTTP 服务启动之前
// （与同包的 memAuditLogger / globalToolRouter 同一口径）。若将来要支持运行中热切三态，
// 必须连同 executor 的 handler 缓存失效一起做，光换这几个字段不会让已缓存的链重新装饰。
var (
	circuitModeValue     = string(toolCircuitOff)
	circuitRegistry      *tooluse.CircuitBreakerRegistry
	circuitDecisions     *tooluse.CircuitDecisionCounter
	circuitConfigApplied tooluse.CircuitBreakerConfig
)

// parseToolCircuitMode 解析开关值。
//
// 关键取舍：布尔式真值（true/1/on/yes）只映射到 **shadow**，不映射到 enforce。
// 一把能改变生产行为的旗子，不该因为有人按习惯写了 `=true` 就直接拿到"拦下请求"的能力。
// 要真拦，必须显式写 enforce/block。认不出的值一律判 off 并告警（与 envFlagEnabled 同口径）。
func parseToolCircuitMode(raw string) toolCircuitMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return toolCircuitOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return toolCircuitOff
	case "shadow", "observe", "watch", "log", "report":
		return toolCircuitShadow
	case "enforce", "block", "active":
		return toolCircuitEnforce
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return toolCircuitShadow
		}
		return toolCircuitOff
	}
	switch v {
	case "yes", "y", "on":
		return toolCircuitShadow
	}
	logger.Warnf("[tool-circuit] %s=%q 无法识别 ⇒ 按 off 处理（熔断不生效）；可用值：off|shadow|enforce", circuitFlagEnv, raw)
	return toolCircuitOff
}

// envInt 取整数配置；第二个返回值为 false 表示未配或越界（越界时已告警）。
//
// logTag 由调用方给出：同一个解析器同时服务熔断与落库两把旗子，告警前缀写死会让
// `TOOL_AUDIT_QUEUE_SIZE` 配错的运维被指到熔断那个文件去。
func envInt(logTag, env string, lo, hi int) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		logger.Warnf("%s %s=%q 不是整数 ⇒ 沿用默认值", logTag, env, raw)
		return 0, false
	}
	if n < lo || n > hi {
		logger.Warnf("%s %s=%d 超出可用区间 [%d,%d] ⇒ 沿用默认值（越界配置比不设配置更危险）", logTag, env, n, lo, hi)
		return 0, false
	}
	return n, true
}

// envDuration 取时长配置；上限用于挡住"把 30s 写成 30"这类量级事故。
func envDuration(env string, min, max time.Duration) (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return 0, false
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		logger.Warnf("[tool-circuit] %s=%q 不是合法时长（如 30s/5m）⇒ 沿用默认值", env, raw)
		return 0, false
	}
	if d < min || d > max {
		logger.Warnf("[tool-circuit] %s=%s 超出可用区间 [%s,%s] ⇒ 沿用默认值", env, d, min, max)
		return 0, false
	}
	return d, true
}

// toolCircuitConfigFromEnv 组装熔断配置。
//
// AC③（阈值走配置不硬编码）：五项全部可覆盖，未配则沿用 tooluse.DefaultCircuitBreakerConfig()。
// 每一项单独校验、单独回退——配错一项不应让其余四项跟着失效。
func toolCircuitConfigFromEnv() tooluse.CircuitBreakerConfig {
	cfg := tooluse.DefaultCircuitBreakerConfig()
	if n, ok := envInt("[tool-circuit]", circuitThresholdEnv, 1, circuitThresholdMax); ok {
		cfg.FailureThreshold = n
	}
	if d, ok := envDuration(circuitBaseCooldownEnv, time.Millisecond, time.Hour); ok {
		cfg.BaseCooldown = d
	}
	if d, ok := envDuration(circuitMaxCooldownEnv, time.Millisecond, 24*time.Hour); ok {
		cfg.MaxCooldown = d
	}
	if raw := strings.TrimSpace(os.Getenv(circuitBackoffEnv)); raw != "" {
		f, err := strconv.ParseFloat(raw, 64)
		switch {
		case err != nil:
			logger.Warnf("[tool-circuit] %s=%q 不是数字 ⇒ 沿用默认值", circuitBackoffEnv, raw)
		case f < 1.0 || f > 100:
			logger.Warnf("[tool-circuit] %s=%s 超出可用区间 [1,100] ⇒ 沿用默认值", circuitBackoffEnv, raw)
		default:
			cfg.BackoffMultiplier = f
		}
	}
	if n, ok := envInt("[tool-circuit]", circuitHalfOpenEnv, 1, circuitHalfOpenMaxLimit); ok {
		cfg.HalfOpenMaxAttempts = n
	}
	if cfg.MaxCooldown < cfg.BaseCooldown {
		logger.Warnf("[tool-circuit] max_cooldown(%s) < base_cooldown(%s) ⇒ 退避上限抬到 base，避免指数退避被反向夹住",
			cfg.MaxCooldown, cfg.BaseCooldown)
		cfg.MaxCooldown = cfg.BaseCooldown
	}
	return cfg
}

// applyToolCircuitBreaker 按环境变量把熔断挂到 executor 配置上，返回生效模式。
//
// 调用方：InitGlobalToolExecutor（建 executor 之前）。off 时显式把字段置回 nil，
// 这样"关旗 = 与接线前完全一致"是可断言的性质，而不是依赖没人赋值。
func applyToolCircuitBreaker(config *tooluse.ToolExecutorConfig) toolCircuitMode {
	mode := parseToolCircuitMode(os.Getenv(circuitFlagEnv))
	circuitModeValue = string(mode)

	if mode == toolCircuitOff {
		config.CircuitBreaker = nil
		config.CircuitBreakerShadow = false
		config.OnCircuitDecision = nil
		circuitRegistry = nil
		circuitDecisions = nil
		circuitConfigApplied = tooluse.CircuitBreakerConfig{}
		return mode
	}

	cfg := toolCircuitConfigFromEnv()
	circuitRegistry = tooluse.NewCircuitBreakerRegistry(cfg)
	circuitDecisions = tooluse.NewCircuitDecisionCounter()
	circuitConfigApplied = cfg

	config.CircuitBreaker = circuitRegistry
	config.CircuitBreakerShadow = mode == toolCircuitShadow
	config.OnCircuitDecision = observeToolCircuitDecision

	logger.Infof("[tool-circuit] ✅ 熔断已接线，模式=%s（threshold=%d base_cooldown=%s max_cooldown=%s backoff=%.1fx half_open_attempts=%d）",
		mode, cfg.FailureThreshold, cfg.BaseCooldown, cfg.MaxCooldown, cfg.BackoffMultiplier, cfg.HalfOpenMaxAttempts)
	if mode == toolCircuitShadow {
		logger.Infof("[tool-circuit] ⚠️ shadow 态：**不拦任何请求**，只记录判定。转 enforce 前先看 /agent/tools/circuit 的 would_block 报告")
	}
	return mode
}

// observeToolCircuitDecision 熔断判定的生产侧消费者：计数 + 仅在"会被拦"时打日志。
//
// 放行判定不打日志——每次工具调用两行、且绝大多数没有信息量；累计数字由 counter 负责。
func observeToolCircuitDecision(ctx context.Context, d tooluse.CircuitDecision) {
	circuitDecisions.Observe(ctx, d)
	if !d.WouldBlock {
		return
	}
	ev := logger.Ctx(ctx).Info()
	if !d.Shadow {
		ev = logger.Ctx(ctx).Warn()
	}
	ev.Str("event", "tool_circuit_decision").
		Bool("shadow", d.Shadow).
		Bool("would_block", d.WouldBlock).
		Str("tool_name", d.ToolName).
		Str("circuit_state", d.State.String()).
		Int32("consecutive_fails", d.ConsecutiveFails).
		Int32("open_count", d.OpenCount).
		Int("failure_threshold", d.FailureThreshold).
		Dur("base_cooldown", d.BaseCooldown).
		Msg("工具熔断判定：本会被拦下的调用")
}

// GetToolCircuitSnapshot 返回熔断接线状态快照，供调试 API 与灰度复盘读取。
//
// registry / decisions 在 off 时为 nil（调用方需 nil-check）。
func GetToolCircuitSnapshot() (mode string, config tooluse.CircuitBreakerConfig, registry *tooluse.CircuitBreakerRegistry, decisions *tooluse.CircuitDecisionCounter) {
	return circuitModeValue, circuitConfigApplied, circuitRegistry, circuitDecisions
}
