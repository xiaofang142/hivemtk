package controller

import (
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// EmailJobsController 任务控制器
type EmailJobsController struct {
	svc *email.EmailJobsService
}

// NewEmailJobsController 创建任务控制器实例
func NewEmailJobsController() *EmailJobsController {
	return &EmailJobsController{svc: email.NewEmailJobsService()}
}

// CreateEmailJobs 创建任务
func (c *EmailJobsController) CreateEmailJobs(ctx *gin.Context) {
	var req dto.CreateEmailJobsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := c.svc.CreateEmailJobsDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, resp, "创建成功")
}

// GetEmailJobsList 获取任务列表
func (c *EmailJobsController) GetEmailJobsList(ctx *gin.Context) {
	var req dto.GetEmailJobsListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	resp, err := c.svc.GetEmailJobsListDTO(ctx.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// GetEmailJobsDetail 获取任务详情
func (c *EmailJobsController) GetEmailJobsDetail(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := c.svc.GetEmailJobsByIDDTO(ctx.Request.Context(), jobsID)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "任务不存在")
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// UpdateEmailJobs 更新任务
func (c *EmailJobsController) UpdateEmailJobs(ctx *gin.Context) {
	var req dto.UpdateEmailJobsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if err := c.svc.UpdateEmailJobsDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	resp, err := c.svc.GetEmailJobsByIDDTO(ctx.Request.Context(), jobsID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// UpdateSendTotal 更新发送总数
func (c *EmailJobsController) UpdateSendTotal(ctx *gin.Context) {
	var req dto.UpdateEmailJobsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if err := c.svc.UpdateEmailJobsDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// DeleteEmailJobs 删除任务
func (c *EmailJobsController) DeleteEmailJobs(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if err = c.svc.DeleteEmailJobs(ctx.Request.Context(), jobsID); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, nil, "删除成功")
}
