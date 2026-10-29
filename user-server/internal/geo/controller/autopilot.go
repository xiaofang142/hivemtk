package controller

import (
	"errors"
	"strconv"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AutopilotController GEO 一键自动驾驶控制器
type AutopilotController struct {
	svc *service.AutopilotService
}

// NewAutopilotController 构造自动驾驶控制器
func NewAutopilotController(svc *service.AutopilotService) *AutopilotController {
	return &AutopilotController{svc: svc}
}

// Create 启动一次一键运行
// POST /geo/autopilot/runs
func (c *AutopilotController) Create(ctx *gin.Context) {
	var req dto.AutopilotCreateRequest
	if !response.BindJSON(ctx, &req) {
		return
	}
	run, err := c.svc.Create(ctx.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, service.ErrAutopilotRunning) {
			ctx.JSON(409, gin.H{"code": 409, "message": err.Error()})
			return
		}
		response.ValidationError(ctx, err.Error())
		return
	}
	response.Success(ctx, toAutopilotRunResponse(run), "已启动")
}

// Get 运行详情（前端轮询入口）
// GET /geo/autopilot/runs/:id
func (c *AutopilotController) Get(ctx *gin.Context) {
	run, err := c.svc.Get(ctx.Request.Context(), ctx.Param("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(ctx, 404, "运行记录不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "查询运行详情失败")
		return
	}
	response.Success(ctx, toAutopilotRunResponse(run), "ok")
}

// List 运行历史
// GET /geo/autopilot/runs
func (c *AutopilotController) List(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	rows, total, err := c.svc.List(ctx.Request.Context(), page, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询运行历史失败")
		return
	}
	out := make([]dto.AutopilotRunResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAutopilotRunResponse(r))
	}
	response.SuccessWithList(ctx, out, total)
}

func toAutopilotRunResponse(r *model.GeoAutopilotRun) dto.AutopilotRunResponse {
	resp := dto.AutopilotRunResponse{
		ID:           r.ID,
		Keyword:      r.Keyword,
		SiteURL:      r.SiteURL,
		ArticleCount: r.ArticleCount,
		Status:       r.Status,
		Progress:     r.Progress,
		CurrentStep:  r.CurrentStep,
		Steps:        r.Steps,
		Summary:      r.Summary,
		Error:        r.Error,
		StartedAt:    r.StartedAt.Format(time.RFC3339),
	}
	if r.FinishedAt != nil {
		s := r.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &s
	}
	return resp
}
