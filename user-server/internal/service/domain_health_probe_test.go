package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type domainProbeRepositoryFake struct {
	repository.DomainPoolRepository
	mu              sync.Mutex
	rows            []*model.DomainPool
	status          map[int]int
	lastCheck       map[int]time.Time
	listCalls       int
	updateStatusErr error
	lastCheckErr    error
	updateHealthErr error
	healthUpdates   int
	availableCalls  int
}

func (r *domainProbeRepositoryFake) GetByID(_ context.Context, id int) (*model.DomainPool, error) {
	for _, row := range r.rows {
		if row.ID == id {
			copy := *row
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("domain %d not found", id)
}

func (r *domainProbeRepositoryFake) List(_ context.Context, page, pageSize int, _ string, _ int) ([]*model.DomainPool, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(r.rows) {
		return nil, int64(len(r.rows)), nil
	}
	end := start + pageSize
	if end > len(r.rows) {
		end = len(r.rows)
	}
	rows := append([]*model.DomainPool(nil), r.rows[start:end]...)
	return rows, int64(len(r.rows)), nil
}

func (r *domainProbeRepositoryFake) UpdateStatus(_ context.Context, id, status int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateStatusErr != nil {
		return r.updateStatusErr
	}
	if r.status == nil {
		r.status = make(map[int]int)
	}
	r.status[id] = status
	return nil
}

func (r *domainProbeRepositoryFake) UpdateLastCheck(_ context.Context, id int, checkedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastCheckErr != nil {
		return r.lastCheckErr
	}
	if r.lastCheck == nil {
		r.lastCheck = make(map[int]time.Time)
	}
	r.lastCheck[id] = checkedAt
	return nil
}

func (r *domainProbeRepositoryFake) UpdateHealth(context.Context, int, int, int, bool, string, int, int, bool, time.Time, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.healthUpdates++
	return r.updateHealthErr
}

func (r *domainProbeRepositoryFake) ListAvailable(context.Context, int) ([]*model.DomainPool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.availableCalls++
	return nil, errors.New("no available domain")
}

type domainProbeLogFake struct {
	createErr error
	created   int
}

func (r *domainProbeLogFake) Create(context.Context, *model.DomainHealthLog) error {
	r.created++
	return r.createErr
}

func (r *domainProbeLogFake) ListByDomain(context.Context, int, int) ([]*model.DomainHealthLog, error) {
	return nil, nil
}

type domainProbeBlacklistFake struct {
	err error
}

func (r *domainProbeBlacklistFake) IsBlacklisted(context.Context, string) (bool, *model.DomainBlacklist, error) {
	return false, nil, r.err
}

func domainProbeTarget(t *testing.T, status int, handler func(http.ResponseWriter, *http.Request)) (*httptest.Server, int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("解析本地测试端口失败：%v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("转换本地测试端口失败：%v", err)
	}
	return server, port
}

// TestDomainProbe_ManualAndHealthUseConfiguredPort 验证手动和健康度检查都访问配置端口并采用统一状态口径。
func TestDomainProbe_ManualAndHealthUseConfiguredPort(t *testing.T) {
	var manualHits, healthHits atomic.Int32
	_, manualPort := domainProbeTarget(t, http.StatusServiceUnavailable, func(w http.ResponseWriter, r *http.Request) {
		manualHits.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			t.Errorf("手动检查请求应为 GET /，得到 %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	_, healthPort := domainProbeTarget(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		healthHits.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			t.Errorf("健康检查请求应为 GET /，得到 %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	manualRepo := &domainProbeRepositoryFake{rows: []*model.DomainPool{{ID: 1, Domain: "127.0.0.1", Port: manualPort}}}
	manualService := &domainPoolService{domainPoolRepo: manualRepo}
	ok, err := manualService.CheckDomain(context.Background(), 1)
	if err != nil {
		t.Fatalf("手动检查返回错误：%v", err)
	}
	if ok || manualHits.Load() != 1 || manualRepo.status[1] != 2 {
		t.Fatalf("503 应按不可用处理并落 status=2，ok=%v hits=%d status=%d", ok, manualHits.Load(), manualRepo.status[1])
	}

	healthRepo := &domainProbeRepositoryFake{rows: []*model.DomainPool{{ID: 2, Domain: "127.0.0.1", Port: healthPort, Status: 1}}}
	health := &domainHealthService{
		repo:       healthRepo,
		logRepo:    &domainProbeLogFake{},
		blacklistR: &domainProbeBlacklistFake{},
		httpClient: &http.Client{Timeout: time.Second},
	}
	result, err := health.CheckOne(context.Background(), 2)
	if err != nil {
		t.Fatalf("健康度检查返回错误：%v", err)
	}
	if !result.HTTPOk || result.HTTPStatus != http.StatusOK || result.Port != healthPort || healthHits.Load() != 1 {
		t.Fatalf("健康检查应命中配置端口并判定成功，result=%+v hits=%d", result, healthHits.Load())
	}
}

// TestDomainProbe_HTTPStatusRedirectAndTimeout 验证重定向状态、4xx/5xx 和超时判定。
func TestDomainProbe_HTTPStatusRedirectAndTimeout(t *testing.T) {
	redirectTarget, redirectPort := domainProbeTarget(t, http.StatusNoContent, nil)
	_, sourcePort := domainProbeTarget(t, http.StatusFound, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/final", http.StatusFound)
	})
	redirectResult := probeDomainHTTP(context.Background(), &http.Client{Timeout: time.Second}, "127.0.0.1", sourcePort)
	if !redirectResult.HTTPOk || redirectResult.HTTPStatus != http.StatusNoContent {
		t.Fatalf("重定向后的 204 应成功，得到 %+v", redirectResult)
	}
	if redirectPort <= 0 {
		t.Fatalf("测试服务端口无效：%d", redirectPort)
	}

	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		_, port := domainProbeTarget(t, status, nil)
		result := probeDomainHTTP(context.Background(), &http.Client{Timeout: time.Second}, "127.0.0.1", port)
		if result.HTTPOk || result.HTTPStatus != status {
			t.Errorf("HTTP %d 应判不可用，得到 %+v", status, result)
		}
	}

	_, timeoutPort := domainProbeTarget(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	result := probeDomainHTTP(context.Background(), &http.Client{Timeout: 20 * time.Millisecond}, "127.0.0.1", timeoutPort)
	if result.HTTPOk || (!strings.Contains(result.HTTPErrorMsg, "timeout") && !strings.Contains(result.HTTPErrorMsg, "deadline exceeded")) {
		t.Fatalf("超时请求应判不可用并保留错误，得到 %+v", result)
	}
}

// TestDomainProbe_PersistenceErrorsPreventSuccessAndSwitch 验证关键写入失败会返回错误且不会自动切换。
func TestDomainProbe_PersistenceErrorsPreventSuccessAndSwitch(t *testing.T) {
	_, port := domainProbeTarget(t, http.StatusServiceUnavailable, nil)
	writeErr := errors.New("status write failed")
	manualRepo := &domainProbeRepositoryFake{
		rows:            []*model.DomainPool{{ID: 3, Domain: "127.0.0.1", Port: port}},
		updateStatusErr: writeErr,
	}
	manualService := &domainPoolService{domainPoolRepo: manualRepo}
	if _, err := manualService.CheckDomain(context.Background(), 3); !errors.Is(err, writeErr) {
		t.Fatalf("手动检查应返回状态写入错误，得到 %v", err)
	}

	for _, failure := range []string{"health", "status", "log", "blacklist"} {
		repo := &domainProbeRepositoryFake{
			rows: []*model.DomainPool{{ID: 4, Domain: "127.0.0.1", Port: port, Status: 1, IsActive: true, AutoSwitchEnabled: true}},
		}
		logRepo := &domainProbeLogFake{}
		blacklistRepo := &domainProbeBlacklistFake{}
		switch failure {
		case "health":
			repo.updateHealthErr = writeErr
		case "status":
			repo.updateStatusErr = writeErr
		case "log":
			logRepo.createErr = writeErr
		case "blacklist":
			blacklistRepo.err = writeErr
		}
		health := &domainHealthService{
			repo:       repo,
			logRepo:    logRepo,
			blacklistR: blacklistRepo,
			httpClient: &http.Client{Timeout: time.Second},
		}
		if _, err := health.CheckOne(context.Background(), 4); !errors.Is(err, writeErr) {
			t.Errorf("%s 失败应返回原始错误，得到 %v", failure, err)
		}
		if repo.availableCalls != 0 {
			t.Errorf("%s 失败后不得自动切换，ListAvailable 调用=%d", failure, repo.availableCalls)
		}
	}
}

// TestDomainProbe_CheckAllManualPaginatesAndAggregatesWriteErrors 验证批量检查跨页执行并汇总落库错误。
func TestDomainProbe_CheckAllManualPaginatesAndAggregatesWriteErrors(t *testing.T) {
	_, port := domainProbeTarget(t, http.StatusOK, nil)
	rows := make([]*model.DomainPool, 201)
	for i := range rows {
		rows[i] = &model.DomainPool{ID: i + 1, Domain: "127.0.0.1", Port: port}
	}
	writeErr := errors.New("last-check write failed")
	repo := &domainProbeRepositoryFake{rows: rows, lastCheckErr: writeErr}
	service := &domainPoolService{domainPoolRepo: repo}
	results, err := service.CheckAllDomains(context.Background())
	if !errors.Is(err, writeErr) {
		t.Fatalf("批量检查应汇总持久化错误，得到 %v", err)
	}
	if len(results) != len(rows) || repo.listCalls != 2 {
		t.Fatalf("应跨两页检查全部 201 行，结果数=%d 分页数=%d", len(results), repo.listCalls)
	}
}

// TestDomainProbe_CheckAllHealthPaginates 验证健康度检查跨过仓储单页上限继续扫描。
func TestDomainProbe_CheckAllHealthPaginates(t *testing.T) {
	_, port := domainProbeTarget(t, http.StatusOK, nil)
	rows := make([]*model.DomainPool, 201)
	for i := range rows {
		rows[i] = &model.DomainPool{ID: i + 1, Domain: "127.0.0.1", Port: port, Status: 1}
	}
	repo := &domainProbeRepositoryFake{rows: rows}
	health := &domainHealthService{
		repo:       repo,
		logRepo:    &domainProbeLogFake{},
		blacklistR: &domainProbeBlacklistFake{},
		httpClient: &http.Client{Timeout: time.Second},
	}
	results, err := health.CheckAll(context.Background())
	if err != nil {
		t.Fatalf("批量健康度检查返回错误：%v", err)
	}
	if len(results) != len(rows) || repo.listCalls != 2 {
		t.Fatalf("应跨两页检查全部 201 行，结果数=%d 分页数=%d", len(results), repo.listCalls)
	}
}

var _ DomainPoolService = (*domainPoolService)(nil)
