package controller

import (
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// OpportunityController 机会清单控制器
type OpportunityController struct {
	oppSvc *service.OpportunityService
}

// NewOpportunityController 构造机会清单控制器
func NewOpportunityController(oppSvc *service.OpportunityService) *OpportunityController {
	return &OpportunityController{oppSvc: oppSvc}
}

// List 数据驱动的优先级行动清单
// GET /geo/opportunities
func (c *OpportunityController) List(ctx *gin.Context) {
	res, err := c.oppSvc.List(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "机会清单生成失败")
		return
	}
	response.Success(ctx, res, "ok")
}
