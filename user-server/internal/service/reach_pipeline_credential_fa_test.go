package service

// reach_pipeline_credential_fa_test.go FA-REACH-02：发送凭据耐久协议。
// 覆盖：发送成功后保存失败、跟踪阶段失败、进程重启、双 worker、
// 明确失败可重试、email 排队与送达分别呈现。判据核心=发送器只收到
// 契约允许的一次动作，未知结果不盲重发。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// faCredSender 计数发送器：记录调用；可注入失败。
type faCredSender struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (s *faCredSender) SendReach(_ context.Context, channel, _, to, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, channel+"|"+to)
	if s.err != nil {
		return "", s.err
	}
	return "msg-" + channel + "-" + to, nil
}

func (s *faCredSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// faSaveGate 包装 repo.SaveJob，按调用序号注入失败；其余透传真实落库。
type faSaveGate struct {
	mu     sync.Mutex
	n      int
	failOn map[int]bool
	real   func(context.Context, *model.ReachJob) error
}

func (g *faSaveGate) fn(ctx context.Context, job *model.ReachJob) error {
	g.mu.Lock()
	g.n++
	fail := g.failOn[g.n]
	g.mu.Unlock()
	if fail {
		return errors.New("injected save failure")
	}
	return g.real(ctx, job)
}

func faCredSetup(t *testing.T) (*ReachPipelineService, *gorm.DB, *faCredSender) {
	db := setupReachTestDB(t)
	svc := NewReachPipelineService(repository.NewReachPipelineRepository(db))
	sender := &faCredSender{}
	svc.SetReachSender(sender)
	return svc, db, sender
}

func faCredNewJob(t *testing.T, svc *ReachPipelineService, channel string) *model.ReachJob {
	pipe, err := svc.CreatePipeline(context.Background(), &CreatePipelineRequest{
		Name: "cred-pipe", Channel: channel, Steps: DefaultPipelineSteps,
		RetryPolicy: DefaultRetryPolicy(), RateLimit: DefaultRateLimit(),
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}
	job, err := svc.EnqueueJob(context.Background(), &EnqueueJobRequest{
		PipelineID: pipe.ID, Channel: channel,
		CustomerID: "cust-x", AccountID: "acc-x",
		Payload: map[string]any{"content": "hello"}, MaxRetry: 3,
	})
	if err != nil {
		t.Fatalf("EnqueueJob: %v", err)
	}
	return job
}

func faResetJobPending(t *testing.T, db *gorm.DB, jobID uint) {
	if err := db.Model(&model.ReachJob{}).Where("id = ?", jobID).
		Updates(map[string]any{"state": JobStatePending, "next_run_at": nil}).Error; err != nil {
		t.Fatalf("reset pending: %v", err)
	}
}

func faReadJob(t *testing.T, db *gorm.DB, jobID uint) *model.ReachJob {
	var job model.ReachJob
	if err := db.First(&job, jobID).Error; err != nil {
		t.Fatalf("read job: %v", err)
	}
	return &job
}

// 发送已受理后最终状态落库失败：恢复复用凭据，不重复外发。
func TestFACred_FinalSaveFailureReusesAcceptedCredential(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "wecom")

	gate := &faSaveGate{failOn: map[int]bool{4: true}, real: svc.repo.SaveJob}
	svc.saveJobFn = gate.fn
	if _, err := svc.ExecuteJob(context.Background(), job.ID); err == nil {
		t.Fatal("期望最终落库失败返回错误")
	}
	if sender.count() != 1 {
		t.Fatalf("首次应发送1次, got=%d", sender.count())
	}

	// 模拟进程重启 + stuck 复位：新 service + 重置 pending。
	svc2 := NewReachPipelineService(repository.NewReachPipelineRepository(db))
	svc2.SetReachSender(sender)
	faResetJobPending(t, db, job.ID)
	done, err := svc2.ExecuteJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("恢复执行: %v", err)
	}
	if done.State != JobStateSuccess {
		t.Fatalf("恢复后应为 success, got=%s", done.State)
	}
	if sender.count() != 1 {
		t.Fatalf("恢复不得再次外发, sender calls=%d", sender.count())
	}
	cred := getReachSendCredential(faReadJob(t, db, job.ID))
	if cred == nil || cred.State != reachSendStateAccepted {
		t.Fatalf("凭据应为 accepted, got=%+v", cred)
	}
}

// accepted 凭据落库失败：库内停在 sending（崩溃窗口）→恢复按未知处理，不重发。
func TestFACred_AcceptedSaveFailureNoResend(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "wecom")

	// 模拟发送后进程崩溃：accepted 凭据落库(#3)与最终落库(#4)都失败，
	// 库内停在 sending（崩溃窗口）。
	gate := &faSaveGate{failOn: map[int]bool{3: true, 4: true}, real: svc.repo.SaveJob}
	svc.saveJobFn = gate.fn
	svc.ExecuteJob(context.Background(), job.ID) // 忽略返回错误
	if sender.count() != 1 {
		t.Fatalf("首次应发送1次, got=%d", sender.count())
	}
	cred := getReachSendCredential(faReadJob(t, db, job.ID))
	if cred == nil || cred.State != reachSendStateSending {
		t.Fatalf("凭据应停在 sending, got=%+v", cred)
	}

	svc2 := NewReachPipelineService(repository.NewReachPipelineRepository(db))
	svc2.SetReachSender(sender)
	faResetJobPending(t, db, job.ID)
	_, err := svc2.ExecuteJob(context.Background(), job.ID)
	if !errors.Is(err, ErrReachSendUnknown) {
		t.Fatalf("未知凭据应返 ErrReachSendUnknown, got=%v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("未知结果不得盲重发, sender calls=%d", sender.count())
	}
}

// 跟踪/报告阶段失败（最终落库失败同族）：accepted 凭据已落库，恢复复用不重发。
func TestFACred_TrackStageFailureReusesCredential(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "wecom")

	// 用精简步骤 [audience, send, track_result, report]，在最终落库(call #3)失败。
	pipe, _ := svc.GetPipeline(context.Background(), job.PipelineID)
	pipe.Steps = toJSONArray(mustJSON([]string{StepAudience, StepSend, StepTrackResult, StepReport}))
	svc.repo.SavePipeline(context.Background(), pipe)

	gate := &faSaveGate{failOn: map[int]bool{3: true}, real: svc.repo.SaveJob}
	svc.saveJobFn = gate.fn
	svc.ExecuteJob(context.Background(), job.ID)
	if sender.count() != 1 {
		t.Fatalf("首次应发送1次, got=%d", sender.count())
	}

	svc2 := NewReachPipelineService(repository.NewReachPipelineRepository(db))
	svc2.SetReachSender(sender)
	faResetJobPending(t, db, job.ID)
	if _, err := svc2.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("恢复: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("跟踪失败恢复不得重发, sender calls=%d", sender.count())
	}
}

// 双 worker 并发抢占：只有一个成功发送。
func TestFACred_DoubleWorkerSendsOnce(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "wecom")

	var wg sync.WaitGroup
	var okCount, conflictCount int64
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ExecuteJob(context.Background(), job.ID)
			if err == nil {
				atomic.AddInt64(&okCount, 1)
			} else if errors.Is(err, ErrReachJobNotPending) {
				atomic.AddInt64(&conflictCount, 1)
			}
		}()
	}
	wg.Wait()
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("应恰1成功1抢占失败, ok=%d conflict=%d", okCount, conflictCount)
	}
	if sender.count() != 1 {
		t.Fatalf("双 worker 只应发送1次, got=%d", sender.count())
	}
	_ = db
}

// 明确失败（发送器报错）→ failed 凭据，恢复可安全重试（这一次才成功）。
func TestFACred_FailedCredentialAllowsRetry(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "wecom")
	sender.err = errors.New("smtp down")
	svc.ExecuteJob(context.Background(), job.ID)
	if sender.count() != 1 {
		t.Fatalf("首次尝试1次, got=%d", sender.count())
	}
	cred := getReachSendCredential(faReadJob(t, db, job.ID))
	if cred == nil || cred.State != reachSendStateFailed {
		t.Fatalf("凭据应为 failed, got=%+v", cred)
	}

	sender.err = nil
	svc2 := NewReachPipelineService(repository.NewReachPipelineRepository(db))
	svc2.SetReachSender(sender)
	faResetJobPending(t, db, job.ID)
	if _, err := svc2.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("重试: %v", err)
	}
	if sender.count() != 2 {
		t.Fatalf("失败可重试应再发1次(总2), got=%d", sender.count())
	}
}

// email 的入队 ID 记为 queued，与真实送达 sent 分别呈现。
func TestFACred_EmailDeliveryQueued(t *testing.T) {
	svc, db, sender := faCredSetup(t)
	job := faCredNewJob(t, svc, "email")
	sender.err = nil
	if _, err := svc.ExecuteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("email 执行: %v", err)
	}
	cred := getReachSendCredential(faReadJob(t, db, job.ID))
	if cred == nil || cred.Delivery != reachSendDeliveryQueued {
		t.Fatalf("email delivery 应为 queued, got=%+v", cred)
	}
	if cred.MessageID == "" {
		t.Fatal("email 入队 ID 应记录")
	}
}
