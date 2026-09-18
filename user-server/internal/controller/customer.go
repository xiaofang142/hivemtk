package controller

import (
	"context"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CustomerController 客户管理控制器
type CustomerController struct {
	customerService *service.CustomerService
}

// NewCustomerController 创建客户管理控制器
func NewCustomerController() *CustomerController {
	return &CustomerController{
		customerService: service.NewCustomerService(),
	}
}

// ListCustomers 获取客户列表
// @Summary 获取客户列表
// @Description 获取所有客户，支持分页
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param page query int false "页码" default(1)
// @Param limit query int false "每页数量" default(20)
// @Success 200 {object} map[string]interface{} "获取成功"
// @Router /api/customer [get]
func (c *CustomerController) ListCustomers(ctx *gin.Context) {
	if cursor, limit, useCursor := utils.ParseCursorParams(ctx, pagination.DefaultPageSize); useCursor {
		customers, total, nextCursor, err := c.customerService.ListKeyset(context.Background(), cursor, limit)
		if err != nil {
			response.ErrorFromDB(ctx, err, err.Error())
			return
		}
		response.Success(ctx, gin.H{
			"list":        customers,
			"total":       total,
			"limit":       limit,
			"next_cursor": nextCursor,
		}, "获取成功")
		return
	}

	page, limit, err := pagination.Parse(ctx)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	customers, total, err := c.customerService.List(context.Background(), page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":  customers,
		"total": total,
		"page":  page,
		"limit": limit,
	}, "获取成功")
}

// GetCustomer 获取客户详情
// @Summary 获取客户详情
// @Description 根据客户 ID 获取客户 360° 视图
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param id path string true "客户 ID"
// @Success 200 {object} object{data=service.CustomerProfile} "获取成功"
// @Router /api/customer/{id} [get]
func (c *CustomerController) GetCustomer(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	profile, err := c.customerService.GetCustomerProfile(context.Background(), customerID)
	if err != nil {
		if err == service.ErrCustomerNotFound {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, profile, "获取成功")
}

// CreateCustomer 创建客户
// @Summary 创建客户
// @Description 创建新客户或更新现有客户（通过身份标识匹配）。至少需要提供一个有效身份标识。
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param request body service.CustomerDTO true "客户信息"
// @Success 200 {object} object{data=interface{}} "创建成功"
// @Failure 400 {object} object{code=string,message=string} "参数错误（缺身份标识 / 格式不合法）"
// @Router /api/customer [post]
func (c *CustomerController) CreateCustomer(ctx *gin.Context) {
	var req service.CustomerDTO
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	customer, err := c.customerService.CreateOrUpdate(context.Background(), &req)
	if err != nil {
		if err == service.ErrInvalidDTO {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		if strings.Contains(err.Error(), "格式不合法") || strings.Contains(err.Error(), "至少需要提供") {
			response.Error(ctx, http.StatusBadRequest, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, customer, "创建成功")
}

// AddTags 给客户添加标签
// @Summary 给客户添加标签
// @Description 给客户添加一个或多个标签
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param id path string true "客户 ID"
// @Param request body object{tags=[]string} true "标签列表"
// @Success 200 {object} object{message=string} "添加成功"
// @Router /api/customer/:id/tags [post]
func (c *CustomerController) AddTags(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.customerService.AddTags(context.Background(), customerID, req.Tags); err != nil {
		if err == service.ErrCustomerNotFound {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"customer_id": customerID,
		"tags":        req.Tags,
	}, "添加成功")
}

// RemoveTags 从客户移除标签
// @Summary 从客户移除标签
// @Description 从客户移除一个或多个标签
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param id path string true "客户 ID"
// @Param request body object{tags=[]string} true "标签列表"
// @Success 200 {object} object{message=string} "移除成功"
// @Router /api/customer/:id/tags [delete]
func (c *CustomerController) RemoveTags(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	var req struct {
		Tags []string `json:"tags" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.customerService.RemoveTags(context.Background(), customerID, req.Tags); err != nil {
		if err == service.ErrCustomerNotFound {
			response.Error(ctx, http.StatusNotFound, err.Error())
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"customer_id": customerID,
		"tags":        req.Tags,
	}, "移除成功")
}

// MergeCustomers 合并客户
// @Summary 合并客户
// @Description 将 secondary 客户合并到 primary 客户，保留 primary 的信息并合并标签
// @Tags CDP-客户管理
// @Accept json
// @Produce json
// @Param request body object{primary_id=string, secondary_id=string} true "客户 ID"
// @Success 200 {object} object{message=string} "合并成功"
// @Router /api/customer/merge [post]
func (c *CustomerController) MergeCustomers(ctx *gin.Context) {
	var req struct {
		PrimaryID   string `json:"primary_id" binding:"required"`
		SecondaryID string `json:"secondary_id" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	op := service.Operator{
		UserID:   getUserIDFromContext(ctx),
		Username: ctx.GetString("username"),
	}
	svcCtx := service.WithOperator(context.Background(), op)

	if err := c.customerService.MergeCustomers(svcCtx, req.PrimaryID, req.SecondaryID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, nil, "合并成功")
}
