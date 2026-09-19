package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"hivemtk-user/internal/config"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"math/big"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ShortLinkService 短链服务接口
type ShortLinkService interface {
	Create(ctx context.Context, req *dto.CreateShortLinkRequest) (*dto.ShortLinkResponse, error)
	Update(ctx context.Context, req *dto.UpdateShortLinkRequest) (*dto.ShortLinkResponse, error)
	Delete(ctx context.Context, id uint) error
	GetByID(ctx context.Context, id uint) (*dto.ShortLinkResponse, error)
	GetByShortCode(ctx context.Context, shortCode string) (*dto.ShortLinkResponse, error)
	GetList(ctx context.Context, req *dto.ListShortLinkRequest) (*dto.ShortLinkListResponse, error)
	AccessShortLink(ctx context.Context, req *dto.AccessShortLinkRequest) (*dto.AccessShortLinkResponse, error)
	GenerateShortCode(ctx context.Context, req *dto.GenerateShortCodeRequest) (*dto.GenerateShortCodeResponse, error)
	GetStats(ctx context.Context, req *dto.ShortLinkStatsRequest) (*dto.ShortLinkStatsResponse, error)
	GetAllStats(ctx context.Context, req *dto.AllShortLinksStatsRequest) (*dto.AllShortLinksStatsResponse, error)
	ShareShortLink(ctx context.Context, req *dto.ShareShortLinkRequest) (*dto.ShareShortLinkResponse, error)
}

// IsShortLinkExpired 检查短链是否已过期（从 model 迁移而来的包级函数）
func IsShortLinkExpired(s *model.ShortLink) bool {
	if s == nil || s.ExpireTime == nil {
		return false
	}
	return time.Now().After(*s.ExpireTime)
}

// IsShortLinkActive 检查短链是否处于活跃状态（从 model 迁移而来的包级函数）
func IsShortLinkActive(s *model.ShortLink) bool {
	return s != nil && s.Status == 1 && !IsShortLinkExpired(s)
}

type shortLinkService struct {
	shortLinkRepo repository.ShortLinkRepository
	domainRepo    repository.DomainPoolRepository
	accessRepo    repository.ShortLinkAccessRepository
}

// NewShortLinkService 创建短链服务实例
func NewShortLinkService(db *gorm.DB) ShortLinkService {
	return &shortLinkService{
		shortLinkRepo: repository.NewShortLinkRepository(db),
		domainRepo:    repository.NewDomainPoolRepository(db),
		accessRepo:    repository.NewShortLinkAccessRepository(db),
	}
}

func validateTargetURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return errors.New("目标链接不能为空")
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return errors.New("目标链接格式无效")
	}
	if parsed.Scheme != "https" {
		return errors.New("铁律#24: 短链目标地址必须为 https://")
	}
	return nil
}

func (s *shortLinkService) Create(ctx context.Context, req *dto.CreateShortLinkRequest) (*dto.ShortLinkResponse, error) {
	if err := validateTargetURL(req.OriginalURL); err != nil {
		return nil, err
	}
	existingLink, _ := s.shortLinkRepo.GetByShortCode(ctx, req.ShortCode)
	if existingLink != nil {
		return nil, errors.New("短码已存在")
	}

	if req.DomainID > 0 {
		domain, err := s.domainRepo.GetByID(ctx, int(req.DomainID))
		if err != nil {
			return nil, errors.New("域名不存在")
		}
		if domain.Status != 1 {
			return nil, errors.New("域名不可用")
		}
	}

	if req.UtmSource != "" || req.UtmMedium != "" || req.UtmCampaign != "" {
		req.OriginalURL = appendUTM(req.OriginalURL, req.UtmSource, req.UtmMedium, req.UtmCampaign)
	}

	shortLink := &model.ShortLink{
		ShortCode:   req.ShortCode,
		OriginalURL: req.OriginalURL,
		Title:       req.Title,
		Description: req.Description,
		DomainID:    req.DomainID,
		Password:    req.Password,
		ExpireTime:  req.ExpireTime,
		Status:      1,
	}

	err := s.shortLinkRepo.Create(ctx, shortLink)
	if err != nil {
		return nil, err
	}

	return s.modelToResponse(ctx, shortLink), nil
}

func (s *shortLinkService) Update(ctx context.Context, req *dto.UpdateShortLinkRequest) (*dto.ShortLinkResponse, error) {
	if req.OriginalURL != "" {
		if err := validateTargetURL(req.OriginalURL); err != nil {
			return nil, err
		}
	}
	shortLink, err := s.shortLinkRepo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	if req.ShortCode != "" && req.ShortCode != shortLink.ShortCode {
		existingLink, _ := s.shortLinkRepo.GetByShortCode(ctx, req.ShortCode)
		if existingLink != nil && existingLink.ID != req.ID {
			return nil, errors.New("短码已存在")
		}
		shortLink.ShortCode = req.ShortCode
	}

	if req.DomainID > 0 && req.DomainID != shortLink.DomainID {
		domain, err := s.domainRepo.GetByID(ctx, int(req.DomainID))
		if err != nil {
			return nil, errors.New("域名不存在")
		}
		if domain.Status != 1 {
			return nil, errors.New("域名不可用")
		}
		shortLink.DomainID = req.DomainID
	}

	shortLink.OriginalURL = req.OriginalURL
	shortLink.Title = req.Title
	shortLink.Description = req.Description
	shortLink.Password = req.Password
	shortLink.ExpireTime = req.ExpireTime
	shortLink.Status = req.Status

	err = s.shortLinkRepo.Update(ctx, shortLink)
	if err != nil {
		return nil, err
	}

	return s.modelToResponse(ctx, shortLink), nil
}

func (s *shortLinkService) Delete(ctx context.Context, id uint) error {

	_, err := s.shortLinkRepo.GetByID(ctx, id)
	if err != nil {
		return errors.New("短链不存在")
	}

	if err := s.shortLinkRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("删除短链失败: %v", err)
	}

	if err := s.accessRepo.DeleteByShortLinkID(ctx, id); err != nil {

		return fmt.Errorf("删除短链访问记录失败(主表已删): %v", err)
	}

	return nil
}

func (s *shortLinkService) GetByID(ctx context.Context, id uint) (*dto.ShortLinkResponse, error) {
	shortLink, err := s.shortLinkRepo.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	return s.modelToResponse(ctx, shortLink), nil
}

func (s *shortLinkService) GetByShortCode(ctx context.Context, shortCode string) (*dto.ShortLinkResponse, error) {
	shortLink, err := s.shortLinkRepo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	return s.modelToResponse(ctx, shortLink), nil
}

func (s *shortLinkService) GetList(ctx context.Context, req *dto.ListShortLinkRequest) (*dto.ShortLinkListResponse, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}

	shortLinks, total, err := s.shortLinkRepo.GetList(ctx, req.Page, req.PageSize, req.ShortCode, req.OriginalURL, req.Status)
	if err != nil {
		return nil, err
	}

	var responses []dto.ShortLinkResponse
	for _, shortLink := range shortLinks {
		responses = append(responses, *s.modelToResponse(ctx, shortLink))
	}

	return &dto.ShortLinkListResponse{
		List:  responses,
		Total: total,
	}, nil
}

func (s *shortLinkService) AccessShortLink(ctx context.Context, req *dto.AccessShortLinkRequest) (*dto.AccessShortLinkResponse, error) {
	shortLink, err := s.shortLinkRepo.GetByShortCode(ctx, req.ShortCode)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	if !IsShortLinkActive(shortLink) {
		return nil, errors.New("短链已过期或已禁用")
	}

	if shortLink.Password != "" && shortLink.Password != req.Password {
		return nil, errors.New("密码错误")
	}

	err = s.shortLinkRepo.IncreaseClickCount(ctx, shortLink.ID)
	if err != nil {
		logger.Errorf("增加点击次数失败: %v", err)
	}

	accessRecord := &model.ShortLinkAccess{
		ShortLinkID: shortLink.ID,
		IP:          req.IP,
		UserAgent:   req.UserAgent,
		Referer:     req.Referer,
		DeviceType:  utils.ParseDeviceType(req.UserAgent),
		Browser:     utils.ParseBrowser(req.UserAgent),
		OS:          utils.ParseOS(req.UserAgent),
		Location:    utils.ParseLocation(req.IP),
		AccessTime:  time.Now(),
	}

	err = s.accessRepo.Create(ctx, accessRecord)
	if err != nil {
		logger.Errorf("记录访问信息失败: %v", err)
	}

	return &dto.AccessShortLinkResponse{
		OriginalURL: shortLink.OriginalURL,
		Title:       shortLink.Title,
	}, nil
}

func (s *shortLinkService) GenerateShortCode(ctx context.Context, req *dto.GenerateShortCodeRequest) (*dto.GenerateShortCodeResponse, error) {
	if req.Length <= 0 {
		req.Length = 6
	}

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	maxAttempts := 100

	for i := 0; i < maxAttempts; i++ {
		shortCode := s.generateRandomString(ctx, req.Length, charset)

		_, err := s.shortLinkRepo.GetByShortCode(ctx, shortCode)
		if err != nil {
			return &dto.GenerateShortCodeResponse{
				ShortCode: shortCode,
			}, nil
		}
	}

	return nil, errors.New("生成短码失败，请重试")
}

func (s *shortLinkService) generateRandomString(ctx context.Context, length int, charset string) string {
	result := make([]byte, length)
	charsetLength := big.NewInt(int64(len(charset)))

	for i := 0; i < length; i++ {
		randomIndex, _ := rand.Int(rand.Reader, charsetLength)
		result[i] = charset[randomIndex.Int64()]
	}

	return string(result)
}

func (s *shortLinkService) modelToResponse(ctx context.Context, shortLink *model.ShortLink) *dto.ShortLinkResponse {
	statusStr := "正常"
	if shortLink.Status == 2 {
		statusStr = "禁用"
	} else if IsShortLinkExpired(shortLink) {
		statusStr = "已过期"
	}

	return &dto.ShortLinkResponse{
		ID:          shortLink.ID,
		ShortCode:   shortLink.ShortCode,
		OriginalURL: shortLink.OriginalURL,
		Title:       shortLink.Title,
		Description: shortLink.Description,
		DomainID:    shortLink.DomainID,
		Password:    shortLink.Password,
		ExpireTime:  shortLink.ExpireTime,
		ClickCount:  shortLink.ClickCount,
		Status:      shortLink.Status,
		StatusStr:   statusStr,
		CreatedAt:   shortLink.CreatedAt,
		UpdatedAt:   shortLink.UpdatedAt,
	}
}

func (s *shortLinkService) GetStats(ctx context.Context, req *dto.ShortLinkStatsRequest) (*dto.ShortLinkStatsResponse, error) {
	shortLink, err := s.shortLinkRepo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	var startDate, endDate time.Time
	if req.StartDate != "" {
		startDate, err = time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return nil, errors.New("开始日期格式错误，请使用YYYY-MM-DD格式")
		}
	}

	if req.EndDate != "" {
		endDate, err = time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return nil, errors.New("结束日期格式错误，请使用YYYY-MM-DD格式")
		}
		// 不再 +23:59:59：仓储层比较的是 DATE(access_time) 这个「日历日」，
		// 结束日整天本就在区间内；再叠 23:59:59 会把区间尾巴推进业务时区的**下一日**
		// （time.Parse 得到的是 UTC 零点，+8h 后跨日），凭空多算一天。
	}

	var dailyStatsResponse []dto.DailyStats
	var totalAccess int64

	if startDate.IsZero() && endDate.IsZero() {

		_, totalAccessCount, err := s.accessRepo.GetByShortLinkID(ctx, req.ID, 1, 0)
		if err != nil {
			logger.Errorf("统计短链 %d 总访问数失败: %v", req.ID, err)
		} else {
			totalAccess = totalAccessCount
		}
	} else {

		dailyStats, err := s.accessRepo.GetDailyStatsByShortLinkID(ctx, req.ID, startDate, endDate)
		if err != nil {
			logger.Errorf("查询短链 %d 每日访问统计失败: %v", req.ID, err)
		}
		for _, stat := range dailyStats {
			if count, ok := stat["count"].(int64); ok {
				totalAccess += count
			}
			date, _ := stat["date"].(string)
			count, _ := stat["count"].(int64)
			dailyStatsResponse = append(dailyStatsResponse, dto.DailyStats{
				Date:  date,
				Count: count,
			})
		}
	}

	// 「今天」= 业务日（CST）。把同一个 now 同时当起止传给仓储层，
	// 由它统一用 timeutil.BusinessDate 折算成日历日，避免在这里再拼一遍 UTC 零点。
	now := time.Now()
	todayStats, err := s.accessRepo.GetDailyStatsByShortLinkID(ctx, req.ID, now, now)
	if err != nil {
		logger.Errorf("查询短链 %d 今日访问统计失败: %v", req.ID, err)
	}
	var todayAccess int64
	if len(todayStats) > 0 {
		if count, ok := todayStats[0]["count"].(int64); ok {
			todayAccess = count
		}
	}

	deviceTypeStats, err := s.accessRepo.GetDeviceTypeStatsByShortLinkID(ctx, req.ID, startDate, endDate)
	if err != nil {
		logger.Errorf("查询短链 %d 设备类型统计失败: %v", req.ID, err)
	}
	var deviceStats []dto.DeviceTypeStats
	for _, stat := range deviceTypeStats {
		deviceType, _ := stat["device_type"].(string)
		count, _ := stat["count"].(int64)

		var percentage float64
		if totalAccess > 0 {
			percentage = float64(count) / float64(totalAccess) * 100
		}

		deviceStats = append(deviceStats, dto.DeviceTypeStats{
			DeviceType: deviceType,
			Count:      count,
			Percentage: percentage,
		})
	}

	return &dto.ShortLinkStatsResponse{
		ShortLinkID:     shortLink.ID,
		ShortCode:       shortLink.ShortCode,
		OriginalURL:     shortLink.OriginalURL,
		Title:           shortLink.Title,
		TotalAccess:     totalAccess,
		TodayAccess:     todayAccess,
		DeviceTypeStats: deviceStats,
		DailyStats:      dailyStatsResponse,
	}, nil
}

func (s *shortLinkService) GetAllStats(ctx context.Context, req *dto.AllShortLinksStatsRequest) (*dto.AllShortLinksStatsResponse, error) {

	var startDate, endDate time.Time
	var err error

	if req.StartDate != "" {
		startDate, err = time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return nil, errors.New("开始日期格式错误，请使用YYYY-MM-DD格式")
		}
	}

	if req.EndDate != "" {
		endDate, err = time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return nil, errors.New("结束日期格式错误，请使用YYYY-MM-DD格式")
		}
		// 同 GetStats：仓储层按日历日比较，结束日整天已在区间内，不再叠 +23:59:59。
	}

	// 每日统计只查一次：总数和按日明细都来自同一份结果，
	// 分两次查既多一倍 DB 往返，也可能因两次查询之间有新访问而让明细加总对不上总数。
	dailyStats, err := s.accessRepo.GetAllDailyStats(ctx, startDate, endDate)
	if err != nil {
		logger.Errorf("查询全量短链每日访问统计失败: %v", err)
	}
	var totalAccess int64
	var dailyStatsResponse []dto.DailyStats
	for _, stat := range dailyStats {
		count, _ := stat["count"].(int64)
		totalAccess += count
		date, _ := stat["date"].(string)
		dailyStatsResponse = append(dailyStatsResponse, dto.DailyStats{
			Date:  date,
			Count: count,
		})
	}

	// 「今天」= 业务日（CST），起止传同一个 now，由仓储层折算成单个日历日。
	now := time.Now()
	todayStats, err := s.accessRepo.GetAllDailyStats(ctx, now, now)
	if err != nil {
		logger.Errorf("查询全量短链今日访问统计失败: %v", err)
	}
	var todayAccess int64
	for _, stat := range todayStats {
		if count, ok := stat["count"].(int64); ok {
			todayAccess += count
		}
	}

	deviceTypeStats, err := s.accessRepo.GetAllDeviceTypeStats(ctx, startDate, endDate)
	if err != nil {
		logger.Errorf("查询全量短链设备类型统计失败: %v", err)
	}
	var deviceStats []dto.DeviceTypeStats
	for _, stat := range deviceTypeStats {
		deviceType, _ := stat["device_type"].(string)
		count, _ := stat["count"].(int64)

		var percentage float64
		if totalAccess > 0 {
			percentage = float64(count) / float64(totalAccess) * 100
		}

		deviceStats = append(deviceStats, dto.DeviceTypeStats{
			DeviceType: deviceType,
			Count:      count,
			Percentage: percentage,
		})
	}

	shortLinksStats, err := s.accessRepo.GetAllShortLinksBasicStats(ctx, startDate, endDate)
	if err != nil {
		logger.Errorf("查询全量短链基础统计失败: %v", err)
	}
	var shortLinksStatsResponse []dto.ShortLinkBasicStats
	for _, stat := range shortLinksStats {
		id, _ := stat["id"].(uint)
		shortCode, _ := stat["short_code"].(string)
		title, _ := stat["title"].(string)
		accessCount, _ := stat["access_count"].(int64)

		shortLinksStatsResponse = append(shortLinksStatsResponse, dto.ShortLinkBasicStats{
			ID:          id,
			ShortCode:   shortCode,
			Title:       title,
			AccessCount: accessCount,
		})
	}

	return &dto.AllShortLinksStatsResponse{
		TotalAccess:     totalAccess,
		TodayAccess:     todayAccess,
		DeviceTypeStats: deviceStats,
		DailyStats:      dailyStatsResponse,
		ShortLinkStats:  shortLinksStatsResponse,
	}, nil
}

func (s *shortLinkService) ShareShortLink(ctx context.Context, req *dto.ShareShortLinkRequest) (*dto.ShareShortLinkResponse, error) {
	shortLink, err := s.shortLinkRepo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, errors.New("短链不存在")
	}

	shortURL := fmt.Sprintf("%s/s/%s", config.DefaultUserServerBaseURL, shortLink.ShortCode)

	qrCode := utils.GenerateQRCode(shortURL)

	return &dto.ShareShortLinkResponse{
		ShortURL: shortURL,
		QRCode:   qrCode,
	}, nil
}

func appendUTM(rawURL, source, medium, campaign string) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	added := []string{}
	if source != "" && !strings.Contains(rawURL, "utm_source=") {
		added = append(added, "utm_source="+url.QueryEscape(source))
	}
	if medium != "" && !strings.Contains(rawURL, "utm_medium=") {
		added = append(added, "utm_medium="+url.QueryEscape(medium))
	}
	if campaign != "" && !strings.Contains(rawURL, "utm_campaign=") {
		added = append(added, "utm_campaign="+url.QueryEscape(campaign))
	}
	if len(added) == 0 {
		return rawURL
	}
	return rawURL + sep + strings.Join(added, "&")
}
