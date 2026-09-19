// human_task.go 统一待办仓储（T-P3-03 / N-9）。
//
// 五层架构里本文件是 service.HumanTaskService 与 human_tasks 表之间唯一的一层。
// 与审批仓储同一取舍：**没有内存版底座**。待办的意义就是"进程外还记着这件事"，
// 一份重启即空的待办池比没有待办更坏 —— 未读数会在每次发布后归零，
// 而运维把它读成"人工终于清完了"。
//
// 读侧一律给三件东西而不是两件：结果、总数、错误。"没有待办"与"读不动"必须分得开，
// 因为前者是一句业务结论（可以拿去做判断），后者只能支撑"未知"。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

// ErrHumanTaskOpenConflict 同一 (subject_type, subject_id) 已有未处理的待办
// （部分唯一索引 uq_human_task_open）。
//
// 单列成 sentinel 而不是让调用方去匹配 PG 约束名：判据要写在代码里才能测。
// 反过来说，**识别**它必须认约束名（见 isHumanTaskOpenConflict）。
var ErrHumanTaskOpenConflict = errors.New("human_task: 该对象已有未处理待办")

// ErrHumanTaskInputInvalid 入参越界（未知 kind/status、空身份、非法分页）。
//
// 过滤器里的错值必须报错而不是回空列表：空列表会被上层读成"这类待办一条都没有"，
// 而事实是这次查询压根无效（AC③ 的读侧最容易在这种地方假绿）。
var ErrHumanTaskInputInvalid = errors.New("human_task: 查询条件非法")

const (
	// humanTaskMaxPageSize 列表单页上限（有上限是因为这条列表会被待办中心每次打开
	// 都拉一次：不设上限时一句 page_size=999999 就能把整表读进内存）。
	// 默认页大小不在这里 —— 它是 HTTP 入参缺省的口径，归 service 层。
	humanTaskMaxPageSize = 200
)

// humanTaskActionWriteColumns 状态跃迁时可写的列（**白名单**）。
//
// 名单之外的一切改不动，各挡一件事：
//   - id / created_at：待办的身份与投递时刻；
//   - kind：把会话待办改成审批待办 = 让审批类待办被坐席的认领动作处理掉；
//   - subject_type / subject_id：待办关于哪件事就是它的身份，改得动等于"处理了 A 记在 B 头上"，
//     而且这一改还能顺手把 uq_human_task_open 的坑挪到另一个对象上；
//   - sla_*：SLA 在开放那一刻随事实源定死，事后能改它 = 能把逾期读数抹平。
var humanTaskActionWriteColumns = []string{
	"status", "assignee_user_id", "claimed_at", "completed_at",
	"cancelled_at", "cancel_reason", "updated_at",
}

// HumanTaskQuery 列表与聚合的过滤条件。零值 = 默认视图（未落定的待办，按投递先后）。
type HumanTaskQuery struct {
	// Kinds 为空 = 三类都要；非空 = 只列这些类（每个都必须是已知 kind）。
	Kinds []string
	// Statuses 为空 = 只列未落定的（与 uq_human_task_open 的谓词同一划分）；
	// 非空 = 精确列这些状态（每个都必须是已知状态）。
	Statuses       []string
	AssigneeUserID string
	// Page/PageSize 分页：**要么都给、要么都不给**（都给时 Page 从 1 起，PageSize 超
	// humanTaskMaxPageSize 会被夹住；都不给时一次读全表，只给进程内调用用）。
	// 半分页在 List 里直接报错，理由见函数内注释。
	Page     int
	PageSize int
}

// HumanTaskRepository 待办读写接口。
type HumanTaskRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// Insert 落一条新待办。同一 (subject_type, subject_id) 已有未落定的行时
	// 返回 ErrHumanTaskOpenConflict，其余冲突原样返回。
	Insert(ctx context.Context, t *model.HumanTask) error

	// GetByID 按主键读取；不存在返回 (nil, nil)，读失败返回 error。
	GetByID(ctx context.Context, id string) (*model.HumanTask, error)

	// GetOpenBySubject 取该对象当前**未落定**的待办（幂等复用用）；没有返回 (nil, nil)。
	// 空身份返回 ErrHumanTaskInputInvalid 而不是"查不到"。
	GetOpenBySubject(ctx context.Context, subjectType, subjectID string) (*model.HumanTask, error)

	// ApplyAction 在一条事务里锁住某行、确认它仍是 expectStatus，再交 fn 就地改，
	// 然后按 humanTaskActionWriteColumns 写回。
	//
	// 返回 (false, nil) = 行不存在或状态已不是 expectStatus（fn 未被调用）；
	// 返回 (false, err) 时 err 可能是 fn 自己给的哨兵（原样上抛，service 靠它判 404/409）。
	//
	// 与审批侧 MutatePending 同一形状，两道判据各管一件事：
	//   - 带 `status = expectStatus` 的写回（CAS）= 并发认领只有一个赢家的来源；
	//   - FOR UPDATE = fn 执行期间这一行不许被别人改，于是 fn 里的判据
	//     （"还开放吗"、"是不是这个人的"）建立在不会被抽走的快照上。
	// 两条各有用例，摘掉哪条红哪条 —— 别把功劳记错对象。
	ApplyAction(ctx context.Context, id, expectStatus string, fn func(*model.HumanTask) error) (bool, error)

	// List 按过滤条件取一页，并回**过滤后的总行数**（不是本页行数）。
	List(ctx context.Context, q HumanTaskQuery) ([]*model.HumanTask, int64, error)

	// CountOpenByKind 每类未落定的行数。三类键恒在（0 也要出现）；
	// 库里若有值域外的 kind，它也会带进来 —— 读数不该悄悄丢行。
	CountOpenByKind(ctx context.Context) (map[string]int64, error)

	// CountOverdueOpenByKind 每类"未落定且**自己那一档** SLA 已过 now"的行数。
	//
	// 每类只判自己那一列（会话判首响、审批判裁决、催收判升级）。用
	// COALESCE(三列) 是这里最省事的写错法：一行 kind 是会话、却被填上审批档时刻的
	// 脏数据，会按审批档的截止被判逾期，坐席的响应指标当场失真（AC④）。
	CountOverdueOpenByKind(ctx context.Context, now time.Time) (map[string]int64, error)
}

type humanTaskRepo struct {
	db *gorm.DB
}

// NewHumanTaskRepository 创建实例（用全局 DB）
func NewHumanTaskRepository() HumanTaskRepository { return &humanTaskRepo{db: _db.GetDB()} }

// NewHumanTaskRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）
func NewHumanTaskRepositoryWithDB(db *gorm.DB) HumanTaskRepository {
	return &humanTaskRepo{db: db}
}

func (r *humanTaskRepo) Available() bool { return r != nil && r.db != nil }

func (r *humanTaskRepo) require() error {
	if !r.Available() {
		return errors.New("human_task repository: db handle is nil")
	}
	return nil
}

func (r *humanTaskRepo) Insert(ctx context.Context, t *model.HumanTask) error {
	if err := r.require(); err != nil {
		return err
	}
	if t == nil || t.ID == "" {
		return errors.New("human_task repository: 空记录或空 ID")
	}
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		if isHumanTaskOpenConflict(err) {
			return ErrHumanTaskOpenConflict
		}
		return err
	}
	return nil
}

// isHumanTaskOpenConflict 判定"命中的是 uq_human_task_open 还是别的约束"。
//
// SQLSTATE 与约束名**两个条件都要命中**：只判 23505 会把主键冲突（ID 生成器重号
// 就会给一次）也读成"这个对象已有开放待办"，于是 service 去复用一条毫不相干的行，
// 并把这次投递当成完成 —— 全程零报错。
func isHumanTaskOpenConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "uq_human_task_open")
}

func (r *humanTaskRepo) GetByID(ctx context.Context, id string) (*model.HumanTask, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	var t model.HumanTask
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *humanTaskRepo) GetOpenBySubject(ctx context.Context, subjectType, subjectID string) (*model.HumanTask, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(subjectType) == "" || strings.TrimSpace(subjectID) == "" {
		return nil, fmt.Errorf("%w: subject 二元组不许为空", ErrHumanTaskInputInvalid)
	}
	var t model.HumanTask
	// 排序是"最早那条"而不是"任意一条"：部分唯一保证最多一行开放，
	// 但历史行不止一行；带 ORDER BY 让这个查询在索引被改动后仍然给出可解释的答案。
	err := r.db.WithContext(ctx).
		Where("subject_type = ? AND subject_id = ? AND "+model.HumanTaskOpenPredicateSQL(), subjectType, subjectID).
		Order("created_at ASC, id ASC").
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ApplyAction 一次加锁读 + 判期望态 + fn 修改 + 按列白名单 CAS 写回。
//
// fn 报错误整笔回滚（不是"写完再报错"），这是 service 敢把跃迁判据放进 fn 的前提。
func (r *humanTaskRepo) ApplyAction(ctx context.Context, id, expectStatus string, fn func(*model.HumanTask) error) (bool, error) {
	if err := r.require(); err != nil {
		return false, err
	}
	if strings.TrimSpace(id) == "" || !model.HumanTaskStatusKnown(expectStatus) {
		return false, fmt.Errorf("%w: id 为空或期望态 %q 不是已知状态", ErrHumanTaskInputInvalid, expectStatus)
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t model.HumanTask
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&t).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if t.Status != expectStatus {
			return nil // 别人已经动过：本次不生效，也不算错误（调用方按 applied 分支）
		}
		if fn != nil {
			if ferr := fn(&t); ferr != nil {
				return ferr // 原样上抛，让 service 用 errors.Is 判 404/409
			}
		}
		// Model 传**空壳**而不是 &t：GORM 会从 Model 的主键值再拼一条 `id = ?` 进 WHERE，
		// 而 t 是刚被 fn 改过的那个实例 —— fn 若动了 t.ID（白名单挡的是落库，不挡内存），
		// 两条 id 条件互斥、更新静默命中 0 行。写回目标只认传进来的 id。
		res := tx.Model(&model.HumanTask{}).
			Where("id = ? AND status = ?", id, expectStatus).
			Select(humanTaskActionWriteColumns).
			Updates(&t)
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

// humanTaskScopeWhere 把过滤条件翻成一条 WHERE 片段（kind 与 status 都是**组内 OR、组间 AND**）。
//
// kind 用 `IN ?` 而不是逐个 `Where("kind = ?")`：后者是多条件 AND，
// "会话类 + 催收类"这一句会翻成"既是会话类又是催收类"，于是**合并视图永远为空** ——
// 而永远为空在 UI 上读起来像"这两类都没活儿"，是一句没人会去质疑的业务结论。
//
// 空 kind 补齐成三类已知值（而不是不加过滤）：默认视图的口径是"这个模型认识的三类待办"，
// 值域外的脏 kind 不该因为"没写过滤"而混进坐席收件箱。
func humanTaskScopeWhere(q HumanTaskQuery) (string, []any, error) {
	kinds := q.Kinds
	if len(kinds) == 0 {
		kinds = model.HumanTaskKinds
	}
	for _, k := range kinds {
		if !model.HumanTaskKindKnown(k) {
			return "", nil, fmt.Errorf("%w: kind %q 不在值域里", ErrHumanTaskInputInvalid, k)
		}
	}
	where := "kind IN ?"
	args := []any{kinds}

	statuses := q.Statuses
	if len(statuses) == 0 {
		// 默认口径 = 未落定，与 uq_human_task_open 的谓词同源（见 model 层）。
		return where + " AND " + model.HumanTaskOpenPredicateSQL(), args, nil
	}
	for _, s := range statuses {
		if !model.HumanTaskStatusKnown(s) {
			return "", nil, fmt.Errorf("%w: status %q 不在值域里", ErrHumanTaskInputInvalid, s)
		}
	}
	return where + " AND status IN ?", append(args, statuses), nil
}

func (r *humanTaskRepo) List(ctx context.Context, q HumanTaskQuery) ([]*model.HumanTask, int64, error) {
	if err := r.require(); err != nil {
		return nil, 0, err
	}
	where, whereArgs, err := humanTaskScopeWhere(q)
	if err != nil {
		return nil, 0, err
	}
	if q.Page < 0 || q.PageSize < 0 {
		return nil, 0, fmt.Errorf("%w: page=%d page_size=%d 不许为负", ErrHumanTaskInputInvalid, q.Page, q.PageSize)
	}
	// 分页要么都给、要么都不给。这条规则的唯一理由是**歧义**：
	// page=0 配 page_size=2 既可能是"第一页"也可能是"未分页"，两种读法差 2 行；
	// page=3 不配 page_size 连偏移都算不出来。含糊的入参在这里拒掉，
	// 到了库里就只能靠"猜一种读法"，而列表少两行没人会报警。
	if (q.Page == 0) != (q.PageSize == 0) {
		return nil, 0, fmt.Errorf("%w: page=%d page_size=%d 分页参数要么都给要么都不给",
			ErrHumanTaskInputInvalid, q.Page, q.PageSize)
	}
	base := func() *gorm.DB {
		tx := r.db.WithContext(ctx).Model(&model.HumanTask{})
		tx = tx.Where(where, whereArgs...)
		if a := strings.TrimSpace(q.AssigneeUserID); a != "" {
			tx = tx.Where("assignee_user_id = ?", a)
		}
		return tx
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sel := base().Order("created_at ASC, id ASC")
	if q.PageSize > 0 {
		size := q.PageSize
		if size > humanTaskMaxPageSize {
			size = humanTaskMaxPageSize
		}
		// 走到这里 Page 必然 >= 1（与 PageSize 同生同灭，上面已拒）。
		sel = sel.Limit(size).Offset((q.Page - 1) * size)
	}
	// 未分页（两个都为 0）= 一次读全表，**只给进程内调用用**：
	// HTTP 侧必须带 page/page_size（controller 里有断言），否则一句不带参数的请求
	// 就能把整张待办表读进内存 —— 那条上限只管得到分页的那一路。
	var rows []*model.HumanTask
	if err := sel.Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// humanTaskFillAllKinds 把三类补齐成键恒在的映射（0 也要出现）。
//
// 缺键与 0 在 JSON 里读起来不同：前者是"这一类没被统计"，后者是一句业务结论。
// 库里若真有值域外的 kind，它也留在映射里（悄悄丢行比多出一键坏）。
func humanTaskFillAllKinds(got map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(got)+len(model.HumanTaskKinds))
	for k, v := range got {
		out[k] = v
	}
	for _, k := range model.HumanTaskKinds {
		if _, ok := out[k]; !ok {
			out[k] = 0
		}
	}
	return out
}

// humanTaskScanCounts 跑一条 GROUP BY kind 的计数查询。
//
// ctx 必须一路带到这一层：请求取消/超时后还占着一条连接跑完的计数，
// 在收件箱轮询场景下会把连接池抽干（而且这份读数是**旧**的）。
func (r *humanTaskRepo) humanTaskScanCounts(ctx context.Context, where string, args ...any) (map[string]int64, error) {
	type row struct {
		Kind string
		N    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.HumanTask{}).
		Select("kind, COUNT(*) AS n").
		Where(where, args...).
		Group("kind").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.Kind] = x.N
	}
	return out, nil
}

func (r *humanTaskRepo) CountOpenByKind(ctx context.Context) (map[string]int64, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	got, err := r.humanTaskScanCounts(ctx, model.HumanTaskOpenPredicateSQL())
	if err != nil {
		return nil, err
	}
	return humanTaskFillAllKinds(got), nil
}

// CountOverdueOpenByKind 逐类各问一次，每类只认自己那一列。
//
// 三次查询而不是一条 CASE 拼出来的 SQL，就是为了让"每类用自己的列"这件事在代码里
// 逐行看得见 —— 合并成一条语句时，把某类指错列变成一处看不出diff的字符串改动。
func (r *humanTaskRepo) CountOverdueOpenByKind(ctx context.Context, now time.Time) (map[string]int64, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(model.HumanTaskKinds))
	for _, kind := range model.HumanTaskKinds {
		col := HumanTaskSLAColumnForKind(kind)
		if col == "" { // 理论上到不了：三类都在值域里。留着是为了不静默用错列。
			continue
		}
		got, err := r.humanTaskScanCounts(ctx, "kind = ? AND "+model.HumanTaskOpenPredicateSQL()+
			" AND "+col+" IS NOT NULL AND "+col+" <= ?", kind, now)
		if err != nil {
			return nil, err
		}
		out[kind] = got[kind]
	}
	return humanTaskFillAllKinds(out), nil
}

// HumanTaskSLAColumnForKind 把 model 的"哪一档用哪一列"搬到仓储侧。
// model.HumanTaskSLAField 导出的就是同一个映射，这里不另立一份列名表。
func HumanTaskSLAColumnForKind(kind string) string { return model.HumanTaskSLAField(kind) }
