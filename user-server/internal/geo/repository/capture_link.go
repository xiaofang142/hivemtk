package repository

import (
	"context"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	mainmodel "hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// CaptureLinkQuery 承接链接列表查询。
type CaptureLinkQuery struct {
	Keyword string
	Page    int
	Limit   int
}

// CaptureResponseStats 首响统计。Matched 是能关联到客服会话的线索数；
// Matched=0 表示会话侧还没接入，不是“零延迟”。
type CaptureResponseStats struct {
	Matched    int64   `json:"matched"`
	Replied    int64   `json:"replied"`
	AvgSeconds float64 `json:"avg_seconds"`
}

// GeoCaptureLinkRepository GEO 承接链接仓储。
type GeoCaptureLinkRepository interface {
	Create(ctx context.Context, link *model.GeoCaptureLink) error
	GetByID(ctx context.Context, id uint) (*model.GeoCaptureLink, error)
	GetByCode(ctx context.Context, code string) (*model.GeoCaptureLink, error)
	List(ctx context.Context, q CaptureLinkQuery) ([]*model.GeoCaptureLink, int64, error)
	Delete(ctx context.Context, id uint) error
	UpdateShortLinkID(ctx context.Context, id, shortLinkID uint) error
	IncrementClicks(ctx context.Context, id uint) error
	// ReserveAsk 原子占一次当日 FAQ 配额；返回 false 表示超限。
	ReserveAsk(ctx context.Context, id uint, day time.Time, limit int) (bool, error)
	// ReserveLead 原子占一次当日留资配额并累加 leads；返回 false 表示超限。
	ReserveLead(ctx context.Context, id uint, day time.Time, limit int) (bool, error)
	// ReleaseLead 留资落库失败时回滚配额与计数。
	ReleaseLead(ctx context.Context, id uint, day time.Time) error
	ResponseStats(ctx context.Context, sourceID string) (CaptureResponseStats, error)
}

type geoCaptureLinkRepo struct{ db *gorm.DB }

func NewGeoCaptureLinkRepository() GeoCaptureLinkRepository {
	return &geoCaptureLinkRepo{db: _db.GetDB()}
}

func NewGeoCaptureLinkRepositoryWithDB(db *gorm.DB) GeoCaptureLinkRepository {
	return &geoCaptureLinkRepo{db: db}
}

func (r *geoCaptureLinkRepo) Create(ctx context.Context, link *model.GeoCaptureLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *geoCaptureLinkRepo) GetByID(ctx context.Context, id uint) (*model.GeoCaptureLink, error) {
	var link model.GeoCaptureLink
	if err := r.db.WithContext(ctx).First(&link, id).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *geoCaptureLinkRepo) GetByCode(ctx context.Context, code string) (*model.GeoCaptureLink, error) {
	var link model.GeoCaptureLink
	if err := r.db.WithContext(ctx).Where("code = ?", strings.TrimSpace(code)).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *geoCaptureLinkRepo) List(ctx context.Context, q CaptureLinkQuery) ([]*model.GeoCaptureLink, int64, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	db := r.db.WithContext(ctx).Model(&model.GeoCaptureLink{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("code LIKE ? OR title LIKE ? OR keyword LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []*model.GeoCaptureLink
	err := db.Order("id DESC").Offset((q.Page - 1) * q.Limit).Limit(q.Limit).Find(&list).Error
	return list, total, err
}

func (r *geoCaptureLinkRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.GeoCaptureLink{}, id).Error
}

func (r *geoCaptureLinkRepo) UpdateShortLinkID(ctx context.Context, id, shortLinkID uint) error {
	return r.db.WithContext(ctx).Model(&model.GeoCaptureLink{}).
		Where("id = ?", id).Update("short_link_id", shortLinkID).Error
}

func (r *geoCaptureLinkRepo) IncrementClicks(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&model.GeoCaptureLink{}).
		Where("id = ?", id).UpdateColumn("clicks", gorm.Expr("clicks + 1")).Error
}

func (r *geoCaptureLinkRepo) ReserveAsk(ctx context.Context, id uint, day time.Time, limit int) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.GeoCaptureLink{}).
		Where("id = ? AND (ask_date IS NULL OR ask_date <> ? OR ask_count < ?)", id, day, limit).
		Updates(map[string]any{
			"ask_date":  day,
			"ask_count": gorm.Expr("CASE WHEN ask_date = ? THEN ask_count + 1 ELSE 1 END", day),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *geoCaptureLinkRepo) ReserveLead(ctx context.Context, id uint, day time.Time, limit int) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.GeoCaptureLink{}).
		Where("id = ? AND (lead_date IS NULL OR lead_date <> ? OR lead_count < ?)", id, day, limit).
		Updates(map[string]any{
			"lead_date":  day,
			"lead_count": gorm.Expr("CASE WHEN lead_date = ? THEN lead_count + 1 ELSE 1 END", day),
			"leads":      gorm.Expr("leads + 1"),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *geoCaptureLinkRepo) ReleaseLead(ctx context.Context, id uint, day time.Time) error {
	return r.db.WithContext(ctx).Model(&model.GeoCaptureLink{}).
		Where("id = ? AND lead_date = ? AND lead_count > 0", id, day).
		Updates(map[string]any{
			"lead_count": gorm.Expr("lead_count - 1"),
			"leads":      gorm.Expr("CASE WHEN leads > 0 THEN leads - 1 ELSE 0 END"),
		}).Error
}

// ResponseStats 首响统计：只统计「线索创建之后」的人工首响，
// 且必须能通过 one_id 关联到客服会话。没有会话的线索不计入 matched。
//
// 走 GORM API 而不是手写 JOIN：多租户隔离 callback 会拒绝引用租户表的
// 预构建 SQL（check-gorm-only 门禁），两条带模型查询各自由 callback 补租户。
func (r *geoCaptureLinkRepo) ResponseStats(ctx context.Context, sourceID string) (CaptureResponseStats, error) {
	var out CaptureResponseStats
	var clues []struct {
		ID         string
		OneID      string
		CreateTime int64
	}
	if err := r.db.WithContext(ctx).Model(&mainmodel.Clue{}).
		Select("id, one_id, create_time").
		Where("source_id = ? AND one_id <> ''", sourceID).
		Find(&clues).Error; err != nil {
		return out, err
	}
	if len(clues) == 0 {
		return out, nil
	}
	oneIDs := make([]string, 0, len(clues))
	seen := make(map[string]bool, len(clues))
	for _, c := range clues {
		if !seen[c.OneID] {
			seen[c.OneID] = true
			oneIDs = append(oneIDs, c.OneID)
		}
	}
	var sessions []struct {
		OneID             string
		FirstHumanReplyAt *time.Time
	}
	if err := r.db.WithContext(ctx).Model(&mainmodel.CustomerSession{}).
		Select("one_id, first_human_reply_at").
		Where("one_id IN ?", oneIDs).
		Find(&sessions).Error; err != nil {
		return out, err
	}
	byOne := make(map[string][]*time.Time, len(sessions))
	for _, s := range sessions {
		byOne[s.OneID] = append(byOne[s.OneID], s.FirstHumanReplyAt)
	}

	var totalSeconds float64
	for _, c := range clues {
		replies, ok := byOne[c.OneID]
		if !ok {
			continue
		}
		out.Matched++
		created := time.Unix(c.CreateTime, 0)
		var earliest *time.Time
		for _, reply := range replies {
			if reply == nil || reply.Before(created) {
				continue
			}
			if earliest == nil || reply.Before(*earliest) {
				earliest = reply
			}
		}
		if earliest != nil {
			out.Replied++
			totalSeconds += earliest.Sub(created).Seconds()
		}
	}
	if out.Replied > 0 {
		out.AvgSeconds = totalSeconds / float64(out.Replied)
	}
	return out, nil
}
