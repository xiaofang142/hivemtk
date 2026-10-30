package service

import (
	"context"
	"errors"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// NotificationService 通知中心服务
type NotificationService struct {
	repo     *repository.NotificationRepository
	readRepo *repository.NotificationReadRepository
}

// NewNotificationService 构造服务
//
// 五层架构 §三.5：构造函数保留 db *gorm.DB 参数（调用方不变），
// 内部创建 repository 实例，service 不再持有 db。
func NewNotificationService() *NotificationService {
	return &NotificationService{
		repo:     repository.NewNotificationRepository(),
		readRepo: repository.NewNotificationReadRepository(),
	}
}

// NotificationListRequest 列表请求
type NotificationListRequest struct {
	UserID  uint
	Page    int
	Size    int
	Type    string
	Level   string
	IsRead  *bool
	Keyword string
}

// NotificationListResponse 列表响应
type NotificationListResponse struct {
	List  []model.Notification `json:"list"`
	Total int64                `json:"total"`
	Page  int                  `json:"page"`
	Size  int                  `json:"size"`
}

// List 拉取通知列表
//
// 平台消息（type = platform_notice）的 is_read **按当前用户**计算：
// 该用户有没有对应的已读回执。本地业务通知沿用行上的 is_read
// （它们的受众是管理员/审核人，"任一人读过即算"是对的）。
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
		Level:   req.Level,
		IsRead:  req.IsRead,
		Keyword: req.Keyword,
	})
	if err != nil {
		return nil, err
	}

	if err := s.applyReadState(ctx, req.UserID, res.List); err != nil {
		return nil, err
	}

	return &NotificationListResponse{
		List:  res.List,
		Total: res.Total,
		Page:  req.Page,
		Size:  req.Size,
	}, nil
}

// applyReadState 把平台消息的已读状态按当前用户的回执改写。
//
// 为什么必须改写：notifications.is_read 是**消息行上的单一布尔位**，
// 平台消息一条对应 N 个用户，A 点已读会把 B 看到的同一条也变成已读
// ⇒ 未读角标被一个人清零。回执表把"消息 × 人"的已读关系显式建模，
// 这里把行上的默认值换成"当前用户视角"的真值。
//
// 本地业务通知不动：它们的受众是管理员/审核人，任一人读过即算。
func (s *NotificationService) applyReadState(ctx context.Context, userID uint, list []model.Notification) error {
	if len(list) == 0 {
		return nil
	}
	tenantID := currentTenantID(ctx)

	read, err := s.readRepo.ReadNotificationIDs(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	for i := range list {
		if list[i].Type != model.NotificationTypePlatformNotice {
			continue
		}
		list[i].IsRead = read[list[i].ID]
		if list[i].IsRead {
			// 回执的 read_at 不在本表里；置一个非零值让前端能区分
			// "已读"与"未读"（有些列表按 read_at 排序）。
			now := time.Now()
			list[i].ReadAt = &now
		} else {
			list[i].ReadAt = nil
		}
	}
	return nil
}

// MarkRead 标记单条已读
//
// 平台消息写已读回执（按人独立）；本地业务通知沿用行上的 is_read。
// ErrNotificationNotFound 通知不存在或不属于当前用户/租户。
// 用哨兵而不是裸字符串：控制器要按它翻 404，而不是把「找不到」当参数错误回 400。
var ErrNotificationNotFound = errors.New("通知不存在或无权限")

func (s *NotificationService) MarkRead(ctx context.Context, userID uint, id uint) error {
	if id == 0 {
		return errors.New("id 不能为空")
	}

	tenantID := currentTenantID(ctx)

	// 先确认这条通知属于当前租户 —— 不属于就报"不存在或无权限"，
	// 不泄露"这条消息存在但不属于你"。
	belongs, err := s.belongsToTenant(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if !belongs {
		return ErrNotificationNotFound
	}

	isPlatform, err := s.isPlatformNotice(ctx, id)
	if err != nil {
		return err
	}
	if isPlatform {
		return s.readRepo.MarkRead(ctx, tenantID, id, userID)
	}

	affected, err := s.repo.MarkReadByID(ctx, userID, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotificationNotFound
	}
	return nil
}

// MarkAllRead 全部标记已读
//
// 平台消息批量写回执；本地业务通知沿用行上的 is_read。
func (s *NotificationService) MarkAllRead(ctx context.Context, userID uint) (int64, error) {
	tenantID := currentTenantID(ctx)

	marked, err := s.readRepo.MarkAllRead(ctx, tenantID, userID)
	if err != nil {
		return 0, err
	}

	local, err := s.repo.MarkAllRead(ctx, userID)
	if err != nil {
		return marked, err
	}
	return marked + local, nil
}

// CountUnread 统计未读数
//
// 平台消息未读 = 该租户内该用户**无回执**的平台消息条数；
// 本地业务通知沿用既有口径（行上的 is_read 未置位）。
func (s *NotificationService) CountUnread(ctx context.Context, userID uint) (int64, error) {
	tenantID := currentTenantID(ctx)

	platformUnread, err := s.readRepo.CountUnread(ctx, tenantID, userID)
	if err != nil {
		return 0, err
	}

	local, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return 0, err
	}
	return platformUnread + local, nil
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

// HasRecentByTypeLink 问一句"这条提醒在最近 window 里发过没有"（SLA 去重用）。
//
// 去重键 = (type, link)：待办 SLA 的节拍器每轮都扫到同一批逾期行，
// 靠"发过就别发"才不至于把提醒刷成噪音。判据放在这一层而不是调用方，
// 是因为"查过没有"和"没有才写"必须同一事务口径：分开写会有 TOCTOU，
// 两个并发节拍会各自查到 0、各写一条。
func (s *NotificationService) HasRecentByTypeLink(ctx context.Context, ntype, link string, since time.Time) (bool, error) {
	n, err := s.repo.CountByTypeLinkSince(ctx, ntype, link, since)
	if err != nil {
		return false, err
	}
	return n > 0, nil
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

// belongsToTenant 判断该通知是否属于当前租户。
func (s *NotificationService) belongsToTenant(ctx context.Context, tenantID, id uint) (bool, error) {
	return s.repo.ExistsInTenant(ctx, tenantID, id)
}

// isPlatformNotice 判断该通知是否是平台下发的消息。
func (s *NotificationService) isPlatformNotice(ctx context.Context, id uint) (bool, error) {
	return s.repo.IsPlatformNotice(ctx, id)
}

// currentTenantID 取当前上下文的租户号。
//
// 取不到时回落默认租户：离线路径（TENANT_MODE=off）本就只有默认租户，
// 而 on 模式下请求期一定有租户绑定（TenantMiddleware 负责）。
func currentTenantID(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id != 0 {
		return id
	}
	return tenant.DefaultTenantID
}
