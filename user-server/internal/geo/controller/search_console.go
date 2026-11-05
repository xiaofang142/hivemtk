package controller

import (
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// SearchConsoleController GSC 数据导入与双端报表。
type SearchConsoleController struct {
	svc *service.SearchConsoleService
}

func NewSearchConsoleController(svc *service.SearchConsoleService) *SearchConsoleController {
	return &SearchConsoleController{svc: svc}
}

// @Summary 导入 GSC 性能数据
// @Tags    geo-search-console
// @Router  /api/geo/search-console/import [post]
// Import 接收 JSON rows 或 CSV 文本；同一 (date,query,page) 幂等覆盖。
func (c *SearchConsoleController) Import(ctx *gin.Context) {
	var body struct {
		Rows []service.SearchConsoleRow `json:"rows"`
		CSV  string                     `json:"csv"`
	}
	if !response.BindJSON(ctx, &body) {
		return
	}
	rows := body.Rows
	csvErrs := []string{}
	if body.CSV != "" {
		parsed, errs, err := service.ParseSearchConsoleCSV(body.CSV)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		rows = append(rows, parsed...)
		csvErrs = errs
		if len(errs) > 0 && len(rows) == 0 {
			response.Error(ctx, http.StatusBadRequest, "CSV 没有合法数据行")
			return
		}
	}
	if len(rows) == 0 {
		response.Error(ctx, http.StatusBadRequest, "rows 与 csv 至少提供一个")
		return
	}
	if len(rows) > 5000 {
		response.Error(ctx, http.StatusBadRequest, "单次最多导入 5000 行")
		return
	}
	imported, skipped, errs, err := c.svc.Import(ctx.Request.Context(), rows)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	skipped += len(csvErrs)
	errs = append(csvErrs, errs...)
	response.Success(ctx, gin.H{"imported": imported, "skipped": skipped, "errors": errs}, "导入完成")
}

// @Summary 双端监测报表（AI 引用 + Google 点击/展现）
// @Tags    geo-search-console
// @Router  /api/geo/reports/dual-engine [get]
// DualEngine GET /geo/reports/dual-engine?days=30
func (c *SearchConsoleController) DualEngine(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	report, err := c.svc.DualEngine(ctx.Request.Context(), days)
	if err != nil {
		response.ErrorFromDB(ctx, err, "生成双端报表失败")
		return
	}
	response.Success(ctx, report, "ok")
}
