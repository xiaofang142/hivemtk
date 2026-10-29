package sop

// testsupport.go 转出少量内部符号，供**跨包集成用例**（根包里的 reach E2E）使用。
// 全部是常量/类型别名/既有方法转发，不改变生产路径。

import (
	"context"
	"time"

	"hivemtk-user/internal/model"
)

const (
	ApprovalOutcomeUnreadable      = approvalOutcomeUnreadable
	ApprovalTimerPayloadToken      = approvalTimerPayloadToken
	ApprovalTimerPayloadApprovalID = approvalTimerPayloadApprovalID
	OutputKeyReachSkip             = outputKeyReachSkip
	OutputKeyReachChannel          = outputKeyReachChannel
	OutputKeyReachMessageID        = outputKeyReachMessageID
	ReachNodeOneIDKey              = reachNodeOneIDKey
)

// DispatchTask 派发任务类型别名。
type DispatchTask = dispatchTask

func FindNodeByID(graph *SOPGraph, id string) *SOPNode     { return findNodeByID(graph, id) }
func MustJSON(v any) []byte                                { return mustJSON(v) }
func ToJSONMapBytes(data []byte) model.JSONMap             { return toJSONMapBytes(data) }
func PayloadString(payload model.JSONMap, k string) string { return payloadString(payload, k) }

// ProcessTaskForTest 同步处理一个派发任务。
func (d *SOPExecutionDispatcher) ProcessTaskForTest(ctx context.Context, workerID int, task *DispatchTask) {
	d.processTask(ctx, workerID, task)
}

// DrainQueueForTest 取出派发队列里的全部任务（不处理）。
func (d *SOPExecutionDispatcher) DrainQueueForTest() []*DispatchTask {
	var out []*DispatchTask
	for {
		select {
		case t := <-d.dispatchQueue:
			out = append(out, t)
		default:
			return out
		}
	}
}

// ProcessDueTimersForTest 同步跑一轮到期 timer 扫描。
func (o *SOPOutboxDispatcher) ProcessDueTimersForTest(ctx context.Context) { o.processDueTimers(ctx) }

// —— 参数中心读取函数（跨包用例断言"真的从库里读"）——

func DispatchWorkerCount(ctx context.Context) int       { return sopDispatcherWorkerCount(ctx) }
func DispatchQueueCapacity(ctx context.Context) int     { return sopDispatcherQueueCapacity(ctx) }
func DispatchLLMConcurrency(ctx context.Context) int    { return sopDispatcherLLMConcurrency(ctx) }
func CompensationMaxAttempts(ctx context.Context) int   { return sopCompensationMaxAttempts(ctx) }
func CompensationTTL(ctx context.Context) time.Duration { return sopCompensationTTL(ctx) }
func CompensationMaxPlansKept(ctx context.Context) int  { return sopCompensationMaxPlansKept(ctx) }

// WorkerCountForTest / QueueCapForTest / LLMSemCapForTest 读派发器内部容量（跨包用例断言用）。
func (d *SOPExecutionDispatcher) WorkerCountForTest() int { return d.workerCount }
func (d *SOPExecutionDispatcher) QueueCapForTest() int    { return cap(d.dispatchQueue) }
func (d *SOPExecutionDispatcher) LLMSemCapForTest() int   { return cap(d.llmSem) }

// SwapCompensationEnabledFn 替换补偿开关并返回恢复函数（跨包用例用）。
func SwapCompensationEnabledFn(fn func() bool) func() {
	old := compensationEnabledFn
	if fn != nil {
		compensationEnabledFn = fn
	}
	return func() { compensationEnabledFn = old }
}

// ConfigForTest 读补偿管理器的生效配置（跨包用例断言用）。
func (m *CompensationManager) ConfigForTest() CompensationConfig { return m.config }
