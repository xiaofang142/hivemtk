package controller

import (
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CustomerRFMController 客户 RFM 控制器
type CustomerRFMController struct {
	svc *service.CustomerRFMService
}

// NewCustomerRFMController 创建 RFM 控制器
func NewCustomerRFMController() *CustomerRFMController {
	return &CustomerRFMController{svc: service.NewCustomerRFMService()}
}

// ComputeForCustomer 计算单个客户 RFM
// @Summary 计算单个客户 RFM
// @Tags 客户RFM
// @Accept json
// @Produce json
// @Param request body dto.RFMComputeRequest true "客户 ID"
// @Success 200 {object} object{data=dto.CustomerRFMResponse}
// @Router /api/customer-rfm/compute [post]
func (c *CustomerRFMController) ComputeForCustomer(ctx *gin.Context) {
	var req dto.RFMComputeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	cfg := service.DefaultRFMConfig()
	rfm, err := c.svc.ComputeForCustomer(ctx.Request.Context(), req.CustomerID, cfg)
	if err != nil {
		response.ErrorFromDB(ctx, err, "计算失败: "+err.Error())
		return
	}
	response.Success(ctx, service.FromCustomerRFMModel(rfm), "ok")
}

// ComputeAll 批量计算所有客户 RFM
// @Summary 批量计算所有客户 RFM
// @Tags 客户RFM
// @Accept json
// @Produce json
// @Param limit query int false "上限"
// @Success 200 {object} object{data=dto.RFMComputeAllResponse}
// @Router /api/customer-rfm/compute-all [post]
func (c *CustomerRFMController) ComputeAll(ctx *gin.Context) {
	limit := parsePositiveInt(ctx.Query("limit"), 200, 1000)
	count, err := c.svc.ComputeAll(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "批量计算失败: "+err.Error())
		return
	}
	response.Success(ctx, dto.RFMComputeAllResponse{Computed: count, Limit: limit}, "ok")
}

// GetByCustomerID 查询客户 RFM
// @Summary 查询客户 RFM
// @Tags 客户RFM
// @Param customer_id path string true "客户 ID"
// @Success 200 {object} object{data=dto.CustomerRFMResponse}
// @Router /api/customer-rfm/{customer_id} [get]
func (c *CustomerRFMController) GetByCustomerID(ctx *gin.Context) {
	customerID := ctx.Param("customer_id")
	rfm, err := c.svc.GetByCustomerID(ctx.Request.Context(), customerID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "未找到")
		return
	}
	response.Success(ctx, service.FromCustomerRFMModel(rfm), "ok")
}

// ListBySegment 按分层分页查询
// @Summary 按分层分页查询
// @Tags 客户RFM
// @Param segment query string false "champion/loyal/potential/at_risk/churn"
// @Param page query int false "页码"
// @Param page_size query int false "每页"
// @Success 200 {object} object{data=dto.CustomerRFMListResponse}
// @Router /api/customer-rfm/list [get]
func (c *CustomerRFMController) ListBySegment(ctx *gin.Context) {
	segment := ctx.Query("segment")
	page := parsePositiveInt(ctx.Query("page"), 1, 10000)
	pageSize := parsePositiveInt(ctx.Query("page_size"), 20, 200)
	list, total, err := c.svc.ListBySegment(ctx.Request.Context(), segment, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	resp := &dto.CustomerRFMListResponse{
		List:     make([]*dto.CustomerRFMResponse, 0, len(list)),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, r := range list {
		resp.List = append(resp.List, service.FromCustomerRFMModel(r))
	}
	response.Success(ctx, resp, "ok")
}

// Distribution 分层分布
// @Summary 分层分布
// @Tags 客户RFM
// @Success 200 {object} object{data=dto.RFMDistributionResponse}
// @Router /api/customer-rfm/distribution [get]
func (c *CustomerRFMController) Distribution(ctx *gin.Context) {
	dist, err := c.svc.Distribution(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	total := int64(0)
	for _, v := range dist {
		total += v
	}
	response.Success(ctx, dto.RFMDistributionResponse{Distribution: dist, Total: total}, "ok")
}
