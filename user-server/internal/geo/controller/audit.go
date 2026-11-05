package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// AuditController GEO 审计中心：体检、历史、对比、报告。
type AuditController struct {
	svc *service.AuditService
}

func NewAuditController(svc *service.AuditService) *AuditController {
	return &AuditController{svc: svc}
}

// @Summary 站点体检（rubric v3；scope=site 时按 sitemap 逐页审计）
// @Tags    geo-audit
// @Router  /api/geo/audit/site [post]
// RunSite POST /geo/audit/site
func (c *AuditController) RunSite(ctx *gin.Context) {
	var req dto.AuditSiteRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = model.GeoAuditScopePage
	}
	if scope != model.GeoAuditScopePage && scope != model.GeoAuditScopeSite {
		response.Error(ctx, http.StatusBadRequest, "scope 仅支持 page 或 site")
		return
	}
	run, view, err := c.svc.RunSiteAudit(ctx.Request.Context(), req.URL, req.Brand,
		scope, req.MaxPages, model.GeoAuditSourceManual, ctx.GetString("username"))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"run": run, "report": view}, "体检完成")
}

// @Summary 内容体检（25 因子；给 URL 自动抓正文）
// @Tags    geo-audit
// @Router  /api/geo/audit/content [post]
// RunContent POST /geo/audit/content
func (c *AuditController) RunContent(ctx *gin.Context) {
	var req dto.AuditContentRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	run, view, err := c.svc.RunContentAudit(ctx.Request.Context(), service.ContentAuditInput{
		URL: req.URL, Title: req.Title, Content: req.Content,
		MetaDesc: req.MetaDesc, SchemaJSONLD: req.SchemaJSONLD,
		Brand: req.Brand, Live: req.Live,
	}, model.GeoAuditSourceManual, ctx.GetString("username"))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"run": run, "report": view}, "体检完成")
}

// @Summary 审计历史
// @Tags    geo-audit
// @Router  /api/geo/audit/runs [get]
// List GET /geo/audit/runs?kind=&url=&rubric_version=&page=&limit=
func (c *AuditController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	runs, total, err := c.svc.List(ctx.Request.Context(), service.AuditRunQuery{
		Kind:          ctx.Query("kind"),
		URL:           ctx.Query("url"),
		RubricVersion: ctx.Query("rubric_version"),
		Page:          page,
		Limit:         limit,
	})
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "查询审计历史失败")
		return
	}
	list := make([]dto.AuditRunBrief, 0, len(runs))
	for _, r := range runs {
		list = append(list, dto.AuditRunBrief{
			ID: r.ID, Kind: r.Kind, Scope: r.Scope, URL: r.URL, Brand: r.Brand,
			RubricVersion: r.RubricVersion, Score: r.Score, MaxScore: r.MaxScore,
			Grade: r.Grade, Source: r.Source, PageCount: r.PageCount,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	response.Success(ctx, dto.AuditRunListResponse{List: list, Total: total}, "ok")
}

// @Summary 审计详情
// @Tags    geo-audit
// @Router  /api/geo/audit/runs/{id} [get]
// Get GET /geo/audit/runs/:id
func (c *AuditController) Get(ctx *gin.Context) {
	run, items, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "审计记录不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "查询审计详情失败")
		return
	}
	response.Success(ctx, gin.H{"run": run, "items": items}, "ok")
}

// @Summary 审计报告（html/csv）
// @Tags    geo-audit
// @Router  /api/geo/audit/runs/{id}/report [get]
// Report GET /geo/audit/runs/:id/report?format=html|csv
func (c *AuditController) Report(ctx *gin.Context) {
	id := ctx.Param("id")
	switch strings.ToLower(ctx.DefaultQuery("format", "html")) {
	case "html":
		body, err := c.svc.BuildReportHTML(ctx.Request.Context(), id)
		if err != nil {
			c.reportError(ctx, err)
			return
		}
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
	case "csv":
		body, err := c.svc.BuildReportCSV(ctx.Request.Context(), id)
		if err != nil {
			c.reportError(ctx, err)
			return
		}
		ctx.Header("Content-Disposition", `attachment; filename="geo-audit-`+id+`.csv"`)
		ctx.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(body))
	default:
		response.Error(ctx, http.StatusBadRequest, "format 仅支持 html 或 csv")
	}
}

func (c *AuditController) reportError(ctx *gin.Context, err error) {
	if errhttp.IsNotFoundError(err) {
		response.Error(ctx, http.StatusNotFound, "审计记录不存在")
		return
	}
	response.Error(ctx, http.StatusInternalServerError, "生成审计报告失败")
}

// @Summary 两次审计对比
// @Tags    geo-audit
// @Router  /api/geo/audit/compare [get]
// Compare GET /geo/audit/compare?from=&to=
func (c *AuditController) Compare(ctx *gin.Context) {
	from, to := ctx.Query("from"), ctx.Query("to")
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		response.Error(ctx, http.StatusBadRequest, "from 与 to 不能为空")
		return
	}
	res, err := c.svc.Compare(ctx.Request.Context(), from, to)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "审计记录不存在")
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "对比审计记录失败")
		return
	}
	response.Success(ctx, res, "ok")
}
