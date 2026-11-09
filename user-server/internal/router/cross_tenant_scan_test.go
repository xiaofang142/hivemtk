package router

// A16（`未完成任务.md` §9）：**逐端点**双租户交叉扫描。
//
// 前面几项修的都是单条链路（WS 广播、/files、缓存、限流、事件总线…）。本条回答的是一个
// 全局问题：**700 多条带路径参数的端点里，有没有哪一条会把租户 A 的行返回给租户 B？**
// 抽查几条不构成结论 —— 抽查全绿只说明抽查到的那几条没事。
//
// 已有的 route_policy_gate_test.go 管的是另一件事："每条路由要么链上有身份守卫，要么在
// 匿名清单里写明为什么可以匿名"。它管**有没有守卫**，不管**取数有没有分域**。两者互补：
// 一条路由可以既有 JWT 又仍然把别人的行读出来（守卫只证明"你是谁"，不证明"你只能看自己的"）。
//
// 所以本文件补三条腿：
//
//   - 闸一（匿名参数路由的租户定位机制）：没有身份守卫、路径里还带参数的端点，**必须能说出
//     这一行/这个租户是怎么定位的**。已经被链上租户解析中间件覆盖的（`PublicIDTenant` /
//     `PublicCodeTenant` / `PublicStringIDTenant` / `AppKeyResolve`）由本文件按活体 handler
//     链自动认；其余必须逐条登记在 anonymousParamMechanisms 里写明机制。未登记 ⇒ 红。
//     这条闸对准本审计反复出现的形状：**没有 JWT ⇒ 没有租户 claim ⇒ 每一次租户表访问都
//     只能靠调用方显式分域**（P0-2 的 `/files`、A7 的知识库导入都是这个形状）。
//   - 闸二（按行自证类必须拒未知键）：链上带"按行/按码定位租户"中间件的匿名参数路由，
//     拿一个一定不存在的键去请求，**不允许 200** —— 定位不到行就该拒，而不是渲染一个空壳。
//     这是闸一的机制声明在行为上的对照：声明了"按行解析租户"，就得真的先解析、解析不到就拒。
//   - 闸三（真扫）：真库上按租户播种，再拿**另一个租户**的令牌去读同一行。判据是
//     "本租户 200 / 外租户 404"。每条都带**正对照**（本租户必须 200）—— 否则外租户的 404
//     可能只是因为这条链路谁读都 404（端点不通 / 表没建），那种绿是空转。
//
// 覆盖边界（写下来，防"全绿 = 全覆盖"的误读）：
//   - 闸三覆盖的是**取数风格**的样例（自增主键 / 业务字符串主键 / 跨包模型 / jsonb 表），
//     不是全部 700 条端点。剩下那些由 GORM 隔离 callback 在 SQL 构造期统一注入租户条件，
//     callback 自身的注入/fail-closed 判据在 pkg/tenancyidx 与本文件闸三的 6 条样例里各验一次。
//   - 本文件不判定"公开分享页该不该公开"（那是产品口径，见 route_policy_gate_test.go 的匿名清单）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// tenantResolvers 在链上就能看出"按行/按键解析归属租户"的中间件。
// 名字取自 gin 暴露的 HandlerFunc 名：Public*Tenant 是具名包装，但其实现
// 是闭包 publicResourceTenant——gin 的 HandlerNames 记录的是闭包实际函数名
// `middleware.publicResourceTenant.funcN`（小写、带 .funcN），构造器名
// PublicIDTenant 反而不出现在链上（A-3：词表失配曾让闸一误报 14 条、闸二空转）。
// 两层名都进词表：构造器改名或闭包内联方式变化时，另一层仍兜得住。
var tenantResolvers = []string{
	"publicResourceTenant", // 闭包实际名（gin HandlerNames 暴露的形态）
	"PublicIDTenant",       // 按数字主键 :id 反查行所属租户
	"PublicStringIDTenant", // 按字符串主键 :id
	"PublicCodeTenant",     // 按业务码 :code
	"AppKeyResolve",        // 官网访客组件：AppKey → 渠道 → 租户
}

// anonymousParamMechanisms 没有身份守卫、链上也没有租户解析中间件的参数路由，
// **逐条**写明"这一行/这个租户靠什么定位"。
//
// 值不是注释，是要被追问的判据：写不出"谁把租户限定住"的路由，不该匿名。
var anonymousParamMechanisms = map[string]string{
	// —— 按 code 自证：先做租户内省，跨租户歧义直接判不存在 ——
	"GET /api/dashboards/public/:code": "仓库层按 code 做租户内省（SystemScope 查 code→tenant_id，要求恰好命中一个租户，歧义即 not-found），再以该租户 ctx 取行",

	// —— 签名 / 一次性 token 自证 ——
	"GET /api/email/track/open/:token":  "HMAC 签名 token：验签失败即不记账；响应恒为 1x1 像素（本文件闸二对它豁免「不得 200」）",
	"GET /api/email/track/click/:token": "HMAC 签名 token：跳转目标只认 claim.Target，验签失败不跳转",

	// —— handler 内显式解析租户（与中间件同一套 resolvePublicTenantCtx）——
	"GET /api/public/help-center/articles/:id": "handler 内 resolvePublicTenantCtx(tenant_code) 先定租户，再按该租户取文章（多租户下缺 tenant_code 直接 400）",
	"GET /api/sso/login/:provider":             "handler 内 resolvePublicTenantCtx(tenant_code)：IdP 配置按租户解析，缺租户直接 400",
	"GET /api/sso/callback/:provider":          "handler 内 resolvePublicTenantCtx(tenant_code/cookie) + state cookie 校验；租户先定再换令牌",

	// —— 渠道回调：按 (channel, account_id) 解析归属租户，解析不到 fail-closed ——
	// 依据 controller/webhook.go 的 bindCallbackTenant（返回 !bound 时请求已被拒），
	// 与 specs/001-saas-multi-tenant T022 同一条判据。
	"POST /api/webhook/:channel":                    "handler 内 bindCallbackTenant：按 (channel, account_id) 解析归属租户，解析失败 404 fail-closed（T022）",
	"POST /api/webhook/:channel/:account_id":        "handler 内 bindCallbackTenant：按 (channel, account_id) 解析归属租户，解析失败 404 fail-closed（T022）",
	"POST /api/webhook/dingtalk/:account_id":        "handler 内 bindCallbackTenant：按 (channel, account_id) 解析归属租户，解析失败 404 fail-closed（T022）",
	"POST /api/webhook/wechat/:account_id":          "handler 内 bindCallbackTenant：按 (channel, account_id) 解析归属租户，解析失败 404 fail-closed（T022）",
	"GET /api/webhook/dingtalk/:account_id":         "URL 校验握手（DingTalkVerify）：先解析账号归属租户，解析不到即拒",
	"GET /api/webhook/feishu/:account_id":           "URL 校验握手（FeishuVerify）：先解析账号归属租户，解析不到即拒",
	"GET /api/webhook/wechat/:account_id":           "URL 校验握手（VerifyURL）：先解析账号归属租户，解析不到即拒",
	"GET /api/webhook/wecom/:account_id":            "URL 校验握手（WeComVerify）：先解析账号归属租户，解析不到即拒",
	"GET /api/webhook/whatsapp/:account_id":         "URL 校验握手（WhatsAppVerify）：先解析账号归属租户，解析不到即拒",
	"POST /api/integration/webhook/order/:platform": "订单回调：VerifyWebhookKey 复核 + 平台侧渠道账号定位租户",
	// 模板/技能市场（F2 只读橱窗）：目录是平台级静态清单（TemplateMarketEntry 无
	// tenant_id，数据源是代码内建目录），行由模板 ID 定位；响应只有名称/分类/简介/
	// 来源/可用性/标签，无个人与租户数据 ⇒ 没有"租户表访问"要限定。
	"GET /api/template-market/detail/:id": "公开橱窗详情：行由模板 ID 定位（平台级目录，无 tenant_id），只回元数据",
}

// alwaysOK 闸二的豁免：这些匿名参数路由按契约必须恒 200（追踪像素），不是"漏了 404"。
var alwaysOK = map[string]bool{
	"GET /api/email/track/open/:token": true,
}

// ── 闸一：匿名参数路由必须能说出"租户/行怎么定位" ───────────────────────────

func TestAnonymousParamRoutesDeclareTenantMechanism(t *testing.T) {
	// 复用既有闸的装配（同一张生产路由表 + 抓 handler 链），不另造一个引擎：
	// 两个闸看到的必须是同一张路由表，否则"一个闸绿一个闸红"又会变成两套事实。
	chains := liveRouteGuardChains(t)

	anonymousParam := 0
	viaResolver := 0
	for spec, chain := range chains {
		if !strings.Contains(spec, ":") {
			continue
		}
		if chainHasAny(chain, identityGuards) {
			continue // 有身份守卫：租户从令牌 claim 来，归闸三与 callback 管
		}
		anonymousParam++

		hit := ""
		for _, r := range tenantResolvers {
			if chainHasGuard(chain, r) {
				hit = r
				break
			}
		}
		if hit != "" {
			viaResolver++
			continue
		}
		if _, ok := anonymousParamMechanisms[spec]; !ok {
			t.Errorf("匿名参数路由 %s 既没有身份守卫、链上也没有租户解析中间件，且没有登记机制。\n"+
				"没有 JWT 就没有租户 claim：这条链路上的每一次租户表访问都只能靠调用方显式分域。"+
				"要么挂上 PublicIDTenant/PublicCodeTenant/PublicStringIDTenant/AppKeyResolve，"+
				"要么在 anonymousParamMechanisms 里写明靠什么把租户/行限定住。", spec)
		}
	}

	// 反向：台账里不许有失效条目（路由已删 / 已经挂上守卫或解析中间件）
	for spec := range anonymousParamMechanisms {
		chain, ok := chains[spec]
		if !ok {
			t.Errorf("台账里的 %s 在真实路由表里不存在（条目已失效，会让人以为覆盖过）", spec)
			continue
		}
		if chainHasAny(chain, identityGuards) {
			t.Errorf("台账里的 %s 现在链上有身份守卫了：它不再需要这条机制说明（台账要跟着代码走）", spec)
		}
		for _, r := range tenantResolvers {
			if chainHasGuard(chain, r) {
				t.Errorf("台账里的 %s 现在已经挂了 %s：机制说明该删掉，让台账继续等于事实", spec, r)
			}
		}
	}

	t.Logf("匿名参数路由 %d 条：链上解析租户 %d 条 / 台账登记 %d 条",
		anonymousParam, viaResolver, len(anonymousParamMechanisms))
}

// ── 闸二：按行自证类必须拒未知键 ───────────────────────────────────────────

func TestAnonymousRowScopedRoutesRejectUnknownKey(t *testing.T) {
	chains := liveRouteGuardChains(t)

	// 只挑"按行/按码定位租户"的三类：它们的契约就是"定位不到行 ⇒ 拒"，
	// 所以未知键必须 404。AppKeyResolve 那组还要看请求头里的 AppKey，拒绝形态不同，
	// 由访客链自己的用例管，这里不掺和。匹配走闭包实际名（A-3，同 tenantResolvers）。
	rowScoped := []string{"publicResourceTenant"}

	db := testutil.NewTestDB(t,
		&model.ShortLink{}, &model.LiveCode{},
		&model.DouyinCard{}, &model.KuaishouCard{}, &model.XiaohongshuCard{}, &model.XianyuCard{},
	)
	if db == nil {
		return
	}
	engine := a16Engine(t, db)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	checked := 0
	for spec, chain := range chains {
		if !strings.Contains(spec, ":") || chainHasAny(chain, identityGuards) {
			continue
		}
		matched := false
		for _, r := range rowScoped {
			if chainHasGuard(chain, r) {
				matched = true
				break
			}
		}
		if !matched || alwaysOK[spec] {
			continue
		}
		parts := strings.SplitN(spec, " ", 2)
		method, routePath := parts[0], parts[1]
		// 取一个必然不存在的键（数字/字符串两种形态都给一遍）
		for _, unknown := range []string{"987654321", "a16-no-such-key"} {
			url := setPathParam(routePath, unknown)
			req := httptest.NewRequest(method, url, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				t.Errorf("%s 用不存在的键 %q 请求却回了 200（body=%s）：按行解析租户的公开口"+
					"定位不到行时必须拒，不能渲染空壳 —— 那等于把「键空间」直接暴露成可枚举的",
					spec, unknown, truncateBody(w.Body.String()))
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("一条按行自证的匿名参数路由都没扫到（路由没挂上/链名变了），这条闸变成空转")
	}
	t.Logf("按行自证类匿名参数路由：未知键探测 %d 次", checked)
}

// ── 闸三：真扫（本租户 200 / 外租户 404）────────────────────────────────────

type crossTenantScanTarget struct {
	method string
	// path 是 gin 路由表里的形态（含参数名）
	path string
	// idOf 在租户 5 名下播一行并返回该行的路径参数值
	idOf func(t *testing.T, db *gorm.DB) string
	// marker 播种行的可辨识值：正对照必须能在 200 响应体里读到它。
	// 只看状态码是不够的 —— 模块闸/租户中间件都可能回 200 但**根本没取到行**
	// （实测 /api/email/smtp/:id 在两个租户下都回 200 + "该功能模块未开通"）。
	marker string
	// why 这条为什么值得扫（取数风格 / 主键形态 / 跨包模型）
	why string
}

func crossTenantScanTargets() []crossTenantScanTarget {
	return []crossTenantScanTarget{
		{
			method: http.MethodGet,
			path:   "/api/alerts/rules/:id",
			why:    "自增主键 + repository.First(&m, id)",
			marker: "a16-rule",
			idOf: func(t *testing.T, db *gorm.DB) string {
				row := model.AlertRule{Name: "a16-rule", Source: "a16"}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 alert_rules 失败: %v", err)
				}
				return strconv.FormatUint(uint64(row.ID), 10)
			},
		},
		{
			method: http.MethodGet,
			path:   "/api/short-link/:id",
			why:    "自增主键 + 表上带业务唯一码（唯一索引已被复合化成 (tenant_id, short_code)）",
			marker: "a16short",
			idOf: func(t *testing.T, db *gorm.DB) string {
				row := model.ShortLink{ShortCode: "a16short", OriginalURL: "https://example.com/a16"}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 short_links 失败: %v", err)
				}
				return strconv.FormatUint(uint64(row.ID), 10)
			},
		},
		{
			method: http.MethodGet,
			path:   "/api/ai/templates/:id",
			why:    "跨包模型（content/model）经 content/repository 取数",
			marker: "a16-tpl",
			idOf: func(t *testing.T, db *gorm.DB) string {
				row := contentmodel.PromptTemplate{
					Name: "a16-tpl", Type: contentmodel.AIGenerationTypeCopywriting, Template: "hi",
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 prompt_templates 失败: %v", err)
				}
				return strconv.FormatUint(uint64(row.ID), 10)
			},
		},
		{
			method: http.MethodGet,
			path:   "/api/prompts/:id",
			why:    "feedback_loop 表（jsonb 列 + 自增主键）",
			marker: "a16-prompt-x",
			idOf: func(t *testing.T, db *gorm.DB) string {
				row := model.PromptCandidate{
					Scenario: "a16", Version: "v1", Title: "a16-prompt-x",
					SystemPrompt: "sys", UserPromptTemplate: "user",
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 prompt_candidates 失败: %v", err)
				}
				return strconv.FormatUint(uint64(row.ID), 10)
			},
		},
		{
			method: http.MethodGet,
			path:   "/api/live-code/:id",
			why:    "非自增主键（varchar(36) 业务 ID，BeforeCreate 生成）：路径参数不是数字，靠 id 前缀做不了隔离",
			marker: "a16-livecode",
			idOf: func(t *testing.T, db *gorm.DB) string {
				// live_codes 的三个 domain 列都是真外键（fk_live_codes_*），
				// 测试库是按会话关外键的，跨连接不生效 ⇒ 这里老实建一条域名。
				dom := model.DomainPool{Domain: "a16.example.com"}
				if err := db.Create(&dom).Error; err != nil {
					t.Fatalf("播种 domain_pool 失败: %v", err)
				}
				row := model.LiveCode{
					Name: "a16-livecode", ShortLink: "a16lc",
					ShortDomainID: dom.ID, EntryDomainID: dom.ID, LandingDomainID: dom.ID,
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 live_codes 失败: %v", err)
				}
				return row.ID
			},
		},
		{
			method: http.MethodGet,
			path:   "/api/human-tasks/:id",
			why:    "业务主键（text，形如 ht_…）+ 部分唯一索引",
			marker: "ht_a16_1",
			idOf: func(t *testing.T, db *gorm.DB) string {
				row := model.HumanTask{
					ID: "ht_a16_1", Kind: model.HumanTaskKindConversationHandoff,
					Status: model.HumanTaskStatusPending, SubjectType: "customer_session", SubjectID: "sess-a16",
				}
				if err := db.Create(&row).Error; err != nil {
					t.Fatalf("播种 human_tasks 失败: %v", err)
				}
				return row.ID
			},
		},
	}
}

func TestCrossTenantScan_ForeignTenantCannotRead(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.AlertRule{}, &model.ShortLink{}, &contentmodel.PromptTemplate{},
		&model.PromptCandidate{}, &model.LiveCode{}, &model.DomainPool{}, &model.HumanTask{},
	)
	if db == nil {
		return
	}
	engine := a16Engine(t, db)

	// on 模式 + testutil 建库时已注册的隔离 callback = 生产 SaaS 形态
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	const owner, foreign = uint(5), uint(6)
	ownerTok, foreignTok := a16Token(t, owner), a16Token(t, foreign)

	// 扫描目标必须在真实路由表里（拼错路径的"扫描"什么都没验）
	inTable := map[string]bool{}
	for _, r := range engine.Routes() {
		inTable[r.Method+" "+r.Path] = true
	}

	for _, tg := range crossTenantScanTargets() {
		tg := tg
		if !inTable[tg.method+" "+tg.path] {
			t.Errorf("扫描目标 %s %s 不在真实路由表里", tg.method, tg.path)
			continue
		}
		t.Run(tg.method+" "+tg.path, func(t *testing.T) {
			// 播种走 owner 的 ctx：create callback 自动填 tenant_id
			// （on 模式无租户 ctx 的写入会被 callback 直接拒掉，这也是闸三的前置条件）
			id := tg.idOf(t, db.WithContext(tenant.WithTenantID(context.Background(), owner)))
			url := setPathParam(tg.path, id)

			// 正对照：本租户必须 200 **且**响应体里读得到播种行。
			// 只对状态码会把"200 + 模块未开通/空壳"也算通过，那种绿什么也没证明。
			code, body := a16Get(t, engine, url, ownerTok)
			if code != http.StatusOK || !strings.Contains(body, tg.marker) {
				t.Fatalf("正对照失败：租户 %d 读自己那行应 200 且响应体含 %q，实际 %d body=%s\n"+
					"（没有这条对照，下面的 404 什么也不能说明：模块闸挡在前面时两个租户都拿不到行）",
					owner, tg.marker, code, truncateBody(body))
			}
			// 判据：外租户读同一行必须 404/403，绝不能 200
			code, body = a16Get(t, engine, url, foreignTok)
			if code == http.StatusOK {
				t.Fatalf("跨租户泄漏：租户 %d 读到了租户 %d 的行（%s id=%s）body=%s",
					foreign, owner, tg.path, id, truncateBody(body))
			}
			if code != http.StatusNotFound && code != http.StatusForbidden {
				t.Fatalf("外租户读别人那行应 404/403，实际 %d body=%s（%s）", code, truncateBody(body), tg.why)
			}
		})
	}
}

// ── 小工具 ─────────────────────────────────────────────────────────────────

// a16Engine 跑一次真 Setup（与生产同一张路由表），并把全局 DB 句柄指过去。
func a16Engine(t *testing.T, db *gorm.DB) *gin.Engine {
	t.Helper()
	t.Setenv("USER_WEB_DIST", t.TempDir())
	t.Setenv("ENABLE_DEBUG_ROUTES", "true")
	prev := dbutil.GetDB()
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(prev) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, db)
	return engine
}

// a16Token 铸一枚"租户 N 的超管"令牌。JWT 中间件只读 claim 不回查库，
// 所以库里不需要真存在这个用户 —— 这是刻意的：判据只允许落在
// "租户 claim 有没有进 ctx、进了之后取数有没有分域"这两步上。
func a16Token(t *testing.T, tenantID uint) string {
	t.Helper()
	tok, err := utils.NewJWTUtils(utils.DefaultJWTConfig).
		GenerateTokenForTenant(1, "a16-admin", "admin", "all", 0, 0, tenantID)
	if err != nil {
		t.Fatalf("签租户 %d 令牌失败: %v", tenantID, err)
	}
	return tok
}

func a16Get(t *testing.T, engine *gin.Engine, url, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

var pathParamRe = regexp.MustCompile(`:[A-Za-z_]+`)

// setPathParam 把路由里**全部**参数段替换成同一个值（本文件用不到多参数场景）。
func setPathParam(routePath, value string) string {
	return pathParamRe.ReplaceAllString(routePath, value)
}
