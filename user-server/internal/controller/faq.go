package controller

import (
	"net/http"
	"strconv"
	"strings"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// FAQController FAQ 知识库控制器
type FAQController struct {
	svc *service.FAQService
}

// NewFAQController 创建 FAQ 控制器
func NewFAQController() *FAQController {
	return &FAQController{svc: service.NewFAQServiceDefault()}
}

// RegisterRoutes 注册路由
func (c *FAQController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/faqs")
	{
		g.POST("/annotate", c.AnnotateFromSession)
		g.GET("", c.List)
		g.GET("/stats", c.Stats)
		g.GET("/:id", c.Get)
		g.POST("", c.Create)
		g.PUT("/:id", c.Update)
		g.PATCH("/:id/toggle", c.Toggle)
		g.DELETE("/:id", c.Delete)
		g.POST("/match", c.Match)
	}
}

// List godoc
// @Summary      FAQ 列表
// @Description  按关键词/分类/意图分页查询 FAQ 词条
// @Tags         FAQ
// @Produce      json
// @Security     BearerAuth
// @Param        keyword  query  string  false  "关键词"
// @Param        category query  string  false  "分类"
// @Param        intent   query  string  false  "意图编码"
// @Param        enabled  query  bool    false  "是否启用"
// @Param        page     query  int     false  "页码"  default(1)
// @Param        page_size query int    false  "每页"  default(20)
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/faqs [get]
func (c *FAQController) List(ctx *gin.Context) {
	// P0-3：kb_id 不许被静默忽略。faq_entries 没有 kb_id 归属列（见 model/faq_entry.go），
	// 本端点只能给全局列表；调用方传 kb_id 时期待的是"这个知识库的内容"，把参数
	// 丢掉就变成"不报错但显示错数据"——比 404 更隐蔽。显式拒绝并指路。
	if strings.TrimSpace(ctx.Query("kb_id")) != "" {
		response.Error(ctx, http.StatusBadRequest,
			"kb_id 过滤不支持：FAQ 列表是全局列表（faq_entries 无 kb_id 归属）；按知识库预览请用 GET /api/knowledge-bases/:id/items")
		return
	}
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	filter := dto.FAQFilter{
		Keyword:  ctx.Query("keyword"),
		Category: ctx.Query("category"),
		Intent:   ctx.Query("intent"),
		Page:     page,
		PageSize: pageSize,
	}
	if enabledStr := ctx.Query("enabled"); enabledStr != "" {
		if v, err := strconv.ParseBool(enabledStr); err == nil {
			filter.Enabled = &v
		}
	}
	list, total, err := c.svc.List(ctx.Request.Context(), filter)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.SuccessWithPage(ctx, list, int64(page), int64(pageSize), total)
}

// Get godoc
// @Summary      FAQ 详情
// @Description  根据 ID 返回 FAQ 词条完整内容
// @Tags         FAQ
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "FAQ ID"
// @Success      200  {object}  response.Response  "成功"
// @Failure      404  {object}  response.Response  "未找到"
// @Router       /api/faqs/{id} [get]
func (c *FAQController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 FAQ ID")
		return
	}
	entry, err := c.svc.GetByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(ctx, "FAQ 不存在")
		return
	}
	response.Success(ctx, entry, "查询成功")
}

type faqCreateReq struct {
	Question   string   `json:"question" binding:"required"`
	Answer     string   `json:"answer" binding:"required"`
	Keywords   []string `json:"keywords"`
	Category   string   `json:"category"`
	Intent     string   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Enabled    *bool    `json:"enabled"`
	AgentID    uint     `json:"agent_id" binding:"required"`
}

// Create godoc
// @Summary      新增 FAQ
// @Description  创建一个 FAQ 词条并绑定到指定智能体
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  faqCreateReq  true  "FAQ 内容"
// @Success      200   {object}  response.Response  "创建成功"
// @Router       /api/faqs [post]
func (c *FAQController) Create(ctx *gin.Context) {
	var req faqCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agentID := req.AgentID
	entry := &model.FAQEntry{
		Question:   req.Question,
		Answer:     req.Answer,
		Keywords:   req.Keywords,
		Category:   req.Category,
		Intent:     req.Intent,
		Confidence: req.Confidence,
		Enabled:    req.Enabled,
		AgentID:    &agentID,
	}
	if err := c.svc.Create(ctx.Request.Context(), entry); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, entry, "创建成功")
}

// Update godoc
// @Summary      更新 FAQ
// @Description  更新 FAQ 词条内容或绑定
// @Tags         FAQ
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  int            true  "FAQ ID"
// @Param        body  body  faqCreateReq   true  "更新内容"
// @Success      200   {object}  response.Response  "更新成功"
// @Router       /api/faqs/{id} [put]
func (c *FAQController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 FAQ ID")
		return
	}
	var req faqCreateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	agentID := req.AgentID
	entry := &model.FAQEntry{
		Question:   req.Question,
		Answer:     req.Answer,
		Keywords:   req.Keywords,
		Category:   req.Category,
		Intent:     req.Intent,
		Confidence: req.Confidence,
		Enabled:    req.Enabled,
		AgentID:    &agentID,
	}
	if err := c.svc.Update(ctx.Request.Context(), uint(id), entry); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "更新成功")
}

// @Summary      状态已更新
// @Tags      f-a-q
// @Router    /api/faqs/{id}/toggle [patch]
// Toggle 切换启用状态（列表页开关专用，只收 enabled，不走全量 required 校验）
func (c *FAQController) Toggle(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 FAQ ID")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if err := c.svc.SetEnabled(ctx.Request.Context(), uint(id), *req.Enabled); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id, "enabled": *req.Enabled}, "状态已更新")
}

// @Summary      删除 FAQ 条目
// @Tags      f-a-q
// @Router    /api/faqs/{id} [delete]
// Delete 删除
func (c *FAQController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的 FAQ ID")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"id": id}, "删除成功")
}

type faqMatchReq struct {
	Msg     string `json:"msg" binding:"required"`
	TopK    int    `json:"top_k"`
	AgentID uint   `json:"agent_id" binding:"required"`
}

// @Summary 关键词匹配 (调试接口 + 未来 RAG 引擎调用)
// @Tags      f-a-q
// @Router    /api/faqs/match [post]
// Match 关键词匹配 (调试接口 + 未来 RAG 引擎调用)
func (c *FAQController) Match(ctx *gin.Context) {
	var req faqMatchReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.AgentID == 0 {
		response.Error(ctx, http.StatusBadRequest, "agent_id 必填且 > 0 (Task 15 强 1对1)")
		return
	}
	matches, err := c.svc.MatchByAgent(ctx.Request.Context(), req.AgentID, req.Msg, req.TopK)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, matches, "匹配成功")
}

// @Summary      FAQ 统计
// @Tags      f-a-q
// @Router    /api/faqs/stats [get]
// Stats 统计
func (c *FAQController) Stats(ctx *gin.Context) {
	total, enabled, err := c.svc.Stats(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"total":   total,
		"enabled": enabled,
	}, "查询成功")
}

// @Summary      已更新既有标注（下次 Layer1 命中将直接返回标准答案）
// @Tags      f-a-q
// @Router    /api/faqs/annotate [post]
func (c *FAQController) AnnotateFromSession(ctx *gin.Context) {
	var req struct {
		SessionID      string `json:"session_id" binding:"required"`
		MessageContent string `json:"message_content" binding:"required"`
		Answer         string `json:"answer" binding:"required"`
		Category       string `json:"category"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	matches, _ := c.svc.Match(ctx.Request.Context(), req.MessageContent, 1)
	if len(matches) > 0 && matches[0].Entry != nil && matches[0].Score >= 0.92 {
		existing := matches[0].Entry
		updated := &model.FAQEntry{
			ID:       existing.ID,
			Question: existing.Question,
			Answer:   req.Answer,
			Enabled:  boolPtr(true),
		}
		if err := c.svc.Update(ctx.Request.Context(), existing.ID, updated); err != nil {
			response.Error(ctx, http.StatusInternalServerError, "标注更新失败: "+err.Error())
			return
		}
		response.Success(ctx, updated, "已更新既有标注（下次 Layer1 命中将直接返回标准答案）")
		return
	}
	entry := &model.FAQEntry{
		Question: req.MessageContent,
		Answer:   req.Answer,
		Category: orStr(req.Category, "annotation"),
		Enabled:  boolPtr(true),
	}
	if err := c.svc.Create(ctx.Request.Context(), entry); err != nil {
		response.Error(ctx, http.StatusInternalServerError, "标注保存失败: "+err.Error())
		return
	}
	response.Success(ctx, entry, "已保存为标注（下次 Layer1 命中将直接返回标准答案）")
}

func orStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func boolPtr(v bool) *bool { return &v }
