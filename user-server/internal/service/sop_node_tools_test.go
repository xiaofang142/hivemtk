package service

import (
	"context"
	"errors"
	"testing"
)

// TestValidateGraph_RejectsNodeTools 钉住 I5 的 fail-closed 口径：
// SOP 节点上的 `Tools` 白名单至今没有执行通路（执行器零读取，`sop.go` 只做深拷贝），
// 因此带 Tools 的图必须在保存/更新的验证期被显式拒绝，而不是让画布配置静默失效
// （与 OWASP "Excessive Agency" 的最小权限方向相反）。
func TestValidateGraph_RejectsNodeTools(t *testing.T) {
	svc := &SOPService{}

	// 基线：无 Tools 的同构合法图必须通过（确认拒绝来自 Tools，而不是别的结构问题）。
	ok := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd},
		},
	}
	if err := svc.validateGraph(context.Background(), ok); err != nil {
		t.Fatalf("无 Tools 的合法图不应被拒：%v", err)
	}

	// 任一节点带非空 Tools 即拒，且错误可被 errors.Is 识别。
	bad := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd, Tools: []string{"crm.update"}},
		},
	}
	err := svc.validateGraph(context.Background(), bad)
	if err == nil {
		t.Fatal("带 Tools 的节点应被拒绝保存")
	}
	if !errors.Is(err, ErrSOPNodeToolsUnsupported) {
		t.Fatalf("错误应包装 ErrSOPNodeToolsUnsupported，实际：%v", err)
	}

	// 空 Tools 切片等价于未配置，不拒。
	empty := &SOPGraph{
		Nodes: []SOPNode{
			{ID: "start", Type: SOPNodeTypeStart, Next: []string{"end"}},
			{ID: "end", Type: SOPNodeTypeEnd, Tools: []string{}},
		},
	}
	if err := svc.validateGraph(context.Background(), empty); err != nil {
		t.Fatalf("空 Tools 切片不应被拒：%v", err)
	}
}
