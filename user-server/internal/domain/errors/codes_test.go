package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestBizError_Error_WithoutCause(t *testing.T) {
	e := New(CodeNotFound, "资源不存在")
	if got := e.Error(); got != "资源不存在" {
		t.Fatalf("Error() = %q, want %q", got, "资源不存在")
	}
}

func TestBizError_Error_WithCause(t *testing.T) {
	cause := errors.New("dial tcp: refused")
	e := Wrap(CodeInternal, "内部错误", cause)
	got := e.Error()
	if !strings.HasPrefix(got, "内部错误: ") || !strings.HasSuffix(got, cause.Error()) {
		t.Fatalf("Error() = %q, want 前缀「内部错误: 」+ cause 文本", got)
	}
}

func TestBizError_Unwrap_ErrorsIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	e := Wrap(CodeSyncFailed, "同步失败", sentinel)
	if !errors.Is(e, sentinel) {
		t.Fatal("errors.Is 应穿透 Unwrap 命中哨兵错误")
	}
	plain := New(CodeSyncFailed, "同步失败")
	if errors.Is(plain, sentinel) {
		t.Fatal("无 Cause 的 BizError 不应命中任意哨兵")
	}
}

func TestNewAndWrap_Fields(t *testing.T) {
	e := New(CodeAssetDup, "资产重复")
	if e.Code != CodeAssetDup || e.Message != "资产重复" || e.Cause != nil {
		t.Fatalf("New 字段错误: %+v", e)
	}
	cause := errors.New("x")
	w := Wrap(CodeLoaderFallback, "降级", cause)
	if w.Code != CodeLoaderFallback || w.Cause != cause {
		t.Fatalf("Wrap 字段错误: %+v", w)
	}
}

func TestCodes_UniqueAndLayered(t *testing.T) {
	codes := map[string]int{
		"CodeSuccess": CodeSuccess, "CodeParamInvalid": CodeParamInvalid,
		"CodeUnauthorized": CodeUnauthorized, "CodeForbidden": CodeForbidden,
		"CodeNotFound": CodeNotFound, "CodeConflict": CodeConflict,
		"CodeInternal": CodeInternal, "CodePlatformUnavail": CodePlatformUnavail,
		"CodeAssetNotFound": CodeAssetNotFound, "CodeAssetDup": CodeAssetDup,
		"CodeAssetInvalid": CodeAssetInvalid, "CodeSyncFailed": CodeSyncFailed,
		"CodeLoaderFallback": CodeLoaderFallback,
	}
	seen := map[int]string{}
	for name, c := range codes {
		if prev, dup := seen[c]; dup {
			t.Fatalf("%s=%d 与 %s 重复", name, c, prev)
		}
		seen[c] = name
	}
	if CodeSuccess != 0 {
		t.Fatal("CodeSuccess 必须为 0")
	}
	for name, c := range codes {
		if name == "CodeSuccess" {
			continue
		}
		if c < 4000 || c > 6999 {
			t.Fatalf("%s=%d 超出 4000–6999 业务码段约定", name, c)
		}
	}
}
