package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/timeutil"

	"gorm.io/gorm"
)

// 仓储层哨兵：服务层据此翻成 404（带业务 ref），不再依赖错误文案比较。
var (
	// ErrCommunityGroupNotFound 社群不存在
	ErrCommunityGroupNotFound = errors.New("社群不存在")
	// ErrCommunityMemberNotFound 社群成员不存在
	ErrCommunityMemberNotFound = errors.New("社群成员不存在")
)

type CommunityRepository interface {
	GetGroups(ctx context.Context, page, pageSize int, search string) ([]*model.CommunityGroup, int64, error)
	CreateGroup(ctx context.Context, group *model.CommunityGroup) (*model.CommunityGroup, error)
	UpdateGroup(ctx context.Context, id string, updates map[string]any) error
	DeleteGroup(ctx context.Context, id string) error
	GetGroupByID(ctx context.Context, id string) (*model.CommunityGroup, error)

	GetMembers(ctx context.Context, groupID string, page, pageSize int, search string) ([]*model.CommunityMember, int64, error)
	AddMember(ctx context.Context, member *model.CommunityMember) (*model.CommunityMember, error)
	ExistsMemberByUsername(ctx context.Context, groupID, username string) (bool, error)
	UpdateMember(ctx context.Context, id string, updates map[string]any) error
	RemoveMember(ctx context.Context, id string) error
	GetMemberByID(ctx context.Context, id string) (*model.CommunityMember, error)

	GetMessages(ctx context.Context, groupID string, page, pageSize int) ([]*model.CommunityMessage, int64, error)
	// GetGroupStats 单群统计：成员数 / 消息数 / 今日活跃成员数（last_seen 落在今日内）。
	GetGroupStats(ctx context.Context, groupID string) (members, messages, activeToday int64, name string, err error)
	GetStatistics(ctx context.Context) (*CommunityStatistics, error)
}

// CommunityStatistics 社群统计口径。
type CommunityStatistics struct {
	TotalGroups     int64
	TotalMembers    int64
	TotalMessages   int64
	ActiveGroups    int64
	NewMembersToday int64
}

type communityRepository struct {
	db *gorm.DB
}

func NewCommunityRepository(db *gorm.DB) CommunityRepository {
	return &communityRepository{db: db}
}

// normalizeCommunityPaging 兜底分页：page<1 会算出负 Offset（PG 直接报错），
// pageSize<1 在 PG 里等于不设上限 —— 两者都必须归一。
func normalizeCommunityPaging(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	return page, pageSize
}

func (r *communityRepository) GetGroups(ctx context.Context, page, pageSize int, search string) ([]*model.CommunityGroup, int64, error) {
	page, pageSize = normalizeCommunityPaging(page, pageSize)
	applyFilter := func(tx *gorm.DB) *gorm.DB {
		if search != "" {
			searchPattern := "%" + search + "%"
			tx = tx.Where("name LIKE ? OR description LIKE ?", searchPattern, searchPattern)
		}
		return tx
	}

	var total int64
	if err := applyFilter(r.db.WithContext(ctx).Model(&model.CommunityGroup{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var groups []*model.CommunityGroup
	if err := applyFilter(r.db.WithContext(ctx).Model(&model.CommunityGroup{})).
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&groups).Error; err != nil {
		return nil, 0, err
	}

	return groups, total, nil
}

func (r *communityRepository) CreateGroup(ctx context.Context, group *model.CommunityGroup) (*model.CommunityGroup, error) {
	if err := r.db.WithContext(ctx).Create(group).Error; err != nil {
		return nil, err
	}
	return group, nil
}

func (r *communityRepository) UpdateGroup(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&model.CommunityGroup{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCommunityGroupNotFound
	}
	return nil
}

func (r *communityRepository) DeleteGroup(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&model.CommunityMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&model.CommunityMessage{}).Error; err != nil {
			return err
		}

		result := tx.Where("id = ?", id).Delete(&model.CommunityGroup{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrCommunityGroupNotFound
		}
		return nil
	})
}

func (r *communityRepository) GetGroupByID(ctx context.Context, id string) (*model.CommunityGroup, error) {
	var group model.CommunityGroup
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &group, nil
}

func (r *communityRepository) GetMembers(ctx context.Context, groupID string, page, pageSize int, search string) ([]*model.CommunityMember, int64, error) {
	page, pageSize = normalizeCommunityPaging(page, pageSize)
	applyFilter := func(tx *gorm.DB) *gorm.DB {
		tx = tx.Where("group_id = ?", groupID)
		if search != "" {
			searchPattern := "%" + search + "%"
			tx = tx.Where("name LIKE ? OR username LIKE ?", searchPattern, searchPattern)
		}
		return tx
	}

	var total int64
	if err := applyFilter(r.db.WithContext(ctx).Model(&model.CommunityMember{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var members []*model.CommunityMember
	if err := applyFilter(r.db.WithContext(ctx).Model(&model.CommunityMember{})).
		Order("join_date DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&members).Error; err != nil {
		return nil, 0, err
	}

	return members, total, nil
}

// ExistsMemberByUsername 判断群组内是否已有同名用户名（租户内判定）。
func (r *communityRepository) ExistsMemberByUsername(ctx context.Context, groupID, username string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).
		Where("group_id = ? AND username = ?", groupID, username).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddMember 纯写入 + 群组成员数递增；同事务保证两者要么都成功要么都回滚。
func (r *communityRepository) AddMember(ctx context.Context, member *model.CommunityMember) (*model.CommunityMember, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(member).Error; err != nil {
			return err
		}
		result := tx.Model(&model.CommunityGroup{}).
			Where("id = ?", member.GroupID).
			UpdateColumn("member_count", gorm.Expr("member_count + ?", 1))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return member, nil
}

func (r *communityRepository) UpdateMember(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&model.CommunityMember{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrCommunityMemberNotFound
	}
	return nil
}

func (r *communityRepository) RemoveMember(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var member model.CommunityMember
		if err := tx.Where("id = ?", id).First(&member).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCommunityMemberNotFound
			}
			return err
		}

		result := tx.Where("id = ?", id).Delete(&model.CommunityMember{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrCommunityMemberNotFound
		}

		// 历史数据里 member_count 可能已是 0，减到负数会把统计带偏。
		return tx.Model(&model.CommunityGroup{}).
			Where("id = ?", member.GroupID).
			UpdateColumn("member_count", gorm.Expr("GREATEST(member_count - ?, 0)", 1)).Error
	})
}

func (r *communityRepository) GetMemberByID(ctx context.Context, id string) (*model.CommunityMember, error) {
	var member model.CommunityMember
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

func (r *communityRepository) GetMessages(ctx context.Context, groupID string, page, pageSize int) ([]*model.CommunityMessage, int64, error) {
	page, pageSize = normalizeCommunityPaging(page, pageSize)

	var total int64
	if err := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).
		Where("group_id = ?", groupID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var messages []*model.CommunityMessage
	if err := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).
		Where("group_id = ?", groupID).
		Order("timestamp DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&messages).Error; err != nil {
		return nil, 0, err
	}

	return messages, total, nil
}

// GetGroupStats 单群统计。群不存在时回 ErrCommunityGroupNotFound，供控制器区分 404 与空统计。
func (r *communityRepository) GetGroupStats(ctx context.Context, groupID string) (int64, int64, int64, string, error) {
	var group model.CommunityGroup
	if err := r.db.WithContext(ctx).Where("id = ?", groupID).First(&group).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, 0, 0, "", ErrCommunityGroupNotFound
		}
		return 0, 0, 0, "", err
	}

	var members, messages, activeToday int64
	if err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).
		Where("group_id = ?", groupID).Count(&members).Error; err != nil {
		return 0, 0, 0, "", err
	}
	if err := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).
		Where("group_id = ?", groupID).Count(&messages).Error; err != nil {
		return 0, 0, 0, "", err
	}
	todayStart := timeutil.StartOfBusinessDay(time.Now())
	if err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).
		Where("group_id = ? AND last_seen >= ?", groupID, todayStart).Count(&activeToday).Error; err != nil {
		return 0, 0, 0, "", err
	}

	return members, messages, activeToday, group.Name, nil
}

func (r *communityRepository) GetStatistics(ctx context.Context) (*CommunityStatistics, error) {
	var totalGroups int64
	var totalMembers int64
	var totalMessages int64

	if err := r.db.WithContext(ctx).Model(&model.CommunityGroup{}).Count(&totalGroups).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).Count(&totalMembers).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).Count(&totalMessages).Error; err != nil {
		return nil, err
	}

	var activeGroups int64
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	if err := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).
		Where("created_at >= ?", sevenDaysAgo).
		Distinct("group_id").
		Count(&activeGroups).Error; err != nil {
		return nil, err
	}

	todayStart := timeutil.StartOfBusinessDay(time.Now())
	var newMembersToday int64
	if err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).
		Where("join_date >= ?", todayStart).
		Count(&newMembersToday).Error; err != nil {
		return nil, err
	}

	return &CommunityStatistics{
		TotalGroups:     totalGroups,
		TotalMembers:    totalMembers,
		TotalMessages:   totalMessages,
		ActiveGroups:    activeGroups,
		NewMembersToday: newMembersToday,
	}, nil
}
