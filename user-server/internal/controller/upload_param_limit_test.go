package controller

import (
	"context"
	"testing"
)

// 阶段二 2a：把参数中心插进上传上限的优先级链，位置在 env 之后、system_config 兼容列之前。
//
//	env UPLOAD_MAX_SIZE > misc.upload_max_size_mb > system_config.max_upload_size_mb > 10MB 兜底
//
// 顺序是这批的全部风险点，所以逐格钉死，而不是只测"最终值是多少"。

func withUploadParamSeam(t *testing.T, bytes int64) {
	t.Helper()
	orig := resolveUploadParamMaxBytes
	resolveUploadParamMaxBytes = func(context.Context) int64 { return bytes }
	t.Cleanup(func() { resolveUploadParamMaxBytes = orig })
}

func TestUploadParamLayerSitsBetweenEnvAndCompatColumn(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadParamSeam(t, 64*1024*1024)
	withUploadMaxSizeSeam(t, 200*1024*1024)

	got := resolveUploadMaxSize(context.Background())
	if want := int64(64 * 1024 * 1024); got != want {
		t.Fatalf("参数中心 64MB 应压过兼容列 200MB：want %d, got %d", want, got)
	}
}

func TestUploadParamDoesNotShadowEnv(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "3145728") // 3MB
	withUploadParamSeam(t, 64*1024*1024)
	withUploadMaxSizeSeam(t, 200*1024*1024)

	if got := resolveUploadMaxSize(context.Background()); got != 3145728 {
		t.Fatalf("env 应压过参数中心：want 3145728, got %d", got)
	}
}

// 参数中心默认值是 0（= 不覆盖），必须原样穿过去落到兼容列，
// 而不是变成 "0MB" 把所有上传打死。
func TestUploadParamZeroFallsThroughToCompatColumn(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadParamSeam(t, 0)
	withUploadMaxSizeSeam(t, 200*1024*1024)

	if got := resolveUploadMaxSize(context.Background()); got != 200*1024*1024 {
		t.Fatalf("参数 0 应视为不覆盖并落到兼容列：want %d, got %d", 200*1024*1024, got)
	}
}

func TestUploadParamZeroWithNoCompatColumnFallsBackToHardcoded(t *testing.T) {
	t.Setenv("UPLOAD_MAX_SIZE", "")
	withUploadParamSeam(t, 0)
	withUploadMaxSizeSeam(t, 0)

	if got := resolveUploadMaxSize(context.Background()); got != MaxUploadSize {
		t.Fatalf("都为空时应兜底 10MB：want %d, got %d", MaxUploadSize, got)
	}
}

// 未注入时默认实现直读参数中心；没有装配 service 时 GlobalConfigParam() 返回无 repo 的
// stub，GetInt 落到 fallback 0 → 不覆盖。这条保证「装配前/装配后配成默认值」行为一致。
func TestResolveUploadParamMaxBytesDefaultsToNoOverride(t *testing.T) {
	if got := resolveUploadParamMaxBytes(context.Background()); got != 0 {
		t.Fatalf("未注入时默认实现应返回 0（不覆盖），got %d", got)
	}
}

func TestResolveUploadParamMaxBytesConvertsMBToBytes(t *testing.T) {
	t.Setenv("MAX_JSON_BODY_MB", "")
	// 直接验算换算：1MB 必须正好 1048576 字节，而不是 1000*1000。
	if got := mbToBytes(1); got != 1048576 {
		t.Fatalf("1MB 应为 1048576 字节，got %d", got)
	}
	if got := mbToBytes(0); got != 0 {
		t.Fatalf("0MB 应为 0（不覆盖），got %d", got)
	}
}
