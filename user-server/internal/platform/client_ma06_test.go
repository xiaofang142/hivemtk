package platform

// client_ma06_test.go —— MA-06：出站响应体封顶（读 cap+1 报超限，不静默截断）。

import (
	"io"
	"strings"
	"testing"
)

func TestReadPlatformBody_CapsAndErrors(t *testing.T) {
	// 恰好封顶：放行（边界含等号）。
	exact := strings.Repeat("a", int(maxRespBodyBytes))
	b, err := readPlatformBody(strings.NewReader(exact))
	if err != nil || len(b) != int(maxRespBodyBytes) {
		t.Fatalf("恰封顶应放行: len=%d err=%v", len(b), err)
	}
	// 封顶+1：报错（不静默截断）。
	over := io.LimitReader(strings.NewReader(strings.Repeat("a", 1<<30)), maxRespBodyBytes+1)
	if _, err := readPlatformBody(over); err == nil {
		t.Fatal("超限应报错（MA-06：不静默截断）")
	} else if !strings.Contains(err.Error(), "上限") {
		t.Errorf("超限报错文案: %v", err)
	}
	// 短体：原样返回。
	short := "hello"
	if b, err := readPlatformBody(strings.NewReader(short)); err != nil || b == nil || string(b) != short {
		t.Errorf("短体应原样返回: %q err=%v", b, err)
	}
}
