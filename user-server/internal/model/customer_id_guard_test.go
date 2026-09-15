package model

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// customer_id 类型收敛守卫（OPT-DB-04）
//
// 背景：DB 列已由 v3_22_0 迁移统一为 varchar(64)，但 Go model 层的 tag 存在
// uint / varchar(36) / varchar(100) / size:128 等混杂声明。本测试用 AST 静态
// 扫描 internal/model/*.go（不连库、排除 _test.go），断言：
//
//	① gorm column 为 customer_id 或 Go 字段名为 CustomerID 的字段，Go 类型必须是 string
//	② gorm tag 中显式 varchar 长度只允许 64；size 只允许 64；无显式长度允许

var (
	reGuardVarchar = regexp.MustCompile(`varchar\((\d+)\)`)
	reGuardSize    = regexp.MustCompile(`(^|;)size:(\d+)`)
)

// guardScanModelFiles 扫描并返回全部违规项（文件:行 描述）。
func guardScanModelFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob model files: %v", err)
	}

	fset := token.NewFileSet()
	var violations []string

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || len(field.Names) == 0 || field.Tag == nil {
				return true
			}
			tag := strings.Trim(field.Tag.Value, "`")
			gormTag := reflectGormTag(tag)
			if gormTag == "" {
				return true
			}

			fieldName := field.Names[0].Name
			colName := gormTagValue(gormTag, "column")
			isCustomerID := fieldName == "CustomerID" || colName == "customer_id"
			if !isCustomerID {
				return true
			}

			line := fset.Position(field.Pos()).Line
			typeStr := exprToString(field.Type)

			if typeStr != "string" {
				violations = append(violations, fmt.Sprintf("%s:%d: field %s type = %s, want string", file, line, fieldName, typeStr))
			}
			if m := reGuardVarchar.FindStringSubmatch(gormTag); m != nil && m[1] != "64" {
				violations = append(violations, fmt.Sprintf("%s:%d: field %s declares varchar(%s), want varchar(64)", file, line, fieldName, m[1]))
			}
			if m := reGuardSize.FindStringSubmatch(gormTag); m != nil && m[2] != "64" {
				violations = append(violations, fmt.Sprintf("%s:%d: field %s declares size:%s, want size:64 or type:varchar(64)", file, line, fieldName, m[2]))
			}
			return true
		})
	}
	return violations
}

// reflectGormTag 从 struct tag 原文中提取 gorm:"..." 的内容。
func reflectGormTag(tag string) string {
	prefix := "gorm:\""
	idx := strings.Index(tag, prefix)
	if idx < 0 {
		return ""
	}
	rest := tag[idx+len(prefix):]
	end := strings.Index(rest, "\"")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// gormTagValue 取 gorm tag 中 key:value 形式的 value（如 column:customer_id → customer_id）。
func gormTagValue(gormTag, key string) string {
	for _, seg := range strings.Split(gormTag, ";") {
		kv := strings.SplitN(strings.TrimSpace(seg), ":", 2)
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

// exprToString 将字段类型表达式转为字符串（支持 []string、*string 等基本形态）。
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return x.Name + "." + e.Sel.Name
		}
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	}
	return fmt.Sprintf("%T", expr)
}

func TestCustomerIDGuard(t *testing.T) {
	violations := guardScanModelFiles(t)
	if len(violations) > 0 {
		t.Fatalf("customer_id type convergence violated (%d):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func TestCustomerIDGuard_ScanSanity(t *testing.T) {
	violations := guardScanModelFiles(t)
	files := 0
	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files++
		}
		return nil
	})
	if files == 0 {
		t.Fatalf("guard scanned zero source files — scanner broken")
	}
	t.Logf("scanned %d model source files, %d violations", files, len(violations))
}
