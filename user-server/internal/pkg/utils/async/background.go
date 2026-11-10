// Package async 提供异步任务工具，统一处理 context 取消、超时、panic 恢复
package async

import (
	"context"
	"runtime/debug"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

// RunWithTimeout 启动异步任务，保留原 ctx 的 Value 但使用新超时控制
//
// 解决：当 HTTP 请求结束时原 ctx 会被 cancel，异步任务会立即终止。
// 此函数保留原 ctx 的 trace ID 等 Value，但用 context.Background() 作为 parent，
// 应用指定 timeout 控制最大运行时长，防止文档永久卡在 processing 状态。
//
// 使用场景：
//   - 文档导入后的异步处理
//   - 索引重建
//   - 外部数据同步
func RunWithTimeout(ctx context.Context, timeout time.Duration, fn func(ctx context.Context)) {
	go func() {
		// 本包是异步启动的统一入口（HTTP 请求路径大量复用）：fn 的 panic
		// 在这里兜底，不能击穿进程（与下方 SafeGo 同一口径）。
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[RunWithTimeout] panic recovered: %v\n%s", r, debug.Stack())
			}
		}()
		newCtx, cancel := context.WithTimeout(DetachContext(ctx), timeout)
		defer cancel()
		fn(newCtx)
	}()
}

// RunDetach 启动异步任务，不带超时控制（适用于长时间任务）
func RunDetach(ctx context.Context, fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[RunDetach] panic recovered: %v\n%s", r, debug.Stack())
			}
		}()
		fn(DetachContext(ctx))
	}()
}

// DetachContext 分离 ctx 的取消链，但保留 Value
//
// context.WithoutCancel 是 Go 1.21+ 的标准方法。
// 如果项目使用更早版本，可以用以下等价实现替换：
//
//	return context.WithoutCancel(ctx)
func DetachContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// SafeGo 安全启动 goroutine，捕获 panic 防止进程崩溃
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[SafeGo] panic recovered: %v\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}
