package cards

import (
	"context"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/reach/card/template"
	"hivemtk-user/internal/repository"
	"time"
)

// DouyinCardService 抖音卡片服务接口
type DouyinCardService interface {
	Create(ctx context.Context, req *dto.DouyinCardCreateRequest) (*dto.DouyinCardResponse, error)
	Update(ctx context.Context, req *dto.DouyinCardUpdateRequest) (*dto.DouyinCardResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.DouyinCardResponse, error)
	GetByIDWithRefresh(ctx context.Context, id uint) (*dto.DouyinCardResponse, error)
	GetCardModelByID(ctx context.Context, id uint) (*model.DouyinCard, error)
	GetList(ctx context.Context, req *dto.DouyinCardListRequest) (*dto.DouyinCardListResponse, error)
	ShareCard(ctx context.Context, id uint, platform string) error
	GenerateHTMLPage(ctx context.Context, id uint) (string, error)
	GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error)
	GenerateShortLink(ctx context.Context, card *model.DouyinCard) error
}

type douyinCardService struct {
	repo              repository.DouyinCardRepository
	statsService      DouyinCardStatsService
	shortLinkService  ShortLinkService
	domainPoolService DomainPoolResolver
	templateService   *template.TemplateService
}

// NewDouyinCardService 创建抖音卡片服务
func NewDouyinCardService(db any) DouyinCardService {
	gormDB := repository.AsGormDB(db)
	return &douyinCardService{
		repo:              repository.NewDouyinCardRepository(gormDB),
		statsService:      NewDouyinCardStatsService(repository.NewDouyinCardStatsRepository(gormDB)),
		shortLinkService:  NewShortLinkService(repository.NewShortLinkRepository(gormDB), repository.NewDomainPoolRepository(gormDB), repository.NewShortLinkAccessRepository(gormDB)),
		domainPoolService: newRepoDomainPoolResolver(repository.NewDomainPoolRepository(gormDB)),
		templateService:   template.NewTemplateService("internal/template"),
	}
}

func (s *douyinCardService) Create(ctx context.Context, req *dto.DouyinCardCreateRequest) (*dto.DouyinCardResponse, error) {
	redirectURL := req.RedirectURL

	if redirectURL == "" {
		redirectURL = "https://www.douyin.com"
	}

	card := &model.DouyinCard{
		Title:        req.Title,
		Description:  req.Description,
		ImageURL:     req.ImageURL,
		RedirectURL:  redirectURL,
		DomainPoolID: req.DomainPoolID,
		Tags:         req.Tags,
		IsActive:     dto.BoolPtrOrDefaultTrue(req.IsActive),
	}

	createdCard, err := s.repo.Create(ctx, card)
	if err != nil {
		return nil, err
	}

	if err := s.GenerateShortLink(ctx, createdCard); err != nil {
		logger.Errorf("警告：生成短链失败：%v", err)
	}

	shortCode := ""
	if createdCard.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, createdCard.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, createdCard, shortCode), nil
}

func (s *douyinCardService) Update(ctx context.Context, req *dto.DouyinCardUpdateRequest) (*dto.DouyinCardResponse, error) {
	card, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	originalDomainPoolID := card.DomainPoolID

	redirectURL := req.RedirectURL

	if redirectURL == "" {
		// 空串=保留原跳转地址，而不是重置为站点默认；只有明确需要默认值时才用默认
		if card.RedirectURL == "" {
			redirectURL = "https://www.douyin.com"
		} else {
			redirectURL = card.RedirectURL
		}
	}

	card.Title = req.Title
	card.Description = req.Description
	card.ImageURL = req.ImageURL
	card.RedirectURL = redirectURL
	card.DomainPoolID = req.DomainPoolID
	card.Tags = req.Tags
	card.ViewCount = req.ViewCount
	if req.IsActive != nil {
		card.IsActive = *req.IsActive
	}

	updatedCard, err := s.repo.Update(ctx, card)
	if err != nil {
		return nil, err
	}

	if originalDomainPoolID != req.DomainPoolID {
		if err := s.GenerateShortLink(ctx, updatedCard); err != nil {
			return nil, err
		}
	}

	shortCode := ""
	if updatedCard.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, updatedCard.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, updatedCard, shortCode), nil
}

func (s *douyinCardService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func (s *douyinCardService) GetByID(ctx context.Context, id uint) (*dto.DouyinCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *douyinCardService) GetByIDWithRefresh(ctx context.Context, id uint) (*dto.DouyinCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *douyinCardService) GetCardModelByID(ctx context.Context, id uint) (*model.DouyinCard, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return card, nil
}

func (s *douyinCardService) GetList(ctx context.Context, req *dto.DouyinCardListRequest) (*dto.DouyinCardListResponse, error) {
	filter := repository.CardListFilter{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, IsActive: req.IsActive}
	cards, total, err := s.repo.GetList(ctx, filter)
	if err != nil {
		return nil, err
	}

	var responses []dto.DouyinCardResponse
	for _, card := range cards {
		shortCode := ""
		if card.ShortLinkID != 0 {
			shortLink, err := s.shortLinkService.GetByID(ctx, card.ShortLinkID)
			if err == nil {
				shortCode = shortLink.ShortCode
			}
		}
		responses = append(responses, *s.toResponseWithShortLink(ctx, &card, shortCode))
	}

	return &dto.DouyinCardListResponse{
		List:  responses,
		Total: total,
	}, nil
}

func (s *douyinCardService) ShareCard(ctx context.Context, id uint, platform string) error {
	err := s.repo.IncrementShareCount(ctx, id)
	if err != nil {
		return err
	}

	_ = s.statsService.RecordActivity(ctx, id, 0, "share", platform, "", "")

	return nil
}

func (s *douyinCardService) GenerateHTMLPage(ctx context.Context, id uint) (string, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}

	data := &template.CardTemplateData{
		Title:       card.Title,
		Description: card.Description,
		ImageURL:    card.ImageURL,
		RedirectURL: card.RedirectURL,
	}

	html, err := s.templateService.GenerateDouyinCardPage(data)
	if err != nil {
		return "", err
	}

	return html, nil
}

func (s *douyinCardService) GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.templateService.RenderCardChatPage("douyin", card.ID, card.Title, card.Description, card.ImageURL, card.Tags, baseURL)
}

func (s *douyinCardService) toResponseWithShortLink(ctx context.Context, card *model.DouyinCard, shortCode string) *dto.DouyinCardResponse {
	shortLinkURL := ""
	if shortCode != "" {
		shortLinkURL = "/s/" + shortCode
	}

	return &dto.DouyinCardResponse{
		ID:           card.ID,
		Title:        card.Title,
		Description:  card.Description,
		ImageURL:     card.ImageURL,
		RedirectURL:  card.RedirectURL,
		DomainPoolID: &card.DomainPoolID,
		ShortLinkURL: shortLinkURL,
		ShortCode:    shortCode,
		Tags:         card.Tags,
		ViewCount:    card.ViewCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    card.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *douyinCardService) GenerateShortLink(ctx context.Context, card *model.DouyinCard) error {
	if card.ShortLinkID != 0 {
		_ = s.shortLinkService.Delete(ctx, card.ShortLinkID)
	}

	generateReq := &dto.GenerateShortCodeRequest{
		Length: 6,
	}
	generateResp, err := s.shortLinkService.GenerateShortCode(ctx, generateReq)
	if err != nil {
		return fmt.Errorf("生成短码失败: %w", err)
	}

	logger.Infof("生成的短码: %s", generateResp.ShortCode)

	var domainID uint
	if card.DomainPoolID != 0 {
		if pool, perr := s.domainPoolService.GetByID(ctx, int(card.DomainPoolID)); perr == nil && pool != nil {
			domainID = uint(pool.ID)
		}
	}

	if card.RedirectURL == "" {
		return nil
	}
	shortLinkReq := &dto.CreateShortLinkRequest{
		ShortCode:   generateResp.ShortCode,
		OriginalURL: card.RedirectURL,
		Title:       card.Title,
		Description: card.Description,
		DomainID:    domainID,
	}

	shortLinkResp, err := s.shortLinkService.Create(ctx, shortLinkReq)
	if err != nil {
		return fmt.Errorf("创建短链失败: %w", err)
	}

	logger.Infof("创建的短链ID: %d", shortLinkResp.ID)

	card.ShortLinkID = shortLinkResp.ID
	_, err = s.repo.Update(ctx, card)
	if err != nil {
		return fmt.Errorf("更新卡片短链ID失败: %w", err)
	}

	logger.Infof("更新卡片短链ID成功: %d", card.ShortLinkID)

	updatedCard, err := s.repo.GetByID(ctx, card.ID)
	if err != nil {
		return fmt.Errorf("获取更新后的卡片失败: %w", err)
	}

	if updatedCard.ShortLinkID != shortLinkResp.ID {
		return fmt.Errorf("短链ID更新失败")
	}

	logger.Infof("验证更新后的卡片短链ID: %d", updatedCard.ShortLinkID)

	return nil
}
