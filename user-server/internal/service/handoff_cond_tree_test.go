package service

import (
	"testing"
)

// D20: 条件树求值
func TestD20_CondTreeEvaluate(t *testing.T) {
	tree, err := ParseCondTree(`{"op":"and","conditions":[
		{"attr":"platform","operator":"eq","value":"wecom"},
		{"op":"or","conditions":[
			{"attr":"ai_reply_count","operator":"gt","value":5},
			{"attr":"status","operator":"eq","value":"waiting"}
		]}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	if tree == nil {
		// ParseCondTree 对空串走 (nil, nil)，err 判空挡不住这一支。Evaluate 自带 nil 守卫，
		// 所以缺这一判不会 panic，只会把「前置没满足」摊成下面三条看不懂的断言失败。
		t.Fatal("ParseCondTree 交回 (nil, nil) ⇒ 非空配置却解析成 nil 树")
	}
	attrs := map[string]any{"platform": "wecom", "ai_reply_count": 8.0, "status": "active"}
	if !tree.Evaluate(attrs) {
		t.Error("platform=wecom 且 ai=8（>5）应通过")
	}
	attrs2 := map[string]any{"platform": "wecom", "ai_reply_count": 1.0, "status": "active"}
	if tree.Evaluate(attrs2) {
		t.Error("ai=1 且 status!=waiting 应不通过")
	}
	attrs3 := map[string]any{"platform": "wecom", "ai_reply_count": 1.0, "status": "waiting"}
	if !tree.Evaluate(attrs3) {
		t.Error("status=waiting 分支应通过")
	}
}

// D20: 深度超限/非法算子拒绝
func TestD20_CondTreeValidation(t *testing.T) {
	deep := `{"op":"and","conditions":[{"op":"and","conditions":[{"op":"and","conditions":[{"attr":"x","operator":"eq","value":1}]}]}]}`
	if _, err := ParseCondTree(deep); err == nil {
		t.Error("深度>3 应拒绝")
	}
	badOp := `{"attr":"x","operator":"regex","value":".*"}`
	if _, err := ParseCondTree(badOp); err == nil {
		t.Error("非法算子应拒绝")
	}
	if _, err := ParseCondTree(""); err != nil {
		t.Errorf("空配置应返回 nil 树（不参与决策）, got %v", err)
	}
}

// D20: 缺 attr = false（宽松安全）
func TestD20_MissingAttrFalse(t *testing.T) {
	tree, err := ParseCondTree(`{"attr":"nonexistent","operator":"eq","value":"x"}`)
	if err != nil {
		t.Fatalf("ParseCondTree: %v", err)
	}
	if tree == nil {
		// 这一判是本腿的承重墙：期望值是 false，而 Evaluate 对 nil 接收者也返回 false，
		// 所以 nil 树在这里「通过」——不钉住 tree 非 nil，这条用例解析失败也照样绿。
		t.Fatal("ParseCondTree 交回 (nil, nil) ⇒ 夹具配置没解析成树，下面的 false 是 nil 守卫给的")
	}
	if tree.Evaluate(map[string]any{"other": "x"}) {
		t.Error("缺失 attr 应 false")
	}
}
