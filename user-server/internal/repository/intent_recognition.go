package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
)

type IntentRecordRepository struct {
	db *gorm.DB
}

func NewIntentRecordRepository() *IntentRecordRepository {
	return &IntentRecordRepository{
		db: _db.GetDB(),
	}
}

func (r *IntentRecordRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

func (r *IntentRecordRepository) GetDB(ctx context.Context) *gorm.DB {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx)
}

func (r *IntentRecordRepository) Create(ctx context.Context, rec *model.IntentRecord) error {
	return r.db.WithContext(ctx).Create(rec).Error
}

// ListSince 查询指定时间之后的所有记录
func (r *IntentRecordRepository) ListSince(ctx context.Context, since time.Time) ([]model.IntentRecord, error) {
	var records []model.IntentRecord
	if err := r.db.WithContext(ctx).Where("created_at > ?", since).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// ListByCustomerID 按 customer_id 查询，按 created_at 倒序，限制条数
func (r *IntentRecordRepository) ListByCustomerID(ctx context.Context, customerID string, limit int) ([]model.IntentRecord, error) {
	var records []model.IntentRecord
	if err := r.db.WithContext(ctx).Where("customer_id = ?", customerID).
		Order("created_at DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// ListPaged 分页查询意图记录，支持 customerID 和 intentType 可选过滤。
func (r *IntentRecordRepository) ListPaged(ctx context.Context, customerID, intentType string, page, pageSize int) ([]model.IntentRecord, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.IntentRecord{})
	if customerID != "" {
		q = q.Where("customer_id = ?", customerID)
	}
	if intentType != "" {
		q = q.Where("intent_type = ?", intentType)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []model.IntentRecord
	if err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

type IntentMethodLevelRow struct {
	Method          string
	ConfidenceLevel string
}

// GetMethodLevelStatsSince 获取 method/level 维度统计（created_at > since），
// 返回原始行，由 service 聚合为 byMethod/byLevel。
func (r *IntentRecordRepository) GetMethodLevelStatsSince(ctx context.Context, since time.Time) ([]IntentMethodLevelRow, error) {
	var rows []IntentMethodLevelRow
	if err := r.db.WithContext(ctx).Model(&model.IntentRecord{}).
		Select("method, confidence_level").
		Where("created_at > ?", since).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

type IntentLogRepository struct {
	db *gorm.DB
}

func NewIntentLogRepository() *IntentLogRepository {
	return &IntentLogRepository{
		db: _db.GetDB(),
	}
}

func (r *IntentLogRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// Create 创建精细意图识别日志（写入 intent_records，强制 source='fine_grained'）
func (r *IntentLogRepository) Create(ctx context.Context, log *model.IntentLog) error {
	if log.Source == "" {
		log.Source = model.IntentLogSource
	}
	return r.db.WithContext(ctx).Create(log).Error
}

// List 查询精细意图识别日志（customerID/major 空字符串表示不筛选；limit 收口到 1~1000）。
// GetByID 单条日志（TB-09 纠错面）。
func (r *IntentLogRepository) GetByID(ctx context.Context, id uint) (*model.IntentLog, error) {
	var rec model.IntentLog
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *IntentLogRepository) List(ctx context.Context, customerID, major string, limit int) ([]model.IntentLog, error) {
	q := r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Where("source = ?", model.IntentLogSource)
	if customerID != "" {
		q = q.Where("customer_id = ?", customerID)
	}
	if major != "" {
		q = q.Where("intent_type = ?", major)
	}
	// limit 由 HTTP query 经 strconv.Atoi 转入且解析错误被丢弃：
	// Limit(-1) 等于取消 LIMIT（全表扫描）、0 是恒空、过大也是全表，这里统一收口。
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var logs []model.IntentLog
	if err := q.Order("timestamp DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// ListByTraceID 按 trace_id 查询，按 timestamp 升序
func (r *IntentLogRepository) ListByTraceID(ctx context.Context, traceID string) ([]model.IntentLog, error) {
	var logs []model.IntentLog
	if err := r.db.WithContext(ctx).Where("trace_id = ? AND source = ?", traceID, model.IntentLogSource).
		Order("timestamp ASC").Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// IntentLogMajorStat 按 major 聚合的统计行
type IntentLogMajorStat struct {
	IntentMajor string  `json:"intent_major"`
	Count       int64   `json:"count"`
	AvgConf     float64 `json:"avg_confidence"`
}

// IntentLogMinorStat 按 major+minor 聚合的统计行
type IntentLogMinorStat struct {
	IntentMinor string `json:"intent_minor"`
	IntentMajor string `json:"intent_major"`
	Count       int64  `json:"count"`
}

// IntentLogMethodStat 按 method 聚合的统计行
type IntentLogMethodStat struct {
	Method string `json:"method"`
	Count  int64  `json:"count"`
}

// GetMajorStatsSince 按 major 聚合统计（timestamp > since，仅 fine_grained 来源）
func (r *IntentLogRepository) GetMajorStatsSince(ctx context.Context, since time.Time) ([]IntentLogMajorStat, error) {
	var stats []IntentLogMajorStat
	if err := r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Select("intent_type as intent_major, COUNT(*) as count, AVG(confidence) as avg_conf").
		Where("timestamp > ? AND source = ?", since, model.IntentLogSource).
		Group("intent_type").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

// GetMinorStatsSince 按 major+minor 聚合统计（timestamp > since，仅 fine_grained 来源）
func (r *IntentLogRepository) GetMinorStatsSince(ctx context.Context, since time.Time) ([]IntentLogMinorStat, error) {
	var stats []IntentLogMinorStat
	if err := r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Select("intent_type as intent_major, intent_subtype as intent_minor, COUNT(*) as count").
		Where("timestamp > ? AND source = ?", since, model.IntentLogSource).
		Group("intent_type, intent_subtype").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

// GetMethodStatsSince 按 method 聚合统计（timestamp > since，仅 fine_grained 来源）
func (r *IntentLogRepository) GetMethodStatsSince(ctx context.Context, since time.Time) ([]IntentLogMethodStat, error) {
	var stats []IntentLogMethodStat
	if err := r.db.WithContext(ctx).Model(&model.IntentLog{}).
		Select("method, COUNT(*) as count").
		Where("timestamp > ? AND source = ?", since, model.IntentLogSource).
		Group("method").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}
