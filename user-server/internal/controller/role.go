package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// RoleController 角色管理控制器
type RoleController struct {
	svc *service.RoleService
}

// NewRoleController 构造
func NewRoleController() *RoleController {
	return &RoleController{svc: service.NewRoleService()}
}

// @Summary GET /api/system/roles
// @Tags      role
// @Router    /api/system/roles [get]
// ListRoles GET /api/system/roles
//
// 列出 3 档系统角色 + 成员数。
func (ctrl *RoleController) ListRoles(c *gin.Context) {
	roles, err := ctrl.svc.ListRoles(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, roles, "获取角色列表成功")
}

// @Summary GET /api/system/roles/:code
// @Tags      role
// @Router    /api/system/roles/{code} [get]
// GetRole GET /api/system/roles/:code
//
// 单个角色详情（带成员数）。
func (ctrl *RoleController) GetRole(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		response.Error(c, http.StatusBadRequest, "角色 code 不能为空")
		return
	}
	role, err := ctrl.svc.GetRole(c.Request.Context(), code)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, role, "获取角色详情成功")
}

// @Summary GET /api/system/roles/:code/members
// @Tags      role
// @Router    /api/system/roles/{code}/members [get]
// ListMembers GET /api/system/roles/:code/members
//
// 角色下成员列表（分页）。
// Query: page / size
func (ctrl *RoleController) ListMembers(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		response.Error(c, http.StatusBadRequest, "角色 code 不能为空")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	members, total, err := ctrl.svc.ListMembersByRole(c.Request.Context(), code, page, size)
	if err != nil {
		writeServiceError(c, err)
		return
	}

	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	response.SuccessWithPage(c, members, int64(page), int64(size), total)
}
