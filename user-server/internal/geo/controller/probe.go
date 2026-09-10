package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// ProbeController 多引擎探针调试控制器
type ProbeController struct {
	probeSvc *service.ProbeService
}

// NewProbeController 构造探针控制器
func NewProbeController(probeSvc *service.ProbeService) *ProbeController {
	return &ProbeController{probeSvc: probeSvc}
}

// TestSingle 调试单引擎探针
// POST /geo/probe/test  body: {"engine":"openai","query":"..."}
func (c *ProbeController) TestSingle(ctx *gin.Context) {
	var body struct {
		Engine string `json:"engine"`
		Query  string `json:"query"`
	}
	if !response.BindJSON(ctx, &body) {
		return
	}
	if body.Query == "" {
		response.Error(ctx, http.StatusBadRequest, "query 必填")
		return
	}
	result, err := c.probeSvc.TestSingle(ctx.Request.Context(), body.Engine, body.Query)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, result, "ok")
}

// ProbeAll 触发所有引擎探针
// POST /geo/probe/all  body: {"query":"..."}
func (c *ProbeController) ProbeAll(ctx *gin.Context) {
	var body struct {
		Query string `json:"query"`
	}
	if !response.BindJSON(ctx, &body) {
		return
	}
	if body.Query == "" {
		response.Error(ctx, http.StatusBadRequest, "query 必填")
		return
	}
	runs, errs := c.probeSvc.ProbeAllEngines(ctx.Request.Context(), body.Query)
	response.Success(ctx, gin.H{
		"runs":   runs,
		"errors": errlistStrings(errs),
		"total":  len(runs),
		"failed": len(errs),
	}, "ok")
}

func errlistStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

// RunNegativeMonitor 手动触发负面监控
// POST /geo/probe/run-negative
func (c *ProbeController) RunNegativeMonitor(ctx *gin.Context) {
	if _, err := service.GetGeoJobManager().Trigger(service.JobNegativeMonitor); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"started": true}, "已启动负面监控任务")
}

// RunSOVRefresh 手动触发 SOV 刷新（SovBoard.vue 刷新按钮依赖）
// POST /geo/probe/run-sov
func (c *ProbeController) RunSOVRefresh(ctx *gin.Context) {
	if _, err := service.GetGeoJobManager().Trigger(service.JobSOVRefresh); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"started": true}, "已启动 SOV 刷新任务")
}

// RunSourceSync 手动触发信源目录同步
// POST /geo/probe/run-source-sync
func (c *ProbeController) RunSourceSync(ctx *gin.Context) {
	if _, err := service.GetGeoJobManager().Trigger(service.JobSourceSync); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, gin.H{"started": true}, "已启动信源同步任务")
}

// ListAvailableEngines 返回当前装配的所有引擎名
// GET /geo/probe/engines
func (c *ProbeController) ListAvailableEngines(ctx *gin.Context) {
	engines := service.NewEngineProbes()
	names := make([]string, 0, len(engines))
	for _, e := range engines {
		names = append(names, e.Name())
	}
	response.Success(ctx, names, "ok")
}

// ListRuns 最近探针运行记录（支持引擎过滤）
// GET /geo/probe/runs?limit=20&engine=
func (c *ProbeController) ListRuns(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	engine := ctx.Query("engine")
	if engine != "" {
		list, _, err := c.probeSvc.ListRunsByEngine(ctx.Request.Context(), engine, 1, limit)
		if err != nil {
			response.ErrorFromDB(ctx, err, "查询探针记录失败")
			return
		}
		response.Success(ctx, list, "ok")
		return
	}
	list, err := c.probeSvc.ListRuns(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询探针记录失败")
		return
	}
	response.Success(ctx, list, "ok")
}

// SourceCatalogController 信源目录控制器
type SourceCatalogController struct {
	crawlerSvc *service.CrawlerService
}

// NewSourceCatalogController 构造信源目录控制器
func NewSourceCatalogController(crawlerSvc *service.CrawlerService) *SourceCatalogController {
	return &SourceCatalogController{crawlerSvc: crawlerSvc}
}

// ListLevels 根据 query 查 SOV 用 — 直接用 crawler.LookupSourceLevel
// GET /geo/source-catalog/levels?url=...
func (c *SourceCatalogController) LookupLevels(ctx *gin.Context) {
	url := ctx.Query("url")
	if url == "" {
		response.Error(ctx, http.StatusBadRequest, "url 必填")
		return
	}
	level, sc, err := c.crawlerSvc.LookupSourceLevel(ctx.Request.Context(), url)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(ctx, gin.H{"level": level, "source": sc}, "ok")
}

// EntityController 实体图谱控制器（读路径；写入由 entity_extractor 定时任务负责）
type EntityController struct {
	entityRepo repository.GeoEntityRepository
	extractor  *service.EntityExtractorService
}

// NewEntityController 构造实体控制器
func NewEntityController(er repository.GeoEntityRepository, ex *service.EntityExtractorService) *EntityController {
	return &EntityController{entityRepo: er, extractor: ex}
}

// ListEntities 查询实体列表
// GET /geo/entity/list?search=&type=&page=&limit=
func (c *EntityController) ListEntities(ctx *gin.Context) {
	search := ctx.Query("search")
	entityType := ctx.Query("type")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	list, total, err := c.entityRepo.List(ctx.Request.Context(), search, entityType, page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询实体列表失败")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total, "page": page, "page_size": limit}, "ok")
}

// GetGraph 查询实体关系子图
// GET /geo/entity/:id/graph?depth=2
func (c *EntityController) GetGraph(ctx *gin.Context) {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 64)
	depth, _ := strconv.Atoi(ctx.DefaultQuery("depth", "2"))
	if depth <= 0 || depth > 5 {
		depth = 2
	}
	if id == 0 {
		response.Error(ctx, http.StatusBadRequest, "id 无效")
		return
	}
	rels, entities, err := c.entityRepo.GetRelationGraph(ctx.Request.Context(), uint(id), depth)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询实体关系图失败")
		return
	}
	response.Success(ctx, gin.H{"entity_id": id, "depth": depth, "entities": entities, "relations": rels}, "ok")
}

// Extract 从指定文档抽取实体
// POST /geo/entities/extract
func (c *EntityController) Extract(ctx *gin.Context) {
	var body struct {
		DocID string `json:"doc_id" binding:"required"`
	}
	if !response.BindJSON(ctx, &body) {
		return
	}
	err := c.extractor.ExtractFromDocument(ctx.Request.Context(), body.DocID)
	if err != nil {
		response.BusinessError(ctx, fmt.Sprintf("实体抽取失败: %v", err))
		return
	}
	response.Success(ctx, gin.H{"doc_id": body.DocID, "status": "completed"}, "实体抽取完成")
}
