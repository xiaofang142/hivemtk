package controller

import (
	"net/http"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// KeywordMiningController 关键词蒸馏 v2（下拉词 + 长尾组合 + 漏斗）
type KeywordMiningController struct {
	svc *service.KeywordMiningService
}

func NewKeywordMiningController(svc *service.KeywordMiningService) *KeywordMiningController {
	return &KeywordMiningController{svc: svc}
}

// CrawlSuggest POST /geo/keyword-mining/crawl-suggest
// body: {"seeds": ["CRM", "SCRM"], "engines": ["baidu","bing","google","360","sogou"]}
func (c *KeywordMiningController) CrawlSuggest(ctx *gin.Context) {
	var req struct {
		Seeds   []string `json:"seeds" binding:"required"`
		Engines []string `json:"engines"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	results, err := c.svc.CrawlSuggest(ctx.Request.Context(), req.Seeds, req.Engines)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := c.svc.SaveMiningResults(ctx.Request.Context(), req.Seeds, results)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "下拉词落库失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"count": len(results), "saved": saved, "keywords": results}, "ok")
}

// CombineLongtail POST /geo/keyword-mining/longtail
// body: {"seeds": ["CRM"], "use_default_templates": true}
func (c *KeywordMiningController) CombineLongtail(ctx *gin.Context) {
	var req struct {
		Seeds           []string `json:"seeds" binding:"required"`
		UseDefaultTpls  bool     `json:"use_default_templates"`
		CustomTemplates []string `json:"custom_templates"`
	}
	if !response.BindJSON(ctx, &req) {
		return
	}
	templates := []service.LongtailTemplate{}
	if req.UseDefaultTpls || len(req.CustomTemplates) == 0 {
		templates = service.DefaultLongtailTemplates
	}
	for _, t := range req.CustomTemplates {
		templates = append(templates, service.LongtailTemplate{Template: t})
	}
	results, err := c.svc.CombineLongtail(ctx.Request.Context(), req.Seeds, templates)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := c.svc.SaveMiningResults(ctx.Request.Context(), req.Seeds, results)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "长尾词落库失败: "+err.Error())
		return
	}
	response.Success(ctx, gin.H{"count": len(results), "saved": saved, "keywords": results}, "ok")
}

// BuildFunnel GET /geo/keyword-mining/funnel
func (c *KeywordMiningController) BuildFunnel(ctx *gin.Context) {
	f, err := c.svc.BuildFunnel(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, f, "ok")
}
