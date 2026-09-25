// bad_case_test.go T-P8-03：Bad Case 服务层（判据、状态机、导出）。
//
// 分两半：
//   - 纯逻辑（ShouldMark / sentinel 翻译 / 全局登记处）不需要库，跑得快也判得准；
//   - 走库的那一半只测**只有服务层才成立**的四件事：
//     1. 自动标记的幂等与"缺 message_id 就不落库"（仓储只会挡同键，挡不住空键互吞）；
//     2. 状态机三个 sentinel（NotFound / Transition / NothingToExport）分别从哪条路来；
//     3. 导出产物刻意**没有** expected_* 列（本卡给不出标准答案，留空字段会被下游当成已有）；
//     4. 已判率这个北极星的分母（四态总数，不是"记了多少行"）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupBadCaseSvc(t *testing.T) (*BadCaseService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.BadCase{})
	return NewBadCaseService(repository.NewBadCaseRepositoryWithDB(db)), db
}

// newBadCaseSvcRow 直接经仓储造一行处于指定态的坏例：服务层的写口是被测对象，
// 拿它给自己铺前置条件会让"Label 把行写坏了"这类缺陷读成"前置没造出来"。
func newBadCaseSvcRow(t *testing.T, db *gorm.DB, id, source, status string) *model.BadCase {
	t.Helper()
	dedup := id
	if source != model.BadCaseSourceManual {
		k, ok := model.BadCaseDedupKey(source, "sess_"+id, "msg_"+id)
		if !ok {
			t.Fatalf("夹具 dedup 键算不出：%s", id)
		}
		dedup = k
	}
	at := time.Now().Add(-time.Duration(len(id)) * time.Minute)
	row := &model.BadCase{
		ID: id, Source: source, Status: status,
		SessionID: "sess_" + id, MessageID: "msg_" + id,
		SignalID: "sig_" + id, IntentType: "price_inquiry",
		QueryText: "问句 " + id, AnswerText: "答句 " + id,
		Confidence: 0.4, Threshold: 0.7, MarkReason: "夹具",
		DedupKey: dedup, CreatedAt: at, UpdatedAt: at,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("夹具造行 %s 失败：%v", id, err)
	}
	return row
}

// ---------- 纯逻辑：标记判据 ----------

func TestBadCaseShouldMark(t *testing.T) {
	const fb = 0.7 // 编排器默认档；下面 case 里 threshold<=0 时用它
	if got := badCaseFallbackThreshold(); got != fb {
		t.Fatalf("兜底阈值应为编排器默认档 %.2f，实际 %.4f", fb, got)
	}

	for _, c := range []struct {
		name       string
		in         BadCaseMarkInput
		wantSource string
		wantOK     bool
	}{
		{"零命中优先于置信度（两者都低）", BadCaseMarkInput{Confidence: 0.1, Threshold: 0.7, RetrievedCount: 0}, model.BadCaseSourceZeroHit, true},
		{"零命中但置信度很高也要记", BadCaseMarkInput{Confidence: 0.99, Threshold: 0.7, RetrievedCount: 0}, model.BadCaseSourceZeroHit, true},
		{"负数检回数按零命中算", BadCaseMarkInput{Confidence: 0.9, Threshold: 0.7, RetrievedCount: -1}, model.BadCaseSourceZeroHit, true},
		{"低于阈值", BadCaseMarkInput{Confidence: 0.69, Threshold: 0.7, RetrievedCount: 3}, model.BadCaseSourceLowConfidence, true},
		{"正好等于阈值不记（判据是 <，不是 <=）", BadCaseMarkInput{Confidence: 0.7, Threshold: 0.7, RetrievedCount: 3}, "", false},
		{"高于阈值不记", BadCaseMarkInput{Confidence: 0.95, Threshold: 0.7, RetrievedCount: 5}, "", false},
		{"阈值缺位时按默认档：低于默认档要记", BadCaseMarkInput{Confidence: 0.5, Threshold: 0, RetrievedCount: 2}, model.BadCaseSourceLowConfidence, true},
		{"阈值负数同样走默认档", BadCaseMarkInput{Confidence: 0.5, Threshold: -1, RetrievedCount: 2}, model.BadCaseSourceLowConfidence, true},
		{"阈值缺位但高于默认档不记", BadCaseMarkInput{Confidence: 0.9, Threshold: 0, RetrievedCount: 2}, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			source, reason, ok := ShouldMark(c.in)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want=%v（reason=%q）", ok, c.wantOK, reason)
			}
			if source != c.wantSource {
				t.Errorf("source=%q want=%q", source, c.wantSource)
			}
			if !ok {
				if reason != "" {
					t.Errorf("不记时不该给理由：%q", reason)
				}
				return
			}
			if strings.TrimSpace(reason) == "" {
				t.Error("要记时必须给一句理由（它是 mark_reason，事后判断据的唯一出处）")
			}
			// 理由里要带上那两个数：阈值本来就会随客户等级漂，
			// 不带数字的 mark_reason 事后无法复算"这条到底算不算低质"。
			for _, frag := range []string{"0."} {
				if !strings.Contains(reason, frag) {
					t.Errorf("理由 %q 里没有读数 %q", reason, frag)
				}
			}
		})
	}
}

// 阈值读成 0 会让 "confidence < 0" 永不成立 ⇒ 整条自动标记静默停摆，
// 而它是本卡唯一的生产者。这条挡住"把兜底改成 0"的那一刀。
func TestBadCaseShouldMark_ZeroThresholdNeverDisablesMarking(t *testing.T) {
	if _, _, ok := ShouldMark(BadCaseMarkInput{Confidence: 0.0, Threshold: 0, RetrievedCount: 1}); !ok {
		t.Error("阈值缺位 + 置信度 0 竟然不记：说明兜底没生效")
	}
}

// ---------- 纯逻辑：底座不可用 / 全局登记处 ----------

func TestBadCaseService_UnavailableRejectsEveryEntry(t *testing.T) {
	for name, svc := range map[string]*BadCaseService{
		"nil 服务":    nil,
		"nil 仓储":    NewBadCaseService(nil),
		"nil 句柄的仓储": NewBadCaseService(repository.NewBadCaseRepositoryWithDB(nil)),
		"空壳服务":      &BadCaseService{},
	} {
		t.Run(name, func(t *testing.T) {
			if svc.Available() {
				t.Fatal("Available 应为 false")
			}
			ctx := context.Background()
			in := BadCaseMarkInput{SessionID: "s", MessageID: "m", Confidence: 0.1, RetrievedCount: 0}
			// 每个入口都要报错，且错误里要说清"没装配"：
			// 回 (nil, nil) 会让控制器把"没底座"渲染成"一条坏例都没有"。
			if ok, err := svc.Mark(ctx, in); ok || err == nil {
				t.Errorf("Mark：%v/%v", ok, err)
			}
			if row, err := svc.MarkManual(ctx, in, "op-1"); row != nil || err == nil {
				t.Errorf("MarkManual：%v/%v", row, err)
			}
			if row, err := svc.Label(ctx, "bc_1", "op-1", model.BadCaseLabelKBMissing, "依据"); row != nil || err == nil {
				t.Errorf("Label：%v/%v", row, err)
			}
			if row, err := svc.Dismiss(ctx, "bc_1", "op-1", "理由"); row != nil || err == nil {
				t.Errorf("Dismiss：%v/%v", row, err)
			}
			if row, err := svc.Get(ctx, "bc_1"); row != nil || err == nil {
				t.Errorf("Get：%v/%v", row, err)
			}
			if list, total, err := svc.List(ctx, repository.BadCaseListQuery{}); list != nil || total != 0 || err == nil {
				t.Errorf("List：%v/%d/%v", list, total, err)
			}
			if st, err := svc.Stats(ctx); st != nil || err == nil {
				t.Errorf("Stats：%v/%v", st, err)
			}
			if set, err := svc.ExportEvalSet(ctx, nil, 0); set != nil || err == nil {
				t.Errorf("ExportEvalSet：%v/%v", set, err)
			}
			// 标记器在底座缺位时必须回 nil（不挂），而不是挂一个会吞错的空函数：
			// 挂上去就永久沉默，装配日志里那句 Warn 也就没有对应的事实了。
			if fn := BadCaseMarker(svc); fn != nil {
				t.Error("底座不可用却挂上了标记器")
			}
		})
	}
}

func TestBadCaseGlobalServiceRoundTrip(t *testing.T) {
	// 全局单例：取走还原，否则同包后续用例读到的是本用例装的实例。
	defer SetGlobalBadCaseService(GlobalBadCaseService())

	if GlobalBadCaseService() == nil {
		// 允许初始态就是 nil（未装配）；下面只测"装—撤"两个动作。
		t.Log("起始全局实例为 nil（未装配）")
	}
	svc := NewBadCaseService(nil)
	SetGlobalBadCaseService(svc)
	if got := GlobalBadCaseService(); got != svc {
		t.Errorf("全局登记处没拿到同一实例：%p vs %p", got, svc)
	}
	SetGlobalBadCaseService(nil)
	if got := GlobalBadCaseService(); got != nil {
		t.Errorf("传 nil 应撤掉登记，实际 %p", got)
	}
	// 并发读写（装配期与请求期各读一次）不该打架：这里只验形状，-race 由门禁跑。
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = GlobalBadCaseService()
		}()
	}
	wg.Wait()
}

// ---------- 走库：自动标记 ----------

func TestBadCaseService_MarkAuto(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	in := BadCaseMarkInput{
		SessionID: "sess_mark", MessageID: "msg_mark", SignalID: "sig_mark",
		IntentType: "price_inquiry", QueryText: "多少钱", AnswerText: "大概那些",
		Confidence: 0.42, Threshold: 0.7, RetrievedCount: 4,
		MarkReason: "｜动态阈值档 A",
	}
	created, err := svc.Mark(ctx, in)
	if err != nil || !created {
		t.Fatalf("首标应落一行：(%v,%v)", created, err)
	}
	var stored model.BadCase
	if err := db.Where("session_id = ? AND message_id = ?", in.SessionID, in.MessageID).First(&stored).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if stored.Source != model.BadCaseSourceLowConfidence {
		t.Errorf("来源判错：%q", stored.Source)
	}
	if stored.Status != model.BadCaseStatusPending {
		t.Errorf("新行状态应为 pending，实际 %q", stored.Status)
	}
	if stored.DedupKey != "low_confidence|sess_mark|msg_mark" {
		t.Errorf("dedup 键形态不对：%q", stored.DedupKey)
	}
	if stored.Confidence != 0.42 || stored.Threshold != 0.7 {
		t.Errorf("判据数字错位：%.4f/%.4f", stored.Confidence, stored.Threshold)
	}
	// mark_reason 要同时含"判据的话"和"调用方补的现场话"：
	// 只留一句的话，事后要么不知道它为什么被标记，要么不知道当时生效的是哪一档。
	if !strings.Contains(stored.MarkReason, "低于") || !strings.Contains(stored.MarkReason, "动态阈值档 A") {
		t.Errorf("mark_reason 不完整：%q", stored.MarkReason)
	}
	if stored.ID == "" || !strings.HasPrefix(stored.ID, "bc_") {
		t.Errorf("id 生成格式不对：%q", stored.ID)
	}

	// 同一轮再标一次：幂等，(false, nil)，且库里仍是一行。
	created, err = svc.Mark(ctx, in)
	if err != nil || created {
		t.Fatalf("重复标记应返回 (false,nil)：(%v,%v)", created, err)
	}
	var n int64
	if err := db.Model(&model.BadCase{}).Where("dedup_key = ?", stored.DedupKey).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 1 {
		t.Errorf("重复标记后库里 %d 行，期望 1", n)
	}

	// 不需要记的一轮：不落库也不报错
	created, err = svc.Mark(ctx, BadCaseMarkInput{
		SessionID: "sess_mark", MessageID: "msg_high", Confidence: 0.95, Threshold: 0.7, RetrievedCount: 6,
	})
	if err != nil || created {
		t.Errorf("高质轮次不该落库：(%v,%v)", created, err)
	}

	// 阈值缺位：落库那一列必须是兜底档而不是 0。
	// 存 0 的话这条记录事后读起来是"当时阈值是 0、置信度 0.5 却仍被记为坏例"，判据无法复算。
	created, err = svc.Mark(ctx, BadCaseMarkInput{
		SessionID: "sess_mark", MessageID: "msg_fb", Confidence: 0.3, RetrievedCount: 2,
	})
	if err != nil || !created {
		t.Fatalf("阈值缺位也要标：(%v,%v)", created, err)
	}
	var fbRow model.BadCase
	if err := db.Where("message_id = ?", "msg_fb").First(&fbRow).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if fbRow.Threshold != badCaseFallbackThreshold() {
		t.Errorf("阈值列没兜底：%v（期望 %v）", fbRow.Threshold, badCaseFallbackThreshold())
	}

	// 零命中的现场：来源必须是 zero_hit（修复责任在知识库侧，不在算法侧）
	if _, err := svc.Mark(ctx, BadCaseMarkInput{
		SessionID: "sess_mark", MessageID: "msg_zero", Confidence: 0.95, Threshold: 0.7, RetrievedCount: 0,
	}); err != nil {
		t.Fatalf("零命中标记失败：%v", err)
	}
	var zeroRow model.BadCase
	if err := db.Where("message_id = ?", "msg_zero").First(&zeroRow).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if zeroRow.Source != model.BadCaseSourceZeroHit {
		t.Errorf("零命中来源判成 %q", zeroRow.Source)
	}
	if zeroRow.RetrievedCount != 0 {
		t.Errorf("retrieved_count 没抄下来：%d", zeroRow.RetrievedCount)
	}
}

// 空 message_id 的自动标记必须被拒且**不落库**：
// 空键会让所有这类轮次挤进同一条 dedup 记录，第一条之后的真坏例全被"已存在"静默吃掉。
func TestBadCaseService_MarkRequiresMessageID(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	for _, c := range []struct {
		name string
		in   BadCaseMarkInput
	}{
		{"缺 message_id", BadCaseMarkInput{SessionID: "s_1", Confidence: 0.1, RetrievedCount: 1}},
		{"message_id 全空白", BadCaseMarkInput{SessionID: "s_2", MessageID: "   ", Confidence: 0.1, RetrievedCount: 1}},
		{"缺 session_id", BadCaseMarkInput{MessageID: "m_3", Confidence: 0.1, RetrievedCount: 1}},
		{"session_id 全空白", BadCaseMarkInput{SessionID: "  ", MessageID: "m_4", Confidence: 0.1, RetrievedCount: 1}},
	} {
		created, err := svc.Mark(ctx, c.in)
		if created {
			t.Errorf("%s：不该落库却回了 created=true", c.name)
		}
		if !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 %v", c.name, err)
		}
	}
	var n int64
	if err := db.Model(&model.BadCase{}).Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 0 {
		t.Errorf("被拒的标记仍落了 %d 行", n)
	}
}

func TestBadCaseService_MarkManual(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	base := BadCaseMarkInput{SessionID: "sess_mm", MessageID: "msg_mm", QueryText: "问", AnswerText: "答"}
	// 同一现场录两条：人工补录不去重（那是人的判断，不是系统的重复）。
	first, err := svc.MarkManual(ctx, base, "op-1")
	if err != nil || first == nil {
		t.Fatalf("补录失败：%v", err)
	}
	if first.Source != model.BadCaseSourceManual {
		t.Errorf("来源应为 manual，实际 %q", first.Source)
	}
	if first.Status != model.BadCaseStatusPending {
		t.Errorf("补录应是待判态，实际 %q", first.Status)
	}
	if first.DedupKey != first.ID {
		t.Errorf("手动来源的 dedup 键应等于本行 id：%q vs %q", first.DedupKey, first.ID)
	}
	if first.MarkReason != "人工补录" {
		t.Errorf("没给理由时要有默认理由，实际 %q", first.MarkReason)
	}
	second, err := svc.MarkManual(ctx, base, "op-2")
	if err != nil || second == nil {
		t.Fatalf("同现场第二条补录应可录入：%v", err)
	}
	if second.ID == first.ID {
		t.Error("两条补录共用一个 id")
	}
	var n int64
	if err := db.Model(&model.BadCase{}).Where("session_id = ?", "sess_mm").Count(&n).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if n != 2 {
		t.Errorf("同现场应有 2 条补录，实际 %d", n)
	}

	// 补录不过 ShouldMark 的门槛：坐席报的是"答得烂"，与置信度无关。
	highConf := base
	highConf.MessageID = "msg_mm_high"
	highConf.Confidence = 0.99
	highConf.RetrievedCount = 9
	if row, err := svc.MarkManual(ctx, highConf, "op-1"); err != nil || row == nil {
		t.Errorf("高置信度轮次也应允许人工补录：%v", err)
	}

	// 入参校验
	for _, c := range []struct {
		name string
		in   BadCaseMarkInput
		who  string
	}{
		{"空 labeler", base, ""},
		{"空白 labeler", base, "   "},
		{"空 session", BadCaseMarkInput{MessageID: "m", QueryText: "问", AnswerText: "答"}, "op-1"},
		{"缺问句", BadCaseMarkInput{SessionID: "s", QueryText: "", AnswerText: "答"}, "op-1"},
		{"缺答句", BadCaseMarkInput{SessionID: "s", QueryText: "问", AnswerText: "  "}, "op-1"},
	} {
		if row, err := svc.MarkManual(ctx, c.in, c.who); row != nil || !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 %v/%v", c.name, row, err)
		}
	}
	// 给了 reason 要原样留（它是打标人写的，不是系统的话术）
	withReason := base
	withReason.MessageID = "msg_mm_reason"
	withReason.MarkReason = "客户当场指出价格说错了"
	row, err := svc.MarkManual(ctx, withReason, "op-1")
	if err != nil || row == nil {
		t.Fatalf("带理由的补录失败：%v", err)
	}
	if row.MarkReason != "客户当场指出价格说错了" {
		t.Errorf("补录理由被覆盖：%q", row.MarkReason)
	}
}

// ---------- 走库：状态机 ----------

func TestBadCaseService_Label(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	row := newBadCaseSvcRow(t, db, "bc_lb_1", model.BadCaseSourceLowConfidence, model.BadCaseStatusPending)
	got, err := svc.Label(ctx, row.ID, "op-9", model.BadCaseLabelRetrieveMiss, "库里有条但检索没召回")
	if err != nil {
		t.Fatalf("打标失败：%v", err)
	}
	if got.Status != model.BadCaseStatusLabeled {
		t.Errorf("状态没迁：%q", got.Status)
	}
	if got.FixLayer != model.BadCaseFixLayerRetrieval {
		t.Errorf("责任层没派生：%q", got.FixLayer)
	}
	if got.LabelerID != "op-9" || got.Label != model.BadCaseLabelRetrieveMiss {
		t.Errorf("结论没落库：%q/%q", got.LabelerID, got.Label)
	}
	if got.LabeledAt == nil {
		t.Error("labeled_at 没写：它既是导出排序键也是判定时长的分子")
	}
	// 现场列一个字都不许动（打标的人不是改写案发现场的人）
	if got.QueryText != row.QueryText || got.Confidence != row.Confidence || got.Source != row.Source {
		t.Errorf("现场被改写：%q/%v/%q", got.QueryText, got.Confidence, got.Source)
	}

	// 重复打标：行已不在 pending ⇒ Transition（且提示里带当前态）
	if again, err := svc.Label(ctx, row.ID, "op-9", model.BadCaseLabelKBMissing, "再判一次"); again != nil || !errors.Is(err, ErrBadCaseTransition) {
		t.Errorf("重复打标应判 Transition，实际 %v/%v", again, err)
	} else if !strings.Contains(err.Error(), model.BadCaseStatusLabeled) {
		t.Errorf("错误里要带上当前状态，前端据此决定要不要刷新队列：%v", err)
	}

	// 不存在的行 → NotFound（不是含糊的 409）
	if got, err := svc.Label(ctx, "bc_absent_xx", "op-9", model.BadCaseLabelKBMissing, "依据"); got != nil || !errors.Is(err, ErrBadCaseNotFound) {
		t.Errorf("不存在的行应判 NotFound，实际 %v/%v", got, err)
	}

	// 入参校验：三个必须项各挡一种"结论不可用"
	for _, c := range []struct {
		name  string
		who   string
		label string
		note  string
	}{
		{"空 labeler", "", model.BadCaseLabelKBMissing, "依据"},
		{"未知类目", "op-9", "bogus_label", "依据"},
		{"空类目", "op-9", "", "依据"},
		{"空依据", "op-9", model.BadCaseLabelKBMissing, "  "},
	} {
		r := newBadCaseSvcRow(t, db, "bc_lb_bad_"+strings.ReplaceAll(c.name, " ", "_"),
			model.BadCaseSourceManual, model.BadCaseStatusPending)
		if got, err := svc.Label(ctx, r.ID, c.who, c.label, c.note); got != nil || !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 %v/%v", c.name, got, err)
		}
		// 校验必须在写库之前：被拒的这行还得能正常判
		var stored model.BadCase
		if err := db.Where("id = ?", r.ID).First(&stored).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if stored.Status != model.BadCaseStatusPending || stored.Label != "" {
			t.Errorf("%s：被拒的打标仍改写了库里的行（%s/%q）", c.name, stored.Status, stored.Label)
		}
	}

	// note 要落库前 TrimSpace 的结果（评测集会带这一列，首尾空白会进下游的比对）
	r := newBadCaseSvcRow(t, db, "bc_lb_trim", model.BadCaseSourceManual, model.BadCaseStatusPending)
	got, err = svc.Label(ctx, r.ID, "op-9", model.BadCaseLabelKBStale, "  旧政策  ")
	if err != nil || got == nil {
		t.Fatalf("打标失败：%v", err)
	}
	if got.LabelNote != "旧政策" {
		t.Errorf("判定依据没去空白：%q", got.LabelNote)
	}
}

func TestBadCaseService_Dismiss(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	// pending → dismissed
	p := newBadCaseSvcRow(t, db, "bc_ds_p", model.BadCaseSourceZeroHit, model.BadCaseStatusPending)
	got, err := svc.Dismiss(ctx, p.ID, "op-3", "这是客户在闲聊，不算低质")
	if err != nil {
		t.Fatalf("撤销 pending 失败：%v", err)
	}
	if got.Status != model.BadCaseStatusDismissed {
		t.Errorf("状态没迁：%q", got.Status)
	}
	if got.CancelReason != "这是客户在闲聊，不算低质" {
		t.Errorf("理由没落库：%q", got.CancelReason)
	}
	if got.LabelerID != "op-3" {
		t.Errorf("撤销没落到具体的人：%q", got.LabelerID)
	}

	// labeled → dismissed（判完发现是误报是常态）
	l := newBadCaseSvcRow(t, db, "bc_ds_l", model.BadCaseSourceManual, model.BadCaseStatusLabeled)
	if got, err := svc.Dismiss(ctx, l.ID, "op-3", "误报"); err != nil || got.Status != model.BadCaseStatusDismissed {
		t.Fatalf("已打标的行也应允许撤销：%v/%v", got, err)
	}

	// exported → 不许改（那份评测集已经交到下游手上）
	e := newBadCaseSvcRow(t, db, "bc_ds_e", model.BadCaseSourceManual, model.BadCaseStatusExported)
	if got, err := svc.Dismiss(ctx, e.ID, "op-3", "想收回"); got != nil || !errors.Is(err, ErrBadCaseTransition) {
		t.Errorf("已导出的行应判 Transition，实际 %v/%v", got, err)
	} else if !strings.Contains(err.Error(), model.BadCaseStatusExported) {
		t.Errorf("错误里没带当前态：%v", err)
	}

	// dismissed → 再撤销：终态，Transition
	if got, err := svc.Dismiss(ctx, p.ID, "op-3", "再撤一次"); got != nil || !errors.Is(err, ErrBadCaseTransition) {
		t.Errorf("终态再撤销应判 Transition，实际 %v/%v", got, err)
	}

	// 不存在的行 → NotFound
	if got, err := svc.Dismiss(ctx, "bc_ds_absent", "op-3", "理由"); got != nil || !errors.Is(err, ErrBadCaseNotFound) {
		t.Errorf("不存在的行应判 NotFound，实际 %v/%v", got, err)
	}

	// 理由与身份都是必填：撤销是一条"这不算坏例"的结论，
	// 没人、没理由的撤销等于把一条样本从队列里抹掉且无处追问。
	for _, c := range []struct {
		name   string
		who    string
		reason string
	}{
		{"空操作者", "", "理由"},
		{"空理由", "op-3", "   "},
	} {
		r := newBadCaseSvcRow(t, db, "bc_ds_bad_"+strings.ReplaceAll(c.name, " ", "_"),
			model.BadCaseSourceManual, model.BadCaseStatusPending)
		if got, err := svc.Dismiss(ctx, r.ID, c.who, c.reason); got != nil || !errors.Is(err, ErrBadCaseInputInvalid) {
			t.Errorf("%s：应判入参非法，实际 %v/%v", c.name, got, err)
		}
		var stored model.BadCase
		if err := db.Where("id = ?", r.ID).First(&stored).Error; err != nil {
			t.Fatalf("回读失败：%v", err)
		}
		if stored.Status != model.BadCaseStatusPending {
			t.Errorf("%s：被拒的撤销仍改写了库里的行（%s）", c.name, stored.Status)
		}
	}
}

// ---------- 走库：导出评测集 ----------

func labelOne(t *testing.T, svc *BadCaseService, db *gorm.DB, id, label string) {
	t.Helper()
	r := newBadCaseSvcRow(t, db, id, model.BadCaseSourceManual, model.BadCaseStatusPending)
	if _, err := svc.Label(context.Background(), r.ID, "op-1", label, "判定依据"); err != nil {
		t.Fatalf("打标失败：%v", err)
	}
}

func TestBadCaseService_ExportEvalSet(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	// 没有可导出的行 → NothingToExport（不是空集：空集会被前端读成"判完了"）
	if set, err := svc.ExportEvalSet(ctx, nil, 0); set != nil || !errors.Is(err, ErrBadCaseNothingToExport) {
		t.Errorf("空队列应判 NothingToExport，实际 %v/%v", set, err)
	}

	labelOne(t, svc, db, "bc_ex_1", model.BadCaseLabelKBMissing)
	labelOne(t, svc, db, "bc_ex_2", model.BadCaseLabelGenerationWrong)
	// 一条没判过的：不该进评测集
	newBadCaseSvcRow(t, db, "bc_ex_pending", model.BadCaseSourceManual, model.BadCaseStatusPending)

	set, err := svc.ExportEvalSet(ctx, nil, 0)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if set.Count != len(set.Rows) || set.Count != 2 {
		t.Errorf("Count 与行数不自洽：count=%d rows=%d", set.Count, len(set.Rows))
	}
	if set.EvalSetID == "" || !strings.HasPrefix(set.EvalSetID, "bce_") {
		t.Errorf("评测集 id 格式不对：%q", set.EvalSetID)
	}
	if set.ExportedAt.IsZero() {
		t.Error("exported_at 没给：下游无法判断这份集子是哪一刻的快照")
	}
	for _, r := range set.Rows {
		if r.CaseID == "" || r.Label == "" || r.FixLayer == "" || r.LabelNote == "" || r.LabelerID == "" {
			t.Errorf("导出行缺结论字段：%+v", r)
		}
		if r.QueryText == "" || r.AnswerText == "" {
			t.Errorf("导出行缺现场：%+v", r)
		}
		if r.LabeledAt.IsZero() {
			t.Errorf("导出行没带判定时刻：%+v", r)
		}
		if r.Threshold <= 0 {
			t.Errorf("导出行的阈值列是 0（判据无法复算）：%+v", r)
		}
	}

	// 再导一次：两条已被取走 ⇒ sentinel；pending 那条不该被顺走
	if again, err := svc.ExportEvalSet(ctx, nil, 0); again != nil || !errors.Is(err, ErrBadCaseNothingToExport) {
		t.Errorf("第二次导出应判 NothingToExport，实际 %v/%v", again, err)
	}
	var still model.BadCase
	if err := db.Where("id = ?", "bc_ex_pending").First(&still).Error; err != nil {
		t.Fatalf("回读失败：%v", err)
	}
	if still.Status != model.BadCaseStatusPending || still.EvalSetID != "" {
		t.Errorf("未打标的行被导出：%s/%q", still.Status, still.EvalSetID)
	}
	// 导出的两行在库里要是 exported 且都归同一个集
	var moved int64
	if err := db.Model(&model.BadCase{}).Where("status = ? AND eval_set_id = ?",
		model.BadCaseStatusExported, set.EvalSetID).Count(&moved).Error; err != nil {
		t.Fatalf("计数失败：%v", err)
	}
	if moved != 2 {
		t.Errorf("库里归属该评测集的应有 2 行，实际 %d", moved)
	}

	// 显式 limit=1：只取走一条
	labelOne(t, svc, db, "bc_ex_3", model.BadCaseLabelRetrieveMiss)
	one, err := svc.ExportEvalSet(ctx, nil, 1)
	if err != nil {
		t.Fatalf("limit=1 导出失败：%v", err)
	}
	if one.Count != 1 {
		t.Errorf("limit=1 应取 1 行，实际 %d", one.Count)
	}

	// 按类目导：只取指定类目（把别的层混进同一份集子等于污染基线的归因）
	labelOne(t, svc, db, "bc_ex_4", model.BadCaseLabelKBStale)
	labelOne(t, svc, db, "bc_ex_5", model.BadCaseLabelIntentMisjudge)
	byKb, err := svc.ExportEvalSet(ctx, []string{model.BadCaseLabelKBMissing, model.BadCaseLabelKBStale}, 50)
	if err != nil {
		t.Fatalf("按类目导出失败：%v", err)
	}
	if byKb.Count != 1 {
		t.Errorf("按类目应只取 1 行（kb_stale），实际 %d", byKb.Count)
	}
	for _, r := range byKb.Rows {
		if r.FixLayer != model.BadCaseFixLayerKnowledge {
			t.Errorf("按类目导出串了责任层：%+v", r)
		}
	}
	// 未知类目由仓储判入参非法（服务层不重复一遍校验，但要把它透上来）
	if set, err := svc.ExportEvalSet(ctx, []string{"bogus_label"}, 10); set != nil || !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("未知类目应判入参非法，实际 %v/%v", set, err)
	}
	// limit 负数：仓储判非法（服务层只兜 0 → 默认档）
	if set, err := svc.ExportEvalSet(ctx, nil, -3); set != nil || !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("负 limit 应判入参非法，实际 %v/%v", set, err)
	}
}

// 导出产物的字段集是本卡与 T-P8-04 的接口。
//
// expected_* 一列刻意**不给**：打标的人判的是"这句回答错在哪"，不是"正确回答是什么"。
// 留一个空字段会被下游当成"已有人写过标准答案"而直接用，整条基线就建在空列上。
func TestBadCaseEvalExport_HasNoExpectedColumns(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(BadCaseEvalRow{}), reflect.TypeOf(BadCaseEvalExport{})} {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.ToLower(f.Name)
			tag := strings.ToLower(string(f.Tag.Get("json")))
			if strings.Contains(name, "expected") || strings.Contains(tag, "expected") ||
				strings.Contains(name, "gold") || strings.Contains(tag, "gold") ||
				strings.Contains(name, "reference") || strings.Contains(tag, "reference") {
				t.Errorf("%s.%s（json=%q）是「标准答案」类字段：本卡给不出它，留空会被下游当成已有", typ.Name(), f.Name, tag)
			}
		}
	}

	// 反向：JSON 化后确实不含这些键，且每条现场字段都在（前端/落盘读的是 json 键名）
	payload, err := json.Marshal(BadCaseEvalRow{CaseID: "bc_1", QueryText: "问", AnswerText: "答", Label: "kb_missing"})
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}
	var keys map[string]any
	if err := json.Unmarshal(payload, &keys); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	for _, want := range []string{"case_id", "query_text", "answer_text", "label", "fix_layer", "label_note", "confidence", "threshold", "labeled_at", "labeler_id", "source"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("导出行缺 json 键 %q：%s", want, payload)
		}
	}
	if strings.Contains(strings.ToLower(string(payload)), "expected") {
		t.Errorf("导出行里出现了 expected：%s", payload)
	}
}

// ---------- 走库：读数 ----------

func TestBadCaseService_ListAndStats(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	// 空库：Stats 要回一个可用结构而不是 nil —— 控制器把这句话渲染成"全 0"，
	// 而 Available() 为 true 时它确实是一句业务结论。
	stats, err := svc.Stats(ctx)
	if err != nil || stats == nil {
		t.Fatalf("空库 Stats 应可用：%v", err)
	}
	if stats.LabeledRatio != 0 {
		t.Errorf("空库已判率应为 0，实际 %v（分母 0 时不许做除法）", stats.LabeledRatio)
	}
	for _, s := range model.BadCaseStatuses {
		if stats.ByStatus[s] != 0 {
			t.Errorf("空库 by_status[%s] 应为 0", s)
		}
	}

	list, total, err := svc.List(ctx, repository.BadCaseListQuery{})
	if err != nil || len(list) != 0 || total != 0 {
		t.Fatalf("空库 List：%v/%d/%v", list, total, err)
	}

	// 造四个态各一行：pending / labeled / exported / dismissed
	newBadCaseSvcRow(t, db, "bc_st_p", model.BadCaseSourceLowConfidence, model.BadCaseStatusPending)
	labelOne(t, svc, db, "bc_st_l", model.BadCaseLabelKBMissing)
	newBadCaseSvcRow(t, db, "bc_st_e", model.BadCaseSourceManual, model.BadCaseStatusExported)
	newBadCaseSvcRow(t, db, "bc_st_d", model.BadCaseSourceManual, model.BadCaseStatusDismissed)
	// 再补三行 pending：让分母是 7，已判 3
	for i := 0; i < 3; i++ {
		newBadCaseSvcRow(t, db, "bc_st_p"+string(rune('2'+i)), model.BadCaseSourceManual, model.BadCaseStatusPending)
	}

	stats, err = svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats 失败：%v", err)
	}
	if stats.ByStatus[model.BadCaseStatusPending] != 4 {
		t.Errorf("pending 应 4，实际 %d（%+v）", stats.ByStatus[model.BadCaseStatusPending], stats.ByStatus)
	}
	if stats.ByStatus[model.BadCaseStatusLabeled] != 1 || stats.ByStatus[model.BadCaseStatusExported] != 1 || stats.ByStatus[model.BadCaseStatusDismissed] != 1 {
		t.Errorf("其余三态应各 1：%+v", stats.ByStatus)
	}
	// 北极星的分母是"四态总数"，不是"记了多少行里的某一部分"：
	// 判完没导出、判完被撤销都算"判过了"，只数 labeled 会让已判率在每次导出后凭空掉一截。
	want := 3.0 / 7.0
	if diff := stats.LabeledRatio - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("已判率应为 3/7=%v，实际 %v", want, stats.LabeledRatio)
	}
	if stats.ByFixLayer[model.BadCaseFixLayerKnowledge] != 1 {
		t.Errorf("by_fix_layer 没带出归因：%+v", stats.ByFixLayer)
	}

	// 服务层的默认分页：HTTP 不给分页时也必须分页（一次读全表那条路只该给进程内调用）。
	// 默认视图 = 只列 pending（打标队列），所以这里的 total 是 pending 那一档的 4，不是总数 7 ——
	// 拿默认视图当"总数"读，会把队列读数当成全库读数（下面 Stats 才是总数的出处）。
	list, total, err = svc.List(ctx, repository.BadCaseListQuery{})
	if err != nil {
		t.Fatalf("默认分页 List 失败：%v", err)
	}
	if len(list) != BadCaseDefaultPageSize && len(list) != int(total) {
		t.Errorf("默认页大小应为 %d（或 pending 总数更小），实际 %d", BadCaseDefaultPageSize, len(list))
	}
	if total != 4 {
		t.Errorf("默认视图（pending）应 4 条，实际 %d", total)
	}
	// 显式要四态时才该读到总数
	if _, total, err := svc.List(ctx, repository.BadCaseListQuery{Statuses: model.BadCaseStatuses}); err != nil || total != 7 {
		t.Errorf("四态全列应 7 条：(%d,%v)", total, err)
	}
	// 显式给 page 而不给 page_size：服务层不兜（仓储会判非法），
	// 这条挡住"服务层顺手补一个 0 页长"把错误吞成空列表。
	if list, total, err := svc.List(ctx, repository.BadCaseListQuery{Page: 2}); list != nil || total != 0 || err == nil {
		t.Errorf("只给 page 时应把仓储的入参错误透上来：%v/%d/%v", list, total, err)
	}
	// 值域外的过滤也要透上来（不吞成空列表）
	if list, total, err := svc.List(ctx, repository.BadCaseListQuery{Statuses: []string{"bogus"}}); list != nil || total != 0 || !errors.Is(err, ErrBadCaseInputInvalid) {
		t.Errorf("未知状态应透传入参错误，实际 %v/%d/%v", list, total, err)
	}
}

func TestBadCaseService_Get(t *testing.T) {
	svc, db := setupBadCaseSvc(t)
	ctx := context.Background()

	r := newBadCaseSvcRow(t, db, "bc_get_1", model.BadCaseSourceZeroHit, model.BadCaseStatusPending)
	got, err := svc.Get(ctx, r.ID)
	if err != nil || got == nil || got.ID != r.ID {
		t.Fatalf("Get 失败：(%v,%v)", got, err)
	}
	if got.Source != model.BadCaseSourceZeroHit {
		t.Errorf("Get 读回来的来源不对：%q", got.Source)
	}
	// 不存在 → (nil, nil)：404 的判断归控制器
	if got, err := svc.Get(ctx, "bc_get_absent"); got != nil || err != nil {
		t.Errorf("不存在应回 (nil,nil)：%v/%v", got, err)
	}
}
