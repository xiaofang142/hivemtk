package service

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// NotificationService 通知中心服务
type NotificationService struct {
	repo *repository.NotificationRepository
}

// NewNotificationService 构造服务
//
// 五层架构 §三.5：构造函数保留 db *gorm.DB 参数（调用方不变），
// 内部创建 repository 实例，service 不再持有 db。
func NewNotificationService(db *gorm.DB) *NotificationService {
	return &NotificationService{repo: repository.NewNotificationRepositoryWithDB(db)}
}

// ListRequest 列表请求
type NotificationListRequest struct {
	UserID  uint
	Page    int
	Size    int
	Type    string
	IsRead  *bool
	Keyword string
}

// ListResponse 列表响应
type NotificationListResponse struct {
	List  []model.Notification `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}

// List 拉取通知列表
func (s *NotificationService) List(ctx context.Context, req NotificationListRequest) (*NotificationListResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Size < 1 || req.Size > 100 {
		req.Size = 20
	}

	res, err := s.repo.List(ctx, repository.NotificationListQuery{
		UserID:  req.UserID,
		Page:    req.Page,
		Size:    req.Size,
		Type:    req.Type,
		IsRead:  req.IsRead,
		Keyword: req.Keyword,
	})
	if err != nil {
		return nil, err
	}

	return &NotificationListResponse{
		List:  res.List,
		Total: res.Total,
		Page:  req.Page,
		Size:  req.Size,
	}, nil
}

// MarkRead 标记单条已读
func (s *NotificationService) MarkRead(ctx context.Context, userID uint, id uint) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}
	affected, err := s.repo.MarkReadByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("通知不存在或无权限")
	}
	return nil
}

// MarkAllRead 全部标记已读
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uint) (int64, error) {
	return s.repo.MarkAllRead(ctx, userID)
}

// CountUnread 统计未读数
func (s *NotificationService) CountUnread(ctx context.Context, userID uint) (int64, error) {
	return s.repo.CountUnread(ctx, userID)
}

// Create 写入一条通知（供业务调用 / 自动迁移后种子数据）
func (s *NotificationService) Create(ctx context.Context, n *model.Notification) error {
	if n.Type == "" {
		n.Type = model.NotificationTypeInfo
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	return s.repo.Create(ctx, n)
}

// SeedIfEmpty 若表为空，注入演示通知（便于首次访问通知中心有数据可看）
func (s *NotificationService) SeedIfEmpty(ctx context.Context) error {
	count, err := s.repo.CountAll(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	seed := []model.Notification{
		{
			UserID:    0,
			Type:      model.NotificationTypeAnnouncement,
			Title:     "欢迎使用营销管理系统",
			Content:   "系统已完成初始化。您可以在此查看版本公告、安全告警与来自平台的系统通知。",
			Link:      "/profile",
			CreatedAt: time.Now().Add(-72 * time.Hour),
		},
		{
			UserID:    0,
			Type:      model.NotificationTypeInfo,
			Title:     "欢迎使用 HiveMtk（开源版）",
			Content:   "本软件完全开源，可自由使用、部署与二次开发。",
			Link:      "/asset-bundle/list",
			CreatedAt: time.Now().Add(-48 * time.Hour),
		},
		{
			UserID:    0,
			Type:      model.NotificationTypeWarning,
			Title:     "建议启用双因素认证",
			Content:   "为保障账户安全，建议在「个人资料」中为管理员账号绑定二次验证（短信/邮箱）。",
			Link:      "/profile",
			CreatedAt: time.Now().Add(-24 * time.Hour),
		},
		{
			UserID:    0,
			Type:      model.NotificationTypeSuccess,
			Title:     "知识库已就绪",
			Content:   "RAG 知识库配置完成，智能体可基于本地文档回答客户问题。可在「知识中心」上传更多语料。",
			Link:      "/knowledge/management",
			CreatedAt: time.Now().Add(-2 * time.Hour),
		},
	}
	for i := range seed {
		if err := s.Create(ctx, &seed[i]); err != nil {
			return err
		}
	}
	return nil
}
