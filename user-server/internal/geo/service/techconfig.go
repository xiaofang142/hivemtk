package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
)

// TechConfigService GEO 技术配置生成服务
//
// llms.txt 的 Documents 支持从 GEO KB 文档表自动装配——
// kbDocs 由装配处注入（geo_routes.go）；为 nil 时保持旧行为（只用请求体传入的
// Documents），不引入新的硬依赖。手工覆盖优先：请求体里显式给的条目不会被自动条目挤掉。
type TechConfigService struct {
	kbDocs GeoKBDocsSource
}

// GeoKBDocsSource 是 llms.txt 自动装配需要的最小 KB 读能力
// （*GeoKnowledgeService 结构上即满足；收接口是为了可测性）。
type GeoKBDocsSource interface {
	ListDocuments(ctx context.Context) ([]*model.GeoKnowledgeDocument, error)
}

// NewTechConfigService 创建技术配置生成服务
func NewTechConfigService() *TechConfigService {
	return &TechConfigService{}
}

// NewTechConfigServiceWithKB 创建带 KB 自动装配能力的实例。
func NewTechConfigServiceWithKB(kb GeoKBDocsSource) *TechConfigService {
	return &TechConfigService{kbDocs: kb}
}

// RobotsConfig robots.txt 生成配置
type RobotsConfig struct {
	SiteURL      string   `json:"site_url" binding:"required"`
	Disallow     []string `json:"disallow"`
	Allow        []string `json:"allow"`
	CrawlDelay   int      `json:"crawl_delay"`
	SitemapPaths []string `json:"sitemaps"`
}

// SitemapConfig sitemap.xml 生成配置
type SitemapConfig struct {
	SiteURL string   `json:"site_url" binding:"required"`
	URLs    []string `json:"urls"`
}

// GenerateRobots 生成 robots.txt
func (s *TechConfigService) GenerateRobots(cfg *RobotsConfig) string {
	var b strings.Builder
	b.WriteString("User-agent: *\n")
	for _, p := range cfg.Allow {
		b.WriteString("Allow: " + p + "\n")
	}
	for _, p := range cfg.Disallow {
		b.WriteString("Disallow: " + p + "\n")
	}
	if cfg.CrawlDelay > 0 {
		b.WriteString(fmt.Sprintf("Crawl-delay: %d\n", cfg.CrawlDelay))
	}
	sm := cfg.SitemapPaths
	if len(sm) == 0 && cfg.SiteURL != "" {
		sm = []string{cfg.SiteURL + "/sitemap.xml"}
	}
	for _, sPath := range sm {
		if !strings.HasPrefix(sPath, "http") && cfg.SiteURL != "" {
			sPath = cfg.SiteURL + sPath
		}
		b.WriteString("Sitemap: " + sPath + "\n")
	}
	return b.String()
}

// GenerateSitemap 生成 sitemap.xml
func (s *TechConfigService) GenerateSitemap(cfg *SitemapConfig) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	now := time.Now().UTC().Format(time.DateOnly)
	for _, u := range cfg.URLs {
		full := u
		if !strings.HasPrefix(full, "http") && cfg.SiteURL != "" {
			full = cfg.SiteURL + u
		}
		b.WriteString("  <url>\n")
		b.WriteString("    <loc>" + full + "</loc>\n")
		b.WriteString("    <lastmod>" + now + "</lastmod>\n")
		b.WriteString("    <changefreq>weekly</changefreq>\n")
		b.WriteString("    <priority>0.8</priority>\n")
		b.WriteString("  </url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// LLMsTxtConfig llms.txt 生成配置
// llms.txt 是面向 AI 引擎的站点知识索引约定（类比 robots.txt 之于搜索引擎），
// 帮助 LLM 快速定位品牌权威文档，提升被引用概率。
type LLMsTxtConfig struct {
	SiteURL   string            `json:"site_url" binding:"required"`
	Brand     string            `json:"brand" binding:"required"`
	Overview  string            `json:"overview"`
	Documents []LLMsDocEntry    `json:"documents"`
	Policies  map[string]string `json:"policies,omitempty"`
}

type LLMsDocEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// GenerateLLMsTxt 输出 Markdown 格式的 llms.txt 内容
//
// 请求体未带 Documents（或带了还要求自动补）时，从 GEO KB 文档表
// 自动装配——KB 文档就是品牌权威资料的第一现场，llms.txt 的受众（AI 引擎）恰恰
// 需要它们。手工覆盖语义：请求体里显式传入的条目排前面且不重复追加。
func (s *TechConfigService) GenerateLLMsTxt(cfg *LLMsTxtConfig) string {
	return s.GenerateLLMsTxtCtx(context.Background(), cfg)
}

// GenerateLLMsTxtCtx 带请求/任务租户的版本：KB 文档表是租户表，
// 内部必须用该 ctx 读取，否则多租户模式下自动装配恒为空。
func (s *TechConfigService) GenerateLLMsTxtCtx(ctx context.Context, cfg *LLMsTxtConfig) string {
	docs := cfg.Documents
	if s.kbDocs != nil {
		docs = mergeKBDocsInto(ctx, s, cfg, docs)
	}
	return renderLLMsTxt(cfg, docs)
}

// llmsTxtAutoDocsThreshold：请求体 Documents 少于该数且 KB 装配可用 ⇒ 自动补齐。
// 显式传了大量文档视为"完全手工接管"，不再混入 KB 条目（可预期性优先）。
const llmsTxtAutoDocsThreshold = 1

func mergeKBDocsInto(ctx context.Context, s *TechConfigService, cfg *LLMsTxtConfig, manual []LLMsDocEntry) []LLMsDocEntry {
	if len(manual) >= llmsTxtAutoDocsThreshold {
		return manual
	}
	kbList, err := s.kbDocs.ListDocuments(ctx)
	if err != nil || len(kbList) == 0 {
		return manual
	}
	seen := map[string]bool{}
	for _, d := range manual {
		seen[d.URL] = true
	}
	out := append([]LLMsDocEntry{}, manual...)
	for _, doc := range kbList {
		u := doc.SourceURL
		if u == "" {
			// KB 文档没有外链时用站内锚：llms.txt 条目必须是可取回的 URL，
			// 编不出就跳过——塞一个 404 链接比缺一条更坏。
			continue
		}
		if seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, LLMsDocEntry{
			Title:       doc.Title,
			URL:         u,
			Description: llmsDocDescription(doc),
		})
	}
	return out
}

func llmsDocDescription(doc *model.GeoKnowledgeDocument) string {
	desc := []rune(doc.Content)
	if len(desc) > 80 {
		desc = desc[:80]
	}
	return string(desc)
}

func renderLLMsTxt(cfg *LLMsTxtConfig, docs []LLMsDocEntry) string {
	var b strings.Builder
	b.WriteString("# " + cfg.Brand + "\n\n")
	if cfg.Overview != "" {
		b.WriteString("> " + cfg.Overview + "\n\n")
	}
	if len(docs) > 0 {
		b.WriteString("## Docs\n\n")
		for _, d := range docs {
			line := "- [" + d.Title + "](" + d.URL + ")"
			if d.Description != "" {
				line += ": " + d.Description
			}
			b.WriteString(line + "\n")
		}
	}
	if len(cfg.Policies) > 0 {
		b.WriteString("\n## Policies\n\n")
		for k, v := range cfg.Policies {
			b.WriteString("- [" + k + "](" + v + ")\n")
		}
	}
	return b.String()
}
