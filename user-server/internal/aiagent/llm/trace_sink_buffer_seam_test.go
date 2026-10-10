package llm

import (
	"testing"
)

func TestDBSinkBufferSizeSeam(t *testing.T) {
	reset := func() { SetDBSinkBufferSizeProvider(nil) }
	t.Cleanup(reset)
	reset()

	if got := DBSinkBufferSize(); got != DefaultDBSinkBufferSize {
		t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, DefaultDBSinkBufferSize)
	}

	SetDBSinkBufferSizeProvider(func() int { return 8192 })
	if got := DBSinkBufferSize(); got != 8192 {
		t.Errorf("注入 8192 未生效：got %v", got)
	}

	// 0 意味着 unbuffered channel：OnEvent 的非阻塞投递在高频 trace 下会大面积走
	// default 分支丢事件，而丢弃只累加计数器不报错，表现为"追踪数据莫名少了一截"。
	SetDBSinkBufferSizeProvider(func() int { return 0 })
	if got := DBSinkBufferSize(); got != DefaultDBSinkBufferSize {
		t.Errorf("注入 0 未回落兜底：got %v，期望 %v", got, DefaultDBSinkBufferSize)
	}
	SetDBSinkBufferSizeProvider(func() int { return -1 })
	if got := DBSinkBufferSize(); got != DefaultDBSinkBufferSize {
		t.Errorf("注入 -1 未回落兜底：got %v，期望 %v", got, DefaultDBSinkBufferSize)
	}

	reset()
	if got := DBSinkBufferSize(); got != DefaultDBSinkBufferSize {
		t.Errorf("传 nil 撤销注入后 = %v，期望 %v", got, DefaultDBSinkBufferSize)
	}
}

func TestNewDBTraceSinkPicksUpBufferSizeSeam(t *testing.T) {
	reset := func() { SetDBSinkBufferSizeProvider(nil) }
	t.Cleanup(reset)
	reset()

	if got := cap(NewDBTraceSink(nil).buffer); got != DefaultDBSinkBufferSize {
		t.Fatalf("兜底下 sink 缓冲容量 = %d，期望 %d", got, DefaultDBSinkBufferSize)
	}

	SetDBSinkBufferSizeProvider(func() int { return 512 })
	if got := cap(NewDBTraceSink(nil).buffer); got != 512 {
		t.Errorf("注入 512 后 sink 缓冲容量 = %d，期望 512", got)
	}

	SetDBSinkBufferSizeProvider(func() int { return 0 })
	if got := cap(NewDBTraceSink(nil).buffer); got != DefaultDBSinkBufferSize {
		t.Errorf("注入 0 后 sink 缓冲容量 = %d，期望回落 %d", got, DefaultDBSinkBufferSize)
	}
}
