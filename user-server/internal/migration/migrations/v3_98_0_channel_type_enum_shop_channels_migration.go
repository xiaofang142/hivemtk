package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// ShopChannelTypeEnumMigration v3.98.0（电商店铺五渠道枚举扩容，2026-11-19 二次审核）：
// channel_type_enum 补 taobao / douyin_shop / kuaishou_shop / shopee / walmart_us 五值。
//
// 背景：与 v3.92.0 同类漂移的再演——channelgw/registry.go 已注册五渠道
// （channel_type_enum 是 PG 原生 ENUM，channel↔智能体绑定运行时经 service/ai_agent.go
// channelTypeOf 映射后落库 channel_agent_bindings.channel_type），枚举缺值时 INSERT/UPDATE
// 直接被 PG 拒绝（invalid input value for enum channel_type_enum）。v3.92.0 补过
// jd/bilibili/weibo/personal_wx/pdd 五值，本轮五渠道注册批漏补，二次审核抓出。
//
// 幂等可重入：逐值先查 pg_enum 现值，缺了才 ALTER TYPE ... ADD VALUE；重复执行为空操作。
// 事务口径同 v3.92.0：迁移执行器逐条 Exec（autocommit），ADD VALUE 不落事务块。
// ENUM 不存在（v3.22.2 尚未执行的链路倒挂）时保持空操作——不抢先建半量 ENUM。
type ShopChannelTypeEnumMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*ShopChannelTypeEnumMigration)(nil)

// NewShopChannelTypeEnumMigration 构造
func NewShopChannelTypeEnumMigration(db *gorm.DB) *ShopChannelTypeEnumMigration {
	return &ShopChannelTypeEnumMigration{db: db}
}

func (m *ShopChannelTypeEnumMigration) Version() string { return "v3.98.0" }

func (m *ShopChannelTypeEnumMigration) Name() string {
	return "channel_type_enum 店铺渠道扩容"
}

func (m *ShopChannelTypeEnumMigration) Description() string {
	return "channel_type_enum 补 taobao/douyin_shop/kuaishou_shop/shopee/walmart_us 五值——与 channelgw 渠道注册表对齐，店铺渠道绑定落库不再被 ENUM 拒写"
}

// shopChannelEnumValues 与 channelgw/registry.go Default 注册表的 2026-11-18 五渠道一致。
var shopChannelEnumValues = []string{"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us"}

func (m *ShopChannelTypeEnumMigration) Up(ctx context.Context) error {
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

	for _, v := range shopChannelEnumValues {
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

func (m *ShopChannelTypeEnumMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// PG 不支持 DROP VALUE（未用值也无法删除）：降级保留枚举值，
	// 多留枚举值不破坏功能（v3.92.0 同一取舍）。
	return nil
}
