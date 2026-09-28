package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// MaxOutboundPushAttempts 出站行的重推上界（批20d-A3 / §8.3 行 3）。
//
// 一次「认领成功」= 服务端把这一行交给桥端一次。到界后这一行离开 owed 集合并落 failed，
// 不再由任何一条取行路径给出。取值 20 的口径：认领超时 30s ⇒ 约 10 分钟的持续失败；
// 再长就是「桥端在线但这条永远发不出去」的死循环（同行给这类循环都设了界：
// Sidekiq 25→dead、SQS maxReceiveCount→DLQ、River discarded）。
// 上界放在仓储而不是由调用方传：三条取行路径必须同口径，谁忘了传就等于没设界。
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
	const q = `UPDATE message_hub
		SET status = 'delivered', sent_at = now()
		WHERE platform = ? AND account_id = ? AND direction = 'outbound' AND msg_id IN ? AND status IN ('pending','inflight')
		RETURNING msg_id`
	tx := r.db.WithContext(ctx).Raw(q, channel, accountID, msgIDs)
	if err = tx.Scan(&updatedIDs).Error; err != nil {
		return nil, 0, err
	}
	affectedRows = int64(len(updatedIDs))
	return updatedIDs, affectedRows, nil
}

// exhaustOutbound 把「重推次数已用尽、此刻仍欠交付」的出站行升级成终态 failed（批20d-A3）。
//
// 为什么由三条取行路径各自顺手做，而不是让调用方记得调一次：这一界的全部意义是
// 「不会再有人重推它」，只要有一条路径忘了（或将来新增一条），那批行就又回到永推状态；
// 忘一次的代价正是本批立项时的事实。放在取行的地方 = 想取就必须先结算。
//
// 落终态时**不写 sent_at**：这条从没交付出去，而 sent_at 是「已发出的时刻」——
// 回显检测（ListRecentOutboundInConv / GetOutboundByPlatformSenderContent*）按它筛候选，
// 给一条页面上从未出现过的内容盖上时间戳，等于让后续回复被误判成「自己发过的回显」而吞掉。
// 桥端主动 ack failed 的路径照旧写 sent_at（那是既有语义，本批不动），两者靠 push_error 分辨。
func (r *MessageHubRepository) exhaustOutbound(ctx context.Context, channel, accountID string, cutoff time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`UPDATE message_hub
		SET status = 'failed', push_error = ?, claimed_at = NULL
		WHERE platform = ? AND account_id = ? AND direction = 'outbound'
		  AND push_attempts >= ?
		  AND (status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?))`,
		pushExhaustedError, channel, accountID, MaxOutboundPushAttempts, cutoff).Error
}

// pushExhaustedError 写在行上的原因。不重复带上界数字：同一行的 push_attempts 就是那个数，
// 在这里再抄一份只是给「两处不一致」留口子。
const pushExhaustedError = "outbound_push_exhausted"

func (r *MessageHubRepository) ClaimPendingOutbound(ctx context.Context, channel, accountID string, limit int, claimTimeout time.Duration) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || limit <= 0 {
		return nil, nil
	}
	cutoff := time.Now().Add(-claimTimeout)

	if err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {
		fmt.Printf("[ClaimPendingOutbound] 到界行升级终态失败（继续认领）: %v\n", err)
	}

	if err := r.db.WithContext(ctx).
		Model(&model.MessageHub{}).
		Where("platform = ? AND account_id = ? AND direction = 'outbound' AND status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?", channel, accountID, cutoff).
		Updates(map[string]any{"status": "pending", "claimed_at": nil}).Error; err != nil {
		fmt.Printf("[ClaimPendingOutbound] 回收超时 inflight 失败（继续认领）: %v\n", err)
	}

	list := make([]model.MessageHub, 0, limit)
	// push_attempts 在这里 +1：一次认领就是一次「交给桥端去发」，无论它后来 ack 没 ack。
	// 回收那一步（上面）刻意不碰这一列——「这次没送成」不等于「没送过」，
	// 清零等于每 30s 白送一轮预算，上界就成了摆设。
	const q = `UPDATE message_hub SET status = 'inflight', claimed_at = now(), push_attempts = push_attempts + 1
		WHERE id IN (
			SELECT id FROM message_hub
			WHERE platform = ? AND account_id = ? AND direction = 'outbound' AND status = 'pending'
			  AND push_attempts < ?
			ORDER BY id ASC LIMIT ?
			FOR UPDATE SKIP LOCKED
		)
		RETURNING *`
	if err := r.db.WithContext(ctx).Raw(q, channel, accountID, MaxOutboundPushAttempts, limit).Scan(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ClaimOutboundForPush 单行认领：SSE 即时下推前必须先过这道门（批11 §3.7-2）。
//
// 认领条件与 ClaimPendingOutbound 严格同口径，只是作用域从「账号的一批」收窄到「刚写入的这一行」：
//   - status='pending'                      —— 无人认领，正常下推
//   - status='inflight' 且 claimed_at 已超超时 —— 上一次下推后扩展没 ack（发送失败/进程被杀/SW 回收），
//     这条就是要重投的那条；不重投它它就永久停在 inflight，客户收不到且无人知晓
//
// 返回 false = 这一行此刻归别人（轮询已认领 / 已 delivered / 已 failed / 入站方向）、
// 或它的重推预算已用完（批20d-A3：与轮询同口径，否则只关一条路径＝换条路径照样永推）→ 调用方必须放弃推送。
// 单表单行、条件更新，天然互斥（并发两个推送只有一个能拿到 1 行），不需要额外锁。
func (r *MessageHubRepository) ClaimOutboundForPush(ctx context.Context, id uint64, claimTimeout time.Duration) (bool, error) {
	if r == nil || r.db == nil || id == 0 {
		return false, nil
	}
	cutoff := time.Now().Add(-claimTimeout)
	const q = `UPDATE message_hub SET status = 'inflight', claimed_at = now(), push_attempts = push_attempts + 1
		WHERE id = ? AND direction = 'outbound' AND push_attempts < ?
		  AND (status = 'pending' OR (status = 'inflight' AND claimed_at IS NOT NULL AND claimed_at < ?))
		RETURNING id`
	var ids []uint64
	if err := r.db.WithContext(ctx).Raw(q, id, MaxOutboundPushAttempts, cutoff).Scan(&ids).Error; err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// FetchOutboundUndelivered 列出「此刻仍欠交付」的出站行（批11 §3.7-2 的重投来源）。
//
// 与 FetchOutboundSince 的差别是本批要修的语义核心：那个按 id 游标取，游标一过就再也看不见
// 这条行——于是「推给客户但发送失败」的消息随游标前移永不再投（扩展端 lastEventID 一存即过），
// 出站静默丢失。本函数按**状态**取：pending（没人碰过）或 inflight 且认领已超时（碰过但没 ack），
// 交付完（delivered/failed）自然离开集合，因此收敛且与游标无关。
//
// inflight 但 claimed_at IS NULL 不算欠：那是别处（非本批两条认领路径）置的状态，
// 无超时可判，宁可漏投一轮也不重复推。
//
// 「收敛」在批20d-A3 之前只是语义上的承诺：一条桥端永远发不出去的消息会同时满足
// pending 与 inflight 超时两个条件，于是每轮都被列出、被重推、再无下文——列表侧没有上界，
// 只有关掉认领那两条路径的界等于没关（本函数是三条取行路径里的第三条，也是唯一一条
// 「只读不认领」的，最容易被漏）。现在它与两条认领路径同口径：先结算到界行，再按预算取。
func (r *MessageHubRepository) FetchOutboundUndelivered(ctx context.Context, channel, accountID string, claimTimeout time.Duration, limit int) ([]model.MessageHub, error) {
	if r == nil || r.db == nil || channel == "" || accountID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	cutoff := time.Now().Add(-claimTimeout)
	if err := r.exhaustOutbound(ctx, channel, accountID, cutoff); err != nil {
		fmt.Printf("[FetchOutboundUndelivered] 到界行升级终态失败（继续取待推）: %v\n", err)
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

// ListRecentOutboundInConv 列出本账号同会话近期 outbound（回环兜底检测用）。
//
// 按 sent_at 过滤（DOM 抓取回显的变体比对在 service 层做归一化后进行，
// 仓储只负责按物理时间窗取候选行，不掺内容判断）。
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
	if terminalStatus != model.BridgeAckStatusDelivered && terminalStatus != model.BridgeAckStatusFailed {
		return nil, 0, fmt.Errorf("invalid terminal status: %q", terminalStatus)
	}
	updatedIDs = make([]string, 0, len(msgIDs))

	const qBase = `UPDATE message_hub
		SET status = ?, sent_at = now()
		WHERE platform = ? AND account_id = ? AND direction = 'outbound'
		  AND msg_id IN ? AND status IN ('pending','inflight')`
	q := qBase
	if conversationID != "" {
		q = qBase + " AND conversation_id = ?"
	}

	var tx *gorm.DB
	if conversationID != "" {
		tx = r.db.WithContext(ctx).Raw(q+" RETURNING msg_id", terminalStatus, channel, accountID, msgIDs, conversationID)
	} else {
		tx = r.db.WithContext(ctx).Raw(q+" RETURNING msg_id", terminalStatus, channel, accountID, msgIDs)
	}
	if err = tx.Scan(&updatedIDs).Error; err != nil {
		return nil, 0, err
	}
	affectedRows = int64(len(updatedIDs))
	return updatedIDs, affectedRows, nil
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
