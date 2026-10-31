package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// CustomReportController 自定义报表控制器
type CustomReportController struct {
	reportService *service.CustomReportService
}

// NewCustomReportController 创建自定义报表控制器
func NewCustomReportController() *CustomReportController {
	return &CustomReportController{
		reportService: service.NewCustomReportService(),
	}
}

func extractUserContext(ctx *gin.Context) (uint, bool) {
	userID, _ := ctx.Get("user_id")
	roleVal, _ := ctx.Get("role")
	role, _ := roleVal.(string)
	isAdmin := role == "admin" || role == "super_admin"
	uid, _ := userID.(uint)
	return uid, isAdmin
}

// CreateReport 创建报表
func (c *CustomReportController) CreateReport(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}

	var req service.CreateReportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// 不拼 err.Error()：encoding/json 的原文会带出 Go 结构体字段名
		// （如 "cannot unmarshal string into Go struct field CreateReportRequest.dimensions"）。
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name / data_source / chart_type 必填；dimensions、metrics 必须是数组，元素形如 {\"field\":\"platform\",\"label\":\"平台\"}")
		return
	}

	report, err := c.reportService.CreateReport(ctx.Request.Context(), userID.(uint), &req)
	if errhttp.HandleServiceError(ctx, err) {
		return
	}

	response.Success(ctx, report, "创建成功")
}

// GetReport 获取报表详情
func (c *CustomReportController) GetReport(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的报表 ID")
		return
	}

	userID, isAdmin := extractUserContext(ctx)
	report, err := c.reportService.GetReport(ctx.Request.Context(), uint(id), userID, isAdmin)
	if err != nil {
		replyReportError(ctx, err, "获取报表")
		return
	}

	response.Success(ctx, report, "获取成功")
}

// GetReportList 获取报表列表
func (c *CustomReportController) GetReportList(ctx *gin.Context) {

	page := 1
	pageSize := 20

	if p := ctx.Query("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if ps := ctx.Query("page_size"); ps != "" {
		pageSize, _ = strconv.Atoi(ps)
	}
	// 回显必须与实际查询用的分页一致：仓储会对越界值纠偏，控制器若不纠，
	// 前端拿到的 page/page_size 就与实际数据页不符（page=0 会显示成第 0 页）。
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	userID, isAdmin := extractUserContext(ctx)
	reports, total, err := c.reportService.GetReportList(ctx.Request.Context(), page, pageSize, userID, isAdmin)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      reports,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// UpdateReport 更新报表
func (c *CustomReportController) UpdateReport(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的报表 ID")
		return
	}

	var req service.UpdateReportRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name / data_source / chart_type 必须是字符串；dimensions、metrics 必须是数组")
		return
	}

	userID, isAdmin := extractUserContext(ctx)
	report, err := c.reportService.UpdateReport(ctx.Request.Context(), uint(id), userID, isAdmin, &req)
	// 与 CreateReport 同一条映射：不支持的图表类型/数据源是**入参错误**（400），
	// 不是数据库错误。用 HandleDBError 会把它压成 500「更新报表失败」，把可行动的
	// 原因（哪一项不支持）换成一句无从下手的话。
	if err != nil {
		replyReportError(ctx, err, "更新报表")
		return
	}

	response.Success(ctx, report, "更新成功")
}

// DeleteReport 删除报表
func (c *CustomReportController) DeleteReport(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的报表 ID")
		return
	}

	userID, isAdmin := extractUserContext(ctx)
	err = c.reportService.DeleteReport(ctx.Request.Context(), uint(id), userID, isAdmin)
	if err != nil {
		replyReportError(ctx, err, "删除报表")
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetPublicTemplates 获取公开模板
func (c *CustomReportController) GetPublicTemplates(ctx *gin.Context) {
	templates, err := c.reportService.GetPublicTemplates(ctx.Request.Context())
	if err != nil {
		replyReportError(ctx, err, "获取报表模板")
		return
	}

	response.Success(ctx, templates, "获取成功")
}

// UseTemplate 使用模板
func (c *CustomReportController) UseTemplate(ctx *gin.Context) {

	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的模板 ID")
		return
	}

	report, err := c.reportService.UseTemplate(ctx.Request.Context(), uint(id), userID.(uint))
	if err != nil {
		replyReportError(ctx, err, "使用报表模板")
		return
	}

	response.Success(ctx, report, "使用模板成功")
}

// QueryReportData 查询报表数据
func (c *CustomReportController) QueryReportData(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的报表 ID")
		return
	}

	params, err := reportQueryParams(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	userID, isAdmin := extractUserContext(ctx)
	report, err := c.reportService.GetReport(ctx.Request.Context(), uint(id), userID, isAdmin)
	if err != nil {
		replyReportError(ctx, err, "获取报表")
		return
	}

	data, err := c.reportService.QueryReportData(ctx.Request.Context(), report, params)
	if err != nil {
		replyReportError(ctx, err, "查询报表数据")
		return
	}

	response.Success(ctx, data, "获取成功")
}

func (c *CustomReportController) ExportCSV(ginCtx *gin.Context) {

	idStr := ginCtx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ginCtx, http.StatusBadRequest, "无效的报表 ID")
		return
	}

	params, err := reportQueryParams(ginCtx)
	if err != nil {
		response.Error(ginCtx, http.StatusBadRequest, err.Error())
		return
	}

	userID, isAdmin := extractUserContext(ginCtx)
	report, err := c.reportService.GetReport(ginCtx.Request.Context(), uint(id), userID, isAdmin)
	if err != nil {
		replyReportError(ginCtx, err, "获取报表")
		return
	}

	// 先把数据算出来再写响应头：CSV 头一旦写出，后续 response.Error 会把 JSON
	// 错误体塞进 CSV 流里，前端只会得到一个既不是 CSV 也不是 JSON 的坏文件。
	data, err := c.reportService.QueryReportData(ginCtx.Request.Context(), report, params)
	if err != nil {
		replyReportError(ginCtx, err, "导出报表")
		return
	}

	ginCtx.Header("Content-Type", "text/csv; charset=utf-8")
	ginCtx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="report_%d.csv"`, id))
	if err := service.ExportReportDataCSV(ginCtx.Writer, data); err != nil {
		response.Error(ginCtx, http.StatusInternalServerError, "写 CSV 失败")
		return
	}
}

// reportQueryParams 解析报表查询区间。日期非法必须报错——静默忽略会让用户
// 以为在查 3 天数据，实际拿到的是全量。
func reportQueryParams(ctx *gin.Context) (map[string]any, error) {
	params := make(map[string]any)
	if raw := ctx.Query("start_time"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, fmt.Errorf("start_time 必须是 YYYY-MM-DD（当前为 %q）", raw)
		}
		params["start_time"] = t
	}
	if raw := ctx.Query("end_time"); raw != "" {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, fmt.Errorf("end_time 必须是 YYYY-MM-DD（当前为 %q）", raw)
		}
		params["end_time"] = t
	}
	if layer := ctx.Query("layer"); layer != "" {
		params["layer"] = layer
	}
	return params, nil
}

// replyReportError 统一报表错误出口：不存在→404、无权限→403、超行→400，
// 其余交给数据库错误映射。原先把所有错误都打成 404 + err.Error()，
// 既把 DB 故障说成「报表不存在」，又把内部错误原文透给前端。
// isReportParamInvalid 报表校验错误（数据源/图表类型等）应回 400，而不是被 HandleDBError 压成 500。
func isReportParamInvalid(err error) bool {
	var be *bizerr.BizError
	return errors.As(err, &be) && be.Code == bizerr.CodeParamInvalid
}

func replyReportError(ctx *gin.Context, err error, fallback string) {
	switch {
	case err == nil:
		return
	case errors.Is(err, service.ErrReportNotFound):
		response.NotFound(ctx, err.Error())
	case errors.Is(err, service.ErrReportForbidden):
		response.Error(ctx, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrReportTooManyRows):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	case isReportParamInvalid(err):
		response.Error(ctx, http.StatusBadRequest, err.Error())
	default:
		errhttp.HandleDBError(ctx, err, fallback)
	}
}
