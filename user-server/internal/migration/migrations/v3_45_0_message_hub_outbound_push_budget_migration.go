package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// MessageHubOutboundPushBudgetMigration v3.45.0：出站行的重推预算（批20d / A3）。
//
// 修的事实：一条「桥端永远发不出去」的出站消息此前会被无限重推——三条取行路径
// （轮询认领、SSE 单行认领、待推列表）都按状态判定「仍欠交付」，而发送失败让状态
// 每 30s 回到 inflight 超时，于是 pending/inflight 两个条件轮流为真，永不清算。
// 上界必须落在**库里的列**而不是内存计数器：SW 回收、进程重启、多实例认领同一行，
// 任何内存态都会在下一轮从 0 开始，等价于没设界。
//
// 两列的默认值口径沿用 v3.43.0：列缺省必须等于代码缺省（0 / ""），
// 否则老行读出来与「新行还没推过」无法区分，上界判断会把存量行当成已到界。
//
// 存量**不做回填**：把历史 pending/inflight 行的 push_attempts 填成非 0，等于在升级那一刻
// 直接判掉它们的一部分预算；填成上界更是当场把真实待发消息标成 failed。老行从 0 起算是
// 安全的——它至多多活一轮预算，而错填会直接吞消息。
type MessageHubOutboundPushBudgetMigration struct {
	db *gorm.DB
}

var _ migration.Migration = (*MessageHubOutboundPushBudgetMigration)(nil)

func NewMessageHubOutboundPushBudgetMigration(db *gorm.DB) *MessageHubOutboundPushBudgetMigration {
	return &MessageHubOutboundPushBudgetMigration{db: db}
}

func (m *MessageHubOutboundPushBudgetMigration) Version() string { return "v3.45.0" }

func (m *MessageHubOutboundPushBudgetMigration) Name() string {
	return "message_hub_outbound_push_budget"
}

func (m *MessageHubOutboundPushBudgetMigration) Description() string {
	return "消息中枢 v3.45.0：message_hub 增加 push_attempts/push_error 两列，给出站重推设可跨进程持久化的上界（到界落 failed 终态）"
}

func (m *MessageHubOutboundPushBudgetMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("message_hub") {
		return nil // 表还没建（全新库由模型标签负责），此处无物可改
	}
	// NOT NULL DEFAULT 0 在 PG 11+ 是纯元数据操作，不重写表；message_hub 是流量表，
	// 任何要重写的加列都会锁住写入，故此处刻意只用常量默认值。
	stmts := []string{
		`ALTER TABLE message_hub
			ADD COLUMN IF NOT EXISTS push_attempts INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE message_hub
			ADD COLUMN IF NOT EXISTS push_error VARCHAR(200) NOT NULL DEFAULT ''`,
	}
	for _, s := range stmts {
		if err := m.db.WithContext(ctx).Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

// Down 不丢列：push_attempts 是「这条消息被交出去几次」的唯一事实源，drop 之后重放历史
// 无从追查，且新代码第一次写它就报错（口径同 v3.39/v3.42/v3.43/v3.44 的「降级不销毁在用列」）。
func (m *MessageHubOutboundPushBudgetMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	declineColumnDrop(m.Version(), "message_hub.push_attempts", "message_hub.push_error")
	return nil
}
