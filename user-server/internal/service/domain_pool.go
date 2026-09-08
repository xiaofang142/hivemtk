package service

import (
	"context"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"io"
	"net/http"
	"sync"
	"time"

	"gorm.io/gorm"
)

// DomainPoolService 域名池服务接口
type DomainPoolService interface {
	Create(ctx context.Context, domain string, port int, purpose string) (*model.DomainPool, error)
	Update(ctx context.Context, id int, domain string, port int, purpose string, status int) (*model.DomainPool, error)
	Delete(ctx context.Context, id int) error
	GetByID(ctx context.Context, id int) (*model.DomainPool, error)
	List(ctx context.Context, page, pageSize int, domain string, status int) ([]*model.DomainPool, int64, error)
	CheckDomain(ctx context.Context, id int) (bool, error)
	CheckAllDomains(ctx context.Context) ([]dto.DomainPoolCheckResponse, error)
	AddBlacklist(ctx context.Context, domain, platform, reason, source string, ttlHours int) error
	RemoveBlacklist(ctx context.Context, domain string) error
	ListBlacklist(ctx context.Context, page, pageSize int) ([]*model.DomainBlacklist, int64, error)
	IsBlacklisted(ctx context.Context, domain string) (bool, error)
	SuspendDomain(ctx context.Context, id int) (*model.DomainPool, error)
	RotateToBackup(ctx context.Context, id int) (*model.DomainPool, error)
	ListAlerts(ctx context.Context) ([]*model.DomainPool, error)
	ResolveAlert(ctx context.Context, id int) (*model.DomainPool, error)
	SetAutoSwitch(ctx context.Context, id int, enabled bool) (*model.DomainPool, error)
}

type domainPoolService struct {
	domainPoolRepo repository.DomainPoolRepository
	blacklistRepo  *repository.DomainBlacklistRepository
	db             *gorm.DB
}

// NewDomainPoolService 创建域名池服务实例
func NewDomainPoolService(db *gorm.DB) DomainPoolService {
	return &domainPoolService{
		domainPoolRepo: repository.NewDomainPoolRepository(db),
		blacklistRepo:  repository.NewDomainBlacklistRepository(db),
		db:             db,
	}
}

func (s *domainPoolService) Create(ctx context.Context, domain string, port int, purpose string) (*model.DomainPool, error) {
	existingDomain, _ := s.domainPoolRepo.GetByDomain(ctx, domain)
	if existingDomain != nil {
		return nil, fmt.Errorf("域名已存在")
	}

	if port <= 0 {
		port = 80
	}

	domainPool := &model.DomainPool{
		Domain:    domain,
		Port:      port,
		Purpose:   purpose,
		Status:    1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err := s.domainPoolRepo.Create(ctx, domainPool)
	if err != nil {
		return nil, err
	}

	return domainPool, nil
}

func (s *domainPoolService) Update(ctx context.Context, id int, domain string, port int, purpose string, status int) (*model.DomainPool, error) {
	domainPool, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if domain != domainPool.Domain {
		existingDomain, _ := s.domainPoolRepo.GetByDomain(ctx, domain)
		if existingDomain != nil && existingDomain.ID != id {
			return nil, fmt.Errorf("域名已被其他记录使用")
		}
	}

	if port <= 0 {
		port = 80
	}

	domainPool.Domain = domain
	domainPool.Port = port
	domainPool.Purpose = purpose
	domainPool.Status = status
	domainPool.UpdatedAt = time.Now()

	err = s.domainPoolRepo.Update(ctx, domainPool)
	if err != nil {
		return nil, err
	}

	return domainPool, nil
}

// SetAutoSwitch 更新自动切换开关
func (s *domainPoolService) SetAutoSwitch(ctx context.Context, id int, enabled bool) (*model.DomainPool, error) {
	domainPool, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	domainPool.AutoSwitchEnabled = enabled
	domainPool.UpdatedAt = time.Now()
	if err := s.domainPoolRepo.Update(ctx, domainPool); err != nil {
		return nil, err
	}
	return domainPool, nil
}

func (s *domainPoolService) Delete(ctx context.Context, id int) error {
	return s.domainPoolRepo.Delete(context.Background(), id)
}

func (s *domainPoolService) GetByID(ctx context.Context, id int) (*model.DomainPool, error) {
	return s.domainPoolRepo.GetByID(context.Background(), id)
}

func (s *domainPoolService) List(ctx context.Context, page, pageSize int, domain string, status int) ([]*model.DomainPool, int64, error) {
	return s.domainPoolRepo.List(context.Background(), page, pageSize, domain, status)
}

func (s *domainPoolService) CheckDomain(ctx context.Context, id int) (bool, error) {
	domainPool, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return false, err
	}

	url := fmt.Sprintf("http://%s:%d", domainPool.Domain, domainPool.Port)

	resp, err := domainCheckHTTPClient.Get(url)
	if err != nil {
		s.domainPoolRepo.UpdateStatus(ctx, id, 2)
		s.domainPoolRepo.UpdateLastCheck(ctx, id, time.Now())
		return false, nil
	}
	defer resp.Body.Close()

	s.domainPoolRepo.UpdateStatus(ctx, id, 1)
	s.domainPoolRepo.UpdateLastCheck(ctx, id, time.Now())

	return true, nil
}

func (s *domainPoolService) CheckAllDomains(ctx context.Context) ([]dto.DomainPoolCheckResponse, error) {
	domainPools, _, err := s.domainPoolRepo.List(ctx, 1, 1000, "", 0)
	if err != nil {
		return nil, err
	}
	if len(domainPools) == 0 {
		return nil, nil
	}

	results := make([]dto.DomainPoolCheckResponse, len(domainPools))
	var wg sync.WaitGroup
	sem := make(chan struct{}, domainCheckConcurrency)
	for i, dp := range domainPools {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, dp *model.DomainPool) {
			defer wg.Done()
			defer func() { <-sem }()

			url := fmt.Sprintf("http://%s:%d", dp.Domain, dp.Port)

			status := 2
			msg := "不可访问"

			resp, err := domainCheckHTTPClient.Get(url)
			if err != nil {
				msg = fmt.Sprintf("连接错误: %s", err.Error())
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode < 400 {
					status = 1
					msg = "可访问"
				} else {
					msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
				}
			}

			s.domainPoolRepo.UpdateStatus(ctx, dp.ID, status)
			s.domainPoolRepo.UpdateLastCheck(ctx, dp.ID, time.Now())

			results[i] = dto.DomainPoolCheckResponse{
				ID:     dp.ID,
				Status: status,
				Msg:    msg,
			}
		}(i, dp)
	}
	wg.Wait()
	return results, nil
}

const domainCheckConcurrency = 16

// domainCheckHTTPClient 域名健康检查共享 HTTP 客户端：
// 复用连接池避免每次检查新建 client（高频检查下耗尽临时端口）。
var domainCheckHTTPClient = &http.Client{Timeout: 5 * time.Second}

func (s *domainPoolService) AddBlacklist(ctx context.Context, domain, platform, reason, source string, ttlHours int) error {
	var expiresAt *time.Time
	if ttlHours > 0 {
		t := time.Now().Add(time.Duration(ttlHours) * time.Hour)
		expiresAt = &t
	}
	return s.blacklistRepo.Add(ctx, domain, platform, reason, source, expiresAt)
}

func (s *domainPoolService) RemoveBlacklist(ctx context.Context, domain string) error {
	return s.blacklistRepo.Remove(ctx, domain)
}

func (s *domainPoolService) ListBlacklist(ctx context.Context, page, pageSize int) ([]*model.DomainBlacklist, int64, error) {
	return s.blacklistRepo.List(ctx, page, pageSize)
}

func (s *domainPoolService) IsBlacklisted(ctx context.Context, domain string) (bool, error) {
	ok, _, err := s.blacklistRepo.IsBlacklisted(ctx, domain)
	return ok, err
}

func (s *domainPoolService) SuspendDomain(ctx context.Context, id int) (*model.DomainPool, error) {
	dp, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	dp.Status = 4
	if err := s.domainPoolRepo.Update(ctx, dp); err != nil {
		return nil, err
	}
	return dp, nil
}

func (s *domainPoolService) RotateToBackup(ctx context.Context, id int) (*model.DomainPool, error) {
	dp, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if dp.Status == 4 {
		return nil, fmt.Errorf("域名已停用，无法轮换激活")
	}
	dp.IsActive = true
	dp.Status = 1
	if err := s.domainPoolRepo.Update(ctx, dp); err != nil {
		return nil, err
	}
	return dp, nil
}

func (s *domainPoolService) ListAlerts(ctx context.Context) ([]*model.DomainPool, error) {
	list, _, err := s.domainPoolRepo.List(ctx, 1, 200, "", 0)
	if err != nil {
		return nil, err
	}
	var alerts []*model.DomainPool
	for _, dp := range list {
		if dp.Status != 1 || dp.ConsecutiveFailures > 0 {
			alerts = append(alerts, dp)
		}
	}
	return alerts, nil
}

func (s *domainPoolService) ResolveAlert(ctx context.Context, id int) (*model.DomainPool, error) {
	dp, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	ok, err := s.CheckDomain(ctx, id)
	if err != nil {
		return nil, err
	}
	if ok {
		dp.ConsecutiveFailures = 0
		dp.Status = 1
		if err := s.domainPoolRepo.Update(ctx, dp); err != nil {
			return nil, err
		}
		return dp, nil
	}
	return dp, fmt.Errorf("域名仍不健康，告警保持")
}
