// audience_selector_test.go T-P5-01 圈选层：一条条件对应一次真实查询，断言打在查询结果上。
//
// 三条判据各自的坏例（都在下面有对应用例）：
// ① 取数报错必须上抛。`return nil, nil` 式的吞错会让「这个条件一个人都没圈到」和
// 「这张表根本读不了」逐字节相同，而后者才是本机 churn_scores 的当前真实状态。
// ② 死源要单独报因。churn_scores 的统计源 defaultChurnStatsQuery 现为 `return nil, nil`，
// 周批每轮自己空跑退出、一行都不写 —— 圈选若只回"0 人"，运营读到的是"条件太严"。
// ③ 上限要真夹住。上限的意义是"一轮最多给这么多人开工"，所以夹住之后必须留下 Truncated 痕迹。
package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupAudienceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.CustomerRFM{},
		&model.CustomerTagAssignment{},
		&model.ChurnScore{},
	)
}

func seedAudienceRFM(t *testing.T, database *gorm.DB, customerID, segment string, composite int) {
	t.Helper()
	row := &model.CustomerRFM{
		CustomerID:     customerID,
		Segment:        segment,
		CompositeScore: composite,
		MonetaryTotal:  int64(composite) * 100,
		ChurnRiskLevel: "low",
		ComputedAt:     time.Now(),
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed customer_rfm %s: %v", customerID, err)
	}
}

func seedAudienceTag(t *testing.T, database *gorm.DB, customerID, tag string) {
	t.Helper()
	row := &model.CustomerTagAssignment{
		CustomerID: customerID,
		Tag:        tag,
		Category:   "behavior",
		Source:     "test",
		Confidence: 0.9,
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed customer_tag_assignment %s/%s: %v", customerID, tag, err)
	}
}

func seedAudienceChurn(t *testing.T, database *gorm.DB, customerKey string, pAlive float64) {
	t.Helper()
	row := &model.ChurnScore{
		CustomerKey: customerKey,
		PAlive:      pAlive,
		Params:      "{}",
		ComputedAt:  time.Now(),
	}
	if err := database.Create(row).Error; err != nil {
		t.Fatalf("seed churn_score %s: %v", customerKey, err)
	}
}

func assertIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("名单不符\n  期望: %v\n  实得: %v", want, got)
	}
}

func hasReason(sel *AudienceSelection, want string) bool {
	for _, r := range sel.Reasons {
		if r == want {
			return true
		}
	}
	return false
}

// TestAudience_SelectBySegment 一条 segment 条件只圈该层的客户。
func TestAudience_SelectBySegment(t *testing.T) {
	database := setupAudienceTestDB(t)
	seedAudienceRFM(t, database, "c-1", model.RFMSegmentChampion, 100)
	seedAudienceRFM(t, database, "c-2", model.RFMSegmentChampion, 90)
	seedAudienceRFM(t, database, "c-3", model.RFMSegmentLoyal, 80)

	sel, err := NewAudienceSelectorWithDB(database).
		Select(context.Background(), AudienceConfig{Segments: []string{model.RFMSegmentChampion}})
	if err != nil {
		t.Fatalf("Select 失败: %v", err)
	}
	assertIDs(t, sel.CustomerIDs, "c-1", "c-2")
	if sel.Truncated {
		t.Error("3 行取 2 人，不该判成截断")
	}
	if len(sel.Reasons) != 0 {
		t.Errorf("圈到人就不该有因, got %v", sel.Reasons)
	}
}

// TestAudience_SelectByTag 一条 tag 条件只圈打过该标的客户。
func TestAudience_SelectByTag(t *testing.T) {
	database := setupAudienceTestDB(t)
	seedAudienceTag(t, database, "c-1", "vip")
	seedAudienceTag(t, database, "c-2", "vip")
	seedAudienceTag(t, database, "c-3", "normal")

	sel, err := NewAudienceSelectorWithDB(database).
		Select(context.Background(), AudienceConfig{Tags: []string{"vip"}})
	if err != nil {
		t.Fatalf("Select 失败: %v", err)
	}
	if len(sel.CustomerIDs) != 2 {
		t.Fatalf("期望 2 人, got %d (%v)", len(sel.CustomerIDs), sel.CustomerIDs)
	}
	for _, id := range []string{"c-1", "c-2"} {
		found := false
		for _, got := range sel.CustomerIDs {
			if got == id {
				found = true
			}
		}
		if !found {
			t.Errorf("名单缺 %s: %v", id, sel.CustomerIDs)
		}
	}
	for _, got := range sel.CustomerIDs {
		if got == "c-3" {
			t.Error("未打 vip 标的 c-3 进了名单")
		}
	}
}

// TestAudience_ChurnBelowThreshold churn 条件取的是 p_alive **低于**阈值的一侧。
func TestAudience_ChurnBelowThreshold(t *testing.T) {
	database := setupAudienceTestDB(t)
	seedAudienceChurn(t, database, "c-1", 0.10)
	seedAudienceChurn(t, database, "c-2", 0.90)

	sel, err := NewAudienceSelectorWithDB(database).
		Select(context.Background(), AudienceConfig{MaxPAlive: 0.3})
	if err != nil {
		t.Fatalf("Select 失败: %v", err)
	}
	assertIDs(t, sel.CustomerIDs, "c-1")
}

// TestAudience_DeadSourceIsNotSilentZero 本卡最容易静默的一条：
// churn_scores 一行都没有（生产者 defaultChurnStatsQuery 现为 `return nil, nil`）时，
// 圈选结果必须报 source_empty；有行但都高于阈值时报 no_match。两者不能混成一个"0 人"。
func TestAudience_DeadSourceIsNotSilentZero(t *testing.T) {
	t.Run("表空=源没在跑", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{MaxPAlive: 0.3})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if len(sel.CustomerIDs) != 0 {
			t.Fatalf("空表不该圈到人: %v", sel.CustomerIDs)
		}
		if !hasReason(sel, "source_empty:churn") {
			t.Errorf("应报 source_empty:churn, got %v", sel.Reasons)
		}
	})

	t.Run("有行但无人符合", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		seedAudienceChurn(t, database, "c-1", 0.90)
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{MaxPAlive: 0.3})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if len(sel.CustomerIDs) != 0 {
			t.Fatalf("无人符合时名单应为空: %v", sel.CustomerIDs)
		}
		if hasReason(sel, "source_empty:churn") {
			t.Errorf("源在跑（有行），不该报 source_empty: %v", sel.Reasons)
		}
		if !hasReason(sel, "no_match:churn") {
			t.Errorf("应报 no_match:churn, got %v", sel.Reasons)
		}
	})
}

// TestAudience_SegmentSourceDiagnostics RFM 侧同一条区分：表全空 vs 该层没人。
func TestAudience_SegmentSourceDiagnostics(t *testing.T) {
	t.Run("rfm 表全空", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{Segments: []string{model.RFMSegmentChampion}})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if !hasReason(sel, "source_empty:rfm") {
			t.Errorf("应报 source_empty:rfm, got %v", sel.Reasons)
		}
	})

	t.Run("该层没人", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		seedAudienceRFM(t, database, "c-1", model.RFMSegmentLoyal, 70)
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{Segments: []string{model.RFMSegmentChampion}})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if hasReason(sel, "source_empty:rfm") {
			t.Errorf("rfm 表有行，不该报 source_empty: %v", sel.Reasons)
		}
		if !hasReason(sel, "no_match:segment=champion") {
			t.Errorf("应报 no_match:segment=champion, got %v", sel.Reasons)
		}
	})

	t.Run("tag 无人命中", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		seedAudienceTag(t, database, "c-1", "vip")
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{Tags: []string{"no-such-tag"}})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if !hasReason(sel, "no_match:tag=no-such-tag") {
			t.Errorf("应报 no_match:tag=no-such-tag, got %v", sel.Reasons)
		}
	})
}

// TestAudience_TwoConditionsIntersect 两条条件同时生效时取交集，不是并集。
// 并集的后果：勾了"champion"又勾了"vip"，收到的却是两拨人的总和，规模翻倍。
func TestAudience_TwoConditionsIntersect(t *testing.T) {
	database := setupAudienceTestDB(t)
	seedAudienceRFM(t, database, "c-1", model.RFMSegmentChampion, 100)
	seedAudienceRFM(t, database, "c-2", model.RFMSegmentChampion, 90)
	seedAudienceRFM(t, database, "c-3", model.RFMSegmentLoyal, 80)
	seedAudienceTag(t, database, "c-2", "vip")
	seedAudienceTag(t, database, "c-3", "vip")

	sel, err := NewAudienceSelectorWithDB(database).
		Select(context.Background(), AudienceConfig{
			Segments: []string{model.RFMSegmentChampion},
			Tags:     []string{"vip"},
		})
	if err != nil {
		t.Fatalf("Select 失败: %v", err)
	}
	assertIDs(t, sel.CustomerIDs, "c-2")
}

// TestAudience_LimitClampedAndTruncated AC②：规模有上限，且夹过人时留下痕迹。
func TestAudience_LimitClampedAndTruncated(t *testing.T) {
	t.Run("显式上限", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		for i := 0; i < 5; i++ {
			seedAudienceRFM(t, database, "lim-"+strconv.Itoa(i), model.RFMSegmentChampion, 100-i)
		}
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{
				Segments: []string{model.RFMSegmentChampion},
				Limit:    2,
			})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if len(sel.CustomerIDs) != 2 {
			t.Fatalf("上限 2 时应只回 2 人, got %d (%v)", len(sel.CustomerIDs), sel.CustomerIDs)
		}
		if !sel.Truncated {
			t.Error("5 人取 2 人，必须标 Truncated")
		}
	})

	t.Run("超上限时夹到 MaxAudienceLimit", func(t *testing.T) {
		database := setupAudienceTestDB(t)
		// 一次批量写入：逐行 seedAudienceRFM 在 501 行时会把用例耗时拉到读代码的价值之外。
		rows := make([]model.CustomerRFM, 0, MaxAudienceLimit+1)
		for i := 0; i <= MaxAudienceLimit; i++ {
			rows = append(rows, model.CustomerRFM{
				CustomerID:     "bulk-" + strconv.Itoa(i),
				Segment:        model.RFMSegmentChampion,
				CompositeScore: 100,
				ChurnRiskLevel: "low",
				ComputedAt:     time.Now(),
			})
		}
		if err := database.CreateInBatches(rows, 200).Error; err != nil {
			t.Fatalf("批量 seed 失败: %v", err)
		}
		sel, err := NewAudienceSelectorWithDB(database).
			Select(context.Background(), AudienceConfig{
				Segments: []string{model.RFMSegmentChampion},
				Limit:    99999,
			})
		if err != nil {
			t.Fatalf("Select 失败: %v", err)
		}
		if len(sel.CustomerIDs) != MaxAudienceLimit {
			t.Fatalf("应夹到 MaxAudienceLimit=%d, got %d", MaxAudienceLimit, len(sel.CustomerIDs))
		}
		if !sel.Truncated {
			t.Error("夹过人时必须标 Truncated")
		}
	})
}

// TestAudience_NoConditionSelectsNobody 一条条件都没生效时不圈任何人，并说明理由。
// 这条是 AC① 的地基：调度器拿到的"空"必须是"条件为空"，不是"回退到某个人"。
func TestAudience_NoConditionSelectsNobody(t *testing.T) {
	database := setupAudienceTestDB(t)
	seedAudienceRFM(t, database, "c-1", model.RFMSegmentChampion, 100)

	sel, err := NewAudienceSelectorWithDB(database).Select(context.Background(), AudienceConfig{})
	if err != nil {
		t.Fatalf("Select 失败: %v", err)
	}
	if len(sel.CustomerIDs) != 0 {
		t.Fatalf("无条件不该圈到人: %v", sel.CustomerIDs)
	}
	if !hasReason(sel, "no_condition") {
		t.Errorf("应报 no_condition, got %v", sel.Reasons)
	}
}

// TestAudience_PropagatesQueryError 表不存在时必须报错，不能吞成"0 人"。
//
// 表是**显式 drop** 的，不是"建库时少列一张表"：同进程的 NewTestDB 共用同一个库，
// 只重建自己列出的那几张表，所以"没列出"并不等于"不存在"（前一个用例建的表还在那儿）。
func TestAudience_PropagatesQueryError(t *testing.T) {
	database := setupAudienceTestDB(t)
	if err := database.Migrator().DropTable(&model.CustomerRFM{}); err != nil {
		t.Fatalf("drop customer_rfm 失败: %v", err)
	}
	_, err := NewAudienceSelectorWithDB(database).
		Select(context.Background(), AudienceConfig{Segments: []string{model.RFMSegmentChampion}})
	if err == nil {
		t.Fatal("读不到表时 Select 必须返回 err，回 (空, nil) 就是吞错")
	}
}

// TestAudience_ParseConfig TriggerConfig["audience"] 的各种形状。
// 坏形状一律降为"没有条件"，让上层报 no_condition —— 解析器不该 panic，也不该猜。
func TestAudience_ParseConfig(t *testing.T) {
	cases := []struct {
		name       string
		cfg        model.JSONMap
		wantFound  bool
		wantSegs   []string
		wantTags   []string
		wantPAlive float64
		wantLimit  int
	}{
		{name: "缺 audience", cfg: model.JSONMap{}, wantFound: false},
		{name: "audience 不是对象", cfg: model.JSONMap{"audience": "champion"}, wantFound: false},
		{
			name:      "三段齐全",
			cfg:       model.JSONMap{"audience": map[string]any{"segments": []any{"champion", "loyal"}, "tags": []any{"vip"}, "max_p_alive": 0.3, "limit": 50.0}},
			wantFound: true, wantSegs: []string{"champion", "loyal"}, wantTags: []string{"vip"}, wantPAlive: 0.3, wantLimit: 50,
		},
		{
			name:      "jsonb 里数字是 float64",
			cfg:       model.JSONMap{"audience": map[string]any{"limit": float64(30)}},
			wantFound: true, wantLimit: 30,
		},
		{
			name:      "limit 写成字符串则忽略",
			cfg:       model.JSONMap{"audience": map[string]any{"limit": "30"}},
			wantFound: true, wantLimit: 0,
		},
		{
			name:      "segments 类型错则丢弃",
			cfg:       model.JSONMap{"audience": map[string]any{"segments": "champion"}},
			wantFound: true, wantSegs: nil,
		},
		{
			name:      "空对象也算声明了 audience",
			cfg:       model.JSONMap{"audience": map[string]any{}},
			wantFound: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, found := parseAudienceConfig(c.cfg)
			if found != c.wantFound {
				t.Fatalf("found=%v, 期望 %v", found, c.wantFound)
			}
			if !c.wantFound {
				return
			}
			if strings.Join(got.Segments, ",") != strings.Join(c.wantSegs, ",") {
				t.Errorf("segments=%v, 期望 %v", got.Segments, c.wantSegs)
			}
			if strings.Join(got.Tags, ",") != strings.Join(c.wantTags, ",") {
				t.Errorf("tags=%v, 期望 %v", got.Tags, c.wantTags)
			}
			if got.MaxPAlive != c.wantPAlive {
				t.Errorf("max_p_alive=%v, 期望 %v", got.MaxPAlive, c.wantPAlive)
			}
			if got.Limit != c.wantLimit {
				t.Errorf("limit=%d, 期望 %d", got.Limit, c.wantLimit)
			}
		})
	}
}
