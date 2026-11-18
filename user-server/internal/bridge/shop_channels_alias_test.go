// shop_channels_alias_test.go 电商店铺五渠道（CH-15/18/19/28/35）别名归一格。
// 与前端 CHANNEL_LEGACY_WEB_TO_CANONICAL、registry 注册名三方一致由
// channelgw/registry_parity_test.go 双向钉住；本文件只钉归一函数本体。
package bridge

import "testing"

func TestShopChannelsAliasNormalization(t *testing.T) {
	for _, tc := range []struct{ alias, want string }{
		{"taobao_web", "taobao"}, {"tmall", "taobao"}, {"qianniu", "taobao"},
		{"jinritemai", "douyin_shop"}, {"feige", "douyin_shop"}, {"douyin_shop_web", "douyin_shop"},
		{"kwaixiaodian", "kuaishou_shop"}, {"kuaishou_shop_web", "kuaishou_shop"},
		{"shopee_web", "shopee"}, {"shopee_seller", "shopee"},
		{"walmart", "walmart_us"}, {"walmart_web", "walmart_us"},
		// canonical 名原样通过。
		{"taobao", "taobao"}, {"douyin_shop", "douyin_shop"},
		{"kuaishou_shop", "kuaishou_shop"}, {"shopee", "shopee"}, {"walmart_us", "walmart_us"},
	} {
		if got := NormalizeBridgeChannel(tc.alias); got != tc.want {
			t.Errorf("别名 %q 归一 = %q，期望 %q", tc.alias, got, tc.want)
		}
	}
	if !IsBridgeChannel("taobao") || !IsBridgeChannel("douyin_shop") ||
		!IsBridgeChannel("kuaishou_shop") || !IsBridgeChannel("shopee") || !IsBridgeChannel("walmart_us") {
		t.Error("五渠道 canonical 名应命中 IsBridgeChannel（注册表装配断链）")
	}
}
