package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	sysmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
)

// StatsRepository 统计仓库接口
type StatsRepository interface {
	CreateAPILog(ctx context.Context, log *sysmodel.APILog) error
	GetAPILogs(ctx context.Context, licenseID string, startTime, endTime time.Time, limit int) ([]*sysmodel.APILog, error)
	GetAPICallCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error)
	GetAPIErrorCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error)
	GetAverageResponseTime(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error)

	CreateVisitLog(ctx context.Context, log *sysmodel.VisitLog) error
	GetVisitLogs(ctx context.Context, licenseID string, startTime, endTime time.Time, limit int) ([]*sysmodel.VisitLog, error)
	GetVisitCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error)
	GetUniqueVisitors(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error)

	GetOrCreateDailyStats(ctx context.Context, licenseID string, date string) (*sysmodel.DailyStats, error)
	UpdateDailyStats(ctx context.Context, stats *sysmodel.DailyStats) error
	GetDailyStats(ctx context.Context, licenseID string, startDate, endDate string) ([]*sysmodel.DailyStats, error)
	GetDailyStatsSummary(ctx context.Context, startDate, endDate string) ([]*sysmodel.DailyStats, error)

	CreateSystemMetrics(ctx context.Context, metrics *sysmodel.SystemMetrics) error
	GetLatestSystemMetrics(ctx context.Context) (*sysmodel.SystemMetrics, error)
	GetSystemMetrics(ctx context.Context, startTime, endTime time.Time, limit int) ([]*sysmodel.SystemMetrics, error)

	GetStatsSummary(ctx context.Context, licenseID string, startTime, endTime time.Time) (map[string]any, error)
}

type statsRepository struct {
	db *gorm.DB
}

func NewStatsRepository(db *gorm.DB) StatsRepository {
	return &statsRepository{db: db}
}

// CreateAPILog 写入 API 日志。OPT-SEC-04：敏感字段（IP/UA）落库前加密，
// 存量明文与新密文双轨共存，读取出口统一解密。
func (r *statsRepository) CreateAPILog(ctx context.Context, log *sysmodel.APILog) error {
	log.IPAddress = dbencrypt.Encrypt(log.IPAddress)
	log.UserAgent = dbencrypt.Encrypt(log.UserAgent)
	return r.db.WithContext(ctx).Create(log).Error
}

// GetAPILogs 读取 API 日志列表。OPT-SEC-04：出口统一解密，明文行原样透传。
func (r *statsRepository) GetAPILogs(ctx context.Context, licenseID string, startTime, endTime time.Time, limit int) ([]*sysmodel.APILog, error) {
	var logs []*sysmodel.APILog
	query := r.db.WithContext(ctx).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	for _, l := range logs {
		l.IPAddress = dbencrypt.Decrypt(l.IPAddress)
		l.UserAgent = dbencrypt.Decrypt(l.UserAgent)
	}
	return logs, err
}

func (r *statsRepository) GetAPICallCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.APILog{}).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Count(&count).Error
	return count, err
}

func (r *statsRepository) GetAPIErrorCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.APILog{}).
		Where("license_id = ? AND created_at >= ? AND created_at <= ? AND status_code >= 400", licenseID, startTime, endTime).
		Count(&count).Error
	return count, err
}

func (r *statsRepository) GetAverageResponseTime(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error) {
	var avgTime float64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.APILog{}).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Select("COALESCE(AVG(duration), 0)").
		Scan(&avgTime).Error
	return int64(avgTime), err
}

func (r *statsRepository) CreateVisitLog(ctx context.Context, log *sysmodel.VisitLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *statsRepository) GetVisitLogs(ctx context.Context, licenseID string, startTime, endTime time.Time, limit int) ([]*sysmodel.VisitLog, error) {
	var logs []*sysmodel.VisitLog
	query := r.db.WithContext(ctx).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&logs).Error
	return logs, err
}

func (r *statsRepository) GetVisitCount(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.VisitLog{}).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Count(&count).Error
	return count, err
}

func (r *statsRepository) GetUniqueVisitors(ctx context.Context, licenseID string, startTime, endTime time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&sysmodel.VisitLog{}).
		Where("license_id = ? AND created_at >= ? AND created_at <= ?", licenseID, startTime, endTime).
		Distinct("ip_address").
		Count(&count).Error
	return count, err
}

func (r *statsRepository) GetOrCreateDailyStats(ctx context.Context, licenseID string, date string) (*sysmodel.DailyStats, error) {
	var stats sysmodel.DailyStats
	err := r.db.WithContext(ctx).
		Where("license_id = ? AND date = ?", licenseID, date).
		First(&stats).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		stats = sysmodel.DailyStats{
			LicenseID:       licenseID,
			Date:            date,
			APICalls:        0,
			Visits:          0,
			UniqueVisitors:  0,
			ErrorCount:      0,
			AvgResponseTime: 0,
		}
		err = r.db.WithContext(ctx).Create(&stats).Error
	}

	return &stats, err
}

func (r *statsRepository) UpdateDailyStats(ctx context.Context, stats *sysmodel.DailyStats) error {
	return r.db.WithContext(ctx).Save(stats).Error
}

func (r *statsRepository) GetDailyStats(ctx context.Context, licenseID string, startDate, endDate string) ([]*sysmodel.DailyStats, error) {
	var stats []*sysmodel.DailyStats
	err := r.db.WithContext(ctx).
		Where("license_id = ? AND date >= ? AND date <= ?", licenseID, startDate, endDate).
		Order("date ASC").
		Find(&stats).Error
	return stats, err
}

func (r *statsRepository) GetDailyStatsSummary(ctx context.Context, startDate, endDate string) ([]*sysmodel.DailyStats, error) {
	var stats []*sysmodel.DailyStats
	err := r.db.WithContext(ctx).
		Where("date >= ? AND date <= ?", startDate, endDate).
		Order("date ASC").
		Find(&stats).Error
	return stats, err
}

func (r *statsRepository) CreateSystemMetrics(ctx context.Context, metrics *sysmodel.SystemMetrics) error {
	return r.db.WithContext(ctx).Create(metrics).Error
}

func (r *statsRepository) GetLatestSystemMetrics(ctx context.Context) (*sysmodel.SystemMetrics, error) {
	var metrics sysmodel.SystemMetrics
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		First(&metrics).Error
	return &metrics, err
}

func (r *statsRepository) GetSystemMetrics(ctx context.Context, startTime, endTime time.Time, limit int) ([]*sysmodel.SystemMetrics, error) {
	var metrics []*sysmodel.SystemMetrics
	query := r.db.WithContext(ctx).
		Where("created_at >= ? AND created_at <= ?", startTime, endTime).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&metrics).Error
	return metrics, err
}

func (r *statsRepository) GetStatsSummary(ctx context.Context, licenseID string, startTime, endTime time.Time) (map[string]any, error) {
	result := make(map[string]any)

	apiCalls, err := r.GetAPICallCount(ctx, licenseID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result["api_calls"] = apiCalls

	apiErrors, err := r.GetAPIErrorCount(ctx, licenseID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result["api_errors"] = apiErrors

	avgResponseTime, err := r.GetAverageResponseTime(ctx, licenseID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result["avg_response_time"] = avgResponseTime

	visits, err := r.GetVisitCount(ctx, licenseID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result["visits"] = visits

	uniqueVisitors, err := r.GetUniqueVisitors(ctx, licenseID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result["unique_visitors"] = uniqueVisitors

	errorRate := float64(0)
	if apiCalls > 0 {
		errorRate = float64(apiErrors) / float64(apiCalls) * 100
	}
	result["error_rate"] = fmt.Sprintf("%.2f%%", errorRate)

	return result, nil
}
