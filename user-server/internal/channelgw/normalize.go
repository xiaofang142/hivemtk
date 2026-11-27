package channelgw

import "strings"

// channelAliases 是协议层的渠道名归一表（G-01）。
//
// 统一消息总线只认一个渠道标识：channelgw.Registry.Name（与 DB channels.code、
// model.Channel* 取值一致）。历史上扩展侧、Webhook 路由和运营文案各自使用过别名，
// 这里把所有已知别名收敛到 canonical 名；新增别名只允许加在本表，禁止在
// bridge/webhook/service 各自再写一份 switch。
//
// 生效顺序：先去空白、转小写，再查本表；未命中则原样返回（由调用方用
// Registry.IsChannel / DirectMineChannels 判断是否为已知渠道）。
var channelAliases = map[string]string{
	"xhs":               "xiaohongshu",
	"xhs_web":           "xiaohongshu",
	"xiaohongshu_web":   "xiaohongshu",
	"douyin_web":        "douyin",
	"kuaishou_web":      "kuaishou",
	"ks":                "kuaishou",
	"xianyu_web":        "xianyu",
	"tiktok_web":        "tiktok",
	"jd_web":            "jd",
	"jingdong":          "jd",
	"jd_chat":           "jd",
	"bili":              "bilibili",
	"bilibili_web":      "bilibili",
	"b站":                "bilibili",
	"wb":                "weibo",
	"weibo_web":         "weibo",
	"wx_personal":       "personal_wx",
	"wechat_personal":   "personal_wx",
	"personal_wechat":   "personal_wx",
	"pinduoduo":         "pdd",
	"pdd_web":           "pdd",
	"taobao_web":        "taobao",
	"tmall":             "taobao",
	"qianniu":           "taobao",
	"jinritemai":        "douyin_shop",
	"feige":             "douyin_shop",
	"douyin_shop_web":   "douyin_shop",
	"kwaixiaodian":      "kuaishou_shop",
	"kuaishou_shop_web": "kuaishou_shop",
	"kuaishouxiao":      "kuaishou_shop",
	"shopee_web":        "shopee",
	"shopee_seller":     "shopee",
	"walmart":           "walmart_us",
	"walmart_web":       "walmart_us",
}

// NormalizeChannel 把历史别名归一为协议 canonical 渠道名。
// 未登记的别名原样返回（小写化），调用方仍需用注册表 / 直连集合做准入判断。
func NormalizeChannel(ch string) string {
	v := strings.ToLower(strings.TrimSpace(ch))
	if canonical, ok := channelAliases[v]; ok {
		return canonical
	}
	return v
}

// IsKnownChannel 判断归一后的名字是否属于统一总线的已知渠道集合：
// 桥接渠道来自 Default 注册表，服务端直连渠道来自 DirectMineChannels。
func IsKnownChannel(ch string) bool {
	v := NormalizeChannel(ch)
	if Default.IsChannel(v) {
		return true
	}
	for _, name := range DirectMineChannels {
		if v == name {
			return true
		}
	}
	return false
}
