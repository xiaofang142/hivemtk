package bridge

import (
	gw "hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/service"
)

func init() {
	service.SetBridgeChannels(gw.Default.Names())
}

const (
	ChannelDouyinWeb   = "douyin"
	ChannelXHSWeb      = "xiaohongshu"
	ChannelTikTok      = "tiktok"
	ChannelKuaishouWeb = "kuaishou"
	ChannelXianyuWeb   = "xianyu"
	ChannelJDWeb       = "jd"
	ChannelBilibiliWeb = "bilibili"
	ChannelWeiboWeb    = "weibo"
	ChannelPersonalWX  = "personal_wx"
	// CH-16 首切片：拼多多商家网页聊天（canonical=pdd）。
	ChannelPDDWeb = "pdd"
	// 2026-11-18 电商店铺网页桥五渠道骨架（CH-15/18/19/28/35，fail-closed，Experimental）。
	ChannelTaobaoWeb       = "taobao"
	ChannelDouyinShopWeb   = "douyin_shop"
	ChannelKuaishouShopWeb = "kuaishou_shop"
	ChannelShopeeWeb       = "shopee"
	ChannelWalmartUSWeb    = "walmart_us"
)

// NormalizeBridgeChannel 保留为 bridge 包兼容入口；别名单源在 channelgw.NormalizeChannel
// （G-01：协议层 canonical 名 = Registry.Name = DB channels.code = model.Channel*）。
func NormalizeBridgeChannel(ch string) string {
	return gw.NormalizeChannel(ch)
}

func IsBridgeChannel(ch string) bool {
	return gw.Default.IsChannel(gw.NormalizeChannel(ch))
}
