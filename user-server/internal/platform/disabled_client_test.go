package platform

import (
	"context"
	"errors"
	"testing"
)

// 平台集成关闭态（PLATFORM_ENABLED 未设置）下，repository.PlatformAPIClient
// 的对外形状：读 → 空且无错，写 → 哨兵 ErrPlatformNotConfigured。
//
// 为什么读面不能返回 error：controller 的 ListMarket 把 error 吞成空列表是"碰巧"，
// 而 detail 会把 error 上抛成业务码 5001 红叉。市场在关态就是"没有货"，
// 不是"出错了"，所以空切片 / 空对象必须由 client 侧保证，而不是靠每个调用点记得吞。

func TestDisabledClientIsWhatFactoryReturns(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "")
	if _, ok := NewPlatformAPIClient().(disabledClient); !ok {
		t.Fatalf("关态工厂必须返回 disabledClient，实际 %T", NewPlatformAPIClient())
	}
	t.Setenv("PLATFORM_ENABLED", "true")
	if _, ok := NewPlatformAPIClient().(disabledClient); ok {
		t.Fatal("开态工厂不得返回 disabledClient（应回到真实适配器）")
	}
}

func TestDisabledClientReadsReturnEmptyNoError(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "") // 显式关态：不依赖"环境变量恰好没设"
	c := NewPlatformAPIClient()
	ctx := context.Background()

	list, total, err := c.ListAssets(ctx, "agent_persona", "", 1, 20)
	if err != nil {
		t.Fatalf("关态读列表不应报错：%v", err)
	}
	if total != 0 {
		t.Fatalf("关态列表 total 必须为 0，实际 %d", total)
	}
	if list == nil {
		t.Fatal("关态列表必须是非 nil 空切片：前端 list.length 直接消费，nil 会炸")
	}
	if len(list) != 0 {
		t.Fatalf("关态列表必须为空，实际 %d 条", len(list))
	}

	detail, err := c.GetAssetDetail(ctx, "no-such-asset")
	if err != nil {
		t.Fatalf("关态读详情不应报错：%v", err)
	}
	if detail == nil {
		t.Fatal("关态详情必须是非 nil 空对象，否则前端 detail.name 取值会炸")
	}
	if len(detail) != 0 {
		t.Fatalf("关态详情应为空对象，实际 %v", detail)
	}

	mine, err := c.MyPurchases(ctx)
	if err != nil {
		t.Fatalf("关态我的购买不应报错：%v", err)
	}
	if mine == nil || len(mine) != 0 {
		t.Fatalf("关态我的购买必须是非 nil 空切片，实际 %v", mine)
	}
}

func TestDisabledClientWritesReturnSentinel(t *testing.T) {
	t.Setenv("PLATFORM_ENABLED", "") // 显式关态：不依赖"环境变量恰好没设"
	c := NewPlatformAPIClient()
	ctx := context.Background()

	// 写面绝不能"静默成功"：那样前端会提示"购买成功"而本地什么都没有。
	// 复用 client.go 既有哨兵，不新增错误类型。
	if err := c.Purchase(ctx, "a1"); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Fatalf("关态 Purchase 必须返回 ErrPlatformNotConfigured，实际=%v", err)
	}
	if p, err := c.PullData(ctx, "a1"); !errors.Is(err, ErrPlatformNotConfigured) || p != nil {
		t.Fatalf("关态 PullData 必须返回 (nil, ErrPlatformNotConfigured)，实际=%+v %v", p, err)
	}
	if err := c.ReportUsage(ctx, "a1", 1); !errors.Is(err, ErrPlatformNotConfigured) {
		t.Fatalf("关态 ReportUsage 必须返回 ErrPlatformNotConfigured，实际=%v", err)
	}
}

// TestEnabledClientStillReachesServer 正向对照：开态下工厂必须回到真实适配器并真的发出请求。
// 没有这一腿，上面的"关态零出站/返回空"可能只是测试夹具本身坏了造成的假绿。
func TestEnabledClientStillReachesServer(t *testing.T) {
	srv := newMarketTestServer(t)
	defer srv.Close()
	t.Setenv("PLATFORM_ENABLED", "true")
	t.Setenv("MERCHANT_API_SECRET", "s")
	t.Setenv("PLATFORM_MERCHANT_KEY", "mk-test")
	withMarketConfig(t, srv.URL)

	c := NewPlatformAPIClient()
	if _, ok := c.(disabledClient); ok {
		t.Fatal("开态不得仍是 disabledClient")
	}
	list, total, err := c.ListAssets(context.Background(), "agent_persona", "美妆", 2, 10)
	if err != nil {
		t.Fatalf("开态 ListAssets 应真的打到平台：%v", err)
	}
	if total != 7 || len(list) == 0 {
		t.Fatalf("开态应拿到测试服务器的夹具数据（total=7），实际 total=%d len=%d", total, len(list))
	}
}
