package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ChannelCodeUnifyMigration v3.116.0：渠道统一的数据落地。
// 与顶层 migrations/100_channel_code_unify.sql 同契约：补齐全局预设渠道行，
// 并把历史业务表中的旧渠道码归一为规范码。幂等可重入；不删数据。
type ChannelCodeUnifyMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*ChannelCodeUnifyMigration)(nil)

func NewChannelCodeUnifyMigration(db *gorm.DB) *ChannelCodeUnifyMigration {
	return &ChannelCodeUnifyMigration{db: db}
}

func (*ChannelCodeUnifyMigration) Version() string { return "v3.116.0" }

func (*ChannelCodeUnifyMigration) Name() string { return "渠道主数据补齐与历史渠道码归一" }

func (*ChannelCodeUnifyMigration) Description() string {
	return "幂等补齐 27 个规范渠道全局行；把已知旧码按单一映射归一到规范码（缺表/缺列跳过）"
}

// canonicalChannels 与 user-web/src/constants/channel.js CHANNEL_OPTIONS 一一对应。
var canonicalChannels = []struct {
	Code, Name, Type string
}{
	{"wecom", "企业微信", "group"}, {"weixin", "微信公众号", "dm"},
	{"personal_wx", "个人微信", "dm"}, {"qq", "QQ", "dm"},
	{"whatsapp", "WhatsApp", "dm"}, {"telegram", "Telegram", "dm"},
	{"feishu", "飞书", "group"}, {"dingtalk", "钉钉", "group"},
	{"douyin", "抖音", "dm"}, {"kuaishou", "快手", "dm"},
	{"xiaohongshu", "小红书", "dm"}, {"xianyu", "闲鱼", "dm"},
	{"tiktok", "TikTok", "dm"}, {"jd", "京东", "dm"},
	{"bilibili", "B站", "dm"}, {"weibo", "微博", "dm"},
	{"pdd", "拼多多", "dm"}, {"taobao", "淘宝/天猫", "dm"},
	{"douyin_shop", "抖店飞鸽", "dm"}, {"kuaishou_shop", "快手小店", "dm"},
	{"shopee", "Shopee", "dm"}, {"walmart_us", "Walmart", "dm"},
	{"sms", "短信", "dm"}, {"email", "邮件", "dm"},
	{"card", "卡片", "dm"}, {"web", "Web Widget", "dm"},
	{"web_embed", "网页", "dm"},
}

// legacyChannelCodes 与 user-web/src/constants/channel.js 的
// CHANNEL_LEGACY_WEB_TO_CANONICAL + PLATFORM_GROUP_MEMBERS 保持一致。
var legacyChannelCodes = []struct{ Old, New string }{
	{"xhs_web", "xiaohongshu"}, {"xhs", "xiaohongshu"}, {"xiaohongshu_web", "xiaohongshu"},
	{"douyin_web", "douyin"}, {"kuaishou_web", "kuaishou"}, {"xianyu_web", "xianyu"},
	{"tiktok_web", "tiktok"}, {"jd_web", "jd"}, {"jingdong", "jd"},
	{"bili", "bilibili"}, {"bilibili_web", "bilibili"}, {"wb", "weibo"},
	{"weibo_web", "weibo"}, {"pdd_web", "pdd"}, {"pinduoduo", "pdd"},
	{"taobao_web", "taobao"}, {"tmall", "taobao"}, {"qianniu", "taobao"},
	{"douyin_shop_web", "douyin_shop"}, {"jinritemai", "douyin_shop"}, {"feige", "douyin_shop"},
	{"kuaishou_shop_web", "kuaishou_shop"}, {"kwaixiaodian", "kuaishou_shop"},
	{"kuaishouxiao", "kuaishou_shop"}, {"shopee_web", "shopee"}, {"shopee_seller", "shopee"},
	{"walmart", "walmart_us"}, {"walmart_web", "walmart_us"},
	{"wx_personal", "personal_wx"}, {"wechat_personal", "personal_wx"}, {"wechat", "weixin"},
}

var legacyChannelColumns = []struct{ Table, Column string }{
	{"message_hub", "platform"}, {"customer_sessions", "platform"}, {"inbox_conversations", "platform"},
	{"bridge_accounts", "channel"}, {"channel_agent_bindings", "channel_type"},
	{"customers", "source_channel"}, {"clues", "source_channel"}, {"customer_channels", "channel"},
	{"contact_consents", "channel"}, {"contact_preferences", "channel"},
	{"follow_up_records", "channel"}, {"follow_up_reminders", "channel"},
	{"customer_do_not_contact", "channel"}, {"customer_rfm", "last_channel"},
	{"lead_mining_judgements", "platform"}, {"lead_mining_judgements", "channel"},
	{"business_account_authorizations", "channel"}, {"channel_event_audit", "channel"},
	{"channel_interaction", "channel"}, {"channel_route_rule", "channel"},
	{"message_trace", "channel"}, {"integration_accounts", "platform"},
	{"external_orders", "platform"}, {"msg_hourly_summary", "platform"},
	{"clue_score", "channel"},
}

func (m *ChannelCodeUnifyMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("channel code unify migration: db is nil")
	}
	db := m.db.WithContext(tenant.WithSystemScope(ctx))
	if !db.Migrator().HasTable("channels") {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, ch := range canonicalChannels {
			if err := tx.Exec(`
				INSERT INTO channels (tenant_id, name, code, type, enabled, remark)
				VALUES (0, ?, ?, ?, true, '')
				ON CONFLICT (code) DO NOTHING`, ch.Name, ch.Code, ch.Type).Error; err != nil {
				return fmt.Errorf("seed channel %s: %w", ch.Code, err)
			}
		}
		for _, pair := range legacyChannelColumns {
			if !tx.Migrator().HasTable(pair.Table) || !tx.Migrator().HasColumn(pair.Table, pair.Column) {
				continue
			}
			stmt := fmt.Sprintf("UPDATE %q SET %q = ? WHERE %q = ?", pair.Table, pair.Column, pair.Column)
			for _, alias := range legacyChannelCodes {
				if err := tx.Exec(stmt, alias.New, alias.Old).Error; err != nil {
					return fmt.Errorf("normalize %s.%s (%s): %w", pair.Table, pair.Column, alias.Old, err)
				}
			}
		}
		return nil
	})
}

// Down 不做反向归一：历史码无法区分原始来源，回滚会把已统一数据改回旧码，
// 因此仅保留迁移账本回滚语义（幂等空操作）。
func (*ChannelCodeUnifyMigration) Down(context.Context) error { return nil }
