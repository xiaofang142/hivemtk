package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// BrowserTaskCampaignBudgetMigration v3.51.0：browser_tasks 增加活动级预算四列（触达 P0 Chunk 4）。
//
// 修的事实：触达成本的两道刹车此前都管不住「活动」——
//
//	① token 预算只有进程内 session 级常量（brainTokenBudget，env 可调），没有任务维度的额度，
//	   于是「这一单活动最多烧多少 token」这件事无处可写、也无从核算；
//	② 触达条数完全没有闸，同一个任务被 cron 反复触发时可以无限次下发评论，
//	   直到平台风控把号封掉（Chunk 2 的熔断是事后补救，不是预算）。
//
// 口径（「活动」= 任务本体，见 model.BrowserTask 注释）：预算落成任务行上的持久累计额度，
// 跨 session、跨重跑累计。Executor 是进程级单例，内存计数在重启/多副本下必然失真。
//
// 列形状沿用 v3.46/v3.48 的「列缺省＝代码缺省」：全部 0/空 = 不限，存量行为逐字不变。
type BrowserTaskCampaignBudgetMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*BrowserTaskCampaignBudgetMigration)(nil)

func NewBrowserTaskCampaignBudgetMigration(db *gorm.DB) *BrowserTaskCampaignBudgetMigration {
	return &BrowserTaskCampaignBudgetMigration{db: db}
}

func (m *BrowserTaskCampaignBudgetMigration) Version() string { return "v3.51.0" }

func (m *BrowserTaskCampaignBudgetMigration) Name() string {
	return "browser_tasks_campaign_budget"
}

func (m *BrowserTaskCampaignBudgetMigration) Description() string {
	return "浏览器自动化 v3.51.0：browser_tasks 增加 token_budget/campaign_key/campaign_act_budget/campaign_act_used 列（活动级预算）"
}

func (m *BrowserTaskCampaignBudgetMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("browser_tasks") {
		return nil // 表还没建（全新库由模型标签负责），此处无物可改
	}
	return m.db.WithContext(ctx).Exec(`ALTER TABLE browser_tasks
		ADD COLUMN IF NOT EXISTS token_budget BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS campaign_key VARCHAR(128) NOT NULL DEFAULT '',
		ADD COLUMN IF NOT EXISTS campaign_act_budget BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS campaign_act_used BIGINT NOT NULL DEFAULT 0`).Error
}

// Down 不丢列：活动预算的累计值是成本核算的账本（口径同 v3.39/v3.42–v3.48 的
// 「降级不销毁在用列」）。
func (m *BrowserTaskCampaignBudgetMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "browser_tasks.token_budget")
	declineColumnDrop(m.Version(), "browser_tasks.campaign_key")
	declineColumnDrop(m.Version(), "browser_tasks.campaign_act_budget")
	declineColumnDrop(m.Version(), "browser_tasks.campaign_act_used")
	return nil
}
