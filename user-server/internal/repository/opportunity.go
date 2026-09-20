// opportunity.go 商机仓储（T-P4-02 / N-1 的第二层）
//
// 五层架构里本文件是 service.OpportunityService（T-P4-03）与 opportunities 表之间
// 唯一的一层。它**只做三件事**：把行写进去、按已命名的维度把行读出来、在改写时
// 保证"后写不静默覆盖先写"（AC①）。
//
// AC②（铁律：仓储无业务判断）在本文件的具体形状是：这里**不校验** stage/status 值域、
// 不判断跃迁是否合法、不看金额范围、不读 ltc.config 的阈值。理由不是洁癖 ——
// 跃迁表与赢率算法要到 T-P4-03 才存在，若这层先写一份，两份判据迟早分家，
// 而分家之后"哪一份说了算"取决于请求先撞上哪个方法。
// 反面证明由 `TestOpportunityRepo_DoesNotValidate` 钉住：它红了意味着有人在这层加了校验，
// 修法是把校验搬去 service，不是改掉用例。
//
// 本卡刻意**没有内存版底座**（同 T-P3-01 的审批表）：商机一旦有内存影子，
// "库里那条已经被别人改到 v5"这件事就永远测不出来，而 AC① 要的正是它。
// 句柄不可用 ⇒ 每个方法明确报错，由调用方决定怎么收场。
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

// ErrOpportunityCodeConflict 对外编号已被占用（唯一索引 idx_opportunities_code）。
//
// 单独成 sentinel：service 对它的反应是"换一个编号重试"，而对主键冲突的反应是
// "这次提交重复了，别再重试"。两者混成一个 error 就只能二选一地错。
var ErrOpportunityCodeConflict = errors.New("opportunity: 对外编号已存在")

// ErrOpportunityStaleVersion 手里那份不是最新的一版，本次改写没有落库。
//
// 它是 AC① 的**交付物**而不是副作用：七个并发写者里七个都该拿到这个。
var ErrOpportunityStaleVersion = errors.New("opportunity: 该商机已被他人改写，请基于最新版本重试")

// ErrOpportunityNotFound 行不存在。与上一个必须分得开：409 该重试、404 不该。
var ErrOpportunityNotFound = errors.New("opportunity: 商机不存在")

// ErrOpportunityStatusFilterEmpty 调用方没说要按哪些状态筛。
//
// 这是一道必须存在的守卫而不是输入洁癖：本表同时装着在跑的与已收口的商机，
// "忘了传状态集"如果读成"不加过滤"，待推进列表里就会混进已赢单的行 ——
// 不报错、不写日志，只在数字上多出一截。
var ErrOpportunityStatusFilterEmpty = errors.New("opportunity: 状态过滤集为空（想看全部请显式传入全部状态）")

// OpportunityRepository 商机读写接口
type OpportunityRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Insert 落一条新商机。编号撞了返回 ErrOpportunityCodeConflict，其余冲突原样返回。
	// o.Version 必须为 0（新行的语义是"一次都没改过"），否则报错。
	Insert(ctx context.Context, o *model.Opportunity) error

	// GetByID 按主键读取；不存在返回 (nil, nil)，**读失败返回 error**。
	// 把故障读成"没有这条商机"，上层会把它当成"还没建"而重走一遍分配流程。
	GetByID(ctx context.Context, id string) (*model.Opportunity, error)

	// GetByClueID 由来源线索反查商机（T-P4-05 的幂等键）。
	// 不存在返回 (nil, nil)、读失败返回 error —— 与上一条同一条判据，但这里的代价更大：
	// 把"读不到"读成"这条线索还没转化过"，一次抖动就会变成同一线索的两行商机。
	// 空白 clueID 直接报错：本表允许 clue_id 为空（手工建的商机就是），
	// 拿空串去查会命中"所有手工商机里的第一条"，一个看着合理的错误答案。
	GetByClueID(ctx context.Context, clueID string) (*model.Opportunity, error)

	// OpenCountByOwner 每名销售当前**在办**的商机数（T-P4-05 负载均衡的输入）。
	//
	// 只数 status=open：把赢单与丢单也计进去的话，一个人业绩越好就越不会再拿到新单，
	// 那不是均衡而是惩罚业绩。无归属的行（owner_user_id 为空或 NULL）不出现在结果里，
	// 它不属于任何人的负载。
	//
	// 返回 map 里**缺席即为 0**（从没建过单的人不会出现在 GROUP BY 的结果里），
	// 调用方别把"没有这个键"当成故障或当成"这个人不存在"。
	OpenCountByOwner(ctx context.Context) (map[string]int, error)

	// Update 按乐观锁改写一行：只写上面那份 map 点名的列，且
	// `WHERE id = ? AND version = ?` 命中才生效，生效后版本 +1。
	//
	// o 必须是刚从 GetByID 拿回来的那一份（它的 Version 就是期望版本）。
	// 返回 ErrOpportunityStaleVersion = 别人先改过（调用方重读再改），
	// ErrOpportunityNotFound = 行已不在。
	Update(ctx context.Context, o *model.Opportunity) error

	// ListByCustomer 按客户倒序列出商机。statuses 为空调 ErrOpportunityStatusFilterEmpty。
	ListByCustomer(ctx context.Context, customerID string, statuses []string, limit, offset int) ([]*model.Opportunity, error)

	// ListByStage 按推进阶段列出（漏斗各格计数与格子内列表共用这一条）。
	ListByStage(ctx context.Context, stage string, statuses []string, limit, offset int) ([]*model.Opportunity, error)

	// ListByOwner 按负责销售列出（销售个人工作台）。
	ListByOwner(ctx context.Context, ownerUserID string, statuses []string, limit, offset int) ([]*model.Opportunity, error)
}

type opportunityRepo struct {
	db *gorm.DB
}

// NewOpportunityRepository 创建实例（用全局 DB）
func NewOpportunityRepository() OpportunityRepository {
	return &opportunityRepo{db: _db.GetDB()}
}

// NewOpportunityRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）
func NewOpportunityRepositoryWithDB(db *gorm.DB) OpportunityRepository {
	return &opportunityRepo{db: db}
}

func (r *opportunityRepo) Available() bool { return r != nil && r.db != nil }

func (r *opportunityRepo) require() error {
	if !r.Available() {
		return errors.New("opportunity repository: db handle is nil")
	}
	return nil
}

func (r *opportunityRepo) Insert(ctx context.Context, o *model.Opportunity) error {
	if err := r.require(); err != nil {
		return err
	}
	if o == nil || o.ID == "" {
		return errors.New("opportunity repository: 空记录或空 ID")
	}
	if o.Version != 0 {
		return errors.New("opportunity repository: 新行 version 必须为 0")
	}
	if err := r.db.WithContext(ctx).Create(o).Error; err != nil {
		if isOpportunityCodeConflict(err) {
			return ErrOpportunityCodeConflict
		}
		return err
	}
	return nil
}

// isOpportunityCodeConflict 判定"撞的是编号唯一索引还是别的约束"。
//
// SQLSTATE 与索引名**两个条件都要命中**：把主键冲突也读成"编号撞了"，service 会
// 去换编号重试一次，于是一次重复提交变成两行商机。
// 与 approval 侧同理，不认 gorm.ErrDuplicatedKey：本仓连接没开 TranslateError，
// 这里拿到的是底层 *pgconn.PgError（错误串含 23505 与索引名）。
func isOpportunityCodeConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "idx_opportunities_code")
}

func (r *opportunityRepo) GetByID(ctx context.Context, id string) (*model.Opportunity, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var o model.Opportunity
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// GetByClueID 反查走的是 (clue_id) 上的**部分**唯一索引（T-P4-05，DDL 见
// internal/pkg/db 的 postMigrateOpportunityClueUniqueIndex）。
//
// First 而不是 Take：同一条线索正常只有一行，真出现多行（索引被人工绕过）时
// First 按主键确定性取一行，Take 取到哪一行则取决于计划器，事后没人能复现。
func (r *opportunityRepo) GetByClueID(ctx context.Context, clueID string) (*model.Opportunity, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	clueID = strings.TrimSpace(clueID)
	if clueID == "" {
		return nil, errors.New("opportunity repository: 空 clueID 不是查询条件（本表允许无线索的商机）")
	}
	var o model.Opportunity
	err := r.db.WithContext(ctx).Where("clue_id = ?", clueID).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *opportunityRepo) OpenCountByOwner(ctx context.Context) (map[string]int, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	// 结果结构体的字段名按 GORM 的下划线映射写：OwnerUserID ↔ owner_user_id。
	type ownerCount struct {
		OwnerUserID string
		N           int
	}
	var rows []ownerCount
	err := r.db.WithContext(ctx).Model(&model.Opportunity{}).
		Select("owner_user_id, COUNT(*) AS n").
		Where("status = ?", model.OpportunityStatusOpen).
		Where("owner_user_id <> ''").
		Group("owner_user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, row := range rows {
		out[row.OwnerUserID] = row.N
	}
	return out, nil
}

// Update 一条 CAS 语句完成改写，返回三种结果之一（成功 / 版本过期 / 行不存在）。
//
// 为什么不用 `SELECT ... FOR UPDATE` 再改：那是悲观锁，而本卡 AC① 写的是**版本控制**。
// 两者的失败面不同：行锁会让第二个写者**等**到第一个提交完、然后拿着刚读到的新值改成功，
// 于是"两个人各自改了不同字段"变成两次都成功、彼此的改动都在 —— 听着很好，但它要求
// 每个调用方都只改自己那一格，而"改了哪几格"这件事没有任何一层能证明。
// CAS 的口径更硬：手里那份不是最新的就整个拒绝，由调用方重读重来（丢改动可见，
// 静默混合不可见）。
func (r *opportunityRepo) Update(ctx context.Context, o *model.Opportunity) error {
	if err := r.require(); err != nil {
		return err
	}
	if o == nil || o.ID == "" {
		return errors.New("opportunity repository: 空记录或空 ID")
	}
	// 下面这份 map 就是**改写白名单**：map 之外的列改不动，各自对应一处真实破坏 ——
	//   - id：主键被别的表抄走（sales_events.opportunity_id 与后续 quote/bill），
	//     改它 = 让所有下游引用指向一条不存在的行；
	//   - code：对外编号会出现在工单、截图与口述里，改它等于把所有历史引用断链；
	//   - customer_id / one_id / clue_id：客户身份与来源线索。改了之后事件流里已抄走的
	//     副本还挂在老客户身上，同一条商机在两个客户名下各计一次。要换客户只能
	//     作废重建（cancelled + 新行），"挪到另一个客户名下"这段历史必须留下；
	//   - created_at：新建商机数是 C6 北极星的分母、按它切时间窗，改它等于改史；
	//   - version：只由 `version + 1` 这一个表达式走，取的不是调用方那份。
	//     写成任意值等于把 CAS 判据交还给改写方，覆盖重新变回静默（AC① 当场作废）。
	//
	// 改写走 map 而不是 struct，另有两种静默失效要一起避开：
	//   - 首版在这里另加了 `Select(白名单)`，SET 语句被那份清单过滤了一遍 ——
	//     不在清单里的 `version = version + 1` 被静默丢弃，版本永远停在 0，
	//     每次 CAS 都比较同一个值，八个写者八个全赢。症状是"改写都成功、并发全通过"，
	//     正是 AC① 最坏的那种烂法：无声。
	//   - struct 形式的 Updates **默认跳过零值字段**，"把金额改成 0""把预计关单日清空"
	//     会当场失效而函数返回成功（见 _WritesZeroAndNull）。map 不跳。
	res := r.db.WithContext(ctx).Model(&model.Opportunity{}).
		Where("id = ? AND version = ?", o.ID, o.Version).
		Updates(map[string]any{
			"stage":             o.Stage,
			"status":            o.Status,
			"amount":            o.Amount,
			"currency":          currencyOrDefault(o.Currency),
			"win_probability":   o.WinProbability,
			"owner_user_id":     o.OwnerUserID,
			"expected_close_at": o.ExpectedCloseAt,
			"lost_reason":       o.LostReason,
			// updated_at 由这层落，而不是取调用方 struct 里那份：它是"多久没动过"的
			// 唯一凭据，谁忘了赋值都不该让一个**成功改写**留不下时间痕。
			"updated_at": time.Now().UTC(),
			// 版本只加，不取 o.Version：写成任意值等于把 CAS 判据交还给改写方。
			"version": gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}
	// 0 行有两种来路，分错的代价不对称：把"已被别人改过"报成"不存在"会让调用方
	// 放弃重试，而把"不存在"报成"改晚了"会让它对一条没有的行反复重读。
	// 所以这里补一次存在性探测。它**不参与**并发正确性（决定权在 UPDATE 那一条），
	// 探测与改写之间行被删了也只是错个标签，不会写进任何东西。
	exists, err := r.exists(ctx, o.ID)
	if err != nil {
		return err
	}
	if exists {
		return ErrOpportunityStaleVersion
	}
	return ErrOpportunityNotFound
}

// currencyOrDefault 空币种按建表默认值写回。
//
// 这不是值域校验（那属于 service），是把建表时那条 DEFAULT 'CNY' 在 UPDATE 路径上
// 补齐：白名单会**强制**写 currency 列，若照抄 struct 里的空串，一次"没人碰币种"的
// 改写会把老行的 CNY 冲成空串 —— 而金额不带币种不可算，报表会静默算错。
func currencyOrDefault(currency string) string {
	if strings.TrimSpace(currency) == "" {
		return model.OpportunityCurrencyDefault
	}
	return currency
}

func (r *opportunityRepo) exists(ctx context.Context, id string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.Opportunity{}).
		Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *opportunityRepo) ListByCustomer(ctx context.Context, customerID string, statuses []string, limit, offset int) ([]*model.Opportunity, error) {
	if strings.TrimSpace(customerID) == "" {
		return nil, errors.New("opportunity repository: 空 customerID 不是查询条件")
	}
	return r.list(ctx, "customer_id = ?", []any{customerID}, statuses, limit, offset)
}

func (r *opportunityRepo) ListByStage(ctx context.Context, stage string, statuses []string, limit, offset int) ([]*model.Opportunity, error) {
	if strings.TrimSpace(stage) == "" {
		return nil, errors.New("opportunity repository: 空 stage 不是查询条件")
	}
	return r.list(ctx, "stage = ?", []any{stage}, statuses, limit, offset)
}

func (r *opportunityRepo) ListByOwner(ctx context.Context, ownerUserID string, statuses []string, limit, offset int) ([]*model.Opportunity, error) {
	if strings.TrimSpace(ownerUserID) == "" {
		return nil, errors.New("opportunity repository: 空 ownerUserID 不是查询条件")
	}
	return r.list(ctx, "owner_user_id = ?", []any{ownerUserID}, statuses, limit, offset)
}

// list 三个维度的共用出口：条件全部走占位符，排序与分页两件事只写一遍。
//
// 排序必须带 id 兜底：同一时刻建的两行（批量导入商机是真实路径）在
// `ORDER BY created_at DESC` 下顺序不定，于是偏移分页会重复一行、漏一行。
// 钉住它的是 `_Lists` 里那段并列行用例，判据是**逐位比对次序**而不是"并集不重不漏" ——
// 变异实测（摘掉 `, id DESC`）：三行按插入顺序返回，limit=1 翻三页仍然不重不漏，
// 只有次序断言红了。
func (r *opportunityRepo) list(ctx context.Context, where string, args []any, statuses []string, limit, offset int) ([]*model.Opportunity, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, ErrOpportunityStatusFilterEmpty
	}
	if limit <= 0 {
		return nil, errors.New("opportunity repository: limit 必须为正（想取全部请分页，别把这层当导出用）")
	}
	if offset < 0 {
		return nil, errors.New("opportunity repository: offset 不能为负")
	}
	var rows []*model.Opportunity
	err := r.db.WithContext(ctx).
		Where(where, args...).
		Where("status IN ?", statuses).
		Order("created_at DESC, id DESC").
		Limit(limit).Offset(offset).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
