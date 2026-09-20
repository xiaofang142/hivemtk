// opportunity_test.go T-P4-03：商机服务层（AC①②③）。
//
// 两条腿并用（沿用 T-P3-01 的口径）：
//   - **真库**跑"跃迁落库 / 赢率回写 / 版本推进"这三条只有靠真实行才成立的路径 ——
//     本层的核心承诺是"改完之后库里那一行就是这个答案"，内存底座证不出这件事；
//   - **假仓储**跑两类真库给不出的判据：读故障、写故障、句柄不可用这些造不出来的分支，
//     以及**本层交给仓储的那一份浮点值** —— numeric 列自己会舍，库里读回来的那份
//     分辨不出"本层舍过"还是"PG 替我们舍"，只有绕开 PG 才看得境。
//
// 赢率的期望值在这里**一律写成字面量**（0.55 / 0.30 / 0.45…），不调用被测函数算：
// 用同一个函数生成期望值等于把"式子算错了"和"用例抄错了"两件事合并成永远不会红的一条断言。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// —— 装配 ——————————————————————————————————————————————

// oppClockBase 是所有涉时钟用例的共同"当前时刻"。冻它不是图省事：逾期判据读 now，
// 不冻的话"把用例挪到 2027 年跑"就会有一半结论翻面。
var oppClockBase = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func newOpportunityServiceWithDB(t *testing.T) (*OpportunityService, repository.OpportunityRepository, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.Opportunity{})
	repo := repository.NewOpportunityRepositoryWithDB(database)
	svc := NewOpportunityService(repo)
	svc.SetClock(func() time.Time { return oppClockBase })
	return svc, repo, database
}

// seedOpportunity 造一行**值域合法**的在跑商机。
//
// 刻意不叫"合法行"：status=lost 的行必须带原因才合法（不变量在本层），
// 那个形状由调用点自己补上，这里给的是最大公约数。
func seedOpportunity(t *testing.T, repo repository.OpportunityRepository, id, stage, status string) *model.Opportunity {
	t.Helper()
	row := &model.Opportunity{
		ID:             id,
		Code:           "OPP-" + strings.ToUpper(id),
		CustomerID:     "cus_" + id,
		Stage:          stage,
		Status:         status,
		Amount:         1200,
		Currency:       "CNY",
		WinProbability: 0.42, // 一个不属于任何公式输出的值：它一旦变，就知道谁重算了
		OwnerUserID:    "sales_a",
		CreatedAt:      oppClockBase.Add(-72 * time.Hour),
	}
	if err := repo.Insert(context.Background(), row); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	return row
}

// readOpportunityRow 独立零值 struct 读回（复用已填充的 struct 再 First() 会把旧字段
// 并进 WHERE，那是另一类假红）。
func readOpportunityRow(t *testing.T, database *gorm.DB, id string) model.Opportunity {
	t.Helper()
	var row model.Opportunity
	if err := database.Where("id = ?", id).First(&row).Error; err != nil {
		t.Fatalf("读回 %s: %v", id, err)
	}
	return row
}

func oppTimePtr(at time.Time) *time.Time {
	local := at
	return &local
}

// —— AC① 跃迁表 ——————————————————————————————————————————

// TestOpportunityAllowedMovesMatchesMachineRules 把跃迁表整体钉成一张对照表。
//
// 三条规则各自对应一种真实破坏：
//   - 向前只一步：跳格会让某一格的证据从未产生（"没确认需求就报价"）；
//   - 向后任意步：禁回退的出口是"作废重建"，而那会把 C6 北极星的分母（新建商机数）
//     灌进一行行修数据的假商机；
//   - 同格不是跃迁：允许就等于任何人可以靠"再点一次"把 version 涨上去，
//     而 version 的语义是"被成功改过几次"。
func TestOpportunityAllowedMovesMatchesMachineRules(t *testing.T) {
	stageMoves := func(targets ...string) []OpportunityMove {
		out := make([]OpportunityMove, 0, len(targets))
		for _, s := range targets {
			out = append(out, OpportunityMove{Kind: OpportunityMoveStage, Target: s})
		}
		return out
	}
	statusMoves := func(targets ...string) []OpportunityMove {
		out := make([]OpportunityMove, 0, len(targets))
		for _, s := range targets {
			out = append(out, OpportunityMove{Kind: OpportunityMoveStatus, Target: s})
		}
		return out
	}
	// 在跑的行：合法的目标阶段（按管线顺序）+ lost + cancelled。won 刻意不在内（下一条用例）。
	openStatusMoves := statusMoves(model.OpportunityStatusLost, model.OpportunityStatusCancelled)
	q := model.OpportunityStageQualification
	n := model.OpportunityStageNeedsConfirmed
	p := model.OpportunityStageProposal
	g := model.OpportunityStageNegotiation

	for _, tc := range []struct {
		name   string
		stage  string
		status string
		want   []OpportunityMove
	}{
		{
			// 第一格往后没有格子可退，向前只有紧邻的那一格。
			name: "qualification 只能前进一格", stage: q, status: model.OpportunityStatusOpen,
			want: append(stageMoves(n), openStatusMoves...),
		},
		{
			// 反向"任意步"、正向"一步"是刻意的不对称：见函数头注释。
			name: "needs_confirmed 可退回首格或前进一格（不可跳到谈判）", stage: n, status: model.OpportunityStatusOpen,
			want: append(stageMoves(q, p), openStatusMoves...),
		},
		{
			name: "proposal 可前进一格或退回任意格", stage: p, status: model.OpportunityStatusOpen,
			want: append(stageMoves(q, n, g), openStatusMoves...),
		},
		{
			name: "negotiation 只能整段回退", stage: g, status: model.OpportunityStatusOpen,
			want: append(stageMoves(q, n, p), openStatusMoves...),
		},
		{
			name: "lost 只有一条回退", stage: n, status: model.OpportunityStatusLost,
			want: statusMoves(model.OpportunityStatusOpen),
		},
		{
			name: "won 什么都不剩", stage: g, status: model.OpportunityStatusWon,
			want: nil,
		},
		{
			name: "cancelled 什么都不剩", stage: q, status: model.OpportunityStatusCancelled,
			want: nil,
		},
		{
			name: "未知阶段不产生任何可请求动作", stage: "sprint_to_demo", status: model.OpportunityStatusOpen,
			want: nil,
		},
		{
			name: "未知状态不产生任何可请求动作", stage: q, status: "on_hold",
			want: nil,
		},
		{
			name: "空阶段空状态同样（值域校验会另报，这里只保证不猜）", stage: "", status: "",
			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AllowedOpportunityMoves(tc.stage, tc.status)
			if len(got) != len(tc.want) {
				t.Fatalf("可请求动作 %d 项 %v，期望 %d 项 %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 项是 %+v，期望 %+v（顺序也是判据：UI 按它渲染按钮）", i, got[i], tc.want[i])
				}
			}
			for _, mv := range got {
				if mv.Target == tc.stage {
					t.Errorf("当前阶段 %s 出现在可请求集合里：同格改写会把 version 白涨一格", tc.stage)
				}
				if mv.Kind == OpportunityMoveStatus && mv.Target == tc.status {
					t.Errorf("当前状态 %s 出现在可请求集合里", tc.status)
				}
			}
		})
	}

	// 赢单永远不在"可请求"集合里 —— 哪怕这一行确实可能被赢单改变。
	for _, stage := range model.OpportunityStages {
		for _, mv := range AllowedOpportunityMoves(stage, model.OpportunityStatusOpen) {
			if mv.Target == model.OpportunityStatusWon {
				t.Errorf("阶段 %s 的可请求动作里有 won：UI 会长出一个服务端必然拒绝的赢单按钮", stage)
			}
		}
	}
}

// TestOpportunityStatusMoveIsGatedByCause AC③ 的数据面：谁能落到哪个终态。
//
// 这一张表就是"唯一入口"的实现机制：新加一个导出方法想写 won，必须**挑一个 cause**，
// 而唯一放行 won 的 cause 是"回款完成"。它比"数一下源码里有几处赋值"更硬 ——
// 复用同一个内部通道的那种绕法在这里照样被拒。
func TestOpportunityStatusMoveIsGatedByCause(t *testing.T) {
	for _, tc := range []struct {
		cause OpportunityCloseCause
		from  string
		to    string
		ok    bool
	}{
		{OpportunityCauseCollection, model.OpportunityStatusOpen, model.OpportunityStatusWon, true},
		{OpportunityCauseCollection, model.OpportunityStatusOpen, model.OpportunityStatusLost, false},
		{OpportunityCauseCollection, model.OpportunityStatusOpen, model.OpportunityStatusCancelled, false},
		{OpportunityCauseCollection, model.OpportunityStatusOpen, model.OpportunityStatusOpen, false},
		{OpportunityCauseCollection, model.OpportunityStatusLost, model.OpportunityStatusWon, false},
		{OpportunityCauseCollection, model.OpportunityStatusCancelled, model.OpportunityStatusWon, false},
		{OpportunityCauseCollection, model.OpportunityStatusWon, model.OpportunityStatusWon, false},
		{OpportunityCauseSales, model.OpportunityStatusOpen, model.OpportunityStatusLost, true},
		{OpportunityCauseSales, model.OpportunityStatusOpen, model.OpportunityStatusCancelled, true},
		{OpportunityCauseSales, model.OpportunityStatusLost, model.OpportunityStatusOpen, true},
		{OpportunityCauseSales, model.OpportunityStatusOpen, model.OpportunityStatusWon, false},
		{OpportunityCauseSales, model.OpportunityStatusWon, model.OpportunityStatusLost, false},
		{OpportunityCauseSales, model.OpportunityStatusCancelled, model.OpportunityStatusOpen, false},
		{OpportunityCauseSales, model.OpportunityStatusWon, model.OpportunityStatusOpen, false},
		// 没人认领的 cause 一个都不放行：新增枚举值不会默认继承某条边。
		{OpportunityCloseCause("autopilot"), model.OpportunityStatusOpen, model.OpportunityStatusWon, false},
		{OpportunityCloseCause(""), model.OpportunityStatusOpen, model.OpportunityStatusLost, false},
	} {
		got := opportunityStatusMoveLegal(tc.cause, tc.from, tc.to)
		if got != tc.ok {
			t.Errorf("跃迁 %s: %s→%s 判成 %v，期望 %v", tc.cause, tc.from, tc.to, got, tc.ok)
		}
	}
}

// TestOpportunityServiceSurfaceIsFrozen 导出面冻结：本层的每一个入口都是被点名过的。
//
// 它看着像形式主义，实际拦的是 AC③ 那种"悄悄多一个写入口"：新增一个导出方法就必须
// 同时改这张清单，而改清单意味着有人得在评审里回答"这个入口谁调、它凭什么改这列"。
func TestOpportunityServiceSurfaceIsFrozen(t *testing.T) {
	want := []string{
		"Available", "Cancel", "Edit", "Get", "MarkLost", "MarkWonByCollection",
		"MoveStage", "Reopen", "SetClock",
	}
	got := make([]string, 0, len(want))
	svcType := reflect.TypeOf(&OpportunityService{})
	for i := 0; i < svcType.NumMethod(); i++ {
		name := svcType.Method(i).Name
		if name[0] >= 'a' && name[0] <= 'z' {
			continue // 非导出方法不在承诺范围内
		}
		got = append(got, name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("导出方法集变了：\n 得到 %v\n 期望 %v", got, want)
	}
}

// —— AC② 赢率计算式 ——————————————————————————————————————

// TestOpportunityWinProbabilityMonotoneInStage C5 的嵌套集合论证。
//
// 若"通过第 k 格"是赢单的必要条件，则 {赢单} ⊆ {到达第 k+1 格} ⊆ {到达第 k 格}，
// 于是 P(赢|到达第 k 格) = P(赢)/P(到达第 k 格) 随 k 单调上升 —— 这是本式**唯一**
// 能被证明的部分：形状（递增）有依据，数值没有。数值是显式登记的先验，
// 等 P8 攒够真实结果行之后回标（那之前它只是排序的刻度）。
func TestOpportunityWinProbabilityMonotoneInStage(t *testing.T) {
	var prev = -1.0
	for _, stage := range model.OpportunityStages {
		p, err := opportunityWinProbability(OpportunityWinInput{Stage: stage, Assigned: true})
		if err != nil {
			t.Fatalf("阶段 %s 计算失败: %v", stage, err)
		}
		if !(p > prev) {
			t.Errorf("阶段 %s 的赢率 %v 不大于前一格的 %v：管线顺序在数值上塌了", stage, p, prev)
		}
		if p <= 0 || p >= 1 {
			t.Errorf("阶段 %s 的赢率 %v 越出 (0,1)：0 会被读成绝不可能成，1 是已知结果不是预测", stage, p)
		}
		prev = p
	}

	// 任何惩罚项叠加都不许把值推出值域，也不许破坏"不增"的次序。
	for _, stage := range model.OpportunityStages {
		for _, assigned := range []bool{true, false} {
			for _, overdue := range []bool{true, false} {
				p, err := opportunityWinProbability(OpportunityWinInput{Stage: stage, Assigned: assigned, Overdue: overdue})
				if err != nil {
					t.Fatalf("计算失败: %v", err)
				}
				if p <= 0 || p >= 1 {
					t.Errorf("阶段 %s assigned=%v overdue=%v 的赢率 %v 越出 (0,1)", stage, assigned, overdue, p)
				}
			}
		}
	}
}

// TestOpportunityWinProbabilityInputsAreOnlyThisRowsOwnFacts AC② 的正面判据（C5）。
//
// 三套评分回答的是三个**不同条件集**下的概率：confidence 以"这一问一答"为条件、
// lead_score 以"这个客户"为条件、win_probability 以"这一单自身的阶段与事实"为条件。
// 条件集不同的两个概率相乘不对应任何事件，所以本式的输入里**不许出现**它们 ——
// 把它做成字段名的集合断言，是因为"顺手多传一个分数进来"在数值上完全无害、
// 只会在语义上悄悄合成一个谁也不敢用的数。
func TestOpportunityWinProbabilityInputsAreOnlyThisRowsOwnFacts(t *testing.T) {
	winInput := reflect.TypeOf(OpportunityWinInput{})
	if winInput.Kind() != reflect.Struct {
		t.Fatalf("OpportunityWinInput 不是 struct")
	}
	if winInput.NumField() != 3 {
		t.Errorf("计算式输入有 %d 个字段，期望 3（阶段 / 有无归属 / 是否逾期）", winInput.NumField())
	}
	foreign := []string{"confidence", "lead", "score", "churn", "rfm", "probability", "value"}
	for i := 0; i < winInput.NumField(); i++ {
		name := strings.ToLower(winInput.Field(i).Name)
		for _, needle := range foreign {
			if strings.Contains(name, needle) {
				t.Errorf("字段 %s 含 %q：赢率不许读别的域的评分，也不许被调用方直接书写", winInput.Field(i).Name, needle)
			}
		}
	}

	// 同理：改写入参里没有赢率这一格。派生量只能由本层算，不能由调用方填。
	edit := reflect.TypeOf(OpportunityEdit{})
	for i := 0; i < edit.NumField(); i++ {
		name := strings.ToLower(edit.Field(i).Name)
		if strings.Contains(name, "prob") || strings.Contains(name, "score") || name == "status" || name == "stage" {
			t.Errorf("OpportunityEdit 出现字段 %s：赢率/阶段/状态都不是编辑能碰的", edit.Field(i).Name)
		}
	}
}

// TestOpportunityWinProbabilityValuesAreExactlyTwoDecimals 把"落库必被舍入"这件事
// 挪到落库之前做。
//
// numeric(5,2) 会把 0.75−0.15 = 0.5999999999999999 存成 0.60；本层不先舍入的话，
// 返回给调用方的那份和库里的那份就是两个数，而 ltc.config 的阈值比较读的是哪一个
// 取决于代码先撞上谁 —— 0.5 阈值线上一次多算一次少算就是"该不该报价"两种结论。
// 下面每个期望值都是**手算的字面量**，不是 round(计算结果)。
func TestOpportunityWinProbabilityValuesAreExactlyTwoDecimals(t *testing.T) {
	for _, tc := range []struct {
		stage    string
		assigned bool
		overdue  bool
		want     float64
	}{
		{model.OpportunityStageQualification, true, false, 0.10},
		{model.OpportunityStageNeedsConfirmed, true, false, 0.30},
		{model.OpportunityStageProposal, true, false, 0.55},
		{model.OpportunityStageNegotiation, true, false, 0.75},
		// 逾期一项（−0.15）：0.75−0.15 在 float64 里是 0.5999999999999999
		{model.OpportunityStageQualification, true, true, 0.05},  // 触到下界，不是 0
		{model.OpportunityStageNeedsConfirmed, true, true, 0.15}, // 0.14999999999999999
		{model.OpportunityStageProposal, true, true, 0.40},       // 0.4000000000000001
		{model.OpportunityStageNegotiation, true, true, 0.60},    // 0.5999999999999999
		// 无归属一项（−0.10）
		{model.OpportunityStageQualification, false, false, 0.05}, // 0.10−0.10=0 → 下界
		{model.OpportunityStageNeedsConfirmed, false, false, 0.20},
		{model.OpportunityStageProposal, false, false, 0.45}, // 0.4500000000000001
		{model.OpportunityStageNegotiation, false, false, 0.65},
		// 两项一起
		{model.OpportunityStageQualification, false, true, 0.05},
		{model.OpportunityStageNeedsConfirmed, false, true, 0.05},
		{model.OpportunityStageProposal, false, true, 0.30},
		{model.OpportunityStageNegotiation, false, true, 0.50},
	} {
		got, err := opportunityWinProbability(OpportunityWinInput{Stage: tc.stage, Assigned: tc.assigned, Overdue: tc.overdue})
		if err != nil {
			t.Fatalf("%s assigned=%v overdue=%v: %v", tc.stage, tc.assigned, tc.overdue, err)
		}
		if got != tc.want {
			t.Errorf("%s assigned=%v overdue=%v 算成 %.20g，期望 %.2f",
				tc.stage, tc.assigned, tc.overdue, got, tc.want)
		}
		if got != math.Round(got*100)/100 {
			t.Errorf("%s 的值 %.20g 不是两位小数：落库会被 PG 舍入，回来的就不是这个数", tc.stage, got)
		}
	}

	// 未知阶段不许退化成"算不出来就当 0"：0 是最响的那个错误答案。
	if _, err := opportunityWinProbability(OpportunityWinInput{Stage: "demo_day", Assigned: true}); err == nil {
		t.Error("未知阶段算出了赢率：宁可报错，不可给 0")
	} else if !errors.Is(err, ErrOpportunityStateInvalid) {
		t.Errorf("未知阶段的错误是 %v，期望包 ErrOpportunityStateInvalid", err)
	}
}

// —— 落库：跃迁 + 重算 ————————————————————————————————————

func TestOpportunityService_MoveStagePersistsRecomputedProbability(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "mv1", model.OpportunityStageQualification, model.OpportunityStatusOpen)

	got, err := svc.MoveStage(ctx, row.ID, row.Version, model.OpportunityStageNeedsConfirmed)
	if err != nil {
		t.Fatalf("MoveStage: %v", err)
	}
	if got.Stage != model.OpportunityStageNeedsConfirmed {
		t.Errorf("返回的阶段是 %s", got.Stage)
	}
	if got.WinProbability != 0.30 {
		t.Errorf("返回的赢率是 %.20g，期望 0.30（needs_confirmed 基准）", got.WinProbability)
	}
	if got.Version != row.Version+1 {
		t.Errorf("返回的版本是 %d，期望 %d", got.Version, row.Version+1)
	}
	if got.LostReason != "" || got.Status != model.OpportunityStatusOpen {
		t.Errorf("一次阶段跃迁顺手改了 status/lost_reason: %+v", got)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if !reflect.DeepEqual(*got, fresh) {
		t.Errorf("返回的那份与库里不一致：\n 返回 %+v\n 库里 %+v", *got, fresh)
	}

	// 跳格必须被拒（资格没确认就到谈判）。
	before := readOpportunityRow(t, database, row.ID)
	if _, err := svc.MoveStage(ctx, row.ID, before.Version, model.OpportunityStageNegotiation); err == nil ||
		!errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("跨格前进没有被拒: %v", err)
	}
	if after := readOpportunityRow(t, database, row.ID); after.Version != before.Version || after.Stage != before.Stage {
		t.Errorf("被拒的跃迁还是改了行: %+v", after)
	}

	// 回退任意步合法（这是"作废重建"的替代出口）。
	back, err := svc.MoveStage(ctx, row.ID, before.Version, model.OpportunityStageQualification)
	if err != nil {
		t.Fatalf("回退应合法: %v", err)
	}
	if back.Stage != model.OpportunityStageQualification || back.WinProbability != 0.10 {
		t.Errorf("回退后 %+v，期望 qualification / 0.10", back)
	}

	// "再点一次当前阶段"不是跃迁：它什么也没改，却会把 version 涨一格，
	// 而 version 的语义是"被成功改过几次"（T-P4-02 的 CAS 判据就建在这句话上）。
	same, err := svc.MoveStage(ctx, row.ID, back.Version, model.OpportunityStageQualification)
	if err == nil || !errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("同格改写没有被拒，得到 (%+v, %v)", same, err)
	}
	if after := readOpportunityRow(t, database, row.ID); after.Version != back.Version {
		t.Errorf("同格改写把版本从 %d 涨到了 %d", back.Version, after.Version)
	}
}

// TestOpportunityService_StoredAmountAndProbabilityMatchReturned 两位小数这件事
// 必须打到真库：只有 PG 的 numeric 才会把"我算的"和"库里存的"分成两个数。
func TestOpportunityService_StoredAmountAndProbabilityMatchReturned(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "rd1", model.OpportunityStageProposal, model.OpportunityStatusOpen)

	// 一次改写同时踩到三个非整值的坑：金额 12.3456（numeric(12,2)）、
	// 逾期后的赢率 0.55−0.10−0.15 = 0.30000000000000004、无归属。
	got, err := svc.Edit(ctx, row.ID, row.Version, OpportunityEdit{
		Amount:          12.3456,
		Currency:        "CNY",
		OwnerUserID:     "", // 移交中：无归属会掉一项
		ExpectedCloseAt: oppTimePtr(oppClockBase.Add(-24 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if got.Amount != 12.35 {
		t.Errorf("返回金额 %.20g，期望 12.35", got.Amount)
	}
	if got.WinProbability != 0.30 {
		t.Errorf("返回赢率 %.20g，期望 0.30", got.WinProbability)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.Amount != got.Amount || fresh.WinProbability != got.WinProbability {
		t.Errorf("库里 (%.20g, %.20g) 与返回 (%.20g, %.20g) 不同：舍入发生在 PG 那一侧",
			fresh.Amount, fresh.WinProbability, got.Amount, got.WinProbability)
	}
	if fresh.OwnerUserID != "" {
		t.Errorf("清空归属没有落库：%q", fresh.OwnerUserID)
	}
}

// TestOpportunityService_EditWithNilCloseDateClearsIt 整份编辑没有"未提供"这一档，
// 所以 nil 的语义只能是"清掉目标日"，不可能是"保留旧值"。
//
// 这条只能打到真库：那一列可空，判据是"读回来必须是 NULL"，而内存底座里
// 空串和没填是同一个值。少做这一步的实现（把 else 分支摘掉）症状很轻也很坏：
// 销售改过一次关单日就再也撤不掉，系统此后一路按"已逾期"给他算赢率。
func TestOpportunityService_EditWithNilCloseDateClearsIt(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "ec1", model.OpportunityStageProposal, model.OpportunityStatusOpen)

	withDate, err := svc.Edit(ctx, row.ID, row.Version, OpportunityEdit{
		Amount: 1200, Currency: "CNY", OwnerUserID: "sales_a",
		ExpectedCloseAt: oppTimePtr(oppClockBase.Add(72 * time.Hour)),
	})
	if err != nil {
		t.Fatalf("先设关单日: %v", err)
	}
	if withDate.ExpectedCloseAt == nil {
		t.Fatalf("设了关单日却读回 nil")
	}

	cleared, err := svc.Edit(ctx, withDate.ID, withDate.Version, OpportunityEdit{
		Amount: 1200, Currency: "CNY", OwnerUserID: "sales_a",
	})
	if err != nil {
		t.Fatalf("传 nil 清掉: %v", err)
	}
	if cleared.ExpectedCloseAt != nil {
		t.Errorf("返回体仍带着 %v：nil 被当成了「没填」", *cleared.ExpectedCloseAt)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.ExpectedCloseAt != nil {
		t.Errorf("库里那一行仍带着 %v：清掉没落库", *fresh.ExpectedCloseAt)
	}
	if fresh.Version != withDate.Version+1 {
		t.Errorf("版本 %d，期望 %d：这次改写没落库就不该算成功", fresh.Version, withDate.Version+1)
	}
}

func TestOpportunityService_LostRequiresReasonAndFreezesProbability(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "ls1", model.OpportunityStageNegotiation, model.OpportunityStatusOpen)

	if _, err := svc.MarkLost(ctx, row.ID, row.Version, "   "); err == nil ||
		!errors.Is(err, ErrOpportunityInputInvalid) {
		t.Fatalf("无原因的输单没有被拒: %v", err)
	}
	if unchanged := readOpportunityRow(t, database, row.ID); unchanged.Version != row.Version {
		t.Fatal("被拒的输单还是改了行")
	}

	const reason = "客户预算撤回，转明年 Q1"
	got, err := svc.MarkLost(ctx, row.ID, row.Version, reason)
	if err != nil {
		t.Fatalf("MarkLost: %v", err)
	}
	if got.Status != model.OpportunityStatusLost || got.LostReason != reason {
		t.Errorf("落库前 %+v", got)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.LostReason != reason {
		t.Errorf("原因没进库：%q", fresh.LostReason)
	}
	// 收口之后赢率**不再重算**：它留下的是"当时预测能不能成"，
	// 而 P8 的校准分析要的正是这个预测与结果的差。种子值 0.42 必须原样还在。
	if fresh.WinProbability != 0.42 {
		t.Errorf("输单把赢率从 0.42 改成 %v：终态之后不许再算预测", fresh.WinProbability)
	}
	if fresh.Stage != model.OpportunityStageNegotiation {
		t.Errorf("输单顺手改了阶段 %s：赢在哪一格与赢没赢是两张表", fresh.Stage)
	}
}

func TestOpportunityService_ReopenClearsReasonAndRecomputes(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "ro1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	lost, err := svc.MarkLost(ctx, row.ID, row.Version, "比价输了")
	if err != nil {
		t.Fatalf("MarkLost: %v", err)
	}

	got, err := svc.Reopen(ctx, lost.ID, lost.Version)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if got.Status != model.OpportunityStatusOpen {
		t.Errorf("回退后状态 %s", got.Status)
	}
	// 不变量：lost_reason 非空 ⟺ status=lost。留着旧原因，P8 的丢单归因
	// 就会在"还在跑的行"上读到一个上一轮的结论。
	if got.LostReason != "" {
		t.Errorf("回退没清空原因：%q", got.LostReason)
	}
	if got.WinProbability != 0.55 {
		t.Errorf("回退后赢率 %.20g，期望按 proposal 重算成 0.55", got.WinProbability)
	}
	if got.Stage != model.OpportunityStageProposal {
		t.Errorf("回退改了阶段 %s", got.Stage)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.LostReason != "" || fresh.WinProbability != 0.55 {
		t.Errorf("库里 %+v", fresh)
	}

	// 在跑的行没有"回退"这个动作；已赢/已作废更不许被回退。
	if _, err := svc.Reopen(ctx, got.ID, got.Version); err == nil ||
		!errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("open 行上的 Reopen 没有被拒: %v", err)
	}
	won, err := svc.MarkWonByCollection(ctx, got.ID)
	if err != nil {
		t.Fatalf("MarkWonByCollection: %v", err)
	}
	if _, err := svc.Reopen(ctx, won.ID, won.Version); err == nil ||
		!errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("won 行上的 Reopen 没有被拒: %v", err)
	}
	if _, err := svc.MoveStage(ctx, won.ID, won.Version, model.OpportunityStageQualification); err == nil ||
		!errors.Is(err, ErrOpportunityClosed) {
		t.Errorf("已赢单的行还能改阶段（且没报成 Closed）: %v", err)
	}
	if after := readOpportunityRow(t, database, won.ID); after.Status != model.OpportunityStatusWon {
		t.Errorf("被拒的改写改了终态：%+v", after)
	}
}

func TestOpportunityService_ClosedRowsRefuseEveryEdit(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()

	row := seedOpportunity(t, repo, "cl1", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	cancelled, err := svc.Cancel(ctx, row.ID, row.Version)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.Status != model.OpportunityStatusCancelled || cancelled.LostReason != "" {
		t.Errorf("作废后的行 %+v（作废不写输单原因：那是 P8 丢单归因的口径）", cancelled)
	}

	for name, call := range map[string]func() error{
		"改阶段": func() error {
			_, err := svc.MoveStage(ctx, cancelled.ID, cancelled.Version, model.OpportunityStageNegotiation)
			return err
		},
		"改金额": func() error {
			_, err := svc.Edit(ctx, cancelled.ID, cancelled.Version, OpportunityEdit{
				Amount: 99, Currency: "CNY", OwnerUserID: "sales_b",
			})
			return err
		},
		"判输": func() error {
			_, err := svc.MarkLost(ctx, cancelled.ID, cancelled.Version, "顺手")
			return err
		},
	} {
		err := call()
		if !errors.Is(err, ErrOpportunityClosed) {
			t.Errorf("%s 在已作废的行上报错成 %v，期望 ErrOpportunityClosed", name, err)
		}
		// 分错标签的代价：把"已收口"报成"跃迁不允许"，调用方会去查状态机；
		// 报成"版本过期"，它会重读再改一次，然后一直撞下去。
		if errors.Is(err, ErrOpportunityTransitionIllegal) ||
			errors.Is(err, repository.ErrOpportunityStaleVersion) {
			t.Errorf("%s 的错误标签混进了别的判据: %v", name, err)
		}
	}
	if after := readOpportunityRow(t, database, cancelled.ID); after.Version != cancelled.Version {
		t.Errorf("三次被拒的改写把版本从 %d 抬到了 %d", cancelled.Version, after.Version)
	}
}

// TestOpportunityService_MarkWonByCollection AC③ 的落地面：钱的通道，不是按钮的通道。
func TestOpportunityService_MarkWonByCollection(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "wn1", model.OpportunityStageProposal, model.OpportunityStatusOpen)

	won, err := svc.MarkWonByCollection(ctx, row.ID)
	if err != nil {
		t.Fatalf("MarkWonByCollection: %v", err)
	}
	if won.Status != model.OpportunityStatusWon {
		t.Errorf("状态 %s", won.Status)
	}
	// 停在 proposal 就成了，是一句合法的商机史（T-P4-01 的注释立着这条）。
	if won.Stage != model.OpportunityStageProposal {
		t.Errorf("赢单把阶段改成 %s", won.Stage)
	}
	if won.WinProbability != row.WinProbability {
		t.Errorf("赢单把预测值改成 %v（原本 %v）", won.WinProbability, row.WinProbability)
	}
	if won.Version != row.Version+1 {
		t.Errorf("版本 %d → 期望 %d", won.Version, row.Version+1)
	}

	// 同一条事实第二次到达：成功、且**什么都没写**。
	// 报成 409 的话 P7 的回款 webhook 会为了一条早已赢单的行一直重试。
	before := readOpportunityRow(t, database, row.ID)
	again, err := svc.MarkWonByCollection(ctx, row.ID)
	if err != nil {
		t.Fatalf("重放回款完成应当幂等成功，得到 %v", err)
	}
	if !reflect.DeepEqual(*again, before) {
		t.Errorf("重放改了行：\n 前 %+v\n 后 %+v", before, *again)
	}
	after := readOpportunityRow(t, database, row.ID)
	if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("重放推进了版本/时间（%d/%v vs %d/%v）：同一条事实不该产生两次改写",
			after.Version, after.UpdatedAt, before.Version, before.UpdatedAt)
	}

	// 已输/已作废的行不能被"钱到了"改成赢单：那是两笔账对不上，要人来判。
	// 出口是显式的两步（先 Reopen 再回款），中间没人替调用方猜。
	lostRow := seedOpportunity(t, repo, "wn2", model.OpportunityStageNegotiation, model.OpportunityStatusOpen)
	lost, err := svc.MarkLost(ctx, lostRow.ID, lostRow.Version, "价格没谈拢")
	if err != nil {
		t.Fatalf("MarkLost: %v", err)
	}
	if _, err := svc.MarkWonByCollection(ctx, lost.ID); err == nil ||
		!errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("已输单被回款改成赢单: %v", err)
	}
	cancelledRow := seedOpportunity(t, repo, "wn3", model.OpportunityStageQualification, model.OpportunityStatusOpen)
	cancelled, err := svc.Cancel(ctx, cancelledRow.ID, cancelledRow.Version)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := svc.MarkWonByCollection(ctx, cancelled.ID); err == nil ||
		!errors.Is(err, ErrOpportunityTransitionIllegal) {
		t.Errorf("误建的商机被回款改成了赢单（那会把误建灌进北极星分子）: %v", err)
	}
	if _, err := svc.MarkWonByCollection(ctx, "wn_missing"); !errors.Is(err, repository.ErrOpportunityNotFound) {
		t.Errorf("不存在的商机报成 %v，期望透传仓储的 NotFound", err)
	}
}

// —— 输入值域：本层是唯一的守门处 ————————————————————————

func TestOpportunityService_RejectsInvalidValuesWithoutWriting(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		stage string
		edit  *OpportunityEdit
		to    string
	}{
		{"负金额", model.OpportunityStageQualification, &OpportunityEdit{Amount: -0.01, Currency: "CNY", OwnerUserID: "sales_a"}, ""},
		{"超出 numeric(12,2) 容量", model.OpportunityStageQualification, &OpportunityEdit{Amount: 1e10, Currency: "CNY", OwnerUserID: "sales_a"}, ""},
		{"金额为 NaN", model.OpportunityStageQualification, &OpportunityEdit{Amount: math.NaN(), Currency: "CNY", OwnerUserID: "sales_a"}, ""},
		{"币种小写", model.OpportunityStageQualification, &OpportunityEdit{Amount: 100, Currency: "cny", OwnerUserID: "sales_a"}, ""},
		{"币种长度不对", model.OpportunityStageQualification, &OpportunityEdit{Amount: 100, Currency: "RMB1", OwnerUserID: "sales_a"}, ""},
		{"币种为空", model.OpportunityStageQualification, &OpportunityEdit{Amount: 100, Currency: "", OwnerUserID: "sales_a"}, ""},
		{"负责人超列宽", model.OpportunityStageQualification, &OpportunityEdit{Amount: 100, Currency: "CNY", OwnerUserID: strings.Repeat("s", 65)}, ""},
		{
			"预计关单日是零值时间", model.OpportunityStageQualification,
			&OpportunityEdit{Amount: 100, Currency: "CNY", OwnerUserID: "sales_a", ExpectedCloseAt: oppTimePtr(time.Time{})}, "",
		},
		{"目标阶段不认识", model.OpportunityStageQualification, nil, "closed_won"},
		{"目标阶段为空", model.OpportunityStageQualification, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "inv_" + fmt.Sprintf("%02d", hashIndexOf(tc.name))
			row := seedOpportunity(t, repo, id, tc.stage, model.OpportunityStatusOpen)
			var err error
			if tc.edit != nil {
				_, err = svc.Edit(ctx, row.ID, row.Version, *tc.edit)
			} else {
				_, err = svc.MoveStage(ctx, row.ID, row.Version, tc.to)
			}
			if !errors.Is(err, ErrOpportunityInputInvalid) {
				t.Fatalf("得到 %v，期望 ErrOpportunityInputInvalid", err)
			}
			after := readOpportunityRow(t, database, row.ID)
			if after.Version != 0 || after.Amount != 1200 || after.Stage != tc.stage {
				t.Errorf("被拒的改写还是动了行: %+v", after)
			}
		})
	}
}

// hashIndexOf 只是给子用例一个稳定的短后缀（不用 map 遍历序，避免撞名）。
func hashIndexOf(s string) int {
	n := 0
	for _, r := range s {
		n = (n*131 + int(r)) % 100000
	}
	return n
}

// TestOpportunityService_RefusesToComputeOnUnreadableRows T-P4-02 刻意不校验值域，
// 那么本层就是唯一的守门处 —— 而守门的意思是"看不懂就停"，不是"看不懂就当默认值"。
func TestOpportunityService_RefusesToComputeOnUnreadableRows(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()

	cases := []struct {
		id     string
		mutate func(*model.Opportunity)
		why    string
	}{
		{"ga1", func(o *model.Opportunity) { o.Stage = "pipeline_v2" }, "阶段不在值域里"},
		{"ga2", func(o *model.Opportunity) { o.Status = "on_hold" }, "状态不在值域里"},
		{"ga3", func(o *model.Opportunity) { o.Stage = "" }, "阶段为空（没赋过值）"},
		{"ga4", func(o *model.Opportunity) { o.WinProbability = 1.4 }, "赢率越界（仓储不拦）"},
		{"ga5", func(o *model.Opportunity) { o.Status = model.OpportunityStatusLost }, "输单没带原因（不变量破了）"},
	}
	for _, tc := range cases {
		row := &model.Opportunity{
			ID: tc.id, Code: "OPP-" + strings.ToUpper(tc.id), CustomerID: "cus_" + tc.id,
			Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
			Amount: 500, Currency: "CNY", WinProbability: 0.55, OwnerUserID: "sales_a",
			CreatedAt: oppClockBase.Add(-time.Hour),
		}
		tc.mutate(row)
		if err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("种入畸形行 %s: %v", tc.id, err)
		}
		_, err := svc.MoveStage(ctx, tc.id, 0, model.OpportunityStageNegotiation)
		if !errors.Is(err, ErrOpportunityStateInvalid) {
			t.Errorf("%s：%v 得到 %v，期望 ErrOpportunityStateInvalid", tc.id, tc.why, err)
		}
		if _, err := svc.Edit(ctx, tc.id, 0, OpportunityEdit{Amount: 1, Currency: "USD", OwnerUserID: "sales_a"}); !errors.Is(err, ErrOpportunityStateInvalid) {
			t.Errorf("%s：Edit 没挡住畸形现值: %v", tc.id, err)
		}
		if after := readOpportunityRow(t, database, tc.id); after.Version != 0 {
			t.Errorf("%s 被改写了（版本 %d）：看不懂还行就动手，等于用一套猜出来的规则盖掉现场", tc.id, after.Version)
		}
	}
}

// —— 并发与故障透传 ————————————————————————————————————————

func TestOpportunityService_StaleClientVersionWritesNothing(t *testing.T) {
	svc, repo, database := newOpportunityServiceWithDB(t)
	ctx := context.Background()
	row := seedOpportunity(t, repo, "sv1", model.OpportunityStageQualification, model.OpportunityStatusOpen)

	// 另一个人先改了一次（直接走仓储，模拟"我看到的还是 v0"）。
	other := readOpportunityRow(t, database, row.ID)
	other.Amount = 4242
	if err := repo.Update(ctx, &other); err != nil {
		t.Fatalf("旁路改写: %v", err)
	}

	if _, err := svc.MoveStage(ctx, row.ID, row.Version, model.OpportunityStageNeedsConfirmed); !errors.Is(err, repository.ErrOpportunityStaleVersion) {
		t.Fatalf("拿着旧版本改写了，得到 %v", err)
	}
	fresh := readOpportunityRow(t, database, row.ID)
	if fresh.Stage != model.OpportunityStageQualification || fresh.Amount != 4242 {
		t.Errorf("冲突的一刀还是落了库: %+v", fresh)
	}

	// 重读再改：一次成功，且返回的那份能直接当下一刀的期望版本。
	// 这一条拦的是"返回改写前那份 struct"的实现 —— 它的版本永远旧一格，
	// 于是**同一位销售的第二次编辑必然失败**，而这正是乐观锁最招恨的烂法。
	resent, err := svc.MoveStage(ctx, row.ID, fresh.Version, model.OpportunityStageNeedsConfirmed)
	if err != nil {
		t.Fatalf("重读后改写: %v", err)
	}
	third, err := svc.MoveStage(ctx, row.ID, resent.Version, model.OpportunityStageProposal)
	if err != nil {
		t.Fatalf("拿着上一次返回的版本连续改写应成功: %v", err)
	}
	if third.Version != 3 || third.Stage != model.OpportunityStageProposal {
		t.Errorf("连续改写后 %+v", third)
	}

	// 负的版本号不是"跳过检查"的哨兵。
	if _, err := svc.MoveStage(ctx, row.ID, -1, model.OpportunityStageNegotiation); !errors.Is(err, ErrOpportunityInputInvalid) {
		t.Errorf("expectedVersion=-1 得到 %v", err)
	}
}

// oppFakeRepo 只负责把"读故障 / 写故障 / 句柄不可用"这三条真库里造不出来的形状递进来。
// 其余方法走内嵌的 nil 接口：本层一个都不该调，调了就 panic —— 那正是要看见的结果。
type oppFakeRepo struct {
	repository.OpportunityRepository

	row     *model.Opportunity
	getErr  error
	updErr  error
	writes  int
	readded int
}

func (f *oppFakeRepo) Available() bool { return f != nil && f.getErr == nil && f.updErr == nil }

func (f *oppFakeRepo) GetByID(context.Context, string) (*model.Opportunity, error) {
	f.readded++
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.row == nil {
		return nil, nil
	}
	copied := *f.row
	return &copied, nil
}

func (f *oppFakeRepo) Update(_ context.Context, o *model.Opportunity) error {
	f.writes++
	if f.updErr != nil {
		return f.updErr
	}
	f.row = o
	return nil
}

func TestOpportunityService_PropagatesRepositoryFailures(t *testing.T) {
	ctx := context.Background()
	newRow := func() *model.Opportunity {
		return &model.Opportunity{
			ID: "fx1", Code: "OPP-FX1", CustomerID: "cus_fx1",
			Stage: model.OpportunityStageProposal, Status: model.OpportunityStatusOpen,
			Amount: 100, Currency: "CNY", WinProbability: 0.55, OwnerUserID: "sales_a",
		}
	}
	boom := errors.New("connection reset by peer")

	// 读故障必须原样上抛：把它读成"没有这条商机"，调用方会去重走一遍新建流程。
	readSvc := NewOpportunityService(&oppFakeRepo{getErr: boom})
	readSvc.SetClock(func() time.Time { return oppClockBase })
	err := mustErr(readSvc.MoveStage(ctx, "fx1", 0, model.OpportunityStageNegotiation))
	if !errors.Is(err, boom) {
		t.Errorf("读故障被改写成 %v", err)
	}
	if errors.Is(err, repository.ErrOpportunityNotFound) {
		t.Error("读故障被报成 NotFound")
	}

	// 写故障不得被吞成成功，也不得自动重试（重试属于调用方的策略，不属于本层）。
	writeRepo := &oppFakeRepo{row: newRow(), updErr: boom}
	writeSvc := NewOpportunityService(writeRepo)
	writeSvc.SetClock(func() time.Time { return oppClockBase })
	if err := mustErr(writeSvc.MoveStage(ctx, "fx1", 0, model.OpportunityStageNegotiation)); !errors.Is(err, boom) {
		t.Errorf("写故障被吞掉了: %v", err)
	}
	if writeRepo.writes != 1 {
		t.Errorf("写故障路径上调了 %d 次 Update，期望 1", writeRepo.writes)
	}

	// 句柄不可用 ⇒ 明确报错，不是"什么也没发生"的成功。
	offSvc := NewOpportunityService(&oppFakeRepo{getErr: errors.New("db handle is nil")})
	if offSvc.Available() {
		t.Error("底座不可用时 Available 不该为真")
	}
	if err := mustErr(offSvc.Cancel(ctx, "fx1", 0)); err == nil {
		t.Error("底座不可用还返回了成功")
	}

	var nilSvc *OpportunityService
	if nilSvc.Available() {
		t.Error("nil 服务的 Available 不该为真")
	}
	for name, call := range map[string]func() error{
		"MoveStage":           func() error { return mustErr(nilSvc.MoveStage(ctx, "fx1", 0, model.OpportunityStageNegotiation)) },
		"Edit":                func() error { return mustErr(nilSvc.Edit(ctx, "fx1", 0, OpportunityEdit{Amount: 1, Currency: "CNY"})) },
		"MarkLost":            func() error { return mustErr(nilSvc.MarkLost(ctx, "fx1", 0, "顺手")) },
		"Cancel":              func() error { return mustErr(nilSvc.Cancel(ctx, "fx1", 0)) },
		"Reopen":              func() error { return mustErr(nilSvc.Reopen(ctx, "fx1", 0)) },
		"MarkWonByCollection": func() error { return mustErr(nilSvc.MarkWonByCollection(ctx, "fx1")) },
	} {
		if err := call(); err == nil {
			t.Errorf("nil 服务上的 %s 不该成功", name)
		}
	}
}

func mustErr(_ *model.Opportunity, err error) error { return err }

// TestOpportunityService_HandsRepositoryColumnScaledValues 打的是**交给仓储的那一份**。
//
// 真库用例证不了这件事：numeric(12,2) 与 numeric(5,2) 自己会舍，本层舍不舍 PG 都舍，
// 库里读回来两边一模一样 —— 变异电池里"把落库前的舍入摘掉"这一条就是这么活下来的。
// 而这一格值得有判据：赢率算出来是 0.5999999999999999 还是 0.60，决定的是
// "阈值比较读到哪一个数"（ltc.config 的 win_probability 阈值与它同量程，见文件头），
// 不该取决于代码先撞上哪一份。所以这里绕开 PG，直接看本层写出去的那两个浮点值。
func TestOpportunityService_HandsRepositoryColumnScaledValues(t *testing.T) {
	ctx := context.Background()
	repo := &oppFakeRepo{row: &model.Opportunity{
		ID: "cs1", Code: "OPP-CS1", CustomerID: "cus_cs1",
		Stage: model.OpportunityStageNegotiation, Status: model.OpportunityStatusOpen,
		Amount: 100, Currency: "CNY", WinProbability: 0.75, OwnerUserID: "sales_a",
	}}
	svc := NewOpportunityService(repo)
	svc.SetClock(func() time.Time { return oppClockBase })

	// 谈判期 + 已过预计关单日 + 有人推进：0.75−0.15 在二进制里是 0.59999999999999997780。
	overdue := oppClockBase.Add(-24 * time.Hour)
	got, err := svc.Edit(ctx, "cs1", 0, OpportunityEdit{
		Amount: 12.3456, Currency: "USD", OwnerUserID: "sales_a", ExpectedCloseAt: &overdue,
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	written := repo.row
	if written.Amount != 12.35 {
		t.Errorf("交给仓储的金额是 %.20g，期望 12.35（两位小数）", written.Amount)
	}
	if written.WinProbability != 0.6 {
		t.Errorf("交给仓储的赢率是 %.20g，期望精确的 0.6", written.WinProbability)
	}
	// 版本不归本层管：+1 是仓储 CAS 语句做的事，就地改会让回读之前那份带一个假新版本。
	if written.Version != 0 {
		t.Errorf("本层把版本改成了 %d，期望仍是 0", written.Version)
	}
	if got.Amount != written.Amount || got.WinProbability != written.WinProbability {
		t.Errorf("返回体 (%.20g,%.20g) 与写出的一份 (%.20g,%.20g) 不同源",
			got.Amount, got.WinProbability, written.Amount, written.WinProbability)
	}
}

// —— T-P4-04 追加：只读入口、机器动作清单、全局登记 ————————————————

// TestOpportunityService_GetDoesNotHideOrInvent 只读入口的三条边界，每条都对应一种真实的误读：
//
//   - 读不到 ⇒ (nil, nil)。本层没有状态码可给，把"没有这一行"报成错误，出口就只能靠
//     匹配错误文案来区分 404 与 500 —— 而文案是最先被改写的那一样东西。
//   - 空 id ⇒ InputInvalid，且不碰库。空串在仓储那里是 `WHERE id = ”`：一次必然落空的查询，
//     于是坏请求与缺行共用同一个答案。
//   - 越界行 ⇒ **照样读得出来**。这是与 transition 刻意相反的一条口径：只读时把坏行说成
//     "不存在"，等于替读方决定这一行不该被看见，而坏数据恰恰要先能被看见才修得掉。
func TestOpportunityService_GetDoesNotHideOrInvent(t *testing.T) {
	ctx := context.Background()
	svc, repo, database := newOpportunityServiceWithDB(t)

	row := seedOpportunity(t, repo, "opp_get_ok", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	got, err := svc.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get(%s): %v", row.ID, err)
	}
	if got == nil {
		t.Fatal("刚种下的行读回来是 nil")
	}
	if got.Stage != row.Stage || got.Status != row.Status || got.WinProbability != 0.42 || got.Version != 0 {
		t.Errorf("读回的一行与种下的不是同一份：stage=%s status=%s win=%v version=%d",
			got.Stage, got.Status, got.WinProbability, got.Version)
	}

	got, err = svc.Get(ctx, "opp_get_absent")
	if err != nil {
		t.Errorf("缺行被报成错误：%v —— 「没有这一行」不是故障，出口要的是能返回 nil", err)
	}
	if got != nil {
		t.Errorf("缺行读出了内容：%v", got)
	}

	for _, blank := range []string{"", "   ", "\t"} {
		if _, err := svc.Get(ctx, blank); !errors.Is(err, ErrOpportunityInputInvalid) {
			t.Errorf("空 id %q 没有被判成入参不合法，得到 %v —— 它会变成库里一次必然落空的查询", blank, err)
		}
	}

	// 越界行：读口出声，写口拒改。两条同时断，才分得开"读不到"与"不敢改"。
	bad := seedOpportunity(t, repo, "opp_get_bad", model.OpportunityStageProposal, model.OpportunityStatusOpen)
	if err := database.Model(&model.Opportunity{}).Where("id = ?", bad.ID).
		Update("win_probability", 7.5).Error; err != nil { // 越出 0–1：仓储不拦，服务读口才看得见
		t.Fatalf("把赢率改越界: %v", err)
	}
	readable, err := svc.Get(ctx, bad.ID)
	if err != nil {
		t.Errorf("越界行读不出来：%v —— 坏数据先要被看见才修得掉", err)
	} else if readable == nil || readable.WinProbability != 7.5 {
		t.Errorf("越界行被读口藏起来了：%+v", readable)
	}
	if _, err := svc.MoveStage(ctx, bad.ID, 0, model.OpportunityStageNegotiation); !errors.Is(err, ErrOpportunityStateInvalid) {
		t.Errorf("同一行在写口得到 %v，期望 ErrOpportunityStateInvalid", err)
	}

	// 未装配：报错而不是返回 nil —— 这里"没句柄"与"没有这行"是两件事，混成一个
	// 出口就会对着未装配的底座回 404，把故障说成数据问题。
	if _, err := (&OpportunityService{}).Get(ctx, "anything"); err == nil {
		t.Error("未装配句柄的 Get 回了 nil 错误：出口分不清「读不到」与「没装底座」")
	}
	// 同一道关的另一臂：整个服务指针为 nil（GlobalOpportunityService() 未装配时就是这个值）。
	// 只测"有指针、没仓储"那一臂，`s == nil` 这半句就是没人验证过的装饰。
	var nilSvc *OpportunityService
	if _, err := nilSvc.Get(ctx, "anything"); err == nil {
		t.Error("nil 服务上的 Get 回了 nil 错误：这道关的第二臂没人守")
	}
}

// TestServerOnlyOpportunityMoves 只走机器动作那一条边的对外清单（AC③ 在契约面上的兑现）。
//
// /rules 里必须能读到"won 是存在的、但它不经 HTTP"这件事。清单为空 = 这套规则看起来
// 根本没有 won 这条路，前端会以为产品没有赢单；清单里冒出 HTTPExposed=true = 有人把
// 机器动作开成了按钮，那张 (来源,起点)→终点 的三元边表当场退化回二元表。
func TestServerOnlyOpportunityMoves(t *testing.T) {
	out := ServerOnlyOpportunityMoves()
	if len(out) == 0 {
		t.Fatal("机器动作清单为空：/rules 就说不清「won 存在但不经 HTTP」这件事")
	}
	for _, m := range out {
		if m.HTTPExposed {
			t.Errorf("机器动作 %s: %s→%s 被标成可经 HTTP 暴露：写 won 的入口只能来自回款完成", m.Cause, m.From, m.Target)
		}
		if m.Cause != OpportunityCauseCollection {
			t.Errorf("机器动作的 cause=%q，期望 %q：落到这个终点的来源只有这一个", m.Cause, OpportunityCauseCollection)
		}
		if m.Target != model.OpportunityStatusWon {
			t.Errorf("机器动作终点 %q 不是 won：边表里除它以外没有第二格是机器独占的", m.Target)
		}
		if m.Kind != OpportunityMoveStatus {
			t.Errorf("机器动作 kind=%q，期望 status：机器独占的是状态维，不是阶段维", m.Kind)
		}
		if got := AllowedOpportunityMoves(model.OpportunityStageNegotiation, m.From); containsOpportunityMove(got, OpportunityMoveStatus, m.Target) {
			t.Errorf("%s 同时出现在可请求动作与机器动作里：HTTP 侧多了一条能写 won 的路", m.Target)
		}
	}
}

func containsOpportunityMove(list []OpportunityMove, kind OpportunityMoveKind, target string) bool {
	for _, m := range list {
		if m.Kind == kind && m.Target == target {
			return true
		}
	}
	return false
}

// TestGlobalOpportunityServiceRoundTrip 全局登记处的三件事：写什么读什么、是同一份对象、
// 写 nil 真的清得回 nil。第三条是装配层"无 DB 句柄"分支的唯一判据 —— 清不干净时，
// 路由会对着一句"未装配"的告警继续回 200，那声告警就成了假的。
func TestGlobalOpportunityServiceRoundTrip(t *testing.T) {
	restore := GlobalOpportunityService()
	defer SetGlobalOpportunityService(restore)

	SetGlobalOpportunityService(nil)
	if got := GlobalOpportunityService(); got != nil {
		t.Errorf("清空之后全局仍是 %p", got)
	}

	svc, _, _ := newOpportunityServiceWithDB(t)
	SetGlobalOpportunityService(svc)
	if got := GlobalOpportunityService(); got != svc {
		t.Errorf("取回的不是登记的那一份（%p vs %p）：两套缓存口径迟早分家", got, svc)
	}

	SetGlobalOpportunityService(nil)
	if got := GlobalOpportunityService(); got != nil {
		t.Errorf("重复装配传 nil 没清掉上一份（%p）⇒ 路由会在底座缺失时继续回 200", got)
	}
}
