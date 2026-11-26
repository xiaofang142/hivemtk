package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
)

type crawlerSourceTransport func(*http.Request) (*http.Response, error)

func (f crawlerSourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func crawlerSourceJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// 主动请求只证明所带 UA 可取得 HTTP 成功响应，来源必须随记录持久化。
func TestCrawlerSource_ActiveUAProbe(t *testing.T) {
	svc := &MonitorCrawlerService{httpClient: &http.Client{Transport: crawlerSourceTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != "GPTBot/1.1" {
			t.Fatalf("主动请求没有携带配置 UA: %v", r.Header)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}}
	visit := svc.doCrawl(context.Background(), "https://site.example/page", "标签关键词", "GPTBot/1.1")
	if visit == nil {
		t.Fatal("HTTP 200 应记录主动 UA 探测成功")
	}
	if got := crawlerSourceJSON(t, visit)["source"]; got != "active_ua_probe" {
		t.Fatalf("主动请求必须标记 active_ua_probe，实际 %v", got)
	}
}

// 只凭请求 UA 识别到的匿名访问不能标成已经验证的外部引擎抓取。
func TestCrawlerSource_AnonymousUAObservation(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	repo := &fakeCrawlerVisitRepo{}
	if err := NewCrawlerVisitService(repo).RecordAnonymous(context.Background(), "GPTBot", "/page", "GPTBot/1.1", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if got := crawlerSourceJSON(t, repo.created[0])["source"]; got != "ua_observation" {
		t.Fatalf("未验证身份的 UA 观察必须单独标记，实际 %v", got)
	}
}

// 21 个 UA 可以归到 8 组；元信息不能拿 UA 条目数当作引擎数，更不能声称真实抓取或收录。
func TestCrawlerSource_ReportContract(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 901)
	uas := make([]string, 0, 21)
	for i := 0; i < 21; i++ {
		uas = append(uas, aiBotUserAgents[i%len(aiBotUserAgents)]+" variant")
	}
	encoded, _ := json.Marshal(uas)
	cacheKey := dictCacheKey(ctx, model.DictCategoryCrawlerUAs, "list")
	dictCache.Store(cacheKey, string(encoded))
	t.Cleanup(func() { dictCache.Delete(cacheKey) })
	svc := &GeoDecisionAnalyticsService{crawler: stubCrawlerRepo{}}
	report, err := svc.GetCrawlerStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := crawlerSourceJSON(t, report)
	meta, ok := data["data_source"].(map[string]any)
	if !ok || meta["kind"] != "active_ua_probe" || meta["external_crawl_verified"] != false || meta["indexing_verified"] != false {
		t.Fatalf("报表缺少主动探测来源与证据边界: %v", data["data_source"])
	}
	if meta["configured_ua_count"] != float64(21) || meta["configured_engine_groups"] != float64(8) || meta["unattributed_records"] != "excluded" {
		t.Fatalf("UA 数、分组数或旧记录排除口径不正确: %v", meta)
	}
	rows, _, _, _ := computeDomainCompare([]repository.DomainStatRow{{Domain: "site.example", VisitCount: 2, IsSelfSite: true}})
	row := crawlerSourceJSON(t, rows[0])
	if row["is_self_site"] != true || row["probe_count"] != float64(2) || row["probe_share_pct"] != float64(100) {
		t.Fatalf("必须输出规范的当前站点与探测字段: %v", row)
	}
}

type crawlerSourceErrorRepo struct{ stubCrawlerRepo }

func (crawlerSourceErrorRepo) StatsByKeyword(context.Context, int) ([]repository.KeywordStatRow, error) {
	return nil, errors.New("read failed")
}

// 读库失败不能伪装成零探测成功响应。
func TestCrawlerSource_ReportReadFailure(t *testing.T) {
	svc := &GeoDecisionAnalyticsService{crawler: crawlerSourceErrorRepo{}}
	if _, err := svc.GetCrawlerStats(tenant.ContextWithDefault(context.Background())); err == nil {
		t.Fatal("读取探测失败应返回错误，不应返回全零报表")
	}
}

// 缺租户必须在任何主动请求/字典回退之前拒绝，避免失败后仍向外探测。
func TestCrawlerSource_MissingTenantFailsClosed(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	svc := &GeoDecisionAnalyticsService{crawler: stubCrawlerRepo{}}
	if _, err := svc.GetCrawlerStats(context.Background()); err == nil {
		t.Fatal("多租户报表缺租户必须拒绝")
	}
	monitor := &MonitorCrawlerService{}
	if _, err := monitor.RunCrawlerCron(context.Background()); err == nil {
		t.Fatal("多租户主动探测缺租户必须在发请求前拒绝")
	}
}
