package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
)

const (
	PlatformJuejin = "juejin"
	PlatformXhs    = "xiaohongshu"
	PlatformZhihu  = "zhihu"
	PlatformCSDN   = "csdn"
	PlatformWechat = "wechat"
	PlatformMedium = "medium"
	PlatformDevTo  = "devto"

	// 平台分类：有 API / 需 Headless
	PlatformAPI     = "api"
	PlatformBrowser = "browser"
)

// PlatformMeta 平台元信息
type PlatformMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	MaxTitleLen int    `json:"max_title_len"`
	MaxBodyLen  int    `json:"max_body_len"`
	NeedLogin   bool   `json:"need_login"`
}

// defaultPlatformMetas 内置平台元信息表（DB 为空/故障时兜底）
func defaultPlatformMetas() map[string]PlatformMeta {
	return map[string]PlatformMeta{
		PlatformJuejin: {Name: PlatformJuejin, DisplayName: "掘金", Category: PlatformBrowser, MaxTitleLen: 30, MaxBodyLen: 15000, NeedLogin: true},
		PlatformXhs:    {Name: PlatformXhs, DisplayName: "小红书", Category: PlatformBrowser, MaxTitleLen: 20, MaxBodyLen: 1000, NeedLogin: true},
		PlatformZhihu:  {Name: PlatformZhihu, DisplayName: "知乎", Category: PlatformBrowser, MaxTitleLen: 30, MaxBodyLen: 20000, NeedLogin: true},
		PlatformCSDN:   {Name: PlatformCSDN, DisplayName: "CSDN", Category: PlatformBrowser, MaxTitleLen: 30, MaxBodyLen: 30000, NeedLogin: true},
		PlatformMedium: {Name: PlatformMedium, DisplayName: "Medium", Category: PlatformAPI, MaxTitleLen: 100, MaxBodyLen: 100000},
		PlatformDevTo:  {Name: PlatformDevTo, DisplayName: "DEV Community", Category: PlatformAPI, MaxTitleLen: 100, MaxBodyLen: 100000},
	}
}

// platformMetasDefaultJSON 缺省平台元信息 JSON（DB 为空/故障时兜底）
var platformMetasDefaultOnce sync.Once
var platformMetasDefaultJSON string

func defaultPlatformMetasJSON() string {
	platformMetasDefaultOnce.Do(func() {
		b, err := json.Marshal(defaultPlatformMetas())
		if err != nil {
			b = []byte("{}")
		}
		platformMetasDefaultJSON = string(b)
	})
	return platformMetasDefaultJSON
}

// PlatformMetas 返回所有平台的元信息表：
// DB 优先（geo_dicts/platform_metas.table），缺行自动播种；
// DB 故障或空表时 fail-open 回内置表。
func PlatformMetas() map[string]PlatformMeta {
	var m map[string]PlatformMeta
	if err := DictJSON(model.DictCategoryPlatformMetas, "table", defaultPlatformMetasJSON(), &m); err != nil || len(m) == 0 {
		return defaultPlatformMetas()
	}
	return m
}

// AdaptedContent 适配某平台后的内容
type AdaptedContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// ContentAdapter 将通用文章适配成某平台的格式
type ContentAdapter func(article *model.GeoArticle, platform string) AdaptedContent

// DefaultContentAdapter 默认适配器：按平台限制截断标题、保留正文 Markdown
func DefaultContentAdapter(article *model.GeoArticle, platform string) AdaptedContent {
	meta := PlatformMetas()[platform]
	title := article.Title
	body := article.Content
	if meta.MaxTitleLen > 0 && len([]rune(title)) > meta.MaxTitleLen {
		title = string([]rune(title)[:meta.MaxTitleLen]) + "…"
	}
	if meta.MaxBodyLen > 0 && len([]rune(body)) > meta.MaxBodyLen {
		body = string([]rune(body)[:meta.MaxBodyLen]) + "\n\n…（内容已截断）"
	}

	if platform == PlatformXhs {
		title = "📝 " + strings.TrimPrefix(title, "#")
		body = strings.ReplaceAll(body, "## ", "")
		body = strings.ReplaceAll(body, "# ", "")
	}
	return AdaptedContent{Title: strings.TrimSpace(title), Body: strings.TrimSpace(body)}
}

// PlatformPublisher 单平台发布器（API 平台实现接口；Browser 平台返回 "需要 headless 登录"）
type PlatformPublisher interface {
	Publish(ctx context.Context, account *model.GeoPlatformAccount, content AdaptedContent) (url string, err error)
	PlatformName() string
}

// PublishPipeline 发布管线：聚合 PlatformPublisher + Worker Pool
type PublishPipeline struct {
	publishers        map[string]PlatformPublisher
	adapters          []ContentAdapter
	articleRepo       repository.GeoArticleRepository
	accountRepo       repository.GeoPlatformAccountRepository
	publishRecordRepo repository.GeoPublishRecordRepository
	workerCount       int
}

// NewPublishPipeline 创建 PublishPipeline
// publishers 外部注入（可用 NewDefaultPublishers() 装配 headless/api stub 集）
func NewPublishPipeline(
	publishers []PlatformPublisher,
	articleRepo repository.GeoArticleRepository,
	accountRepo repository.GeoPlatformAccountRepository,
	publishRecordRepo repository.GeoPublishRecordRepository,
) *PublishPipeline {
	m := map[string]PlatformPublisher{}
	for _, p := range publishers {
		m[p.PlatformName()] = p
	}
	return &PublishPipeline{
		publishers:        m,
		adapters:          []ContentAdapter{DefaultContentAdapter},
		articleRepo:       articleRepo,
		accountRepo:       accountRepo,
		publishRecordRepo: publishRecordRepo,
		workerCount:       3,
	}
}

// SetWorkerCount 覆盖默认 worker 数量（默认 3）
func (p *PublishPipeline) SetWorkerCount(n int) {
	if n > 0 {
		p.workerCount = n
	}
}

// PublishRequest 单条发布请求
type PublishRequest struct {
	ArticleID string   `json:"article_id"`
	Platforms []string `json:"platforms"`
}

// PublishResult 单条平台发布结果
type PublishResult struct {
	Platform    string    `json:"platform"`
	AccountID   string    `json:"account_id,omitempty"`
	Status      string    `json:"status"`
	URL         string    `json:"url,omitempty"`
	Simulated   bool      `json:"simulated"`
	Message     string    `json:"message,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}

// Publish 并发发布一篇文章到多个平台
// 使用 worker pool + PublishRecord 持久化，单平台失败不阻断其它平台
func (p *PublishPipeline) Publish(ctx context.Context, req PublishRequest) ([]PublishResult, error) {
	article, err := p.articleRepo.GetByID(req.ArticleID)
	if err != nil {
		return nil, fmt.Errorf("article %s: %w", req.ArticleID, err)
	}

	platforms := req.Platforms
	if len(platforms) == 0 {
		for name := range p.publishers {
			platforms = append(platforms, name)
		}
	}

	type job struct {
		platform string
	}
	jobs := make(chan job)
	results := make(chan PublishResult, len(platforms))

	var wg sync.WaitGroup
	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- p.publishOne(ctx, article, j.platform)
			}
		}()
	}
	for _, pl := range platforms {
		jobs <- job{platform: pl}
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]PublishResult, 0, len(platforms))
	for r := range results {
		out = append(out, r)
	}
	return out, nil
}

func (p *PublishPipeline) publishOne(ctx context.Context, article *model.GeoArticle, platform string) PublishResult {
	pr, ok := p.publishers[platform]
	if !ok {
		return PublishResult{Platform: platform, Status: "skipped", Message: "no publisher registered"}
	}
	adapter := p.adapters[0]
	content := adapter(article, platform)

	account, err := p.accountRepo.GetLatestByPlatform(platform)
	if err != nil {
		return PublishResult{Platform: platform, Status: "failed", Message: fmt.Sprintf("account: %v", err)}
	}

	url, pubErr := pr.Publish(ctx, account, content)
	now := time.Now()

	rec := &model.GeoPublishRecord{
		ArticleID:    article.ID,
		Platform:     platform,
		AccountID:    account.ID,
		Status:       "success",
		PublishedURL: url,
		PublishedAt:  now,
	}
	simulated := strings.HasPrefix(url, "mock://") || pubErr == nil && url == ""
	if pubErr != nil {
		rec.Status = "failed"
	} else if simulated {
		rec.Status = "pending"
	}
	_ = p.publishRecordRepo.Create(rec)

	if pubErr != nil {
		return PublishResult{Platform: platform, AccountID: account.ID, Status: "failed", Message: pubErr.Error()}
	}
	return PublishResult{Platform: platform, AccountID: account.ID, Status: rec.Status, URL: url, Simulated: simulated, PublishedAt: now}
}

// GetPublishRecords 查询某文章/平台的发布历史（透传）
func (p *PublishPipeline) GetPublishRecords(articleID, platform string, page, limit int) ([]*model.GeoPublishRecord, int64, error) {
	return p.publishRecordRepo.GetList(articleID, platform, page, limit)
}

// ListRegisteredPlatforms 返回已注册的发布器平台名
func (p *PublishPipeline) ListRegisteredPlatforms() []string {
	names := make([]string, 0, len(p.publishers))
	for k := range p.publishers {
		names = append(names, k)
	}
	return names
}
