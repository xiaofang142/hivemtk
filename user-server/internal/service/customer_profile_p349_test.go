// P-3 / P-4 / P-9 实施测试：RFM 双体系统一、画像四字段真实来源、Clue Level 动态化
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/repository"
)

func TestRFMConfigFromRule_NilFallsBackToBuckets(t *testing.T) {
	cfg := RFMConfigFromRule(nil)
	if cfg.Rule != nil {
		t.Fatal("nil rule 应回退默认配置")
	}
	def := DefaultRFMConfig()
	if len(cfg.RecencyBuckets) != len(def.RecencyBuckets) {
		t.Fatalf("默认分位桶应与 DefaultRFMConfig 一致")
	}
}

// TestRFMConfigFromRule_ScoreEquivalence 验证 Rule 打分与原 rfm_calculator 语义逐点等价
func TestRFMConfigFromRule_ScoreEquivalence(t *testing.T) {
	rule := &model.RFMRule{
		RDays1: 7, RDays2: 14, RDays3: 30, RDays4: 60, RDays5: 90,
		FCount1: 1, FCount2: 3, FCount3: 5, FCount4: 10, FCount5: 20,
		MAmount1: 10000, MAmount2: 50000, MAmount3: 100000, MAmount4: 500000, MAmount5: 1000000,
	}
	cfg := RFMConfigFromRule(rule)

	rCases := []struct {
		days int
		want int
	}{
		{0, 5}, {7, 5}, {8, 4}, {14, 4}, {15, 3}, {30, 3}, {31, 2}, {60, 2}, {61, 1}, {90, 1}, {9999, 1},
	}
	for _, c := range rCases {
		if got := rfmScoreRecencyCfg(c.days, cfg); got != c.want {
			t.Errorf("R days=%d expected %d, got %d", c.days, c.want, got)
		}
	}

	fCases := []struct {
		freq int
		want int
	}{
		{0, 1}, {1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 3}, {6, 3}, {9, 3}, {10, 4}, {11, 4}, {20, 5}, {21, 5},
	}
	for _, c := range fCases {
		if got := rfmScoreFrequencyCfg(c.freq, cfg); got != c.want {
			t.Errorf("F freq=%d expected %d, got %d", c.freq, c.want, got)
		}
	}

	mCases := []struct {
		m    int64
		want int
	}{
		{0, 1}, {9999, 1}, {10000, 1}, {10001, 1}, {50000, 2}, {50001, 2},
		{100000, 3}, {100001, 3}, {500000, 4}, {500001, 4}, {1000000, 5}, {1000001, 5},
	}
	for _, c := range mCases {
		if got := rfmScoreMonetaryCfg(c.m, cfg); got != c.want {
			t.Errorf("M amount=%d expected %d, got %d", c.m, c.want, got)
		}
	}
}

// TestRFMConfigFromRule_NilRuleUsesBuckets 无 Rule 时走默认分位桶（行为不变）
func TestRFMConfigFromRule_NilRuleUsesBuckets(t *testing.T) {
	cfg := RFMConfigFromRule(nil)
	if got := rfmScoreRecencyCfg(8, cfg); got != rfmScoreRecency(8, cfg.RecencyBuckets) {
		t.Fatalf("无 Rule 时应等价于分位桶打分")
	}
	if got := rfmScoreRecencyCfg(8, cfg); got != 4 {
		t.Fatalf("days=8 默认分位应为 4，got %d", got)
	}
}

func TestClueLevelFromScore(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{0, "cold"}, {39, "cold"}, {40, "warm"}, {69, "warm"}, {70, "hot"}, {100, "hot"},
	}
	for _, c := range cases {
		if got := ClueLevelFromScore(c.score); got != c.want {
			t.Errorf("score=%d expected %s, got %s", c.score, c.want, got)
		}
	}
}

func TestClueScoreService_WriteBackLevel(t *testing.T) {
	database := setupClueScoreTestDB(t)
	svc := NewClueScoreServiceWithRepos(
		repository.NewClueScoreRepositoryWithDB(database),
		repository.NewClueEngagementRepositoryWithDB(database),
		repository.NewClueRepositoryWithDB(database),
	)

	clue := &model.Clue{
		ID:         "clue-level-hot",
		Account:    "acc-level-1",
		Type:       ClueTypePhone,
		IsVerify:   1,
		Name:       "甲",
		City:       "上海",
		Address:    "浦东",
		Desc:       "高质量线索",
		CreateTime: time.Now().Unix(),
	}
	if err := database.Create(clue).Error; err != nil {
		t.Fatalf("seed clue 失败: %v", err)
	}

	score, err := svc.ScoreClue(context.Background(), clue)
	if err != nil {
		t.Fatalf("ScoreClue failed: %v", err)
	}

	var updated model.Clue
	if err := database.First(&updated, "id = ?", clue.ID).Error; err != nil {
		t.Fatalf("reload clue 失败: %v", err)
	}
	if want := ClueLevelFromScore(score.TotalScore); updated.Level != want {
		t.Errorf("level 写回期望 %s，got %s (score=%d)", want, updated.Level, score.TotalScore)
	}

	cold := &model.Clue{
		ID:      "clue-level-cold",
		Account: "acc-level-2",
		Type:    ClueTypeTwitter,
		Name:    "乙",
	}
	if err := database.Create(cold).Error; err != nil {
		t.Fatalf("seed cold clue 失败: %v", err)
	}
	scoreCold, err := svc.ScoreClue(context.Background(), cold)
	if err != nil {
		t.Fatalf("ScoreClue cold failed: %v", err)
	}
	var updatedCold model.Clue
	if err := database.First(&updatedCold, "id = ?", cold.ID).Error; err != nil {
		t.Fatalf("reload cold clue 失败: %v", err)
	}
	if want := ClueLevelFromScore(scoreCold.TotalScore); updatedCold.Level != want {
		t.Errorf("cold level 写回期望 %s，got %s", want, updatedCold.Level)
	}
}

func TestPreferredTimeLabel(t *testing.T) {
	if got := preferredTimeLabel(nil); got != "" {
		t.Errorf("空直方图应返回空串，got %q", got)
	}
	hist := map[int]int64{10: 3, 20: 9, 21: 12, 22: 5}
	if got := preferredTimeLabel(hist); got != "21:00-23:00" {
		t.Errorf("峰值 21 点期望 21:00-23:00，got %q", got)
	}

	tie := map[int]int64{8: 5, 19: 5}
	if got := preferredTimeLabel(tie); got != "08:00-10:00" {
		t.Errorf("平峰应取更早小时，got %q", got)
	}
}

func TestRiskLevelFromChurn(t *testing.T) {
	cases := map[string]string{
		"high":     "high",
		"medium":   "medium",
		"low":      "normal",
		"":         "normal",
		"whatever": "normal",
	}
	for in, want := range cases {
		if got := riskLevelFromChurn(in); got != want {
			t.Errorf("riskLevelFromChurn(%q) expected %s, got %s", in, want, got)
		}
	}
}

// TestBuildUserProfile_Enrichment 四字段端到端富化（真实 PG 测试库）
func TestBuildUserProfile_Enrichment(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.CustomerTagAssignment{},
		&model.CustomerRFM{},
	)
	prev := db.GetDB()
	db.SetTestDB(database)
	defer db.SetTestDB(prev)

	const customerID = "cust-p4-001"

	assignments := []*model.CustomerTagAssignment{
		{CustomerID: customerID, Tag: "tag-low", Category: "behavior", Confidence: 0.3},
		{CustomerID: customerID, Tag: "tag-high1", Category: "behavior", Confidence: 0.9},
		{CustomerID: customerID, Tag: "tag-high2", Category: "price", Confidence: 0.8},
		{CustomerID: customerID, Tag: "tag-mid1", Category: "channel", Confidence: 0.6},
		{CustomerID: customerID, Tag: "tag-mid2", Category: "lifecycle", Confidence: 0.5},
		{CustomerID: customerID, Tag: "tag-mid3", Category: "other", Confidence: 0.4},
	}
	for i, a := range assignments {
		a.CreatedAt = time.Now().Add(time.Duration(i) * time.Minute)
		if err := database.Create(a).Error; err != nil {
			t.Fatalf("seed tag assignment 失败: %v", err)
		}
	}

	interestTag := &model.CustomerTagAssignment{
		CustomerID: customerID,
		Tag:        "interest:beauty",
		Category:   "interest",
		Source:     "ai_chat",
		Confidence: 0.7,
	}
	if err := database.Create(interestTag).Error; err != nil {
		t.Fatalf("seed interest tag 失败: %v", err)
	}

	rfm := &model.CustomerRFM{
		CustomerID:     customerID,
		RecencyDays:    200,
		Frequency:      1,
		Segment:        model.RFMSegmentChurn,
		ChurnRiskLevel: "high",
		ChurnScore:     85,
		ComputedAt:     time.Now(),
	}
	if err := database.Create(rfm).Error; err != nil {
		t.Fatalf("seed customer_rfm 失败: %v", err)
	}

	sess := &model.CustomerSession{
		SessionID: "sess-p4-001",
		UserID:    "user-p4-001",
		Platform:  model.PlatformDouyin,
		Status:    model.SessionStatusResolved,
	}
	if err := database.Create(sess).Error; err != nil {
		t.Fatalf("seed session 失败: %v", err)
	}
	now := time.Now()
	// 种子时刻必须写成**业务时的 20:00**，不能用 time.Local：PreferredTime 的小时
	// 直方图是 SQL 侧 `EXTRACT(HOUR FROM created_at)`，而所有测试库连接串把会话时区
	// 钉在 Asia/Shanghai ⇒ 分桶口径是业务时。写成 time.Local 的话，UTC 容器上
	// 20:00Z 存成业务时 04:00，用例跟着宿主时区漂（第二十六轮 CI 实测红因）。
	peakAt := timeutil.StartOfBusinessDay(now).Add(20 * time.Hour)
	for i := 0; i < 3; i++ {
		msg := &model.SessionMessage{
			SessionID:  sess.SessionID,
			SenderType: "user",
			CreatedAt:  peakAt.Add(time.Duration(i) * time.Minute),
		}
		if err := database.Create(msg).Error; err != nil {
			t.Fatalf("seed message 失败: %v", err)
		}
	}

	svc := &Customer360Service{
		sessionRepo: repository.NewCustomerSessionRepositoryWithDB(database),
		messageRepo: repository.NewSessionMessageRepositoryWithDB(database),
		tagRepo:     repository.NewCustomerTagAssignmentRepository(),
		insightRepo: repository.NewCustomerProfileInsightRepositoryWithDB(database),
		rfmRepo:     repository.NewCustomerRFMRepositoryWithDB(database),
		aiTagger:    NewAITagger(),
	}

	stats := &InteractionStats{TotalInteractions: 10}
	profile := svc.buildUserProfile(context.Background(), []*model.CustomerSession{sess}, stats, nil, customerID)

	if len(profile.Tags) != 5 {
		t.Fatalf("Tags 期望 top5，got %v", profile.Tags)
	}
	if profile.Tags[0] != "tag-high1" {
		t.Errorf("最高置信标签应为 tag-high1，got %s", profile.Tags[0])
	}
	for _, tag := range profile.Tags {
		if tag == "tag-low" {
			t.Errorf("第 6 位低置信标签不应入选: %v", profile.Tags)
		}
	}

	if len(profile.Interests) != 1 || profile.Interests[0] != "beauty" {
		t.Errorf("Interests 期望 [beauty]，got %v", profile.Interests)
	}

	if profile.RiskLevel != "high" {
		t.Errorf("RiskLevel 期望 high，got %q", profile.RiskLevel)
	}

	if profile.PreferredTime != "20:00-22:00" {
		t.Errorf("PreferredTime 期望 20:00-22:00，got %q", profile.PreferredTime)
	}
}

// TestBuildUserProfile_NoCustomerID_KeepsDefaults 列表路径（customerID 空）不富化、不报错
func TestBuildUserProfile_NoCustomerID_KeepsDefaults(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.CustomerTagAssignment{},
		&model.CustomerRFM{},
	)

	svc := &Customer360Service{
		sessionRepo: repository.NewCustomerSessionRepositoryWithDB(database),
		messageRepo: repository.NewSessionMessageRepositoryWithDB(database),
		tagRepo:     repository.NewCustomerTagAssignmentRepository(),
		insightRepo: repository.NewCustomerProfileInsightRepositoryWithDB(database),
		rfmRepo:     repository.NewCustomerRFMRepositoryWithDB(database),
		aiTagger:    NewAITagger(),
	}

	profile := svc.buildUserProfile(context.Background(), nil, &InteractionStats{}, nil, "")
	if len(profile.Tags) != 0 || len(profile.Interests) != 0 {
		t.Errorf("未富化时 Tags/Interests 应为空")
	}
	if profile.RiskLevel != "normal" || profile.PurchasePower != "medium" {
		t.Errorf("未富化时应保持默认值，got %+v", profile)
	}
}
