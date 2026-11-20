// delayed_outbound_repo.go AI 回复延迟出站与 bridge 出站持久化仓储（五层 L5）
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/google/uuid"
)

// errDelayedOutboundUnavailable 仓储句柄缺失。
//
// 与读口「没数据就返回空」不同：写口在句柄缺失时必须报错。调用方
// （webhook_outbound 的入口分流）拿到错误会**退回直接发送**，而拿到 nil 会认为
// 「已入延迟队列」并把这条回复吞掉 —— 同一个函数两种返回，一种是消息照发、
// 一种是消息消失。
var errDelayedOutboundUnavailable = errors.New("delayed outbound repository not initialized")

// clampBatchLimit 夹取批量上限：GORM 的 Limit(0) 等于**不加 limit**，
// 一次误传 0 就会把整张待投队列读进内存（本表按设计只该有几打到几百行，
// 但 0 的含义是「无限」，不是「零条」）。
func clampBatchLimit(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > 200 {
		return 200
	}
	return limit
}

// DelayedOutboundLease 单次投递租约时长（D03）。正常投递秒级收敛，5 分钟足够
// 覆盖外部渠道超时；租约过期即视为持有者已失联，回收进程可以接管该行。
const DelayedOutboundLease = 5 * time.Minute

// DelayedOutboundRepository reach_delayed_outbound 队列收口
type DelayedOutboundRepository struct {
	db *gorm.DB
}

// NewDelayedOutboundRepository 构造
func NewDelayedOutboundRepository(db *gorm.DB) *DelayedOutboundRepository {
	return &DelayedOutboundRepository{db: db}
}

// NewDelayedOutboundRepositoryDefault 默认构造（全局句柄；与传 GetDB() 同语义）。
//
// AV-04c 批次6：service/inbox_ingress.go 装配（SetPendingOutboundProbe 的默认探针）
// 原先自己取全局句柄再传进来（service 直取 DB 句柄的宽口径），收口为包级构造——
// 调用方以 repository.DBReady() 探针保持「无库即留 nil」的原装配语义。
func NewDelayedOutboundRepositoryDefault() *DelayedOutboundRepository {
	return NewDelayedOutboundRepository(GetDB())
}

// NewReplayRepositories 保持回扫资格查询与任务仓储使用相同句柄，不向Service暴露数据库。
func NewReplayRepositories(r *DelayedOutboundRepository) (*CustomerSessionRepository, *MessageHubRepository) {
	if r == nil {
		return nil, nil
	}
	return NewCustomerSessionRepositoryWithDB(r.db), NewMessageHubRepositoryWithDB(r.db)
}

// ClaimForReplay 只认领指定租户与账号的到期任务，返回含随机租约的权威载荷。
func (r *DelayedOutboundRepository) ClaimForReplay(ctx context.Context, id uint, platform, accountID string) (*model.DelayedOutboundReply, error) {
	if r == nil || r.db == nil {
		return nil, errDelayedOutboundUnavailable
	}
	if ctx == nil || tenant.IsSystemScope(ctx) || id == 0 || platform == "" || accountID == "" {
		return nil, errors.New("delayed replay claim scope unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("delayed replay claim tenant unavailable")
	}
	var current model.DelayedOutboundReply
	err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND id = ? AND platform = ? AND account_id = ?", tenantID, id, platform, accountID).
			Where("status = ? AND send_at <= clock_timestamp()", model.DelayedStatusPending).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).First(&current).Error; err != nil {
			return err
		}
		if current.ConversationID == "" || current.SenderID == "" {
			return errors.New("delayed replay claim identity unavailable")
		}
		now := time.Now()
		until := now.Add(DelayedOutboundLease)
		current.ClaimToken, current.ClaimedAt, current.LeaseUntil = uuid.NewString(), &now, &until
		current.Status = model.DelayedStatusSending
		return _db.EnsureAffected(tx.Model(&model.DelayedOutboundReply{}).
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, model.DelayedStatusPending).
			Updates(map[string]any{"status": current.Status, "claim_token": current.ClaimToken,
				"claimed_at": now, "lease_until": until}))
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 未到期或已被其它执行者认领属于正常空结果，不掩盖其它数据库错误。
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &current, nil
}

// CreatePending 写入 pending 延迟回复记录。
//
// 句柄缺失 ⇒ 报错而不是静默成功：调用方把 nil 读成「已入队」，于是这条
// 被延迟的回复既没进队列也没发出 —— 一条消息凭空消失，且日志里只有一行
// "已进入延迟队列"。报错会让调用方退回直接发送，行为退化但消息不丢。
// MarkDueByConversation 关注事件驱动的积压补发（2026-11-15）：客户关注我们后平台
// 限额解除，该会话 quiet_hours/send_retry 延迟行立即到期（send_at=now），由常规
// 认领链路消费。返回影响行数。幂等：已到期行再次更新无副作用。
func (r *DelayedOutboundRepository) MarkDueByConversation(ctx context.Context, platform, accountID, conversationID string, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("platform = ? AND account_id = ? AND conversation_id = ? AND status IN ('pending','sending')",
			platform, accountID, conversationID).
		Updates(map[string]any{"send_at": now})
	return res.RowsAffected, res.Error
}

func (r *DelayedOutboundRepository) CreatePending(ctx context.Context, rec *model.DelayedOutboundReply) error {
	if r == nil || r.db == nil {
		return errDelayedOutboundUnavailable
	}
	if rec == nil {
		return errors.New("delayed outbound: nil 记录不是入队对象")
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// ConfirmReplayLease 在投递前确认当前租约和身份，返回数据库中的权威载荷。
// 不持锁跨网络调用；后续回写仍须使用原有令牌栅栏。
func (r *DelayedOutboundRepository) ConfirmReplayLease(ctx context.Context, snapshot *model.DelayedOutboundReply, now time.Time) (*model.DelayedOutboundReply, error) {
	if r == nil || r.db == nil {
		return nil, errDelayedOutboundUnavailable
	}
	if ctx == nil || tenant.IsSystemScope(ctx) || snapshot == nil {
		return nil, errors.New("delayed outbound: 未确认重放上下文")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 || snapshot.TenantID != tenantID {
		return nil, errors.New("delayed outbound: 重放租户不匹配")
	}
	if snapshot.ID == 0 || snapshot.ClaimToken == "" || snapshot.Platform == "" || snapshot.AccountID == "" || snapshot.ConversationID == "" || snapshot.SenderID == "" {
		return nil, errors.New("delayed outbound: 重放身份或令牌缺失")
	}
	var current model.DelayedOutboundReply
	err := r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).
		Where("tenant_id = ? AND id = ? AND claim_token = ? AND status = ?", tenantID, snapshot.ID, snapshot.ClaimToken, model.DelayedStatusSending).
		Where("lease_until > ? AND claimed_at IS NOT NULL", now).
		Where("platform = ? AND account_id = ? AND conversation_id = ? AND sender_id = ?", snapshot.Platform, snapshot.AccountID, snapshot.ConversationID, snapshot.SenderID).
		First(&current).Error
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 查询等待也会消耗租约：SQL中的起始时间不能代替返回时的有效期。
	if current.LeaseUntil == nil || !current.LeaseUntil.After(time.Now()) {
		return nil, errors.New("delayed outbound: 查询返回时租约已失效")
	}
	return &current, nil
}

// PickDueForUpdate 事务：FOR UPDATE SKIP LOCKED 抢占到期的 pending 记录并置 sending，
// 逐行签发 claim_token + lease_until（D03）。token 是后续回写的栅栏：
// 租约过期行被回收/重认领后，旧持有者带旧 token 的回写必须 0 行拒绝。
func (r *DelayedOutboundRepository) PickDueForUpdate(ctx context.Context, now time.Time, limit int) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var picked []model.DelayedOutboundReply
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.DelayedOutboundReply{}).
			Where("status = ? AND send_at <= ?", "pending", now).
			Order("send_at ASC").
			Limit(clampBatchLimit(limit)).
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Find(&picked).Error; err != nil {
			return err
		}
		if len(picked) == 0 {
			return nil
		}
		for i := range picked {
			token := uuid.NewString()
			leaseUntil := now.Add(DelayedOutboundLease)
			if err := tx.Model(&model.DelayedOutboundReply{}).Where("id = ?", picked[i].ID).
				Updates(map[string]any{
					"status":      "sending",
					"claim_token": token,
					"claimed_at":  now,
					"lease_until": leaseUntil,
				}).Error; err != nil {
				return err
			}
			picked[i].Status = "sending"
			picked[i].ClaimToken = token
			claimedAt := now
			picked[i].ClaimedAt = &claimedAt
			picked[i].LeaseUntil = &leaseUntil
		}
		return nil
	})
	return picked, err
}

// ExpireStale 把 send_at 早于 cutoff 且仍 pending 的记录判为 expired，返回影响行数。
// 边界取严格小于：恰好等于 cutoff 的行本轮不动（时间单调前进，下一轮必然覆盖）。
func (r *DelayedOutboundRepository) ExpireStale(ctx context.Context, cutoff time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at < ?", "pending", cutoff).
		Update("status", "expired")
	return res.RowsAffected, res.Error
}

// CountStuckSending 只读观测：统计"到期已远超 send_at 却仍卡在 sending"的行数。
// 行进入 sending 时不另记时间戳，故以 send_at 年龄为代理信号——正常一轮投递在秒级
// 收敛，阈值外仍为 sending 即意味着进程在投递中途崩溃。本方法不改任何状态，
// 回收/重投语义待产品拍板后再引入。
func (r *DelayedOutboundRepository) CountStuckSending(ctx context.Context, sendAtBefore time.Time) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at < ?", "sending", sendAtBefore).
		Count(&n).Error
	return n, err
}

// PluckDueIDs 无事务 fallback：取到期 pending 记录 ID
func (r *DelayedOutboundRepository) PluckDueIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if r.db == nil {
		return nil, nil
	}
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("status = ? AND send_at <= ?", "pending", now).
		Order("send_at ASC").Limit(clampBatchLimit(limit)).
		Pluck("id", &ids).Error
	return ids, err
}

// MarkSendingIfPending CAS：pending → sending，返回是否抢占成功。
// 仅无事务 fallback 路径使用；成功后必须接着调 ClaimSendingRows 签发租约，
// 否则回写没有栅栏 token 可验证。
func (r *DelayedOutboundRepository) MarkSendingIfPending(ctx context.Context, ids []uint) (bool, error) {
	if r.db == nil {
		return false, nil
	}
	res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id IN ? AND status = ?", ids, "pending").Update("status", "sending")
	return res.RowsAffected > 0, res.Error
}

// ClaimSendingRows 给 fallback 路径的 sending 行逐行签发租约（D03）。
// 只认领「尚无有效 token」的行（claim_token 为空），返回带 token 的行供栅栏回写。
func (r *DelayedOutboundRepository) ClaimSendingRows(ctx context.Context, ids []uint, now time.Time) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var claimed []model.DelayedOutboundReply
	for _, id := range ids {
		token := uuid.NewString()
		leaseUntil := now.Add(DelayedOutboundLease)
		res := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
			Where("id = ? AND status = ? AND (claim_token = '' OR claim_token IS NULL)", id, "sending").
			Updates(map[string]any{
				"claim_token": token,
				"claimed_at":  now,
				"lease_until": leaseUntil,
			})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		var rec model.DelayedOutboundReply
		if err := r.db.WithContext(ctx).First(&rec, id).Error; err != nil {
			return nil, err
		}
		claimed = append(claimed, rec)
	}
	return claimed, nil
}

// replayUpdate 只允许仍持有效租约的当前执行者改写自己的租户行。
// 使用数据库当前时钟，避免等待行锁后仍沿用进入方法时的过期许可。
func (r *DelayedOutboundRepository) replayUpdate(ctx context.Context, id uint, token string) (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, errDelayedOutboundUnavailable
	}
	if ctx == nil || tenant.IsSystemScope(ctx) || id == 0 || token == "" {
		return nil, errors.New("delayed outbound: 当前执行身份未确认")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tenantID := tenant.WriteTenantID(ctx)
	if tenantID == 0 {
		return nil, errors.New("delayed outbound: 当前租户未确认")
	}
	return r.db.WithContext(tenant.OnlyTenant(ctx, tenantID)).Model(&model.DelayedOutboundReply{}).
		Where("tenant_id = ? AND id = ? AND status = ? AND claim_token = ?", tenantID, id, model.DelayedStatusSending, token).
		Where("claimed_at IS NOT NULL AND lease_until > clock_timestamp()"), nil
}

// MarkSent 重放成功：status → sent，仅代表已向下游交接。
//
// 0 行 ⇒ ErrRecordNotFound：可能是这行在投递途中被删，也可能（D03）是本 worker
// 租约已过期、行已被重认领给新 token —— 两种情况都不得把「状态没回写」记成
// 「已送达」。
func (r *DelayedOutboundRepository) MarkSent(ctx context.Context, id uint, token string, sentAt time.Time) error {
	q, err := r.replayUpdate(ctx, id, token)
	if err != nil {
		return err
	}
	return _db.EnsureAffected(q.Updates(map[string]any{"status": model.DelayedStatusSent, "sent_at": &sentAt, "lease_until": nil}))
}

// HasPendingByConversation 该会话是否还有「尚未投递且仍会被投递」的延迟出站。
//
// 只认 pending：sending 是本轮已抢占的瞬时态，进程崩溃时会永久悬挂在该状态，
// 若把它算作待发，recheck 就会被一条没人投的行长期压制，客户反而永远收不到回复。
func (r *DelayedOutboundRepository) HasPendingByConversation(ctx context.Context, conversationID string) (bool, error) {
	if r.db == nil || conversationID == "" {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("conversation_id = ? AND status = ?", conversationID, model.DelayedStatusPending).
		Count(&n).Error
	return n > 0, err
}

// ScheduleRetry 重投失败后原地改排：回到 pending、send_at 推到 nextAt、attempts+1、记 last_error、
// 清租约。不新建行：一条待投回复在队列里始终只有一行，避免同一份内容因多次失败被重放多次。
// 必须持当前有效 token；终态、过期执行者和未认领行均不可改排。
func (r *DelayedOutboundRepository) ScheduleRetry(ctx context.Context, id uint, token string, nextAt time.Time, lastError string) error {
	q, err := r.replayUpdate(ctx, id, token)
	if err != nil {
		return err
	}
	return _db.EnsureAffected(q.Updates(map[string]any{
		"status":      model.DelayedStatusPending,
		"send_at":     nextAt,
		"attempts":    gorm.Expr("attempts + 1"),
		"last_error":  truncateForColumn(lastError, 1024),
		"claim_token": "",
		"claimed_at":  nil,
		"lease_until": nil,
		"sent_at":     nil,
	}))
}

// FinishReplay 重放收口到终态（sent / superseded / failed / unknown），失败原因留在 last_error。
// 当前执行者仅可从有效 sending 租约收口；只有 sent 填写发送时间。
func (r *DelayedOutboundRepository) FinishReplay(ctx context.Context, id uint, token, status string, at time.Time, lastError string) error {
	switch status {
	case model.DelayedStatusSent, model.DelayedStatusSuperseded, model.DelayedStatusFailed, model.DelayedStatusUnknown:
	default:
		return errors.New("delayed outbound: 非法执行收口状态")
	}
	q, err := r.replayUpdate(ctx, id, token)
	if err != nil {
		return err
	}
	updates := map[string]any{"status": status, "sent_at": nil, "lease_until": nil}
	if status == model.DelayedStatusSent {
		updates["sent_at"] = &at
	}
	if lastError != "" {
		updates["last_error"] = truncateForColumn(lastError, 1024)
	}
	return _db.EnsureAffected(q.Updates(updates))
}

// ListExpiredLeaseSending 列出租约已过期的 sending 行（D03 回收对象）。
func (r *DelayedOutboundRepository) ListExpiredLeaseSending(ctx context.Context, now time.Time, limit int) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.DelayedOutboundReply
	err := r.db.WithContext(ctx).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", "sending", now).
		Order("send_at ASC").Limit(clampBatchLimit(limit)).
		Find(&rows).Error
	return rows, err
}

// ListLegacyStuckSending 列出无租约字段的历史 sending 悬挂行（D03 之前的旧数据：
// 没有 claim_token/lease_until，只能按 send_at 年龄判定）。
func (r *DelayedOutboundRepository) ListLegacyStuckSending(ctx context.Context, sendAtBefore time.Time, limit int) ([]model.DelayedOutboundReply, error) {
	if r.db == nil {
		return nil, nil
	}
	var rows []model.DelayedOutboundReply
	err := r.db.WithContext(ctx).
		Where("status = ? AND (lease_until IS NULL) AND send_at < ?", "sending", sendAtBefore).
		Order("send_at ASC").Limit(clampBatchLimit(limit)).
		Find(&rows).Error
	return rows, err
}

// MarkSendingUnknown 把 sending 行（带或不带 token）判为 unknown 待人工核对。
// 外部渠道不提供可靠的按条查询时，「可能已发」的行禁止自动重发，只能转 unknown。
func (r *DelayedOutboundRepository) MarkSendingUnknown(ctx context.Context, id uint, token, reason string) error {
	if r == nil || r.db == nil {
		return errDelayedOutboundUnavailable
	}
	q := r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ? AND status = ?", id, "sending")
	if token != "" {
		q = q.Where("claim_token = ?", token)
	}
	updates := map[string]any{
		"status":      model.DelayedStatusUnknown,
		"lease_until": nil,
		"last_error":  truncateForColumn("发送结果未知待核对: "+reason, 1024),
	}
	return _db.EnsureAffected(q.Updates(updates))
}

// ResolveUnknown 人工核对 unknown 行后的收敛出口（D03：不永久无主悬挂）。
//   - target=pending：核对确认没发出去，重新入队（sendAt=now 立即投）；
//   - target=sent：核对确认已送达（补记 sentAt）；
//   - target=superseded：核对确认不需要再发（如人工已另行回复）。
func (r *DelayedOutboundRepository) ResolveUnknown(ctx context.Context, id uint, target string, sendAt time.Time, note string) error {
	if r == nil || r.db == nil {
		return errDelayedOutboundUnavailable
	}
	if id == 0 {
		return errors.New("delayed outbound: 空 ID 不是核对对象")
	}
	updates := map[string]any{"lease_until": nil}
	switch target {
	case model.DelayedStatusPending:
		updates["status"] = model.DelayedStatusPending
		updates["send_at"] = sendAt
	case model.DelayedStatusSent:
		updates["status"] = model.DelayedStatusSent
		updates["sent_at"] = &sendAt
	case model.DelayedStatusSuperseded:
		updates["status"] = model.DelayedStatusSuperseded
		updates["sent_at"] = &sendAt
	default:
		return fmt.Errorf("delayed outbound: 不支持的 unknown 核对出口 %q（允许 pending/sent/superseded）", target)
	}
	if note != "" {
		updates["last_error"] = truncateForColumn("人工核对: "+note, 1024)
	}
	return _db.EnsureAffected(r.db.WithContext(ctx).Model(&model.DelayedOutboundReply{}).
		Where("id = ? AND status = ?", id, model.DelayedStatusUnknown).Updates(updates))
}

// truncateForColumn 按 rune 上限截断入库文本（varchar 超长会整条 UPDATE 失败，
// 宁可截断也不能让失败原因把重试改排一起带崩）。
func truncateForColumn(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	return string(r)
}

// CreateMessageHubIdempotent 幂等写 message_hub（platform+msg_id+conversation_id 冲突忽略），
// 返回实际持久化的行与首次错误（供调用方重试判定）。
//
// 租户 fail-closed（2026-11-14 生产事故修复）：msg.TenantID==0 时先从 ctx 补租户；
// ctx 也没有则**拒绝落库**而非放行——隔离 callback 对无租户 ctx 不填列，gorm
// default:1 会把行写进 tenant 1，成为"认领查不到、管理台看不见、AI 侧却记已回复"
// 的三重静默孤儿（2026-11-14 douyin 4610 行实证；10-22/11-04 另有 3 条同型孤儿）。
// 唯一索引 (tenant_id,platform,msg_id,conversation_id,direction) 含 tenant_id：
// 缺租户还会绕过与真实租户行的幂等冲突判定，造成跨租户重复回复。
func (r *DelayedOutboundRepository) CreateMessageHubIdempotent(ctx context.Context, msg *model.MessageHub) (persisted *model.MessageHub, firstErr error) {
	if r.db == nil {
		return msg, nil
	}
	if err := prepareMessageHubTenant(ctx, msg); err != nil {
		return nil, err
	}
	// D-2（2026-10-23）：唯一索引 uni_message_hub_platform_msg_conv 已加 direction
	//（priority:4），ON CONFLICT 的列清单必须与索引形状一致，否则 PG 报
	// "no unique or exclusion constraint matching the ON CONFLICT specification"。
	//
	// 未尝试/未认领的真实失败仍可恢复，避免唯一键静默吞回复；审计、已尝试失败、
	// 已交付/未知/对账终态不得因同文案或软删自动复活。恢复不是新预算。
	conflict := clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "platform"}, {Name: "msg_id"}, {Name: "conversation_id"}, {Name: "direction"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":      "pending",
			"deleted_at":  nil,
			"claimed_at":  nil,
			"push_error":  "",
			"content":     clause.Column{Table: "excluded", Name: "content"},
			"msg_type":    clause.Column{Table: "excluded", Name: "msg_type"},
			"media_url":   clause.Column{Table: "excluded", Name: "media_url"},
			"sent_at":     clause.Column{Table: "excluded", Name: "sent_at"},
			"trace_id":    clause.Column{Table: "excluded", Name: "trace_id"},
			"is_ai_reply": clause.Column{Table: "excluded", Name: "is_ai_reply"},
			"ai_agent":    clause.Column{Table: "excluded", Name: "ai_agent"},
			"extra":       clause.Column{Table: "excluded", Name: "extra"},
			"receiver_id": clause.Column{Table: "excluded", Name: "receiver_id"},
			"sender_id":   clause.Column{Table: "excluded", Name: "sender_id"},
		}),
		Where: clause.Where{Exprs: []clause.Expression{gorm.Expr(
			"excluded.status = 'pending' AND excluded.direction = 'outbound' AND " +
				outboundDeliveryPredicate("excluded") + " AND " +
				outboundDeliveryPredicate("message_hub") + " AND " +
				"(message_hub.status = 'failed' OR (message_hub.status = 'pending' AND message_hub.deleted_at IS NOT NULL)) AND " +
				"message_hub.push_attempts = 0 AND message_hub.claimed_at IS NULL AND " +
				outboundRecoverableErrorPredicate("message_hub"),
		)}},
	}
	if err := r.db.WithContext(ctx).Clauses(conflict).Create(msg).Error; err != nil {
		return nil, err
	}
	return msg, nil
}

// CreateMessageHubAudit 持久化不可投递的失败审计；同键重入只保留原记录，不改状态或预算。
func (r *DelayedOutboundRepository) CreateMessageHubAudit(ctx context.Context, msg *model.MessageHub) (*model.MessageHub, error) {
	if err := prepareMessageHubTenant(ctx, msg); err != nil {
		return nil, err
	}
	msg.Status, msg.Direction = "failed", "outbound"
	extra := make(model.JSONMap, len(msg.Extra)+1)
	for key, value := range msg.Extra {
		extra[key] = value
	}
	extra["delivery_kind"] = "audit"
	msg.Extra = extra
	if r.db == nil {
		return msg, nil
	}
	conflict := clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "platform"}, {Name: "msg_id"}, {Name: "conversation_id"}, {Name: "direction"}},
		DoNothing: true,
	}
	if err := r.db.WithContext(ctx).Clauses(conflict).Create(msg).Error; err != nil {
		return nil, err
	}
	return msg, nil
}

func prepareMessageHubTenant(ctx context.Context, msg *model.MessageHub) error {
	if msg == nil {
		return fmt.Errorf("message_hub 落库缺消息")
	}
	if msg.TenantID != 0 {
		return nil
	}
	if tid, ok := tenant.FromContext(ctx); ok && tid != 0 {
		msg.TenantID = tid
		return nil
	}
	return fmt.Errorf("message_hub 落库缺租户上下文（fail-closed）：platform=%s msg_id=%s conv=%s",
		msg.Platform, msg.MsgID, msg.ConversationID)
}
