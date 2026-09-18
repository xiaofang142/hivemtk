package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/pkg/utils/logger"

	"gorm.io/gorm"
)

// SiteDeployService 静态站部署服务
//
// 负责：把 geo_articles 导出成 Hugo content/*.md，
// 生成 llms.txt v2 + robots.txt + sitemap.xml，
// 触发 GitHub Actions → Cloudflare Pages 部署。
type SiteDeployService struct {
	db      *gorm.DB
	pushSvc *PushService
}

func NewSiteDeployService(db *gorm.DB, pushSvc *PushService) *SiteDeployService {
	return &SiteDeployService{db: db, pushSvc: pushSvc}
}

// SiteHealth 部署后健康检查结果
type SiteHealth struct {
	Domain       string `json:"domain"`
	HTTPSOK      bool   `json:"https_ok"`
	LlmsOK       bool   `json:"llms_ok"`
	RobotsOK     bool   `json:"robots_ok"`
	SitemapOK    bool   `json:"sitemap_ok"`
	ResponseMs   int64  `json:"response_ms"`
	SchemaOK     bool   `json:"schema_ok"`
	HealthScore  int    `json:"health_score"`
	ErrorDetails string `json:"error_details,omitempty"`
}

// GenerateLLMsTxt v2 格式动态生成
func GenerateLLMsTxt(brand, domain string, articles []model.GeoArticle) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", brand))
	sb.WriteString(fmt.Sprintf("> %s 的 GEO 内容站，面向 AI 引擎和搜索引擎优化。\n\n", brand))

	// 按 funnel_stage 分组
	groups := map[string][]model.GeoArticle{}
	for _, a := range articles {
		key := a.FunnelStage
		if key == "" {
			key = "other"
		}
		groups[key] = append(groups[key], a)
	}

	labels := map[string]string{
		"cognitive":  "## 认知层文章（How-to / 问题解决）",
		"evaluation": "## 评估层文章（对比 / 排行 / 选型）",
		"decision":   "## 决策层文章（价格 / 免费版）",
		"retention":  "## 留存层文章（案例 / 实践）",
		"other":      "## 其他文章",
	}
	for _, stage := range []string{"cognitive", "evaluation", "decision", "retention", "other"} {
		arts, ok := groups[stage]
		if !ok || len(arts) == 0 {
			continue
		}
		sb.WriteString(labels[stage] + "\n\n")
		for _, a := range arts {
			url := a.SiteURL
			if url == "" {
				url = fmt.Sprintf("https://%s%s", domain, a.SitePath)
			}
			desc := a.Keyword
			if desc == "" {
				desc = a.Title
			}
			sb.WriteString(fmt.Sprintf("- [%s](%s): %s\n", a.Title, url, desc))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// GenerateRobots 2026 AI 爬虫友好 robots.txt
func GenerateRobots() string {
	return `# HiveMtk GEO Site - 2026 AI Crawler Friendly
User-agent: *
Allow: /
Disallow: /admin/
Disallow: /private/

# AI 搜索索引爬虫（放行！决定 AI 可见性）
User-agent: OAI-SearchBot
Allow: /
User-agent: Claude-SearchBot
Allow: /
User-agent: PerplexityBot
Allow: /

# 传统搜索爬虫
User-agent: Googlebot
Allow: /
User-agent: Baiduspider
Allow: /
User-agent: bingbot
Allow: /
User-agent: 360Spider
Allow: /
User-agent: Sogou
Allow: /

# AI 训练爬虫（放行，需要 AI 引用）
User-agent: GPTBot
Allow: /
User-agent: ClaudeBot
Allow: /
User-agent: DeepSeekBot
Allow: /
User-agent: Bytespider
Allow: /
User-agent: QwenBot
Allow: /
User-agent: ErnieBot
Allow: /
User-agent: TencentAIspider
Allow: /
User-agent: MoonshotBot
Allow: /
User-agent: Meta-ExternalAgent
Allow: /
User-agent: CCBot
Allow: /
User-agent: Applebot-Extended
Allow: /

# Amazonbot
User-agent: Amazonbot
Allow: /

Sitemap: /sitemap.xml
`
}

// ExportToHugo 文章导出到 Hugo content/ 目录
// 同时写 llms.txt + robots.txt + sitemap.xml 到 Hugo static/
func (s *SiteDeployService) ExportToHugo(ctx context.Context) (int, error) {
	// 读配置
	var siteCfg model.GeoSite
	if err := s.db.Where("active = true").First(&siteCfg).Error; err != nil {
		return 0, fmt.Errorf("geo_site 未配置: %w", err)
	}
	if siteCfg.HugoPath == "" {
		return 0, fmt.Errorf("hugo_path 未配置")
	}

	// 确保目录存在
	contentDir := filepath.Join(siteCfg.HugoPath, "content", "posts")
	staticDir := filepath.Join(siteCfg.HugoPath, "static")
	for _, d := range []string{contentDir, staticDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return 0, err
		}
	}

	// 读已发布文章
	var articles []model.GeoArticle
	if err := s.db.Where("status = ?", "published").Find(&articles).Error; err != nil {
		return 0, err
	}

	// 写 Markdown
	count := 0
	for _, a := range articles {
		slug := slugify(a.Title)
		mdPath := filepath.Join(contentDir, slug+".md")

		var frontmatter strings.Builder
		frontmatter.WriteString("---\n")
		frontmatter.WriteString(fmt.Sprintf("title: %q\n", a.Title))
		frontmatter.WriteString(fmt.Sprintf("date: %s\n", time.Now().Format("2006-01-02")))
		if a.Keyword != "" {
			frontmatter.WriteString(fmt.Sprintf("keywords: %q\n", a.Keyword))
		}
		if a.BrandName != "" {
			frontmatter.WriteString(fmt.Sprintf("brand: %q\n", a.BrandName))
		}
		if a.FunnelStage != "" {
			frontmatter.WriteString(fmt.Sprintf("funnel_stage: %q\n", a.FunnelStage))
		}
		if a.QueryIntent != "" {
			frontmatter.WriteString(fmt.Sprintf("query_intent: %q\n", a.QueryIntent))
		}
		frontmatter.WriteString("---\n\n")

		content := frontmatter.String() + a.Content
		if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
			logger.Warnf("写 markdown 失败 %s: %v", slug, err)
			continue
		}

		// 更新 GeoArticle 部署字段
		siteURL := fmt.Sprintf("https://%s/article/%s.html", siteCfg.Domain, slug)
		sitePath := fmt.Sprintf("/article/%s.html", slug)
		mdRelPath := fmt.Sprintf("/posts/%s.md", slug)
		now := time.Now()
		s.db.Model(&a).Updates(map[string]any{
			"site_url":      siteURL,
			"site_path":     sitePath,
			"markdown_path": mdRelPath,
			"deployed_at":   now,
			"llms_included": true,
		})
		count++
	}

	// 写 llms.txt（v2）
	llmsContent := GenerateLLMsTxt(siteCfg.Domain, siteCfg.Domain, articles)
	os.WriteFile(filepath.Join(staticDir, "llms.txt"), []byte(llmsContent), 0o644)

	// 写 robots.txt
	os.WriteFile(filepath.Join(staticDir, "robots.txt"), []byte(GenerateRobots()), 0o644)

	// 写 sitemap.xml
	sitemap := s.buildSitemapXML(siteCfg.Domain, articles)
	os.WriteFile(filepath.Join(staticDir, "sitemap.xml"), []byte(sitemap), 0o644)

	logger.Warnf("ExportToHugo: %d 篇文章已导出到 %s", count, siteCfg.HugoPath)
	return count, nil
}

// buildSitemapXML 生成 sitemap.xml
func (s *SiteDeployService) buildSitemapXML(domain string, articles []model.GeoArticle) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, a := range articles {
		url := a.SiteURL
		if url == "" {
			url = fmt.Sprintf("https://%s%s", domain, a.SitePath)
		}
		if url == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf(`<url><loc>%s</loc><changefreq>weekly</changefreq></url>`, url))
	}
	sb.WriteString(`</urlset>`)
	return sb.String()
}

// TriggerDeploy 触发部署（git add → commit → push → GitHub Actions → Cloudflare）
func (s *SiteDeployService) TriggerDeploy(ctx context.Context) (string, error) {
	var siteCfg model.GeoSite
	if err := s.db.Where("active = true").First(&siteCfg).Error; err != nil {
		return "", err
	}

	cmds := [][]string{
		{"git", "-C", siteCfg.HugoPath, "add", "."},
		{"git", "-C", siteCfg.HugoPath, "commit", "-m", fmt.Sprintf("GEO deploy: %s", time.Now().Format("2006-01-02 15:04"))},
		{"git", "-C", siteCfg.HugoPath, "push", "origin", "main"},
	}
	for _, args := range cmds {
		c := exec.CommandContext(ctx, args[0], args[1:]...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			logger.Warnf("TriggerDeploy 失败: %v", err)
			return "", fmt.Errorf("%v: %w", strings.Join(args, " "), err)
		}
	}

	now := time.Now()
	s.db.Model(&siteCfg).Update("last_deploy_at", now)

	// 触发蜘蛛推送（异步，不等部署完成）
	// 用 >= 而不是 =，避免时间戳精度导致漏查
	triggerFrom := now.Add(-30 * time.Second)
	var articles []model.GeoArticle
	s.db.Where("deployed_at >= ?", triggerFrom).Find(&articles)
	urls := make([]string, 0, len(articles))
	for _, a := range articles {
		if a.SiteURL != "" {
			urls = append(urls, a.SiteURL)
		}
	}
	if len(urls) > 0 && s.pushSvc != nil {
		go func() {
			_ = s.pushSvc.PushURLs(context.Background(), urls)
		}()
	}

	return fmt.Sprintf("https://%s", siteCfg.Domain), nil
}

// HealthCheck 部署后健康检查（简化版，真实部署时应做 HTTP 请求）
func (s *SiteDeployService) HealthCheck(ctx context.Context, site *model.GeoSite) (*SiteHealth, error) {
	h := &SiteHealth{Domain: site.Domain}
	score := 0
	if h.HTTPSOK {
		score += 20
	}
	if h.LlmsOK {
		score += 25
	}
	if h.RobotsOK {
		score += 20
	}
	if h.SitemapOK {
		score += 20
	}
	if h.SchemaOK {
		score += 15
	}
	h.HealthScore = score
	return h, nil
}

// slugify 中文标题转 URL slug（简化版）
func slugify(title string) string {
	title = strings.ReplaceAll(title, " ", "-")
	title = strings.ReplaceAll(title, "/", "-")
	title = strings.ReplaceAll(title, "\\", "-")
	title = strings.ReplaceAll(title, "?", "")
	title = strings.ReplaceAll(title, ":", "")
	title = strings.ReplaceAll(title, "，", "")
	title = strings.ReplaceAll(title, "。", "")
	title = strings.ReplaceAll(title, "、", "-")
	if len(title) > 60 {
		title = title[:60]
	}
	title = strings.Trim(title, "-")
	return title
}
