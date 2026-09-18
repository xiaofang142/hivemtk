package repository

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
	"hivemtk-user/internal/pkg/timeutil"
)

type CommunityRepository interface {
	GetGroups(ctx context.Context, page, pageSize int, search string) ([]*model.CommunityGroup, int64, error)
	CreateGroup(ctx context.Context, group *model.CommunityGroup) (*model.CommunityGroup, error)
	UpdateGroup(ctx context.Context, id string, updates map[string]any) error
	DeleteGroup(ctx context.Context, id string) error
	GetGroupByID(ctx context.Context, id string) (*model.CommunityGroup, error)

	GetMembers(ctx context.Context, groupID string, page, pageSize int, search string) ([]*model.CommunityMember, int64, error)
	AddMember(ctx context.Context, member *model.CommunityMember) (*model.CommunityMember, error)
	UpdateMember(ctx context.Context, id string, updates map[string]any) error
	RemoveMember(ctx context.Context, id string) error
	GetMemberByID(ctx context.Context, id string) (*model.CommunityMember, error)

	GetMessages(ctx context.Context, groupID string, page, pageSize int) ([]*model.CommunityMessage, int64, error)
	AddMessage(ctx context.Context, message *model.CommunityMessage) (*model.CommunityMessage, error)
	GetStatistics(ctx context.Context) (*map[string]any, error)
}

type communityRepository struct {
	db *gorm.DB
}

func NewCommunityRepository(db *gorm.DB) CommunityRepository {
	return &communityRepository{db: db}
}

func NewCommunityRepositoryDefault() CommunityRepository {
	return &communityRepository{db: _db.GetDB()}
}

func (r *communityRepository) GetGroups(ctx context.Context, page, pageSize int, search string) ([]*model.CommunityGroup, int64, error) {
	var groups []*model.CommunityGroup
	var total int64

	query := r.db.WithContext(ctx).Model(&model.CommunityGroup{})

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Find(&groups).Error; err != nil {
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
		return errors.New("社群不存在")
	}
	return nil
}

func (r *communityRepository) DeleteGroup(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("group_id = ?", id).Delete(&model.CommunityMember{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("group_id = ?", id).Delete(&model.CommunityMessage{}).Error; err != nil {
		return err
	}

	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CommunityGroup{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("社群不存在")
	}
	return nil
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
	var members []*model.CommunityMember
	var total int64

	query := r.db.WithContext(ctx).Model(&model.CommunityMember{}).Where("group_id = ?", groupID)

	if search != "" {
		searchPattern := "%" + search + "%"
		query = query.Where("name LIKE ? OR username LIKE ?", searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Find(&members).Error; err != nil {
		return nil, 0, err
	}

	return members, total, nil
}

func (r *communityRepository) AddMember(ctx context.Context, member *model.CommunityMember) (*model.CommunityMember, error) {

	var existingMember model.CommunityMember
	result := r.db.WithContext(ctx).Where("group_id = ? AND username = ?", member.GroupID, member.Username).First(&existingMember)
	if result.Error == nil {
		return nil, errors.New("该用户名已在群组中")
	}

	if err := r.db.WithContext(ctx).Create(member).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Model(&model.CommunityGroup{}).Where("id = ?", member.GroupID).UpdateColumn("member_count", gorm.Expr("member_count + ?", 1)).Error; err != nil {
		return member, err
	}

	return member, nil
}

func (r *communityRepository) UpdateMember(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&model.CommunityMember{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("社群成员不存在")
	}
	return nil
}

func (r *communityRepository) RemoveMember(ctx context.Context, id string) error {
	var member model.CommunityMember
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("社群成员不存在")
		}
		return err
	}

	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.CommunityMember{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("社群成员不存在")
	}

	if err := r.db.WithContext(ctx).Model(&model.CommunityGroup{}).Where("id = ?", member.GroupID).UpdateColumn("member_count", gorm.Expr("member_count - ?", 1)).Error; err != nil {
		return err
	}

	return nil
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
	var messages []*model.CommunityMessage
	var total int64

	query := r.db.WithContext(ctx).Model(&model.CommunityMessage{}).Where("group_id = ?", groupID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Order("timestamp DESC").Find(&messages).Error; err != nil {
		return nil, 0, err
	}

	return messages, total, nil
}

func (r *communityRepository) AddMessage(ctx context.Context, message *model.CommunityMessage) (*model.CommunityMessage, error) {
	if err := r.db.WithContext(ctx).Create(message).Error; err != nil {
		return nil, err
	}
	return message, nil
}

func (r *communityRepository) GetStatistics(ctx context.Context) (*map[string]any, error) {
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

	todayStart := timeutil.StartOfDay(time.Now())
	var newMembersToday int64
	if err := r.db.WithContext(ctx).Model(&model.CommunityMember{}).
		Where("join_date >= ?", todayStart).
		Count(&newMembersToday).Error; err != nil {
		return nil, err
	}

	stats := map[string]any{
		"total_groups":      totalGroups,
		"total_members":     totalMembers,
		"total_messages":    totalMessages,
		"active_groups":     activeGroups,
		"new_members_today": newMembersToday,
	}

	return &stats, nil
}
