package router

// TestDumpLiveRouteTable 把生产装配入口 Setup() 跑出来的那张路由表原样导成 TSV，供离线取证复算。
//
// 为什么要有它：同目录的 route_surface_consistency_test.go 是**门**，它只在比对失败时出声，
// 不会把"哪些路由没有任何消费方"这份名单交出来。而僵尸接口分诊要的不是一个通过率，
// 是一张可复核的名册（判"某条口能不能下线/要不要补前端接线"得逐条点名）。
// 2026-10-11 的那轮分诊用一份一次性脚本算出四档计数，脚本没入库 ⇒ 今天既复算不出那四个数，
// 也拿不到名册。这里补的就是那条"事实源出口"：表还是同一张表（同一个 liveRoutes），
// 只是多一个导出口，不新增判据、不动门。
//
// 为什么不挂进 CI：没设 ROUTE_DUMP_FILE 时它直接 Skip ⇒ 门禁与 CI 都不为它付建库成本。
// 它是取证件，不是门；把它当门用会退化成"每次 CI 都导一遍没人读的快照"。

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/middleware"
)

// routeDumpFileEnv 是导出路径的环境变量名：设为某个文件路径才导出，未设则跳过本用例。
const routeDumpFileEnv = "ROUTE_DUMP_FILE"

func TestDumpLiveRouteTable(t *testing.T) {
	target := strings.TrimSpace(os.Getenv(routeDumpFileEnv))
	if target == "" {
		t.Skipf("未设置 %s：本用例只在需要复算路由消费面时导出路由表，不作为门禁", routeDumpFileEnv)
	}
	exact, _ := liveRoutes(t)

	rows := make([]string, 0, len(exact))
	for key, handler := range exact {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("路由表键的形状不是 \"METHOD /path\"：%q", key)
		}
		rows = append(rows, parts[0]+"\t"+parts[1]+"\t"+handler)
	}
	sort.Strings(rows)

	f, err := os.Create(target)
	if err != nil {
		t.Fatalf("写路由表导出件 %s 失败：%v", target, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	if _, err := w.WriteString("METHOD\tPATH\tHANDLER\n"); err != nil {
		t.Fatalf("写表头失败：%v", err)
	}
	for _, r := range rows {
		if _, err := w.WriteString(r + "\n"); err != nil {
			t.Fatalf("写行失败：%v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush 失败：%v", err)
	}

	// 自证：导出件非空且含一条本仓一定在的口，否则"复算"会建立在一个空文件上。
	// 挑 GET /health：Setup()（router.go:165）在第 200 行无条件注册它，
	// 它不在鉴权组里、不随前端构建产物或特性位变化，因此这条缺席只可能是"导出的不是完整路由表"。
	if len(rows) == 0 {
		t.Fatalf("Setup() 跑出的路由表是空的：装配入口坏了，导出无意义")
	}
	var sawHealth bool
	for _, r := range rows {
		if strings.HasPrefix(r, "GET\t/health\t") {
			sawHealth = true
			break
		}
	}
	if !sawHealth {
		t.Fatalf("导出的 %d 行里没有 GET /health ⇒ 事实源不是完整的路由表，读数不可用", len(rows))
	}
	t.Logf("导出 %d 行路由表到 %s", len(rows), target)
}

// routeGuardDumpFileEnv 是守卫链导出路径的环境变量名：设为某个文件路径才导出，未设则跳过。
const routeGuardDumpFileEnv = "ROUTE_GUARD_DUMP_FILE"

// 档位名：非超管能不能走到 handler，由五类授权来源区分。
const (
	tierAnonymous          = "anonymous"
	tierIdentity           = "identity"
	tierCapability         = "capability"
	tierSuperAdmin         = "super-admin"
	tierExternalCredential = "external-credential"
)

// routeGuardTier 把一条 live 路由的守卫链折成"有效档位"。
//
// 判的是**有效**授权而不是"链上出现过哪个守卫名"：RoutePolicyGuard 挂在 auth 组的每一条
// 链上（router.go 里 auth.Use），但它只在路径命中仅超管表时才真的拦人——按守卫名分档会把
// 全表读成"仅超管"（实测踩过，正控制当场抓住它）。
//
// 能力档（capability）是链上有身份守卫、但拦不拦取决于调用者有没有被授予该形状所属菜单节点
// 的那一档；它必须是独立一档而不是混进 identity，否则"任何登录角色都能调"的口径会把
// 能力口一起算成泄露面。
func routeGuardTier(method, path string, chain []string) string {
	hasIdentity := chainHasAny(chain, identityGuards)
	hardAdmin := chainHasGuard(chain, guardRequireAdm) || chainHasGuard(chain, guardAdminAuth)
	policyAdmin := chainHasGuard(chain, guardPolicy) && middleware.IsAdminOnlyRoute(method, path)
	switch {
	case hasIdentity && (hardAdmin || policyAdmin):
		return tierSuperAdmin
	case chainHasGuard(chain, guardMCP) || isSageXExternalIdentityRoute(method, path):
		return tierExternalCredential
	case hasIdentity && len(middleware.CapabilityOwnerKeysFor(path)) > 0:
		return tierCapability
	case hasIdentity:
		return tierIdentity
	default:
		return tierAnonymous
	}
}

// isSageXExternalIdentityRoute covers the two routes whose identity is established by identify_code
// in the controller/service chain rather than by a Gin identity middleware.
func isSageXExternalIdentityRoute(method, path string) bool {
	if method != "POST" {
		return false
	}
	switch path {
	case "/api/integrations/sagex/upload", "/api/integrations/sagex/bootstrap":
		return true
	default:
		return false
	}
}

// TestDumpLiveRouteGuardChains 把每条 live 路由"链上认得的守卫"导成 TSV，供跨层复算。
//
// 为什么要有它：菜单可见性和路由守卫是两份独立写下的授权（model/menu.go 的节点 Roles
// 决定侧栏给谁看，注册点上的守卫决定谁能真调到），两者会各改各的。要把"某角色看得见
// 菜单却调不动接口 / 菜单只给超管看却谁都能调"这类错配算出来，需要一份既是路由表、
// 又带授权档位的事实源——而 ROUTE_DUMP_FILE 那份只有 HANDLER 名，读不出档位。
//
// 导出的第三列是**档位**而不是原始函数名链：链上的符号名会随内联而变（见同目录
// route_policy_gate_test.go 里对 guardJWT 的说明），档位用的是同一份守卫名单，
// 所以门与导出件对同一条口的判断不会分叉。
//
// 第四列是链上命中的守卫名（逗号分隔，只列认得的那些）：给取证方核对档位是不是
// 由它以为的那个守卫撑起来的。
//
// 不挂进 CI：没设 ROUTE_GUARD_DUMP_FILE 时直接 Skip。
func TestDumpLiveRouteGuardChains(t *testing.T) {
	target := strings.TrimSpace(os.Getenv(routeGuardDumpFileEnv))
	if target == "" {
		t.Skipf("未设置 %s：本用例只在复算菜单/守卫错配时导出，不作为门禁", routeGuardDumpFileEnv)
	}
	chains := liveRouteGuardChains(t)

	rows := make([]string, 0, len(chains))
	for key, chain := range chains {
		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			t.Fatalf("路由键的形状不是 \"METHOD /path\"：%q", key)
		}
		// 档位判的是**有效**授权，不是"链上出现过哪个守卫名"——口径见 routeGuardTier。
		// 导出件与行级隔离的覆盖率门共用这一个函数：两处各算一遍会分叉。
		tier := routeGuardTier(parts[0], parts[1], chain)

		found := make([]string, 0, len(superAdminGuards)+len(identityGuards)+1)
		known := append(append([]string{}, identityGuards...), superAdminGuards...)
		known = append(known, guardSetupOnly)
		for _, g := range known {
			if chainHasGuard(chain, g) {
				found = append(found, g)
			}
		}
		rows = append(rows, parts[0]+"\t"+parts[1]+"\t"+tier+"\t"+strings.Join(found, ","))
	}
	sort.Strings(rows)

	f, err := os.Create(target)
	if err != nil {
		t.Fatalf("写守卫链导出件 %s 失败：%v", target, err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err := w.WriteString("METHOD\tPATH\tTIER\tGUARDS\n"); err != nil {
		t.Fatalf("写表头失败：%v", err)
	}
	for _, r := range rows {
		if _, err := w.WriteString(r + "\n"); err != nil {
			t.Fatalf("写行失败：%v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush 失败：%v", err)
	}

	// 自证：档位分类必须真的按**有效**规则开火。四条形控制分别从三档里点名，
	// 其中 GET /api/menus 是关键的一条：它的链上确实有 RoutePolicyGuard（挂在 auth 组），
	// 但它不在仅超管表里，所以每个角色都读得到菜单——若分档按守卫名而不是按命中表，
	// 这条会被误读成 super-admin，而"菜单谁都能读"正是全站侧栏能渲染的前提。
	if len(rows) == 0 {
		t.Fatalf("Setup() 跑出的路由表是空的：装配入口坏了，导出无意义")
	}
	tierOf := map[string]string{}
	for _, r := range rows {
		p := strings.Split(r, "\t")
		tierOf[p[0]+" "+p[1]] = p[2]
	}
	for key, want := range map[string]string{
		"GET /health":                            "anonymous",
		"GET /api/system/info":                   "anonymous",
		"GET /api/menus":                         "identity",
		"GET /api/llm/providers/policy":          "super-admin",
		"POST /api/customer-sessions":            "identity",
		"GET /api/feature-flags":                 "capability",
		"POST /api/integrations/sagex/upload":    "external-credential",
		"POST /api/integrations/sagex/bootstrap": "external-credential",
		"POST /api/mcp":                          "external-credential",
	} {
		if got := tierOf[key]; got != want {
			t.Fatalf("正控制失效：%s 档位应为 %q，实得 %q（分档口径或路由表变了）", key, want, got)
		}
	}
	// 分档计数按档各点名一次：capability 一档的存在性必须由读数证明，
	// 否则"生成表空了/匹配器坏了"这种全表失效也会让上面那条正控制照样绿
	// （正控制命中的是表里的一条，表空时它读回 identity，两头的红都出得来；
	// 但 SHARED/LEAK 那批复算会静默少一整档）。
	tierCount := map[string]int{}
	for _, r := range rows {
		tierCount[strings.Split(r, "\t")[2]]++
	}
	for _, tierName := range []string{"anonymous", "identity", "capability", "super-admin", "external-credential"} {
		if tierCount[tierName] == 0 {
			t.Fatalf("导出件里 %q 一档为 0 条：这一档的判据没开火（守卫装配或生成表出了问题）", tierName)
		}
	}
	t.Logf("档位分布：anonymous %d · identity %d · capability %d · super-admin %d",
		tierCount["anonymous"], tierCount["identity"], tierCount["capability"], tierCount["super-admin"])
	t.Logf("外部凭证档位 %d 条", tierCount["external-credential"])
	t.Logf("导出 %d 行守卫档位到 %s", len(rows), target)
}
