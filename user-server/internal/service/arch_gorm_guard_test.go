package service

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// arch_gorm_guard_test.go —— E-4 新代码守卫（2026-10-24，规划 §4.4）。
//
// 规划口径：静态扫描禁止新增 `*gorm.DB` 字段或形参进入 service 层；
// 按域下沉是长期债，本轮不做，但基线写入门禁只许降。
//
// ============================================================================
// 2026-10-24 第二轮：度量改为 AST（只数代码），并把两类计数分开
//
// 旧版用 `grep -rnE '\*gorm\.DB'` 数**文本行**，两个后果都是反的激励：
//   - 注释也算数。实测 181 处里有 **21 处是注释**（多数还是"本 service 不持有
//     *gorm.DB"这类**已经修好**的说明）⇒ 债务在降、数字在涨；
//   - `func(tx *gorm.DB)` 事务回调与"握句柄"混在一个数里，而五层架构 §2 的依赖表
//     明确写着 Service 允许 `gorm（仅事务）`。
//
// 现在分两档，各自"只许降"：
//   - 句柄档（handle）：字段/形参里出现的 `*gorm.DB` —— §3.2 明令阻断的东西；
//   - 事务档（txCallback）：`func(tx *gorm.DB) error` 这种事务回调 —— 架构允许，
//     但**只许出现在 `X.Transaction(...)` 的实参位置**
//     （见 TestTxCallbacksAreInTransactionPosition；该测试 2026-11-07 AV-03 才
//     真正落地——此前注释就引用了它，但源码里只有度量夹具，属于"把不存在的
//     测试算现覆盖"，本轮订正）。
//
// 两档都用 go/ast 解析（注释天然不在语法树里），不再 grep 文本：
// 数出来的东西必须等于"代码里真的有一个 *gorm.DB 类型"。
// ============================================================================
//
// ============================================================================
// 2026-11-07 AV-03 统一全域 Service 守卫范围（第三轮）
//
// 1) 扫描根从 cwd（internal/service）改为：从模块根自动发现 internal 下所有
//    「目录分量为 service」的目录树（含子目录，排 *_test.go 与 testdata/，
//    不读 build tag——parser.ParseFile 对带 tag 的文件照常解析）。发现规则与
//    scripts/check-architecture.sh §2.0 共用同一口径，两侧各自实现、互为对照；
//    解析失败 Fatal（go test 非零退出）。旧版 gormGuardScope 注释声称
//    "含 internal/geo/service 等子包"是错的：geo/service 是 internal/service 的
//    **兄弟**目录，cwd 相对扫描根本到不了。扩面后实测 10 域 / 632 生产文件。
// 2) gorm.DB 判定改为**别名感知**：按 import 路径（gorm.io/gorm）解析本文件
//    本地名，`orm "gorm.io/gorm"` 别名、点导入都能抓（旧版只认字面 `gorm.DB`
//    拼写，别名一换就漏）。
// 3) 事务档位置收紧：只有 `.Transaction(...)` / `.Connection(...)` **实参位置**
//    的函数字面量才算事务档（这是 gorm 把句柄递进回调的两个接缝；仓储自带的
//    Transaction/Connection 同形接缝一体适用，"合法事务例外不一律禁掉"）。
//    回调之外的 *gorm.DB——局部 var、非事务位置的函数字面量、返回值——一律
//    句柄档。旧度量把函数体内**任何** *gorm.DB 都记事务档，会让回调外的句柄
//    冒充事务例外（实测样本：ops/service/dashboard_screen.go 的 scope 闭包）。
// 4) 新增 **GetDB 档**：service 生产文件里任何 `.GetDB()` 调用，含 `x().GetDB()`
//    链式形态——该形态 shell 宽口径 grep（`[A-Za-z0-9_]+\.GetDB`）抓不到，
//    实测漏网 2 处：internal/service/auth.go:1020、
//    internal/service/feedback_loop/sop_optimization_gate.go:80。
// 5) 棘轮基线从"句柄/事务两个总数"改为**逐文件三元组快照**（gormGuardRatchet，
//    键相对仓库根，与 scripts/arch-l4-getdb.baseline 同一键口径）：只许降、
//    不许涨，清零即删行；新增文件 / 超出 / 收窄 / 条目失效一律红。
//    根域契约保持原样：句柄 0、事务回调 1（trace_learning 的 Connection 接缝，
//    沿原基线，见 TestArchServiceNoNewGormDB 里的根域断言）。
//    扩面发现的兄弟域存量**没有**被算成"自然全绿"：它们逐文件登记在快照里、
//    逐条列在 AV-04c 存量清单（与 check-architecture.sh 扩面后读数同源）。
// 6) 已知 AST 边界（诚实声明，不宣称全知）：`type H = *gorm.DB` 类型别名转手、
//    经变量中转的 GetDB 方法值（`f := db.GetDB; f()`）、值类型 `gorm.DB`
//    （非指针）不计数；这三类需要类型信息，交给 code review 与 AV-04c。
// ============================================================================

const gormPath = "gorm.io/gorm"

// 根域契约（沿 2026-10-24 原基线，只许降）。
const (
	gormGuardRootMaxHandles     = 0
	gormGuardRootMaxTxCallbacks = 1
)

// gormGuardStat 三档计数：句柄 / 事务回调 / GetDB 调用。
type gormGuardStat struct {
	handles int
	tx      int
	getdb   int
}

func (s gormGuardStat) isZero() bool {
	return s.handles == 0 && s.tx == 0 && s.getdb == 0
}

// gormGuardFinding 单条违规明细（规则/行/原因），进了棘轮快照的存量也照常产出。
type gormGuardFinding struct {
	rule  string
	line  int
	cause string
}

// ----------------------------------------------------------------------------
// 发现：与 scripts/check-architecture.sh §2.0 同一规则（目录分量为 service）
// ----------------------------------------------------------------------------

// gormGuardModuleRoot 定位 user-server 模块根。基于 runtime.Caller 而不是 cwd：
// `go test ./...` 在任何目录跑都不漏检（不因当前 cwd 漏掉违规）。
func gormGuardModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源文件路径（runtime.Caller 失败）")
	}
	// thisFile = <模块根>/internal/service/arch_gorm_guard_test.go
	modRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if fi, err := os.Stat(filepath.Join(modRoot, "go.mod")); err != nil || fi.IsDir() {
		t.Fatalf("模块根定位失败（%s 下没有 go.mod）：runtime.Caller = %s", modRoot, thisFile)
	}
	return modRoot
}

type gormGuardIndex struct {
	dirs     []string          // 域目录（仓库根相对，如 user-server/internal/geo/service）
	files    []string          // 生产 .go（仓库根相对，排序去重）
	domainOf map[string]string // 文件 → 所属域目录
	countOf  map[string]int    // 域目录 → 生产文件数
}

// gormGuardIndexProduction 从模块根发现全部 service 域与其生产文件。
// 规则：internal 下目录分量为 `service` 的目录树（含子目录）；排 *_test.go；
// 剔除 testdata/（测试夹具）；嵌套 service 目录由外层域认领（不重复计数）；
// 不读 build tag（parser 层面照扫）。
func gormGuardIndexProduction(t *testing.T) *gormGuardIndex {
	t.Helper()
	modRoot := gormGuardModuleRoot(t)
	internalRoot := filepath.Join(modRoot, "internal")

	var dirsAbs []string
	if err := filepath.Walk(internalRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if info.Name() == "testdata" {
			return filepath.SkipDir
		}
		if info.Name() == "service" {
			dirsAbs = append(dirsAbs, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("发现 service 目录失败: %v", err)
	}
	sort.Strings(dirsAbs)

	idx := &gormGuardIndex{
		domainOf: map[string]string{},
		countOf:  map[string]int{},
	}
	rel := func(abs string) string {
		r, err := filepath.Rel(modRoot, abs)
		if err != nil {
			t.Fatalf("相对化 %s 失败: %v", abs, err)
		}
		// 键带模块目录名前缀（user-server/...），与 scripts/arch-l4-getdb.baseline
		// 的仓库根相对键口径一致，两侧读数可直接对照。
		return filepath.ToSlash(filepath.Base(modRoot) + "/" + r)
	}
	claimed := map[string]bool{}
	for _, d := range dirsAbs {
		if err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			name := info.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			if claimed[path] { // 嵌套 service 目录：外层域已认领
				return nil
			}
			claimed[path] = true
			idx.files = append(idx.files, rel(path))
			idx.domainOf[rel(path)] = rel(d)
			idx.countOf[rel(d)]++
			return nil
		}); err != nil {
			t.Fatalf("枚举 %s 失败: %v", d, err)
		}
	}
	idx.dirs = make([]string, 0, len(dirsAbs))
	for _, d := range dirsAbs {
		idx.dirs = append(idx.dirs, rel(d))
	}
	sort.Strings(idx.files)
	return idx
}

// ----------------------------------------------------------------------------
// 扫描引擎：别名感知 + 事务位置感知
// ----------------------------------------------------------------------------

// gormGuardScanTree 扫描一棵目录树（ teeth 测试的夹具用），返回三档合计。
func gormGuardScanTree(root string) (gormGuardStat, []gormGuardFinding, int, error) {
	var stat gormGuardStat
	var findings []gormGuardFinding
	files := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		st, fd, err := gormGuardScanFile(path)
		if err != nil {
			return err
		}
		stat.handles += st.handles
		stat.tx += st.tx
		stat.getdb += st.getdb
		findings = append(findings, fd...)
		return nil
	})
	return stat, findings, files, err
}

// gormGuardScanFile 扫描单个生产文件。
func gormGuardScanFile(path string) (gormGuardStat, []gormGuardFinding, error) {
	var stat gormGuardStat
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return stat, nil, err
	}
	var findings []gormGuardFinding
	pos := func(p token.Pos) int { return fset.Position(p).Line }
	add := func(rule string, p token.Pos, cause string) {
		findings = append(findings, gormGuardFinding{rule: rule, line: pos(p), cause: cause})
	}

	// 别名感知：本文件内绑定到 gorm.io/gorm 的本地名集合（可能多于一个：
	// 同一路径可以被以不同名字导入多次）；点导入时裸 `DB` 标识符也算。
	gormNames := map[string]bool{}
	dotGorm := false
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != gormPath {
			continue
		}
		switch {
		case imp.Name == nil:
			gormNames["gorm"] = true
		case imp.Name.Name == ".":
			dotGorm = true
		default:
			gormNames[imp.Name.Name] = true
		}
	}
	isGormDBType := func(x ast.Expr) bool {
		switch v := x.(type) {
		case *ast.SelectorExpr:
			id, ok := v.X.(*ast.Ident)
			return ok && v.Sel.Name == "DB" && gormNames[id.Name]
		case *ast.Ident:
			return dotGorm && v.Name == "DB"
		}
		return false
	}

	// 事务例外位置（预扫）：.Transaction(...) / .Connection(...) 实参位置的
	// 函数字面量。这是 gorm 把句柄递进回调的接缝；仓储自带的同形接缝一体适用。
	sanctioned := map[*ast.FuncLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		se, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if se.Sel.Name != "Transaction" && se.Sel.Name != "Connection" {
			return true
		}
		for _, arg := range ce.Args {
			if fl, ok := arg.(*ast.FuncLit); ok {
				sanctioned[fl] = true
			}
		}
		return true
	})

	// 数一个类型表达式里的 *gorm.DB（递归数组/映射/可变参/回调函数类型）。
	// 回调函数类型形态（func(tx *gorm.DB) error 作为形参/返回值）的 gorm 形参
	// 按事务档计——与旧度量一致：它是接缝形状，不是握在手里的句柄。
	var countType func(e ast.Expr, inSanctioned bool) (h, tx int)
	countType = func(e ast.Expr, inSanctioned bool) (h, tx int) {
		if e == nil {
			return 0, 0
		}
		ast.Inspect(e, func(m ast.Node) bool {
			switch v := m.(type) {
			case *ast.FuncType:
				if v.Params != nil {
					for _, p := range v.Params.List {
						hh, tt := countType(p.Type, true)
						h += hh
						tx += tt
					}
				}
				if v.Results != nil {
					for _, r := range v.Results.List {
						hh, tt := countType(r.Type, inSanctioned)
						h += hh
						tx += tt
					}
				}
				return false
			case *ast.StarExpr:
				if isGormDBType(v.X) {
					if inSanctioned {
						tx++
					} else {
						h++
					}
				}
			}
			return true
		})
		return h, tx
	}

	var walk func(n ast.Node, inSanctioned bool)
	walk = func(n ast.Node, inSanctioned bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch v := m.(type) {
			case *ast.FuncDecl:
				if v.Type != nil {
					if v.Type.Params != nil {
						for _, p := range v.Type.Params.List {
							if p.Type == nil {
								continue
							}
							h, tx := countType(p.Type, false)
							stat.handles += h
							stat.tx += tx
							if h > 0 {
								add("gorm_handle_param", p.Pos(), "声明形参含 *gorm.DB（应改 Repository 注入）")
							}
							if tx > 0 {
								add("gorm_tx_callback_shape", p.Pos(), "回调类型形参（接缝同形，计入事务档）")
							}
						}
					}
					if v.Type.Results != nil {
						for _, r := range v.Type.Results.List {
							if r.Type == nil {
								continue
							}
							h, _ := countType(r.Type, false)
							stat.handles += h
							if h > 0 {
								add("gorm_handle_return", r.Pos(), "声明返回 *gorm.DB（把句柄递出去）")
							}
						}
					}
				}
				if v.Body != nil {
					walk(v.Body, false)
				}
				return false
			case *ast.FuncLit:
				here := sanctioned[v]
				if v.Type.Params != nil {
					for _, p := range v.Type.Params.List {
						if p.Type == nil {
							continue
						}
						h, tx := countType(p.Type, here)
						if here {
							stat.tx += tx
							if tx > 0 {
								add("gorm_tx_callback", p.Pos(), "Transaction/Connection 实参位置的回调形参（事务档）")
							}
						} else {
							stat.handles += h + tx
							if h+tx > 0 {
								add("gorm_handle_funclit_param", p.Pos(), "非事务位置的函数字面量 gorm 形参（回调外句柄）")
							}
						}
					}
				}
				if v.Type.Results != nil {
					for _, r := range v.Type.Results.List {
						if r.Type == nil {
							continue
						}
						h, _ := countType(r.Type, false)
						stat.handles += h
						if h > 0 {
							add("gorm_handle_return", r.Pos(), "函数字面量返回 *gorm.DB")
						}
					}
				}
				if v.Body != nil {
					walk(v.Body, here)
				}
				return false
			case *ast.StructType:
				if v.Fields != nil {
					for _, fld := range v.Fields.List {
						if fld.Type == nil {
							continue
						}
						h, _ := countType(fld.Type, false)
						stat.handles += h
						if h > 0 {
							add("gorm_handle_field", fld.Pos(), "struct 字段 *gorm.DB（应改 Repository 注入）")
						}
					}
				}
				return false
			case *ast.InterfaceType:
				if v.Methods != nil {
					for _, fld := range v.Methods.List {
						ft, ok := fld.Type.(*ast.FuncType)
						if !ok {
							continue
						}
						if ft.Params != nil {
							for _, p := range ft.Params.List {
								if p.Type == nil {
									continue
								}
								h, tx := countType(p.Type, false)
								stat.handles += h
								stat.tx += tx
								if h > 0 {
									add("gorm_handle_param", p.Pos(), "interface 方法签名含 *gorm.DB")
								}
								if tx > 0 {
									add("gorm_tx_callback_shape", p.Pos(), "interface 回调类型形参（接缝同形，计入事务档）")
								}
							}
						}
						if ft.Results != nil {
							for _, r := range ft.Results.List {
								if r.Type == nil {
									continue
								}
								h, _ := countType(r.Type, false)
								stat.handles += h
								if h > 0 {
									add("gorm_handle_return", r.Pos(), "interface 方法返回 *gorm.DB")
								}
							}
						}
					}
				}
				return false
			case *ast.CallExpr:
				if se, ok := v.Fun.(*ast.SelectorExpr); ok && se.Sel.Name == "GetDB" {
					stat.getdb++
					add("getdb_call", v.Pos(), "service 内调用 GetDB() 取句柄（含 x().GetDB() 链式）")
				}
				return true
			case *ast.ValueSpec:
				h, _ := countType(v.Type, inSanctioned)
				stat.handles += h
				if h > 0 {
					add("gorm_handle_var", v.Pos(), "var 声明 *gorm.DB 句柄（旧度量会把它冒充事务例外）")
				}
				return true
			default:
				return true
			}
		})
	}
	walk(f, false)
	return stat, findings, nil
}

// ----------------------------------------------------------------------------
// 棘轮快照：AV-03 扩面时的存量登记（只许降）
// ----------------------------------------------------------------------------

// gormGuardRatchet —— AV-03 扩面时的存量快照（2026-11-07 实测；**含并行在途
// 工作区状态**：geo/knowledge/ops 等域当时有另一会话未提交改动，读数按当时树取）。
//
// 语义与 scripts/arch-l4-getdb.baseline 一致：登记只是承认存量，不是认可写法。
// 键相对仓库根（与该 baseline 同一键口径），值 = {句柄档, 事务档, GetDB 档}。
// 只许降：一域清零就删该行；新增文件 / 同文件涨数 / 收窄（该改快照不改）/ 条目
// 失效（文件没了或已清零）一律红。逐条明细（规则/行号/原因）见 AV-04c 存量清单，
// 与 check-architecture.sh 扩面后的全域读数同源。
var gormGuardRatchet = map[string]gormGuardStat{
	// ---- aiagent/knowledge 域（17 句柄 / 1 事务 / 6 GetDB）----
	"user-server/internal/aiagent/knowledge/service/kb_crawler.go":           {handles: 2},
	"user-server/internal/aiagent/knowledge/service/kb_qa_split.go":          {handles: 2},
	"user-server/internal/aiagent/knowledge/service/knowledge.go":            {handles: 3, getdb: 1},
	"user-server/internal/aiagent/knowledge/service/knowledge_base.go":       {handles: 2, tx: 1, getdb: 2}, // tx=1：全域唯一真 Transaction 回调（DeleteDocument）
	"user-server/internal/aiagent/knowledge/service/knowledge_merchant.go":   {handles: 3, getdb: 1},        // 含 GetDB() *gorm.DB 方法声明（返回句柄）
	"user-server/internal/aiagent/knowledge/service/knowledge_statistics.go": {handles: 2, getdb: 1},
	"user-server/internal/aiagent/knowledge/service/rag_searcher.go":         {handles: 3, getdb: 1},
	// ---- browser_automation 域（0 句柄 / 0 GetDB）----
	// AV-04c 批次3（2026-11-07）：feedback.go getdb 1 清零除名 —— notifyFailure 的
	// system_users Raw 邮箱查询下沉 SystemUserRepository.GetEmailByID（显式
	// SystemScope 保全局解析语义），FeedbackService 构造期直持仓储（无装配点改动）。
	// browser_automation 域整域出快照，守卫对该域回归为「新增即红」（无豁免行）。
	// ---- content 域（9 句柄）----
	// AV-04c 刀 1（2026-11-07）：ai_content/batch_operation/marketing_flow 清零除名
	// （deps 工厂 NewXxxDeps(db) 收口到装配处直构，service 层不再持 *gorm.DB 形参）。
	// AV-04c 刀 2（2026-11-07）：material/template_market 同法清零除名（OBS adapter 改收仓储）。
	// AV-04c 刀 3（2026-11-07）：script_template 清零除名。
	// AV-04c 刀 4（2026-11-07）：script_template_sync 清零除名 —— content 域整域出快照，
	// 守卫对该域回归为「新增即红」（无豁免行）。
	// ---- email 域（曾登记 1 句柄 / 1 GetDB）----
	// AV-04c 批次2（2026-11-07）：email_list.go 构造器 *gorm.DB 形参改 ClueRepository
	// 注入（装配点传 NewClueRepositoryWithDB(原句柄)，连接不变）；email_smtp.go 的
	// GetRandEmailSmtp 记账改直持 EmailListRepository（同仓储同查询，不再经
	// NewEmailListService(dbpkg.GetDB()) 拼服务）。两文件清零除名 —— email 域整域出
	// 快照，守卫对该域回归为「新增即红」（无豁免行）。
	// ---- geo 域（0 句柄 / 0 GetDB）----
	// AV-04b：autopilot getdb 5→2（3 处 KeywordMiningService 构造去 pkgdb.GetDB）；
	// keyword_mining.go 句柄已清零（持久化下沉 GeoKeywordRepository），按
	// 「清零即删行」纪律除名，守卫对该文件不再有豁免行。
	// AV-04c 批次3（2026-11-07）：geo 域整域出快照 —— job_manager.go 句柄 2 清零
	// （db 字段+SetJobManagerDB 形参改 GeoJobLockRepository 注入，advisory lock 下沉
	// repository/job_lock.go，装配点 internal/pkg/cron）；keyword_pipeline.go 句柄 1
	// + getdb 1 清零（种子回填/待 SEO 词拉取下沉 GeoKeywordRepository，种子/思维链/
	// 补位任务仓储改无参构造）；search_probe.go 句柄 1 + getdb 2 清零（llm_providers
	// 名单下沉 mainrepo.LLMProviderRepository.ListEnabled、品牌投影下沉
	// GeoConfigRepository.GetBrandProfile，NewEngineProbesFromDB 改收仓储，装配点
	// router/geo_routes.go）；autopilot.go getdb 2 清零（pusher/push 仓储改无参构造）。
	// 四文件清零除名，守卫对 geo 域回归为「新增即红」（无豁免行）。
	// ---- ops 域（6 句柄 / 3 GetDB）----
	// AV-04c 批次4（2026-11-07）：churn_prediction 句柄 2 + getdb 1 清零除名
	// （db 字段与 WithDB 形参改四仓储注入 NewChurnPredictionServiceWithRepos，
	// customer_events 两段聚合 SELECT 下沉 ChurnPredictionRepository.Aggregate-
	// CustomerBehavior / ListRecentPurchaseEventPayloads，SQL 逐字保留）；ltc_rates
	// 句柄 1 清零除名（WithDB 形参改 WithRepo 仓储注入）；web_vital getdb 1 清零
	// 除名（直写下沉 WebVitalRepository.Create，repository/web_vital.go）。
	"user-server/internal/ops/service/custom_report.go":    {handles: 2, getdb: 1},
	"user-server/internal/ops/service/dashboard_screen.go": {handles: 2, getdb: 2}, // h=2：scope 闭包（回调外句柄，旧度量会冒充事务例外）
	"user-server/internal/ops/service/report_filter.go":    {handles: 2},
	// ---- 根域 internal/service（0 句柄 / 1 事务 / 34 GetDB）----
	// AV-04c 批次4（2026-11-07）：auth.go getdb 1 清零除名（开户赠送改
	// NewStarterKitRepositoryDefault，repository/starter_kit.go）；feedback_loop/
	// sop_optimization_gate.go getdb 1 清零除名（改 FeedbackLoopRepository.
	// TraceLearningRepo() 共享连接，FeedbackLoopRepository.GetDB 方法随之删除）。
	"user-server/internal/service/cards/tiktok_card.go":      {getdb: 1},
	"user-server/internal/service/customer_session.go":       {getdb: 3},
	"user-server/internal/service/customer_session_scope.go": {getdb: 2},
	"user-server/internal/service/data_export.go":            {getdb: 1},
	"user-server/internal/service/faq.go":                    {getdb: 1},
	"user-server/internal/service/feedback_learning.go":      {getdb: 2},
	"user-server/internal/service/feedback_learning_cron.go": {getdb: 2},
	"user-server/internal/service/feedback_loop_cron.go":     {getdb: 1},
	"user-server/internal/service/feedback_loop_init.go":     {getdb: 2},
	"user-server/internal/service/inbox.go":                  {getdb: 1},
	"user-server/internal/service/inbox_ingress.go":          {getdb: 1},
	"user-server/internal/service/inbox_scope.go":            {getdb: 1},
	"user-server/internal/service/knowledge_base.go":         {getdb: 1},
	"user-server/internal/service/objection_handler.go":      {getdb: 1},
	"user-server/internal/service/openapi.go":                {getdb: 2},
	"user-server/internal/service/rag_product.go":            {getdb: 1},
	"user-server/internal/service/sales_persona.go":          {getdb: 2},
	"user-server/internal/service/sop/sop_template.go":       {getdb: 1},
	"user-server/internal/service/tenant.go":                 {getdb: 1},
	"user-server/internal/service/trace_learning/service.go": {tx: 1, getdb: 3}, // tx=1：原基线的 Connection 接缝（advisory lock），沿原基线
	"user-server/internal/service/webhook.go":                {getdb: 2},
	"user-server/internal/service/wecom.go":                  {getdb: 1},
}

// ----------------------------------------------------------------------------
// 测试
// ----------------------------------------------------------------------------

// TestArchServiceNoNewGormDB 全域 service 守卫（AV-03 扩面后同入口主门）：
// 发现金丝雀 + 解析失败门 + 逐文件棘轮 + 根域契约。
func TestArchServiceNoNewGormDB(t *testing.T) {
	idx := gormGuardIndexProduction(t)

	// —— 发现金丝雀：目录集必须覆盖根域且不止根域。发现逻辑一旦回归
	//（比如退回 cwd 相对扫描、或目录分量匹配写错），在这里红，而不是
	// 静默缩小覆盖面出假绿。这是"最小合理性断言"，不是目录白名单：
	// 没有任何域因为它被排除，新域出现即自动纳入。
	rootDomain := "user-server/internal/service"
	rootSeen := false
	for _, d := range idx.dirs {
		if d == rootDomain {
			rootSeen = true
		}
	}
	if len(idx.dirs) < 2 || !rootSeen {
		t.Fatalf("service 域发现异常（%d 域，根域在列=%v）：发现逻辑回归，修 gormGuardIndexProduction，不要动棘轮",
			len(idx.dirs), rootSeen)
	}
	t.Logf("全域 service 目录集：%d 域 / %d 生产文件（排 *_test.go 与 testdata）", len(idx.dirs), len(idx.files))
	for _, d := range idx.dirs {
		t.Logf("  域 %-55s %d 个生产文件", d, idx.countOf[d])
	}

	// —— 扫描 + 解析失败门。
	measured := map[string]gormGuardStat{}
	findingsByFile := map[string][]gormGuardFinding{}
	var parseFailures []string
	for _, f := range idx.files {
		st, fd, err := gormGuardScanFile(filepath.Join(gormGuardModuleRoot(t), filepath.FromSlash(strings.TrimPrefix(f, filepath.Base(gormGuardModuleRoot(t))+"/"))))
		if err != nil {
			parseFailures = append(parseFailures, f+": "+err.Error())
			continue
		}
		measured[f] = st
		findingsByFile[f] = fd
	}
	if len(parseFailures) > 0 {
		sort.Strings(parseFailures)
		t.Fatalf("service 生产文件解析失败 %d 个（解析不动=守卫不可信，先修语法）：\n  %s",
			len(parseFailures), strings.Join(parseFailures, "\n  "))
	}
	t.Logf("扫描完成 %d 个文件；快照登记 %d 个；三档合计 句柄 %d / 事务 %d / GetDB %d",
		len(measured), len(gormGuardRatchet), totalHandles(measured), totalTx(measured), totalGetDB(measured))

	// —— 逐文件棘轮（双向）。
	var violations []string
	for _, f := range idx.files { // 按 idx.files 迭代保证输出顺序稳定
		st := measured[f]
		want, registered := gormGuardRatchet[f]
		if !registered {
			if !st.isZero() {
				violations = append(violations, fmt.Sprintf("新增: %s 有 句柄 %d / 事务 %d / GetDB %d，快照里没有这一行（新违规必须修掉，不许登记）", f, st.handles, st.tx, st.getdb))
			}
			continue
		}
		if st.handles > want.handles || st.tx > want.tx || st.getdb > want.getdb {
			violations = append(violations, fmt.Sprintf("超出: %s 现值 句柄 %d / 事务 %d / GetDB %d > 快照 %d/%d/%d（棘轮只许降）",
				f, st.handles, st.tx, st.getdb, want.handles, want.tx, want.getdb))
		} else if st.handles < want.handles || st.tx < want.tx || st.getdb < want.getdb {
			violations = append(violations, fmt.Sprintf("收窄: %s 现值 句柄 %d / 事务 %d / GetDB %d < 快照 %d/%d/%d（好事，请把 gormGuardRatchet 该行下调到实际值）",
				f, st.handles, st.tx, st.getdb, want.handles, want.tx, want.getdb))
		}
	}
	for f, want := range gormGuardRatchet {
		if _, ok := measured[f]; !ok {
			violations = append(violations, fmt.Sprintf("条目失效（文件已不存在，请删行）: %s", f))
			continue
		}
		if measured[f].isZero() && !want.isZero() {
			violations = append(violations, fmt.Sprintf("条目失效（已清零，请删行）: %s", f))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("service 层 gorm 守卫棘轮被触发（%d 条）：\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}

	// —— 根域契约（沿 2026-10-24 原基线，显式断言防"悄悄抬高"）。
	rootHandles, rootTx := 0, 0
	for f, st := range measured {
		if strings.HasPrefix(f, rootDomain+"/") {
			rootHandles += st.handles
			rootTx += st.tx
		}
	}
	if rootHandles != gormGuardRootMaxHandles {
		t.Fatalf("根域 service 句柄档 %d != 基线 %d：按五层架构 §3.2，Service 直访 db 被阻断，新增应走 Repository 注入",
			rootHandles, gormGuardRootMaxHandles)
	}
	if rootHandles < gormGuardRootMaxHandles {
		t.Fatalf("根域句柄档收窄（现 %d < 基线 %d）：这是好事，请把 gormGuardRootMaxHandles 下调到实际值",
			rootHandles, gormGuardRootMaxHandles)
	}
	if rootTx != gormGuardRootMaxTxCallbacks {
		t.Fatalf("根域事务回调 %d != 基线 %d：事务回调是架构允许的例外，例外只许变小；新代码请优先用 Repository 的 WithTx 接缝",
			rootTx, gormGuardRootMaxTxCallbacks)
	}
	if rootTx < gormGuardRootMaxTxCallbacks {
		t.Fatalf("根域事务回调收窄（现 %d < 基线 %d）：请把 gormGuardRootMaxTxCallbacks 下调到实际值",
			rootTx, gormGuardRootMaxTxCallbacks)
	}
}

func totalHandles(m map[string]gormGuardStat) int {
	n := 0
	for _, s := range m {
		n += s.handles
	}
	return n
}
func totalTx(m map[string]gormGuardStat) int {
	n := 0
	for _, s := range m {
		n += s.tx
	}
	return n
}
func totalGetDB(m map[string]gormGuardStat) int {
	n := 0
	for _, s := range m {
		n += s.getdb
	}
	return n
}

// TestGormGuardMetricHasTeeth 度量本身的自检：三档各形态必须落对档。
//
// 立项理由：这一轮改的就是"度量把注释算成代码"——如果自检缺位，下一次有人再把它改回
// grep 文本、或把事务回调并回句柄档，所有读数都会静默失真，而门照样绿。
// AV-03 起夹具扩到：别名 import、点导入、同名非 GORM 类型、局部句柄、
// 非事务位置函数字面量、返回值、GetDB 链式、注释/字符串反例。
func TestGormGuardMetricHasTeeth(t *testing.T) {
	dir := t.TempDir()
	src := `package service

import (
	"gorm.io/gorm"
	orm "gorm.io/gorm"
	other "example.com/nonorm"
	pkgdb "hivemtk-user/internal/pkg/db"
)

// 注释里的 *gorm.DB / orm.DB / pkgdb.GetDB() 不算数 —— 本行是度量反例。
const note = "字符串里的 *gorm.DB 与 GetDB() 也不算数"

// DB 本包同名非 GORM 类型（反例：不许因名字叫 DB 而计数）。
type DB struct{ Name string }

type Svc struct {
	db     *gorm.DB  // 句柄档：字段（canonical 拼写）
	odb    *orm.DB   // 句柄档：字段（别名 import —— 别名感知）
	legacy *DB       // 反例：本包同名 DB，不许计入
	ndb    *other.DB // 反例：非 gorm 包的同名类型，不许计入
}

func NewSvc(db *gorm.DB) *Svc { return &Svc{db: db} } // 句柄档：形参

func (s *Svc) Txn() error {
	return s.db.Transaction(func(tx *gorm.DB) error { // 事务档：Transaction 实参位置
		return nil
	})
}

func (s *Svc) Lock() error {
	return s.db.Connection(func(conn *gorm.DB) error { // 事务档：Connection 实参位置
		return nil
	})
}

var smuggle = func(tx *gorm.DB) error { return nil } // 句柄档：非事务位置的函数字面量

func (s *Svc) Run() error {
	var local *gorm.DB // 句柄档：局部句柄声明（旧度量会把它冒充事务例外）
	_ = local
	db := pkgdb.GetDB()    // GetDB 档：包级 accessor
	chain := pkgdb.GetDB() // GetDB 档：链式入口
	_ = db
	_ = chain
	h := s.Repo().GetDB() // GetDB 档：x().GetDB() 链式（shell 宽口径抓不到）
	_ = h
	return nil
}

func (s *Svc) Repo() *DB { return &DB{} }

func (s *Svc) HandleOut() *gorm.DB { return s.db } // 句柄档：返回值递句柄
`
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	handles, txCallbacks, getdb, files := scanTreeForTest(t, dir)
	if handles != 6 {
		t.Errorf("句柄档 = %d，期望 6（字段×2 + 构造形参 + 非事务字面量 + 局部 var + 返回值；注释/字符串/同名类型**不许**计入）", handles)
	}
	if txCallbacks != 2 {
		t.Errorf("事务档 = %d，期望 2（Transaction 与 Connection 实参位置各一）", txCallbacks)
	}
	if getdb != 3 {
		t.Errorf("GetDB 档 = %d，期望 3（包级 accessor×2 + x().GetDB() 链式；字符串里的不算）", getdb)
	}
	if files != 1 {
		t.Errorf("涉及文件 = %d，期望 1", files)
	}

	// 点导入：裸 `DB` 标识符也要抓（别名感知的另一侧）。
	dir2 := t.TempDir()
	src2 := `package service

import . "gorm.io/gorm"

type S2 struct {
	db *DB // 点导入 gorm 的裸 DB —— 句柄档
}
`
	if err := os.WriteFile(filepath.Join(dir2, "fixture_dot.go"), []byte(src2), 0o644); err != nil {
		t.Fatalf("写点导入夹具失败: %v", err)
	}
	h2, _, _, _ := scanTreeForTest(t, dir2)
	if h2 != 1 {
		t.Errorf("点导入句柄档 = %d，期望 1（裸 *DB 经点导入解析到 gorm.io/gorm）", h2)
	}
}

// TestTxCallbacksAreInTransactionPosition 事务回调**位置**正/反探针。
//
// 旧版注释自 2026-10-24 起就引用本测试，但源码里从来没有它——只有度量夹具。
// 本测试 2026-11-07（AV-03）补上：事务档只认 `.Transaction(...)` /
// `.Connection(...)` 实参位置的函数字面量；同一形状出现在任何其他位置
// （赋值、传给非事务方法、goroutine、返回值）都落句柄档——既不把合法事务
// 例外一律禁掉，也不让回调之外的句柄冒充例外。
func TestTxCallbacksAreInTransactionPosition(t *testing.T) {
	// 正例：三种合法接缝位置（含仓储自带的同形 Transaction 接缝），全部落事务档，
	// 且**不许**溢出到句柄档。
	posDir := t.TempDir()
	posSrc := `package service

import "gorm.io/gorm"

func good() {
	_ = dbHolder.Transaction(func(tx *gorm.DB) error { return nil })      // 事务档：Transaction 实参
	_ = dbHolder.Connection(func(conn *gorm.DB) error { return nil })     // 事务档：Connection 实参
	_ = repoSeam.Transaction(ctx, func(tx *gorm.DB) error { return nil }) // 事务档：仓储同形接缝
}
`
	if err := os.WriteFile(filepath.Join(posDir, "fixture_pos.go"), []byte(posSrc), 0o644); err != nil {
		t.Fatalf("写正例夹具失败: %v", err)
	}
	h, tx, getdb, _ := scanTreeForTest(t, posDir)
	if tx != 3 || h != 0 || getdb != 0 {
		t.Errorf("正例位置读数 = 句柄 %d / 事务 %d / GetDB %d，期望 0/3/0（合法事务例外不许被禁掉，也不许溢出成句柄）", h, tx, getdb)
	}

	// 反例：同样形状离开 Transaction/Connection 实参位置，一律句柄档、事务档必须为 0。
	negDir := t.TempDir()
	negSrc := `package service

import "gorm.io/gorm"

var smuggled = func(tx *gorm.DB) error { return nil } // 句柄档：包级函数字面量

func bad() {
	step := func(tx *gorm.DB) error { return nil } // 句柄档：赋给局部变量
	_ = step
	runner.Run(func(tx *gorm.DB) error { return nil }) // 句柄档：传给非事务方法
	_ = func(tx *gorm.DB) *gorm.DB { return nil }      // 句柄档：字面量形参 + gorm 返回值（双计）
	go func(tx *gorm.DB) { _ = tx }()                  // 句柄档：goroutine 字面量
}
`
	if err := os.WriteFile(filepath.Join(negDir, "fixture_neg.go"), []byte(negSrc), 0o644); err != nil {
		t.Fatalf("写反例夹具失败: %v", err)
	}
	h, tx, getdb, _ = scanTreeForTest(t, negDir)
	if h != 6 || tx != 0 || getdb != 0 {
		t.Errorf("反例位置读数 = 句柄 %d / 事务 %d / GetDB %d，期望 6/0/0（回调外的句柄不许冒充事务例外）", h, tx, getdb)
	}
}

func scanTreeForTest(t *testing.T, dir string) (handles, tx, getdb, files int) {
	t.Helper()
	stat, _, n, err := gormGuardScanTree(dir)
	if err != nil {
		t.Fatalf("扫描夹具 %s 失败: %v", dir, err)
	}
	return stat.handles, stat.tx, stat.getdb, n
}
