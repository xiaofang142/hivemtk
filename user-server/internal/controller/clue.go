package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
)

type ClueController struct {
	svc *service.ClueService
}

func NewClueController() *ClueController {
	return &ClueController{svc: service.NewClueService()}
}

// GetClueList godoc
// @Summary      获取线索列表
// @Description  分页查询线索池，支持按来源/状态筛选
// @Tags         Clue
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page      query  int  false  "页码"  default(1)
// @Param        page_size query  int  false  "每页数量"  default(20)
// @Param        source    query  int  false  "来源 ID"
// @Param        status    query  int  false  "状态"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/clues [get]
func (c *ClueController) GetClueList(ctx *gin.Context) {
	var req dto.GetClueRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	clues, total, err := c.svc.GetClueList(ctx.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索列表失败")
		return
	}
	resp := dto.GetClueListResponse{
		Total: total,
		List:  []*dto.ClueListResponse{},
	}
	for _, clue := range clues {
		resp.List = append(resp.List, &dto.ClueListResponse{
			ID:       clue.ID,
			SourceID: clue.SourceID,
			Account:  clue.Account,
			IsVerify: clue.IsVerify,
			Name:     clue.Name,
			City:     clue.City,
			Address:  clue.Address,
			Type:     clue.Type,
		})
	}

	response.Success(ctx, resp, "获取线索列表成功")
}

// DeleteClue godoc
// @Summary      删除线索
// @Description  通过 ID 软删除线索
// @Tags         Clue
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  int  true  "线索 ID"
// @Success      200  {object}  response.Response  "删除成功"
// @Failure      400  {object}  response.Response  "参数错误"
// @Router       /api/clues/{id} [delete]
func (c *ClueController) DeleteClue(ctx *gin.Context) {
	var req dto.DeleteClueRequest
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}
	err := c.svc.DeleteClue(ctx.Request.Context(), req.ID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "删除线索失败")
		return
	}
	response.Success(ctx, nil, "删除线索成功")
}

// GetClueStatistics godoc
// @Summary      获取线索统计数据
// @Description  返回线索总数、转化率、来源分布
// @Tags         Clue
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/clues/statistics [get]
func (c *ClueController) GetClueStatistics(ctx *gin.Context) {
	statistics, err := c.svc.GetClueStatistics(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索统计失败")
		return
	}
	response.Success(ctx, statistics, "获取线索统计成功")
}

func (c *ClueController) ImportClues(ctx *gin.Context) {
	var req []dto.ImportClueRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "参数错误")
		return
	}

	for _, item := range req {
		if _, err := strconv.ParseInt(item.Type, 10, 64); err != nil {
			response.Error(ctx, http.StatusBadRequest, "线索类型格式错误")
			return
		}
	}

	successCount, skipCount, err := c.svc.BatchImportCluesFromDTO(ctx.Request.Context(), req)
	if err != nil {
		response.ErrorFromDB(ctx, err, "导入线索失败")
		return
	}

	response.Success(ctx, map[string]int64{
		"success_count": successCount,
		"skip_count":    skipCount,
	}, "导入线索成功")
}

// GetClueTypes 获取线索类型列表
func (c *ClueController) GetClueTypes(ctx *gin.Context) {
	types, err := c.svc.GetClueTypes(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索类型失败")
		return
	}
	response.Success(ctx, types, "获取线索类型成功")
}
