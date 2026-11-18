package bridge

import (
	gw "hivemtk-user/internal/channelgw"
	"hivemtk-user/internal/model"
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

func NormalizeBridgeChannel(ch string) string {
	switch ch {
	case "xhs", "xhs_web", "xiaohongshu_web":
		return model.ChannelXHS
	case "douyin_web":
		return model.ChannelDouyin
	case "kuaishou_web", "ks":
		return model.ChannelKuaishou
	case "xianyu_web":
		return model.ChannelXianyu
	case "tiktok_web":
		return model.ChannelTikTok
	case "jd_web", "jingdong", "jd_chat":
		return model.ChannelJD
	case "bili", "bilibili_web", "b站":
		return model.ChannelBilibili
	case "wb", "weibo_web":
		return model.ChannelWeibo
	case "wx_personal", "wechat_personal", "personal_wechat":
		return model.ChannelPersonalWX
	// CH-16（2026-11-09）：拼多多别名归一。pinduoduo 是平台英文名的历史写法，
	// pdd_web 是扩展侧早期占位别名；canonical=pdd。
	case "pinduoduo", "pdd_web":
		return model.ChannelPDD
	// 2026-11-18 电商店铺五渠道别名归一（与前端 CHANNEL_LEGACY_WEB_TO_CANONICAL
	// 及各渠道调研行 canonical 口径一致）。
	case "taobao_web", "tmall", "qianniu":
		return model.ChannelTaobao
	case "jinritemai", "feige", "douyin_shop_web":
		return model.ChannelDouyinShop
	case "kwaixiaodian", "kuaishou_shop_web", "kuaishouxiao":
		return model.ChannelKuaishouShop
	case "shopee_web", "shopee_seller":
		return model.ChannelShopee
	case "walmart", "walmart_web":
		return model.ChannelWalmartUS
	default:
		return ch
	}
}

func IsBridgeChannel(ch string) bool {
	return gw.Default.IsChannel(NormalizeBridgeChannel(ch))
}
