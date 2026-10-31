package service

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ErrQuickReplyNotFound 快捷回复不存在（update/delete 的目标 id 查无此条）
var ErrQuickReplyNotFound = errors.New("快捷回复不存在")

// QuickReplyService 快捷回复服务
type QuickReplyService struct {
	replyRepo *repository.QuickReplyRepository
}

// NewQuickReplyService 创建快捷回复服务实例
func NewQuickReplyService() *QuickReplyService {
	return &QuickReplyService{
		replyRepo: repository.NewQuickReplyRepository(),
	}
}

// CreateReplyRequest 创建/更新快捷回复请求
//
// IsPublic 用 *bool 表达 PATCH 语义：nil = 未传 = 保留原值。
// 前端编辑表单不含 is_public 字段，若用 bool 零值会把已有记录打成私有、
// 使其从 is_public=true 的列表查询中消失（R2-1）。
type CreateReplyRequest struct {
	Category  string `json:"category" binding:"required"`
	Title     string `json:"title" binding:"required"`
	Content   string `json:"content" binding:"required"`
	Channel   string `json:"channel"`
	SortOrder int    `json:"sort_order"`
	IsPublic  *bool  `json:"is_public"`
}

// CreateReply 创建快捷回复
func (s *QuickReplyService) CreateReply(ctx context.Context, createdBy uint, req *CreateReplyRequest) (*model.QuickReply, error) {
	// 创建口径保持既有行为：无论显式传什么，新建回复一律公开
	reply := &model.QuickReply{
		Category:  req.Category,
		Title:     req.Title,
		Content:   req.Content,
		Channel:   req.Channel,
		SortOrder: req.SortOrder,
		IsPublic:  true,
		CreatedBy: createdBy,
	}

	if err := s.replyRepo.Create(ctx, reply); err != nil {
		return nil, err
	}

	return reply, nil
}

// UpdateReply 更新快捷回复
func (s *QuickReplyService) UpdateReply(ctx context.Context, id uint, req *CreateReplyRequest) (*model.QuickReply, error) {
	reply, err := s.replyRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %d", ErrQuickReplyNotFound, id)
		}
		return nil, err
	}

	reply.Category = req.Category
	reply.Title = req.Title
	reply.Content = req.Content
	reply.Channel = req.Channel
	reply.SortOrder = req.SortOrder
	// PATCH 语义：未传 is_public 保留原值，防止编辑把公开模板打成私有
	if req.IsPublic != nil {
		reply.IsPublic = *req.IsPublic
	}

	if err := s.replyRepo.Update(ctx, reply); err != nil {
		return nil, err
	}

	return reply, nil
}

// DeleteReply 删除快捷回复
func (s *QuickReplyService) DeleteReply(ctx context.Context, id uint) error {
	reply, err := s.replyRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %d", ErrQuickReplyNotFound, id)
		}
		return err
	}
	_ = reply

	return s.replyRepo.Delete(ctx, id)
}

// GetReplies 获取快捷回复列表
func (s *QuickReplyService) GetReplies(ctx context.Context, category string) ([]*model.QuickReply, error) {
	return s.replyRepo.GetByMerchant(ctx, category)
}

// GetCategories 获取快捷回复分类
func (s *QuickReplyService) GetCategories(ctx context.Context) ([]string, error) {
	return s.replyRepo.GetCategories(ctx)
}
