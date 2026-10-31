package controller

import (
	"fmt"
	"hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/errhttp"
	"hivemtk-user/internal/pkg/utils/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// DashboardScreenController 数据大屏控制器
type DashboardScreenController struct {
	screenService *service.DashboardScreenService
}

// NewDashboardScreenController 创建数据大屏控制器实例
func NewDashboardScreenController() *DashboardScreenController {
	return &DashboardScreenController{
		screenService: service.NewDashboardScreenService(),
	}
}

// CreateScreen 创建大屏
func (c *DashboardScreenController) CreateScreen(ctx *gin.Context) {

	userID, ok := dashboardUserID(ctx)
	if !ok {
		return
	}

	var req service.CreateScreenRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		// 不再回显 gin/encoding-json 的原始报错（会带出 Go 结构体字段名）
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name 必填，layout / widgets 需为合法 JSON 结构")
		return
	}

	screen, err := c.screenService.CreateScreen(ctx.Request.Context(), userID, &req)
	if errhttp.HandleDBError(ctx, err, "创建大屏") {
		return
	}

	response.Success(ctx, screen, "创建成功")
}

// GetScreenList 获取大屏列表
func (c *DashboardScreenController) GetScreenList(ctx *gin.Context) {

	page, pageSize := dashboardPaging(ctx)

	screens, total, err := c.screenService.GetScreenList(ctx.Request.Context(), page, pageSize)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取大屏列表失败")
		return
	}

	response.Success(ctx, gin.H{
		"list":      screens,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取成功")
}

// GetScreenByID 获取大屏详情
func (c *DashboardScreenController) GetScreenByID(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的大屏 ID")
		return
	}

	screen, err := c.screenService.GetScreenByID(ctx.Request.Context(), uint(id))
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("大屏不存在（id=%d）", id))
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "获取大屏详情失败")
		return
	}

	response.Success(ctx, screen, "获取成功")
}

func (c *DashboardScreenController) requireScreenOwnership(ctx *gin.Context, id uint) (uid uint, ok bool) {
	uidAny, _ := ctx.Get("user_id")
	uid, _ = uidAny.(uint)
	if uid == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return 0, false
	}
	if isDashboardAdmin(ctx) {
		return uid, true
	}
	screen, err := c.screenService.GetScreenByID(ctx.Request.Context(), id)
	if err != nil || screen == nil {
		response.Error(ctx, http.StatusNotFound, "大屏不存在")
		return 0, false
	}
	if screen.CreatedBy != uid {
		response.Error(ctx, http.StatusForbidden, "仅创建者或管理员可操作该大屏")
		return 0, false
	}
	return uid, true
}

func (c *DashboardScreenController) UpdateScreen(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的大屏 ID")
		return
	}
	if _, ok := c.requireScreenOwnership(ctx, uint(id)); !ok {
		return
	}

	var req service.UpdateScreenRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, "请求参数错误：name / layout / widgets 需为与服务端一致的类型")
		return
	}

	screen, err := c.screenService.UpdateScreen(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("大屏不存在（id=%d）", id))
			return
		}
		errhttp.HandleDBError(ctx, err, "更新大屏")
		return
	}

	response.Success(ctx, screen, "更新成功")
}

// DeleteScreen 删除大屏
func (c *DashboardScreenController) DeleteScreen(ctx *gin.Context) {

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的大屏 ID")
		return
	}
	if _, ok := c.requireScreenOwnership(ctx, uint(id)); !ok {
		return
	}

	if err := c.screenService.DeleteScreen(ctx.Request.Context(), uint(id)); err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("大屏不存在（id=%d）", id))
			return
		}
		errhttp.HandleDBError(ctx, err, "删除大屏")
		return
	}

	response.Success(ctx, nil, "删除成功")
}

// PublicViewScreen 公开访问大屏
func (c *DashboardScreenController) PublicViewScreen(ctx *gin.Context) {
	code := ctx.Param("code")

	requestCtx := ctx.Request.Context()
	screen, err := c.screenService.GetPublicScreenCtx(requestCtx, code)
	if err != nil {
		if errhttp.IsNotFoundError(err) {
			response.NotFound(ctx, fmt.Sprintf("大屏不存在或未公开（code=%s）", code))
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "获取公开大屏失败")
		return
	}

	widgets, _ := c.screenService.GetScreenWidgetsCtx(requestCtx, screen.ID)

	response.Success(ctx, gin.H{
		"screen":  screen,
		"widgets": widgets,
	}, "获取成功")
}

// GetDashboardData 获取仪表板大屏实时聚合数据
// 该接口汇总线索、客户、订单、消息、抖音/小红书卡片等多源真实数据
// 用于前端大屏可视化展示,绝不返回模拟数据
// 架构修复：通过 Service 层访问 DB，不再直接调 db.GetDB；
// 权限隔离：admin 返回全量，普通用户按 data_scope 过滤（私域单租户当前统一返回全量）
func (c *DashboardScreenController) GetDashboardData(ctx *gin.Context) {
	data, err := c.screenService.AggregateDashboardData(ctx.Request.Context(), isDashboardAdmin(ctx))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "聚合大屏数据失败")
		return
	}
	response.Success(ctx, data, "获取仪表板数据成功")
}

// GetRealtimeActivities 获取实时活动
// 架构修复：通过 Service 层访问 DB
func (c *DashboardScreenController) GetRealtimeActivities(ctx *gin.Context) {
	activities, err := c.screenService.FetchRealtimeActivities(ctx.Request.Context(), dashboardActivityLimit(ctx))
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, "获取实时活动失败")
		return
	}
	response.Success(ctx, activities, "获取实时活动成功")
}

// dashboardPaging 归一化分页参数：page<1→1、page_size<1→10、>100→100。
// 仓储按 (page-1)*page_size 计算 offset，未归一化时 page=0 会算出负 offset。
func dashboardPaging(ctx *gin.Context) (int, int) {
	page, _ := strconv.Atoi(strings.TrimSpace(ctx.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(ctx.DefaultQuery("page_size", "10")))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// dashboardActivityLimit 实时活动条数：缺省 20，上限 100。
func dashboardActivityLimit(ctx *gin.Context) int {
	limit, _ := strconv.Atoi(strings.TrimSpace(ctx.DefaultQuery("limit", "20")))
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return limit
}

// dashboardUserID 从上下文取登录用户主键，缺失即回 401（原实现直接断言 bool，会 panic）。
func dashboardUserID(ctx *gin.Context) (uint, bool) {
	uidAny, _ := ctx.Get("user_id")
	uid, _ := uidAny.(uint)
	if uid == 0 {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return 0, false
	}
	return uid, true
}

// isDashboardAdmin 管理员口径：admin 与 super_admin 都算（与其他模块一致）。
func isDashboardAdmin(ctx *gin.Context) bool {
	roleAny, _ := ctx.Get("role")
	role, _ := roleAny.(string)
	return role == "admin" || role == "super_admin"
}
