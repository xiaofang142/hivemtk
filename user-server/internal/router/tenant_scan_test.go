// tenant_scan_test.go —— S-18（specs/001-saas-multi-tenant 审计二期）静态锚。
//
// 背景见 cross_tenant_scan_test.go 的三条闸：闸三已用真库钉住 6 条取数风格样例，
// 既有结论是「其余端点靠 GORM 隔离 callback 在 SQL 构造期统一注入」
// （internal/pkg/tenancyidx/tenant_callback.go：Query/Row 注 WHERE、Update/Delete
// 注 WHERE、Create 填列、Save/upsert 有专门守卫、预构建 SQL 整体拒绝）。
//
// 但 callback 有一个**已知盲区**（champion_dialogue.go 的注释是第一现场）：
// 写进 Select/Where/Order/Table 字面量里的**子查询与派生表**，callback 注入不到——
// 它只认外层表、显式 JOIN（scopeTenantJoins 注 ON）和整段预构建 SQL（Raw 闸）。
// 本轮审计（S-18）在盲区里实测命中一个真洞：
//
//	internal/ops/repository/ai_productivity.go GetAvgResponseTime 曾用
//	`Table("(SELECT … FROM session_messages …)")` 派生表字面量，on 模式下把全部
//	租户的消息聚进同一个平均响应时长（/api/analytics/ai-productivity 是租户端点）；
//	回归用例见 internal/ops/repository/ai_productivity_tenant_test.go。
//
// 同批加固：trace_learning（NOT IN 字面量）、reflux.JoinCluster（SET 内子查询）、
// message_hub DeleteOrphanConvInboxRows / help_center ListPublicArticles（Where
// 字面量子查询）——全部改为 GORM 子查询或显式 tenant_id 相关条件。
//
// 本文件把「盲区里这次核过什么」固化为两条静态锚（纯源码扫描，不依赖测试库）：
//
//	锚一（派生表字面量）：repository 层禁止 `Table("(SELECT …)"` 字面量派生表——
//	  外层表名解析不出 ⇒ 永远不注入。要用窗口函数/派生表就写 GORM 子查询
//	  （Table("(?)", sub)：*gorm.DB 经 callbacks.Query().Execute(DryRun) 展开，
//	  注入对子查询照常生效）。
//	锚二（字面量引用租户表）：字符串字面量里出现 `FROM/JOIN/UPDATE/INTO <租户表>`
//	  的，同一字面量组必须自带 `tenant_id` 条件。GORM 子查询不含 FROM 字面量，
//	  天然豁免；`.Joins(...)` 实参组也豁免——scopeTenantJoins 会往 ON 里注入，
//	  已有对抗用例。豁免一律登记在 fragmentExemptions 并写明理由。
//
// 租户表清单的取法与 scanModelTables 同思路（internal/model/module_tables_test.go）：
// 扫源码而非维护清单。knownTenantTablesGuard 钉住本轮实际核过的表——struct 解析、
// TableName 关联或 NamingStrategy 回退任何一环退化，这里先红。
package router

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm/schema"
)

// ── 租户表清单：扫 model 源码（含 TenantID 字段的 struct → 表名） ──────────

// knownTenantTablesGuard 防扫描退化的钉子：这些表必须被扫出来且判为租户表。
// 全部是本轮审计实际核过的表。
var knownTenantTablesGuard = []string{
	"session_messages", "trace_eval_log", "knowledge_chunks", "knowledge_documents",
	"message_hub", "inbox_conversations", "message_trace", "feedback_events",
	"unknown_questions", "unknown_question_clusters", "asset_listings", "browser_tasks",
}

var (
	tenantFieldRe = regexp.MustCompile(`\bTenantID\b`)
	structOpenRe  = regexp.MustCompile(`^type\s+(\w+)\s+struct\s*\{`)
)

// walkInternalDirs 收集 internal 下名字匹配 want(dirName) 的目录；geo 与点目录整棵跳过
// （geo 是另一条在途 lane 的禁区，本锚不读也不判）。
func walkInternalDirs(t *testing.T, root string, want func(dirName string) bool) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "geo" || strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		if want(name) {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 internal 失败: %v", err)
	}
	return dirs
}

// scanTenantTables 返回表名 → 是否租户表。struct 与 TableName 按同文件配对
// （go/ast 精确解析，非正则）；无显式 TableName 时按 GORM NamingStrategy 回推。
func scanTenantTables(t *testing.T, root string) map[string]bool {
	t.Helper()
	modelDirs := walkInternalDirs(t, root, func(name string) bool { return name == "model" })
	if len(modelDirs) == 0 {
		t.Fatal("一个 model 目录都没扫到：扫描规则失效")
	}
	fset := token.NewFileSet()
	tenantStructs := map[string]bool{}
	explicitTables := map[string]string{}
	for _, dir := range modelDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("解析 %s 失败: %v", path, err)
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || fd.Name.Name != "TableName" || fd.Body == nil {
					continue
				}
				recv := fd.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				ident, ok := recv.(*ast.Ident)
				if !ok {
					continue
				}
				var lit string
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING && lit == "" {
						lit = strings.Trim(bl.Value, "`\"")
					}
					return lit == ""
				})
				if lit != "" {
					explicitTables[ident.Name] = lit
				}
			}
			// struct 块按行扫（gofmt 后 `}` 独占一行，配对可靠）
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读 %s 失败: %v", path, err)
			}
			curStruct := ""
			var body strings.Builder
			flush := func() {
				if curStruct != "" && tenantFieldRe.MatchString(body.String()) {
					tenantStructs[curStruct] = true
				}
				curStruct = ""
				body.Reset()
			}
			for _, line := range strings.Split(string(src), "\n") {
				if m := structOpenRe.FindStringSubmatch(line); m != nil {
					flush()
					curStruct = m[1]
					continue
				}
				if curStruct != "" {
					if line == "}" {
						flush()
						continue
					}
					body.WriteString(line)
					body.WriteByte('\n')
				}
			}
			flush()
		}
	}
	naming := schema.NamingStrategy{}
	tables := map[string]bool{}
	for structName := range tenantStructs {
		if tbl, ok := explicitTables[structName]; ok && tbl != "" {
			tables[strings.ToLower(tbl)] = true
			continue
		}
		tables[strings.ToLower(naming.TableName(structName))] = true
	}
	// tenants 本身是全局表（租户根），不该混进租户数据表清单
	if tables["tenants"] {
		t.Fatal("Tenant 模型被扫成租户数据表（struct 混进了 TenantID 字段？），登记表判据需要复核")
	}
	return tables
}

// ── repository 源码清单 ────────────────────────────────────────────────────

func tenantScanRepoDirs(t *testing.T, root string) []string {
	t.Helper()
	dirs := walkInternalDirs(t, root, func(name string) bool { return name == "repository" })
	if len(dirs) < 3 {
		t.Fatalf("repository 目录只扫到 %d 个（预期 internal/repository 与各域包），扫描规则失效", len(dirs))
	}
	return dirs
}

// ── 字符串字面量分组 ───────────────────────────────────────────────────────

type sqlFragmentGroup struct {
	file  string
	line  int    // 组首字面量所在行
	text  string // 组内全部字面量的拼接
	start int    // 组首字面量起始偏移（回看前置代码用）
}

// gofmt 链式调用里点号常落在上一行行尾（`Select(...).\n\t\tJoins(`），
// 所以点号与调用名之间允许跨行空白。
var joinsCallRe = regexp.MustCompile(`(?:\.\s*|\(\s*)Joins\s*\($`)
var tableCallRe = regexp.MustCompile(`\.\s*Table\s*\($`)

// literalGroupsOfFile 把源码里的字符串字面量（双引号/反引号）按 `+` 连接分组。
// 注释整体跳过——修复说明会举例写洞的旧形状，不剥注释会把注释当证据误报。
func literalGroupsOfFile(t *testing.T, file, src string) []sqlFragmentGroup {
	t.Helper()
	type lit struct {
		start, end int
		line       int
		text       string
	}
	var lits []lit
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
		case c == '"':
			startLine, start := line, i
			i++
			var sb strings.Builder
			for i < len(src) && src[i] != '"' {
				if src[i] == '\\' && i+1 < len(src) {
					sb.WriteByte(src[i])
					sb.WriteByte(src[i+1])
					i += 2
					continue
				}
				if src[i] == '\n' {
					line++
				}
				sb.WriteByte(src[i])
				i++
			}
			i++
			lits = append(lits, lit{start, i, startLine, sb.String()})
		case c == '`':
			startLine, start := line, i
			i++
			var sb strings.Builder
			for i < len(src) && src[i] != '`' {
				if src[i] == '\n' {
					line++
				}
				sb.WriteByte(src[i])
				i++
			}
			i++
			lits = append(lits, lit{start, i, startLine, sb.String()})
		default:
			i++
		}
	}
	// 相邻字面量之间只有空白 / + / 括号 ⇒ 同一组（Go 字符串拼接的常见排版）
	var groups []sqlFragmentGroup
	for k, l := range lits {
		if k > 0 {
			gap := src[lits[k-1].end:l.start]
			mergeable := true
			for _, gc := range gap {
				if gc != ' ' && gc != '\t' && gc != '\n' && gc != '\r' && gc != '+' && gc != '(' && gc != ')' {
					mergeable = false
					break
				}
			}
			if mergeable {
				groups[len(groups)-1].text += l.text
				continue
			}
		}
		groups = append(groups, sqlFragmentGroup{file: file, line: l.line, text: l.text, start: l.start})
	}
	return groups
}

// isJoinsArgument 回看组首字面量之前的代码，是否紧跟 `.Joins(`。
// 显式 JOIN 的租户条件由 scopeTenantJoins 注入 ON 子句（对抗用例在
// pkg/db/tenant_callback_adversarial_test.go），不要求字面量自带 tenant_id。
func isJoinsArgument(src string, group sqlFragmentGroup) bool {
	prefix := strings.TrimRight(src[:group.start], " \t\n\r")
	return joinsCallRe.MatchString(prefix)
}

// isTableArgument 同口径回看，是否 `.Table(` 实参——锚一只禁 Table 派生表字面量；
// gorm.Expr("(SELECT …)") 这类 SET/Where 片段不在此列（它们归锚二管：必须自带
// tenant_id 条件）。
func isTableArgument(src string, group sqlFragmentGroup) bool {
	prefix := strings.TrimRight(src[:group.start], " \t\n\r")
	return tableCallRe.MatchString(prefix)
}

// ── 锚一 + 锚二 ────────────────────────────────────────────────────────────

var fromRefRe = regexp.MustCompile(`(?i)\b(?:from|join|update|into)\s+(?:([a-z_][a-z0-9_$]*)\.)?([a-z_][a-z0-9_$]*)`)

// fragmentExemptions 锚二的显式豁免台账（键 = 文件基名:行号，值 = 理由）。
// 台账里的条目失效（行号对不上）会被反向报错，台账永远等于事实。
var fragmentExemptions = map[string]string{}

type fragmentViolation struct {
	file, line, frag, ref string
}

func TestRepositorySQLFragmentsCarryTenantScope(t *testing.T) {
	root := tenantScanRoot(t)
	tables := scanTenantTables(t, root)
	for _, want := range knownTenantTablesGuard {
		if !tables[want] {
			t.Fatalf("租户表清单缺 %q（struct/TableName 扫描退化），锚二对它静默失效", want)
		}
	}

	var viols []fragmentViolation
	liveKeys := map[string]bool{}
	scanned := 0
	for _, dir := range tenantScanRepoDirs(t, root) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读 %s 失败: %v", path, err)
			}
			scanned++
			for _, g := range literalGroupsOfFile(t, path, string(src)) {
				key := filepath.Base(g.file) + ":" + strconv.Itoa(g.line)
				liveKeys[key] = true
				trimmed := strings.TrimSpace(strings.ToLower(g.text))
				// 锚一：Table 派生表字面量（外层解析不出表名 ⇒ 永远不注入）。
				// 仅限 .Table( 实参：gorm.Expr("(SELECT …)") 这类 SET/Where 片段
				// 归锚二管（必须自带 tenant_id 条件）。
				if strings.HasPrefix(trimmed, "(select") && isTableArgument(string(src), g) {
					viols = append(viols, fragmentViolation{g.file, strconv.Itoa(g.line), g.text, "(SELECT 派生表字面量"})
					continue
				}
				// 锚二：字面量引用租户表 ⇒ 必须自带 tenant_id 条件
				for _, m := range fromRefRe.FindAllStringSubmatch(g.text, -1) {
					ref := strings.ToLower(m[2])
					if !tables[ref] {
						continue
					}
					if isJoinsArgument(string(src), g) {
						continue
					}
					if strings.Contains(strings.ToLower(g.text), "tenant_id") {
						continue
					}
					if _, ok := fragmentExemptions[key]; ok {
						continue
					}
					viols = append(viols, fragmentViolation{g.file, strconv.Itoa(g.line), g.text, ref})
				}
			}
		}
	}
	if scanned < 200 {
		t.Fatalf("repository 源文件只扫到 %d 个（预期 300+），扫描规则失效", scanned)
	}
	// 反向：台账不许有失效条目
	for key := range fragmentExemptions {
		if !liveKeys[key] {
			t.Errorf("豁免台账里的 %s 已失效（行号对不上），请删除条目让台账等于事实", key)
		}
	}

	if len(viols) > 0 {
		sort.Slice(viols, func(i, j int) bool {
			return viols[i].file+viols[i].line < viols[j].file+viols[j].line
		})
		var sb strings.Builder
		for _, v := range viols {
			sb.WriteString(filepath.Base(v.file) + ":" + v.line + " 引用 " + v.ref +
				" 但字面量不带 tenant_id：" + summarizeFragment(v.frag) + "\n")
		}
		t.Fatalf("repository 层 SQL 字面量存在租户隔离 callback 注入不到的子查询/派生表（S-18 盲区）：%s\n"+
			"修法二选一：改 GORM 子查询（*gorm.DB 经 DryRun 展开，注入照常生效），"+
			"或在同一字面量组显式带上 tenant_id 条件（相关式）；确需豁免则登记 fragmentExemptions 并写明理由。",
			sb.String())
	}
	t.Logf("锚一+锚二：扫过 %d 个 repository 源文件，租户表 %d 张，违规 0", scanned, len(tables))
}

// ── 小工具 ─────────────────────────────────────────────────────────────────

func tenantScanRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到本文件路径")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func summarizeFragment(frag string) string {
	frag = strings.Join(strings.Fields(frag), " ")
	if len(frag) > 120 {
		frag = frag[:120] + "…"
	}
	return frag
}
