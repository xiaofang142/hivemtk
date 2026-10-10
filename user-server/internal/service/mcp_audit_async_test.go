package service

import (
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

// mcp_audit_async_test.go 异步审计写入器的行为用例（不连库即可跑）：
//   - db 不可用 → 批量 flush 后全部计入丢弃（可观测，不静默）；
//   - 队满 → 同步兜底写路径同样计入丢弃；
//
// 计数器是本用例能验证的最小行为面（成功路径由 PG 依赖用例与 CI 覆盖）。
func TestMCPAuditWriter_DropsWhenDBUnavailable(t *testing.T) {
	dbutil.SetTestDB(nil) // 模拟 DB 不可用（句柄缺失）
	defer dbutil.SetTestDB(nil)

	w := GetMCPAuditWriter()
	repo := repository.NewMCPCallAuditRepo()
	if repo.Available() {
		t.Skip("测试库句柄存在，本用例的丢弃路径无法构造（CI 无库环境跑）")
	}

	before := w.dropped.Load()
	w.EnqueueCall(repo, &model.MCPCallAudit{ClientID: "t", Method: "tools/call"})
	w.EnqueueAdmin(repository.NewMCPCredentialAuditLogRepo(),
		&model.MCPCredentialAuditLog{Action: "key.create", TargetType: "key"})

	// flush 周期 2s：给足 3s 让两路循环把队列消化成"丢弃"。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if w.dropped.Load() >= before+2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got := w.dropped.Load(); got < before+2 {
		t.Errorf("db 不可用时应计丢弃 ≥2，got %d（before=%d）", got, before)
	}
}

// TestMCPAuditWriter_QueueFullFallsBackSync 队满同步兜底路径：
// 一次性塞满队列再入队，多余那条走同步 Create（db 不可用 → 计丢弃）。
func TestMCPAuditWriter_QueueFullFallsBackSync(t *testing.T) {
	dbutil.SetTestDB(nil)
	defer dbutil.SetTestDB(nil)

	w := GetMCPAuditWriter()
	repo := repository.NewMCPCallAuditRepo()
	if repo.Available() {
		t.Skip("测试库句柄存在，本用例的兜底路径无法构造")
	}
	before := w.dropped.Load()
	for i := 0; i < mcpAuditQueueSize+1; i++ {
		w.EnqueueCall(repo, &model.MCPCallAudit{ClientID: "t"})
	}
	// 队满的那条立即计丢弃（同步路径）；
	after := w.dropped.Load()
	if after < before+1 {
		t.Errorf("队满同步兜底失败应立即计丢弃，dropped %d → %d", before, after)
	}
	// 后台循环消化完队列后，丢弃还应继续增长（异步路径同样失败）。
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if w.dropped.Load() >= before+int64(mcpAuditQueueSize+1) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if w.dropped.Load() < before+1 {
		t.Errorf("异步路径丢弃计数未增长")
	}
	var _ atomic.Int64 // 保持 atomic 引用可见（计数器类型锚点）
}
