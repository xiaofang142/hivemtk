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
// panic 收口：goroutine 内 recover（对齐本包文档「统一处理 panic 恢复」自述），
// recover 后 logger.Errorf 带 name/stack（形态对齐 utils/recover.go 的 defaultOnPanic）；
// 仅防进程崩，不吞业务错误语义。生产调用点在知识库导入/重建索引链。
//
// 使用场景：
//   - 文档导入后的异步处理
//   - 索引重建
//   - 外部数据同步
func RunWithTimeout(ctx context.Context, timeout time.Duration, fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[async.RunWithTimeout] panic recovered: %v\n%s", r, debug.Stack())
			}
		}()
		newCtx, cancel := context.WithTimeout(DetachContext(ctx), timeout)
		defer cancel()
		fn(newCtx)
	}()
}

// RunDetach 启动异步任务，不带超时控制（适用于长时间任务）
//
// panic 收口同 RunWithTimeout：recover 后 logger.Errorf 带 name/stack。
func RunDetach(ctx context.Context, fn func(ctx context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[async.RunDetach] panic recovered: %v\n%s", r, debug.Stack())
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
