package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ErrSessionTagCodeExists 标签编码重复（唯一索引 idx_session_tags_code）。
//
// 在 service 层归一：controller 不允许 import repository（depguard），
// 而 PG 的 duplicate key 原文（索引名 + SQL 片段）也不该直接回给前端。
var ErrSessionTagCodeExists = errors.New("标签编码已存在")

// ErrSessionTagNotFound 会话标签不存在（update/delete 的目标 id 查无此条）
var ErrSessionTagNotFound = errors.New("会话标签不存在")

// SessionTagService 会话标签服务
type SessionTagService struct {
	tagRepo *repository.SessionTagRepository
}

// NewSessionTagService 创建会话标签服务实例
func NewSessionTagService() *SessionTagService {
	return &SessionTagService{
		tagRepo: repository.NewSessionTagRepository(),
	}
}

// CreateTagRequest 创建标签请求
type CreateTagRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Group       string `json:"group"`
	Color       string `json:"color"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// CreateTag 创建标签
func (s *SessionTagService) CreateTag(ctx context.Context, req *CreateTagRequest) (*model.SessionTag, error) {
	tag := &model.SessionTag{
		Name:        req.Name,
		Code:        req.Code,
		Group:       req.Group,
		Color:       req.Color,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if tag.Color == "" {
		tag.Color = "#1890ff"
	}

	if err := s.tagRepo.Create(ctx, tag); err != nil {
		if repository.IsDuplicateKeyErr(err) {
			return nil, ErrSessionTagCodeExists
		}
		return nil, err
	}

	return tag, nil
}

// UpdateTag 更新标签
func (s *SessionTagService) UpdateTag(ctx context.Context, id uint, req *CreateTagRequest) (*model.SessionTag, error) {
	tag, err := s.tagRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %d", ErrSessionTagNotFound, id)
		}
		return nil, err
	}

	tag.Name = req.Name
	tag.Code = req.Code
	tag.Group = req.Group
	tag.Color = req.Color
	tag.Description = req.Description
	tag.SortOrder = req.SortOrder

	if err := s.tagRepo.Update(ctx, tag); err != nil {
		// 改编码撞唯一索引同样是「这个编码已经有人用了」，不是 500。
		if repository.IsDuplicateKeyErr(err) {
			return nil, ErrSessionTagCodeExists
		}
		return nil, err
	}

	return tag, nil
}

// DeleteTag 删除标签
func (s *SessionTagService) DeleteTag(ctx context.Context, id uint) error {
	tag, err := s.tagRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %d", ErrSessionTagNotFound, id)
		}
		return err
	}
	_ = tag

	return s.tagRepo.Delete(ctx, id)
}

// GetTags 获取标签列表
func (s *SessionTagService) GetTags(ctx context.Context) ([]*model.SessionTag, error) {
	return s.tagRepo.GetByMerchant(ctx)
}
