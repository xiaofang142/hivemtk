package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ErrSiteConfigInvalid 站点配置不完整（未启用站点 / 目录不存在）。
// 这类错误是用户在「静态站部署」页可以自行修正的配置问题，控制器应回 400，
// 而不是 500「服务器内部错误」。
var ErrSiteConfigInvalid = errors.New("站点配置不完整")

// SiteDeployService 静态站部署服务
//
// 负责：把 geo_articles 导出成 Hugo content/*.md，
// 生成 llms.txt v2 + robots.txt + sitemap.xml，
// 触发 GitHub Actions → Cloudflare Pages 部署。
type SiteDeployService struct {
	sites    repository.GeoSiteRepository
	articles repository.GeoArticleRepository
	pushSvc  *PushService
}

func NewSiteDeployService(
	sites repository.GeoSiteRepository,
	articles repository.GeoArticleRepository,
	pushSvc *PushService,
) *SiteDeployService {
	return &SiteDeployService{sites: sites, articles: articles, pushSvc: pushSvc}
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
	siteCfg, err := s.sites.GetActive(ctx)
	if err != nil {
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
	articles, err := s.articles.ListPublished(ctx)
	if err != nil {
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
		a.SiteURL = siteURL
		a.SitePath = sitePath
		a.MarkdownPath = mdRelPath
		a.DeployedAt = &now
		a.LlmsIncluded = true
		if err := s.articles.Update(ctx, a); err != nil {
			logger.Warnf("回写文章部署字段失败 %s: %v", slug, err)
			continue
		}
		count++
	}

	// 写 llms.txt（v2）
	llmsContent := GenerateLLMsTxt(siteCfg.Domain, siteCfg.Domain, derefArticles(articles))
	if err := os.WriteFile(filepath.Join(staticDir, "llms.txt"), []byte(llmsContent), 0o644); err != nil {
		logger.Warnf("写 llms.txt 失败: %v", err)
	}

	// 写 robots.txt
	if err := os.WriteFile(filepath.Join(staticDir, "robots.txt"), []byte(GenerateRobots()), 0o644); err != nil {
		logger.Warnf("写 robots.txt 失败: %v", err)
	}

	// 写 sitemap.xml
	sitemap := s.buildSitemapXML(siteCfg.Domain, derefArticles(articles))
	if err := os.WriteFile(filepath.Join(staticDir, "sitemap.xml"), []byte(sitemap), 0o644); err != nil {
		logger.Warnf("写 sitemap.xml 失败: %v", err)
	}

	logger.Warnf("ExportToHugo: %d 篇文章已导出到 %s", count, siteCfg.HugoPath)
	return count, nil
}

// derefArticles 值切片适配（生成函数按值接收，仓储返回指针切片）
func derefArticles(in []*model.GeoArticle) []model.GeoArticle {
	out := make([]model.GeoArticle, 0, len(in))
	for _, a := range in {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out
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
	siteCfg, err := s.sites.GetActive(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%w: 尚未启用静态站，请先在「静态站部署」保存并启用一个站点", ErrSiteConfigInvalid)
		}
		return "", err
	}
	if strings.TrimSpace(siteCfg.HugoPath) == "" {
		return "", fmt.Errorf("%w: 站点目录未配置（hugo_path 为空）", ErrSiteConfigInvalid)
	}
	if st, statErr := os.Stat(siteCfg.HugoPath); statErr != nil || !st.IsDir() {
		return "", fmt.Errorf("%w: 站点目录不存在或不可访问：%s", ErrSiteConfigInvalid, siteCfg.HugoPath)
	}
	// 目录存在但不是 git 仓库时，git add 会以 128 退出；这是「静态站部署」配置问题，
	// 不是服务端内部错误，提前判掉并给出可照做的提示。
	if out, gitErr := exec.CommandContext(ctx, "git", "-C", siteCfg.HugoPath, "rev-parse", "--is-inside-work-tree").CombinedOutput(); gitErr != nil {
		_ = out
		return "", fmt.Errorf("%w: 站点目录不是 git 仓库：%s（请先 git init 或填写正确的 Hugo 目录）", ErrSiteConfigInvalid, siteCfg.HugoPath)
	}

	cmds := [][]string{
		{"git", "-C", siteCfg.HugoPath, "add", "."},
		{"git", "-C", siteCfg.HugoPath, "commit", "-m", fmt.Sprintf("GEO deploy: %s", time.Now().Format("2006-01-02 15:04"))},
		{"git", "-C", siteCfg.HugoPath, "push", "origin", "main"},
	}
	for _, args := range cmds {
		c := exec.CommandContext(ctx, args[0], args[1:]...)
		out, err := c.CombinedOutput()
		if len(out) > 0 {
			_, _ = os.Stdout.Write(out)
		}
		if err != nil {
			// 没有任何内容可提交时 git commit 会以非 0 退出；这不是部署失败，
			// 后续 push 仍应执行（否则「只改配置不动文章」的部署永远红）。
			joined := string(out)
			isCommit := false
			for _, a := range args {
				if a == "commit" {
					isCommit = true
					break
				}
			}
			if isCommit && (strings.Contains(joined, "nothing to commit") || strings.Contains(joined, "no changes added to commit")) {
				continue
			}
			logger.Warnf("TriggerDeploy 失败: %v", err)
			return "", fmt.Errorf("%v: %w", strings.Join(args, " "), err)
		}
	}

	now := time.Now()
	if err := s.sites.UpdateLastDeployAt(ctx, siteCfg.ID, now); err != nil {
		logger.Warnf("回写 last_deploy_at 失败 site=%s: %v", siteCfg.ID, err)
	}

	// 触发蜘蛛推送（异步，不等部署完成）
	// 用 >= 而不是 =，避免时间戳精度导致漏查
	triggerFrom := now.Add(-30 * time.Second)
	articles, err := s.articles.ListDeployedSince(ctx, triggerFrom)
	if err != nil {
		return "", err
	}
	urls := make([]string, 0, len(articles))
	for _, a := range articles {
		if a.SiteURL != "" {
			urls = append(urls, a.SiteURL)
		}
	}
	if len(urls) > 0 && s.pushSvc != nil {
		// 行自证（多租户，二审 P1-9）：站点部署推送是**租户业务** —— 推送记录
		// （geo_push_records，经 PushURLs 的 tx）与配额回写（geo_pusher_configs，
		// persistQuota）都是租户表且已改为消费 ctx。本 goroutine 脱离请求生命周期、
		// 拿不到请求 ctx，用**站点行自己的 TenantID** 钉住（siteCfg 由上面带租户的
		// 请求 ctx 读出，行值可信）。此前这里用裸 context.Background()：on 模式下
		// 推送记录 Create / 配额 Update 全被隔离 callback fail-closed ⇒ 记账与配额
		// 静默全丢（推送本身照发，事后完全无账可查）。
		pushCtx := tenant.WithTenantID(context.Background(), siteCfg.TenantID)
		go func() {
			// 推送失败必须出声：这里在 goroutine 里，丢弃错误等于零日志。
			if err := s.pushSvc.PushURLs(pushCtx, urls); err != nil {
				logger.Warnf("部署后蜘蛛推送失败: %v", err)
			}
		}()
	}

	return fmt.Sprintf("https://%s", siteCfg.Domain), nil
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
