package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestGCRAFixture_DoesNotClearSharedRedisDatabase(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "reach_gcra_limiter_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (selector.Sel.Name == "FlushDB" || selector.Sel.Name == "FlushAll") {
			t.Error("限流测试不能清除共享Redis数据库，必须只清理本用例唯一键")
		}
		return true
	})
}
