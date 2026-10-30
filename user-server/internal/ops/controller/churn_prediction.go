package controller

import (
	"errors"
	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ChurnPredictionController 流失预警控制器
type ChurnPredictionController struct {
	churnService *service.ChurnPredictionService
}

// NewChurnPredictionController 创建流失预警控制器实例
func NewChurnPredictionController() *ChurnPredictionController {
	return &ChurnPredictionController{
		churnService: service.NewChurnPredictionService(),
	}
}

// churnPaging 解析并归一化分页参数。
//
// 仓储会把 page<1 夹到 1、page_size 夹到 1..200，但控制器原来把原始值原样回显：
// `?page=0&page_size=999` 实际返回第 1 页 / 200 条，响应里却写 page=0 / page_size=999，
// 前端据此算总页数会得到 0 页。这里与仓储保持同一套归一化规则。
func churnPaging(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

// GetChurnPrediction 获取用户流失预测
func (c *ChurnPredictionController) GetChurnPrediction(ctx *gin.Context) {

	userID := ctx.Query("user_id")
	if userID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少用户 ID 参数")
		return
	}

	prediction, err := c.churnService.GetChurnPrediction(ctx.Request.Context(), userID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, prediction, "获取成功")
}

// GetChurnPredictions 获取流失预测列表
func (c *ChurnPredictionController) GetChurnPredictions(ctx *gin.Context) {

	page, pageSize := churnPaging(ctx)

	predictions, total, err := c.churnService.GetChurnPredictions(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      predictions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetHighRiskUsers 获取高风险用户列表
func (c *ChurnPredictionController) GetHighRiskUsers(ctx *gin.Context) {

	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "50"))

	users, err := c.churnService.GetHighRiskUsers(ctx.Request.Context(), limit)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, users, "获取成功")
}

// GetChurnWarnings 获取流失预警列表
func (c *ChurnPredictionController) GetChurnWarnings(ctx *gin.Context) {

	page, pageSize := churnPaging(ctx)

	warnings, total, err := c.churnService.GetChurnWarnings(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      warnings,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetUnhandledWarnings 获取未处理的流失预警
func (c *ChurnPredictionController) GetUnhandledWarnings(ctx *gin.Context) {

	page, pageSize := churnPaging(ctx)

	warnings, total, err := c.churnService.GetUnhandledWarnings(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      warnings,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// MarkWarningHandled 标记预警为已处理
func (c *ChurnPredictionController) MarkWarningHandled(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的预警 ID")
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	// 前端允许不填备注，空 body 应当等同 Note=""，不能回「请求参数错误：EOF」。
	if err := ctx.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：note 必须是字符串")
		return
	}

	userID, _ := ctx.Get("user_id")
	handledBy, _ := userID.(uint)

	err = c.churnService.MarkWarningHandled(ctx.Request.Context(), uint(id), handledBy, req.Note)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(ctx, "预警不存在（id="+idStr+"）")
		return
	}
	if errhttp.HandleDBError(ctx, err, "标记预警") {
		return
	}

	response.Success(ctx, nil, "标记成功")
}

// InterveneUser 对流失预警用户进行干预
//
// 入参 JSON 二选一：
//
//	{ "warning_id": 1, "intervention_type": "discount" }   —— 按预警 ID
//	{ "user_id": "u-1", "intervention_type": "discount" }  —— 按用户取最新未处理预警
//
// 预测列表的行里只有 user_id（没有预警 ID），前端原来把它当 warning_id 传，
// 字符串 UUID 直接 400、纯数字则误标记别人的预警。支持 user_id 才对得上按钮语义。
func (c *ChurnPredictionController) InterveneUser(ctx *gin.Context) {
	var req struct {
		WarningID        uint   `json:"warning_id"`
		UserID           string `json:"user_id"`
		InterventionType string `json:"intervention_type"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：warning_id 必须是数字、user_id 必须是字符串")
		return
	}
	if req.InterventionType == "" {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：intervention_type 必填")
		return
	}
	if req.WarningID == 0 && req.UserID == "" {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：warning_id 或 user_id 至少提供一个")
		return
	}

	userID, _ := ctx.Get("user_id")
	handledBy, _ := userID.(uint)

	warningID := req.WarningID
	if warningID == 0 {
		id, err := c.churnService.InterveneLatestByUser(ctx.Request.Context(), req.UserID, handledBy, req.InterventionType)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(ctx, "该用户没有待处理的流失预警（user_id="+req.UserID+"）")
			return
		}
		if errhttp.HandleDBError(ctx, err, "干预预警") {
			return
		}
		warningID = id
	} else {
		err := c.churnService.InterveneWarning(ctx.Request.Context(), warningID, handledBy, req.InterventionType)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.NotFound(ctx, "预警不存在（id="+strconv.FormatUint(uint64(warningID), 10)+"）")
			return
		}
		if errhttp.HandleDBError(ctx, err, "干预预警") {
			return
		}
	}

	response.Success(ctx, gin.H{
		"warning_id":        warningID,
		"user_id":           req.UserID,
		"intervention_type": req.InterventionType,
	}, "干预成功")
}

// GetModelConfig 获取模型配置
func (c *ChurnPredictionController) GetModelConfig(ctx *gin.Context) {

	config, err := c.churnService.GetModelConfig(ctx.Request.Context())
	if errhttp.HandleDBError(ctx, err, "获取模型配置") {
		return
	}

	response.Success(ctx, config, "获取成功")
}

// SaveModelConfig 保存模型配置
func (c *ChurnPredictionController) SaveModelConfig(ctx *gin.Context) {

	var config model.ChurnModelConfig
	if err := ctx.ShouldBindJSON(&config); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：权重与阈值必须是数字（字段名见 GET /api/churn-prediction/model-config）")
		return
	}

	if errhttp.HandleDBError(ctx, c.churnService.SaveModelConfig(ctx.Request.Context(), &config), "保存模型配置") {
		return
	}

	// 回读落库结果：Upsert 拿到的就是请求体那份结构体，tenant_id 与时间戳都是零值，
	// 直接回显会让前端以为「保存后租户被清空、创建时间回到 0001 年」。
	if saved, err := c.churnService.GetModelConfig(ctx.Request.Context()); err == nil {
		response.Success(ctx, saved, "保存成功")
		return
	}
	response.Success(ctx, config, "保存成功")
}

// GetChurnStatistics 获取流失统计
//
// 同类修复：start_date / end_date 为必填参数，缺失时返回 400
// （不允许用默认值/mock 数据兜底，符合"不允许使用模拟数据"硬约束）。
// 前端页面初次加载由前端主动传入最近 30 天日期。
func (c *ChurnPredictionController) GetChurnStatistics(ctx *gin.Context) {

	startDate := ctx.Query("start_date")
	endDate := ctx.Query("end_date")

	if startDate == "" || endDate == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少必要参数：start_date / end_date")
		return
	}

	stats, err := c.churnService.GetChurnStatistics(ctx.Request.Context(), startDate, endDate)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// GetRiskDistribution 获取风险分布
func (c *ChurnPredictionController) GetRiskDistribution(ctx *gin.Context) {

	distribution, err := c.churnService.GetRiskDistribution(ctx.Request.Context())
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, distribution, "获取成功")
}
