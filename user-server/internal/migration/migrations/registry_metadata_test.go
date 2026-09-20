package migrations

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"hivemtk-user/internal/migration"
)

var versionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// TestRegisteredMigrationsMetadata 覆盖全部迁移的 Version/Name/Description：
// 版本格式、命名与描述非空是运维排错（migrations 列表页 / 日志）的唯一信息来源。
func TestRegisteredMigrationsMetadata(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)

	all := reg.GetAll()
	if len(all) < 60 {
		t.Fatalf("注册迁移数=%d, 期望覆盖仓库内全部 60+ 个迁移文件", len(all))
	}
	for _, m := range all {
		v := m.Version()
		if !versionRe.MatchString(v) {
			t.Errorf("版本格式非法: %q (%s)", v, m.Name())
		}
		if strings.TrimSpace(m.Name()) == "" {
			t.Errorf("%s Name() 为空", v)
		}
		if strings.TrimSpace(m.Description()) == "" {
			t.Errorf("%s Description() 为空", v)
		}
	}

	// GetAll 必须按版本升序（决定新装实例的 Up 顺序）；重复版本在此暴露为「不小于前项」。
	for i := 1; i < len(all); i++ {
		if strings.Compare(versionKey(all[i-1].Version()), versionKey(all[i].Version())) >= 0 {
			t.Errorf("GetAll 未按版本升序: %s 在 %s 之前", all[i-1].Version(), all[i].Version())
		}
	}
}

// versionKey 把 "v3.9.0" / "v3.10.0" 归一为可字典序比较的定宽键（补零到 3 段 × 3 位）。
func versionKey(v string) string {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	out := make([]string, 3)
	for i := 0; i < 3; i++ {
		n := "0"
		if i < len(parts) {
			n = parts[i]
		}
		for len(n) < 3 {
			n = "0" + n
		}
		out[i] = n
	}
	return strings.Join(out, ".")
}

// TestEmptyRegistryLookups 空注册表不应返回任何迁移。
func TestEmptyRegistryLookups(t *testing.T) {
	reg := migration.NewMigrationRegistry()
	if m, ok := reg.Get(""); ok || m != nil {
		t.Errorf("空注册表 Get(\"\") 应返回 (nil,false), got (%v,%v)", m, ok)
	}
	if m, ok := reg.Get("v2.11.0"); ok || m != nil {
		t.Errorf("未注册版本应返回 (nil,false), got (%v,%v)", m, ok)
	}
	if got := reg.GetAll(); len(got) != 0 {
		t.Errorf("空注册表 GetAll 应为空, got %d", len(got))
	}
	if err := reg.Validate(); err != nil {
		t.Errorf("空注册表校验应通过: %v", err)
	}
}

// TestNoopMigrationsRunWithoutDB no-op 迁移（Up/Down 直接 return nil）应在 nil db 下也可调用。
// 非 no-op 迁移不在本用例范围内：它们需要真实连接，由全链路用例覆盖。
func TestNoopMigrationsRunWithoutDB(t *testing.T) {
	noop := []struct {
		version string
		build   func() migration.Migration
	}{
		{"v2.11.0", func() migration.Migration { return NewADomainP1Migration(nil) }},
	}
	ctx := context.Background()
	for _, n := range noop {
		mig := n.build()
		if mig.Version() != n.version {
			t.Fatalf("样例迁移版本漂移: %q want %q", mig.Version(), n.version)
		}
		if err := mig.Up(ctx); err != nil {
			t.Errorf("%s 应为 no-op Up, got %v", n.version, err)
		}
		if err := mig.Down(ctx); err != nil {
			t.Errorf("%s 应为 no-op Down, got %v", n.version, err)
		}
	}
}

// TestEveryImplementedMigrationIsRegistered 静态扫描本包源码：凡是实现了 Migration 接口
// （同类型既有 Up 又有返回字面量的 Version）的类型，其版本必须出现在注册表里。
// 注册靠手写 register(...) 清单，漏一行 = 该迁移在任何环境都永不执行，且不会有任何报错。
func TestEveryImplementedMigrationIsRegistered(t *testing.T) {
	// knownUnregistered 登记「已实现但未注册」的历史欠账；一旦补注册必须从这里删条。
	knownUnregistered := map[string]string{
		"v3.25.0": "CustomerOwnerAgentMigration 从未进 RegisterMigrations 清单",
		"v3.26.0": "ReachTablesMigration 从未进 RegisterMigrations 清单",
	}

	implemented, err := implementedVersions(".")
	if err != nil {
		t.Fatalf("扫描包源码失败: %v", err)
	}
	if len(implemented) < 60 {
		t.Fatalf("静态扫描只找到 %d 个迁移实现，解析口径可能已失效", len(implemented))
	}

	reg := migration.NewMigrationRegistry()
	RegisterMigrations(reg, nil)
	registered := map[string]bool{}
	for _, m := range reg.GetAll() {
		registered[m.Version()] = true
	}

	missing := map[string]string{}
	for v, typeName := range implemented {
		if !registered[v] {
			missing[v] = typeName
		}
	}
	for v, why := range knownUnregistered {
		if _, stillMissing := missing[v]; !stillMissing {
			t.Errorf("已登记的未注册迁移 %s 如今已在注册表里（原因：%s），请从 knownUnregistered 移除", v, why)
			continue
		}
		delete(missing, v)
	}
	if len(missing) > 0 {
		t.Errorf("有 %d 个迁移实现了却从未注册：%s", len(missing), formatFailures(missing))
	}
}

type scannedMigration struct {
	hasUp   bool
	version string
}

// implementedVersions 用 AST 扫出 dir 下非测试源文件里的 版本 -> 迁移类型名。
func implementedVersions(dir string) (map[string]string, error) {
	byType := map[string]*scannedMigration{}
	seen := func(recv string) *scannedMigration {
		if byType[recv] == nil {
			byType[recv] = &scannedMigration{}
		}
		return byType[recv]
	}

	pkgs, err := parser.ParseDir(token.NewFileSet(), dir,
		func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") },
		0)
	if err != nil {
		return nil, err
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
					continue
				}
				recv := receiverName(fn.Recv.List[0].Type)
				switch fn.Name.Name {
				case "Up":
					seen(recv).hasUp = true
				case "Version":
					if v := returnedStringLiteral(fn); v != "" {
						seen(recv).version = v
					}
				}
			}
		}
	}

	out := map[string]string{}
	for recv, m := range byType {
		if m.hasUp && m.version != "" {
			out[m.version] = recv
		}
	}
	return out, nil
}

func receiverName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func returnedStringLiteral(fn *ast.FuncDecl) string {
	if fn.Body == nil {
		return ""
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		if bl, ok := ret.Results[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
			return strings.Trim(bl.Value, "`\"")
		}
	}
	return ""
}
