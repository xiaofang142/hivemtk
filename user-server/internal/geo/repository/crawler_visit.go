package repository

import (
	"context"
	"net/url"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

// GeoCrawlerVisitRepository 爬虫访问仓储
type GeoCrawlerVisitRepository interface {
	Create(ctx context.Context, v *model.GeoCrawlerVisit) error
	BulkCreate(ctx context.Context, vs []*model.GeoCrawlerVisit) error
	StatsByEngine(ctx context.Context, days int) (map[string]int64, error)
	// StatsByDomain 按域名 + 引擎聚合（近 days 天）
	StatsByDomain(ctx context.Context, days int) ([]DomainStatRow, error)
	// StatsByKeyword 按关键词 + 引擎聚合 —— 核心：每个关键词被多少 AI Bot 引擎爬过
	StatsByKeyword(ctx context.Context, days int) ([]KeywordStatRow, error)
	// TotalVisits 近 days 天总访问数
	TotalVisits(ctx context.Context, days int) (int64, error)
	// ActiveDomains 近 days 天活跃域名数
	ActiveDomains(ctx context.Context, days int) (int64, error)
	// ActiveKeywords 近 days 天活跃关键词数
	ActiveKeywords(ctx context.Context, days int) (int64, error)
	// ActiveEngines 近 days 天活跃 AI Bot 引擎数
	ActiveEngines(ctx context.Context, days int) (int64, error)
	// Clean 删除全部历史数据（爬虫整轮重置）
	Clean(ctx context.Context) error
}

// DomainStatRow 爬虫访问按站点聚合行
//
// Domain 是"站点标识"而非纯 host：自家官网在 GitHub Pages 项目页形态下取
// host+/路径，其余站点取 host（见 siteKeyOf）。
type DomainStatRow struct {
	Domain      string `json:"domain"`
	Engine      string `json:"engine"`
	VisitCount  int64  `json:"visit_count"`
	SourceLevel string `json:"source_level"`
	IsSelfSite  bool   `json:"is_self_site"`
}

// KeywordStatRow 爬虫访问按关键词聚合行 —— AI Bot 对某关键词的搜索频次
type KeywordStatRow struct {
	Keyword    string `json:"keyword"`
	Engine     string `json:"engine"`
	VisitCount int64  `json:"visit_count"`
}

// domainSourceLevel 第三方站点的源等级。自家站刻意不在表里：
// 官网基址是运行期配置（GEO_SITE_BASE_URL），写死进静态表就会随域名搬家而失效。
var domainSourceLevel = map[string]string{
	"weibanzhushou.com": "B",
	"tanmascrm.com":     "C",
	"fengchenscrm.com":  "C",
	"hubspot.com":       "A",
	"producthunt.com":   "A",
	"techcrunch.com":    "A",
	"intercom.com":      "A",
	"baidu.com":         "A",
	"google.com":        "A",
	"bing.com":          "A",
}

func sourceLevelOf(site string, isSelfSite bool) string {
	if isSelfSite {
		return "A"
	}
	if lv, ok := domainSourceLevel[site]; ok {
		return lv
	}
	return "D"
}

func stripScheme(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		return raw[i+len("://"):]
	}
	return raw
}

func extractDomain(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// siteKeyOf 把一条访问 URL 归一成"站点标识"，并给出它是否属于自家官网。
//
// 自家站取基址的 host+路径（如 xiaofang142.github.io/hivemtk），其余取 host。
// 路径不可省：GitHub Pages 项目页形态下 host 是无数项目共用的，
// 只按 host 聚合会把别人的项目页和自家站折成同一行，可见度统计就此失真。
func siteKeyOf(rawURL string) (site string, isSelfSite bool) {
	if rawURL == "" {
		return "", false
	}
	if config.IsSelfSiteURL(rawURL) {
		return stripScheme(config.WebsiteBaseURL()), true
	}
	return extractDomain(rawURL), false
}

type geoCrawlerVisitRepo struct{ db *gorm.DB }

func NewGeoCrawlerVisitRepository(db *gorm.DB) GeoCrawlerVisitRepository {
	return &geoCrawlerVisitRepo{db: db}
}

// NewGeoCrawlerVisitRepository 默认使用全局 DB（cron / controller 调用）
func NewGeoCrawlerVisitRepositoryDefault() GeoCrawlerVisitRepository {
	return &geoCrawlerVisitRepo{db: db.GetDB()}
}

func (r *geoCrawlerVisitRepo) Create(ctx context.Context, v *model.GeoCrawlerVisit) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *geoCrawlerVisitRepo) BulkCreate(ctx context.Context, vs []*model.GeoCrawlerVisit) error {
	if len(vs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(vs, 200).Error
}

func (r *geoCrawlerVisitRepo) StatsByEngine(ctx context.Context, days int) (map[string]int64, error) {
	since := time.Now().AddDate(0, 0, -days)
	type row struct {
		Engine string
		N      int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Select("engine, COUNT(*) as n").
		Where("created_at >= ?", since).
		Group("engine").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Engine] = r.N
	}
	return out, nil
}

func (r *geoCrawlerVisitRepo) StatsByDomain(ctx context.Context, days int) ([]DomainStatRow, error) {
	since := time.Now().AddDate(0, 0, -days)

	type rawRow struct {
		Path   string
		Engine string
		N      int64
	}
	var raw []rawRow
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Select("path, engine, COUNT(*) as n").
		Where("created_at >= ?", since).
		Group("path, engine").Find(&raw).Error
	if err != nil {
		return nil, err
	}

	type key struct{ Site, Engine string }
	bucket := map[key]int64{}
	selfSite := map[string]bool{}
	for _, row := range raw {
		site, isSelf := siteKeyOf(row.Path)
		if site == "" {
			continue
		}
		bucket[key{site, row.Engine}] += row.N
		selfSite[site] = isSelf
	}

	out := make([]DomainStatRow, 0, len(bucket))
	for k, n := range bucket {
		out = append(out, DomainStatRow{
			Domain:      k.Site,
			Engine:      k.Engine,
			VisitCount:  n,
			SourceLevel: sourceLevelOf(k.Site, selfSite[k.Site]),
			IsSelfSite:  selfSite[k.Site],
		})
	}
	return out, nil
}

func (r *geoCrawlerVisitRepo) StatsByKeyword(ctx context.Context, days int) ([]KeywordStatRow, error) {
	since := time.Now().AddDate(0, 0, -days)
	type row struct {
		Keyword string
		Engine  string
		N       int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Select("keyword, engine, COUNT(*) as n").
		Where("created_at >= ? AND keyword != ''", since).
		Group("keyword, engine").
		Order("n DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]KeywordStatRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, KeywordStatRow{
			Keyword:    row.Keyword,
			Engine:     row.Engine,
			VisitCount: row.N,
		})
	}
	return out, nil
}

func (r *geoCrawlerVisitRepo) TotalVisits(ctx context.Context, days int) (int64, error) {
	since := time.Now().AddDate(0, 0, -days)
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Where("created_at >= ?", since).Count(&n).Error
	return n, err
}

func (r *geoCrawlerVisitRepo) ActiveDomains(ctx context.Context, days int) (int64, error) {
	var paths []string
	since := time.Now().AddDate(0, 0, -days)
	if err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Distinct("path").Where("created_at >= ?", since).Pluck("path", &paths).Error; err != nil {
		return 0, err
	}
	sites := map[string]bool{}
	for _, p := range paths {
		if s, _ := siteKeyOf(p); s != "" {
			sites[s] = true
		}
	}
	return int64(len(sites)), nil
}

func (r *geoCrawlerVisitRepo) ActiveKeywords(ctx context.Context, days int) (int64, error) {
	since := time.Now().AddDate(0, 0, -days)
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Where("created_at >= ? AND keyword != ''", since).
		Distinct("keyword").Count(&n).Error
	return n, err
}

func (r *geoCrawlerVisitRepo) ActiveEngines(ctx context.Context, days int) (int64, error) {
	since := time.Now().AddDate(0, 0, -days)
	var n int64
	err := r.db.WithContext(ctx).Model(&model.GeoCrawlerVisit{}).
		Where("created_at >= ?", since).
		Distinct("engine").Count(&n).Error
	return n, err
}

func (r *geoCrawlerVisitRepo) Clean(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("1=1").Delete(&model.GeoCrawlerVisit{}).Error
}
