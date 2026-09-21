package service

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupSOPSchedulerTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.SOPAgent{},
		&model.SOPExecution{},
	)
}

func TestSOPScheduler_NewAndInterval(t *testing.T) {
	s := NewSOPScheduler(nil, nil, 0)
	if s.interval != 60*time.Second {
		t.Errorf("default interval should be 60s, got %v", s.interval)
	}
	s2 := NewSOPScheduler(nil, nil, 5*time.Second)
	if s2.interval != 5*time.Second {
		t.Errorf("expected 5s, got %v", s2.interval)
	}
}

func TestSOPScheduler_StartStop(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, 100*time.Millisecond)
	s.Start(context.Background())
	time.Sleep(150 * time.Millisecond)
	s.Stop(context.Background())
}

func TestSOPScheduler_StartTwiceIsIdempotent(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Second)
	s.Start(context.Background())
	s.Start(context.Background())
	s.Stop(context.Background())
}

func TestSOPScheduler_StopTwiceSafe(t *testing.T) {
	s := NewSOPScheduler(nil, nil, time.Second)
	s.Start(context.Background())
	s.Stop(context.Background())
	if s.running {
		t.Error("expected running=false after Stop")
	}
}

func TestSOPScheduler_CleanupStuckExecutions(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	ctx := context.Background()
	now := time.Now()

	stuck := &model.SOPExecution{
		SOPID:      1,
		CustomerID: "c-1",
		Status:     SOPStatusRunning,
		StartedAt:  now.Add(-25 * time.Hour),
	}
	if err := db.Create(stuck).Error; err != nil {
		t.Fatalf("create stuck: %v", err)
	}

	fresh := &model.SOPExecution{
		SOPID:      1,
		CustomerID: "c-2",
		Status:     SOPStatusRunning,
		StartedAt:  now.Add(-1 * time.Hour),
	}
	if err := db.Create(fresh).Error; err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.cleanupStuckExecutions(ctx)

	var stuckGot model.SOPExecution
	if err := db.Where("id = ?", stuck.ID).First(&stuckGot).Error; err != nil {
		t.Fatalf("read stuck: %v", err)
	}
	if stuckGot.Status != SOPStatusFailed {
		t.Errorf("expected failed, got %s", stuckGot.Status)
	}

	var freshGot model.SOPExecution
	if err := db.Where("id = ?", fresh.ID).First(&freshGot).Error; err != nil {
		t.Fatalf("read fresh: %v", err)
	}
	if freshGot.Status != SOPStatusRunning {
		t.Errorf("expected running, got %s", freshGot.Status)
	}
}

func TestSOPScheduler_DispatchAutoSOP(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	ctx := context.Background()

	agent := &model.SOPAgent{
		Name:          "test",
		Scenario:      "test",
		TriggerType:   SOPTriggerAuto,
		TriggerConfig: model.JSONMap{"customer_ids": []any{"c-1", "c-2"}},
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     1,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.dispatchAutoSOPs(ctx)

	var execs []model.SOPExecution
	db.Where("sop_id = ?", agent.ID).Find(&execs)
	if len(execs) != 2 {
		t.Errorf("expected 2 executions, got %d", len(execs))
	}
}

func TestSOPScheduler_DispatchAutoSOP_NilDB(t *testing.T) {
	s := NewSOPScheduler(nil, nil, time.Hour)
	s.dispatchAutoSOPs(context.Background())
}

func TestSOPScheduler_DispatchScheduledSOP_FirstRun(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	agent := &model.SOPAgent{
		Name:          "sched-test",
		Scenario:      "sched",
		TriggerType:   SOPTriggerSchedule,
		TriggerConfig: model.JSONMap{"customer_ids": []any{"c-1"}},
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     1,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.dispatchScheduledSOPs(context.Background())

	var execs []model.SOPExecution
	db.Where("sop_id = ?", agent.ID).Find(&execs)
	if len(execs) != 1 {
		t.Errorf("expected 1 execution on first run, got %d", len(execs))
	}
}

func TestSOPScheduler_DispatchScheduledSOP_IntervalGuard(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	recent := time.Now().UTC().Format(time.RFC3339)
	agent := &model.SOPAgent{
		Name:        "sched-test",
		Scenario:    "sched",
		TriggerType: SOPTriggerSchedule,
		TriggerConfig: model.JSONMap{
			"customer_ids":     []any{"c-1"},
			"interval_minutes": float64(60),
			"last_run_at":      recent,
		},
		SOPGraph:  model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:  true,
		CreatedBy: 1,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.dispatchScheduledSOPs(context.Background())

	var execs []model.SOPExecution
	db.Where("sop_id = ?", agent.ID).Find(&execs)
	if len(execs) != 0 {
		t.Errorf("expected 0 executions (interval not met), got %d", len(execs))
	}
}

func TestSOPScheduler_TryExecute_DuplicateGuard(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	ctx := context.Background()
	agent := &model.SOPAgent{
		Name:          "dup",
		Scenario:      "dup",
		TriggerType:   SOPTriggerAuto,
		TriggerConfig: model.JSONMap{"customer_ids": []any{"c-1"}},
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     1,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.tryExecute(ctx, *agent)
	s.tryExecute(ctx, *agent)

	var execs []model.SOPExecution
	db.Where("sop_id = ? AND status = ?", agent.ID, SOPStatusRunning).Find(&execs)
	if len(execs) != 1 {
		t.Errorf("expected 1 running execution due to dedup, got %d", len(execs))
	}
}

func TestSOPScheduler_ExtractCustomerIDs(t *testing.T) {
	cases := []struct {
		name string
		cfg  model.JSONMap
		want int
	}{
		{"nil", nil, 0},
		{"empty", model.JSONMap{}, 0},
		{"list", model.JSONMap{"customer_ids": []any{"a", "b"}}, 2},
		{"single", model.JSONMap{"customer_ids": []any{"x"}}, 1},
		{"wrong_type", model.JSONMap{"customer_ids": "string-not-list"}, 0},
		{"mixed_with_int", model.JSONMap{"customer_ids": []any{"a", 1, "b"}}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _ := extractCustomerIDs(c.cfg)
			if len(out) != c.want {
				t.Errorf("expected %d, got %d", c.want, len(out))
			}
		})
	}
}

func TestSOPScheduler_SetJSONMapValue(t *testing.T) {
	out := setJSONMapValue(model.JSONMap{"a": "1"}, "b", "2")
	if out == "" {
		t.Fatal("expected non-empty")
	}
	if out != `{"a":"1","b":"2"}` && out != `{"b":"2","a":"1"}` {
		t.Errorf("unexpected json: %s", out)
	}

	out2 := setJSONMapValue(nil, "x", "y")
	if out2 != `{"x":"y"}` {
		t.Errorf("expected x:y, got %s", out2)
	}
}

func TestSOPScheduler_FmtUintSafe(t *testing.T) {
	if fmtUintSafe(0) != "0" {
		t.Error("expected 0")
	}
	if fmtUintSafe(123) != "123" {
		t.Error("expected 123")
	}
	if fmtUintSafe(9999999) != "9999999" {
		t.Error("expected 9999999")
	}
}

func TestSOPScheduler_Tick_NilDB(t *testing.T) {
	s := NewSOPScheduler(nil, nil, time.Hour)
	s.tick(context.Background())
}

func TestSOPScheduler_Tick_WithDB_NoData(t *testing.T) {
	db := setupSOPSchedulerTestDB(t)
	svc := NewSOPService(db, nil)
	s := NewSOPScheduler(svc, db, time.Hour)
	s.tick(context.Background())
}

// ---------------------------------------------------------------- T-P5-01 动态人群圈选

// setupSOPSchedulerAudienceTestDB 比基础夹具多三张圈选源表。
// 三张表都建（而不是只建用到的那张）的理由：NewTestDB 会 DropTable+AutoMigrate，
// 漏一张就等于把同进程里前一个用例的数据留在库里当"名单"。
func setupSOPSchedulerAudienceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.SOPAgent{},
		&model.SOPExecution{},
		&model.CustomerRFM{},
		&model.CustomerTagAssignment{},
		&model.ChurnScore{},
	)
}

func seedSchedulerChampion(t *testing.T, db *gorm.DB, customerID string, composite int) {
	t.Helper()
	row := &model.CustomerRFM{
		CustomerID:     customerID,
		Segment:        model.RFMSegmentChampion,
		CompositeScore: composite,
		ChurnRiskLevel: "low",
		ComputedAt:     time.Now(),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed champion %s: %v", customerID, err)
	}
}

func createSchedulerAgent(t *testing.T, db *gorm.DB, triggerType string, cfg model.JSONMap) *model.SOPAgent {
	t.Helper()
	agent := &model.SOPAgent{
		Name:          "audience-test",
		Scenario:      "audience",
		TriggerType:   triggerType,
		TriggerConfig: cfg,
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     7,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return agent
}

func executionCustomerIDs(t *testing.T, db *gorm.DB, sopID uint) []string {
	t.Helper()
	var execs []model.SOPExecution
	if err := db.Where("sop_id = ?", sopID).Order("customer_id ASC").Find(&execs).Error; err != nil {
		t.Fatalf("读执行记录失败: %v", err)
	}
	out := make([]string, 0, len(execs))
	for _, e := range execs {
		out = append(out, e.CustomerID)
	}
	return out
}

func reloadTriggerConfig(t *testing.T, db *gorm.DB, agentID uint) model.JSONMap {
	t.Helper()
	var got model.SOPAgent
	if err := db.First(&got, agentID).Error; err != nil {
		t.Fatalf("重读 agent 失败: %v", err)
	}
	return got.TriggerConfig
}

// TestSOPScheduler_EmptyAudienceDoesNotTargetCreator AC① 坏例锁定：
// 既没有静态 customer_ids、也没有 audience 时，一个人都不能开工。
// 旧实现在这里回退到 CreatedBy —— 创建者是运营账号本人，等于"配置没填 → 给自己发一轮 SOP"。
func TestSOPScheduler_EmptyAudienceDoesNotTargetCreator(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 0 {
		t.Fatalf("空配置应零开工, got %v（回退到创建者 7 就是这个形状）", got)
	}
}

// TestSOPScheduler_AudienceQueryFailureDoesNotFallBackToCreator 圈选读不到表时，错误只能
// 止于日志：回退到创建者等于把"数据源坏了"翻译成"给运营自己发一轮"。
// 表用显式 drop 构造（同进程 NewTestDB 共库，"没列进模型清单"不等于"表不存在"）。
func TestSOPScheduler_AudienceQueryFailureDoesNotFallBackToCreator(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	if err := db.Migrator().DropTable(&model.CustomerRFM{}); err != nil {
		t.Fatalf("drop customer_rfm 失败: %v", err)
	}
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience":           map[string]any{"segments": []any{"champion"}},
		"audience_confirmed": true,
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 0 {
		t.Fatalf("圈选报错时应零开工, got %v", got)
	}
}

// TestSOPScheduler_AudienceFirstRoundPreviewsOnly AC③：audience 未经人工确认时，
// 本轮只把名单写回 trigger_config.audience_preview，零开工、零外发。
func TestSOPScheduler_AudienceFirstRoundPreviewsOnly(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	seedSchedulerChampion(t, db, "c-1", 100)
	seedSchedulerChampion(t, db, "c-2", 90)
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience": map[string]any{"segments": []any{model.RFMSegmentChampion}},
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 0 {
		t.Fatalf("未确认的首轮应零开工, got %v", got)
	}

	cfg := reloadTriggerConfig(t, db, agent.ID)
	raw, ok := cfg["audience_preview"]
	if !ok {
		t.Fatalf("首轮应写回 audience_preview, 实际 keys=%v", keysOf(cfg))
	}
	preview, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("audience_preview 应为对象, got %T", raw)
	}
	if count := preview["count"]; count != float64(2) {
		t.Errorf("预览 count 应为 2, got %v", count)
	}
	ids, ok := preview["customer_ids"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("预览名单应为 2 个 id, got %v", preview["customer_ids"])
	}
	if _, tainted := cfg["audience_confirmed"]; tainted {
		t.Error("预览轮不得替人工写下 audience_confirmed")
	}
}

// TestSOPScheduler_ConfirmedAudienceDispatchesEachCustomer 人工确认后：名单里每人一条执行。
func TestSOPScheduler_ConfirmedAudienceDispatchesEachCustomer(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	seedSchedulerChampion(t, db, "c-1", 100)
	seedSchedulerChampion(t, db, "c-2", 90)
	seedSchedulerChampion(t, db, "c-3", 80)
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience":           map[string]any{"segments": []any{model.RFMSegmentChampion}},
		"audience_confirmed": true,
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	got := executionCustomerIDs(t, db, agent.ID)
	if len(got) != 3 {
		t.Fatalf("期望 3 条执行, got %d (%v)", len(got), got)
	}
	for _, id := range []string{"c-1", "c-2", "c-3"} {
		if !slices.Contains(got, id) {
			t.Errorf("名单缺 %s: %v", id, got)
		}
	}
	if slices.Contains(got, "7") {
		t.Error("创建者 7 不该出现在名单里")
	}
}

// TestSOPScheduler_ConfirmedAudienceOnlyDispatchesOnce 确认位是一次性的：第二轮起
// 每人已有运行中执行 → 去重生效，不该每 tick 就把整个名单重跑一遍。
func TestSOPScheduler_ConfirmedAudienceOnlyDispatchesOnce(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	seedSchedulerChampion(t, db, "c-1", 100)
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience":           map[string]any{"segments": []any{model.RFMSegmentChampion}},
		"audience_confirmed": true,
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 1 {
		t.Fatalf("去重后每人应只有 1 条运行中执行, got %v", got)
	}
}

// TestSOPScheduler_AudienceLimitCapsExecutions AC②：上限是在调度侧生效的，
// 5 个人圈进 2 个上限 ⇒ 只能开工 2 条（多出来的既不排队也不补发）。
func TestSOPScheduler_AudienceLimitCapsExecutions(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	for i := 0; i < 5; i++ {
		seedSchedulerChampion(t, db, "c-"+strconv.Itoa(i), 100-i)
	}
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience":           map[string]any{"segments": []any{model.RFMSegmentChampion}, "limit": float64(2)},
		"audience_confirmed": true,
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 2 {
		t.Fatalf("上限 2 时应开工 2 条, got %d (%v)", len(got), got)
	}
}

// TestSOPScheduler_StaticCustomerIDsStillWork 既有静态名单通道零变化：
// 本卡只把"空配置回退到创建者"这一条换掉，填了 customer_ids 的老 SOP 行为照旧。
func TestSOPScheduler_StaticCustomerIDsStillWork(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"customer_ids": []any{"x-1", "x-2"},
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 2 {
		t.Fatalf("静态名单应开工 2 条, got %v", got)
	}
}

// TestSOPScheduler_RunningBudgetClampsRound maxRunningPerSOP 的额度要在**本轮内**生效：
// 圈选一轮最多可回 MaxAudienceLimit 人，只在"上一轮已跑满"才拦的话，一轮就能把并发从 0 顶满。
func TestSOPScheduler_RunningBudgetClampsRound(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	bulk := make([]model.SOPExecution, 0, 49)
	for i := 0; i < 49; i++ {
		bulk = append(bulk, model.SOPExecution{
			SOPID:      0,
			CustomerID: "busy-" + strconv.Itoa(i),
			Status:     SOPStatusRunning,
			StartedAt:  time.Now(),
		})
	}
	agent := createSchedulerAgent(t, db, SOPTriggerAuto, model.JSONMap{
		"audience":           map[string]any{"segments": []any{model.RFMSegmentChampion}},
		"audience_confirmed": true,
	})
	for i := range bulk {
		bulk[i].SOPID = agent.ID
	}
	if err := db.CreateInBatches(bulk, 100).Error; err != nil {
		t.Fatalf("seed 运行中执行失败: %v", err)
	}
	seedSchedulerChampion(t, db, "c-1", 100)
	seedSchedulerChampion(t, db, "c-2", 90)
	seedSchedulerChampion(t, db, "c-3", 80)

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchAutoSOPs(context.Background())

	got := executionCustomerIDs(t, db, agent.ID)
	newOnes := 0
	for _, id := range got {
		if strings.HasPrefix(id, "c-") {
			newOnes++
		}
	}
	if newOnes != 1 {
		t.Fatalf("剩余额度 1 时本轮应只新增 1 条, got %d (%v)", newOnes, got)
	}
}

// TestSOPScheduler_ScheduledAudiencePreviewSurvivesLastRunAt schedule 型的一次 tick 会写两次
// trigger_config：预览一次、last_run_at 一次，且后者是把**内存里那份 map** 整体序列化回库。
// 预览只落库不落那份 map 的话，同一次 tick 里就会被 last_run_at 覆盖掉 —— 于是
// "首轮只出预览"变成"首轮什么都没留下"，运营侧看到的是一个不存在的待确认名单。
func TestSOPScheduler_ScheduledAudiencePreviewSurvivesLastRunAt(t *testing.T) {
	db := setupSOPSchedulerAudienceTestDB(t)
	seedSchedulerChampion(t, db, "c-1", 100)
	agent := createSchedulerAgent(t, db, SOPTriggerSchedule, model.JSONMap{
		"audience":         map[string]any{"segments": []any{model.RFMSegmentChampion}},
		"interval_minutes": float64(60),
	})

	s := NewSOPScheduler(NewSOPService(db, nil), db, time.Hour)
	s.dispatchScheduledSOPs(context.Background())

	if got := executionCustomerIDs(t, db, agent.ID); len(got) != 0 {
		t.Fatalf("未确认的 schedule 首轮应零开工, got %v", got)
	}
	cfg := reloadTriggerConfig(t, db, agent.ID)
	if _, ok := cfg["last_run_at"]; !ok {
		t.Error("last_run_at 应已写入（否则每 tick 都会重跑一次圈选）")
	}
	if _, ok := cfg["audience_preview"]; !ok {
		t.Fatalf("预览被 last_run_at 的回写覆盖掉了, 现存 keys=%v", keysOf(cfg))
	}
}

func keysOf(m model.JSONMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
