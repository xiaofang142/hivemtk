package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// LeadMiningAgentController 「线索发现智能体」独立注册管理入口（FR-026，T047）。
// 仅解析入参、调 Service、组响应；不碰 DB、不 import repository/gorm。
type LeadMiningAgentController struct {
	svc *service.LeadMiningAgentService
}

func NewLeadMiningAgentController(svc *service.LeadMiningAgentService) *LeadMiningAgentController {
	return &LeadMiningAgentController{svc: svc}
}

// leadMiningAgentRequest 线索发现智能体入参（写在 controller 内，避免冗余 _dto.go 文件）。
type leadMiningAgentRequest struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Avatar       string `json:"avatar"`
	AgentMode    string `json:"agent_mode"`
	Persona      string `json:"persona"`
	SystemPrompt string `json:"system_prompt"`
	Greeting     string `json:"greeting"`
	LLMModel     string `json:"llm_model"`
	Status       int    `json:"status"`
}

func (c *LeadMiningAgentController) List(ctx *gin.Context) {
	items, err := c.svc.List(ctx.Request.Context(), ctx.Query("keyword"))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索发现智能体列表失败")
		return
	}
	response.Success(ctx, items, "ok")
}

func (c *LeadMiningAgentController) Get(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "智能体ID非法")
		return
	}
	a, err := c.svc.Get(ctx.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索发现智能体失败")
		return
	}
	if a == nil {
		response.Error(ctx, http.StatusNotFound, "线索发现智能体不存在")
		return
	}
	response.Success(ctx, a, "ok")
}

func (c *LeadMiningAgentController) Create(ctx *gin.Context) {
	var req leadMiningAgentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.Error(ctx, http.StatusBadRequest, "名称不能为空")
		return
	}
	a := &model.AIAgent{
		Name: req.Name, Description: req.Description, Avatar: req.Avatar,
		AgentMode: req.AgentMode, Persona: req.Persona, SystemPrompt: req.SystemPrompt,
		Greeting: req.Greeting, LLMModel: req.LLMModel, Status: req.Status,
	}
	if err := c.svc.Create(ctx.Request.Context(), a); err != nil {
		response.Error(ctx, http.StatusBadRequest, "新增线索发现智能体失败："+err.Error())
		return
	}
	response.Success(ctx, a, "ok")
}

func (c *LeadMiningAgentController) Update(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "智能体ID非法")
		return
	}
	var req leadMiningAgentRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.Error(ctx, http.StatusBadRequest, "名称不能为空")
		return
	}
	a := &model.AIAgent{
		ID: uint(id), Name: req.Name, Description: req.Description, Avatar: req.Avatar,
		AgentMode: req.AgentMode, Persona: req.Persona, SystemPrompt: req.SystemPrompt,
		Greeting: req.Greeting, LLMModel: req.LLMModel, Status: req.Status,
	}
	if err := c.svc.Update(ctx.Request.Context(), a); err != nil {
		response.Error(ctx, http.StatusBadRequest, "修改线索发现智能体失败："+err.Error())
		return
	}
	response.Success(ctx, a, "ok")
}

func (c *LeadMiningAgentController) Delete(ctx *gin.Context) {
	id, err := strconv.ParseUint(ctx.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(ctx, http.StatusBadRequest, "智能体ID非法")
		return
	}
	if err := c.svc.Delete(ctx.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(ctx, err, "删除线索发现智能体失败")
		return
	}
	response.Success(ctx, gin.H{"id": id}, "ok")
}
