package controller

import (
	"errors"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ClueScoreController 线索评分控制器
type ClueScoreController struct {
	svc *service.ClueScoreService
}

// NewClueScoreController 创建线索评分控制器（服务由装配点注入）
func NewClueScoreController(svc *service.ClueScoreService) *ClueScoreController {
	return &ClueScoreController{svc: svc}
}

// ScoreClue 评分单条线索
// @Summary 评分单条线索
// @Tags 线索评分
// @Accept json
// @Produce json
// @Param request body dto.ClueScoreRequest true "线索 ID"
// @Success 200 {object} object{data=dto.ClueScoreResponse}
// @Router /api/clue/score [post]
func (c *ClueScoreController) ScoreClue(ctx *gin.Context) {
	var req dto.ClueScoreRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	clue, err := c.svc.LoadClueForScoring(ctx.Request.Context(), req.ClueID)
	if err != nil {
		if errors.Is(err, service.ErrClueNotFound) {
			response.Error(ctx, http.StatusNotFound, "线索不存在")
			return
		}
		response.ErrorFromDB(ctx, err, "加载线索失败: "+err.Error())
		return
	}
	score, err := c.svc.ScoreClue(ctx.Request.Context(), clue)
	if err != nil {
		response.ErrorFromDB(ctx, err, "评分失败: "+err.Error())
		return
	}
	resp := service.FromClueScoreModel(score)
	response.Success(ctx, resp, "评分完成")
}

// ScoreAll 批量评分
// @Summary 批量评分所有线索
// @Tags 线索评分
// @Accept json
// @Produce json
// @Param limit query int false "评分上限"
// @Success 200 {object} object{data=dto.ScoreAllResponse}
// @Router /api/clue/score-all [post]
func (c *ClueScoreController) ScoreAll(ctx *gin.Context) {
	limit := 200
	if v := ctx.Query("limit"); v != "" {
		limit = parsePositiveInt(v, 200, 1000)
	}
	count, err := c.svc.ScoreAll(ctx.Request.Context(), limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "批量评分失败: "+err.Error())
		return
	}
	response.Success(ctx, dto.ScoreAllResponse{Scored: count, Limit: limit}, "批量评分完成")
}

// GetByClueID 查询评分
// @Summary 查询线索评分
// @Tags 线索评分
// @Param clue_id path string true "线索 ID"
// @Success 200 {object} object{data=dto.ClueScoreResponse}
// @Router /api/clue/score/{clue_id} [get]
func (c *ClueScoreController) GetByClueID(ctx *gin.Context) {
	clueID := ctx.Param("clue_id")
	score, err := c.svc.GetByClueID(ctx.Request.Context(), clueID)
	if err != nil {
		if errors.Is(err, service.ErrClueScoreNotFound) {
			response.Error(ctx, http.StatusNotFound, "未找到评分")
			return
		}
		response.ErrorFromDB(ctx, err, "查询评分失败: "+err.Error())
		return
	}
	response.Success(ctx, service.FromClueScoreModel(score), "ok")
}

// ListByGrade 按等级分页
// @Summary 按等级分页查询评分
// @Tags 线索评分
// @Param grade query string false "S/A/B/C/D"
// @Param page query int false "页码"
// @Param page_size query int false "每页"
// @Success 200 {object} object{data=dto.ClueScoreListResponse}
// @Router /api/clue/score/list [get]
func (c *ClueScoreController) ListByGrade(ctx *gin.Context) {
	grade := ctx.Query("grade")
	page := parsePositiveInt(ctx.Query("page"), 1, 10000)
	pageSize := parsePositiveInt(ctx.Query("page_size"), 20, 200)
	list, total, err := c.svc.ListByGrade(ctx.Request.Context(), grade, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "查询失败: "+err.Error())
		return
	}
	resp := &dto.ClueScoreListResponse{
		List:     make([]*dto.ClueScoreResponse, 0, len(list)),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
	for _, s := range list {
		resp.List = append(resp.List, service.FromClueScoreModel(s))
	}
	response.Success(ctx, resp, "ok")
}

// RecordEngagement 记录互动事件
// @Summary 记录线索互动事件
// @Tags 线索评分
// @Accept json
// @Produce json
// @Param request body dto.ClueEngagementRequest true "互动事件"
// @Success 200 {object} object{message=string}
// @Router /api/clue/engagement [post]
func (c *ClueScoreController) RecordEngagement(ctx *gin.Context) {
	var req dto.ClueEngagementRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：请检查字段类型与必填项")
		return
	}
	if err := c.svc.RecordEngagement(ctx.Request.Context(), req.ClueID, req.EventType, req.Channel, req.Payload); err != nil {
		response.ErrorFromDB(ctx, err, "记录失败: "+err.Error())
		return
	}
	response.Success(ctx, nil, "ok")
}
