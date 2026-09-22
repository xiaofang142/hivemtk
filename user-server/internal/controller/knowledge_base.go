package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// KnowledgeBaseController 知识库控制器
type KnowledgeBaseController struct {
	svc *service.KnowledgeBaseService
}

// NewKnowledgeBaseController 创建知识库控制器
func NewKnowledgeBaseController() *KnowledgeBaseController {
	return &KnowledgeBaseController{
		svc: service.NewKnowledgeBaseServiceDefault(),
	}
}

// RegisterRoutes 注册路由
func (c *KnowledgeBaseController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/knowledge-bases")
	{
		g.GET("", c.List)
		g.GET("/by-agent/:aid", c.ListByAgent)
		g.GET("/by-type/:type", c.ListByType)
		g.GET("/:id", c.Get)
		g.GET("/:id/stats", c.Stats)
		g.POST("", c.Create)
		g.PUT("/:id", c.Update)
		g.DELETE("/:id", c.Delete)
		g.POST("/:id/bind", c.BindToAgent)
		g.POST("/:id/unbind", c.UnbindFromAgent)
		// 版本与灰度（T-P2-05 / G-1）：三处都只动 knowledge_bases 的版本三列与
		// rag_answer_cache 的命名空间，不碰内容表。
		g.GET("/:id/versions", c.VersionInfo)
		g.POST("/:id/version", c.SwitchVersion)
		g.PUT("/:id/canary", c.PutCanary)
	}
}

// List godoc
// @Summary      知识库列表
// @Description  按类型/所有者/关键词分页查询知识库
// @Tags         Knowledge Base
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        type        query  string  false  "知识库类型：faq/rag/sop"
// @Param        owner_type  query  string  false  "所有者类型"
// @Param        agent_id    query  int     false  "绑定的智能体 ID"
// @Param        keyword     query  string  false  "关键词"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/knowledge-bases [get]
func (c *KnowledgeBaseController) List(ctx *gin.Context) {
	kbType := ctx.Query("type")
	ownerType := ctx.Query("owner_type")
	keyword := ctx.Query("keyword")
	var agentID uint
	if v := ctx.Query("agent_id"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest, "无效的 agent_id")
			return
		}
		agentID = uint(n)
	}
	list, total, err := c.svc.ListKBs(ctx.Request.Context(), kbType, ownerType, agentID, keyword)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"list":  list,
		"total": total,
	}, "查询成功")
}

// Get godoc
// @Summary      知识库详情
// @Description  根据 ID 返回知识库完整定义
// @Tags         Knowledge Base
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "知识库 ID"
// @Success      200  {object}  response.Response  "成功"
// @Failure      404  {object}  response.Response  "未找到"
// @Router       /api/knowledge-bases/{id} [get]
func (c *KnowledgeBaseController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	kb, err := c.svc.GetKB(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "知识库不存在")
		return
	}
	if kb == nil {
		response.NotFound(ctx, "知识库不存在")
		return
	}
	response.Success(ctx, kb, "查询成功")
}

// Stats 知识库统计（前端 KBDrawer: GET /api/knowledge-bases/:id/stats）
func (c *KnowledgeBaseController) Stats(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	stats, err := c.svc.GetKBStats(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if stats == nil {
		response.NotFound(ctx, "知识库不存在")
		return
	}
	response.Success(ctx, stats, "查询成功")
}

// ListByAgent 查某智能体可用的知识库
func (c *KnowledgeBaseController) ListByAgent(ctx *gin.Context) {
	aid, err := strconv.ParseUint(ctx.Param("aid"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的智能体 ID")
		return
	}
	if aid == 0 {
		response.Error(ctx, http.StatusBadRequest, "agent_id 必填且 > 0")
		return
	}
	list, err := c.svc.ListByAgent(ctx.Request.Context(), uint(aid))
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, list, "查询成功")
}

// ListByType 按类型查
func (c *KnowledgeBaseController) ListByType(ctx *gin.Context) {
	kbType := ctx.Param("type")
	if !service.IsValidKBType(kbType) {
		response.Error(ctx, http.StatusBadRequest, "type 必须为 faq/rag/sop")
		return
	}
	list, err := c.svc.ListByType(ctx.Request.Context(), kbType)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, list, "查询成功")
}

type knowledgeBaseCreateReq struct {
	KBCode       string `json:"kb_code" binding:"required"`
	Type         string `json:"type" binding:"required"`
	Name         string `json:"name" binding:"required"`
	Description  string `json:"description"`
	OwnerType    string `json:"owner_type"`
	OwnerAgentID *uint  `json:"owner_agent_id"`
	Enabled      *bool  `json:"enabled"`
}

// Create 新增
func (c *KnowledgeBaseController) Create(ctx *gin.Context) {
	var req knowledgeBaseCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	kb := &model.KnowledgeBase{
		KBCode:       req.KBCode,
		Type:         req.Type,
		Name:         req.Name,
		Description:  req.Description,
		OwnerType:    req.OwnerType,
		OwnerAgentID: req.OwnerAgentID,
		Enabled:      req.Enabled,
	}
	if err := c.svc.CreateKB(ctx.Request.Context(), kb); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, kb, "创建成功")
}

type knowledgeBaseUpdateReq struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	OwnerType    string `json:"owner_type"`
	OwnerAgentID *uint  `json:"owner_agent_id"`
	Enabled      *bool  `json:"enabled"`
}

// Update 更新
func (c *KnowledgeBaseController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	var req knowledgeBaseUpdateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	kb := &model.KnowledgeBase{
		Type:         req.Type,
		Name:         req.Name,
		Description:  req.Description,
		OwnerType:    req.OwnerType,
		OwnerAgentID: req.OwnerAgentID,
		Enabled:      req.Enabled,
	}
	if err := c.svc.UpdateKB(ctx.Request.Context(), uint(id), kb); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "更新成功")
}

// Delete 删除 (业务级联: 同步删除 agent_kb_bindings)
func (c *KnowledgeBaseController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	if err := c.svc.DeleteKB(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

type knowledgeBaseBindReq struct {
	AgentID uint `json:"agent_id" binding:"required"`
}

// BindToAgent 绑定到智能体
func (c *KnowledgeBaseController) BindToAgent(ctx *gin.Context) {
	kbID, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	var req knowledgeBaseBindReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.AgentID == 0 {
		response.Error(ctx, http.StatusBadRequest, "agent_id 必填且 > 0")
		return
	}
	if err := c.svc.BindToAgent(ctx.Request.Context(), uint(kbID), req.AgentID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"kb_id": kbID, "agent_id": req.AgentID}, "绑定成功")
}

// UnbindFromAgent 从智能体解绑
func (c *KnowledgeBaseController) UnbindFromAgent(ctx *gin.Context) {
	kbID, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	var req knowledgeBaseBindReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.AgentID == 0 {
		response.Error(ctx, http.StatusBadRequest, "agent_id 必填且 > 0")
		return
	}
	if err := c.svc.UnbindFromAgent(ctx.Request.Context(), uint(kbID), req.AgentID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"kb_id": kbID, "agent_id": req.AgentID}, "解绑成功")
}

// knowledgeBaseVersionSwitchReq 切版本请求。
//
// version 省略或 0 = 把当前灰度版本转正（Version+1）；显式给正整数 = 激活该号
// （含退回旧号，即回滚）。负数按参数错误拒掉，不当"未传"处理。
type knowledgeBaseVersionSwitchReq struct {
	Version *int `json:"version"`
}

// SwitchVersion 切版本（转正 / 回滚），语义详见 service.PublishKBVersion。
//
// 切完直接回读整份版本信息，运营不用二次查询就能看到"新稳定命名空间是哪个号、
// 两个版本的行是否都还在"。
func (c *KnowledgeBaseController) SwitchVersion(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	var req knowledgeBaseVersionSwitchReq
	if err := ctx.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	target := 0
	if req.Version != nil {
		target = *req.Version
	}
	if target < 0 {
		// 负号在这里没有"未传"的含义，判 400 而不是让 service 的错误经 ErrorFromDB 变 500。
		response.Error(ctx, http.StatusBadRequest, "version 不能为负")
		return
	}
	kb, err := c.svc.PublishKBVersion(ctx.Request.Context(), uint(id), target)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	info, err := c.svc.KBVersionInfo(ctx.Request.Context(), kb.ID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, info, "版本已切换")
}

// knowledgeBaseCanaryReq 灰度放量参数。
//
// enabled=true + percent=0 是合法组合（先把参数配好、比例随后抬），
// 与"没开灰度"在行为上一致：都不放量。
type knowledgeBaseCanaryReq struct {
	Enabled *bool `json:"enabled" binding:"required"`
	Percent *int  `json:"percent" binding:"required"`
}

// PutCanary 设置灰度比例（只改参数，不动内容，也不 bump updated_at）。
func (c *KnowledgeBaseController) PutCanary(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	var req knowledgeBaseCanaryReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if *req.Percent < 0 || *req.Percent > 100 {
		response.Error(ctx, http.StatusBadRequest, "percent 必须在 [0,100] 区间内")
		return
	}
	kb, err := c.svc.SetKBCanary(ctx.Request.Context(), uint(id), *req.Enabled, *req.Percent)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	info, err := c.svc.KBVersionInfo(ctx.Request.Context(), kb.ID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, info, "灰度配置已更新")
}

// VersionInfo 读版本与灰度现状 + rag_answer_cache 各命名空间行数。
//
// 缺行口径与同文件的 Get 一致：service 回 (nil, nil) ⇒ response.NotFound。
func (c *KnowledgeBaseController) VersionInfo(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的知识库 ID")
		return
	}
	info, err := c.svc.KBVersionInfo(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	if info == nil {
		response.NotFound(ctx, "知识库不存在")
		return
	}
	response.Success(ctx, info, "查询成功")
}
