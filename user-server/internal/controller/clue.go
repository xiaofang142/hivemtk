package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// clueImportMaxBatch 单次导入上限：cts 一次提交几十万行会把请求体、去重查询
// （按 500 条分块）和插入事务同时撑爆，分批导入更可控。
const clueImportMaxBatch = 5000

type ClueController struct {
	svc *service.ClueService
}

func NewClueController(svc *service.ClueService) *ClueController {
	return &ClueController{svc: svc}
}

// GetClueList godoc
// @Summary      获取线索列表
// @Description  分页查询线索池，筛选条件在 DB 侧生效并与 total 同口径
// @Tags         Clue
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page      query  int     false  "页码"  default(1)
// @Param        limit     query  int     false  "每页数量"  default(20)
// @Param        keyword   query  string  false  "名称/账号/城市/地址模糊匹配"
// @Param        type      query  int     false  "线索类型（见 /api/clues/type）"
// @Param        is_verify query  int     false  "验证状态：0 未验证 / 1 已验证"
// @Param        start     query  int     false  "创建时间下界（unix 秒）"
// @Param        end       query  int     false  "创建时间上界（unix 秒）"
// @Success      200  {object}  response.Response  "成功"
// @Router       /api/clues [get]
func (c *ClueController) GetClueList(ctx *gin.Context) {
	var req dto.GetClueRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：page / limit / type / is_verify / start / end 必须是整数")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	clues, total, err := c.svc.ListWithQuery(ctx.Request.Context(), req)
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
			ID:           clue.ID,
			SourceID:     clue.SourceID,
			Account:      clue.Account,
			IsVerify:     clue.IsVerify,
			Name:         clue.Name,
			City:         clue.City,
			Address:      clue.Address,
			Type:         clue.Type,
			OwnerAccount: clue.OwnerAccount,
			CreateTime:   clue.CreateTime,
			UpdatedAt:    clue.UpdatedAt,
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
// @Router       /api/clues/delete/{id} [delete]
func (c *ClueController) DeleteClue(ctx *gin.Context) {
	var req dto.DeleteClueRequest
	if err := ctx.ShouldBindUri(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：缺少线索 ID")
		return
	}
	err := c.svc.DeleteClue(ctx.Request.Context(), req.ID)
	if err != nil {
		if errhttp.IsNotFoundError(err) || errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(ctx, fmt.Sprintf("线索不存在（id=%s）", req.ID))
			return
		}
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
	// 无数据时 Scan 出来是 nil，JSON 会序列化成 null；前端按数组消费，统一回 []。
	if statistics == nil {
		statistics = []map[string]any{}
	}
	response.Success(ctx, statistics, "获取线索统计成功")
}

// @Summary POST /api/clues/import
// @Tags      clue
// @Router    /api/clues/import [post]
func (c *ClueController) ImportClues(ctx *gin.Context) {
	var req []dto.ImportClueRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest,
			`请求参数错误：请求体必须是线索数组，例如 [{"name":"张三","account":"13800000000","type":"3"}]`)
		return
	}
	if len(req) == 0 {
		response.Error(ctx, http.StatusBadRequest, "导入数据不能为空")
		return
	}
	if len(req) > clueImportMaxBatch {
		response.Error(ctx, http.StatusBadRequest,
			fmt.Sprintf("单次导入不超过 %d 条，请分批导入（当前 %d 条）", clueImportMaxBatch, len(req)))
		return
	}

	for i, item := range req {
		clueType, err := strconv.ParseInt(item.Type, 10, 64)
		if err != nil {
			response.Error(ctx, http.StatusBadRequest,
				fmt.Sprintf("第 %d 条线索的 type 格式错误：必须是数字字符串（如 \"3\"），当前为 %q", i+1, item.Type))
			return
		}
		if !service.IsValidClueType(clueType) {
			response.Error(ctx, http.StatusBadRequest,
				fmt.Sprintf("第 %d 条线索的 type=%d 不是合法的线索类型（可用值见 GET /api/clues/type）", i+1, clueType))
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

// @Summary 获取线索类型列表
// @Tags      clue
// @Router    /api/clues/type [get]
// GetClueTypes 获取线索类型列表
func (c *ClueController) GetClueTypes(ctx *gin.Context) {
	types, err := c.svc.GetClueTypes(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, "获取线索类型失败")
		return
	}
	response.Success(ctx, types, "获取线索类型成功")
}
