package service

import (
	"errors"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"time"

	"context"

	"github.com/google/uuid"
)

// 活码写口的业务错误哨兵。
//
// 修前这些分支一律 errors.New，controller 走 HandleDBError ⇒ 非"不存在"类错误
// 统统折成 500「创建活码失败」，调用方拿不到"短链重了"还是"域名停了"。
// 现在按原因分码：短链冲突 409、域名不可用 400（与 A15/A16 同一口径）。
var (
	ErrLiveCodeShortLinkExists   = errors.New("短链已存在")
	ErrLiveCodeDomainUnavailable = errors.New("活码域名不可用")
)

// LiveCodeService 活码服务接口
type LiveCodeService interface {
	Create(ctx context.Context, req *dto.CreateLiveCodeRequest) (*dto.LiveCodeResponse, error)
	Update(ctx context.Context, id string, req *dto.UpdateLiveCodeRequest) (*dto.LiveCodeResponse, error)
	Delete(ctx context.Context, id string) error
	RotateLiveCodes(ctx context.Context) error
	RecordClick(ctx context.Context, id string, ip, userAgent, referrer string) error

	GetByID(ctx context.Context, id string) (*dto.LiveCodeResponse, error)
	GetByShortLink(ctx context.Context, shortLink string) (*dto.LiveCodeResponse, error)
	GetList(ctx context.Context, page, pageSize int, name, status string) ([]*dto.LiveCodeResponse, int64, error)
	GetStats(ctx context.Context, id string) (*dto.LiveCodeStatsResponse, error)
	GenerateQRCode(ctx context.Context, id string, req *dto.GenerateQRCodeRequest) (*dto.LiveCodeQRResponse, error)
	GetQRCodes(ctx context.Context, id string) ([]*dto.LiveCodeQRResponse, error)
	GetQRStats(ctx context.Context, qrID string) (*dto.LiveCodeQRStatsResponse, error)
	Share(ctx context.Context, id string, req *dto.ShareLiveCodeRequest) (*dto.ShareLiveCodeResponse, error)
	DeleteQRCode(ctx context.Context, qrID string) error
	UpdateQRCode(ctx context.Context, qrID string, req *dto.UpdateLiveCodeQRRequest) error
}

type liveCodeService struct {
	liveCodeRepo repository.LiveCodeRepository
	qrCodeRepo   repository.LiveCodeQRRepository
	qrStatRepo   repository.LiveCodeQRRepository
	clickLogRepo repository.LiveCodeClickLogRepository
	domainRepo   repository.DomainPoolRepository
}

// NewLiveCodeService 创建活码服务实例（句柄由 repository 自取全局库）
func NewLiveCodeService() LiveCodeService {
	return &liveCodeService{
		liveCodeRepo: repository.NewLiveCodeRepositoryDefault(),
		qrCodeRepo:   repository.NewLiveCodeQRRepositoryDefault(),
		qrStatRepo:   repository.NewLiveCodeQRRepositoryDefault(),
		clickLogRepo: repository.NewLiveCodeClickLogRepositoryDefault(),
		domainRepo:   repository.NewDomainPoolRepositoryDefault(),
	}
}

func (s *liveCodeService) Create(ctx context.Context, req *dto.CreateLiveCodeRequest) (*dto.LiveCodeResponse, error) {
	existingCode, _ := s.liveCodeRepo.GetByShortLink(ctx, req.ShortLink)
	if existingCode != nil {
		return nil, fmt.Errorf("%w: %s", ErrLiveCodeShortLinkExists, req.ShortLink)
	}

	shortDomain, err := s.domainRepo.GetByID(ctx, int(req.ShortDomainID))
	if err != nil {
		return nil, fmt.Errorf("%w: 短链域名 %d", ErrLiveCodeDomainUnavailable, req.ShortDomainID)
	}
	if shortDomain.Status != 1 {
		return nil, fmt.Errorf("%w: 短链域名 %d", ErrLiveCodeDomainUnavailable, req.ShortDomainID)
	}

	entryDomain, err := s.domainRepo.GetByID(ctx, int(req.EntryDomainID))
	if err != nil {
		return nil, fmt.Errorf("%w: 入口域名 %d", ErrLiveCodeDomainUnavailable, req.EntryDomainID)
	}
	if entryDomain.Status != 1 {
		return nil, fmt.Errorf("%w: 入口域名 %d", ErrLiveCodeDomainUnavailable, req.EntryDomainID)
	}

	landingDomain, err := s.domainRepo.GetByID(ctx, int(req.LandingDomainID))
	if err != nil {
		return nil, fmt.Errorf("%w: 落地域名 %d", ErrLiveCodeDomainUnavailable, req.LandingDomainID)
	}
	if landingDomain.Status != 1 {
		return nil, fmt.Errorf("%w: 落地域名 %d", ErrLiveCodeDomainUnavailable, req.LandingDomainID)
	}

	liveCode := &model.LiveCode{
		ID:              uuid.New().String(),
		Name:            req.Name,
		ShortLink:       req.ShortLink,
		ShortDomainID:   req.ShortDomainID,
		EntryDomainID:   req.EntryDomainID,
		LandingDomainID: req.LandingDomainID,
		Status:          req.Status,
		TotalViews:      0,
		TodayViews:      0,
		ImageURL:        req.ImageURL,
		EntryURL:        req.EntryURL,
		LandingURL:      req.LandingURL,
	}

	err = s.liveCodeRepo.Create(ctx, liveCode)
	if err != nil {
		return nil, err
	}

	// 回读一次再回显：Create 时模型上没有 domain 关联，直接 modelToResponse
	// 会让创建响应里的 short_domain/entry_domain/landing_domain 全是 null，
	// 而紧接着的 GET 又是有值的——同一份数据两个形状（现场实测）。
	created, err := s.liveCodeRepo.GetByID(ctx, liveCode.ID)
	if err != nil {
		return s.modelToResponse(ctx, liveCode), nil
	}
	return s.modelToResponse(ctx, created), nil
}

func (s *liveCodeService) Update(ctx context.Context, id string, req *dto.UpdateLiveCodeRequest) (*dto.LiveCodeResponse, error) {
	liveCode, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	if req.ShortLink != "" && req.ShortLink != liveCode.ShortLink {
		existingCode, _ := s.liveCodeRepo.GetByShortLink(ctx, req.ShortLink)
		if existingCode != nil && existingCode.ID != id {
			return nil, fmt.Errorf("%w: %s", ErrLiveCodeShortLinkExists, req.ShortLink)
		}
		liveCode.ShortLink = req.ShortLink
	}

	if req.ShortDomainID > 0 && req.ShortDomainID != liveCode.ShortDomainID {
		domain, err := s.domainRepo.GetByID(ctx, int(req.ShortDomainID))
		if err != nil {
			return nil, fmt.Errorf("%w: 短链域名 %d", ErrLiveCodeDomainUnavailable, req.ShortDomainID)
		}
		if domain.Status != 1 {
			return nil, fmt.Errorf("%w: 短链域名 %d", ErrLiveCodeDomainUnavailable, req.ShortDomainID)
		}
		liveCode.ShortDomainID = req.ShortDomainID
	}

	if req.EntryDomainID > 0 && req.EntryDomainID != liveCode.EntryDomainID {
		domain, err := s.domainRepo.GetByID(ctx, int(req.EntryDomainID))
		if err != nil {
			return nil, fmt.Errorf("%w: 入口域名 %d", ErrLiveCodeDomainUnavailable, req.EntryDomainID)
		}
		if domain.Status != 1 {
			return nil, fmt.Errorf("%w: 入口域名 %d", ErrLiveCodeDomainUnavailable, req.EntryDomainID)
		}
		liveCode.EntryDomainID = req.EntryDomainID
	}

	if req.LandingDomainID > 0 && req.LandingDomainID != liveCode.LandingDomainID {
		domain, err := s.domainRepo.GetByID(ctx, int(req.LandingDomainID))
		if err != nil {
			return nil, fmt.Errorf("%w: 落地域名 %d", ErrLiveCodeDomainUnavailable, req.LandingDomainID)
		}
		if domain.Status != 1 {
			return nil, fmt.Errorf("%w: 落地域名 %d", ErrLiveCodeDomainUnavailable, req.LandingDomainID)
		}
		liveCode.LandingDomainID = req.LandingDomainID
	}

	if req.Name != "" {
		liveCode.Name = req.Name
	}
	if req.Status != 0 {
		liveCode.Status = req.Status
	}
	if req.ImageURL != "" {
		liveCode.ImageURL = req.ImageURL
	}
	if req.EntryURL != "" {
		liveCode.EntryURL = req.EntryURL
	}
	if req.LandingURL != "" {
		liveCode.LandingURL = req.LandingURL
	}

	err = s.liveCodeRepo.Update(ctx, liveCode)
	if err != nil {
		return nil, err
	}

	return s.modelToResponse(ctx, liveCode), nil
}

func (s *liveCodeService) Delete(ctx context.Context, id string) error {
	_, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return errors.New("活码不存在")
	}

	return s.liveCodeRepo.Delete(ctx, id)
}

func (s *liveCodeService) RotateLiveCodes(ctx context.Context) error {
	liveCodes, err := s.liveCodeRepo.GetAvailableLiveCodes(ctx)
	if err != nil {
		return err
	}

	for _, liveCode := range liveCodes {
		if liveCode.DailyClicks >= 200 {
			continue
		}

		qrCode := &model.LiveCodeQR{
			LiveCodeID: liveCode.ID,
			Status:     1,
		}

		err := s.qrCodeRepo.Create(ctx, qrCode)
		if err != nil {
			continue
		}
	}

	return nil
}

func (s *liveCodeService) RecordClick(ctx context.Context, qrID string, ip, userAgent, referrer string) error {
	qrCode, err := s.qrCodeRepo.GetByID(ctx, qrID)
	if err != nil {
		return err
	}

	if _, err := s.liveCodeRepo.GetByID(ctx, qrCode.LiveCodeID); err != nil {
		return fmt.Errorf("活码 %s 不存在: %w", qrCode.LiveCodeID, err)
	}

	if err := s.liveCodeRepo.IncrementClicks(ctx, qrCode.LiveCodeID); err != nil {
		return err
	}

	if err := s.qrCodeRepo.IncrementClickStat(ctx, qrID); err != nil {
		return err
	}

	if err := s.clickLogRepo.CreateLiveCodeClick(ctx, &model.LiveCodeClickLog{
		LiveCodeID: qrCode.LiveCodeID,
		QRCodeID:   qrID,
		IPAddress:  ip,
		UserAgent:  userAgent,
		Referrer:   referrer,
	}); err != nil {
		return err
	}
	if err := s.clickLogRepo.CreateQRCodeClick(ctx, &model.QRCodeClickLog{
		QRCodeID:   qrID,
		LiveCodeID: qrCode.LiveCodeID,
		IPAddress:  ip,
		UserAgent:  userAgent,
		Referrer:   referrer,
	}); err != nil {
		return err
	}

	return nil
}

func (s *liveCodeService) GetByID(ctx context.Context, id string) (*dto.LiveCodeResponse, error) {
	liveCode, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	return s.modelToResponse(ctx, liveCode), nil
}

func (s *liveCodeService) GetByShortLink(ctx context.Context, shortLink string) (*dto.LiveCodeResponse, error) {
	liveCode, err := s.liveCodeRepo.GetByShortLink(ctx, shortLink)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	return s.modelToResponse(ctx, liveCode), nil
}

func (s *liveCodeService) GetList(ctx context.Context, page, pageSize int, name, status string) ([]*dto.LiveCodeResponse, int64, error) {
	liveCodes, total, err := s.liveCodeRepo.GetList(ctx, page, pageSize, name, status)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]*dto.LiveCodeResponse, len(liveCodes))
	for i, liveCode := range liveCodes {
		responses[i] = s.modelToResponse(ctx, liveCode)
	}

	return responses, total, nil
}

func (s *liveCodeService) GetStats(ctx context.Context, id string) (*dto.LiveCodeStatsResponse, error) {
	liveCode, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	qrCodes, err := s.qrCodeRepo.GetByLiveCodeID(ctx, id)
	if err != nil {
		return nil, err
	}

	totalQRShown := 0
	totalQRClicks := 0
	activeQRCount := 0

	for _, qrCode := range qrCodes {
		if qrCode.Status == 1 {
			activeQRCount++
		}
	}

	if shown, clicks, err := s.qrCodeRepo.SumLiveCodeStats(ctx, id); err == nil {
		totalQRShown = int(shown)
		totalQRClicks = int(clicks)
	}

	return &dto.LiveCodeStatsResponse{
		LiveCodeID:     id,
		TotalViews:     liveCode.TotalViews,
		TodayViews:     liveCode.TodayViews,
		TotalClicks:    liveCode.TotalClicks,
		TodayClicks:    liveCode.DailyClicks,
		TotalQRShown:   totalQRShown,
		TotalQRClicks:  totalQRClicks,
		ActiveQRCount:  activeQRCount,
		TotalQRCount:   len(qrCodes),
		ConversionRate: calculateConversionRate(totalQRShown, totalQRClicks),
	}, nil
}

func (s *liveCodeService) GenerateQRCode(ctx context.Context, id string, req *dto.GenerateQRCodeRequest) (*dto.LiveCodeQRResponse, error) {
	_, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	imagePath := fmt.Sprintf("/uploads/qrcodes/%s.png", uuid.New().String())

	qrCode := &model.LiveCodeQR{
		ID:         uuid.New().String(),
		LiveCodeID: id,
		ImageURL:   imagePath,
		Status:     req.Status,
		ExpireDays: req.ExpireDays,
	}

	err = s.qrCodeRepo.Create(ctx, qrCode)
	if err != nil {
		return nil, err
	}

	return s.qrModelToResponse(ctx, qrCode), nil
}

func (s *liveCodeService) GetQRCodes(ctx context.Context, id string) ([]*dto.LiveCodeQRResponse, error) {
	_, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	qrCodes, err := s.qrCodeRepo.GetByLiveCodeID(ctx, id)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.LiveCodeQRResponse, len(qrCodes))
	for i, qrCode := range qrCodes {
		responses[i] = s.qrModelToResponse(ctx, qrCode)
	}

	return responses, nil
}

func (s *liveCodeService) GetQRStats(ctx context.Context, qrID string) (*dto.LiveCodeQRStatsResponse, error) {
	qrCode, err := s.qrCodeRepo.GetByID(ctx, qrID)
	if err != nil {
		return nil, errors.New("二维码不存在")
	}

	stats, err := s.qrCodeRepo.GetStats(ctx, qrID)
	if err != nil {
		return nil, err
	}

	accessStats := make([]*dto.LiveCodeQRStatItem, len(stats))
	for i, stat := range stats {
		accessStats[i] = &dto.LiveCodeQRStatItem{
			ID:        fmt.Sprintf("%d", stat.ID),
			QRCodeID:  stat.QRCodeID,
			Date:      stat.Date,
			CreatedAt: stat.CreatedAt,
		}
	}

	viewCount := 0
	clickCount := 0
	if shown, clicks, err := s.qrCodeRepo.SumStats(ctx, qrID); err == nil {
		viewCount = int(shown)
		clickCount = int(clicks)
	}

	return &dto.LiveCodeQRStatsResponse{
		QRCodeID:            qrCode.ID,
		ExpireDays:          qrCode.ExpireDays,
		ViewCount:           viewCount,
		ClickCount:          clickCount,
		ExpireTime:          time.Now().AddDate(0, 0, qrCode.ExpireDays),
		Status:              qrCode.Status,
		IsExpired:           false,
		IsDailyLimitReached: false,
		AccessStats:         accessStats,
	}, nil
}

func (s *liveCodeService) Share(ctx context.Context, id string, req *dto.ShareLiveCodeRequest) (*dto.ShareLiveCodeResponse, error) {
	liveCode, err := s.liveCodeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("活码不存在")
	}

	var availableQR *model.LiveCodeQR
	qrCodes, err := s.qrCodeRepo.GetByLiveCodeID(ctx, id)
	if err != nil {
		return nil, err
	}

	for _, qr := range qrCodes {
		if qr.Status == 1 {
			availableQR = qr
			break
		}
	}

	if availableQR == nil {
		return nil, errors.New("没有可用的二维码")
	}

	if err := s.qrCodeRepo.IncrementViewStat(ctx, availableQR.ID); err != nil {
		return nil, err
	}

	return &dto.ShareLiveCodeResponse{
		ShortLink:   liveCodeFullShortLink(liveCode),
		EntryLink:   liveCodeFullEntryLink(liveCode),
		LandingLink: liveCodeFullLandingLink(liveCode),
		QRImagePath: availableQR.ImageURL,
		QRCodeID:    availableQR.ID,
	}, nil
}

func (s *liveCodeService) DeleteQRCode(ctx context.Context, qrID string) error {
	_, err := s.qrCodeRepo.GetByID(ctx, qrID)
	if err != nil {
		return errors.New("二维码不存在")
	}

	return s.qrCodeRepo.Delete(ctx, qrID)
}

func (s *liveCodeService) UpdateQRCode(ctx context.Context, qrID string, req *dto.UpdateLiveCodeQRRequest) error {
	qrCode, err := s.qrCodeRepo.GetByID(ctx, qrID)
	if err != nil {
		return errors.New("二维码不存在")
	}

	if req.Status != nil {
		qrCode.Status = *req.Status
	}
	if req.ExpireDays != nil {
		qrCode.ExpireDays = *req.ExpireDays
	}

	return s.qrCodeRepo.Update(ctx, qrCode)
}

func (s *liveCodeService) modelToResponse(ctx context.Context, liveCode *model.LiveCode) *dto.LiveCodeResponse {
	response := &dto.LiveCodeResponse{
		ID:              liveCode.ID,
		Name:            liveCode.Name,
		ShortLink:       liveCode.ShortLink,
		ShortDomainID:   liveCode.ShortDomainID,
		EntryDomainID:   liveCode.EntryDomainID,
		LandingDomainID: liveCode.LandingDomainID,
		Status:          liveCode.Status,
		TotalViews:      liveCode.TotalViews,
		TodayViews:      liveCode.TodayViews,
		CreatedAt:       liveCode.CreatedAt,
		UpdatedAt:       liveCode.UpdatedAt,
		FullShortLink:   liveCodeFullShortLink(liveCode),
		FullEntryLink:   liveCodeFullEntryLink(liveCode),
		FullLandingLink: liveCodeFullLandingLink(liveCode),
		ImageURL:        liveCode.ImageURL,
		EntryURL:        liveCode.EntryURL,
		LandingURL:      liveCode.LandingURL,
	}

	if liveCode.ShortDomain != nil {
		response.ShortDomain = &dto.DomainPoolResponse{
			ID:        liveCode.ShortDomain.ID,
			Domain:    liveCode.ShortDomain.Domain,
			Port:      liveCode.ShortDomain.Port,
			Purpose:   liveCode.ShortDomain.Purpose,
			Status:    liveCode.ShortDomain.Status,
			CreatedAt: liveCode.ShortDomain.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: liveCode.ShortDomain.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	if liveCode.EntryDomain != nil {
		response.EntryDomain = &dto.DomainPoolResponse{
			ID:        liveCode.EntryDomain.ID,
			Domain:    liveCode.EntryDomain.Domain,
			Port:      liveCode.EntryDomain.Port,
			Purpose:   liveCode.EntryDomain.Purpose,
			Status:    liveCode.EntryDomain.Status,
			CreatedAt: liveCode.EntryDomain.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: liveCode.EntryDomain.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	if liveCode.LandingDomain != nil {
		response.LandingDomain = &dto.DomainPoolResponse{
			ID:        liveCode.LandingDomain.ID,
			Domain:    liveCode.LandingDomain.Domain,
			Port:      liveCode.LandingDomain.Port,
			Purpose:   liveCode.LandingDomain.Purpose,
			Status:    liveCode.LandingDomain.Status,
			CreatedAt: liveCode.LandingDomain.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt: liveCode.LandingDomain.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
	}

	return response
}

func (s *liveCodeService) qrModelToResponse(ctx context.Context, qrCode *model.LiveCodeQR) *dto.LiveCodeQRResponse {
	viewCount := 0
	clickCount := 0
	if shown, clicks, err := s.qrCodeRepo.SumStats(ctx, qrCode.ID); err == nil {
		viewCount = int(shown)
		clickCount = int(clicks)
	}

	return &dto.LiveCodeQRResponse{
		ID:                  qrCode.ID,
		LiveCodeID:          qrCode.LiveCodeID,
		ImageURL:            qrCode.ImageURL,
		QRImageURL:          qrCode.ImageURL,
		ExpireDays:          qrCode.ExpireDays,
		ViewCount:           viewCount,
		ClickCount:          clickCount,
		Status:              qrCode.Status,
		ExpireTime:          time.Now().AddDate(0, 0, qrCode.ExpireDays),
		CreatedAt:           qrCode.CreatedAt,
		UpdatedAt:           qrCode.UpdatedAt,
		IsExpired:           false,
		IsDailyLimitReached: false,
	}
}

func liveCodeFullShortLink(l *model.LiveCode) string {
	if l.ShortDomain != nil {
		protocol := "http"
		if l.ShortDomain.Port == 443 {
			protocol = "https"
		}
		return protocol + "://" + l.ShortDomain.Domain + "/" + l.ShortLink
	}
	return ""
}

func liveCodeFullEntryLink(l *model.LiveCode) string {
	if l.EntryDomain != nil {
		protocol := "http"
		if l.EntryDomain.Port == 443 {
			protocol = "https"
		}
		return protocol + "://" + l.EntryDomain.Domain + "/entry/" + l.ShortLink
	}
	return ""
}

func liveCodeFullLandingLink(l *model.LiveCode) string {
	if l.LandingDomain != nil {
		protocol := "http"
		if l.LandingDomain.Port == 443 {
			protocol = "https"
		}
		return protocol + "://" + l.LandingDomain.Domain + "/landing/" + l.ShortLink
	}
	return ""
}

func calculateConversionRate(shown, clicks int) float64 {
	if shown == 0 {
		return 0
	}
	return float64(clicks) / float64(shown) * 100
}
