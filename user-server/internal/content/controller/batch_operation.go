package controller

import (
	"encoding/csv"
	"fmt"
	"hivemtk-user/internal/pkg/errhttp"
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// BatchImportController 批量导入控制器
type BatchImportController struct {
	svc *service.BatchOperationService
}

// NewBatchImportController 创建批量导入控制器
func NewBatchImportController() *BatchImportController {
	return &BatchImportController{
		svc: service.NewBatchOperationService(),
	}
}

// ImportResult 导入结果（对外暴露）
type ImportResult struct {
	ImportType string   `json:"import_type"`
	Total      int      `json:"total"`
	Success    int      `json:"success"`
	Failed     int      `json:"failed"`
	Skipped    int      `json:"skipped"`
	Errors     []string `json:"errors,omitempty"`
	ImportedAt string   `json:"imported_at"`
}

// ImportFile 导入文件
func (c *BatchImportController) ImportFile(ctx *gin.Context) {

	importType := ctx.PostForm("type")
	if importType == "" {
		importType = string(service.ImportTypeClue)
	}

	fileHeader, err := ctx.FormFile("file")
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "上传文件失败："+err.Error())
		return
	}

	if fileHeader.Size > 50*1024*1024 {
		response.Error(ctx, http.StatusBadRequest, "文件大小超过 50MB 限制")
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "打开上传文件失败："+err.Error())
		return
	}
	defer func() { _ = f.Close() }()

	result, err := c.svc.ImportFromCSV(ctx.Request.Context(), service.ImportType(importType), f)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "导入失败："+err.Error())
		return
	}

	resp := &ImportResult{
		ImportType: string(result.ImportType),
		Total:      result.Total,
		Success:    result.Success,
		Failed:     result.Failed,
		Skipped:    result.Skipped,
		ImportedAt: result.ImportedAt,
		Errors:     make([]string, 0, len(result.Errors)),
	}
	for _, e := range result.Errors {
		resp.Errors = append(resp.Errors, fmt.Sprintf("第 %d 行: %s", e.Row, e.Reason))
	}

	response.Success(ctx, resp, "导入完成")
}

// DownloadTemplate 下载导入模板
func (c *BatchImportController) DownloadTemplate(ctx *gin.Context) {
	importType := ctx.Query("type")
	if importType == "" {
		importType = string(service.ImportTypeClue)
	}

	format := ctx.Query("format")
	if format == "" {
		format = "csv"
	}

	tmpl, err := c.svc.GetTemplate(service.ImportType(importType))
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "获取模板失败："+err.Error())
		return
	}

	var buf *strings.Builder
	{
		b := new(strings.Builder)
		w := csv.NewWriter(newStringBuilderWriter(b))
		_ = w.Write(tmpl.Headers)
		for _, ex := range tmpl.Examples {
			_ = w.Write(ex)
		}
		w.Flush()
		buf = b
	}

	ctx.Header("Content-Type", "text/csv; charset=utf-8")
	ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_template.%s", importType, format))
	ctx.String(http.StatusOK, buf.String())
}

type stringBuilderWriter struct {
	b *strings.Builder
}

func newStringBuilderWriter(b *strings.Builder) *stringBuilderWriter {
	return &stringBuilderWriter{b: b}
}

func (w *stringBuilderWriter) Write(p []byte) (int, error) {
	return w.b.Write(p)
}

// BatchExportController 批量导出控制器
type BatchExportController struct {
	svc *service.BatchOperationService
}

// NewBatchExportController 创建批量导出控制器
func NewBatchExportController() *BatchExportController {
	return &BatchExportController{
		svc: service.NewBatchOperationService(),
	}
}

// ExportData 导出数据
func (c *BatchExportController) ExportData(ctx *gin.Context) {
	_ = ctx

	exportType := ctx.PostForm("type")
	if exportType == "" {
		exportType = string(service.ExportTypeClue)
	}

	format := ctx.PostForm("format")
	if format == "" {
		format = "csv"
	}

	idsStr := ctx.PostForm("ids")
	var ids []string
	if idsStr != "" {
		for _, id := range strings.Split(idsStr, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}

	// 防御空请求全量导出（数据暴露）：必须显式指定要导出的记录 ID
	if len(ids) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请至少指定一个要导出的记录 ID（ids）")
		return
	}

	switch format {
	case "csv":
		buf, err := c.svc.GenerateCSV(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export_%s.csv", exportType, strconv.FormatInt(int64(buf.Len()), 10))
		ctx.Header("Content-Type", "text/csv; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "text/csv", buf.Bytes())
	case "jsonl":
		buf, err := c.svc.GenerateJSONL(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export.jsonl", exportType)
		ctx.Header("Content-Type", "application/jsonl; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "application/jsonl", buf.Bytes())
	case "markdown":
		buf, err := c.svc.GenerateMarkdown(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export.md", exportType)
		ctx.Header("Content-Type", "text/markdown; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "text/markdown", buf.Bytes())
	default:
		response.Error(ctx, http.StatusBadRequest, "暂不支持的导出格式: "+format)
	}
}

// BatchOperationController 批量操作控制器
type BatchOperationController struct {
	svc *service.BatchOperationService
}

// NewBatchOperationController 创建批量操作控制器
func NewBatchOperationController() *BatchOperationController {
	return &BatchOperationController{
		svc: service.NewBatchOperationService(),
	}
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// BatchDelete 批量删除
func (c *BatchOperationController) BatchDelete(ctx *gin.Context) {
	_ = ctx

	var req BatchDeleteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if len(req.IDs) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请选择要删除的记录")
		return
	}

	count, err := c.svc.BatchDeleteClues(ctx.Request.Context(), req.IDs)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"requested":     len(req.IDs),
		"deleted_count": count,
	}, "批量删除成功")
}

// BatchUpdateRequest 批量更新请求
type BatchUpdateRequest struct {
	IDs    []string          `json:"ids" binding:"required"`
	Fields map[string]string `json:"fields" binding:"required"`
}

// BatchUpdate 批量更新
func (c *BatchOperationController) BatchUpdate(ctx *gin.Context) {
	_ = ctx

	var req BatchUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if len(req.IDs) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请选择要更新的记录")
		return
	}
	if len(req.Fields) == 0 {
		response.Error(ctx, http.StatusBadRequest, "更新字段不能为空")
		return
	}

	count, err := c.svc.BatchUpdateClues(&service.BatchUpdateRequest{
		IDs:    req.IDs,
		Fields: req.Fields,
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "更新字段不能为空") || strings.Contains(errMsg, "无可更新字段") {
			response.Error(ctx, http.StatusBadRequest, errMsg)
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "更新失败："+errMsg)
		return
	}

	response.Success(ctx, gin.H{
		"requested":     len(req.IDs),
		"updated_count": count,
	}, "批量更新成功")
}

// GetTools 获取可用的批量操作工具列表
func (c *BatchOperationController) GetTools(ctx *gin.Context) {
	tools := []gin.H{
		{"name": "import", "label": "批量导入", "description": "通过 CSV 文件批量导入数据"},
		{"name": "export", "label": "批量导出", "description": "导出数据为 CSV 文件"},
		{"name": "delete", "label": "批量删除", "description": "批量删除选中的记录"},
		{"name": "update", "label": "批量更新", "description": "批量更新选中记录的字段"},
	}
	response.Success(ctx, gin.H{"list": tools, "total": len(tools)}, "获取成功")
}

// GetHistories 获取批量操作历史列表
func (c *BatchOperationController) GetHistories(ctx *gin.Context) {

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	list, total, err := c.svc.GetHistories(page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取历史记录失败: "+err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetHistoryByID 获取单条批量操作历史
func (c *BatchOperationController) GetHistoryByID(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的历史记录 ID")
		return
	}

	history, err := c.svc.GetHistoryByID(uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "历史记录不存在")
		return
	}

	response.Success(ctx, history, "获取成功")
}

// CancelHistory 取消批量操作
func (c *BatchOperationController) CancelHistory(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的历史记录 ID")
		return
	}

	if errhttp.HandleDBError(ctx, c.svc.CancelHistory(uint(id)), "取消批量操作") {
		return
	}

	response.Success(ctx, nil, "取消成功")
}

// PreviewRequest 批量操作预览请求
type PreviewRequest struct {
	OperationType string   `json:"operation_type" binding:"required"`
	DataType      string   `json:"data_type" binding:"required"`
	IDs           []string `json:"ids"`
}

// Preview 预览批量操作
func (c *BatchOperationController) Preview(ctx *gin.Context) {
	var req PreviewRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	validOps := map[string]bool{
		"import": true, "export": true, "delete": true, "update": true,
	}
	if !validOps[req.OperationType] {
		response.Error(ctx, http.StatusBadRequest, "不支持的操作类型: "+req.OperationType)
		return
	}

	validTypes := map[string]bool{
		"clue": true, "user": true, "account": true,
	}
	if !validTypes[req.DataType] {
		response.Error(ctx, http.StatusBadRequest, "不支持的数据类型: "+req.DataType)
		return
	}

	if (req.OperationType == "delete" || req.OperationType == "update" || req.OperationType == "export") && len(req.IDs) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请选择要操作的记录")
		return
	}

	response.Success(ctx, gin.H{
		"operation_type": req.OperationType,
		"data_type":      req.DataType,
		"selected_count": len(req.IDs),
		"preview":        true,
	}, "预览成功")
}
