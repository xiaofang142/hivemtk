package controller

import (
	"context"
	"testing"
)

// 阶段 0.5 的实测 bug：运维在「系统配置」页把上传上限调大，页面显示保存成功，
// 实际上传永远被 controller/upload.go 的 10MB 拦死——因为那条链路一处都不读配置。
// 这里把修好之后的优先级链逐格钉死：env > 库 > 代码兜底。
// 顺序本身就是最容易改错的地方（env 压过库还是库压过 env），只测"最终值"不够。

func withUploadMaxSizeSeam(t *testing.T, dbBytes int64) {
	t.Helper()
	orig := resolveMaxUploadBytes
	resolveMaxUploadBytes = func(context.Context) int64 { return dbBytes }
	t.Cleanup(func() { resolveMaxUploadBytes = orig })
}

func TestResolveUploadMaxSizePrefersEnvOverDB(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "3145728") // 3MB
	withUploadMaxSizeSeam(t, 200*1024*1024)

	if got := resolveUploadMaxSize(context.Background()); got != 3145728 {
		t.Fatalf("env 应压过库配置：want 3145728, got %d", got)
	}
}

func TestResolveUploadMaxSizeFallsToDBWhenEnvAbsent(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadMaxSizeSeam(t, 200*1024*1024)

	if got := resolveUploadMaxSize(context.Background()); got != 200*1024*1024 {
		t.Fatalf("库里 200MB 应生效：want %d, got %d", 200*1024*1024, got)
	}
}

// env 存在但解析不出正数时不能让整条链短路——否则一个写错的 env 会把管理员
// 刚在页面上调好的值重新盖回硬编码，那正是本次要修的那个病。
func TestResolveUploadMaxSizeInvalidEnvDoesNotShadowDB(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "abc")
	withUploadMaxSizeSeam(t, 200*1024*1024)

	if got := resolveUploadMaxSize(context.Background()); got != 200*1024*1024 {
		t.Fatalf("非法 env 不该压过库配置：want %d, got %d", 200*1024*1024, got)
	}
}

// 兜底那 10MB 必须还在：没有配置行的存量站点不能因为这次接线就突然放宽 5 倍。
func TestResolveUploadMaxSizeFallsBackToHardcodedWhenNothingElse(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadMaxSizeSeam(t, 0)

	if got := resolveUploadMaxSize(context.Background()); got != MaxUploadSize {
		t.Fatalf("无 env 无库值时应兜底 MaxUploadSize：want %d, got %d", MaxUploadSize, got)
	}
	if MaxUploadSize != 10*1024*1024 {
		t.Fatalf("兜底常量被改了，存量站点的上传上限会跟着变：want %d, got %d", 10*1024*1024, MaxUploadSize)
	}
}

// 这次修复的正面证据：调成 200MB 之后真的按 200MB 走，而不是继续 10MB。
func TestUploadMaxSizeConfigActuallyTakesEffect(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadMaxSizeSeam(t, 200*1024*1024)

	got := resolveUploadMaxSize(context.Background())
	if got <= 10*1024*1024 {
		t.Fatalf("库里配了 200MB 却仍被 10MB 拦死（这正是本次要修的 bug）：got %d", got)
	}
	if want := int64(200 * 1024 * 1024); got != want {
		t.Fatalf("want %d, got %d", want, got)
	}
}
