package service

// 拆包（第五刀）后仍留在根包的用例所需的共享测试夹具：
// 审批时钟冻结/恢复走 internal/service/approval 的测试接缝。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service/approval"
)

// recordingPolicy 记录 auto-approve 判据的调用（随审批域拆出的测试桩，跨包用例复用）。
type recordingPolicy struct {
	allow  bool
	reason string
	calls  int
	seen   []approval.ApprovalSubmitInput
}

func (p *recordingPolicy) AutoApproves(_ context.Context, in approval.ApprovalSubmitInput) (bool, string) {
	p.calls++
	p.seen = append(p.seen, in)
	return p.allow, p.reason
}

// seedOnModeTenantEnv 设 on 模式 + 活跃租户枚举器，并保证用例间复位。
func seedOnModeTenantEnv(t *testing.T, ids []uint) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) { return ids, nil })
	t.Cleanup(func() {
		tenant.SetActiveTenantLoader(nil)
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
}

// freezeApprovalClock 把审批服务的时钟换成可控值，返回"往前推"的函数。
func freezeApprovalClock(t *testing.T, at time.Time) func(time.Duration) {
	t.Helper()
	current := at
	previous := approval.LoadNowFnForTest()
	approval.StoreNowFnForTest(func() time.Time { return current })
	t.Cleanup(func() { approval.StoreNowFnForTest(previous) })
	return func(d time.Duration) { current = current.Add(d) }
}
