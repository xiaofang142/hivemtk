package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
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

// 自建资产的「名称」是列表里唯一的人类可读标识，空名称会建出一行空白记录：
// 列表上只能看到空白单元格，删除确认框显示「确认删除「」？」。
// 前端表单虽然标了 required，但没有 rules，曾经点「创建」直接提交空名称。
//
// 这里钉住 service 层：空白名称必须回客户端可纠正的 4000，不能落库。
func TestLocalAssetCreateManual_RejectsBlankName(t *testing.T) {
	svc := &LocalAssetService{}

	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := svc.CreateManual(context.Background(), &CreateAssetInput{
			Name:      name,
			AssetType: "agent_persona",
			Industry:  "美妆",
		})
		if err == nil {
			t.Fatalf("名称 %q 应被拒绝，实际创建成功", name)
		}
		if !bizerr.IsClientError(err) {
			t.Fatalf("名称 %q 应回客户端错误（400），实际：%v", name, err)
		}
		if !strings.Contains(err.Error(), "名称") {
			t.Fatalf("错误文案应指明名称，实际：%v", err)
		}
	}
}

// 名称列是 varchar(128)。超长值不拦会在 INSERT 时被数据库拒绝，
// 上层只看到「创建资产失败」5000，用户不知道是名字太长。
func TestLocalAssetCreateManual_RejectsOverlongName(t *testing.T) {
	svc := &LocalAssetService{}

	_, err := svc.CreateManual(context.Background(), &CreateAssetInput{
		Name:      strings.Repeat("长", 129),
		AssetType: "agent_persona",
		Industry:  "美妆",
	})
	if err == nil {
		t.Fatal("129 个字符的名称应被拒绝")
	}
	if !bizerr.IsClientError(err) {
		t.Fatalf("超长名称应回客户端错误（400），实际：%v", err)
	}
	if !strings.Contains(err.Error(), "128") {
		t.Fatalf("错误文案应指明上限，实际：%v", err)
	}
}
