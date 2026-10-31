// csat_controller.go CSAT 控制器（五层 L2）
package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/reporting"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// CSATController 满意度控制器
type CSATController struct {
	svc *service.CSATService
}

// NewCSATController 构造
func NewCSATController() *CSATController {
	return &CSATController{svc: service.NewCSATService()}
}

// @Summary      评分已提交
// @Tags      c-s-a-t
// @Router    /api/customer-sessions/{id}/csat [post]
// Submit POST /api/customer-sessions/:id/csat {score, comment}
func (c *CSATController) Submit(ctx *gin.Context) {
	var req struct {
		Score   int    `json:"score" binding:"required"`
		Comment string `json:"comment"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：score 必填(1-5)")
		return
	}
	sessionID := ctx.Param("id")
	survey, err := c.svc.Submit(ctx.Request.Context(), sessionID, req.Score, req.Comment)
	if errhttp.IsNotFoundError(err) {
		// 与 Trigger 配套：没有调查单就无从评分。原先落到通用 404「记录不存在」，
		// 前端看不出是会话错了还是这张单不存在。
		response.NotFound(ctx, fmt.Sprintf("该会话没有待评价的满意度调查单（session_id=%s）", sessionID))
		return
	}
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, survey, "评分已提交")
}

// @Summary      调查已触发
// @Tags      c-s-a-t
// @Router    /api/customer-sessions/{id}/csat/trigger [post]
// Trigger POST /api/customer-sessions/:id/csat/trigger
func (c *CSATController) Trigger(ctx *gin.Context) {
	sessionID := ctx.Param("id")
	survey, err := c.svc.Trigger(ctx.Request.Context(), sessionID, "manual")
	if errhttp.IsNotFoundError(err) {
		response.NotFound(ctx, fmt.Sprintf("会话不存在（id=%s）", sessionID))
		return
	}
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, survey, "调查已触发")
}

// @Summary      CSAT 满意度统计（可按时间窗）
// @Tags      c-s-a-t
// @Router    /api/csat/stats [get]
// Stats GET /api/csat/stats?window=month|week|7d|30d（缺省=全量）
func (c *CSATController) Stats(ctx *gin.Context) {
	stats, err := c.svc.Stats(ctx.Request.Context(), ctx.Query("window"))
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, stats, "ok")
}

// @Summary      CSAT 满意度趋势（按天）
// @Tags      c-s-a-t
// @Router    /api/csat/trend [get]
// Trend GET /api/csat/trend?days=30
func (c *CSATController) Trend(ctx *gin.Context) {
	days := queryInt(ctx, "days", reporting.DefaultWindowDays(ctx.Request.Context()))
	list, err := c.svc.Trend(ctx.Request.Context(), days)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list)}, "ok")
}

// @Summary      查询差评会话列表
// @Tags      c-s-a-t
// @Router    /api/csat/negative [get]
// Negative GET /api/csat/negative
func (c *CSATController) Negative(ctx *gin.Context) {
	limit := queryInt(ctx, "limit", 50)
	list, threshold, err := c.svc.Negative(ctx.Request.Context(), limit)
	if HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": len(list), "threshold": threshold}, "ok")
}

// @Summary      获取 CSAT 评价模板
// @Tags      c-s-a-t
// @Router    /api/csat/template [get]
// GetTemplate GET /api/csat/template
func (c *CSATController) GetTemplate(ctx *gin.Context) {
	response.Success(ctx, c.svc.GetTemplate(ctx.Request.Context()), "ok")
}

// @Summary      模板已保存
// @Tags      c-s-a-t
// @Router    /api/csat/template [put]
// SaveTemplate PUT /api/csat/template
func (c *CSATController) SaveTemplate(ctx *gin.Context) {
	var req map[string]any
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// 别把 encoding/json 的原文（含 Go 类型名）回给前端。
		response.Error(ctx, http.StatusBadRequest, `请求参数错误：请求体必须是 JSON 对象，形如 {"content":"请给个评价","low_threshold":3}`)
		return
	}
	// 防御空 body {} 把模板配置清空
	if len(req) == 0 {
		response.Error(ctx, http.StatusBadRequest, "模板内容不能为空")
		return
	}
	// low_threshold 是「好评/差评」的唯一事实源（服务层按它算差评数与差评列表），
	// 类型错了会被静默忽略并回落 3 —— 于是看板与列表各说各话，所以入口就挡。
	if v, ok := req["low_threshold"]; ok {
		f, isNum := toFloat(v)
		if !isNum || f < 1 || f > 5 {
			response.Error(ctx, http.StatusBadRequest, "low_threshold 必须是 1-5 之间的数字（差评阈值）")
			return
		}
		req["low_threshold"] = f
	}
	if v, ok := req["content"]; ok {
		text, isStr := v.(string)
		if !isStr || len([]rune(text)) == 0 || len([]rune(text)) > 500 {
			response.Error(ctx, http.StatusBadRequest, "content 必须是 1-500 个字符的文本")
			return
		}
	}
	if err := c.svc.SaveTemplate(ctx.Request.Context(), req); HandleServiceError(ctx, err) {
		return
	}
	response.Success(ctx, req, "模板已保存")
}

// toFloat 兼容 JSON 数字（float64）与数字字符串（"3"）两种来源。
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func queryInt(ctx *gin.Context, key string, def int) int {
	raw := ctx.Query(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
