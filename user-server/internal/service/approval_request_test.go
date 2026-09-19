// approval_request_test.go T-P3-01：审批检查点服务（AC①②③）。
//
// 两条腿并用：
//   - 真库（testutil.NewTestDB + 真仓储）跑"幂等/裁决/过期"这三条只有靠索引与行锁
//     才成立的路径；
//   - 假仓储跑真库里**制造不出来**的分支：Insert 撞 23505 之后重查落空、读故障、
//     凭证生成失败。这些分支规定的是"闸门在最坏时刻做什么"，不能只靠读代码确认。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 假仓储 ——————————————————————————————————————————————

type fakeApprovalRepo struct {
	available bool

	insertErr      error
	getPendingRow  *model.ApprovalRequest
	getPendingErr  error
	getPendingFn   func() (*model.ApprovalRequest, error)
	getByIDRow     *model.ApprovalRequest
	getByIDErr     error
	expireRows     []*model.ApprovalRequest
	expireErr      error
	expireSawNow   time.Time
	expireSawLimit int
	mutateErr      error
	mutateApplied  bool

	calls struct {
		insert, getPending, getByID, getByToken, mutate, expire int
		inserted                                                []*model.ApprovalRequest
	}
}

func (f *fakeApprovalRepo) Available() bool { return f.available }

func (f *fakeApprovalRepo) Insert(_ context.Context, a *model.ApprovalRequest) error {
	f.calls.insert++
	if a != nil {
		f.calls.inserted = append(f.calls.inserted, a)
	}
	return f.insertErr
}

func (f *fakeApprovalRepo) GetByID(_ context.Context, id string) (*model.ApprovalRequest, error) {
	f.calls.getByID++
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	if f.getByIDRow != nil && f.getByIDRow.ID == id {
		return f.getByIDRow, nil
	}
	return nil, nil
}

func (f *fakeApprovalRepo) GetByResumeToken(context.Context, string) (*model.ApprovalRequest, error) {
	f.calls.getByToken++
	return nil, nil
}

func (f *fakeApprovalRepo) GetPendingBySubject(context.Context, string, string, string) (*model.ApprovalRequest, error) {
	f.calls.getPending++
	if f.getPendingFn != nil {
		return f.getPendingFn()
	}
	return f.getPendingRow, f.getPendingErr
}

func (f *fakeApprovalRepo) MutatePending(_ context.Context, _ string, fn func(*model.ApprovalRequest)) (bool, error) {
	f.calls.mutate++
	if f.mutateErr != nil {
		return false, f.mutateErr
	}
	if !f.mutateApplied {
		// 未生效时 fn 不该被调用 —— 仓储的契约就是"要么就地改要么什么都没动"。
		return false, nil
	}
	if fn != nil {
		fn(&model.ApprovalRequest{ID: "apr_fake", Status: model.ApprovalStatusPending})
	}
	return true, nil
}

func (f *fakeApprovalRepo) ExpirePendingBatch(_ context.Context, now time.Time, limit int) ([]*model.ApprovalRequest, error) {
	f.calls.expire++
	f.expireSawNow, f.expireSawLimit = now, limit
	return f.expireRows, f.expireErr
}

// —— 真库装配 ——————————————————————————————————————————————

// newApprovalServiceWithDB 返回 (服务, 仓储, 库句柄)：第三份用来直接数行数 ——
// "幂等"的关键断言是**库里只有一行**，只看返回值的话，插了两条也会假装返回同一条。
func newApprovalServiceWithDB(t *testing.T, policy AutoApprovalPolicy) (*ApprovalRequestService, repository.ApprovalRequestRepository, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.ApprovalRequest{})
	repo := repository.NewApprovalRequestRepositoryWithDB(database)
	return NewApprovalRequestService(repo, policy), repo, database
}

func countApprovalRows(t *testing.T, database *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := database.Model(&model.ApprovalRequest{}).Count(&n).Error; err != nil {
		t.Fatalf("统计行数失败：%v", err)
	}
	return n
}

// freezeApprovalClock 把服务的时钟换成可控值，返回"往前推"的函数。
// TTL 与"是否到期"两条判据都读同一个时钟，不冻它就写不出确定性的到期用例。
func freezeApprovalClock(t *testing.T, at time.Time) func(time.Duration) {
	t.Helper()
	current := at
	previous := approvalNowFn
	approvalNowFn = func() time.Time { return current }
	t.Cleanup(func() { approvalNowFn = previous })
	return func(d time.Duration) { current = current.Add(d) }
}

// —— verdict 映射 —————————————————————————————————————————

func TestApprovalVerdictMapsToStatus(t *testing.T) {
	// verdict 的字面值就是目标状态，中间没有映射表 ⇒ 一旦有人把
	// model.ApprovalStatusApproved 改成别的串，落库的 status 会跟着变，
	// 而待办中心按 "approved" 查就再也查不到了。这里把两边钉在一起。
	if got := string(ApprovalApprove); got != model.ApprovalStatusApproved {
		t.Errorf("ApprovalApprove = %q，期望 %q", got, model.ApprovalStatusApproved)
	}
	if got := string(ApprovalReject); got != model.ApprovalStatusRejected {
		t.Errorf("ApprovalReject = %q，期望 %q", got, model.ApprovalStatusRejected)
	}
	for _, v := range []ApprovalVerdict{ApprovalApprove, ApprovalReject} {
		target, ok := v.targetStatus()
		if !ok || target != string(v) {
			t.Errorf("verdict %q 解析成 (%q,%v)", v, target, ok)
		}
	}
	// 其余取值一律解析失败：expired 不是裁决（走 ExpireOverdue），"" 与拼错的必须拒。
	for _, v := range []ApprovalVerdict{"", "approve", "APPROVED", model.ApprovalStatusExpired, "resume", "auto"} {
		if target, ok := v.targetStatus(); ok {
			t.Errorf("verdict %q 不应被解析成状态，实际 %q", v, target)
		}
	}
}

// —— 入参归一与校验 ————————————————————————————————————————

func TestApprovalSubmitInputNormalize(t *testing.T) {
	for _, tc := range []struct {
		name       string
		in         ApprovalSubmitInput
		wantType   string
		wantID     string
		wantPolicy string
		wantErr    bool
	}{
		{
			name:       "trim-and-lowercase-keys",
			in:         ApprovalSubmitInput{SubjectType: "  Quote ", SubjectID: " q-1 ", PolicyKey: "Reach.Batch_Outreach"},
			wantType:   "quote",
			wantID:     "q-1",
			wantPolicy: "reach.batch_outreach",
		},
		{
			// subject_id 大小写**不归一**：外部平台单号可以合法区分大小写，
			// 小写化会把两张不同单据看成同一对象 —— 那是把两个审批合成一个，比不归一危险。
			name:       "subject-id-keeps-case",
			in:         ApprovalSubmitInput{SubjectType: "quote", SubjectID: "AbC-123", PolicyKey: "quote.send"},
			wantType:   "quote",
			wantID:     "AbC-123",
			wantPolicy: "quote.send",
		},
		{name: "empty-subject-type", in: ApprovalSubmitInput{SubjectID: "q", PolicyKey: "p"}, wantErr: true},
		{name: "empty-subject-id", in: ApprovalSubmitInput{SubjectType: "quote", PolicyKey: "p"}, wantErr: true},
		{name: "empty-policy", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q"}, wantErr: true},
		{name: "space-in-policy", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "quote send"}, wantErr: true},
		// 竖线正是门禁脚本按 | 分列的分隔符：放进去会把一行登记劈成两行。
		{name: "pipe-in-policy", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "a|b"}, wantErr: true},
		{name: "comma-in-subject-type", in: ApprovalSubmitInput{SubjectType: "a,b", SubjectID: "q", PolicyKey: "p"}, wantErr: true},
		{name: "cjk-in-policy", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "报价.发送"}, wantErr: true},
		// subject_id 允许任意字符（业务主键可能带中文/冒号），只限长度。
		{
			name:     "cjk-in-subject-id-allowed",
			in:       ApprovalSubmitInput{SubjectType: "quote", SubjectID: "光子嫩肤-单号:9", PolicyKey: "p"},
			wantType: "quote", wantID: "光子嫩肤-单号:9", wantPolicy: "p",
		},
		{name: "over-long-subject-type", in: ApprovalSubmitInput{SubjectType: strings.Repeat("a", approvalSubjectTypeMaxLen+1), SubjectID: "q", PolicyKey: "p"}, wantErr: true},
		{name: "over-long-policy", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: strings.Repeat("p", approvalPolicyKeyMaxLen+1)}, wantErr: true},
		{name: "over-long-subject-id", in: ApprovalSubmitInput{SubjectType: "quote", SubjectID: strings.Repeat("s", approvalSubjectIDMaxLen+1), PolicyKey: "p"}, wantErr: true},
		// 长度上限含等号：正好卡上限的必须过，否则边界值变成一个说不清的错误。
		{
			name: "at-max-length",
			in: ApprovalSubmitInput{
				SubjectType: strings.Repeat("a", approvalSubjectTypeMaxLen),
				SubjectID:   strings.Repeat("s", approvalSubjectIDMaxLen),
				PolicyKey:   strings.Repeat("p", approvalPolicyKeyMaxLen),
			},
			wantType:   strings.Repeat("a", approvalSubjectTypeMaxLen),
			wantID:     strings.Repeat("s", approvalSubjectIDMaxLen),
			wantPolicy: strings.Repeat("p", approvalPolicyKeyMaxLen),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			err := in.normalize()
			if tc.wantErr {
				if !errors.Is(err, ErrApprovalInputInvalid) {
					t.Fatalf("期望 ErrApprovalInputInvalid，实际 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("归一失败：%v", err)
			}
			if in.SubjectType != tc.wantType || in.SubjectID != tc.wantID || in.PolicyKey != tc.wantPolicy {
				t.Errorf("归一结果 (%q,%q,%q)，期望 (%q,%q,%q)",
					in.SubjectType, in.SubjectID, in.PolicyKey, tc.wantType, tc.wantID, tc.wantPolicy)
			}
		})
	}
}

func TestApprovalService_SubmitTTLBoundary(t *testing.T) {
	clock := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	freezeApprovalClock(t, clock)
	repo := &fakeApprovalRepo{available: true}
	svc := NewApprovalRequestService(repo, nil)
	ctx := context.Background()
	base := ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-ttl", PolicyKey: "quote.send"}

	// TTL=0 走默认值
	row, created, err := svc.Submit(ctx, base)
	if err != nil || !created {
		t.Fatalf("默认 TTL 入队失败：(%v,%v)", created, err)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.Equal(clock.Add(DefaultApprovalRequestTTL)) {
		t.Errorf("TTL=0 应挂默认 %v，实际 %v", DefaultApprovalRequestTTL, row.ExpiresAt)
	}

	explicit := base
	explicit.SubjectID = "q-ttl-2"
	explicit.TTL = 2 * time.Hour
	row2, _, err := svc.Submit(ctx, explicit)
	if err != nil || row2.ExpiresAt == nil || !row2.ExpiresAt.Equal(clock.Add(2*time.Hour)) {
		t.Errorf("显式 TTL=2h 未生效，实际 %v（err=%v）", row2.ExpiresAt, err)
	}

	atMax := base
	atMax.SubjectID = "q-ttl-3"
	atMax.TTL = MaxApprovalRequestTTL
	if _, _, err := svc.Submit(ctx, atMax); err != nil {
		t.Errorf("TTL 恰好等于上限应接受，实际 %v", err)
	}
	overMax := base
	overMax.SubjectID = "q-ttl-4"
	overMax.TTL = MaxApprovalRequestTTL + time.Nanosecond
	if _, _, err := svc.Submit(ctx, overMax); !errors.Is(err, ErrApprovalTTLTooLong) {
		t.Errorf("越上限期望 ErrApprovalTTLTooLong，实际 %v", err)
	}
	negative := base
	negative.SubjectID = "q-ttl-5"
	negative.TTL = -time.Hour
	if _, _, err := svc.Submit(ctx, negative); !errors.Is(err, ErrApprovalInputInvalid) {
		t.Errorf("负 TTL 期望 ErrApprovalInputInvalid（要默认值请传 0），实际 %v", err)
	}
	// 越界的入参**一条都不该落库**：静默夹一个默认值等于替调用方做了它没做的决定。
	if repo.calls.insert != 3 {
		t.Errorf("只应写入 3 条合法请求（默认/2h/30d），实际 %d", repo.calls.insert)
	}
}

func TestApprovalService_WithoutRepoFailsClosed(t *testing.T) {
	ctx := context.Background()
	in := ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "p"}

	// nil 接收者也不能 panic：装配漏一步时这是一个可读的错误，不是 500 栈。
	var nilSvc *ApprovalRequestService
	if _, _, err := nilSvc.Submit(ctx, in); err == nil {
		t.Error("nil 服务 Submit 应报错")
	}
	if _, err := nilSvc.Decide(ctx, "x", ApprovalApprove, "alice", ""); err == nil {
		t.Error("nil 服务 Decide 应报错")
	}
	if _, _, err := nilSvc.ByResumeToken(ctx, "rt"); err == nil {
		t.Error("nil 服务 ByResumeToken 应报错")
	}
	if _, err := nilSvc.ExpireOverdue(ctx, 10); err == nil {
		t.Error("nil 服务 ExpireOverdue 应报错")
	}
	if _, err := nilSvc.Get(ctx, "x"); err == nil {
		t.Error("nil 服务 Get 应报错")
	}

	if _, _, err := NewApprovalRequestService(nil, nil).Submit(ctx, in); err == nil {
		t.Error("未接仓储应报错")
	}
	// available=false：句柄在，但库不通。此时**必须**报错，不能"当作没批过"而放行。
	if _, _, err := NewApprovalRequestService(&fakeApprovalRepo{available: false}, nil).Submit(ctx, in); err == nil {
		t.Error("仓储不可用时 Submit 应报错")
	}
	if _, err := NewApprovalRequestService(&fakeApprovalRepo{available: false}, nil).Decide(ctx, "x", ApprovalApprove, "alice", ""); err == nil {
		t.Error("仓储不可用时 Decide 应报错")
	}
}

// —— AC②：auto-approve 快速路径 ————————————————————————————

type recordingPolicy struct {
	allow  bool
	reason string
	calls  int
	seen   []ApprovalSubmitInput
}

func (p *recordingPolicy) AutoApproves(_ context.Context, in ApprovalSubmitInput) (bool, string) {
	p.calls++
	p.seen = append(p.seen, in)
	return p.allow, p.reason
}

func TestApprovalService_AutoApproveFastPath(t *testing.T) {
	pol := &recordingPolicy{allow: true, reason: "低风险账号在白名单内"}
	svc, repo, database := newApprovalServiceWithDB(t, pol)
	ctx := context.Background()

	row, created, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: " Reach_Plan ", SubjectID: "rp-1", PolicyKey: "Reach.Batch_Outreach"})
	if err != nil || !created {
		t.Fatalf("入队失败：(%v,%v)", created, err)
	}
	// AC② 的正文：**入队即 approved**，调用方原地拿结果，不需要轮询。
	if row.Status != model.ApprovalStatusApproved {
		t.Fatalf("快速路径应以 approved 落库（不是先 pending 再改），实际 %s", row.Status)
	}
	if row.DecidedBy != model.ApprovalDecidedByPolicy {
		t.Errorf("decided_by 应是 %q，实际 %q", model.ApprovalDecidedByPolicy, row.DecidedBy)
	}
	if row.DecisionNote != pol.reason {
		t.Errorf("策略理由应原样留痕（事后要能回答「这条为什么没人看就过了」），实际 %q", row.DecisionNote)
	}
	if row.ExpiresAt != nil {
		t.Errorf("已裁决行的 expires_at 应为 NULL，实际 %v", row.ExpiresAt)
	}
	if row.ResumeToken != "" {
		t.Errorf("auto-approve 记录不该发恢复凭证（没人要恢复它），实际 %q", row.ResumeToken)
	}
	if row.DecidedAt == nil {
		t.Error("approved 必须带裁决时刻，否则自动放行率无法按时间归集")
	}
	if !model.ApprovalDecidedAutomatically(row.DecidedBy) {
		t.Error("这条记录应被统计读成「自动放行」而不是「人工批准」")
	}
	// 策略看到的入参是**归一后**的：否则策略拿 "Reach.Batch_Outreach" 去查白名单会永远查不中。
	if pol.calls != 1 || pol.seen[0].SubjectType != "reach_plan" || pol.seen[0].PolicyKey != "reach.batch_outreach" {
		t.Errorf("策略收到 %d 次调用，入参 %+v", pol.calls, pol.seen)
	}

	// 落库形状以库为准（内存那份不算证据）。
	back, err := repo.GetByID(ctx, row.ID)
	if err != nil || back == nil {
		t.Fatalf("回读失败：(%v,%v)", back, err)
	}
	if back.Status != model.ApprovalStatusApproved || back.ResumeToken != "" {
		t.Errorf("库内形状不一致：%+v", back)
	}

	// 第二条 auto-approve 也必须写得进去：token 留空且唯一索引是部分的。
	// 若索引少了 `<> 空串` 谓词，这里当场撞 23505 —— 而 auto-approve 是 C2 里的常态路径，
	// 也就是说这个失败会在上线第一天、第二次自动放行时发生。
	second, created2, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "reach_plan", SubjectID: "rp-2", PolicyKey: "reach.batch_outreach"})
	if err != nil || !created2 || second.Status != model.ApprovalStatusApproved {
		t.Fatalf("第二条 auto-approve 应成功，实际 (%v,%v,%v)", second, created2, err)
	}
	if n := countApprovalRows(t, database); n != 2 {
		t.Errorf("两条自动放行应各留一行，实际 %d 行", n)
	}
}

func TestApprovalService_AutoApproveWithoutReasonLeavesGrepMarker(t *testing.T) {
	// 策略放行但**没说理由**这一档用现有的 recordingPolicy 造，生产侧不另配"函数即策略"的
	// 适配器（unused 闸门按 tests=false 计数，详见 approval_request.go 里那段说明）。
	svc, _, _ := newApprovalServiceWithDB(t, &recordingPolicy{allow: true, reason: "   "})
	row, _, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q1", PolicyKey: "quote.send"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	// 空白理由要换成可 grep 的字面值：note="" 的自动放行在审计里读起来与"字段没写"
	// 无法区分，事后看不出是不是所有自动放行都这么含糊。
	if row.DecisionNote != "auto_unspecified" {
		t.Errorf("空理由应落成 auto_unspecified，实际 %q", row.DecisionNote)
	}
}

func TestApprovalService_NilPolicyAlwaysWaitsForHuman(t *testing.T) {
	svc, _, _ := newApprovalServiceWithDB(t, nil)
	row, created, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q1", PolicyKey: "quote.send"})
	if err != nil || !created {
		t.Fatalf("入队失败：(%v,%v)", created, err)
	}
	// policy=nil 是最保守的一档：全部走人工，绝不因为"没配策略"就自动放行。
	if row.Status != model.ApprovalStatusPending {
		t.Errorf("未接策略时期望 pending，实际 %s", row.Status)
	}
	// rt_ + 32 字节的 hex = 67 个字符。长度写死是为了让"哪天有人把 rand 换成 8 字节"
	// 在这里变红，而不是等到有人暴力枚举凭证才发现。
	if !strings.HasPrefix(row.ResumeToken, "rt_") || len(row.ResumeToken) != 67 {
		t.Errorf("凭证应是 rt_ + 64 位 hex（crypto/rand 32 字节），实际 %q（长度 %d）", row.ResumeToken, len(row.ResumeToken))
	}
}

// —— AC③：重复提交幂等 ————————————————————————————————————

func TestApprovalService_SubmitIdempotentWhilePending(t *testing.T) {
	pol := &recordingPolicy{}
	svc, _, database := newApprovalServiceWithDB(t, pol)
	ctx := context.Background()
	in := ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-idem", PolicyKey: "quote.send", TTL: time.Hour}

	first, created, err := svc.Submit(ctx, in)
	if err != nil || !created {
		t.Fatalf("首次入队失败：(%v,%v)", created, err)
	}
	if pol.calls != 1 {
		t.Fatalf("首次应问过策略，实际 %d 次", pol.calls)
	}

	// 换个 TTL、换个大小写写法再提交：仍是同一条，且库里不多一行。
	again := ApprovalSubmitInput{SubjectType: " Quote ", SubjectID: "q-idem ", PolicyKey: "QUOTE.SEND", TTL: 3 * time.Hour}
	second, created2, err := svc.Submit(ctx, again)
	if err != nil {
		t.Fatalf("二次入队报错：%v", err)
	}
	if created2 {
		t.Error("重复提交不该新增记录（待办中心会出现两条同样的事）")
	}
	if second.ID != first.ID {
		t.Errorf("幂等要返回同一条：第一次 %s，第二次 %s", first.ID, second.ID)
	}
	if second.ResumeToken != first.ResumeToken {
		t.Errorf("复用旧行时凭证必须保持原值（挂起流程手里那份不能莫名失效）：%q vs %q", second.ResumeToken, first.ResumeToken)
	}
	if first.ExpiresAt == nil || second.ExpiresAt == nil || !second.ExpiresAt.Equal(*first.ExpiresAt) {
		t.Errorf("复用旧行时 TTL 不该被第二次入队改写：%v vs %v", second.ExpiresAt, first.ExpiresAt)
	}
	// 已有 pending 时**不再问策略**：策略刚被改过也不能把已经发出去的待办就地批掉。
	if pol.calls != 1 {
		t.Errorf("已有 pending 时不该再问策略，实际问了 %d 次", pol.calls)
	}
	if n := countApprovalRows(t, database); n != 1 {
		t.Fatalf("两次入队应只有 1 行，实际 %d 行", n)
	}

	// 另一道策略必须另开一条：否则一道审批顺手把另一道批了（幂等键少一维的后果）。
	other, createdOther, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-idem", PolicyKey: "quote.high_discount"})
	if err != nil || !createdOther || other.ID == first.ID {
		t.Errorf("同对象不同策略应各自一条，实际 (%v,%v,%v)", other, createdOther, err)
	}
	if n := countApprovalRows(t, database); n != 2 {
		t.Errorf("期望 2 行（两道策略各一条），实际 %d", n)
	}
}

func TestApprovalService_ResubmitAfterDecisionCreatesNewRequest(t *testing.T) {
	svc, repo, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	in := ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-re", PolicyKey: "quote.send"}

	first, _, err := svc.Submit(ctx, in)
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := svc.Decide(ctx, first.ID, ApprovalReject, "alice", "折扣过高"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}
	// 幂等键只约束"同一时刻一条在途申请"。裁决落定即让坑（部分索引的谓词），
	// 于是被拒的对象修正后可以重审 —— 代价是留下两条记录说明过程。
	second, created, err := svc.Submit(ctx, in)
	if err != nil || !created {
		t.Fatalf("已裁决后再申请应新建：(%v,%v)", created, err)
	}
	if second.ID == first.ID {
		t.Error("重审必须是一条新记录：原地覆盖等于抹掉第一次的裁决")
	}
	if _, err := svc.Decide(ctx, second.ID, ApprovalApprove, "bob", "已降价"); err != nil {
		t.Fatalf("二次裁决失败：%v", err)
	}
	firstBack, err := repo.GetByID(ctx, first.ID)
	if err != nil || firstBack == nil || firstBack.Status != model.ApprovalStatusRejected {
		t.Errorf("第一次的拒绝结论不该被第二次的批准改写：%+v", firstBack)
	}
}

// —— 真库里造不出来的失败分支 ——————————————————————————————

func TestApprovalService_SubmitConflictRereadsWinner(t *testing.T) {
	winner := &model.ApprovalRequest{ID: "apr_winner", Status: model.ApprovalStatusPending, ResumeToken: "rt_winner"}
	repo := &fakeApprovalRepo{available: true, insertErr: repository.ErrApprovalPendingConflict}
	// 第一次查没有 pending（对手还没落库），Insert 撞 23505，第二次查到对手那一行。
	getCalls := 0
	repo.getPendingFn = func() (*model.ApprovalRequest, error) {
		getCalls++
		if getCalls == 1 {
			return nil, nil
		}
		return winner, nil
	}
	svc := NewApprovalRequestService(repo, nil)
	row, created, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "p"})
	if err != nil {
		t.Fatalf("撞车应被兜住而不是上抛：%v", err)
	}
	if created {
		t.Error("对手那一行不是本次新建的")
	}
	if row == nil || row.ID != winner.ID {
		t.Fatalf("幂等的正确形状是返回同一条，实际 %+v", row)
	}
	if repo.calls.insert != 1 {
		t.Errorf("冲突后不该再插一次，实际插了 %d 次", repo.calls.insert)
	}
}

func TestApprovalService_SubmitConflictWithVanishedWinnerFailsClosed(t *testing.T) {
	repo := &fakeApprovalRepo{available: true, insertErr: repository.ErrApprovalPendingConflict}
	repo.getPendingFn = func() (*model.ApprovalRequest, error) {
		return nil, nil // 冲突说"有"、重查说"没有"：对手那一行刚被裁决掉
	}
	svc := NewApprovalRequestService(repo, nil)
	row, created, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "p"})
	// 唯一正确的失败方向是"这次动作没拿到批准"，而不是"再开一条待办"。
	if err == nil || created || row != nil {
		t.Fatalf("期望 (nil,false,err)，实际 (%v,%v,%v)", row, created, err)
	}
	if !errors.Is(err, repository.ErrApprovalPendingConflict) {
		t.Errorf("应把冲突原样上抛供调用方重试，实际 %v", err)
	}
	if repo.calls.insert != 1 {
		t.Errorf("不该重试插入，实际 %d 次", repo.calls.insert)
	}
}

func TestApprovalService_SubmitTreatsReadFailureAsFailure(t *testing.T) {
	repo := &fakeApprovalRepo{available: true, getPendingErr: errors.New("connection reset")}
	svc := NewApprovalRequestService(repo, nil)
	_, _, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "p"})
	if err == nil {
		t.Fatal("读故障必须报错")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("应把底层错误带上（排障要看得到原因），实际 %v", err)
	}
	// 关键判据：读失败**没有**被折叠成"没有 pending"，于是不会多出一条可独立批准的待办。
	if repo.calls.insert != 0 {
		t.Errorf("读失败时不该插入，实际 %d 次", repo.calls.insert)
	}
}

func TestApprovalService_SubmitWithoutTokenDoesNotCreateSleepingRow(t *testing.T) {
	previous := approvalResumeTokFn
	approvalResumeTokFn = func() (string, error) { return "", errors.New("rand unavailable") }
	t.Cleanup(func() { approvalResumeTokFn = previous })

	repo := &fakeApprovalRepo{available: true}
	svc := NewApprovalRequestService(repo, nil)
	_, _, err := svc.Submit(context.Background(), ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q", PolicyKey: "p"})
	if err == nil {
		t.Fatal("凭证生成失败应报错")
	}
	// 宁可入队失败也不能留下一条 pending 却无凭证：那条审批永远唤不醒，
	// 而库里看起来一切正常（失败会重试，卡死不会）。
	if repo.calls.insert != 0 {
		t.Errorf("无凭证时不该落库，实际 %d 次", repo.calls.insert)
	}
}

// —— AC①：裁决侧 ——————————————————————————————————————————

func TestApprovalService_DecideRejectsBadInputWithoutTouchingRow(t *testing.T) {
	svc, repo, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-g", PolicyKey: "quote.send"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}

	for _, tc := range []struct {
		name      string
		id        string
		verdict   ApprovalVerdict
		decidedBy string
	}{
		{name: "empty-id", id: "", verdict: ApprovalApprove, decidedBy: "alice"},
		{name: "blank-id", id: "   ", verdict: ApprovalApprove, decidedBy: "alice"},
		{name: "empty-verdict", id: row.ID, verdict: "", decidedBy: "alice"},
		{name: "expired-is-not-a-verdict", id: row.ID, verdict: ApprovalVerdict(model.ApprovalStatusExpired), decidedBy: "alice"},
		{name: "uppercase-verdict", id: row.ID, verdict: "APPROVED", decidedBy: "alice"},
		{name: "no-decider", id: row.ID, verdict: ApprovalApprove, decidedBy: ""},
		{name: "blank-decider", id: row.ID, verdict: ApprovalApprove, decidedBy: " \t "},
		// 人工入口不许写出系统保留值：写了等于伪造一条"人批过了"，
		// 而自动放行率/超时率这两个数正是从这个字段算的。
		{name: "forged-policy-decider", id: row.ID, verdict: ApprovalApprove, decidedBy: model.ApprovalDecidedByPolicy},
		{name: "forged-ttl-decider", id: row.ID, verdict: ApprovalApprove, decidedBy: model.ApprovalDecidedByTTL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.Decide(ctx, tc.id, tc.verdict, tc.decidedBy, "note")
			if !errors.Is(err, ErrApprovalInputInvalid) {
				t.Fatalf("期望 ErrApprovalInputInvalid，实际 (%+v,%v)", got, err)
			}
			if strings.TrimSpace(tc.id) == "" {
				return // 没有指定行，无从比对
			}
			back, rerr := repo.GetByID(ctx, row.ID)
			if rerr != nil || back == nil {
				t.Fatalf("回读失败：(%v,%v)", back, rerr)
			}
			if back.Status != model.ApprovalStatusPending || back.DecidedBy != "" || back.DecidedAt != nil {
				t.Errorf("被拒的裁决请求改动了行：%+v", back)
			}
		})
	}
}

func TestApprovalService_DecideApproveLands(t *testing.T) {
	svc, repo, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-ok", PolicyKey: "quote.send"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	before := *row
	decided, err := svc.Decide(ctx, row.ID, ApprovalApprove, "alice", "客户已签")
	if err != nil {
		t.Fatalf("裁决失败：%v", err)
	}
	if decided.Status != model.ApprovalStatusApproved || decided.DecidedBy != "alice" || decided.DecisionNote != "客户已签" {
		t.Fatalf("裁决结果不对：%+v", decided)
	}
	if decided.DecidedAt == nil {
		t.Error("缺少裁决时刻")
	}
	if model.ApprovalDecidedAutomatically(decided.DecidedBy) {
		t.Error("人工裁决不该被统计读成自动放行")
	}
	// 身份列与凭证不因裁决而变（写集合是白名单）。
	back, err := repo.GetByID(ctx, row.ID)
	if err != nil || back == nil {
		t.Fatalf("回读失败：(%v,%v)", back, err)
	}
	if back.SubjectID != before.SubjectID || back.PolicyKey != before.PolicyKey || back.ResumeToken != before.ResumeToken {
		t.Errorf("裁决改动了身份/凭证：%+v vs %+v", back, before)
	}
	// 返回的那份必须与库一致（不是内存里拼出来的）。
	if back.Status != decided.Status || back.DecidedBy != decided.DecidedBy {
		t.Errorf("返回与库不一致：库 %+v / 返回 %+v", back, decided)
	}
}

// TestApprovalService_DecideRejectLands 是跃迁表在**服务写路径**上的探针。
//
// 变异（从 model 的表里删掉 pending→rejected 这条边）应当让本用例红在
// ErrApprovalIllegalTransition 上，而不只是红在 model 包的表测试上 —— 前者才证明
// 服务真的查了这张表，后者只证明"表少了一条边"。
// 本卡变异电池 Mu-T1 已实跑过该变异（红点见下面的错误分支），那份"拦在落库前"的
// 证据因此已内联成断言，不再依赖注释转述。
func TestApprovalService_DecideRejectLands(t *testing.T) {
	svc, repo, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-rj", PolicyKey: "quote.send"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	decided, err := svc.Decide(ctx, row.ID, ApprovalReject, "bob", "折扣过高")
	if err != nil {
		// 失败**必须**什么都没改。这一句是给"跃迁表被改坏"那种漂移准备的：
		// 变异电池实测（摘掉表里的 pending→rejected）本用例红在
		// `ErrApprovalIllegalTransition` 上，而紧跟的回读确认行**仍是 pending**
		// ⇒ 服务确实拦在落库之前，不是"写完再报错"（后者会把行改成表外状态还报成功）。
		if back, e := repo.GetByID(ctx, row.ID); e != nil || back == nil || back.Status != model.ApprovalStatusPending {
			t.Errorf("裁决失败却已改写库：期望仍是 pending，实际 %+v (err=%v)；本次错误=%v", back, e, err)
		}
		t.Fatalf("拒绝裁决失败：%v", err)
	}
	if decided.Status != model.ApprovalStatusRejected {
		t.Fatalf("期望 rejected，实际 %s", decided.Status)
	}
	if back, e := repo.GetByID(ctx, row.ID); e != nil || back == nil || back.Status != model.ApprovalStatusRejected {
		t.Errorf("拒绝未落库：%+v (%v)", back, e)
	}
}

func TestApprovalService_DecideOnDecidedRowReturnsCurrentWithItsAnswer(t *testing.T) {
	svc, _, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-twice", PolicyKey: "quote.send"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := svc.Decide(ctx, row.ID, ApprovalReject, "alice", "第一次拒"); err != nil {
		t.Fatalf("首次裁决失败：%v", err)
	}
	// 第二个审批人点"批准"：拿到 ErrApprovalAlreadyDecided **加上当前记录**。
	// 只回一个笼统失败的话，他会以为服务出错了去重试，而真答案是"这件事已经被拒了"。
	got, err := svc.Decide(ctx, row.ID, ApprovalApprove, "bob", "我要批")
	if !errors.Is(err, ErrApprovalAlreadyDecided) {
		t.Fatalf("期望 ErrApprovalAlreadyDecided，实际 %v", err)
	}
	if got == nil || got.Status != model.ApprovalStatusRejected || got.DecidedBy != "alice" || got.DecisionNote != "第一次拒" {
		t.Fatalf("应带回已被拒的那一条（含其说明），实际 %+v", got)
	}
}

func TestApprovalService_DecideUnknownIDIsNotFound(t *testing.T) {
	svc, _, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()
	// "不存在"与"存在但已裁决"必须是两个错误：后者暗示"有人批过了"，
	// 拿着它去查一条不存在的审批会白查一轮。
	if _, err := svc.Decide(ctx, "apr_查无此单", ApprovalApprove, "alice", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("期望 ErrApprovalNotFound，实际 %v", err)
	}
	got, err := svc.Get(ctx, "apr_查无此单")
	if err != nil || got != nil {
		t.Errorf("Get 读不到应是 (nil,nil) 而不是错误（调用方要分得清「没有」和「查不动」）：(%v,%v)", got, err)
	}
}

// TestApprovalService_DecideDistinguishesFailureFromAlreadyDecided 盯的是三种
// 在真库里几乎撞不出来、但决定"要不要外发"的时刻。
//
// 把它们塌成同一个返回的代价很具体：一次数据库抖动被说成"已被他人裁决"，调用方就会
// 拿着一条**并不存在**的人工结论放弃这次动作；反过来，写成功却回读失败时报"已裁决"
// 更是无中生有。
func TestApprovalService_DecideDistinguishesFailureFromAlreadyDecided(t *testing.T) {
	ctx := context.Background()
	dbFailure := errors.New("connection reset by peer")

	sawDBFailure := NewApprovalRequestService(&fakeApprovalRepo{available: true, mutateErr: dbFailure}, nil)
	if _, err := sawDBFailure.Decide(ctx, "apr_fake", ApprovalApprove, "alice", ""); !errors.Is(err, dbFailure) {
		t.Errorf("写路径故障应把底层错误原样上抛，实际 %v", err)
	}

	// 写成功、回读故障：此时"裁决到底生效了吗"无人能答，只能是 error。
	readAfterWriteFailure := NewApprovalRequestService(&fakeApprovalRepo{
		available: true, mutateApplied: true, getByIDErr: dbFailure,
	}, nil)
	if got, err := readAfterWriteFailure.Decide(ctx, "apr_fake", ApprovalApprove, "alice", ""); err == nil || got != nil {
		t.Errorf("期望 (nil,err)，实际 (%v,%v)", got, err)
	}

	// 写成功、回读报"没有这行"（同一瞬间被人删了）：报 NotFound，不是 nil,nil。
	// 调用方下一步是"要不要真的外发"，拿着 nil 无从判断。
	vanished := NewApprovalRequestService(&fakeApprovalRepo{available: true, mutateApplied: true}, nil)
	if _, err := vanished.Decide(ctx, "apr_fake", ApprovalApprove, "alice", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Errorf("期望 ErrApprovalNotFound，实际 %v", err)
	}

	// 写失败（CAS 落败）+ 行不存在：报 NotFound 而不是"已被他人裁决"。
	lostAndMissing := NewApprovalRequestService(&fakeApprovalRepo{available: true}, nil)
	if _, err := lostAndMissing.Decide(ctx, "apr_fake", ApprovalApprove, "alice", ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Errorf("期望 ErrApprovalNotFound，实际 %v", err)
	}
}

// —— 恢复凭证侧（T-P3-02 的读入口，本卡先把语义钉住）——————————

func TestApprovalService_ByResumeTokenStates(t *testing.T) {
	svc, _, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()

	row, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "reach_plan", SubjectID: "rp-bt", PolicyKey: "reach.batch"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	// 等待中：可续跑=false，但**不是错误**。报成错误会让调用方写出一堆 retry，
	// 把一次挂起变成轮询风暴（C2 要的是"不阻塞、不轮询"）。
	got, resumable, err := svc.ByResumeToken(ctx, row.ResumeToken)
	if err != nil || resumable || got == nil || got.ID != row.ID {
		t.Fatalf("pending 期望 (%s,false,nil)，实际 (%+v,%v,%v)", row.ID, got, resumable, err)
	}
	if _, _, err := svc.ByResumeToken(ctx, "  "+row.ResumeToken+"  "); err != nil {
		t.Errorf("凭证两侧空白应被容忍（值来自 checkpoint 里的 JSON）：%v", err)
	}
	// 空凭证必须报错：库里有很多条 token 为空串的 auto-approve 行，
	// 不拦就是任何一次漏传都能"恢复"到一条与它毫无关系的已批准记录上。
	if _, _, err := svc.ByResumeToken(ctx, ""); !errors.Is(err, repository.ErrApprovalTokenEmpty) {
		t.Errorf("期望 ErrApprovalTokenEmpty，实际 %v", err)
	}
	if _, _, err := svc.ByResumeToken(ctx, "   "); !errors.Is(err, repository.ErrApprovalTokenEmpty) {
		t.Errorf("纯空白凭证同样应被拒，实际 %v", err)
	}
	// 未知凭证：查不到，不是错误，也不可续跑。
	if a, resumable, err := svc.ByResumeToken(ctx, "rt_没有这个凭证"); err != nil || resumable || a != nil {
		t.Errorf("期望 (nil,false,nil)，实际 (%v,%v,%v)", a, resumable, err)
	}

	if _, err := svc.Decide(ctx, row.ID, ApprovalApprove, "alice", "批了"); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}
	gotAfter, resumableAfter, err := svc.ByResumeToken(ctx, row.ResumeToken)
	if err != nil || !resumableAfter || gotAfter == nil || gotAfter.Status != model.ApprovalStatusApproved {
		t.Fatalf("批准后应可续跑，实际 (%+v,%v,%v)", gotAfter, resumableAfter, err)
	}

	// 已拒的那条：明确"没放行"，而不是静默不可续跑。
	rejected, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "reach_plan", SubjectID: "rp-rj", PolicyKey: "reach.batch"})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := svc.Decide(ctx, rejected.ID, ApprovalReject, "alice", ""); err != nil {
		t.Fatalf("裁决失败：%v", err)
	}
	if _, _, err := svc.ByResumeToken(ctx, rejected.ResumeToken); !errors.Is(err, ErrApprovalResumeNotPending) {
		t.Errorf("已拒绝期望 ErrApprovalResumeNotPending，实际 %v", err)
	}
}

// —— 到期 ————————————————————————————————————————————————

func TestApprovalService_ExpireOverdueUsesSameClockAsDecision(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	advance := freezeApprovalClock(t, base)
	svc, repo, _ := newApprovalServiceWithDB(t, nil)
	ctx := context.Background()

	soon, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-soon", PolicyKey: "quote.send", TTL: time.Hour})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	later, _, err := svc.Submit(ctx, ApprovalSubmitInput{SubjectType: "quote", SubjectID: "q-later", PolicyKey: "quote.send", TTL: 2 * time.Hour})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}

	// 未到点：一条都不该翻（提前过期 = 把还在等人裁决的审批自己判死）。
	if n, err := svc.ExpireOverdue(ctx, 10); err != nil || n != 0 {
		t.Fatalf("未到点期望 0，实际 (%d,%v)", n, err)
	}

	advance(time.Hour + time.Minute) // now = 01:01 ⇒ 只有 soon 到期
	n, err := svc.ExpireOverdue(ctx, 10)
	if err != nil || n != 1 {
		t.Fatalf("期望翻 1 条，实际 (%d,%v)", n, err)
	}
	expired, err := repo.GetByID(ctx, soon.ID)
	if err != nil || expired == nil {
		t.Fatalf("回读失败：(%v,%v)", expired, err)
	}
	if expired.Status != model.ApprovalStatusExpired {
		t.Fatalf("到期应翻成 expired，实际 %s", expired.Status)
	}
	// 超时**不是**人工拒绝：混成一个值会让拒绝率虚高、且看不到"没人处理"这个真问题。
	if expired.DecidedBy != model.ApprovalDecidedByTTL {
		t.Errorf("decided_by 应是 %q，实际 %q", model.ApprovalDecidedByTTL, expired.DecidedBy)
	}
	if !model.ApprovalDecidedAutomatically(expired.DecidedBy) {
		t.Error("超时不该被读成人工裁决")
	}
	if expired.DecidedAt == nil || !expired.DecidedAt.Equal(base.Add(time.Hour+time.Minute)) {
		t.Errorf("decided_at 应等于本轮清扫的时钟值，实际 %v", expired.DecidedAt)
	}
	if kept, _ := repo.GetByID(ctx, later.ID); kept == nil || kept.Status != model.ApprovalStatusPending {
		t.Errorf("未到期那条不该被动，实际 %+v", kept)
	}

	// 过期不可复活：人工事后补批要新建一条，不能在已超时的行上原地改判。
	if _, err := svc.Decide(ctx, soon.ID, ApprovalApprove, "alice", "事后想起来批"); !errors.Is(err, ErrApprovalAlreadyDecided) {
		t.Errorf("对已过期行裁决期望 ErrApprovalAlreadyDecided，实际 %v", err)
	}
	// 已超时的挂起流程不该还能续跑。
	if _, _, err := svc.ByResumeToken(ctx, soon.ResumeToken); !errors.Is(err, ErrApprovalResumeNotPending) {
		t.Errorf("过期后恢复期望 ErrApprovalResumeNotPending，实际 %v", err)
	}

	advance(2 * time.Hour) // later 也到期（01:01 + 2h = 03:01 > 02:00）
	if n, err := svc.ExpireOverdue(ctx, 1); err != nil || n != 1 {
		t.Fatalf("limit=1 时应只翻 1 条，实际 (%d,%v)", n, err)
	}
	if n, err := svc.ExpireOverdue(ctx, 0); err != nil || n != 0 {
		t.Fatalf("扫完后期望 0 条，实际 (%d,%v)", n, err)
	}
}

func TestApprovalService_ExpireOverduePropagatesRepoError(t *testing.T) {
	repo := &fakeApprovalRepo{available: true, expireErr: errors.New("deadlock detected")}
	svc := NewApprovalRequestService(repo, nil)
	// 清扫失败必须上抛：本表刻意没有内存底座，没有"退回内存再记一笔"这条路。
	if _, err := svc.ExpireOverdue(context.Background(), 7); err == nil {
		t.Fatal("清扫失败应报错")
	}
	if repo.expireSawLimit != 7 {
		t.Errorf("limit 应原样透传，实际 %d", repo.expireSawLimit)
	}
	if repo.expireSawNow.IsZero() {
		t.Error("清扫时刻应来自服务时钟（与 decided_at 同源）")
	}
}

func TestApprovalService_AllowedTransitionsView(t *testing.T) {
	svc := NewApprovalRequestService(&fakeApprovalRepo{available: true}, nil)
	if got := svc.AllowedTransitions(" Pending "); len(got) != 3 {
		t.Errorf("pending 应有 3 条出边，实际 %v", got)
	}
	for _, s := range []string{"approved", "rejected", "expired", "", "未知"} {
		if got := svc.AllowedTransitions(s); len(got) != 0 {
			t.Errorf("%q 不该有出边，实际 %v", s, got)
		}
	}
	// 返回的切片可被调用方排序而不影响全进程词表（这是视图，不是判据本体）。
	got := svc.AllowedTransitions(model.ApprovalStatusPending)
	got[0] = "tampered"
	if again := svc.AllowedTransitions(model.ApprovalStatusPending); again[0] == "tampered" {
		t.Error("AllowedTransitions 返回了共享切片")
	}
}
