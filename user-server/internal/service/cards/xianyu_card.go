package cards

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/reach/card/template"
	"hivemtk-user/internal/repository"
)

// XianyuCardService 闲鱼卡片服务接口
type XianyuCardService interface {
	Create(ctx context.Context, req *dto.XianyuCardCreateRequest) (*dto.XianyuCardResponse, error)
	Update(ctx context.Context, req *dto.XianyuCardUpdateRequest) (*dto.XianyuCardResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.XianyuCardResponse, error)
	GetByIDWithRefresh(ctx context.Context, id uint) (*dto.XianyuCardResponse, error)
	GetList(ctx context.Context, req *dto.XianyuCardListRequest) (*dto.XianyuCardListResponse, error)
	GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error)
	GenerateShortLink(ctx context.Context, card *model.XianyuCard) error
}

type xianyuCardService struct {
	repo              repository.XianyuCardRepository
	statsService      XianyuCardStatsService
	shortLinkService  ShortLinkService
	domainPoolService DomainPoolResolver
	templateService   *template.TemplateService
}

// NewXianyuCardService 创建闲鱼卡片服务
func NewXianyuCardService(db any) XianyuCardService {
	gormDB := repository.AsGormDB(db)
	return &xianyuCardService{
		repo:              repository.NewXianyuCardRepository(gormDB),
		statsService:      NewXianyuCardStatsService(gormDB),
		shortLinkService:  NewShortLinkService(repository.NewShortLinkRepository(gormDB), repository.NewDomainPoolRepository(gormDB), repository.NewShortLinkAccessRepository(gormDB)),
		domainPoolService: newRepoDomainPoolResolver(repository.NewDomainPoolRepository(gormDB)),
		templateService:   template.NewTemplateService("internal/template"),
	}
}

func (s *xianyuCardService) Create(ctx context.Context, req *dto.XianyuCardCreateRequest) (*dto.XianyuCardResponse, error) {
	card := &model.XianyuCard{
		Title:        req.Title,
		Description:  req.Description,
		ImageURL:     req.ImageURL,
		RedirectURL:  req.RedirectURL,
		DomainPoolID: req.DomainPoolID,
		Tags:         req.Tags,
		IsActive:     dto.BoolPtrOrDefaultTrue(req.IsActive),
	}

	if err := s.repo.Create(ctx, card); err != nil {
		return nil, fmt.Errorf("创建闲鱼卡片失败: %w", err)
	}

	// 没有跳转地址就不生成短链：条目的展示依赖 redirect_url，硬生成只会留下死链，
	// 且会让「无跳转地址」变成一条噪音告警。
	if card.RedirectURL != "" {
		if err := s.GenerateShortLink(ctx, card); err != nil {
			logger.Errorf("警告：生成短链失败：%v", err)
		}
	}

	return s.convertToResponse(ctx, card), nil
}

func (s *xianyuCardService) Update(ctx context.Context, req *dto.XianyuCardUpdateRequest) (*dto.XianyuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("获取闲鱼卡片失败: %w", err)
	}

	card.Title = req.Title
	card.Description = req.Description
	card.ImageURL = req.ImageURL
	card.RedirectURL = req.RedirectURL
	card.DomainPoolID = req.DomainPoolID
	card.Tags = req.Tags
	card.LikeCount = req.LikeCount
	card.ShareCount = req.ShareCount
	card.ViewCount = req.ViewCount
	if req.IsActive != nil {
		card.IsActive = *req.IsActive
	}

	if err := s.repo.Update(ctx, card); err != nil {
		return nil, fmt.Errorf("更新闲鱼卡片失败: %w", err)
	}

	return s.convertToResponse(ctx, card), nil
}

func (s *xianyuCardService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func (s *xianyuCardService) GetByID(ctx context.Context, id uint) (*dto.XianyuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取闲鱼卡片失败: %w", err)
	}
	return s.convertToResponse(ctx, card), nil
}

func (s *xianyuCardService) GetByIDWithRefresh(ctx context.Context, id uint) (*dto.XianyuCardResponse, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取闲鱼卡片失败: %w", err)
	}

	if err := s.GenerateShortLink(ctx, card); err != nil {
		// 没填跳转地址是可引导的用户错误：交回 controller 变 400，
		// 不能像 D-44 之前那样「旧链删了、新链没建、还回 200」。
		if errors.Is(err, ErrCardRedirectURLRequired) {
			return nil, err
		}
		// 其它失败（域名池被停用等）不阻断读取，但必须留痕，
		// 避免又回到「静默 200」的假成功。
		logger.Warnf("[xianyu] 刷新短链失败（不影响卡片读取）: %v", err)
	}

	return s.convertToResponse(ctx, card), nil
}

func (s *xianyuCardService) GetList(ctx context.Context, req *dto.XianyuCardListRequest) (*dto.XianyuCardListResponse, error) {
	filter := repository.CardListFilter{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, IsActive: req.IsActive}
	cards, total, err := s.repo.GetList(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("获取闲鱼卡片列表失败: %w", err)
	}

	list := make([]dto.XianyuCardResponse, len(cards))
	for i, card := range cards {
		list[i] = *s.convertToResponse(ctx, &card)
	}

	totalPage := int(total) / req.PageSize
	if int(total)%req.PageSize > 0 {
		totalPage++
	}

	return &dto.XianyuCardListResponse{
		List:      list,
		Total:     total,
		Page:      req.Page,
		PageSize:  req.PageSize,
		TotalPage: totalPage,
	}, nil
}

func (s *xianyuCardService) GenerateCardChatPage(ctx context.Context, id uint, baseURL string) (string, error) {
	card, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("获取闲鱼卡片失败: %w", err)
	}
	return s.templateService.RenderCardChatPage("xianyu", card.ID, card.Title, card.Description, card.ImageURL, card.Tags, baseURL)
}

func (s *xianyuCardService) GenerateShortLink(ctx context.Context, card *model.XianyuCard) error {
	// 跳转地址校验必须发生在删除旧短链之前：否则「重新生成短链」会把已经分发出去的
	// /s/xxxxxx 删掉、然后因为没目标地址直接返回，旧链接 404 而接口仍报成功（D-44）。
	if card.RedirectURL == "" {
		return ErrCardRedirectURLRequired
	}

	if card.ShortLinkID > 0 {
		// 旧短链已经被删过（历史 D-44 就留下过这种悬空 ID）时不能让它挡住重建：
		// 先确认存在再删，查不到就当作「已经删掉了」继续造新链。
		if _, err := s.shortLinkService.GetByID(ctx, card.ShortLinkID); err == nil {
			if err := s.shortLinkService.Delete(ctx, card.ShortLinkID); err != nil {
				return fmt.Errorf("删除旧短链失败: %w", err)
			}
		}
	}

	generateReq := &dto.GenerateShortCodeRequest{
		Length: 6,
	}
	generateResp, err := s.shortLinkService.GenerateShortCode(ctx, generateReq)
	if err != nil {
		return fmt.Errorf("生成短码失败：%w", err)
	}

	var domainID uint = 0
	if card.DomainPoolID != 0 {
		domainID = card.DomainPoolID
	}
	shortLink, err := s.shortLinkService.Create(ctx, &dto.CreateShortLinkRequest{
		ShortCode:   generateResp.ShortCode,
		OriginalURL: card.RedirectURL,
		Title:       card.Title,
		Description: card.Description,
		DomainID:    domainID,
	})
	if err != nil {
		return fmt.Errorf("创建短链失败: %w", err)
	}

	card.ShortLinkID = shortLink.ID
	return s.repo.Update(ctx, card)
}

func (s *xianyuCardService) convertToResponse(ctx context.Context, card *model.XianyuCard) *dto.XianyuCardResponse {
	shortLinkURL := ""
	shortCode := ""
	if card.ShortLinkID > 0 {
		shortLink, err := s.shortLinkService.GetByID(ctx, card.ShortLinkID)
		if err == nil && shortLink != nil {
			shortLinkURL = "/s/" + shortLink.ShortCode
			shortCode = shortLink.ShortCode
		}
	}

	return &dto.XianyuCardResponse{
		ID:           card.ID,
		Title:        card.Title,
		Description:  card.Description,
		ImageURL:     card.ImageURL,
		RedirectURL:  card.RedirectURL,
		DomainPoolID: &card.DomainPoolID,
		ShortLinkURL: shortLinkURL,
		ShortCode:    shortCode,
		Tags:         card.Tags,
		LikeCount:    card.LikeCount,
		ShareCount:   card.ShareCount,
		ViewCount:    card.ViewCount,
		IsActive:     card.IsActive,
		CreatedAt:    card.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:    card.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
