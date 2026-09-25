// bad_case.go Bad Case 读写仓储（新规划任务清单 T-P8-03 / G-2）。
//
// 与 human_tasks 同一取舍：**没有内存版底座**。Bad Case 的全部意义是"进程外还记着
// 这条判过、判成什么"，一份重启即空的队列比没有队列更坏 —— 打标的人会以为今天没坏例，
// 而事实是上一次发布把没导完的那批抹掉了。
//
// 读侧一律给三件东西（结果、总数、错误），理由与待办仓储同一句：
// "一条坏例都没有"是业务结论，"读不动"只能支撑"未知"。
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

// ErrBadCaseInputInvalid 入参越界（未知 source/status/label、空身份、非法分页、空 id 集）。
//
// 过滤器里的错值必须报错而不是回空列表：空列表会被读成"这类坏例一条都没有"，
// 而事实是这次查询压根无效。
var ErrBadCaseInputInvalid = errors.New("bad_case: 查询条件非法")

// ErrBadCaseAlreadyClaimed 这一批已被另一次导出取走。
//
// 单独成 sentinel 是因为它是**并发**的结果而不是错误：两个运营同时点"导出评测集"，
// 后一个应当拿到一句"这批被人取走了"，而不是一个静默的空集（空集会让他以为库里没货）。
var ErrBadCaseAlreadyClaimed = errors.New("bad_case: 待导出的行已被其他导出取走")

const (
	// badCaseMaxPageSize 列表单页上限（同待办：一句 page_size=999999 不该能拉整表）。
	badCaseMaxPageSize = 200
	// badCaseMaxExportRows 单次导出的行数上限。
	//
	// 有上限不是因为导出慢，是因为导出产物会变成评测集：一次"手滑全选"把三年的坏例
	// 灌进 gold set，T-P8-04 的基线分数当场被一坨未复核样本淹没，而分数是拿来比好坏的。
	badCaseMaxExportRows = 2000
)

// badCaseActionWriteColumns 状态跃迁时可写的列（**白名单**）。
//
// 名单之外改不动，各挡一件事：
//   - id / dedup_key / source：这条的身份与它"怎么进来的"，改得动就能把自动标记
//     伪装成人工补录（打标队列的可信度全靠这一列没被动过）；
//   - query_text / answer_text / confidence / threshold / retrieved_count / signal_id /
//     session_id / message_id / mark_reason：标记那一刻的现场。判完之后再改现场，
//     等于让打标的人对着一份能被事后修饰的证据下结论，而它已经进了评测集；
//   - created_at：队列排序与"标记到判定的时长"这个指标的分子。
var badCaseActionWriteColumns = []string{
	"status", "label", "label_note", "labeler_id", "labeled_at",
	"fix_layer", "eval_set_id", "exported_at", "cancel_reason", "updated_at",
}

// BadCaseListQuery 列表与聚合的过滤条件。零值 = 默认视图（按标记先后）。
type BadCaseListQuery struct {
	// Sources 为空 = 全部来源；非空 = 只列这些来源（每个都必须是已知 source）。
	Sources []string
	// Statuses 为空 = 只列**未落定**的（pending）—— 打标队列的默认视图就是"还没人判的"；
	// 非空 = 精确列这些状态（每个都必须是已知 status）。
	//
	// 这里刻意不像 human_tasks 那样把"已打标"也算未落定：坏例判完没导出，
	// 那是"等导出"而不是"等人判"，两拨人不看同一个队列。
	Statuses []string
	Labels   []string
	FixLayer string
	// LabelerID / PendingOnly 之类交叉条件由 service 组装；这里只放列表页真用得上的。
	Page     int
	PageSize int
}

// BadCaseRepository bad_cases 读写接口。
type BadCaseRepository interface {
	// Available 报告是否持有可用 DB 句柄（装配回显用，不用于吞错）。
	Available() bool

	// InsertIfAbsent 落一条。dedup_key 撞已有的行时返回 (false, nil) ——
	// "这一轮已经标过"不是错误，热路径上它每秒会发生很多次。
	// 撞的是别的约束（主键、非空等）时原样上抛，不许被读成"已存在"。
	InsertIfAbsent(ctx context.Context, bc *model.BadCase) (bool, error)

	// GetByID 按主键读取；不存在返回 (nil, nil)。
	GetByID(ctx context.Context, id string) (*model.BadCase, error)

	// ApplyAction 事务内锁行、确认仍是 expectStatus，交 fn 就地改，按列白名单写回。
	// (false, nil) = 行不存在或状态已被别人改动（fn 未被调用）。
	ApplyAction(ctx context.Context, id, expectStatus string, fn func(*model.BadCase) error) (bool, error)

	// List 按过滤条件取一页，并回**过滤后的总行数**。
	List(ctx context.Context, q BadCaseListQuery) ([]*model.BadCase, int64, error)

	// CountByStatus 每个状态的行数（四个键恒在，0 也要出现）。
	CountByStatus(ctx context.Context) (map[string]int64, error)

	// CountByFixLayer 已归因行按责任层聚合（label 为空的行不进任何一层）。
	// 未知 label（值域外的脏数据）会以 "unknown" 为键带回来 —— 读数不许悄悄丢行。
	CountByFixLayer(ctx context.Context) (map[string]int64, error)

	// ClaimForExport 在一笔事务里把"已打标且未导出"的行取走：SELECT ... FOR UPDATE
	// SKIP LOCKED 后立刻盖上 eval_set_id 与 exported_at，返回被取走的那批行。
	// 并发的第二次导出看不到这些行（SKIP LOCKED 跳过被锁的），因此同一份样本
	// 不会进两个评测集。取不到任何行时返回 ErrBadCaseAlreadyClaimed。
	ClaimForExport(ctx context.Context, labels []string, limit int, evalSetID string, at time.Time) ([]*model.BadCase, error)
}

type badCaseRepo struct {
	db *gorm.DB
}

// NewBadCaseRepository 创建实例（用全局 DB）
func NewBadCaseRepository() BadCaseRepository { return &badCaseRepo{db: _db.GetDB()} }

// NewBadCaseRepositoryWithDB 创建指定数据库连接的实例（用于测试与装配注入）
func NewBadCaseRepositoryWithDB(db *gorm.DB) BadCaseRepository {
	return &badCaseRepo{db: db}
}

func (r *badCaseRepo) Available() bool { return r != nil && r.db != nil }

func (r *badCaseRepo) require() error {
	if !r.Available() {
		return errors.New("bad_case repository: db handle is nil")
	}
	return nil
}

func (r *badCaseRepo) InsertIfAbsent(ctx context.Context, bc *model.BadCase) (bool, error) {
	if err := r.require(); err != nil {
		return false, err
	}
	if bc == nil || bc.ID == "" {
		return false, errors.New("bad_case repository: 空记录或空 ID")
	}
	if err := r.db.WithContext(ctx).Create(bc).Error; err != nil {
		if isBadCaseDedupConflict(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// isBadCaseDedupConflict 判定"命中的是 bad_cases_dedup_key_key 还是别的约束"。
//
// SQLSTATE 与约束名两个条件都要命中：只判 23505 会把主键冲突（id 生成器重号就会给一次）
// 也读成"这一轮已标记"，于是热路径上真坏例被当成重复悄悄丢掉，全程零报错。
func isBadCaseDedupConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") && strings.Contains(msg, "bad_cases_dedup_key")
}

func (r *badCaseRepo) GetByID(ctx context.Context, id string) (*model.BadCase, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: id 为空", ErrBadCaseInputInvalid)
	}
	var bc model.BadCase
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&bc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bc, nil
}

func (r *badCaseRepo) ApplyAction(ctx context.Context, id, expectStatus string, fn func(*model.BadCase) error) (bool, error) {
	if err := r.require(); err != nil {
		return false, err
	}
	if strings.TrimSpace(id) == "" || !model.IsKnownBadCaseStatus(expectStatus) {
		return false, fmt.Errorf("%w: id 为空或期望态 %q 不是已知状态", ErrBadCaseInputInvalid, expectStatus)
	}
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var bc model.BadCase
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&bc).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if bc.Status != expectStatus {
			return nil // 别人已经动过：本次不生效，也不算错误
		}
		if fn != nil {
			if ferr := fn(&bc); ferr != nil {
				return ferr // fn 的错误原样上抛，整笔回滚
			}
		}
		res := tx.Model(&model.BadCase{}).Where("id = ? AND status = ?", id, expectStatus).
			Select(badCaseActionWriteColumns).Updates(bc)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			// 锁到手却写不动 = 期望态在锁住的瞬间被改（不该发生），
			// 宁可判失败也不留一次"以为改了"的静默。
			return fmt.Errorf("bad_case repository: 状态 %s 的写回影响 %d 行，期望 1 行", expectStatus, res.RowsAffected)
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (r *badCaseRepo) List(ctx context.Context, q BadCaseListQuery) ([]*model.BadCase, int64, error) {
	if err := r.require(); err != nil {
		return nil, 0, err
	}
	if q.Page > 0 || q.PageSize > 0 {
		if q.Page < 1 || q.PageSize < 1 {
			return nil, 0, fmt.Errorf("%w: 分页要么都不给、要么两个都为正（page=%d page_size=%d）",
				ErrBadCaseInputInvalid, q.Page, q.PageSize)
		}
	}
	// count 与取页各起一条语句、各自套一遍条件：GORM 的 Where 挂在同一个 *DB 上，
	// 拿同一条链先 Count 再 Find，Count 会把 Find 的列/排序状态算进去（回过一次的假数）。
	base := r.db.WithContext(ctx).Model(&model.BadCase{})
	countStmt, err := applyBadCaseFilter(base, q)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := countStmt.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	pageStmt, err := applyBadCaseFilter(r.db.WithContext(ctx).Model(&model.BadCase{}), q)
	if err != nil {
		return nil, 0, err
	}
	pageStmt = pageStmt.Order("created_at DESC, id DESC")
	if q.PageSize > 0 {
		size := q.PageSize
		if size > badCaseMaxPageSize {
			size = badCaseMaxPageSize
		}
		pageStmt = pageStmt.Offset((q.Page - 1) * size).Limit(size)
	}
	var rows []*model.BadCase
	if err := pageStmt.Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// applyBadCaseFilter 把查询条件套到一条语句上。值域外的过滤器在这里就报错，不放行到 SQL：
// ?status=bogus 回空列表会被前端读成"这个状态没有坏例"。
func applyBadCaseFilter(db *gorm.DB, q BadCaseListQuery) (*gorm.DB, error) {
	if len(q.Sources) > 0 {
		for _, s := range q.Sources {
			if !model.IsKnownBadCaseSource(s) {
				return nil, fmt.Errorf("%w: 未知来源 %q", ErrBadCaseInputInvalid, s)
			}
		}
		db = db.Where("source IN ?", q.Sources)
	}
	if len(q.Statuses) > 0 {
		for _, s := range q.Statuses {
			if !model.IsKnownBadCaseStatus(s) {
				return nil, fmt.Errorf("%w: 未知状态 %q", ErrBadCaseInputInvalid, s)
			}
		}
		db = db.Where("status IN ?", q.Statuses)
	} else {
		db = db.Where("status = ?", model.BadCaseStatusPending)
	}
	if len(q.Labels) > 0 {
		for _, l := range q.Labels {
			if !model.IsKnownBadCaseLabel(l) {
				return nil, fmt.Errorf("%w: 未知归因类目 %q", ErrBadCaseInputInvalid, l)
			}
		}
		db = db.Where("label IN ?", q.Labels)
	}
	if q.FixLayer != "" {
		if !badCaseInStr(model.BadCaseFixLayers, q.FixLayer) {
			return nil, fmt.Errorf("%w: 未知责任层 %q", ErrBadCaseInputInvalid, q.FixLayer)
		}
		db = db.Where("fix_layer = ?", q.FixLayer)
	}
	return db, nil
}

func badCaseInStr(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func (r *badCaseRepo) CountByStatus(ctx context.Context) (map[string]int64, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	type row struct {
		Status string
		N      int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&model.BadCase{}).
		Select("status, COUNT(*) AS n").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(model.BadCaseStatuses)+len(rows))
	for _, s := range model.BadCaseStatuses {
		out[s] = 0
	}
	for _, rw := range rows {
		out[rw.Status] = rw.N
	}
	return out, nil
}

func (r *badCaseRepo) CountByFixLayer(ctx context.Context) (map[string]int64, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	type row struct {
		Label string
		N     int64
	}
	var rows []row
	// 已导出与已打标的都算"判过"：判完没导出的行同样是一条归因，
	// 只数 status='labeled' 会让"责任层分布"这块看板在每次导出后凭空少一截。
	if err := r.db.WithContext(ctx).Model(&model.BadCase{}).
		Where("status IN ?", []string{model.BadCaseStatusLabeled, model.BadCaseStatusExported}).
		Where("label <> ''").
		Select("label, COUNT(*) AS n").Group("label").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(model.BadCaseFixLayers))
	for _, l := range model.BadCaseFixLayers {
		out[l] = 0
	}
	unknown := int64(0)
	for _, rw := range rows {
		if layer := model.FixLayerOfLabel(rw.Label); layer != "" {
			out[layer] += rw.N
		} else {
			unknown += rw.N
		}
	}
	if unknown > 0 {
		out["unknown"] = unknown
	}
	return out, nil
}

func (r *badCaseRepo) ClaimForExport(ctx context.Context, labels []string, limit int, evalSetID string, at time.Time) ([]*model.BadCase, error) {
	if err := r.require(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(evalSetID) == "" {
		return nil, fmt.Errorf("%w: eval_set_id 为空", ErrBadCaseInputInvalid)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit 必须为正（got %d）", ErrBadCaseInputInvalid, limit)
	}
	if limit > badCaseMaxExportRows {
		limit = badCaseMaxExportRows
	}
	for _, l := range labels {
		if !model.IsKnownBadCaseLabel(l) {
			return nil, fmt.Errorf("%w: 未知归因类目 %q", ErrBadCaseInputInvalid, l)
		}
	}

	var claimed []*model.BadCase
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND eval_set_id = ?", model.BadCaseStatusLabeled, "").
			Order("labeled_at ASC, id ASC").
			Limit(limit)
		if len(labels) > 0 {
			q = q.Where("label IN ?", labels)
		}
		var rows []*model.BadCase
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrBadCaseAlreadyClaimed
		}
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		res := tx.Model(&model.BadCase{}).
			Where("id IN ? AND status = ? AND eval_set_id = ?", ids, model.BadCaseStatusLabeled, "").
			Updates(map[string]any{
				"status":      model.BadCaseStatusExported,
				"eval_set_id": evalSetID,
				"exported_at": at,
				"updated_at":  at,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != int64(len(rows)) {
			// 锁住了还写不动：只能是值域外的行混进了这批（不该发生）。
			// 宁可回滚整笔，也不交出一份"少了几条但没说"的评测集。
			return fmt.Errorf("bad_case repository: 导出取走 %d 行却只盖上 %d 行，整笔回滚",
				len(rows), res.RowsAffected)
		}
		for _, row := range rows {
			row.Status = model.BadCaseStatusExported
			row.EvalSetID = evalSetID
			row.ExportedAt = &at
		}
		claimed = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}
