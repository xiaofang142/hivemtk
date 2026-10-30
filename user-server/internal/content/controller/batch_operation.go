package controller

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/content/model"
	contentrepo "hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/content/service"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// recordBatchHistory 写入一条批量操作历史。
//
// 历史表此前从未被任何端点写入，前端的「操作历史」页因此恒空、取消与详情只能
// 返回 404。历史是旁路观测：写入失败只记日志，绝不回滚已经成功的批量操作。
func recordBatchHistory(ctx *gin.Context, svc *service.BatchOperationService, op, dataType, status string, total, success, failed int, errMsg string, started time.Time) {
	now := time.Now()
	var uid uint
	if v, ok := ctx.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			uid = id
		}
	}
	// 错误信息只留摘要：CSV 逐行失败时 errMsg 可能很长，表上是 text 也不必塞满。
	if len(errMsg) > 900 {
		errMsg = errMsg[:900] + "…"
	}
	h := &model.BatchOperationHistory{
		OperationType: op,
		DataType:      dataType,
		Status:        status,
		TotalCount:    total,
		SuccessCount:  success,
		FailedCount:   failed,
		OperatorID:    uid,
		ErrorMessage:  errMsg,
		StartedAt:     started,
		FinishedAt:    &now,
	}
	if err := svc.RecordHistory(ctx.Request.Context(), h); err != nil {
		logger.Error(err, "写入批量操作历史失败: "+op)
	}
}

// DefaultBatchImportMaxSizeMB 是参数中心 misc.batch_import_max_file_mb 的兜底值。
//
// 取 50 = 接线前这里写死的那个数：兜底必须与接线前的实际行为逐字一致，
// 否则「读不到库」会变成一次静默的行为变更。
const DefaultBatchImportMaxSizeMB = 50

// batchImportMaxSizeMBProvider 由 internal/app 的参数装配层注入。
// 形状与同包的 materialMaxSizeMBProvider 一致：本子模块不在 controller 里直接摸参数服务。
var batchImportMaxSizeMBProvider = func() int { return DefaultBatchImportMaxSizeMB }

// SetBatchImportMaxSizeMBProvider 注入批量导入单文件大小上限（MB）的读取口，仅装配层调用。
// 传 nil 是「不注入」而不是「复位」（与全仓 setter 同一语义）。
func SetBatchImportMaxSizeMBProvider(fn func() int) {
	if fn != nil {
		batchImportMaxSizeMBProvider = fn
	}
}

// batchImportMaxUploadSize 返回生效中的批量导入文件上限（字节与 MB，报错文案要用后者）。
//
// 读到 <=0 视为「未配置」回落到兜底：这条的 Min 是 1，界面配不出 0；
// 真按 0 放行判定会让任何带字节的文件都被拒，排查方向从「文件太大」变成「导入全废」。
func batchImportMaxUploadSize() (int64, int) {
	mb := batchImportMaxSizeMBProvider()
	if mb <= 0 {
		mb = DefaultBatchImportMaxSizeMB
	}
	return int64(mb) * 1024 * 1024, mb
}

// ProbeBatchImportMaxUploadSize 暴露读取口当前值，供装配层测试断言接线确实生效。
func ProbeBatchImportMaxUploadSize() (int64, int) { return batchImportMaxUploadSize() }

// BatchImportController 批量导入控制器
type BatchImportController struct {
	svc *service.BatchOperationService
}

// NewBatchImportController 创建批量导入控制器
func NewBatchImportController() *BatchImportController {
	return &BatchImportController{
		svc: service.NewBatchOperationService(db.GetDB()),
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
		// 原文是 http: no such file / request Content-Type isn't multipart —— 对用户没有信息量
		response.Error(ctx, http.StatusBadRequest, "上传文件失败：请选择要导入的文件（multipart 字段名 file）")
		return
	}

	maxBytes, maxMB := batchImportMaxUploadSize()
	if fileHeader.Size > maxBytes {
		response.Error(ctx, http.StatusBadRequest, fmt.Sprintf("文件大小超过 %dMB 限制", maxMB))
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "打开上传文件失败："+err.Error())
		return
	}
	defer func() { _ = f.Close() }()

	started := time.Now()
	result, err := c.svc.ImportFromCSV(ctx.Request.Context(), service.ImportType(importType), f)
	if err != nil {
		if errors.Is(err, service.ErrUnsupportedImportType) {
			response.Error(ctx, http.StatusBadRequest, "导入失败："+err.Error())
			return
		}
		recordBatchHistory(ctx, c.svc, "import", importType, "failed", 0, 0, 0, err.Error(), started)
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

	status := "completed"
	if result.Failed > 0 {
		status = "partial"
	}
	errSummary := ""
	if len(resp.Errors) > 0 {
		errSummary = strings.Join(resp.Errors, "; ")
	}
	recordBatchHistory(ctx, c.svc, "import", importType, status, result.Total, result.Success, result.Failed, errSummary, started)

	response.Success(ctx, resp, "导入完成")
}

// DownloadTemplate 下载导入模板
func (c *BatchImportController) DownloadTemplate(ctx *gin.Context) {
	importType := ctx.Query("type")
	if importType == "" {
		importType = string(service.ImportTypeClue)
	}

	// 模板只有 CSV 一种形态：以前把 format 直接拼进文件名（format=md 会得到
	// 一个叫 .md 的 CSV 文件），扩展名与内容不符。
	format := ctx.Query("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" {
		response.Error(ctx, http.StatusBadRequest, "暂不支持的模板格式: "+format+"（仅支持 csv）")
		return
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
		svc: service.NewBatchOperationService(db.GetDB()),
	}
}

// ExportData 导出数据
func (c *BatchExportController) ExportData(ctx *gin.Context) {
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

	// 类型失败是用户传参问题（400），不是服务端故障：先挡在流式写出之前，
	// 否则 GenerateCSV 之类返回的哨兵会被兜成 500「导出失败」。
	switch exportType {
	case string(service.ExportTypeClue), string(service.ExportTypeUser):
	default:
		response.Error(ctx, http.StatusBadRequest, "暂不支持的导出类型: "+exportType+"（支持 clue / user）")
		return
	}

	started := time.Now()
	record := func(status, errMsg string) {
		success, failed := len(ids), 0
		if status == "failed" {
			success, failed = 0, len(ids)
		}
		recordBatchHistory(ctx, c.svc, "export", exportType, status, len(ids), success, failed, errMsg, started)
	}

	switch format {
	case "csv":
		buf, err := c.svc.GenerateCSV(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			record("failed", err.Error())
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export_%s.csv", exportType, strconv.FormatInt(int64(buf.Len()), 10))
		ctx.Header("Content-Type", "text/csv; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "text/csv", buf.Bytes())
		record("completed", "")
	case "jsonl":
		buf, err := c.svc.GenerateJSONL(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			record("failed", err.Error())
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export.jsonl", exportType)
		ctx.Header("Content-Type", "application/jsonl; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "application/jsonl", buf.Bytes())
		record("completed", "")
	case "markdown":
		buf, err := c.svc.GenerateMarkdown(ctx.Request.Context(), service.ExportType(exportType), ids)
		if err != nil {
			record("failed", err.Error())
			response.Error(ctx, http.StatusInternalServerError, "导出失败："+err.Error())
			return
		}
		filename := fmt.Sprintf("%s_export.md", exportType)
		ctx.Header("Content-Type", "text/markdown; charset=utf-8")
		ctx.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		ctx.Data(http.StatusOK, "text/markdown", buf.Bytes())
		record("completed", "")
	default:
		response.Error(ctx, http.StatusBadRequest, "暂不支持的导出格式: "+format+"（支持 csv / jsonl / markdown）")
	}
}

// BatchOperationController 批量操作控制器
type BatchOperationController struct {
	svc *service.BatchOperationService
}

// NewBatchOperationController 创建批量操作控制器
func NewBatchOperationController() *BatchOperationController {
	return &BatchOperationController{
		svc: service.NewBatchOperationService(db.GetDB()),
	}
}

// BatchDeleteRequest 批量删除请求
type BatchDeleteRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

// BatchDelete 批量删除
func (c *BatchOperationController) BatchDelete(ctx *gin.Context) {
	var req BatchDeleteRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：ids 必须是非空字符串数组")
		return
	}

	if len(req.IDs) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请选择要删除的记录")
		return
	}

	started := time.Now()
	count, err := c.svc.BatchDeleteClues(ctx.Request.Context(), req.IDs)
	if err != nil {
		recordBatchHistory(ctx, c.svc, "delete", "clue", "failed", len(req.IDs), 0, len(req.IDs), err.Error(), started)
		response.Error(ctx, http.StatusInternalServerError, "删除失败："+err.Error())
		return
	}
	recordBatchHistory(ctx, c.svc, "delete", "clue", "completed", len(req.IDs), count, len(req.IDs)-count, "", started)

	if count == 0 {
		// 一条都没删掉却回「批量删除成功」，前端只会给个绿 toast，用户无从发现
		response.Error(ctx, http.StatusBadRequest, "没有匹配到可删除的线索：所选 ID 均不存在或已被删除")
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
	var req BatchUpdateRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：ids 必须是非空字符串数组，fields 必须是对象")
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

	started := time.Now()
	count, err := c.svc.BatchUpdateClues(ctx.Request.Context(), &service.BatchUpdateRequest{
		IDs:    req.IDs,
		Fields: req.Fields,
	})
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "更新字段不能为空") || strings.Contains(errMsg, "无可更新字段") {
			response.Error(ctx, http.StatusBadRequest, errMsg)
			return
		}
		recordBatchHistory(ctx, c.svc, "update", "clue", "failed", len(req.IDs), 0, len(req.IDs), errMsg, started)
		response.Error(ctx, http.StatusInternalServerError, "更新失败："+errMsg)
		return
	}
	recordBatchHistory(ctx, c.svc, "update", "clue", "completed", len(req.IDs), count, len(req.IDs)-count, "", started)

	if count == 0 {
		response.Error(ctx, http.StatusBadRequest, "没有匹配到可更新的线索：所选 ID 均不存在或已被删除")
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

	list, total, err := c.svc.GetHistories(ctx.Request.Context(), page, pageSize)
	if errhttp.HandleDBError(ctx, err, "获取批量操作历史") {
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

	history, err := c.svc.GetHistoryByID(ctx.Request.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, "操作记录不存在")
			return
		}
		errhttp.HandleDBError(ctx, err, "获取批量操作历史")
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

	if err := c.svc.CancelHistory(ctx.Request.Context(), uint(id)); err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			response.NotFound(ctx, "操作记录不存在")
		case errors.Is(err, contentrepo.ErrBatchOperationNotCancellable):
			// 记录在，只是状态不允许（已完成/已取消）：404 会让用户以为记录被删了
			response.Error(ctx, http.StatusConflict, err.Error())
		default:
			errhttp.HandleDBError(ctx, err, "取消批量操作")
		}
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：operation_type 与 data_type 必填")
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
