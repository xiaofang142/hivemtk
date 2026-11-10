// Package migrations 数据库迁移集合。
package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// LLMProviderPlanPriceMigration v3.90.0（PLT-01 刀1 单价落地）：
// llm_providers 加平台套餐单价快照两列（分/1k token）。
//
// 背景：平台下发的 provider 视图（ProviderViewsForDeliver）不带价，商户端
// platform_llm_usage_records 成本三列与 llm_routing_logs 平台行 cost 恒 0。
// 用户端 sync 时 Providers()×Plans() 按 plan_code 关联，把套餐单价落到
// provider 镜像行（零协议变化）；写入时点按当次快照算成本，读侧不重算不回填。
//
// 无回填：存量行没有"当时单价"这个事实可恢复（平台价目是现行值，回填会让
// 历史明细漂移，违背写时快照口径），历史行成本保持 0 如实降级。
// 幂等可重入：列已存在则跳过。
type LLMProviderPlanPriceMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*LLMProviderPlanPriceMigration)(nil)

// NewLLMProviderPlanPriceMigration 构造 v3.90.0 迁移
func NewLLMProviderPlanPriceMigration(db *gorm.DB) *LLMProviderPlanPriceMigration {
	return &LLMProviderPlanPriceMigration{db: db}
}

func (m *LLMProviderPlanPriceMigration) Version() string { return "v3.90.0" }

func (m *LLMProviderPlanPriceMigration) Name() string {
	return "llm_providers 平台套餐单价快照列"
}

func (m *LLMProviderPlanPriceMigration) Description() string {
	return "llm_providers 加 input/output_price_per_1k_cents（平台套餐单价快照，分/1k token；" +
		"sync 按 plan_code 关联落列，join 不上=0=cost 如实降级）"
}

func (m *LLMProviderPlanPriceMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	db := m.db.WithContext(ctx)
	if !db.Migrator().HasTable("llm_providers") {
		return nil // 全新部署：AutoMigrate 建表时模型 tag 已带列
	}
	for _, col := range []string{"input_price_per_1k_cents", "output_price_per_1k_cents"} {
		if db.Migrator().HasColumn(llmProviderPriceColumnProbe{}, col) {
			continue // 幂等重入
		}
		if err := db.Exec(
			fmt.Sprintf(`ALTER TABLE llm_providers ADD COLUMN %s bigint NOT NULL DEFAULT 0`, col)).Error; err != nil {
			return fmt.Errorf("加 llm_providers.%s 列失败: %w", col, err)
		}
	}
	return nil
}

func (m *LLMProviderPlanPriceMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	// 两列由模型声明（AutoMigrate 持有），降级不销毁在用对象
	// （与 v3.84.0 payments.reason 同一取舍：删列会把已落库的单价快照变成
	// 不可恢复的数据丢失，多留两列带默认值不破坏任何功能）。
	declineColumnDrop(m.Version(),
		"llm_providers.input_price_per_1k_cents",
		"llm_providers.output_price_per_1k_cents")
	return nil
}

// llmProviderPriceColumnProbe 只为 Migrator().HasColumn 服务（按 TableName 判列存在性）。
type llmProviderPriceColumnProbe struct{}

func (llmProviderPriceColumnProbe) TableName() string { return "llm_providers" }
