// Package controller T4 知识库冷启动：爬取源 / 批次 / QA 候选 HTTP 接口（§6）。
//
// 风格对齐 controller/knowledge_base.go：controller 薄、service 收口业务、
// 中文错误、response.Success/Error 包裹。
package controller

import (
	"context"
	"strconv"

	kbservice "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// KBCrawlerController 爬取源控制器
type KBCrawlerController struct {
	svc *kbservice.CrawlerService
	qa  *kbservice.QASplitService
}

// NewKBCrawlerController 构造
func NewKBCrawlerController(svc *kbservice.CrawlerService, qa *kbservice.QASplitService) *KBCrawlerController {
	return &KBCrawlerController{svc: svc, qa: qa}
}

// RegisterRoutes 挂路由（挂到既有 auth 组，路径前缀 /kb/crawler 与 /kb/qa）
func (ctrl *KBCrawlerController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/kb/crawler")
	{
		g.GET("/sources", ctrl.ListSources)
		g.POST("/sources", ctrl.CreateSource)
		g.PUT("/sources/:id", ctrl.UpdateSource)
		g.POST("/sources/:id/pause", ctrl.PauseSource)
		g.POST("/sources/:id/resume", ctrl.ResumeSource)
		g.POST("/sources/:id/verify", ctrl.VerifySite)
		g.DELETE("/sources/:id", ctrl.DeleteSource)
		g.POST("/sources/:id/sync", ctrl.SyncSource)
		g.GET("/batches", ctrl.ListBatches)
		g.POST("/batches/:id/retry", ctrl.RetryBatch)
	}
	qag := router.Group("/kb/qa")
	{
		qag.GET("/candidates", ctrl.ListQACandidates)
		qag.POST("/candidates/:id/approve", ctrl.ApproveQACandidate)
		qag.POST("/candidates/:id/reject", ctrl.RejectQACandidate)
		qag.POST("/candidates/:id/publish", ctrl.PublishQACandidate)
	}
}

func pathUint(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	return uint(v), err
}

func pathUint64(c *gin.Context, name string) (uint64, error) {
	return strconv.ParseUint(c.Param(name), 10, 64)
}

// ListSources GET /sources?kb_id=&status=
func (ctrl *KBCrawlerController) ListSources(c *gin.Context) {
	kbID, _ := strconv.ParseUint(c.Query("kb_id"), 10, 64)
	out, err := ctrl.svc.ListSources(c.Request.Context(), uint(kbID), c.Query("status"))
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	response.Success(c, out, "ok")
}

// CreateSource POST /sources
func (ctrl *KBCrawlerController) CreateSource(c *gin.Context) {
	var in kbservice.CreateSourceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	src, err := ctrl.svc.CreateSource(c.Request.Context(), &in)
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	response.Success(c, src, "创建成功；请先完成站点验证（hivemtk-verify.txt 或 meta 标签）再触发同步")
}

// UpdateSource PUT /sources/:id
func (ctrl *KBCrawlerController) UpdateSource(c *gin.Context) {
	id, err := pathUint(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	var fields map[string]any
	if err := c.ShouldBindJSON(&fields); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	if err := ctrl.svc.UpdateSource(c.Request.Context(), id, fields); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	response.Success(c, nil, "ok")
}

// PauseSource POST /sources/:id/pause
func (ctrl *KBCrawlerController) PauseSource(c *gin.Context) {
	ctrl.transition(c, "paused")
}

// ResumeSource POST /sources/:id/resume
func (ctrl *KBCrawlerController) ResumeSource(c *gin.Context) {
	ctrl.transition(c, "active")
}

func (ctrl *KBCrawlerController) transition(c *gin.Context, to string) {
	id, err := pathUint(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	if err := ctrl.svc.TransitionSource(c.Request.Context(), id, to); err != nil {
		response.Error(c, 409, err.Error())
		return
	}
	response.Success(c, nil, "ok")
}

// VerifySite POST /sources/:id/verify
func (ctrl *KBCrawlerController) VerifySite(c *gin.Context) {
	id, err := pathUint(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	if err := ctrl.svc.VerifySite(c.Request.Context(), id); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	response.Success(c, nil, "验证通过")
}

// DeleteSource DELETE /sources/:id
func (ctrl *KBCrawlerController) DeleteSource(c *gin.Context) {
	id, err := pathUint(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	if err := ctrl.svc.DeleteSource(c.Request.Context(), id); err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	response.Success(c, nil, "ok")
}

// SyncSource POST /sources/:id/sync（等价 Chatbase "Resync now"）
func (ctrl *KBCrawlerController) SyncSource(c *gin.Context) {
	id, err := pathUint(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	batch, err := ctrl.svc.CreateBatch(c.Request.Context(), id, "manual")
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	// 手动同步异步执行（§4.2）：接口只负责建 pending 批次并立即返回，
	// 实际爬取由后台 goroutine 执行（cron worker 同理）。
	go func(batchID uint64) {
		if err := ctrl.svc.RunBatch(context.Background(), batchID); err != nil {
			logger.Warnf("[kb_crawler] 手动同步批次 %d 执行失败: %v", batchID, err)
		}
	}(batch.ID)
	response.Success(c, gin.H{"batch_no": batch.BatchNo, "status": batch.Status}, "同步已排队")
}

// ListBatches GET /batches?source_id=&page=&page_size=
func (ctrl *KBCrawlerController) ListBatches(c *gin.Context) {
	sourceID, _ := strconv.ParseUint(c.Query("source_id"), 10, 64)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	out, total, err := ctrl.svc.ListBatches(c.Request.Context(), uint(sourceID), page, pageSize)
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	response.Success(c, gin.H{"items": out, "total": total}, "ok")
}

// RetryBatch POST /batches/:id/retry
func (ctrl *KBCrawlerController) RetryBatch(c *gin.Context) {
	id, err := pathUint64(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	batch, err := ctrl.svc.RetryBatch(c.Request.Context(), id)
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	response.Success(c, batch, "重试已排队")
}

// ListQACandidates GET /kb/qa/candidates?source_type=&status=&page=&page_size=
func (ctrl *KBCrawlerController) ListQACandidates(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	out, total, err := ctrl.qa.ListQACandidates(c.Request.Context(),
		c.Query("source_type"), c.Query("status"), page, pageSize)
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	response.Success(c, gin.H{"items": out, "total": total}, "ok")
}

// ApproveQACandidate POST /candidates/:id/approve
func (ctrl *KBCrawlerController) ApproveQACandidate(c *gin.Context) { ctrl.review(c, "approved") }

// RejectQACandidate POST /candidates/:id/reject
func (ctrl *KBCrawlerController) RejectQACandidate(c *gin.Context) { ctrl.review(c, "rejected") }

func (ctrl *KBCrawlerController) review(c *gin.Context, to string) {
	id, err := pathUint64(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	if err := ctrl.qa.ReviewQACandidate(c.Request.Context(), id, to, c.GetString("username")); err != nil {
		response.Error(c, 409, err.Error())
		return
	}
	response.Success(c, nil, "ok")
}

// PublishQACandidate POST /candidates/:id/publish（approved → indexed 落库）
func (ctrl *KBCrawlerController) PublishQACandidate(c *gin.Context) {
	id, err := pathUint64(c, "id")
	if err != nil {
		response.Error(c, 400, "id 非法")
		return
	}
	if err := ctrl.qa.PublishQACandidate(c.Request.Context(), id, ctrl.svc.KB()); err != nil {
		response.Error(c, 409, err.Error())
		return
	}
	response.Success(c, nil, "已发布入库，向量化处理已启动")
}
