package controller

import (
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// CustomerEventController 客户事件控制器
type CustomerEventController struct {
	tracker *service.EventTracker
}

// NewCustomerEventController 创建客户事件控制器
func NewCustomerEventController() *CustomerEventController {
	customerService := service.NewCustomerService()
	return &CustomerEventController{
		tracker: service.NewEventTracker(customerService),
	}
}

// TrackEvent 追踪事件
// @Summary 追踪客户事件
// @Description 追踪客户的各类行为事件，如页面浏览、点击、购买等
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, event_type=string, event_source=string, event_data=object} true "事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/track [post]
func (c *CustomerEventController) TrackEvent(ctx *gin.Context) {
	var req struct {
		CustomerID  string         `json:"customer_id" binding:"required"`
		EventType   string         `json:"event_type" binding:"required"`
		EventSource string         `json:"event_source"`
		EventData   map[string]any `json:"event_data"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 与 event_type 均为必填，event_source / event_data 可选")
		return
	}

	dto := &service.EventDTO{
		CustomerID:  req.CustomerID,
		EventType:   model.EventType(req.EventType),
		EventSource: model.EventSource(req.EventSource),
		EventData:   req.EventData,
	}

	if dto.EventSource == "" {
		dto.EventSource = model.EventSourceWebsite
	}

	if err := c.tracker.Track(ctx.Request.Context(), dto); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "事件追踪成功")
}

// GetEventHistory 获取客户事件历史
// @Summary 获取客户事件历史
// @Description 获取指定客户的事件历史记录
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param id path string true "客户 ID"
// @Param limit query int false "返回数量" default(50)
// @Success 200 {object} object{data=[]model.CustomerEvent} "获取成功"
// @Router /api/events/customer/{id} [get]
func (c *CustomerEventController) GetEventHistory(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	limit := 50
	if l := ctx.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	events, err := c.tracker.GetEventHistory(ctx.Request.Context(), customerID, limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, events, "获取成功")
}

// DeleteEvent 删除指定客户的所有事件
// @Summary 删除客户事件
// @Description 删除指定客户的所有事件记录
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param id path string true "客户 ID"
// @Success 200 {object} object{data=object{deleted_count=int}} "删除成功"
// @Router /api/events/customer/{id} [delete]
func (c *CustomerEventController) DeleteEvent(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	count, err := c.tracker.DeleteByCustomerID(ctx.Request.Context(), customerID)
	if err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, gin.H{"deleted_count": count}, "删除成功")
}

// @Summary      客户事件统计（按时间范围聚合）
// @Tags      customer-event
// @Router    /api/customer-events [get]
func (c *CustomerEventController) GetEventStats(ctx *gin.Context) {
	start := strings.TrimSpace(ctx.Query("start"))
	end := strings.TrimSpace(ctx.Query("end"))

	// 缺省区间必须是**业务日**串：service.EventTracker.GetStats 用 ParseBusinessDate
	// 解回来，两边口径要同源，否则默认窗口的首末日各差 8 小时。
	if start == "" {
		start = timeutil.BusinessDate(timeutil.TrailingMonthStart(time.Now()))
	}
	if end == "" {
		end = timeutil.BusinessToday()
	}

	// 显式传入的区间非法时不能静默回落到默认窗口：调用方会以为看到的是自己要求的区间。
	startTime, err := timeutil.ParseBusinessDate(start)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest,
			fmt.Sprintf("start 必须是业务日（YYYY-MM-DD），当前为 %q", start))
		return
	}
	endTime, err := timeutil.ParseBusinessDate(end)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest,
			fmt.Sprintf("end 必须是业务日（YYYY-MM-DD），当前为 %q", end))
		return
	}
	if startTime.After(endTime) {
		response.Error(ctx, http.StatusBadRequest, "start 不能晚于 end")
		return
	}

	stats, err := c.tracker.GetStats(ctx.Request.Context(), start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, stats, "获取成功")
}

// TrackPageView 追踪页面浏览事件（便捷接口）
// @Summary 追踪页面浏览事件
// @Description 追踪客户的页面浏览行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, url=string, title=string} true "页面浏览信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/pageview [post]
func (c *CustomerEventController) TrackPageView(ctx *gin.Context) {
	var req struct {
		CustomerID string `json:"customer_id" binding:"required"`
		URL        string `json:"url" binding:"required"`
		Title      string `json:"title"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 与 url 均为必填，title 可选")
		return
	}

	if err := c.tracker.TrackPageView(ctx.Request.Context(), req.CustomerID, req.URL, req.Title); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "页面浏览事件追踪成功")
}

// TrackClick 追踪点击事件（便捷接口）
// @Summary 追踪点击事件
// @Description 追踪客户的点击行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, element=string, target=string} true "点击事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/click [post]
func (c *CustomerEventController) TrackClick(ctx *gin.Context) {
	var req struct {
		CustomerID string `json:"customer_id" binding:"required"`
		Element    string `json:"element" binding:"required"`
		Target     string `json:"target"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 与 element 均为必填，target 可选")
		return
	}

	if err := c.tracker.TrackClick(ctx.Request.Context(), req.CustomerID, req.Element, req.Target); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "点击事件追踪成功")
}

// TrackPurchase 追踪购买事件（便捷接口）
// @Summary 追踪购买事件
// @Description 追踪客户的购买行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, amount=number, items=[]string} true "购买事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/purchase [post]
func (c *CustomerEventController) TrackPurchase(ctx *gin.Context) {
	var req struct {
		CustomerID string   `json:"customer_id" binding:"required"`
		Amount     float64  `json:"amount" binding:"required"`
		Items      []string `json:"items"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 必填，amount 必须是非零数字，items 为字符串数组")
		return
	}

	if err := c.tracker.TrackPurchase(ctx.Request.Context(), req.CustomerID, req.Amount, req.Items); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "购买事件追踪成功")
}

// TrackSignup 追踪注册事件（便捷接口）
// @Summary 追踪注册事件
// @Description 追踪客户的注册行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, signup_method=string} true "注册事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/signup [post]
func (c *CustomerEventController) TrackSignup(ctx *gin.Context) {
	var req struct {
		CustomerID   string `json:"customer_id" binding:"required"`
		SignupMethod string `json:"signup_method"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 必填，signup_method 可选")
		return
	}

	if err := c.tracker.TrackSignup(ctx.Request.Context(), req.CustomerID, req.SignupMethod); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "注册事件追踪成功")
}

// TrackLogin 追踪登录事件（便捷接口）
// @Summary 追踪登录事件
// @Description 追踪客户的登录行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, login_method=string} true "登录事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/login [post]
func (c *CustomerEventController) TrackLogin(ctx *gin.Context) {
	var req struct {
		CustomerID  string `json:"customer_id" binding:"required"`
		LoginMethod string `json:"login_method"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 必填，login_method 可选")
		return
	}

	if err := c.tracker.TrackLogin(ctx.Request.Context(), req.CustomerID, req.LoginMethod); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "登录事件追踪成功")
}

// TrackAddToCart 追踪加购事件（便捷接口）
// @Summary 追踪加购事件
// @Description 追踪客户的加购行为
// @Tags CDP-事件追踪
// @Accept json
// @Produce json
// @Param request body object{customer_id=string, product_id=string, product_name=string, price=number, quantity=int} true "加购事件信息"
// @Success 200 {object} object{message=string} "追踪成功"
// @Router /api/events/add-to-cart [post]
func (c *CustomerEventController) TrackAddToCart(ctx *gin.Context) {
	var req struct {
		CustomerID  string  `json:"customer_id" binding:"required"`
		ProductID   string  `json:"product_id" binding:"required"`
		ProductName string  `json:"product_name"`
		Price       float64 `json:"price" binding:"required"`
		Quantity    int     `json:"quantity"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：customer_id 与 product_id 必填，price 必须是非零数字，quantity 可选")
		return
	}

	if err := c.tracker.TrackAddToCart(ctx.Request.Context(), req.CustomerID, req.ProductID, req.ProductName, req.Price, req.Quantity); err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}

	response.Success(ctx, nil, "加购事件追踪成功")
}

// @Summary      分页查询全局客户事件列表
// @Tags      customer-event
// @Router    /api/customer-events/list [get]
func (c *CustomerEventController) ListGlobal(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	// 与仓储 ListGlobal 的夹取规则保持一致，回显的分页参数必须是真正生效的那组。
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	list, total, err := c.tracker.ListGlobalEvents(ctx.Request.Context(), ctx.Query("event_type"), limit, (page-1)*limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, "事件追踪失败")
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total, "page": page, "page_size": limit}, "ok")
}
