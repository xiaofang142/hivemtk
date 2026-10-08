package service

import (
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
)

// CustomerChannelBindings 把 customer_channels 绑定行折成「渠道 → 身份」。
//
// 空身份（channel_user_id 为空）的行不进这张表：绑定存在而身份为空，
// 在外发侧与"没绑"无法区分，放进来只会让收件人取到空串。
func CustomerChannelBindings(rows []model.CustomerChannel) map[string]string {
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		ch := strings.TrimSpace(r.Channel)
		id := strings.TrimSpace(r.ChannelUserID)
		if ch == "" || id == "" {
			continue
		}
		if _, exists := out[ch]; !exists {
			out[ch] = id
		}
	}
	return out
}

// CustomerIdentity 客户身份来源的并集：customers 表的反规范化列 + customer_channels 绑定表。
//
// 为什么要有这个类型：渠道身份在本仓有**两处**存储——customers 表上的反规范化列
// （wechat_open_id / telegram_chat_id / …）与规范化绑定表 customer_channels。
// 写侧只保证「BindChannel 一条路两处都写」（见 channel_overview.go 的 BindChannel），
// 读侧原先一律只认反规范化列，于是任何只落在绑定表里的客户
// （直接 SQL 播种、导入脚本、迁移、或任何绕开 BindChannel 的写入口）
// 都会被外发闸门判成「无渠道身份」——报价次发、主动触达、渠道预览全线失败，
// 而客户在渠道总览里明明绑得好好的。
// 本类型把两处并成一处口径：反规范化列优先（它是 BindChannel 同步过去的主字段），
// 列上没有才回落到绑定表。回落只会**增加**可用渠道，不会凭空造出身份——
// 没有绑定行就没有回落值。
type CustomerIdentity struct {
	cust     *model.Customer
	bindings map[string]string
}

// NewCustomerIdentity 组装身份并集。bindings 为 nil 时行为与只读反规范化列完全一致。
func NewCustomerIdentity(c *model.Customer, bindings map[string]string) *CustomerIdentity {
	return &CustomerIdentity{cust: c, bindings: bindings}
}

// Identity 取某渠道的身份：反规范化列优先，其次绑定表。
func (ci *CustomerIdentity) Identity(channel string) string {
	if ci == nil || ci.cust == nil {
		return ""
	}
	if id := CustomerChannelIdentity(ci.cust, channel); id != "" {
		return id
	}
	return ci.bindings[channel]
}

// Has 判断某渠道是否有完整身份。
func (ci *CustomerIdentity) Has(channel string) bool {
	return ci.Identity(channel) != ""
}

// Available 列出客户所有有完整身份的渠道（顺序口径与 CustomerAvailableChannels 一致）。
func (ci *CustomerIdentity) Available(preferredFirst []string) []string {
	ordered := make([]string, 0, 13)

	for _, ch := range preferredFirst {
		if ci.Has(ch) {
			ordered = append(ordered, ch)
		}
	}

	for _, ch := range customerChannelDefaultOrder {
		exists := false
		for _, x := range ordered {
			if x == ch {
				exists = true
				break
			}
		}
		if !exists && ci.Has(ch) {
			ordered = append(ordered, ch)
		}
	}
	return ordered
}

// 渠道默认优先级：数字 ID 类（TG/抖音）先于文本 OpenID 类，短信/邮件（离线触达）垫底。
// 提成常量而非就地写字面量，好让 CustomerAvailableChannels 与 CustomerIdentity.Available
// 共用同一份口径——两处各写一遍，改一处就必然漂移。
var customerChannelDefaultOrder = []string{
	"sms", "email", "telegram", "whatsapp", "wecom", "wechat", "feishu",
	"douyin", "tiktok", "kuaishou", "xiaohongshu", "xianyu", "dingtalk",
}

// CustomerChannelIdentity 提取客户在某个渠道的身份（只认 customers 表的反规范化列）。
func CustomerChannelIdentity(c *model.Customer, channel string) string {
	switch channel {
	case "telegram":
		if c.TelegramChatID != 0 {
			return strconv.FormatInt(c.TelegramChatID, 10)
		}
		return c.TelegramUsername
	case "whatsapp", "sms":
		if c.WhatsAppPhone != "" {
			return c.WhatsAppPhone
		}
		return c.Phone
	case "email":
		return c.Email
	case "wechat":
		return c.WechatOpenID
	case "feishu":
		return c.FeishuOpenID
	case "wecom":
		return c.WeComExternalID
	case "douyin":
		return c.DouyinOpenID
	case "tiktok":
		return c.TikTokOpenID
	case "kuaishou":
		return c.KuaishouOpenID
	case "xiaohongshu":
		return c.XiaohongshuID
	case "xianyu":
		return c.XianyuID
	}
	return ""
}

// CustomerHasChannelIdentity 判断客户是否在某渠道有完整身份
func CustomerHasChannelIdentity(c *model.Customer, channel string) bool {
	return CustomerChannelIdentity(c, channel) != ""
}

// CustomerAvailableChannels 列出客户所有有完整身份的渠道（按优先级排序）
//
// 优先级排序：1) 客户偏好渠道 2) 数字 ID 类（TG/抖音）3) 文本 OpenID 类 4) 离线触达（SMS/Email）
//
// 只认 customers 表的反规范化列。需要把 customer_channels 绑定表一并算进来的读侧
// （外发闸门、渠道预览）改用 NewCustomerIdentity(c, bindings)。
func CustomerAvailableChannels(c *model.Customer, preferredFirst []string) []string {
	return NewCustomerIdentity(c, nil).Available(preferredFirst)
}
