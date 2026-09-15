package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// TestSOPTemplate_Render_BasicVars 测试基本变量替换
func TestSOPTemplate_Render_BasicVars(t *testing.T) {
	svc := &SOPTemplateService{}
	vars := map[string]any{
		"customer_id":  "张三",
		"product_name": "纸皮核桃",
		"agent_name":   "顺丰",
	}
	tpl := "亲 {{.customer_id}}，{{.product_name}} 发 {{.agent_name}} 哦"
	got, err := svc.Render(tpl, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "亲 张三，纸皮核桃 发 顺丰 哦"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestSOPTemplate_Render_EmptyTemplate 测试空模板
func TestSOPTemplate_Render_EmptyTemplate(t *testing.T) {
	svc := &SOPTemplateService{}
	got, err := svc.Render("", map[string]any{"a": 1})
	if err != nil {
		t.Errorf("expected nil error for empty template, got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestSOPTemplate_Render_MissingKey 测试缺失变量 (missingkey=zero 模式)
func TestSOPTemplate_Render_MissingKey(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := "你好 {{.unknown_var}}"
	got, err := svc.Render(tpl, map[string]any{})
	if err != nil {
		t.Fatalf("expected nil error with missingkey=zero, got %v", err)
	}
	if !strings.Contains(got, "你好") {
		t.Errorf("expected result to contain 你好, got %q", got)
	}
}

// TestSOPTemplate_Render_InvalidSyntax 测试非法模板语法
func TestSOPTemplate_Render_InvalidSyntax(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := "非法 {{ .unclosed"
	_, err := svc.Render(tpl, nil)
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

// TestSOPTemplate_ShouldSkipLLM_HighConfidence 测试高置信度跳过 LLM
func TestSOPTemplate_ShouldSkipLLM_HighConfidence(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := &model.SOPTemplate{Confidence: 0.85}
	if !svc.ShouldSkipLLM(tpl) {
		t.Error("expected skip=true for confidence=0.85")
	}
}

// TestSOPTemplate_ShouldSkipLLM_LowConfidence 测试低置信度不跳过
func TestSOPTemplate_ShouldSkipLLM_LowConfidence(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := &model.SOPTemplate{Confidence: 0.5}
	if svc.ShouldSkipLLM(tpl) {
		t.Error("expected skip=false for confidence=0.5")
	}
}

// TestSOPTemplate_ShouldSkipLLM_NilTemplate 测试 nil 模板
func TestSOPTemplate_ShouldSkipLLM_NilTemplate(t *testing.T) {
	svc := &SOPTemplateService{}
	if svc.ShouldSkipLLM(nil) {
		t.Error("expected skip=false for nil template")
	}
}

// TestSOPTemplate_BuildLayer1Reply 测试完整 Layer1 回复构造
func TestSOPTemplate_BuildLayer1Reply(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := &model.SOPTemplate{
		Template:   "{{.intent}} 阶段 {{.stage}}: 你好",
		Confidence: 0.9,
	}
	vars := map[string]any{
		"intent": "greeting",
		"stage":  "initial",
	}
	got, err := svc.BuildLayer1Reply(tpl, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "greeting 阶段 initial: 你好"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestSOPTemplate_BuildLayer1Reply_NilTpl 测试 nil 模板
func TestSOPTemplate_BuildLayer1Reply_NilTpl(t *testing.T) {
	svc := &SOPTemplateService{}
	got, err := svc.BuildLayer1Reply(nil, nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// TestSOPTemplate_IncrementHitCount_NilRepo 测试 nil repo + id=0 安全
func TestSOPTemplate_IncrementHitCount_NilRepo(t *testing.T) {
	svc := &SOPTemplateService{}
	svc.IncrementHitCount(nil, 0)
	svc.IncrementHitCount(nil, 1)
}

// TestSOPTemplate_InvalidateCache_NilSafe 测试 nil cache 安全
//
// Task 16: InvalidateCache 现在要求传 agentID 参数, agentID=0 表示失效共享池。
func TestSOPTemplate_InvalidateCache_NilSafe(t *testing.T) {
	svc := &SOPTemplateService{}
	svc.InvalidateCache(0)
	svc.InvalidateCache(1)
}

// TestSOPTemplate_MatchByIntent_NilRepo 测试空仓库
func TestSOPTemplate_MatchByIntent_NilRepo(t *testing.T) {
	svc := &SOPTemplateService{repo: nil}
	got, err := svc.MatchByIntent(nil, "logistics")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil matches for nil repo, got %v", got)
	}

	got2, _ := svc.MatchByIntent(nil, "")
	if got2 != nil {
		t.Errorf("expected nil for empty intent, got %v", got2)
	}
}

// TestSOPTemplate_MatchByIntentStage_NilRepo 测试 (intent, stage) 空仓库
func TestSOPTemplate_MatchByIntentStage_NilRepo(t *testing.T) {
	svc := &SOPTemplateService{repo: nil}
	got, err := svc.MatchByIntentStage(nil, "logistics", "initial")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil matches for nil repo, got %v", got)
	}
}

// TestSOPTemplate_Render_UserMessageNotAllowed 验证 user_message 不入模板
//
// 攻击场景: 模板里写 {{.user_message}}, 攻击者输入 {{ .Intent }} 之类的模板注入。
// 修复: Render 走白名单 filterWhitelistVars, user_message 被丢弃。
func TestSOPTemplate_Render_UserMessageNotAllowed(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := "USER=[{{.user_message}}]"
	vars := map[string]any{
		"customer_id":  "c123",
		"intent":       "logistics",
		"user_message": "{{.SneakyTemplate}}",
	}
	got, err := svc.Render(tpl, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "SneakyTemplate") {
		t.Errorf("user_message should be filtered, but got leaked payload: %q", got)
	}
	if !strings.Contains(got, "USER=[<no value>]") {
		t.Errorf("expected user_message to be <no value>, got %q", got)
	}
}

// TestSOPTemplate_Render_OnlyWhitelistVars 验证只有白名单字段透传
func TestSOPTemplate_Render_OnlyWhitelistVars(t *testing.T) {
	svc := &SOPTemplateService{}
	tpl := "客户={{.customer_id}} 意图={{.intent}} 阶段={{.stage}} 坐席={{.agent_name}} 商品={{.product_name}} 意图名={{.intent_name}}"
	vars := map[string]any{
		"customer_id":  "c001",
		"intent":       "logistics",
		"stage":        "initial",
		"agent_name":   "小薇",
		"product_name": "纸皮核桃",
		"intent_name":  "物流查询",
		"user_message": "SHOULD_NOT_APPEAR",
		"admin_secret": "topsecret",
		"jwt_token":    "ey...",
		"db_password":  "p@ssw0rd",
	}
	got, err := svc.Render(tpl, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "客户=c001 意图=logistics 阶段=initial 坐席=小薇 商品=纸皮核桃 意图名=物流查询"
	if got != want {
		t.Errorf("whitelist mismatch:\n got:  %q\n want: %q", got, want)
	}
	for _, leaked := range []string{"SHOULD_NOT_APPEAR", "topsecret", "ey...", "p@ssw0rd"} {
		if strings.Contains(got, leaked) {
			t.Errorf("non-whitelisted field leaked: %q", leaked)
		}
	}
}

// TestSOPTemplate_filterWhitelistVars_NilSafe 验证 nil 入参安全
func TestSOPTemplate_filterWhitelistVars_NilSafe(t *testing.T) {
	out := filterWhitelistVars(nil)
	if out == nil {
		t.Error("expected non-nil empty map for nil input")
	}
	if len(out) != 0 {
		t.Errorf("expected empty map, got %v", out)
	}
}

// TestSOPTemplate_filterWhitelistVars_DoesNotMutate 验证不修改入参
func TestSOPTemplate_filterWhitelistVars_DoesNotMutate(t *testing.T) {
	vars := map[string]any{
		"customer_id":  "c1",
		"user_message": "leak",
	}
	_ = filterWhitelistVars(vars)
	if v, ok := vars["user_message"]; !ok || v != "leak" {
		t.Errorf("filterWhitelistVars mutated input map, user_message=%v", v)
	}
}

type mockSOPRepoForTask16 struct {
	byAgent         map[uint][]model.SOPTemplate
	listByAgentErr  error
	matchByAgentErr error
	matchCalls      []struct {
		AgentID uint
		Intent  string
		Stage   string
	}
	createCalls []*model.SOPTemplate
}

func (m *mockSOPRepoForTask16) Create(ctx context.Context, tpl *model.SOPTemplate) error {
	m.createCalls = append(m.createCalls, tpl)
	return nil
}

func (m *mockSOPRepoForTask16) GetByID(ctx context.Context, id uint) (*model.SOPTemplate, error) {
	for _, tpls := range m.byAgent {
		for i := range tpls {
			if tpls[i].ID == id {
				return &tpls[i], nil
			}
		}
	}
	return nil, nil
}

func (m *mockSOPRepoForTask16) ListEnabled(ctx context.Context, limit int) ([]model.SOPTemplate, error) {
	out := make([]model.SOPTemplate, 0)
	for _, tpls := range m.byAgent {
		out = append(out, tpls...)
	}
	return out, nil
}

func (m *mockSOPRepoForTask16) MatchByIntent(ctx context.Context, intent string) ([]model.SOPTemplate, error) {
	return nil, nil
}

func (m *mockSOPRepoForTask16) MatchByIntentStage(ctx context.Context, intent, stage string) ([]model.SOPTemplate, error) {
	return nil, nil
}

func (m *mockSOPRepoForTask16) MatchByAgent(ctx context.Context, agentID uint, intent, stage string) ([]model.SOPTemplate, error) {
	m.matchCalls = append(m.matchCalls, struct {
		AgentID uint
		Intent  string
		Stage   string
	}{agentID, intent, stage})
	if m.matchByAgentErr != nil {
		return nil, m.matchByAgentErr
	}
	if agentID == 0 {
		return nil, nil
	}
	src := m.byAgent[agentID]
	out := make([]model.SOPTemplate, 0, len(src))
	for _, t := range src {
		if t.Enabled != nil && !*t.Enabled {
			continue
		}
		if intent != "" && t.Intent != intent {
			continue
		}
		if stage != "" && t.Stage != stage {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (m *mockSOPRepoForTask16) ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.SOPTemplate, error) {
	if m.listByAgentErr != nil {
		return nil, m.listByAgentErr
	}
	if agentID == 0 {
		return nil, nil
	}
	src := m.byAgent[agentID]
	if limit > 0 && len(src) > limit {
		src = src[:limit]
	}
	return src, nil
}

func (m *mockSOPRepoForTask16) IncrementHitCount(ctx context.Context, id uint) error {
	return nil
}

func (m *mockSOPRepoForTask16) ListWithFilter(ctx context.Context, filter repository.SOPTemplateListParams) ([]model.SOPTemplate, int64, error) {
	return nil, 0, nil
}

func (m *mockSOPRepoForTask16) Update(ctx context.Context, id uint, tpl *model.SOPTemplate) error {
	return nil
}

func (m *mockSOPRepoForTask16) Delete(ctx context.Context, id uint) error {
	return nil
}

type mockSOPBindingRepoForTask16 struct{}

func (m *mockSOPBindingRepoForTask16) ListByAgent(ctx context.Context, agentID uint, kbType string) ([]model.AgentKBBinding, error) {
	return nil, nil
}

// TestSOPTemplate_MatchByAgent_AgentIDZero Task 16: agentID=0 直接返回 nil
func TestSOPTemplate_MatchByAgent_AgentIDZero(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Name: "t1", Intent: "logistics", Stage: "initial", Template: "x", Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 0, "logistics", "initial", 3)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for agentID=0, got %v", matches)
	}
	if len(repo.matchCalls) != 0 {
		t.Errorf("repo should not be called for agentID=0, but got %d calls", len(repo.matchCalls))
	}
}

// TestSOPTemplate_MatchByAgent_Bound Task 16: agentID>0 走 MatchByAgent
func TestSOPTemplate_MatchByAgent_Bound(t *testing.T) {
	enabled := true
	a1 := uint(1)
	a2 := uint(2)
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {
				{ID: 10, Name: "t1", Intent: "logistics", Stage: "initial", Template: "x", Confidence: 0.85, Enabled: &enabled, AgentID: &a1},
				{ID: 11, Name: "t2", Intent: "logistics", Stage: "initial", Template: "y", Confidence: 0.75, Enabled: &enabled, AgentID: &a1},
			},
			2: {
				{ID: 20, Name: "t3", Intent: "pricing", Stage: "initial", Template: "z", Confidence: 0.9, Enabled: &enabled, AgentID: &a2},
			},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "logistics", "initial", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for agent=1, got %d", len(matches))
	}
	if len(repo.matchCalls) != 1 {
		t.Fatalf("expected 1 match call, got %d", len(repo.matchCalls))
	}
	if repo.matchCalls[0].AgentID != 1 {
		t.Errorf("expected matchAgentID=1, got %d", repo.matchCalls[0].AgentID)
	}
	if repo.matchCalls[0].Intent != "logistics" {
		t.Errorf("expected intent=logistics, got %q", repo.matchCalls[0].Intent)
	}
}

// TestSOPTemplate_MatchByAgent_TopKLimit 验证 topK 截断
func TestSOPTemplate_MatchByAgent_TopKLimit(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {
				{ID: 1, Name: "t1", Intent: "x", Template: "a", Enabled: &enabled},
				{ID: 2, Name: "t2", Intent: "x", Template: "b", Enabled: &enabled},
				{ID: 3, Name: "t3", Intent: "x", Template: "c", Enabled: &enabled},
				{ID: 4, Name: "t4", Intent: "x", Template: "d", Enabled: &enabled},
			},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "x", "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 matches after topK=2, got %d", len(matches))
	}
}

// TestSOPTemplate_MatchByAgent_DefaultTopK topK<=0 走 sopTopK=5
func TestSOPTemplate_MatchByAgent_DefaultTopK(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Name: "t1", Intent: "x", Template: "a", Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "x", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

// TestSOPTemplate_MatchByAgent_RepoError 验证 repo 错误透传
func TestSOPTemplate_MatchByAgent_RepoError(t *testing.T) {
	repo := &mockSOPRepoForTask16{matchByAgentErr: fmt.Errorf("db down")}
	svc := NewSOPTemplateServiceWithRepo(repo)

	_, err := svc.MatchByAgent(context.Background(), 1, "x", "", 3)
	if err == nil {
		t.Error("expected error when MatchByAgent fails")
	}
}

// TestSOPTemplate_MatchByAgent_NilRepo nil repo 安全
func TestSOPTemplate_MatchByAgent_NilRepo(t *testing.T) {
	svc := &SOPTemplateService{repo: nil}
	matches, err := svc.MatchByAgent(context.Background(), 1, "x", "", 3)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for nil repo, got %v", matches)
	}
}

// TestSOPTemplate_MatchByAgent_EmptyIntent 验证 intent="" 不过滤 (返回该 agent 全部)
func TestSOPTemplate_MatchByAgent_EmptyIntent(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {
				{ID: 1, Name: "t1", Intent: "logistics", Stage: "initial", Template: "a", Enabled: &enabled},
				{ID: 2, Name: "t2", Intent: "pricing", Stage: "initial", Template: "b", Enabled: &enabled},
			},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "", "", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 matches (no intent filter), got %d", len(matches))
	}
}

// TestSOPTemplate_Create_RequireAgentID Task 16: Create 必填 AgentID
func TestSOPTemplate_Create_RequireAgentID(t *testing.T) {
	repo := &mockSOPRepoForTask16{}
	svc := NewSOPTemplateServiceWithRepo(repo)

	err := svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Intent:   "x",
		Stage:    "y",
		Template: "t",
	})
	if err == nil {
		t.Error("expected error when AgentID is nil")
	}

	zero := uint(0)
	err = svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Intent:   "x",
		Stage:    "y",
		Template: "t",
		AgentID:  &zero,
	})
	if err == nil {
		t.Error("expected error when AgentID=0")
	}

	one := uint(1)
	err = svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Stage:    "y",
		Template: "t",
		AgentID:  &one,
	})
	if err == nil {
		t.Error("expected error when intent is empty")
	}

	err = svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Intent:   "x",
		Template: "t",
		AgentID:  &one,
	})
	if err == nil {
		t.Error("expected error when stage is empty")
	}

	err = svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Intent:   "x",
		Stage:    "y",
		Template: "t",
		AgentID:  &one,
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

// TestSOPTemplate_Create_NilTpl 验证 nil tpl 拒绝
func TestSOPTemplate_Create_NilTpl(t *testing.T) {
	repo := &mockSOPRepoForTask16{}
	svc := NewSOPTemplateServiceWithRepo(repo)
	err := svc.Create(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil tpl")
	}
}

// TestSOPTemplate_Create_Success 验证成功路径
func TestSOPTemplate_Create_Success(t *testing.T) {
	repo := &mockSOPRepoForTask16{}
	svc := NewSOPTemplateServiceWithRepo(repo)

	one := uint(1)
	err := svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "n",
		Intent:   "x",
		Stage:    "y",
		Template: "t",
		AgentID:  &one,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(repo.createCalls))
	}
	if repo.createCalls[0].Enabled == nil || !*repo.createCalls[0].Enabled {
		t.Error("expected Enabled=true to be auto-set")
	}
}

// TestSOPTemplate_WarmupCache_PerAgent Task 16: 按 agentID 分片预热
func TestSOPTemplate_WarmupCache_PerAgent(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Name: "t1", Intent: "x", Template: "a", Enabled: &enabled}},
			2: {{ID: 2, Name: "t2", Intent: "x", Template: "b", Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)

	if err := svc.WarmupCache(context.Background(), 1); err != nil {
		t.Fatalf("warmup agent=1: %v", err)
	}
	if err := svc.WarmupCache(context.Background(), 2); err != nil {
		t.Fatalf("warmup agent=2: %v", err)
	}
	if err := svc.WarmupCache(context.Background(), 0); err != nil {
		t.Fatalf("warmup shared: %v", err)
	}

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if _, ok := svc.cache[1]; !ok {
		t.Error("expected cache entry for agent=1")
	}
	if _, ok := svc.cache[2]; !ok {
		t.Error("expected cache entry for agent=2")
	}
	if _, ok := svc.cache[0]; !ok {
		t.Error("expected cache entry for shared pool (agentID=0)")
	}
	if len(svc.cache) != 3 {
		t.Errorf("expected 3 cache buckets, got %d", len(svc.cache))
	}
}

// TestSOPTemplate_InvalidateCache_PerAgent Task 16: 精确失效单 agent 桶
func TestSOPTemplate_InvalidateCache_PerAgent(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Enabled: &enabled}},
			2: {{ID: 2, Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)
	_ = svc.WarmupCache(context.Background(), 1)
	_ = svc.WarmupCache(context.Background(), 2)

	svc.InvalidateCache(1)

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if _, ok := svc.cache[1]; ok {
		t.Error("expected agent=1 cache to be invalidated")
	}
	if _, ok := svc.cache[2]; !ok {
		t.Error("expected agent=2 cache to be intact")
	}
}

// TestSOPTemplate_InvalidateAllCache 验证全量失效
func TestSOPTemplate_InvalidateAllCache(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Enabled: &enabled}},
			2: {{ID: 2, Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)
	_ = svc.WarmupCache(context.Background(), 1)
	_ = svc.WarmupCache(context.Background(), 2)

	svc.InvalidateAllCache()

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if len(svc.cache) != 0 {
		t.Errorf("expected 0 cache buckets, got %d", len(svc.cache))
	}
}

// TestSOPTemplate_SetBindingRepo 验证 binding 注入
func TestSOPTemplate_SetBindingRepo(t *testing.T) {
	svc := &SOPTemplateService{}
	binding := &mockSOPBindingRepoForTask16{}
	svc.SetBindingRepo(binding)
	if svc.bindingRepo == nil {
		t.Error("expected bindingRepo to be set")
	}

	original := svc.bindingRepo
	svc.SetBindingRepo(nil)
	if svc.bindingRepo != original {
		t.Error("SetBindingRepo(nil) should not overwrite existing binding")
	}
}

// TestSOPTemplate_NewSOPTemplateServiceWithRepos 验证双 repo 注入
func TestSOPTemplate_NewSOPTemplateServiceWithRepos(t *testing.T) {
	repo := &mockSOPRepoForTask16{}
	binding := &mockSOPBindingRepoForTask16{}
	svc := NewSOPTemplateServiceWithRepos(repo, binding)
	if svc.repo == nil {
		t.Error("expected repo to be set")
	}
	if svc.bindingRepo == nil {
		t.Error("expected bindingRepo to be set")
	}
	if svc.cache == nil {
		t.Error("expected cache map to be initialized")
	}
	if svc.loaded == nil {
		t.Error("expected loaded map to be initialized")
	}
}

// TestSOPTemplate_Create_InvalidatesAgentCache Task 16: Create 必填 agentID 后, 精确失效对应桶
func TestSOPTemplate_Create_InvalidatesAgentCache(t *testing.T) {
	enabled := true
	repo := &mockSOPRepoForTask16{
		byAgent: map[uint][]model.SOPTemplate{
			1: {{ID: 1, Name: "existing", Intent: "x", Stage: "y", Template: "a", Enabled: &enabled}},
		},
	}
	svc := NewSOPTemplateServiceWithRepo(repo)
	_ = svc.WarmupCache(context.Background(), 1)

	svc.mu.RLock()
	_, hasBefore := svc.cache[1]
	svc.mu.RUnlock()
	if !hasBefore {
		t.Fatal("expected cache[1] to be present after warmup")
	}

	one := uint(1)
	if err := svc.Create(context.Background(), &model.SOPTemplate{
		Name:     "new",
		Intent:   "x",
		Stage:    "y",
		Template: "t",
		AgentID:  &one,
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	svc.mu.RLock()
	_, hasAfter := svc.cache[1]
	svc.mu.RUnlock()
	if hasAfter {
		t.Error("expected cache[1] to be invalidated after Create")
	}
}
