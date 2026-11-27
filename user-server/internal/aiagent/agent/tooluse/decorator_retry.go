package tooluse

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

type RetryPolicy interface {
	NextBackoff(attempt int, lastErr error) (delay time.Duration, ok bool)
	MaxAttempts() int
}

func RetryDecorator(policy RetryPolicy) ToolDecorator {
	return func(next ToolHandler) ToolHandler {
		return func(ctx context.Context, args map[string]any) (result ToolResult, err error) {
			toolName := GetToolName(ctx)
			if policy == nil {
				return next(ctx, args)
			}
			maxAttempts := policy.MaxAttempts()
			if maxAttempts < 1 {
				maxAttempts = 1
			}
			// 副作用不确定或高危工具禁止自动重放：必须由上层 ledger 对账后决定是否重试。
			if toolRetryForbidden(ctx) {
				maxAttempts = 1
			}
			var lastErr error
			for attempt := 0; attempt < maxAttempts; attempt++ {

				if ctx.Err() != nil {
					err = ctx.Err()

					return ErrorResult(toolName, err), err
				}

				if attempt > 0 {
					delay, ok := policy.NextBackoff(attempt, lastErr)
					if !ok {
						err = lastErr
						return ensureErrorResult(toolName, result, err), err
					}
					select {
					case <-ctx.Done():
						err = ctx.Err()
						return ErrorResult(toolName, err), err
					case <-time.After(delay):
					}
				}

				result, err = safeExecute(ctx, next, args)
				if err == nil && result.Success {
					result.Timing.RetryCount = attempt
					return result, nil
				}
				lastErr = err
				if err == nil && !result.Success {
					lastErr = fmt.Errorf("tool returned failure: %s", result.Error)
				}

				if isNonRetryableError(err) || isNonRetryableResult(result) {
					result = ensureErrorResult(toolName, result, lastErr)
					result.Timing.RetryCount = attempt
					return result, err
				}
			}

			result = ensureErrorResult(toolName, result, fmt.Errorf("重试 %d 次后仍失败：%v", maxAttempts, lastErr))
			result.Timing.RetryCount = maxAttempts - 1
			err = lastErr
			return result, err
		}
	}
}

func ensureErrorResult(toolName string, result ToolResult, err error) ToolResult {
	if result.Success || result.ToolName != "" || result.Error != "" || err == nil {
		return result
	}
	return ErrorResult(toolName, err)
}

func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	switch ClassifyToolError(err) {
	case ToolErrPermissionDenied, ToolErrApprovalDenied, ToolErrDNCBlocked,
		ToolErrInvalidParams, ToolErrRateLimited, ToolErrCircuitOpen:
		return true
	case ToolErrInternal:

		if errors.Is(err, context.Canceled) || errors.Is(err, ErrLoopDetected) {
			return true
		}
	case ToolErrTimeout:

		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "invalid argument") ||
		strings.Contains(errMsg, "validation failed") ||
		strings.Contains(errMsg, "参数校验失败") ||
		strings.Contains(errMsg, "参数无效") {
		return true
	}
	return false
}

func isNonRetryableResult(result ToolResult) bool {
	if result.Success {
		return false
	}
	errMsg := result.Error

	nonRetryablePatterns := []string{
		"not_found",
		"already_exists",
		"already_used",
		"expired",
		"insufficient_",
		"permission_denied",
		"invalid_argument",
		"validation_failed",
		"参数无效",
		"参数校验失败",
		"不存在",
		"已存在",
		"已使用",
		"已过期",
	}
	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}
	return false
}

func safeExecute(ctx context.Context, h ToolHandler, args map[string]any) (result ToolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			err = fmt.Errorf("%w: %v\n%s", ErrToolPanic, r, stack)
		}
	}()
	return h(ctx, args)
}
