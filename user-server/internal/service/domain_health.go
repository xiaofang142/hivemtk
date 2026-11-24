package service

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DomainHealthService 域名健康度服务
// G 域 ：定时探测（DNS / HTTP GET / 平台黑名单）+ 健康度评分 + 自动切换
//
// 设计要点：
//   - 探测与评分完全同步执行（不引入后台 goroutine），由调用方触发；
//   - 自动切换遵循"评分 < 阈值 且 连续失败次数 >= N"才切换，避免抖动；
//   - 切换目标：评分最高且未在黑名单的可用域名（ListAvailable）；
//   - 所有操作都会写入 DomainHealthLog，便于事后追查；
//   - 私域独立部署：无 merchant_id 字段。
type DomainHealthService interface {
	CheckOne(ctx context.Context, domainID int) (*HealthCheckResult, error)
	CheckAll(ctx context.Context) ([]*HealthCheckResult, error)
	SwitchActive(ctx context.Context, domainID int, reason string) error
	SwitchToBest(ctx context.Context, reason string) (*model.DomainPool, error)
	GetActiveDomain(ctx context.Context) (*model.DomainPool, error)
	ListAvailable(ctx context.Context, minScore int) ([]*model.DomainPool, error)
	ListHealthLogs(ctx context.Context, domainID int, limit int) ([]*model.DomainHealthLog, error)
	ProbeOnce(ctx context.Context, domain string) (*HealthCheckResult, error)
}

// HealthCheckResult 一次健康度探测的完整结果
type HealthCheckResult struct {
	DomainID     int       `json:"domain_id"`
	Domain       string    `json:"domain"`
	Port         int       `json:"port"`
	CheckedAt    time.Time `json:"checked_at"`
	DNSOK        bool      `json:"dns_ok"`
	DNSError     string    `json:"dns_error,omitempty"`
	HTTPOk       bool      `json:"http_ok"`
	HTTPStatus   int       `json:"http_status"`
	HTTPLatency  int       `json:"http_latency_ms"`
	HTTPErrorMsg string    `json:"http_error,omitempty"`
	OnBlacklist  bool      `json:"on_blacklist"`
	BlacklistSrc string    `json:"blacklist_source,omitempty"`
	HealthScore  int       `json:"health_score"`
	ActionTaken  string    `json:"action_taken"`
}

// 评分阈值（导出常量，供测试断言使用）。这四个常量现在只是兜底值：运行时读的是
// health_weights_runtime_params.go 里的 domain_health 组（除 HealthCheckTimeout 在
// http.Client 构造时定型、种子标 Restart=true 外，其余三条每请求读）。
const (
	HealthScoreSwitchThreshold = 30
	HealthScoreHealthy         = 80
	ConsecutiveFailureLimit    = 3
	HealthCheckTimeout         = 5 * time.Second
)

type domainHealthLogRepository interface {
	Create(ctx context.Context, log *model.DomainHealthLog) error
	ListByDomain(ctx context.Context, domainID int, limit int) ([]*model.DomainHealthLog, error)
}

type domainBlacklistRepository interface {
	IsBlacklisted(ctx context.Context, domain string) (bool, *model.DomainBlacklist, error)
}

type domainHealthService struct {
	repo       repository.DomainPoolRepository
	logRepo    domainHealthLogRepository
	blacklistR domainBlacklistRepository
	httpClient *http.Client
	mu         sync.Mutex
}

// NewDomainHealthService 创建域名健康度服务
func NewDomainHealthService(db interface{}, repo repository.DomainPoolRepository) DomainHealthService {
	_ = db
	blRepo := repository.NewDomainBlacklistRepository(nil)
	return &domainHealthService{
		repo:       repo,
		logRepo:    repository.NewDomainHealthLogRepository(nil),
		blacklistR: blRepo,
		httpClient: &http.Client{Timeout: domainHealthCheckTimeout(context.Background())},
	}
}

// NewDomainHealthServiceWithDeps 显式注入 db（测试用）
func NewDomainHealthServiceWithDeps(repo repository.DomainPoolRepository, logRepo *repository.DomainHealthLogRepository, blRepo *repository.DomainBlacklistRepository) DomainHealthService {
	if logRepo == nil {
		logRepo = repository.NewDomainHealthLogRepository(nil)
	}
	if blRepo == nil {
		blRepo = repository.NewDomainBlacklistRepository(nil)
	}
	return &domainHealthService{
		repo:       repo,
		logRepo:    logRepo,
		blacklistR: blRepo,
		httpClient: &http.Client{Timeout: domainHealthCheckTimeout(context.Background())},
	}
}

func (s *domainHealthService) CheckOne(ctx context.Context, domainID int) (*HealthCheckResult, error) {
	dp, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return nil, fmt.Errorf("get domain: %w", err)
	}
	if dp == nil {
		return nil, errors.New("域名不存在")
	}
	return s.checkAndPersist(ctx, dp)
}

func (s *domainHealthService) CheckAll(ctx context.Context) ([]*HealthCheckResult, error) {
	var rows []*model.DomainPool
	for page := 1; ; page++ {
		batch, total, err := s.repo.List(ctx, page, 200, "", 0)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(rows) >= int(total) || len(batch) == 0 {
			break
		}
	}
	results := make([]*HealthCheckResult, 0, len(rows))
	var checkErrors []error
	for _, dp := range rows {
		result, err := s.checkAndPersist(ctx, dp)
		if err != nil {
			checkErrors = append(checkErrors, fmt.Errorf("check domain id=%d: %w", dp.ID, err))
			continue
		}
		results = append(results, result)
	}
	return results, errors.Join(checkErrors...)
}

func (s *domainHealthService) checkAndPersist(ctx context.Context, dp *model.DomainPool) (*HealthCheckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	probe := s.probeDomain(ctx, dp.Domain, dp.Port)
	blacklisted, blEntry, err := s.blacklistR.IsBlacklisted(ctx, dp.Domain)
	if err != nil {
		return nil, fmt.Errorf("check domain blacklist: %w", err)
	}
	blacklistSource := ""
	if blEntry != nil {
		blacklistSource = blEntry.Source + ":" + blEntry.Platform
	}

	score, failures := calcHealthScore(dp.HealthScore, dp.ConsecutiveFailures, probe, blacklisted)
	action := "none"
	newStatus := dp.Status
	if !probe.DNSOK || !probe.HTTPOk || blacklisted || score == 0 {
		newStatus = 2
		action = "mark_unhealthy"
	} else if score >= domainHealthScoreHealthy(ctx) && newStatus != 1 {
		newStatus = 1
	}

	blacklistAt := time.Time{}
	if blacklisted && blEntry != nil && !blEntry.CreatedAt.IsZero() {
		blacklistAt = blEntry.CreatedAt
	}
	blacklistNote := ""
	if blEntry != nil {
		blacklistNote = blEntry.Reason
	}
	if err := s.repo.UpdateHealth(ctx, dp.ID, score, failures, probe.DNSOK, probe.DNSError,
		probe.HTTPStatus, probe.HTTPLatency, blacklisted, blacklistAt, blacklistNote); err != nil {
		return nil, fmt.Errorf("update domain health: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, dp.ID, newStatus); err != nil {
		return nil, fmt.Errorf("update domain status: %w", err)
	}
	logRow := &model.DomainHealthLog{
		DomainID:     dp.ID,
		Domain:       dp.Domain,
		CheckedAt:    probe.CheckedAt,
		DNSOK:        probe.DNSOK,
		DNSError:     probe.DNSError,
		HTTPOk:       probe.HTTPOk,
		HTTPStatus:   probe.HTTPStatus,
		HTTPLatency:  probe.HTTPLatency,
		HTTPErrorMsg: probe.HTTPErrorMsg,
		OnBlacklist:  blacklisted,
		BlacklistSrc: blacklistSource,
		HealthScore:  score,
		ActionTaken:  action,
	}
	if err := s.logRepo.Create(ctx, logRow); err != nil {
		return nil, fmt.Errorf("write domain health log: %w", err)
	}
	if dp.AutoSwitchEnabled && dp.IsActive && (score < domainHealthScoreSwitchThreshold(ctx) || failures >= domainHealthConsecutiveFailureLimit(ctx)) {
		if _, err := s.SwitchToBest(ctx, fmt.Sprintf("健康度自动切换: score=%d failures=%d", score, failures)); err != nil {
			logger.Errorf("自动切换失败: %v", err)
		} else {
			action = "switch_over"
		}
	}
	probe.ActionTaken = action
	probe.HealthScore = score
	probe.DomainID = dp.ID
	return probe, nil
}

func (s *domainHealthService) probeDomain(ctx context.Context, domain string, port int) *HealthCheckResult {
	httpProbe := probeDomainHTTP(ctx, s.httpClient, domain, port)
	probe := &HealthCheckResult{
		Domain:       domain,
		Port:         port,
		CheckedAt:    httpProbe.CheckedAt,
		HTTPOk:       httpProbe.HTTPOk,
		HTTPStatus:   httpProbe.HTTPStatus,
		HTTPLatency:  int(httpProbe.HTTPLatency / time.Millisecond),
		HTTPErrorMsg: httpProbe.HTTPErrorMsg,
	}
	if probe.Port <= 0 {
		probe.Port = 80
	}
	ips, err := net.LookupHost(domain)
	if err != nil {
		probe.DNSError = err.Error()
	} else if len(ips) > 0 {
		probe.DNSOK = true
	} else {
		probe.DNSError = "no such host"
	}
	return probe
}

func calcHealthScore(prevScore, prevFailures int, probe *HealthCheckResult, blacklisted bool) (int, int) {
	if blacklisted {
		return 0, prevFailures + 1
	}
	score := 100
	if !probe.DNSOK {
		score -= 30
	}
	if !probe.HTTPOk {
		score -= 50
	}
	failures := prevFailures
	if !probe.DNSOK || !probe.HTTPOk {
		failures++
		if failures > 10 {
			failures = 10
		}
		score -= failures * 5
	} else {
		failures = 0
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, failures
}

func (s *domainHealthService) ProbeOnce(ctx context.Context, domain string) (*HealthCheckResult, error) {
	res := &HealthCheckResult{Domain: domain, CheckedAt: time.Now()}
	ips, dnsErr := net.LookupHost(domain)
	if dnsErr != nil || len(ips) == 0 {
		if dnsErr != nil {
			res.DNSError = dnsErr.Error()
		} else {
			res.DNSError = "no such host"
		}
	} else {
		res.DNSOK = true
	}
	for _, target := range []struct {
		url  string
		port int
	}{
		{url: "http://" + net.JoinHostPort(domain, "80") + "/", port: 80},
		{url: "https://" + net.JoinHostPort(domain, "443") + "/", port: 443},
	} {
		ok, status, latency, errMsg := s.doHTTPHead(ctx, target.url)
		if ok {
			res.HTTPOk = true
			res.HTTPStatus = status
			res.HTTPLatency = int(latency / time.Millisecond)
			res.Port = target.port
			break
		}
		res.HTTPErrorMsg = errMsg
	}
	res.HealthScore, _ = calcHealthScore(100, 0, res, false)
	return res, nil
}

func (s *domainHealthService) doHTTPHead(ctx context.Context, target string) (bool, int, time.Duration, string) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return false, 0, time.Since(start), err.Error()
	}
	req.Header.Set("User-Agent", "mtk-domain-health/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, 0, time.Since(start), err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	latency := time.Since(start)
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < 400 {
		return true, resp.StatusCode, latency, ""
	}
	return false, resp.StatusCode, latency, "HTTP " + strconv.Itoa(resp.StatusCode)
}

func (s *domainHealthService) SwitchActive(ctx context.Context, domainID int, reason string) error {
	dp, err := s.repo.GetByID(ctx, domainID)
	if err != nil {
		return err
	}
	if dp == nil {
		return errors.New("域名不存在")
	}
	if dp.OnBlacklist {
		return errors.New("域名在黑名单中，禁止切换")
	}
	if dp.HealthScore < domainHealthScoreHealthy(ctx) {
		return fmt.Errorf("域名健康度过低（%d），禁止切换", dp.HealthScore)
	}
	if err := s.repo.DeactivateAll(ctx); err != nil {
		return err
	}
	now := time.Now()
	return s.repo.UpdateActive(ctx, dp.ID, true, &now, 0)
}

func (s *domainHealthService) SwitchToBest(ctx context.Context, reason string) (*model.DomainPool, error) {
	candidates, err := s.repo.ListAvailable(ctx, domainHealthScoreHealthy(ctx))
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errors.New("无可用域名（健康度 >= " + strconv.Itoa(domainHealthScoreHealthy(ctx)) + " 且未在黑名单）")
	}
	best := candidates[0]
	actives, _ := s.repo.ListActive(ctx)
	prevID := 0
	for _, a := range actives {
		prevID = a.ID
		break
	}
	if err := s.repo.DeactivateAll(ctx); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.repo.UpdateActive(ctx, best.ID, true, &now, prevID); err != nil {
		return nil, err
	}
	logger.Infof("[domain-health] 自动切换完成 target=%s reason=%s", best.Domain, reason)
	return best, nil
}

func (s *domainHealthService) GetActiveDomain(ctx context.Context) (*model.DomainPool, error) {
	actives, err := s.repo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	if len(actives) == 0 {
		return nil, nil
	}
	return actives[0], nil
}

func (s *domainHealthService) ListAvailable(ctx context.Context, minScore int) ([]*model.DomainPool, error) {
	if minScore <= 0 {
		minScore = domainHealthScoreHealthy(ctx)
	}
	return s.repo.ListAvailable(ctx, minScore)
}

func (s *domainHealthService) ListHealthLogs(ctx context.Context, domainID int, limit int) ([]*model.DomainHealthLog, error) {
	return s.logRepo.ListByDomain(ctx, domainID, limit)
}
