// bill.go 账单仓储（T-P7-01 / N-6）。
//
// 表 bills 的读写面只有三条写不出去的路径都不在这里：派生（Create）、
// 状态跃迁（UpdateStatus），以及两条读（按账单号、按报价行）。
// 之所以只有这么点：账单是**凭证表**——
//   - 没有改写内容的方法：amount 是那一版报价合计的快照，能改它的人就等于能改历史；
//   - 没有 Delete：作废走 open/partial→voided 的跃迁，留痕；抹掉一行会让
//     "这张报价开过几张账单"这个问题永远答不上来（AC① 的反面）。
//
// 与 quotes / opportunities 同一取向：本层刻意**不做内存版底座**。
// 一旦有影子实现，"同一版被派生过两次"这条判据就永远测不出来，
// 而那正是 AC① 的全部内容——它的实现位置是库上的唯一索引，不是 Go 里的 map。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// ErrBillAlreadyDerived 这一版报价已经派生过账单了。
//
// 单独成 sentinel 的理由与报价版本冲突同族：调用方对它的反应是**复用已有那一张**
// （幂等，AC① 的重入通路），而对别的 23505（主键、非本索引）的反应是"这次写入本身坏了，
// 别再重试"。混成一个 error 就只能二选一地错——选"复用"会把一次没写成功的行当成已存在，
// 选"报错"会让客户接受报价的重试按钮永远点不动。
var ErrBillAlreadyDerived = errors.New("bill: 该版报价已派生出账单，请直接沿用那一张")

// ErrBillStatusConflict 手里那个"它现在是什么状态"已过期，本次跃迁没有落库。
//
// 与 ErrBillNotFound 分开的代价不对称（与 quotes 同一条理由）：前者该重读再跃迁，
// 后者重读多少次都没有。
var ErrBillStatusConflict = errors.New("bill: 账单当前状态与跃迁起点不符，本次跃迁没有落库")

// ErrBillNotFound 行不存在（读侧回 (nil, nil)，写侧回本 sentinel）。
var ErrBillNotFound = errors.New("bill: 账单不存在")

// billQuoteRowConstraint 与 model.Bill 上那处 uniqueIndex 标签同源。
//
// 23505 的判据要同时认 SQLSTATE 与这个索引名：把 bills 的主键冲突也读成"已派生过"，
// service 就会去读一张从没写过的账单，而读回的是 (nil, nil) —— 一路静默到对账那天。
const billQuoteRowConstraint = "uq_bills_quote_row"

// BillRepository bills 读写接口。方法集由 bill_test.go 的
// TestBillRepository_MethodSetIsExactlyTheDocumentedSix 逐字钉住（六格里最后一格
// ListByQuoteID 是 T-P7-02 补的，放宽理由写在同一条用例的注释里）。
type BillRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Create 派生一张账单：四把身份列任一为空即拒（空 quote_row_id 既引用不了
	// 报价、也排除不了重复，它在库里是一张追不回来源的应收）；status 必须落在
	// model.BillStatuses 内（库里没有 CHECK 约束，这是值域唯一的落点）。
	// 撞 uq_bills_quote_row ⇒ ErrBillAlreadyDerived。
	Create(ctx context.Context, b *model.Bill) error

	// UpdateStatus 生命周期跃迁：`WHERE id = ? AND status = ?` 命中才生效，
	// 写集合只有 status + updated_at —— amount 若能被这条路径顺手改到，AC② 的对账就没主体了。
	// 两侧状态都必须在值域内；跃迁**合法性**（哪格能到哪格）不在本层判，见方法注释。
	UpdateStatus(ctx context.Context, id, from, to string) error

	// GetByID 按账单号读取；不存在返回 (nil, nil)，读失败返回 error。
	GetByID(ctx context.Context, id string) (*model.Bill, error)

	// GetByQuoteRowID 按"哪一版报价"读取——重复派生时的幂等复用走这条路。
	// 不存在返回 (nil, nil)。
	GetByQuoteRowID(ctx context.Context, quoteRowID string) (*model.Bill, error)

	// ListByQuoteID 按**逻辑报价号**捞出这张报价单开过的全部应收，按派生早晚升序。
	// T-P7-02 补的那一格（对账读口："这张报价开了几张应收、各欠多少"）；
	// 放宽接口形状的判据、以及"为什么这一格不构成列全表"，逐字见 bill_test.go 的
	// TestBillRepository_MethodSetIsExactlyTheDocumentedSix。零命中是空切片 + nil error。
	ListByQuoteID(ctx context.Context, quoteID string) ([]*model.Bill, error)
}

type billRepo struct {
	db *gorm.DB
}

// NewBillRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）。
//
// 本卡不产出 NewBillRepository()（读全局句柄的那个版本）：账单的第一个消费者
// 是报价接受路径，它的装配点与 quote 服务同处，等那边的注入落地再一起给，
// 先造一个没人调的构造函数就是把未走过的路写进生产代码。
func NewBillRepositoryWithDB(db *gorm.DB) BillRepository {
	return &billRepo{db: db}
}

func (r *billRepo) Available() bool { return r != nil && r.db != nil }

func (r *billRepo) require() error {
	if !r.Available() {
		return errors.New("bill repository: db handle is nil")
	}
	return nil
}

func (r *billRepo) Create(ctx context.Context, b *model.Bill) error {
	if err := r.require(); err != nil {
		return err
	}
	if b == nil || b.ID == "" {
		return errors.New("bill repository: 空记录或空账单号（它会被抄进 payments.bill_id，空串无法引用也无法排除）")
	}
	if strings.TrimSpace(b.QuoteID) == "" {
		return errors.New("bill repository: 空 quote_id 不是合法身份（它决定这张应收挂在哪个报价链上）")
	}
	if strings.TrimSpace(b.QuoteRowID) == "" {
		return errors.New("bill repository: 空 quote_row_id 不是合法身份（幂等键为空时「派没派生过」这个问题没有答案）")
	}
	if strings.TrimSpace(b.OpportunityID) == "" {
		return errors.New("bill repository: 空 opportunity_id 不是合法身份（下游按它聚合回款，空串会让这张应收在报表里凭空消失）")
	}
	// 状态值域守在这里，理由见 bill_test.go 的 TestBillRepository_StatusMustBeInTheDomain：
	// 库里没有 CHECK 约束，一格拼错的状态既进不了 T-P7-03 的逾期扫描、也出不了
	// 这里的 CAS 跃迁，两张脸都是静默的。
	if !model.BillStatusKnown(b.Status) {
		return errors.New("bill repository: status 不在账单值域内（它既扫不到也改不动，两种坏法都静默）")
	}
	if err := r.db.WithContext(ctx).Create(b).Error; err != nil {
		if isBillAlreadyDerived(err) {
			return ErrBillAlreadyDerived
		}
		return err
	}
	return nil
}

// isBillAlreadyDerived 判定"撞的是派生幂等键还是别的约束"。
//
// SQLSTATE 与索引名两个条件都要命中，与 quote 侧 isQuoteVersionConflict 同一形状。
// 不认 gorm.ErrDuplicatedKey：本仓连接没开 TranslateError，这里拿到的是底层
// *pgconn.PgError（错误串含 23505 与索引名）。
func isBillAlreadyDerived(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, billQuoteRowConstraint)
}

// UpdateStatus 一条 CAS 语句完成跃迁，返回三种结果之一（成功 / 起点过期 / 行不存在）。
//
// 写集合是 map 形式的白名单，只有两列：id / quote_id / quote_row_id / opportunity_id
// 是这张应收的身份与来路，currency 与 amount 是承诺内容，created_at 是历史——
// 全都不在这条路径上。updated_at 由这层落，不取调用方那份：它是"多久没动过"的唯一凭据，
// 而 T-P7-03 的账龄读的正是它。
//
// 本层**不查跃迁表**（model.BillStatusCanTransit）：那是一张没有并发语义的静态表，
// 真正的裁决者是这里的 WHERE。把合法性检查放两处的后果与报价侧同源——
// 两边各改一次就出现"表里允许、库里落不下去"的第三种答案。跃迁合法性由 service 把关，
// 那里读得到完整上下文（钱到没到、谁点的）。
func (r *billRepo) UpdateStatus(ctx context.Context, id, from, to string) error {
	if err := r.require(); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("bill repository: 空账单号不是改写目标")
	}
	// 两侧都取值域内的词：`from` 是 WHERE 的条件，越域的值永远命不中，
	// 报出来的会是"起点过期"或"不存在"——两个都是假供词，会把调用方支去重读一条根本没这格状态的行。
	// `to` 是写进库里的那个词，越域的后果见上面 Create 的同一条注释。
	if !model.BillStatusKnown(from) {
		return errors.New("bill repository: 跃迁起点不在账单值域内（库里没有这格状态，命不中任何行）")
	}
	if !model.BillStatusKnown(to) {
		return errors.New("bill repository: 跃迁目标不在账单值域内")
	}
	if from == to {
		return errors.New("bill repository: 跃迁起点与目标相同（零位移跃迁不是跃迁）")
	}
	res := r.db.WithContext(ctx).Model(&model.Bill{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{
			"status":     to,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}
	// 0 行有两种来路：探测不参与并发正确性（决定权在 UPDATE 那一条），
	// 探测与改写之间行被删了也只是错个标签，不会写进任何东西。
	exists, err := r.exists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return ErrBillStatusConflict
	}
	return ErrBillNotFound
}

func (r *billRepo) exists(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.Bill{}).
		Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *billRepo) GetByID(ctx context.Context, id string) (*model.Bill, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("bill repository: 空账单号不是读取条件（它会退化成「读第一行」）")
	}
	// 每次都用新的零值 struct：复用已填充的 dest 会把旧字段并进 WHERE（本仓已踩过一次）。
	var row model.Bill
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return billOrNil(&row, err)
}

func (r *billRepo) GetByQuoteRowID(ctx context.Context, quoteRowID string) (*model.Bill, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteRowID) == "" {
		return nil, errors.New("bill repository: 空 quote_row_id 不是读取条件")
	}
	var row model.Bill
	err := r.db.WithContext(ctx).First(&row, "quote_row_id = ?", quoteRowID).Error
	return billOrNil(&row, err)
}

// ListByQuoteID 这张报价单开过的全部应收，按派生早晚（created_at，打平时按行号）升序。
//
// 空号先拒：它若进到这里就变成 `WHERE quote_id = ”` 的一次真实查询，而 quote_id 这一列
// 的唯一守卫住在 Create —— 放行等于给"没有条件的读"留了一条后门（同一判据见
// payment 仓储的 SumSettledByBill）。
//
// 零命中回空切片而不是 error：对账读的是"这张报价开过几张"，一张没开是合法答案。
func (r *billRepo) ListByQuoteID(ctx context.Context, quoteID string) ([]*model.Bill, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteID) == "" {
		return nil, errors.New("bill repository: 空 quote_id 不是读取条件（它会退化成「读全表」）")
	}
	var rows []*model.Bill
	if err := r.db.WithContext(ctx).
		Where("quote_id = ?", quoteID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// billOrNil 把"没有这一行"读成 (nil, nil)，其余错误原样上抛。
//
// 二者必须分开：调用方对 (nil, nil) 的动作是"那就派生一张"，对 error 的动作是"别写"。
// 把库故障读成"还没有账单"，一次数据库抖动就能让同一版报价长出两张应收。
func billOrNil(row *model.Bill, err error) (*model.Bill, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}
