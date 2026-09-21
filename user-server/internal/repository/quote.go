// quote.go 报价版本链仓储（T-P6-01 / N-5 报价域的第二层）
//
// 本文件是 service（T-P6-02）与 quotes / quote_line_items 之间唯一的一层，
// 它只负责三件事：**把版本链的走向算对**、**把旧版守住不被改写**、
// **按已命名的键把行读出来**。三条各自对应一张 AC，判据落在 quote_test.go。
//
// 「版本只能由这层递增」是本层存在的理由。若把 version 交给调用方写：
// 客户还一次价，service 读回最新版、加一、插回去 —— 读与写之间另一个人也在还价，
// 两个人算出同一个版本号，第二个撞上库级复合唯一索引（那条索引在 internal/pkg/db
// 的建表用例里钉着）。这里的关键次序是：**先由库裁决，再由本层把 23505 翻成
// ErrQuoteVersionConflict**，而不是反过来在本层加进程内锁 —— 锁在多实例部署下
// 只管得住半个链，而 AC① 要的是"哪一版是谁"这件事在任何部署下都有唯一答案。
//
// AC②（铁律：仓储无业务判断）在本层的具体形状：不校验 status 值域、不判断跃迁
// 是否合法、不看折扣与金额、不读 ltc.config 的阈值。值域在 model（QuoteStatusKnown），
// 跃迁表在 T-P4-03 的同伴卡（报价的跃迁归 T-P6-03 的闸门），阈值在配置。
// 反面证明由 `TestQuoteRepo_DoesNotValidate` 钉住：它红了意味着有人在这层加了校验，
// 修法是把校验搬去 service，不是改掉用例。
//
// 「旧版不可变」在本层不是约定而是形状的结果：接口里没有任何能改写已存在版本行
// 内容列的方法（唯一的写方法是 UpdateStatus，它的写集合只有 status + updated_at），
// 明细只能 Add 不能改。这条由 `TestQuoteRepo_InterfaceHasNoContentRewriter` 钉住。
//
// 本卡刻意**没有内存版底座**（同 T-P4-02 的商机表）：一旦有内存影子，
// "这一版已经被别人追加过了"就永远测不出来。句柄不可用 ⇒ 每个方法明确报错。
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// ErrQuoteVersionConflict (quote_id, version) 已被占用。
//
// 单独成 sentinel 的理由与商机侧的编号冲突同族但更硬：service 对它的反应是
// "有人抢先追加了一版，重读最新再决定"，而对别的 23505（主键、非本索引）的反应是
// "这次提交本身重复了，别再重试"。混成一个 error 就只能二选一地错。
var ErrQuoteVersionConflict = errors.New("quote: 该版本号已被占用（同一条报价链的同一版本只能有一行），请基于最新版重试")

// ErrQuoteVersionReserved 调用方自带版本号。
//
// 这不是输入洁癖，是 AC② 的落点：谁都能声明"我是第三版"，链就不再是链。
// 报错而不是静默改写成 base+1 —— 带着版本号的调用方说明它以为自己在做另一件事
// （覆盖），那件事本层不支持，静默换算只会把误会留到线上。
var ErrQuoteVersionReserved = errors.New("quote: 版本号与来路只能由仓储写（第一版走 Create，后续版走 Append）")

// ErrQuoteStatusConflict 手里那个"它现在是什么状态"已经过期，本次跃迁没有落库。
//
// 与 ErrQuoteNotFound 必须分得开：前者该重读再跃迁，后者重读多少次都没有。
var ErrQuoteStatusConflict = errors.New("quote: 报价当前状态与跃迁起点不符，本次跃迁没有落库")

// ErrQuoteNotFound 行不存在（读侧回 (nil, nil)，写侧回本 sentinel）。
var ErrQuoteNotFound = errors.New("quote: 报价版本行不存在")

// QuoteRepository 报价版本链读写接口。
//
// 写路径只有三条：Create（第一版）、Append（追加下一版）、UpdateStatus（生命周期）。
// 没有 Delete、没有改内容列的方法 —— 作废一版靠追加新内容与状态跃迁表达，
// 不靠抹掉历史（LTC-12 要的正是"客户还了三次价，每一次报的是什么"）。
type QuoteRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Create 落一条报价链的**第一版**：调用方留空 Version，本层补成
	// model.QuoteVersionFirst 并把 SourceID 清空（第一版没有来路）。
	// 自带非第一版版本号 ⇒ ErrQuoteVersionReserved；撞 (quote_id, 1) ⇒ ErrQuoteVersionConflict。
	Create(ctx context.Context, q *model.Quote) error

	// Append 以 baseID 那一版为基准追加下一版：Version = 基准 +1、SourceID = baseID，
	// 两者都由本层写。next 的内容列（含 Status）由调用方给，本层不复制也不校验。
	//
	// 行项目**不跟着复制**：新版该带哪些行是报价生成（T-P6-02）的判据，
	// 在这里复制一份就等于替那张卡定了"还价不改行"这个默认值。
	//
	// 基准行不存在 ⇒ ErrQuoteNotFound；基准与 next 不同链（quote_id 或商机归属不符）
	// ⇒ 明确报错；基准不是最新版时算出的版本号已被占 ⇒ ErrQuoteVersionConflict，
	// 由库级索引裁决（见文件头）。
	Append(ctx context.Context, baseID string, next *model.Quote) error

	// UpdateStatus 生命周期跃迁：`WHERE id = ? AND status = ?` 命中才生效，
	// 写集合只有 status + updated_at（version 不动：改状态不产生新的一版）。
	UpdateStatus(ctx context.Context, id, from, to string) error

	// AddLines 给某一版批量落行项目：先探存在性再写，整批同事务（要么全进要么全不进）。
	// 每条的 QuoteRowID 由本层落为 quoteRowID；自带别的行 ID ⇒ 整批拒。
	AddLines(ctx context.Context, quoteRowID string, lines []*model.QuoteLineItem) error

	// GetByID 按版本行主键读取；不存在返回 (nil, nil)，读失败返回 error。
	GetByID(ctx context.Context, id string) (*model.Quote, error)

	// GetVersion 按 (quote_id, version) 精确取某一版 —— "旧版不可变"唯一能被读出来的地方。
	// 不存在返回 (nil, nil)。
	GetVersion(ctx context.Context, quoteID string, version int64) (*model.Quote, error)

	// Latest 取版本号最大的那一版。
	//
	// 选版凭 version 而不是 created_at：补录一版旧内容时两者会分家，
	// 而"当前报给客户的是哪一版"只能有一个答案。
	Latest(ctx context.Context, quoteID string) (*model.Quote, error)

	// ListVersions 按版本升序列出整条链（谈判过程按当时顺序重放）。
	//
	// 刻意不分页：链长由谈判轮次决定（个位数），给它加 limit/offset 只会让人
	// 以为"可以拿它做全站扫描"。
	ListVersions(ctx context.Context, quoteID string) ([]*model.Quote, error)

	// ListLines 按行号升序列出某版的行项目（合计与前端行序都靠这个次序）。
	ListLines(ctx context.Context, quoteRowID string) ([]*model.QuoteLineItem, error)
}

type quoteRepo struct {
	db *gorm.DB
}

// NewQuoteRepository 创建实例（用全局 DB）
func NewQuoteRepository() QuoteRepository {
	return &quoteRepo{db: _db.GetDB()}
}

// NewQuoteRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）
func NewQuoteRepositoryWithDB(db *gorm.DB) QuoteRepository {
	return &quoteRepo{db: db}
}

func (r *quoteRepo) Available() bool { return r != nil && r.db != nil }

func (r *quoteRepo) require() error {
	if !r.Available() {
		return errors.New("quote repository: db handle is nil")
	}
	return nil
}

func (r *quoteRepo) Create(ctx context.Context, q *model.Quote) error {
	if err := r.require(); err != nil {
		return err
	}
	if q == nil || q.ID == "" {
		return errors.New("quote repository: 空记录或空行 ID（行 ID 会被明细表抄走，空串无法引用也无法排除）")
	}
	if strings.TrimSpace(q.QuoteID) == "" {
		return errors.New("quote repository: 空 quote_id 不是合法身份（它决定这一版挂在哪条链上）")
	}
	if strings.TrimSpace(q.OpportunityID) == "" {
		return errors.New("quote repository: 空 opportunity_id 不是合法身份（报价必从商机长出，空串会让它在下游按商机聚合时凭空消失）")
	}
	if q.Version != 0 && q.Version != model.QuoteVersionFirst {
		return ErrQuoteVersionReserved
	}
	// 两个链字段由本层定死：Create 只产第一版、且第一版没有来路。
	// SourceID 是**覆盖**而不是拒绝 —— 调用方在这里填什么都不改变"它是第一版"这个事实，
	// 而 Version 一旦不自带就直接报错：那说明调用方想做的是另一件事（覆盖），必须让它撞上说法。
	q.Version = model.QuoteVersionFirst
	q.SourceID = ""
	if err := r.db.WithContext(ctx).Create(q).Error; err != nil {
		if isQuoteVersionConflict(err) {
			return ErrQuoteVersionConflict
		}
		return err
	}
	return nil
}

func (r *quoteRepo) Append(ctx context.Context, baseID string, next *model.Quote) error {
	if err := r.require(); err != nil {
		return err
	}
	if next == nil || next.ID == "" {
		return errors.New("quote repository: 空记录或空行 ID")
	}
	if next.Version != 0 {
		return ErrQuoteVersionReserved
	}
	if strings.TrimSpace(next.QuoteID) == "" {
		return errors.New("quote repository: 空 quote_id 不是合法身份")
	}
	base, err := r.GetByID(ctx, baseID)
	if err != nil {
		return err
	}
	if base == nil {
		return ErrQuoteNotFound
	}
	// 两条同链判据：跨链追加的坏法是静默的 —— 两条链各自都"看起来完整"，
	// 只是各自的版本历史里少了一格，而没人知道少了哪一格。
	if next.QuoteID != base.QuoteID {
		return errors.New("quote repository: 基准行与目标行不同链（quote_id 不符）")
	}
	if next.OpportunityID != base.OpportunityID {
		return errors.New("quote repository: 同一条链的商机归属恒定（要换商机只能另起一条 quote_id）")
	}
	// 版本号从**基准行**读出来递增，不读 next 那份也不查"链上最大版本"：
	// 基准是谁由调用方明说，算出的目标号若已被占（有人抢先从同一基准追加了）
	// 就交给库裁决 —— 这正是 AC① 要的失败形状。
	next.Version = base.Version + 1
	next.SourceID = base.ID
	if err := r.db.WithContext(ctx).Create(next).Error; err != nil {
		if isQuoteVersionConflict(err) {
			return ErrQuoteVersionConflict
		}
		return err
	}
	return nil
}

// isQuoteVersionConflict 判定"撞的是版本链的唯一索引还是别的约束"。
//
// SQLSTATE 与索引名**两个条件都要命中**：把 quotes 的主键冲突也读成"版本号撞了"，
// service 会换一个行 ID 再试一次，于是一次重复提交变成链上多一版。
// 与商机/审批侧同理，不认 gorm.ErrDuplicatedKey：本仓连接没开 TranslateError，
// 这里拿到的是底层 *pgconn.PgError（错误串含 23505 与索引名）。
func isQuoteVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, quoteVersionIndexName)
}

// quoteVersionIndexName 与 model.Quote 上那两处 uniqueIndex 标签同源。
//
// 写在这里而不是就地抄字符串：索引名一改（哪怕只是大小写），
// 冲突判定会静默失效而把 ErrQuoteVersionConflict 变成裸驱动错误 —— 建表用例
// （internal/pkg/db/quote_migration_test.go）钉的是库里的实际形状，这里钉的是读它的这一处。
const quoteVersionIndexName = "uq_quotes_quote_version"

// UpdateStatus 一条 CAS 语句完成跃迁，返回三种结果之一（成功 / 起点过期 / 行不存在）。
//
// 写集合只有两列，其余一律改不动，各自对应一处真实破坏：
//   - id / quote_id / version / source_id：版本行的身份与链走向。改它们等于把一版
//     历史挪到另一条链上、或者冒充最新版本；
//   - opportunity_id：下游（sales_events、账单）按它抄走引用，改它 = 让历史指向别的商机；
//   - valid_until / currency：报价内容的一部分。有效期的改动属于"重新出一版"
//     （延长期限是给客户的又一个承诺），走 Append 而不是这里；
//   - created_at：改史；
//   - updated_at：由这层落，不取调用方那份 —— 它是"多久没动过"的唯一凭据。
//
// version **不动**：version 标识的是内容那一版，状态跃迁不产生新内容。
// 若这里顺手 +1，链上就会出现"没人见过第二次报价、却凭空多了个 v2"。
//
// 用 map 而不是 struct 写：struct 形式的 Updates 默认跳过零值字段，
// 本方法只写两列且都非零，看起来无所谓 —— 但 map 是**白名单**这件事本身要写在形状上，
// 将来有人加第三列时会在这里被看到，而 struct 加字段不需要碰这一段。
func (r *quoteRepo) UpdateStatus(ctx context.Context, id, from, to string) error {
	if err := r.require(); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("quote repository: 空行 ID 不是改写目标")
	}
	if from == to {
		// 零位移跃迁会返回"成功"而库里什么都没变 —— 调用方从此分不清
		// "没人改过"与"改成了同一个值"，而这两件事在审批链上代价不同。
		return errors.New("quote repository: 跃迁起点与目标相同（零位移跃迁不是跃迁）")
	}
	res := r.db.WithContext(ctx).Model(&model.Quote{}).
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
	// 0 行有两种来路，分错的代价不对称（与商机侧同一条理由）：把"起点过期"报成
	// "不存在"会让调用方放弃重试，把"不存在"报成"起点过期"会让它对一条没有的行反复重读。
	// 探测不参与并发正确性（决定权在 UPDATE 那一条），探测与改写之间行被删了
	// 也只是错个标签，不会写进任何东西。
	exists, err := r.exists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return ErrQuoteStatusConflict
	}
	return ErrQuoteNotFound
}

func (r *quoteRepo) exists(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.Quote{}).
		Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// AddLines 一批行项目的落点。
//
// 存在性探针在写入**之前**：明细指向不存在的版本行时它是一块永远读不到的孤儿，
// 而那张报价单在报表上显示为"没有行项目、合计 0"—— 不报错、不写日志，只在数字上少一截。
// 本仓不建外键（与商机/审批同一取向，理由见 model 侧注释），所以这一眼探测是唯一的一层。
//
// 整批一个事务：半截批次的合计是一个**看起来合理**的错误数字，比一个明显的失败难查得多
// （T-P6-02 AC③ 要的正是"行项目金额合计与落库一致"）。
//
// 行号（line_no）是复合主键的一半，不是业务值域，所以这里只守"必须为正"：
// 0 与负数会被"按行号定位"的前端读成不存在或第一行。折扣与金额的正负不归本层管。
func (r *quoteRepo) AddLines(ctx context.Context, quoteRowID string, lines []*model.QuoteLineItem) error {
	if err := r.require(); err != nil {
		return err
	}
	quoteRowID = strings.TrimSpace(quoteRowID)
	if quoteRowID == "" {
		return errors.New("quote repository: 空 quote_row_id 不是写入目标")
	}
	if len(lines) == 0 {
		// 空批次报错而不是静默成功：静默会让上层把"这份报价有 0 个行项目"
		// 当成一个合法事实读下去。
		return errors.New("quote repository: 空批次（想什么都不写就别调用）")
	}
	for _, l := range lines {
		if l == nil {
			return errors.New("quote repository: 行项目里有 nil")
		}
		if l.LineNo <= 0 {
			return errors.New("quote repository: line_no 必须为正（它是主键的一半，0 与负数定位不到任何行）")
		}
		if l.QuoteRowID != "" && l.QuoteRowID != quoteRowID {
			return errors.New("quote repository: 行项目自带了别的版本行 ID（整批拒，不做逐条改写）")
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&model.Quote{}).Where("id = ?", quoteRowID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrQuoteNotFound
		}
		for _, l := range lines {
			l.QuoteRowID = quoteRowID
		}
		// 一次批量 INSERT：批内重复行号会被主键当场拒掉，而整条语句要么全成要么全不进，
		// 上面那个事务保证的是"这条语句之外也不留半截"（将来若拆成逐条写，原子性仍成立）。
		return tx.Create(&lines).Error
	})
}

func (r *quoteRepo) GetByID(ctx context.Context, id string) (*model.Quote, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var q model.Quote
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&q).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func (r *quoteRepo) GetVersion(ctx context.Context, quoteID string, version int64) (*model.Quote, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteID) == "" {
		return nil, errors.New("quote repository: 空 quote_id 不是查询条件")
	}
	var q model.Quote
	err := r.db.WithContext(ctx).
		Where("quote_id = ? AND version = ?", quoteID, version).Take(&q).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// Latest 用 Take 而不是 First：First 会在显式排序之外再补一段主键序，
// 而这里的排序判据是 version 单一一列（(quote_id, version) 由唯一索引保证不复，
// 故 ORDER BY version DESC 已是全序，再补主键只是掩盖"排序写错了"这件事）。
func (r *quoteRepo) Latest(ctx context.Context, quoteID string) (*model.Quote, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteID) == "" {
		return nil, errors.New("quote repository: 空 quote_id 不是查询条件")
	}
	var q model.Quote
	err := r.db.WithContext(ctx).
		Where("quote_id = ?", quoteID).Order("version DESC").Take(&q).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func (r *quoteRepo) ListVersions(ctx context.Context, quoteID string) ([]*model.Quote, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteID) == "" {
		return nil, errors.New("quote repository: 空 quote_id 不是查询条件")
	}
	var rows []*model.Quote
	if err := r.db.WithContext(ctx).
		Where("quote_id = ?", quoteID).Order("version ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListLines 排序是判据不是礼貌：合计按行号读、前端按行号排版，
// 不带 ORDER 时"读回来是什么顺序"取决于计划器，两次调用能给出两份行序。
func (r *quoteRepo) ListLines(ctx context.Context, quoteRowID string) ([]*model.QuoteLineItem, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(quoteRowID) == "" {
		return nil, errors.New("quote repository: 空 quote_row_id 不是查询条件（它命中的是「所有没主行的明细里的第一条」）")
	}
	var rows []*model.QuoteLineItem
	if err := r.db.WithContext(ctx).
		Where("quote_row_id = ?", quoteRowID).Order("line_no ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
