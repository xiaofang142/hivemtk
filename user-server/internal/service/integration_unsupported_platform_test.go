package service

// 契约：不支持的平台做同步，必须回「客户端可纠正」的错误（400 + 指名可用平台），
// 不能是裸 errors.New 被上层压成 500「操作失败，请稍后重试」。
//
// 坑（2026-11-02 API 全量回归实测）：POST /api/integrations/{id}/sync-customers
// 在 custom 平台下稳定 500 INTERNAL_ERROR_6002，调用方既不知道是平台选错、
// 也不知道能选什么。

import (
	"context"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
)

func TestIntegrationSync_UnsupportedPlatformIsClientError(t *testing.T) {
	svc := &IntegrationService{}
	acc := &model.IntegrationAccount{Platform: "custom"}

	cases := []struct {
		name string
		call func() error
	}{
		{"customers", func() error { _, err := svc.SyncCustomers(context.Background(), acc); return err }},
		{"orders", func() error { _, err := svc.SyncOrders(context.Background(), acc); return err }},
		{"products", func() error { _, err := svc.SyncProducts(context.Background(), acc); return err }},
	}
	for _, c := range cases {
		err := c.call()
		if err == nil {
			t.Fatalf("%s: 不支持的平台必须报错", c.name)
		}
		if !bizerr.IsClientError(err) {
			t.Fatalf("%s: 应为客户端错误（400），实际被归为其它类型：%v", c.name, err)
		}
		if bizerr.IsUpstreamFailure(err) {
			t.Fatalf("%s: 不得标记为上游失败（平台选择错误不是渠道故障）", c.name)
		}
	}
}
