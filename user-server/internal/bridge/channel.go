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
	default:
		return ch
	}
}

func IsBridgeChannel(ch string) bool {
	return gw.Default.IsChannel(NormalizeBridgeChannel(ch))
}
