package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type ClueRepository interface {
	Create(ctx context.Context, user *model.Clue) error
	BatchCreateWithDedup(ctx context.Context, clues []*model.Clue) (successCount, skipCount int64, err error)
	GetByID(ctx context.Context, id string) (*model.Clue, error)
	// GetByIDs 按主键批量取线索（群发按选中 ID 精确取人；不按线索类型过滤，
	// 前端选择器列的是全类型线索，按类型过滤会把其它来源的选中项静默丢掉）。
	GetByIDs(ctx context.Context, ids []string) ([]*model.Clue, error)
	GetClueList(ctx context.Context, page int, limit int) ([]*model.Clue, int64, error)
	Delete(ctx context.Context, id string) error
	GetClueStatistics(ctx context.Context) ([]map[string]any, error)
	GetClueAllList(ctx context.Context, clueType int64) ([]*model.Clue, int64, error)
	GetWhatsappClues(ctx context.Context) ([]*model.Clue, int64, error)
	ExistsByTypeAndAccount(ctx context.Context, clueType int64, account string) (bool, error)
	FindByTypeAndAccount(ctx context.Context, clueType int64, account string) (*model.Clue, error)
	UpdateByID(ctx context.Context, id string, updates map[string]any) error
	ListByAccounts(ctx context.Context, accounts []string) ([]*model.Clue, error)
	BatchUpdateInTx(ctx context.Context, ids []string, updates map[string]any) (int, error)
	ListWithQuery(ctx context.Context, q ClueQuery, page, limit int) ([]*model.Clue, int64, error)
	CountWithQuery(ctx context.Context, q ClueQuery) (int64, error)
}

type clueRepo struct {
	db *gorm.DB
}

// clueDedupChunkSize 批量导入去重查询的分块大小（每块 200 个 (type, account)）。
const clueDedupChunkSize = 200

// NewClueRepositoryWithDB 用调用方注入的连接构造仓储（DI 装配点：router / app wiring）。
// NewClueRepositoryDefault 默认构造（全局句柄）。
func NewClueRepositoryDefault() ClueRepository {
	return NewClueRepositoryWithDB(GetDB())
}

func NewClueRepositoryWithDB(db *gorm.DB) ClueRepository {
	return &clueRepo{db: db}
}

// Create 纯写入。去重判据属业务规则，由 service 层先查 ExistsByTypeAndAccount 再决定。
func (r *clueRepo) Create(ctx context.Context, clue *model.Clue) error {
	return r.db.WithContext(ctx).Create(clue).Error
}

func (r *clueRepo) BatchCreateWithDedup(ctx context.Context, clues []*model.Clue) (successCount, skipCount int64, err error) {
	if len(clues) == 0 {
		return 0, 0, nil
	}

	// 空 account 不参与去重：空串会让同类型下所有「没有账号」的线索互相判重，
	// 批次里的第 2 条起会被当成重复丢掉。
	seen := make(map[string]struct{}, len(clues))
	deduped := make([]*model.Clue, 0, len(clues))
	for _, c := range clues {
		if strings.TrimSpace(c.Account) == "" {
			deduped = append(deduped, c)
			continue
		}
		key := fmt.Sprintf("%d|%s", c.Type, c.Account)
		if _, ok := seen[key]; ok {
			skipCount++
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, c)
	}

	type pair struct {
		Type    int64
		Account string
	}
	pairs := make([]pair, 0, len(deduped))
	for _, c := range deduped {
		if strings.TrimSpace(c.Account) == "" {
			continue
		}
		pairs = append(pairs, pair{Type: c.Type, Account: c.Account})
	}

	type resultRow struct {
		Type    int64
		Account string
	}
	existsSet := make(map[string]struct{})
	db := r.db.WithContext(ctx)
	// 分块查已存在的 (type, account)：一次导入上千行时，单条 OR 链会把 SQL 文本
	// 撑到几十万字符，PG 解析开销和占位符上限都会成为真问题。
	for start := 0; start < len(pairs); start += clueDedupChunkSize {
		end := start + clueDedupChunkSize
		if end > len(pairs) {
			end = len(pairs)
		}
		conds := db.Model(&model.Clue{})
		for i, p := range pairs[start:end] {
			if i == 0 {
				conds = conds.Where("(type = ? AND account = ?)", p.Type, p.Account)
			} else {
				conds = conds.Or("(type = ? AND account = ?)", p.Type, p.Account)
			}
		}
		var rows []resultRow
		if err := conds.Distinct("type, account").Find(&rows).Error; err != nil {
			return 0, 0, err
		}
		for _, r := range rows {
			existsSet[fmt.Sprintf("%d|%s", r.Type, r.Account)] = struct{}{}
		}
	}

	toInsert := make([]*model.Clue, 0, len(deduped))
	for _, c := range deduped {
		k := fmt.Sprintf("%d|%s", c.Type, c.Account)
		if _, ok := existsSet[k]; ok {
			skipCount++
			continue
		}
		toInsert = append(toInsert, c)
	}

	if len(toInsert) == 0 {
		return 0, skipCount, nil
	}

	if err := r.db.WithContext(ctx).CreateInBatches(toInsert, 100).Error; err != nil {
		return 0, skipCount, err
	}
	successCount = int64(len(toInsert))
	return successCount, skipCount, nil
}

func (r *clueRepo) GetByID(ctx context.Context, id string) (*model.Clue, error) {
	var clue model.Clue
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&clue).Error
	if err != nil {
		return nil, err
	}
	return &clue, nil
}

// GetClueList 兼容分页接口（新版页面走 ListWithQuery）。
// page/limit 在这里归一：调用方传 page=0 会算出负 Offset，PG 直接报
// "OFFSET must not be negative"；limit=0 在 PG 是「不限制」，等于全表。
func (r *clueRepo) GetByIDs(ctx context.Context, ids []string) ([]*model.Clue, error) {
	if len(ids) == 0 {
		return []*model.Clue{}, nil
	}
	var clues []*model.Clue
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&clues).Error; err != nil {
		return nil, err
	}
	return clues, nil
}

func (r *clueRepo) GetClueList(ctx context.Context, page int, limit int) ([]*model.Clue, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	var cluelists []*model.Clue
	var total int64
	db := r.db.WithContext(ctx)
	err := db.Model(&model.Clue{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = db.Order("create_time DESC").Offset((page - 1) * limit).Limit(limit).Find(&cluelists).Error
	if err != nil {
		return nil, 0, err
	}
	return cluelists, total, err
}
func (r *clueRepo) Delete(ctx context.Context, id string) error {
	// 0 行必须报「不存在」：DELETE 成功但没有任何行被删，对调用方等于假成功
	// （A5/A6 一类）。上层 ErrorFromDB 会把 ErrRecordNotFound 映射成 404。
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Clue{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *clueRepo) GetClueStatistics(ctx context.Context) ([]map[string]any, error) {
	var statistics []map[string]any
	err := r.db.WithContext(ctx).Model(&model.Clue{}).
		Select("type AS type, COUNT(*) AS total, " +
			"SUM(CASE WHEN COALESCE(is_verify,0) >= 1 THEN 1 ELSE 0 END) AS verify_total").
		Where("deleted_at IS NULL").
		Group("type").
		Order("type").
		Scan(&statistics).Error
	return statistics, err
}

func (r *clueRepo) GetClueAllList(ctx context.Context, clueType int64) ([]*model.Clue, int64, error) {
	var cluelists []*model.Clue
	var total int64
	db := r.db.WithContext(ctx)
	err := db.Where("type = ?", clueType).Model(&model.Clue{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	// 无界兜底:默认 500,线索表持续增长后防全表载入;已达上限时调用方需分页重取(如群发场景需全量)
	q := db.Where("type = ?", clueType)
	q = applyListLimit(q, 500)
	err = q.Find(&cluelists).Error
	if err != nil {
		return nil, 0, err
	}
	return cluelists, total, nil
}

func (r *clueRepo) GetWhatsappClues(ctx context.Context) ([]*model.Clue, int64, error) {
	var cluelists []*model.Clue
	var total int64
	db := r.db.WithContext(ctx)
	err := db.Where("type IN (?)", []int64{5, 7}).Model(&model.Clue{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	// 无界兜底:默认 1000,WhatsApp 线索批量处理防全表载入;已达上限时调用方需分页重取
	wq := db.Where("type IN (?)", []int64{5, 7})
	wq = applyListLimit(wq, 1000)
	err = wq.Find(&cluelists).Error
	if err != nil {
		return nil, 0, err
	}
	return cluelists, total, nil
}

func (r *clueRepo) ExistsByTypeAndAccount(ctx context.Context, clueType int64, account string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Clue{}).Where("type = ? and account = ?", clueType, account).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *clueRepo) FindByTypeAndAccount(ctx context.Context, clueType int64, account string) (*model.Clue, error) {
	var clue model.Clue
	err := r.db.WithContext(ctx).Model(&model.Clue{}).Where("type = ? and account = ?", clueType, account).First(&clue).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &clue, nil
}

func (r *clueRepo) UpdateByID(ctx context.Context, id string, updates map[string]any) error {
	if id == "" {
		return errors.New("线索 ID 不能为空")
	}
	if len(updates) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&model.Clue{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("线索不存在或未更新")
	}
	return nil
}

// clueListByAccountsLimit ListByAccounts 单次取数上限。
// 调用方（customer-360）按「当前页客户」反查线索，键集本身有限；设上限只为挡住
// 上游把整表账号塞进来的调用，避免一次查询把线索表全量载入。
const clueListByAccountsLimit = 1000

func (r *clueRepo) ListByAccounts(ctx context.Context, accounts []string) ([]*model.Clue, error) {
	if len(accounts) == 0 {
		return nil, nil
	}
	unique := make([]string, 0, len(accounts))
	seen := make(map[string]struct{}, len(accounts))
	for _, a := range accounts {
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		unique = append(unique, a)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	var clues []*model.Clue
	err := r.db.WithContext(ctx).
		Where("account IN ? OR name IN ?", unique, unique).
		Order("create_time DESC").
		Limit(clueListByAccountsLimit).
		Find(&clues).Error
	if err != nil {
		return nil, err
	}
	return clues, nil
}

// BatchUpdateInTx 逐条更新并返回**实际写入的行数**。
//
// 返回值口径是 RowsAffected 而不是请求数：命中 0 行（跨租户 id 被租户谓词挡掉就是
// 0 行）不许被算成成功；出错直接让事务回滚。
func (r *clueRepo) BatchUpdateInTx(ctx context.Context, ids []string, updates map[string]any) (int, error) {
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			if id == "" {
				continue
			}
			res := tx.Model(&model.Clue{}).Where("id = ?", id).Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			count += int(res.RowsAffected)
		}
		return nil
	})
	return count, err
}
