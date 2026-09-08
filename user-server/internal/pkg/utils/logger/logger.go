package logger

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// LoggingConfig 全局日志配置，对应 config.yaml 的 logging: 段。
// 统一日志系统以 zerolog 为唯一实现，所有模块（HTTP/WebSocket/编排/触达）
// 都通过本包输出日志，从而保证级别、格式、落盘、trace 透传一致。
type LoggingConfig struct {
	Level     string `yaml:"level"`
	Format    string `yaml:"format"`
	Output    string `yaml:"output"`
	File      string `yaml:"file"`
	MaxSizeMB int    `yaml:"max_size"`
	Component string `yaml:"component"`
}

// DefaultConfig 返回生产友好的默认配置：info 级、控制台、组件名 user-server。
func DefaultConfig() LoggingConfig {
	return LoggingConfig{
		Level:     "info",
		Format:    "console",
		Output:    "stdout",
		File:      "logs/user-server.log",
		MaxSizeMB: 100,
		Component: "user-server",
	}
}

var (
	mu   sync.RWMutex
	inst *zerolog.Logger
	conf LoggingConfig
)

func parseLevel(s string) zerolog.Level {
	switch s {
	case "debug":
		return zerolog.DebugLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func applyDefaults(c LoggingConfig) LoggingConfig {
	if c.Level == "" {
		c.Level = "info"
	}
	if c.Format == "" {
		c.Format = "console"
	}
	if c.Output == "" {
		c.Output = "stdout"
	}
	if c.Component == "" {
		c.Component = "user-server"
	}
	if (c.Output == "file" || c.Output == "both") && c.File == "" {
		c.File = "logs/user-server.log"
	}
	if c.MaxSizeMB <= 0 {
		c.MaxSizeMB = 100
	}
	return c
}

// InitLogger 依据配置初始化全局日志器；可重复调用，以最后一次为准（幂等）。
func InitLogger(c LoggingConfig) {
	c = applyDefaults(c)

	mu.Lock()
	conf = c
	mu.Unlock()

	level := parseLevel(c.Level)

	var writer io.Writer
	switch c.Output {
	case "file":
		writer = newRotatingWriter(c.File, int64(c.MaxSizeMB)*1024*1024)
	case "both":
		writer = zerolog.MultiLevelWriter(
			consoleWriter(c.Format),
			newRotatingWriter(c.File, int64(c.MaxSizeMB)*1024*1024),
		)
	default:
		writer = consoleWriter(c.Format)
	}

	z := zerolog.New(writer).
		Level(level).
		With().
		Timestamp().
		Str("component", c.Component).
		Logger()

	mu.Lock()
	inst = &z
	mu.Unlock()
}

func consoleWriter(format string) io.Writer {
	if format == "json" {
		return os.Stdout
	}
	return zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
}

// GetLogger 返回全局日志器；未初始化时惰性使用默认配置，保证任意调用都不会 panic。
func GetLogger() *zerolog.Logger {
	mu.RLock()
	l := inst
	mu.RUnlock()
	if l != nil {
		return l
	}
	InitLogger(DefaultConfig())
	mu.RLock()
	l = inst
	mu.RUnlock()
	return l
}

type ctxKey int

const (
	traceIDKey ctxKey = iota
	moduleKey
)

// GenerateTraceID 生成分布式追踪 ID（UUID v4）。
func GenerateTraceID() string {
	return uuid.NewString()
}

// WithTraceID 将 trace_id 注入 context；为空时自动生成，保证链路必有追踪标识。
// 防御：ctx 为 nil 时回退到 context.Background()，避免 context.WithValue(nil, ...) panic。
func WithTraceID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		id = GenerateTraceID()
	}
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFromContext 从 context 取出 trace_id（无则返回空串）。
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

// WithModule 将模块名（子系统标识）注入 context，如 orchestrator / reach / websocket。
// 防御：ctx 为 nil 时回退到 context.Background()，避免 context.WithValue(nil, ...) panic。
func WithModule(ctx context.Context, module string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, moduleKey, module)
}

func moduleFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(moduleKey).(string); ok {
		return v
	}
	return ""
}

// Ctx 返回绑定了 trace_id 与 module 的子日志器；context 中无追踪信息时回退全局日志器。
// 这是业务代码记录结构化日志的主入口：logger.Ctx(ctx).Info().Str("k","v").Msg("...")
func Ctx(ctx context.Context) *zerolog.Logger {
	base := GetLogger()
	if ctx == nil {
		return base
	}
	tid := TraceIDFromContext(ctx)
	mod := moduleFromContext(ctx)
	if tid == "" && mod == "" {
		return base
	}
	ev := base.With()
	if tid != "" {
		ev = ev.Str("trace_id", tid)
	}
	if mod != "" {
		ev = ev.Str("module", mod)
	}
	c := ev.Logger()
	return &c
}

// Debug 记录 debug 级日志。
func Debug(msg string) { GetLogger().Debug().Msg(msg) }

// Debugf 格式化记录 debug 级日志。
func Debugf(format string, args ...any) { GetLogger().Debug().Msgf(format, args...) }

// Info 记录 info 级日志。
func Info(msg string) { GetLogger().Info().Msg(msg) }

// Infof 格式化记录 info 级日志。
func Infof(format string, args ...any) { GetLogger().Info().Msgf(format, args...) }

// Warn 记录 warn 级日志。
func Warn(msg string) { GetLogger().Warn().Msg(msg) }

// Warnf 格式化记录 warn 级日志。
func Warnf(format string, args ...any) { GetLogger().Warn().Msgf(format, args...) }

// Error 记录 error 级日志，并附带 error 对象。
func Error(err error, msg string) { GetLogger().Error().Err(err).Msg(msg) }

// Errorf 格式化记录 error 级日志。
func Errorf(format string, args ...any) { GetLogger().Error().Msgf(format, args...) }

type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	size     int64
	f        *os.File
}

func newRotatingWriter(path string, maxBytes int64) *rotatingWriter {
	return &rotatingWriter{path: path, maxBytes: maxBytes}
}

func (w *rotatingWriter) ensure() error {
	if w.f != nil {
		return nil
	}
	if dir := filepath.Dir(w.path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	if fi, err := f.Stat(); err == nil {
		w.size = fi.Size()
	}
	return nil
}

func (w *rotatingWriter) rotate() error {
	_ = w.f.Close()
	backup := w.path + ".1"
	_ = os.Rename(w.path, backup)
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	w.size = 0
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensure(); err != nil {
		return 0, err
	}
	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// StdLogger 返回 io.Writer 兼容的标准 logger 适配器，
// 供 cron.PrintfLogger 等需要 *log.Logger 的第三方库使用（Write 会带 \n，去掉多余换行）。
type stdLoggerAdapter struct{}

func (stdLoggerAdapter) Write(p []byte) (int, error) {
	Infof("%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// StdLogger 返回全局日志器的 *log.Logger 视图。
func StdLogger() *log.Logger {
	return log.New(stdLoggerAdapter{}, "", 0)
}
