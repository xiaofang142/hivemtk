package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// DingTalkAppAccountController 钉钉企业内部应用账号管理控制器
//
// 职责：
//   - 应用账号（AppKey / AppSecret / AgentId / 回调 Token / AESKey）CRUD
//   - 入站收消息开关、绑定 AI 智能体
//   - 凭据（AppSecret）不回显，返回掩码
type DingTalkAppAccountController struct {
	svc *service.DingTalkAppService
}

// NewDingTalkAppAccountController 创建控制器
func NewDingTalkAppAccountController(svc *service.DingTalkAppService) *DingTalkAppAccountController {
	return &DingTalkAppAccountController{svc: svc}
}

// RegisterRoutes 注册路由
func (ctrl *DingTalkAppAccountController) RegisterRoutes(router *gin.RouterGroup) {
	g := router.Group("/dingtalk-app/accounts")
	{
		g.GET("", ctrl.List)
		g.GET("/:id", ctrl.Get)
		g.POST("", ctrl.Create)
		g.PUT("/:id", ctrl.Update)
		g.DELETE("/:id", ctrl.Delete)
		g.POST("/:id/test", ctrl.Test)
	}
}

type dingTalkAppAccountRequest struct {
	AccountName    string `json:"account_name"`
	AppKey         string `json:"app_key"`
	AppSecret      string `json:"app_secret"`
	AgentID        string `json:"agent_id"`
	Token          string `json:"token"`
	AESKey         string `json:"aes_key"`
	InboundEnabled bool   `json:"inbound_enabled"`
	AIAgentID      string `json:"ai_agent_id"`
	UserID         uint   `json:"user_id"`
	Status         int    `json:"status"`
}

type dingTalkAppAccountVO struct {
	ID              uint       `json:"id"`
	AccountName     string     `json:"account_name"`
	AppKey          string     `json:"app_key"`
	AgentID         string     `json:"agent_id"`
	Token           string     `json:"token"`
	AESKeyMasked    string     `json:"aes_key_masked"`
	InboundEnabled  bool       `json:"inbound_enabled"`
	AIAgentID       string     `json:"ai_agent_id"`
	AppSecretMasked string     `json:"app_secret_masked"`
	LastErrorAt     *time.Time `json:"last_error_at"`
	LastErrorMsg    string     `json:"last_error_msg"`
	Status          int        `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toDingTalkAppVO(a *model.DingTalkAppAccount) *dingTalkAppAccountVO {
	return &dingTalkAppAccountVO{
		ID:              a.ID,
		AccountName:     a.AccountName,
		AppKey:          a.AppKey,
		AgentID:         a.AgentID,
		Token:           a.Token,
		AESKeyMasked:    maskFeishuSecret(a.AESKey),
		InboundEnabled:  a.InboundEnabled,
		AIAgentID:       a.AIAgentID,
		AppSecretMasked: maskFeishuSecret(a.AppSecret),
		LastErrorAt:     a.LastErrorAt,
		LastErrorMsg:    a.LastErrorMsg,
		Status:          a.Status,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	}
}

// @Summary      查询钉钉应用账号列表
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts [get]
// List 列出所有钉钉应用账号
func (ctrl *DingTalkAppAccountController) List(c *gin.Context) {
	accs, err := ctrl.svc.ListAccounts(c.Request.Context())
	if err != nil {
		response.ErrorFromDB(c, err, "查询钉钉应用账号列表失败")
		return
	}
	out := make([]*dingTalkAppAccountVO, 0, len(accs))
	for i := range accs {
		out = append(out, toDingTalkAppVO(&accs[i]))
	}
	response.Success(c, out, "查询成功")
}

// @Summary      按 ID 查询钉钉应用账号详情
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts/{id} [get]
// Get 查询单个账号
func (ctrl *DingTalkAppAccountController) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号 ID")
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		replyDingTalkAccountError(c, err, id)
		return
	}
	if !guardChannelAccountOwnership(c, acc.UserID) {
		return
	}
	response.Success(c, toDingTalkAppVO(acc), "查询成功")
}

// @Summary      创建钉钉应用账号
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts [post]
// Create 创建账号
func (ctrl *DingTalkAppAccountController) Create(c *gin.Context) {
	var req dingTalkAppAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：请求体需为 JSON 对象（字段见接口文档，密钥类字段可选）")
		return
	}
	// #134 必填校验：缺账号名/AppKey/AppSecret 会写出无法回调、无法取 token 的孤儿账号
	if strings.TrimSpace(req.AccountName) == "" || strings.TrimSpace(req.AppKey) == "" || strings.TrimSpace(req.AppSecret) == "" {
		response.Error(c, http.StatusBadRequest, "请求参数错误：account_name、app_key、app_secret 均为必填（token / aes_key / ai_agent_id 可选）")
		return
	}

	acc := &model.DingTalkAppAccount{
		AccountName:    req.AccountName,
		AppKey:         req.AppKey,
		AppSecret:      req.AppSecret,
		AgentID:        req.AgentID,
		Token:          req.Token,
		AESKey:         req.AESKey,
		InboundEnabled: req.InboundEnabled,
		AIAgentID:      req.AIAgentID,
		UserID:         currentStaffUserID(c),
		Status:         req.Status,
	}
	if acc.Status == 0 {
		acc.Status = 1
	}
	if err := ctrl.svc.CreateAccount(c.Request.Context(), acc); err != nil {
		// #135 不再把 err.Error() 塞进 data：会把 SQL/驱动原文回传前端
		response.ErrorFromDB(c, err, "创建钉钉应用账号失败")
		return
	}
	response.Success(c, toDingTalkAppVO(acc), "创建成功")
}

// @Summary      更新钉钉应用账号
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts/{id} [put]
// Update 更新账号
func (ctrl *DingTalkAppAccountController) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号 ID")
		return
	}
	var req dingTalkAppAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "请求参数错误：请求体需为 JSON 对象（字段见接口文档，密钥类字段可选）")
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		replyDingTalkAccountError(c, err, id)
		return
	}
	if !guardChannelAccountOwnership(c, acc.UserID) {
		return
	}
	acc.AccountName = req.AccountName
	acc.AppKey = req.AppKey
	acc.AgentID = req.AgentID
	// 密钥类字段前端编辑表单不回填（VO 只回掩码），提交时缺字段：空值=保留原值，防止编辑把密钥清空
	if req.AppSecret != "" {
		acc.AppSecret = req.AppSecret
	}
	if req.Token != "" {
		acc.Token = req.Token
	}
	if req.AESKey != "" {
		acc.AESKey = req.AESKey
	}
	acc.InboundEnabled = req.InboundEnabled
	acc.AIAgentID = req.AIAgentID
	// 前端编辑不传 status：0=保留原值，防止编辑把账号状态打回禁用
	if req.Status != 0 {
		acc.Status = req.Status
	}
	if err := ctrl.svc.UpdateAccount(c.Request.Context(), acc); err != nil {
		response.ErrorFromDB(c, err, "更新钉钉应用账号失败")
		return
	}
	response.Success(c, toDingTalkAppVO(acc), "更新成功")
}

// @Summary      删除钉钉应用账号
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts/{id} [delete]
// Delete 删除账号
func (ctrl *DingTalkAppAccountController) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号 ID")
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		replyDingTalkAccountError(c, err, id)
		return
	}
	if !guardChannelAccountOwnership(c, acc.UserID) {
		return
	}
	if err := ctrl.svc.DeleteAccount(c.Request.Context(), uint(id)); err != nil {
		response.ErrorFromDB(c, err, "删除钉钉应用账号失败")
		return
	}
	response.Success(c, nil, "删除成功")
}

// @Summary      配置校验通过
// @Tags      ding-talk-app-account
// @Router    /api/dingtalk-app/accounts/{id}/test [post]
// Test 测试账号配置（校验必填项是否已填写）
func (ctrl *DingTalkAppAccountController) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "无效的账号 ID")
		return
	}
	acc, err := ctrl.svc.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		replyDingTalkAccountError(c, err, id)
		return
	}
	if !guardChannelAccountOwnership(c, acc.UserID) {
		return
	}
	if acc.AppKey == "" || acc.AppSecret == "" || acc.Token == "" {
		response.Error(c, http.StatusBadRequest, "配置不完整", "AppKey/AppSecret/Token 均为必填")
		return
	}
	response.Success(c, gin.H{"ok": true, "inbound_enabled": acc.InboundEnabled}, "配置校验通过")
}

// replyDingTalkAccountError 账号读取失败：真·不存在回 404（带 id），其余回 500。
// 原实现把任何错误都回 404 并把 err.Error() 放进 data，DB 故障会伪装成"账号不存在"。
func replyDingTalkAccountError(c *gin.Context, err error, id uint64) {
	if errhttp.IsNotFoundError(err) {
		response.NotFound(c, fmt.Sprintf("钉钉应用账号不存在（id=%d）", id))
		return
	}
	response.Error(c, http.StatusInternalServerError, "读取钉钉应用账号失败")
}
