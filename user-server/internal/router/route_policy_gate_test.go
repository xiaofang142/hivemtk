package router

// 常驻门：路由守卫链快照。钉住两件此前只能靠人肉核对的事：
//
//  一、每条 live 路由要么链上有身份守卫，要么在下面这份匿名清单里逐条写明"为什么
//     可以匿名"。清单是双向的：不在清单里的口一旦丢了守卫就红，清单里的口一旦被
//     接上了守卫也红（后者说明有人在收窄面，那就该同时删掉这行，让清单继续等于事实）。
//     立项依据是注册顺序缺陷：gin 在注册时快照当时的 handler 链，`auth.Use(JWT)` 之前
//     注册的路由不带 JWT —— 7 条 /api/monitor/* 因此匿名可读数长达一年。
//     monitor_auth_test.go 钉的是那 7 条口；这道门钉的是"任何一条口"，包括以后新增的。
//
//  二、middleware.RoutePolicyGuard 那张"仅超管"表落地与否：每一行必须命中至少一条
//     live 路由（写错路径的死行会静默不生效），且被命中的那条路由链上必须真的出现
//     超管守卫（表命中却注册在没挂守卫的组上＝形同虚设）。
//
// 判据全部来自运行期事实：生产装配入口 Setup() 建出的路由表，加上 gin 的
// c.HandlerNames()。记录型守卫挂在链上第一位、抓到链立刻 c.Abort()，所以业务 handler
// 一次都不会执行 —— 含 POST/PUT/DELETE 在内的全量方法都能安全枚举，不需要担心
// "探测把测试库写脏"或"某个 handler 去打外部渠道"。

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 守卫在链上的函数名片段。刻意不写包前缀，也不写 .funcN 后缀：
// 构造器被内联时，闭包符号会改挂到内联方（实测 middleware.JWTAuthMiddleware 的闭包
// 在 Setup 里编译成 router.Setup.JWTAuthMiddleware.func27），前缀与编号都随构建方式变，
// 只有构造器名本身是稳定的。
const (
	guardJWT         = "JWTAuthMiddleware"
	guardBridge      = "BridgeIngressGuard"
	guardMCP         = "MCPGuard"
	guardIngress     = "IngressSecretAuth"
	guardRequireAdm  = "RequireAdminMiddleware"
	guardAdminAuth   = "AdminAuthMiddleware"
	guardPolicy      = "RoutePolicyGuard"
	guardSetupOnly   = "SetupOnlyGuard"
	jwtNoTokenMsg    = "未提供认证令牌"
	policyDeniedMsg  = "权限不足，需要超管权限"
	adminAuthDenyMsg = "权限不足，需要管理员权限"
	// 能力面（菜单授予）拒绝文案，与 middleware.capabilityDeniedMsg 同字面量：
	// 守卫的拒绝口径是对外契约，改文案要同时改这里。
	capabilityDeniedMsg = "权限不足，该功能需先被授予对应菜单"
)

// identityGuards 认"这条链知道请求者是谁"的守卫：坐席登录态、渠道机对接令牌、
// MCP 工具令牌、坐席侧投递密钥都算。InitGuard/SetupOnlyGuard 不是身份守卫（它们只读
// 安装态），所以安装引导口仍要进匿名清单并写明它靠哪一道门收口。
var identityGuards = []string{guardJWT, guardBridge, guardMCP, guardIngress}

// superAdminGuards 认"这条链会拒绝非超管"的守卫。
var superAdminGuards = []string{guardRequireAdm, guardAdminAuth, guardPolicy}

func chainHasGuard(chain []string, guard string) bool {
	for _, name := range chain {
		if strings.Contains(name, guard) {
			return true
		}
	}
	return false
}

func chainHasAny(chain []string, guards []string) bool {
	for _, g := range guards {
		if chainHasGuard(chain, g) {
			return true
		}
	}
	return false
}

// anonymousAllowedRoutes 匿名可达面（"METHOD /path" → 为什么允许）。
// 值不是装饰：判据是"清单 == 事实"的双向对账，删一行但路由还在＝红，加一行而它其实
// 有守卫＝红。新增一条公开口时必须同时在这里写清它靠什么自证身份。
var anonymousAllowedRoutes = map[string]string{
	"GET /":                                                      "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /api/browser/host-ws":                                   "NM Host 通道：只认回环对端 IP + 主机令牌握手（两道门都在 handler 内，见 HostWSHandler.Handle）",
	"GET /api/egolite/runner-ws":                                 "ego-runner 通道：eg_ 前缀令牌（KV 双候选轮换+常量时间比较）+ 形态开关回环门 + register 握手，全部在 handler 内（EgoRunnerWSHandler.Handle）；token 即身份，与 host-ws 同形状",
	"GET /api/chat/public/agents/available":                      "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/channel/:app_key/info":                 "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/sessions/:session_id/messages":         "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/sessions/:session_id/offline-messages": "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/sessions/active":                       "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/sessions/recent-closed":                "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/chat/public/upload-token":                          "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"GET /api/dashboards/public/:code":                           "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"GET /api/email/track/click/:token":                          "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/email/track/open/:token":                           "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/email/unsubscribe":                                 "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/health":                                            "存活/就绪探针：只回进程状态",
	"GET /api/knowledge":                                         "已下线老域名墓碑路由（D-10）：恒 410 + 迁移指引，不读库、不读租户数据、不写状态，属终点态",
	"GET /api/knowledge/search":                                  "已下线老域名墓碑路由（D-10）：恒 410 + 迁移指引，不读库、不读租户数据、不写状态，属终点态",
	"GET /api/l/:code":                                           "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"GET /api/livecode/:id":                                      "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"POST /api/tenant/register":                                  "SaaS 自助开户（specs/001-saas-multi-tenant）：登录前唯一可写端点，限流 + 参数校验 + off 模式 4104",
	"GET /api/tenant/regulatory-notice":                          "开户监管告知：注册页登录前展示，纯静态文案（平台优先/离线回落）",
	"GET /api/module-market/list":                                "模块市场公开橱窗：未登录可浏览商品清单，无个人数据；下单/查单在鉴权档",
	"GET /api/module-market/payment-guide":                       "付款指引公开页：模块市场购买前置说明，纯静态文案",
	"GET /api/template-market/list":                              "模板/技能市场公开橱窗（F2）：只回目录元数据（名称/分类/简介/来源/可用性），无个人与租户数据",
	"GET /api/template-market/categories":                        "模板/技能市场分类清单：与 list 同源的公开元数据，无个人与租户数据",
	"GET /api/template-market/detail/:id":                        "模板/技能市场单条元数据：只回目录条目自身，无个人与租户数据",
	"GET /api/public/config":                                     "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"GET /api/public/help-center/articles":                       "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"GET /api/public/help-center/articles/:id":                   "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"GET /api/public/help-center/categories":                     "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"GET /api/public/help-center/search":                         "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"GET /api/s/:code":                                           "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"GET /api/sso/callback/:provider":                            "第三方 SSO 跳转与回调：身份由 provider 侧令牌带回",
	"GET /api/sso/login/:provider":                               "第三方 SSO 跳转与回调：身份由 provider 侧令牌带回",
	"GET /api/sso/providers":                                     "第三方 SSO 跳转与回调：身份由 provider 侧令牌带回",
	// 这一条是"字段裁剪型"公开口：链上只有 OptionalAuthMiddleware（认身份、不拦人），
	// 匿名必须拿到 200，泄露面靠 handler 按 role 裁字段收口，判据在
	// system_info_platform_test.go（匿名无机器画像 + 超管有）。
	"GET /api/system/info":                                "公开字段裁剪型：匿名只回 platform_enabled，机器画像字段要 admin",
	"GET /api/system/init-status":                         "安装向导状态查询：响应体经 publicInitStatus 剥掉 install_id（见 controller 侧用例）",
	"GET /api/webhook/dingtalk/:account_id":               "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/webhook/feishu/:account_id":                 "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/webhook/health":                             "存活/就绪探针：只回进程状态",
	"GET /api/webhook/wechat/:account_id":                 "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/webhook/wecom/:account_id":                  "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/webhook/whatsapp/:account_id":               "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"GET /api/ws/visitor":                                 "访客 WebSocket：握手内校验 visitor_token",
	"GET /assets/*filepath":                               "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /chat/embed":                                     "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /chat/embed/*path":                               "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /favicon.ico":                                    "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /files/*filepath":                                "本地上传目录的公开外链：文件名是随机 uuid、目录不可列举，serveUploadsGuarded 另挡同源可执行扩展名并加 nosniff/sandbox",
	"GET /health":                                         "存活/就绪探针：只回进程状态",
	"GET /healthz":                                        "存活/就绪探针：只回进程状态",
	"GET /index.html":                                     "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /logo.png":                                       "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /readyz":                                         "存活/就绪探针：只回进程状态",
	"GET /share/douyin/:id":                               "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /share/kuaishou/:id":                             "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /share/xianyu/:id":                               "前端静态产物与分享页：只有模板与图片，无商户数据",
	"GET /share/xiaohongshu/:id":                          "前端静态产物与分享页：只有模板与图片，无商户数据",
	"HEAD /assets/*filepath":                              "前端静态产物与分享页：只有模板与图片，无商户数据",
	"HEAD /favicon.ico":                                   "前端静态产物与分享页：只有模板与图片，无商户数据",
	"HEAD /logo.png":                                      "前端静态产物与分享页：只有模板与图片，无商户数据",
	"POST /api/auth/login":                                "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/auth/mfa/verify":                           "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/auth/register":                             "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/chat/public/sessions":                      "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/chat/public/sessions/:session_id/close":    "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/chat/public/sessions/:session_id/messages": "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/chat/public/sessions/:session_id/rate":     "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/chat/public/sessions/:session_id/token":    "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/chat/public/sessions/:session_id/transfer": "官网访客对话组件：AppKeyResolve + visitor_token（HMAC 绑定 channel/visitor/session）+ 访客限流",
	"POST /api/email/track/webhook/postmark":              "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/email/track/webhook/sendcloud":             "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/email/unsubscribe/confirm":                 "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/integration/webhook/order/:platform":       "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/knowledge-merchant/external/import":        "外部系统导入：X-Knowledge-Token 校验 + write 作用域 + 产品作用域",
	"POST /api/livecode/:id/click":                        "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"POST /api/public/forgot-password":                    "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/public/register":                           "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/public/reset-password":                     "登录前必须可达（登录、注册、口令找回、站点公开配置、帮助中心）",
	"POST /api/short-link/access":                         "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"POST /api/shortlink/access":                          "访客侧短链/直播码/看板分享：凭 code 可达，回的是分享者主动公开的内容",
	"POST /api/sms/delivery/webhook":                      "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/sms/webhook/inbound":                       "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/system/create-default-admin":               "首次安装写入口：SetupOnlyGuard（已初始化 → 403），service.InitAdmin 再核库里超管数",
	"POST /api/system/init-admin":                         "首次安装写入口：SetupOnlyGuard（已初始化 → 403），service.InitAdmin 再核库里超管数",
	"POST /api/system/init-complete":                      "首次安装写入口：SetupOnlyGuard（已初始化 → 403），service.InitAdmin 再核库里超管数",
	"POST /api/webhook/:channel":                          "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/webhook/:channel/:account_id":              "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/webhook/dingtalk/:account_id":              "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/webhook/wechat/:account_id":                "渠道/邮件/短信回调：签名或一次性 token 自证身份（订单回调另有 VerifyWebhookKey 复核）",
	"POST /api/xianyu/:id/click":                          "闲鱼卡片落地页埋点：页面无登录态，凭渲染时按卡片 id + 过期时间签发的 HMAC 凭证自证（handler 内校验），全局限流覆盖",
	"POST /api/xianyu/:id/share":                          "闲鱼卡片落地页埋点：页面无登录态，凭渲染时按卡片 id + 过期时间签发的 HMAC 凭证自证（handler 内校验），全局限流覆盖",
	"POST /api/xianyu/:id/view":                           "闲鱼卡片落地页埋点：页面无登录态，凭渲染时按卡片 id + 过期时间签发的 HMAC 凭证自证（handler 内校验），全局限流覆盖",
}

// liveRouteGuardChains 装配生产路由表并抓每条路由注册时的完整 handler 链。
// 前提与 liveRoutes 对齐（USER_WEB_DIST 指向空临时目录），否则前端静态面按目录
// 是否存在条件注册，清单会在"有没有跑过 npm build"的机器上给出不同读数。
func liveRouteGuardChains(t *testing.T) map[string][]string {
	t.Helper()
	t.Setenv("USER_WEB_DIST", t.TempDir())
	// 调试路由的注册条件是"本地/开发环境"，测试里 APP_ENV 通常为空即命中，
	// 但生产态环境变量会让它不注册；显式打开，让清单与判据都不随机器环境变量摆动。
	t.Setenv("ENABLE_DEBUG_ROUTES", "true")

	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{}, &model.SystemUser{})
	prev := dbutil.GetDB()
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(prev) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	chains := map[string][]string{}
	engine.Use(func(c *gin.Context) {
		key := c.Request.Method + " " + c.Request.URL.Path
		if _, ok := chains[key]; !ok {
			chains[key] = c.HandlerNames()
		}
		c.Abort()
	})
	Setup(engine, database)

	missing := []string{}
	for _, ri := range engine.Routes() {
		if ri.Method == http.MethodOptions {
			continue
		}
		key := ri.Method + " " + ri.Path
		if _, ok := chains[key]; ok {
			continue
		}
		engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(ri.Method, ri.Path, nil))
		if _, ok := chains[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("有 %d 条注册路由发请求后拿不到 handler 链（记录守卫没跑在第一位？）：%v",
			len(missing), missing)
	}
	return chains
}

// TestRouteGuardChain_AnonymousSurfaceIsExactlyTheAllowlist 对账匿名面。
func TestRouteGuardChain_AnonymousSurfaceIsExactlyTheAllowlist(t *testing.T) {
	chains := liveRouteGuardChains(t)

	guarded, listed := 0, 0
	var unlistedAnonymous, staleAllowed, wronglyAllowed []string
	for key, chain := range chains {
		isIdentity := chainHasAny(chain, identityGuards)
		if isIdentity {
			guarded++
			if _, ok := anonymousAllowedRoutes[key]; ok {
				wronglyAllowed = append(wronglyAllowed, key)
			}
			continue
		}
		if _, ok := anonymousAllowedRoutes[key]; ok {
			listed++
			continue
		}
		unlistedAnonymous = append(unlistedAnonymous, key)
	}
	for key := range anonymousAllowedRoutes {
		if _, ok := chains[key]; !ok {
			staleAllowed = append(staleAllowed, key)
		}
	}

	if len(unlistedAnonymous) > 0 {
		t.Errorf("有 %d 条 live 路由链上没有身份守卫，却不在匿名清单里：%v"+
			"（要么补守卫，要么在 anonymousAllowedRoutes 里写明为什么允许匿名）",
			len(unlistedAnonymous), unlistedAnonymous)
	}
	if len(staleAllowed) > 0 {
		t.Errorf("匿名清单里有 %d 条已不存在的路由，请删除：%v（清单必须等于事实）",
			len(staleAllowed), staleAllowed)
	}
	if len(wronglyAllowed) > 0 {
		t.Errorf("匿名清单里有 %d 条其实已经带守卫的路由，请删除：%v", len(wronglyAllowed), wronglyAllowed)
	}
	t.Logf("链上带身份守卫 %d 条；匿名可达且已登记理由 %d 条；清单共 %d 行",
		guarded, listed, len(anonymousAllowedRoutes))
}

// TestRouteGuardChain_MonitorRoutesKeepJWT 单独点名那批历史缺陷口：它们必须仍在链上带
// JWT 的那一侧。清单对账已经覆盖这一条，这里是把"当年是 which 7 条"留在读者眼前。
func TestRouteGuardChain_MonitorRoutesKeepJWT(t *testing.T) {
	chains := liveRouteGuardChains(t)
	monitorPaths := []string{
		"/api/monitor/health", "/api/monitor/anomalies", "/api/monitor/node-health",
		"/api/monitor/latency", "/api/monitor/lifecycle", "/api/monitor/traces",
		"/api/monitor/trace-tree",
	}
	for _, p := range monitorPaths {
		key := "GET " + p
		chain, ok := chains[key]
		if !ok {
			t.Errorf("监控口不在 live 路由表里：%s", key)
			continue
		}
		if !chainHasGuard(chain, guardJWT) {
			t.Errorf("监控口 %s 链上没有 JWTAuthMiddleware（注册顺序缺陷复发：auth.Use 之前注册）", key)
		}
	}
}

// TestRouteGuardChain_AdminOnlyRulesAreNotDeadLines 每条"仅超管"规则都必须命中真实路由。
func TestRouteGuardChain_AdminOnlyRulesAreNotDeadLines(t *testing.T) {
	chains := liveRouteGuardChains(t)
	rules := middleware.AdminOnlyRoutePatterns()
	if len(rules) == 0 {
		t.Fatal("仅超管表是空的：这道门会退化成恒真")
	}

	dead := []string{}
	totalHits := 0
	for _, rule := range rules {
		methods, pattern := rule[0], rule[1][0]
		hits := 0
		for key := range chains {
			parts := strings.SplitN(key, " ", 2)
			if patternsMatch(methods, parts[0]) && middleware.MatchAdminOnlyPattern(pattern, parts[1]) {
				hits++
			}
		}
		if hits == 0 {
			dead = append(dead, strings.Join(methods, ",")+" "+pattern)
			continue
		}
		totalHits += hits
	}
	if len(dead) > 0 {
		t.Errorf("仅超管表里有 %d 行命中不到任何 live 路由（路径写错或接口已下线）：%v", len(dead), dead)
	}
	t.Logf("规则 %d 行，命中 live 路由合计 %d 条", len(rules), totalHits)
}

func patternsMatch(methods []string, method string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		if strings.EqualFold(m, method) {
			return true
		}
	}
	return false
}

// TestRouteGuardChain_AdminOnlyRoutesCarrySuperAdminGuard 表命中的路由链上必须真的有超管守卫。
func TestRouteGuardChain_AdminOnlyRoutesCarrySuperAdminGuard(t *testing.T) {
	chains := liveRouteGuardChains(t)
	naked := []string{}
	covered := 0
	for key, chain := range chains {
		parts := strings.SplitN(key, " ", 2)
		if !middleware.IsAdminOnlyRoute(parts[0], parts[1]) {
			continue
		}
		covered++
		if !chainHasAny(chain, superAdminGuards) {
			naked = append(naked, key)
		}
	}
	if covered == 0 {
		t.Fatal("没有任何 live 路由落进仅超管表：守卫形同虚设")
	}
	if len(naked) > 0 {
		t.Errorf("有 %d 条路由命中仅超管表但链上没有超管守卫（多半是注册在 auth 组之外）：%v",
			len(naked), naked)
	}
	t.Logf("仅超管表覆盖 live 路由 %d 条，全部链上带守卫", covered)
}

// TestRouteGuardChain_SetupOnlyOnBootstrapWrites 三条安装引导写入口都必须挂着"只在未初始化
// 时可用"的门；这条不靠清单兜，点名判据。
func TestRouteGuardChain_SetupOnlyOnBootstrapWrites(t *testing.T) {
	chains := liveRouteGuardChains(t)
	for _, key := range []string{
		"POST /api/system/init-admin",
		"POST /api/system/create-default-admin",
		"POST /api/system/init-complete",
	} {
		chain, ok := chains[key]
		if !ok {
			t.Errorf("安装引导口不在 live 路由表里：%s", key)
			continue
		}
		if !chainHasGuard(chain, guardSetupOnly) {
			t.Errorf("%s 链上没有 SetupOnlyGuard：装完之后还能匿名重复走这一步", key)
		}
	}
}

// 行为腿：链上有守卫名字还不够，还得真的拦得住。
//
// 身份档位（匿名 / staff / customer_service / admin）打真实引擎。判据按**响应体文案**而不是状态码：
// 状态码会把"守卫拦下"和"handler 自己回 401/403"并成一类，而这条要证的恰恰是前者。
// admin 腿只断言"不是守卫的那句 403"——handler 在测试库里回什么是它自己的事，
// 门管的是"这道守卫没有把超管也拦在门外"。

func gateEngine(t *testing.T) *gin.Engine {
	t.Helper()
	engine, _ := gateEngineAndDB(t)
	return engine
}

// gateEngineAndDB 装配真实引擎并回库句柄：能力面的行为腿要往 role_menu_permissions
// 里插一行授予、再删掉，只有引擎句柄做不到。
//
// 表里必须带 RoleMenuPermission：它是能力守卫的数据源。少建这张表时守卫读库会拿到
// "relation does not exist"，按无授予拦下一切非超管 —— 用例照样绿（它只断"被拦"），
// 但拦的原因是缺表而不是缺授权，判据就此失去区分力。实测踩过一次。
func gateEngineAndDB(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	t.Setenv("USER_WEB_DIST", t.TempDir())
	t.Setenv("ENABLE_DEBUG_ROUTES", "true")
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{}, &model.SystemUser{},
		&model.RoleMenuPermission{})
	prev := dbutil.GetDB()
	dbutil.SetTestDB(database)
	t.Cleanup(func() {
		dbutil.SetTestDB(prev)
		// 授予查询在装配期绑的是本用例的库句柄，缓存也是包级全局：
		// 留给下一个用例一个指向已回收库的 provider，它读到的就是 stale 值而非真判定。
		middleware.SetMenuGrantProvider(nil)
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)
	return engine, database
}

func gateCall(t *testing.T, engine *gin.Engine, method, path, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// gateRoleToken 按角色签一枚带 data_scope 的令牌：能力档与坐席名单两条行为腿都要
// staff／customer_service 两档身份，参数只差在角色名，收在一处免得各处手拼出不同形状。
// admin 仍走 gateTokens 里原来那个不带 scope 的签法（历史上那批用例就是这么装的）。
func gateRoleToken(t *testing.T, role string) string {
	t.Helper()
	token, err := utils.NewJWTUtils(utils.DefaultJWTConfig).
		GenerateTokenWithScope(7, "gate-"+role, role, "self", 0, 0)
	if err != nil {
		t.Fatalf("签 %s 令牌失败: %v", role, err)
	}
	return token
}

func gateTokens(t *testing.T) (staff string, admin string) {
	t.Helper()
	staff = gateRoleToken(t, "staff")
	var err error
	admin, err = utils.NewJWTUtils(utils.DefaultJWTConfig).GenerateToken(1, "gate-admin", "admin")
	if err != nil {
		t.Fatalf("签 admin 令牌失败: %v", err)
	}
	return staff, admin
}

// TestRouteGuardChain_AnonymousCannotReadPrivilegedSurfaces 本轮修掉的匿名泄露面：
// 整张路由表、webhook 统计。
//
// GET /api/system/info 不在这一档：它的 public 契约（匿名 200 拿 platform_enabled）是
// 前端启动期依赖，泄露面靠 handler 里的字段裁剪收，判据在 system_info_platform_test.go。
func TestRouteGuardChain_AnonymousCannotReadPrivilegedSurfaces(t *testing.T) {
	engine := gateEngine(t)
	for _, key := range [][2]string{
		{"GET", "/__debug__/routes"},
		{"GET", "/api/webhook/stats"},
	} {
		code, body := gateCall(t, engine, key[0], key[1], "")
		if code != http.StatusUnauthorized || !strings.Contains(body, jwtNoTokenMsg) {
			t.Errorf("匿名读 %s 应被 JWT 守卫拦成 401+%q，实得 %d / %s",
				key[1], jwtNoTokenMsg, code, strings.TrimSpace(body))
		}
	}
	// 控制组（正反各一）：同一条口带超管令牌时必须真的走到 handler，
	// 否则上面三条可以在"路由压根没注册"的情况下恒绿。
	_, admin := gateTokens(t)
	code, body := gateCall(t, engine, "GET", "/__debug__/routes", admin)
	if code == http.StatusUnauthorized && strings.Contains(body, jwtNoTokenMsg) {
		t.Errorf("admin 令牌读 %s 被当成无令牌拒了：%d / %s", "/__debug__/routes", code, strings.TrimSpace(body))
	}
	if code == http.StatusNotFound {
		t.Errorf("admin 令牌读 /__debug__/routes 得 404：调试路由没注册，上面那三条断言测的是不存在的东西")
	}
}

// TestRouteGuardChain_StaffRejectedOnAdminOnlyRoutes 仅超管表的行为证据：
// staff 打表内口＝403 且是守卫那句；admin 打同一口＝不被守卫拦。
func TestRouteGuardChain_StaffRejectedOnAdminOnlyRoutes(t *testing.T) {
	engine := gateEngine(t)
	staff, admin := gateTokens(t)

	cases := []struct {
		method, path, denyMsg string
	}{
		// 兼容别名：权威口在 admin 组，别名曾只要求登录。
		// 这两条也进了仅超管表，所以先开口的是 RoutePolicyGuard（它在链上排在
		// 路由自带的 AdminAuthMiddleware 之前），文案是超管那一句而不是管理员那一句。
		{"GET", "/api/feishu/accounts/list", policyDeniedMsg},
		{"GET", "/api/telegram/accounts/list", policyDeniedMsg},
		// 表内口（走 RoutePolicyGuard）：口令规则的写侧。
		{"PUT", "/api/auth/password-policy", policyDeniedMsg},
		// 表内口（走 RoutePolicyGuard）：LLM 供应商健康检查（全局凭据面）。
		{"GET", "/api/llm/providers/health", policyDeniedMsg},
	}
	for _, c := range cases {
		code, body := gateCall(t, engine, c.method, c.path, staff)
		if code != http.StatusForbidden || !strings.Contains(body, c.denyMsg) {
			t.Errorf("staff 打仅超管口 %s %s 应 403+%q，实得 %d / %s",
				c.method, c.path, c.denyMsg, code, strings.TrimSpace(body))
		}
		code, body = gateCall(t, engine, c.method, c.path, admin)
		if code == http.StatusForbidden && strings.Contains(body, c.denyMsg) {
			t.Errorf("admin 打 %s %s 被超管守卫拦了（表或令牌有问题）：%d / %s",
				c.method, c.path, code, strings.TrimSpace(body))
		}
	}
}

// TestRouteGuardChain_StaffStillPassesLoginTier 反向：守卫不许顺手把登录档也拦了。
// 表里只写 PUT /api/auth/password-policy，GET 同一路径必须照旧放行；
// /api/customer-sessions 这类"本人数据"口对 staff 也必须可达。
//
// 清单里的路径必须是**两张表都不沾**的：能力表落进来一条（/api/feature-flags 就落进过）
// 而这里只认前两句拒绝文案，守卫拦下来也照样 PASS —— 门就此失明，判据要连第三档一起吃。
func TestRouteGuardChain_StaffStillPassesLoginTier(t *testing.T) {
	engine := gateEngine(t)
	staff, _ := gateTokens(t)

	// 控制组：令牌本身要能穿过 JWT 守卫，否则下面的"没被拦"是假绿。
	code, body := gateCall(t, engine, "GET", "/api/auth/current-user", staff)
	if code == http.StatusUnauthorized && strings.Contains(body, jwtNoTokenMsg) {
		t.Fatalf("staff 令牌被 JWT 守卫拒了（%d/%s），这道反向判据无从谈起", code, strings.TrimSpace(body))
	}

	for _, c := range [][2]string{
		{"GET", "/api/auth/password-policy"},
		{"GET", "/api/customer-sessions"},
	} {
		if len(middleware.CapabilityOwnerKeysFor(c[1])) > 0 {
			t.Errorf("登录档对照组 %s %s 已在能力表里，它不再是「没被拦」的证据，请换一条并另写授予用例",
				c[0], c[1])
			continue
		}
		code, body := gateCall(t, engine, c[0], c[1], staff)
		if code == http.StatusForbidden &&
			(strings.Contains(body, policyDeniedMsg) ||
				strings.Contains(body, adminAuthDenyMsg) ||
				strings.Contains(body, capabilityDeniedMsg)) {
			t.Errorf("登录档口 %s %s 被当成仅超管/能力口拦下（表写宽了）：%d / %s",
				c[0], c[1], code, strings.TrimSpace(body))
		}
	}
}

// TestRouteGuardChain_PolicyGuardFailsClosedWithoutIdentity 表命中却没有身份＝装配错误，
// 必须向拒的方向收（401），不能因为"读不到 role"就放行。
func TestRouteGuardChain_PolicyGuardFailsClosedWithoutIdentity(t *testing.T) {
	// 一条只挂 RoutePolicyGuard、不挂 JWT 的裸路由：模拟表行注册在没身份的组上。
	engine := gin.New()
	engine.GET("/api/system/configs", middleware.RoutePolicyGuard(),
		func(c *gin.Context) { c.String(http.StatusTeapot, "reached") })

	code, body := gateCall(t, engine, "GET", "/api/system/configs", "")
	if code == http.StatusTeapot {
		t.Errorf("表命中但上下文无身份，守卫却放行了（fail-open）：%d / %s", code, strings.TrimSpace(body))
	}
	if code != http.StatusUnauthorized || !strings.Contains(body, "未授权") {
		t.Errorf("表命中且无身份时应 401 加未授权，实得 %d / %s", code, strings.TrimSpace(body))
	}
}

// —— 能力面（菜单授予）那张生成件的常驻腿 ——
//
// 生成表由 scripts/menu-route-role-audit.py 从"菜单注册表 × live 路由档位 × 前端调用图"
// 推导，一旦落进树里就没有编译器管它了：路径写错的死行不生效、菜单 key 拼错的行会把
// 所有人都拦在门外、注册在守卫之前（auth.Use 快照）的行等于没写。三条静态腿各钉一种失效，
// 第四条钉"授予真的参与判定"（不是恒拦、也不是恒放）。

func TestRouteGuardChain_CapabilityTableNotEmpty(t *testing.T) {
	rules := model.CapabilityPatterns()
	if len(rules) == 0 {
		t.Fatal("能力表是空的：守卫这一档退化成恒真（生成件没进树，或被覆盖成空）")
	}
	t.Logf("能力表 %d 条规则", len(rules))
}

// TestRouteGuardChain_CapabilityRulesAreNotDeadLines 每条 pattern 必须命中至少一条 live 路由，
// 且不能**全部**被仅超管表遮蔽（遮蔽后守卫先走超管分支 return，能力行永远读不到）。
func TestRouteGuardChain_CapabilityRulesAreNotDeadLines(t *testing.T) {
	chains := liveRouteGuardChains(t)
	rules := model.CapabilityPatterns()

	dead, shadowed := []string{}, []string{}
	totalHits := 0
	for _, rule := range rules {
		pattern := rule[0][0]
		hits, adminOnlyHits := 0, 0
		for key := range chains {
			parts := strings.SplitN(key, " ", 2)
			if !model.MatchCapabilityPattern(pattern, parts[1]) {
				continue
			}
			hits++
			if middleware.IsAdminOnlyRoute(parts[0], parts[1]) {
				adminOnlyHits++
			}
		}
		if hits == 0 {
			dead = append(dead, pattern)
			continue
		}
		if adminOnlyHits == hits {
			shadowed = append(shadowed, pattern)
			continue
		}
		totalHits += hits - adminOnlyHits
	}
	if len(dead) > 0 {
		t.Errorf("能力表里有 %d 条命中不到任何 live 路由（路径写错或接口已下线）：%v", len(dead), dead)
	}
	if len(shadowed) > 0 {
		t.Errorf("能力表里有 %d 条命中的路由全被仅超管表遮蔽（这一行永远轮不到裁决）：%v"+
			"（要么它们该从能力表里去掉，要么仅超管表写宽了）", len(shadowed), shadowed)
	}
	t.Logf("规则 %d 条，实际由能力档裁决的 live 路由 %d 条", len(rules), totalHits)
}

// TestRouteGuardChain_CapabilityMenuKeysExist 每条归属菜单 key 必须在注册表里存在。
// 拼错的 key 永远查不到授予 ⇒ 这条口对所有非超管恒 403，症状和"接口坏了"一模一样。
func TestRouteGuardChain_CapabilityMenuKeysExist(t *testing.T) {
	unknown := []string{}
	keyTotal := 0
	for _, rule := range model.CapabilityPatterns() {
		pattern := rule[0][0]
		if len(rule[1]) == 0 {
			t.Errorf("能力规则 %s 没有任何归属菜单 key：它对非超管恒拦，且界面无从放开", pattern)
			continue
		}
		for _, k := range rule[1] {
			keyTotal++
			if !model.ExistsMenuKey(k) {
				unknown = append(unknown, pattern+" -> "+k)
			}
		}
	}
	if len(unknown) > 0 {
		t.Errorf("能力表里有 %d 个菜单 key 不在注册表里：%v", len(unknown), unknown)
	}
	t.Logf("归属引用合计 %d 处，全部命中注册表", keyTotal)
}

// TestRouteGuardChain_CapabilityRoutesCarryGuard 命中能力表的 live 路由，链上必须同时有
// RoutePolicyGuard（裁决者）和 JWT（role 的来源）。能力行写在守卫快照之外的路由上＝静默无效，
// 正是当初 7 条 /api/monitor/* 匿名可读的同一种缺陷。
func TestRouteGuardChain_CapabilityRoutesCarryGuard(t *testing.T) {
	chains := liveRouteGuardChains(t)
	naked, noIdentity := []string{}, []string{}
	covered, anonymous := 0, []string{}
	for key, chain := range chains {
		parts := strings.SplitN(key, " ", 2)
		if len(middleware.CapabilityOwnerKeysFor(parts[1])) == 0 {
			continue
		}
		if middleware.IsAdminOnlyRoute(parts[0], parts[1]) {
			continue // 由仅超管档裁决，已在上一条腿里排除
		}
		if _, ok := anonymousAllowedRoutes[key]; ok {
			// 能力表是"路径前缀 + 段通配"、没有方法维，通配段会连带吃到同层的公开字面量段：
			// /api/shortlink/* 同时罩住访客侧公开口 POST /api/shortlink/access。公开口按设计
			// 不带任何守卫，这条断言对它没有意义（它由匿名清单那条腿双向钉住：一旦被人接上
			// JWT/守卫，清单对账就红）。这里跳过并点名，别让它变成静默的例外。
			anonymous = append(anonymous, key)
			continue
		}
		covered++
		if !chainHasGuard(chain, guardPolicy) {
			naked = append(naked, key)
			continue
		}
		if !chainHasGuard(chain, guardJWT) {
			noIdentity = append(noIdentity, key)
		}
	}
	if covered == 0 {
		t.Fatal("没有 live 路由落进能力档：这张生成表没在裁决任何东西")
	}
	if len(naked) > 0 {
		t.Errorf("有 %d 条路由在能力表里但链上没有 RoutePolicyGuard（注册在 auth.Use 之前）：%v",
			len(naked), naked)
	}
	if len(noIdentity) > 0 {
		t.Errorf("有 %d 条能力口链上没有 JWTAuthMiddleware（守卫读不到 role）：%v", len(noIdentity), noIdentity)
	}
	slices.Sort(anonymous)
	t.Logf("能力档裁决 live 路由 %d 条；通配连带到的公开口 %d 条（%v，由匿名清单那条腿管）",
		covered, len(anonymous), anonymous)
}

// TestRouteGuardChain_CapabilityGrantDrivesAccess 行为腿：判定跟着授予走，且授予撞上界。
//
// 七步闭环，缺任何一步都能被"恒拦"或"恒放"的守卫骗过去：
//
//	一. 出厂分配不含该能力（staff 打超管专属口）⇒ 拦（403 + 能力文案）；
//	二. 同一口给超管 ⇒ 不被这档拦；
//	三. 库里塞一行**注册表不允许 staff 拥有**的授予（systemFeatureFlags）⇒ 仍然拦。
//	   这一条钉的是"授权上界在守卫这一侧也生效"：分配表只能在注册表之内再收窄，
//	   脏行／历史遗留行开不出超管专属口；
//	四. 上界之内的角色（customer_service）在表非空但该角色零行 ⇒ 拦。
//	   这条同时证明判定读的是表，不是出厂词表——quickReply 就在 cs 的出厂默认里，
//	   表非空时它不作数；
//	五. 插一行 (customer_service, quickReply) 并撤缓存 ⇒ 那句 403 必须消失
//	   （handler 在测试库里回什么不由门管）；
//	六. 删掉那行 ⇒ 回到拦；
//	七. 收尾清点：本用例不留任何授予行，否则后续用例读到的"表是否空"口径会被改变。
func TestRouteGuardChain_CapabilityGrantDrivesAccess(t *testing.T) {
	engine, database := gateEngineAndDB(t)
	staff, admin := gateTokens(t)
	cs := gateRoleToken(t, "customer_service")

	const adminOnlyPath = "/api/feature-flags"
	adminOnlyOwners := middleware.CapabilityOwnerKeysFor(adminOnlyPath)
	if len(adminOnlyOwners) == 0 {
		t.Fatalf("%s 不在能力表里，第三、四步的前置没了", adminOnlyPath)
	}
	adminOnlyKey := adminOnlyOwners[0]
	if _, ok := model.EligibleMenuKeys("staff")[adminOnlyKey]; ok {
		t.Fatalf("注册表已把 %s 放开给 staff，第三/四步的前置没了", adminOnlyKey)
	}
	const grantPath = "/api/quick-replies"
	grantKey := "quickReply"
	if !slices.Contains(middleware.CapabilityOwnerKeysFor(grantPath), grantKey) {
		t.Fatalf("%s 不在能力表里，这条行为腿测的是不存在的东西", grantPath)
	}
	if _, ok := model.EligibleMenuKeys("customer_service")[grantKey]; !ok {
		t.Fatalf("注册表没把 %s 放开给 customer_service，第五步的前置没了", grantKey)
	}

	// 一、出厂分配不含该能力 ⇒ 拦。
	code, body := gateCall(t, engine, "GET", adminOnlyPath, staff)
	if code != http.StatusForbidden || !strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("未授予时 staff 打 %s 应 403+%q，实得 %d / %s",
			adminOnlyPath, capabilityDeniedMsg, code, strings.TrimSpace(body))
	}

	// 二、超管恒放行（这一档不该把 admin 也关在外面）。
	code, body = gateCall(t, engine, "GET", adminOnlyPath, admin)
	if code == http.StatusForbidden && strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("admin 打 %s 被能力档拦了：%d / %s", adminOnlyPath, code, strings.TrimSpace(body))
	}

	// 三、脏授予行不撞上界 ⇒ 依然拦。这行同时把"表是否为空"翻成非空，供第四、五步用。
	if err := database.Create(&model.RoleMenuPermission{RoleCode: "staff", MenuKey: adminOnlyKey}).Error; err != nil {
		t.Fatalf("插入越界授予行失败: %v", err)
	}
	middleware.InvalidateMenuGrants("")
	code, body = gateCall(t, engine, "GET", adminOnlyPath, staff)
	if code != http.StatusForbidden || !strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("库里有越界授予行 %s，staff 打 %s 却放行了（上界没参与守卫判定）：%d / %s",
			adminOnlyKey, adminOnlyPath, code, strings.TrimSpace(body))
	}

	// 四、表非空、该角色零行 ⇒ 拦（判定读表，不读出厂词表）。
	code, body = gateCall(t, engine, "GET", grantPath, cs)
	if code != http.StatusForbidden || !strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("customer_service 无授予行时打 %s 应 403+%q，实得 %d / %s",
			grantPath, capabilityDeniedMsg, code, strings.TrimSpace(body))
	}

	// 五、授予 ⇒ 守卫那句 403 必须消失。
	if err := database.Create(&model.RoleMenuPermission{RoleCode: "customer_service", MenuKey: grantKey}).Error; err != nil {
		t.Fatalf("插入授予行失败: %v", err)
	}
	middleware.InvalidateMenuGrants("")
	code, body = gateCall(t, engine, "GET", grantPath, cs)
	if code == http.StatusForbidden && strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("已授予 %s 却仍被能力档拦下（授予没参与判定，或撤缓存没生效）：%d / %s",
			grantKey, code, strings.TrimSpace(body))
	}

	// 六、撤授予 ⇒ 回到拦，兼作"跟着授予走"的另一半。
	if err := database.Where("role_code = ? AND menu_key = ?", "customer_service", grantKey).
		Delete(&model.RoleMenuPermission{}).Error; err != nil {
		t.Fatalf("清理授予行失败: %v", err)
	}
	middleware.InvalidateMenuGrants("")
	code, body = gateCall(t, engine, "GET", grantPath, cs)
	if code != http.StatusForbidden || !strings.Contains(body, capabilityDeniedMsg) {
		t.Errorf("撤授予后 customer_service 打 %s 应回到 403+%q，实得 %d / %s",
			grantPath, capabilityDeniedMsg, code, strings.TrimSpace(body))
	}

	if err := database.Where("role_code = ? AND menu_key = ?", "staff", adminOnlyKey).
		Delete(&model.RoleMenuPermission{}).Error; err != nil {
		t.Fatalf("清理越界授予行失败: %v", err)
	}
	var left int64
	database.Model(&model.RoleMenuPermission{}).Count(&left)
	if left != 0 {
		t.Errorf("本用例残留 %d 行授予数据，会改变后续用例的授予口径", left)
	}
}
