// bill.go T-P7-01：账单派生层（"这一版报价成交了" → 一张应收）。
//
// 一句话职责：把一版**已经发到客户手上**的报价认成成交，并当场开出它对应的那张应收。
// 两件事必须同时发生、且有先后（见 顺序 那一段），因为 M3 之后所有回款动作
// （T-P7-02 的入账、T-P7-03 的催收、T-P7-04 的赢单）读的都是这一行。
//
// 「成交」在这一天之前系统里没有任何一处写得住：
//   - model.QuoteStatusAccepted 这个值从 T-P6-01 起就在值域里，但全仓非测试代码
//     没有任何一条路径写它（实测：repository.QuoteRepository.UpdateStatus 的调用方
//     只有发送腿的 draft↔sent 两次），所以"客户接了"这件事在库里从来没有发生过；
//   - external_orders 那边有真实成交数据，但它不带报价引用（见 model/integration.go
//     的 ExternalOrder：只有 platform_order_no / pay_amount），追不回"这是哪一版报的价"。
//
// 于是本层就是 `accepted` 的**唯一生产写入口**。这既是本卡的价值，也是本卡最需要
// 说清楚的一句限制：调用方是人（销售在工作台上点"客户已确认"），不是客户。
// 报价控制器当年刻意没开这个口（internal/controller/quote.go 的 RegisterRoutes 注释：
// "接受报价是客户的动作，不是销售的按钮"），那条判断今天仍然成立 ——
// 变化的是它现在有了后果：点这一格的人同时在开一张应收，而这一条是**可审计**的：
// bills.created_at 记"什么时候开的"，quote_row_id 记"从哪一版来的"，
// 报价行的 updated_at 记"哪一版被动过"。
// 【限制如实记下】本卡没有"谁确认的"这一格 —— bills 的列清单里没有，
// 而把它塞进 sales_events 属 T-P8-01 的埋点范围（那张卡给全事件类型统一补
// opportunity_id / quote_id）。等它落地时，"是谁点的"才有地方存；现在不假装有。
//
// 四条判据各挡一种坏法：
//
//	① 只有 sent 那一版能被确认。draft 是"还没给客户看过"，rejected 是"客户回了不要"，
//	   expired 是"那版已经作废"。三格里任何一格能派生应收，库里就会有一张
//	   "客户从没见过的账单"。
//	② 必须是链上**版本号最大**的那一版。客户手上看到的是最新一版；按旧版开应收，
//	   开出的是另一份价格，而两处（旧版确实 sent 过、金额确实等于旧版合计）都自洽，
//	   只有对账时才看得见。
//	③ 一条链上只留一次成交。已经有一版被接受过，再来一次就是**重新成交**，
//	   原账单一式两份会变成两张应收。本层不自动作废原来那张 —— 作废要看回款，
//	   而回款行在 T-P7-02；宁可挡在门外让人先去处理那一张。
//	④ 金额只有一个来源：那一版的行项目相加（quoteSumAmount，与发送腿同一个函数）。
//	   入参里没有 amount 这一格，"账单金额与报价合计一致"（AC②）因此不是两条数据
//	   比对出来的巧合，而是同一个算法的同一个结果。
//
// 顺序（这一条是本层最容易日后被人"顺手优化"掉的地方）：
// 所有**只读**判据（状态、链位、行项目、合计）都在第一次写之前跑完，
// 然后才是 CAS(sent→accepted) → Create。于是两个失败窗口的方向分别是：
//   - 只读判据没过 ⇒ 库里什么都没动（报价还是 sent，可以修好数据再来一次）；
//   - CAS 成了而账单没落（崩溃 / 库抖）⇒ 留下"已接受但没账单"，
//     这一格靠重入修得回来：再点一次会跳过 CAS、直接派生（见 DeriveFromQuote 里
//     那一支 accepted 分支）。反过来先插账单再改报价，坏的那一侧是
//     "报价没成交而库里有一张应收"，而本表没有删除口，那是要人去删的数据。
//
// 不做的事：不发通知、不写 sales_events（同上，属 P8-01）、不做部分回款、
// 不算账期（due_at 恒 NULL，理由见 model/bill.go 那一列的注释）。

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var (
	// ErrBillServiceUnavailable 依赖缺件（部署的事）。
	ErrBillServiceUnavailable = errors.New("bill: 账单服务未装配（账单存储与报价存储缺一即不能派生）")
	// ErrBillInputInvalid 入参不合法（调用方的事：改载荷）。
	ErrBillInputInvalid = errors.New("bill: 账单派生入参不合法")
	// ErrBillQuoteNotFound 那一版报价根本不存在（拿错了行号）。
	// 与下面的 NotSent 分开：前者是"没有这个东西"，后者是"有，但还没走到那一步"。
	ErrBillQuoteNotFound = errors.New("bill: 来源报价版本行不存在")
	// ErrBillQuoteNotSent 这一版没发出去过，谈不上被客户接受。
	ErrBillQuoteNotSent = errors.New("bill: 该版报价尚未发出，不能作为成交依据")
	// ErrBillNotLatestVersion 要确认的是链上被取代的旧版。
	ErrBillNotLatestVersion = errors.New("bill: 只能确认链上最新那一版报价成交")
	// ErrBillChainAlreadyAccepted 这条报价链已经有一次成交了。
	ErrBillChainAlreadyAccepted = errors.New("bill: 该报价链已有被接受的版本，请先处置原账单再重新成交")
	// ErrBillLinesMissing 版本行在库里而行项目一条都没有（与发送腿同一格数据事故）。
	ErrBillLinesMissing = errors.New("bill: 该版报价没有行项目，合计无从算出")
	// ErrBillStatusStuck 报价状态没能落到 accepted。
	// 单独成 sentinel 是因为处置动作是"重读那一版再决定"，而不是"改载荷再来"。
	ErrBillStatusStuck = errors.New("bill: 报价状态没能落到 accepted，账单未派生")
)

// billStore 派生腿对账单存储的全部依赖。
//
// **没有** UpdateStatus：接口里没有，这条腿就写不出"派生时顺手标成 paid"，
// 而那一格真正的调用方在 T-P7-02（回款累计到位才算结清）。方法集合由
// bill_test.go 的 TestBillServicePortSurfacesAreNarrow 用反射钉住 ——
// 用注释守这件事的先例在本仓不止一次失效（同 quote 生成腿那份）。
//
// GetByID 有真实调用方：写完读回来出视图。返回值是要进 API 响应的，
// 库里那一行到底存了什么（尤其 numeric(14,2) 落进去之后还是不是那个数）只能读回来才知道。
type billStore interface {
	Available() bool
	Create(ctx context.Context, b *model.Bill) error
	GetByID(ctx context.Context, id string) (*model.Bill, error)
	GetByQuoteRowID(ctx context.Context, quoteRowID string) (*model.Bill, error)
}

// billQuoteStore 派生腿对报价存储的全部依赖。
//
// 四格里只有一格是写（UpdateStatus：落 accepted），三格是读：
// 版本行本身、链上全部版本（判据②③）、那一版的行项目（判据④）。
// **没有** Create / Append：确认成交的腿不许顺手造报价 —— 否则"账单由已成交报价派生"
// 会退化成"账单由这条腿自己编出来的报价派生"。
type billQuoteStore interface {
	GetByID(ctx context.Context, id string) (*model.Quote, error)
	UpdateStatus(ctx context.Context, id, from, to string) error
	ListVersions(ctx context.Context, quoteID string) ([]*model.Quote, error)
	ListLines(ctx context.Context, quoteRowID string) ([]*model.QuoteLineItem, error)
}

// BillDeriveInput 一次成交确认的全部输入：**只有**版本行号（字段集合由反射用例钉成白名单）。
//
// 没有 amount / currency / status / due_at 任何一格：那四格全都是从库里那一版推出来的，
// 入参里能递进来一个，AC② 的"账单金额与报价合计一致"就当场失去对账对象。
type BillDeriveInput struct {
	QuoteRowID string // 必填：quotes.id（**版本行主键**，不是 quotes.quote_id 逻辑号）
}

// BillView 一张账单的对外视图（派生与复用两条路都回它）。
//
// 与 model.Bill 的字段差一格：Reused。它不是装饰——"我刚开了一张应收"与
// "这张应收早就在了"对调用方是两句话：前者该去通知客户付款，后者该去核对为什么重复点。
// 只回账单本体就逼调用方自己去数库里几行，而它没有那个读口。
type BillView struct {
	ID            string     `json:"id"`
	QuoteID       string     `json:"quote_id"`
	QuoteRowID    string     `json:"quote_row_id"`
	OpportunityID string     `json:"opportunity_id"`
	Amount        float64    `json:"amount"`
	Currency      string     `json:"currency"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	Reused        bool       `json:"reused"`
}

// BillService 回款域的派生层。依赖全走注入，本层不自己去拿全局 DB。
type BillService struct {
	bills  billStore
	quotes billQuoteStore
	now    func() time.Time
}

// NewBillService 构造。缺件时构造照旧成功，由 Available / DeriveFromQuote 报出来 ——
// 装配点要能在没配账单存储的环境里起来，但绝不能因此"静默不派生"。
func NewBillService(bills billStore, quotes billQuoteStore) *BillService {
	return &BillService{bills: bills, quotes: quotes, now: time.Now}
}

// SetClock 注入时钟（账单号里的纳秒与 created_at 共用它）。
func (s *BillService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// Available 报告能不能派生。
func (s *BillService) Available() bool {
	return s != nil && s.bills != nil && s.quotes != nil
}

// DeriveFromQuote 确认这一版报价成交，并开出那张应收。
//
// 幂等由两处合力（缺任何一处都还剩一种坏法）：
//   - 已 accepted 且账单在 ⇒ 直接复用那一行（不碰库，重复点第二次不产生任何写入）；
//   - 并发里两路同时冲进来 ⇒ 后到的那一路被 uq_bills_quote_row 挡下
//     （repository.ErrBillAlreadyDerived），据此回读那一行。
//
// 第二处不能省：第一处的判据读在写之前，两个读都落空、两个写都到达的窗口是真实存在的。
func (s *BillService) DeriveFromQuote(ctx context.Context, in BillDeriveInput) (*BillView, error) {
	if !s.Available() {
		return nil, ErrBillServiceUnavailable
	}
	rowID := strings.TrimSpace(in.QuoteRowID)
	if rowID == "" {
		return nil, fmt.Errorf("%w: 版本行号为空（账单钉在版本行上，逻辑号跨版本重复出现）", ErrBillInputInvalid)
	}

	row, err := s.quotes.GetByID(ctx, rowID)
	if err != nil {
		return nil, fmt.Errorf("bill: 读报价版本行 %s 失败：%w", rowID, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: %s", ErrBillQuoteNotFound, rowID)
	}
	if row.Status != model.QuoteStatusAccepted && row.Status != model.QuoteStatusSent {
		return nil, fmt.Errorf("%w: 该版当前是 %s", ErrBillQuoteNotSent, row.Status)
	}

	// 成交了的那一版有没有账单？有就是重入，直接复用 —— 这一步在跃迁之前，
	// 所以"确认两次"在库里一次写都不产生。
	if row.Status == model.QuoteStatusAccepted {
		existing, err := s.bills.GetByQuoteRowID(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("bill: 读报价行 %s 对应的账单失败：%w", row.ID, err)
		}
		if existing != nil {
			return billViewOf(existing, true), nil
		}
		// existing == nil ⇒ "已接受但没账单"那个崩溃窗口，往下走补派生（本行不再跃迁）。
	}

	// 链位与"链上只留一次成交"两判据都要读整条链。用 ListVersions 而不是再给报价
	// 仓储开一个"按 quote_id 数 accepted"的读口：链长由谈判轮次决定（个位数，
	// 见仓储那句"刻意不分页"），而为个位数的集合专门建一条聚合查询是第二个事实源。
	versions, err := s.quotes.ListVersions(ctx, row.QuoteID)
	if err != nil {
		return nil, fmt.Errorf("bill: 读报价链 %s 失败：%w", row.QuoteID, err)
	}
	if err := checkBillChainPosition(row, versions); err != nil {
		return nil, err
	}

	lines, err := s.quotes.ListLines(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("bill: 读报价行 %s 的行项目失败：%w", row.ID, err)
	}
	if len(lines) == 0 {
		// 报在这而不是算出个 0.00 继续走：合计 0 的应收与"这一版根本没行"是两回事，
		// 而后者派生的输入都不存在。
		return nil, fmt.Errorf("%w: %s", ErrBillLinesMissing, row.ID)
	}

	if row.Status != model.QuoteStatusAccepted {
		if err := s.quotes.UpdateStatus(ctx, row.ID, model.QuoteStatusSent, model.QuoteStatusAccepted); err != nil {
			return nil, fmt.Errorf("%w（%s，sent→accepted）：%w", ErrBillStatusStuck, row.ID, err)
		}
	}

	now := s.now()
	key := newBillKey(now, nextBillSeq())
	currency := strings.TrimSpace(row.Currency)
	if currency == "" {
		currency = model.BillCurrencyDefault
	}
	bill := &model.Bill{
		ID: key,
		// 两把来路键都从报价行原样抄：不回查、不自造。
		QuoteID:       row.QuoteID,
		QuoteRowID:    row.ID,
		OpportunityID: row.OpportunityID,
		Amount:        quoteSumAmount(lines),
		Currency:      currency,
		DueAt:         nil, // 账期未定：本卡没有任何一处定义过付款条件
		Status:        model.BillStatusOpen,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.bills.Create(ctx, bill); err != nil {
		if !errors.Is(err, repository.ErrBillAlreadyDerived) {
			return nil, fmt.Errorf("bill: 派生账单失败（报价行 %s 已记为 accepted，重入这一动作可修）：%w", row.ID, err)
		}
		// 并发窗口：另一路刚插进去。回读那一行，把它当成本次的结论返回。
		existing, gerr := s.bills.GetByQuoteRowID(ctx, row.ID)
		if gerr != nil {
			return nil, fmt.Errorf("bill: 撞到\"已派生\"之后回读又失败：%w", gerr)
		}
		if existing == nil {
			// 仓储说"这张报价行已经有账单了"而按同一把键读不回东西 —— 两种解释都是坏的，
			// 且都不能静默：要么约束名认错人了（别的 23505 被当成幂等键），
			// 要么那一行刚被删掉。宁可乐观地报一条需要人看的话，也不回 (nil, nil)。
			return nil, fmt.Errorf("bill: 仓储报\"已派生过\"而按 quote_row_id=%s 读不到那一行（约束名与索引不同源？须人工核对）", row.ID)
		}
		return billViewOf(existing, true), nil
	}

	// 读回来出视图：返回值以库里那一行为准。
	stored, err := s.bills.GetByID(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("bill: 账单 %s 已写库但读不回来（须人工核对这一张是否真的存在，别盲目重试）：%w", key, err)
	}
	if stored == nil {
		return nil, fmt.Errorf("bill: 账单 %s 写入返回成功而按主键读不回（同一事务里刚写的那一行，须人工核对）", key)
	}
	return billViewOf(stored, false), nil
}

// checkBillChainPosition 判据②③：链位与"一条链只留一次成交"。
//
// 两条都在**整条链**上判，所以必须拿到 ListVersions 的结果；单看那一版自己，
// "它是第三版"与"第一版已经被接受了"两件事都读不出来。
func checkBillChainPosition(row *model.Quote, versions []*model.Quote) error {
	var latest int64
	for _, v := range versions {
		if v == nil {
			continue
		}
		if v.Version > latest {
			latest = v.Version
		}
	}
	if row.Version != latest {
		return fmt.Errorf("%w: 要确认的是 v%d，而链 %s 上最新是 v%d",
			ErrBillNotLatestVersion, row.Version, row.QuoteID, latest)
	}
	for _, v := range versions {
		if v == nil || v.ID == row.ID {
			continue
		}
		if v.Status == model.QuoteStatusAccepted {
			return fmt.Errorf("%w: 链 %s 上的 v%d（%s）已被接受过",
				ErrBillChainAlreadyAccepted, row.QuoteID, v.Version, v.ID)
		}
	}
	return nil
}

// billViewOf 账单行 → 视图。逐列搬，不重算金额：
// 视图里出现一次算术，AC② 就有了第二个算法。
func billViewOf(b *model.Bill, reused bool) *BillView {
	return &BillView{
		ID: b.ID, QuoteID: b.QuoteID, QuoteRowID: b.QuoteRowID, OpportunityID: b.OpportunityID,
		Amount: b.Amount, Currency: b.Currency, DueAt: b.DueAt, Status: b.Status,
		CreatedAt: b.CreatedAt, Reused: reused,
	}
}

// —— 主键生成 ——————————————————————————————————————————————————————————

// billSeq 进程内单调计数器，口径与 quoteSeq / approvalSeq 完全一致：
// 只取纳秒会在同一纳秒内的两次生成撞，只取计数器则两个进程各自从 1 开始就撞。
var billSeq int64

func nextBillSeq() int64 { return atomic.AddInt64(&billSeq, 1) }

// newBillKey 纯函数版生成器（同一时刻 + 同一 seq ⇒ 同一个号），用例可直接复算。
//
// **不含日期串**：带日期就要选一个时区格式化，而本仓 PG 会话钉在 CST、
// Go 侧按宿主机时区读，同一时刻能生成两个"当天序号"（日期边界裂脑那条老账）。
// 前缀 b_ 与 quotes 的 q_ 同族：运营在库里肉眼就能分域。
// bills.id 是 text，没有宽度上限问题；但它会被抄进 payments.bill_id（T-P7-02），
// 那一列按计划是 varchar(64)，所以这里必须短于它 —— 2+19+1+13 = 35 字符。
func newBillKey(now time.Time, seq int64) string {
	return fmt.Sprintf("b_%d_%d", now.UnixNano(), seq)
}
