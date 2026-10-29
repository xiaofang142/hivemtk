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

// KuaishouCardService 快手卡片服务接口
type KuaishouCardService interface {
	Create(ctx context.Context, req *dto.KuaishouCardCreateRequest) (*dto.KuaishouCardResponse, error)
	Update(ctx context.Context, req *dto.KuaishouCardUpdateRequest) (*dto.KuaishouCardResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.KuaishouCardResponse, error)
	GetByIDWithRefresh(ctx context.Context, id uint) (*dto.KuaishouCardResponse, error)
	GetCardModelByID(ctx context.Context, id uint) (*model.KuaishouCard, error)
	GetList(ctx context.Context, req *dto.KuaishouCardListRequest) (*dto.KuaishouCardListResponse, error)
	ViewCard(ctx context.Context, id uint) error
	LikeCard(ctx context.Context, id uint) error
	ShareCard(ctx context.Context, id uint, platform string) (*dto.KuaishouCardResponse, error)
	GenerateHTMLPage(ctx context.Context, id uint) (string, error)
	GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error)
	GenerateShortLink(ctx context.Context, card *model.KuaishouCard) error
}

type kuaishouCardService struct {
	repo              repository.KuaishouCardRepository
	shortLinkService  ShortLinkService
	domainPoolService DomainPoolResolver
	templateService   *template.TemplateService
}

// NewKuaishouCardService 创建快手卡片服务实例
func NewKuaishouCardService(db any) KuaishouCardService {
	gormDB := repository.AsGormDB(db)
	return &kuaishouCardService{
		repo:              repository.NewKuaishouCardRepository(gormDB),
		shortLinkService:  NewShortLinkService(repository.NewShortLinkRepository(gormDB), repository.NewDomainPoolRepository(gormDB), repository.NewShortLinkAccessRepository(gormDB)),
		domainPoolService: newRepoDomainPoolResolver(repository.NewDomainPoolRepository(gormDB)),
		templateService:   template.NewTemplateService("internal/template"),
	}
}

func (s *kuaishouCardService) Create(ctx context.Context, req *dto.KuaishouCardCreateRequest) (*dto.KuaishouCardResponse, error) {
	redirectURL := req.RedirectURL
	if redirectURL == "" {
		redirectURL = "https://www.kuaishou.com"
	}

	card := &model.KuaishouCard{
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

	card, err = s.repo.GetByID(ctx, createdCard.ID)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *kuaishouCardService) Update(ctx context.Context, req *dto.KuaishouCardUpdateRequest) (*dto.KuaishouCardResponse, error) {
	card, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	redirectURL := req.RedirectURL
	if redirectURL == "" {
		// 空串=保留原跳转地址，不重置为站点默认
		redirectURL = card.RedirectURL
		if redirectURL == "" {
			redirectURL = "https://www.kuaishou.com"
		}
	}

	card.Title = req.Title
	card.Description = req.Description
	card.ImageURL = req.ImageURL
	card.RedirectURL = redirectURL
	card.DomainPoolID = req.DomainPoolID
	card.Tags = req.Tags
	if req.IsActive != nil {
		card.IsActive = *req.IsActive
	}

	updatedCard, err := s.repo.Update(ctx, card)
	if err != nil {
		return nil, err
	}

	if err := s.GenerateShortLink(ctx, updatedCard); err != nil {
		logger.Errorf("警告：生成短链失败：%v", err)
	}

	card, err = s.repo.GetByID(ctx, updatedCard.ID)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *kuaishouCardService) Delete(ctx context.Context, id uint) error {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		_ = s.shortLinkService.Delete(ctx, *card.ShortLinkID)
	}

	return s.repo.Delete(ctx, id)
}

func (s *kuaishouCardService) GetByID(ctx context.Context, id uint) (*dto.KuaishouCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *kuaishouCardService) GetByIDWithRefresh(ctx context.Context, id uint) (*dto.KuaishouCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	shortCode := ""
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
		}
	}

	return s.toResponseWithShortLink(ctx, card, shortCode), nil
}

func (s *kuaishouCardService) GetCardModelByID(ctx context.Context, id uint) (*model.KuaishouCard, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *kuaishouCardService) GetList(ctx context.Context, req *dto.KuaishouCardListRequest) (*dto.KuaishouCardListResponse, error) {
	filter := repository.CardListFilter{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, IsActive: req.IsActive}
	cards, total, err := s.repo.GetList(ctx, filter)
	if err != nil {
		return nil, err
	}

	var responseList []dto.KuaishouCardResponse
	for _, card := range cards {
		shortCode := ""
		if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
			shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
			if err == nil {
				shortCode = shortLink.ShortCode
			}
		}
		responseList = append(responseList, *s.toResponseWithShortLink(ctx, &card, shortCode))
	}

	return &dto.KuaishouCardListResponse{
		List:  responseList,
		Total: total,
	}, nil
}

func (s *kuaishouCardService) LikeCard(ctx context.Context, id uint) error {
	err := s.repo.IncrementLikeCount(ctx, id)
	if err != nil {
		return err
	}

	ip, _ := ctx.Value("ip").(string)
	userAgent, _ := ctx.Value("user_agent").(string)
	activity := &model.KuaishouCardActivity{
		CardID:       id,
		ActivityType: "like",
		IPAddress:    ip,
		UserAgent:    userAgent,
	}
	return s.repo.CreateActivity(ctx, activity)
}

func (s *kuaishouCardService) ViewCard(ctx context.Context, id uint) error {
	_, err := s.repo.IncrementViewCount(ctx, id)
	if err != nil {
		return err
	}

	ip, _ := ctx.Value("ip").(string)
	userAgent, _ := ctx.Value("user_agent").(string)
	activity := &model.KuaishouCardActivity{
		CardID:       id,
		ActivityType: "view",
		IPAddress:    ip,
		UserAgent:    userAgent,
	}
	return s.repo.CreateActivity(ctx, activity)
}

func (s *kuaishouCardService) ShareCard(ctx context.Context, id uint, platform string) (*dto.KuaishouCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	err = s.repo.IncrementShareCount(ctx, id)
	if err != nil {
		return nil, err
	}

	ip, _ := ctx.Value("ip").(string)
	userAgent, _ := ctx.Value("user_agent").(string)
	activity := &model.KuaishouCardActivity{
		CardID:       id,
		ActivityType: "share",
		IPAddress:    ip,
		UserAgent:    userAgent,
	}
	_ = s.repo.CreateActivity(ctx, activity)

	return s.toResponse(ctx, card), nil
}

func (s *kuaishouCardService) GenerateHTMLPage(ctx context.Context, id uint) (string, error) {
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

	html, err := s.templateService.GenerateKuaishouCardPage(data)
	if err != nil {
		return "", err
	}

	return html, nil
}

func (s *kuaishouCardService) GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return s.templateService.RenderCardChatPage("kuaishou", card.ID, card.Title, card.Description, card.ImageURL, card.Tags, baseURL)
}

func (s *kuaishouCardService) GenerateShortLink(ctx context.Context, card *model.KuaishouCard) error {
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		_ = s.shortLinkService.Delete(ctx, *card.ShortLinkID)
	}

	generateReq := &dto.GenerateShortCodeRequest{
		Length: 6,
	}
	generateResp, err := s.shortLinkService.GenerateShortCode(ctx, generateReq)
	if err != nil {
		return fmt.Errorf("生成短码失败: %v", err)
	}

	logger.Infof("生成的短码: %s", generateResp.ShortCode)

	var domainID uint = 0
	if card.DomainPoolID != 0 {
		domainID = card.DomainPoolID
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
		return fmt.Errorf("创建短链失败: %v", err)
	}

	logger.Infof("创建的短链ID: %d", shortLinkResp.ID)

	card.ShortLinkID = &shortLinkResp.ID
	_, err = s.repo.Update(ctx, card)
	if err != nil {
		return fmt.Errorf("更新卡片短链ID失败: %v", err)
	}

	logger.Infof("更新卡片短链ID成功: %d", *card.ShortLinkID)

	updatedCard, err := s.repo.GetByID(ctx, card.ID)
	if err != nil {
		return fmt.Errorf("获取更新后的卡片失败: %v", err)
	}

	if updatedCard.ShortLinkID == nil || *updatedCard.ShortLinkID != shortLinkResp.ID {
		return fmt.Errorf("短链ID更新失败")
	}

	logger.Infof("验证更新后的卡片短链ID: %d", *updatedCard.ShortLinkID)

	return nil
}

func (s *kuaishouCardService) toResponse(ctx context.Context, card *model.KuaishouCard) *dto.KuaishouCardResponse {
	shortCode := ""
	shortLinkURL := ""
	if card.ShortLinkID != nil && *card.ShortLinkID != 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, *card.ShortLinkID)
		if err == nil {
			shortCode = shortLink.ShortCode
			shortLinkURL = "/s/" + shortCode
		}
	}

	return &dto.KuaishouCardResponse{
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
		LikeCount:    card.LikeCount,
		ShareCount:   card.ShareCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    card.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *kuaishouCardService) toResponseWithShortLink(ctx context.Context, card *model.KuaishouCard, shortCode string) *dto.KuaishouCardResponse {
	shortLinkURL := ""
	if shortCode != "" {
		shortLinkURL = "/s/" + shortCode
	}

	return &dto.KuaishouCardResponse{
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
		LikeCount:    card.LikeCount,
		ShareCount:   card.ShareCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    card.UpdatedAt.Format(time.RFC3339),
	}
}
