// reach_deferred_quiet_queue_test.go D02（主动触达 quiet 队列持久化）测试。
// 验收口径（docs/assessment/functionality-2026-10-27/开发顺序与验收.md D02）：
//   - 进入 quiet 后重启仍有一条待发（持久化，不再随进程重启丢队）；
//   - 到期重发走原管线且不再二次判延（bypass）；
//   - 取消后不发送；
//   - 发送失败有持久结果和重试计划；租约过期转 unknown 不自动重发。
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type stubDeferredPipeline struct {
	resp    *SendResponse
	lastReq *ReachSendRequest
	lastCtx context.Context
	calls   int
}

func (s *stubDeferredPipeline) Send(ctx context.Context, req *ReachSendRequest) *SendResponse {
	s.calls++
	s.lastReq = req
	s.lastCtx = ctx
	return s.resp
}

func setupDeferredQueue(t *testing.T) (*PersistentQuietHoursQueue, *repository.ReachDeferredOutboundRepository, *gorm.DB) {
	database := testutil.NewTestDB(t,
		&model.ReachDeferredOutbound{},
	)
	repo := repository.NewReachDeferredOutboundRepository(database)
	return NewPersistentQuietHoursQueue(repo), repo, database
}

func deferredTestReq() *ReachSendRequest {
	return &ReachSendRequest{
		Channel:     "wecom",
		AccountID:   "acc-1",
		RecipientID: "cust-1",
		CustomerID:  "cust-1",
		OperatorID:  "op-1",
		MsgType:     "text",
		Content:     "安静时段攒下的主动触达",
		Params:      map[string]string{"k": "v"},
	}
}

// Defer 必须持久落库：完整请求快照 + 到期时刻 + job id 回填。
func TestPersistentQuietQueue_DeferPersistsIntent(t *testing.T) {
	q, repo, _ := setupDeferredQueue(t)
	ctx := context.Background()
	sendAt := time.Now().Add(time.Hour)

	req := deferredTestReq()
	if err := q.Defer(ctx, req, sendAt); err != nil {
		t.Fatalf("Defer: %v", err)
	}
	if req.Metadata["deferred_job_id"] == "" {
		t.Fatal("Defer 应回填持久 job id（deferred_job_id）")
	}

	rows, total, err := repo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("应恰有 1 条 pending，got %d/%d", len(rows), total)
	}
	if !rows[0].SendAt.Equal(sendAt) {
		t.Errorf("send_at = %v, want %v", rows[0].SendAt, sendAt)
	}
	if rows[0].Channel != "wecom" || rows[0].RecipientID != "cust-1" {
		t.Errorf("列字段不符: %+v", rows[0])
	}
	// 快照可还原完整意图
	var snap ReachSendRequest
	raw, _ := json.Marshal(rows[0].RequestJSON)
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("snapshot unmarshal: %v", err)
	}
	if snap.Content != req.Content || snap.Params["k"] != "v" || snap.AccountID != "acc-1" {
		t.Errorf("快照丢失发送意图字段: %+v", snap)
	}
}

// 模拟重启：新队列实例（同仓储）派发到期行 → 经原管线重发成功并按 sent 收口。
func TestPersistentQuietQueue_SurvivesRestartAndReplays(t *testing.T) {
	q, repo, _ := setupDeferredQueue(t)
	ctx := context.Background()

	if err := q.Defer(ctx, deferredTestReq(), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Defer: %v", err)
	}

	// "重启"：全新的队列实例 + 全新 pipeline stub（同一仓储）
	pipe := &stubDeferredPipeline{resp: &SendResponse{Success: true, MessageID: "mh-1"}}
	q2 := NewPersistentQuietHoursQueue(repo)
	q2.dispatchDue(ctx, pipe)

	if pipe.calls != 1 {
		t.Fatalf("重启后应重发 1 次，got %d", pipe.calls)
	}
	if pipe.lastReq == nil || pipe.lastReq.Content != "安静时段攒下的主动触达" {
		t.Fatalf("重发必须按原始意图: %+v", pipe.lastReq)
	}
	// bypass 标记必须在 ctx 上（防二次判延自旋）
	if !quietHoursBypassed(pipe.lastCtx) {
		t.Fatal("重发 ctx 必须带 quiet bypass 标记")
	}

	rows, _, err := repo.ListPending(ctx, "", model.DeferredReachStatusSent, 1, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("重发成功应按 sent 收口: %v (%d)", err, len(rows))
	}
}

// 发送失败：1 次失败改排退避；连续 3 次失败判 failed 且不再重发。
func TestPersistentQuietQueue_FailureHasRetryPlan(t *testing.T) {
	q, repo, database := setupDeferredQueue(t)
	ctx := context.Background()

	if err := q.Defer(ctx, deferredTestReq(), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Defer: %v", err)
	}

	// 第 1 次失败 → pending + attempts=1
	pipe := &stubDeferredPipeline{resp: &SendResponse{Success: false, Error: "wecom api 500"}}
	q.dispatchDue(ctx, pipe)
	rows, _, _ := repo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if len(rows) != 1 || rows[0].Attempts != 1 {
		t.Fatalf("失败 1 次应回 pending 且 attempts=1，got %+v", rows)
	}
	if rows[0].LastError != "wecom api 500" {
		t.Errorf("失败原因应持久化，got %q", rows[0].LastError)
	}
	if !rows[0].SendAt.After(time.Now()) {
		t.Errorf("应有未来重试时刻，got %v", rows[0].SendAt)
	}

	// 连续到第 3 次失败 → failed 终态（每轮把退避后的 send_at 拨回过期，模拟时间流逝）
	for i := 0; i < 2; i++ {
		if err := database.Model(&model.ReachDeferredOutbound{}).Where("id = ?", rows[0].ID).
			Update("send_at", time.Now().Add(-time.Minute)).Error; err != nil {
			t.Fatalf("force due: %v", err)
		}
		pipe2 := &stubDeferredPipeline{resp: &SendResponse{Success: false, Error: "wecom api 500"}}
		q.dispatchDue(ctx, pipe2)
	}
	rows, _, _ = repo.ListPending(ctx, "", model.DeferredReachStatusFailed, 1, 10)
	if len(rows) != 1 {
		t.Fatalf("3 次失败应判 failed，got %d", len(rows))
	}

	// failed 行不再被派发
	pipe3 := &stubDeferredPipeline{resp: &SendResponse{Success: true}}
	q.dispatchDue(ctx, pipe3)
	if pipe3.calls != 0 {
		t.Fatalf("failed 行不得再重发，实际派发 %d 次", pipe3.calls)
	}
}

// 取消：pending 行取消后不发送。
func TestPersistentQuietQueue_CancelPreventsSend(t *testing.T) {
	q, repo, _ := setupDeferredQueue(t)
	ctx := context.Background()

	if err := q.Defer(ctx, deferredTestReq(), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Defer: %v", err)
	}
	rows, _, _ := repo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if len(rows) != 1 {
		t.Fatalf("应有一条 pending，got %d", len(rows))
	}
	if err := q.CancelDeferredOutbound(ctx, rows[0].ID); err != nil {
		t.Fatalf("CancelDeferredOutbound: %v", err)
	}
	pipe := &stubDeferredPipeline{resp: &SendResponse{Success: true}}
	q.dispatchDue(ctx, pipe)
	if pipe.calls != 0 {
		t.Fatalf("取消后不得重发，实际派发 %d 次", pipe.calls)
	}
	after, _, _ := repo.ListPending(ctx, "", model.DeferredReachStatusCancelled, 1, 10)
	if len(after) != 1 {
		t.Fatalf("取消后应为 cancelled，got %d", len(after))
	}
}

// 租约过期的 sending 行转 unknown，不自动重发；人工核对可收敛。
func TestPersistentQuietQueue_ExpiredLeaseGoesUnknown(t *testing.T) {
	q, repo, _ := setupDeferredQueue(t)
	ctx := context.Background()
	now := time.Now()

	rec := &model.ReachDeferredOutbound{
		Channel: "wecom", RecipientID: "cust-x",
		SendAt: now.Add(-time.Hour), Status: model.DeferredReachStatusSending,
		ClaimToken: "expired",
	}
	past := now.Add(-time.Minute)
	rec.LeaseUntil = &past
	rec.RequestJSON = model.JSONMap{"channel": "wecom"}
	if err := repo.CreatePending(ctx, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pipe := &stubDeferredPipeline{resp: &SendResponse{Success: true}}
	q.dispatchDue(ctx, pipe)
	if pipe.calls != 0 {
		t.Fatalf("租约过期行不得自动重发，实际派发 %d 次", pipe.calls)
	}
	rows, _, _ := repo.ListPending(ctx, "", model.DeferredReachStatusUnknown, 1, 10)
	if len(rows) != 1 {
		t.Fatalf("租约过期行应转 unknown，got %d", len(rows))
	}

	// 人工核对：确认没发出去 → 回 pending 等下轮重发
	if err := repo.ResolveUnknown(ctx, rec.ID, model.DeferredReachStatusPending, now, "渠道后台确认未送达"); err != nil {
		t.Fatalf("ResolveUnknown → pending: %v", err)
	}
	pipe2 := &stubDeferredPipeline{resp: &SendResponse{Success: true}}
	q.dispatchDue(ctx, pipe2)
	if pipe2.calls != 1 {
		t.Fatalf("核对回 pending 后应重发 1 次，got %d", pipe2.calls)
	}
}

// worker 改期兜底：bypass 丢失导致管线再次判延时，同一行改期而非产生重复行。
func TestPersistentQuietQueue_ReDeferredReschedulesSameRow(t *testing.T) {
	q, repo, _ := setupDeferredQueue(t)
	ctx := context.Background()

	if err := q.Defer(ctx, deferredTestReq(), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Defer: %v", err)
	}
	next := time.Now().Add(2 * time.Hour)
	pipe := &stubDeferredPipeline{resp: &SendResponse{Success: false, Deferred: true, DeferredAt: next}}
	q.dispatchDue(ctx, pipe)

	rows, total, _ := repo.ListPending(ctx, "", model.DeferredReachStatusPending, 1, 10)
	if total != 1 || len(rows) != 1 {
		t.Fatalf("再次判延必须改期同一行（不得产生重复行），got %d/%d", len(rows), total)
	}
	if !rows[0].SendAt.Equal(next) {
		t.Errorf("send_at 应改为 %v，got %v", next, rows[0].SendAt)
	}
}
