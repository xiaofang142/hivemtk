// kb_release_test.go T-P9-02：变更与发布的服务层（全桩，无库）。
//
// service 层按架构门不持任何 GORM 句柄，所以本文件能覆盖到本卡最要命的那几条判据，
// 而且每条都是"只有这一层才判得出"的：
//
//   - AC①（未审批的正文不进线上检索）在这里有两处落点：SubmitChange 的**先入队审批、
//     后落变更行**顺序（反过来得到一条没有审批的 pending 孤儿，它长得像正常待办），
//     以及 PublishPending 的**逐条回读审批**分账（一条都没批时压根不调用事务）。
//   - "读不到审批结论"与"没批"必须是两件事：前者整次发布停下上抛，后者只是不带这条。
//     把前者做成后者，等于把一次故障写成一条业务决定 —— 那是本卡最贵的一条口径。
//   - 审批行的三列（subject_type / subject_id / policy_key）必须逐一对上，少比一列
//     就是给别的对象开门。
//   - 向量补算是"补强"不是"生效"：失败只出声、不让已经提交的事务回滚。
//
// 落库那一段（锁行→分桶→写 chunk→移指针→记审计）在 repository 的带库用例里。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/kbrelease"
	"hivemtk-user/internal/repository"
)

// ---------------------------------------------------------------------------
// 桩
// ---------------------------------------------------------------------------

// stubReleaseStore 覆盖 kbReleaseStore 的全部 15 个方法，并记录调用顺序。
//
// calls 是本文件最重要的字段：SubmitChange 的"先审批后入库"、PublishPending 的
// "一条都没批就不进事务"都是**顺序**判据，只断言最终状态测不出来。
type stubReleaseStore struct {
	available bool

	calls []string

	release    *model.KBRelease
	releaseErr error
	releases   []model.KBRelease

	insertErr  error
	inserted   []*model.KBChangeRequest
	getRow     *model.KBChangeRequest
	getErr     error
	listRows   []model.KBChangeRequest
	listTotal  int64
	listErr    error
	listFilter repository.KBChangeFilter
	pending    []model.KBChangeRequest
	pendErr    error

	withdrawOK  bool
	withdrawErr error

	audits    []model.KBChangeAuditLog
	auditErr  error
	auditRows []model.KBChangeAuditLog
	auditErr2 error
	auditKey  string
	auditLim  int

	publishIntents []repository.PublishIntent
	publishRes     *repository.PublishResult
	publishErr     error

	rollbackRes *repository.RollbackResult
	rollbackErr error
	restoreRes  *repository.RollbackResult
	restoreErr  error
	governedGot []bool

	inForce, total int64
	countErr       error
}

func (s *stubReleaseStore) Available() bool { return s.available }

func (s *stubReleaseStore) call(n string) { s.calls = append(s.calls, n) }

func (s *stubReleaseStore) GetRelease(ctx context.Context, productID string) (*model.KBRelease, error) {
	s.call("GetRelease")
	return s.release, s.releaseErr
}

func (s *stubReleaseStore) ListReleases(ctx context.Context) ([]model.KBRelease, error) {
	s.call("ListReleases")
	return s.releases, nil
}

func (s *stubReleaseStore) SetGoverned(ctx context.Context, productID string, governed bool, actor string) (*model.KBRelease, error) {
	s.call("SetGoverned")
	s.governedGot = append(s.governedGot, governed)
	if s.release == nil {
		s.release = &model.KBRelease{ID: 1, ProductID: productID}
	}
	s.release.Governed = governed
	return s.release, s.releaseErr
}

func (s *stubReleaseStore) InsertChange(ctx context.Context, ch *model.KBChangeRequest) error {
	s.call("InsertChange")
	s.inserted = append(s.inserted, ch)
	return s.insertErr
}

func (s *stubReleaseStore) GetChange(ctx context.Context, id string) (*model.KBChangeRequest, error) {
	s.call("GetChange")
	return s.getRow, s.getErr
}

func (s *stubReleaseStore) ListChanges(ctx context.Context, f repository.KBChangeFilter) ([]model.KBChangeRequest, int64, error) {
	s.call("ListChanges")
	s.listFilter = f
	return s.listRows, s.listTotal, s.listErr
}

func (s *stubReleaseStore) PendingChanges(ctx context.Context, productID string) ([]model.KBChangeRequest, error) {
	s.call("PendingChanges")
	return s.pending, s.pendErr
}

func (s *stubReleaseStore) WithdrawChange(ctx context.Context, id string) (bool, error) {
	s.call("WithdrawChange")
	return s.withdrawOK, s.withdrawErr
}

func (s *stubReleaseStore) RecordAudit(ctx context.Context, log model.KBChangeAuditLog) error {
	s.call("RecordAudit")
	s.audits = append(s.audits, log)
	return s.auditErr
}

func (s *stubReleaseStore) ListAuditBySubject(ctx context.Context, key string, limit int) ([]model.KBChangeAuditLog, error) {
	s.call("ListAuditBySubject")
	s.auditKey, s.auditLim = key, limit
	return s.auditRows, s.auditErr2
}

func (s *stubReleaseStore) Publish(ctx context.Context, in repository.PublishIntent) (*repository.PublishResult, error) {
	s.call("Publish")
	s.publishIntents = append(s.publishIntents, in)
	if s.publishRes == nil && s.publishErr == nil {
		// 真实仓储在成功那一回必回非 nil 的结果。让桩回 (nil, nil) 的话，
		// 任何把"该被拒的写放行"的变异都会在服务侧读 res.FromVersion 时崩掉，
		// 一次 panic 带走整个测试二进制，那一格往后所有用例的 settled 计数都不可信。
		return &repository.PublishResult{}, nil
	}
	return s.publishRes, s.publishErr
}

func (s *stubReleaseStore) Rollback(ctx context.Context, productID, actor string) (*repository.RollbackResult, error) {
	s.call("Rollback")
	return s.rollbackRes, s.rollbackErr
}

func (s *stubReleaseStore) Restore(ctx context.Context, productID, actor string) (*repository.RollbackResult, error) {
	s.call("Restore")
	return s.restoreRes, s.restoreErr
}

func (s *stubReleaseStore) CountChunksInForce(ctx context.Context, productID string) (int64, int64, error) {
	s.call("CountChunksInForce")
	return s.inForce, s.total, s.countErr
}

// stubApprovals 审批门面。get 用函数而不是表：分账用例要按每条变更给不同结论。
type stubApprovals struct {
	calls     *[]string
	submitRes *model.ApprovalRequest
	submitErr error
	submitOut []ApprovalSubmitInput
	submitSeq int
	getFn     func(id string) (*model.ApprovalRequest, error)
}

func (a *stubApprovals) Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
	*a.calls = append(*a.calls, "ApprovalSubmit")
	a.submitOut = append(a.submitOut, in)
	a.submitSeq++
	if a.submitErr != nil {
		return nil, false, a.submitErr
	}
	if a.submitRes == nil {
		return &model.ApprovalRequest{ID: "apr_stub", Status: model.ApprovalStatusPending}, true, nil
	}
	return a.submitRes, true, nil
}

func (a *stubApprovals) Get(ctx context.Context, id string) (*model.ApprovalRequest, error) {
	*a.calls = append(*a.calls, "ApprovalGet:"+id)
	if a.getFn == nil {
		return nil, nil
	}
	return a.getFn(id)
}

type kbEmbedStub struct {
	calls   int
	product string
	ids     []uint64
	err     error
}

func (e *kbEmbedStub) EmbedAndPersistChunks(ctx context.Context, productID string, chunks []model.KnowledgeChunk) error {
	e.calls++
	e.product = productID
	for _, c := range chunks {
		e.ids = append(e.ids, c.ID)
	}
	return e.err
}

// kbTestApproval 造一条"确实是这条变更的"审批行。
//
// 三列必须一起给全：service 逐列比对（subject_type / subject_id / policy_key），
// 少填一列的用例只会全部退化成"结论不明"，把分账判据测成空转。
func kbTestApproval(id, changeID, status string) *model.ApprovalRequest {
	return &model.ApprovalRequest{
		ID: id, SubjectType: KBChangeApprovalSubjectType, SubjectID: changeID,
		PolicyKey: KBChangeApprovalPolicyKey, Status: status,
	}
}

// newKBService 造一套默认桩：底座可用、审批入队成功、读回结论按 changes 表给。
func newKBService(t *testing.T) (*KBReleaseService, *stubReleaseStore, *stubApprovals) {
	t.Helper()
	st := &stubReleaseStore{available: true}
	ap := &stubApprovals{calls: &st.calls}
	svc := NewKBReleaseService(st, ap, nil)
	svc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return svc, st, ap
}

func kbChangeRow(id, product, approvalID string) model.KBChangeRequest {
	return model.KBChangeRequest{
		ID: id, ProductID: product, Op: model.KBChangeOpAdd, Content: "正文 " + id,
		Reason: "理由", Status: model.KBChangeStatusPending, ApprovalID: approvalID, RequestedBy: "op-1",
	}
}

// kbGoverned 给桩装一行"已进发布制"的发布指针。
//
// PublishPending 现在先问这一条再问待办（未进治理回 409，而不是"没有可发布的变更"），
// 所以每个走发布路径的用例都要**显式**装这一行：把它做成 newKBService 的默认值，
// "发布不查治理开关"这个变异就再也没有用例能发现它。
// bucket>0 是"导入链路已经打过待发布戳"的形状（见 PublishPending 里那条桶判据）。
func kbGoverned(bucket int) *model.KBRelease {
	return &model.KBRelease{ID: 1, ProductID: "p1", Governed: true, DraftVersion: bucket}
}

// ---------------------------------------------------------------------------
// 未装配 / 空接收者
// ---------------------------------------------------------------------------

// TestKBRelease_UnavailableEverywhere 每个公开方法都要在 Available 为假时回那句
// "底座未装配"，而不是回空结果：空列表会被运营读成"这个库一条待办都没有"。
func TestKBRelease_UnavailableEverywhere(t *testing.T) {
	ctx := context.Background()
	noStore := NewKBReleaseService(&stubReleaseStore{available: false}, &stubApprovals{calls: new([]string)}, nil)
	noApprovals := NewKBReleaseService(&stubReleaseStore{available: true}, nil, nil)
	var nilSvc *KBReleaseService

	for i, svc := range []*KBReleaseService{noStore, noApprovals, nilSvc} {
		if svc.Available() {
			t.Fatalf("第 %d 套依赖不该是可用", i)
		}
		if _, err := svc.SubmitChange(ctx, KBChangeSubmitInput{}); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] SubmitChange=%v", i, err)
		}
		if _, err := svc.WithdrawChange(ctx, "kbc_1", "op"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] WithdrawChange=%v", i, err)
		}
		if _, _, err := svc.PublishPending(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] PublishPending=%v", i, err)
		}
		if _, err := svc.Rollback(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] Rollback=%v", i, err)
		}
		if _, err := svc.Restore(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] Restore=%v", i, err)
		}
		if _, err := svc.SetGoverned(ctx, "p1", true, "op"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] SetGoverned=%v", i, err)
		}
		if _, err := svc.GetRelease(ctx, "p1"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] GetRelease=%v", i, err)
		}
		if _, err := svc.ListReleases(ctx); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] ListReleases=%v", i, err)
		}
		if _, _, err := svc.ListChanges(ctx, repository.KBChangeFilter{}); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] ListChanges=%v", i, err)
		}
		if _, err := svc.GetChange(ctx, "kbc_1"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] GetChange=%v", i, err)
		}
		if _, err := svc.ListAudit(ctx, "change:kbc_1", 10); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] ListAudit=%v", i, err)
		}
		if _, err := svc.Stats(ctx, "p1"); !errors.Is(err, ErrKBReleaseUnavailable) {
			t.Errorf("[%d] Stats=%v", i, err)
		}
	}
}

// TestKBRelease_GlobalRegistryRoundTrip 全局登记处：set→get→撤掉→get 为 nil。
//
// 撤掉这一半必须测：InitKBReleaseRuntime(nil) 靠的就是"传 nil 等于端点回 503"，
// 而路由是现取全局的 —— 忘了清空会留下"上一份实例还在回 200"的窗口。
func TestKBRelease_GlobalRegistryRoundTrip(t *testing.T) {
	before := GlobalKBReleaseService()
	t.Cleanup(func() { SetGlobalKBReleaseService(before) })

	svc, _, _ := newKBService(t)
	SetGlobalKBReleaseService(svc)
	if GlobalKBReleaseService() != svc {
		t.Fatal("登记后取回的应是同一实例")
	}
	SetGlobalKBReleaseService(nil)
	if GlobalKBReleaseService() != nil {
		t.Fatal("传 nil 应撤掉登记（端点回 503）")
	}
}

// ---------------------------------------------------------------------------
// 入参判据
// ---------------------------------------------------------------------------

func TestKBRelease_SubmitInputNormalization(t *testing.T) {
	base := func() KBChangeSubmitInput {
		return KBChangeSubmitInput{
			ProductID: "  p1  ", Op: " ADD ", Content: "正文", DocumentID: 7,
			Reason: "  要改  ", RequestedBy: " op-1 ",
		}
	}
	in, err := base().normalize()
	if err != nil {
		t.Fatalf("合法入参不该报错: %v", err)
	}
	if in.ProductID != "p1" || in.Op != model.KBChangeOpAdd || in.Reason != "要改" || in.RequestedBy != "op-1" {
		t.Errorf("去空白/归一未做全：%+v", in)
	}

	cases := []struct {
		name   string
		mut    func(*KBChangeSubmitInput)
		expect string
	}{
		{"product 为空", func(in *KBChangeSubmitInput) { in.ProductID = "   " }, "product_id 为空"},
		{"product 超列宽", func(in *KBChangeSubmitInput) { in.ProductID = strings.Repeat("p", kbChangeProductMaxLen+1) }, "超列宽"},
		{"理由为空", func(in *KBChangeSubmitInput) { in.Reason = "" }, "reason 为空"},
		{"理由超长", func(in *KBChangeSubmitInput) { in.Reason = strings.Repeat("理", kbChangeReasonMaxLen+1) }, "超 "},
		{"发起人为空", func(in *KBChangeSubmitInput) { in.RequestedBy = "" }, "requested_by 为空"},
		{"发起人超长", func(in *KBChangeSubmitInput) { in.RequestedBy = strings.Repeat("a", kbChangeActorMaxLen+1) }, "超"},
		{"动作不认识", func(in *KBChangeSubmitInput) { in.Op = "delete" }, "op="},
		{"内容超上限", func(in *KBChangeSubmitInput) { in.Content = strings.Repeat("字", kbChangeContentMaxRunes+1) }, "超单条分段上限"},
		{"add 无内容", func(in *KBChangeSubmitInput) { in.Content = "" }, "add 必须有内容"},
		{"add 无文档", func(in *KBChangeSubmitInput) { in.DocumentID = 0 }, "add 必须指定 document_id"},
		{"add 带目标分段", func(in *KBChangeSubmitInput) { in.TargetChunkID = 9 }, "add 不该带 target_chunk_id"},
		{"revise 无新内容", func(in *KBChangeSubmitInput) { in.Op = "revise"; in.TargetChunkID = 9; in.Content = "" }, "revise 必须有新内容"},
		{"revise 无目标", func(in *KBChangeSubmitInput) { in.Op = "revise"; in.TargetChunkID = 0 }, "revise 必须指定"},
		{"retire 无目标", func(in *KBChangeSubmitInput) { in.Op = "retire"; in.Content = ""; in.TargetChunkID = 0 }, "retire 必须指定"},
		{"retire 带内容", func(in *KBChangeSubmitInput) { in.Op = "retire"; in.TargetChunkID = 9 }, "retire 不该带 content"},
	}
	for _, c := range cases {
		in := base()
		c.mut(&in)
		_, err := in.normalize()
		if err == nil {
			t.Errorf("%s：应报错而没有", c.name)
			continue
		}
		if !errors.Is(err, ErrKBChangeInputInvalid) {
			t.Errorf("%s：错误类型应是 ErrKBChangeInputInvalid，实得 %v", c.name, err)
		}
		if !strings.Contains(err.Error(), c.expect) {
			t.Errorf("%s：报错文案应含 %q，实得 %q", c.name, c.expect, err.Error())
		}
	}
}

// TestKBRelease_WriteArgs 写路径共用的三格参数判据（含列宽闸）。
//
// 列宽必须在这里挡：product_id 是 varchar(64)，超长的写入到库里才报错的话，
// 控制层会得到 500，而同一件事在提交入口是 400 —— 同一个原因两种码，运营学不会。
func TestKBRelease_WriteArgs(t *testing.T) {
	if p, a, err := kbWriteArgs("  p1 ", " op "); err != nil || p != "p1" || a != "op" {
		t.Errorf("合法入参应去空白后返回：%q %q %v", p, a, err)
	}
	for _, bad := range [][2]string{
		{"", "op"}, {"p1", ""}, {"   ", "op"}, {"p1", "   "},
		{strings.Repeat("p", KBReleaseProductMaxLen+1), "op"},
	} {
		if _, _, err := kbWriteArgs(bad[0], bad[1]); !errors.Is(err, ErrKBChangeInputInvalid) {
			t.Errorf("kbWriteArgs(%q,%q) 应判入参不合法，实得 %v", bad[0], bad[1], err)
		}
	}
	if _, _, err := kbWriteArgs(strings.Repeat("p", KBReleaseProductMaxLen), "op"); err != nil {
		t.Errorf("恰好等于列宽应放行: %v", err)
	}
}

// TestKBRelease_ChangeIDErrsOnOverflow 变更号超 40 必须**报错而不是截断**。
//
// 截断后的号写进 knowledge_chunks.change_id，变更与分段就对不上账 —— 而那正是 AC③
// 要回答的问题（"这一版上线的是哪条变更"）。这里把序号推到极长来逼出这条分支。
func TestKBRelease_ChangeIDErrsOnOverflow(t *testing.T) {
	svc, _, _ := newKBService(t)
	svc.seq.Store(9_999_999_999_999_999_999)
	if _, err := svc.changeID(); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Fatalf("超长变更号应报错，实得 %v", err)
	}
	svc.seq.Store(0)
	first, err := svc.changeID()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) > kbChangeIDMaxLen {
		t.Errorf("号长 %d 超 %d", len(first), kbChangeIDMaxLen)
	}
	svc.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	second, err := svc.changeID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("同一时刻两次取号相同（%s）：自增位没生效，同库并发提交会撞主键", first)
	}
}

// ---------------------------------------------------------------------------
// SubmitChange：AC① 的顺序判据
// ---------------------------------------------------------------------------

func TestKBRelease_SubmitChangeHappyPath(t *testing.T) {
	svc, st, ap := newKBService(t)
	ap.submitRes = &model.ApprovalRequest{ID: "apr_77", Status: model.ApprovalStatusPending}

	ch, err := svc.SubmitChange(context.Background(), KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 7,
		Reason: "补一条价目", RequestedBy: "op-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 顺序：审批必须先于入库。
	if len(st.calls) < 2 || st.calls[0] != "ApprovalSubmit" || st.calls[1] != "InsertChange" {
		t.Fatalf("调用顺序应为 审批→入库，实得 %v", st.calls)
	}
	if len(ap.submitOut) != 1 {
		t.Fatalf("审批应只入队一次，实得 %d", len(ap.submitOut))
	}
	got := ap.submitOut[0]
	if got.SubjectType != KBChangeApprovalSubjectType || got.PolicyKey != KBChangeApprovalPolicyKey {
		t.Errorf("审批命名键错：(%s,%s)", got.SubjectType, got.PolicyKey)
	}
	if got.SubjectID != ch.ID {
		t.Errorf("审批的 subject_id=%q 应等于变更号 %q（回读时按这三列认结论）", got.SubjectID, ch.ID)
	}
	if ch.Status != model.KBChangeStatusPending || ch.ApprovalID != "apr_77" || ch.RequestedBy != "op-1" {
		t.Errorf("变更行形状错：%+v", ch)
	}
	if ch.ReleaseVersion != 0 || ch.AppliedChunkID != 0 {
		t.Error("提交阶段不该写生效号或指认分段（那是发布的事）")
	}
	if len(st.audits) != 1 || st.audits[0].Action != model.KBAuditSubmitted ||
		st.audits[0].SubjectKey != "change:"+ch.ID || st.audits[0].Actor != "op-1" {
		t.Errorf("submitted 留痕缺失或形状错：%+v", st.audits)
	}
}

func TestKBRelease_SubmitChangeApprovalFailureLeavesNoRow(t *testing.T) {
	svc, st, ap := newKBService(t)
	ap.submitErr = errors.New("审批底座挂了")
	if _, err := svc.SubmitChange(context.Background(), KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 1,
		Reason: "r", RequestedBy: "op-1",
	}); err == nil {
		t.Fatal("审批入队失败应上抛")
	}
	if len(st.inserted) != 0 || len(st.audits) != 0 {
		t.Errorf("审批没入队就不该有变更行/留痕：inserted=%d audits=%d", len(st.inserted), len(st.audits))
	}
}

// TestKBRelease_SubmitChangeNilApprovalWithoutError 实现漂了的形态：Submit 既不给行也不给错。
//
// 这一格防的是"审批服务换成一个返回 (nil,nil) 的替身/半初始化实例" ⇒ 变更行落库却没有
// 审批可回读，发布时只能按"没批"处理，而它在待办列表里长得像一条正常待办（AC① 的反例）。
func TestKBRelease_SubmitChangeNilApprovalWithoutError(t *testing.T) {
	svc, st, _ := newKBService(t)
	// 直接换成回 (nil,nil) 的实现（桩的默认分支会给一行，这里要的是"漂了"的那种）。
	svc.approvals = &nilApprovalGate{calls: &st.calls}

	if _, err := svc.SubmitChange(context.Background(), KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 1,
		Reason: "r", RequestedBy: "op-1",
	}); !errors.Is(err, ErrKBReleaseApprovalMissing) {
		t.Errorf("应判 ErrKBReleaseApprovalMissing，实得 %v", err)
	}
	if len(st.inserted) != 0 {
		t.Error("没拿到审批结论就不该落变更行")
	}
}

type nilApprovalGate struct{ calls *[]string }

func (g *nilApprovalGate) Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
	*g.calls = append(*g.calls, "ApprovalSubmit")
	return nil, false, nil
}

func (g *nilApprovalGate) Get(ctx context.Context, id string) (*model.ApprovalRequest, error) {
	return nil, nil
}

func TestKBRelease_SubmitChangeInsertFailure(t *testing.T) {
	svc, st, _ := newKBService(t)
	st.insertErr = errors.New("唯一索引冲突")
	_, err := svc.SubmitChange(context.Background(), KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 1,
		Reason: "r", RequestedBy: "op-1",
	})
	if err == nil || !strings.Contains(err.Error(), "审批") {
		t.Fatalf("报错要点名那条已入队的审批（运维据此判断会不会有孤儿）: %v", err)
	}
	if !strings.Contains(err.Error(), "apr_stub") {
		t.Errorf("报错里应带审批号，实得 %q", err.Error())
	}
	if len(st.audits) != 0 {
		t.Error("变更行没落库就不该记 submitted 留痕（留痕指向一条不存在的变更更糟）")
	}
}

// TestKBRelease_SubmitChangeAuditFailureStillSucceeds 留痕失败只出声：审批与变更都已落库，
// 回滚反而制造孤儿。AC③ 少一条是可补的，静默少一条才是问题 —— 所以这里是"成功返回"。
func TestKBRelease_SubmitChangeAuditFailureStillSucceeds(t *testing.T) {
	svc, st, _ := newKBService(t)
	st.auditErr = errors.New("审计表写入失败")
	ch, err := svc.SubmitChange(context.Background(), KBChangeSubmitInput{
		ProductID: "p1", Op: model.KBChangeOpAdd, Content: "正文", DocumentID: 1,
		Reason: "r", RequestedBy: "op-1",
	})
	if err != nil || ch == nil {
		t.Fatalf("留痕失败不该让整个提交失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WithdrawChange
// ---------------------------------------------------------------------------

func TestKBRelease_WithdrawChange(t *testing.T) {
	ctx := context.Background()
	t.Run("入参判据", func(t *testing.T) {
		svc, _, _ := newKBService(t)
		if _, err := svc.WithdrawChange(ctx, " ", "op"); !errors.Is(err, ErrKBChangeInputInvalid) {
			t.Errorf("空 id 应判入参: %v", err)
		}
		if _, err := svc.WithdrawChange(ctx, "kbc_1", ""); !errors.Is(err, ErrKBChangeInputInvalid) {
			t.Errorf("空 actor 应判入参（留痕要归人）: %v", err)
		}
	})
	t.Run("查无此条", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		st.getRow = nil
		if _, err := svc.WithdrawChange(ctx, "kbc_x", "op"); !errors.Is(err, ErrKBChangeNotFound) {
			t.Errorf("应判 404 类错误，实得 %v", err)
		}
	})
	t.Run("读故障上抛", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		st.getErr = errors.New("连接断了")
		if _, err := svc.WithdrawChange(ctx, "kbc_1", "op"); err == nil || errors.Is(err, ErrKBChangeNotFound) {
			t.Errorf("读故障不能被当成查无此条: %v", err)
		}
	})
	t.Run("已发布的撤不掉", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		row := kbChangeRow("kbc_1", "p1", "apr_1")
		row.Status = model.KBChangeStatusApplied
		st.getRow = &row
		_, err := svc.WithdrawChange(ctx, "kbc_1", "op")
		if !errors.Is(err, ErrKBChangeNotWithdrawable) {
			t.Fatalf("applied 不该能直接撤回: %v", err)
		}
		if !strings.Contains(err.Error(), "retire") {
			t.Errorf("报错要给出下一步（改提一条 retire 走审批），实得 %q", err.Error())
		}
		if len(st.calls) != 1 || st.calls[0] != "GetChange" {
			t.Errorf("判据不过时不该再碰仓储的写：%v", st.calls)
		}
	})
	t.Run("撤回成功", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		row := kbChangeRow("kbc_1", "p1", "apr_1")
		st.getRow = &row
		st.withdrawOK = true
		if _, err := svc.WithdrawChange(ctx, "kbc_1", " op-2 "); err != nil {
			t.Fatal(err)
		}
		if len(st.audits) != 1 || st.audits[0].Action != model.KBAuditWithdrawn ||
			st.audits[0].Actor != "op-2" || st.audits[0].OldValue != model.KBChangeStatusPending ||
			st.audits[0].NewValue != model.KBChangeStatusWithdrawn {
			t.Errorf("withdrawn 留痕形状错：%+v", st.audits)
		}
	})
	t.Run("CAS 被抢先", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		row := kbChangeRow("kbc_1", "p1", "apr_1")
		st.getRow = &row
		st.withdrawOK = false // 刚判完能撤，别人在这中间把它发布了
		_, err := svc.WithdrawChange(ctx, "kbc_1", "op")
		if !errors.Is(err, ErrKBChangeNotWithdrawable) {
			t.Fatalf("CAS 没中应报撤不了: %v", err)
		}
		if len(st.audits) != 0 {
			t.Error("没改成行就不该记留痕（记了就是审计里多一条没发生的事）")
		}
	})
	t.Run("留痕失败上抛", func(t *testing.T) {
		svc, st, _ := newKBService(t)
		row := kbChangeRow("kbc_1", "p1", "apr_1")
		st.getRow = &row
		st.withdrawOK = true
		st.auditErr = errors.New("审计表写入失败")
		if _, err := svc.WithdrawChange(ctx, "kbc_1", "op"); err == nil {
			t.Fatal("撤回的留痕失败必须上抛：状态已改而查不到是谁改的，AC③ 就断了")
		}
	})
}

// ---------------------------------------------------------------------------
// PublishPending：AC① 的分账判据
// ---------------------------------------------------------------------------

func TestKBRelease_PublishPendingVerdicts(t *testing.T) {
	pending := []model.KBChangeRequest{
		kbChangeRow("kbc_ok", "p1", "apr_ok"),
		kbChangeRow("kbc_wait", "p1", "apr_wait"),
		kbChangeRow("kbc_no", "p1", "apr_no"),
		kbChangeRow("kbc_exp", "p1", "apr_exp"),
		kbChangeRow("kbc_bogus", "p1", "apr_bogus"),
		kbChangeRow("kbc_noid", "p1", ""),           // 没有审批号
		kbChangeRow("kbc_gone", "p1", "apr_gone"),   // 审批行查不到
		kbChangeRow("kbc_other", "p1", "apr_other"), // subject_type 是别人的对象
		kbChangeRow("kbc_wrong", "p1", "apr_wrong"), // 三列里 subject_id 对不上
		kbChangeRow("kbc_pol", "p1", "apr_pol"),     // 三列里 policy_key 不是本卡那条策略
	}
	table := map[string]*model.ApprovalRequest{
		"apr_ok":    kbTestApproval("apr_ok", "kbc_ok", model.ApprovalStatusApproved),
		"apr_wait":  kbTestApproval("apr_wait", "kbc_wait", model.ApprovalStatusPending),
		"apr_no":    kbTestApproval("apr_no", "kbc_no", model.ApprovalStatusRejected),
		"apr_exp":   kbTestApproval("apr_exp", "kbc_exp", model.ApprovalStatusExpired),
		"apr_bogus": kbTestApproval("apr_bogus", "kbc_bogus", "someday"),
		"apr_gone":  nil,
		"apr_other": {ID: "apr_other", SubjectType: "quote", SubjectID: "kbc_other", PolicyKey: KBChangeApprovalPolicyKey, Status: model.ApprovalStatusApproved},
		// 类型与 policy 都对、唯独 subject_id 指着另一条：少比这一列等于给别的变更开门。
		"apr_wrong": kbTestApproval("apr_wrong", "kbc_someone-else", model.ApprovalStatusApproved),
		// 类型与 subject_id 都对、唯独策略键不是本卡那条：审批是别套规则给的结论。
		"apr_pol": {
			ID: "apr_pol", SubjectType: KBChangeApprovalSubjectType, SubjectID: "kbc_pol",
			PolicyKey: "ltc.some.other_policy", Status: model.ApprovalStatusApproved,
		},
	}
	svc, st, ap := newKBService(t)
	st.release = kbGoverned(0)
	st.pending = pending
	ap.getFn = func(id string) (*model.ApprovalRequest, error) { return table[id], nil }
	st.publishRes = &repository.PublishResult{FromVersion: 3, ToVersion: 4}

	v, res, err := svc.PublishPending(context.Background(), "p1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("有一条批准就该进事务")
	}
	if len(v.Included) != 1 || v.Included[0] != "kbc_ok" {
		t.Errorf("included 应恰好是 kbc_ok，实得 %v", v.Included)
	}
	if len(v.SkippedPending) != 1 || v.SkippedPending[0] != "kbc_wait" {
		t.Errorf("skipped_pending=%v", v.SkippedPending)
	}
	if len(v.SkippedRefused) != 1 || v.SkippedRefused[0] != "kbc_no" {
		t.Errorf("skipped_refused=%v", v.SkippedRefused)
	}
	// unknown 六条：状态无法识别 / 无审批号 / 审批行查不到 / 类型是别的对象 / subject_id 对不上 /
	// policy_key 不是本卡那条策略。
	if len(v.SkippedExpired) != 1 || len(v.SkippedUnknown) != 6 {
		t.Errorf("skipped_expired=%v unknown=%v", v.SkippedExpired, v.SkippedUnknown)
	}
	// 三列对不上的必须落进 unknown（它们是"别的对象的审批"，不是"没批"）。
	if !kbHasStr(v.SkippedUnknown, "kbc_other") || !kbHasStr(v.SkippedUnknown, "kbc_noid") ||
		!kbHasStr(v.SkippedUnknown, "kbc_gone") || !kbHasStr(v.SkippedUnknown, "kbc_wrong") ||
		!kbHasStr(v.SkippedUnknown, "kbc_pol") {
		t.Errorf("unknown 分账缺项：%v", v.SkippedUnknown)
	}
	// 事务只带那一条批准的变更，且带的是裁决完的意图。
	if len(st.publishIntents) != 1 || len(st.publishIntents[0].Changes) != 1 {
		t.Fatalf("Publish 入参=%+v", st.publishIntents)
	}
	in := st.publishIntents[0]
	if in.ProductID != "p1" || in.Actor != "op-1" {
		t.Errorf("intent 归属/操作者错：%+v", in)
	}
	c := in.Changes[0]
	if c.ID != "kbc_ok" || c.Op != model.KBChangeOpAdd || c.ProductID != "p1" {
		t.Errorf("intent 里的变更错：%+v", c)
	}
	// 每条待办都回读过审批（不是只读第一条）。
	for _, id := range []string{"apr_ok", "apr_wait", "apr_no", "apr_exp", "apr_bogus", "apr_gone", "apr_other", "apr_wrong", "apr_pol"} {
		if !kbHasStr(st.calls, "ApprovalGet:"+id) {
			t.Errorf("没有回读审批 %s：逐条问资格是本卡的地基", id)
		}
	}
}

func TestKBRelease_PublishPendingApprovalReadFailureStopsEverything(t *testing.T) {
	svc, st, ap := newKBService(t)
	st.release = kbGoverned(0)
	st.pending = []model.KBChangeRequest{
		kbChangeRow("kbc_ok", "p1", "apr_ok"),
		kbChangeRow("kbc_bad", "p1", "apr_bad"),
	}
	ap.getFn = func(id string) (*model.ApprovalRequest, error) {
		if id == "apr_bad" {
			return nil, errors.New("审批表读取失败")
		}
		// 夹具约定：审批号 apr_X 属于变更 kbc_X，故第一条已批准、第二条读不到 ⇒ 整次停下。
		return kbTestApproval(id, strings.Replace(id, "apr_", "kbc_", 1), model.ApprovalStatusApproved), nil
	}
	_, _, err := svc.PublishPending(context.Background(), "p1", "op-1")
	if err == nil {
		t.Fatal("读结论失败必须整次发布停下")
	}
	if !strings.Contains(err.Error(), "kbc_bad") {
		t.Errorf("报错要点名读不到结论的那条变更: %q", err.Error())
	}
	for _, c := range st.calls {
		if c == "Publish" {
			t.Fatal(`读不到结论时绝不能落库：把"读不到"当成"没批"，等于让一次故障被写成一条业务决定`)
		}
	}
}

// TestKBRelease_PublishPendingNothingApprovedSkipsTransaction 一条都没批、桶也是空的：
// 不进事务（那会留下一条不存在的发布留痕），但结论照常分账。
// 与 TestKBRelease_PublishPendingDraftBucketWithoutChanges 成对：那边是"空变更 + 有桶"，
// 必须进事务 —— 少这一句区分的实现会把导入的内容永远攒在桶里。
func TestKBRelease_PublishPendingNothingApprovedSkipsTransaction(t *testing.T) {
	svc, st, ap := newKBService(t)
	st.release = kbGoverned(0)
	st.pending = []model.KBChangeRequest{kbChangeRow("kbc_wait", "p1", "apr_wait")}
	ap.getFn = func(id string) (*model.ApprovalRequest, error) {
		return kbTestApproval(id, "kbc_wait", model.ApprovalStatusPending), nil
	}
	v, res, err := svc.PublishPending(context.Background(), "p1", "op-1")
	if err != nil || res != nil {
		t.Fatalf("一条都没批：无结果、无错误，实得 %v/%v", res, err)
	}
	if len(v.SkippedPending) != 1 {
		t.Errorf("待审数该带上：%+v", v)
	}
	for _, c := range st.calls {
		if c == "Publish" {
			t.Fatal("不该调用事务：空指针移动会留下一条误导性发布留痕")
		}
	}
}

func TestKBRelease_PublishPendingArgsAndStoreErrors(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newKBService(t)
	st.release = kbGoverned(0)
	if _, _, err := svc.PublishPending(ctx, "  ", "op"); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("空库号应判入参: %v", err)
	}
	if _, _, err := svc.PublishPending(ctx, "p1", " "); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("空操作者应判入参（发布要归人）: %v", err)
	}
	// 读指针失败要原样上抛：把它折成"未进发布制"等于把一次故障写成一条业务决定
	// —— 运营照着那句只会去点"启用发布制"，而真正该查的是这次读为什么失败。
	st.releaseErr = errors.New("kb_releases 读取失败")
	if _, _, err := svc.PublishPending(ctx, "p1", "op"); err == nil ||
		strings.Contains(err.Error(), "未启用发布制") {
		t.Errorf("读指针失败不该说成未进发布制：%v", err)
	}
	// 反过来，"查无此行"确实就是"没开门"（不是库不存在）：它该落进 governed 档。
	st.releaseErr = repository.ErrKBReleaseNotFound
	if _, _, err := svc.PublishPending(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseNotGoverned) {
		t.Errorf("没有发布行该按未进发布制回答，实得 %v", err)
	}
	st.releaseErr = nil
	st.pendErr = errors.New("待办读取失败")
	if _, _, err := svc.PublishPending(ctx, "p1", "op"); err == nil {
		t.Error("待办读失败应上抛")
	}
	st.pendErr = nil
	st.pending = []model.KBChangeRequest{kbChangeRow("kbc_ok", "p1", "apr_ok")}
	svc.approvals = &fixedGate{calls: &st.calls, row: kbTestApproval("apr_ok", "kbc_ok", model.ApprovalStatusApproved)}
	st.publishErr = repository.ErrKBReleaseRecallBan
	if _, _, err := svc.PublishPending(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseRecallBan) {
		t.Errorf("仓储的发布禁令要原样上抛（控制层据此回 409），实得 %v", err)
	}
}

// TestKBRelease_PublishPendingRequiresGoverned 未进发布制的库，发布这件事回答"没开门"。
//
// 两条腿各测一种"没开门"：从没 SetGoverned 过（没有行）与开过又关掉（有行但 false）。
// 两条都断言**没读待办**：资格问在内容之前，否则"这个库一条待办都没有"会盖住
// "这个库根本不受发布制管"这两件不同的事（前者是运营的正常状态，后者是用错了入口）。
func TestKBRelease_PublishPendingRequiresGoverned(t *testing.T) {
	for _, c := range []struct {
		name    string
		release *model.KBRelease
	}{
		{"没有发布行", nil},
		{"有行但 governed=false", &model.KBRelease{ID: 1, ProductID: "p1", Governed: false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc, st, ap := newKBService(t)
			st.release = c.release
			st.pending = []model.KBChangeRequest{kbChangeRow("kbc_ok", "p1", "apr_ok")}
			ap.getFn = func(id string) (*model.ApprovalRequest, error) {
				return kbTestApproval(id, "kbc_ok", model.ApprovalStatusApproved), nil
			}
			_, res, err := svc.PublishPending(context.Background(), "p1", "op-1")
			if !errors.Is(err, ErrKBReleaseNotGoverned) {
				t.Fatalf("期望未进发布制，实得 %v", err)
			}
			if res != nil {
				t.Errorf("被拒时不该有结果：%+v", res)
			}
			for _, call := range st.calls {
				if call == "PendingChanges" || call == "Publish" {
					t.Errorf("资格没过时不该去读待办/进事务（calls=%v）", st.calls)
					break
				}
			}
		})
	}
}

// TestKBRelease_PublishPendingDraftBucketWithoutChanges 只导入了内容、一条变更都没提，
// 也必须真的发布。
//
// 这一格是"导入也走发布制"的承重判断：变更列表为空时，服务侧原先直接回一句
// "没有可发布的变更"就走了，于是导入链路预先打好的待发布桶**永远**推不上线 ——
// 每次点发布都回 200 且 result 为 null，页面上看是"没有东西待发"，
// 实际是有一批内容卡在桶里。判据打在"事务到底调没调"上，而不是打在回显上。
func TestKBRelease_PublishPendingDraftBucketWithoutChanges(t *testing.T) {
	svc, st, ap := newKBService(t)
	st.release = kbGoverned(5) // 导入链路已把这一格打好戳
	st.pending = nil
	ap.getFn = func(string) (*model.ApprovalRequest, error) {
		t.Error("没有待办时不该回读审批")
		return nil, nil
	}
	st.publishRes = &repository.PublishResult{FromVersion: 4, ToVersion: 5}
	v, res, err := svc.PublishPending(context.Background(), "p1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.ToVersion != 5 {
		t.Fatalf("有桶就得进事务把指针推过去，实得 %+v", res)
	}
	if !kbHasStr(st.calls, "Publish") {
		t.Errorf("事务没被调用（calls=%v）", st.calls)
	}
	if len(v.Included) != 0 {
		t.Errorf("这批里没有变更，included=%v", v.Included)
	}
	if got := st.publishIntents[len(st.publishIntents)-1]; len(got.Changes) != 0 || got.Actor != "op-1" {
		t.Errorf("intent 该是空变更批 + 归人：%+v", got)
	}
}

type fixedGate struct {
	calls *[]string
	row   *model.ApprovalRequest
}

func (g *fixedGate) Submit(ctx context.Context, in ApprovalSubmitInput) (*model.ApprovalRequest, bool, error) {
	return g.row, true, nil
}

func (g *fixedGate) Get(ctx context.Context, id string) (*model.ApprovalRequest, error) {
	*g.calls = append(*g.calls, "ApprovalGet:"+id)
	return g.row, nil
}

// TestKBRelease_EmbedAppliedAfterCommit 向量补算的三个形状：
// 只带真正落了新行的那几条（retire 与新行号为 0 的不带）、库号原样传、失败咽下。
func TestKBRelease_EmbedAppliedAfterCommit(t *testing.T) {
	svc, st, _ := newKBService(t)
	st.release = kbGoverned(0)
	emb := &kbEmbedStub{}
	svc.embedder = emb
	st.pending = []model.KBChangeRequest{kbChangeRow("kbc_ok", "p1", "apr_ok")}
	svc.approvals = &fixedGate{calls: &st.calls, row: kbTestApproval("apr_ok", "kbc_ok", model.ApprovalStatusApproved)}
	st.publishRes = &repository.PublishResult{
		FromVersion: 1, ToVersion: 2,
		Applied: []repository.AppliedChange{
			{ChangeID: "kbc_ok", Op: model.KBChangeOpAdd, AppliedChunkID: 88, Content: "正文"},
			{ChangeID: "kbc_ret", Op: model.KBChangeOpRetire, AppliedChunkID: 0, Content: "旧正文"},
			{ChangeID: "kbc_nocontent", Op: model.KBChangeOpRevise, AppliedChunkID: 99, Content: ""},
		},
	}
	if _, _, err := svc.PublishPending(context.Background(), "p1", "op-1"); err != nil {
		t.Fatal(err)
	}
	if emb.calls != 1 || emb.product != "p1" || len(emb.ids) != 1 || emb.ids[0] != 88 {
		t.Errorf("补算入参错：calls=%d product=%s ids=%v", emb.calls, emb.product, emb.ids)
	}

	// 失败只出声：语料已经生效，把补强失败报成发布失败会让人重放一次已成功的发布。
	emb.calls, emb.err = 0, errors.New("embedding 服务不可用")
	st.publishRes = &repository.PublishResult{ToVersion: 3, Applied: []repository.AppliedChange{
		{ChangeID: "kbc_ok", AppliedChunkID: 101, Content: "正文"},
	}}
	if _, res, err := svc.PublishPending(context.Background(), "p1", "op-1"); err != nil || res == nil {
		t.Fatalf("补算失败不该影响发布结论: %v", err)
	}

	// 全是 retire：一条新行都没有，不该发起一次空调用。
	emb.calls, emb.err = 0, nil
	st.publishRes = &repository.PublishResult{ToVersion: 4, Applied: []repository.AppliedChange{
		{ChangeID: "kbc_ret", Op: model.KBChangeOpRetire},
	}}
	if _, _, err := svc.PublishPending(context.Background(), "p1", "op-1"); err != nil {
		t.Fatal(err)
	}
	if emb.calls != 0 {
		t.Errorf("没有新行时不该调用向量通路，实得 %d 次", emb.calls)
	}
}

// ---------------------------------------------------------------------------
// 指针移动 / 治理开关 / 视图
// ---------------------------------------------------------------------------

func TestKBRelease_PointerMovesGoToStore(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newKBService(t)
	st.rollbackRes = &repository.RollbackResult{FromVersion: 5, ToVersion: 4, Recalled: 5}
	st.restoreRes = &repository.RollbackResult{FromVersion: 4, ToVersion: 5, Recalled: 0}

	rb, err := svc.Rollback(ctx, " p1 ", " op ")
	if err != nil || rb.ToVersion != 4 || rb.Recalled != 5 {
		t.Fatalf("Rollback=%+v/%v", rb, err)
	}
	rs, err := svc.Restore(ctx, "p1", "op")
	if err != nil || rs.ToVersion != 5 || rs.Recalled != 0 {
		t.Fatalf("Restore=%+v/%v", rs, err)
	}
	if st.calls[len(st.calls)-2] != "Rollback" || st.calls[len(st.calls)-1] != "Restore" {
		t.Errorf("两个方向该各走一次仓储：%v", st.calls)
	}
	// 入参判据在仓储之前
	if _, err := svc.Rollback(ctx, strings.Repeat("p", KBReleaseProductMaxLen+1), "op"); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("超列宽库号该在入参层挡住: %v", err)
	}
	st.rollbackErr = repository.ErrKBReleaseRollbackUnavailable
	if _, err := svc.Rollback(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseRollbackUnavailable) {
		t.Errorf("没有上一版要原样上抛（控制层据此回 409）: %v", err)
	}
	st.restoreErr = repository.ErrKBReleaseRestoreUnavailable
	if _, err := svc.Restore(ctx, "p1", "op"); !errors.Is(err, ErrKBReleaseRestoreUnavailable) {
		t.Errorf("没有撤下号要原样上抛: %v", err)
	}
}

func TestKBRelease_SetGoverned(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newKBService(t)
	if _, err := svc.SetGoverned(ctx, "p1", true, " op-3 "); err != nil {
		t.Fatal(err)
	}
	if len(st.governedGot) != 1 || !st.governedGot[0] {
		t.Errorf("SetGoverned 透传错：%v", st.governedGot)
	}
	if _, err := svc.SetGoverned(ctx, "p1", false, ""); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("空操作者应判入参（谁启的治理要留得下来）: %v", err)
	}
	st.releaseErr = errors.New("kb_releases 写入失败")
	if _, err := svc.SetGoverned(ctx, "p1", true, "op"); err == nil {
		t.Error("仓储错误必须原样上抛（吞掉它就是\"界面说启用成功了、库里没这行\"）")
	}
	if n := len(st.governedGot); n != 2 {
		t.Errorf("判据过了就该走到仓储，实得 %d 次", n)
	}
}

func TestKBRelease_Views(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newKBService(t)

	st.releaseErr = repository.ErrKBReleaseNotFound
	rel, err := svc.GetRelease(ctx, "p1")
	if rel != nil || err != nil {
		t.Errorf("没有发布行该回 (nil,nil) 让界面把\"未启用\"当状态展示，实得 %v/%v", rel, err)
	}
	st.releaseErr = errors.New("连接断了")
	if _, err := svc.GetRelease(ctx, "p1"); err == nil {
		t.Error("读故障必须上抛，不能读成\"未启用\"")
	}
	st.releaseErr = nil

	st.listRows = []model.KBChangeRequest{kbChangeRow("kbc_1", "p1", "apr")}
	st.listTotal = 1
	rows, total, err := svc.ListChanges(ctx, repository.KBChangeFilter{ProductID: "  p1  ", Status: model.KBChangeStatusPending})
	if err != nil || len(rows) != 1 || total != 1 {
		t.Fatalf("ListChanges=%v/%d/%v", rows, total, err)
	}
	if st.listFilter.ProductID != "p1" {
		t.Errorf("过滤器里的库号该去空白后透传：%q", st.listFilter.ProductID)
	}
	st.listErr = errors.New("读失败")
	if _, _, err := svc.ListChanges(ctx, repository.KBChangeFilter{}); err == nil {
		t.Error("列表读失败要上抛（空列表是一条业务结论）")
	}

	if _, err := svc.ListAudit(ctx, "  ", 10); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("空 subject_key 应判入参: %v", err)
	}
	st.auditRows = []model.KBChangeAuditLog{{SubjectKey: "release:p1", Action: model.KBAuditPublished}}
	got, err := svc.ListAudit(ctx, " release:p1 ", 5)
	if err != nil || len(got) != 1 || st.auditKey != "release:p1" || st.auditLim != 5 {
		t.Errorf("ListAudit=%v/%v key=%q limit=%d", got, err, st.auditKey, st.auditLim)
	}
}

func TestKBRelease_StatsFlagsShadowReadings(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newKBService(t)
	st.inForce, st.total = 90, 120
	st.pending = []model.KBChangeRequest{kbChangeRow("kbc_1", "p1", "apr_1")}
	st.release = &model.KBRelease{ID: 1, ProductID: "p1", Governed: true, EffectiveVersion: 4}

	for _, c := range []struct {
		env    string
		shadow bool
	}{{"off", true}, {"shadow", true}, {"on", false}} {
		t.Setenv(kbrelease.FlagEnv, c.env)
		s, err := svc.Stats(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if s.GateMode != c.env || s.ShadowStats != c.shadow {
			t.Errorf("%s=%q 时读数应为 (mode=%s shadow=%t)，实得 (%s,%t)",
				kbrelease.FlagEnv, c.env, c.env, c.shadow, s.GateMode, s.ShadowStats)
		}
		if s.InForce != 90 || s.Total != 120 || s.Pending != 1 {
			t.Errorf("读数透传错：%+v", s)
		}
		if s.Release == nil || s.Release.EffectiveVersion != 4 {
			t.Errorf("发布指针没带上：%+v", s.Release)
		}
	}
	if _, err := svc.Stats(ctx, "   "); !errors.Is(err, ErrKBChangeInputInvalid) {
		t.Errorf("空库号应判入参: %v", err)
	}
	st.countErr = errors.New("统计读失败")
	if _, err := svc.Stats(ctx, "p1"); err == nil {
		t.Error("统计读失败要上抛：把故障读成\"这个库 0 条在服\"是最坏的一种假数")
	}
}

func kbHasStr(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
