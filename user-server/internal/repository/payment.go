// payment.go 回款仓储（T-P7-02 / N-6）。
//
// payments 的读写面只有四条写路径与三条读路径，而它们合起来就是结清判据的全部操作数：
//   - Create：记一笔到账（幂等键 = channel_ref 上的唯一索引）；
//   - MarkReversed：把那一笔标掉（CAS，写集合只有 status + updated_at）；
//   - SumSettledByBill：这张应收一共收到多少（SQL 侧求和）；
//   - GetByID / GetByChannelRef / ListByBillID：三条读。
//
// 与 bills 同一取向的两件事**刻意不做**：
//   - 没有内存版底座：AC②"同 channel_ref 不重复入账"的实现位置是库上那条唯一索引，
//     有影子实现时这条 AC 永远测不出来；
//   - 没有 Delete、没有改金额/改账单号的方法：回款行是资金凭据，
//     记错了一笔的处置方式是"再记一笔反向的"（这里是 MarkReversed），不是抹掉。
//     抹掉之后"这张应收到底收过多少钱"就永远答不上了。
package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

// ErrPaymentAlreadyRecorded 这个渠道流水号已经入过账了（AC② 的重入通路）。
//
// 单独成 sentinel 的理由与 ErrBillAlreadyDerived 同族：调用方对它的反应是
// "回读那一行并比对内容"（同笔重投 ⇒ 幂等复用；内容不同 ⇒ 渠道号被复用 ⇒ 拒），
// 而对别的 23505（主键、非本索引）的反应是"这次写入本身坏了，别再重试"。
var ErrPaymentAlreadyRecorded = errors.New("payment: 该渠道流水号已入账，本次没有写入第二笔")

// ErrPaymentNotConfirmed 手里那个"它还算数"已过期，本次冲销没有落库。
var ErrPaymentNotConfirmed = errors.New("payment: 该回款行不是 confirmed，冲销没有落库")

// ErrPaymentNotFound 行不存在（读侧回 (nil, nil)，写侧回本 sentinel）。
var ErrPaymentNotFound = errors.New("payment: 回款行不存在")

// paymentChannelRefConstraint 与 model.Payment 上那处 uniqueIndex 标签同源
// （用例 TestPaymentRepository_ConstraintNameIsTheOneOnTheModel 逐字比）。
const paymentChannelRefConstraint = "uq_payments_channel_ref"

// paymentChannelRefMaxLen 与 model.Payment.ChannelRef 的 varchar(128) 同宽。
//
// 为什么在这里判而不是交给库：超限的一次写入会被 PG 截断或报错（取决于版本与列型），
// 而报错的方向是整条回调 500 ⇒ 渠道重试循环。判在边界上才能给出"是这一格太长"这句话。
// 刻意**不校验字符集**：这一格只是幂等键，渠道给什么形状的单号都有，
// 自造白名单等于替所有平台赌一次它们的编号格式（回显进日志时由调用方用 %q 兜住）。
const paymentChannelRefMaxLen = 128

// PaymentRepository payments 读写接口。方法集由 payment_test.go 的
// TestPaymentRepository_MethodSetIsExactlyTheDocumentedSix 逐字钉住。
type PaymentRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Create 记一笔回款：来路三格（账单号 / 平台 / 外部订单号）任一为空即拒 ——
	// 追不到来源的资金记录在对账时等同于没有；金额必须过 model.PaymentAmountInRange；
	// status 必须落在 model.PaymentStatuses 内（库里没有 CHECK，这是值域唯一的落点）。
	// 撞 uq_payments_channel_ref ⇒ ErrPaymentAlreadyRecorded。
	Create(ctx context.Context, p *model.Payment) error

	// MarkReversed 冲销那一笔：`WHERE id = ? AND status = 'confirmed'` 命中才生效，
	// 写集合只有 status + updated_at。已是 reversed ⇒ ErrPaymentNotConfirmed。
	MarkReversed(ctx context.Context, id string) error

	// SumSettledByBill 这张应收一共收到多少：**只加计入结清的那些状态**
	// （条件由 model.PaymentStatusesCounted 生成，本层不另写字面值）。
	// 一张没有 ⇒ 0 且无错（"还没收到钱"是合法答案）。
	SumSettledByBill(ctx context.Context, billID string) (float64, error)

	// GetByID 按行号读取；不存在返回 (nil, nil)。
	GetByID(ctx context.Context, id string) (*model.Payment, error)

	// GetByChannelRef 按幂等键读取——撞到"已入账"之后的比对走这条路；不存在返回 (nil, nil)。
	GetByChannelRef(ctx context.Context, channelRef string) (*model.Payment, error)

	// ListByBillID 某一应收名下的全部回款行，按 paid_at 升序（对账读的是流水顺序）。
	ListByBillID(ctx context.Context, billID string) ([]*model.Payment, error)
}

type paymentRepo struct {
	db *gorm.DB
}

// NewPaymentRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）。
//
// 与 bills 同一判据：不产出读全局句柄的 NewPaymentRepository()，等注入点落地再一起给。
func NewPaymentRepositoryWithDB(db *gorm.DB) PaymentRepository {
	return &paymentRepo{db: db}
}

func (r *paymentRepo) Available() bool { return r != nil && r.db != nil }

func (r *paymentRepo) require() error {
	if !r.Available() {
		return errors.New("payment repository: db handle is nil")
	}
	return nil
}

func (r *paymentRepo) Create(ctx context.Context, p *model.Payment) error {
	if err := r.require(); err != nil {
		return err
	}
	if p == nil || p.ID == "" {
		return errors.New("payment repository: 空记录或空回款行号（它会进日志与对账导出，空串指不回任何一笔钱）")
	}
	if strings.TrimSpace(p.BillID) == "" {
		return errors.New("payment repository: 空 bill_id 不是合法身份（结清判据按它求和，空串这笔钱永远算不到任何账单上）")
	}
	ref := strings.TrimSpace(p.ChannelRef)
	if ref == "" {
		return errors.New("payment repository: 空 channel_ref 不是合法身份（它是本表唯一的幂等键，空键等于每笔重投都能插进来）")
	}
	if len(ref) > paymentChannelRefMaxLen {
		return errors.New("payment repository: channel_ref 超上限 " +
			strconv.Itoa(paymentChannelRefMaxLen) + " 字符（外部可控文本必须有界）")
	}
	if !model.PaymentAmountInRange(p.Amount) {
		return errors.New("payment repository: 金额不是「正数且不超过 numeric(14,2) 上界」（方向由 status 表达，符号不许进金额列）")
	}
	if strings.TrimSpace(p.Currency) == "" {
		return errors.New("payment repository: 空币种：不带币种的钱与账单上的那个数不可比")
	}
	if strings.TrimSpace(p.Platform) == "" {
		return errors.New("payment repository: 空平台：资金凭据不留来路，渠道问起这笔钱时无从答起")
	}
	if strings.TrimSpace(p.OrderID) == "" {
		return errors.New("payment repository: 空外部订单号：同上，这一格是「哪条回调带来的这笔钱」的唯一线索")
	}
	if !model.PaymentStatusKnown(p.Status) {
		return errors.New("payment repository: status 不在回款值域内（库里没有 CHECK，拼错的一格会静默改变结清金额）")
	}
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		if isPaymentAlreadyRecorded(err) {
			return ErrPaymentAlreadyRecorded
		}
		return err
	}
	return nil
}

// isPaymentAlreadyDerived 判定"撞的是幂等键还是别的约束"：SQLSTATE 与索引名都要命中。
// 与 bills 侧同一形状，理由也同 —— 认错方向的坏法是"把一次坏写入吞成一次成功复用"。
func isPaymentAlreadyRecorded(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, paymentChannelRefConstraint)
}

// MarkReversed 一条 CAS 语句完成冲销（三种结果：成功 / 起点不是 confirmed / 行不存在）。
//
// 写集合是 map 白名单，只有两列：id / bill_id / amount / currency / paid_at /
// channel_ref / platform / order_id 全是这一笔钱的身份与来路，不在可写集合里。
// 冲销**不许**改 paid_at：那是"钱哪天到过账"的历史事实，被冲掉不等于它没发生过。
func (r *paymentRepo) MarkReversed(ctx context.Context, id string) error {
	if err := r.require(); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("payment repository: 空回款行号不是改写目标")
	}
	res := r.db.WithContext(ctx).Model(&model.Payment{}).
		Where("id = ? AND status = ?", id, model.PaymentStatusConfirmed).
		Updates(map[string]any{
			"status":     model.PaymentStatusReversed,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}
	exists, err := r.exists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return ErrPaymentNotConfirmed
	}
	return ErrPaymentNotFound
}

func (r *paymentRepo) exists(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.Payment{}).
		Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// SumSettledByBill 求和发生在 SQL 侧，且**只加 model.PaymentStatusesCounted 里的那些状态**。
//
// 两个判据合在这一条方法上，各自挡一种坏法：
//   - 逐行搬到 Go 里累加：float64 的舍入轨迹与列上的 numeric(14,2) 不同源，
//     结清判据（Σ ≥ amount）会在刚好相等的那一单上翻脸；
//   - 状态条件写死字面量：将来值域里多一格"也算到账"时，SQL 与内存复算两边各改一边，
//     差出来的那一格钱不会报错，只会让对账不上。所以条件从那个切片生成。
//
// ROUND(...,2) 是列量程而非取巧：numeric(14,2) 相加本来就只有两位，
// 显式写出来是为了让"这一列的量程"在本文件里也看得见。
// COALESCE 让"一张没有"回 0：那是合法答案而不是错误（"还没收到钱"要能答出来）。
func (r *paymentRepo) SumSettledByBill(ctx context.Context, billID string) (float64, error) {
	if err := r.require(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(billID) == "" {
		return 0, errors.New("payment repository: 空 bill_id 不是求和条件（它会退化成全表求和）")
	}
	statuses := model.PaymentStatusesCounted
	if len(statuses) == 0 {
		// 不是"不可能"：这一格与 model 的判据同源，值域被改空是一次真实的模型层改动，
		// 而 `IN ()` 是 SQL 语法错。报一句人话比让 PG 报语法错便宜。
		return 0, errors.New("payment repository: 计入结清的状态集为空（model.PaymentStatusesCounted 被改空了？）")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, 0, len(statuses)+1)
	args = append(args, strings.TrimSpace(billID))
	for _, s := range statuses {
		args = append(args, s)
	}
	var sum float64
	sql := "SELECT COALESCE(ROUND(SUM(amount), 2), 0) FROM payments WHERE bill_id = ? AND status IN (" + placeholders + ")"
	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&sum).Error; err != nil {
		return 0, err
	}
	return sum, nil
}

func (r *paymentRepo) GetByID(ctx context.Context, id string) (*model.Payment, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("payment repository: 空回款行号不是读取条件（它会退化成「读第一行」）")
	}
	var row model.Payment
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	return paymentOrNil(&row, err)
}

func (r *paymentRepo) GetByChannelRef(ctx context.Context, channelRef string) (*model.Payment, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(channelRef)
	if ref == "" {
		return nil, errors.New("payment repository: 空 channel_ref 不是读取条件")
	}
	var row model.Payment
	err := r.db.WithContext(ctx).First(&row, "channel_ref = ?", ref).Error
	return paymentOrNil(&row, err)
}

func (r *paymentRepo) ListByBillID(ctx context.Context, billID string) ([]*model.Payment, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(billID) == "" {
		return nil, errors.New("payment repository: 空 bill_id 不是列表条件（它会退化成全表扫，而这是凭证表）")
	}
	var rows []*model.Payment
	if err := r.db.WithContext(ctx).
		Where("bill_id = ?", strings.TrimSpace(billID)).
		Order("paid_at ASC").Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// paymentOrNil 把"没有这一行"读成 (nil, nil)，其余错误原样上抛（同 billOrNil 判据）。
func paymentOrNil(row *model.Payment, err error) (*model.Payment, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}
