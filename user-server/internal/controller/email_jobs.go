package controller

import (
	"hivemtk-user/internal/dto"
	email "hivemtk-user/internal/email/service"
	"hivemtk-user/internal/pkg/errhttp"
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
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	resp, err := c.svc.CreateEmailJobsDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "创建邮件任务失败")
		return
	}
	response.Success(ctx, resp, "创建成功")
}

// @Summary      查询邮件任务列表
// @Tags      email-jobs
// @Router    /api/email/jobs [get]
// GetEmailJobsList 获取任务列表
func (c *EmailJobsController) GetEmailJobsList(ctx *gin.Context) {
	var req dto.GetEmailJobsListRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
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
		response.ErrorFromDB(ctx, err, "获取邮件任务列表失败")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// @Summary      查询邮件任务详情
// @Tags      email-jobs
// @Router    /api/email/jobs/{id} [get]
// GetEmailJobsDetail 获取任务详情
func (c *EmailJobsController) GetEmailJobsDetail(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	resp, err := c.svc.GetEmailJobsByIDDTO(ctx.Request.Context(), jobsID)
	if err != nil {
		if isNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "任务不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "邮件任务不存在")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// UpdateEmailJobs 更新任务
func (c *EmailJobsController) UpdateEmailJobs(ctx *gin.Context) {
	var req dto.UpdateEmailJobsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	if err := c.svc.UpdateEmailJobsDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, "更新邮件任务失败")
		return
	}

	resp, err := c.svc.GetEmailJobsByIDDTO(ctx.Request.Context(), jobsID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "操作失败，请稍后重试")
		return
	}
	response.Success(ctx, resp, "获取成功")
}

// UpdateSendTotal 更新发送总数
func (c *EmailJobsController) UpdateSendTotal(ctx *gin.Context) {
	var req dto.UpdateEmailJobsRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	if _, err := uuid.Parse(req.ID); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	if err := c.svc.UpdateEmailJobsDTO(ctx.Request.Context(), req); err != nil {
		response.ErrorFromDB(ctx, err, "更新发送计数失败")
		return
	}
	response.Success(ctx, nil, "更新成功")
}

// @Summary      删除邮件任务
// @Tags      email-jobs
// @Router    /api/email/jobs/{id} [delete]
// DeleteEmailJobs 删除任务
func (c *EmailJobsController) DeleteEmailJobs(ctx *gin.Context) {
	var req struct {
		ID string `uri:"id" binding:"required"`
	}
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	jobsID, err := uuid.Parse(req.ID)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的任务 ID（需 UUID）")
		return
	}

	if err = c.svc.DeleteEmailJobs(ctx.Request.Context(), jobsID); err != nil {
		if errhttp.IsNotFoundError(err) {
			response.Error(ctx, http.StatusNotFound, "邮件任务不存在")
			return
		}
		errhttp.HandleDBError(ctx, err, "删除邮件任务失败")
		return
	}
	response.Success(ctx, nil, "删除成功")
}
