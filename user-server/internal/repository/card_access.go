package repository

import (
	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"time"

	"gorm.io/gorm"
)

type CardAccessRepository interface {
	Create(ctx context.Context, access *model.CardAccess) error
	GetByCardID(ctx context.Context, cardID uint, page, pageSize int) ([]*model.CardAccess, int64, error)
	CountAccess(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (int, error)
	CountDistinctIP(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (int, error)
	HasAccessToday(ctx context.Context, cardID uint, ip string) (bool, error)
}

type cardAccessRepository struct {
	db *gorm.DB
}

func NewCardAccessRepository(db *gorm.DB) CardAccessRepository {
	return &cardAccessRepository{db: db}
}

func (r *cardAccessRepository) Create(ctx context.Context, access *model.CardAccess) error {
	return r.db.Create(access).Error
}

func (r *cardAccessRepository) GetByCardID(ctx context.Context, cardID uint, page, pageSize int) ([]*model.CardAccess, int64, error) {
	var accesses []*model.CardAccess
	var total int64

	err := r.db.Model(&model.CardAccess{}).Where("card_id = ?", cardID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = r.db.Where("card_id = ?", cardID).Order("access_time DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&accesses).Error

	return accesses, total, err
}

func (r *cardAccessRepository) CountAccess(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (int, error) {
	var count int64
	query := r.db.Model(&model.CardAccess{}).Where("card_id = ?", cardID)

	if cardType != "" {
		query = query.Where("card_type = ?", cardType)
	}

	if !startDate.IsZero() {
		query = query.Where("access_time >= ?", startDate)
	}

	if !endDate.IsZero() {
		query = query.Where("access_time <= ?", endDate)
	}

	err := query.Count(&count).Error
	return int(count), err
}

func (r *cardAccessRepository) CountDistinctIP(ctx context.Context, cardID uint, cardType string, startDate, endDate time.Time) (int, error) {
	var count int64
	query := r.db.Model(&model.CardAccess{}).Where("card_id = ?", cardID)

	if cardType != "" {
		query = query.Where("card_type = ?", cardType)
	}

	if !startDate.IsZero() {
		query = query.Where("access_time >= ?", startDate)
	}

	if !endDate.IsZero() {
		query = query.Where("access_time <= ?", endDate)
	}

	err := query.Distinct("ip_address").Count(&count).Error
	return int(count), err
}

// accessTodayWindow 返回 now 所属业务日（CST）的半开区间 [当日 00:00, 次日 00:00)。
//
// 抽成纯函数是为了可确定性地测：窗口整体漂一天，「同卡同 IP 每日一限」就会
// 在 UTC 容器上变成「同一业务日可访问两次」。
func accessTodayWindow(now time.Time) (start, end time.Time) {
	start = timeutil.StartOfBusinessDay(now)
	return start, start.AddDate(0, 0, 1)
}

func (r *cardAccessRepository) HasAccessToday(ctx context.Context, cardID uint, ip string) (bool, error) {
	start, end := accessTodayWindow(time.Now())

	var count int64
	err := r.db.Model(&model.CardAccess{}).
		Where("card_id = ? AND ip_address = ? AND access_time >= ? AND access_time < ?",
			cardID, ip, start, end).
		Count(&count).Error

	return count > 0, err
}

type DailyCardUVStatsRepository interface {
	Create(ctx context.Context, stats *model.DailyCardUVStats) error
	Update(ctx context.Context, stats *model.DailyCardUVStats) error
	GetByCardID(ctx context.Context, cardID uint, cardType string) ([]*model.DailyCardUVStats, error)
	GetByCardIDAndDate(ctx context.Context, cardID uint, cardType string, date string) (*model.DailyCardUVStats, error)
}

type dailyCardUVStatsRepository struct {
	db *gorm.DB
}

func NewDailyCardUVStatsRepository(db *gorm.DB) DailyCardUVStatsRepository {
	return &dailyCardUVStatsRepository{db: db}
}

func (r *dailyCardUVStatsRepository) Create(ctx context.Context, stats *model.DailyCardUVStats) error {
	return r.db.Create(stats).Error
}

func (r *dailyCardUVStatsRepository) Update(ctx context.Context, stats *model.DailyCardUVStats) error {
	return r.db.Save(stats).Error
}

func (r *dailyCardUVStatsRepository) GetByCardID(ctx context.Context, cardID uint, cardType string) ([]*model.DailyCardUVStats, error) {
	var stats []*model.DailyCardUVStats
	query := r.db.Where("card_id = ?", cardID)

	if cardType != "" {
		query = query.Where("card_type = ?", cardType)
	}

	err := query.Order("date DESC").Find(&stats).Error
	return stats, err
}

func (r *dailyCardUVStatsRepository) GetByCardIDAndDate(ctx context.Context, cardID uint, cardType string, date string) (*model.DailyCardUVStats, error) {
	var stats model.DailyCardUVStats
	err := r.db.Where("card_id = ? AND card_type = ? AND date = ?", cardID, cardType, date).
		First(&stats).Error

	if err == gorm.ErrRecordNotFound {
		return nil, err
	}

	return &stats, err
}
