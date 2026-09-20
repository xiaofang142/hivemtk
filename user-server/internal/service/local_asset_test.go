package service

import (
	"fmt"
	"testing"

	"hivemtk-user/internal/platform"
)

// R11 把"平台用 HTTP 200 承载的拒绝"收口到传输层，错误类型从
// `fmt.Errorf("platform error %d: %s")` 变成 *platform.PlatformError。
// 购买文案若继续按老字符串切片，用户就会看到
// "平台购买失败: platform request failed: status=200, code=4002, msg=余额不足"
// 这种把内部格式当产品文案的东西 —— 原因必须按结构化字段取。

func TestPurchaseFailMsg_UsesPlatformEnvelopeMsg(t *testing.T) {
	err := &platform.PlatformError{
		StatusCode: 200,
		RawBody:    `{"code":4002,"msg":"余额不足"}`,
		Resp:       &platform.BaseResp{Code: 4002, Msg: "余额不足"},
	}
	if got, want := purchaseFailMsg(err), "平台购买失败: 余额不足"; got != want {
		t.Fatalf("应取信封 msg，得到 %q 期望 %q", got, want)
	}
}

// TestPurchaseFailMsg_PlatformErrorWithoutMsg 反向闸门：平台没给 msg 时不能塌成空文案。
func TestPurchaseFailMsg_PlatformErrorWithoutMsg(t *testing.T) {
	err := &platform.PlatformError{StatusCode: 200, RawBody: `{"code":500}`}
	got := purchaseFailMsg(err)
	if got == "平台购买失败: " || got == "" {
		t.Fatalf("空 msg 时也要留下可定位的原因，得到 %q", got)
	}
}

func TestPurchaseFailMsg_KeepsTransportErrorReason(t *testing.T) {
	got := purchaseFailMsg(fmt.Errorf("dial tcp 10.0.0.1:8080: connect: connection refused"))
	if got != "平台购买失败: dial tcp 10.0.0.1:8080: connect: connection refused" {
		t.Fatalf("非平台拒绝应原样带上原因，得到 %q", got)
	}
}
