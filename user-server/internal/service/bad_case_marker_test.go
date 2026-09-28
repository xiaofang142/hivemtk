// bad_case_marker_test.go T-P8-03 / AC①：编排器上的 Bad Case 留痕接缝。
//
// 这条接缝是"标记发生在对话热路径上"这件事唯一的生产入口，
// 它要证明的是**它不改主链路**：门槛没过不启协程、底座没挂零动作、
// 标记器炸了不影响回答、请求 ctx 随响应取消后留痕仍要写完。
//
// 异步面的断言口径：留痕在协程里发，所以一律"等到有 / 等够了再断没有"，
// 计数走 channel 的 len（并发安全），不读共享切片。
package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

// newBadCaseOrchestrator 造一个只带配置与标记器的编排器：
// engine / 各仓储都留 nil —— 留痕这条旁路一次都不碰它们，
// 真碰了会当场 nil 指针炸在这里，那正是本组用例要抓的形状。
func newBadCaseOrchestrator(mark BadCaseMarkerFn) *SmartCSOrchestrator {
	o := NewSmartCSOrchestrator(nil, &OrchestratorConfig{
		ConfidenceThreshold: 0.7, EnableAutoReply: true, MaxAIConsecutive: 10,
	}, nil)
	o.SetBadCaseMarker(mark)
	return o
}

// badCaseMarkRecorder 收下每次留痕调用。缓冲给足：
// 无缓冲 channel 会让"没人收"变成"卡住"，用例失败会表现为超时而不是断言。
type badCaseMarkRecorder struct{ calls chan BadCaseMarkInput }

func recordMarker() *badCaseMarkRecorder {
	return &badCaseMarkRecorder{calls: make(chan BadCaseMarkInput, 32)}
}

func (r *badCaseMarkRecorder) fn() BadCaseMarkerFn {
	return func(_ context.Context, in BadCaseMarkInput) {
		select {
		case r.calls <- in:
		default:
		}
	}
}

// next 等一条留痕；ok=false 表示窗口内没有留痕交出来。
func (r *badCaseMarkRecorder) next(t *testing.T, d time.Duration) (BadCaseMarkInput, bool) {
	t.Helper()
	select {
	case in := <-r.calls:
		return in, true
	case <-time.After(d):
		return BadCaseMarkInput{}, false
	}
}

// assertNoMark 给协程一个跑完的窗口，再断"确实一条都没交"。
func (r *badCaseMarkRecorder) assertNoMark(t *testing.T) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	if n := len(r.calls); n != 0 {
		var samples []BadCaseMarkInput
		for i := 0; i < n; i++ {
			samples = append(samples, <-r.calls)
		}
		t.Errorf("不该留痕却交了 %d 条：%+v", n, samples)
	}
}

// oneMark 断"正好交了一条"并把它交回来（第二条也在这里判掉：
// 同一轮重复留痕会让队列里出现两条同现场的样本）。
func (r *badCaseMarkRecorder) oneMark(t *testing.T) BadCaseMarkInput {
	t.Helper()
	in, ok := r.next(t, 2*time.Second)
	if !ok {
		t.Fatal("该留痕的一轮没有交出去")
	}
	r.assertNoMark(t)
	return in
}

func lowConfResp(reply string, chunks int) *SalesResponse {
	resp := &SalesResponse{Reply: reply, RAGChunks: make([]RAGChunk, chunks)}
	for i := range resp.RAGChunks {
		resp.RAGChunks[i] = RAGChunk{Content: "片段"}
	}
	return resp
}

func TestBadCaseMarkBadCase_NilMarkerIsNoop(t *testing.T) {
	o := newBadCaseOrchestrator(nil)
	// 不挂标记器时连门槛都不该过（更不该起协程）；这里只要不炸、不卡就是结论。
	o.markBadCase(context.Background(), "sess_1", lowConfResp("答", 1),
		&IncomingContext{MessageID: "m"}, nil, 0.1, 0.7)
	// 传 nil resp / nil in 同样不该炸（留痕是旁路，任何形状都不能把回答带崩）
	o.markBadCase(context.Background(), "sess_1", nil,
		&IncomingContext{MessageID: "m"}, nil, 0.1, 0.7)
	o.markBadCase(context.Background(), "sess_1", lowConfResp("答", 1), nil, nil, 0.1, 0.7)
}

func TestBadCaseMarkBadCase_Gates(t *testing.T) {
	in := &IncomingContext{MessageID: "msg_gate", Content: "问句"}
	for _, c := range []struct {
		name     string
		resp     *SalesResponse
		in       *IncomingContext
		conf     float64
		thr      float64
		wantMark bool
	}{
		{"低于阈值 → 留痕", lowConfResp("答", 2), in, 0.30, 0.7, true},
		{"高于阈值且有命中 → 不留痕", lowConfResp("答", 2), in, 0.95, 0.7, false},
		{"正好等于阈值 → 不留痕（判据是 <）", lowConfResp("答", 2), in, 0.7, 0.7, false},
		{"零命中但高置信 → 留痕", lowConfResp("答", 0), in, 0.95, 0.7, true},
		{"空回答 → 不留痕（评测集里是一条无法判定的样本）", lowConfResp("   ", 2), in, 0.3, 0.7, false},
		{"nil resp → 不留痕", nil, in, 0.3, 0.7, false},
		{"nil in → 不留痕", lowConfResp("答", 2), nil, 0.3, 0.7, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := recordMarker()
			o := newBadCaseOrchestrator(rec.fn())
			o.markBadCase(context.Background(), "sess_gate", c.resp, c.in, nil, c.conf, c.thr)
			if !c.wantMark {
				rec.assertNoMark(t)
				return
			}
			rec.oneMark(t)
		})
	}
}

// 交出去的现场要完整：坏例队列里判案的人只看得到这几列，
// 少一列就得回会话表再捞一次 —— 而消息表允许被后续业务改写。
func TestBadCaseMarkBadCase_CarriesTheFacts(t *testing.T) {
	rec := recordMarker()
	o := newBadCaseOrchestrator(rec.fn())
	resp := lowConfResp("这款大概三千左右", 3)
	resp.Intent = &dto.RecognizeResult{IntentType: "price_inquiry", Confidence: 0.4}
	dec := &dto.ConfidenceDecision{
		SignalID: "sig_facts", DecisionBand: "auto", VetoTriggered: "urgent_keyword",
	}
	o.markBadCase(context.Background(), "sess_facts", resp,
		&IncomingContext{MessageID: "msg_facts", Content: "这个多少钱"}, dec, 0.35, 0.7)

	c := rec.oneMark(t)
	if c.SessionID != "sess_facts" || c.MessageID != "msg_facts" {
		t.Errorf("指回现场的列不对：%q/%q", c.SessionID, c.MessageID)
	}
	if c.QueryText != "这个多少钱" || c.AnswerText != resp.Reply {
		t.Errorf("问答抄本不对：%q/%q", c.QueryText, c.AnswerText)
	}
	if c.IntentType != "price_inquiry" {
		t.Errorf("intent 没带上：%q", c.IntentType)
	}
	if c.RetrievedCount != 3 {
		t.Errorf("检回数应为 RAGChunks 长度 3，实际 %d", c.RetrievedCount)
	}
	if c.Confidence != 0.35 || c.Threshold != 0.7 {
		t.Errorf("判据两个数不对：%.4f/%.4f", c.Confidence, c.Threshold)
	}
	// AC① 的正身：signal_id 从置信度决策里带出来，坏例与信号行同一条链
	if c.SignalID != "sig_facts" {
		t.Errorf("signal_id 没接上：%q（G4 割裂就是断在这一列）", c.SignalID)
	}
	if !strings.Contains(c.MarkReason, "auto") || !strings.Contains(c.MarkReason, "urgent_keyword") {
		t.Errorf("理由里要带决策档与否决规则名：%q", c.MarkReason)
	}
}

// 没有决策（走启发式）时不许凭空造一个 signal_id / 决策档。
func TestBadCaseMarkBadCase_NoDecisionNoFabricatedSignal(t *testing.T) {
	rec := recordMarker()
	o := newBadCaseOrchestrator(rec.fn())
	o.markBadCase(context.Background(), "sess_nodec", lowConfResp("答", 2),
		&IncomingContext{MessageID: "msg_nodec", Content: "问"}, nil, 0.35, 0.7)
	c := rec.oneMark(t)
	if c.SignalID != "" {
		t.Errorf("无决策时 signal_id 应为空，实际 %q", c.SignalID)
	}
	if strings.Contains(c.MarkReason, "决策档") {
		t.Errorf("无决策时不许编造决策档：%q", c.MarkReason)
	}
}

// 否决为空时只留档位，不许拖一句空的"否决规则 "。
func TestBadCaseMarkBadCase_EmptyVetoLeavesNoTrailingFragment(t *testing.T) {
	rec := recordMarker()
	o := newBadCaseOrchestrator(rec.fn())
	o.markBadCase(context.Background(), "sess_noveto", lowConfResp("答", 2),
		&IncomingContext{MessageID: "msg_noveto", Content: "问"},
		&dto.ConfidenceDecision{SignalID: "s3", DecisionBand: "review"}, 0.35, 0.7)
	c := rec.oneMark(t)
	if !strings.Contains(c.MarkReason, "review") {
		t.Errorf("决策档没写进理由：%q", c.MarkReason)
	}
	if strings.Contains(c.MarkReason, "否决规则") {
		t.Errorf("无否决时理由里出现了否决规则字样：%q", c.MarkReason)
	}
}

// 抬权后的置信度才是这一轮生效的判据：卡片把 confidence 抬到阈值之上后，
// 留痕不该把它记成低质（那正是"抬权"这件事的语义）。
func TestBadCaseMarkBadCase_UsesEffectiveConfidence(t *testing.T) {
	rec := recordMarker()
	o := newBadCaseOrchestrator(rec.fn())
	o.markBadCase(context.Background(), "sess_eff", lowConfResp("带卡片的答案", 1),
		&IncomingContext{MessageID: "msg_eff"}, nil, 0.7, 0.7)
	rec.assertNoMark(t)
}

// 留痕必须挂在新 ctx 上：传进来的 ctx 随请求返回就取消了，
// 沿用它会写出"日志说有坏例、库里一行都没有"的半程。
func TestBadCaseMarkBadCase_UsesFreshContextNotRequestContext(t *testing.T) {
	var hadDeadline atomic.Bool
	var ctxHadErrAtEntry atomic.Bool
	var blockedSawCancel atomic.Bool
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once

	o := newBadCaseOrchestrator(func(ctx context.Context, in BadCaseMarkInput) {
		_, ok := ctx.Deadline()
		hadDeadline.Store(ok)
		ctxHadErrAtEntry.Store(ctx.Err() != nil)
		once.Do(func() { close(entered) })
		<-release // 故意拖过请求 ctx 的取消时刻
		blockedSawCancel.Store(ctx.Err() != nil)
		close(finished)
	})
	reqCtx, cancel := context.WithCancel(context.Background())
	o.markBadCase(reqCtx, "sess_ctx", lowConfResp("答", 1),
		&IncomingContext{MessageID: "msg_ctx"}, nil, 0.2, 0.7)
	cancel() // 响应已返回

	<-entered
	if !hadDeadline.Load() {
		t.Error("留痕的 ctx 没带超时：库挂住时会拖出一条永不结束的协程")
	}
	if ctxHadErrAtEntry.Load() {
		t.Error("留痕的 ctx 一进来就带着取消信号：它继承了请求 ctx")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("留痕协程没能收尾")
	}
	if blockedSawCancel.Load() {
		t.Error("留痕协程用的是请求 ctx：请求一结束这次写入就被取消掉")
	}
}

// 标记器里 panic 不许带走会话主链路（它是回答的旁路）。
//
// recover 在留痕协程自己身上，所以这里等的不是"主线程没炸"（那由 Go 的
// 默认行为决定），而是"这条旁路没把进程带下去"：进程还活着、后续用例还能跑。
func TestBadCaseMarkBadCase_MarkerPanicIsRecovered(t *testing.T) {
	panicked := make(chan struct{})
	o := newBadCaseOrchestrator(func(ctx context.Context, in BadCaseMarkInput) {
		close(panicked)
		panic("留痕里炸了")
	})
	o.markBadCase(context.Background(), "sess_panic", lowConfResp("答", 1),
		&IncomingContext{MessageID: "msg_panic"}, nil, 0.2, 0.7)
	select {
	case <-panicked:
	case <-time.After(3 * time.Second):
		t.Fatal("标记器没被调用（协程没起来？）")
	}
	// panic 之后进程仍活着：再标一次还能被调用，说明 recover 兜住了这一条而不是全局停摆
	second := make(chan struct{})
	o2 := newBadCaseOrchestrator(func(ctx context.Context, in BadCaseMarkInput) { close(second) })
	o2.markBadCase(context.Background(), "sess_panic2", lowConfResp("答", 1),
		&IncomingContext{MessageID: "msg_panic2"}, nil, 0.2, 0.7)
	select {
	case <-second:
	case <-time.After(3 * time.Second):
		t.Fatal("上一次 panic 之后留痕接缝停了")
	}
	// 等 recover 那一句打完日志，别把它的输出挤到下一个用例头上
	time.Sleep(150 * time.Millisecond)
}

// BadCaseMarker 的边界：底座没装配时回 nil（不挂），装配了就吞掉 Mark 的错误只留日志。
func TestBadCaseMarker_NilWhenUnavailableAndSwallowsMarkError(t *testing.T) {
	if fn := BadCaseMarker(nil); fn != nil {
		t.Error("svc 为 nil 时应回 nil（不挂）")
	}
	if fn := BadCaseMarker(NewBadCaseService(nil)); fn != nil {
		t.Error("底座不可用时应回 nil —— 装配点那句 Warn 才有对应的事实")
	}
	if fn := BadCaseMarker(&BadCaseService{}); fn != nil {
		t.Error("空壳服务应回 nil")
	}

	// 挂上之后调用它：Mark 里的错误必须被吞（签名回 void），且不许 panic。
	// 用"必被拒"的入参（自动标记缺 message_id）走 Mark 的错误分支。
	svc, db := setupBadCaseSvc(t)
	marker := BadCaseMarker(svc)
	if marker == nil {
		t.Fatal("可用底座应挂上标记器")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker(ctx, BadCaseMarkInput{SessionID: "s_mk", Confidence: 0.1, RetrievedCount: 1})
	var n int64
	if err := db.Model(&model.BadCase{}).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 0 {
		t.Errorf("被拒的标记仍落了 %d 行", n)
	}
	// 同一入口走成功分支：补齐 message_id 就该落一行
	marker(ctx, BadCaseMarkInput{SessionID: "s_mk", MessageID: "m_ok", Confidence: 0.1, RetrievedCount: 1})
	if err := db.Model(&model.BadCase{}).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 1 {
		t.Errorf("补齐现场后应落 1 行，实际 %d（标记器把成功也吞掉了？）", n)
	}
}

// ShouldMark 与编排器用的是同一份门槛：判据只有一份。
// 这条把两处口径钉成一条 —— 编排器 pre-gate 掉的，服务层也必须判成不留痕。
func TestBadCaseMarkBadCase_PregateMatchesServiceGate(t *testing.T) {
	for _, in := range []BadCaseMarkInput{
		{Confidence: 0.1, Threshold: 0.7, RetrievedCount: 3},
		{Confidence: 0.95, Threshold: 0.7, RetrievedCount: 0},
		{Confidence: 0.95, Threshold: 0.7, RetrievedCount: 3},
		{Confidence: 0.5, Threshold: 0, RetrievedCount: 2},
		{Confidence: 0.5, Threshold: 0, RetrievedCount: 0},
		{Confidence: 0.9, Threshold: 0, RetrievedCount: 1},
	} {
		_, _, want := ShouldMark(in)
		rec := recordMarker()
		o := newBadCaseOrchestrator(rec.fn())
		resp := lowConfResp("答", in.RetrievedCount)
		o.markBadCase(context.Background(), "s_pre", resp,
			&IncomingContext{MessageID: "m_pre"}, nil, in.Confidence, in.Threshold)
		if want {
			rec.oneMark(t)
		} else {
			rec.assertNoMark(t)
		}
	}
}

// 接缝的形状本身：挂 nil 就等于不挂（与 humanTaskProducer 同一取舍）。
func TestBadCaseSetAndReadMarker(t *testing.T) {
	o := newBadCaseOrchestrator(nil)
	if o.badCaseMark != nil {
		t.Error("挂 nil 后字段应为 nil")
	}
	o.SetBadCaseMarker(recordMarker().fn())
	if o.badCaseMark == nil {
		t.Fatal("注入后应能取到标记器")
	}
	o.SetBadCaseMarker(nil)
	if o.badCaseMark != nil {
		t.Error("SetBadCaseMarker(nil) 应撤掉")
	}
	// 撤掉之后再标一次：不该炸（这是"本卡之前逐字一致"那一句的机器证明）
	o.markBadCase(context.Background(), "s_off", lowConfResp("答", 1),
		&IncomingContext{MessageID: "m_off"}, nil, 0.1, 0.7)
}

// badCaseIntentOf：nil 与无意图都要回空串而不是 panic。
func TestBadCaseIntentOf(t *testing.T) {
	if got := badCaseIntentOf(nil); got != "" {
		t.Errorf("nil resp 应回空串：%q", got)
	}
	if got := badCaseIntentOf(&SalesResponse{}); got != "" {
		t.Errorf("无意图应回空串：%q", got)
	}
	if got := badCaseIntentOf(&SalesResponse{Intent: &dto.RecognizeResult{IntentType: "complaint"}}); got != "complaint" {
		t.Errorf("意图没读出：%q", got)
	}
}

// 这一轮根本没产出答案的三类轮次不许记成坏例（AC③）。
//
// 零命中那条判据是"检回数 0 就记"，而串行引擎在**检索之前**就因为澄清与转人工
// return 了：不设闸的话，一句 `[系统自动转人工] …` 会以"知识缺口"的身份进队列、
// 再被导出进评测集。所以每条"不该留痕"都配一条只差那一个信号的对照组 ——
// 否则"没留痕"可能来自门槛（置信度够高、检回数非 0）而不是这道闸，测了个空。
func TestBadCaseMarkBadCase_TurnsWithNoAnswerAreNotMarked(t *testing.T) {
	withStep := func(status string) *SalesResponse {
		r := lowConfResp("抱歉, 服务暂时不可用, 请稍后重试。", 0)
		r.Steps = []dto.SalesStepLog{{Step: "6_generate_candidate", Status: status, Error: "llm down"}}
		return r
	}
	transfer := func(chunks int) *SalesResponse {
		r := lowConfResp("[系统自动转人工] 用户明确要求人工", chunks)
		r.TransferredToHuman = true
		return r
	}
	clarify := func() *SalesResponse {
		r := lowConfResp("您是想问发货时间还是到货时间？", 0)
		r.Intent = &dto.RecognizeResult{IntentType: IntentClarify, Confidence: 0.31}
		return r
	}
	known := func() *SalesResponse {
		r := lowConfResp("这款大概三千左右", 0)
		r.Intent = &dto.RecognizeResult{IntentType: "price_inquiry", Confidence: 0.31}
		return r
	}
	for _, c := range []struct {
		name     string
		resp     *SalesResponse
		wantMark bool
	}{
		{"引擎自行转人工 + 零命中 → 不留痕（该事实已有人工任务那一行）", transfer(0), false},
		{"引擎自行转人工 + 有检回 → 同样不留痕（挡的是轮次不是判据）", transfer(3), false},
		{"对照：只差 TransferredToHuman 这一个信号 → 留痕", lowConfResp("这款大概三千左右", 0), true},
		{"追问澄清 → 不留痕（含糊的是问句，不是知识库缺料）", clarify(), false},
		{"对照：意图已识别且零命中 → 留痕（无本之木正是要收的那类）", known(), true},
		{"生成失败（回复是兜底文案）→ 不留痕", withStep("fail"), false},
		{"对照：同一步骤 status=ok → 留痕（闸认的是显式失败，不是步骤缺席）", withStep("ok"), true},
		{"layer1 fastpath（整条没有 6_generate_candidate 步骤）→ 留痕",
			lowConfResp("营业时间 9:00–18:00", 0), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := recordMarker()
			o := newBadCaseOrchestrator(rec.fn())
			// 置信度一律给到阈值之上：此时门槛唯一开火的理由就是零命中，
			// 于是"没留痕"只可能来自这道闸。
			o.markBadCase(context.Background(), "sess_noanswer", c.resp,
				&IncomingContext{MessageID: "msg_" + t.Name(), Content: "问句"}, nil, 0.99, 0.7)
			if !c.wantMark {
				rec.assertNoMark(t)
				return
			}
			in := rec.oneMark(t)
			if in.RetrievedCount != 0 {
				t.Errorf("对照组的检回数应为 0，实际 %d", in.RetrievedCount)
			}
		})
	}
}

// 时间窗口口径：与订单草稿那条同源（同一件事不该有两个时间口径）。
func TestBadCaseMarkTimeoutSharesOneSource(t *testing.T) {
	if badCaseMarkTimeout <= 0 {
		t.Errorf("留痕超时必须为正，实际 %v", badCaseMarkTimeout)
	}
	if badCaseMarkTimeout != orderDraftProduceTimeout {
		t.Errorf("留痕超时 %v 与订单草稿 %v 不同源", badCaseMarkTimeout, orderDraftProduceTimeout)
	}
}

// 来源由服务层那一份门槛判：编排器只交现场，不自报来源 ——
// 否则同一件事会有两处口径，零命中那批会被读成低质（两拨责任人不同）。
func TestBadCaseMarkBadCase_SourceComesFromServiceGate(t *testing.T) {
	rec := recordMarker()
	o := newBadCaseOrchestrator(rec.fn())
	o.markBadCase(context.Background(), "s_src", lowConfResp("答", 0),
		&IncomingContext{MessageID: "m_src"}, nil, 0.99, 0.7)
	in := rec.oneMark(t)
	if in.RetrievedCount != 0 {
		t.Errorf("检回数没交 0：%d", in.RetrievedCount)
	}
	source, _, ok := ShouldMark(in)
	if !ok || source != model.BadCaseSourceZeroHit {
		t.Errorf("同一份现场在服务层判成 %q/%v，期望 zero_hit", source, ok)
	}
}
