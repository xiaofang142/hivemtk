package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// PlatformConnectivityController 平台端连通性探测
//
// 为什么单独一个 controller 而不挂在 PlatformController 上：PlatformController
// 持有的是**已装配的**平台客户端（构造时读 merchant key），而连通性探测要在
// 「地址刚改完、客户端还没重建」的时刻也能用 —— 平台端迁移后运维第一件事就是
// 探一下新地址通不通，而这个时刻的地址可能来自参数中心（管理端刚改的），
// 与客户端构造时读到的地址未必是同一个。
type PlatformConnectivityController struct{}

func NewPlatformConnectivityController() *PlatformConnectivityController {
	return &PlatformConnectivityController{}
}

// platformConnectivityResp 连通性探测结果
type platformConnectivityResp struct {
	// Address 实际探测的地址（已 trim 尾斜杠）
	Address string `json:"address"`
	// Source 地址来源：config_param / yaml / env / empty
	Source string `json:"source"`
	// Reachable 平台 /health 是否返回 200
	Reachable bool `json:"reachable"`
	// PlatformEnabled 平台集成开关当前状态
	PlatformEnabled bool `json:"platform_enabled"`
	// Status unreachable / not_configured / ok
	Status string `json:"status"`
	// Message 人类可读的结论
	Message string `json:"message"`
	// LatencyMs 探测耗时
	LatencyMs int64 `json:"latency_ms"`
}

// @Summary POST /api/manage/platform-host/connectivity
// @Tags      platform-connectivity
// @Router    /api/manage/platform-host/connectivity [post]
// TestConnectivity POST /api/manage/platform-host/connectivity
//
// 请求体（可空）：{"address": "https://new-platform.example.com"}
//   - 传了 address ⇒ 探测**这个**地址（用于「还没保存，先试一下」）
//   - 不传 ⇒ 探测**当前生效**的地址（参数中心 > yaml > env）
//
// 探针是 GET {host}/health，刻意**不走鉴权链**（不做商户签名、不拉 JWT）：
// 这是一个连通性探针，把它绑到鉴权链上会多造两类假故障 —— 签名没配好、
// 平台管理员密码不对，都会被读成"平台挂了"，把排查方向带偏。
//
// 平台端 /health 的定义见 hivemtk-platform/platform-server/internal/router/router.go
// （r.GET("/health", LivenessCheck())），不要求任何凭据。
func (c *PlatformConnectivityController) TestConnectivity(ctx *gin.Context) {
	var req struct {
		Address string `json:"address"`
	}
	// 绑定失败不阻断：body 可以完全为空。
	_ = ctx.ShouldBindJSON(&req)

	target := strings.TrimSpace(req.Address)
	source := "request"
	if target == "" {
		target = config.PlatformURL()
		source = platformHostSource()
	}
	target = strings.TrimRight(target, "/")

	resp := platformConnectivityResp{
		Address:         target,
		Source:          source,
		PlatformEnabled: config.PlatformEnabled(),
	}

	if !config.PlatformEnabled() {
		resp.Status = "not_configured"
		resp.Message = "平台集成未启用（PLATFORM_ENABLED 未开启）：平台侧功能全部不可用，" +
			"此时探测地址没有意义。请先开启平台集成并配置平台地址。"
		ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": resp.Message})
		return
	}

	if target == "" {
		resp.Status = "not_configured"
		resp.Message = "平台地址未配置：请在参数中心设置 platform.api_host，" +
			"或通过环境变量 PLATFORM_API_HOST/PLATFORM_API_URL 或 config/platform.yaml 的 api_url 指定。"
		ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": resp.Message})
		return
	}

	if err := validatePlatformHost(target); err != nil {
		resp.Status = "not_configured"
		resp.Message = "平台地址格式非法：" + err.Error()
		ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": resp.Message})
		return
	}

	_, latency, err := probePlatformHealth(target)
	resp.LatencyMs = latency
	if err != nil {
		resp.Status = "unreachable"
		resp.Message = "平台不可达：" + err.Error()
		logger.Warnf("[platform-connectivity] 探测失败 address=%s err=%v", target, err)
		ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": resp.Message})
		return
	}

	resp.Reachable = true
	resp.Status = "ok"
	resp.Message = fmt.Sprintf("平台可达（%d ms）", latency)
	ctx.JSON(http.StatusOK, gin.H{"code": 0, "data": resp, "message": resp.Message})
}

// @Summary PUT /api/manage/platform-host
// @Tags      platform-connectivity
// @Router    /api/manage/platform-host [put]
// SetPlatformHost PUT /api/manage/platform-host
//
// 请求体：{"address": "https://new-platform.example.com"}
//
// 写的是**平台默认行**（config_params 的 platform.api_host），不是租户覆盖行：
// 平台地址是部署级事实（这台部署连哪个平台），按租户分叉会让一个租户填错
// 地址就把全租户的资产市场/订阅/钱包一起拉空。守卫在 service 层
// （UpdatePlatformDefault 只放行 platformScopedParams 里的键）。
//
// 与参数中心通用页面（PUT /api/manage/config-params/platform/api_host）的关系：
// 通用页面在 SaaS 模式下会落进租户覆盖行（UpdateValue 见到租户 ctx 就走租户分支），
// 对这一条是**无效写入** —— 页面回「保存成功」而地址没变。这条专用端点绕开
// 那个分支，直接写平台默认行。两条路都通，但语义只有这条是对的。
//
// 为什么不做成"改一下通用页面的落点"：通用页面的落点由参数自身的租户可见性
// 决定（config_param_tenant_scope.go），改了它会影响所有平台级参数；
// 而"平台级参数的专用写入口"这个需求只有平台地址这一个。
func (c *PlatformConnectivityController) SetPlatformHost(ctx *gin.Context) {
	var req struct {
		Address string `json:"address"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "参数错误: " + err.Error()})
		return
	}
	address := strings.TrimRight(strings.TrimSpace(req.Address), "/")
	if address == "" {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "平台地址不能为空（要清空请用参数中心的「重置为默认值」）"})
		return
	}
	if err := validatePlatformHost(address); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": "平台地址格式非法: " + err.Error()})
		return
	}

	cpSvc := service.GlobalConfigParam()
	if cpSvc == nil {
		ctx.JSON(http.StatusOK, gin.H{"code": 500, "message": "参数中心未初始化"})
		return
	}
	actorID := c.currentUserID(ctx)
	if err := cpSvc.UpdatePlatformDefault(ctx.Request.Context(), "platform", "api_host", address, actorID); err != nil {
		logger.Warnf("[platform-host] UpdatePlatformDefault err=%v", err)
		ctx.JSON(http.StatusOK, gin.H{"code": 400, "message": err.Error()})
		return
	}
	logger.Infof("[platform-host] 平台地址已更新为 %s（改完约 60 秒内生效，无需重启）", address)
	ctx.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "平台地址已更新，约 60 秒内生效（无需重启）",
		"data":    platformConnectivityResp{Address: address, Source: "config_param", PlatformEnabled: config.PlatformEnabled()},
	})
}

// currentUserID 取当前登录用户 ID（审计用）。
func (c *PlatformConnectivityController) currentUserID(ctx *gin.Context) uint {
	if id, exists := ctx.Get("user_id"); exists {
		switch v := id.(type) {
		case uint:
			return v
		case int:
			return uint(v)
		case string:
			n, _ := strconv.Atoi(v)
			return uint(n)
		case float64:
			return uint(v)
		}
	}
	return 0
}

// platformHostSource 报告当前生效地址的来源，供运维判断「我改的值到底生效没」。
//
// 只做形状判定不做真值判定：参数中心优先 ⇒ 能读到非空就报 config_param，
// 否则看 yaml（PlatformCfg 已装配且有 APIURL），再否则看 env。
func platformHostSource() string {
	if v := strings.TrimSpace(config.PlatformURL()); v == "" {
		return "empty"
	}
	if config.PlatformCfg != nil && strings.TrimSpace(config.PlatformCfg.APIURL) != "" {
		return "config_param_or_yaml"
	}
	return "config_param_or_env"
}

// validatePlatformHost 校验平台地址形状：必须是 http/https 的绝对地址。
//
// 挡的是"保存了但根本不是地址"这类值：拼到出站 URL 上会变成一个打不通的
// 字符串，而错误只会在几百毫秒后的一次网络超时里以另一种面貌出现。
func validatePlatformHost(host string) error {
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("不是合法 URL：%w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("协议必须是 http 或 https（当前是 %q）", u.Scheme)
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("缺少主机名")
	}
	return nil
}

// probePlatformHealth 打 GET {host}/health 探活。
//
// 超时 5s：探针要快出结论，拖着只会让迁移窗口变长。返回耗时是为了让运维
// 看到"通了但很慢"这种情况 —— 平台迁移后地址可达不代表网络质量没问题。
func probePlatformHealth(host string) (bool, int64, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, host+"/health", nil)
	if err != nil {
		return false, time.Since(start).Milliseconds(), err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, time.Since(start).Milliseconds(), err
	}
	defer func() { _ = resp.Body.Close() }()
	latency := time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		return false, latency, fmt.Errorf("平台返回状态码 %d（期望 200）", resp.StatusCode)
	}
	return true, latency, nil
}
