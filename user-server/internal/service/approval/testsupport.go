package approval

// testsupport.go 暴露审批时钟/凭证接缝的读写入口，供**跨包用例**冻结与恢复。
// 拆包（第五刀）后原来同包的服务用例留在根包，它们需要驱动"到期/裁决"这两条
// 与时钟强相关的判据。这里只转出既有 unexported 接缝，不新增生产能力。

import (
	"time"

	"hivemtk-user/internal/repository"
)

// RepoForTest 暴露仓储句柄，供跨包用例做只读断言（拆包前沿用同包直读字段）。
func (s *ApprovalRequestService) RepoForTest() repository.ApprovalRequestRepository { return s.repo }

// LoadNowFnForTest 读当前时钟函数。
func LoadNowFnForTest() func() time.Time { return loadApprovalNowFn() }

// StoreNowFnForTest 替换时钟函数（调用方负责恢复原值）。
func StoreNowFnForTest(fn func() time.Time) {
	approvalSeamMu.Lock()
	approvalNowFn = fn
	approvalSeamMu.Unlock()
}

// LoadResumeTokFnForTest 读当前恢复凭证生成函数。
func LoadResumeTokFnForTest() func() (string, error) { return loadApprovalResumeTokFn() }

// StoreResumeTokFnForTest 替换恢复凭证生成函数（调用方负责恢复原值）。
func StoreResumeTokFnForTest(fn func() (string, error)) {
	approvalSeamMu.Lock()
	approvalResumeTokFn = fn
	approvalSeamMu.Unlock()
}
