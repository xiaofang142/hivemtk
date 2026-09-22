// quote.go T-P6-02：报价生成（模板 + 行项目 + 话术分片）。
//
// 本层是 quotes 版本链第一个**写入方**（T-P6-01 建的是形状，今天有了内容）。
// 三条 AC 在本层的落点各不相同，各自的反面就是本文件存在的理由：
//
//	AC① 话术必经版本/灰度（C8 改判后的口径：复用 script_library 那套整数版本 +
//	     激活指针 + 过期拦截 + FNV 分桶，**不**挂知识库的 canary 列）。反面是
//	     「入参里能递正文」或「先取可变列、等有快照再切」—— 两者都会让一张
//	     没有版本依据的话术发给客户，而库里那行看起来与走正路的一模一样。
//	     所以本层的入参结构体里没有话术字段，正文只能从一个端口拿。
//	AC② 生成即草稿。反面是"顺手提供改状态的方法"，那会让 P6-03 那道发送闸门
//	     变成可绕过的建议。所以本层依赖的 quoteStore 是 QuoteRepository **去掉
//	     UpdateStatus** 的那一份：拦住这件事的是类型，不是注释。
//	AC③ 行项目金额合计与落库一致。反面是"先加总再舍一次" —— 那会得出另一个数，
//	     而报价单的总价必须只有一个答案：库里那些行相加。视图的合计因此**从库里
//	     读回来的行算出**，不由内存那份直接返回。
//
// 三处失败面刻意写成"一行都不写"：闸门关着、模板没配、话术没注册。
// 它们的共同反面是先落一行再补齐，而那行会立刻被 P6-03 的读侧当成可发对象捞走 ——
// 一张没有依据的价格文件发给客户，代价不对等。
//
// 本层**不持久化话术**（quotes 表也没有那一列）：生成时选中的那版只是视图里带出去
// 的临时事实，发送时（T-P6-03）必须重新解析生效版本 —— 否则发送动作会发出
// 一份生成时快照，而那份话术可能早在两次谈判之间被运营下线。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// —— 配置键（system_config_kv）——————————————————————————————————————————

const (
	// QuoteTemplateKVPrefix 报价模板住在配置里而不是代码里：换一份价目表不该等一次发版
	// （与 ltc.config、script_ab.{id} 同族）。完整键 = 前缀 + 模板代号。
	QuoteTemplateKVPrefix = "quote.template."

	// QuoteScriptIDKVKey 「报价封面用哪个话术分片」这条指针。
	// 存的是 script_library.id 那个整数而**不是正文**：正文每次从生效快照读，
	// 抄一份进配置就是第二个事实源（AC① 的反面）。
	QuoteScriptIDKVKey = "quote.script_id"
)

// —— 错误哨兵：每一种都对应一种不同的处置动作 ——————————————————————————————

var (
	// ErrQuoteServiceUnavailable 依赖缺件（部署的事）。
	ErrQuoteServiceUnavailable = errors.New("quote: 报价服务未装配（存储 / 商机 / 闸门 / 配置 / 话术端口任一缺失）")
	// ErrQuoteInputInvalid 入参越界（调用方的事：改载荷）。
	ErrQuoteInputInvalid = errors.New("quote: 报价入参不合法")
	// ErrQuoteGateClosed 闸门按运营的选择没开（谁的都不用修，等开启）。
	ErrQuoteGateClosed = errors.New("quote: 报价阶段闸门未生效")
	// ErrQuoteOpportunityMissing 商机不存在（先去把那条商机建出来）。
	ErrQuoteOpportunityMissing = errors.New("quote: 来源商机不存在")
	// ErrQuoteTemplateMissing 模板没配（去配）。与 Invalid 分开：改配置内容还是加配置项，
	// 是两种动作；合成一个就只能三选一地标错。
	ErrQuoteTemplateMissing = errors.New("quote: 报价模板未配置")
	// ErrQuoteTemplateInvalid 模板配了但读不出可用行（去改那份 JSON）。
	ErrQuoteTemplateInvalid = errors.New("quote: 报价模板内容不合法")
	// ErrQuoteScriptUnavailable 话术分片没注册 / 生效指针没有快照 / 已过期（AC① 的门）。
	ErrQuoteScriptUnavailable = errors.New("quote: 报价话术没有可用的生效版本")
	// ErrQuoteVersionMissing 按 (quote_id, version) 或行键读不到那一版。
	ErrQuoteVersionMissing = errors.New("quote: 报价版本不存在")
)

// quoteStore 本层对存储的全部依赖 —— QuoteRepository **去掉 UpdateStatus** 的那一份。
//
// 少一个方法是刻意的：接口里没有，service 里就写不出"生成完顺手标成已发送"，
// 而 C1 把"未过闸门不得发送"判成了硬约束。用注释守这件事的先例在本仓不止一次失效
// （见 quote_test.go 的 TestQuoteServiceStoreSurfaceHasNoStatusWriter 那条白名单）。
type quoteStore interface {
	Available() bool
	Create(ctx context.Context, q *model.Quote) error
	Append(ctx context.Context, baseID string, next *model.Quote) error
	AddLines(ctx context.Context, quoteRowID string, lines []*model.QuoteLineItem) error
	GetByID(ctx context.Context, id string) (*model.Quote, error)
	GetVersion(ctx context.Context, quoteID string, version int64) (*model.Quote, error)
	Latest(ctx context.Context, quoteID string) (*model.Quote, error)
	ListVersions(ctx context.Context, quoteID string) ([]*model.Quote, error)
	ListLines(ctx context.Context, quoteRowID string) ([]*model.QuoteLineItem, error)
}

// opportunityReader 商机存在性的读口。只要 GetByID：本层不推进商机、不看阶段，
// 多给一个方法就会有人顺手在这里改商机（那是 T-P4-03 的跃迁表管的）。
type opportunityReader interface {
	GetByID(ctx context.Context, id string) (*model.Opportunity, error)
}

// quoteConfigGetter 模板与话术指针的读口（SystemConfigKVRepository 的那一格）。
//
// 「没配」回空串、「读不到」回 error —— 这个区分由仓储那份语义承担，本层必须保住它：
// 存储抖动被读成"运营没配模板"，症状是运营去配了一份本来就对的配置。
type quoteConfigGetter interface {
	Get(ctx context.Context, key string) (string, error)
}

// QuoteScript 一次解析出来的生效话术（AC① 的结论物）。
type QuoteScript struct {
	ScriptID uint   `json:"script_id"`
	Version  int    `json:"version"`
	Bucket   string `json:"bucket,omitempty"`
	Content  string `json:"content"`
}

// QuoteScriptPort 拿生效话术的那道口子。
//
// 它是接口而不是直接把 *QuoteScriptSource 钉进签名：本层要的是"给我这个分片给这个人
// 的生效正文"，版本判据、过期拦截、分桶函数都在里面。
type QuoteScriptPort interface {
	ActiveQuoteScript(ctx context.Context, scriptID uint, oneID string) (QuoteScript, error)
}

// QuoteGenerateInput 一次生成/还价的全部输入。
//
// **没有话术字段**（AC①）：能递正文就迟早会有人递正文，那时版本、灰度、过期三判据
// 全被绕过而库里读不出差别。字段集合由反射用例钉成白名单。
type QuoteGenerateInput struct {
	OpportunityID string           // 必填（还价侧留空 = 继承基准那一版的归属）
	OneID         string           // 可空：没定位到人时分桶为空，正文照样要生效版本
	TemplateCode  string           // 生成第一版必填；还价不重跑模板（见 Revise）
	Currency      string           // 空 = 取模板那份，再空 = 建表默认
	ValidUntil    *time.Time       // nil = 由模板 valid_days 推，再没有 = 不设
	Lines         []QuoteLineInput // 对模板/基准行的覆盖与追加，可空
}

// QuoteLineInput 行项目覆盖项。
//
// 全指针是为了「显式 0」与「没给」分得开：按非零才覆盖实现，"客户还价、折扣回到原价"
// 这个最常见的动作做不出来，而且不报错 —— 折扣悄悄还在。
type QuoteLineInput struct {
	ProductID       string
	Title           *string
	Quantity        *float64
	UnitPrice       *float64
	DiscountPercent *float64
}

// QuoteLineView 行项目的读视图。Gross 是折前小计，由库里那行的数量×单价现算
// （它不是存储列：存一份就少一份会漂的账，判据同商机侧不存赢率历史）。
type QuoteLineView struct {
	LineNo          int64   `json:"line_no"`
	ProductID       string  `json:"product_id,omitempty"`
	Title           string  `json:"title"`
	Quantity        float64 `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	DiscountPercent float64 `json:"discount_percent"`
	Gross           float64 `json:"gross"`
	Amount          float64 `json:"amount"`
}

// QuoteView 一版报价的完整视图。
type QuoteView struct {
	ID            string          `json:"id"`
	QuoteID       string          `json:"quote_id"`
	OpportunityID string          `json:"opportunity_id"`
	Version       int64           `json:"version"`
	Status        string          `json:"status"`
	Currency      string          `json:"currency"`
	ValidUntil    *time.Time      `json:"valid_until,omitempty"`
	SourceID      string          `json:"source_id,omitempty"`
	Lines         []QuoteLineView `json:"lines"`
	GrossTotal    float64         `json:"gross_total"`
	Total         float64         `json:"total"`

	// Script 本次解析到的生效话术。只在生成/还价的返回里出现，**不落库**：
	// 发送时（T-P6-03）要重新解析一次，拿这一份去发等于把谈判期间可能已下线的文案发出去。
	Script *QuoteScript `json:"script,omitempty"`
}

// QuoteService 报价生成层。依赖全部注入，本层不自己去拿全局 DB。
type QuoteService struct {
	store   quoteStore
	opps    opportunityReader
	gate    LTCConfigReader
	cfg     quoteConfigGetter
	scripts QuoteScriptPort
	now     func() time.Time
}

// NewQuoteService 构造。配置端口与话术端口用 Set 注入（同 ScriptABService 的 kv 口径：
// 装配点拿得到 DB 却未必拿得到 kv，缺件的后果由 Available 报出来）。
func NewQuoteService(store quoteStore, opps opportunityReader, gate LTCConfigReader) *QuoteService {
	return &QuoteService{store: store, opps: opps, gate: gate, now: time.Now}
}

// SetClock 注入时钟（键里的时间戳与 valid_days 的推导共用它）。
func (s *QuoteService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	if now == nil {
		now = time.Now
	}
	s.now = now
}

// SetConfigStore 注入 system_config_kv 读口。
func (s *QuoteService) SetConfigStore(getter quoteConfigGetter) {
	if s == nil {
		return
	}
	s.cfg = getter
}

// SetScriptSource 注入话术端口。
func (s *QuoteService) SetScriptSource(port QuoteScriptPort) {
	if s == nil {
		return
	}
	s.scripts = port
}

// Available 报告能不能生成。闸门配置**不在**这一条里（nil 那份按降级处理，
// 那是"关"不是"坏"），但 gate 这个端口本身缺件是装配事故，必须报得出来。
func (s *QuoteService) Available() bool {
	return s != nil && s.store != nil && s.store.Available() &&
		s.opps != nil && s.gate != nil && s.cfg != nil && s.scripts != nil
}

// —— 生成 / 还价 ——————————————————————————————————————————————————

// Generate 按模板落一条报价链的第一版（draft）。
//
// 判据次序：装配 → 身份与量程 → 闸门 → 商机存在 → 话术生效版本 → 模板 → 合并行 → 写。
// 闸门排在任何 DB 读之前（一次没开的阶段不该去扫两张表），话术排在模板之前
// （AC① 是本卡的顺序依赖，模板坏了还能换一份，没有生效版本的话术是根本不该出报价）。
func (s *QuoteService) Generate(ctx context.Context, in QuoteGenerateInput) (*QuoteView, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	oppID := strings.TrimSpace(in.OpportunityID)
	if oppID == "" {
		return nil, fmt.Errorf("%w: 商机号为空 —— 没有来源商机的报价在漏斗里读不出是谁的", ErrQuoteInputInvalid)
	}
	reqCurrency, err := normalizeQuoteCurrency(in.Currency)
	if err != nil {
		return nil, err
	}
	if err := s.checkGate(ctx); err != nil {
		return nil, err
	}
	opp, err := s.opps.GetByID(ctx, oppID)
	if err != nil {
		return nil, fmt.Errorf("quote: 读商机 %s 失败：%w", oppID, err)
	}
	if opp == nil {
		return nil, fmt.Errorf("%w: %s（报价必从商机长出，挂空的行在闭环率里读不出来而它长得完全正常）",
			ErrQuoteOpportunityMissing, oppID)
	}
	script, err := s.resolveScript(ctx, in.OneID)
	if err != nil {
		return nil, err
	}
	tpl, err := s.loadTemplate(ctx, in.TemplateCode)
	if err != nil {
		return nil, err
	}
	base := make([]quoteLine, 0, len(tpl.Lines))
	for i, l := range tpl.Lines {
		base = append(base, quoteLine{lineNo: int64(i + 1), productID: l.ProductID, title: l.Title,
			quantity: l.Quantity, unitPrice: l.UnitPrice, disc: l.DiscountPercent})
	}
	lines, err := mergeQuoteLines(base, in.Lines)
	if err != nil {
		return nil, err
	}

	currency := reqCurrency
	if currency == "" {
		currency = tpl.Currency
	}
	if currency == "" {
		currency = model.QuoteCurrencyDefault
	}
	validUntil, err := s.resolveValidUntil(in.ValidUntil, tpl.ValidDays)
	if err != nil {
		return nil, err
	}

	now := s.now()
	id, code := newQuoteKeysFromClock(now)
	row := &model.Quote{
		ID: id, QuoteID: code, OpportunityID: oppID,
		Status: model.QuoteStatusDraft, Currency: currency, ValidUntil: validUntil,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("quote: 第一版落库失败：%w", err)
	}
	return s.persistLines(ctx, row, lines, script)
}

// Revise 在最新一版之上追加下一版（LTC-12 的谈判过程）。
//
// 两条与 Generate 不同的判据：
//   - **不重跑模板**：还价是对"已经报出去的那些行"做修改。模板在两次谈判之间被运营
//     改过的话，重跑就会把客户没见过的一批行悄悄换进来，而版本号只说明"这是第二版"。
//     所以行起点是基准版的持久行；TemplateCode 在这一路不参与。
//   - 商机归属不可变：留空继承，给了别的号就是想把单子挪到另一条链上 —— 那是
//     另一件事（另起一条 quote_id），本层不做。
//
// 版本号与来路都不由本层写：仓储从基准行递增（ErrQuoteVersionReserved 挡的就是
// 调用方自带版本号）。两个人同时基于同一版追加时，输家拿到
// repository.ErrQuoteVersionConflict 并**原样上抛** —— 不重读重试，那是"谁报价了"
// 这个事实的裁决，得让人看见。
func (s *QuoteService) Revise(ctx context.Context, quoteID string, in QuoteGenerateInput) (*QuoteView, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	quoteID = strings.TrimSpace(quoteID)
	if quoteID == "" {
		return nil, fmt.Errorf("%w: 报价编号为空（追加哪一条链必须由调用方明说）", ErrQuoteInputInvalid)
	}
	reqCurrency, err := normalizeQuoteCurrency(in.Currency)
	if err != nil {
		return nil, err
	}
	if err := s.checkGate(ctx); err != nil {
		return nil, err
	}
	base, err := s.store.Latest(ctx, quoteID)
	if err != nil {
		return nil, fmt.Errorf("quote: 读报价链 %s 的最新版失败：%w", quoteID, err)
	}
	if base == nil {
		return nil, fmt.Errorf("%w: 链 %s 上一版都没有，先走 Generate", ErrQuoteVersionMissing, quoteID)
	}
	if opp := strings.TrimSpace(in.OpportunityID); opp != "" && opp != base.OpportunityID {
		return nil, fmt.Errorf("%w: 还价不许改商机归属（基准是 %s，递进来的是 %s）—— 换商机只能另起一条报价链",
			ErrQuoteInputInvalid, base.OpportunityID, opp)
	}
	script, err := s.resolveScript(ctx, in.OneID)
	if err != nil {
		return nil, err
	}
	baseLines, err := s.store.ListLines(ctx, base.ID)
	if err != nil {
		return nil, fmt.Errorf("quote: 读基准版 %s 的行项目失败：%w", base.ID, err)
	}
	if len(baseLines) == 0 {
		return nil, fmt.Errorf("%w: 基准版 %s 没有任何行项目，没有可继承的内容（这是数据事故而非入参问题：该行不该被当成可还价的报价）",
			ErrQuoteVersionMissing, base.ID)
	}
	lines, err := mergeQuoteLines(quoteLinesFromRows(baseLines), in.Lines)
	if err != nil {
		return nil, err
	}

	currency := reqCurrency
	if currency == "" {
		currency = base.Currency
	}
	if currency == "" {
		currency = model.QuoteCurrencyDefault
	}
	validUntil := in.ValidUntil
	if validUntil == nil {
		validUntil = base.ValidUntil
	}

	now := s.now()
	nextID, _ := newQuoteKeysFromClock(now)
	next := &model.Quote{
		ID:            nextID,
		QuoteID:       quoteID,
		OpportunityID: base.OpportunityID,
		Status:        model.QuoteStatusDraft,
		Currency:      currency,
		ValidUntil:    validUntil,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.Append(ctx, base.ID, next); err != nil {
		return nil, err
	}
	return s.persistLines(ctx, next, lines, script)
}

// —— 读侧 ————————————————————————————————————————————————

// View 按版本行键读一版。
func (s *QuoteService) View(ctx context.Context, id string) (*QuoteView, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: 版本行键为空", ErrQuoteInputInvalid)
	}
	row, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("quote: 读版本行 %s 失败：%w", id, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: 行键 %s", ErrQuoteVersionMissing, id)
	}
	return s.buildView(ctx, row)
}

// ViewAt 按 (quote_id, version) 读**某一版** —— "旧版不可变"唯一能被读出来的地方。
func (s *QuoteService) ViewAt(ctx context.Context, quoteID string, version int64) (*QuoteView, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	quoteID = strings.TrimSpace(quoteID)
	if quoteID == "" {
		return nil, fmt.Errorf("%w: 报价编号为空", ErrQuoteInputInvalid)
	}
	row, err := s.store.GetVersion(ctx, quoteID, version)
	if err != nil {
		return nil, fmt.Errorf("quote: 读 %s 第 %d 版失败：%w", quoteID, version, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: %s 没有第 %d 版", ErrQuoteVersionMissing, quoteID, version)
	}
	return s.buildView(ctx, row)
}

// LatestView 读链上版本号最大的那一版（"现在报给客户的是哪一版"）。
func (s *QuoteService) LatestView(ctx context.Context, quoteID string) (*QuoteView, error) {
	if !s.Available() {
		return nil, ErrQuoteServiceUnavailable
	}
	quoteID = strings.TrimSpace(quoteID)
	if quoteID == "" {
		return nil, fmt.Errorf("%w: 报价编号为空", ErrQuoteInputInvalid)
	}
	row, err := s.store.Latest(ctx, quoteID)
	if err != nil {
		return nil, fmt.Errorf("quote: 读链 %s 失败：%w", quoteID, err)
	}
	if row == nil {
		return nil, fmt.Errorf("%w: 链 %s 不存在", ErrQuoteVersionMissing, quoteID)
	}
	return s.buildView(ctx, row)
}

// buildView 从**库里的行**算出视图与两个合计。
//
// AC③ 的判据是"服务报的数 == 库里那些行相加"，而不是"服务报的数 == 服务自己算的数"：
// 合计若从内存那份带出来，库里少写一行、多写一成都读不出来。
func (s *QuoteService) buildView(ctx context.Context, row *model.Quote) (*QuoteView, error) {
	items, err := s.store.ListLines(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("quote: 读 %s 的行项目失败：%w", row.ID, err)
	}
	view := &QuoteView{
		ID: row.ID, QuoteID: row.QuoteID, OpportunityID: row.OpportunityID,
		Version: row.Version, Status: row.Status, Currency: row.Currency,
		ValidUntil: row.ValidUntil, SourceID: row.SourceID,
		Lines: make([]QuoteLineView, 0, len(items)),
	}
	for _, it := range items {
		gross := quoteRound2(it.Quantity * it.UnitPrice)
		view.Lines = append(view.Lines, QuoteLineView{
			LineNo: it.LineNo, ProductID: it.ProductID, Title: it.Title,
			Quantity: it.Quantity, UnitPrice: it.UnitPrice,
			DiscountPercent: it.DiscountPercent, Gross: gross, Amount: it.Amount,
		})
		view.GrossTotal += gross
		view.Total += it.Amount
	}
	// 累加本身会带进 float 误差（三行相加能差出 1.8e-9），而"总价"是一位小数都不该
	// 带尾巴的数：交出去之前把这份误差收掉。收的是**累加误差**，不是任何数值判断 ——
	// 每行早已在落库前各自舍到分，这里加的就是那些分。
	view.GrossTotal = quoteRound2(view.GrossTotal)
	view.Total = quoteRound2(view.Total)
	return view, nil
}

// persistLines 把合并好的行落到刚落库的那一版上，然后**读回来**出视图。
func (s *QuoteService) persistLines(ctx context.Context, row *model.Quote, lines []quoteLine, script QuoteScript) (*QuoteView, error) {
	rows := make([]*model.QuoteLineItem, 0, len(lines))
	for _, l := range lines {
		rows = append(rows, &model.QuoteLineItem{
			QuoteRowID: row.ID, LineNo: l.lineNo, ProductID: l.productID, Title: l.title,
			Quantity: l.quantity, UnitPrice: l.unitPrice,
			DiscountPercent: l.disc, Amount: quoteLineAmount(l),
		})
	}
	if err := s.store.AddLines(ctx, row.ID, rows); err != nil {
		// 版本行已经在库里而明细没了：这一版读出来是"没有行、合计 0"，
		// 一个看起来完全合理而全是错的数字。报得越难听越好，且原始错因必须能被
		// 调用方 errors.Is 分到 —— 一次可重试的抖动与一次数据事故不能长同一个样。
		return nil, fmt.Errorf("quote: 版本行 %s 已落库但行项目没跟上（%w）⇒ 该版合计读出来是 0，必须人工处置，不得当成可发对象", row.ID, err)
	}
	view, err := s.buildView(ctx, row)
	if err != nil {
		return nil, err
	}
	kept := script
	view.Script = &kept
	return view, nil
}

// —— 闸门 / 话术 / 模板 ————————————————————————————————————————————

// checkGate 域开关（ltc.config 的 quote 阶段）。关着的三种形状各报各的原因。
func (s *QuoteService) checkGate(ctx context.Context) error {
	cfg := s.gate.Config(ctx)
	// StageActive 的接收者可为 nil（nil 那份按降级），所以这里不用再判空。
	active, reason := cfg.StageActive(LTCStageQuote)
	if active {
		return nil
	}
	detail := reason
	if cfg != nil && cfg.DegradeReason != "" {
		detail += "：" + cfg.DegradeReason
	}
	return fmt.Errorf("%w: 拦下原因 %s ⇒ 一行不写", ErrQuoteGateClosed, detail)
}

// resolveScript 取配置里那个分片的生效正文（AC① 的必经之路）。
func (s *QuoteService) resolveScript(ctx context.Context, oneID string) (QuoteScript, error) {
	raw, err := s.cfg.Get(ctx, QuoteScriptIDKVKey)
	if err != nil {
		return QuoteScript{}, fmt.Errorf("quote: 读报价话术指针 %s 失败：%w", QuoteScriptIDKVKey, err)
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		return QuoteScript{}, fmt.Errorf("%w: 配置项 %s 未登记 ⇒ 报价封面没有可引用的分片",
			ErrQuoteScriptUnavailable, QuoteScriptIDKVKey)
	}
	id64, err := strconv.ParseUint(text, 10, 64)
	if err != nil || id64 == 0 {
		return QuoteScript{}, fmt.Errorf("%w: 配置项 %s 的值 %q 不是话术分片 ID（它得是 script_library.id 那个正整数，不能按 0 号往下走）",
			ErrQuoteScriptUnavailable, QuoteScriptIDKVKey, raw)
	}
	script, err := s.scripts.ActiveQuoteScript(ctx, uint(id64), oneID)
	if err != nil {
		return QuoteScript{}, err
	}
	if strings.TrimSpace(script.Content) == "" {
		return QuoteScript{}, fmt.Errorf("%w: 分片 %d 的第 %d 版快照正文为空",
			ErrQuoteScriptUnavailable, script.ScriptID, script.Version)
	}
	return script, nil
}

// quoteTemplateLine 模板里的一行。
type quoteTemplateLine struct {
	ProductID       string  `json:"product_id"`
	Title           string  `json:"title"`
	Quantity        float64 `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	DiscountPercent float64 `json:"discount_percent"`
}

// quoteTemplate 报价模板。name 那一格刻意不接：它落不进任何列（quotes 没有标题列），
// 读进来再丢掉只会让人以为它进了库。
type quoteTemplate struct {
	Currency  string              `json:"currency"`
	ValidDays int                 `json:"valid_days"`
	Lines     []quoteTemplateLine `json:"lines"`
}

// loadTemplate 读并校验模板。三种坏法分得开：没配 / 配坏 / 读不到。
func (s *QuoteService) loadTemplate(ctx context.Context, code string) (quoteTemplate, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return quoteTemplate{}, fmt.Errorf("%w: 模板代号为空（报价模板按代号取，没有「默认模板」这一格）", ErrQuoteInputInvalid)
	}
	key := QuoteTemplateKVPrefix + code
	raw, err := s.cfg.Get(ctx, key)
	if err != nil {
		// 不能报成"未配置"：那会把一次存储抖动读成一次运营缺失，
		// 而运营对着一个本来就对的配置改第三遍。
		return quoteTemplate{}, fmt.Errorf("quote: 读报价模板 %s 失败：%w", key, err)
	}
	if strings.TrimSpace(raw) == "" {
		return quoteTemplate{}, fmt.Errorf("%w: %s", ErrQuoteTemplateMissing, key)
	}
	var tpl quoteTemplate
	if err := json.Unmarshal([]byte(raw), &tpl); err != nil {
		return quoteTemplate{}, fmt.Errorf("%w: %s 不是合法 JSON（%v）", ErrQuoteTemplateInvalid, key, err)
	}
	return tpl, s.validateTemplate(key, tpl)
}

func (s *QuoteService) validateTemplate(key string, tpl quoteTemplate) error {
	if _, err := normalizeQuoteCurrency(tpl.Currency); err != nil {
		return fmt.Errorf("%w: %s 的 currency %q 不是三位 ISO 4217 码（它是 varchar(3)，塞长一点的字符串在写库时才炸）",
			ErrQuoteTemplateInvalid, key, tpl.Currency)
	}
	if tpl.ValidDays < 0 {
		return fmt.Errorf("%w: %s 的 valid_days=%d 为负（推出来的有效期在过去）",
			ErrQuoteTemplateInvalid, key, tpl.ValidDays)
	}
	if len(tpl.Lines) == 0 {
		return fmt.Errorf("%w: %s 一行都没有（空模板生成出来的是一张 0 元的报价单，那是个数字而不是个故障）",
			ErrQuoteTemplateInvalid, key)
	}
	for i, l := range tpl.Lines {
		if strings.TrimSpace(l.ProductID) == "" {
			return fmt.Errorf("%w: %s 第 %d 行没有 product_id（覆盖项按它匹配，没有编号的行谁也覆盖不了）",
				ErrQuoteTemplateInvalid, key, i+1)
		}
		// 同一品项**允许**出现两次（一个 SKU 两种数量是真实价目表），所以这里不查重；
		// 代价是覆盖项按 product_id 命中时只能有确定答案 —— 取第一条，见 mergeQuoteLines。
		if err := checkQuoteLineShape(l.ProductID, l.Title, l.Quantity, l.UnitPrice, l.DiscountPercent); err != nil {
			return fmt.Errorf("%w: %s 第 %d 行 —— %v", ErrQuoteTemplateInvalid, key, i+1, err)
		}
	}
	return nil
}

// —— 行项目：合并与算法 ——————————————————————————————————————————————

// quoteLine 待写入的一行（内容列，不含行净额）。
type quoteLine struct {
	lineNo    int64
	productID string
	title     string
	quantity  float64
	unitPrice float64
	disc      float64
}

func quoteLinesFromRows(rows []*model.QuoteLineItem) []quoteLine {
	out := make([]quoteLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, quoteLine{lineNo: r.LineNo, productID: r.ProductID, title: r.Title,
			quantity: r.Quantity, unitPrice: r.UnitPrice, disc: r.DiscountPercent})
	}
	return out
}

// mergeQuoteLines 把覆盖项套到起点行上。
//
// 起点在 Generate 是模板行、在 Revise 是基准版的持久行 —— 两条路的合并规则只有一份，
// 因为「还一次价」与「第一次出」在数值上不该是两套算法（两套迟早漂，而漂的那一分
// 会出现在客户手里的报价单上）。
//
// 覆盖项按 product_id 命中**第一条**同品项的行：一份模板可以有同一个 SKU 的两行
// （两种数量），此时"改哪一行"必须由行号才能表达，而调用方今天没有行号这个入口 ——
// 与其留一个说不清的语义，不如把"只改第一条"写死在这里，将来要改第二行就加 line_no。
func mergeQuoteLines(base []quoteLine, over []QuoteLineInput) ([]quoteLine, error) {
	lines := make([]quoteLine, len(base))
	copy(lines, base)
	nextNo := int64(0)
	for _, l := range lines {
		if l.lineNo > nextNo {
			nextNo = l.lineNo
		}
	}
	for _, o := range over {
		pid := strings.TrimSpace(o.ProductID)
		if pid == "" {
			return nil, fmt.Errorf("%w: 覆盖行必须给出品项编号，否则不知道覆盖哪一行", ErrQuoteInputInvalid)
		}
		title, qty, unit, disc := "", 0.0, 0.0, 0.0
		hit := -1
		for i := range lines {
			if lines[i].productID == pid {
				hit = i
				break
			}
		}
		if hit >= 0 {
			title, qty, unit, disc = lines[hit].title, lines[hit].quantity, lines[hit].unitPrice, lines[hit].disc
		}
		if hit < 0 {
			// 追加行没有可继承的起点，本仓也没有商品目录可查：三项齐了才写得出来。
			// 缺单价静默按 0 落，等于在客户的那张单上多出一行免费的东西而不报错。
			// （折扣不在这一列：没给就是不打折，那是一个有唯一答案的默认。）
			var missing []string
			if o.Title == nil {
				missing = append(missing, "title")
			}
			if o.Quantity == nil {
				missing = append(missing, "quantity")
			}
			if o.UnitPrice == nil {
				missing = append(missing, "unit_price")
			}
			if len(missing) > 0 {
				return nil, fmt.Errorf("%w: 追加行 %s 缺 %s —— 它不在起点行里，没有可继承的值",
					ErrQuoteInputInvalid, pid, strings.Join(missing, "、"))
			}
		}
		if o.Title != nil {
			title = *o.Title
		}
		if o.Quantity != nil {
			qty = *o.Quantity
		}
		if o.UnitPrice != nil {
			unit = *o.UnitPrice
		}
		if o.DiscountPercent != nil {
			disc = *o.DiscountPercent
		}
		if err := checkQuoteLineShape(pid, title, qty, unit, disc); err != nil {
			return nil, err
		}
		if hit >= 0 {
			lines[hit] = quoteLine{lineNo: lines[hit].lineNo, productID: pid, title: title,
				quantity: qty, unitPrice: unit, disc: disc}
			continue
		}
		nextNo++
		lines = append(lines, quoteLine{lineNo: nextNo, productID: pid, title: title,
			quantity: qty, unitPrice: unit, disc: disc})
	}
	return lines, nil
}

// 三列的列宽上界（抄自 internal/model/quote.go 的 gorm tag）：quantity numeric(12,2)
// 能表达到 9 999 999 999.99，unit_price 与 amount numeric(14,2) 到 999 999 999 999.99。
//
// 界必须在这一层判，不能留给 PG：越界的写法照样能落库**到一半** —— Create 先成功、
// AddLines 被 22003 numeric field overflow 拒掉，于是链上留下一行"0 明细、合计 0"的
// 孤儿版本行，报回来的还是"必须人工处置"那一句（实测）。那既不是入参错的样子,
// 也不是本层承诺的"三处失败面一行都不写"。
const (
	quoteMaxQuantity = 1e10
	quoteMaxMoney    = 1e12
)

// checkQuoteLineShape 一行的值域。
//
// 折扣的 NaN 走的是同一支：两个比较都为 false，取反后仍为 false ⇒ 判越界。
// 数量与单价的上界同样用「!(x < 界)」这一形状：NaN 与 ±Inf 与任何数比较都是 false，
// 于是它们与天文数字一起落在同一臂里，不需要另开判据（+Inf 单看下界是过得起的）。
// 数量必须为正：0 数量的行在报价单上是一行"什么都没有而占一行"的条目，
// 而模板侧那臂要的就是它报错而不是静默出一张 0 元行。
func checkQuoteLineShape(pid, title string, qty, unit, disc float64) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("%w: 品项 %s 没有品名（报价单上会出现一行不知道是什么的东西）", ErrQuoteInputInvalid, pid)
	}
	if !(qty > 0 && qty < quoteMaxQuantity) {
		return fmt.Errorf("%w: 品项 %s 的数量 %v 不在 numeric(12,2) 的范围内（要 0 < 数量 < %.0f，NaN 与 ±Inf 同样落这一臂）",
			ErrQuoteInputInvalid, pid, qty, quoteMaxQuantity)
	}
	if !(unit >= 0 && unit < quoteMaxMoney) {
		return fmt.Errorf("%w: 品项 %s 的单价 %v 不在 numeric(14,2) 的范围内（要 0 ≤ 单价 < %.0f）",
			ErrQuoteInputInvalid, pid, unit, quoteMaxMoney)
	}
	if !(disc >= 0 && disc <= 100) {
		return fmt.Errorf("%w: 品项 %s 的折扣 %v 越出 0–100（它是百分数，不是 0–1 的小数）", ErrQuoteInputInvalid, pid, disc)
	}
	// 乘积要单独判：数量与单价**各自**合规时，行净额仍可出列（1e7 在 12,2 内、
	// 1e6 在 14,2 内，乘积 1e13 已经超了 amount 那一列）。判的是落库的那个值。
	if amount := qty * unit * (1 - disc/100); !(amount < quoteMaxMoney) {
		return fmt.Errorf("%w: 品项 %s 的行净额超出 numeric(14,2)（%v × %v 折去 %v%% = %v，上限 %.0f）—— 数量和单价各自都没超",
			ErrQuoteInputInvalid, pid, qty, unit, disc, amount, quoteMaxMoney)
	}
	return nil
}

// quoteRound2 舍到分，半分向上 —— 按**十进制**向上，不是按二进制。
//
// 不复用商机侧的任何金额函数：那一边算的是"这单值多少钱"的量级估算，
// 而这一边算的是要印在报价单上的那一位数。
//
// 为什么不写成 math.Round(v*100)/100：0.5×1.15 的真值是 0.575，float64 里存的是
// 0.57499999999999995551，乘 100 落在 57.49999… ⇒ 舍出 0.57；而同一笔数交给
// 库里的 numeric(14,2) 是 0.58（PG 按十进制取舍，实测）。半分那一格按二进制就是
// **向下**，与本页写死的「半分向上」自相矛盾，差的那一分落在客户那张单子上。
// 所以先把这个 float64 打成它最短的十进制表示（也就是录进来那位数），再离零取整。
func quoteRound2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	cents, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', -1, 64))
	if !ok {
		return v
	}
	cents.Mul(cents, big.NewRat(100, 1))
	num, den := cents.Num(), cents.Denom() // Denom() > 0
	// 离零取整：floor((2·|num| + den) / 2·den)，再套回符号。
	half := new(big.Int).Lsh(den, 1)
	rounded := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Lsh(new(big.Int).Abs(num), 1), den), half)
	if num.Sign() < 0 {
		rounded.Neg(rounded)
	}
	return float64(rounded.Int64()) / 100
}

// quoteLineAmount 行净额 = 数量 × 单价 × (1 − 折扣/100)，**逐行**舍到分。
//
// 关键在"逐行"：合计是那一分一分舍完之后的行相加（AC③）。先加总再舍一次会差一分，
// 而"这张报价的总价是多少"必须只有一个答案 —— 答案是库里那些行相加。
// 100% 折扣合法（赠送行），净额 0.00 而不是 NULL。
func quoteLineAmount(l quoteLine) float64 {
	return quoteRound2(l.quantity * l.unitPrice * (1 - l.disc/100))
}

// —— 币种 / 有效期 ————————————————————————————————————————————————

// normalizeQuoteCurrency 规范化并校验 ISO 4217 三位码。
//
// 只做大小写规范化、不做别的：它是对外码位不是自由文本。放行 "USDT"/"C N"/"人民币"
// 的那一列是 varchar(3) —— 在写库时才炸，而那时已经错过了能给出"哪一格写错了"的时机。
// 空串合法（= 没给，由上层继续兜模板与建表默认）。
func normalizeQuoteCurrency(text string) (string, error) {
	cur := strings.ToUpper(strings.TrimSpace(text))
	if cur == "" {
		return "", nil
	}
	if len(cur) != 3 {
		return "", fmt.Errorf("%w: 币种 %q 不是三位码（本列宽 varchar(3)）", ErrQuoteInputInvalid, text)
	}
	for i := 0; i < len(cur); i++ {
		if cur[i] < 'A' || cur[i] > 'Z' {
			return "", fmt.Errorf("%w: 币种 %q 含非字母字符", ErrQuoteInputInvalid, text)
		}
	}
	return cur, nil
}

// resolveValidUntil 有效期三级：请求显式 > 模板 valid_days > 不设（NULL）。
//
// 不设必须是 NULL 而不是零值时间 —— 0001-01-01 会被"早已过期"的读侧吞掉，
// 而那与"还没定价到什么时候"是两件事（同 quotes.valid_until 可空的那条判据）。
func (s *QuoteService) resolveValidUntil(explicit *time.Time, validDays int) (*time.Time, error) {
	if explicit != nil {
		at := *explicit
		return &at, nil
	}
	if validDays <= 0 {
		return nil, nil
	}
	at := s.now().AddDate(0, 0, validDays)
	return &at, nil
}

// —— 主键与对外编号 ————————————————————————————————————————————————

// quoteSeq 进程内单调计数器。
//
// 与商机/审批/human_task 那三处同一口径：只取纳秒会在同一纳秒内的两次生成撞
// （一次循环里连着生成两单是真实路径），只取计数器则两个进程各自从 1 开始就撞。
var quoteSeq int64

func nextQuoteSeq() int64 { return atomic.AddInt64(&quoteSeq, 1) }

// newQuoteKeys 纯函数版生成器（同一时刻 + 同一 seq ⇒ 同一对键），用例可直接复算。
//
// ID 走 unix 纳秒（它会被抄进 quote_line_items.quote_row_id，必须多实例不撞）；
// 编号走 unix 秒的 base36（它是给人念的，越短越好）。
// **刻意不含日期串**：带日期就要选一个时区格式化，而本仓 PG 会话钉在 CST、
// Go 侧按宿主机时区读，同一时刻能生成两个"当天序号"（日期边界裂脑那条老账）。
// 长度上限：id = 2+19+1+13 = 35，编号 = 3+11+1+13 = 28，都 < 64（两列同宽）。
func newQuoteKeys(now time.Time, seq int64) (id, code string) {
	return fmt.Sprintf("q_%d_%d", now.UnixNano(), seq),
		fmt.Sprintf("QT-%s-%s", strconv.FormatInt(now.Unix(), 36), strconv.FormatInt(seq, 36))
}

func newQuoteKeysFromClock(now time.Time) (string, string) {
	return newQuoteKeys(now, nextQuoteSeq())
}

// —— 话术生效版本解析器（AC① 的那一侧）————————————————————————————————

// QuoteScriptSource 把「哪个分片 + 给谁」解析成「生效版本的正文 + 落哪个桶」。
//
// 正文**只**取自 script_versions 的快照：script_library.content 那一列是编辑区，
// 谁都能改。"没有快照就先用可变列"这条退路是本函数唯一拒绝的东西 ——
// CreateVersion 之前的日常编辑恰好就是"指针有、快照无"的状态，
// 走退路等于把一份未经版本化的正文发给客户，而库里那行看起来完全正常。
type QuoteScriptSource struct {
	repo *repository.ScriptLibraryRepository
	ab   *ScriptABService
	now  func() time.Time
}

// NewQuoteScriptSource 构造。ab 为 nil 时分桶退化为空桶（正文照常给：
// 分桶是归因用的，不是内容的判据）。
func NewQuoteScriptSource(repo *repository.ScriptLibraryRepository, ab *ScriptABService,
	now func() time.Time) *QuoteScriptSource {
	if now == nil {
		now = time.Now
	}
	return &QuoteScriptSource{repo: repo, ab: ab, now: now}
}

// ActiveQuoteScript 取分片的生效版本。
//
// 四条不可用：分片不存在、status/时间已下线、生效指针没有快照、读失败。
// 前三条统一回 ErrQuoteScriptUnavailable（调用方的处置动作是同一个：别出报价），
// 读失败单独上抛（那是去修存储不是去改配置）。
//
// 状态用「” 或 active 才算活着」，与 ListObjectionTemplates 那道既有过滤同口径：
// 存量行里有历史遗留的空串，把它判死会让运营没动过的话术突然全灭。
func (src *QuoteScriptSource) ActiveQuoteScript(ctx context.Context, scriptID uint, oneID string) (QuoteScript, error) {
	if src == nil || src.repo == nil {
		return QuoteScript{}, errors.New("quote script source: 未装配话术库仓储")
	}
	var sc model.ScriptLibrary
	if err := src.repo.FirstScriptByID(ctx, scriptID, &sc); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return QuoteScript{}, fmt.Errorf("%w: 分片 %d 不存在", ErrQuoteScriptUnavailable, scriptID)
		}
		return QuoteScript{}, fmt.Errorf("quote: 读话术分片 %d 失败：%w", scriptID, err)
	}
	if sc.Status != "" && sc.Status != "active" {
		return QuoteScript{}, fmt.Errorf("%w: 分片 %d 的当前状态是 %q，运营已下线",
			ErrQuoteScriptUnavailable, scriptID, sc.Status)
	}
	if sc.ExpiresAt != nil && !src.now().Before(*sc.ExpiresAt) {
		return QuoteScript{}, fmt.Errorf("%w: 分片 %d 已过有效期（%s）",
			ErrQuoteScriptUnavailable, scriptID, sc.ExpiresAt.Format(time.RFC3339))
	}
	if sc.Version <= 0 {
		return QuoteScript{}, fmt.Errorf("%w: 分片 %d 没有生效版本指针（version=%d），先在话术库发布一个版本",
			ErrQuoteScriptUnavailable, scriptID, sc.Version)
	}
	var ver model.ScriptVersion
	if err := src.repo.FirstVersionByID(ctx, scriptID, sc.Version, &ver); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return QuoteScript{}, fmt.Errorf("%w: 分片 %d 的生效指针停在第 %d 版而那一版没有快照 —— 可变列那份内容不算版本，不能拿来顶",
				ErrQuoteScriptUnavailable, scriptID, sc.Version)
		}
		return QuoteScript{}, fmt.Errorf("quote: 读分片 %d 第 %d 版快照失败：%w", scriptID, sc.Version, err)
	}
	out := QuoteScript{ScriptID: scriptID, Version: ver.Version, Content: ver.Content}
	if strings.TrimSpace(oneID) != "" && src.ab != nil {
		// 分桶调的就是 ScriptABService.AssignBucket 那一个函数：报价侧再算一遍，
		// 明天 SplitA 语义一改就会分成两拨人群，而 AB 报表读不出差异来源。
		out.Bucket = src.ab.AssignBucket(scriptID, oneID, src.ab.GetConfig(ctx, scriptID))
	}
	return out, nil
}
