package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserController struct {
	svc service.UserService
}

func NewUserController() *UserController {
	return &UserController{svc: service.NewUserService()}
}

func (c *UserController) GetUserList(ctx *gin.Context) {
	page, pageSize, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	result, err := c.svc.GetUserList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, result, "获取用户列表成功")
}

// GetUser 获取用户详情
func (c *UserController) GetUser(ctx *gin.Context) {
	idStr := ctx.Param("id")

	user, err := c.svc.GetUser(ctx.Request.Context(), idStr)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "用户不存在")
		return
	}

	response.Success(ctx, user, "获取用户详情成功")
}

// CreateUser 创建用户
func (c *UserController) CreateUser(ctx *gin.Context) {
	var req dto.CreateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	user, err := c.svc.RegisterUser(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, user, "创建用户成功")
}

// UpdateUser 更新用户
func (c *UserController) UpdateUser(ctx *gin.Context) {
	idStr := ctx.Param("id")

	var req dto.UpdateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	user, err := c.svc.UpdateUser(ctx.Request.Context(), idStr, &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, user, "更新用户成功")
}

func (c *UserController) DeleteUser(ctx *gin.Context) {
	idStr := ctx.Param("id")

	err := c.svc.DeleteUser(ctx.Request.Context(), idStr)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"message": "用户删除成功"}, "删除用户成功")
}

// UpdatePassword 修改密码
func (c *UserController) UpdatePassword(ctx *gin.Context) {
	idStr := ctx.Param("id")

	var req dto.UpdatePasswordRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	err := c.svc.UpdatePassword(ctx.Request.Context(), idStr, &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"message": "密码修改成功"}, "修改密码成功")
}

// Login 用户登录
func (c *UserController) Login(ctx *gin.Context) {
	var req dto.LoginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	result, err := c.svc.Login(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusUnauthorized, err.Error())
		return
	}

	response.Success(ctx, result, "登录成功")
}
