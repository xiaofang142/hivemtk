package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// IntentController 意图识别控制器
type IntentController struct {
	rec *service.IntentRecognizer
}

// NewIntentController 创建意图识别控制器
func NewIntentController(rec *service.IntentRecognizer) *IntentController {
	return &IntentController{
		rec: rec,
	}
}

// RecognizeRequest 识别请求
//
// 同时兼容两种入参风格（前端 / 销冠 / 自动化测试共用）：
//   - 前端：{message, context?, customer_id?, platform?}
//   - 销冠/服务端：{text, session_id?, customer_id?}
type RecognizeRequest struct {
	SessionID  string `json:"session_id"`
	CustomerID string `json:"customer_id"`
	Text       string `json:"text"`
	Message    string `json:"message"`
	Context    string `json:"context"`
	Platform   string `json:"platform"`
}

func (r *RecognizeRequest) resolveText() string {
	if r.Text != "" {
		return r.Text
	}
	return r.Message
}

// Recognize godoc
// @Summary      单条意图识别
// @Description  对单条用户消息做意图分类，返回意图编码与置信度
// @Tags         Intent
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  RecognizeRequest  true  "识别请求（text 或 message 二选一）"
// @Success      200   {object}  response.Response  "成功"
// @Router       /api/intent/recognize [post]
func (c *IntentController) Recognize(ctx *gin.Context) {
	var req RecognizeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	text := req.resolveText()
	if text == "" {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: text/message 不能为空")
		return
	}
	result, err := c.rec.Recognize(ctx.Request.Context(), req.SessionID, req.CustomerID, text)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, result, "识别成功")
}

// BatchRecognizeRequest 批量识别请求
//
// 兼容两种入参风格：
//   - 复杂：{items: [{session_id, customer_id, text}, ...]}
//   - 简单：{messages: ["msg1", "msg2", ...]}（前端/批量测试使用）
type BatchRecognizeRequest struct {
	Items    []RecognizeRequest `json:"items"`
	Messages []string           `json:"messages"`
}

// BatchRecognize godoc
// @Summary      批量意图识别
// @Description  支持 items 或 messages 两种入参风格
// @Tags         Intent
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  BatchRecognizeRequest  true  "批量请求"
// @Success      200   {object}  response.Response  "成功"
// @Router       /api/intent/batch [post]
func (c *IntentController) BatchRecognize(ctx *gin.Context) {
	var req BatchRecognizeRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}

	type item struct {
		sessionID  string
		customerID string
		text       string
	}
	normalized := make([]item, 0, len(req.Items)+len(req.Messages))
	for _, it := range req.Items {
		normalized = append(normalized, item{sessionID: it.SessionID, customerID: it.CustomerID, text: it.resolveText()})
	}
	for _, m := range req.Messages {
		normalized = append(normalized, item{text: m})
	}
	if len(normalized) == 0 {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: items/messages 至少需要一个")
		return
	}

	out := make([]*dto.RecognizeResult, 0, len(normalized))
	for _, it := range normalized {
		if it.text == "" {
			continue
		}
		r, err := c.rec.Recognize(ctx.Request.Context(), it.sessionID, it.customerID, it.text)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	response.Success(ctx, out, "批量识别成功")
}

// Stats 意图统计
func (c *IntentController) Stats(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	stats, err := c.rec.GetIntentStats(ctx.Request.Context(), days)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	byIntent := map[string]int{}
	total := 0
	for k, v := range stats {
		byIntent[k] = v
		total += v
	}
	byMethod, byLevel := c.rec.GetMethodLevelStats(ctx.Request.Context(), days)
	distribution := make([]map[string]any, 0, len(byIntent))
	for k, v := range byIntent {
		distribution = append(distribution, map[string]any{"type": k, "count": v})
	}
	sortDistributionDesc(distribution)
	resp := map[string]any{
		"total":        total,
		"period_days":  days,
		"distribution": distribution,
		"by_intent":    byIntent,
		"by_method":    byMethod,
		"by_level":     byLevel,
	}
	response.Success(ctx, resp, "查询成功")
}

func sortDistributionDesc(arr []map[string]any) {
	for i := 1; i < len(arr); i++ {
		for j := i; j > 0; j-- {
			ci, _ := arr[j]["count"].(int)
			cj, _ := arr[j-1]["count"].(int)
			if ci > cj {
				arr[j], arr[j-1] = arr[j-1], arr[j]
			}
		}
	}
}

// RecentIntents 客户近期意图（分页 + 意图筛选）
func (c *IntentController) RecentIntents(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")
	intentType := ctx.Query("intent_type")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	list, total, err := c.rec.GetRecentIntentsPaged(ctx.Request.Context(), customerID, intentType, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	if list == nil {
		list = []model.IntentRecord{}
	}
	resp := map[string]any{
		"list":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}
	response.Success(ctx, resp, "查询成功")
}

// Intents 意图词典
func (c *IntentController) Intents(ctx *gin.Context) {
	response.Success(ctx, service.DefaultIntents, "查询成功")
}

// RecognizeFineRequest 精细识别请求
type RecognizeFineRequest struct {
	Message    string `json:"message" binding:"required"`
	CustomerID string `json:"customer_id"`
	SessionID  string `json:"session_id"`
}

// RecognizeFine 精细意图识别
// POST /api/intent/recognize/fine
func (c *IntentController) RecognizeFine(ctx *gin.Context) {
	var req RecognizeFineRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	result, err := c.rec.RecognizeIntent(ctx.Request.Context(), req.Message, req.CustomerID, req.SessionID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, result, "识别成功")
}

// IntentLogs 查询意图识别日志
// GET /api/intent/logs?customer_id=xxx&major=xxx&limit=100
func (c *IntentController) IntentLogs(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")
	major := ctx.Query("major")
	limit, _ := strconv.Atoi(ctx.DefaultQuery("limit", "100"))
	logs, err := c.rec.GetIntentLogs(ctx.Request.Context(), customerID, major, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, logs, "查询成功")
}

// IntentStatsFine 精细意图统计
// GET /api/intent/stats/fine?days=7
func (c *IntentController) IntentStatsFine(ctx *gin.Context) {
	days, _ := strconv.Atoi(ctx.DefaultQuery("days", "7"))
	stats, err := c.rec.GetIntentLogStats(ctx.Request.Context(), days)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, stats, "查询成功")
}

// GetConfig 获取意图识别配置
// GET /api/intent/config
//
// 返回值：
//   - enabled:          是否启用意图识别（true=进规则+LLM 流程；false=直接兜底）
//   - updated_at:       最近一次更新时间
//   - updated_by:       最近一次更新人
func (c *IntentController) GetConfig(ctx *gin.Context) {
	cfg, err := service.LoadIntentConfig(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, cfg, "查询成功")
}

// UpdateConfigRequest 更新意图识别配置请求体
// Enabled 用 *bool 表达必填：nil = 未传（空 body {} 不得把开关静默置为 false）
type UpdateConfigRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// UpdateConfig 更新意图识别配置
// PUT /api/intent/config
//
// 行为：
//  1. 持久化到 system_config_kv 表
//  2. 立即更新内存态 IntentEnabled
//  3. 不重启服务即可生效
func (c *IntentController) UpdateConfig(ctx *gin.Context) {
	var req UpdateConfigRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.Enabled == nil {
		response.Error(ctx, http.StatusBadRequest, "enabled 必填")
		return
	}

	updatedBy := "admin"
	if v, ok := ctx.Get("username"); ok && v != nil {
		if s, ok := v.(string); ok && s != "" {
			updatedBy = s
		}
	}

	cfg := &service.IntentConfig{
		Enabled:   *req.Enabled,
		UpdatedAt: time.Now().Format(time.RFC3339),
		UpdatedBy: updatedBy,
	}
	if err := service.UpdateIntentConfig(ctx.Request.Context(), cfg); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, cfg, "更新成功")
}

// GetKeywordOverride GET /api/intent-records/keywords-override
func (c *IntentController) GetKeywordOverride(ctx *gin.Context) {
	response.Success(ctx, service.GetIntentKeywordOverride(), "查询成功")
}

// UpdateKeywordOverrideRequest 覆盖词表请求体（意图类型 → 追加关键词列表）
type UpdateKeywordOverrideRequest struct {
	Override map[string][]string `json:"override"`
}

// UpdateKeywordOverride PUT /api/intent-records/keywords-override
//
// 覆盖语义为追加（不删除默认词表），保存即热生效。
func (c *IntentController) UpdateKeywordOverride(ctx *gin.Context) {
	var req UpdateKeywordOverrideRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return
	}
	if req.Override == nil {
		req.Override = map[string][]string{}
	}

	valid := make(map[string]bool, len(service.DefaultIntents))
	for _, def := range service.DefaultIntents {
		valid[def.Type] = true
	}
	cleaned := make(map[string][]string, len(req.Override))
	for k, words := range req.Override {
		if !valid[k] {
			continue
		}
		kept := make([]string, 0, len(words))
		for _, w := range words {
			if s := strings.TrimSpace(w); s != "" && len(s) <= 64 {
				kept = append(kept, s)
			}
		}
		cleaned[k] = kept
	}
	if err := service.SaveIntentKeywordOverride(ctx.Request.Context(), cleaned); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"types": len(cleaned)}, "词表已更新并热生效")
}
