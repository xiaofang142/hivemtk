package service

import (
	"context"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/reach/card/template"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// XiaohongshuCardService 小红书卡片服务接口
type XiaohongshuCardService interface {
	Create(ctx context.Context, req *dto.XiaohongshuCardCreateRequest) (*dto.XiaohongshuCardResponse, error)
	Update(ctx context.Context, req *dto.XiaohongshuCardUpdateRequest) (*dto.XiaohongshuCardResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.XiaohongshuCardResponse, error)
	GetCardModelByID(ctx context.Context, id uint) (*model.XiaohongshuCard, error)
	GetList(ctx context.Context, req *dto.XiaohongshuCardListRequest) (*dto.XiaohongshuCardListResponse, error)
	GenerateHTMLPage(ctx context.Context, id uint) (string, error)
	GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error)
	GenerateShortLink(ctx context.Context, card *model.XiaohongshuCard) error
	ShareCard(ctx context.Context, id uint, platform string) (*dto.XiaohongshuCardResponse, error)
}

type xiaohongshuCardService struct {
	repo              repository.XiaohongshuCardRepository
	templateService   *template.TemplateService
	shortLinkService  ShortLinkService
	domainPoolService DomainPoolService
}

// NewXiaohongshuCardService 创建小红书卡片服务实例
func NewXiaohongshuCardService(db *gorm.DB) XiaohongshuCardService {
	return &xiaohongshuCardService{
		repo:              repository.NewXiaohongshuCardRepository(db),
		templateService:   template.NewTemplateService("internal/template"),
		shortLinkService:  NewShortLinkService(db),
		domainPoolService: NewDomainPoolService(db),
	}
}

func (s *xiaohongshuCardService) Create(ctx context.Context, req *dto.XiaohongshuCardCreateRequest) (*dto.XiaohongshuCardResponse, error) {
	redirectURL := req.RedirectURL
	if redirectURL == "" {
		redirectURL = "https://www.xiaohongshu.com"
	}

	card := &model.XiaohongshuCard{
		Title:        req.Title,
		Description:  req.Description,
		ImageURL:     req.ImageURL,
		RedirectURL:  redirectURL,
		DomainPoolID: req.DomainPoolID,
		Tags:         req.Tags,
		ViewCount:    0,
		IsActive:     req.IsActive,
	}

	createdCard, err := s.repo.Create(ctx, card)
	if err != nil {
		return nil, err
	}

	err = s.GenerateShortLink(ctx, createdCard)
	if err != nil {
		return nil, err
	}

	card, err = s.repo.GetByID(ctx, createdCard.ID)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != nil {
		shortLink, err := s.shortLinkService.GetByID(context.Background(), *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *xiaohongshuCardService) Update(ctx context.Context, req *dto.XiaohongshuCardUpdateRequest) (*dto.XiaohongshuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	originalDomainPoolID := card.DomainPoolID

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		// 空串=保留原跳转地址，不重置为站点默认
		redirectURL = card.RedirectURL
		if redirectURL == "" {
			redirectURL = "https://www.xiaohongshu.com"
		}
	}

	card.Title = req.Title
	card.Description = req.Description
	card.ImageURL = req.ImageURL
	card.RedirectURL = redirectURL
	card.DomainPoolID = req.DomainPoolID
	card.Tags = req.Tags
	card.IsActive = req.IsActive

	updatedCard, err := s.repo.Update(ctx, card)
	if err != nil {
		return nil, err
	}

	if originalDomainPoolID != req.DomainPoolID {
		err = s.GenerateShortLink(ctx, updatedCard)
		if err != nil {
			return nil, err
		}

		updatedCard, err = s.repo.GetByID(ctx, updatedCard.ID)
		if err != nil {
			return nil, err
		}
	}

	shortCode := ""
	if updatedCard.ShortLinkID != nil {
		shortLink, err := s.shortLinkService.GetByID(context.Background(), *updatedCard.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, updatedCard, shortCode), nil
}

func (s *xiaohongshuCardService) Delete(ctx context.Context, id uint) error {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if card.ShortLinkID != nil {
		_ = s.shortLinkService.Delete(context.Background(), *card.ShortLinkID)
	}

	return s.repo.Delete(ctx, id)
}

func (s *xiaohongshuCardService) GetByID(ctx context.Context, id uint) (*dto.XiaohongshuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toResponse(ctx, card), nil
}

func (s *xiaohongshuCardService) GetCardModelByID(ctx context.Context, id uint) (*model.XiaohongshuCard, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return card, nil
}

func (s *xiaohongshuCardService) GetList(ctx context.Context, req *dto.XiaohongshuCardListRequest) (*dto.XiaohongshuCardListResponse, error) {
	filter := repository.CardListFilter{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, IsActive: req.IsActive}
	cards, total, err := s.repo.GetList(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]dto.XiaohongshuCardResponse, len(cards))
	for i, card := range cards {
		shortCode := ""
		shortLinkURL := ""
		if card.ShortLinkID != nil {
			shortLink, err := s.shortLinkService.GetByID(context.Background(), *card.ShortLinkID)
			if err == nil {
				shortCode = shortLink.ShortCode
				shortLinkURL = "/s/" + shortCode
			}
		}

		responses[i] = dto.XiaohongshuCardResponse{
			ID:           card.ID,
			Title:        card.Title,
			Description:  card.Description,
			ImageURL:     card.ImageURL,
			RedirectURL:  card.RedirectURL,
			DomainPoolID: card.DomainPoolID,
			ShortLinkURL: shortLinkURL,
			ShortCode:    shortCode,
			Tags:         card.Tags,
			ViewCount:    card.ViewCount,
			IsActive:     card.IsActive,
			CreatedAt:    card.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:    card.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	return &dto.XiaohongshuCardListResponse{
		List:  responses,
		Total: total,
	}, nil
}

func (s *xiaohongshuCardService) ShareCard(ctx context.Context, id uint, platform string) (*dto.XiaohongshuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	activity := &model.XiaohongshuCardActivity{
		CardID:       id,
		ActivityType: "share",
		Content:      platform,
	}
	_ = s.repo.CreateActivity(ctx, activity)

	return s.toResponse(ctx, card), nil
}

func (s *xiaohongshuCardService) GenerateHTMLPage(ctx context.Context, id uint) (string, error) {
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

	html, err := s.templateService.GenerateXiaohongshuCardPage(data)
	if err != nil {
		return "", err
	}

	return html, nil
}

func (s *xiaohongshuCardService) GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.templateService.RenderCardChatPage("xiaohongshu", card.ID, card.Title, card.Description, card.ImageURL, card.Tags, baseURL)
}

func (s *xiaohongshuCardService) GenerateShortLink(ctx context.Context, card *model.XiaohongshuCard) error {
	if card.ShortLinkID != nil {
		return nil // 已绑定短链：幂等跳过，避免重复造短码留下孤儿
	}

	var domainID uint = 0
	if card.DomainPoolID != nil {
		domainID = *card.DomainPoolID
	}

	shortCodeReq := &dto.GenerateShortCodeRequest{
		Length: 6,
	}
	shortCodeResp, err := s.shortLinkService.GenerateShortCode(context.Background(), shortCodeReq)
	if err != nil {
		return err
	}

	if card.RedirectURL == "" {
		return nil
	}
	createReq := &dto.CreateShortLinkRequest{
		ShortCode:   shortCodeResp.ShortCode,
		OriginalURL: card.RedirectURL,
		Title:       card.Title,
		Description: card.Description,
		DomainID:    domainID,
	}

	shortLinkResp, err := s.shortLinkService.Create(ctx, createReq)
	if err != nil {
		return err
	}

	err = s.repo.UpdateShortLinkID(ctx, card.ID, &shortLinkResp.ID)
	if err != nil {
		return err
	}

	return nil
}

func (s *xiaohongshuCardService) toResponse(ctx context.Context, card *model.XiaohongshuCard) *dto.XiaohongshuCardResponse {
	shortCode := ""
	shortLinkURL := ""
	if card.ShortLinkID != nil {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
			shortLinkURL = "/s/" + shortCode
		}
	}

	return &dto.XiaohongshuCardResponse{
		ID:           card.ID,
		Title:        card.Title,
		Description:  card.Description,
		ImageURL:     card.ImageURL,
		RedirectURL:  card.RedirectURL,
		DomainPoolID: card.DomainPoolID,
		ShortLinkURL: shortLinkURL,
		ShortCode:    shortCode,
		Tags:         card.Tags,
		ViewCount:    card.ViewCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:    card.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func (s *xiaohongshuCardService) toResponseWithShortLink(ctx context.Context, card *model.XiaohongshuCard, shortCode string) *dto.XiaohongshuCardResponse {
	shortLinkURL := ""
	if shortCode != "" {
		shortLinkURL = "/s/" + shortCode
	}

	return &dto.XiaohongshuCardResponse{
		ID:           card.ID,
		Title:        card.Title,
		Description:  card.Description,
		ImageURL:     card.ImageURL,
		RedirectURL:  card.RedirectURL,
		DomainPoolID: card.DomainPoolID,
		ShortLinkURL: shortLinkURL,
		ShortCode:    shortCode,
		Tags:         card.Tags,
		ViewCount:    card.ViewCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:    card.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
