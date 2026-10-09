package repository

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// NotificationRepository 系统通知仓库
type NotificationRepository struct {
	db *gorm.DB
}

// NewNotificationRepository 创建通知仓库实例
func NewNotificationRepository() *NotificationRepository {
	return &NotificationRepository{db: _db.GetDB()}
}

// NewNotificationRepositoryWithDB 创建指定数据库连接的 NotificationRepository 实例（用于测试 / 服务层注入）
func NewNotificationRepositoryWithDB(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// SetDB 注入 db（用于测试）
//
// 五层架构 §三.5 + §七：仓库方法必须首参为 ctx context.Context。
func (r *NotificationRepository) SetDB(ctx context.Context, db *gorm.DB) {
	if db != nil {
		r.db = db
	}
}

// NotificationListQuery 通知列表查询条件（避免依赖 service 包）
type NotificationListQuery struct {
	UserID  uint
	Page    int
	Size    int
	Type    string
	IsRead  *bool
	Keyword string
}

// NotificationListResult 通知列表查询结果
type NotificationListResult struct {
	List  []model.Notification
	Total int64
}

// List 通知列表查询（user_id = 0 表示全体，与具体 userID 取并集）
func (r *NotificationRepository) List(ctx context.Context, q NotificationListQuery) (*NotificationListResult, error) {
	if r == nil || r.db == nil {
		return &NotificationListResult{}, nil
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 || q.Size > 100 {
		q.Size = 20
	}

	tx := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = 0 OR user_id = ?", q.UserID)

	if q.Type != "" {
		tx = tx.Where("type = ?", q.Type)
	}
	if q.IsRead != nil {
		tx = tx.Where("is_read = ?", *q.IsRead)
	}
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		tx = tx.Where("title ILIKE ? OR content ILIKE ?", like, like)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}

	var list []model.Notification
	if err := tx.Order("created_at DESC").
		Offset((q.Page - 1) * q.Size).
		Limit(q.Size).
		Find(&list).Error; err != nil {
		return nil, err
	}

	return &NotificationListResult{List: list, Total: total}, nil
}

// MarkReadByID 标记单条已读，返回受影响行数（用于权限校验）
func (r *NotificationRepository) MarkReadByID(ctx context.Context, userID, id uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND (user_id = 0 OR user_id = ?)", id, userID).
		Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// MarkAllRead 全部标记已读，返回受影响行数
func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("is_read = ? AND (user_id = 0 OR user_id = ?)", false, userID).
		Updates(map[string]any{
			"is_read": true,
			"read_at": &now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// CountUnread 统计未读数（user_id = 0 与 userID 取并集）
func (r *NotificationRepository) CountUnread(ctx context.Context, userID uint) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("is_read = ? AND (user_id = 0 OR user_id = ?)", false, userID).
		Count(&count).Error
	return count, err
}

// Create 创建一条通知
func (r *NotificationRepository) Create(ctx context.Context, n *model.Notification) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Create(n).Error
}

// CountAll 统计通知总数（用于 SeedIfEmpty 判定）
func (r *NotificationRepository) CountAll(ctx context.Context) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Notification{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountByTypeLinkSince 统计 type=ntype 且 link=link 且 created_at >= since 的通知条数。
//
// 存在的唯一理由是**去重**：待办 SLA 的节拍器每轮都会重新扫到同一批逾期行，
// 没有这一问，"逾期提醒"会在每个节拍给坐席刷一条新通知，提醒本身变成噪音。
// 用 type+link 而不是 metadata 里的 JSON 串做键：前者是建了索引的列，
// 后者要 LIKE 全表，而这张表会被通知中心每次打开都读。
func (r *NotificationRepository) CountByTypeLinkSince(ctx context.Context, ntype, link string, since time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("type = ? AND link = ? AND created_at >= ?", ntype, link, since).
		Count(&count).Error
	return count, err
}
