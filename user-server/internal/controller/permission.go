package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// PermissionController 授权管理控制器
type PermissionController struct {
	svc *service.AuthorizationService
}

// NewPermissionController 构造
func NewPermissionController() *PermissionController {
	return &PermissionController{svc: service.NewAuthorizationService()}
}

// @Summary      账号
// @Tags      permission
// @Router    /api/system/permissions/{id}/enabled [put]
// SetEnabled PUT /api/system/permissions/:id/enabled
//
// Body: { "enabled": true|false }
func (ctrl *PermissionController) SetEnabled(c *gin.Context) {
	id, ok := parseSysUserIDParam(c, "id")
	if !ok {
		return
	}
	actorID, ok := extractActorID(c)
	if !ok {
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !response.BindJSON(c, &req) {
		return
	}

	if err := ctrl.svc.SetEnabled(c.Request.Context(), actorID, id, req.Enabled); err != nil {
		writePermissionServiceError(c, err)
		return
	}

	action := "启用"
	if !req.Enabled {
		action = "禁用"
	}
	response.Success(c, nil, "账号"+action+"成功")
}

// @Summary PUT /api/system/permissions/:id/password
// @Tags      permission
// @Router    /api/system/permissions/{id}/password [put]
// ResetPassword PUT /api/system/permissions/:id/password
//
// Body: { "password": "NewPass123" }
func (ctrl *PermissionController) ResetPassword(c *gin.Context) {
	id, ok := parseSysUserIDParam(c, "id")
	if !ok {
		return
	}
	actorID, ok := extractActorID(c)
	if !ok {
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if !response.BindJSON(c, &req) {
		return
	}
	if req.Password == "" {
		response.Error(c, http.StatusBadRequest, "新密码不能为空")
		return
	}

	if err := ctrl.svc.ResetPassword(c.Request.Context(), actorID, id, req.Password); err != nil {
		writePermissionServiceError(c, err)
		return
	}
	response.Success(c, nil, "密码重置成功")
}

// @Summary      查询权限变更审计日志（按人/动作筛选）
// @Tags      permission
// @Router    /api/system/permissions/audit-logs [get]
// ListAuditLogs GET /api/system/permissions/audit-logs
//
// Query: user_id / action / page / page_size
func (ctrl *PermissionController) ListAuditLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	userID, _ := strconv.ParseUint(c.Query("user_id"), 10, 64)
	action := c.Query("action")

	req := &service.ListAuditLogsRequest{
		Action:   action,
		Page:     page,
		PageSize: size,
	}
	if userID > 0 {
		req.UserID = uint(userID)
	}

	resp, err := ctrl.svc.ListAuditLogs(c.Request.Context(), req)
	if err != nil {
		writePermissionServiceError(c, err)
		return
	}

	p := req.Page
	if p < 1 {
		p = 1
	}
	s := req.PageSize
	if s <= 0 {
		s = 20
	}
	if s > 100 {
		s = 100
	}
	response.SuccessWithPage(c, resp.List, int64(p), int64(s), resp.Total)
}

// writePermissionServiceError 把服务层的错误映射成 HTTP 状态码。
//
// 两类都要 400（不是 500）：
//   - ErrInvalidInput：入参/业务前提不满足（不能改自己的密码、密码不合策略…）
//   - ErrInitialAdminProtected：初始超管（id=1）的密码受系统级保护，所有改密
//     入口都拒绝。这**不是**服务端故障，是一条明确的业务规则。
//
// 之前只有 ErrInvalidInput 被显式判，其余一律落 ErrorFromDB ⇒ 500：
// 「初始超管账号的密码不允许通过此入口修改」被报成 500，调用方会以为服务端炸了，
// 而实际上这条 500 与数据库毫无关系（ErrorFromDB 对非 DB 错误也只是原样回 500）。
// serviceErrorMessage 去掉包装哨兵错误的内部后缀，避免把 "service: invalid input"
// 这种实现细节透给前端（2026-10-31 API 回归：前端提示出现了 "不能启停自己的账号: service: invalid input"）。
func serviceErrorMessage(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{service.ErrInvalidInput, service.ErrInitialAdminProtected} {
		msg = strings.TrimSuffix(msg, ": "+sentinel.Error())
	}
	return msg
}

func writePermissionServiceError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, service.ErrInvalidInput) || errors.Is(err, service.ErrInitialAdminProtected) {
		response.Error(c, http.StatusBadRequest, serviceErrorMessage(err))
		return
	}
	response.ErrorFromDB(c, err, err.Error())
}
