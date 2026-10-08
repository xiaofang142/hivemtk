// bridge_offline_replay_repo.go 桥接离线回扫仓储（五层 L5）
package repository

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// BridgeOfflineReplayRepository 离线渠道检测与延迟出站重放查询收口
type BridgeOfflineReplayRepository struct {
	db *gorm.DB
}

// NewBridgeOfflineReplayRepository 构造
// NewBridgeOfflineReplayRepository 构造（无参，内部取全局 DB；对齐 customer_session.go 先例）
func NewBridgeOfflineReplayRepository() *BridgeOfflineReplayRepository {
	return NewBridgeOfflineReplayRepositoryWithDB(_db.GetDB())
}

// NewBridgeOfflineReplayRepositoryWithDB 显式注入 DB（测试/装配用）
func NewBridgeOfflineReplayRepositoryWithDB(db *gorm.DB) *BridgeOfflineReplayRepository {
	return &BridgeOfflineReplayRepository{db: db}
}

// BridgeChannelRow 桥接渠道账号快照。
//
// 列集合必须与 model.BridgeAccount 一致：bridge_accounts 的渠道列叫 channel 不叫 platform，
// 而 bridge_metrics 是指标时间序列（metric_name/labels/value/ts），根本没有渠道维度 ——
// 拿它当"渠道最近活跃"的数据源，两条查询都撞 42703，回扫于是永远静默空转。
type BridgeChannelRow struct {
	Channel    string     `gorm:"column:channel"`
	AccountID  string     `gorm:"column:account_id"`
	Status     string     `gorm:"column:status"`
	LastSyncAt *time.Time `gorm:"column:last_sync_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
	// Reachable 由 SQL 侧按 bridgeOnlineSQLPredicate 算出，不是表里的列。
	// 带着算好的值出来，是为了让"这一轮扫到几台在线"与渠道总览报的在线数是同一个数；
	// 若留给 service 按 status 划，同一份数据会量出两个口径。
	Reachable bool `gorm:"column:reachable"`
}

// ListBridgeAccounts 全量渠道账号快照（在线/离线两批由 Reachable 决定）。
//
// graceSeconds 由调用方从 config_params(bridge/online_grace_window) 读好传入。
func (r *BridgeOfflineReplayRepository) ListBridgeAccounts(ctx context.Context, graceSeconds int) ([]BridgeChannelRow, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []BridgeChannelRow
	err := r.db.WithContext(ctx).Table("bridge_accounts").
		Select("channel, account_id, status, last_sync_at, updated_at, ("+bridgeOnlineSQLPredicate+") AS reachable",
			bridgeStatusOffline, graceSeconds).
		Order("channel ASC, account_id ASC").
		Scan(&rows).Error
	return rows, err
}

// DelayedOutboundRow reach_delayed_outbound 待重放消息行
//
// 列集合必须与 model.DelayedOutboundReply 一致：表里没有 receiver_id / msg_type /
// event_id / retry_count / replayed_at 这五列，结构体宣称有只会让 gorm 的 SELECT *
// 静默把字段留成零值，重放带着空 msg_type 出站且无人报警。
type DelayedOutboundRow struct {
	ID             uint          `gorm:"column:id"`
	Platform       string        `gorm:"column:platform"`
	AccountID      string        `gorm:"column:account_id"`
	ConversationID string        `gorm:"column:conversation_id"`
	SenderID       string        `gorm:"column:sender_id"`
	Content        string        `gorm:"column:content"`
	Kind           string        `gorm:"column:kind"`
	Cards          model.JSONMap `gorm:"column:cards"`
	Attempts       int           `gorm:"column:attempts"`
}

// ListPendingDelayedOutbound 取指定渠道已到期的 pending 延迟出站消息
//
// 必须带 send_at <= now：quiet_hours 行的 send_at 就是窗口开放时刻，
// 不看它等于在客户免打扰时段把回复推出去。
func (r *BridgeOfflineReplayRepository) ListPendingDelayedOutbound(ctx context.Context, platform, accountID string, limit int) ([]DelayedOutboundRow, error) {
	if r.db == nil {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Select("id, platform, account_id, conversation_id, sender_id, content, kind, cards, attempts").
		Where("platform = ? AND account_id = ? AND status = ? AND send_at <= ?",
			platform, accountID, model.DelayedStatusPending, time.Now()).
		Order("send_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var msgs []DelayedOutboundRow
	err := q.Scan(&msgs).Error
	return msgs, err
}

// ClaimDelayedOutboundForReplay 抢占重放权：pending → sending，返回是否拿到入场券。
//
// H-3 主链路 drain 的是同一张表的同一批到期 pending 行，两边都直接投递就是
// 一次周期里把同一份内容发给客户两次；只有把这条 CAS 的 RowsAffected 当门票才互斥。
func (r *BridgeOfflineReplayRepository) ClaimDelayedOutboundForReplay(ctx context.Context, id uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ? AND status = ?", id, model.DelayedStatusPending).
		Update("status", model.DelayedStatusSending)
	return res.RowsAffected == 1, res.Error
}

// MarkDelayedOutboundReplayFailed 重放失败：回到 pending 待下一轮，累计 attempts 并记错误
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayFailed(ctx context.Context, id uint, lastErr string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.DelayedStatusPending,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": truncateForColumn(lastErr, 1024),
		}).Error
}

// MarkDelayedOutboundReplayed 重放成功：收口到 sent
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundReplayed(ctx context.Context, id uint) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":  model.DelayedStatusSent,
			"sent_at": time.Now(),
		}).Error
}

// MarkDelayedOutboundAbandoned 重投次数用尽：收口到终态 failed，不再被回扫取到
func (r *BridgeOfflineReplayRepository) MarkDelayedOutboundAbandoned(ctx context.Context, id uint, reason string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.DelayedStatusFailed,
			"sent_at":    time.Now(),
			"last_error": truncateForColumn(reason, 1024),
		}).Error
}

// OrphanOutboundGroup 一个「桥接渠道账号 + 已无望送达的 message_hub 出站行数」分组。
type OrphanOutboundGroup struct {
	Platform  string `gorm:"column:platform"`
	AccountID string `gorm:"column:account_id"`
	Rows      int64  `gorm:"column:orphan_rows"`
}

// orphanOutboundAgePredicate 孤儿行的「时候到了」判定，列名带 m/a 前缀（两处查询共用同一段）。
//
// 每一条各挡一种误伤：
//   - deleted_at IS NULL：本文件其余查询走 GORM 模型、自动带软删过滤，这里写裸 SQL 不补
//     就会把已删会话的行算进报数（判掉无害但数字对不上任何一条取行路径）。
//   - status IN ('pending','inflight')：delivered/failed/send_failed 已是终态。
//     inflight 只出现在「认领后未回写」的遗弃行上（租约 30s），能与 pending 同批处理。
//   - created_at 超阈：账号刚掉线不等于这行的死刑——扩展几分钟后再连就该照常补投，
//     所以必须行本身也老到没有现实补投可能。
//   - last_sync_at 超阈/缺失：账号此刻仍持续同步就不算孤儿。LEFT JOIN 让"bridge_accounts
//     里根本没这行"（未注册渠道）落进 IS NULL 这一支，与"注册过但早已不同步"同判。
//
// 不看 bridge_accounts.status：那一列只有 SSE 正常收尾时 SetOffline 会改回 offline，
// 扩展崩溃 / 浏览器被杀 / 断网都不走那条路径，列就粘在 online 上（实测 155 行里 152 行标 online，
// 按最后同步时间判定的真值是 0）。可达性只按 last_sync_at 判。
const orphanOutboundAgePredicate = `m.direction = 'outbound'
	  AND m.deleted_at IS NULL
	  AND m.status IN ('pending','inflight')
	  AND m.created_at < now() - (? * interval '1 second')
	  AND (a.last_sync_at IS NULL OR a.last_sync_at < now() - (? * interval '1 second'))`

const orphanOutboundFrom = `FROM message_hub m
	  LEFT JOIN bridge_accounts a ON a.channel = m.platform AND a.account_id = m.account_id`

// ListOrphanOutboundGroups 按渠道账号汇总候选孤儿行数（供门控与 dry-run 报数）。
//
// 只碰 platforms 里的渠道：直接投递渠道（telegram/feishu/…）的行由写侧结算收口，
// 本来就没有任何轮询者，按"没人拉"判会把它们的正常历史一并烧掉。
func (r *BridgeOfflineReplayRepository) ListOrphanOutboundGroups(ctx context.Context, platforms []string, ttl time.Duration) ([]OrphanOutboundGroup, error) {
	if r.db == nil || len(platforms) == 0 {
		return nil, nil
	}
	secs := int64(ttl.Seconds())
	var rows []OrphanOutboundGroup
	err := r.db.WithContext(ctx).Raw(`SELECT m.platform, m.account_id, count(*) AS orphan_rows `+orphanOutboundFrom+
		` WHERE m.platform IN ? AND `+orphanOutboundAgePredicate+` GROUP BY m.platform, m.account_id ORDER BY orphan_rows DESC, m.platform ASC`,
		platforms, secs, secs).Scan(&rows).Error
	return rows, err
}

// SettleOrphanOutbound 把指定渠道账号下已成孤儿的出站行落终态 failed。
//
// 判定条件在写的那条 SQL 里重算一遍（不信任调用方传来的分组快照）：从报数到这里之间
// 账号可能重新同步，重算才让"刚回来的扩展"不被误烧。渠道白名单由 groups 的成对键继承，
// 所以这里不再单独过 platforms —— 前提是 groups 只能来自 ListOrphanOutboundGroups。
// 与 exhaustOutbound 同规格：写 push_error 写明原因、清 claimed_at、**不写 sent_at**。
func (r *BridgeOfflineReplayRepository) SettleOrphanOutbound(ctx context.Context, groups []OrphanOutboundGroup, ttl time.Duration, reason string, limit int) (int64, error) {
	if r.db == nil || len(groups) == 0 {
		return 0, nil
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	pairs := make([]string, 0, len(groups))
	pairArgs := make([]any, 0, len(groups)*2)
	for _, g := range groups {
		if g.Platform == "" || g.AccountID == "" {
			continue
		}
		pairs = append(pairs, "(?, ?)")
		pairArgs = append(pairArgs, g.Platform, g.AccountID)
	}
	if len(pairs) == 0 {
		return 0, nil
	}
	secs := int64(ttl.Seconds())
	// 顺序必须与 SQL 里占位符出现顺序一致：push_error → 两段阈秒 → 成对键 → LIMIT。
	args := append([]any{reason, secs, secs}, append(pairArgs, limit)...)
	// 依赖 (platform, account_id) 成对匹配：不能拆成两个 IN 列表，那样 A 渠道 + B 账号的
	// 组合也会被选中，等于跨渠道误烧。
	q := `UPDATE message_hub SET status = 'failed', push_error = ?, claimed_at = NULL
		WHERE id IN (
		  SELECT m.id ` + orphanOutboundFrom + `
		  WHERE ` + orphanOutboundAgePredicate + `
		    AND (m.platform, m.account_id) IN (` + strings.Join(pairs, ", ") + `)
		  ORDER BY m.id LIMIT ?)`
	res := r.db.WithContext(ctx).Exec(q, args...)
	return res.RowsAffected, res.Error
}
