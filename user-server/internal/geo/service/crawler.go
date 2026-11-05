package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

// CrawlerService AI 引擎爬虫访问追踪 + 信源等级查询
//
// 负责两件事：
//  1. RecordVisit：记录一次爬虫访问，并将信源 URL 关联到 geo_source_catalogs；
//  2. LookupSourceLevel：根据 URL 查询其所属 domain 的信源等级（A/B/C/D）。
//
// 数据访问全部下沉到 Repository，Service 层只做编排和轻量解析。
type CrawlerService struct {
	sourceCatalogRepo repository.GeoSourceCatalogRepository
}

// NewCrawlerService 创建 CrawlerService
func NewCrawlerService(sourceCatalogRepo repository.GeoSourceCatalogRepository) *CrawlerService {
	return &CrawlerService{sourceCatalogRepo: sourceCatalogRepo}
}

// RecordVisit 登记一次信源访问（刷新同步时间戳）
//
// 本条链路只维护 geo_source_catalogs：传入完整 URL，自动提取 domain；
// 条目不存在则新建（level 留空，等人工/规则补齐），存在则只刷新 last_checked
// 与非空字段，不会把已标注的等级清空。访问明细的落库在爬虫追踪链路
// （geo_crawler_visits），不在本方法内。
func (s *CrawlerService) RecordVisit(ctx context.Context, sourceURL string) error {
	sourceURL = strings.TrimSpace(sourceURL)
	u, err := url.Parse(sourceURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("信源 URL 必须是 http(s) 绝对地址: %s", sourceURL)
	}
	domain := ExtractDomain(sourceURL)
	if domain == "" {
		return fmt.Errorf("无法解析信源域名: %s", sourceURL)
	}
	// 真做一次有界 GET：信源同步的“可达”必须有请求支撑，不能只 upsert
	// 一行 Verified=true。safe client 会解析 IP 并拒绝内网目标。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", siteAuditUserAgent)
	req.Header.Set("Range", "bytes=0-2047")
	resp, err := newSiteAuditHTTPClient(10 * time.Second).Do(req)
	if err != nil {
		return fmt.Errorf("信源不可达: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("信源不可达: HTTP %d", resp.StatusCode)
	}

	catalog := &model.GeoSourceCatalog{
		SourceURL:   sourceURL,
		Domain:      domain,
		Verified:    true,
		LastChecked: time.Now(),
	}
	return s.sourceCatalogRepo.Upsert(ctx, catalog)
}

// LookupSourceLevel 查询指定 URL 所属 domain 的信源等级
//
// 返回信源等级（A/B/C/D 或空串表示未登记），以及完整的 SourceCatalog 条目。
// 若 sourceURL 未登记，则回退到同 domain 下的任意已验证条目。
func (s *CrawlerService) LookupSourceLevel(ctx context.Context, sourceURL string) (string, *model.GeoSourceCatalog, error) {

	item, err := s.sourceCatalogRepo.GetByURL(ctx, sourceURL)
	if err == nil && item != nil && item.Level != "" {
		return item.Level, item, nil
	}

	domain := ExtractDomain(sourceURL)
	if domain == "" {
		return "", item, nil
	}
	items, err := s.sourceCatalogRepo.FindByDomain(ctx, domain)
	if err != nil || len(items) == 0 {
		return "", nil, err
	}

	best := items[0]
	for _, it := range items[1:] {
		if it.Level != "" && (best.Level == "" || it.Level < best.Level) {
			best = it
		}
	}
	return best.Level, best, nil
}

// ExtractDomain 从 URL 中提取 host（去掉 http(s):// 前缀和路径）
//
// 简单字符串切片，避免引入 net/url 的严格解析（需要合法 scheme）：
//
//	https://www.example.com/path → www.example.com
//	example.com                   → example.com
func ExtractDomain(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}

	if idx := strings.Index(u, "://"); idx >= 0 {
		u = u[idx+3:]
	}

	if idx := strings.Index(u, ":"); idx >= 0 {
		u = u[:idx]
	}

	for _, sep := range []string{"/", "?", "#"} {
		if idx := strings.Index(u, sep); idx >= 0 {
			u = u[:idx]
		}
	}
	return strings.TrimSpace(u)
}
