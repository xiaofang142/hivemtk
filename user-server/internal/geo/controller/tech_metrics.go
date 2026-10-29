package controller

import (
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// TechMetricsController GEO 技术配置生成 + 内容质量指标分析控制器。
type TechMetricsController struct {
	techconfigSvc *service.TechConfigService
	metricsSvc    *service.MetricsService
}

// NewTechMetricsController 构造技术配置+指标控制器
//
// llms.txt 的 Docs 段在请求体未显式给文档时，由 TechConfigService 从 GEO KB
// 自动装配（手工条目优先，见 techconfig.go）。
func NewTechMetricsController(kbSvc *service.KBService) *TechMetricsController {
	return &TechMetricsController{
		techconfigSvc: service.NewTechConfigServiceWithKB(kbSvc),
		metricsSvc:    service.NewMetricsService(),
	}
}

// @Summary      生成成功
// @Tags      tech-metrics
// @Router    /api/geo/techconfig/robots [post]
// GenerateRobots 生成 robots.txt
// POST /geo/techconfig/robots
func (c *TechMetricsController) GenerateRobots(ctx *gin.Context) {
	var cfg service.RobotsConfig
	if !response.BindJSON(ctx, &cfg) {
		return
	}
	response.Success(ctx, gin.H{"content": c.techconfigSvc.GenerateRobots(&cfg)}, "生成成功")
}

// @Summary      生成成功
// @Tags      tech-metrics
// @Router    /api/geo/techconfig/llms-txt [post]
// GenerateLLMsTxt 生成 llms.txt（AI 引擎知识索引）
// POST /geo/techconfig/llms-txt
func (c *TechMetricsController) GenerateLLMsTxt(ctx *gin.Context) {
	var cfg service.LLMsTxtConfig
	if !response.BindJSON(ctx, &cfg) {
		return
	}
	response.Success(ctx, gin.H{"content": c.techconfigSvc.GenerateLLMsTxt(&cfg)}, "生成成功")
}

// @Summary      生成成功
// @Tags      tech-metrics
// @Router    /api/geo/techconfig/sitemap [post]
// GenerateSitemap 生成 sitemap.xml
// POST /geo/techconfig/sitemap
func (c *TechMetricsController) GenerateSitemap(ctx *gin.Context) {
	var cfg service.SitemapConfig
	if !response.BindJSON(ctx, &cfg) {
		return
	}
	response.Success(ctx, gin.H{"content": c.techconfigSvc.GenerateSitemap(&cfg)}, "生成成功")
}

// @Summary      审计完成（含真实技术检查）
// @Tags      tech-metrics
// @Router    /api/geo/techconfig/audit [post]
// AuditContent 内容审计（25 因子评分 + 等级 + 修复清单）
// POST /geo/techconfig/audit
func (c *TechMetricsController) AuditContent(ctx *gin.Context) {
	var req struct {
		URL          string `json:"url"`
		Title        string `json:"title"`
		Content      string `json:"content" binding:"required"`
		MetaDesc     string `json:"meta_desc"`
		SchemaJSONLD string `json:"schema_jsonld"`
		Live         bool   `json:"live"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	if req.Live && req.URL != "" {
		response.Success(ctx, c.techconfigSvc.RunGEOAuditLive(ctx.Request.Context(), req.URL, req.Title, req.Content, req.MetaDesc, req.SchemaJSONLD), "审计完成（含真实技术检查）")
		return
	}
	response.Success(ctx, c.techconfigSvc.RunGEOAudit(req.URL, req.Title, req.Content, req.MetaDesc, req.SchemaJSONLD), "审计完成")
}

// @Summary      站点审计完成（rubric v2 九类百分制）
// @Tags      tech-metrics
// @Router    /api/geo/techconfig/site-audit [post]
// SiteAudit 站点审计 v2：活体抓取目标 URL，按九类 rubric v2 打分并给修复清单。
// 仅显式触发；出站请求只访问目标域公开资源，不带任何品牌配置数据。
// POST /geo/techconfig/site-audit
func (c *TechMetricsController) SiteAudit(ctx *gin.Context) {
	var req struct {
		URL   string `json:"url" binding:"required"`
		Brand string `json:"brand"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	rep, err := c.techconfigSvc.RunSiteAudit(ctx.Request.Context(), req.URL, req.Brand)
	if err != nil {
		response.ValidationError(ctx, err.Error())
		return
	}
	response.Success(ctx, rep, "站点审计完成")
}

// @Summary      分析完成
// @Tags      tech-metrics
// @Router    /api/geo/metrics/analyze [post]
// AnalyzeMetrics 分析内容质量指标
// POST /geo/metrics/analyze
func (c *TechMetricsController) AnalyzeMetrics(ctx *gin.Context) {
	var req struct {
		Content string `json:"content" binding:"required"`
		Keyword string `json:"keyword"`
		Brand   string `json:"brand"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	response.Success(ctx, c.metricsSvc.Analyze(req.Content, req.Keyword, req.Brand), "分析完成")
}
