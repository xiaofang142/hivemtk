// order_draft.go 订单草稿仓储（T-P2-01 / R-1）
//
// 五层架构里本文件是 service.OrderDraftService 与 order_drafts 表之间唯一的一层：
// service 不碰 *gorm.DB，只依赖本接口（内存实现见 service/order_draft_store.go）。
//
// 刻意没有"写失败就退回内存"这条路：草稿是销售要拿去成单的工作数据，静默丢一份
// 比明确报错严重得多（对照 T-P1-08 的审计落库——审计可以降级，业务数据不行）。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// ErrOrderDraftPendingConflict 同一客户同一产品已存在他人先建的 pending 草稿。
//
// 单列成错误而非让调用方去匹配 PG 约束名：判据要写在代码里才能测，
// 而 "23505" 这种字符串比较散在 service 里等于没有判据。
var ErrOrderDraftPendingConflict = errors.New("order_draft: 该客户同产品已有 pending 草稿")

// draftWriteColumns 更新时整行覆盖的列（不含 id/created_at）。
//
// 为什么显式列清单而不是 UpdateAll：`Select("*")` 在 GORM 里会连 created_at 一起写，
// 于是一次普通编辑会把草稿的创建时间挪到现在 —— 列表排序与"7 天未确认过期"都按
// created_at/expires_at 算，创建时间被挪等于把草稿的寿命无限续期。
var draftWriteColumns = []string{
	"customer_id", "one_id", "owner_id", "product_name", "product_id", "category",
	"quantity", "unit_price", "total_amount", "confidence",
	"source", "source_text", "intent_id",
	"status", "order_id", "note", "cancel_reason", "metadata",
	"updated_at", "expires_at", "confirmed_at", "cancelled_at",
}

// OrderDraftRepository 草稿持久化读写接口
type OrderDraftRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Upsert 按主键插入或整行更新；同客户同产品已有 pending 行时返回
	// ErrOrderDraftPendingConflict（部分唯一索引 uq_order_draft_pending）。
	Upsert(ctx context.Context, d *model.OrderDraft) error

	// GetByID 按草稿 ID 读取；不存在返回 (nil, nil)。
	GetByID(ctx context.Context, id string) (*model.OrderDraft, error)

	// ListPendingByCustomer 列出某客户仍处于 pending 的草稿（按创建时间升序）。
	// 供 service 做"同名/包含关系"模糊去重：SQL 只负责收窄候选集，
	// 匹配规则留在业务层，避免把业务判据劈成两处各说各话。
	ListPendingByCustomer(ctx context.Context, customerID string) ([]*model.OrderDraft, error)

	// ListPending 列出未过期的 pending 草稿，排序 = 置信度↓、金额↓、到期时间↑。
	// ownerID 为空 = 不限销售；limit<=0 = 不限条数。
	ListPending(ctx context.Context, ownerID string, now time.Time, limit int) ([]*model.OrderDraft, error)

	// ListByCustomer 某客户的全部草稿（含终态），按创建时间倒序。
	ListByCustomer(ctx context.Context, customerID string) ([]*model.OrderDraft, error)

	// ListByOwner 某销售的全部草稿，pending 优先、其余按创建时间倒序。
	ListByOwner(ctx context.Context, ownerID string) ([]*model.OrderDraft, error)

	// ExpirePendingBatch 在一条事务里锁定一批"已到期且仍 pending"的草稿并翻成 expired，
	// 返回**实际被翻转**的行（供上层逐条发统计事件）。limit<=0 表示整批不限。
	//
	// 为什么不用"先查一批再按 id 更新"：那样查到的集合与真正被改写的行之间可以夹进
	// 一次 Confirm —— 结果是把刚成交的草稿刷成过期，且统计里多一条假的 expired 事件。
	// FOR UPDATE SKIP LOCKED 同时解决两件事：锁住本轮要改的行，并让多副本 worker 各拿
	// 一份不相交的集合（而不是两个进程把同一批草稿各过期一次、事件记两遍）。
	ExpirePendingBatch(ctx context.Context, now time.Time, limit int) ([]*model.OrderDraft, error)

	// MutatePending 在一条事务里锁住某行、确认仍是 pending 后交 fn 就地改，再写回。
	// 返回 false = 行不存在或已不是 pending（fn 未被调用）。
	//
	// "读—判—写"三步合成一次加锁读，是为了让 Confirm/Cancel/Edit 不必各写一遍
	// 乐观重试：两个销售同时点确认时，落败方拿到的是 false 而不是"成功"。
	MutatePending(ctx context.Context, id string, fn func(*model.OrderDraft)) (bool, error)

	// PurgeTerminal 删除终态（cancelled/expired）且 updated_at 早于 before 的行，
	// 返回删除行数。pending/confirmed 不在删除集合内，见 model.OrderDraftTerminalStatuses。
	PurgeTerminal(ctx context.Context, before time.Time, limit int) (int64, error)

	// CountByStatus 各状态行数（观测"是否还在无界增长"的唯一入口）。
	CountByStatus(ctx context.Context) (map[string]int64, error)
}

type orderDraftRepo struct {
	db *gorm.DB
}

// NewOrderDraftRepository 创建草稿仓库实例（用全局 DB）
func NewOrderDraftRepository() OrderDraftRepository {
	return &orderDraftRepo{db: _db.GetDB()}
}

// NewOrderDraftRepositoryWithDB 创建指定数据库连接的草稿仓库实例（用于测试）
func NewOrderDraftRepositoryWithDB(db *gorm.DB) OrderDraftRepository {
	return &orderDraftRepo{db: db}
}

func (r *orderDraftRepo) Available() bool { return r != nil && r.db != nil }

func (r *orderDraftRepo) require() error {
	if !r.Available() {
		return errors.New("order_draft repository: db handle is nil")
	}
	return nil
}

func (r *orderDraftRepo) Upsert(ctx context.Context, d *model.OrderDraft) error {
	if err := r.require(); err != nil {
		return err
	}
	if d == nil || d.ID == "" {
		return errors.New("order_draft repository: 空草稿或空 ID")
	}
	updates := make([]clause.Assignment, 0, len(draftWriteColumns))
	for _, col := range draftWriteColumns {
		updates = append(updates, clause.Assignment{
			Column: clause.Column{Name: col},
			Value:  gorm.Expr("EXCLUDED." + quoteIdent(col)),
		})
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: updates,
	}).Create(d).Error
	if err == nil {
		return nil
	}
	if isPendingConflict(err) {
		return ErrOrderDraftPendingConflict
	}
	return err
}

// quoteIdent 双引号包裹标识符。
//
// 本函数的输入全部来自包内常量 draftWriteColumns（不是用户输入），加引号不是为了
// 防注入，而是让 EXCLUDED.<col> 在 GORM 的表达式解析里保持原样；写在这里是
// 免得每列手拼一遍把引号漏掉。
func quoteIdent(s string) string { return `"` + s + `"` }

// isPendingConflict 判定"命中的是 uq_order_draft_pending 还是别的约束"。
//
// 必须区分：id 主键冲突已被 ON CONFLICT 消化，正常情况下不会回到这里；真回到这里的
// 唯一预期就是那个部分唯一索引。误把别的唯一约束当 pending 会让 service 去做一次
// 毫无意义的重查，所以 SQLSTATE 与约束名两个条件都要命中。
//
// 不认 gorm.ErrDuplicatedKey：那个类型只在 Open 时设 `TranslateError: true` 才产出，
// 本仓连接配置没开，这里拿到的是底层 *pgconn.PgError（错误串含 23505 与约束名）。
func isPendingConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "uq_order_draft_pending")
}

func (r *orderDraftRepo) GetByID(ctx context.Context, id string) (*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var d model.OrderDraft
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *orderDraftRepo) ListPendingByCustomer(ctx context.Context, customerID string) ([]*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var rows []*model.OrderDraft
	err := r.db.WithContext(ctx).
		Where("customer_id = ? AND status = ?", customerID, model.OrderDraftStatusPending).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *orderDraftRepo) ListPending(ctx context.Context, ownerID string, now time.Time, limit int) ([]*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	q := r.db.WithContext(ctx).
		Where("status = ? AND expires_at > ?", model.OrderDraftStatusPending, now)
	if ownerID != "" {
		q = q.Where("owner_id = ?", ownerID)
	}
	q = q.Order("confidence DESC, total_amount DESC, expires_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []*model.OrderDraft
	err := q.Find(&rows).Error
	return rows, err
}

func (r *orderDraftRepo) ListByCustomer(ctx context.Context, customerID string) ([]*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var rows []*model.OrderDraft
	err := r.db.WithContext(ctx).
		Where("customer_id = ?", customerID).
		Order("created_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (r *orderDraftRepo) ListByOwner(ctx context.Context, ownerID string) ([]*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	// GORM 1.30 的 Order 只收单个表达式、不接占位符参数，所以 pending 的字面量只能内联。
	// 拼进去的是本包编译期常量（值 "pending"），不是入参 —— owner_id 仍走占位符绑定。
	pendingFirst := "CASE WHEN status = '" + model.OrderDraftStatusPending + "' THEN 0 ELSE 1 END"
	var rows []*model.OrderDraft
	err := r.db.WithContext(ctx).
		Where("owner_id = ?", ownerID).
		Order(pendingFirst + ", created_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (r *orderDraftRepo) ExpirePendingBatch(ctx context.Context, now time.Time, limit int) ([]*model.OrderDraft, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var flipped []*model.OrderDraft
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND expires_at <= ?", model.OrderDraftStatusPending, now).
			Order("id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		var rows []*model.OrderDraft
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]string, 0, len(rows))
		for _, d := range rows {
			ids = append(ids, d.ID)
		}
		if err := tx.Model(&model.OrderDraft{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"status":     model.OrderDraftStatusExpired,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		for _, d := range rows {
			d.Status = model.OrderDraftStatusExpired
			d.UpdatedAt = now
		}
		flipped = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return flipped, nil
}

// MutatePending 一次加锁读 + 判态 + 写回，返回 fn 是否真的落库。
//
// 为什么不写成"GetByID → 判 status → Save"三步：那样两个销售同时点确认时，两边都
// 读到 pending、都去创建订单，结果是**一单变两单**。SELECT ... FOR UPDATE 把
// 读—判—写收进同一个行锁，落败方拿到的是 applied=false（调用方据此报"状态已变"），
// 顺带覆盖多副本进程。
func (r *orderDraftRepo) MutatePending(ctx context.Context, id string, fn func(*model.OrderDraft)) (bool, error) {
	if err := r.require(); err != nil {
		return false, err
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.OrderDraft
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&m).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if m.Status != model.OrderDraftStatusPending {
			return nil
		}
		if fn != nil {
			fn(&m)
		}
		res := tx.Model(&m).
			Where("id = ? AND status = ?", id, model.OrderDraftStatusPending).
			Select(draftWriteColumns).
			Updates(&m)
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (r *orderDraftRepo) PurgeTerminal(ctx context.Context, before time.Time, limit int) (int64, error) {
	if err := r.require(); err != nil {
		return 0, err
	}
	// 先取 id 再删：PG 不支持 DELETE ... LIMIT，直接删会让"单轮清理多少行"失去控制；
	// 分批的意义在于让一次清理的最坏耗时与锁范围有上界（同 recovery worker 的单轮上限口径）。
	q := r.db.WithContext(ctx).Model(&model.OrderDraft{}).
		Where("status IN ? AND updated_at < ?", model.OrderDraftTerminalStatuses, before).
		Order("updated_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var ids []string
	if err := q.Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).
		Where("id IN ? AND status IN ? AND updated_at < ?", ids, model.OrderDraftTerminalStatuses, before).
		Delete(&model.OrderDraft{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *orderDraftRepo) CountByStatus(ctx context.Context) (map[string]int64, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	type row struct {
		Status string
		N      int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.OrderDraft{}).
		Select("status, COUNT(*) AS n").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, g := range rows {
		out[g.Status] = g.N
	}
	return out, nil
}
