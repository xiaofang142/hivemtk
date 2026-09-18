package controller

import (
	"hivemtk-user/internal/model"
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
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
		response.ErrorFromDB(ctx, err, err.Error())
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
// @Router /api/events/customer/:id [get]
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
		response.ErrorFromDB(ctx, err, err.Error())
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
// @Router /api/events/customer/:id [delete]
func (c *CustomerEventController) DeleteEvent(ctx *gin.Context) {
	customerID := ctx.Param("id")
	if customerID == "" {
		response.Error(ctx, http.StatusBadRequest, "缺少客户 ID")
		return
	}

	count, err := c.tracker.DeleteByCustomerID(ctx.Request.Context(), customerID)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{"deleted_count": count}, "删除成功")
}

func (c *CustomerEventController) GetEventStats(ctx *gin.Context) {
	start := ctx.Query("start")
	end := ctx.Query("end")

	if start == "" {
		start = time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	}
	if end == "" {
		end = time.Now().Format("2006-01-02")
	}

	stats, err := c.tracker.GetStats(ctx.Request.Context(), start, end)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackPageView(ctx.Request.Context(), req.CustomerID, req.URL, req.Title); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackClick(ctx.Request.Context(), req.CustomerID, req.Element, req.Target); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackPurchase(ctx.Request.Context(), req.CustomerID, req.Amount, req.Items); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackSignup(ctx.Request.Context(), req.CustomerID, req.SignupMethod); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackLogin(ctx.Request.Context(), req.CustomerID, req.LoginMethod); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
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
		response.Error(ctx, http.StatusBadRequest, "请求参数错误："+err.Error())
		return
	}

	if err := c.tracker.TrackAddToCart(ctx.Request.Context(), req.CustomerID, req.ProductID, req.ProductName, req.Price, req.Quantity); err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, nil, "加购事件追踪成功")
}

func (c *CustomerEventController) ListGlobal(ctx *gin.Context) {
	limit, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "50"))
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	list, total, err := c.tracker.ListGlobalEvents(ctx.Request.Context(), ctx.Query("event_type"), limit, (page-1)*limit)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{"list": list, "total": total, "page": page, "page_size": limit}, "ok")
}
