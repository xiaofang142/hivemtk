package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/pagination"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/platform"

	"github.com/gin-gonic/gin"
)

type PlatformController struct {
	platformClient *platform.Client
}

func NewPlatformController() *PlatformController {
	var client *platform.Client
	merchantKey := platform.GetMerchantKey()
	if merchantKey != "" {
		client = platform.NewPlatformClient(merchantKey)
	}
	return &PlatformController{
		platformClient: client,
	}
}

func (pc *PlatformController) platformCall(c *gin.Context, method, path string, req, resp any, errMsg string) {
	if pc.platformData(c, method, path, req, resp, errMsg) {
		response.Success(c, resp, "获取成功")
	}
}

func (pc *PlatformController) platformData(c *gin.Context, method, path string, req, resp any, errMsg string) bool {
	if pc.platformClient == nil {

		response.Success(c, resp, "平台未初始化，返回空数据")
		return false
	}
	ok, err := pc.platformDataRaw(c, method, path, req, resp)
	if ok {
		return true
	}

	// R11 起"平台答话了但拒绝"会上抛 *PlatformError，与"根本没答话"分得开：
	// 混成一句"平台不可达"会把商户被停用/签名不对推给网络排查。
	reason := "平台不可达"
	var perr *platform.PlatformError
	if errors.As(err, &perr) && perr.Resp != nil && perr.Resp.Code != 0 {
		reason = fmt.Sprintf("平台拒绝(code=%d): %s", perr.Resp.Code, perr.Resp.Msg)
	}
	response.Success(c, resp, errMsg+"（"+reason+"，返回空数据）")
	return false
}

func (pc *PlatformController) platformDataRaw(c *gin.Context, method, path string, req, resp any) (bool, error) {
	if pc.platformClient == nil {
		return false, platform.ErrPlatformNotConfigured
	}

	const platformCacheTTL = 20 * time.Second
	cacheKey := method + ":" + path
	if method == http.MethodGet {
		if gc := cache.GetGlobalCache(); gc != nil {
			if err := gc.GetJSON(c.Request.Context(), cacheKey, resp); err == nil {
				return true, nil
			}
		}
	}

	var wrapper struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := pc.platformClient.Do(method, path, req, &wrapper); err != nil {
		return false, err
	}

	if len(wrapper.Data) > 0 && string(wrapper.Data) != "null" {
		if err := json.Unmarshal(wrapper.Data, resp); err != nil {
			return false, fmt.Errorf("响应解析失败: %w", err)
		}
	}

	if method == http.MethodGet {
		if gc := cache.GetGlobalCache(); gc != nil {
			_ = gc.SetJSON(c.Request.Context(), cacheKey, resp, platformCacheTTL)
		}
	}
	return true, nil
}

// RegisterMerchant 保留给平台集成的手动补注册：生产路由里已不再注册它
// （旧形态是 public 组下的 POST /api/platform/register，那条 404 由
// router/authorization_routes_gone_test.go 钉住）。
// 仓内现在唯一的挂载点是 controller/platform_test.go 的本地路径，
// 断言的是"平台不可达/被拒时原因要原样带出去"这一格。
func (pc *PlatformController) RegisterMerchant(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		ContactEmail string `json:"contact_email"`
		ContactPhone string `json:"contact_phone"`
		DeviceInfo   string `json:"device_info"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}

	platformReq := platform.RegisterMerchantReq{
		Name:         req.Name,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
		DeviceInfo:   req.DeviceInfo,
	}

	var resp platform.BaseResp
	pc.platformCall(c, "POST", "/merchant-api/merchant/register", platformReq, &resp, "注册商户失败")
}

// 平台端方法 - 驾驶舱 - GET /platform/dashboard
func (pc *PlatformController) GetDashboard(c *gin.Context) {
	var resp struct {
		TotalMerchants  int64   `json:"total_merchants"`
		ActiveMerchants int64   `json:"active_merchants"`
		TotalRevenue    float64 `json:"total_revenue"`
		TodayNew        int     `json:"today_new"`
	}
	pc.platformCall(c, "GET", "/platform/dashboard", nil, &resp, "获取驾驶舱失败")
}

func (pc *PlatformController) GetMerchantList(c *gin.Context) {
	page, pageSize, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	status := c.Query("status")

	var resp struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}

	path := fmt.Sprintf("/platform/merchants/list?page=%d&page_size=%d", page, pageSize)
	if status != "" {
		path += "&status=" + status
	}

	pc.platformCall(c, "GET", path, nil, &resp, "获取商户列表失败")
}

func (pc *PlatformController) GetMerchantStats(c *gin.Context) {
	merchantID := c.Param("id")
	if merchantID == "" {
		response.Error(c, http.StatusBadRequest, "商户ID不能为空")
		return
	}

	var resp struct {
		TotalUsers   int64   `json:"total_users"`
		ActiveUsers  int64   `json:"active_users"`
		TotalRevenue float64 `json:"total_revenue"`
	}
	path := fmt.Sprintf("/platform/merchants/%s/stats", c.Param("id"))
	pc.platformCall(c, "GET", path, nil, &resp, "获取商户统计失败")
}

// 平台端方法 - 站内信列表 - GET /platform/message/list
func (pc *PlatformController) GetMessageList(c *gin.Context) {
	page, pageSize, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var resp struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}

	path := fmt.Sprintf("/platform/message/list?page=%d&page_size=%d", page, pageSize)
	pc.platformCall(c, "GET", path, nil, &resp, "获取站内信列表失败")
}

// 平台端方法 - 发送站内信 - POST /platform/message/send
func (pc *PlatformController) SendMessage(c *gin.Context) {
	var req struct {
		Title   string   `json:"title" binding:"required"`
		Content string   `json:"content" binding:"required"`
		Type    string   `json:"type"`
		Targets []string `json:"targets"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}

	var resp map[string]any
	pc.platformCall(c, "POST", "/platform/message/send", req, &resp, "发送站内信失败")
}

// 平台端方法 - 标记站内信已读 - POST /platform/message/{id}/read
func (pc *PlatformController) MarkPlatformMessageRead(c *gin.Context) {
	messageID := c.Param("id")
	if messageID == "" {
		response.Error(c, http.StatusBadRequest, "消息ID不能为空")
		return
	}

	path := fmt.Sprintf("/platform/message/%s/read", messageID)
	var resp map[string]any
	pc.platformCall(c, "POST", path, nil, &resp, "标记站内信已读失败")
}

// 平台端方法 - 最新站内信 - GET /platform/message/latest
// 供前端 MessageNotification 全局轮询。平台客户端未初始化或平台不可达/无该端点时，
// 静默返回 null（而非 404/5xx），避免每次页面加载都产生控制台噪声。
// 注意：必须走 platformData 而非 platformCall——后者会先写出完整列表响应，
// 本方法再写单条响应，导致响应体出现两段拼接的 JSON（历史双写 bug）。
func (pc *PlatformController) GetLatestMessage(c *gin.Context) {
	if pc.platformClient == nil {
		response.Success(c, nil, "")
		return
	}
	var resp struct {
		List []map[string]any `json:"list"`
	}

	if ok, _ := pc.platformDataRaw(c, http.MethodGet, "/platform/message/list?page=1&page_size=1", nil, &resp); !ok {
		response.Success(c, nil, "")
		return
	}
	if len(resp.List) > 0 {
		response.Success(c, resp.List[0], "")
		return
	}
	response.Success(c, nil, "")
}

// 平台端方法 - 用户管理 - GET /platform/user/list
func (pc *PlatformController) GetUserList(c *gin.Context) {
	page, pageSize, err := pagination.Parse(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	var resp struct {
		List  []map[string]any `json:"list"`
		Total int64            `json:"total"`
	}

	path := fmt.Sprintf("/platform/user/list?page=%d&page_size=%d", page, pageSize)
	pc.platformCall(c, "GET", path, nil, &resp, "获取用户列表失败")
}

// 平台端方法 - 创建用户 - POST /platform/user/create
func (pc *PlatformController) CreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role" binding:"required"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "参数错误")
		return
	}

	var resp map[string]any
	pc.platformCall(c, "POST", "/platform/user/create", req, &resp, "创建用户失败")
}

// 平台端方法 - 删除用户 - DELETE /platform/user/{id}
func (pc *PlatformController) DeleteUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, http.StatusBadRequest, "用户ID不能为空")
		return
	}

	path := fmt.Sprintf("/platform/user/%s", userID)
	var resp map[string]any
	pc.platformCall(c, "DELETE", path, nil, &resp, "删除用户失败")
}

// 平台端方法 - 系统统计 - GET /platform/stats/system
func (pc *PlatformController) GetSystemStats(c *gin.Context) {
	var resp struct {
		ServerTime  string  `json:"server_time"`
		Uptime      float64 `json:"uptime"`
		MemoryUsage float64 `json:"memory_usage"`
		CPUUsage    float64 `json:"cpu_usage"`
		DiskUsage   float64 `json:"disk_usage"`
	}
	pc.platformCall(c, "GET", "/platform/stats/system", nil, &resp, "获取系统统计失败")
}

// 平台端方法 - 平台总览统计 - GET /platform/stats/overview
func (pc *PlatformController) GetPlatformStats(c *gin.Context) {
	var resp struct {
		TotalMerchants  int64   `json:"total_merchants"`
		ActiveMerchants int64   `json:"active_merchants"`
		TotalUsers      int64   `json:"total_users"`
		TotalRevenue    float64 `json:"total_revenue"`
		TodayStats      struct {
			NewMerchants int     `json:"new_merchants"`
			ActiveUsers  int     `json:"active_users"`
			Revenue      float64 `json:"revenue"`
		} `json:"today_stats"`
	}
	pc.platformCall(c, "GET", "/platform/stats/overview", nil, &resp, "获取平台统计失败")
}

func (pc *PlatformController) GetPlatformMerchantStats(c *gin.Context) {
	days := c.DefaultQuery("days", "7")

	var resp struct {
		Days  int              `json:"days"`
		Stats []map[string]any `json:"stats"`
	}

	path := fmt.Sprintf("/platform/stats/merchant?days=%s", days)
	pc.platformCall(c, "GET", path, nil, &resp, "获取商户统计失败")
}
