package controller

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthController 认证控制器
type AuthController struct {
	authService *service.AuthService
	mfaService  *service.MFAService
	riskService *service.LoginRiskService
}

// NewAuthController 创建认证控制器实例
func NewAuthController() *AuthController {
	return &AuthController{
		authService: service.NewAuthService(),
		mfaService:  service.NewMFAService(),
		riskService: service.NewLoginRiskService(),
	}
}

// Login godoc
// @Summary      用户登录
// @Description  使用用户名+密码登录，支持 MFA 第二步验证；登录成功返回 JWT token
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  service.LoginRequest  true  "登录请求"
// @Success      200   {object}  response.Response  "登录成功，返回 token"
// @Failure      400   {object}  response.Response  "参数错误"
// @Failure      401   {object}  response.Response  "认证失败"
// @Router       /api/auth/login [post]
func (c *AuthController) Login(ctx *gin.Context) {
	var req service.LoginRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	resp, err := c.authService.Login(ctx.Request.Context(), &req)
	if err != nil {
		middleware.RecordBruteForceFailure(ctx, "auth.login")

		c.recordLoginRiskAsync(ctx, req.Username, false, err.Error())
		response.Error(ctx, http.StatusUnauthorized, err.Error())
		return
	}

	middleware.ClearBruteForceFailure(ctx, "auth.login")

	// 登录是公开路由（不挂 TenantMiddleware），租户只能由命中的用户行携带。
	// service 层的 bindUserTenant 只改了局部 ctx，不回流到 request ctx ⇒
	// recordLoginRiskAsync 取不到租户 ⇒ 登录风控整链（login_events 写入 /
	// 最近失败次数 / 异常登录告警）在 on 模式下被隔离闸 fail-closed 静默拒绝。
	// 这里把租户回填进 request ctx，风控链才拿得到归属。
	if resp != nil && resp.TenantID != 0 && tenant.IsMultiTenant() {
		ctx.Request = ctx.Request.WithContext(tenant.WithTenantID(ctx.Request.Context(), resp.TenantID))
		ctx.Set(middleware.TenantContextKey, resp.TenantID)
	}

	c.recordLoginRiskAsync(ctx, req.Username, true, "")
	response.Success(ctx, resp, "登录成功")
}

func (c *AuthController) recordLoginRiskAsync(ctx *gin.Context, username string, success bool, reason string) {
	lctx := &service.LoginRiskContext{
		Username:  username,
		IP:        ctx.ClientIP(),
		UserAgent: ctx.Request.UserAgent(),
		Success:   success,
		LoginAt:   time.Now(),
		Reason:    reason,
	}
	// 协程里只剩 Background ctx，on 模式下 login_events/security_alerts 的
	// 隔离闸按 tenant_id fail-closed 会拒绝全部读写（登录风控整链静默失效）。
	// 必须在请求还活着时先把租户绑定摘出来，进协程再挂回去。
	tid, hasTenant := tenant.FromContext(ctx.Request.Context())
	// 测试收尾/装配早期 dbstate 可能未就绪——无库窗口里的风控记账没有意义，
	// 与其在协程里 recover 一条 nil-DB panic 栈，不如派发前显式跳过并留痕。
	if dbstate.Get() == nil {
		logger.GetLogger().Warn().Str("username", username).Msg("[login-risk] DB 未就绪，跳过本次异步风控记账")
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 走统一日志器而不是 stdlib log：stdlib 直接写 stderr，
				// 任何被打进消息行的可控文本都能凭空劈出一条假日志行。
				logger.GetLogger().Error().Any("panic", r).Str("stack", string(debug.Stack())).Msg("[login-risk] panic recovered")
			}
		}()
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if hasTenant {
			dctx = tenant.OnlyTenant(dctx, tid)
		} else {
			// 失败登录（口令错/用户名不存在）没有租户可绑——而这正是防爆破
			// 计数最需要记账的那一半。登录遥测是**部署级安全数据**（攻击者
			// 的登录不归属任何租户，与 C-5 的 401 遥测走 platform_config_kv
			// 同一裁决），落系统作用域；不修则 on 模式下"最近 1 小时失败 N 次"
			// 恒读 0、审计表里只有成功行（RELEASE_PREPARATION-实跑结果 §5.1
			// 实测的正是这条：1 次口令探针整链被闸拒）。
			dctx = tenant.WithSystemScope(dctx)
		}
		detector := service.NewAnomalyLoginDetector()
		if _, err := detector.DetectAndAlert(dctx, lctx); err != nil {
			logger.GetLogger().Warn().Err(err).
				Str("username", username).
				Bool("success", success).
				Msg("[login-risk] 登录风险检测失败")
		}
	}()
}

// Register godoc
// @Summary      用户注册
// @Description  自主注册，支持邮箱验证（可选），注册成功返回 JWT token
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        body  body  service.RegisterRequest  true  "注册请求"
// @Success      200   {object}  response.Response  "注册成功，返回 token"
// @Failure      400   {object}  response.Response  "参数错误"
// @Failure      409   {object}  response.Response  "用户名/邮箱已存在"
// @Router       /api/auth/register [post]
func (c *AuthController) Register(ctx *gin.Context) {
	var req service.RegisterRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	// 注册开关是租户配置：先解析租户再读，on 模式缺短码直接拒绝（4106 口径）
	registerCtx, err := resolvePublicTenantCtx(ctx, req.TenantCode)
	if err != nil {
		tenantFail(ctx, err)
		return
	}
	// enable_register 开关：关闭时 403（配置读失败按保守口径同样拒绝）
	cfg, cfgErr := service.NewSystemConfigService().GetConfig(registerCtx)
	if cfgErr != nil || !cfg.EnableRegister {
		response.Error(ctx, http.StatusForbidden, "当前站点未开放注册")
		return
	}

	resp, err := c.authService.Register(registerCtx, &req)
	if err != nil {
		if errors.Is(err, service.ErrUsernameExists) || errors.Is(err, service.ErrEmailExists) {
			response.Error(ctx, http.StatusConflict, err.Error())
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, resp, "注册成功")
}

// RefreshToken godoc
// @Summary      刷新 JWT 令牌
// @Description  使用旧 Bearer token 换发新 token，旧 token 会被加入黑名单
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "刷新成功"
// @Failure      401  {object}  response.Response  "认证失败"
// @Router       /api/auth/refresh-token [post]
func (c *AuthController) RefreshToken(ctx *gin.Context) {
	authHeader := ctx.GetHeader("Authorization")
	if authHeader == "" {
		response.Error(ctx, http.StatusUnauthorized, "未提供认证令牌")
		return
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		response.Error(ctx, http.StatusUnauthorized, "Authorization 头格式错误，应为 Bearer <token>")
		return
	}
	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" {
		response.Error(ctx, http.StatusUnauthorized, "认证令牌不能为空")
		return
	}

	newToken, err := c.authService.RefreshToken(ctx.Request.Context(), token)
	if err != nil {
		response.Error(ctx, http.StatusUnauthorized, "刷新令牌失败", err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"token": newToken,
	}, "刷新令牌成功")
}

// Logout godoc
// @Summary      登出
// @Description  将当前 Bearer 令牌加入黑名单（立即失效）
// @Tags         Auth
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "登出成功"
// @Router       /api/auth/logout [post]
func (c *AuthController) Logout(ctx *gin.Context) {
	authHeader := ctx.GetHeader("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(authHeader, prefix) {
		token := strings.TrimSpace(authHeader[len(prefix):])
		if token != "" {
			_ = c.authService.Logout(ctx.Request.Context(), token)
		}
	}
	response.Success(ctx, nil, "登出成功")
}

// GetCurrentUser godoc
// @Summary      获取当前登录用户信息
// @Description  从 JWT token 解析 user_id 并返回用户基本信息
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Response  "成功"
// @Failure      401  {object}  response.Response  "未登录"
// @Router       /api/auth/current-user [get]
func (c *AuthController) GetCurrentUser(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	user, err := c.authService.GetCurrentUser(ctx.Request.Context(), uid)
	if err != nil {
		if HandleServiceError(ctx, err) {
			return
		}
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, user, "获取用户信息成功")
}

// ChangePassword godoc
// @Summary      修改当前用户密码
// @Description  修改成功后旧 token 会被加入黑名单
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  service.ChangePasswordRequest  true  "修改密码请求"
// @Success      200   {object}  response.Response  "成功"
// @Failure      400   {object}  response.Response  "参数错误"
// @Router       /api/auth/change-password [post]
func (c *AuthController) ChangePassword(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}

	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	var req service.ChangePasswordRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if err := c.authService.ChangePassword(ctx.Request.Context(), uid, &req); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	if authHeader := ctx.GetHeader("Authorization"); authHeader != "" {
		if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
			utils.BlacklistJWT(parts[1])
		}
	}

	response.Success(ctx, nil, "修改密码成功")
}

// @Summary      请使用 Google Authenticator 扫描二维码
// @Tags      auth
// @Router    /api/auth/mfa/setup [post]
// SetupMFA 设置 MFA：生成 TOTP 密钥并返回 otpauth URL
// 用户使用 Google Authenticator 扫描二维码
func (c *AuthController) SetupMFA(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	username, _ := ctx.Get("username")
	usernameStr, _ := username.(string)

	resp, err := c.mfaService.SetupMFA(ctx.Request.Context(), uid, usernameStr)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, resp, "请使用 Google Authenticator 扫描二维码")
}

// @Summary      MFA 启用成功，下次登录需输入验证码
// @Tags      auth
// @Router    /api/auth/mfa/confirm [post]
// ConfirmMFASetup 确认 MFA 设置：用户输入 6 位码验证，验证成功后启用 MFA
func (c *AuthController) ConfirmMFASetup(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	var req service.MFASetupVerifyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if err := c.mfaService.ConfirmMFASetup(ctx.Request.Context(), uid, req.Code); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"mfa_enabled": true,
		"message":     "MFA 启用成功，下次登录需输入验证码",
	}, "MFA 启用成功")
}

// @Summary      MFA 已禁用
// @Tags      auth
// @Router    /api/auth/mfa/disable [post]
// DisableMFA 禁用 MFA
// 需要校验密码 + TOTP 码（双重保护）
func (c *AuthController) DisableMFA(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	var req service.MFADisableRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if err := c.mfaService.DisableMFA(ctx.Request.Context(), uid, req.Password, req.Code); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"mfa_enabled": false,
		"message":     "MFA 已禁用",
	}, "MFA 已禁用")
}

// @Summary MFA 登录验证（登录第二步）
// @Tags      auth
// @Router    /api/auth/mfa/verify [post]
// VerifyMFALogin MFA 登录验证（登录第二步）
// POST /api/auth/mfa/verify
// Body: { "temp_token": "...", "code": "123456" }
// 成功返回 JWT token
func (c *AuthController) VerifyMFALogin(ctx *gin.Context) {
	var req service.MFAVerifyRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	userID, username, role, err := c.mfaService.VerifyMFALogin(ctx.Request.Context(), req.TempToken, req.Code)
	if err != nil {
		// 路由已挂 BruteForceGuard("auth.mfa")：失败必须在此单点计数，
		// 否则守卫永不触发（TOTP 6 位码 ±90s 窗口可被持续爆破）。
		middleware.RecordBruteForceFailure(ctx, "auth.mfa")
		response.Error(ctx, http.StatusUnauthorized, err.Error())
		return
	}
	middleware.ClearBruteForceFailure(ctx, "auth.mfa")

	// 终令牌按用户行签发，携带租户归属（T031：MFA 临时令牌不含租户）；
	// 租户字段与普通登录响应同口径补齐（此前 MFA 响应缺 tenant_id/tenant_code，
	// 前端落库的用户信息在 SaaS 模式下丢租户归属）。
	token, tenantID, tenantCode, err := c.authService.IssueTokenWithTenant(ctx.Request.Context(), userID)
	if err != nil {
		if HandleServiceError(ctx, err) {
			return
		}
		response.Error(ctx, http.StatusInternalServerError, "颁发令牌失败")
		return
	}

	middleware.MarkMFAVerified(userID)

	resp := gin.H{
		"token":   token,
		"expires": 86400,
		"user": gin.H{
			"id":       userID,
			"username": username,
			"role":     role,
		},
	}
	// off 模式 tenants 可为空，租户字段只在 on 模式下发（与 loginWithUser 一致）
	if tenant.IsMultiTenant() {
		resp["tenant_id"] = tenantID
		resp["tenant_code"] = tenantCode
	}

	response.Success(ctx, resp, "MFA 验证成功，登录完成")
}

// @Summary      查询当前用户的 MFA 启用状态
// @Tags      auth
// @Router    /api/auth/mfa/status [get]
// GetMFAStatus 查询当前用户的 MFA 启用状态
func (c *AuthController) GetMFAStatus(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	enabled, err := c.mfaService.IsMFAEnabled(ctx.Request.Context(), uid)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"mfa_enabled": enabled,
	}, "查询成功")
}

// @Summary      恢复码已生成，旧码已失效。请立即保存，此页面关闭后将无法再次查看明文码。
// @Tags      auth
// @Router    /api/auth/mfa/backup-codes [post]
// GenerateBackupCodes 生成新的 MFA 恢复码（使旧码全部失效）
// POST /api/auth/mfa/backup-codes
// 返回 10 个明文恢复码（仅展示一次，后续只存 bcrypt 哈希）
// 安全：需要用户已登录（JWT 鉴权）；生成后旧码立即失效
func (c *AuthController) GenerateBackupCodes(ctx *gin.Context) {
	userID, exists := ctx.Get("user_id")
	if !exists {
		response.Error(ctx, http.StatusUnauthorized, "未找到用户信息")
		return
	}
	uid, ok := userID.(uint)
	if !ok {
		response.Error(ctx, http.StatusInternalServerError, "用户ID类型错误")
		return
	}

	codes, err := c.mfaService.GenerateBackupCodes(ctx.Request.Context(), uid)
	if err != nil {
		response.Error(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"backup_codes": codes,
		"message":      "恢复码已生成，旧码已失效。请立即保存，此页面关闭后将无法再次查看明文码。",
	}, "生成成功")
}

// @Summary      查询登录事件记录
// @Tags      auth
// @Router    /api/auth/login-events [get]
// ListLoginEvents 查询登录事件列表
// GET /api/auth/login-events?page=1&page_size=20
func (c *AuthController) ListLoginEvents(ctx *gin.Context) {
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	events, total, err := c.riskService.ListLoginEvents(ctx.Request.Context(), uid, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      events,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "查询成功")
}

// @Summary      查询安全告警列表
// @Tags      auth
// @Router    /api/auth/security-alerts [get]
// ListSecurityAlerts 查询安全告警列表
// GET /api/auth/security-alerts?status=open&page=1&page_size=20
func (c *AuthController) ListSecurityAlerts(ctx *gin.Context) {
	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	status := ctx.Query("status")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	alerts, total, err := c.riskService.ListSecurityAlerts(ctx.Request.Context(), uid, status, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      alerts,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "查询成功")
}

// @Summary      告警已处理
// @Tags      auth
// @Router    /api/auth/security-alerts/{id}/resolve [post]
// ResolveSecurityAlert 处理安全告警
// POST /api/auth/security-alerts/:id/resolve
func (c *AuthController) ResolveSecurityAlert(ctx *gin.Context) {
	alertIDStr := ctx.Param("id")
	alertID, err := strconv.ParseUint(alertIDStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的告警 ID")
		return
	}

	userID, _ := ctx.Get("user_id")
	uid, _ := userID.(uint)

	var req struct {
		Note string `json:"note"`
	}
	_ = ctx.ShouldBindJSON(&req)

	if err := c.riskService.ResolveSecurityAlert(ctx.Request.Context(), uint(alertID), uid, req.Note); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "告警已处理")
}

// @Summary      获取密码策略配置
// @Tags      auth
// @Router    /api/auth/password-policy [get]
// GetPasswordPolicy 查询当前密码策略
// GET /api/auth/password-policy
func (c *AuthController) GetPasswordPolicy(ctx *gin.Context) {
	policySvc := service.NewPasswordPolicyService()
	policy := policySvc.GetPolicy(ctx.Request.Context())
	response.Success(ctx, policy, "查询成功")
}

// @Summary      密码策略已更新
// @Tags      auth
// @Router    /api/auth/password-policy [put]
// SavePasswordPolicy 更新密码策略（仅 admin）
// PUT /api/auth/password-policy
func (c *AuthController) SavePasswordPolicy(ctx *gin.Context) {
	role, _ := ctx.Get("role")
	roleStr, _ := role.(string)
	if roleStr != "admin" {
		response.Error(ctx, http.StatusForbidden, "仅管理员可修改密码策略")
		return
	}

	var policy service.PasswordPolicy
	if err := ctx.ShouldBindJSON(&policy); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	policySvc := service.NewPasswordPolicyService()
	if err := policySvc.SavePolicy(ctx.Request.Context(), &policy); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, policy, "密码策略已更新")
}

// SystemUserController 系统用户控制器
type SystemUserController struct {
	userService *service.SystemUserService
}

// NewSystemUserController 创建系统用户控制器实例
func NewSystemUserController() *SystemUserController {
	return &SystemUserController{
		userService: service.NewSystemUserService(),
	}
}

// @Summary 获取用户列表
// @Tags      system-user
// @Router    /api/users [get]
// GetUsers 获取用户列表
func (c *SystemUserController) GetUsers(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	// keyword 不是可选装饰：前端 UserList.vue 的搜索框就是往本端点丢 keyword，
	// 这里不认就等于搜索框点了没反应（2026-10-31 API 回归实测 total 恒为全量）。
	// 复用 SearchUsers 的服务实现，保持分页形状一致，不再复制一份查询条件。
	keyword := strings.TrimSpace(ctx.Query("keyword"))
	var (
		users []*service.SystemUserResponse
		total int64
		err   error
	)
	if keyword != "" {
		users, total, err = c.userService.SearchUsers(ctx.Request.Context(), keyword, page, pageSize)
	} else {
		users, total, err = c.userService.GetUsers(ctx.Request.Context(), page, pageSize)
	}
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"list":      users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "获取用户列表成功")
}

// @Summary GET /api/users/search?keyword=xxx
// @Tags      system-user
// @Router    /api/users/search [get]
// SearchUsers GET /api/users/search?keyword=xxx
func (c *SystemUserController) SearchUsers(ctx *gin.Context) {
	keyword := ctx.Query("keyword")
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	users, total, err := c.userService.SearchUsers(ctx.Request.Context(), keyword, page, pageSize)
	if err != nil {
		response.ErrorFromDB(ctx, err, err.Error())
		return
	}
	response.Success(ctx, gin.H{
		"list":      users,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "搜索成功")
}

// @Summary 获取用户详情
// @Tags      system-user
// @Router    /api/users/{id} [get]
// GetUser 获取用户详情
func (c *SystemUserController) GetUser(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的用户ID")
		return
	}

	user, err := c.userService.GetUserByID(ctx.Request.Context(), uint(id))
	if err != nil {
		response.Error(ctx, http.StatusNotFound, err.Error())
		return
	}

	response.Success(ctx, user, "获取用户信息成功")
}

// @Summary 创建用户
// @Tags      system-user
// @Router    /api/users [post]
// CreateUser 创建用户
func (c *SystemUserController) CreateUser(ctx *gin.Context) {
	var req service.CreateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	user, err := c.userService.CreateUser(ctx.Request.Context(), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, user, "创建用户成功")
}

// @Summary 更新用户
// @Tags      system-user
// @Router    /api/users/{id} [put]
// UpdateUser 更新用户
func (c *SystemUserController) UpdateUser(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的用户ID")
		return
	}

	var req service.UpdateUserRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	user, err := c.userService.UpdateUser(ctx.Request.Context(), uint(id), &req)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, user, "更新用户成功")
}

// @Summary 删除用户
// @Tags      system-user
// @Router    /api/users/{id} [delete]
// DeleteUser 删除用户
//
// 安全（Round32 复测发现）：旧别名端点 /api/user/:id、/api/users/:id 此前走
// 无保护的 UserService.DeleteUser，可绕过"不能删除自己的账号"与"至少保留
// 一个超管"双重防护（实测 DELETE /api/user/1 可将超管自身删除）。
// 统一收敛到 DeleteByAdmin（含 actor 自删校验 + ErrLastAdmin 校验）。
func (c *SystemUserController) DeleteUser(ctx *gin.Context) {
	actorID, ok := extractActorID(ctx)
	if !ok {
		return
	}

	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的用户ID")
		return
	}

	if err := c.userService.DeleteByAdmin(ctx.Request.Context(), actorID, uint(id)); err != nil {
		writeServiceError(ctx, err)
		return
	}

	response.Success(ctx, nil, "删除用户成功")
}

// @Summary 重置用户密码
// @Tags      system-user
// @Router    /api/users/{id}/password [put]
// ResetPassword 重置用户密码
func (c *SystemUserController) ResetPassword(ctx *gin.Context) {
	idStr := ctx.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(ctx, http.StatusBadRequest, "无效的用户ID")
		return
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	if err := c.userService.ResetPassword(ctx.Request.Context(), uint(id), req.Password); err != nil {
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, nil, "重置密码成功")
}

// @Summary      超管创建成功，请使用此账号登录
// @Tags      auth
// @Router    /api/system/init-admin [post]
// InitAdmin 公开：初始化系统超管
// 路由：POST /api/system/init-admin（详见 admin_routes.go setupPublicRoutes）
// 流程：SetupOnlyGuard（已初始化直接 403）→ 本方法的安装态快检 → BindJSON → service.AuthService.InitAdmin → install.lock
//
// 这里开头的 IsSystemInitialized() 只是把"已初始化"挡在参数校验之前（省一次 bind、
// 且不给探测性请求回显字段规则）；真正的判定在 service 层，见 assertFirstAdminPossible。
//
// InitAdminRequest 复用 controller/system_init.go 已有的同 struct（避免重复定义）：
//   - Username: 3-20 位
//   - Password: ≥8 位
//   - Email:    可选，格式合法
//   - RealName / ContactPhone: 可选（开源版 init 上报用，本流程不消费）
func (c *AuthController) InitAdmin(ctx *gin.Context) {
	if IsSystemInitialized() {
		response.Error(ctx, http.StatusForbidden, response.ErrSystemAlreadyInitialized)
		return
	}

	var req InitAdminRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	// 监管登记必填校验（与自助注册、自助开户同一口径；003 T012：地址必填、邮箱选填）
	if err := c.authService.ValidateOfflineRegistration(req.CompanyName, req.RealName,
		req.ContactPhone, req.Email, req.Address, req.RegulatoryAck); err != nil {
		response.ErrorWithBusinessCode(ctx, bizerr.CodeModuleRegistrationIncomplete,
			err.Error(), gin.H{})
		return
	}

	if err := c.authService.InitAdminWithRegistration(ctx.Request.Context(),
		req.Username, req.Password, req.Email, req.RealName, req.ContactPhone,
		req.CompanyName, req.LicenseNo, req.Address,
		req.RegulatoryAck, req.AgreementVersion,
		// 003 T010 静默合规采集：服务端视角取 ClientIP/UA，前端无感知
		ctx.ClientIP(), ctx.Request.UserAgent()); err != nil {
		if errors.Is(err, service.ErrAdminAlreadyInitialized) {
			response.Error(ctx, http.StatusForbidden, response.ErrSystemAlreadyInitialized)
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	// reg_mode 按 TENANT_MODE 实际返回（003 T012）：硬编码 offline 会让
	// 云端 SaaS 实例（TENANT_MODE=on）的初始化在响应里自称"离线部署"。
	regMode := model.TenantRegModeOffline
	if tenant.IsMultiTenant() {
		regMode = model.TenantRegModeSaaS
	}
	response.Success(ctx, gin.H{
		"username":      req.Username,
		"real_name":     req.RealName,
		"company_name":  req.CompanyName,
		"reg_mode":      regMode,
		"platform_sync": model.TenantSyncSkipped,
		"message":       "超管创建成功，请使用此账号登录",
	}, "超管初始化完成")
}

// CreateDefaultAdmin 创建默认超管（兼容旧前端的入口，无认证）
//
// 系统用户统一 plan v3.1 §3.2：
//   - 函数名保留（避免破坏旧路由 / 测试 / 文档引用）
//   - 函数体重写：不再读取 config.GetAdminConfig().DefaultAdmin 的硬编码密码，
//     从请求体 InitAdminRequest 读取 username/password/email
//   - 委派给 service.AuthService.InitAdmin（统一的强密码 / 唯一性 / install.lock 流程）
//
// 装配现状（本注释只描述磁盘上的事实，别照着旧版本改回去）：
//   - 生产路由 POST /api/system/create-default-admin 挂的是 AuthController.CreateDefaultAdmin
//     （见 admin_routes.go），它会转打本文件上方的 InitAdmin；本方法当前没有生产挂载点，
//     只被 internal/controller/auth_test.go 直接调用。
//   - "已初始化就禁止再建超管"的判定在 service.AuthService.InitAdmin 里做，
//     所以哪怕哪天把本方法重新挂到某个公开前缀，也只会撞上 403，不会静默建号。
func (c *SystemUserController) CreateDefaultAdmin(ctx *gin.Context) {
	var req InitAdminRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Error(ctx, http.StatusBadRequest, response.ErrInvalidParams, err.Error())
		return
	}

	authSvc := service.NewAuthService()
	if err := authSvc.InitAdmin(ctx.Request.Context(), req.Username, req.Password, req.Email); err != nil {
		if errors.Is(err, service.ErrAdminAlreadyInitialized) {
			response.Error(ctx, http.StatusForbidden, response.ErrSystemAlreadyInitialized)
			return
		}
		response.Error(ctx, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(ctx, gin.H{
		"username": req.Username,
		"message":  "默认管理员创建成功（兼容旧路由）",
	}, "默认管理员创建成功")
}

// @Summary      创建默认管理员（兼容旧路由）
// @Tags      auth
// @Router    /api/system/create-default-admin [post]
func (c *AuthController) CreateDefaultAdmin(ctx *gin.Context) {
	c.InitAdmin(ctx)
}
