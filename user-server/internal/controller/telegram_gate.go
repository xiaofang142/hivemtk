package controller

import (
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// TelegramGateController TG 群组入群管控控制器（方案 A 申请审批 / 方案 B 禁言解锁）
type TelegramGateController struct {
	svc *service.TelegramGateService
}

func NewTelegramGateController(svc *service.TelegramGateService) *TelegramGateController {
	if svc == nil {
		svc = service.NewTelegramGateService(nil)
	}
	return &TelegramGateController{svc: svc}
}

// RegisterRoutes 注册路由（挂 /api/telegram 下）
func (ctrl *TelegramGateController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/telegram/gates")
	{
		g.GET("", ctrl.List)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.GET("/:id/members", ctrl.Members)
		g.POST("/:id/members/:member_id/authorize", ctrl.AuthorizeMember) // 人工放行兜底
	}
}

type telegramGateVO struct {
	ID             uint   `json:"id"`
	AccountID      uint   `json:"account_id"`
	ChatID         string `json:"chat_id"`
	ChatTitle      string `json:"chat_title"`
	Mode           string `json:"mode"`
	Enabled        bool   `json:"enabled"`
	VerifyTTLMin   int    `json:"verify_ttl_min"`
	WelcomeMsg     string `json:"welcome_msg"`
	VerifyMsg      string `json:"verify_msg"`
	AIAgentEnabled bool   `json:"ai_agent_enabled"`
}

func toTelegramGateVO(g *model.TelegramGroupGate) telegramGateVO {
	return telegramGateVO{
		ID:             g.ID,
		AccountID:      g.AccountID,
		ChatID:         g.ChatID,
		ChatTitle:      g.ChatTitle,
		Mode:           g.Mode,
		Enabled:        g.Enabled,
		VerifyTTLMin:   g.VerifyTTLMin,
		WelcomeMsg:     g.WelcomeMsg,
		VerifyMsg:      g.VerifyMsg,
		AIAgentEnabled: g.AIAgentEnabled,
	}
}

// List 网关列表（?account_id= 过滤）
func (ctrl *TelegramGateController) List(c *gin.Context) {
	accountID := uint(0)
	if v := c.Query("account_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			response.Error(c, 400, "account_id 非法")
			return
		}
		accountID = uint(id)
	}
	gates, err := ctrl.svc.ListGates(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFromDB(c, err, "获取网关列表失败")
		return
	}
	list := make([]telegramGateVO, 0, len(gates))
	for _, g := range gates {
		list = append(list, toTelegramGateVO(g))
	}
	response.SuccessWithList(c, list, int64(len(list)))
}

type telegramGateReq struct {
	AccountID      uint   `json:"account_id" binding:"required"`
	ChatID         string `json:"chat_id" binding:"required"`
	ChatTitle      string `json:"chat_title"`
	Mode           string `json:"mode" binding:"required,oneof=join_request mute_unlock"`
	Enabled        *bool  `json:"enabled"`
	VerifyTTLMin   *int   `json:"verify_ttl_min"`
	WelcomeMsg     string `json:"welcome_msg"`
	VerifyMsg      string `json:"verify_msg"`
	AIAgentEnabled *bool  `json:"ai_agent_enabled"`
}

// Create 创建网关配置
func (ctrl *TelegramGateController) Create(c *gin.Context) {
	var req telegramGateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	gate := &model.TelegramGroupGate{
		AccountID:      req.AccountID,
		ChatID:         strings.TrimSpace(req.ChatID),
		ChatTitle:      req.ChatTitle,
		Mode:           req.Mode,
		Enabled:        req.Enabled != nil && *req.Enabled,
		VerifyTTLMin:   10,
		WelcomeMsg:     req.WelcomeMsg,
		VerifyMsg:      req.VerifyMsg,
		AIAgentEnabled: req.AIAgentEnabled != nil && *req.AIAgentEnabled,
	}
	if req.VerifyTTLMin != nil && *req.VerifyTTLMin > 0 {
		gate.VerifyTTLMin = *req.VerifyTTLMin
	}
	if err := ctrl.svc.CreateGate(c.Request.Context(), gate); err != nil {
		response.ErrorFromDB(c, err, "创建失败（群组可能已配置）")
		return
	}
	response.Success(c, toTelegramGateVO(gate), "ok")
}

// Update 更新网关配置
func (ctrl *TelegramGateController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, 400, "非法 ID")
		return
	}
	var req telegramGateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	gate, err := ctrl.svc.GetGate(c.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(c, err, "网关不存在")
		return
	}
	gate.ChatTitle = req.ChatTitle
	gate.Mode = req.Mode
	gate.WelcomeMsg = req.WelcomeMsg
	gate.VerifyMsg = req.VerifyMsg
	if req.Enabled != nil {
		gate.Enabled = *req.Enabled
	}
	if req.VerifyTTLMin != nil && *req.VerifyTTLMin > 0 {
		gate.VerifyTTLMin = *req.VerifyTTLMin
	}
	if req.AIAgentEnabled != nil {
		gate.AIAgentEnabled = *req.AIAgentEnabled
	}
	if err := ctrl.svc.UpdateGate(c.Request.Context(), gate); err != nil {
		response.ErrorFromDB(c, err, "更新失败")
		return
	}
	response.Success(c, toTelegramGateVO(gate), "ok")
}

// Delete 删除网关配置
func (ctrl *TelegramGateController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, 400, "非法 ID")
		return
	}
	if err := ctrl.svc.DeleteGate(c.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(c, err, "删除失败")
		return
	}
	response.Success(c, nil, "ok")
}

// Members 成员验证台账（?status=）
func (ctrl *TelegramGateController) Members(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, 400, "非法 ID")
		return
	}
	gate, err := ctrl.svc.GetGate(c.Request.Context(), uint(id))
	if err != nil {
		response.ErrorFromDB(c, err, "网关不存在")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	members, total, err := ctrl.svc.ListMembers(c.Request.Context(), gate.AccountID, gate.ChatID, c.Query("status"), limit, offset)
	if err != nil {
		response.ErrorFromDB(c, err, "获取成员台账失败")
		return
	}
	response.SuccessWithList(c, members, total)
}

// AuthorizeMember 人工放行兜底（管理员手动验证通过）
func (ctrl *TelegramGateController) AuthorizeMember(c *gin.Context) {
	memberID, err := strconv.ParseUint(c.Param("member_id"), 10, 64)
	if err != nil || memberID == 0 {
		response.Error(c, 400, "非法成员 ID")
		return
	}
	if err := ctrl.svc.AuthorizeMemberByID(c.Request.Context(), uint(memberID)); err != nil {
		response.ErrorFromDB(c, err, "放行失败")
		return
	}
	response.Success(c, nil, "ok")
}
