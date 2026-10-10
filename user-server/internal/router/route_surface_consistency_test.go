package router

// 契约锁：接口注解与 e2e 冒烟清单都必须逐字命中真实路由表，且注解必须挂在真正服务该路由的函数上。
//
// 立项依据有两层。
// 第一层是"声明面说谎"：/api/domainpool/* 那 9 条端点早已随命名重构下线，但 controller 里的接口
// 注解没跟着删，生成的 swagger 因此对外宣称了一批根本不存在的接口；同一批里 12 条活接口反而没注解。
// 第二层是更贵的一种说谎：路径存在、注解也在，但两者不属于同一个函数——
// 例如 GET /api/customer/:id 由 Customer360Controller.GetCustomerDetail 服务，
// 而注解写在另一个控制器的同名方法上。只看"路径在不在路由表里"的门对这种错位完全没有牙，
// 它会一路放行，直到有人照着文档去找实现才发现问题。
// 因此这里的归属判定用运行期事实：gin 1.12 的 RouteInfo.Handler 就是 nameOfFunction(terminal handler)，
// 由生产装配入口 Setup() 跑出来的那张表自带"这条路由由哪个函数服务"。
// 第三类是写法本身：gin 用 :id 表示参数段，OpenAPI 用 {id}；注解喂给 swagger 生成器时是原样进
// paths 的键的，写 :id 就会生成 "/api/customer/:id" 这种非法路径模板，工具链（校验器、代码生成器、
// 导入 Postman/Apifox 的人）都会在这一条上失败，所以这一层也在这里钉住。
//
// 第三个面是端到端冒烟清单：tests/e2e/routes_user.tsv 由静态正则从 Go 源码里拼路径，
// 拼不出带中间件的 Group 链，留下一堆 /:id、/my-purchases 这类半截路径——
// 探测脚本对半截路径只会拿到 NoRoute 的 404，而 404 在该脚本的判据里记作"存活"，
// 于是清单里的死行既不会被发现也不会被修。这里把它钉回路由表，并守住覆盖条数下界。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// routerDocToken 用拼接写法还原注解标签：本文件自己的源码里不能出现连写的完整标签，
// 否则 swagger 生成器会把守卫里的正则当成一条接口注解扫进文档。
const routerDocToken = "@" + "Router"

// minE2EInventoryCoverage 是清单必须覆盖到的 /api/ 路由条数下界（棘轮：只许上调，不许下调）。
// 下调它意味着有人从清单里删掉了真实存在的接口，而清单是端到端冒烟唯一的输入面。
const minE2EInventoryCoverage = 738

// annotationScanRoot 是接口注解的扫描根：模块根（本包目录的 ../..）。
// 只扫 controller/router/service 会漏掉 internal/geo/controller 这类同样进 swagger 的目录，
// 而 swagger 的输入面是整个依赖树——门的面必须不小于它在守的那个面。
var annotationScanRoot = filepath.Join("..", "..")

// skipAnnotationDirs 是不进扫描的目录名：docs 是 swagger 生成物（把生成物当声明面自证＝循环），
// 其余是构建产物与前端目录。
var skipAnnotationDirs = map[string]bool{"docs": true, "node_modules": true, "dist": true, ".git": true}

// liveRoutes 用生产装配入口跑出路由表。
//
// 前端静态面按目录是否存在条件注册（见 setupEmbedStaticRoutes），因此这里显式把 dist 目录
// 指到一个空临时目录：路由是否注册只看该目录 stat 得到与否，与服务是否真的加载 index.html 无关，
// 而清单里的 GET / 、/index.html 、/chat/embed 三行探测的正是这组路由。
// 不固定这个前提的话，同一份清单在有前端构建产物的机器上过、在没有的机器上红，
// 门就变成了"别人有没有跑过 npm build"的探针。
func liveRoutes(t *testing.T) (exact map[string]string, byShape map[string][]string) {
	t.Helper()
	t.Setenv("USER_WEB_DIST", t.TempDir())
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Quote{}, &model.QuoteLineItem{},
		&model.ApprovalRequest{}, &model.SalesEvent{}, &model.SystemConfigKV{})
	prev := dbutil.GetDB()
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(prev) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Setup(engine, database)

	exact = make(map[string]string, 2048)
	byShape = make(map[string][]string, 2048)
	for _, ri := range engine.Routes() {
		exact[ri.Method+" "+ri.Path] = ri.Handler
		shapeKey := ri.Method + " " + routeShape(ri.Path)
		byShape[shapeKey] = append(byShape[shapeKey], ri.Handler)
	}
	return exact, byShape
}

// paramSegRe 匹配 gin 的通配段（:id）与 swagger 的花括号段（{id}、{id:[0-9]+}）。
var paramSegRe = regexp.MustCompile(`:[A-Za-z_][A-Za-z0-9_]*|\{[A-Za-z_][A-Za-z0-9_]*(?::[^}]*)?\}`)

// routeShape 把所有通配段折成 :x。注解里的参数名是文档措辞、路由里的参数名是代码标识符，
// 两者可以合法地不一致（例如注解写 {id}、注册写 :customer_id），所以按形状比对归属。
func routeShape(p string) string {
	return paramSegRe.ReplaceAllString(p, ":x")
}

type routeAnnotation struct {
	pos       string // file:line，便于直接定位
	method    string
	path      string // 已折成形状
	raw       string // 注解里原样写的路径，用于检查参数写法本身
	key       string // METHOD + 空格 + path
	handlerOf string // 期望的服务方：(*Type).Func 或 Func
}

var annotationRe = regexp.MustCompile(regexp.QuoteMeta(routerDocToken) + `\s+(\S+)\s+\[(\w+)\]`)

// collectAnnotations 用 go/ast 把每条接口注解和它真正修饰的那个函数配对。
//
// 用正则扫源码行会在这两处骗人：接收者名不止一个字母（ctrl 而不是 c）、
// 注解块与函数之间还夹着别的声明。go/ast 的 FuncDecl.Doc 就是 swag 用来生成文档的同一份注释，
// 配对口径必须和生成器一致，否则门绿而文档红。
func collectAnnotations(t *testing.T) []routeAnnotation {
	t.Helper()
	var out []routeAnnotation
	fset := token.NewFileSet()
	walkErr := filepath.Walk(annotationScanRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipAnnotationDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return fmt.Errorf("解析 %s 失败：%w", path, perr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			owner := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				owner = "(*" + exprIdent(fn.Recv.List[0].Type) + ")." + fn.Name.Name
			}
			for _, cg := range fn.Doc.List {
				m := annotationRe.FindStringSubmatch(cg.Text)
				if m == nil {
					continue
				}
				p := routeShape(m[1])
				out = append(out, routeAnnotation{
					pos:       fmt.Sprintf("%s:%d", path, fset.Position(cg.Slash).Line),
					method:    strings.ToUpper(m[2]),
					path:      p,
					raw:       m[1],
					key:       strings.ToUpper(m[2]) + " " + p,
					handlerOf: owner,
				})
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("扫描接口注解失败：%v", walkErr)
	}
	return out
}

// exprIdent 取出接收者类型表达式里的类型名（*Foo 与 Foo 都归一到 Foo）。
func exprIdent(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.StarExpr:
		return exprIdent(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return fmt.Sprintf("%T", e)
}

// TestAnnotationsMatchLiveRoutes 断言每条接口注解既指向真实注册的路由，又挂在该路由的服务函数上。
func TestAnnotationsMatchLiveRoutes(t *testing.T) {
	_, byShape := liveRoutes(t)
	anns := collectAnnotations(t)
	if len(anns) == 0 {
		t.Fatal("一条接口注解都没扫到：扫描根或标签写法变了，这道门已经空转")
	}
	var ghosts, misplaced, badForm []string
	for _, a := range anns {
		// gin 的路径参数写法是 :id，OpenAPI 的写法是 {id}。
		// 注解是喂给 swagger 生成器的，它照原样写进 paths 的键，
		// 所以写 :id 会让生成的文档出现 "/api/customer/:id" 这种非法路径模板。
		if strings.Contains(a.raw, ":") {
			badForm = append(badForm, fmt.Sprintf("%s 写成 %s，参数段要用花括号形式", a.pos, a.raw))
		}
		handlers := byShape[a.key]
		if len(handlers) == 0 {
			ghosts = append(ghosts, fmt.Sprintf("%s 宣称 %s，路由表里没有", a.pos, a.key))
			continue
		}
		owned := false
		for _, h := range handlers {
			if handlerNames(h, a.handlerOf) {
				owned = true
				break
			}
		}
		if !owned {
			misplaced = append(misplaced, fmt.Sprintf("%s 宣称服务 %s，实际由 %s 服务",
				a.pos, a.key, strings.Join(handlers, "|")))
		}
	}
	// 先出声再判定：这条读数只说明扫到了多少条注解，命中与否由下面两个 Errorf 决定，
	// 放在判红之后会把" errored 的那一批"也带上"全部命中"的字样。
	t.Logf("扫到接口注解 %d 条，逐条核对路径存在性与服务方归属", len(anns))
	if len(badForm) > 0 {
		sort.Strings(badForm)
		t.Errorf("接口注解的路径参数用了 gin 的冒号写法，生成的 OpenAPI 会得到非法路径模板（%d/%d 条）：\n  %s",
			len(badForm), len(anns), strings.Join(badForm, "\n  "))
	}
	if len(ghosts) > 0 {
		sort.Strings(ghosts)
		t.Errorf("接口注解声明了路由表里不存在的端点（%d/%d 条）：\n  %s",
			len(ghosts), len(anns), strings.Join(ghosts, "\n  "))
	}
	if len(misplaced) > 0 {
		sort.Strings(misplaced)
		t.Errorf("接口注解挂错了函数——路由在、但不是这个函数在服务它（%d/%d 条）：\n  %s",
			len(misplaced), len(anns), strings.Join(misplaced, "\n  "))
	}
}

// handlerNames 判定 gin 报告的服务方名字是否就是注解所属函数。
// gin 用 runtime.FuncForPC 取名字，形如 hivemtk-user/internal/controller.(*AuthController).Login-fm；
// 裸函数没有 (*T) 与 -fm 后缀。
func handlerNames(ginName, owner string) bool {
	if strings.HasPrefix(owner, "(*") {
		return strings.Contains(ginName, owner)
	}
	return strings.Contains(ginName, "."+owner) && !strings.Contains(ginName, "(*")
}

// pageTemplateDirs 是"服务端自己生成出去的那张页面"这一面的位置。
// 为什么它是第四个面：接口注解与冒烟清单都只描述"后端对外承诺了什么"，
// 而活码/闲鱼卡片这类落地页是后端用 html 模板当场生成、由终端用户浏览器直接执行的——
// 模板里 fetch 的那个路径既不在注解里、也不在前端 src 里，只在这份 html 里。
// 实测这一面有过一次真事故：live_code.html 的点击上报写的是 /api/live-codes/{{.ID}}/click，
// 而注册的是 /api/livecode/:id/click（public 组、无鉴权），于是落地页每次点击都打在 404 上，
// 点击数静默归零，而前三个面全都绿。
// 这一面的边界要写明：只扫卡片模板目录，不覆盖 Go 源码里 fmt.Sprintf 出来的外链
// （邮件像素/短链那一族由 service 侧的常驻用例守着，见 email_open_tracker_test.go）。
var pageTemplateDirs = []string{"../reach/card/template"}

// goTemplateSegRe 匹配 html 模板里的 Go 模板占位段（{{.ID}}、{{ .ShortLink }}）。
var goTemplateSegRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

// pageMethodRe 在 fetch 之后的片段里找上报方法。
var pageMethodRe = regexp.MustCompile(`method:\s*'([A-Z]+)'`)

// pageLinkExempt 是"模板引用了路由表里没有的路径"的有界豁免表。
// 每条都写清它为什么还留着、等什么处置；这张表是棘轮：
// 新增一条坏引用必须显式在这里登记，而这里登记的条目一旦哪天被注册上了、
// 或者被删掉了，下面那条"豁免已失效"的判据就会开火，逼着登记它的人回来删掉这行。
var pageLinkExempt = map[string]string{
	"POST /api/xianyu/:x/click": "闲鱼卡片的三个计数口从未注册过（xianyu_card.go 的 PostRecordClick 是写完没接线的处理器），" +
		"注册无鉴权写口还是删上报属产品决策，先记在这里，不在本门里代拍",
	"POST /api/xianyu/:x/share": "同上，分享计数无注册口",
	"POST /api/xianyu/:x/view":  "同上，浏览计数无注册口",
}

// TestPageTemplateLinksMatchLiveRoutes 断言服务端生成的落地页里每一个 /api 引用都指向已注册的路由。
func TestPageTemplateLinksMatchLiveRoutes(t *testing.T) {
	_, byShape := liveRoutes(t)
	var files []string
	for _, dir := range pageTemplateDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.html"))
		if err != nil {
			t.Fatalf("枚举 %s 失败：%v", dir, err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Fatalf("一个落地页模板都没扫到（%s）：这一面已经空转", strings.Join(pageTemplateDirs, ", "))
	}
	seen := make(map[string]bool, 64)
	var ghosts, staleExempt []string
	matched := 0
	// \x60 是反引号：模板里的上报串既可能用单/双引号，也可能用 JS 模板串。
	// 前导斜杠必须在捕获组内：路由表里的键一律是 "/api/..." 的形状，
	// 把它留在组外会拼出 "POST api/livecode/:x/click" 这种少一个斜杠的键——
	// 于是已注册的口会被报成 404，豁免表也会整批对不上，两条判据同时指向错的东西。
	linkRe := regexp.MustCompile(`["'\x60](/api/[A-Za-z0-9_/{}.\-]+)`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读模板 %s 失败：%v", f, err)
		}
		text := string(data)
		rel := strings.TrimPrefix(f, "../")
		for _, m := range linkRe.FindAllStringSubmatchIndex(text, -1) {
			raw := text[m[2]:m[3]]
			// 上报方法写在紧跟其后的 fetch 选项里；模板里所有 fetch 的 method 都在同一个对象字面量里，
			// 因此只在 URL 之后的一小段窗口里找，避免把上一个 fetch 的 method 算到这一个头上。
			method := "GET"
			tail := text[m[1]:]
			if len(tail) > 260 {
				tail = tail[:260]
			}
			if mm := pageMethodRe.FindStringSubmatch(tail); mm != nil {
				method = mm[1]
			}
			key := method + " " + routeShape(goTemplateSegRe.ReplaceAllString(raw, ":x"))
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(byShape[key]) > 0 {
				matched++
				continue
			}
			if _, ok := pageLinkExempt[key]; ok {
				continue
			}
			ghosts = append(ghosts, fmt.Sprintf("%s 里页面执行的是 %s，路由表里没有", rel, key))
		}
	}
	// 反向棘轮：豁免表里不许留已经不必豁免的条目。两种失效形态都要开火：
	// 上报从模板里被删掉了（这条引用再也扫不到），或者口真的注册上了（路由表里查得到）。
	for key := range pageLinkExempt {
		if !seen[key] || len(byShape[key]) > 0 {
			staleExempt = append(staleExempt, key)
		}
	}
	// 空转自检分两支，各说各的话，免得读数指向错的因：
	// 支一：模板里一个 /api 引用都扫不到 ⇒ 这一面整块离开了门的面（正则坏了、模板搬家、上报改成别的宿主）；
	// 支二：扫到了引用却一条都没命中路由表 ⇒ 键的形状对不上。实测踩过支二那种坏法：
	// 把前导斜杠留在捕获组外，键就成了 "POST api/livecode/:x/click"，于是已注册的上报口
	// 被整批报成 404、豁免表整批失配——门一脸证据地判红，测的却是它自己拼键的那行代码。
	if len(seen) == 0 {
		t.Fatalf("落地页模板里一个 /api 引用都没扫到（%d 份模板）：这一面已经离开门的面了", len(files))
	}
	if matched == 0 {
		t.Fatalf("落地页模板里的 %d 个 /api 引用一条都没命中路由表（已登记豁免 %d 条）：键的形状对不上，比对已经空转",
			len(seen), len(pageLinkExempt))
	}
	t.Logf("扫到落地页模板 %d 份、其中不同的 /api 引用 %d 个（命中路由表 %d 个）；已登记豁免 %d 条",
		len(files), len(seen), matched, len(pageLinkExempt))
	if len(staleExempt) > 0 {
		sort.Strings(staleExempt)
		t.Errorf("豁免表里 %d 条引用已经不在模板里出现了（口注册上了或上报删掉了），把这条豁免一并删掉：\n  %s",
			len(staleExempt), strings.Join(staleExempt, "\n  "))
	}
	if len(ghosts) > 0 {
		sort.Strings(ghosts)
		t.Errorf("落地页模板里有 %d 个 /api 引用不指向任何已注册路由，终端用户点了只会拿到 404：\n  %s",
			len(ghosts), strings.Join(ghosts, "\n  "))
	}
}

// TestE2EInventoryMatchesLiveRoutes 断言冒烟清单每一行都命中路由表、无重复，
// 并守住清单对 /api/ 路由的覆盖条数下界。
func TestE2EInventoryMatchesLiveRoutes(t *testing.T) {
	keys, _ := liveRoutes(t)
	const inventory = "../../tests/e2e/routes_user.tsv"
	data, err := os.ReadFile(inventory)
	if err != nil {
		t.Fatalf("读不到端到端清单 %s：%v —— 清单是 probe_endpoints.sh 的唯一输入面，缺失即门空转", inventory, err)
	}
	seen := make(map[string]bool, 1024)
	var stale, dup []string
	covered, apiLive, rows := 0, 0, 0
	for k := range keys {
		if strings.Contains(k, " /api/") {
			apiLive++
		}
	}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		rows++
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("清单第 %d 行不是三列（标签/方法/路径）：%q", i+1, line)
		}
		key := cols[1] + " " + cols[2]
		if seen[key] {
			dup = append(dup, key)
			continue
		}
		seen[key] = true
		if _, ok := keys[key]; !ok {
			stale = append(stale, fmt.Sprintf("%s（清单第 %d 行）", key, i+1))
			continue
		}
		if strings.HasPrefix(cols[2], "/api/") {
			covered++
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("清单里 %d 行不指向任何已注册路由，探测它们只会拿到 NoRoute：\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}
	if len(dup) > 0 {
		t.Errorf("清单有重复行：%s", strings.Join(dup, ", "))
	}
	if covered < minE2EInventoryCoverage {
		t.Errorf("清单覆盖的 /api/ 路由只剩 %d 条，低于下界 %d：要么删了真实接口的探测行，要么路由表口径变了",
			covered, minE2EInventoryCoverage)
	}
	// 覆盖缺口只出声不判红：路由表面比清单大得多，补齐属于扩面决策；
	// 把读数打出来是为了让缺口无法被悄悄扩大。
	t.Logf("清单 %d 行全部命中路由表；/api/ 路由 %d 条，覆盖 %d 条，未覆盖 %d 条",
		rows, apiLive, covered, apiLive-covered)
}
