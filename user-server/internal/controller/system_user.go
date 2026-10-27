package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

// SystemUserAdminController 人员管理控制器（阶段 4 新增）
type SystemUserAdminController struct {
	svc *service.SystemUserService
}

// NewSystemUserAdminController 构造
func NewSystemUserAdminController() *SystemUserAdminController {
	return &SystemUserAdminController{
		svc: service.NewSystemUserService(),
	}
}

func extractActorID(c *gin.Context) (uint, bool) {
	v, ok := c.Get("user_id")
	if !ok || v == nil {
		response.Error(c, http.StatusUnauthorized, "未授权，请先登录")
		return 0, false
	}
	switch val := v.(type) {
	case uint:
		return val, true
	case int:
		if val < 0 {
			response.Error(c, http.StatusUnauthorized, "user_id 非法")
			return 0, false
		}
		return uint(val), true
	case int64:
		if val < 0 {
			response.Error(c, http.StatusUnauthorized, "user_id 非法")
			return 0, false
		}
		return uint(val), true
	case float64:
		if val < 0 {
			response.Error(c, http.StatusUnauthorized, "user_id 非法")
			return 0, false
		}
		return uint(val), true
	default:
		response.Error(c, http.StatusUnauthorized, "user_id 类型错误")
		return 0, false
	}
}

func parseSysUserIDParam(c *gin.Context, paramName string) (uint, bool) {
	raw := c.Param(paramName)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, "无效的 ID")
		return 0, false
	}
	return uint(id), true
}

func writeServiceError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, service.ErrUserNotFound) {
		response.NotFound(c, "用户不存在")
		return
	}
	if errors.Is(err, service.ErrInvalidInput) {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.ErrorFromDB(c, err, err.Error())
}

// @Summary      分页查询系统用户（关键字/角色筛选）
// @Tags      system-user-admin
// @Router    /api/system/users [get]
// GetUsers GET /api/system/users
//
// Query：keyword / role / page / size
func (c *SystemUserAdminController) GetUsers(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(ctx.DefaultQuery("size", "20"))
	req := &service.GetUsersRequest{
		Keyword: ctx.Query("keyword"),
		Role:    ctx.Query("role"),
		Page:    page,
		Size:    size,
	}
	resp, err := c.svc.GetUsersAdmin(ctx.Request.Context(), req)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	p := req.Page
	if p < 1 {
		p = 1
	}
	s := req.Size
	if s <= 0 {
		s = 20
	}
	if s > 100 {
		s = 100
	}
	response.SuccessWithPage(ctx, resp.List, int64(p), int64(s), resp.Total)
}

// @Summary      获取账号详情成功
// @Tags      system-user-admin
// @Router    /api/system/users/{id} [get]
// GetByID GET /api/system/users/:id
func (c *SystemUserAdminController) GetByID(ctx *gin.Context) {
	id, ok := parseSysUserIDParam(ctx, "id")
	if !ok {
		return
	}
	user, err := c.svc.GetByIDAdmin(ctx.Request.Context(), id)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	response.Success(ctx, user, "获取账号详情成功")
}

// @Summary      账号创建成功
// @Tags      system-user-admin
// @Router    /api/system/users [post]
// Create POST /api/system/users
func (c *SystemUserAdminController) Create(ctx *gin.Context) {
	actorID, ok := extractActorID(ctx)
	if !ok {
		return
	}
	var req service.CreateUserByAdminRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	user, err := c.svc.CreateByAdmin(ctx.Request.Context(), actorID, &req)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	response.Success(ctx, user, "账号创建成功")
}

// @Summary      账号更新成功
// @Tags      system-user-admin
// @Router    /api/system/users/{id} [put]
// Update PUT /api/system/users/:id
func (c *SystemUserAdminController) Update(ctx *gin.Context) {
	actorID, ok := extractActorID(ctx)
	if !ok {
		return
	}
	id, ok := parseSysUserIDParam(ctx, "id")
	if !ok {
		return
	}
	var req service.UpdateUserByAdminRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	user, err := c.svc.UpdateByAdmin(ctx.Request.Context(), actorID, id, &req)
	if err != nil {
		writeServiceError(ctx, err)
		return
	}
	response.Success(ctx, user, "账号更新成功")
}

// @Summary      账号删除成功
// @Tags      system-user-admin
// @Router    /api/system/users/{id} [delete]
// Delete DELETE /api/system/users/:id
func (c *SystemUserAdminController) Delete(ctx *gin.Context) {
	actorID, ok := extractActorID(ctx)
	if !ok {
		return
	}
	id, ok := parseSysUserIDParam(ctx, "id")
	if !ok {
		return
	}
	if err := c.svc.DeleteByAdmin(ctx.Request.Context(), actorID, id); err != nil {
		writeServiceError(ctx, err)
		return
	}
	response.Success(ctx, nil, "账号删除成功")
}
