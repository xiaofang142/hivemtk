package controller

import (
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// UserSegmentController 用户分层控制器
//
// H3（技术债清理）：底层已由 RFMCalculatorService 切换为全系统统一口径的
// CustomerRFMService。口径字段名差异适配：
//   - 分层标识：旧 layer(important_value/general_keep/...) → 新 segment(champion/loyal/at_risk/churn/potential)
//   - 主体标识：旧 user_id(uint, TgID) → 新 customer_id(string)
//   - 列表项：  旧 UserRFMWithUser → 新 model.CustomerRFM（r_score/f_score/m_score/composite_score/churn_* 等）
type UserSegmentController struct {
	rfmService *service.CustomerRFMService
}

// NewUserSegmentController 创建用户分层控制器
func NewUserSegmentController() *UserSegmentController {
	return &UserSegmentController{
		rfmService: service.NewCustomerRFMService(),
	}
}

// GetRFMRule 获取 RFM 规则
// 若未配置 RFM 规则（表为空或无 active 规则），返回成功态 + nil 数据，提示前端使用系统默认。
func (c *UserSegmentController) GetRFMRule(ctx *gin.Context) {
	rule, err := c.rfmService.GetRFMRule(ctx.Request.Context())
	if err != nil {
		if utils.IsRecordNotFound(err) {
			response.Success(ctx, nil, "RFM 规则未配置，将使用系统默认")
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, rule, "获取成功")
}

// ListRFMRules 列出所有 RFM 规则（分页）
func (c *UserSegmentController) ListRFMRules(ctx *gin.Context) {
	page := 1
	pageSize := 20
	if p := ctx.Query("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if ps := ctx.Query("page_size"); ps != "" {
		pageSize, _ = strconv.Atoi(ps)
		if pageSize > 100 {
			pageSize = 100
		}
	}
	rules, total, err := c.rfmService.ListRFMRules(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"list":      rules,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// SaveRFMRule 保存 RFM 规则
func (c *UserSegmentController) SaveRFMRule(ctx *gin.Context) {
	var req service.SaveRFMRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	rule, err := c.rfmService.SaveRFMRule(ctx.Request.Context(), &req)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, rule, "保存成功")
}

// UpdateRFMRule 更新 RFM 规则
func (c *UserSegmentController) UpdateRFMRule(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的规则 ID")
		return
	}

	var req service.SaveRFMRuleRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	rule, err := c.rfmService.UpdateRFMRule(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, rule, "更新成功")
}

// DeleteRFMRule 删除 RFM 规则
func (c *UserSegmentController) DeleteRFMRule(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的规则 ID")
		return
	}

	if HandleDBError(ctx, c.rfmService.DeleteRFMRule(ctx.Request.Context(), uint(id)), "删除 RFM 规则") {
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// GetRFMList 获取客户 RFM 列表（新口径：customer_rfm / segment）
// 兼容参数：layer 沿用旧查询键，值按 segment 解释；空值返回全量分页。
func (c *UserSegmentController) GetRFMList(ctx *gin.Context) {
	page := 1
	pageSize := 20

	if p := ctx.Query("page"); p != "" {
		page, _ = strconv.Atoi(p)
	}
	if ps := ctx.Query("page_size"); ps != "" {
		pageSize, _ = strconv.Atoi(ps)
		if pageSize > 100 {
			pageSize = 100
		}
	}
	if l := ctx.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	segment := ctx.Query("segment")
	if segment == "" {
		segment = ctx.Query("layer")
	}

	rfms, total, err := c.rfmService.ListBySegment(ctx.Request.Context(), segment, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      rfms,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetRFMStats 获取 RFM 统计（新口径：segment 分布）
// 保留 layer_count 键以兼容前端，同时提供 segment_count 同义键。
func (c *UserSegmentController) GetRFMStats(ctx *gin.Context) {
	dist, err := c.rfmService.Distribution(ctx.Request.Context())
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	totalUsers := int64(0)
	for _, count := range dist {
		totalUsers += count
	}

	response.Success(ctx, gin.H{
		"total_users":   totalUsers,
		"layer_count":   dist,
		"segment_count": dist,
	}, "获取成功")
}

// CalculateRFM 手动触发 RFM 全量计算（CustomerRFMService.ComputeAll，含挽回队列联动）
func (c *UserSegmentController) CalculateRFM(ctx *gin.Context) {
	count, err := c.rfmService.ComputeAll(ctx, 0)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"updated_count": count,
	}, "计算完成")
}

// GetUserRFM 获取单个客户的 RFM
// 口径适配：旧接口查询键 user_id 现按 customer_id 解释（string）。
func (c *UserSegmentController) GetUserRFM(ctx *gin.Context) {
	customerID := ctx.Query("customer_id")
	if customerID == "" {
		customerID = ctx.Query("user_id")
	}
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少 customer_id")
		return
	}

	rfm, err := c.rfmService.GetByCustomerID(ctx.Request.Context(), customerID)
	if err != nil {
		response.Error(ctx, http.StatusNotFound, "未找到客户 RFM 信息")
		return
	}

	response.Success(ctx, rfm, "获取成功")
}

// GetLayerDescription 获取分层说明（新口径 segment 五档）
func (c *UserSegmentController) GetLayerDescription(ctx *gin.Context) {
	layers := []map[string]string{
		{"layer": "champion", "name": "冠军客户", "desc": "最近消费、消费频次高——最优质客户群"},
		{"layer": "loyal", "name": "忠诚客户", "desc": "消费活跃且频次较高"},
		{"layer": "at_risk", "name": "流失风险客户", "desc": "消费频次或金额下滑，需重点维护"},
		{"layer": "potential", "name": "潜在客户", "desc": "有消费记录但活跃度一般"},
		{"layer": "churn", "name": "已流失客户", "desc": "超过流失阈值未消费，自动进入挽回队列"},
	}

	response.Success(ctx, layers, "获取成功")
}
