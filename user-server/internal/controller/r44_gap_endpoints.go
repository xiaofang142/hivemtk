package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	ksvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// BackupGapController backup 页面端点组
type BackupGapController struct {
	svc *service.BackupGapService
}

// NewBackupGapController 构造
func NewBackupGapController() *BackupGapController {
	return &BackupGapController{svc: service.NewBackupGapServiceFromGlobal()}
}

// List GET /api/backup/list → [{id, createdAt, size, checksum, status, type, name}]（复用既有 backups 表）
func (c *BackupGapController) List(ctx *gin.Context) {
	rows, err := c.svc.ListBackups(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		out = append(out, gin.H{
			"id": r.ID, "name": r.Name, "type": r.Type,
			"status": r.Status, "size": r.Size, "checksum": r.Checksum, "createdAt": r.CreatedAt,
		})
	}
	response.Success(ctx, out, "ok")
}

// Stats GET /api/backup/stats
func (c *BackupGapController) Stats(ctx *gin.Context) {
	st, err := c.svc.Stats(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, st, "ok")
}

// GetStrategy GET /api/backup/strategy
func (c *BackupGapController) GetStrategy(ctx *gin.Context) {
	response.Success(ctx, c.svc.GetStrategy(ctx.Request.Context()), "ok")
}

// SaveStrategy PUT /api/backup/strategy
func (c *BackupGapController) SaveStrategy(ctx *gin.Context) {
	var req service.BackupStrategy
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	if err := c.svc.SaveStrategy(ctx.Request.Context(), &req); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, req, "策略已保存")
}

// Create POST /api/backup/create（复用 BackupService.CreateBackupSimple，异步执行）
func (c *BackupGapController) Create(ctx *gin.Context) {
	backupSvc := service.NewBackupService()
	b, err := backupSvc.CreateBackupSimple(ctx.Request.Context(), 0, "manual-"+time.Now().Format("20060102150405"), "full")
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"id": b.ID, "status": b.Status}, "备份已启动")
}

// RagEvalGapController RAG 评测端点组
type RagEvalGapController struct {
	svc *service.RagEvalGapService
}

// NewRagEvalGapController 构造（检索端口复用既有 RagSearcher 混合检索管线）
func NewRagEvalGapController() *RagEvalGapController {
	searcher := ksvc.NewRagSearcher()
	svc := service.NewRagEvalGapServiceFromGlobal(func(ctx context.Context, productID, query string) ([]string, error) {
		chunks, err := searcher.Search(ctx, query, 5)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(chunks))
		for _, ch := range chunks {
			out = append(out, ch.Content)
		}
		return out, nil
	})
	return &RagEvalGapController{svc: svc}
}

// Latest GET /api/rag/eval/latest
func (c *RagEvalGapController) Latest(ctx *gin.Context) {
	run, err := c.svc.Latest(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, run, "ok")
}

// Runs GET /api/rag/eval/runs?limit=20
func (c *RagEvalGapController) Runs(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	runs, err := c.svc.Runs(ctx.Request.Context(), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, runs, "ok")
}

// Run POST /api/rag/eval/run
func (c *RagEvalGapController) Run(ctx *gin.Context) {
	var body struct {
		ProductID string `json:"product_id"`
	}
	_ = ctx.ShouldBindJSON(&body)
	run, err := c.svc.RunAsync(body.ProductID)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, run, "评测已启动（后台执行，稍后刷新查看结果）")
}

// Upload POST /api/rag/eval/upload (multipart file)
func (c *RagEvalGapController) Upload(ctx *gin.Context) {
	file, _, err := ctx.Request.FormFile("file")
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "缺少 file 字段")
		return
	}
	defer file.Close()
	n, err := c.svc.UploadCSV(ctx.Request.Context(), file, ctx.PostForm("product_id"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"imported": n}, "评估集已上传")
}

// Diff GET /api/rag/eval/diff?baseline=N
func (c *RagEvalGapController) Diff(ctx *gin.Context) {
	bid, err := strconv.ParseUint(ctx.Query("baseline"), 10, 64)
	if err != nil || bid == 0 {
		response.Error(ctx, http.StatusBadRequest, "baseline 必填（run id）")
		return
	}
	res, err := c.svc.Diff(ctx.Request.Context(), uint(bid))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}

// AnalyticsGapController cohort/path 端点组
type AnalyticsGapController struct {
	svc *service.CohortGapService
}

// NewAnalyticsGapController 构造
func NewAnalyticsGapController() *AnalyticsGapController {
	return &AnalyticsGapController{svc: service.NewCohortGapServiceFromGlobal()}
}

// Cohort GET /api/analytics/cohort?period=weekly
func (c *AnalyticsGapController) Cohort(ctx *gin.Context) {
	res, err := c.svc.Cohort(ctx.Request.Context(), 8)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}

// Path GET /api/analytics/path?limit=5
func (c *AnalyticsGapController) Path(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "5"))
	res, err := c.svc.Path(ctx.Request.Context(), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}

// EmailGapController 邮件送达端点组
type EmailGapController struct {
	svc *service.EmailGapService
}

// NewEmailGapController 构造
func NewEmailGapController() *EmailGapController {
	return &EmailGapController{svc: service.NewEmailGapServiceFromGlobal()}
}

// Deliverability GET /api/email/deliverability?days=30
func (c *EmailGapController) Deliverability(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	st, err := c.svc.Deliverability(ctx.Request.Context(), days)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, st, "ok")
}

// BounceBreakdown GET /api/email/bounces/breakdown
func (c *EmailGapController) BounceBreakdown(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "30"))
	res, err := c.svc.BounceBreakdown(ctx.Request.Context(), days)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}

// DomainReputation GET /api/email/domain-reputation
func (c *EmailGapController) DomainReputation(ctx *gin.Context) {
	res, err := c.svc.DomainReputation(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, res, "ok")
}

// SuspendDomain POST /api/email/domains/:id/suspend {domain}（Deliverability.vue 暂停按钮）
// 前端当前只传 row.id；id = 域名列表序号（1-based），按序号回查域名后写入暂停清单
func (c *EmailGapController) SuspendDomain(ctx *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
	}
	_ = ctx.ShouldBindJSON(&req)
	if req.Domain == "" {
		list, err := c.svc.DomainReputation(ctx.Request.Context())
		if HandleServiceError(ctx, err) {
			return
		}
		id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
		if err != nil || id < 1 || int(id) > len(list) {
			response.Error(ctx, http.StatusBadRequest, "无效的域名 ID")
			return
		}
		req.Domain = list[id-1].Domain
	}
	if err := c.svc.SuspendEmailDomain(ctx.Request.Context(), req.Domain); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"domain": req.Domain, "suspended": true}, "已暂停")
}

// TestSend POST /api/email/test-send {subject, html, to[]}
func (c *EmailGapController) TestSend(ctx *gin.Context) {
	var req struct {
		Subject string   `json:"subject" binding:"required"`
		HTML    string   `json:"html" binding:"required"`
		To      []string `json:"to" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	svc := service.NewEmailServiceAuto()
	sent, failed := 0, 0
	var lastErr error
	for _, to := range req.To {
		if _, err := svc.Send(ctx.Request.Context(), 0, to, req.Subject, req.HTML, nil); err != nil {
			failed++
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 && failed > 0 && lastErr != nil {
		response.Error(ctx, http.StatusServiceUnavailable, "测试发送失败（检查 SMTP 配置）: "+lastErr.Error())
		return
	}
	response.Success(ctx, gin.H{"sent": sent, "failed": failed}, "测试发送完成")
}

// RFMMatrix GET /api/user-segments/rfm（矩阵行 [{recency, frequency, count}]）
func (c *EmailGapController) RFMMatrix(ctx *gin.Context) {
	rows, _, err := c.svc.RFMMatrix(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, rows, "ok")
}

// RFMMatrixStats GET /api/user-segments/rfm/stats
func (c *EmailGapController) RFMMatrixStats(ctx *gin.Context) {
	_, stats, err := c.svc.RFMMatrix(ctx.Request.Context())
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, stats, "ok")
}

// DLQBatchRetry POST /api/message-hub/dlq/batch-retry（重试全部死信）
func (c *EmailGapController) DLQBatchRetry(ctx *gin.Context) {
	n, err := c.svc.RetryDeadLetters(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"requeued": n}, "批量重试已入队")
}

// PlaygroundPresets GET /api/knowledge/playground/presets（检索调参预设）
func (c *EmailGapController) PlaygroundPresets(ctx *gin.Context) {
	response.Success(ctx, gin.H{"list": []gin.H{
		{"name": "精确匹配", "top_k": 3, "threshold": 0.85, "rerank": true, "desc": "高阈值低召回——适合 FAQ 精准命中"},
		{"name": "均衡检索", "top_k": 5, "threshold": 0.70, "rerank": true, "desc": "默认推荐配置"},
		{"name": "广泛召回", "top_k": 10, "threshold": 0.50, "rerank": true, "desc": "低阈值高召回——适合开放探索"},
		{"name": "向量优先", "top_k": 5, "threshold": 0.65, "rerank": false, "desc": "语义相似优先，关闭重排降延迟"},
		{"name": "关键词优先", "top_k": 5, "threshold": 0.30, "rerank": true, "desc": "BM25 权重提升——适合型号/编号类查询"},
	}, "total": 5}, "ok")
}

func (c *EmailGapController) ClueApplySuggestions(ctx *gin.Context) {
	var req struct {
		Duplicates []struct {
			ExistingClueID int64          `json:"existingClueId"`
			Row            map[string]any `json:"row"`
		} `json:"duplicates"`
		Action string `json:"action"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}
	res, err := c.svc.ClueApplySuggestions(ctx.Request.Context(), req.Action, req.Duplicates)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"merged": res.Merged, "skipped": res.Skipped, "failed": res.Failed}, "处理完成")
}

// ClueMerge POST /api/clues/:id/merge {from:{...row}}
func (c *EmailGapController) ClueMerge(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(ctx, http.StatusBadRequest, "无效的线索 ID")
		return
	}
	var req struct {
		From map[string]any `json:"from"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil || req.From == nil {
		response.Error(ctx, http.StatusBadRequest, "缺少 from 行数据")
		return
	}
	ok, err := c.svc.ClueMerge(ctx.Request.Context(), id, req.From)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"merged": ok}, "已合并")
}

// ClueForceCreate POST /api/clues/force-create {row}
func (c *EmailGapController) ClueForceCreate(ctx *gin.Context) {
	var row map[string]any
	if err := ctx.ShouldBindJSON(&row); err != nil || row == nil {
		response.Error(ctx, http.StatusBadRequest, "缺少 row 数据")
		return
	}
	ok, err := c.svc.ClueForceCreate(ctx.Request.Context(), row)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"created": ok}, "已强制创建")
}

// BackupPreview GET /api/backup/:id/preview
func (c *BackupGapController) Preview(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	backupSvc := service.NewBackupService()
	b, err := backupSvc.GetBackupByID(ctx.Request.Context(), id)
	if HandleServiceError(ctx, err) {
		return
	}

	out, err := c.svc.PreviewTableStats(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	_ = b
	response.Success(ctx, out, "恢复将覆盖上述表现有数据（备份: "+b.BackupName+"），操作不可逆")
}

// BackupRestore POST /api/backup/:id/restore
func (c *BackupGapController) Restore(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	restoreSvc := service.NewRestoreService()
	rec, err := restoreSvc.RestoreBackup(ctx.Request.Context(), 0, &service.RestoreBackupRequest{BackupID: id})
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, rec, "恢复指令已下发")
}

// BackupDelete DELETE /api/backup/:id
func (c *BackupGapController) Delete(ctx *gin.Context) {
	id, ok := parseUintParam(ctx, "id")
	if !ok {
		return
	}
	backupSvc := service.NewBackupService()
	if err := backupSvc.DeleteBackup(ctx.Request.Context(), id); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"deleted": true}, "已删除")
}
