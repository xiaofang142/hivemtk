package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
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
}

// NewDomainPoolService 创建域名池服务实例
func NewDomainPoolService(domainPoolRepo repository.DomainPoolRepository, blacklistRepo *repository.DomainBlacklistRepository) DomainPoolService {
	return &domainPoolService{
		domainPoolRepo: domainPoolRepo,
		blacklistRepo:  blacklistRepo,
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

	// 零值不覆盖：PUT 是整表单提交，但前端可能不带 port / purpose / status。
	// status 是健康状态（1 可用 / 2 不可访问 …），被零值覆盖会把域名直接打成「禁用」。
	if port <= 0 {
		port = domainPool.Port
		if port <= 0 {
			port = 80
		}
	}
	if purpose == "" {
		purpose = domainPool.Purpose
	}
	if status == 0 {
		status = domainPool.Status
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
	return s.domainPoolRepo.Delete(ctx, id)
}

func (s *domainPoolService) GetByID(ctx context.Context, id int) (*model.DomainPool, error) {
	return s.domainPoolRepo.GetByID(ctx, id)
}

func (s *domainPoolService) List(ctx context.Context, page, pageSize int, domain string, status int) ([]*model.DomainPool, int64, error) {
	return s.domainPoolRepo.List(ctx, page, pageSize, domain, status)
}

func (s *domainPoolService) CheckDomain(ctx context.Context, id int) (bool, error) {
	domainPool, err := s.domainPoolRepo.GetByID(ctx, id)
	if err != nil {
		return false, err
	}

	probe := probeDomainHTTP(ctx, domainCheckHTTPClient, domainPool.Domain, domainPool.Port)
	status := 2
	if probe.HTTPOk {
		status = 1
	}
	writeErr := errors.Join(
		s.domainPoolRepo.UpdateStatus(ctx, id, status),
		s.domainPoolRepo.UpdateLastCheck(ctx, id, probe.CheckedAt),
	)
	if writeErr != nil {
		return false, fmt.Errorf("persist domain check: %w", writeErr)
	}
	return probe.HTTPOk, nil
}

func (s *domainPoolService) CheckAllDomains(ctx context.Context) ([]dto.DomainPoolCheckResponse, error) {
	var domainPools []*model.DomainPool
	for page := 1; ; page++ {
		rows, total, err := s.domainPoolRepo.List(ctx, page, 200, "", 0)
		if err != nil {
			return nil, err
		}
		domainPools = append(domainPools, rows...)
		if len(domainPools) >= int(total) || len(rows) == 0 {
			break
		}
	}
	if len(domainPools) == 0 {
		return nil, nil
	}

	results := make([]dto.DomainPoolCheckResponse, len(domainPools))
	checkErrors := make([]error, len(domainPools))
	var wg sync.WaitGroup
	sem := make(chan struct{}, domainCheckConcurrency())
	for i, dp := range domainPools {
		wg.Add(1)
		sem <- struct{}{}
		// utils.SafeGo 收口（Go 1.22+ 循环变量逐迭代语义下闭包捕获与原参数传递等价）：
		// 域名连通性探测（HTTP）panic 不击穿进程，该结果槽零值由 wg.Wait 后既有汇总兜底
		utils.SafeGo(ctx, "domainpool.check", func(ctx context.Context) {
			defer wg.Done()
			defer func() { <-sem }()

			probe := probeDomainHTTP(ctx, domainCheckHTTPClient, dp.Domain, dp.Port)
			status := 2
			msg := probe.HTTPErrorMsg
			if probe.HTTPOk {
				status = 1
				msg = "可访问"
			}
			if writeErr := errors.Join(
				s.domainPoolRepo.UpdateStatus(ctx, dp.ID, status),
				s.domainPoolRepo.UpdateLastCheck(ctx, dp.ID, probe.CheckedAt),
			); writeErr != nil {
				checkErrors[i] = fmt.Errorf("persist domain check id=%d: %w", dp.ID, writeErr)
			}

			results[i] = dto.DomainPoolCheckResponse{
				ID:     dp.ID,
				Status: status,
				Msg:    msg,
			}
		})
	}
	wg.Wait()
	return results, errors.Join(checkErrors...)
}

// defaultDomainCheckConcurrency 域名批量检测的默认并发，也是参数中心
// `misc.domain_check_concurrency` 的代码兜底值（两者必须一致）。
const defaultDomainCheckConcurrency = 16

// domainCheckConcurrencyProvider 由装配层注入；未注入时回落默认值。
var domainCheckConcurrencyProvider = func() int { return defaultDomainCheckConcurrency }

// SetDomainCheckConcurrencyProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetDomainCheckConcurrencyProvider(fn func() int) {
	if fn != nil {
		domainCheckConcurrencyProvider = fn
	}
}

// domainCheckConcurrency 取当前并发值。
//
// 非正数一律回落默认值：make(chan struct{}, 0) 是一条**永不有人通过的**信号量，
// CheckAllDomains 会整批挂死在 wg.Wait()，参数填错等于把这个接口打成超时。
func domainCheckConcurrency() int {
	if n := domainCheckConcurrencyProvider(); n > 0 {
		return n
	}
	return defaultDomainCheckConcurrency
}

// domainCheckHTTPClient 域名健康检查共享 HTTP 客户端：
// 复用连接池避免每次检查新建 client（高频检查下耗尽临时端口）。
var domainCheckHTTPClient = &http.Client{Timeout: 5 * time.Second}

type domainHTTPProbeResult struct {
	HTTPOk       bool
	HTTPStatus   int
	HTTPLatency  time.Duration
	CheckedAt    time.Time
	HTTPErrorMsg string
}

func probeDomainHTTP(ctx context.Context, client *http.Client, domain string, port int) domainHTTPProbeResult {
	if port <= 0 {
		port = 80
	}
	result := domainHTTPProbeResult{CheckedAt: time.Now()}
	start := time.Now()
	address := net.JoinHostPort(domain, strconv.Itoa(port))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/", nil)
	if err != nil {
		result.HTTPErrorMsg = err.Error()
		return result
	}
	req.Header.Set("User-Agent", "mtk-domain-health/1.0")
	resp, err := client.Do(req)
	result.HTTPLatency = time.Since(start)
	if err != nil {
		result.HTTPErrorMsg = "连接错误: " + err.Error()
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	result.HTTPStatus = resp.StatusCode
	result.HTTPOk = resp.StatusCode >= http.StatusOK && resp.StatusCode < 400
	if !result.HTTPOk {
		result.HTTPErrorMsg = "HTTP " + strconv.Itoa(resp.StatusCode)
	}
	return result
}

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
