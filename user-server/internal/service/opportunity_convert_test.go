// opportunity_convert_test.go T-P4-05 AC①②③：线索到达标后转成商机。
//
// 三条判据各自对应一种"看起来一样、坏法完全不同"的实现：
//   - AC① 阈值不达标不建商机 —— 判的是**闸门读的是运营配的那份数字**，
//     而不是代码里的常量。所以每条拒签用例都换阈值再跑一遍同形入参。
//   - AC② clues.is_opportunity 不破 —— 判的是本层**一个字节都不写那一列**。
//     这条只有真库跑得出来：假仓储连"有没有人写 clues 表"都表达不出来。
//   - AC③ 分配可解释 —— 规则本身见 opportunity_assign_test.go，
//     这里判的是"那份解释有没有活着到达落库的那一行与审计行"。
//
// 赢率期望值一律写字面量（0.10 / 0.05），不调用被测式子生成：同 opportunity_test.go 的口径。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 夹具 ——————————————————————————————————————————————

// convGate 直接递一份 *LTCConfig：闸门读的就是这一份，不经存储、不经缓存。
// 用它而不是真 LTCConfigService，是为了能把"配置说 0.80"与"配置根本没有"
// 这两种形状各自摆到调用点面前 —— 后者的判据是"绝不按猜出来的阈值放行"。
type convGate struct{ cfg *LTCConfig }

func (g convGate) Config(context.Context) *LTCConfig { return g.cfg }

// convGateOn 返回一份"商机阶段已开、阈值就是默认值"的策略。
// 阈值在这里写死而不是引用 DefaultLTCConfig()：默认值漂了本文件应当跟着红，
// 否则"运营以为的闸门"与"测试证明过的闸门"会在没人看见的地方分家。
func convGateOn() *LTCConfig {
	return &LTCConfig{
		Enabled:       true,
		StagesEnabled: LTCStages{}.With(LTCStageOpportunity),
		Thresholds:    LTCThresholds{LeadScore: 70, Confidence: 0.80, DiscountPercent: 15, WinProbability: 0.50},
		Source:        SourceLTC,
	}
}

type convRepo struct {
	repository.OpportunityRepository

	byClue    *model.Opportunity
	prior     *model.Opportunity
	loads     map[string]int
	getErr    error
	listErr   error
	loadErr   error
	insErr    error
	inserted  []*model.Opportunity
	getCalls  int
	listCalls int
}

func (r *convRepo) Available() bool { return r != nil }

func (r *convRepo) GetByClueID(context.Context, string) (*model.Opportunity, error) {
	r.getCalls++
	if r.getErr != nil {
		return nil, r.getErr
	}
	if r.byClue == nil {
		return nil, nil
	}
	copied := *r.byClue
	return &copied, nil
}

func (r *convRepo) ListByCustomer(context.Context, string, []string, int, int) ([]*model.Opportunity, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.prior == nil {
		return nil, nil
	}
	return []*model.Opportunity{r.prior}, nil
}

func (r *convRepo) Insert(_ context.Context, o *model.Opportunity) error {
	if r.insErr != nil {
		return r.insErr
	}
	r.inserted = append(r.inserted, o)
	return nil
}

func (r *convRepo) OpenCountByOwner(context.Context) (map[string]int, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	return r.loads, nil
}

type convAudit struct {
	rows []*model.OperationLog
	err  error
}

func (a *convAudit) Create(_ context.Context, log *model.OperationLog) error {
	if a.err != nil {
		return a.err
	}
	a.rows = append(a.rows, log)
	return nil
}

// convHarness 把"一份配置 + 一份名单 + 一份负载"装配成被测服务。
//
// 故障一律在 build() **之前**落到 harness 上：分配器持有的假名单是值拷贝，
// 装配之后再改它的话改的是另一个副本，用例会绿在一个根本没被执行到的分支上。
type convHarness struct {
	cfg       *LTCConfig
	repo      *convRepo
	audit     *convAudit
	roster    []string
	rosterErr error
}

func newConvHarness(cfg *LTCConfig, roster []string, loads map[string]int) *convHarness {
	return &convHarness{cfg: cfg, repo: &convRepo{loads: loads}, audit: &convAudit{}, roster: roster}
}

func (h *convHarness) build() *OpportunityConvertService {
	assigner := NewOwnerAssigner(fakeRoster{ids: h.roster, err: h.rosterErr}, h.repo)
	svc := NewOpportunityConvertService(h.repo, convGate{h.cfg}, assigner, h.audit)
	svc.SetClock(func() time.Time { return oppClockBase })
	return svc
}

func newConvService(t *testing.T, cfg *LTCConfig, roster []string, loads map[string]int) (*OpportunityConvertService, *convRepo, *convAudit) {
	t.Helper()
	h := newConvHarness(cfg, roster, loads)
	return h.build(), h.repo, h.audit
}

func convInput() OpportunityConversion {
	return OpportunityConversion{
		ClueID:     "clue-0f6c-0001",
		CustomerID: "cust_7",
		OneID:      "whatsapp:8613800000000",
		LeadScore:  88,
		Confidence: 0.91,
	}
}

// —— AC①：闸门 ——————————————————————————————————————————

func TestConvertRefusedWhenOpportunityStageInactive(t *testing.T) {
	cfg := convGateOn()
	cfg.StagesEnabled = LTCStages{} // 商机阶段没开
	svc, repo, _ := newConvService(t, cfg, []string{"alpha"}, map[string]int{"alpha": 0})

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("闸门关着却报错了（应当是一次合法的拒签）：%v", err)
	}
	if got.Allowed || got.Created {
		t.Errorf("阶段没开却建了商机：%+v", got)
	}
	if got.GateReason != LTCReasonStageOff {
		t.Errorf("拒签原因记成 %q，期望 %q", got.GateReason, LTCReasonStageOff)
	}
	if len(repo.inserted) != 0 {
		t.Errorf("拒签的那次仍然写了 %d 行", len(repo.inserted))
	}
}

func TestConvertRefusedWhenConfigDegraded(t *testing.T) {
	cfg := convGateOn()
	cfg.Degraded = true // 存储读不到、回落默认
	cfg.DegradeReason = "kv down"
	svc, repo, _ := newConvService(t, cfg, []string{"alpha"}, map[string]int{"alpha": 0})

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("降级应当是一次合法的拒签而不是故障：%v", err)
	}
	if got.Created || len(repo.inserted) != 0 {
		t.Errorf("配置降级时建了商机：%+v", got)
	}
	if got.GateReason != LTCReasonDegraded {
		t.Errorf("拒签原因记成 %q，期望 %q（必须能让运维看出是库的问题）", got.GateReason, LTCReasonDegraded)
	}
}

func TestConvertNilConfigRefusedNotGuessed(t *testing.T) {
	svc, repo, _ := newConvService(t, nil, []string{"alpha"}, map[string]int{"alpha": 0})
	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("nil 配置应当拒签而不是报错：%v", err)
	}
	if got.Created || len(repo.inserted) != 0 {
		t.Errorf("没有配置却按猜出来的阈值建了商机：%+v", got)
	}
	if got.GateReason != LTCReasonDegraded {
		t.Errorf("nil 配置的拒签原因记成 %q，期望 %q", got.GateReason, LTCReasonDegraded)
	}
}

func TestConvertRefusedWhenLeadScoreBelowThreshold(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	in := convInput()
	in.LeadScore = 69 // 阈值 70

	got, err := svc.ConvertFromClue(context.Background(), in)
	if err != nil {
		t.Fatalf("达标判定报错：%v", err)
	}
	if got.Created || len(repo.inserted) != 0 {
		t.Errorf("lead_score 69 却建了商机：%+v", got)
	}
	if got.GateReason != ConvertGateThresholdUnmet {
		t.Errorf("拒签原因记成 %q，期望 %q", got.GateReason, ConvertGateThresholdUnmet)
	}
	// 解释里必须能读出这两个数，否则"为什么不转"只能靠人去复算阈值。
	if !strings.Contains(got.GateDetail, "69") || !strings.Contains(got.GateDetail, "70") {
		t.Errorf("拒签说明没给出实际值与阈值：%q", got.GateDetail)
	}

	// 同一份入参、阈值抬到 90 ⇒ 88 分不该过：闸门读的是运营那份数，不是代码常量。
	raised := convGateOn()
	raised.Thresholds.LeadScore = 90
	svcHigh, repoHigh, _ := newConvService(t, raised, []string{"alpha"}, map[string]int{"alpha": 0})
	if h, err := svcHigh.ConvertFromClue(context.Background(), convInput()); err != nil {
		t.Fatalf("阈值 90 那次报错：%v", err)
	} else if h.Created || len(repoHigh.inserted) != 0 {
		t.Errorf("阈值抬到 90 后 88 分仍然建了商机：%+v", h)
	}
}

func TestConvertRefusedWhenConfidenceBelowThreshold(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	in := convInput()
	in.Confidence = 0.79 // 阈值 0.80，分数已经够

	got, err := svc.ConvertFromClue(context.Background(), in)
	if err != nil {
		t.Fatalf("达标判定报错：%v", err)
	}
	if got.Created || len(repo.inserted) != 0 {
		t.Errorf("confidence 0.79 却建了商机：%+v", got)
	}
	if !strings.Contains(got.GateDetail, "0.79") {
		t.Errorf("拒签说明没给出实际置信度：%q", got.GateDetail)
	}
}

// TestConvertRejectsConfidenceOutsideZeroToOne C5 的那道陷阱，判据落在输入边界上。
//
// 本仓里叫 confidence 的数有两个量程：clue_scores 侧是 0–100（维度覆盖度），
// 判定侧是 0–1（模型对自己结论的把握）。把 80 递进来若照单全收，
// 80 ≥ 0.80 恒成立 ⇒ 这道闸门当场变成没有闸门，而且它永远是绿的。
// 越界必须是**入参错误**（ErrOpportunityInputInvalid）而不是"不达标"：
// 后者的修法是"再等等，分数会涨"，前者的修法是"改调用方"。
func TestConvertRejectsConfidenceOutsideZeroToOne(t *testing.T) {
	for _, v := range []float64{80, 1.5, -0.01, math.NaN(), math.Inf(1)} {
		svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
		in := convInput()
		in.Confidence = v
		got, err := svc.ConvertFromClue(context.Background(), in)
		if !errors.Is(err, ErrOpportunityInputInvalid) {
			t.Errorf("confidence=%v 得到了 %v，期望入参错误", v, err)
		}
		if got.Created || len(repo.inserted) != 0 {
			t.Errorf("confidence=%v 越界仍建了商机：%+v", v, got)
		}
	}
}

// TestConvertRejectsLeadScoreOutsideZeroToHundred 另一个方向的同款陷阱：
// 把 0–1 的把握当成 0–100 的线索分递进来（0.85 会被整数化读成 0 分），
// 于是每条线索都"不达标"—— 这个形状比放行更隐蔽，因为它每天都绿。
func TestConvertRejectsLeadScoreOutsideZeroToHundred(t *testing.T) {
	for _, v := range []int{-1, 101, 1000} {
		svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
		in := convInput()
		in.LeadScore = v
		if _, err := svc.ConvertFromClue(context.Background(), in); !errors.Is(err, ErrOpportunityInputInvalid) {
			t.Errorf("lead_score=%d 得到了 %v，期望入参错误", v, err)
		}
		if len(repo.inserted) != 0 {
			t.Errorf("lead_score=%d 越界仍写了行", v)
		}
	}
}

func TestConvertRejectsBlankClueID(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	in := convInput()
	in.ClueID = "   "
	if _, err := svc.ConvertFromClue(context.Background(), in); !errors.Is(err, ErrOpportunityInputInvalid) {
		t.Errorf("空白线索号得到了 %v", err)
	}
	if len(repo.inserted) != 0 {
		t.Error("连幂等键都没有，却写了行")
	}
}

func TestConvertUnavailableConverter(t *testing.T) {
	if _, err := (&OpportunityConvertService{}).ConvertFromClue(context.Background(), convInput()); err == nil {
		t.Error("未装配的转换器给出了结论")
	}
	if (&OpportunityConvertService{}).Available() {
		t.Error("未装配的转换器自称 Available")
	}
	var nilSvc *OpportunityConvertService
	if nilSvc.Available() {
		t.Error("nil 转换器自称 Available")
	}
	if _, err := nilSvc.ConvertFromClue(context.Background(), convInput()); err == nil {
		t.Error("nil 转换器回了 nil 错误")
	}
}

// —— 幂等：AC① 的另一半是"重复投递不会变成两行" ——————————————

func TestConvertIsIdempotentPerClue(t *testing.T) {
	existing := &model.Opportunity{ID: "opp_existing", Code: "OPP-OLD-1", ClueID: "clue-0f6c-0001",
		Stage: model.OpportunityStageNeedsConfirmed, Status: model.OpportunityStatusOpen, OwnerUserID: "beta"}
	repo := &convRepo{byClue: existing, loads: map[string]int{"alpha": 0}}
	svc := NewOpportunityConvertService(repo, convGate{convGateOn()},
		NewOwnerAssigner(fakeRoster{ids: []string{"alpha"}}, repo), &convAudit{})
	svc.SetClock(func() time.Time { return oppClockBase })

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("幂等重放报错：%v", err)
	}
	if got.Created {
		t.Error("这条线索早就有商机了，又建了一行")
	}
	if !got.Allowed {
		t.Error("已转化被记成了闸门拒签 —— 两者在响应里必须分得开")
	}
	if got.GateReason != ConvertGateAlreadyConverted {
		t.Errorf("原因记成 %q，期望 %q", got.GateReason, ConvertGateAlreadyConverted)
	}
	if got.Opportunity == nil || got.Opportunity.ID != "opp_existing" {
		t.Errorf("没把既有那一行交回去：%+v", got.Opportunity)
	}
	if len(repo.inserted) != 0 {
		t.Errorf("重放写了新行：%+v", repo.inserted)
	}
	// 幂等那一支不该再分配：换人是比"多一行"更贵的副作用。
	if got.Assignment != nil {
		t.Errorf("重放那次又跑了一遍分配：%+v", got.Assignment)
	}
	if repo.listCalls != 0 {
		t.Errorf("重放那次还去查了客户历史：%d 次", repo.listCalls)
	}
}

// TestConvertIdempotencyReadFailureIsNotMiss 反查失败 ≠ "这条线索还没转化过"。
func TestConvertIdempotencyReadFailureIsNotMiss(t *testing.T) {
	want := errors.New("read failed")
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	repo.getErr = want
	if _, err := svc.ConvertFromClue(context.Background(), convInput()); !errors.Is(err, want) {
		t.Errorf("反查失败得到了 %v，期望原样上抛", err)
	}
	if len(repo.inserted) != 0 {
		t.Error("连有没有转化过都不知道，却往下建了行")
	}
}

// —— AC③ + 落库形状 ————————————————————————————————————

func TestConvertCreatesRowAndCarriesAssignment(t *testing.T) {
	svc, repo, audit := newConvService(t, convGateOn(), []string{"alpha", "beta"},
		map[string]int{"alpha": 3, "beta": 1})

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("达标的那次报错了：%v", err)
	}
	if !got.Created || !got.Allowed {
		t.Fatalf("没建成：%+v", got)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("写了 %d 行", len(repo.inserted))
	}
	row := repo.inserted[0]

	if row.Status != model.OpportunityStatusOpen {
		t.Errorf("新行状态 %q，期望 open", row.Status)
	}
	if row.Stage != model.OpportunityStageQualification {
		t.Errorf("新行阶段 %q，期望 qualification（转来的商机从第一格开始）", row.Stage)
	}
	if row.Version != 0 {
		t.Errorf("新行 version=%d，期望 0（仓储侧的硬前置）", row.Version)
	}
	if row.ClueID != "clue-0f6c-0001" || row.CustomerID != "cust_7" || row.OneID == "" {
		t.Errorf("身份三列递错了：%+v", row)
	}
	if row.Currency != model.OpportunityCurrencyDefault {
		t.Errorf("币种空着会落库成空串：%q", row.Currency)
	}
	if row.OwnerUserID != "beta" {
		t.Errorf("归属 %q，期望负载最少的 beta", row.OwnerUserID)
	}
	// 0.10 = qualification 基础值；已归属 ⇒ 不扣无归属那 0.10。
	if row.WinProbability != 0.10 {
		t.Errorf("赢率 %v，期望字面量 0.10", row.WinProbability)
	}
	if got.Assignment == nil || got.Assignment.Rule != AssignRuleLeastLoaded {
		t.Errorf("结论里没带分配解释：%+v", got.Assignment)
	}
	if got.Opportunity == nil || got.Opportunity.ID != row.ID {
		t.Errorf("结果里没带回那一行：%+v", got.Opportunity)
	}
	if !got.AuditWritten || got.AuditError != "" {
		t.Errorf("审计写成功了却没报出来：%+v", got)
	}

	if len(audit.rows) != 1 {
		t.Fatalf("审计写了 %d 行", len(audit.rows))
	}
	log := audit.rows[0]
	if log.Module != OpportunityConvertAuditModule {
		t.Errorf("审计模块 %q", log.Module)
	}
	if log.ResourceID != "clue-0f6c-0001" {
		t.Errorf("审计资源键 %q，期望线索号（它是幂等键，也是唯一的反查入口）", log.ResourceID)
	}
	for _, want := range []string{"beta", string(AssignRuleLeastLoaded), "0.91", "88", row.Code, row.ID} {
		if !strings.Contains(log.Detail, want) {
			t.Errorf("审计明细缺 %q：%q", want, log.Detail)
		}
	}
	if log.UserID != 0 || !strings.HasPrefix(log.Username, "system:") {
		t.Errorf("无人值守的动作被记成了某个人的操作：%+v", log)
	}
}

// TestConvertUsesPriorOwnerOfSameCustomer 规则一活着穿过转换器：同客户已有商机 ⇒ 沿用。
func TestConvertUsesPriorOwnerOfSameCustomer(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha", "beta"},
		map[string]int{"alpha": 9, "beta": 0})
	repo.prior = &model.Opportunity{OwnerUserID: "alpha"}

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("分配报错：%v", err)
	}
	row := repo.inserted[0]
	if row.OwnerUserID != "alpha" {
		t.Errorf("同客户的老销售没被沿用：%+v", row)
	}
	if got.Assignment == nil || got.Assignment.Rule != AssignRuleCustomerOwner {
		t.Errorf("规则记成了 %+v", got.Assignment)
	}
}

// TestConvertWithoutCustomerSkipsPriorOwnerRead 没有 customer_id 的线索不该去查归属。
//
// 走"拿空串查一次"那条路，命中什么完全取决于别的行 —— 一个看着合理、
// 实际把商机交给了陌生销售的结论。
func TestConvertWithoutCustomerSkipsPriorOwnerRead(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), []string{"alpha"}, map[string]int{"alpha": 2})
	repo.prior = &model.Opportunity{OwnerUserID: "someone_else"}
	in := convInput()
	in.CustomerID = ""

	if _, err := svc.ConvertFromClue(context.Background(), in); err != nil {
		t.Fatalf("报错：%v", err)
	}
	if got := repo.inserted[0]; got.OwnerUserID != "alpha" {
		t.Errorf("没有客户身份却沿用了别人的销售：%+v", got)
	}
	if repo.listCalls != 0 {
		t.Errorf("customer_id 为空仍然查了客户历史：%d 次", repo.listCalls)
	}
}

// TestConvertUnassignedRowStillCreated 没人可分不是不建商机的理由（规则三，见分配文件头）。
func TestConvertUnassignedRowStillCreated(t *testing.T) {
	svc, repo, _ := newConvService(t, convGateOn(), nil, map[string]int{})

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("空名单被当成故障了：%v", err)
	}
	row := repo.inserted[0]
	if row.OwnerUserID != "" {
		t.Errorf("没人却分出了归属：%+v", row)
	}
	// 0.10 基础值 - 0.10 无归属惩罚 = 0 ⇒ 落到地板 0.05。断字面量而不是 "<=0.10"：
	// 地板存在的理由正是"0 是最响的那个错误答案"。
	if row.WinProbability != 0.05 {
		t.Errorf("无归属赢率 %v，期望字面量 0.05（地板）", row.WinProbability)
	}
	if got.Assignment == nil || got.Assignment.Rule != AssignRuleNoRoster {
		t.Errorf("没把「没人可分」这件事递出来：%+v", got.Assignment)
	}
}

// TestConvertFailurePathsWriteNothing 名单/负载/历史读失败与写失败：一行都不该留下。
func TestConvertFailurePathsWriteNothing(t *testing.T) {
	want := errors.New("boom")
	cases := []struct {
		name  string
		tweak func(*convHarness)
	}{
		{"名单读失败", func(h *convHarness) { h.rosterErr = want }},
		{"负载读失败", func(h *convHarness) { h.repo.loadErr = want }},
		{"客户历史读失败", func(h *convHarness) {
			h.repo.prior = &model.Opportunity{OwnerUserID: "alpha"}
			h.repo.listErr = want
		}},
		{"落库失败", func(h *convHarness) { h.repo.insErr = want }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newConvHarness(convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
			tc.tweak(h)
			svc := h.build()

			got, err := svc.ConvertFromClue(context.Background(), convInput())
			if !errors.Is(err, want) {
				t.Errorf("得到了 %v，期望原样上抛 %v", err, want)
			}
			if got.Created || len(h.repo.inserted) != 0 {
				t.Errorf("故障那次留下了行：%+v / %d 行", got, len(h.repo.inserted))
			}
			if got.Opportunity != nil {
				t.Errorf("故障那次还递回了一行商机：%+v", got.Opportunity)
			}
		})
	}
}

// TestConvertAuditFailureStillReportsConversionSuccess 与 ltc.config 同一口径：
// 商机已经落库是既成事实，审计写失败要**分开报**，不能伪装成整笔失败（重放会变两行）。
func TestConvertAuditFailureStillReportsConversionSuccess(t *testing.T) {
	h := newConvHarness(convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	want := errors.New("audit table missing")
	h.audit = &convAudit{err: want}
	svc := h.build()

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("审计失败不该把整笔转化报成失败：%v", err)
	}
	if !got.Created || len(h.repo.inserted) != 1 {
		t.Errorf("商机没落库：%+v", got)
	}
	if got.AuditWritten {
		t.Error("审计没落库却报 AuditWritten=true")
	}
	if !strings.Contains(got.AuditError, want.Error()) {
		t.Errorf("AuditError 里查不到原始故障：%q", got.AuditError)
	}
}

func TestConvertNilAuditReportsGap(t *testing.T) {
	h := newConvHarness(convGateOn(), []string{"alpha"}, map[string]int{"alpha": 0})
	svc := h.build()
	svc.audit = nil

	got, err := svc.ConvertFromClue(context.Background(), convInput())
	if err != nil {
		t.Fatalf("报错：%v", err)
	}
	if got.AuditWritten || got.AuditError == "" {
		t.Errorf("审计仓储没装配却被报成写成功了：%+v", got)
	}
	if !got.Created {
		t.Error("审计缺失不该影响商机本身是否落库")
	}
}

// —— 主键与编号构造器 ————————————————————————————————————

func TestOpportunityKeyShapes(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, seq := range []int64{1, 26, 37, 1295, math.MaxInt64} {
		id, code := newOpportunityKeys(now, seq)
		if !strings.HasPrefix(id, "opp_") {
			t.Errorf("ID %q 没有 opp_ 前缀", id)
		}
		if len(id) > 64 {
			t.Errorf("ID 长度 %d 超过下游 sales_events.opportunity_id 的 varchar(64)：%q", len(id), id)
		}
		if !strings.HasPrefix(code, "OPP-") {
			t.Errorf("编号 %q 没有 OPP- 前缀（人要在电话里念它）", code)
		}
		if len(code) > 32 {
			t.Errorf("编号长度 %d 超过本列 varchar(32)：%q", len(code), code)
		}
		// 编号里不许出现日期串：宿主机时区与 PG 会话时区不一致时，
		// 同一时刻会生成两个不同的"当天序号"，那正是日期边界裂脑的形状。
		for _, sep := range []string{"/", ":", " "} {
			if strings.Contains(code, sep) {
				t.Errorf("编号含日期/时间分隔符：%q", code)
			}
		}
		if strings.ContainsAny(id, " \t") {
			t.Errorf("键含空白：%q", id)
		}
	}
	// 同一时刻、同一 seq ⇒ 同一对键（构造器必须是纯函数，重放才解释得通）。
	a, ac := newOpportunityKeys(now, 7)
	b, bc := newOpportunityKeys(now, 7)
	if a != b || ac != bc {
		t.Errorf("构造器不纯：%q/%q vs %q/%q", a, ac, b, bc)
	}
}

// TestOpportunityKeysUniqueUnderConcurrentConvert 并发转换不许撞同一把键。
//
// 撞了的形状不是报错，是两行商机共用一个 code ⇒ Insert 当场撞唯一索引，
// 而那两条线索其实各自有效。这里跑并发只验计数器，不碰库。
func TestOpportunityKeysUniqueUnderConcurrentConvert(t *testing.T) {
	const n = 512
	var wg sync.WaitGroup
	ids := make([]string, n)
	codes := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], codes[i] = newOpportunityKeysFromClock(time.Now())
		}(i)
	}
	wg.Wait()
	seenID := make(map[string]bool, n)
	seenCode := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		if seenID[ids[i]] {
			t.Fatalf("第 %d 次生成撞了 ID %q", i, ids[i])
		}
		seenID[ids[i]] = true
		if seenCode[codes[i]] {
			t.Fatalf("第 %d 次生成撞了编号 %q（撞了就是 Insert 撞唯一索引）", i, codes[i])
		}
		seenCode[codes[i]] = true
	}
}

// —— 真库那条腿：AC② 只有在这里才证得出来 ——————————————

type fixedRoster struct{ ids []string }

func (r fixedRoster) ActiveSalesIDs(context.Context) ([]string, error) { return r.ids, nil }

func newConvertServiceWithDB(t *testing.T, roster []string) (*OpportunityConvertService, repository.OpportunityRepository, repository.ClueRepository, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.Opportunity{}, &model.Clue{}, &model.OperationLog{})
	repo := repository.NewOpportunityRepositoryWithDB(database)
	clueRepo := repository.NewClueRepositoryWithDB(database)
	audit := repository.NewOperationLogRepositoryWithDB(database)
	svc := NewOpportunityConvertService(repo, convGate{convGateOn()},
		NewOwnerAssigner(fixedRoster{ids: roster}, repo), audit)
	svc.SetClock(func() time.Time { return oppClockBase })
	return svc, repo, clueRepo, database
}

// TestConvertNeverWritesClueIsOpportunity AC② 的全部含义：转不转都别碰那一列。
//
// 那一列今天的语义是"挖掘侧按 intent_score 顺手打上的热度标记"（lead_mining.go 与
// lead_miner_unified.go 两处都在写它），不是"这条线索已经变成商机"。转换器一旦也写它，
// 同一列就同时承载两个判据、且取值时刻不同 —— 旧读取方（列表筛选
// COALESCE(is_opportunity,0)>=1）看到的人群会凭空换一批。
// 反查一律走 opportunities.clue_id。
func TestConvertNeverWritesClueIsOpportunity(t *testing.T) {
	ctx := context.Background()
	svc, _, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha"})

	for _, seeded := range []int64{0, 1} {
		name := fmt.Sprintf("seed-%d", seeded)
		t.Run(name, func(t *testing.T) {
			clue := &model.Clue{Type: ClueTypeLeadMining, Account: "acct-" + name,
				Name: "线索", IntentScore: 30, IsOpportunity: seeded, OneID: "one-" + name}
			if err := clueRepo.Create(ctx, clue); err != nil {
				t.Fatalf("造线索失败：%v", err)
			}
			in := convInput()
			in.ClueID = clue.ID
			res, err := svc.ConvertFromClue(ctx, in)
			if err != nil {
				t.Fatalf("转化失败：%v", err)
			}
			if !res.Created {
				t.Fatalf("这一臂没建成：%+v", res)
			}

			var after model.Clue
			if err := database.Where("id = ?", clue.ID).First(&after).Error; err != nil {
				t.Fatalf("读回线索失败：%v", err)
			}
			if after.IsOpportunity != seeded {
				t.Errorf("转换器改了 clues.is_opportunity：%d → %d（AC② 要求一个字节都不写）",
					seeded, after.IsOpportunity)
			}
			if after.IntentScore != 30 {
				t.Errorf("顺手把意向分也改了：%d", after.IntentScore)
			}
			if after.Level != "" {
				t.Errorf("顺手把温度等级也改了：%q", after.Level)
			}
		})
	}
}

// TestConvertPersistsAndIsIdempotentOnRealRows 真库那条路径：落的就是读回的那一行，
// 且同一线索第二次转换不再产生新行。
func TestConvertPersistsAndIsIdempotentOnRealRows(t *testing.T) {
	ctx := context.Background()
	svc, repo, clueRepo, database := newConvertServiceWithDB(t, []string{"alpha", "beta"})

	clue := &model.Clue{Type: ClueTypeLeadMining, Account: "acct-conv", Name: "线索", IsOpportunity: 1}
	if err := clueRepo.Create(ctx, clue); err != nil {
		t.Fatalf("造线索失败：%v", err)
	}
	in := convInput()
	in.ClueID = clue.ID
	in.CustomerID = "cust_conv"

	first, err := svc.ConvertFromClue(ctx, in)
	if err != nil {
		t.Fatalf("首次转化失败：%v", err)
	}
	if !first.Created || first.Opportunity == nil || first.Opportunity.ID == "" {
		t.Fatalf("首次转化结果不对：%+v", first)
	}
	if !first.AuditWritten {
		t.Errorf("审计已落库却报 AuditWritten=false：%+v", first)
	}

	var count int64
	if err := database.Model(&model.Opportunity{}).Where("clue_id = ?", clue.ID).Count(&count).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if count != 1 {
		t.Fatalf("库里 %d 行", count)
	}

	// 读回来判，而不是信返回值。
	back, err := repo.GetByClueID(ctx, clue.ID)
	if err != nil || back == nil {
		t.Fatalf("按线索反查失败：%v / %+v", err, back)
	}
	if back.Status != model.OpportunityStatusOpen || back.Stage != model.OpportunityStageQualification {
		t.Errorf("落库的初态不对：%+v", back)
	}
	if back.Version != 0 {
		t.Errorf("落库的 version=%d", back.Version)
	}
	if back.Code == "" || len(back.Code) > 32 {
		t.Errorf("落库的编号不对：%q", back.Code)
	}

	second, err := svc.ConvertFromClue(ctx, in)
	if err != nil {
		t.Fatalf("重放报错：%v", err)
	}
	if second.Created || second.GateReason != ConvertGateAlreadyConverted {
		t.Errorf("重放结果不对：%+v", second)
	}
	if second.Opportunity == nil || second.Opportunity.ID != back.ID {
		t.Errorf("重放没把既有那一行交回去：%+v", second.Opportunity)
	}
	if err := database.Model(&model.Opportunity{}).Where("clue_id = ?", clue.ID).Count(&count).Error; err != nil {
		t.Fatalf("再计数失败：%v", err)
	}
	if count != 1 {
		t.Errorf("重放后库里 %d 行，期望仍是 1", count)
	}

	// 审计那一行也必须真的在库里。
	var logs []model.OperationLog
	if err := database.Where("module = ? AND resource_id = ?", OpportunityConvertAuditModule, clue.ID).
		Find(&logs).Error; err != nil {
		t.Fatalf("读审计失败：%v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("审计 %d 行：%+v", len(logs), logs)
	}
}

// TestConvertLoadCountsOnlyOpenOpportunities 负载均衡的输入必须是"还在跑的"。
//
// 把已赢单/已丢单的也算进负载，一个人赢单越多就越不会再拿到新单 ——
// 那不是均衡，那是惩罚业绩。这一条在真库上跑，因为它判的就是那条 GROUP BY。
// 顺带把 clue_id 的部分唯一索引按住：这里的种子行 clue_id 全是空串，
// 空值若被索引当成"同一个值"，第二行就插不进去。
func TestConvertLoadCountsOnlyOpenOpportunities(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, database := newConvertServiceWithDB(t, []string{"alpha", "beta"})
	svc.assigner = NewOwnerAssigner(fixedRoster{ids: []string{"alpha", "beta"}}, repo)

	seed := func(owner, status string, n int) {
		for i := 0; i < n; i++ {
			row := &model.Opportunity{
				ID:     fmt.Sprintf("opp_seed_%s_%s_%d", owner, status, i),
				Code:   fmt.Sprintf("OPP-SEED-%s-%s-%d", owner, status, i),
				Stage:  model.OpportunityStageQualification,
				Status: status, OwnerUserID: owner,
			}
			if err := database.Create(row).Error; err != nil {
				t.Fatalf("造种子行失败：%v", err)
			}
		}
	}
	seed("alpha", model.OpportunityStatusOpen, 1)
	seed("alpha", model.OpportunityStatusWon, 5)
	seed("alpha", model.OpportunityStatusLost, 5)
	seed("beta", model.OpportunityStatusOpen, 2)
	seed("", model.OpportunityStatusOpen, 3) // 无归属：谁的负载都不该加

	in := convInput()
	in.ClueID = "clue_load"
	in.CustomerID = ""
	got, err := svc.ConvertFromClue(ctx, in)
	if err != nil {
		t.Fatalf("转化失败：%v", err)
	}
	if got.Assignment == nil {
		t.Fatalf("没带分配解释：%+v", got)
	}
	if got.Opportunity.OwnerUserID != "alpha" {
		t.Errorf("选了 %q，期望 alpha（1 个在办）而不是 beta（2 个在办）：%+v",
			got.Opportunity.OwnerUserID, got)
	}
	for _, c := range got.Assignment.Candidates {
		switch c.SalesID {
		case "alpha":
			if c.OpenCount != 1 {
				t.Errorf("alpha 的在办数 %d，期望 1（赢单/丢单不该算）", c.OpenCount)
			}
		case "beta":
			if c.OpenCount != 2 {
				t.Errorf("beta 的在办数 %d，期望 2", c.OpenCount)
			}
		}
	}
}
