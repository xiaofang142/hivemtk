package controller

import (
	"os"
	"strings"
	"testing"
)

// F7 编排位置锁：host/status 的 admin/普通用户两个分支必须走同一个 StatusFor，
// 否则「普通用户分支少给 count」这一形状差会让扩展 popup 永久误报 Host 离线
// （形状本身由 service 层 TestStatusForShapeIsRoleIndependent 锁，此处只锁接线）。
func TestHostStatusUsesSingleUnifiedPath(t *testing.T) {
	b, err := os.ReadFile("host.go")
	if err != nil {
		t.Fatalf("host.go 不可读：接线锁失效即视同红，got %v", err)
	}
	src := string(b)
	if got := strings.Count(src, "c.registry.StatusFor("); got != 1 {
		t.Errorf("GetStatus 应恰有一处 StatusFor 调用（单一路径），got %d", got)
	}
	if strings.Contains(src, "registry.MyStatus(") {
		t.Error("GetStatus 不得回退到 MyStatus 旧形状分支")
	}
	if strings.Contains(src, "len(c.registry.Status())") {
		t.Error("count 不得由 Status() 二次调用求得（与 hosts 之间可插入注册/摘除）")
	}
}
