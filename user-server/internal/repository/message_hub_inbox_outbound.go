package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxOutboundPushAttempts 出站行的重推上界。一次认领成功 = 服务端把这一行交给桥端一次，
// 到界后该行落终态 failed、不再由任何取行路径给出。三条取行路径必须同口径，上界因此放在仓储。
const MaxOutboundPushAttempts = 20

func (r *MessageHubRepository) AckOutboundDeliveredBatch(ctx context.Context, channel, accountID string, msgIDs []string) (int64, error) {
	if r.db == nil || len(msgIDs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Model(&model.MessageHub{}).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ? AND status IN ('pending','inflight')", channel, accountID, msgIDs).
		Update("status", model.BridgeAckStatusDelivered)
	return res.RowsAffected, res.Error
}

func (r *MessageHubRepository) AckOutboundDeliveredBatchReturning(ctx context.Context, channel, accountID string, msgIDs []string) (updatedIDs []string, affectedRows int64, err error) {
	if r.db == nil || len(msgIDs) == 0 {
		return nil, 0, nil
	}
	updatedIDs = make([]string, 0, len(msgIDs))
	// 事务内 FOR UPDATE 取号 + 条件更新，行锁保证取号与更新之间状态不被并发改写。
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ? AND status IN ('pending','inflight')", channel, accountID, msgIDs).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Pluck("msg_id", &updatedIDs).Error; e != nil {
			return e
		}
		if len(updatedIDs) == 0 {
			affectedRows = 0
			return nil
		}
		res := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND msg_id IN ? AND status IN ('pending','inflight')", channel, accountID, updatedIDs).
			Updates(map[string]any{"status": model.BridgeAckStatusDelivered, "sent_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		affectedRows = res.RowsAffected
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return updatedIDs, affectedRows, nil
}

// exhaustOutbound 把重推次数已用尽、此刻仍欠交付的出站行升级成终态 failed。
// 由各取行路径顺手调用（想取就必须先结算），否则一条路径漏掉那批行就回到永推状态。
// 落终态时不写 sent_at：这条从没交付出去，而回显检测按 sent_at 筛候选，盖时间戳会让
// 后续回复被误判成回显而吞掉。桥端主动 ack failed 的路径照旧写 sent_at，两者靠 push_error 分辨。
func (r *MessageHubRepository) exhaustOutbound(ctx context.Context, channel, accountID string, cutoff time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND push_attempts >= ? "+
			"AND (status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?))",
			channel, accountID, MaxOutboundPushAttempts, cutoff).
		Updates(map[string]any{
			"status":     "failed",
			"push_error": pushExhaustedError,
			"claimed_at": nil,
		}).Error
}

// pushExhaustedError 写在行上的失败原因。刻意不带界数字：行上的 push_attempts 就是那个数。
const pushExhaustedError = "outbound_push_exhausted"

// pushAckUnknownError CH-37b 刀4（R2/R6）：详细 ack 落 unknown 终态时写在 push_error 上的
// 原因标记。push_error 在这一格充当「操作来源」痕迹：status='unknown' + push_error=
// 'ack_unknown' = 桥端 ack 报告结果不明（而非对账/回收写入）。
const pushAckUnknownError = "ack_unknown"

// pushReconciledUnknownPrefix CH-37b 刀4（R2/R6）：对账迁移在 push_error 上留的操作来源前缀，
// 完整值为 <prefix><operator>（operator 由 service 层强制非空）。status 是 delivered/failed
// 而 push_error 带本前缀 = 这行曾进 unknown、由人工/API 显式对账迁出。
const pushReconciledUnknownPrefix = "reconciled_unknown:"

// terminalAckUpdates 详细 ack 终态翻转的列值集：三态共用（delivered/failed/unknown）。
// sent_at 与桥端主动 ack failed 路径同口径照写（提交动作确实发生过）；unknown 额外写
// push_error=ack_unknown 记「结果不明」的来源（delivered/failed 不碰 push_error，
// 与既有二值行为逐字节一致）。
func terminalAckUpdates(terminalStatus string) map[string]any {
	upd := map[string]any{"status": terminalStatus, "sent_at": time.Now()}
	if terminalStatus == model.BridgeAckStatusUnknown {
		upd["push_error"] = pushAckUnknownError
	}
	return upd
}

func (r *MessageHubRepository) ClaimPendingOutbound(ctx context.Context, channel, accountID string, limit int, claimTimeout time.Duration) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || limit <= 0 {
		return nil, nil
	}
	cutoff := time.Now().Add(-claimTimeout)

	if err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {
		logger.Warnf("[ClaimPendingOutbound] 到界行升级终态失败（继续认领）: %v", err)
	}

	if err := r.db.WithContext(ctx).
		Model(&model.MessageHub{}).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?", channel, accountID, cutoff).
		Updates(map[string]any{"status": "pending", "claimed_at": nil}).Error; err != nil {
		logger.Warnf("[ClaimPendingOutbound] 回收超时 inflight 失败（继续认领）: %v", err)
	}

	list := make([]model.MessageHub, 0, limit)
	// push_attempts 在这里 +1：一次认领就是一次「交给桥端去发」。回收超时 inflight 那一步
	// 刻意不碰这一列——清零等于每次超时白送一轮预算，上界就成了摆设。
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND direction = 'outbound' AND status = 'pending' AND push_attempts < ?",
				channel, accountID, MaxOutboundPushAttempts).
			Order("id ASC").
			Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Find(&list).Error; e != nil {
			return e
		}
		if len(list) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(list))
		for i := range list {
			ids = append(ids, list[i].ID)
		}
		return tx.Model(&model.MessageHub{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":        "inflight",
				"claimed_at":    time.Now(),
				"push_attempts": gorm.Expr("push_attempts + 1"),
			}).Error
	})
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		ids := make([]uint, 0, len(list))
		for i := range list {
			ids = append(ids, list[i].ID)
		}
		if err := r.db.WithContext(ctx).Where("id IN ?", ids).Order("id ASC").Find(&list).Error; err != nil {
			return nil, err
		}
	}
	return list, nil
}

// ClaimOutboundForPush 单行认领：SSE 即时下推前必须先过这道门。
// 认领条件与 ClaimPendingOutbound 严格同口径，只是作用域收窄到「刚写入的这一行」：
// status='pending'（无人认领）或 inflight 且 claimed_at 已超超时（上次下推后没 ack，需重投）。
// 返回 false = 这一行归别人（已认领/已终态/入站方向）或重推预算已用完，调用方必须放弃推送。
// 单表单行条件更新，天然互斥，无需额外锁。
func (r *MessageHubRepository) ClaimOutboundForPush(ctx context.Context, id uint64, claimTimeout time.Duration) (bool, error) {
	if r == nil || r.db == nil || id == 0 {
		return false, nil
	}
	cutoff := time.Now().Add(-claimTimeout)
	res := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("id = ? AND direction = 'outbound' AND push_attempts < ? "+
			"AND (status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?))",
			id, MaxOutboundPushAttempts, cutoff).
		Updates(map[string]any{
			"status":        "inflight",
			"claimed_at":    time.Now(),
			"push_attempts": gorm.Expr("push_attempts + 1"),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// FetchOutboundUndelivered 列出「此刻仍欠交付」的出站行（重投来源）。
// 与 FetchOutboundSince 的差别：那个按 id 游标取，游标一过就再也看不见发送失败的行，
// 出站静默丢失；本函数按状态取（pending 或 inflight 且认领已超时），交付完自然离开集合，收敛且与游标无关。
// inflight 但 claimed_at IS NULL 不算欠（非认领路径置的状态，无超时可判），宁可漏投一轮也不重复推。
func (r *MessageHubRepository) FetchOutboundUndelivered(ctx context.Context, channel, accountID string, claimTimeout time.Duration, limit int) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || channel == "" || accountID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	cutoff := time.Now().Add(-claimTimeout)
	if err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {
		logger.Warnf("[FetchOutboundUndelivered] 到界行升级终态失败（继续取待推）: %v", err)
	}
	var rows []model.MessageHub
	err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND direction = 'outbound'", channel, accountID).
		Where("push_attempts < ?", MaxOutboundPushAttempts).
		Where("(status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?))", cutoff).
		Order("id ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MessageHubRepository) GetByMsgIDsInScope(ctx context.Context, platform, accountID string, msgIDs []string) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || len(msgIDs) == 0 || platform == "" || accountID == "" {
		return nil, nil
	}
	rows := make([]model.MessageHub, 0, len(msgIDs))
	err := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ?", platform, accountID, msgIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MessageHubRepository) GetByMsgIDsInScopeWithConv(ctx context.Context, platform, accountID, conversationID string, msgIDs []string) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || len(msgIDs) == 0 || platform == "" || accountID == "" {
		return nil, nil
	}
	rows := make([]model.MessageHub, 0, len(msgIDs))
	q := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ?", platform, accountID, msgIDs)
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *MessageHubRepository) GetOutboundByPlatformSenderContent(ctx context.Context, platform, senderName, content string) (*model.MessageHub, error) {
	return r.GetOutboundByPlatformSenderContentConv(ctx, platform, senderName, content, "")
}

// GetOutboundByPlatformSenderContentConv 同上但按 conversation_id 限定范围（包含匹配兜底必需）。
func (r *MessageHubRepository) GetOutboundByPlatformSenderContentConv(ctx context.Context, platform, senderName, content, conversationID string) (*model.MessageHub, error) {
	if r == nil || r.db == nil || platform == "" || content == "" {
		return nil, nil
	}
	var row model.MessageHub
	q := r.db.WithContext(ctx).
		Where("platform = ? AND direction = 'outbound' AND content = ?", platform, content).
		Where("(CASE WHEN sent_at > '2000-01-01'::timestamptz THEN sent_at ELSE created_at END) > now() - interval '2 hours'").
		Order("id DESC")
	if conversationID != "" {
		q = q.Where("conversation_id = ?", conversationID)
	}
	if senderName != "" {
		q = q.Where("(sender_name = ? OR sender_name = '' OR sender_name IS NULL)", senderName)
	}
	if err := q.First(&row).Error; err == nil {
		return &row, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if conversationID == "" || len(content) < 20 {
		return nil, nil
	}
	var rows []model.MessageHub
	subQ := r.db.WithContext(ctx).
		Where("platform = ? AND direction = 'outbound' AND conversation_id = ?", platform, conversationID).
		Where("(CASE WHEN sent_at > '2000-01-01'::timestamptz THEN sent_at ELSE created_at END) > now() - interval '2 hours'").
		Where("length(content) >= 10").
		Order("id DESC").
		Limit(20)
	if err := subQ.Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		oc := rows[i].Content
		if len(oc) >= 10 && len(content) > len(oc) && strings.Contains(content, oc) {
			return &rows[i], nil
		}
	}
	return nil, nil
}

// ListRecentOutboundInConv 列出同会话近期 outbound（回环兜底检测用）。
// 仓储只按 sent_at 物理时间窗取候选行，内容归一化与变体比对在 service 层。
func (r *MessageHubRepository) ListRecentOutboundInConv(ctx context.Context, platform, accountID, conversationID string, since time.Time, limit int) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || platform == "" || conversationID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []model.MessageHub
	q := r.db.WithContext(ctx).
		Where("platform = ? AND direction = 'outbound' AND conversation_id = ?", platform, conversationID).
		Where("sent_at > ?", since).
		Order("id DESC").
		Limit(limit)
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	err := q.Find(&rows).Error
	return rows, err
}

func (r *MessageHubRepository) AckOutboundDeliveredBatchReturningWithStatus(
	ctx context.Context,
	channel, accountID, conversationID, terminalStatus string,
	msgIDs []string,
) (updatedIDs []string, affectedRows int64, err error) {
	if r.db == nil || len(msgIDs) == 0 {
		return nil, 0, nil
	}
	// CH-37b 刀4：terminalStatus 扩为三值（delivered/failed/unknown），词表收口到
	// model.IsBridgeAckTerminalStatus。unknown 走同一谓词（pending/inflight → 终态），
	// 差别仅在列值集（terminalAckUpdates：unknown 额外写 push_error=ack_unknown）。
	if !model.IsBridgeAckTerminalStatus(terminalStatus) {
		return nil, 0, fmt.Errorf("invalid terminal status: %q", terminalStatus)
	}
	updatedIDs = make([]string, 0, len(msgIDs))

	// 事务内 FOR UPDATE 取号 + 条件更新（与无状态版同构）
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sel := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ? AND status IN ('pending','inflight')",
				channel, accountID, msgIDs)
		if conversationID != "" {
			sel = sel.Where("conversation_id = ?", conversationID)
		}
		if e := sel.Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("msg_id", &updatedIDs).Error; e != nil {
			return e
		}
		if len(updatedIDs) == 0 {
			affectedRows = 0
			return nil
		}
		upd := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND msg_id IN ? AND status IN ('pending','inflight')",
				channel, accountID, updatedIDs)
		if conversationID != "" {
			upd = upd.Where("conversation_id = ?", conversationID)
		}
		res := upd.Updates(terminalAckUpdates(terminalStatus))
		if res.Error != nil {
			return res.Error
		}
		affectedRows = res.RowsAffected
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return updatedIDs, affectedRows, nil
}

// AckOutboundDeliveredBatchReturningWithStatusFenced CH-37b 刀2：claim 栅栏版详细 ack 条件更新。
// 与 AckOutboundDeliveredBatchReturningWithStatus 同构（事务内 FOR UPDATE 取号 + 条件更新），
// 差别仅在谓词：status 收紧为 'inflight' 且追加 claimed_at IN (调用方所见集合)——
// 仅当行仍是「调用方认领的那一次」才翻转（乐观并发，防 30s 认领租约回收重领后旧 acker
// 覆盖新一轮认领）。无 DDL：谓词完全落在既有 claimed_at 列上，不动索引不动 schema。
//
// 批量语义（一批多行各自 claimed_at）：谓词用本批 msgIDs 对应的 claimed_at 去重集合
// 单条 UPDATE 放行，逐行结果经返回的 updatedIDs 区分——未翻转的行由 service 层按既有
// 「toAck 但未更新」路径归 duplicate。claimedAtByMsgID 中键不属于本批 msgIDs 的条目
// 不参与谓词；行无对应条目则不可能翻转（栅栏是白名单语义，不是黑名单）。
// 集合内撞值（同批两行共用同一 claimed_at 瞬时值）是纳秒级时钟碰撞，忽略。
func (r *MessageHubRepository) AckOutboundDeliveredBatchReturningWithStatusFenced(
	ctx context.Context,
	channel, accountID, conversationID, terminalStatus string,
	msgIDs []string,
	claimedAtByMsgID map[string]time.Time,
) (updatedIDs []string, affectedRows int64, err error) {
	if r.db == nil || len(msgIDs) == 0 || len(claimedAtByMsgID) == 0 {
		return nil, 0, nil
	}
	// CH-37b 刀4：terminalStatus 扩为三值（delivered/failed/unknown），与无栅栏版同词表。
	// 栅栏谓词（status='inflight' AND claimed_at IN ...）对 unknown 行天然不匹配——
	// 行一旦落 unknown 就不再被任何 ack 翻转，对账只能走 ReconcileUnknownOutbound。
	if !model.IsBridgeAckTerminalStatus(terminalStatus) {
		return nil, 0, fmt.Errorf("invalid terminal status: %q", terminalStatus)
	}
	// 栅栏值域收紧到本批 msgIDs：键不在批内的条目绝不参与谓词。
	leaseValues := make([]time.Time, 0, len(msgIDs))
	seenLease := make(map[time.Time]struct{}, len(msgIDs))
	for _, id := range msgIDs {
		t, ok := claimedAtByMsgID[id]
		if !ok {
			continue
		}
		if _, dup := seenLease[t]; dup {
			continue
		}
		seenLease[t] = struct{}{}
		leaseValues = append(leaseValues, t)
	}
	if len(leaseValues) == 0 {
		return nil, 0, nil
	}
	updatedIDs = make([]string, 0, len(msgIDs))

	// 事务内 FOR UPDATE 取号 + 条件更新（与无栅栏版同构，仅谓词收紧）
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sel := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ? "+
				"AND status = 'inflight' AND claimed_at IN ?",
				channel, accountID, msgIDs, leaseValues)
		if conversationID != "" {
			sel = sel.Where("conversation_id = ?", conversationID)
		}
		if e := sel.Clauses(clause.Locking{Strength: "UPDATE"}).Pluck("msg_id", &updatedIDs).Error; e != nil {
			return e
		}
		if len(updatedIDs) == 0 {
			affectedRows = 0
			return nil
		}
		upd := tx.Model(&model.MessageHub{}).
			Where("platform = ? AND account_id = ? AND msg_id IN ? "+
				"AND status = 'inflight' AND claimed_at IN ?",
				channel, accountID, updatedIDs, leaseValues)
		if conversationID != "" {
			upd = upd.Where("conversation_id = ?", conversationID)
		}
		res := upd.Updates(terminalAckUpdates(terminalStatus))
		if res.Error != nil {
			return res.Error
		}
		affectedRows = res.RowsAffected
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return updatedIDs, affectedRows, nil
}

// ReconcileUnknownOutbound CH-37b 刀4（R2/R6）对账出口：把 unknown 行**显式**迁移到
// delivered 或 failed（unknown 的唯一出路——永不自动重发，详见 model.BridgeAckStatusUnknown）。
//
// 谓词刻意收窄到 status='unknown'：与 ack/回收/exhaust 的取行集合零交集，因此
//   - 对账与迟到 ack 并发时二者互斥（ack 谓词 pending/inflight 匹配不到 unknown 行）；
//   - 两个对账并发时 PG 行锁 + WHERE status='unknown' 重评保证单胜者（败者 0 行）。
//
// 操作来源痕迹：push_error 写 'reconciled_unknown:<operator>'（operator 由 service 层强制
// 非空；本层再兜一道）。sent_at/claimed_at 不动：unknown 翻入时已盖 sent_at，行已是终态，
// 重盖只会抹掉「何时变成结果不明」的现场。租户作用域依赖隔离 callback（on 模式下 ctx
// 未绑租户即 fail-closed 拒绝），与仓内其余 message_hub 写路径同口径。
func (r *MessageHubRepository) ReconcileUnknownOutbound(ctx context.Context, channel, accountID string, ids []uint, target, operator string) (int64, error) {
	if r == nil || r.db == nil || len(ids) == 0 {
		return 0, nil
	}
	// 对账目标只许 delivered/failed：unknown→unknown 无意义，其余值一律拒绝。
	if target != model.BridgeAckStatusDelivered && target != model.BridgeAckStatusFailed {
		return 0, fmt.Errorf("invalid reconcile target: %q (must be delivered or failed)", target)
	}
	if strings.TrimSpace(operator) == "" {
		return 0, errors.New("reconcile operator required: 对账必须留操作来源")
	}
	trace := pushReconciledUnknownPrefix + operator
	if len(trace) > 200 { // push_error varchar(200)：截断 operator 保住前缀可辨识
		trace = trace[:200]
	}
	res := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND id IN ? AND status = ?",
			channel, accountID, ids, model.BridgeAckStatusUnknown).
		Updates(map[string]any{"status": target, "push_error": trace})
	return res.RowsAffected, res.Error
}

// CountOutboundUnknown CH-37b 刀4（R2/R6）：unknown 行数计量（对账入口的待办读数）。
// channel/accountID 传空串 = 不按该维过滤（全局口径）；租户作用域同上依赖隔离 callback。
func (r *MessageHubRepository) CountOutboundUnknown(ctx context.Context, channel, accountID string) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	q := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("direction = 'outbound' AND status = ?", model.BridgeAckStatusUnknown)
	if channel != "" {
		q = q.Where("platform = ?", channel)
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// OutboundUnknownRow unknown 对账管理面的行视图（CH-37b 刀5）：只暴露对账需要的字段，
// 不整行返回——content 等大字段与客户侧消息体对管理动作无用且可能含 PII。
type OutboundUnknownRow struct {
	ID             uint       `json:"id"`
	Platform       string     `json:"platform"`
	AccountID      string     `json:"account_id"`
	ConversationID string     `json:"conversation_id"`
	MsgID          string     `json:"msg_id"`
	Status         string     `json:"status"`
	PushError      string     `json:"push_error"`
	PushAttempts   int        `json:"push_attempts"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at"`
}

// ListOutboundUnknown CH-37b 刀5：unknown 行清单（对账入口的待办读数，与
// CountOutboundUnknown 同谓词同作用域）。id 游标升序分页——对账操作按行 id 定位，
// 稳定排序保证翻页不重不漏。channel/accountID 传空串 = 不按该维过滤（全局口径）；
// 租户作用域同 CountOutboundUnknown 依赖隔离 callback。limit<=0 用默认 50，上界 200
// （管理读口不承载全量导出）。
func (r *MessageHubRepository) ListOutboundUnknown(ctx context.Context, channel, accountID string, limit, offset int) ([]OutboundUnknownRow, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	q := r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("direction = 'outbound' AND status = ?", model.BridgeAckStatusUnknown).
		Select("id, platform, account_id, conversation_id, msg_id, status, push_error, push_attempts, created_at, sent_at").
		Order("id ASC").
		Limit(limit).Offset(offset)
	if channel != "" {
		q = q.Where("platform = ?", channel)
	}
	if accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	rows := make([]OutboundUnknownRow, 0, limit)
	err := q.Scan(&rows).Error
	return rows, err
}

func (r *MessageHubRepository) AnyExistsByMsgIDs(ctx context.Context, channel string, msgIDs []string) (map[string]bool, error) {
	if r == nil || r.db == nil || len(msgIDs) == 0 || channel == "" {
		return map[string]bool{}, nil
	}
	out := make(map[string]bool, len(msgIDs))
	for _, id := range msgIDs {
		out[id] = false
	}
	type idRow struct {
		MsgID string `gorm:"column:msg_id"`
	}
	var rows []idRow
	err := r.db.WithContext(ctx).
		Model(&model.MessageHub{}).
		Select("DISTINCT msg_id").
		Where("platform = ? AND msg_id IN ?", channel, msgIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.MsgID] = true
	}
	return out, nil
}

func (r *MessageHubRepository) FetchOutboundSince(ctx context.Context, channel, accountID string, sinceID uint64, limit int) ([]model.MessageHub, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	var rows []model.MessageHub
	q := r.db.WithContext(ctx).
		Where("platform = ? AND account_id = ? AND direction = 'outbound'", channel, accountID)
	if sinceID > 0 {
		q = q.Where("id > ?", sinceID)
	}
	q = q.Order("id ASC").Limit(limit)
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
