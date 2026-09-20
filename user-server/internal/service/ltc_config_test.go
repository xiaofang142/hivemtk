// ltc_config_test.go T-P3-06（LTC-25 运营一键开启落点定义）。
//
// 这张卡的验收对象是"开关落点"，所以测试盯的是三件容易在实现里悄悄变形的事：
// ① 两道锁（总开关 × 阶段开关）不许塌成一道；
// ② C5 裁定的"两个独立闸门"必须是结构上不可合并的（阈值置 0 = 拆掉那道闸，直接拒）；
// ③ 读侧任何异常都fail-closed 全关，且"读不动"与"运营关了"在响应里不能长同一个样。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// ---- 测试替身 ---------------------------------------------------------------

// fakeKV 是 SystemConfigKVRepository 的内存替身，带读错注入与调用计数。
type fakeKV struct {
	rows   map[string]string
	getErr error
	calls  int
}

func newFakeKV() *fakeKV { return &fakeKV{rows: map[string]string{}} }

func (f *fakeKV) Available() bool { return true }

func (f *fakeKV) Get(ctx context.Context, key string) (string, error) {
	f.calls++
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.rows[key], nil
}

func (f *fakeKV) Upsert(ctx context.Context, key, value string) (string, error) {
	f.rows[key] = value
	return value, nil
}

func (f *fakeKV) EnsureTable(ctx context.Context) error { return nil }

var _ ltcKVStore = (*fakeKV)(nil)

// ---- AC：默认全关 ----------------------------------------------------------

func TestLTCConfig_DefaultsAreFullyOff(t *testing.T) {
	def := DefaultLTCConfig()
	if def.Enabled {
		t.Error("默认 enabled=true：本卡的第一条承诺就是开箱不用，运营必须显式打开")
	}
	for _, st := range LTCKnownStages {
		if def.StageEnabled(st) {
			t.Errorf("默认下阶段 %s 已开", st)
		}
	}
	// 默认值自身必须过校验，否则"重置为默认"这条路会立刻自相矛盾。
	if err := def.Validate(); err != nil {
		t.Errorf("默认配置过不了自己的校验器：%v", err)
	}
}

// ---- AC①②：两道锁 --------------------------------------------------------

func TestLTCConfig_TwoLocksNotCollapsed(t *testing.T) {
	ctx := context.Background()
	t.Run("总开关关 ⇒ 阶段开关独立为 true 也不生效", func(t *testing.T) {
		svc := newLTCSvcWithKV(t, &LTCConfig{
			Enabled:       false,
			StagesEnabled: LTCStages{Opportunity: true, Quote: true},
			Thresholds:    DefaultLTCConfig().Thresholds,
		})
		for _, st := range LTCKnownStages {
			active, why := svc.StageActive(ctx, st)
			if active {
				t.Errorf("总开关关着，阶段 %s 却生效了", st)
			}
			if why != LTCReasonMasterOff {
				t.Errorf("阶段 %s 未生效的原因=%q，期望 %q（两把锁锁在哪一把上要能分辨）",
					st, why, LTCReasonMasterOff)
			}
		}
	})

	t.Run("总开关开 ⇒ 只有显式打开的阶段生效", func(t *testing.T) {
		svc := newLTCSvcWithKV(t, &LTCConfig{
			Enabled:       true,
			StagesEnabled: LTCStages{Quote: true},
			Thresholds:    DefaultLTCConfig().Thresholds,
		})
		active, _ := svc.StageActive(ctx, LTCStageQuote)
		if !active {
			t.Error("quote 已显式打开却不生效")
		}
		for _, st := range LTCKnownStages {
			if st == LTCStageQuote {
				continue
			}
			got, why := svc.StageActive(ctx, st)
			if got {
				t.Errorf("阶段 %s 没打开却生效", st)
			}
			if why != LTCReasonStageOff {
				t.Errorf("阶段 %s 原因=%q，期望 %q", st, why, LTCReasonStageOff)
			}
		}
	})

	t.Run("未知阶段名 fail-closed", func(t *testing.T) {
		svc := newLTCSvcWithKV(t, &LTCConfig{Enabled: true, StagesEnabled: LTCStages{Quote: true}, Thresholds: DefaultLTCConfig().Thresholds})
		active, why := svc.StageActive(ctx, LTCStage("nonsense"))
		if active {
			t.Error("未知阶段名被判成生效：拼错的配置键会静默放行")
		}
		if why != LTCReasonUnknownStage {
			t.Errorf("原因=%q，期望 %q", why, LTCReasonUnknownStage)
		}
	})
}

// newLTCSvcWithKV 造一个只读内存 KV 的服务实例（不碰 DB）。
func newLTCSvcWithKV(t *testing.T, cfg *LTCConfig) *LTCConfigService {
	t.Helper()
	kv := newFakeKV()
	if cfg != nil {
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("夹具序列化失败：%v", err)
		}
		kv.rows[LTCConfigKVKey] = string(b)
	}
	return NewLTCConfigServiceWithStore(kv)
}

// ---- AC②：表驱动的阶段独立性 ----------------------------------------------

func TestLTCStage_EachStageTurnsOnAlone(t *testing.T) {
	ctx := context.Background()
	for _, want := range LTCKnownStages {
		t.Run(string(want), func(t *testing.T) {
			svc := newLTCSvcWithKV(t, &LTCConfig{
				Enabled:       true,
				StagesEnabled: LTCStages{}.With(want),
				Thresholds:    DefaultLTCConfig().Thresholds,
			})
			var actives []string
			for _, st := range LTCKnownStages {
				if ok, _ := svc.StageActive(ctx, st); ok {
					actives = append(actives, string(st))
				}
			}
			if len(actives) != 1 || actives[0] != string(want) {
				t.Errorf("只打开 %s 时生效集合=%v，期望恰好 [%s]", want, actives, want)
			}
		})
	}
}

func TestLTCKnownStages_MatchConfigShape(t *testing.T) {
	// 阶段清单与 JSON 形状必须同集：漏一个键 = 那个阶段永远打不开（静默失效）。
	b, err := json.Marshal(DefaultLTCConfig().StagesEnabled)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]bool
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(LTCKnownStages) {
		t.Errorf("JSON 阶段键 %d 个，LTCKnownStages %d 个：两边不同集", len(decoded), len(LTCKnownStages))
	}
	for _, st := range LTCKnownStages {
		if _, ok := decoded[string(st)]; !ok {
			t.Errorf("阶段 %s 在 JSON 里没有对应键", st)
		}
	}
}

// ---- AC③：非法值被拒 ------------------------------------------------------

// ltcBody 造一份除注入的那处缺陷外**完全合法**的策略文本。
//
// 每条用例只留一处坏：`{"enabled":true,"thresholds":{"score":80}}` 这种"啥都缺"的夹具
// 会被更早的一条校验挡下，测试绿了却没人证明过"合并分数"这条规则真的在生效。
func ltcBody(stages, thresholds string) string {
	return `{"enabled":true,"stages_enabled":` + stages + `,"thresholds":` + thresholds + `}`
}

const ltcValidThresholds = `{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}`

func TestLTCConfig_ParseRejectsUnknownAndMergedFields(t *testing.T) {
	cases := []struct{ name, body, wantMention string }{
		{"阶段名拼错", ltcBody(`{"colleciton":true}`, ltcValidThresholds), "colleciton"},
		{"合并成一个分数", ltcBody(`{"quote":true}`, `{"score":80,"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}`), "score"},
		{"把两个闸门合成一个", ltcBody(`{"quote":true}`, `{"lead_confidence":70,"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}`), "lead_confidence"},
		{"顶层塞进别人管着的键", `{"enabled":true,"default_allow":true,"stages_enabled":{"quote":true},"thresholds":` + ltcValidThresholds + `}`, "default_allow"},
		{"整个 thresholds 缺省", `{"enabled":true,"stages_enabled":{"quote":true}}`, "thresholds"},
		{"整个 stages_enabled 缺省", `{"enabled":true,"thresholds":` + ltcValidThresholds + `}`, "stages_enabled"},
		{"不是对象", `[{"enabled":true}]`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseLTCConfig([]byte(tc.body))
			if err == nil {
				t.Fatalf("非法字段被静默接受：%s", tc.body)
			}
			if !strings.Contains(err.Error(), tc.wantMention) {
				t.Errorf("错误里没有点名 %q：%v（运营看不懂哪一行写错了）", tc.wantMention, err)
			}
		})
	}
}

// 边界表是给管理端输入框渲染用的，它必须和 Validate 说的是同一件事：
// 按表里的 min 填要存得下，比 min 再小一点、比 max 再大一点都必须被拒。
// 表与校验各写一份数字时，漂移的方向恰好是"表单允许、后端拒收"，
// 运营在同一个输入框里看到第三种报错 —— 这条断的是那两个数字有没有同源。
func TestLTCConfig_ThresholdBoundsAgreeWithValidate(t *testing.T) {
	set := func(cfg *LTCConfig, key string, v float64) {
		switch key {
		case "lead_score":
			cfg.Thresholds.LeadScore = int(v)
		case "confidence":
			cfg.Thresholds.Confidence = v
		case "discount_percent":
			cfg.Thresholds.DiscountPercent = v
		case "win_probability":
			cfg.Thresholds.WinProbability = v
		default:
			t.Fatalf("边界表里冒出未知阈值键 %s", key)
		}
	}
	step := func(key string) float64 {
		if key == "lead_score" {
			return 0.5 // 取整后正好落到 min 之下（1 → 0）
		}
		return 0.0005
	}

	if len(LTCKnownThresholdKeys) != 4 {
		t.Fatalf("阈值键数量=%d，期望 4（表与解析必须共用同一份键清单）", len(LTCKnownThresholdKeys))
	}
	for _, key := range LTCKnownThresholdKeys {
		b, ok := LTCThresholdBoundOf(key)
		if !ok {
			t.Fatalf("阈值 %s 没有边界定义：管理端拿不到输入范围，只能凭记忆填", key)
		}

		atMin := *DefaultLTCConfig()
		set(&atMin, key, b.Min)
		if err := atMin.Validate(); err != nil {
			t.Errorf("%s 取表里的 min=%v 却被 Validate 拒了：%v", key, b.Min, err)
		}
		atMax := *DefaultLTCConfig()
		set(&atMax, key, b.Max)
		if err := atMax.Validate(); err != nil {
			t.Errorf("%s 取表里的 max=%v 却被 Validate 拒了：%v", key, b.Max, err)
		}

		below := *DefaultLTCConfig()
		set(&below, key, b.Min-step(key))
		if err := below.Validate(); err == nil {
			t.Errorf("%s 低于 min（%v）仍被 Validate 收下：闸门比表单松", key, b.Min)
		}
		above := *DefaultLTCConfig()
		set(&above, key, b.Max+1)
		if err := above.Validate(); err == nil {
			t.Errorf("%s 越过 max（%v）仍被 Validate 收下", key, b.Max)
		}

		// 0 这个数在四个键上方向不同：折扣的 0 是"任何折扣都要审批"（朝严），
		// 另三个的 0 是拆闸（朝松）。表里说合法，校验就必须真放行。
		zero := *DefaultLTCConfig()
		set(&zero, key, 0)
		err := zero.Validate()
		if b.ZeroLegal && err != nil {
			t.Errorf("%s 表里标 0 合法，Validate 却拒了：%v", key, err)
		}
		if !b.ZeroLegal && err == nil {
			t.Errorf("%s 表里标 0 非法，Validate 却放行了：这道闸门被拆了", key)
		}
	}
}

// 超大浮点字面量必须被拒，而不是折成 +Inf 后一路通过。
//
// 这条不是臆想出来的担心：+Inf 落在 confidence 上是"这道闸门永不通过"，
// 落在 discount_percent 上是"审批永不触发"，同一个坏值在两个字段上方向相反，
// 静默折算等于让运营在毫不知情的情况下拆掉一道、焊死另一道。
func TestLTCConfig_ParseRejectsNonFiniteNumbers(t *testing.T) {
	cases := []struct{ body, wantMention string }{
		{ltcBody(`{"quote":true}`, `{"lead_score":70,"confidence":1e999,"discount_percent":10,"win_probability":0.5}`), "confidence"},
		{ltcBody(`{"quote":true}`, `{"lead_score":70,"confidence":0.8,"discount_percent":1e999,"win_probability":0.5}`), "discount_percent"},
		{ltcBody(`{"quote":true}`, `{"lead_score":1e999,"confidence":0.8,"discount_percent":10,"win_probability":0.5}`), "lead_score"},
	}
	for _, tc := range cases {
		cfg, err := ParseLTCConfig([]byte(tc.body))
		if err == nil {
			t.Fatalf("非有限数值被接受：%s ⇒ %+v", tc.body, cfg.Thresholds)
		}
		if !strings.Contains(err.Error(), tc.wantMention) {
			t.Errorf("错误未点名出问题的字段 %s：%v", tc.wantMention, err)
		}
	}
}

func TestLTCConfig_ValidateThresholds(t *testing.T) {
	base := DefaultLTCConfig()
	cases := []struct {
		name  string
		mut   func(*LTCConfig)
		match string
	}{
		{"lead_score 为负", func(c *LTCConfig) { c.Thresholds.LeadScore = -1 }, "lead_score"},
		{"lead_score 越界", func(c *LTCConfig) { c.Thresholds.LeadScore = 101 }, "lead_score"},
		{"confidence > 1", func(c *LTCConfig) { c.Thresholds.Confidence = 1.5 }, "confidence"},
		{"confidence 为负", func(c *LTCConfig) { c.Thresholds.Confidence = -0.01 }, "confidence"},
		{"win_probability 越界", func(c *LTCConfig) { c.Thresholds.WinProbability = 2 }, "win_probability"},
		{"discount_percent 越界", func(c *LTCConfig) { c.Thresholds.DiscountPercent = 120 }, "discount_percent"},
		{"confidence NaN", func(c *LTCConfig) { c.Thresholds.Confidence = math.NaN() }, "confidence"},
		{"win_probability 为 +Inf", func(c *LTCConfig) { c.Thresholds.WinProbability = math.Inf(1) }, "win_probability"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *base
			tc.mut(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("非法阈值通过了校验：%+v", cfg.Thresholds)
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Errorf("错误未点名 %s：%v", tc.match, err)
			}
		})
	}
}

// ---- AC④：两个闸门不可合并（0 = 拆闸）------------------------------------

// 缺键必须报出来：阈值缺省会塌成 0，而 0 在 C5 的语义里等于"这道闸门不要了"。
func TestLTCConfig_ParseRequiresEveryThreshold(t *testing.T) {
	partial := `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10}}`
	_, err := ParseLTCConfig([]byte(partial))
	if err == nil {
		t.Fatal("少写 win_probability 被当成合法：缺键会静默塌成 0")
	}
	if !strings.Contains(err.Error(), "win_probability") {
		t.Errorf("错误未点名缺的那个键：%v", err)
	}

	full := `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`
	cfg, err := ParseLTCConfig([]byte(full))
	if err != nil {
		t.Fatalf("完整合法的一份被拒：%v", err)
	}
	if !cfg.StageEnabled(LTCStageQuote) {
		t.Error("解析结果不对")
	}
}

// 边界值表：范围端点与"0 是否合法"逐条钉住（discount_percent 的 0 是"任何折扣都要审批"，
// 与另三个的"0 = 拆闸"方向相反，故必须分开判）。
func TestLTCConfig_ValidateAcceptsDocumentedBoundaries(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*LTCConfig)
	}{
		{"lead_score 下界 1", func(c *LTCConfig) { c.Thresholds.LeadScore = 1 }},
		{"lead_score 上界 100", func(c *LTCConfig) { c.Thresholds.LeadScore = 100 }},
		{"confidence 上界 1", func(c *LTCConfig) { c.Thresholds.Confidence = 1 }},
		{"confidence 下界最小正数", func(c *LTCConfig) { c.Thresholds.Confidence = 0.001 }},
		{"win_probability 上界 1", func(c *LTCConfig) { c.Thresholds.WinProbability = 1 }},
		{"discount_percent 0 = 最严档", func(c *LTCConfig) { c.Thresholds.DiscountPercent = 0 }},
		{"discount_percent 100", func(c *LTCConfig) { c.Thresholds.DiscountPercent = 100 }},
		{"关着时默认阈值仍须自洽", func(c *LTCConfig) { c.Enabled = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *DefaultLTCConfig()
			cfg.Enabled = true
			tc.mut(&cfg)
			if err := cfg.Validate(); err != nil {
				t.Errorf("合法边界被拒：%v", err)
			}
		})
	}
}

func TestLTCConfig_ZeroThresholdDisablesAGateIsRejected(t *testing.T) {
	// 阈值设成 0 = 该闸门恒通过 = 把 C5 的两个闸门塌回一个。
	// 校验与总开关无关：关着时存进去的坏阈值，将来抬开关会带着一颗已拆掉的闸门上线。
	for _, tc := range []struct {
		name  string
		mut   func(*LTCConfig)
		match string
	}{
		{"lead_score=0", func(c *LTCConfig) { c.Thresholds.LeadScore = 0 }, "lead_score"},
		{"confidence=0", func(c *LTCConfig) { c.Thresholds.Confidence = 0 }, "confidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *DefaultLTCConfig()
			cfg.Enabled = true
			tc.mut(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("阈值为 0 被接受：这道闸门等于被拆掉，C5 的两个独立闸门塌成一个")
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Errorf("错误未点名 %s：%v", tc.match, err)
			}
		})
	}

	t.Run("关着的时候阈值仍然必须合法", func(t *testing.T) {
		cfg := DefaultLTCConfig()
		cfg.Enabled = false
		cfg.Thresholds.LeadScore = 0
		if err := cfg.Validate(); err == nil {
			t.Error("enabled=false 时阈值 0 也被接受：将来抬总开关会带着一颗拆掉的闸门上线")
		}
	})
}

func TestLTCLeadQualified_NeedsBothGates(t *testing.T) {
	cfg := DefaultLTCConfig()
	cfg.Enabled = true
	cfg.Thresholds.LeadScore = 70
	cfg.Thresholds.Confidence = 0.8

	cases := []struct {
		score  int
		conf   float64
		want   bool
		reason string
	}{
		{90, 0.95, true, ""},
		{90, 0.30, false, "confidence"}, // 高价值客户 + 低置信回答 ⇒ 不能过（C5 的理由本身）
		{20, 0.95, false, "lead_score"}, // 高置信 + 低价值线索 ⇒ 不能过
		{70, 0.8, true, ""},             // 边界是 ≥，不是 >
		{69, 0.8, false, "lead_score"},
		{70, 0.799, false, "confidence"},
	}
	for _, tc := range cases {
		got, why := cfg.LeadQualified(tc.score, tc.conf)
		if got != tc.want {
			t.Errorf("LeadQualified(%v,%v)=%v(%q)，期望 %v(%q)", tc.score, tc.conf, got, why, tc.want, tc.reason)
		}
		if !tc.want && !strings.Contains(why, tc.reason) {
			t.Errorf("未过闸的理由=%q，应点名是哪一道闸（%s）拦的", why, tc.reason)
		}
	}
}

// ---- 读侧优先级与 fail-closed ---------------------------------------------

func TestLTCConfig_ReadPriority(t *testing.T) {
	ctx := context.Background()

	t.Run("KV 为空 ⇒ 默认全关", func(t *testing.T) {
		svc := NewLTCConfigServiceWithStore(newFakeKV())
		cfg := svc.Config(ctx)
		if cfg.Enabled {
			t.Error("没有配置行却读到 enabled=true")
		}
		if cfg.Degraded {
			t.Error("正常空态被标成降级：运营会把「没配过」读成「配置坏了」")
		}
	})

	t.Run("KV 里是坏 JSON ⇒ 全关 + 降级可见", func(t *testing.T) {
		kv := newFakeKV()
		kv.rows[LTCConfigKVKey] = `{"enabled":true,"stages_enabled":{`
		svc := NewLTCConfigServiceWithStore(kv)
		cfg := svc.Config(ctx)
		if cfg.Enabled || cfg.StageEnabled(LTCStageQuote) {
			t.Error("坏 JSON 里的 true 被采信了：解析失败必须整份作废")
		}
		if !cfg.Degraded || cfg.DegradeReason == "" {
			t.Errorf("降级未可见：degraded=%v reason=%q", cfg.Degraded, cfg.DegradeReason)
		}
	})

	t.Run("KV 里是合法 JSON 但阈值非法 ⇒ 同样全关", func(t *testing.T) {
		kv := newFakeKV()
		kv.rows[LTCConfigKVKey] = `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":900,"confidence":0.9,"discount_percent":10,"win_probability":0.5}}`
		svc := NewLTCConfigServiceWithStore(kv)
		cfg := svc.Config(ctx)
		if cfg.Enabled {
			t.Error("非法阈值的一份配置被整份采信：应当回落默认全关")
		}
		if !cfg.Degraded {
			t.Error("缺 degraded 标记：读侧回落在响应上必须看得见")
		}
	})

	t.Run("读 KV 报错 ⇒ 全关 + 降级（fail-closed）", func(t *testing.T) {
		kv := newFakeKV()
		kv.getErr = errors.New("connection reset")
		svc := NewLTCConfigServiceWithStore(kv)
		cfg := svc.Config(ctx)
		if cfg.Enabled {
			t.Error("读不动却当作已启用")
		}
		if !cfg.Degraded || !strings.Contains(cfg.DegradeReason, "connection reset") {
			t.Errorf("读失败原因没带上：degraded=%v reason=%q", cfg.Degraded, cfg.DegradeReason)
		}
	})
}

// ---- 缓存：TTL 与写后失效 --------------------------------------------------

func TestLTCConfig_CacheTTLBoundsConvergence(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	kv.rows[LTCConfigKVKey] = `{"enabled":false,"stages_enabled":{"quote":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`
	svc := NewLTCConfigServiceWithStore(kv)
	clock := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	svc.nowFn = func() time.Time { return clock }

	before := kv.calls
	if svc.Config(ctx).Enabled {
		t.Fatal("夹具里写的是 enabled=false")
	}
	if svc.Config(ctx).Enabled {
		t.Fatal("第二次读也不该变")
	}
	if kv.calls != before+1 {
		t.Errorf("TTL 内读了 %d 次库，期望 1 次（其余走缓存）", kv.calls-before)
	}

	// 另一个副本改了库：本进程必须在不重启的前提下最迟一个 TTL 后收敛。
	kv.rows[LTCConfigKVKey] = `{"enabled":true,"stages_enabled":{"quote":true},"thresholds":{"lead_score":70,"confidence":0.8,"discount_percent":10,"win_probability":0.5}}`
	if svc.Config(ctx).Enabled {
		t.Fatal("缓存还没过期就看到新值：TTL 形同虚设")
	}
	clock = clock.Add(LTCConfigCacheTTL + time.Second)
	if !svc.Config(ctx).Enabled {
		t.Errorf("超过 TTL 后仍未收敛：多副本下开关最迟 %s 生效是这张卡的对外承诺", LTCConfigCacheTTL)
	}
}

func TestLTCConfig_SaveInvalidatesOwnCache(t *testing.T) {
	ctx := context.Background()
	svc := newLTCSvcWithKV(t, nil)
	if svc.Config(ctx).Enabled {
		t.Fatal("初始应全关")
	}
	next := *DefaultLTCConfig()
	next.Enabled = true
	next.StagesEnabled = LTCStages{Opportunity: true}
	if _, err := svc.Save(ctx, &next, 7); err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	cfg := svc.Config(ctx)
	if !cfg.Enabled || !cfg.StageEnabled(LTCStageOpportunity) {
		t.Errorf("同进程保存后仍是旧值：enabled=%v opportunity=%v", cfg.Enabled, cfg.StageEnabled(LTCStageOpportunity))
	}
}

// ---- 落库：真表真句柄 ------------------------------------------------------

func setupLTCConfigDB(t *testing.T) *gorm.DB {
	t.Helper()
	prev := db.GetDB()
	database := testutil.NewTestDB(t, &model.SystemConfigKV{}, &model.OperationLog{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(prev) })
	return database
}

func TestLTCConfig_SavePersistsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	setupLTCConfigDB(t)

	svc := NewLTCConfigService()
	want := *DefaultLTCConfig()
	want.Enabled = true
	want.StagesEnabled = LTCStages{Bill: true, Payment: true}
	want.Thresholds.DiscountPercent = 15
	if _, err := svc.Save(ctx, &want, 42); err != nil {
		t.Fatalf("Save 失败：%v", err)
	}

	// 换一个实例 = 模拟进程重启：不重启读不回来说明只写了缓存。
	reopened := NewLTCConfigService()
	got := reopened.Config(ctx)
	if !got.Enabled || !got.StageEnabled(LTCStageBill) || !got.StageEnabled(LTCStagePayment) {
		t.Fatalf("重启后读不回：enabled=%v bill=%v payment=%v", got.Enabled, got.StageEnabled(LTCStageBill), got.StageEnabled(LTCStagePayment))
	}
	if got.StageEnabled(LTCStageQuote) {
		t.Error("没打开的 quote 读回来是开的")
	}
	if got.Thresholds.DiscountPercent != 15 {
		t.Errorf("discount_percent=%v，期望 15", got.Thresholds.DiscountPercent)
	}
}

func TestLTCConfig_SaveIllegalLeavesStoreUntouched(t *testing.T) {
	ctx := context.Background()
	database := setupLTCConfigDB(t)

	svc := NewLTCConfigService()
	good := *DefaultLTCConfig()
	good.Enabled = true
	good.StagesEnabled = LTCStages{Outreach: true}
	if _, err := svc.Save(ctx, &good, 1); err != nil {
		t.Fatal(err)
	}
	oldRaw, err := rawKVValue(ctx, database, LTCConfigKVKey)
	if err != nil {
		t.Fatal(err)
	}
	auditBefore := countOperationLogs(ctx, database)

	bad := good
	bad.Thresholds.LeadScore = 500
	if _, err := svc.Save(ctx, &bad, 2); err == nil {
		t.Fatal("非法阈值被保存")
	}
	newRaw, err := rawKVValue(ctx, database, LTCConfigKVKey)
	if err != nil {
		t.Fatal(err)
	}
	if newRaw != oldRaw {
		t.Errorf("校验失败仍改写了 KV：old=%s new=%s", oldRaw, newRaw)
	}
	if got := countOperationLogs(ctx, database); got != auditBefore {
		t.Errorf("校验失败却留下 %d 条审计（原来 %d）：被拒的写入不该伪装成发生过", got-auditBefore, auditBefore)
	}
	// 缓存也不能被脏值污染。
	if svc.Config(ctx).Thresholds.LeadScore != good.Thresholds.LeadScore {
		t.Error("被拒的配置进了缓存")
	}
}

func TestLTCConfig_SaveWritesOperationLogWithActor(t *testing.T) {
	ctx := context.Background()
	database := setupLTCConfigDB(t)
	svc := NewLTCConfigService()
	cfg := *DefaultLTCConfig()
	cfg.Enabled = true
	cfg.StagesEnabled = LTCStages{Quote: true}
	if _, err := svc.Save(ctx, &cfg, 9001); err != nil {
		t.Fatal(err)
	}

	var row model.OperationLog
	if err := database.Where("module = ?", LTCConfigAuditModule).First(&row).Error; err != nil {
		t.Fatalf("没有留下运营开关变更审计：%v（这张卡的开关是「谁点的」必须有答案的）", err)
	}
	if row.UserID != 9001 {
		t.Errorf("审计 actor=%d，期望 9001", row.UserID)
	}
	if !strings.Contains(row.Detail, string(LTCStageQuote)) {
		t.Errorf("审计 detail=%q 没记录开了哪个阶段", row.Detail)
	}
}

// failingAudit 是一个必然写失败的审计实现，用来钉住"开关已生效、审计没落下"这一支。
type failingAudit struct{ calls int }

func (a *failingAudit) Create(ctx context.Context, log *model.OperationLog) error {
	a.calls++
	return errors.New("operation_logs 写入失败：connection reset")
}

// 审计失败不能反过来把已经写进库的开关伪装成"没发生过"：回滚会制造第二次不一致，
// 而运营再点一次会变成两次变更。正确形态是 200 + persisted=true + audit_written=false。
func TestLTCConfig_AuditFailureStillReportsPersistedAndSaysWhy(t *testing.T) {
	ctx := context.Background()
	svc := newLTCSvcWithKV(t, nil)
	aud := &failingAudit{}
	svc.audit = aud

	cfg := *DefaultLTCConfig()
	cfg.Enabled = true
	cfg.StagesEnabled = LTCStages{Payment: true}
	res, err := svc.Save(ctx, &cfg, 5)
	if err != nil {
		t.Fatalf("审计写失败被升级成整次保存失败：%v", err)
	}
	if aud.calls != 1 {
		t.Errorf("审计被调用 %d 次，期望 1 次", aud.calls)
	}
	if !res.Persisted {
		t.Error("persisted=false：KV 里那份是写进去了的，读数会被带偏")
	}
	if res.AuditWritten {
		t.Error("audit_written=true：失败被吞掉，operation_logs 里根本没有这一行")
	}
	if !strings.Contains(res.AuditError, "connection reset") {
		t.Errorf("audit_error=%q，没带上底层原因", res.AuditError)
	}
	if !svc.Config(ctx).StageEnabled(LTCStagePayment) {
		t.Error("保存后的读侧没反映这次变更")
	}
}

// 仓储压根没装配（只注入了存储、没注入审计）时同样要显式可见，
// 而不是静默补装一个全局仓储 —— 那等于把"没有审计"渲染成"审计写成功"。
func TestLTCConfig_SaveWithoutAuditSinkIsVisible(t *testing.T) {
	ctx := context.Background()
	svc := newLTCSvcWithKV(t, nil)
	cfg := *DefaultLTCConfig()
	cfg.Enabled = true
	res, err := svc.Save(ctx, &cfg, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Persisted || res.AuditWritten || res.AuditError == "" {
		t.Errorf("persisted=%v audit_written=%v audit_error=%q，期望 true/false/非空",
			res.Persisted, res.AuditWritten, res.AuditError)
	}
}

// 读路径的故障元信息（degraded / source）是 `json:"-"` 的：这三个标签是承重的，
// 一旦漂成普通字段，"这一次读坏了"就会被写回库里继承给下一个副本。
func TestLTCConfig_SaveAfterDegradedReadDoesNotPersistFaultState(t *testing.T) {
	ctx := context.Background()
	kv := newFakeKV()
	kv.getErr = errors.New("connection reset")
	svc := NewLTCConfigServiceWithStore(kv)
	if !svc.Config(ctx).Degraded {
		t.Fatal("夹具没能造出降级读，这条测试等于没跑")
	}

	kv.getErr = nil
	if _, err := svc.Save(ctx, svc.Config(ctx), 8); err != nil {
		t.Fatal(err)
	}
	stored := kv.rows[LTCConfigKVKey]
	for _, leak := range []string{"degraded", "source", "connection reset"} {
		if strings.Contains(stored, leak) {
			t.Errorf("存回去的 JSON 里出现了 %q：%s", leak, stored)
		}
	}
	back, err := ParseLTCConfig([]byte(stored))
	if err != nil {
		t.Fatalf("自己存回去的东西读不回来：%v\n%s", err, stored)
	}
	if back.Degraded {
		t.Error("回读结果被判成降级")
	}
}

// 无库句柄时的写失败必须带上 ErrLTCStoreUnavailable：HTTP 层据此分 503 与 400。
// 回成 400 会让运营去改一个根本没写错的表单。
func TestLTCConfig_SaveWithoutStoreIsAStorageFailure(t *testing.T) {
	prev := db.GetDB()
	db.SetTestDB(nil)
	t.Cleanup(func() { db.SetTestDB(prev) })

	svc := NewLTCConfigService()
	cfg := *DefaultLTCConfig()
	cfg.Enabled = true
	if _, err := svc.Save(context.Background(), &cfg, 3); !errors.Is(err, ErrLTCStoreUnavailable) {
		t.Errorf("err=%v，期望包着 ErrLTCStoreUnavailable", err)
	}
}

func TestLTCConfig_ReadWithoutDBHandleIsDegraded(t *testing.T) {
	// 全局句柄为空 = 进程没接上库。此时"读不到配置"绝不能渲染成"运营把开关关了"。
	prev := db.GetDB()
	db.SetTestDB(nil)
	t.Cleanup(func() { db.SetTestDB(prev) })

	svc := NewLTCConfigService()
	cfg := svc.Config(context.Background())
	if cfg.Enabled {
		t.Error("无库却启用")
	}
	if !cfg.Degraded {
		t.Error("无库必须标降级：否则 degraded=false 会被读成「配置完好且关着」")
	}
}

// ---- 报告面：容易被读错的地方要自己说出口 ---------------------------------

func TestLTCConfig_ReadingHintsCoverTheExpensiveMisreadings(t *testing.T) {
	ctx := context.Background()
	svc := newLTCSvcWithKV(t, nil)
	hints := svc.Config(ctx).ReadingHints(nil)
	joined := strings.Join(hints, "|")
	for _, must := range []string{"没有任何路由", "副本"} {
		if !strings.Contains(joined, must) {
			t.Errorf("reading_hint 缺一条（含 %q）：%s", must, joined)
		}
	}
}

func TestLTCConfig_JSONRoundTrip(t *testing.T) {
	// 存进去的形状必须能原样读回：字段标签漂了，运营端表单与后端就会各说各话。
	cfg := DefaultLTCConfig()
	cfg.Enabled = true
	cfg.StagesEnabled = LTCStages{Opportunity: true, Outreach: true, Quote: true, Bill: true, Payment: true, Collection: true}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseLTCConfig(b)
	if err != nil {
		t.Fatalf("自己 marshal 出来的东西 parse 不回来：%v\n%s", err, b)
	}
	if !back.Enabled || !back.StageEnabled(LTCStageCollection) {
		t.Errorf("往返丢失：%s", b)
	}
	for _, key := range []string{"enabled", "stages_enabled", "thresholds", "lead_score", "confidence", "discount_percent", "win_probability"} {
		if !strings.Contains(string(b), fmt.Sprintf("%q", key)) {
			t.Errorf("序列化结果缺键 %s：%s", key, b)
		}
	}
}

// ---- helpers ---------------------------------------------------------------

func rawKVValue(ctx context.Context, database *gorm.DB, key string) (string, error) {
	var row model.SystemConfigKV
	if err := database.WithContext(ctx).Where("key = ?", key).First(&row).Error; err != nil {
		return "", err
	}
	return row.Value, nil
}

func countOperationLogs(ctx context.Context, database *gorm.DB) int64 {
	var n int64
	if err := database.WithContext(ctx).Model(&model.OperationLog{}).Count(&n).Error; err != nil {
		return -1
	}
	return n
}
