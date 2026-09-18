package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"time"

	"gorm.io/gorm"
)

// TikTokCardService TikTok 卡片服务接口
type TikTokCardService interface {
	Create(ctx context.Context, req *dto.TikTokCardCreateRequest) (*dto.TikTokCardResponse, error)
	Update(ctx context.Context, req *dto.TikTokCardUpdateRequest) (*dto.TikTokCardResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.TikTokCardResponse, error)
	GetCardModelByID(ctx context.Context, id uint) (*model.TikTokCard, error)
	GetList(ctx context.Context, req *dto.TikTokCardListRequest) (*dto.TikTokCardListResponse, error)
	GenerateShortLink(ctx context.Context, cardID uint) (*dto.TikTokCardResponse, error)
	StatsOverall(ctx context.Context) (*dto.TikTokCardStatsOverallResponse, error)
	Stats(ctx context.Context, cardID uint) (*dto.TikTokCardStatsDetailResponse, error)
	RecordView(ctx context.Context, cardID uint, ip, userAgent string) error
}

type tiktokCardService struct {
	repo          repository.TikTokCardRepository
	shortLinkRepo repository.ShortLinkRepository
}

// NewTikTokCardService 创建 TikTok 卡片服务
func NewTikTokCardService(repo repository.TikTokCardRepository, shortLinkRepo repository.ShortLinkRepository) TikTokCardService {
	return &tiktokCardService{
		repo:          repo,
		shortLinkRepo: shortLinkRepo,
	}
}

// NewTikTokCardServiceWithDB 通过 *gorm.DB 创建 TikTok 卡片服务(用于测试 / router 装配)
func NewTikTokCardServiceWithDB(gormDB *gorm.DB) TikTokCardService {
	return &tiktokCardService{
		repo:          repository.NewTikTokCardRepository(gormDB),
		shortLinkRepo: repository.NewShortLinkRepository(gormDB),
	}
}

func (s *tiktokCardService) Create(ctx context.Context, req *dto.TikTokCardCreateRequest) (*dto.TikTokCardResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("标题不能为空")
	}

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		redirectURL = "https://www.tiktok.com"
	}

	card := &model.TikTokCard{
		Title:        req.Title,
		Description:  req.Description,
		ImageURL:     req.ImageURL,
		RedirectURL:  redirectURL,
		DomainPoolID: req.DomainPoolID,
		Tags:         req.Tags,
		IsActive:     req.IsActive,
	}

	created, err := s.repo.Create(ctx, card)
	if err != nil {
		return nil, fmt.Errorf("创建 TikTok 卡片失败: %w", err)
	}

	return s.toResponse(ctx, created, ""), nil
}

func (s *tiktokCardService) Update(ctx context.Context, req *dto.TikTokCardUpdateRequest) (*dto.TikTokCardResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("请求不能为空")
	}

	card, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		redirectURL = card.RedirectURL
		if redirectURL == "" {
			redirectURL = "https://www.tiktok.com"
		}
	}

	card.Title = req.Title
	card.Description = req.Description
	card.ImageURL = req.ImageURL
	card.RedirectURL = redirectURL
	card.DomainPoolID = req.DomainPoolID
	card.Tags = req.Tags
	card.ViewCount = req.ViewCount
	card.LikeCount = req.LikeCount
	card.ShareCount = req.ShareCount
	card.IsActive = req.IsActive

	updated, err := s.repo.Update(ctx, card)
	if err != nil {
		return nil, fmt.Errorf("更新 TikTok 卡片失败: %w", err)
	}
	return s.toResponse(ctx, updated, ""), nil
}

func (s *tiktokCardService) Delete(ctx context.Context, id uint) error {
	_, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("卡片不存在: %w", err)
	}
	return s.repo.Delete(ctx, id)
}

func (s *tiktokCardService) GetByID(ctx context.Context, id uint) (*dto.TikTokCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != 0 {
		sl, err := s.shortLinkRepo.GetByID(ctx, card.ShortLinkID)
		if err == nil && sl != nil {
			shortCode = sl.ShortCode
		}
	}
	return s.toResponse(ctx, card, shortCode), nil
}

func (s *tiktokCardService) GetCardModelByID(ctx context.Context, id uint) (*model.TikTokCard, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *tiktokCardService) GetList(ctx context.Context, req *dto.TikTokCardListRequest) (*dto.TikTokCardListResponse, error) {
	if req == nil {
		req = &dto.TikTokCardListRequest{Page: 1, PageSize: 20}
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := repository.CardListFilter{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, IsActive: req.IsActive}
	cards, total, err := s.repo.GetList(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]dto.TikTokCardResponse, 0, len(cards))
	for i := range cards {
		responses = append(responses, *s.toResponse(ctx, &cards[i], ""))
	}
	return &dto.TikTokCardListResponse{List: responses, Total: total}, nil
}

func (s *tiktokCardService) GenerateShortLink(ctx context.Context, cardID uint) (*dto.TikTokCardResponse, error) {
	card, err := s.repo.GetByID(ctx, cardID)
	if err != nil {
		return nil, fmt.Errorf("卡片不存在: %w", err)
	}

	shortCode := generateRandomCode(8)

	if card.ShortLinkID != 0 {
		_ = s.shortLinkRepo.Delete(ctx, card.ShortLinkID)
	}

	if card.RedirectURL == "" {
		return nil, nil
	}
	sl := &model.ShortLink{
		ShortCode:   shortCode,
		OriginalURL: card.RedirectURL,
		Title:       card.Title,
		Description: card.Description,
		DomainID:    card.DomainPoolID,
	}
	if sl.DomainID == 0 {
		sl.DomainID = 1
	}
	if err := s.shortLinkRepo.Create(ctx, sl); err != nil {
		return nil, fmt.Errorf("创建短链失败: %w", err)
	}

	card.ShortLinkID = sl.ID
	if _, err := s.repo.Update(ctx, card); err != nil {
		return nil, fmt.Errorf("更新卡片短链ID失败: %w", err)
	}

	return s.toResponse(ctx, card, shortCode), nil
}

func (s *tiktokCardService) StatsOverall(ctx context.Context) (*dto.TikTokCardStatsOverallResponse, error) {
	totalCards, activeCards, totalViews, popular, err := s.repo.GetOverallStats(ctx)
	if err != nil {
		return nil, err
	}

	daily := make([]dto.TikTokCardDailyStat, 0, 7)
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dayCount, _ := s.repo.CountDailyView(ctx, day)
		daily = append(daily, dto.TikTokCardDailyStat{Date: day, ViewCount: dayCount})
	}

	activities, _ := s.repo.ListRecentActivities(ctx, 10)

	titleMap := make(map[uint]string, len(popular))
	for _, c := range popular {
		titleMap[c.ID] = c.Title
	}

	recent := make([]dto.TikTokCardActivityItem, 0, len(activities))
	for _, a := range activities {
		recent = append(recent, dto.TikTokCardActivityItem{
			CardTitle: titleMap[a.CardID],
			Action:    a.ActivityType,
			Username:  a.UserID,
			CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	popularResp := make([]dto.TikTokCardResponse, 0, len(popular))
	for i := range popular {
		popularResp = append(popularResp, *s.toResponse(ctx, &popular[i], ""))
	}

	return &dto.TikTokCardStatsOverallResponse{
		TotalCards:     totalCards,
		ActiveCards:    activeCards,
		TotalViews:     totalViews,
		PopularCards:   popularResp,
		DailyStats:     daily,
		RecentActivity: recent,
	}, nil
}

func (s *tiktokCardService) Stats(ctx context.Context, cardID uint) (*dto.TikTokCardStatsDetailResponse, error) {
	card, activities, err := s.repo.GetCardStats(ctx, cardID, 7)
	if err != nil {
		return nil, err
	}

	daily := make([]dto.TikTokCardDailyStat, 0, 7)
	for i := 6; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		dayCount, _ := s.repo.CountCardDailyView(ctx, cardID, day)
		daily = append(daily, dto.TikTokCardDailyStat{Date: day, ViewCount: dayCount})
	}

	recent := make([]dto.TikTokCardActivityItem, 0, len(activities))
	for _, a := range activities {
		recent = append(recent, dto.TikTokCardActivityItem{
			CardTitle: card.Title,
			Action:    a.ActivityType,
			Username:  a.UserID,
			CreatedAt: a.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return &dto.TikTokCardStatsDetailResponse{
		CardID:         card.ID,
		Title:          card.Title,
		ViewCount:      card.ViewCount,
		DailyStats:     daily,
		RecentActivity: recent,
	}, nil
}

func (s *tiktokCardService) RecordView(ctx context.Context, cardID uint, ip, userAgent string) error {
	if err := s.repo.IncrementViewCount(ctx, cardID); err != nil {
		return err
	}
	activity := &model.TikTokCardActivity{
		CardID:       cardID,
		ActivityType: "view",
		IPAddress:    ip,
		UserAgent:    userAgent,
		Platform:     "tiktok",
	}
	return s.repo.CreateActivity(ctx, activity)
}

func (s *tiktokCardService) toResponse(ctx context.Context, card *model.TikTokCard, shortCode string) *dto.TikTokCardResponse {
	shortLinkURL := ""
	if shortCode != "" {
		shortLinkURL = "/s/" + shortCode
	}
	domainPoolID := card.DomainPoolID
	return &dto.TikTokCardResponse{
		ID:           card.ID,
		Title:        card.Title,
		Description:  card.Description,
		ImageURL:     card.ImageURL,
		RedirectURL:  card.RedirectURL,
		DomainPoolID: &domainPoolID,
		ShortLinkURL: shortLinkURL,
		ShortCode:    shortCode,
		Tags:         card.Tags,
		ViewCount:    card.ViewCount,
		LikeCount:    card.LikeCount,
		ShareCount:   card.ShareCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    card.UpdatedAt.Format(time.RFC3339),
	}
}

func generateRandomCode(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = charset[now%int64(len(charset))]
		now = now/int64(len(charset)) + 1
	}
	return string(b)
}
