package app

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/service/confidence"
	"hivemtk-user/internal/service/humanize"
)

// wantConfidenceWired 是 confidence 组本轮接线的 5 条，逐条写死而不是从
// DefaultParamDefs() 过滤——理由同 wantMiscWired：过滤只能钉住"接上的都在本组内"，
// 拼错 key 时断言照样成立，真正漏接的那条没人发现。
//
// 注意这里只有 5 条不是 7 条：另两条 persona_* 的实现 persona_evaluator.go
// 已随 9a5f716c 整体删除，代码里没有等价能力可接，如实留在种子里标「未接线」。
var wantConfidenceWired = []string{
	"confidence.humanize_default_threshold",
	"confidence.intent_fewshot_min_cos",
	"confidence.weak_truth_min_confidence",
	"confidence.emb_retry_cooldown",
	"confidence.veto_low_rag_threshold",
}

// newConfidenceParamFixture 见 newMiscParamFixture；每组各建一个是因为
// t.Cleanup 还原全局单例，两个 fixture 不能并存于同一次运行。
func newConfidenceParamFixture(t *testing.T) *service.ConfigParamService {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no DB")
	}
	if err := service.SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := service.NewConfigParamService(database)
	service.SetGlobalForTest(svc)
	t.Cleanup(func() { service.SetGlobalForTest(nil) })
	return svc
}

// TestWireConfidenceConfigParamsKeys 装配点声明接线的键，必须与上面那 5 条逐条相等。
func TestWireConfidenceConfigParamsKeys(t *testing.T) {
	got := WireConfidenceConfigParams()
	if len(got) != len(wantConfidenceWired) {
		t.Fatalf("接线条数 = %d，期望 %d\ngot  = %v\nwant = %v", len(got), len(wantConfidenceWired), got, wantConfidenceWired)
	}
	seen := map[string]bool{}
	for i, k := range got {
		if k != wantConfidenceWired[i] {
			t.Errorf("第 %d 条接线 = %q，期望 %q", i, k, wantConfidenceWired[i])
		}
		if seen[k] {
			t.Errorf("键 %q 被接了两次：同一个点位注入两次，后一次覆盖前一次，读点会跟着错位", k)
		}
		seen[k] = true
	}
}

// TestWiredConfidenceKeysExistInSeed 上面那 5 条必须在种子表里真实存在。
// 引用一个没登记的键 = 运行期永远读兜底值 + 管理台上看不见这一行，
// 和当初那 72 条僵尸一模一样的病。
func TestWiredConfidenceKeysExistInSeed(t *testing.T) {
	index := map[string]bool{}
	for _, d := range service.DefaultParamDefs() {
		index[d.Group+"."+d.Key] = true
	}
	for _, k := range wantConfidenceWired {
		if !index[k] {
			t.Errorf("%q 在 DefaultParamDefs() 里不存在：接了线但没有参数行，改了也没人看得到", k)
		}
	}
}

// TestWireConfidenceConfigParamsReachesReaders 装配完之后，参数中心的值必须真的
// 出现在读取点上。只断言 provider 被 setter 收下没有意义——那证明不了
// 「运维在页面上改值真的改变了行为」。
func TestWireConfidenceConfigParamsReachesReaders(t *testing.T) {
	svc := newConfidenceParamFixture(t)
	ctx := context.Background()

	// 先记住"没装配"时各读取口的值：接上之后它们必须被参数中心的值顶掉。
	beforeThreshold := humanize.EffectiveThreshold()
	beforeFewShot := service.ProbeFewShotMinCos()
	beforeWeakTruth := service.ProbeWeakTruthMinConfidence()
	beforeCooldown := service.ProbeEmbRetryCooldown()
	beforeVeto := confidence.ProbeVetoLowRAGThreshold()

	WireConfidenceConfigParams()

	for _, tc := range []struct {
		key, val string
	}{
		{"humanize_default_threshold", "0.5"},
		{"intent_fewshot_min_cos", "0.9"},
		{"weak_truth_min_confidence", "0.4"},
		{"emb_retry_cooldown", "120"},
		{"veto_low_rag_threshold", "0.3"},
	} {
		if err := svc.UpdateValue(ctx, "confidence", tc.key, tc.val, 1); err != nil {
			t.Fatalf("改 confidence.%s 失败：%v", tc.key, err)
		}
	}

	if got, want := humanize.EffectiveThreshold(), 0.5; got != want {
		t.Errorf("humanize_default_threshold 未被读走：got %v（原 %v）want %v", got, beforeThreshold, want)
	}
	if got, want := service.ProbeFewShotMinCos(), 0.9; got != want {
		t.Errorf("intent_fewshot_min_cos 未被读走：got %v（原 %v）want %v", got, beforeFewShot, want)
	}
	if got, want := service.ProbeWeakTruthMinConfidence(), 0.4; got != want {
		t.Errorf("weak_truth_min_confidence 未被读走：got %v（原 %v）want %v", got, beforeWeakTruth, want)
	}
	if got, want := service.ProbeEmbRetryCooldown(), 120*time.Second; got != want {
		t.Errorf("emb_retry_cooldown 未被读走：got %v（原 %v）want %v", got, beforeCooldown, want)
	}
	if got, want := confidence.ProbeVetoLowRAGThreshold(), 0.3; got != want {
		t.Errorf("veto_low_rag_threshold 未被读走：got %v（原 %v）want %v", got, beforeVeto, want)
	}
}

// TestWireConfidenceConfigParamsWithoutDB 参数表不存在时（没跑迁移、或装配早于建表），
// 接线必须仍把各点位托在代码兜底值上，而不是返回一个 nil 函数在首次请求时炸掉。
func TestWireConfidenceConfigParamsWithoutDB(t *testing.T) {
	service.SetGlobalForTest(nil)

	if got := WireConfidenceConfigParams(); len(got) != len(wantConfidenceWired) {
		t.Fatalf("无库时接线条数 = %d，期望 %d（接线不该依赖库是否可用）", len(got), len(wantConfidenceWired))
	}
	if got, want := humanize.EffectiveThreshold(), humanize.DefaultThreshold; got != want {
		t.Errorf("无库时 humanize_default_threshold = %v，期望兜底 %v", got, want)
	}
	if got, want := service.ProbeFewShotMinCos(), service.FewShotMinCosDefault; got != want {
		t.Errorf("无库时 intent_fewshot_min_cos = %v，期望兜底 %v", got, want)
	}
	if got, want := service.ProbeWeakTruthMinConfidence(), service.WeakTruthMinConfidence; got != want {
		t.Errorf("无库时 weak_truth_min_confidence = %v，期望兜底 %v", got, want)
	}
	if got, want := service.ProbeEmbRetryCooldown(), service.EmbRetryCooldownDefault; got != want {
		t.Errorf("无库时 emb_retry_cooldown = %v，期望兜底 %v", got, want)
	}
	if got, want := confidence.ProbeVetoLowRAGThreshold(), confidence.DefaultVetoLowRAGThreshold; got != want {
		t.Errorf("无库时 veto_low_rag_threshold = %v，期望兜底 %v", got, want)
	}
}
