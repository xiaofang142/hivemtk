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
	default:
		return ch
	}
}

func IsBridgeChannel(ch string) bool {
	return gw.Default.IsChannel(NormalizeBridgeChannel(ch))
}
