// payment.go T-P7-02：回款入账与结清层（渠道说"这笔钱到了" → payments 一行 → bills.status 跟着钱动）。
//
// 一句话职责：把外部渠道断言的一次到账记成一行凭据，并当场把那张应收的状态
// 折算成"Σ 计入结清 vs 应收金额"的结果。两件事在一次调用里前后发生。
//
// 为什么结清住在这一层而不是账单那一层：bills 刻意不存"已收"那一格（判据见
// model/bill.go），因为一次存两份数字就有了第二个事实源；已收只能是
// `SELECT SUM(amount) FROM payments WHERE bill_id = ? AND status ∈ 计入结清` 的结果。
// 于是 bills.status 从 T-P7-02 起是**那个和的函数**，而本层是唯一会算它的人 ——
// 这也是 model/bill.go 那三条回退边（paid→partial / paid→open / partial→open）
// 在 T-P7-02 才被打开的原因：只有会算的人才能把算错的方向修正回来。
//
// 三条判据各挡一种坏法：
//
//	① 幂等键（AC②）：同 channel_ref 只入账一次。这一条物理上住在库上的唯一索引，
//	   本层做的是"撞键之后回读那一行并比对内容"——内容不同就不是同一笔钱，
//	   此时复用的后果是把第二笔真钱吞掉，所以拒。
//	② 来路：账单必须存在且**没作废**。往 voided 的应收上记账，等于把一张已收口的
//	   凭据重新拽回催收队列（而 voided 在跃迁表里没有出边，那一刻会直接卡死）。
//	③ 币种：与账单不同的币种不进同一个和（混币的合计没有定义）。
//
// 两个刻意的方向选择，都写在用例名上：
//   - **超收不拒**（TestPaymentServiceOverpaymentIsVisibleNotRejected）：渠道已经收到的钱
//     在我方账上没有记录，是财务上最贵的一种错（少记）；收下并标出 oversettled，
//     两边一比就看得见。于是 outstanding 恒 >=0，欠额那一格不会被读成负数。
//   - **跃迁失败不回滚回款**（TestPaymentServiceAvailabilityAndFailures）：钱记下了就是记下了，
//     回滚它等于少记一笔；报 ErrPaymentStatusStuck 并说明"重放同一笔回调可补上"，
//     因为整条腿是幂等的 —— 这一格的可重放性是上面那条方向选择的**代价**，不是巧合。
//
// 不做的事，逐条有归属：不推送催收（T-P7-03）、不在结清时改商机（T-P7-04）、
// 不做部分冲销（渠道按整笔冲，金额恒正的判据见 model/payment.go）、
// 不开"人工补记一笔回款"的 HTTP 口（钱只在外部电商到账，X8/D-4；没有数据源的写口就是没有判据的写口）。

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var (
	// ErrPaymentServiceUnavailable 依赖缺件（部署的事）。
	ErrPaymentServiceUnavailable = errors.New("payment: 回款服务未装配（回款存储与账单存储缺一即不能入账）")
	// ErrPaymentInputInvalid 入参不合法（调用方的事：改载荷）。
	ErrPaymentInputInvalid = errors.New("payment: 回款入参不合法")
	// ErrPaymentBillNotFound 那张应收不存在（拿错了账单号）。
	ErrPaymentBillNotFound = errors.New("payment: 目标账单不存在")
	// ErrPaymentBillVoided 那张应收已经作废，不再收钱。
	ErrPaymentBillVoided = errors.New("payment: 账单已作废，这笔回款没有入账")
	// ErrPaymentCurrencyMismatch 这笔钱的币种与账单不同：不进同一个和。
	ErrPaymentCurrencyMismatch = errors.New("payment: 回款币种与账单币种不符，这笔回款没有入账")
	// ErrPaymentRefReuse 同一个渠道流水号被用来记**另一笔**钱（内容不符）。
	ErrPaymentRefReuse = errors.New("payment: 该渠道流水号已记过一笔内容不同的回款，本次没有入账")
	// ErrPaymentAlreadyReversed 这一笔已经被冲掉，不接受一次普通重投把它复活。
	ErrPaymentAlreadyReversed = errors.New("payment: 该回款已被冲销，重投不会使它重新算数")
	// ErrPaymentNothingToReverse 载荷说"冲掉那一笔"，而我方从没记过它。
	ErrPaymentNothingToReverse = errors.New("payment: 要冲销的回款行不存在，本次没有入账")
	// ErrPaymentStatusStuck 钱已经落库，而账单状态没折算到位。
	// 单独成 sentinel 的理由与账单侧的同类同一条：处置动作是"重放这一笔"，不是"改载荷再来"。
	ErrPaymentStatusStuck = errors.New("payment: 回款已入账，账单状态没能折算")
)

// paymentStore 入账腿对回款存储的全部依赖（方法集合由反射用例钉住，见
// TestPaymentServicePortSurfacesAreNarrow —— 用注释守这件事的先例在本仓不止一次失效）。
//
// **没有**改金额 / 改账单号 / Delete：回款行是资金凭据，记错一笔的处置方式是
// 再记一笔反向的（MarkReversed 就是那一条），不是抹掉 —— 抹掉之后
// "这张应收到底收过多少钱"永远答不上来（同 repository/payment.go 文件头）。
type paymentStore interface {
	Available() bool
	Create(ctx context.Context, p *model.Payment) error
	GetByChannelRef(ctx context.Context, channelRef string) (*model.Payment, error)
	ListByBillID(ctx context.Context, billID string) ([]*model.Payment, error)
	MarkReversed(ctx context.Context, id string) error
	SumSettledByBill(ctx context.Context, billID string) (float64, error)
}

// settlementBillStore 入账腿对账单存储的全部依赖。
//
// **没有** Create / DeriveFromQuote：入账这条腿不许凭空造一张应收 —— 否则
// "回款冲的是哪张账单"会变成它自己的决定。
// ListByQuoteID 只有一格批量读，且必须带键（对账读口，判据见 repository/bill.go 同一条）。
type settlementBillStore interface {
	Available() bool
	GetByID(ctx context.Context, id string) (*model.Bill, error)
	ListByQuoteID(ctx context.Context, quoteID string) ([]*model.Bill, error)
	UpdateStatus(ctx context.Context, id, from, to string) error
}

// RecordPaymentInput 一次回款通知的全部输入。
//
// 字段集合是白名单（用例 TestRecordPaymentInputIsWhitelistShaped 逐格试补集）：
// **没有** quote_id / opportunity_id 任何一格 —— 账单的来路只有 bill_id 一条，
// 从调用方再递一份就是第二个事实源（副本会漂，判据见 model/payment.go 那两个字段）。
// 也**没有**账单金额：那一格是账单自己的事，本层只往里加钱。
type RecordPaymentInput struct {
	BillID     string     // 必填：这张钱冲的是哪张应收（bills.id）
	ChannelRef string     // 必填：渠道流水号，本层的幂等键（AC②）
	Amount     float64    // 必填：>0 且不超过 numeric(14,2) 上界（方向由 Status 表达）
	Currency   string     // 可空 ⇒ 随账单币种；非空则必须与账单一致
	PaidAt     *time.Time // 可空 ⇒ 填接收时刻，并在视图上承认是填的（FilledPaidAt）
	Platform   string     // 必填：取自回调路径上的 :platform（载荷自述不作数）
	OrderID    string     // 必填：触发这笔回款的外部订单号
	Status     string     // 可空 ⇒ confirmed；只接受 confirmed / reversed 两个词
}

// PaymentView 一行回款凭据的对外视图。
//
// FilledPaidAt 不是装饰：paid_at 那一格是"哪天到账"，运营按它排账龄。
// 载荷没给而系统填了接收时刻，调用方就必须能看出这一格不是渠道说的。
type PaymentView struct {
	ID           string    `json:"id"`
	BillID       string    `json:"bill_id"`
	Amount       float64   `json:"amount"`
	Currency     string    `json:"currency"`
	PaidAt       time.Time `json:"paid_at"`
	ChannelRef   string    `json:"channel_ref"`
	Status       string    `json:"status"`
	Platform     string    `json:"platform"`
	OrderID      string    `json:"order_id"`
	FilledPaidAt bool      `json:"filled_paid_at,omitempty"`
}

// SettlementView 一张应收在**本次结算后**的三格数。
//
// Amount / Settled / Outstanding 一起给：只给"还欠多少"的话，对账两边各自
// 少一个可比的数（差一个数就能吵一场"你说的欠是哪套口径"）。
type SettlementView struct {
	BillID      string  `json:"bill_id"`
	Status      string  `json:"status"`
	Amount      float64 `json:"amount"`
	Settled     float64 `json:"settled"`
	Outstanding float64 `json:"outstanding"`
	// Oversettled 收的比主张的多。方向选择见文件头：收下并标出，不拒。
	Oversettled bool `json:"oversettled"`
	// Transited 本次调用有没有真的推动账单状态。幂等重投通常给 false，
	// 调用方据此把"重复通知"与"这一笔把账单结掉了"分开。
	Transited bool `json:"transited"`
}

// PaymentReceipt 一次入账的完整结论：那一行 + 那张单的三格数。
type PaymentReceipt struct {
	Payment    PaymentView    `json:"payment"`
	Reused     bool           `json:"reused"`
	Settlement SettlementView `json:"settlement"`
}

// BillStatementView 账单的对账读视图（读侧唯一出口，无 Reused：那不是账单的属性）。
type BillStatementView struct {
	BillID        string        `json:"bill_id"`
	QuoteID       string        `json:"quote_id"`
	QuoteRowID    string        `json:"quote_row_id"`
	OpportunityID string        `json:"opportunity_id"`
	Status        string        `json:"status"`
	Amount        float64       `json:"amount"`
	Currency      string        `json:"currency"`
	DueAt         *time.Time    `json:"due_at,omitempty"`
	Settled       float64       `json:"settled"`
	Outstanding   float64       `json:"outstanding"`
	Oversettled   bool          `json:"oversettled"`
	Payments      []PaymentView `json:"payments"`
}

// PaymentService 回款域的入账与结算层。依赖全走注入，本层不自己去拿全局 DB。
type PaymentService struct {
	payments paymentStore
	bills    settlementBillStore
	now      func() time.Time
}

// NewPaymentService 构造。缺件时构造照旧成功，由 Available / 各方法报出来。
func NewPaymentService(payments paymentStore, bills settlementBillStore) *PaymentService {
	return &PaymentService{payments: payments, bills: bills, now: time.Now}
}

// SetClock 注入时钟（回款行号里的纳秒与"载荷没给 paid_at"时的填充值共用它）。
func (s *PaymentService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// Available 报告能不能入账。
func (s *PaymentService) Available() bool {
	return s != nil && s.payments != nil && s.payments.Available() && s.bills != nil && s.bills.Available()
}

// paymentCentEpsilon 一分的一半。
//
// 结清比较不许用裸 ==：两边走的是不同的路（一个是 numeric(14,2) 里 SUM 出来的，
// 一个是账单头那一格），任何一分的缺口都不该被读成"收清了"，
// 也不该把"还差一分"读成"已结清"。取半个最小单位是唯一不会误伤的方向。
const paymentCentEpsilon = 0.005

// RecordPayment 记下一笔回款，并把那张应收折算到位。
//
// 顺序与账单派生腿同一取向：所有只读判据（来路、值域、币种、幂等比对）
// 都在第一次写之前跑完；写下去的是回款行，最后才是账单状态。
// 于是三种失败窗口的方向分别是：
//   - 只读判据没过 ⇒ 库里两边都没动（可以修好载荷再来）；
//   - 回款行写成了而账单没折算 ⇒ 报 ErrPaymentStatusStuck 并说明可重放
//     （整条腿幂等，重投同一笔会跳过写入、只做折算）；
//   - 反过来先改账单再记钱，坏的一侧是"账单说收清了而库里没有那一笔" ——
//     那是要人去删的数据，而 payments 没有删除口。
func (s *PaymentService) RecordPayment(ctx context.Context, in RecordPaymentInput) (*PaymentReceipt, error) {
	if !s.Available() {
		return nil, ErrPaymentServiceUnavailable
	}
	billID := strings.TrimSpace(in.BillID)
	ref := strings.TrimSpace(in.ChannelRef)
	platform := strings.TrimSpace(in.Platform)
	orderID := strings.TrimSpace(in.OrderID)
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if billID == "" {
		return nil, fmt.Errorf("%w: bill_id 为空（不知道冲的是哪张应收的钱没法进任何一张单的和对账）", ErrPaymentInputInvalid)
	}
	if ref == "" {
		return nil, fmt.Errorf("%w: channel_ref 为空（它是本层唯一的幂等键，空键等于每笔重投都能记一次）", ErrPaymentInputInvalid)
	}
	if platform == "" || orderID == "" {
		return nil, fmt.Errorf("%w: platform / order_id 为空（资金凭据不留来路，渠道问起这笔钱时无从答起）", ErrPaymentInputInvalid)
	}
	if !model.PaymentAmountInRange(in.Amount) {
		return nil, fmt.Errorf("%w: 金额 %v 不是「正数且不超过 numeric(14,2) 上界」（方向由 status 表达，符号不许进金额列）",
			ErrPaymentInputInvalid, in.Amount)
	}
	statusWord := strings.TrimSpace(in.Status)
	if statusWord == "" {
		statusWord = model.PaymentStatusConfirmed
	}
	if !model.PaymentStatusKnown(statusWord) {
		return nil, fmt.Errorf("%w: 状态 %q 不在回款值域 %v 内（越域的词会被求和侧读成「不计入」或「计入」，两种都是静默的）",
			ErrPaymentInputInvalid, statusWord, model.PaymentStatuses)
	}
	reversal := statusWord == model.PaymentStatusReversed

	bill, err := s.bills.GetByID(ctx, billID)
	if err != nil {
		return nil, fmt.Errorf("payment: 读账单 %s 失败：%w", billID, err)
	}
	if bill == nil {
		return nil, fmt.Errorf("%w: %s", ErrPaymentBillNotFound, billID)
	}
	if bill.Status == model.BillStatusVoided {
		return nil, fmt.Errorf("%w: %s（作废的凭据不再收钱，请先人工核对这笔钱该冲哪张）", ErrPaymentBillVoided, billID)
	}
	if currency == "" {
		currency = bill.Currency
	} else if currency != bill.Currency {
		return nil, fmt.Errorf("%w: 这笔是 %s 而账单 %s 主张 %s（混币的合计没有定义）",
			ErrPaymentCurrencyMismatch, currency, billID, bill.Currency)
	}

	// 幂等通路：先按渠道流水号读。撞上了就不是"再记一笔"，而是"这一笔的又一次通知"。
	existing, err := s.payments.GetByChannelRef(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("payment: 按 channel_ref 回读失败：%w", err)
	}
	if existing != nil {
		return s.settleKnown(ctx, existing, bill, ref, in.Amount, currency, reversal)
	}
	if reversal {
		// 不"顺手补一行 reversed"：那会让库里出现一笔从没到账过的钱的记录，
		// 而 reversed 的全部含义是"这一笔曾经算数过"。
		return nil, fmt.Errorf("%w: channel_ref=%s", ErrPaymentNothingToReverse, ref)
	}

	now := s.now()
	row := &model.Payment{
		ID: newPaymentKey(now, nextPaymentSeq()), BillID: billID,
		Amount: money2(in.Amount), Currency: currency, PaidAt: payTimeOf(in.PaidAt, now),
		ChannelRef: ref, Status: model.PaymentStatusConfirmed, Platform: platform, OrderID: orderID,
		CreatedAt: now, UpdatedAt: now,
	}
	filled := in.PaidAt == nil
	if err := s.payments.Create(ctx, row); err != nil {
		if !errors.Is(err, repository.ErrPaymentAlreadyRecorded) {
			return nil, fmt.Errorf("payment: 记录回款失败（账单 %s 未做任何折算）：%w", billID, err)
		}
		// 并发窗口：另一路刚把这一笔记进去了（唯一索引就是为这一刻建的）。
		again, gerr := s.payments.GetByChannelRef(ctx, ref)
		if gerr != nil {
			return nil, fmt.Errorf("payment: 撞到\"已入账\"之后回读又失败：%w", gerr)
		}
		if again == nil {
			return nil, fmt.Errorf("payment: 仓储报\"已入账\"而按 channel_ref=%s 读不到那一行（约束名与索引不同源？须人工核对）", ref)
		}
		return s.settleKnown(ctx, again, bill, ref, in.Amount, currency, false)
	}

	view := paymentViewOf(row, filled)
	settled, err := s.applySettlement(ctx, bill)
	if err != nil {
		// 钱已经落库：这一条必须把这件事说清楚，并指出重放可修（判据见文件头）。
		return nil, fmt.Errorf("%w（回款 %s 已入账，账单 %s 的折算没做成；重放同一笔回调可补上）：%v",
			ErrPaymentStatusStuck, view.ID, billID, err)
	}
	return &PaymentReceipt{Payment: view, Reused: false, Settlement: settled}, nil
}

// settleKnown 幂等通路上的三种结论：复用（无写入）、冲销（只改 status）、拒（内容不符）。
//
// 比对的是"这笔钱是不是同一笔"的三格：账单号、金额、币种。
// 少比任何一格的后果都是吞掉第二笔真钱（AC② 的反面）。
func (s *PaymentService) settleKnown(ctx context.Context, existing *model.Payment, bill *model.Bill,
	ref string, amount float64, currency string, reversal bool) (*PaymentReceipt, error) {
	if existing.BillID != bill.ID || money2(existing.Amount) != money2(amount) || existing.Currency != currency {
		return nil, fmt.Errorf("%w: 已记的是账单 %s 的 %v %s，本次要记的是账单 %s 的 %v %s",
			ErrPaymentRefReuse, existing.BillID, existing.Amount, existing.Currency,
			bill.ID, amount, currency)
	}
	if reversal {
		// 已是 reversed 的这一支是**幂等重投**：同一笔冲销通知再来一次，不再写任何东西。
		if existing.Status == model.PaymentStatusReversed {
			return s.receiptOf(ctx, existing, bill)
		}
		if err := s.payments.MarkReversed(ctx, existing.ID); err != nil {
			return nil, fmt.Errorf("payment: 冲销回款 %s 失败：%w", existing.ID, err)
		}
		// 冲销之后**那张单**要重新折算：账单可能从 paid 退回 partial / open。
		return s.receiptOfReversed(ctx, existing, bill)
	}
	if existing.Status == model.PaymentStatusReversed {
		// 已冲销的行不许被一次普通重投复活（复活的那一格钱在渠道那边并不存在）。
		return nil, fmt.Errorf("%w: channel_ref=%s", ErrPaymentAlreadyReversed, ref)
	}
	return s.receiptOf(ctx, existing, bill)
}

// receiptOfReversed 冲销之后的结论：视图读的是**库里那一行现在的样子**（重新按流水号读一次），
// 而不是把内存里那份旧行的 status 改掉就交出去 —— 后者在 CAS 与读之间隔着一次写，
// 视图说了库没说的话。
func (s *PaymentService) receiptOfReversed(ctx context.Context, existing *model.Payment, bill *model.Bill) (*PaymentReceipt, error) {
	fresh, err := s.payments.GetByChannelRef(ctx, existing.ChannelRef)
	if err != nil {
		return nil, fmt.Errorf("payment: 冲销后回读 %s 失败：%w", existing.ChannelRef, err)
	}
	if fresh == nil {
		// 仓储说冲成功了而按同一把键读不回 —— 两种解释都是坏的，且都不能静默（同派生腿那句）。
		return nil, fmt.Errorf("payment: 冲销返回成功而按 channel_ref=%s 读不到那一行（须人工核对）", existing.ChannelRef)
	}
	return s.receiptOf(ctx, fresh, bill)
}

// receiptOf 无新写入的一支（复用、重复冲销都走这里）：那一行原样出视图，
// 并把那张单重新折算一遍（这一步顺带修好"上一次折算失败"留下的缺口）。
func (s *PaymentService) receiptOf(ctx context.Context, row *model.Payment, bill *model.Bill) (*PaymentReceipt, error) {
	settled, err := s.applySettlement(ctx, bill)
	if err != nil {
		return nil, fmt.Errorf("%w（回款 %s 早已入账，账单 %s 的折算没做成；重放这一笔通知可补上）：%v",
			ErrPaymentStatusStuck, row.ID, bill.ID, err)
	}
	return &PaymentReceipt{Payment: paymentViewOf(row, false), Reused: true, Settlement: settled}, nil
}

// applySettlement 把账单状态折算成"Σ 计入结清 vs 应收金额"的结果。
//
// 求和走仓储的 SQL 侧（SumSettledByBill），本层不逐行搬进 Go 里加：
// 两处各有一份加法，就对账而言有两个答案。
func (s *PaymentService) applySettlement(ctx context.Context, bill *model.Bill) (SettlementView, error) {
	settled, err := s.payments.SumSettledByBill(ctx, bill.ID)
	if err != nil {
		return SettlementView{}, err
	}
	view := settlementOf(bill, money2(settled))
	if view.Status == bill.Status {
		return view, nil
	}
	if !model.BillStatusCanTransit(bill.Status, view.Status) {
		// 到这一步说明"库里那一行的状态"与"名下的钱"对不上，而跃迁表里没有那条边 ——
		// 猜哪一侧都是假事实，所以报一条需要人看的话，且**不**写库。
		return SettlementView{}, fmt.Errorf("账单 %s 当前 %q，按名下回款合计 %v/%v 该折算成 %q，而跃迁表里没有这条边",
			bill.ID, bill.Status, view.Settled, bill.Amount, view.Status)
	}
	if err := s.bills.UpdateStatus(ctx, bill.ID, bill.Status, view.Status); err != nil {
		return SettlementView{}, err
	}
	view.Transited = true
	return view, nil
}

// settlementOf 三格数。**唯一**做这笔算术的地方（视图里多一处算术，对账就多一个答案）。
func settlementOf(bill *model.Bill, settled float64) SettlementView {
	amount := money2(bill.Amount)
	outstanding := amount - settled
	if outstanding < 0 {
		outstanding = 0
	}
	return SettlementView{
		BillID: bill.ID, Status: billStatusForSettlement(amount, settled),
		Amount: amount, Settled: settled, Outstanding: money2(outstanding),
		Oversettled: settled > amount+paymentCentEpsilon,
	}
}

// billStatusForSettlement 那个和翻成账单词。零位移不在此判（调用方比过 status 才跃迁）。
func billStatusForSettlement(amount, settled float64) string {
	switch {
	case settled+paymentCentEpsilon >= amount:
		return model.BillStatusPaid
	case settled >= paymentCentEpsilon:
		return model.BillStatusPartial
	default:
		return model.BillStatusOpen
	}
}

// Statement 一张应收的对账视图（读侧：只读，不折算、不写库）。
func (s *PaymentService) Statement(ctx context.Context, billID string) (*BillStatementView, error) {
	if !s.Available() {
		return nil, ErrPaymentServiceUnavailable
	}
	id := strings.TrimSpace(billID)
	if id == "" {
		return nil, fmt.Errorf("%w: bill_id 为空（它会退化成「读全表的第一行」）", ErrPaymentInputInvalid)
	}
	bill, err := s.bills.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("payment: 读账单 %s 失败：%w", id, err)
	}
	if bill == nil {
		return nil, fmt.Errorf("%w: %s", ErrPaymentBillNotFound, id)
	}
	return s.statementOfBill(ctx, bill)
}

// StatementsOfQuote 这张报价单开过的全部应收，逐张带上"收了多少、还欠多少"。
//
// 结转遗留①的兑现点：一张报价单的多个版本各自成交过一次时（改价后又接受），
// 对账读的就是这一串。链长由谈判轮次决定（个位数），所以逐张求和不在这里优化成分页聚合。
func (s *PaymentService) StatementsOfQuote(ctx context.Context, quoteID string) ([]*BillStatementView, error) {
	if !s.Available() {
		return nil, ErrPaymentServiceUnavailable
	}
	id := strings.TrimSpace(quoteID)
	if id == "" {
		return nil, fmt.Errorf("%w: quote_id 为空（按它捞账单会退化成一次没有条件的读）", ErrPaymentInputInvalid)
	}
	bills, err := s.bills.ListByQuoteID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("payment: 按报价 %s 捞账单失败：%w", id, err)
	}
	out := make([]*BillStatementView, 0, len(bills))
	for _, b := range bills {
		if b == nil {
			continue
		}
		st, err := s.statementOfBill(ctx, b)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// statementOfBill 一张账单行 → 对账视图（两次读：名单与和）。
func (s *PaymentService) statementOfBill(ctx context.Context, bill *model.Bill) (*BillStatementView, error) {
	rows, err := s.payments.ListByBillID(ctx, bill.ID)
	if err != nil {
		return nil, fmt.Errorf("payment: 读账单 %s 名下的回款行失败：%w", bill.ID, err)
	}
	settled, err := s.payments.SumSettledByBill(ctx, bill.ID)
	if err != nil {
		return nil, fmt.Errorf("payment: 求账单 %s 的已收失败：%w", bill.ID, err)
	}
	view := settlementOf(bill, money2(settled))
	// 读侧不写库，所以这一格报的是**库里那一行**的状态；视图上另外两格
	// 若与它矛盾（例如跃迁那天失败了），对账读到的就是"库里说的"而不是"我以为的"。
	out := &BillStatementView{
		BillID: bill.ID, QuoteID: bill.QuoteID, QuoteRowID: bill.QuoteRowID, OpportunityID: bill.OpportunityID,
		Status: bill.Status, Amount: view.Amount, Currency: bill.Currency, DueAt: bill.DueAt,
		Settled: view.Settled, Outstanding: view.Outstanding, Oversettled: view.Oversettled,
		Payments: make([]PaymentView, 0, len(rows)),
	}
	for _, p := range rows {
		if p == nil {
			continue
		}
		out.Payments = append(out.Payments, paymentViewOf(p, false))
	}
	return out, nil
}

// paymentViewOf 回款行 → 视图。逐列搬，不重算金额。
func paymentViewOf(p *model.Payment, filled bool) PaymentView {
	return PaymentView{
		ID: p.ID, BillID: p.BillID, Amount: p.Amount, Currency: p.Currency, PaidAt: p.PaidAt,
		ChannelRef: p.ChannelRef, Status: p.Status, Platform: p.Platform, OrderID: p.OrderID,
		FilledPaidAt: filled,
	}
}

// payTimeOf 载荷给的到账时间优先；没给才填接收时刻（填了就要在视图上承认，见 FilledPaidAt）。
func payTimeOf(given *time.Time, now time.Time) time.Time {
	if given != nil && !given.IsZero() {
		return *given
	}
	return now
}

// money2 折到分。numeric(14,2) 落库时还会再折一次，这里是让**本层的比较**只在整分上做。
func money2(v float64) float64 { return math.Round(v*100) / 100 }

// —— 主键生成 ——————————————————————————————————————————————————————————

// paymentSeq 进程内单调计数器，口径与 billSeq / quoteSeq 完全一致。
var paymentSeq int64

func nextPaymentSeq() int64 { return atomic.AddInt64(&paymentSeq, 1) }

// newPaymentKey 纯函数版生成器（同一时刻 + 同一 seq ⇒ 同一个号），用例可直接复算。
//
// 与 newBillKey 同一条不含日期串的理由（时区裂脑），前缀 p_ 与 b_ / q_ 同族。
func newPaymentKey(now time.Time, seq int64) string {
	return fmt.Sprintf("p_%d_%d", now.UnixNano(), seq)
}
