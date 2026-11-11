package async

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/utils/logger"
)

type ctxKey string

func TestDetachContext_KeepsValueStopsCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey("trace"), "t1"))
	detached := DetachContext(parent)
	cancel() // 取消父 ctx，detached 不应受影响

	if got := detached.Value(ctxKey("trace")); got != "t1" {
		t.Fatalf("value should survive detach, got %v", got)
	}
	select {
	case <-detached.Done():
		t.Fatal("detached ctx should not be cancelled by parent")
	default:
	}
}

func TestRunWithTimeout_TimeoutFires(t *testing.T) {
	done := make(chan struct{})
	RunWithTimeout(context.Background(), 30*time.Millisecond, func(ctx context.Context) {
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("task did not observe timeout in time")
	}
}

// captureAsyncStdout 换管抓 fn 期间写到 stdout 的日志（形态对齐 internal/app 真抓日志
// 用例 approval_wiring_test.go：logger 的 stdout 接缝是函数、写时读 os.Stdout，
// 所以换管子 + 重建 logger 即可真抓；不给 recover 塞假回调——AC 说的是
// 「错误日志带 name/stack」，只有经真 logger、真 encoder 落出去才算数）。
// 调用方必须在 fn 完成其异步 goroutine 的收尾（含 recover 日志写入）后再返回。
func captureAsyncStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "error", Format: "json", Output: "stdout"})
	defer func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	}()

	fn()

	// fn 返回前已确保 goroutine 内 recover+日志写入完成（见各用例内的等待+余量），
	// 此刻关写端再收全量，不会吞掉末尾那行错误日志。
	if err := w.Close(); err != nil {
		t.Fatalf("关闭写端失败：%v", err)
	}
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	return string(captured)
}

// TestRunWithTimeout_RecoversPanic panic 探针（CC-01 判据①）：
// fn 内 panic → 进程不崩（本用例能跑完即证：无 recover 时 goroutine panic 直接击穿测试
// 进程，测试二进制当场死）+ logger.Errorf 带 name（async.RunWithTimeout）+ stack。
func TestRunWithTimeout_RecoversPanic(t *testing.T) {
	done := make(chan struct{})
	s := captureAsyncStdout(t, func() {
		RunWithTimeout(context.Background(), time.Second, func(ctx context.Context) {
			defer close(done) // 内层 defer 先于外层 recover 执行：done 关闭即 panic 已展开到收口层
			panic("boom-withtimeout")
		})
		// done 关闭与 recover 日志写在同一 goroutine 的相邻两步栈帧上，
		// 留一拍余量保证日志落管再返回（captureAsyncStdout 要关写端）。
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("panic 应被 recover 后正常展开（done 未关闭）")
		}
		time.Sleep(200 * time.Millisecond)
	})
	if !strings.Contains(s, "panic recovered") {
		t.Fatalf("recover 日志缺失，实抓：%s", s)
	}
	if !strings.Contains(s, "async.RunWithTimeout") {
		t.Fatalf("recover 日志应带 name=async.RunWithTimeout，实抓：%s", s)
	}
	if !strings.Contains(s, "goroutine") {
		t.Fatalf("recover 日志应带 stack（goroutine 行），实抓：%s", s)
	}
}

// TestRunDetach_RecoversPanic panic 探针（RunDetach 同口径）。
func TestRunDetach_RecoversPanic(t *testing.T) {
	done := make(chan struct{})
	s := captureAsyncStdout(t, func() {
		RunDetach(context.Background(), func(ctx context.Context) {
			defer close(done)
			panic("boom-detach")
		})
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("panic 应被 recover 后正常展开（done 未关闭）")
		}
		time.Sleep(200 * time.Millisecond)
	})
	if !strings.Contains(s, "panic recovered") {
		t.Fatalf("recover 日志缺失，实抓：%s", s)
	}
	if !strings.Contains(s, "async.RunDetach") {
		t.Fatalf("recover 日志应带 name=async.RunDetach，实抓：%s", s)
	}
	if !strings.Contains(s, "goroutine") {
		t.Fatalf("recover 日志应带 stack（goroutine 行），实抓：%s", s)
	}
}
