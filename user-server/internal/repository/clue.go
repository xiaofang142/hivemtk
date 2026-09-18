package repository

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"time"

	"gorm.io/gorm"
)

type ClueRepository interface {
	Create(ctx context.Context, user *model.Clue) error
	BatchCreateWithDedup(ctx context.Context, clues []*model.Clue) (successCount, skipCount int64, err error)
	GetByID(ctx context.Context, id string) (*model.Clue, error)
	GetClueList(ctx context.Context, page int, limit int) ([]*model.Clue, int64, error)
	Delete(ctx context.Context, id string) error
	GetRecentClueList(ctx context.Context) ([]*model.Clue, error)
	GetClueStatistics(ctx context.Context) ([]map[string]any, error)
	GetClueAllList(ctx context.Context, clueType int64) ([]*model.Clue, int64, error)
	GetWhatsappClues(ctx context.Context) ([]*model.Clue, int64, error)
	ExistsByTypeAndAccount(ctx context.Context, clueType int64, account string) (bool, error)
	FindByTypeAndAccount(ctx context.Context, clueType int64, account string) (*model.Clue, error)
	GetDistinctTypes(ctx context.Context) ([]int64, error)
	UpdateByID(ctx context.Context, id string, updates map[string]any) error
	ListByAccounts(ctx context.Context, accounts []string) ([]*model.Clue, error)
	BatchUpdateInTx(ctx context.Context, ids []string, updates map[string]any) (int, error)
	ListWithQuery(ctx context.Context, q ClueQuery, page, limit int) ([]*model.Clue, int64, error)
	CountWithQuery(ctx context.Context, q ClueQuery) (int64, error)
	TypeDistribution(ctx context.Context, q ClueQuery) ([]ClueTypeAgg, error)
	TrendByDay(ctx context.Context, q ClueQuery) ([]ClueTrendAgg, error)
}

type clueRepo struct {
	db *gorm.DB
}

func NewClueRepository() ClueRepository {
	return &clueRepo{db: _db.GetDB()}
}

// NewClueRepositoryWithDB 创建指定数据库连接的 ClueRepository 实例（用于测试）
func NewClueRepositoryWithDB(db *gorm.DB) ClueRepository {
	return &clueRepo{db: db}
}

func (r *clueRepo) Create(ctx context.Context, clue *model.Clue) error {

	var count int64
	db := r.db.WithContext(ctx)
	err := db.Model(&model.Clue{}).Where("type = ? and account = ?", clue.Type, clue.Account).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("重复数据")
	}
	return db.Create(clue).Error
}

func (r *clueRepo) BatchCreateWithDedup(ctx context.Context, clues []*model.Clue) (successCount, skipCount int64, err error) {
	if len(clues) == 0 {
		return 0, 0, nil
	}

	seen := make(map[string]struct{}, len(clues))
	deduped := make([]*model.Clue, 0, len(clues))
	for _, c := range clues {
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
		pairs = append(pairs, pair{Type: c.Type, Account: c.Account})
	}

	existsSet := make(map[string]struct{})
	if len(pairs) > 0 {
		db := r.db.WithContext(ctx)

		conds := db.Model(&model.Clue{})
		for i, p := range pairs {
			if i == 0 {
				conds = conds.Where("(type = ? AND account = ?)", p.Type, p.Account)
			} else {
				conds = conds.Or("(type = ? AND account = ?)", p.Type, p.Account)
			}
		}
		type resultRow struct {
			Type    int64
			Account string
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

func (r *clueRepo) GetClueList(ctx context.Context, page int, limit int) ([]*model.Clue, int64, error) {
	var cluelists []*model.Clue
	var total int64
	db := r.db.WithContext(ctx)
	err := db.Model(&model.Clue{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = db.Offset((page - 1) * limit).Limit(limit).Find(&cluelists).Error
	if err != nil {
		return nil, 0, err
	}
	return cluelists, total, err
}
func (r *clueRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Clue{}).Error
}

func (r *clueRepo) GetRecentClueList(ctx context.Context) ([]*model.Clue, error) {
	var cluelists []*model.Clue

	var start_time = time.Now().Add(-time.Hour * 48).Unix()
	var end_time = time.Now().Unix()
	err := r.db.WithContext(ctx).Where("create_time > ? and create_time < ?", start_time, end_time).Order("create_time desc").Find(&cluelists).Error
	return cluelists, err
}

func (r *clueRepo) GetClueStatistics(ctx context.Context) ([]map[string]any, error) {
	var statistics []map[string]any
	db := r.db.WithContext(ctx)
	err := db.Raw("SELECT type AS type, COUNT(*) AS total, SUM(CASE WHEN COALESCE(is_verify,0) >= 1 THEN 1 ELSE 0 END) AS verify_total FROM clues WHERE deleted_at IS NULL GROUP BY type ORDER BY type").Scan(&statistics).Error
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

func (r *clueRepo) GetDistinctTypes(ctx context.Context) ([]int64, error) {
	var types []int64
	err := r.db.WithContext(ctx).Model(&model.Clue{}).Distinct("type").Pluck("type", &types).Error
	return types, err
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
		Find(&clues).Error
	if err != nil {
		return nil, err
	}
	return clues, nil
}

func (r *clueRepo) BatchUpdateInTx(ctx context.Context, ids []string, updates map[string]any) (int, error) {
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range ids {
			if id == "" {
				continue
			}
			if err := tx.Model(&model.Clue{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				continue
			}
			count++
		}
		return nil
	})
	return count, err
}
