// reach_quota_test.go D05（配额原子预占与幂等扣额）测试。
// 验收口径（docs/assessment/functionality-2026-10-27/开发顺序与验收.md D05）：
//   - 并发不超配置上限（PG 行锁真源，两进程语义同库内两事务）；
//   - 进程重启不清硬额度（持久表）；
//   - unknown 不释放；重复回执不重复 commit；失败重试复用同 reservation；
//   - 同 intent 异 payload 返回冲突；取消已提交动作不错误返额。
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupQuotaManager(t *testing.T) (*ReachQuotaManager, *repository.ReachQuotaRepository, *gorm.DB) {
	database := testutil.NewTestDB(t,
		&model.ReachQuotaCounter{},
		&model.ReachQuotaReservation{},
	)
	repo := repository.NewReachQuotaRepository(database)
	return NewReachQuotaManager(repo), repo, database
}

// 基本流转：预占 → commit（幂等）；预占 → release（返额可再占）。
func TestQuota_ReserveCommitRelease(t *testing.T) {
	m, repo, _ := setupQuotaManager(t)
	ctx := context.Background()

	token, reused, err := m.ReserveIntent(ctx, "intent-1", "hash-a", []QuotaScopeDef{{Key: "customer:c1", Limit: 2}})
	if err != nil || reused {
		t.Fatalf("首次预占: %v reused=%v", err, reused)
	}
	if token == "" {
		t.Fatal("预占必须回填令牌")
	}
	used, _ := repo.GetCounterUsed(ctx, "customer:c1", m.day())
	if used != 1 {
		t.Fatalf("预占后 used = %d, want 1", used)
	}

	// 同 intent 同载荷重入：复用同一预占，额度不加
	token2, reused2, err := m.ReserveIntent(ctx, "intent-1", "hash-a", []QuotaScopeDef{{Key: "customer:c1", Limit: 2}})
	if err != nil || !reused2 || token2 != token {
		t.Fatalf("重入应复用: err=%v reused=%v", err, reused2)
	}
	used, _ = repo.GetCounterUsed(ctx, "customer:c1", m.day())
	if used != 1 {
		t.Fatalf("重入不得重复占额: used = %d", used)
	}

	// commit 幂等
	if err := m.CommitIntent(ctx, "intent-1", token); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := m.CommitIntent(ctx, "intent-1", token); err != nil {
		t.Fatalf("重复 commit 应幂等: %v", err)
	}
	used, _ = repo.GetCounterUsed(ctx, "customer:c1", m.day())
	if used != 1 {
		t.Fatalf("重复回执不得重复计数: used = %d", used)
	}

	// committed 不可 release（取消已提交动作不得错误返额）
	if err := m.ReleaseIntent(ctx, "intent-1", token); !errors.Is(err, repository.ErrQuotaInvalidState) {
		t.Fatalf("committed release 应被拒，got %v", err)
	}
}

// 超限与释放后重占。
func TestQuota_LimitEnforcedAndReleaseReturns(t *testing.T) {
	m, repo, _ := setupQuotaManager(t)
	ctx := context.Background()

	t1, _, err := m.ReserveIntent(ctx, "q-1", "h", []QuotaScopeDef{{Key: "customer:c2", Limit: 1}})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := m.ReserveIntent(ctx, "q-2", "h", []QuotaScopeDef{{Key: "customer:c2", Limit: 1}}); !errors.Is(err, repository.ErrQuotaExceeded) {
		t.Fatalf("超限应拒绝: %v", err)
	}

	// 确定未发送 → release 返额
	if err := m.ReleaseIntent(ctx, "q-1", t1); err != nil {
		t.Fatalf("release: %v", err)
	}
	used, _ := repo.GetCounterUsed(ctx, "customer:c2", m.day())
	if used != 0 {
		t.Fatalf("释放后 used = %d, want 0", used)
	}
	// 失败重试（同 intent）复用同一行重新占额
	t1b, reused, err := m.ReserveIntent(ctx, "q-1", "h", []QuotaScopeDef{{Key: "customer:c2", Limit: 1}})
	if err != nil || reused {
		t.Fatalf("released 复用: %v reused=%v", err, reused)
	}
	if t1b == "" {
		t.Fatal("重占必须回填新令牌")
	}
}

// 同 intent 异 payload = 冲突。
func TestQuota_IntentPayloadConflict(t *testing.T) {
	m, _, _ := setupQuotaManager(t)
	ctx := context.Background()
	if _, _, err := m.ReserveIntent(ctx, "intent-x", "hash-1", []QuotaScopeDef{{Key: "customer:c3", Limit: 5}}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, _, err := m.ReserveIntent(ctx, "intent-x", "hash-2", []QuotaScopeDef{{Key: "customer:c3", Limit: 5}}); !errors.Is(err, repository.ErrQuotaIntentConflict) {
		t.Fatalf("同 intent 异 payload 应冲突: %v", err)
	}
}

// unknown 不释放；事后核对可 commit。
func TestQuota_UnknownKeepsReservation(t *testing.T) {
	m, repo, _ := setupQuotaManager(t)
	ctx := context.Background()

	token, _, err := m.ReserveIntent(ctx, "intent-u", "h", []QuotaScopeDef{{Key: "customer:c4", Limit: 1}})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := m.MarkUnknownIntent(ctx, "intent-u", token); err != nil {
		t.Fatalf("mark unknown: %v", err)
	}
	used, _ := repo.GetCounterUsed(ctx, "customer:c4", m.day())
	if used != 1 {
		t.Fatalf("unknown 必须保留额度: used = %d", used)
	}
	if err := m.ReleaseIntent(ctx, "intent-u", token); !errors.Is(err, repository.ErrQuotaInvalidState) {
		t.Fatalf("unknown release 应被拒: %v", err)
	}
	// 事后核对确认已发送 → commit
	if err := m.CommitIntent(ctx, "intent-u", token); err != nil {
		t.Fatalf("unknown → commit: %v", err)
	}

	// 令牌不匹配被拒
	if err := m.CommitIntent(ctx, "intent-u", "forged-token"); !errors.Is(err, repository.ErrQuotaTokenMismatch) {
		t.Fatalf("伪令牌 commit 应被拒: %v", err)
	}
}

// 并发预占不超上限（单库两事务 = 两进程语义的库内等价）。
func TestQuota_ConcurrentReserveNeverExceedsLimit(t *testing.T) {
	m, repo, _ := setupQuotaManager(t)
	ctx := context.Background()

	const limit = 5
	const workers = 30
	var (
		mu       sync.Mutex
		granted  int
		denied   int
		wg       sync.WaitGroup
		startSig = make(chan struct{})
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-startSig
			_, _, err := m.ReserveIntent(ctx, "concurrent-intent-"+time.Now().Format("150405.000000000")+fmtInt(n), "h", []QuotaScopeDef{{Key: "customer:hot", Limit: limit}})
			mu.Lock()
			if err == nil {
				granted++
			} else if errors.Is(err, repository.ErrQuotaExceeded) {
				denied++
			}
			mu.Unlock()
		}(i)
	}
	close(startSig)
	wg.Wait()

	used, _ := repo.GetCounterUsed(ctx, "customer:hot", m.day())
	if used > limit {
		t.Fatalf("并发预占后 used = %d，超过上限 %d（原子性被破坏）", used, limit)
	}
	if granted != used {
		t.Fatalf("granted=%d 与 used=%d 不一致", granted, used)
	}
	if granted+denied != workers {
		t.Fatalf("结果总数 %d != %d", granted+denied, workers)
	}
}

// 管线挂钩：成功 commit、不可重试失败 release、可重试失败保持 reserved、
// deferred 保持 reserved（重试复用）。
func TestPipeline_QuotaSettlement(t *testing.T) {
	m, repo, _ := setupQuotaManager(t)
	ctx := context.Background()
	scopes := []QuotaScopeDef{{Key: "customer:pipe", Limit: 10}}

	type outcome struct {
		name  string
		resp  *SendResponse
		check func(t *testing.T)
	}
	newReq := func(intent string) *ReachSendRequest {
		return &ReachSendRequest{
			Channel: "wecom", CustomerID: "pipe",
			Content:  "配额内的触达",
			Metadata: map[string]string{"action_intent_id": intent, "one_id": "pipe"},
		}
	}
	buildPipeline := func(resp *SendResponse) *defaultSendPipeline {
		cfg := DefaultSendPipelineConfig(&fakeConsentAdapter{})
		cfg.QuotaReserver = m
		cfg.QuotaScopesFn = func(ctx context.Context, req *ReachSendRequest) []QuotaScopeDef { return scopes }
		// 只走 retry(真实适配器调用)+send 两步；MaxRetries=0 消除退避等待
		cfg.Steps = []string{SendStepRetry, SendStepSend}
		cfg.RetryPolicy = SendRetryPolicy{MaxRetries: 0}
		pipeline := NewSendPipeline(cfg).(*defaultSendPipeline)
		pipeline.config.Adapter = &fakeOutcomeAdapter{resp: resp}
		return pipeline
	}

	outcomes := []outcome{
		{
			name: "成功→committed",
			resp: &SendResponse{Success: true},
			check: func(t *testing.T) {
				rec, _ := repo.GetReservation(ctx, "pipe-ok#customer:pipe")
				if rec == nil || rec.State != model.QuotaStateCommitted {
					t.Fatalf("成功应 commit: %+v", rec)
				}
			},
		},
		{
			name: "不可重试失败→released 返额",
			resp: &SendResponse{Success: false, Error: "send tg msg: Bad Request: status 403 Forbidden: bot blocked"},
			check: func(t *testing.T) {
				rec, _ := repo.GetReservation(ctx, "pipe-deny#customer:pipe")
				if rec == nil || rec.State != model.QuotaStateReleased {
					t.Fatalf("确定失败应 release: %+v", rec)
				}
			},
		},
		{
			name: "可重试失败→保持 reserved",
			resp: &SendResponse{Success: false, Error: "send tg msg: dial tcp: connection refused"},
			check: func(t *testing.T) {
				rec, _ := repo.GetReservation(ctx, "pipe-retry#customer:pipe")
				if rec == nil || rec.State != model.QuotaStateReserved {
					t.Fatalf("可重试失败应保持 reserved: %+v", rec)
				}
			},
		},
	}

	// deferred 结算直测：进延迟队列 = 占额保留（队列重发带同 intent 复用）。
	t.Run("deferred→committed（占额保留给队列重发）", func(t *testing.T) {
		token, _, err := m.ReserveIntent(ctx, "pipe-def", "h", scopes)
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		m.SetClockForTest(time.Now) // 保持默认日
		p := &defaultSendPipeline{config: SendPipelineConfig{QuotaReserver: m}}
		p.settleQuota(ctx, newReq("pipe-def"), &SendResponse{Success: false, Deferred: true}, "pipe-def", token)
		rec, _ := repo.GetReservation(ctx, "pipe-def#customer:pipe")
		if rec == nil || rec.State != model.QuotaStateCommitted {
			t.Fatalf("deferred 应按 commit 保留: %+v", rec)
		}
	})
	for _, oc := range outcomes {
		t.Run(oc.name, func(t *testing.T) {
			intent := map[string]string{}
			switch {
			case oc.resp.Success:
				intent["id"] = "pipe-ok"
			case oc.resp.Deferred:
				intent["id"] = "pipe-def"
			default:
				if ce := AsChannelError(errors.New(oc.resp.Error)); ce != nil && ce.Retryable {
					intent["id"] = "pipe-retry"
				} else {
					intent["id"] = "pipe-deny"
				}
			}
			pipeline := buildPipeline(oc.resp)
			got := pipeline.Send(ctx, newReq(intent["id"]))
			_ = got
			oc.check(t)
		})
	}
}

// fakeOutcomeAdapter 按预设响应结束 send 步骤
type fakeOutcomeAdapter struct{ resp *SendResponse }

func (f *fakeOutcomeAdapter) Send(ctx context.Context, req *ReachSendRequest) (string, error) {
	if f.resp.Success {
		return "mh-quota", nil
	}
	if f.resp.Deferred {
		// deferred 语义由 quiet 步骤产生；send 步骤这里直接成功即可（结算按 resp.Deferred 判）
		return "mh-deferred", nil
	}
	return "", errors.New(f.resp.Error)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
