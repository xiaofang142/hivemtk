// collection_job_test.go T-P7-03：催收任务（逾期识别 → 提醒外发 → 升级待办）。
//
// 这一族的判据全部落在"没有一条能静默变成没催"上，所以本文件的替身都是**记账型**的：
// 每个假件把"被叫了几次、拿的是什么参数"留在切片里，断言打在那份账上。
// 三条贯穿全文的取向：
//   - **不发也不写**：shadow 档一条消息不出、一条待办不落、一个锁键不占
//     （占了锁就等于这一轮把下一轮的额度烧掉，观察档不能有这种后续效果）；
//   - **取锁失败拒绝发送**（fail-closed）：与触达服务自己的冷却相反（那边 err 就放行，
//     定位是"少打扰"；这边定位是"同一张单别催两遍"），这条方向差异必须由用例钉住，
//     否则下一个人会"顺手统一成 fail-open"；
//   - **催收这条腿没有改账单的能力**：接口面上就只有 `ScanOverdue` 一格读，
//     写状态那格（`UpdateStatus`）压根不在依赖里 —— 反射用例钉死（见端口用例）。
//
// 时间一律显式给出（`SetClock`），不用 `time.Sleep`：这一族的判据是"逾期多少天"，
// 睡一觉既测不出东西又自带日历引信。
package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // 下面那条 DST 判据要 America/New_York；内嵌时区表，不赌宿主机有没有 /usr/share/zoneinfo

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ---------------------------------------------------------------- 测试替身

type fakeCollectionScanner struct {
	res *repository.BillOverdueScan
	err error
	// nilRes 造出 (nil, nil) 这一格：既不是"今天没人逾期"也不是"读失败了"。
	// 假件默认永远回一个非 nil 的读数（哪怕空），所以这一格必须单独开一格开关，
	// 否则"扫描回了 nil 读数"那条守卫在测试里根本没有对应的输入。
	nilRes  bool
	cutoffs []time.Time
	limits  []int
	// entered/release 成对给出时，扫描会停在"刚被叫到"的那一刻：用来把一轮跑到一半
	// 这个瞬间停住，好观测那一格里读到的读数是哪一份。只有一条用例配这两个字段。
	entered chan struct{}
	release chan struct{}
}

func (f *fakeCollectionScanner) ScanOverdue(_ context.Context, cutoff time.Time, limit int) (*repository.BillOverdueScan, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default: // 只拦第一次；回填后不再挂住（本用例只跑两轮，写这里是为了不赌）
		}
		<-f.release
	}
	if f.nilRes {
		return nil, nil
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	return &repository.BillOverdueScan{}, nil
}

type fakeCollectionOpps struct {
	rows  map[string]*model.Opportunity
	err   error
	calls []string
}

func (f *fakeCollectionOpps) GetByID(_ context.Context, id string) (*model.Opportunity, error) {
	f.calls = append(f.calls, id)
	if f.err != nil {
		return nil, f.err
	}
	row, ok := f.rows[id]
	if !ok {
		return nil, nil
	}
	return row, nil
}

type fakeCollectionReach struct {
	calls []*ProactiveReachRequest
	// errs 按调用次序取用；用完回落到最后一格（一格都配就是 nil）。
	errs []error
}

func (f *fakeCollectionReach) ReachByCustomer(_ context.Context, req *ProactiveReachRequest) (*ProactiveReachResponse, error) {
	f.calls = append(f.calls, req)
	idx := len(f.calls) - 1
	if idx < len(f.errs) {
		if err := f.errs[idx]; err != nil {
			return nil, err
		}
	} else if len(f.errs) > 0 {
		if err := f.errs[len(f.errs)-1]; err != nil {
			return nil, err
		}
	}
	return &ProactiveReachResponse{MessageID: "msg-c-1", Channel: "sms", Status: "sent"}, nil
}

type fakeCollectionTasks struct {
	created bool
	err     error
	inputs  []HumanTaskSubmitInput
}

func (f *fakeCollectionTasks) Submit(_ context.Context, in HumanTaskSubmitInput) (*model.HumanTask, bool, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, false, f.err
	}
	return &model.HumanTask{ID: "ht-1", Kind: in.Kind, SubjectType: in.SubjectType, SubjectID: in.SubjectID}, f.created, nil
}

type fakeCollectionStage struct {
	on     bool
	reason string
	calls  []LTCStage
}

func (f *fakeCollectionStage) StageActive(_ context.Context, stage LTCStage) (bool, string) {
	f.calls = append(f.calls, stage)
	if f.on {
		return true, LTCReasonActive
	}
	if f.reason == "" {
		return false, LTCReasonStageOff
	}
	return false, f.reason
}

// fakeCollectionClaim 把 SetNX / ReleaseLock 两格搬到内存里。
//
// 两格都要：只测"占没占到"测不出"发失败时把窗还回去"那一格，而后者是本卡的一条判据。
type fakeCollectionClaim struct {
	held     map[string]string
	setErr   error
	relErr   error
	sets     []string
	releases []string
	ttls     map[string]time.Duration
}

func newFakeCollectionClaim() *fakeCollectionClaim {
	return &fakeCollectionClaim{held: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (f *fakeCollectionClaim) setNX(_ context.Context, key, token string, ttl time.Duration) (bool, error) {
	if f.setErr != nil {
		return false, f.setErr
	}
	f.sets = append(f.sets, key)
	if _, ok := f.held[key]; ok {
		return false, nil
	}
	f.held[key] = token
	f.ttls[key] = ttl
	return true, nil
}

func (f *fakeCollectionClaim) release(_ context.Context, key, token string) (bool, error) {
	f.releases = append(f.releases, key)
	if f.relErr != nil {
		return false, f.relErr
	}
	if got, ok := f.held[key]; ok && got == token {
		delete(f.held, key)
		return true, nil
	}
	return false, nil
}

// ---------------------------------------------------------------- 夹具

func collFixtureNow() time.Time {
	return time.Date(2026, 11, 20, 6, 0, 0, 0, time.UTC)
}

// collBill 造一张逾期应收。dueDaysBeforeNow 为负 = 还没到期（本卡的夹具不该造出那种行，
// 除非某条用例专门要验"没到期不进集合"，而那格归仓储层的用例）。
func collBill(id, oppID string, amount float64, dueDaysBeforeNow float64) *model.Bill {
	due := collFixtureNow().Add(-time.Duration(dueDaysBeforeNow * float64(24*time.Hour)))
	return &model.Bill{
		ID: id, QuoteID: "QT-" + id, QuoteRowID: "qr-" + id, OpportunityID: oppID,
		Amount: amount, Currency: model.BillCurrencyDefault,
		DueAt: &due, Status: model.BillStatusOpen, CreatedAt: due.Add(-24 * time.Hour),
	}
}

func collOpp(id, customerID, oneID string) *model.Opportunity {
	return &model.Opportunity{ID: id, CustomerID: customerID, OneID: oneID,
		Status: model.OpportunityStatusWon, Stage: model.OpportunityStageNegotiation}
}

// newTestCollectionJob 直接搭出任务（不经 env、不起协程），四依赖 + 锁 + 时钟全部显式给。
// mode 用现成的三态解析器那一族（与挽回 worker 同口径，见 collection_job.go 文件头）。
func newTestCollectionJob(mode RecoveryWorkerMode) (
	*CollectionJob, *fakeCollectionScanner, *fakeCollectionOpps, *fakeCollectionReach, *fakeCollectionTasks, *fakeCollectionClaim,
) {
	scanner := &fakeCollectionScanner{}
	opps := &fakeCollectionOpps{rows: map[string]*model.Opportunity{}}
	reach := &fakeCollectionReach{}
	tasks := &fakeCollectionTasks{created: true}
	claim := newFakeCollectionClaim()
	stages := &fakeCollectionStage{on: true}
	job := NewCollectionJob(scanner, opps, reach, tasks, stages)
	job.mode = mode
	job.batch = collectionJobDefaultBatch
	job.interval = 6 * time.Hour
	job.remindWindow = CollectionReminderWindow
	job.escalateWindow = CollectionEscalateWindow
	job.nowFunc = collFixtureNow
	job.setNX = claim.setNX
	job.release = claim.release
	return job, scanner, opps, reach, tasks, claim
}

func collScan(bills ...*model.Bill) *repository.BillOverdueScan {
	return &repository.BillOverdueScan{Overdue: bills}
}

// ---------------------------------------------------------------- 口径（AC①）

// TestCollectionCutoffAppliesTheDocumentedGrace 逾期口径的算式只有一处，两条腿：
// cutoff = now − 宽限期，且**不做日历日取整**。
//
// 第二腿是本仓踩过的那条（PG 会话钉 CST、Go 按宿主机时区格式化日期）：
// 同一个"瞬间"用两个不同时区的 time.Time 表示，判据必须给出同一个 cutoff。
// 一旦有人写成 now.AddDate(0,0,-3)（会按日历走），这一条就在跨时区上分开。
func TestCollectionCutoffAppliesTheDocumentedGrace(t *testing.T) {
	nowUTC := collFixtureNow()
	sameInstantShanghai := nowUTC.In(time.FixedZone("CST", 8*3600))

	want := nowUTC.Add(-CollectionGraceDays * 24 * time.Hour)
	if got := collectionCutoff(nowUTC); !got.Equal(want) {
		t.Errorf("cutoff = %v，期望 %v（宽限期 %d 天 = %s 的滚动窗口）",
			got.UTC(), want.UTC(), CollectionGraceDays, CollectionGraceDays*24*time.Hour)
	}
	if got := collectionCutoff(sameInstantShanghai); !got.Equal(want) {
		t.Errorf("同一瞬间换时区表示，cutoff 变成 %v：口径改成了按日历取整，"+
			"逾期天数会随宿主机时区摆动", got.UTC())
	}
	if CollectionGraceDays < 0 {
		t.Errorf("宽限期是负数（%d）：那是提前催", CollectionGraceDays)
	}
}

// TestCollectionCutoffIsAnAbsoluteWindowNotCalendarDays 上面那条"换时区表示"不够狠：
// 本仓夹具用的 `time.FixedZone("CST", 8*3600)` **永不夏令时**，所以
// `now.Add(-3*24h)` 与 `now.AddDate(0,0,-3)` 在它身上给出同一个瞬间 —— 那条判据是死锁。
//
// 这里换一个真会回拨的时区，且把期望值**写死成 UTC 时刻**（不用 `now.Add(...)` 复算，
// 否则就又变成同义反复）。America/New_York 在 2026-11-01 02:00 回拨：
// 从 11-02 00:30 EST 往回三个日历日只有 **71 个真实小时**。
//   - 按绝对时刻（本卡的口径）：cutoff = 10-30 05:30 UTC；
//   - 按日历取整（`AddDate`）：cutoff = 10-30 04:30 UTC，早了一小时。
//
// 差这一小时的方向是"少催"：账期落在 04:30–05:30 之间的那些单这一轮扫不到，
// 而日志里写的是"本轮扫到 N 张逾期"，N 看起来完全正常。
func TestCollectionCutoffIsAnAbsoluteWindowNotCalendarDays(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("载不入 America/New_York（tzdata 已内嵌，这一步失败说明夹具坏了）：%v", err)
	}
	straddling := time.Date(2026, 11, 2, 0, 30, 0, 0, ny) // EST，= 11-02 05:30 UTC
	if _, off := straddling.Zone(); off != -5*3600 {
		t.Fatalf("夹具不成立：该瞬间的偏移是 %d 秒，期望 EST(-5h)（本条判据靠这一次回拨成立）", off)
	}
	want := time.Date(2026, 10, 30, 5, 30, 0, 0, time.UTC)
	if got := collectionCutoff(straddling); !got.Equal(want) {
		t.Errorf("cutoff = %v，期望 %v：按日历取整会吃掉回拨那一个小时", got.UTC(), want)
	}

	// 同一个瞬间**真的**经任务递进 SQL：只测助手函数挡不住"在调用点另算一份"。
	job, scanner, _, reach, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	job.nowFunc = func() time.Time { return straddling }
	scanner.res = collScan()
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(scanner.cutoffs) != 1 || !scanner.cutoffs[0].Equal(want) {
		t.Errorf("递给扫描的 cutoff = %v，期望 %v", scanner.cutoffs, want)
	}
	if len(reach.calls) != 0 {
		t.Errorf("空集合却发了 %d 条", len(reach.calls))
	}
}

// TestCollectionLadderThresholdsAreOrdered 三档阈值必须互不重叠且方向一致，
// 否则"提醒"与"升级"会在同一天抢同一张单，或者中间出现一段谁都不管的空窗。
func TestCollectionLadderThresholdsAreOrdered(t *testing.T) {
	if !(CollectionGraceDays < CollectionEscalateAfterDays) {
		t.Errorf("宽限期 %d 天 ≥ 升级线 %d 天：一越过宽限期就直接升级，中间没有任何一次自动提醒",
			CollectionGraceDays, CollectionEscalateAfterDays)
	}
	if CollectionReminderWindow <= 0 || CollectionEscalateWindow <= CollectionReminderWindow {
		t.Errorf("提醒窗 %v / 升级窗 %v：升级窗必须更长，否则人还没处理完就又升一次",
			CollectionReminderWindow, CollectionEscalateWindow)
	}
}

// ---------------------------------------------------------------- 阶段开关

// TestCollectionStageOffSkipsTheWholeRound 给 ltc.config 的 collection 那一档
// 装上**第一个业务读者**（T-P3-06 交付它时全仓零读者）。
//
// 判据是"一次都不读库"而不是"读了库但不发"：这一档的语义是"催收这件事没开"，
// 而不是"开了但只观察"（那是 shadow 档的活，两档塌成一个的那天，
// 运营关掉阶段后就再也分不清是"没开"还是"在观察"）。
func TestCollectionStageOffSkipsTheWholeRound(t *testing.T) {
	job, scanner, _, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	job.stages = &fakeCollectionStage{on: false}

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("阶段关着不是执行失败：%v", err)
	}
	if len(scanner.cutoffs) != 0 {
		t.Errorf("阶段关着仍去扫库 %d 次：催收整档没开时这一轮不该产生任何查询", len(scanner.cutoffs))
	}
	if len(reach.calls) != 0 || len(tasks.inputs) != 0 || len(claim.sets) != 0 {
		t.Errorf("阶段关着却有动作：外发 %d 待办 %d 占锁 %d", len(reach.calls), len(tasks.inputs), len(claim.sets))
	}
	if res.StageReason != LTCReasonStageOff {
		t.Errorf("报告里 stage_reason=%q，期望把\"为什么没跑\"照实写出来", res.StageReason)
	}
	if res.SkippedByStage != 1 {
		t.Errorf("skipped_by_stage=%d，期望 1（运维要能看出\"跑过但被开关拦住\"与\"压根没起协程\"是两件事）", res.SkippedByStage)
	}
}

// TestCollectionStageReadIsPerRound 开关每轮现读，不在装配期抄一份：
// 一键回滚若要到下次重启才咬人，正是出事故时最没用的那把门（与放量档同判据）。
func TestCollectionStageReadIsPerRound(t *testing.T) {
	job, scanner, _, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	st := &fakeCollectionStage{on: false}
	job.stages = st

	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮失败: %v", err)
	}
	if len(scanner.cutoffs) != 0 {
		t.Fatal("开关关着却扫了库")
	}
	st.on = true
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第二轮失败: %v", err)
	}
	if len(scanner.cutoffs) != 1 {
		t.Errorf("改档后第二轮扫了 %d 次，期望 1 次：档位改动必须下一轮就生效", len(scanner.cutoffs))
	}
	for _, got := range st.calls {
		if got != LTCStageCollection {
			t.Errorf("读的是阶段 %q，期望 collection", got)
		}
	}
}

// ---------------------------------------------------------------- 提醒外发

// TestCollectionRemindsOverdueBillThroughTheReachSeam 正常路径一格：一张逾期 5 天的应收
// ⇒ 恰好一次外发，走 `ReachByCustomer`，带着商机上那两把身份钥匙。
func TestCollectionRemindsOverdueBillThroughTheReachSeam(t *testing.T) {
	bill := collBill("b_rem_1", "opp_rem_1", 1890.50, 5)
	job, scanner, opps, reach, tasks, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "cust-1", "one-1")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 1 {
		t.Fatalf("外发 %d 次，期望恰好 1 次", len(reach.calls))
	}
	req := reach.calls[0]
	if req.DryRun {
		t.Error("enforce 档发出了 DryRun=true 的请求：那是一条都不会真发的空跑")
	}
	if req.CustomerID != "cust-1" || req.OneID != "one-1" {
		t.Errorf("身份取错了：customer_id=%q one_id=%q（应收上没有客户列，身份只能来自商机）",
			req.CustomerID, req.OneID)
	}
	if !strings.Contains(req.Content, bill.ID) || !strings.Contains(req.Content, "1890.50") {
		t.Errorf("正文没写清代哪张单多少钱：%q", req.Content)
	}
	if res.Reminded != 1 {
		t.Errorf("reminded=%d，期望 1", res.Reminded)
	}
	if len(tasks.inputs) != 0 {
		t.Errorf("逾期 5 天（升级线 %d 天）就投了人工待办：升级必须由那条线说了算", CollectionEscalateAfterDays)
	}
	if res.Overdue != 1 || res.Truncated {
		t.Errorf("读数没抄回来：overdue=%d truncated=%v", res.Overdue, res.Truncated)
	}
}

// TestCollectionRemindsOncePerCustomerPerRound 同一客户的两张逾期单 ⇒ 一条消息。
//
// 这条不是省流量，是**频控的口径**：触达服务自己的冷却键按客户算（一小时一条），
// 一张单一条消息的后果是第二张永远撞冷却 —— 于是报告里写着"失败 1"，
// 而真实世界是"这个人只收到过一条、另一张单今天根本没人提"。
func TestCollectionRemindsOncePerCustomerPerRound(t *testing.T) {
	b1 := collBill("b_grp_1", "opp_grp", 100.00, 6)
	b2 := collBill("b_grp_2", "opp_grp", 200.00, 8)
	job, scanner, opps, reach, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(b1, b2)
	opps.rows["opp_grp"] = collOpp("opp_grp", "cust-2", "one-2")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 1 {
		t.Fatalf("同一客户收到 %d 条，期望 1 条", len(reach.calls))
	}
	body := reach.calls[0].Content
	for _, want := range []string{b1.ID, b2.ID, "300.00"} {
		if !strings.Contains(body, want) {
			t.Errorf("合并发的那条正文里没有 %q：%q", want, body)
		}
	}
	if res.Reminded != 1 {
		t.Errorf("reminded=%d：合并发只算一条外发，但两张单都得在 overdue 里", res.Reminded)
	}
	if res.Overdue != 2 {
		t.Errorf("overdue=%d，期望 2（扫到几张就是几张，合并不改变这件事）", res.Overdue)
	}
}

// TestCollectionGroupsTheSamePersonAcrossCustomerIDs 合并键是"人"（one_id），不是"客户关系"。
//
// 两张单挂在**两个不同商机**上、那两个商机的 customer_id 也不同，但 one_id 是同一个 ——
// 那是同一个人（one_id 正是跨渠道归一后的那个身份）。分组键若退回 customer_id：
// 同一个人一轮收到两条催款，而触达服务自己的冷却按客户算，第二条照样放出去
// （两把钥匙不同源，那一层拦不住这里）。报告上写的是"催了 2 张"，一句真话没有。
func TestCollectionGroupsTheSamePersonAcrossCustomerIDs(t *testing.T) {
	b1 := collBill("b_one_1", "opp_one_a", 100, 5)
	b2 := collBill("b_one_2", "opp_one_b", 200, 7)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(b1, b2)
	opps.rows[b1.OpportunityID] = collOpp(b1.OpportunityID, "cust-A", "one-SHARED")
	opps.rows[b2.OpportunityID] = collOpp(b2.OpportunityID, "cust-B", "one-SHARED")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 1 {
		t.Fatalf("同一个人（同 one_id）收到 %d 条，期望 1 条", len(reach.calls))
	}
	if got := reach.calls[0].OneID; got != "one-SHARED" {
		t.Errorf("外发请求上的 one_id=%q，期望归一后的那把钥匙", got)
	}
	for _, want := range []string{b1.ID, b2.ID, "300.00"} {
		if !strings.Contains(reach.calls[0].Content, want) {
			t.Errorf("两条逾期合并发，正文里却没有 %q：%q", want, reach.calls[0].Content)
		}
	}
	// 两张单各自占窗：合并的是消息，不是"这张单催过了"那两格记录，
	// 否则下一轮只有一张单被记住、另一张会被再催一遍。
	if len(claim.sets) != 2 {
		t.Errorf("占窗 %d 把，期望 2（%v）：一个客户一条消息，但窗按张算", len(claim.sets), claim.sets)
	}
	if res.Reminded != 1 || res.Overdue != 2 {
		t.Errorf("reminded=%d overdue=%d，期望 1/2", res.Reminded, res.Overdue)
	}
}

// TestCollectionFallsBackToCustomerIDWhenOneIDIsAbsent 没有 one_id 时必须退回 customer_id，
// 且**不许退化成空串**。
//
// 上一格钉的是"one_id 优先"，这一格钉的是它的 else 分支：那一支在全部夹具都填了 one_id 时
// 永远走不到，于是"把 fallback 整支摘掉"（`return i.oneID`）能活着走完每一格。
// 而它的真实坏法很具体：两个各自独立的企业客户，商机都还没做过身份归一（one_id 为空），
// 分组键就都成了 `""` ⇒ 两张单并进同一条催款信，信里列着"别人家的应收"，
// 而另一个人一条都没收到。这不是"少发一条"，是把 A 的账单明细发给了 B。
func TestCollectionFallsBackToCustomerIDWhenOneIDIsAbsent(t *testing.T) {
	b1 := collBill("b_fb_1", "opp_fb_a", 100, 5)
	b2 := collBill("b_fb_2", "opp_fb_b", 200, 7)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(b1, b2)
	opps.rows[b1.OpportunityID] = collOpp(b1.OpportunityID, "cust-A", "")
	opps.rows[b2.OpportunityID] = collOpp(b2.OpportunityID, "cust-B", "")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 2 {
		t.Fatalf("两个不同客户（各自没有 one_id）收到 %d 条，期望各 1 条", len(reach.calls))
	}
	byCustomer := map[string]string{}
	for _, c := range reach.calls {
		byCustomer[c.CustomerID] = c.Content
	}
	for _, tc := range []struct {
		cust, own, other string
	}{
		{"cust-A", b1.ID, b2.ID},
		{"cust-B", b2.ID, b1.ID},
	} {
		body, ok := byCustomer[tc.cust]
		if !ok {
			t.Fatalf("客户 %s 一条都没收到（calls=%d）", tc.cust, len(reach.calls))
		}
		if !strings.Contains(body, tc.own) {
			t.Errorf("%s 那条正文里没有自己的账单 %q：%q", tc.cust, tc.own, body)
		}
		if strings.Contains(body, tc.other) {
			t.Errorf("%s 那条正文里混进了另一个客户的账单 %q：%q ⇒ 分组键塌成了空串", tc.cust, tc.other, body)
		}
	}
	// 两把窗各自按账单号占：合并的是消息，不是"这张单催过了"的记录。
	if len(claim.sets) != 2 {
		t.Errorf("占窗 %d 把，期望 2（%v）", len(claim.sets), claim.sets)
	}
	if res.Reminded != 2 {
		t.Errorf("reminded=%d，期望 2（两个客户各一条）", res.Reminded)
	}
}

// TestCollectionUndatedAndTruncatedSurfaceInTheReport T-P7-01 许下的那一格：
// "账期未定"必须被数出来；封顶截断必须说实话。两者都只是读数，一条也不许发消息。
func TestCollectionUndatedAndTruncatedSurfaceInTheReport(t *testing.T) {
	job, scanner, _, reach, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = &repository.BillOverdueScan{
		Overdue: []*model.Bill{collBill("b_tr_1", "opp_tr", 50, 5)},
		Undated: 7, Truncated: true,
	}
	job.opps = &fakeCollectionOpps{rows: map[string]*model.Opportunity{"opp_tr": collOpp("opp_tr", "c", "o")}}

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if res.Undated != 7 || !res.Truncated {
		t.Errorf("undated=%d truncated=%v，期望 7/true（少了那 7 张单在视图里就不存在）", res.Undated, res.Truncated)
	}
	if len(scanner.cutoffs) != 1 || scanner.limits[0] != collectionJobDefaultBatch {
		t.Errorf("扫描参数 cutoff=%v limit=%v，期望按配置的封顶", scanner.cutoffs, scanner.limits)
	}
	if len(reach.calls) != 1 {
		t.Errorf("外发 %d 次，期望 1", len(reach.calls))
	}
}

// TestCollectionBillTheJobWontChaseIsCountedApart 任务自己不认的那三行，一行都不许动。
//
// 仓储的查询条件（due_at 非空 ∧ 早于 cutoff）与这里的判据是同一件事的两份写法，
// 所以这三行在正常链路上走不到 —— 这一格守的不是"今天的行为"，是**改其中一边的人**：
// 只挪 SQL 那一格（比如把宽限期从 3 天改成 0 天）而这里不复核，后果是在宽限期内催款，
// 那是这一族最贵的一次打扰；而"读不出账期"那一行若不挡，取天数就是 nil panic，
// 一次 panic 会带走整个 worker 协程（下一轮就不跑了，且没人看得见）。
//
// 三行合成一个计数是有意的（见 CollectionRoundReport 那格注释）：处置动作完全相同。
// 顺带断言"商机一次都没读"：拼身份只在真要发的时候才该花那次查询，
// 否则每张不该催的单都白跳一次库，而这一族一轮就是两百张的封顶。
func TestCollectionBillTheJobWontChaseIsCountedApart(t *testing.T) {
	unreadable := collBill("b_nil", "opp_nil", 100, 10)
	unreadable.DueAt = nil
	insideGrace := collBill("b_grace", "opp_grace", 100, float64(CollectionGraceDays)-1)
	job, scanner, opps, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(nil, unreadable, insideGrace)
	opps.rows[unreadable.OpportunityID] = collOpp(unreadable.OpportunityID, "c1", "o1")
	opps.rows[insideGrace.OpportunityID] = collOpp(insideGrace.OpportunityID, "c2", "o2")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("读数与判据不符不该让整轮失败: %v", err)
	}
	if res.Overdue != 3 {
		t.Errorf("overdue=%d，期望 3（这是扫描原样递来的行数，含不认的那三行）", res.Overdue)
	}
	if res.SkippedNotOverdue != 3 {
		t.Errorf("skipped_not_overdue=%d，期望 3", res.SkippedNotOverdue)
	}
	if len(reach.calls) != 0 || len(tasks.inputs) != 0 || len(claim.sets) != 0 {
		t.Errorf("不认的行被处置了：calls=%d task=%d sets=%v", len(reach.calls), len(tasks.inputs), claim.sets)
	}
	if res.Reminded != 0 || res.WouldRemind != 0 || res.Failed != 0 {
		t.Errorf("reminded=%d would_remind=%d failed=%d，期望全 0（这一格既不是发送也不是失败）",
			res.Reminded, res.WouldRemind, res.Failed)
	}
	if len(opps.calls) != 0 {
		t.Errorf("读了 %d 次商机：身份只在真要发的时候才该去拼", len(opps.calls))
	}
}

// TestCollectionGraceBoundaryIsTheFirstDayToChase 逾期满整三天那一刻**就算逾期**。
//
// 上面那条只摆了"宽限期内"的那一侧，于是 `<` 写成 `<=` 无人看守（判据没有牙）：
// 界点上那张单会被再宽限一整天，而 overdue / skipped 两个计数都照常，报告里读不出任何异常。
// 界点必须自己成一格 —— 这条与"升级线那一格"合起来，才是"三档阈值"真正落地的证据。
func TestCollectionGraceBoundaryIsTheFirstDayToChase(t *testing.T) {
	bill := collBill("b_edge_grace", "opp_edge_grace", 100, float64(CollectionGraceDays))
	if d := collFixtureNow().Sub(*bill.DueAt); d != CollectionGraceDays*24*time.Hour {
		t.Fatalf("夹具没落在界点上：欠了 %v，期望恰好 %v", d, CollectionGraceDays*24*time.Hour)
	}
	job, scanner, opps, reach, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 1 || res.Reminded != 1 {
		t.Errorf("界点上的单没被催：外发 %d 条 reminded=%d skipped_not_overdue=%d（满 %d 天就是逾期，不是还差一天）",
			len(reach.calls), res.Reminded, res.SkippedNotOverdue, CollectionGraceDays)
	}
}

// ---------------------------------------------------------------- 频控（AC②）

// TestCollectionDoesNotRemindTwiceWithinWindow 同一张单在提醒窗内只催一次：
// 锁键按**账单号**算、TTL 就是那个窗。
func TestCollectionDoesNotRemindTwiceWithinWindow(t *testing.T) {
	bill := collBill("b_win_1", "opp_win", 100, 5)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c1", "o1")

	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮失败: %v", err)
	}
	res2, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮失败: %v", err)
	}
	if len(reach.calls) != 1 {
		t.Fatalf("两轮共外发 %d 次，期望第二轮被窗拦住", len(reach.calls))
	}
	if res2.RemindersHeld != 1 {
		t.Errorf("reminders_held=%d，期望 1（这一轮确实跑到了那张单，只是不发）", res2.RemindersHeld)
	}
	if len(claim.sets) != 2 {
		// 这是后面两格索引的前置：只 Errorf 的话，占锁一次都没发生时这里会 panic 带走整个二进制
		// （同一族的坑见 TestCollectionEscalationIsOncePerWindow 里 sets[1] 那格）。
		t.Fatalf("占锁 %d 次，期望 2（第二轮也要先占才知道占没占到）", len(claim.sets))
	}
	if got := claim.ttls[claim.sets[0]]; got != CollectionReminderWindow {
		t.Errorf("提醒键 TTL=%v，期望 %v（这就是\"两次提醒至少隔多久\"那句话在代码里的样子）",
			got, CollectionReminderWindow)
	}
	if !strings.HasPrefix(claim.sets[0], collectionRemindKeyPrefix) || !strings.HasSuffix(claim.sets[0], bill.ID) {
		t.Errorf("提醒键 %q 的形状不对（前缀 %s + 账单号）", claim.sets[0], collectionRemindKeyPrefix)
	}
}

// TestCollectionClaimUnavailableIsFailClosed 取锁这件事本身失败 ⇒ **不发**。
//
// 方向与触达服务里的 checkCooldown 刻意相反，所以这条必须有用例兜着：
// 冷却 fail-open 是"少打扰"，这里的锁是"别催两遍"，缓存故障时放行就是重复外发。
func TestCollectionClaimUnavailableIsFailClosed(t *testing.T) {
	bill := collBill("b_cc_1", "opp_cc", 100, 5)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c1", "o1")
	claim.setErr = errors.New("redis down")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条取锁失败不该让整轮失败: %v", err)
	}
	if len(reach.calls) != 0 {
		t.Errorf("取锁失败还发了 %d 条：这一格的位置是\"别催两遍\"，故障时必须停手", len(reach.calls))
	}
	if res.ClaimUnavailable != 1 {
		t.Errorf("claim_unavailable=%d，期望 1（运维要能看出这一轮是被缓存故障挡住的）", res.ClaimUnavailable)
	}
}

// TestCollectionSendFailureReleasesTheWindow 发失败要把窗还回去。
//
// 不还的代价是"一次渠道抖动 = 这张单七天没人催"，而那七天里日志什么都不会再说。
func TestCollectionSendFailureReleasesTheWindow(t *testing.T) {
	bill := collBill("b_rel_1", "opp_rel", 100, 5)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c1", "o1")
	reach.errs = []error{errors.New("channel 500")}

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条失败不该让整轮失败: %v", err)
	}
	if len(claim.sets) != 1 {
		t.Fatalf("提醒键占了 %d 把，期望 1 ⇒ 前置不成立（要先占到才谈得上还）：sets=%v", len(claim.sets), claim.sets)
	}
	if len(claim.releases) != 1 || claim.releases[0] != claim.sets[0] {
		t.Errorf("发失败后没把提醒键还掉：releases=%v sets=%v", claim.releases, claim.sets)
	}
	if res.Failed != 1 || res.Reminded != 0 {
		t.Errorf("failed=%d reminded=%d，期望 1/0", res.Failed, res.Reminded)
	}
	if job.RemindedTotal() != 0 {
		t.Errorf("累计已催=%d，期望 0（失败的一轮不许进累计读数）", job.RemindedTotal())
	}
}

// TestCollectionBlockedReasonsAreCountedApart 退订 / 闸门拒 / 冷却三种"没发出去"分开记。
//
// 合成一个 failed 的话，运维看到的是"催收发不出去"，而三者的处置完全不同：
// 退订要停（且要升级）、闸门要补授权、冷却只要下一轮再来。
func TestCollectionBlockedReasonsAreCountedApart(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    func(*CollectionRoundReport) int
		keepKey bool
	}{
		{"退订", fmt.Errorf("%w: opted out", ErrDoNotContact),
			func(r *CollectionRoundReport) int { return r.BlockedByDNC }, true},
		{"闸门拒", fmt.Errorf("%w: no approval", ErrReachApprovalDenied),
			func(r *CollectionRoundReport) int { return r.BlockedByApproval }, false},
		{"冷却", fmt.Errorf("%w: recently received", ErrReachCooldown),
			func(r *CollectionRoundReport) int { return r.BlockedByCooldown }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bill := collBill("b_blk_"+tc.name, "opp_blk", 100, 5)
			job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
			scanner.res = collScan(bill)
			opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c1", "o1")
			reach.errs = []error{tc.err}

			res, err := job.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("本轮失败: %v", err)
			}
			if got := tc.want(res); got != 1 {
				t.Errorf("那一格计数=1，期望 1；报告 %+v", res)
			}
			if tc.keepKey && len(claim.releases) != 0 {
				t.Errorf("退订是要长期停的一格，键却被还掉了：下轮又试一遍，等于每天重打扰一次已退订的人")
			}
			if !tc.keepKey && len(claim.releases) == 0 {
				t.Errorf("%s 拒发后没还窗：这一次没发出去却占了七天", tc.name)
			}
			if res.Reminded != 0 {
				t.Errorf("%s 拒发却记了 reminded=%d", tc.name, res.Reminded)
			}
		})
	}
}

// ---------------------------------------------------------------- 升级待办（AC③）

// TestCollectionEscalatesAfterThresholdAndStopsReminding 越过升级线 ⇒ 投待办、不再自动催。
//
// "不再自动催"是这一族的收口判据：升级的意义是"这件事交给人了"，
// 交给人之后系统还在按窗群发，人处理完回来发现客户又被机器人催了两轮。
// 两档由那条线互斥地分开，所以这里要同时断言"外发 0 次"。
func TestCollectionEscalatesAfterThresholdAndStopsReminding(t *testing.T) {
	bill := collBill("b_esc_1", "opp_esc", 5000, float64(CollectionEscalateAfterDays)+2)
	job, scanner, opps, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "cust-e", "one-e")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 0 {
		t.Errorf("已升级的单还外发 %d 条：升级之后催收的话由人说，不由任务说", len(reach.calls))
	}
	if len(tasks.inputs) != 1 {
		t.Fatalf("待办投了 %d 条，期望 1", len(tasks.inputs))
	}
	in := tasks.inputs[0]
	if in.Kind != model.HumanTaskKindCollectionEscalation {
		t.Errorf("kind=%q，期望 %q（三类分离视图按这一格分流）", in.Kind, model.HumanTaskKindCollectionEscalation)
	}
	if in.SubjectType != HumanTaskSubjectCollectionCase {
		t.Errorf("subject_type=%q，期望常量 %q", in.SubjectType, HumanTaskSubjectCollectionCase)
	}
	if in.SubjectID != bill.ID {
		t.Errorf("subject_id=%q，期望账单号 %q（待办必须钉在**那一张应收**上，不是商机也不是报价）", in.SubjectID, bill.ID)
	}
	if in.SlaDueAt == nil {
		t.Fatal("sla_due_at 为空：collection 类的截止是 slaFor 的必填格，空着 Submit 会直接拒")
	}
	if got := in.SlaDueAt.Sub(collFixtureNow()); got != CollectionEscalateResponseWindow {
		t.Errorf("待办截止是 now+%v，期望 %v", got, CollectionEscalateResponseWindow)
	}
	// 上一格是**拿常量算的**（同义反复）：把 CollectionEscalateResponseWindow 改成 0、
	// 改成 3 小时，它会跟着一起绿。这一格是那一句的独立下界：待办截止短于一天，
	// 意味着值班的人早上打开中心，这条已经躺在"逾期未处理"里了 ——
	// 升级待办的整条 SLA 读数会开始自己制造噪音，而没人会想到是装配期的一个常数。
	if CollectionEscalateResponseWindow < 24*time.Hour {
		t.Errorf("升级待办处理窗 = %v，短于一天 ⇒ 投出去即逾期，SLA 视图会被自己刷屏",
			CollectionEscalateResponseWindow)
	}
	if in.OneID != "one-e" {
		t.Errorf("one_id=%q：待办上要能直接看到是谁的钱", in.OneID)
	}
	if !strings.Contains(in.Title, bill.ID) {
		t.Errorf("标题里没有账单号：%q", in.Title)
	}
	// 前缀写成字面量而不是 `collectionBillPayloadRefPrefix`：这一格证的正是
	// "待办上那个跳转地址与账单读侧路由同源"这句话，跟着常量算就又成同义反复了。
	if in.PayloadRef != "/api/bill/"+bill.ID {
		t.Errorf("payload_ref=%q，期望 \"/api/bill/%s\"（接手的人点开要落在那张应收上，"+
			"拼错的人只会让他再跳两跳，然后当作又一条系统噪音关掉）", in.PayloadRef, bill.ID)
	}
	for _, want := range []string{bill.ID, "5000.00", strconv.Itoa(CollectionEscalateAfterDays) + " 天"} {
		if !strings.Contains(in.Reason, want) {
			t.Errorf("升级理由里没有 %q：%q（待办正文要一次说清是哪张单、多少钱、越的是哪条线）", want, in.Reason)
		}
	}
	if res.Escalated != 1 {
		t.Errorf("escalated=%d，期望 1", res.Escalated)
	}
	if len(claim.sets) == 0 || !strings.HasPrefix(claim.sets[0], collectionEscalateKeyPrefix) {
		t.Errorf("升级没走升级键：%v（提醒键与升级键必须是两把，共用一把会互相吞掉）", claim.sets)
	}
}

// TestCollectionEscalateBoundaryIsTheFirstDayToHandOff 逾期满十四天那一刻**就算要升级**。
//
// 与宽限期那一格成对（那边挡 `<`→`<=`，这边挡 `>=`→`>`）：写成严格大于，
// 界点上的那张单会被自动催**又**不升级，人看到它时已经晚了整整一轮（6 小时），
// 而那一轮里客户又收到了一条机器人的催款 —— 两档的互斥正是"升级"这件事的全部意义。
func TestCollectionEscalateBoundaryIsTheFirstDayToHandOff(t *testing.T) {
	bill := collBill("b_edge_esc", "opp_edge_esc", 100, float64(CollectionEscalateAfterDays))
	if d := collFixtureNow().Sub(*bill.DueAt); d != CollectionEscalateAfterDays*24*time.Hour {
		t.Fatalf("夹具没落在界点上：欠了 %v，期望恰好 %v", d, CollectionEscalateAfterDays*24*time.Hour)
	}
	job, scanner, opps, reach, tasks, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(tasks.inputs) != 1 || res.Escalated != 1 {
		t.Errorf("界点上的单没升级：待办 %d 条 escalated=%d", len(tasks.inputs), res.Escalated)
	}
	if len(reach.calls) != 0 || res.Reminded != 0 {
		t.Errorf("同一张单同一条既升级又自动催：外发 %d 条 reminded=%d（两档必须由那条线互斥分开）",
			len(reach.calls), res.Reminded)
	}
}

// TestCollectionEscalationIsOncePerWindow 升级窗内不许反复投：人工关闭那条待办之后
// 也不能下一轮（6 小时）就凭空多出一条一模一样的。
//
// 这一格挡的是 Submit 自己的幂等边界之外的东西 —— 它按"有没有**开放**待办"判重，
// 人已处理完（completed）就再投一条。窗口由这把按账单号的锁兜住。
func TestCollectionEscalationIsOncePerWindow(t *testing.T) {
	bill := collBill("b_twice", "opp_twice", 100, float64(CollectionEscalateAfterDays)+1)
	job, scanner, opps, _, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")

	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	res2, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if len(tasks.inputs) != 1 {
		t.Errorf("两轮投了 %d 条待办，期望 1（升级窗内一个人接手一次就够）", len(tasks.inputs))
	}
	// 拦住了要说得出"为什么没投"：escalated 与 tasks_reused 都停在 0 而没人报 held，
	// 运维读到的就是"这一轮什么都没发生"，与 Redis 坏了那一种长得一模一样。
	if res2.EscalationHeld != 1 || res2.Escalated != 0 || res2.TasksReused != 0 {
		t.Errorf("第二轮 escalation_held=%d escalated=%d tasks_reused=%d，期望 1/0/0",
			res2.EscalationHeld, res2.Escalated, res2.TasksReused)
	}
	// 索引前先把长度关进本腿：S55 那一格（shadow 判定写反）会让这条路径一个键都不占，
	// 裸 `sets[1]` 于是 panic 带走整个二进制 —— 后面的用例全都不跑，电池的计数也就不能判。
	if len(claim.sets) < 2 {
		t.Fatalf("两轮升级只占了 %d 把窗（sets=%v）⇒ 前置不成立：第二轮没去占升级窗，下面的 TTL 断言没有对象",
			len(claim.sets), claim.sets)
	}
	if got := claim.ttls[claim.sets[1]]; got != CollectionEscalateWindow {
		t.Errorf("升级键 TTL=%v，期望 %v", got, CollectionEscalateWindow)
	}
}

// TestCollectionTaskReuseIsNotAFailure Submit 回 created=false 是"这件事已经有人在等"，
// 既不是失败也不该把窗还掉。
func TestCollectionTaskReuseIsNotAFailure(t *testing.T) {
	bill := collBill("b_reuse", "opp_reuse", 100, float64(CollectionEscalateAfterDays)+1)
	job, scanner, opps, _, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")
	tasks.created = false

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("created=false 被当成了失败: %v", err)
	}
	if res.TasksReused != 1 || res.Escalated != 0 {
		t.Errorf("tasks_reused=%d escalated=%d，期望 1/0", res.TasksReused, res.Escalated)
	}
	if len(claim.releases) != 0 {
		t.Errorf("复用了一条开放待办却把窗还掉：下一轮还会去 Submit 一次：%v", claim.releases)
	}
}

// TestCollectionEscalateSubmitFailureReleasesWindow 与发失败同一条理由：
// 待办没落成 ⇒ 这次升级等于没发生，别占着窗。
func TestCollectionEscalateSubmitFailureReleasesWindow(t *testing.T) {
	bill := collBill("b_sf", "opp_sf", 100, float64(CollectionEscalateAfterDays)+1)
	job, scanner, opps, _, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")
	tasks.err = errors.New("human task 底座不可用")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条失败不该让整轮失败: %v", err)
	}
	if len(claim.releases) != 1 {
		t.Errorf("Submit 失败后 releases=%v，期望把升级键还掉", claim.releases)
	}
	if res.Failed != 1 || res.Escalated != 0 {
		t.Errorf("failed=%d escalated=%d，期望 1/0", res.Failed, res.Escalated)
	}
}

// TestCollectionEscalateClaimUnavailableSubmitsNothing 升级这一路也 fail-closed。
//
// 催的那一路有 fail-closed 判据不代表升这一路也有 —— 两路各自一次 setNX，代码差一行。
// 缓存故障时"照常投待办"的后果比重复外发更隐蔽：待办落了、窗没占住，
// 下一轮（6 小时后）再投一次，Submit 的幂等按"开放待办"判重会复用它（不报错），
// 于是运维面上只看到 escalated=0 而 tasks_reused 一路涨，看不出是 Redis 坏了。
func TestCollectionEscalateClaimUnavailableSubmitsNothing(t *testing.T) {
	bill := collBill("b_esc_cc", "opp_esc_cc", 100, float64(CollectionEscalateAfterDays)+3)
	job, scanner, opps, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.rows[bill.OpportunityID] = collOpp(bill.OpportunityID, "c", "o")
	claim.setErr = errors.New("redis down")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条取锁失败不该让整轮失败: %v", err)
	}
	if len(tasks.inputs) != 0 {
		t.Errorf("取锁失败仍投了 %d 条待办：窗没占住就重复投递", len(tasks.inputs))
	}
	if res.ClaimUnavailable != 1 || res.Escalated != 0 || res.TasksReused != 0 {
		t.Errorf("claim_unavailable=%d escalated=%d tasks_reused=%d，期望 1/0/0",
			res.ClaimUnavailable, res.Escalated, res.TasksReused)
	}
	if res.Failed != 0 {
		t.Errorf("failed=%d：缓存故障有自己的那一格，不许塌进通用失败", res.Failed)
	}
	if len(reach.calls) != 0 {
		t.Errorf("已越过升级线的单还外发了：越线以后催的话由人说，不由任务说")
	}
}

// ---------------------------------------------------------------- 身份来路

// TestCollectionMissingIdentitySendsNothingAndKeepsNoWindow 商机读不到、
// 或商机上没有客户身份 ⇒ 一条都不发，且**不占窗**。
//
// 不占窗是这条的牙齿：应收表上没有客户列（判据见 model/bill.go），
// 身份只能从 opportunity_id 现推。推不出来的时候如果还占了窗，
// 那这七天里即使有人把商机补好了，系统也不会再催 —— 一个数据缺口变成一次静默免催。
func TestCollectionMissingIdentitySendsNothingAndKeepsNoWindow(t *testing.T) {
	for _, tc := range []struct{ name, oppID string }{
		{"商机不存在", "opp_missing"},
		{"商机没有客户身份", "opp_blank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bill := collBill("b_id_"+tc.name, tc.oppID, 100, 5)
			job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
			scanner.res = collScan(bill)
			opps.rows["opp_blank"] = collOpp("opp_blank", "", "")

			res, err := job.RunOnce(context.Background())
			if err != nil {
				t.Fatalf("本轮失败: %v", err)
			}
			if len(reach.calls) != 0 {
				t.Errorf("身份不明却发了 %d 条", len(reach.calls))
			}
			if len(claim.sets) != 0 {
				t.Errorf("没发出去却占了窗：%v（补好身份后这一轮本可以再试）", claim.sets)
			}
			if res.SkippedNoIdentity != 1 {
				t.Errorf("skipped_no_identity=%d，期望 1", res.SkippedNoIdentity)
			}
		})
	}
}

// TestCollectionOpportunityReadFailureIsNotMissingIdentity 库里读商机失败是"这一刻不知道"，
// 不是"这张单没有身份"：前者要下一轮重试并记 failed，后者是终局。
func TestCollectionOpportunityReadFailureIsNotMissingIdentity(t *testing.T) {
	bill := collBill("b_err_1", "opp_err", 100, 5)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)
	opps.err = errors.New("connection reset")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条读失败不该让整轮失败: %v", err)
	}
	if len(reach.calls) != 0 || len(claim.sets) != 0 {
		t.Errorf("读商机失败还发了/占窗了：calls=%d sets=%v", len(reach.calls), claim.sets)
	}
	if res.Failed != 1 || res.SkippedNoIdentity != 0 {
		t.Errorf("failed=%d skipped_no_identity=%d，期望 1/0（两种坏法的处置动作不同）",
			res.Failed, res.SkippedNoIdentity)
	}
}

// TestCollectionBillWithoutQuoteLineageIsAFailure 应收行上没有商机来路 ⇒ 记 failed，不记"没身份"。
//
// 这一格离得开 `SkippedNoIdentity` 才有意义：后者是**这一族正常的空**（商机可以还没补齐客户），
// 前者是"这张凭证根本不该存在"。两张都往 skipped 里塌，等于把一次数据完整性破口
// 归进"今天没人可催"那句 harmless 的读数里 —— 而催收这条腿读的正是账单行的来路列，
// 它空着说明写这张单的人（派生那一路）漏了字段，那必须有人在 failed 里看见。
//
// 顺带断言"商机一次都没读"：来路为空时连查都不该查（拿空串去 WHERE 是"没有条件的读"）。
func TestCollectionBillWithoutQuoteLineageIsAFailure(t *testing.T) {
	bill := collBill("b_noline", "", 100, 5)
	job, scanner, opps, reach, _, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.res = collScan(bill)

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("单条数据破口不该让整轮失败: %v", err)
	}
	if res.Failed != 1 || res.SkippedNoIdentity != 0 {
		t.Errorf("failed=%d skipped_no_identity=%d，期望 1/0（缺来路是破口，不是「没有身份」）",
			res.Failed, res.SkippedNoIdentity)
	}
	if len(opps.calls) != 0 {
		t.Errorf("拿着空商机号去查了 %d 次：来路为空的行在读库之前就该被拒", len(opps.calls))
	}
	if len(reach.calls) != 0 || len(claim.sets) != 0 {
		t.Errorf("来路不明却发了/占窗了：calls=%d sets=%v", len(reach.calls), claim.sets)
	}
}

// ---------------------------------------------------------------- shadow 档

// TestCollectionShadowSendsDryRunAndWritesNothing shadow：只选路、不发、不写、不占窗。
//
// 三格一起断言是因为它们是一个意思：观察档不能对现网留下任何后续效果。
// 占窗那一格最容易被漏 —— DryRun 在触达服务里短路于冷却 SetNX 之前，
// 所以那边不留痕；这边若先占了自己的窗，shadow 跑一轮就把真催的额度烧掉了。
func TestCollectionShadowSendsDryRunAndWritesNothing(t *testing.T) {
	near := collBill("b_sh_1", "opp_sh", 100, 5)
	far := collBill("b_sh_2", "opp_sh2", 200, float64(CollectionEscalateAfterDays)+3)
	job, scanner, opps, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeShadow)
	scanner.res = collScan(near, far)
	opps.rows[near.OpportunityID] = collOpp(near.OpportunityID, "c1", "o1")
	opps.rows[far.OpportunityID] = collOpp(far.OpportunityID, "c2", "o2")

	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if len(reach.calls) != 1 {
		t.Fatalf("shadow 档外发调用 %d 次，期望恰好 1 次（只干那一张还没到升级线的）", len(reach.calls))
	}
	if !reach.calls[0].DryRun {
		t.Error("shadow 档发出了 DryRun=false 的请求：那是真发")
	}
	if len(tasks.inputs) != 0 {
		t.Errorf("shadow 档投了 %d 条待办：观察档不写台账", len(tasks.inputs))
	}
	if len(claim.sets) != 0 {
		t.Errorf("shadow 档占了 %d 把窗：%v —— 观察一轮不该烧掉真催的额度", len(claim.sets), claim.sets)
	}
	if res.WouldRemind != 1 || res.WouldEscalate != 1 {
		t.Errorf("would_remind=%d would_escalate=%d，期望 1/1（预计值才是 shadow 的产出）",
			res.WouldRemind, res.WouldEscalate)
	}
	if res.Reminded != 0 || res.Escalated != 0 {
		t.Errorf("shadow 档却记了 reminded=%d escalated=%d", res.Reminded, res.Escalated)
	}
	if job.RemindedTotal() != 0 || job.EscalatedTotal() != 0 {
		t.Errorf("累计读数被 shadow 污染: reminded=%d escalated=%d", job.RemindedTotal(), job.EscalatedTotal())
	}
}

// TestCollectionOffModeRunsNoRound off（含未配置）连查询都不发：
// 这一档的语义是"这条腿没接"，与"接了但只观察"（shadow）必须分得开。
func TestCollectionOffModeRunsNoRound(t *testing.T) {
	job, scanner, _, reach, tasks, _ := newTestCollectionJob(RecoveryWorkerModeOff)
	res, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("off 档不是执行失败: %v", err)
	}
	if len(scanner.cutoffs) != 0 || len(reach.calls) != 0 || len(tasks.inputs) != 0 {
		t.Errorf("off 档仍跑了：scan=%d reach=%d task=%d", len(scanner.cutoffs), len(reach.calls), len(tasks.inputs))
	}
	if res.Mode != string(RecoveryWorkerModeOff) {
		t.Errorf("报告 mode=%q，期望 off", res.Mode)
	}
	// 关档也要留下一句"为什么这一轮什么都没干"：off 与"跑了但一张都没扫到"
	// 在计数面上完全相同（全 0），只有这一格分得开。
	if res.DependencyError == "" {
		t.Error("off 档的报告是空的 ⇒ 端点上 last.dependency_error 读不出\"这条腿没接\"")
	}
}

// ---------------------------------------------------------------- 失败面

// TestCollectionScanFailureIsReportedNotReadAsQuiet 扫描失败必须让整轮出声。
//
// 与仓储侧那条判据同源（T-P7-01 的 K38）：把库故障读成"今天没人逾期"，
// 日志里写下的正是一句假话，而催收停摆最舒服的形状就是这种安静。
func TestCollectionScanFailureIsReportedNotReadAsQuiet(t *testing.T) {
	job, scanner, _, reach, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.err = errors.New(`relation "bills" does not exist`)

	res, err := job.RunOnce(context.Background())
	if err == nil {
		t.Error("扫描失败却回了 nil error：调用方（与 loop 里的日志）会把这一轮当成正常空跑")
	}
	if res.ScanError == "" {
		t.Error("报告里 scan_error 是空的")
	}
	if len(reach.calls) != 0 {
		t.Errorf("扫描失败后还发了 %d 条", len(reach.calls))
	}
}

// TestCollectionNilScanReadingIsRefused (nil, nil) 既不是"没人逾期"也不是"读失败"，不能采信。
//
// 这一格单独存在的理由：`len(nil.Overdue)` 在 Go 里对 nil **指针**解引用是 panic，
// 而一次 panic 会带走整个测试二进制（其余用例会一起消失，看起来像"跑得快"）。
// 所以这里把 panic 收在本条之内：正确实现下走不到 recover；有人删掉那道守卫时，
// 本条要报的是"panic 了"，而不是把全族带走。
func TestCollectionNilScanReadingIsRefused(t *testing.T) {
	job, scanner, _, reach, tasks, claim := newTestCollectionJob(RecoveryWorkerModeEnforce)
	scanner.nilRes = true

	var res *CollectionRoundReport
	var runErr error
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		res, runErr = job.RunOnce(context.Background())
	}()
	if recovered != nil {
		t.Fatalf("扫描回 nil 读数时直接 panic（%v）：那既不是空集也不是错，必须先拒掉再继续", recovered)
	}
	if runErr == nil {
		t.Error("采信了 (nil, nil)：这一轮的日志会写成「本轮扫到 0 张逾期」，而真实情况是读不出")
	}
	if res == nil {
		t.Fatal("没有报告：调用方（loop 里的日志与 /ops 读数）会拿到 nil")
	}
	if res.ScanError == "" {
		t.Error("报告里 scan_error 是空的")
	}
	if len(reach.calls) != 0 || len(tasks.inputs) != 0 || len(claim.sets) != 0 {
		t.Errorf("读数不可采信却动了手：外发 %d 待办 %d 占窗 %d", len(reach.calls), len(tasks.inputs), len(claim.sets))
	}
	if res.Overdue != 0 || res.Undated != 0 || res.Truncated {
		t.Errorf("不可采信的读数被抄进了报告：overdue=%d undated=%d truncated=%v",
			res.Overdue, res.Undated, res.Truncated)
	}
}

// TestCollectionRoundCountersAreCumulative 累计读数跨轮单调（运维面的 /ops 读它）。
//
// 只留 LastReport 的后果在挽回 worker 那边已经付过一次学费：一轮覆盖一轮，
// 轮询到的可能正好是被清掉之后的那一格。
func TestCollectionRoundCountersAreCumulative(t *testing.T) {
	b1 := collBill("b_cum_1", "opp_cum", 100, 5)
	job, scanner, opps, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	opps.rows["opp_cum"] = collOpp("opp_cum", "c", "o")

	scanner.res = collScan(b1)
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	first := job.RemindedTotal()
	scanner.res = collScan(collBill("b_cum_2", "opp_cum", 100, 6))
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if job.RemindedTotal() <= first {
		t.Errorf("第二轮之后累计已催 %d ≤ 第一轮的 %d：累计只朝一个方向走", job.RemindedTotal(), first)
	}
	if job.LastReport() == nil {
		t.Fatal("LastReport 为空")
	}
}

// TestCollectionLastReportIsNeverAHalfRound 观测面不许读到"跑到一半"的那一轮。
//
// 这一格盯的是一个真会发生的并发：`/api/ltc/collection/status` 在任意时刻调 LastReport，
// 而协程那一侧的 RunOnce 正拿着**同一个** report 往上填数。写的一侧不持锁、读的一侧持
// RLock —— 锁只订得住那个指针，订不住字段。所以判据有两层：
//   - detector 层：读者常驻整条用例，只有它真的一直在读，`-race` 才有机会把没拿锁的
//     那次写抓出来（按本仓口径认"本腿名 + collection_job.go"，不认栈里的变量名 ——
//     摘锁的单行 getter 会被内联，栈上根本没有名字）；
//   - 断言层（不打 -race 也成立）：一轮跑到一半时读到的必须仍是**上一轮跑完的**那份，
//     这也是同族挽回 worker 的口径（它把 setLast 放在每一个 return 上）。
//
// 半份读数为什么贵：那一格写的是"这一轮催了几条"。它被读到 0 的时候，运营看到的就是
// "一个逾期一堆人的轮次居然一条没发"，而真实答案在这一行还没写完的那半格里。
func TestCollectionLastReportIsNeverAHalfRound(t *testing.T) {
	job, scanner, opps, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	opps.rows["opp_race"] = collOpp("opp_race", "cust-race", "one-race")
	scanner.res = &repository.BillOverdueScan{Overdue: []*model.Bill{
		collBill("b_rc_1", "opp_race", 100, 4),
		collBill("b_rc_2", "opp_race", 200, 5),
		collBill("b_rc_3", "opp_race", 300, 6),
	}}

	stopReading := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 2; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stopReading:
					return
				default:
					job.LastReport()
				}
			}
		}()
	}
	defer func() {
		close(stopReading)
		readers.Wait()
	}()

	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	first := job.LastReport()
	if first == nil || first.Overdue != 3 || first.Reminded != 1 {
		t.Fatalf("第一轮跑完后读数 = %+v，期望 overdue=3 reminded=1（前置不成立，后面的断言没有对象）", first)
	}

	scanner.entered = make(chan struct{}, 1)
	scanner.release = make(chan struct{})
	roundDone := make(chan struct{})
	go func() {
		defer close(roundDone)
		if _, err := job.RunOnce(context.Background()); err != nil {
			t.Errorf("第二轮: %v", err)
		}
	}()
	select {
	case <-scanner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("第二轮没进扫描：夹具没把那一轮停在半途")
	}
	if mid := job.LastReport(); mid == nil || mid.Overdue != 3 || mid.Reminded != 1 {
		t.Errorf("一轮跑到一半时观测面读数 = %+v，期望仍是上一轮跑完的那份（overdue=3 reminded=1）"+
			"⇒ 报告在开跑之前就被发布了，被翻到的数可能是半份", mid)
	}
	close(scanner.release)
	<-roundDone

	after := job.LastReport()
	if after == nil || after.Overdue != 3 || after.Reminded != 0 || after.RemindersHeld != 3 {
		t.Errorf("第二轮跑完后读数 = %+v，期望 overdue=3 reminded=0 reminders_held=3"+
			"（第二轮跑完了却没发布，或发布的不是它）", after)
	}
}

// TestCollectionPublishedRoundIsDetachedFromBothHands 一轮读数交出去两份，两份都必须是抄件。
//
// 写侧（RunOnce 的返回值）与读侧（LastReport）若是同一个格子，"拿到返回值的那一方"改一格
// 就污染了运维端点上的读数 —— 而谁持有那个返回值编译器与观测面都管不着。
// 读侧那一半由本卡的变异电池钉（把拷贝摘掉必须红），写侧这一半由下面第二条断言钉。
func TestCollectionPublishedRoundIsDetachedFromBothHands(t *testing.T) {
	job, scanner, opps, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	opps.rows["opp_dt"] = collOpp("opp_dt", "cust-dt", "one-dt")
	scanner.res = collScan(collBill("b_dt_1", "opp_dt", 100, 5))

	got, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("本轮失败: %v", err)
	}
	if got == nil {
		t.Fatal("RunOnce 回了 nil 读数")
	}
	base := job.LastReport()
	if base == nil || base.Overdue != got.Overdue {
		t.Fatalf("LastReport=%+v RunOnce=%+v ⇒ 前置不成立：这一轮根本没发布", base, got)
	}

	if probe := job.LastReport(); probe == got {
		t.Error("LastReport 与 RunOnce 交出同一个指针 ⇒ 任一侧改一格，另一侧跟着变")
	}
	// 读侧：拿到的抄件改掉一格，下一次读必须仍是原值。
	probe := job.LastReport()
	probe.Overdue = 9999
	if again := job.LastReport(); again == nil || again.Overdue != base.Overdue {
		t.Errorf("LastReport 交出的是活的指针：改探针之后读到 %+v，原值 overdue=%d", again, base.Overdue)
	}
	// 写侧：拿到返回值的一方改一格，观测端点不许跟着变。
	got.Reminded = 8888
	if after := job.LastReport(); after == nil || after.Reminded != base.Reminded {
		t.Errorf("RunOnce 的返回值与发布出去的读数同格：调用方改一格后读到 %+v，原值 reminded=%d",
			after, base.Reminded)
	}
}

// TestCollectionEscalatedTotalIsCumulative 升级那一半的累计读数同判据（运维面 /ops 两格都读）。
//
// 这里断言的是**确切值**而不是"单调"：只写"变大了"的话，一轮加两下与一轮加一下
// 都能过，而这一格要给运营的答案恰恰是"系统一共把人叫来过几次"。
func TestCollectionEscalatedTotalIsCumulative(t *testing.T) {
	job, scanner, opps, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	const days = float64(CollectionEscalateAfterDays) + 1
	opps.rows["opp_ea"] = collOpp("opp_ea", "c", "o")
	opps.rows["opp_eb"] = collOpp("opp_eb", "c", "o")

	scanner.res = collScan(collBill("b_ea_1", "opp_ea", 100, days))
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第一轮: %v", err)
	}
	if got := job.EscalatedTotal(); got != 1 {
		t.Fatalf("第一轮后累计升级=%d，期望 1", got)
	}
	scanner.res = collScan(collBill("b_eb_1", "opp_eb", 100, days))
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("第二轮: %v", err)
	}
	if got := job.EscalatedTotal(); got != 2 {
		t.Errorf("第二轮后累计升级=%d，期望 2（每真落成一条加一下，不重复计、不漏计）", got)
	}
	if job.RemindedTotal() != 0 {
		t.Errorf("累计已催=%d，期望 0：两格计数各记各的，混了就没法回答「到底发出去几条」", job.RemindedTotal())
	}
}

// ---------------------------------------------------------------- 正文可复现

// TestCollectionReminderBodyIsPure 正文是 (账单行, now) 的纯函数。
//
// 这条判据对着的是本仓那条旧账"外发正文不可复现"（SOP 那一路读的是上游文案）。
// 催收这一路只有两个输入，所以它**必须**逐字节可复现：同一张单、同一时刻，
// 两次跑出来的话不一样，事后就无法回答"我们当时到底说了什么"。
func TestCollectionReminderBodyIsPure(t *testing.T) {
	bills := []*model.Bill{collBill("b_pure_1", "opp_p", 1890.50, 5), collBill("b_pure_2", "opp_p", 12.05, 9)}
	first := collectionReminderBody(bills, collFixtureNow())
	second := collectionReminderBody(bills, collFixtureNow())
	if first != second {
		t.Errorf("同输入不同输出：\n%q\n%q", first, second)
	}
	for _, want := range []string{"b_pure_1", "b_pure_2", "1902.55", model.BillCurrencyDefault} {
		if !strings.Contains(first, want) {
			t.Errorf("正文里没有 %q：%q", want, first)
		}
	}
	if !strings.Contains(first, strconv.Itoa(9)) {
		t.Errorf("正文里看不出欠得最久的那张逾期几天：%q", first)
	}
	// 不带上游内部字段：报价行号是内部主键，客户看了也无法对应到任何一张单。
	if strings.Contains(first, "qr-b_pure_1") {
		t.Errorf("正文漏出了内部行主键：%q", first)
	}
}

// TestCollectionOverdueDaysDisplayFloorsRatherThanRounds 正文里的"已逾期 N 天"向下取整。
//
// 这一格盯的是显示与判据**Say the same thing**：是否催、是否升级由时长比较决定
// （`overdueFor >= CollectionEscalateAfterDays*24h`），给客户看的那句话由
// collectionOverdueDays 的整数除法决定。全部整数日夹具下，把 `int(d/24h)` 改成
// 进位（`int((d+24h-1ns)/24h)`）每一格都还是绿的 —— 而在 13.9 天那一格它会红：
// 正文写"已逾期 14 天"，客户据此认定自己越过了升级线、该有人介入了，
// 系统却还在自动催（判据那边 13.9 < 14 成立不了）。两边各说各话，
// 而收信的人只看得见其中一个数。
func TestCollectionOverdueDaysDisplayFloorsRatherThanRounds(t *testing.T) {
	now := collFixtureNow()
	part := collBill("b_floor_1", "opp_floor", 100, 5.5)
	justBelow := collBill("b_floor_2", "opp_floor", 200, float64(CollectionEscalateAfterDays)-0.1)
	body := collectionReminderBody([]*model.Bill{part, justBelow}, now)

	lines := map[string]string{}
	for _, l := range strings.Split(body, "\n") {
		for _, b := range []*model.Bill{part, justBelow} {
			if strings.Contains(l, b.ID) {
				lines[b.ID] = l
			}
		}
	}
	for _, tc := range []struct {
		id   string
		want string
		deny string
	}{
		{part.ID, "已逾期 5 天", "已逾期 6 天"},
		{justBelow.ID, "已逾期 " + strconv.Itoa(CollectionEscalateAfterDays-1) + " 天",
			"已逾期 " + strconv.Itoa(CollectionEscalateAfterDays) + " 天"},
	} {
		l, ok := lines[tc.id]
		if !ok {
			t.Fatalf("正文里没有 %s 这一行：%q", tc.id, body)
		}
		if !strings.Contains(l, tc.want) {
			t.Errorf("%s 那行没写 %q：%q", tc.id, tc.want, l)
		}
		if strings.Contains(l, tc.deny) {
			t.Errorf("%s 那行进位成了 %q：%q ⇒ 显示说越了线，判据却还没", tc.id, tc.deny, l)
		}
	}
}

// TestCollectionReminderBodyKeepsCurrenciesApart 两种币种各列各的合计，没填的回落默认币种。
//
// 混成一个总数是给人发一张算错的账单：客户照那个数付、财务照凭证对，两边都错。
// 这条同时把"逐字节可复现"放大到 50 次 —— 合计那段是按币种排序拼的，
// 而 Go 的 map 遍历序**每次 range 都随机**：只跑两遍的话，摘掉 sort 有约一半概率
// 两次恰好同序（判据没有牙），50 次同序的概率是 2^-49。
// 这不是把用例写重，是"可复现"这句话本来就要按可复现的方式测。
func TestCollectionReminderBodyKeepsCurrenciesApart(t *testing.T) {
	usd := collBill("b_cur_usd", "opp_cur", 1890.50, 5)
	usd.Currency = "USD"
	blank := collBill("b_cur_blank", "opp_cur", 12.05, 6)
	blank.Currency = "" // 上游没填 ⇒ 必须落成默认币种，而不是落成一个空格
	bills := []*model.Bill{usd, blank}
	now := collFixtureNow()

	first := collectionReminderBody(bills, now)
	for i := 0; i < 50; i++ {
		if got := collectionReminderBody(bills, now); got != first {
			t.Fatalf("第 %d 次与第 1 次不同：\n%q\n%q ⇒ 正文里有一格读的是随机序（map 遍历）", i+2, first, got)
		}
	}
	for _, want := range []string{"1890.50 USD", "12.05 CNY", " + "} {
		if !strings.Contains(first, want) {
			t.Errorf("正文里没有 %q：%q", want, first)
		}
	}
	if strings.Contains(first, "1902.55") {
		t.Errorf("两种币种被加成了 1902.55：%q —— 那是把 USD 当 CNY 收", first)
	}
}

// TestCollectionMoneyIsRenderedToCents 金额按分固定输出，不用 %v。
// 0.1+0.2 那种 float64 尾巴出现在**催款消息**里是最贵的一种显示 bug：
// 客户会照着那个数字付款，而财务照着账单对账。
func TestCollectionMoneyIsRenderedToCents(t *testing.T) {
	if got := collectionMoney(0.1 + 0.2); got != "0.30" {
		t.Errorf("collectionMoney(0.1+0.2) = %q，期望 \"0.30\"", got)
	}
	if got := collectionMoney(1890.5); got != "1890.50" {
		t.Errorf("collectionMoney(1890.5) = %q，期望两位小数", got)
	}
}

// ---------------------------------------------------------------- 端口形状

// TestCollectionJobPortSurfacesAreNarrow 五条依赖的方法集合逐字反射比。
//
// 这一条是"催收不许改写账单"这句话唯一的硬保证：任务能拿到什么接口，
// 它就能做什么事。今天整条腿上只需要一次批量读、一次身份读、一次外发、
// 一次投递、一次开关判断 —— 多出来的任何一格（尤其 bills.UpdateStatus）
// 都会让"催收把应收标成已付"这种事从"写不出来"变成"评审时要盯着看"。
func TestCollectionJobPortSurfacesAreNarrow(t *testing.T) {
	for _, tc := range []struct {
		name string
		port any
		want []string
	}{
		{"bills", (*collectionBillScanner)(nil), []string{"ScanOverdue"}},
		{"opportunities", (*collectionOpportunityReader)(nil), []string{"GetByID"}},
		{"reach", (*collectionReachSender)(nil), []string{"ReachByCustomer"}},
		{"tasks", (*collectionTaskSubmitter)(nil), []string{"Submit"}},
		{"stages", (*collectionStageGate)(nil), []string{"StageActive"}},
	} {
		st := reflect.TypeOf(tc.port).Elem()
		names := make([]string, 0, st.NumMethod())
		for i := 0; i < st.NumMethod(); i++ {
			names = append(names, st.Method(i).Name)
		}
		if len(names) != len(tc.want) || names[0] != tc.want[0] {
			t.Errorf("%s 的方法集是 %v，期望 %v", tc.name, names, tc.want)
		}
	}
}

// TestCollectionJobNilSafety 缺件时 Available 为假、RunOnce 出声而不是 panic
// （装配半边的形状与 bill/payment 两条腿同判据）。
func TestCollectionJobNilSafety(t *testing.T) {
	job, _, _, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	job.bills = nil
	if job.Available() {
		t.Error("账单读口为 nil 还报 Available=true")
	}
	if _, err := job.RunOnce(context.Background()); err == nil {
		t.Error("半装配的一轮竟然静默成功了")
	}
	if res := job.LastReport(); res == nil || res.DependencyError == "" {
		t.Errorf("半装配的一轮没在报告里点名缺哪几条依赖：%+v（error 只活一轮，报告才是端点读得到的那一份）", res)
	}

	// 五把依赖各自都要能被单独摘掉测到。
	//
	// 为什么不能只测 bills 那一格：`Available()` 是五个 `!= nil` 的合取，摘掉任意一项的
	// 注码（少写一个条件、或者把 `&&` 写成 `||`）在只测 bills 的形状下**全部存活** ——
	// 而合取式少一项正是这条判据最自然的坏法：那时"待办服务没接"这种半装配会被
	// 报成可用、协程照常起来，每轮却在升级那一格 panic 或静默少投。
	for _, tc := range []struct {
		name string
		drop func(*CollectionJob)
	}{
		{"bills", func(j *CollectionJob) { j.bills = nil }},
		{"opps", func(j *CollectionJob) { j.opps = nil }},
		{"reach", func(j *CollectionJob) { j.reach = nil }},
		{"tasks", func(j *CollectionJob) { j.tasks = nil }},
		{"stages", func(j *CollectionJob) { j.stages = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, _, _, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
			if !j.Available() {
				t.Fatal("前置不成立：五把依赖接齐时 Available 就该为真")
			}
			tc.drop(j)
			if j.Available() {
				t.Errorf("只摘掉 %s 还报 Available=true ⇒ 合取式少了一项", tc.name)
			}
		})
	}

	var zero *CollectionJob
	if zero.Available() {
		t.Error("nil 任务不该报可用")
	}
	if _, err := zero.RunOnce(context.Background()); err == nil {
		t.Error("nil 任务的一轮必须报错")
	}
}

// TestCollectionJobUsesTheDocumentedSubjectType subject_type 的字面值与
// model 上那份注释（(collection_case, cc_…)）同源，写错的那格在待办中心会自成一路。
func TestCollectionJobUsesTheDocumentedSubjectType(t *testing.T) {
	if HumanTaskSubjectCollectionCase != "collection_case" {
		t.Errorf("HumanTaskSubjectCollectionCase = %q（internal/model/human_task.go 的 subject 示例用的是 collection_case）",
			HumanTaskSubjectCollectionCase)
	}
}

// ---------------------------------------------------------------- 旋钮面

// TestCollectionEnvKnobsFallBackRatherThanTrustTheInput 三格 env 的坏值都必须回落到默认。
//
// 判据不是"参数可配"而是"参数可配**而不会配坏**"：
//   - 旗子认不得的档位回 off（fail-closed：一个拼错的 `enforec` 不该让客户收到催款信）；
//   - batch 越界回默认而不是被夹到边界：一轮 500 张的意义就是"别把出站队列打满"，
//     把它夹成 200 仍然是同一次事故，只是小一点；
//   - 间隔认不得的时长回默认：`0s` 若被读成"立刻再来一轮"，同一张单会被两轮各领一次。
//
// 这些数一旦能在启动参数里悄悄改掉，"这一轮为什么只催了 3 条"就又多了一个
// 只有当场才知道答案的来源 —— 所以回落必须回**默认**，而默认必须是文档里那两个数。
func TestCollectionEnvKnobsFallBackRatherThanTrustTheInput(t *testing.T) {
	// 三格都显式清空：不关宿主机环境的话，"未配置 ⇒ 默认值"这一腿测的是别人的部署。
	t.Setenv(CollectionJobFlagEnv, "")
	t.Setenv(CollectionJobBatchEnv, "")
	t.Setenv(CollectionJobIntervalEnv, "")
	off := NewCollectionJob(nil, nil, nil, nil, nil)
	if off.Mode() != RecoveryWorkerModeOff {
		t.Errorf("未配置 ⇒ mode=%s，期望 off（默认必须是「不打扰」）", off.Mode())
	}
	if off.Batch() != collectionJobDefaultBatch || off.Interval() != collectionJobDefaultInterval {
		t.Errorf("未配置 ⇒ batch=%d interval=%v，期望默认 %d / %v",
			off.Batch(), off.Interval(), collectionJobDefaultBatch, collectionJobDefaultInterval)
	}

	for _, tc := range []struct {
		name  string
		flag  string
		batch string
		intvl string
		want  RecoveryWorkerMode
		b     int
		d     time.Duration
	}{
		{"旗子拼错", "enforec", "", "", RecoveryWorkerModeOff, collectionJobDefaultBatch, collectionJobDefaultInterval},
		{"上限之上", "shadow", "500", "", RecoveryWorkerModeShadow, collectionJobDefaultBatch, collectionJobDefaultInterval},
		{"非整数", "enforce", "many", "6h", RecoveryWorkerModeEnforce, collectionJobDefaultBatch, 6 * time.Hour},
		{"零与负", "enforce", "0", "0s", RecoveryWorkerModeEnforce, collectionJobDefaultBatch, collectionJobDefaultInterval},
		{"合法区间内", "shadow", "5", "90m", RecoveryWorkerModeShadow, 5, 90 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(CollectionJobFlagEnv, tc.flag)
			t.Setenv(CollectionJobBatchEnv, tc.batch)
			t.Setenv(CollectionJobIntervalEnv, tc.intvl)
			j := NewCollectionJob(nil, nil, nil, nil, nil)
			if j.Mode() != tc.want {
				t.Errorf("mode=%s，期望 %s", j.Mode(), tc.want)
			}
			if j.Batch() != tc.b {
				t.Errorf("batch=%d，期望 %d", j.Batch(), tc.b)
			}
			if j.Interval() != tc.d {
				t.Errorf("interval=%v，期望 %v", j.Interval(), tc.d)
			}
		})
	}
}

// TestCollectionDocumentedContractSurfaceIsExact 运维契约上的那批字面值，一格一个。
//
// 为什么这一格全是字面量、不许借用常量：仓里其余用例都写成
// `got != CollectionReminderWindow` 这种**拿常量算**的形状（那是对的 —— 用例要跟着口径走），
// 但它换来的后果是"把口径改掉"这件事在整张测试面上完全隐形：改 7 天为 0，
// 所有比较同时跟着变，全绿。上一格锁的是"坏值回落到默认"，它同样只说"回落到那个常量"。
// 于是"这条腿对客户到底多久催一次"这句话在代码里就没有任何独立锚点。
//
// 这一格是那个锚点：这些数与字符串全部出现在卡面口径/运维文档里
// （旗子名是运维敲的那行、两把键前缀会在 Redis 面板里被检索、payload 前缀是待办上那个链接），
// 改掉它们中的任何一个都不是"调参"而是改契约，必须在这一格红一次、逼人来对账。
func TestCollectionDocumentedContractSurfaceIsExact(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  any
		want any
	}{
		{"主开关 env 名", CollectionJobFlagEnv, "FF_LTC_COLLECTION_JOB"},
		{"封顶 env 名", CollectionJobBatchEnv, "LTC_COLLECTION_JOB_BATCH"},
		{"间隔 env 名", CollectionJobIntervalEnv, "LTC_COLLECTION_JOB_INTERVAL"},
		{"默认单轮封顶", collectionJobDefaultBatch, 20},
		{"单轮封顶上限", collectionJobMaxBatch, 200},
		{"默认轮询间隔", collectionJobDefaultInterval, 6 * time.Hour},
		{"轮询间隔下限", collectionJobMinInterval, 30 * time.Minute},
		{"宽限期（天）", CollectionGraceDays, 3},
		{"升级线（天）", CollectionEscalateAfterDays, 14},
		{"同一张单两次提醒的最小间隔", CollectionReminderWindow, 7 * 24 * time.Hour},
		{"同一张单两次升级的最小间隔", CollectionEscalateWindow, 30 * 24 * time.Hour},
		{"升级待办的处理截止", CollectionEscalateResponseWindow, 3 * 24 * time.Hour},
		{"提醒键前缀", collectionRemindKeyPrefix, "mtk:collection:remind:"},
		{"升级键前缀", collectionEscalateKeyPrefix, "mtk:collection:escalate:"},
		{"待办跳转前缀", collectionBillPayloadRefPrefix, "/api/bill/"},
		{"对外露出的日期形状", collectionDateLayout, "2006-01-02"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v，期望 %v（这是写进运维口径的那一格，改它要连文档一起改）", tc.name, tc.got, tc.want)
		}
	}

	// 两把键前缀必须**互不为前缀**：一张越过升级线的单会同时落在两把窗里，
	// 共用一把前缀 ⇒ "催过"与"升过"在同一个键上互相覆盖，先占哪个全看代码顺序。
	if strings.HasPrefix(collectionRemindKeyPrefix, collectionEscalateKeyPrefix) ||
		strings.HasPrefix(collectionEscalateKeyPrefix, collectionRemindKeyPrefix) {
		t.Errorf("两把锁前缀 %q / %q 有包含关系 ⇒ 同一张单的两把窗会互相覆盖",
			collectionRemindKeyPrefix, collectionEscalateKeyPrefix)
	}
	// 旗子三档的字面值是运维敲进配置的东西，与 recovery 那一族同源；
	// 这里只锁"这一族的三档名"，避免有一天 collection 悄悄长出一个第四档。
	for _, tc := range []struct{ got, want string }{
		{string(RecoveryWorkerModeOff), "off"},
		{string(RecoveryWorkerModeShadow), "shadow"},
		{string(RecoveryWorkerModeEnforce), "enforce"},
	} {
		if tc.got != tc.want {
			t.Errorf("档位字面值 = %q，期望 %q（%s 的取值集合是运维文档的一部分）",
				tc.got, tc.want, CollectionJobFlagEnv)
		}
	}
}

// TestCollectionStartRaisesTheIntervalFloor 间隔低于一轮的执行时间 ⇒ 抬到下限，而不是照跑。
//
// 下限是从"一轮多长"推出来的，不是从"多快算快"推出来的：两轮之间没有间隔时，
// 同一张单会被两轮各领一次锁（锁拦得住外发，但报告会开始大量出现 reminders_held，
// 于是"这个客户刚被催过"与"本进程自己在跟自己抢"在同一个读数里分不开）。
// 这一格盯的注码是 `if j.interval < collectionJobMinInterval` 整支被摘掉：
// 摘掉之后 `LTC_COLLECTION_JOB_INTERVAL=1s` 会照收，而 Start 里的告警也不会印。
func TestCollectionStartRaisesTheIntervalFloor(t *testing.T) {
	job, _, _, _, _, _ := newTestCollectionJob(RecoveryWorkerModeEnforce)
	job.interval = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job.Start(ctx)
	defer job.Stop(context.Background())

	if got := job.Interval(); got != collectionJobMinInterval {
		t.Errorf("Start 之后间隔=%v，期望被抬到下限 %v（装配期解析的值不在此处再用）",
			got, collectionJobMinInterval)
	}
	if !job.Running() {
		t.Error("enforce 档 + 依赖齐 ⇒ Running 必须为真，否则快照那格读数没对象")
	}
}
