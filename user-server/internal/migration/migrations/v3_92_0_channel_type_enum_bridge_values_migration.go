package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ChannelTypeEnumBridgeValuesMigration v3.92.0（桥接渠道枚举扩容，2026-11-12）：
// channel_type_enum 补 jd / bilibili / weibo / personal_wx / pdd 五值。
//
// 背景：channel_type_enum 是 v3.22.2 建的 PG 原生 ENUM，当时按渠道白名单只有 12 值；
// 2026-11 六渠道统一接待与 CH-16 拼多多首切片后，channelgw/registry.go 已注册
// jd/bilibili/weibo/personal_wx/pdd 五个桥接渠道，渠道↔智能体绑定运行时会以这些值写
// channel_agent_bindings.channel_type（service/ai_agent.go channelTypeOf 映射后落库）——
// 跑过 v3.22.2 的存量库上 INSERT/UPDATE 直接被 PG 拒绝
// （invalid input value for enum channel_type_enum），属枚举词表与渠道注册表的漂移。
//
// 幂等可重入：逐值先查 pg_enum 现值，缺了才 ALTER TYPE ... ADD VALUE；重复执行为空操作。
// 事务口径：迁移执行器逐条 Exec（autocommit），ADD VALUE 不落事务块，无 PG 12 前
// 「cannot run inside a transaction block」限制（生产库 pg15）。
// ENUM 不存在（v3.22.2 尚未执行的链路倒挂）时保持空操作——不抢先建半量 ENUM。
type ChannelTypeEnumBridgeValuesMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*ChannelTypeEnumBridgeValuesMigration)(nil)

// NewChannelTypeEnumBridgeValuesMigration 构造
func NewChannelTypeEnumBridgeValuesMigration(db *gorm.DB) *ChannelTypeEnumBridgeValuesMigration {
	return &ChannelTypeEnumBridgeValuesMigration{db: db}
}

func (m *ChannelTypeEnumBridgeValuesMigration) Version() string { return "v3.92.0" }

func (m *ChannelTypeEnumBridgeValuesMigration) Name() string {
	return "channel_type_enum 桥接渠道扩容"
}

func (m *ChannelTypeEnumBridgeValuesMigration) Description() string {
	return "channel_type_enum 补 jd/bilibili/weibo/personal_wx/pdd 五值——与 channelgw 渠道注册表对齐，桥接渠道绑定落库不再被 ENUM 拒写"
}

// bridgeChannelEnumValues 与 channelgw/registry.go Default 注册表的 2026-11 新增桥接
// 渠道一致；douyin/xiaohongshu/xianyu/tiktok/kuaishou 六值 v3.22.2 已含，此处只补五值。
var bridgeChannelEnumValues = []string{"jd", "bilibili", "weibo", "personal_wx", "pdd"}

func (m *ChannelTypeEnumBridgeValuesMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)

	var enumExists bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'channel_type_enum')`,
	).Scan(&enumExists).Error; err != nil {
		return fmt.Errorf("检查 channel_type_enum 是否存在失败: %w", err)
	}
	if !enumExists {
		// v3.22.2 尚未跑过的库：随后轮到 v3.22.2 自会建全量枚举，本迁移空操作。
		return nil
	}

	for _, v := range bridgeChannelEnumValues {
		var present bool
		if err := db.Raw(
			`SELECT EXISTS (SELECT 1 FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid
			 WHERE t.typname = 'channel_type_enum' AND e.enumlabel = ?)`,
			v,
		).Scan(&present).Error; err != nil {
			return fmt.Errorf("检查 channel_type_enum 值 %s 失败: %w", v, err)
		}
		if present {
			continue
		}
		if err := db.Exec(
			fmt.Sprintf(`ALTER TYPE channel_type_enum ADD VALUE '%s'`, v),
		).Error; err != nil {
			return fmt.Errorf("channel_type_enum ADD VALUE %s 失败: %w", v, err)
		}
	}
	return nil
}

func (m *ChannelTypeEnumBridgeValuesMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// PG 不支持 DROP VALUE（未用值也无法删除）：降级保留枚举值，
	// 多留枚举值不破坏功能（v3.90.0 在用对象降级不销毁同一取舍）。
	return nil
}
