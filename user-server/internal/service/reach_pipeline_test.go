package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupReachTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.ReachPipeline{},
		&model.ReachJob{},
	)
}

// reachStubSender 测试用发送器：把"这条确实出过网"变成可断言的字面值 sent_{渠道}。
//
// 为什么每个要走完整状态机的用例都得装一个：dispatchOutbound 在没有发送器时对非 bridge
// 渠道直接判失败（旧实现是编一个 msg_ 开头的假号再回成功，那等于让作业停在"已投递"）。
// 装在这里而不是让各用例自己装，是为了让"发出去了"成为夹具的前提，而不是某个用例的运气。
type reachStubSender struct {
	mu    sync.Mutex
	calls []string
}

func (s *reachStubSender) SendReach(_ context.Context, channel, _, to, _ string) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, channel+"|"+to)
	s.mu.Unlock()
	return "sent_" + channel, nil
}

func (s *reachStubSender) sentTo() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func newReachTestService(t *testing.T) (*ReachPipelineService, *gorm.DB) {
	db := setupReachTestDB(t)
	svc := NewReachPipelineService(db)
	svc.SetReachSender(&reachStubSender{})
	return svc, db
}

func newReachPipelineReq(merchantID string) *CreatePipelineRequest {
	return &CreatePipelineRequest{

		Name:        "测试 Pipeline",
		Description: "单元测试",
		Channel:     "wecom",
		Steps:       DefaultPipelineSteps,
		RetryPolicy: DefaultRetryPolicy(),
		RateLimit:   DefaultRateLimit(),
	}
}

var reachTestCounter int64

func newReachJobReq(pipelineID uint) *EnqueueJobRequest {
	n := atomic.AddInt64(&reachTestCounter, 1)
	return &EnqueueJobRequest{
		PipelineID: pipelineID,
		Channel:    "wecom",
		CustomerID: fmt.Sprintf("customer-%d", n),
		AccountID:  fmt.Sprintf("acc-%d", n),
		Payload:    map[string]any{"content": "hello {{customer_id}}", "text": "hello"},
		MaxRetry:   3,
	}
}

func TestReachChannels_AllSupported(t *testing.T) {
	channels := []string{"wecom", "sms", "email", "card", "dingtalk", "douyin", "kuaishou", "xiaohongshu", "telegram", "whatsapp", "feishu"}
	for _, c := range channels {
		if !ReachChannels[c] {
			t.Errorf("expected %s to be valid channel", c)
		}
	}
}

func TestReachChannels_Unsupported(t *testing.T) {
	channels := []string{"", "unknown", "facebook", "twitter", "wechat", "weixin", "QQ", "邮箱", "voice", "line", "signal"}
	for _, c := range channels {
		if ReachChannels[c] {
			t.Errorf("expected %s to be invalid channel", c)
		}
	}
}

func TestDefaultPipelineSteps_Complete(t *testing.T) {
	if len(DefaultPipelineSteps) != 9 {
		t.Errorf("expected 9 steps, got %d", len(DefaultPipelineSteps))
	}
	expected := map[string]bool{
		StepAudience: true, StepContentPrepare: true, StepAccountSelect: true,
		StepRateLimit: true, StepMessageGen: true, StepSend: true,
		StepTrackResult: true, StepRetry: true, StepReport: true,
	}
	for _, s := range DefaultPipelineSteps {
		if !expected[s] {
			t.Errorf("unexpected step %s", s)
		}
		delete(expected, s)
	}
	if len(expected) > 0 {
		t.Errorf("missing steps: %v", expected)
	}
}

func TestDefaultRetryPolicy_Fields(t *testing.T) {
	rp := DefaultRetryPolicy()
	if rp.MaxRetries != 3 {
		t.Errorf("expected max_retries=3, got %d", rp.MaxRetries)
	}
	if rp.IntervalMs != 1000 {
		t.Errorf("expected interval_ms=1000, got %d", rp.IntervalMs)
	}
	if rp.Backoff != "exponential" {
		t.Errorf("expected exponential, got %s", rp.Backoff)
	}
}

func TestDefaultRateLimit_Fields(t *testing.T) {
	rl := DefaultRateLimit()
	if rl.QPS != 10 {
		t.Errorf("expected qps=10, got %d", rl.QPS)
	}
	if rl.Burst != 20 {
		t.Errorf("expected burst=20, got %d", rl.Burst)
	}
	if rl.DailyQuota != 10000 {
		t.Errorf("expected daily_quota=10000, got %d", rl.DailyQuota)
	}
}

func TestValidateSteps_AllDefault(t *testing.T) {
	svc := &ReachPipelineService{}
	if err := svc.validateSteps(context.Background(), DefaultPipelineSteps); err != nil {
		t.Errorf("default steps should be valid, got %v", err)
	}
}

func TestValidateSteps_Empty(t *testing.T) {
	svc := &ReachPipelineService{}
	if err := svc.validateSteps(context.Background(), []string{}); err == nil {
		t.Error("expected error on empty steps")
	}
}

func TestValidateSteps_UnknownStep(t *testing.T) {
	svc := &ReachPipelineService{}
	err := svc.validateSteps(context.Background(), []string{"unknown_step"})
	if err == nil {
		t.Error("expected error on unknown step")
	}
}

func TestValidateSteps_NoSend(t *testing.T) {
	svc := &ReachPipelineService{}
	steps := []string{StepAudience, StepContentPrepare, StepReport}
	err := svc.validateSteps(context.Background(), steps)
	if err == nil {
		t.Error("expected error when send is missing")
	}
}

func TestValidateSteps_MixedValid(t *testing.T) {
	svc := &ReachPipelineService{}
	steps := []string{StepAudience, StepSend, StepReport}
	if err := svc.validateSteps(context.Background(), steps); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestComputeNextRunTime_Fixed(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 3, IntervalMs: 1000, Backoff: "fixed"}
	now := time.Now()
	next := computeNextRunTime(rp, 1)
	diff := next.Sub(now)
	if diff < 900*time.Millisecond || diff > 1100*time.Millisecond {
		t.Errorf("expected ~1s, got %v", diff)
	}
}

func TestComputeNextRunTime_Exponential(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 5, IntervalMs: 1000, Backoff: "exponential", MaxIntervalMs: 60000}
	t1 := computeNextRunTime(rp, 0)
	t2 := computeNextRunTime(rp, 1)
	t3 := computeNextRunTime(rp, 2)
	t4 := computeNextRunTime(rp, 3)
	if t2.Sub(t1) >= t3.Sub(t2) {
		t.Error("expected exponential growth")
	}
	if t3.Sub(t2) >= t4.Sub(t3) {
		t.Error("expected exponential growth")
	}
}

func TestComputeNextRunTime_ExponentialCapped(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 10, IntervalMs: 1000, Backoff: "exponential", MaxIntervalMs: 5000}
	t1 := computeNextRunTime(rp, 10)
	t2 := computeNextRunTime(rp, 20)
	if time.Until(t1) > 6*time.Second {
		t.Errorf("expected capped, got %v", time.Until(t1))
	}
	if time.Until(t2) > 6*time.Second {
		t.Errorf("expected capped, got %v", time.Until(t2))
	}
}

func TestRateBucket_InitialBurst(t *testing.T) {
	b := &rateBucket{tokens: 5, lastFill: time.Now(), burst: 5, qps: 1}
	for i := 0; i < 5; i++ {
		if !b.allow(context.Background()) {
			t.Errorf("expected allow at %d", i)
		}
	}
	if b.allow(context.Background()) {
		t.Error("expected deny after burst exhausted")
	}
}

func TestRateBucket_Refill(t *testing.T) {
	b := &rateBucket{tokens: 0, lastFill: time.Now().Add(-1 * time.Second), burst: 1, qps: 10}
	if !b.allow(context.Background()) {
		t.Error("expected allow after refill")
	}
}

func TestRateBucket_BurstBounded(t *testing.T) {
	b := &rateBucket{tokens: 5, lastFill: time.Now().Add(-10 * time.Second), burst: 3, qps: 100}
	for i := 0; i < 10; i++ {
		b.allow(context.Background())
	}
	if b.tokens > float64(b.burst) {
		t.Errorf("expected tokens <= burst, got %f", b.tokens)
	}
}

func TestCreatePipeline_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, err := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if pipe.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if pipe.Status != PipelineStatusActive {
		t.Errorf("expected active, got %s", pipe.Status)
	}
}

func TestCreatePipeline_DefaultSteps(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.Steps = nil
	pipe, err := svc.CreatePipeline(context.Background(), req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(pipe.Steps) != 9 {
		t.Errorf("expected 9 default steps, got %d", len(pipe.Steps))
	}
}

func TestCreatePipeline_InvalidChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.Channel = "unknown"
	_, err := svc.CreatePipeline(context.Background(), req)
	if err != ErrReachInvalidChannel {
		t.Errorf("expected ErrReachInvalidChannel, got %v", err)
	}
}

// TestCreatePipeline_NoMerchant 单租户创建验证
// 单租户私有部署：创建 Pipeline 不再需要 merchant_id，应正常成功。
func TestCreatePipeline_NoMerchant(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("")
	pipe, err := svc.CreatePipeline(context.Background(), req)
	if err != nil {
		t.Fatalf("CreatePipeline in single-tenant should succeed without merchant_id, got: %v", err)
	}
	if pipe.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestCreatePipeline_NilDB(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.CreatePipeline(context.Background(), newReachPipelineReq("m"))
	if err == nil {
		t.Error("expected error with nil db")
	}
}

func TestCreatePipeline_AllChannels(t *testing.T) {
	channels := []string{"wecom", "sms", "email", "card", "dingtalk"}
	for _, c := range channels {
		svc, _ := newReachTestService(t)
		req := newReachPipelineReq("m-001")
		req.Channel = c
		pipe, err := svc.CreatePipeline(context.Background(), req)
		if err != nil {
			t.Errorf("channel %s: %v", c, err)
		}
		if pipe.Channel != c {
			t.Errorf("expected %s, got %s", c, pipe.Channel)
		}
	}
}

func TestCreatePipeline_VersionIncrement(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if pipe.Version != 1 {
		t.Errorf("expected version=1, got %d", pipe.Version)
	}
}

func TestUpdatePipeline_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachPipelineReq("m-001")
	req.Name = "更新后"
	updated, err := svc.UpdatePipeline(context.Background(), pipe.ID, req)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "更新后" {
		t.Errorf("expected updated name, got %s", updated.Name)
	}
	if updated.Version <= pipe.Version {
		t.Errorf("expected version increment, got %d -> %d", pipe.Version, updated.Version)
	}
}

func TestUpdatePipeline_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, err := svc.UpdatePipeline(context.Background(), 999, newReachPipelineReq("m-001"))
	if err != ErrReachPipelineNotFound {
		t.Errorf("expected ErrReachPipelineNotFound, got %v", err)
	}
}

func TestUpdatePipeline_InvalidChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachPipelineReq("m-001")
	req.Channel = "unknown"
	_, err := svc.UpdatePipeline(context.Background(), pipe.ID, req)
	if err != ErrReachInvalidChannel {
		t.Errorf("expected ErrReachInvalidChannel, got %v", err)
	}
}

func TestGetPipeline_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	got, err := svc.GetPipeline(context.Background(), pipe.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != pipe.ID {
		t.Errorf("expected %d, got %d", pipe.ID, got.ID)
	}
}

func TestGetPipeline_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, err := svc.GetPipeline(context.Background(), 999)
	if err != ErrReachPipelineNotFound {
		t.Errorf("expected ErrReachPipelineNotFound, got %v", err)
	}
}

// TestGetPipeline_SingleTenant 单租户访问验证
// 单租户私有部署：所有 Pipeline 归当前部署实例所有，GetPipeline 不做跨租户校验。
func TestGetPipeline_SingleTenant(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, err := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.GetPipeline(context.Background(), pipe.ID)
	if err != nil {
		t.Fatalf("GetPipeline should succeed in single-tenant mode, got: %v", err)
	}
	if got == nil || got.ID != pipe.ID {
		t.Errorf("Expected pipeline ID %d, got %v", pipe.ID, got)
	}
}

func TestListPipelines_All(t *testing.T) {
	svc, _ := newReachTestService(t)
	for i := 0; i < 5; i++ {
		svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	}
	list, total, err := svc.ListPipelines(context.Background(), "", "", 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(list) != 5 {
		t.Errorf("expected 5 items, got %d", len(list))
	}
}

func TestListPipelines_ByChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	req1 := newReachPipelineReq("m-001")
	req1.Channel = "wecom"
	svc.CreatePipeline(context.Background(), req1)
	req2 := newReachPipelineReq("m-001")
	req2.Channel = "sms"
	svc.CreatePipeline(context.Background(), req2)
	list, total, _ := svc.ListPipelines(context.Background(), "sms", "", 1, 10)
	if total != 1 {
		t.Errorf("expected 1, got %d", total)
	}
	if len(list) != 1 || list[0].Channel != "sms" {
		t.Errorf("expected sms, got %v", list)
	}
}

func TestListPipelines_ByStatus(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.PausePipeline(context.Background(), pipe.ID)
	_, total, _ := svc.ListPipelines(context.Background(), "", PipelineStatusPaused, 1, 10)
	if total != 1 {
		t.Errorf("expected 1 paused, got %d", total)
	}
}

func TestListPipelines_Pagination(t *testing.T) {
	svc, _ := newReachTestService(t)
	for i := 0; i < 25; i++ {
		svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	}
	list, total, _ := svc.ListPipelines(context.Background(), "", "", 1, 10)
	if total != 25 {
		t.Errorf("expected total=25, got %d", total)
	}
	if len(list) != 10 {
		t.Errorf("expected 10 per page, got %d", len(list))
	}
}

func TestListPipelines_EmptyMerchant(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, total, _ := svc.ListPipelines(context.Background(), "", "", 1, 10)
	if total != 0 {
		t.Errorf("expected 0, got %d", total)
	}
}

func TestListPipelines_PageBounds(t *testing.T) {
	svc, _ := newReachTestService(t)
	svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	_, _, err := svc.ListPipelines(context.Background(), "", "", 0, 0)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestListPipelines_LargePageSize(t *testing.T) {
	svc, _ := newReachTestService(t)
	svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	_, _, err := svc.ListPipelines(context.Background(), "", "", 1, 300)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeletePipeline_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if err := svc.DeletePipeline(context.Background(), pipe.ID); err != nil {
		t.Errorf("delete: %v", err)
	}
	_, err := svc.GetPipeline(context.Background(), pipe.ID)
	if err != ErrReachPipelineNotFound {
		t.Errorf("expected not found after delete, got %v", err)
	}
}

func TestDeletePipeline_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	if err := svc.DeletePipeline(context.Background(), 999); err != ErrReachPipelineNotFound {
		t.Errorf("expected ErrReachPipelineNotFound, got %v", err)
	}
}

func TestPausePipeline(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if err := svc.PausePipeline(context.Background(), pipe.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	var got model.ReachPipeline
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusPaused {
		t.Errorf("expected paused, got %s", got.Status)
	}
}

func TestResumePipeline(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.PausePipeline(context.Background(), pipe.ID)
	if err := svc.ResumePipeline(context.Background(), pipe.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	var got model.ReachPipeline
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusActive {
		t.Errorf("expected active, got %s", got.Status)
	}
}

func TestArchivePipeline(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	if err := svc.ArchivePipeline(context.Background(), pipe.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	var got model.ReachPipeline
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusArchived {
		t.Errorf("expected archived, got %s", got.Status)
	}
}

func TestEnqueueJob_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, err := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.State != JobStatePending {
		t.Errorf("expected pending, got %s", job.State)
	}
}

func TestEnqueueJob_NilPayload(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachJobReq(pipe.ID)
	req.Payload = nil
	_, err := svc.EnqueueJob(context.Background(), req)
	if err != ErrReachInvalidPayload {
		t.Errorf("expected ErrReachInvalidPayload, got %v", err)
	}
}

func TestEnqueueJob_PipelineNotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, err := svc.EnqueueJob(context.Background(), newReachJobReq(999))
	if err != ErrReachPipelineNotFound {
		t.Errorf("expected ErrReachPipelineNotFound, got %v", err)
	}
}

func TestEnqueueJob_PipelinePaused(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.PausePipeline(context.Background(), pipe.ID)
	_, err := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	if err == nil {
		t.Error("expected error when pipeline paused")
	}
}

func TestEnqueueJob_InvalidChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachJobReq(pipe.ID)
	req.Channel = "unknown"
	_, err := svc.EnqueueJob(context.Background(), req)
	if err != ErrReachInvalidChannel {
		t.Errorf("expected ErrReachInvalidChannel, got %v", err)
	}
}

func TestEnqueueJob_DefaultChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachJobReq(pipe.ID)
	req.Channel = ""
	job, err := svc.EnqueueJob(context.Background(), req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.Channel != "wecom" {
		t.Errorf("expected wecom default, got %s", job.Channel)
	}
}

func TestEnqueueJob_DefaultMaxRetry(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	pipe.RetryPolicy = model.JSONMap{}
	db := setupReachTestDB(t)
	db.Save(pipe)
	pipe2, _ := svc.GetPipeline(context.Background(), pipe.ID)
	_ = pipe2
	req := newReachJobReq(pipe.ID)
	req.MaxRetry = 0
	job, _ := svc.EnqueueJob(context.Background(), req)
	if job.MaxRetry <= 0 {
		t.Errorf("expected positive max_retry, got %d", job.MaxRetry)
	}
}

func TestEnqueueJob_WithRunAt(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	runAt := time.Now().Add(1 * time.Hour)
	req := newReachJobReq(pipe.ID)
	req.RunAt = &runAt
	job, err := svc.EnqueueJob(context.Background(), req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.NextRunAt == nil || !job.NextRunAt.Equal(runAt) {
		t.Errorf("expected run_at preserved, got %v", job.NextRunAt)
	}
}

func TestEnqueueJob_DefaultNextRunAt(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachJobReq(pipe.ID)
	req.RunAt = nil
	job, err := svc.EnqueueJob(context.Background(), req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.NextRunAt == nil {
		t.Error("expected default next_run_at")
	}
}

func TestGetJob_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	got, err := svc.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != job.ID {
		t.Errorf("expected %d, got %d", job.ID, got.ID)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, err := svc.GetJob(context.Background(), 999)
	if err != ErrReachJobNotFound {
		t.Errorf("expected ErrReachJobNotFound, got %v", err)
	}
}

func TestListJobs_All(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	for i := 0; i < 5; i++ {
		svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	}
	list, total, _ := svc.ListJobs(context.Background(), "", "", 1, 10)
	if total != 5 {
		t.Errorf("expected 5, got %d", total)
	}
	if len(list) != 5 {
		t.Errorf("expected 5 items, got %d", len(list))
	}
}

func TestListJobs_ByState(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.CancelJob(context.Background(), job.ID)
	list, total, _ := svc.ListJobs(context.Background(), "", JobStateCanceled, 1, 10)
	if total != 1 {
		t.Errorf("expected 1, got %d", total)
	}
	if list[0].State != JobStateCanceled {
		t.Errorf("expected canceled, got %s", list[0].State)
	}
}

func TestListJobs_ByChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe1, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req1 := newReachPipelineReq("m-001")
	req1.Channel = "sms"
	pipe2, _ := svc.CreatePipeline(context.Background(), req1)
	j1 := newReachJobReq(pipe1.ID)
	j1.Channel = "wecom"
	svc.EnqueueJob(context.Background(), j1)
	j2 := newReachJobReq(pipe2.ID)
	j2.Channel = "sms"
	svc.EnqueueJob(context.Background(), j2)
	_, total, _ := svc.ListJobs(context.Background(), "sms", "", 1, 10)
	if total != 1 {
		t.Errorf("expected 1 sms job, got %d", total)
	}
}

func TestListJobs_Pagination(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	for i := 0; i < 25; i++ {
		svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	}
	list, total, _ := svc.ListJobs(context.Background(), "", "", 1, 10)
	if total != 25 {
		t.Errorf("expected 25, got %d", total)
	}
	if len(list) != 10 {
		t.Errorf("expected 10 per page, got %d", len(list))
	}
}

func TestListJobs_EmptyMerchant(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, total, _ := svc.ListJobs(context.Background(), "", "", 1, 10)
	if total != 0 {
		t.Errorf("expected 0, got %d", total)
	}
}

func TestCancelJob_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	if err := svc.CancelJob(context.Background(), job.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	got, _ := svc.GetJob(context.Background(), job.ID)
	if got.State != JobStateCanceled {
		t.Errorf("expected canceled, got %s", got.State)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at set")
	}
}

func TestCancelJob_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	err := svc.CancelJob(context.Background(), 999)
	if err != ErrReachJobNotPending {
		t.Errorf("expected ErrReachJobNotPending, got %v", err)
	}
}

func TestCancelJob_AlreadySucceeded(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.ExecuteJob(context.Background(), job.ID)
	err := svc.CancelJob(context.Background(), job.ID)
	if err != ErrReachJobNotPending {
		t.Errorf("expected ErrReachJobNotPending, got %v", err)
	}
}

func TestRetryJob_Success(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	db.Model(&model.ReachJob{}).Where("id = ?", job.ID).Update("state", JobStateFailed)
	if err := svc.RetryJob(context.Background(), job.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got, _ := svc.GetJob(context.Background(), job.ID)
	if got.State != JobStatePending {
		t.Errorf("expected pending, got %s", got.State)
	}
}

func TestRetryJob_NotFailed(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	err := svc.RetryJob(context.Background(), job.ID)
	if err != ErrReachJobNotPending {
		t.Errorf("expected ErrReachJobNotPending, got %v", err)
	}
}

func TestRetryJob_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	err := svc.RetryJob(context.Background(), 999)
	if err != ErrReachJobNotPending {
		t.Errorf("expected ErrReachJobNotPending, got %v", err)
	}
}

func TestExecuteJob_Success(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if executed.State != JobStateSuccess {
		t.Errorf("expected success, got %s", executed.State)
	}
	if executed.DurationMs < 0 {
		t.Errorf("expected positive duration, got %d", executed.DurationMs)
	}
}

func TestExecuteJob_NotFound(t *testing.T) {
	svc, _ := newReachTestService(t)
	_, err := svc.ExecuteJob(context.Background(), 999)
	if err != ErrReachJobNotFound {
		t.Errorf("expected ErrReachJobNotFound, got %v", err)
	}
}

func TestExecuteJob_AlreadySuccess(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.ExecuteJob(context.Background(), job.ID)
	_, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != ErrReachJobNotPending {
		t.Errorf("expected ErrReachJobNotPending, got %v", err)
	}
}

func TestExecuteJob_StepResultsRecorded(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	if len(executed.StepResults) == 0 {
		t.Error("expected step results")
	}
	results := []StepResult{}
	_ = jsonUnmarshal(toBytes(executed.StepResults), &results)
	if len(results) == 0 {
		t.Fatal("expected at least 1 step result")
	}
	if results[0].Step != StepAudience {
		t.Errorf("expected first step audience, got %s", results[0].Step)
	}
}

func TestExecuteJob_PipelineStats(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	for i := 0; i < 3; i++ {
		job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
		svc.ExecuteJob(context.Background(), job.ID)
	}
	got, _ := svc.GetPipeline(context.Background(), pipe.ID)
	if got.TotalRuns != 3 {
		t.Errorf("expected total_runs=3, got %d", got.TotalRuns)
	}
	if got.TotalSuccess != 3 {
		t.Errorf("expected total_success=3, got %d", got.TotalSuccess)
	}
}

func TestExecuteJob_PipelineNotFound(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	db.WithContext(context.Background()).Delete(pipe)
	_, err := svc.ExecuteJob(context.Background(), job.ID)
	if err == nil {
		t.Error("expected error when pipeline deleted")
	}
}

func TestExecuteJob_RateLimited(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.RateLimit.DailyQuota = 1
	pipe, _ := svc.CreatePipeline(context.Background(), req)
	j1 := newReachJobReq(pipe.ID)
	j1.AccountID = "shared-acc"
	job1, _ := svc.EnqueueJob(context.Background(), j1)
	svc.ExecuteJob(context.Background(), job1.ID)
	j2 := newReachJobReq(pipe.ID)
	j2.AccountID = "shared-acc"
	job2, _ := svc.EnqueueJob(context.Background(), j2)
	executed, err := svc.ExecuteJob(context.Background(), job2.ID)
	if err == nil {
		t.Error("expected rate limit error")
	}
	if executed == nil || executed.State != JobStateRateLimited {
		t.Errorf("expected rate_limited, got %v", executed)
	}
}

func TestCheckRateLimit_NoLimit(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{}
	if !svc.checkRateLimit(context.Background(), "wecom", "acc", "u-1", &rl, false) {
		t.Error("expected allow when no limits set")
	}
}

func TestCheckRateLimit_DailyQuotaExceeded(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{DailyQuota: 2}
	svc.checkRateLimit(context.Background(), "wecom", "acc1", "u-1", &rl, false)
	svc.checkRateLimit(context.Background(), "wecom", "acc2", "u-1", &rl, false)
	if svc.checkRateLimit(context.Background(), "wecom", "acc3", "u-1", &rl, false) {
		t.Error("expected deny after quota exceeded")
	}
}

func TestCheckRateLimit_DailyReset(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{DailyQuota: 1}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	svc.dailyQuotaMu.Lock()
	for k := range svc.dailyQuota {
		svc.dailyQuota[k] = &dailyCounter{date: "2020-01-01", count: 0}
	}
	svc.dailyQuotaMu.Unlock()
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow after day reset")
	}
}

func TestCheckRateLimit_PerUserExceeded(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{PerUserLimit: 2, CooldownSecs: 60}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected deny after per-user limit")
	}
}

// TestCheckRateLimit_PerUserDifferent 测试不同用户限流独立
// 同一用户第 2 次调用应被限流（PerUserLimit=1），不同用户应被允许。
func TestCheckRateLimit_PerUserDifferent(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{PerUserLimit: 1, CooldownSecs: 60}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow for first call")
	}
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected deny for same user second call")
	}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-2", &rl, false) {
		t.Error("expected allow for different user")
	}
}

func TestCheckRateLimit_PerUserCooldown(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{PerUserLimit: 1, CooldownSecs: 0}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow after cooldown=0")
	}
}

func TestCheckRateLimit_QPSExceeded(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{QPS: 1, Burst: 1}
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow for first call")
	}
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected deny for second call in same second")
	}
}

func TestCheckRateLimit_DifferentAccount(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{QPS: 1, Burst: 1}
	svc.checkRateLimit(context.Background(), "wecom", "acc1", "u-1", &rl, false)
	if !svc.checkRateLimit(context.Background(), "wecom", "acc2", "u-1", &rl, false) {
		t.Error("expected allow for different account")
	}
}

func TestResetRateLimit(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{QPS: 1, Burst: 1}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	if svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected deny before reset")
	}
	svc.ResetRateLimit(context.Background(), "wecom")
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow after reset")
	}
}

func TestConsumeDailyQuota(t *testing.T) {
	svc := NewReachPipelineService(nil)
	if !svc.ConsumeDailyQuota(context.Background(), "wecom") {
		t.Error("expected allow")
	}
	if !svc.ConsumeDailyQuota(context.Background(), "wecom") {
		t.Error("expected allow second")
	}
}

func TestRunStep_Audience_NoCustomer(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{CustomerID: ""}
	res := svc.runStep(context.Background(), StepAudience, job, &RateLimitConfig{})
	if res.Success {
		t.Error("expected failure on empty customer_id")
	}
}

func TestRunStep_Audience_WithCustomer(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{CustomerID: "user-1"}
	res := svc.runStep(context.Background(), StepAudience, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if res.Output["customer_id"] != "user-1" {
		t.Errorf("expected customer_id, got %v", res.Output)
	}
}

// V3 整改：StepContentPrepare 现在要求 payload.content 或 payload.template_id
// 至少有一个，否则返回明确错误（不允许静默 no-op）。
func TestRunStep_ContentPrepare(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		Payload: model.JSONMap{"content": "Hello {{customer_id}}"},
	}
	res := svc.runStep(context.Background(), StepContentPrepare, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if res.Output["content"] != "Hello " {
		t.Errorf("expected rendered content 'Hello ', got %v", res.Output["content"])
	}
}

func TestRunStep_AccountSelect_Empty(t *testing.T) {
	svc := NewReachPipelineService(nil)
	res := svc.runStep(context.Background(), StepAccountSelect, &model.ReachJob{}, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if res.Output["account_id"] != "auto" {
		t.Errorf("expected auto, got %v", res.Output)
	}
}

func TestRunStep_AccountSelect_WithAccount(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{AccountID: "acc-1"}
	res := svc.runStep(context.Background(), StepAccountSelect, job, &RateLimitConfig{})
	if res.Output["account_id"] != "acc-1" {
		t.Errorf("expected acc-1, got %v", res.Output)
	}
}

// V3 整改：StepMessageGen 现在基于 ContentPrepare 真实实现，
// 要求 payload.content + customer_id 至少非空。
func TestRunStep_MessageGen(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "user-1",
		Channel:    "wecom",
		Payload:    model.JSONMap{"content": "Hi {{customer_id}}"},
	}
	res := svc.runStep(context.Background(), StepMessageGen, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if msg, _ := res.Output["message"].(string); msg == "" {
		t.Errorf("expected message in output, got %v", res.Output)
	}
}

// V3 整改：StepSend 现在按 channel 路由，必须指定合法 channel + customer_id。
// 这里显式装发送器：没装的话这一步该判失败（见 TestDispatchOutbound_WithoutSenderRefuses），
// 本用例断言的是"装好之后这一步交回发送器给的消息号"。
func TestRunStep_Send(t *testing.T) {
	svc := NewReachPipelineService(nil)
	svc.SetReachSender(&reachStubSender{})
	job := &model.ReachJob{
		Channel:    "wecom",
		CustomerID: "user-1",
	}
	res := svc.runStep(context.Background(), StepSend, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if res.Output["message_id"] != "sent_wecom" {
		t.Errorf("message_id 应是发送器交回的号，got %v", res.Output["message_id"])
	}
}

// V3 整改：StepTrackResult 写入 _tracking 字段。
func TestRunStep_TrackResult(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		ID:         1,
		Channel:    "wecom",
		CustomerID: "user-1",
		Payload:    model.JSONMap{},
	}
	res := svc.runStep(context.Background(), StepTrackResult, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	tracking, _ := job.Payload["_tracking"].(map[string]any)
	if tracking == nil {
		t.Errorf("expected _tracking in payload, got %v", job.Payload)
	}
	if tracking["tracked_at"] == nil {
		t.Errorf("expected tracked_at in tracking, got %v", tracking)
	}
}

func TestRunStep_Retry(t *testing.T) {
	svc := NewReachPipelineService(nil)
	res := svc.runStep(context.Background(), StepRetry, &model.ReachJob{}, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
}

// V3 整改：StepReport 现在聚合 StepResults，需要非空结果集。
func TestRunStep_Report(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		ID:         1,
		PipelineID: 1,
		Channel:    "wecom",
		CustomerID: "user-1",
		StepResults: model.JSONArray{
			map[string]any{
				"step":        StepAudience,
				"success":     true,
				"duration_ms": 10,
			},
			map[string]any{
				"step":        StepSend,
				"success":     true,
				"duration_ms": 100,
			},
		},
	}
	res := svc.runStep(context.Background(), StepReport, job, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
	if res.Output["total_steps"] == nil {
		t.Errorf("expected total_steps in output, got %v", res.Output)
	}
	if res.Output["success_steps"] == nil {
		t.Errorf("expected success_steps in output, got %v", res.Output)
	}
}

func TestRunStep_Unknown(t *testing.T) {
	svc := NewReachPipelineService(nil)
	res := svc.runStep(context.Background(), "unknown", &model.ReachJob{}, &RateLimitConfig{})
	if res.Success {
		t.Error("expected failure on unknown step")
	}
}

func TestRunStep_RateLimit_Pass(t *testing.T) {
	svc := NewReachPipelineService(nil)
	res := svc.runStep(context.Background(), StepRateLimit, &model.ReachJob{CustomerID: "u"}, &RateLimitConfig{})
	if !res.Success {
		t.Errorf("expected success, got %v", res)
	}
}

func TestRunStep_RateLimit_Deny(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{QPS: 1, Burst: 1}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	job := &model.ReachJob{Channel: "wecom", AccountID: "a", CustomerID: "u"}
	res := svc.runStep(context.Background(), StepRateLimit, job, &rl)
	if res.Success {
		t.Error("expected failure when rate limited")
	}
}

func TestStats_ReachEmpty(t *testing.T) {
	svc, _ := newReachTestService(t)
	stats, _ := svc.Stats(context.Background())
	if stats["total"] != 0 {
		t.Errorf("expected 0, got %d", stats["total"])
	}
}

func TestStats_WithData(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	pipe2, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.PausePipeline(context.Background(), pipe2.ID)
	job1, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.ExecuteJob(context.Background(), job1.ID)
	stats, _ := svc.Stats(context.Background())
	if stats["total"] != 2 {
		t.Errorf("expected total=2, got %d", stats["total"])
	}
	if stats["active"] != 1 {
		t.Errorf("expected active=1, got %d", stats["active"])
	}
	if stats["paused"] != 1 {
		t.Errorf("expected paused=1, got %d", stats["paused"])
	}
	if stats["success"] != 1 {
		t.Errorf("expected success=1, got %d", stats["success"])
	}
}

func TestStats_ReachNilDB(t *testing.T) {
	svc := NewReachPipelineService(nil)
	stats, _ := svc.Stats(context.Background())
	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestStats_RateLimitedCount(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.RateLimit.DailyQuota = 1
	pipe, _ := svc.CreatePipeline(context.Background(), req)
	j1 := newReachJobReq(pipe.ID)
	j1.AccountID = "shared-acc"
	job1, _ := svc.EnqueueJob(context.Background(), j1)
	svc.ExecuteJob(context.Background(), job1.ID)
	j2 := newReachJobReq(pipe.ID)
	j2.AccountID = "shared-acc"
	job, _ := svc.EnqueueJob(context.Background(), j2)
	svc.ExecuteJob(context.Background(), job.ID)
	stats, _ := svc.Stats(context.Background())
	if stats["rate_limited"] != 1 {
		t.Errorf("expected rate_limited=1, got %d", stats["rate_limited"])
	}
}

func TestStats_CanceledCount(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.CancelJob(context.Background(), job.ID)
	stats, _ := svc.Stats(context.Background())
	if stats["canceled"] != 1 {
		t.Errorf("expected canceled=1, got %d", stats["canceled"])
	}
}

func TestPipelineStateMachine_ActiveToPausedToActive(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.PausePipeline(context.Background(), pipe.ID)
	var got model.ReachPipeline
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusPaused {
		t.Errorf("expected paused, got %s", got.Status)
	}
	svc.ResumePipeline(context.Background(), pipe.ID)
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusActive {
		t.Errorf("expected active, got %s", got.Status)
	}
}

func TestPipelineStateMachine_ActiveToArchived(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.ArchivePipeline(context.Background(), pipe.ID)
	var got model.ReachPipeline
	db.First(&got, pipe.ID)
	if got.Status != PipelineStatusArchived {
		t.Errorf("expected archived, got %s", got.Status)
	}
	_, err := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	if err == nil {
		t.Error("expected error when enqueuing to archived pipeline")
	}
}

func TestJobStateMachine_PendingToSuccess(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	if executed.State != JobStateSuccess {
		t.Errorf("expected success, got %s", executed.State)
	}
}

func TestJobStateMachine_PendingToCanceled(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.CancelJob(context.Background(), job.ID)
	got, _ := svc.GetJob(context.Background(), job.ID)
	if got.State != JobStateCanceled {
		t.Errorf("expected canceled, got %s", got.State)
	}
}

func TestJobStateMachine_FailedToPending(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	db.Model(&model.ReachJob{}).Where("id = ?", job.ID).Update("state", JobStateFailed)
	svc.RetryJob(context.Background(), job.ID)
	got, _ := svc.GetJob(context.Background(), job.ID)
	if got.State != JobStatePending {
		t.Errorf("expected pending, got %s", got.State)
	}
}

func TestJobStateMachine_RateLimitedToPending(t *testing.T) {
	svc, db := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.RateLimit.DailyQuota = 1
	pipe, _ := svc.CreatePipeline(context.Background(), req)
	j1 := newReachJobReq(pipe.ID)
	j1.AccountID = "shared-acc"
	job1, _ := svc.EnqueueJob(context.Background(), j1)
	svc.ExecuteJob(context.Background(), job1.ID)
	j2 := newReachJobReq(pipe.ID)
	j2.AccountID = "shared-acc"
	job, _ := svc.EnqueueJob(context.Background(), j2)
	svc.ExecuteJob(context.Background(), job.ID)
	got, _ := svc.GetJob(context.Background(), job.ID)
	if got.State != JobStateRateLimited {
		t.Errorf("expected rate_limited, got %s", got.State)
	}
	db.Model(&model.ReachJob{}).Where("id = ?", job.ID).Update("state", JobStatePending)
	svc.ResetRateLimit(context.Background(), "wecom")
	got2, _ := svc.GetJob(context.Background(), job.ID)
	if got2.State != JobStatePending {
		t.Errorf("expected pending, got %s", got2.State)
	}
}

func TestExecuteJob_Concurrent(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	jobs := []uint{}
	for i := 0; i < 10; i++ {
		j, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
		jobs = append(jobs, j.ID)
	}
	var wg sync.WaitGroup
	for _, id := range jobs {
		wg.Add(1)
		go func(id uint) {
			defer wg.Done()
			svc.ExecuteJob(context.Background(), id)
		}(id)
	}
	wg.Wait()
	stats, _ := svc.Stats(context.Background())
	if stats["success"] != 10 {
		t.Errorf("expected 10 success, got %d", stats["success"])
	}
}

func TestCreatePipeline_Concurrent(t *testing.T) {
	svc, _ := newReachTestService(t)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
		}()
	}
	wg.Wait()
	list, total, _ := svc.ListPipelines(context.Background(), "", "", 1, 100)
	if total != 10 {
		t.Errorf("expected 10, got %d", total)
	}
	if len(list) != 10 {
		t.Errorf("expected 10 items, got %d", len(list))
	}
}

func TestEnqueueJob_Concurrent(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
		}()
	}
	wg.Wait()
	_, total, _ := svc.ListJobs(context.Background(), "", "", 1, 100)
	if total != 20 {
		t.Errorf("expected 20, got %d", total)
	}
}

func TestCheckRateLimit_Concurrent(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{QPS: 100, Burst: 50, PerUserLimit: 100, CooldownSecs: 60}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.checkRateLimit(context.Background(), "wecom", "acc", "u-1", &rl, false)
		}()
	}
	wg.Wait()
}

func TestCreatePipeline_EmptyName(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.Name = ""
	pipe, err := svc.CreatePipeline(context.Background(), req)
	if err != nil {
		t.Errorf("expected create to succeed, got %v", err)
	}
	_ = pipe
}

func TestCreatePipeline_CustomSteps(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.Steps = []string{StepAudience, StepSend, StepReport}
	pipe, err := svc.CreatePipeline(context.Background(), req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(pipe.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(pipe.Steps))
	}
}

func TestUpdatePipeline_ReplaceSteps(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachPipelineReq("m-001")
	req.Steps = []string{StepAudience, StepSend, StepReport}
	updated, _ := svc.UpdatePipeline(context.Background(), pipe.ID, req)
	if len(updated.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(updated.Steps))
	}
}

func TestExecuteJob_WithCustomSteps(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-001")
	req.Steps = []string{StepAudience, StepSend, StepReport}
	pipe, _ := svc.CreatePipeline(context.Background(), req)
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	if executed.State != JobStateSuccess {
		t.Errorf("expected success, got %s", executed.State)
	}
	if len(executed.StepResults) != 3 {
		t.Errorf("expected 3 step results, got %d", len(executed.StepResults))
	}
}

func TestExecuteJob_PipelineNotFoundRace(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	_, err := svc.ExecuteJob(context.Background(), job.ID+999)
	if err == nil {
		t.Error("expected error")
	}
}

func TestEnqueueJob_NilDB(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.EnqueueJob(context.Background(), &EnqueueJobRequest{

		PipelineID: 1,
		CustomerID: "u",
		Payload:    map[string]any{"k": "v"},
	})
	if err == nil {
		t.Error("expected error with nil db")
	}
}

func TestEnqueueJob_ZeroMaxRetry_Fallback(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	req := newReachJobReq(pipe.ID)
	req.MaxRetry = 0
	job, err := svc.EnqueueJob(context.Background(), req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.MaxRetry <= 0 {
		t.Errorf("expected fallback max_retry, got %d", job.MaxRetry)
	}
}

func TestListJobs_PageBoundaries(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	_, _, err := svc.ListJobs(context.Background(), "", "", 0, 0)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestListPipelines_PageBoundaries(t *testing.T) {
	svc, _ := newReachTestService(t)
	svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	_, _, err := svc.ListPipelines(context.Background(), "", "", -1, 500)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestResetRateLimit_EmptyChannel(t *testing.T) {
	svc := NewReachPipelineService(nil)
	svc.ResetRateLimit(context.Background(), "")
}

func TestCheckRateLimit_AfterReset(t *testing.T) {
	svc := NewReachPipelineService(nil)
	rl := RateLimitConfig{DailyQuota: 1}
	svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false)
	svc.ResetRateLimit(context.Background(), "wecom")
	if !svc.checkRateLimit(context.Background(), "wecom", "a", "u-1", &rl, false) {
		t.Error("expected allow after reset")
	}
}

func TestExecuteJob_AfterPipelineDeleted(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	db.WithContext(context.Background()).Delete(pipe)
	_, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != ErrReachPipelineNotFound {
		t.Errorf("expected ErrReachPipelineNotFound, got %v", err)
	}
}

func TestComputeNextRunTime_FirstRetry(t *testing.T) {
	rp := RetryPolicy{MaxRetries: 3, IntervalMs: 500, Backoff: "fixed"}
	now := time.Now()
	next := computeNextRunTime(rp, 1)
	diff := next.Sub(now)
	if diff < 400*time.Millisecond {
		t.Errorf("expected ~500ms, got %v", diff)
	}
}

func TestExecuteJob_StartEndTime(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	if executed.StartedAt == nil {
		t.Error("expected started_at")
	}
	if executed.CompletedAt == nil {
		t.Error("expected completed_at")
	}
	if executed.DurationMs < 0 {
		t.Errorf("expected positive duration, got %d", executed.DurationMs)
	}
}

func TestExecuteJob_StepOrder(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	expected := DefaultPipelineSteps
	if len(executed.StepResults) != len(expected) {
		t.Fatalf("expected %d steps, got %d", len(expected), len(executed.StepResults))
	}
	results := []StepResult{}
	_ = jsonUnmarshal(toBytes(executed.StepResults), &results)
	for i, sr := range results {
		if sr.Step != expected[i] {
			t.Errorf("step %d: expected %s, got %s", i, expected[i], sr.Step)
		}
	}
}

func TestExecuteJob_AllStepsSuccessful(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	executed, _ := svc.ExecuteJob(context.Background(), job.ID)
	results := []StepResult{}
	_ = jsonUnmarshal(toBytes(executed.StepResults), &results)
	for i, sr := range results {
		if !sr.Success {
			t.Errorf("step %d (%s): expected success, got error %s", i, sr.Step, sr.Error)
		}
	}
}

func TestAppendStepResult(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-001"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	svc.appendStepResult(context.Background(), job, StepResult{Step: "test", Success: true})
	got, _ := svc.GetJob(context.Background(), job.ID)
	if len(got.StepResults) == 0 {
		t.Error("expected step results appended")
	}
	_ = db
}

func toBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func TestPrepareContent_NilJob(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.prepareContent(context.Background(), nil)
	if err == nil {
		t.Error("expected error on nil job")
	}
}

func TestPrepareContent_NoContent(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.prepareContent(context.Background(), &model.ReachJob{Payload: model.JSONMap{}})
	if err == nil {
		t.Error("expected error on empty payload")
	}
}

func TestPrepareContent_FromStringTemplate(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "u-1",
		Channel:    "wecom",
		Payload:    model.JSONMap{"content": "Hi {{customer_id}}, welcome to {{channel}}!"},
	}
	got, err := svc.prepareContent(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Hi u-1, welcome to wecom!"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestPrepareContent_FromPayloadVars(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "u-1",
		Channel:    "sms",
		Payload: model.JSONMap{
			"content": "Hi {{name}}, your code is {{code}}",
			"name":    "Alice",
			"code":    12345,
		},
	}
	got, err := svc.prepareContent(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hi Alice, your code is 12345" {
		t.Errorf("got %q", got)
	}
}

func TestPrepareContent_UnfilledKeyPreserved(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		Payload: model.JSONMap{"content": "Hello {{unknown_key}}"},
	}
	got, err := svc.prepareContent(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hello {{unknown_key}}" {
		t.Errorf("expected unfilled key preserved, got %q", got)
	}
}

func TestPrepareContent_EmptyString(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{Payload: model.JSONMap{"content": ""}}
	_, err := svc.prepareContent(context.Background(), job)
	if err == nil {
		t.Error("expected error on empty content")
	}
}

func TestGenerateMessage_TrimsAndFolds(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "u-1",
		Payload:    model.JSONMap{"content": "  Hello\n\n   World  \n\n"},
	}
	got, err := svc.generateMessage(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Hello World" {
		t.Errorf("expected trimmed/folded message, got %q", got)
	}
}

func TestGenerateMessage_ChannelFooter(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "u-1",
		Channel:    "wecom",
		Payload: model.JSONMap{
			"content":                "Hello",
			"include_channel_footer": true,
		},
	}
	got, err := svc.generateMessage(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "[via wecom @") {
		t.Errorf("expected channel footer, got %q", got)
	}
}

func TestGenerateMessage_NoFooter(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		CustomerID: "u-1",
		Channel:    "wecom",
		Payload:    model.JSONMap{"content": "Hello"},
	}
	got, err := svc.generateMessage(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "[via") {
		t.Errorf("expected no footer, got %q", got)
	}
}

func TestDispatchOutbound_NilJob(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.dispatchOutbound(context.Background(), nil)
	if err == nil {
		t.Error("expected error on nil job")
	}
}

func TestDispatchOutbound_UnsupportedChannel(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{Channel: "unknown", CustomerID: "u-1"}
	_, err := svc.dispatchOutbound(context.Background(), job)
	if err == nil {
		t.Error("expected error on unsupported channel")
	}
}

func TestDispatchOutbound_EmptyCustomerID(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{Channel: "wecom"}
	_, err := svc.dispatchOutbound(context.Background(), job)
	if err == nil {
		t.Error("expected error on empty customer_id")
	}
}

func TestDispatchOutbound_SenderBackedChannels(t *testing.T) {
	svc := NewReachPipelineService(nil)
	stub := &reachStubSender{}
	svc.SetReachSender(stub)
	channels := []string{"wecom", "feishu", "telegram", "whatsapp", "sms", "email", "card", "dingtalk"}
	for _, ch := range channels {
		job := &model.ReachJob{Channel: ch, CustomerID: "u-1"}
		mid, err := svc.dispatchOutbound(context.Background(), job)
		if err != nil {
			t.Errorf("渠道 %s: unexpected error: %v", ch, err)
			continue
		}
		if mid != "sent_"+ch {
			t.Errorf("渠道 %s: 消息号应来自发送器（sent_%s），got %q", ch, ch, mid)
		}
	}
	if got := stub.sentTo(); len(got) != len(channels) {
		t.Errorf("发送器被调用 %d 次，want %d：有一条渠道没走到发送器", len(got), len(channels))
	}
}

func TestDispatchOutbound_BridgeChannelsAttemptOutboxWithoutSender(t *testing.T) {
	svc := NewReachPipelineService(nil)
	channels := []string{"douyin", "kuaishou", "xiaohongshu"}
	for _, ch := range channels {
		job := &model.ReachJob{Channel: ch, CustomerID: "u-1"}
		_, err := svc.dispatchOutbound(context.Background(), job)
		if err == nil {
			t.Errorf("渠道 %s: 外发台没接线时应当报错，got nil", ch)
			continue
		}
		// 这条分支证明的是：bridge 那几条不依赖注入的 sender，走的是包级外发台。
		// 外发台没接时它必须把原因说出来，而不是落进"没有发送器"那句通用错误里。
		if !strings.Contains(err.Error(), "bridge") {
			t.Errorf("渠道 %s: 失败原因应指向 bridge 外发台，读起来才不像装配点漏装: %v", ch, err)
		}
	}
}

// TestDispatchOutbound_WithoutSenderRefuses 没装发送器的非 bridge 渠道必须失败，
// 并且不许在 payload 里留下任何"投递过"的痕迹：旧实现会编一个 msg_ 开头的假号写进
// _last_send，再由 trackSendResult 抄进 _tracking 给运营台当回执。
func TestDispatchOutbound_WithoutSenderRefuses(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{Channel: "wecom", CustomerID: "u-1"}
	mid, err := svc.dispatchOutbound(context.Background(), job)
	if err == nil {
		t.Fatalf("没装发送器却回了成功，消息号=%q", mid)
	}
	if !strings.Contains(err.Error(), "没有装配真实发送器") {
		t.Errorf("错误应说清是发送器没装，got: %v", err)
	}
	if _, ok := job.Payload["_last_send"]; ok {
		t.Errorf("失败的一律不许写 _last_send，实际 payload=%v", job.Payload)
	}
}

func TestDispatchOutbound_SenderMessageIDLandsInPayload(t *testing.T) {
	svc := NewReachPipelineService(nil)
	svc.SetReachSender(&reachStubSender{})
	job := &model.ReachJob{Channel: "wecom", CustomerID: "u-1"}
	mid, err := svc.dispatchOutbound(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last, ok := job.Payload["_last_send"].(map[string]any)
	if !ok {
		t.Fatalf("发送过的作业要留 _last_send，实际 payload=%v", job.Payload)
	}
	if last["message_id"] != mid {
		t.Errorf("_last_send.message_id=%v，与返回值 %q 不一致", last["message_id"], mid)
	}
	if last["channel"] != "wecom" {
		t.Errorf("_last_send.channel=%v, want wecom", last["channel"])
	}
}

func TestTrackSendResult_NilJob(t *testing.T) {
	svc := NewReachPipelineService(nil)
	err := svc.trackSendResult(context.Background(), nil, StepResult{})
	if err == nil {
		t.Error("expected error on nil job")
	}
}

func TestTrackSendResult_WritesTracking(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{
		ID:         1,
		Channel:    "wecom",
		CustomerID: "u-1",
		State:      "running",
		Payload: model.JSONMap{
			"_last_send": map[string]any{
				"message_id": "msg_test_1",
				"channel":    "wecom",
			},
		},
	}
	if err := svc.trackSendResult(context.Background(), job, StepResult{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tracking, _ := job.Payload["_tracking"].(map[string]any)
	if tracking == nil {
		t.Fatal("expected _tracking in payload")
	}
	if tracking["message_id"] != "msg_test_1" {
		t.Errorf("expected message_id=msg_test_1, got %v", tracking["message_id"])
	}
	if tracking["channel"] != "wecom" {
		t.Errorf("expected channel=wecom, got %v", tracking["channel"])
	}
	if tracking["tracked_at"] == nil {
		t.Error("expected tracked_at set")
	}
}

func TestAggregateReport_NilJob(t *testing.T) {
	svc := NewReachPipelineService(nil)
	_, err := svc.aggregateReport(context.Background(), nil)
	if err == nil {
		t.Error("expected error on nil job")
	}
}

func TestAggregateReport_EmptyResults(t *testing.T) {
	svc := NewReachPipelineService(nil)
	job := &model.ReachJob{}
	report, err := svc.aggregateReport(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report["total_steps"] != 0 {
		t.Errorf("expected total_steps=0, got %v", report["total_steps"])
	}
	if report["success_steps"] != 0 {
		t.Errorf("expected success_steps=0, got %v", report["success_steps"])
	}
}

func TestAggregateReport_WithStepResults(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-1"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	job.StepResults = model.JSONArray{
		map[string]any{"step": StepAudience, "success": true, "duration_ms": 5},
		map[string]any{"step": StepSend, "success": true, "duration_ms": 50},
		map[string]any{"step": StepReport, "success": false, "duration_ms": 1, "error": "x"},
	}
	job.Payload = model.JSONMap{
		"_tracking": map[string]any{
			"message_id": "msg_test_1",
			"channel":    "wecom",
		},
	}
	report, err := svc.aggregateReport(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report["total_steps"] != 3 {
		t.Errorf("expected total_steps=3, got %v", report["total_steps"])
	}
	if report["success_steps"] != 2 {
		t.Errorf("expected success_steps=2, got %v", report["success_steps"])
	}
	if report["failed_steps"] != 1 {
		t.Errorf("expected failed_steps=1, got %v", report["failed_steps"])
	}
	if report["total_duration_ms"] != 56 {
		t.Errorf("expected total_duration_ms=56, got %v", report["total_duration_ms"])
	}
	if report["slowest_step"] != StepSend {
		t.Errorf("expected slowest_step=send, got %v", report["slowest_step"])
	}
	if report["tracking_message_id"] != "msg_test_1" {
		t.Errorf("expected tracking_message_id=msg_test_1, got %v", report["tracking_message_id"])
	}
}

func TestAggregateReport_UpdatesPipelineCounters(t *testing.T) {
	svc, db := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-1"))
	job, _ := svc.EnqueueJob(context.Background(), newReachJobReq(pipe.ID))
	job.StepResults = model.JSONArray{
		map[string]any{"step": StepSend, "success": true, "duration_ms": 50},
	}
	if _, err := svc.aggregateReport(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var updated model.ReachPipeline
	db.First(&updated, pipe.ID)
	if updated.TotalSuccess != 1 {
		t.Errorf("expected TotalSuccess=1, got %d", updated.TotalSuccess)
	}
}

func TestRenderReachTemplate_EmptyTemplate(t *testing.T) {
	job := &model.ReachJob{}
	if got := renderReachTemplate("", job); got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestRenderReachTemplate_NilJob(t *testing.T) {
	if got := renderReachTemplate("Hello", nil); got != "Hello" {
		t.Errorf("expected unchanged template, got %q", got)
	}
}

func TestRenderReachTemplate_MultipleVars(t *testing.T) {
	job := &model.ReachJob{
		CustomerID: "u-1",
		Channel:    "wecom",
		Payload:    model.JSONMap{"name": "Alice", "vip": true},
	}
	got := renderReachTemplate("Hi {{name}}, customer={{customer_id}}, channel={{channel}}, vip={{vip}}, today={{date}}", job)
	if !strings.Contains(got, "Hi Alice") {
		t.Errorf("expected name replaced, got %q", got)
	}
	if !strings.Contains(got, "customer=u-1") {
		t.Errorf("expected customer_id replaced, got %q", got)
	}
	if !strings.Contains(got, "channel=wecom") {
		t.Errorf("expected channel replaced, got %q", got)
	}
	if !strings.Contains(got, "vip=true") {
		t.Errorf("expected vip replaced, got %q", got)
	}
}

func TestRenderReachTemplate_NoPlaceholder(t *testing.T) {
	job := &model.ReachJob{}
	got := renderReachTemplate("Plain text with no placeholders.", job)
	if got != "Plain text with no placeholders." {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestRenderReachTemplate_UnclosedPlaceholder(t *testing.T) {
	job := &model.ReachJob{}
	got := renderReachTemplate("Hello {{ unclosed", job)
	if got != "Hello {{ unclosed" {
		t.Errorf("expected unclosed placeholder preserved, got %q", got)
	}
}

func TestFullPipeline_RenderAndTrack(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-1")
	pipe, err := svc.CreatePipeline(context.Background(), req)
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	jobReq := &EnqueueJobRequest{
		PipelineID: pipe.ID,
		Channel:    "wecom",
		CustomerID: "u-test-1",
		AccountID:  "acc-test-1",
		Payload: map[string]any{
			"content":   "Hi {{customer_id}}, here is your {{vip_level}} coupon for {{channel}}!",
			"vip_level": "GOLD",
		},
		MaxRetry: 3,
	}
	job, err := svc.EnqueueJob(context.Background(), jobReq)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	executed, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if executed.State != JobStateSuccess {
		t.Errorf("expected state=success, got %s (error: %s)", executed.State, executed.ErrorMessage)
	}
	t.Logf("executed.Payload: %+v", executed.Payload)
	tracking, _ := executed.Payload["_tracking"].(map[string]any)
	if tracking == nil {
		t.Error("expected _tracking in payload")
	} else if tracking["message_id"] == nil {
		t.Errorf("expected message_id in tracking, got %+v", tracking)
	}
}

func TestFullPipeline_FailOnEmptyContent(t *testing.T) {
	svc, _ := newReachTestService(t)
	pipe, _ := svc.CreatePipeline(context.Background(), newReachPipelineReq("m-1"))
	jobReq := &EnqueueJobRequest{
		PipelineID: pipe.ID,
		Channel:    "wecom",
		CustomerID: "u-test-1",
		Payload:    map[string]any{},
	}
	job, _ := svc.EnqueueJob(context.Background(), jobReq)
	executed, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if executed.State != JobStateFailed {
		t.Errorf("expected state=failed, got %s", executed.State)
	}
	if !strings.Contains(executed.ErrorMessage, "content prepare") {
		t.Errorf("expected error message about content prepare, got %q", executed.ErrorMessage)
	}
}

func TestFullPipeline_FailOnUnimplementedChannel(t *testing.T) {
	svc, _ := newReachTestService(t)
	req := newReachPipelineReq("m-1")
	req.Channel = "douyin"
	pipe, _ := svc.CreatePipeline(context.Background(), req)
	jobReq := &EnqueueJobRequest{
		PipelineID: pipe.ID,
		Channel:    "douyin",
		CustomerID: "u-test-1",
		Payload:    map[string]any{"content": "test"},
	}
	job, _ := svc.EnqueueJob(context.Background(), jobReq)
	executed, err := svc.ExecuteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	if executed.State != JobStateFailed {
		t.Errorf("expected state=failed for douyin, got %s", executed.State)
	}
	if !strings.Contains(executed.ErrorMessage, "douyin") {
		t.Errorf("expected error message about douyin, got %q", executed.ErrorMessage)
	}
}
