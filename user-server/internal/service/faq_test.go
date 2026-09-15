package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

func makeFAQMatchResult(q, a, intent string, score float64) dto.FAQMatchResult {
	return dto.FAQMatchResult{
		Entry: &dto.FAQEntry{
			ID:         0,
			Question:   q,
			Answer:     a,
			Intent:     intent,
			Confidence: score,
		},
		Score:     score,
		Rank:      0,
		MatchType: "keyword",
	}
}

// TestFAQService_Match_NilRepo 测试 repo==nil 时的安全行为
func TestFAQService_Match_NilRepo(t *testing.T) {
	svc := &FAQService{repo: nil}
	matches, err := svc.Match(nil, "你好", 3)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for nil repo, got %v", matches)
	}
}

// TestFAQService_ShouldSkipLLM_HighScore 测试高分命中 -> SkipLLM=true
func TestFAQService_ShouldSkipLLM_HighScore(t *testing.T) {
	svc := &FAQService{repo: nil}
	matches := []dto.FAQMatchResult{
		makeFAQMatchResult("韵达发货吗", "韵达不发的哦", "logistics", 0.85),
	}
	skip, top := svc.ShouldSkipLLM(matches)
	if !skip {
		t.Error("expected skip=true for high-score match")
	}
	if top == nil {
		t.Fatal("expected non-nil top match")
	}
	if top.Score < faqHitThresh {
		t.Errorf("expected score >= %f, got %f", faqHitThresh, top.Score)
	}
}

// TestFAQService_ShouldSkipLLM_LowScore 测试低分命中 -> SkipLLM=false
func TestFAQService_ShouldSkipLLM_LowScore(t *testing.T) {
	svc := &FAQService{repo: nil}
	matches := []dto.FAQMatchResult{
		makeFAQMatchResult("模糊问句", "回答", "unknown", 0.3),
	}
	skip, top := svc.ShouldSkipLLM(matches)
	if skip {
		t.Error("expected skip=false for low-score match")
	}
	if top != nil {
		t.Error("expected nil top when not skipping")
	}
}

// TestFAQService_ShouldSkipLLM_Empty 测试空匹配列表 -> SkipLLM=false
func TestFAQService_ShouldSkipLLM_Empty(t *testing.T) {
	svc := &FAQService{repo: nil}
	skip, top := svc.ShouldSkipLLM(nil)
	if skip {
		t.Error("expected skip=false for empty matches")
	}
	if top != nil {
		t.Error("expected nil top for empty matches")
	}
}

// TestFAQService_ShouldSkipLLM_Boundary 测试临界值
func TestFAQService_ShouldSkipLLM_Boundary(t *testing.T) {
	svc := &FAQService{repo: nil}

	matches := []dto.FAQMatchResult{
		makeFAQMatchResult("临界", "临界回复", "logistics", faqHitThresh),
	}
	skip, _ := svc.ShouldSkipLLM(matches)
	if !skip {
		t.Errorf("expected skip=true at exact threshold %f", faqHitThresh)
	}

	matches2 := []dto.FAQMatchResult{
		makeFAQMatchResult("略低", "略低回复", "logistics", faqHitThresh-0.01),
	}
	skip2, _ := svc.ShouldSkipLLM(matches2)
	if skip2 {
		t.Errorf("expected skip=false just below threshold %f", faqHitThresh)
	}
}

// TestFAQService_IncrementHitCount_NilRepo 测试 id=0 / nil repo 安全
func TestFAQService_IncrementHitCount_NilRepo(t *testing.T) {
	svc := &FAQService{repo: nil}
	svc.IncrementHitCount(nil, 0)
	svc.IncrementHitCount(nil, 1)
}

// TestFAQService_Stats_NilRepo 测试空仓库 Stats
func TestFAQService_Stats_NilRepo(t *testing.T) {
	svc := &FAQService{repo: nil}
	total, enabled, err := svc.Stats(nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if total != 0 || enabled != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", total, enabled)
	}
}

// TestFAQService_InvalidateCache_NilSafe 测试 nil cache 也能安全失效
func TestFAQService_InvalidateCache_NilSafe(t *testing.T) {
	svc := &FAQService{repo: nil}
	svc.InvalidateCache(0)
}

type mockFAQRepoForDecay struct {
	candidates []model.FAQEntry
	cutoffSeen time.Time
	decayCalls []struct {
		ID    uint
		Decay float64
	}
	listErr         error
	decayErr        error
	agentIDSeen     uint
	msgSeen         string
	matchByAgentErr error
	listByAgentErr  error
	entriesByAgent  map[uint][]model.FAQEntry
}

func (m *mockFAQRepoForDecay) MatchByKeyword(ctx context.Context, msg string, topK int) ([]model.FAQEntry, error) {
	return nil, nil
}

func (m *mockFAQRepoForDecay) MatchByAgent(ctx context.Context, agentID uint, msg string, topK int) ([]model.FAQEntry, error) {
	m.agentIDSeen = agentID
	m.msgSeen = msg
	if m.matchByAgentErr != nil {
		return nil, m.matchByAgentErr
	}
	out := make([]model.FAQEntry, 0, len(m.entriesByAgent[agentID]))
	for _, e := range m.entriesByAgent[agentID] {
		if e.Enabled != nil && !*e.Enabled {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *mockFAQRepoForDecay) ListCandidates(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	m.agentIDSeen = agentID
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]model.FAQEntry, 0, len(m.entriesByAgent[agentID]))
	for _, e := range m.entriesByAgent[agentID] {
		if e.Enabled != nil && !*e.Enabled {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *mockFAQRepoForDecay) ScoreCandidates(ctx context.Context, entries []model.FAQEntry, msg string, topK int) ([]model.FAQEntry, error) {
	if m.matchByAgentErr != nil {
		return nil, m.matchByAgentErr
	}
	return entries, nil
}

func (m *mockFAQRepoForDecay) ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.FAQEntry, error) {
	if m.listByAgentErr != nil {
		return nil, m.listByAgentErr
	}
	out := make([]model.FAQEntry, 0, len(m.entriesByAgent[agentID]))
	for _, e := range m.entriesByAgent[agentID] {
		if e.Enabled != nil && !*e.Enabled {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *mockFAQRepoForDecay) IncrementHitCount(ctx context.Context, id uint) error {
	return nil
}

func (m *mockFAQRepoForDecay) DecayQuality(ctx context.Context, id uint, decay float64) error {
	m.decayCalls = append(m.decayCalls, struct {
		ID    uint
		Decay float64
	}{id, decay})
	if m.decayErr != nil {
		return m.decayErr
	}
	return nil
}

func (m *mockFAQRepoForDecay) ListDecayCandidates(ctx context.Context, cutoff time.Time, limit int) ([]model.FAQEntry, error) {
	m.cutoffSeen = cutoff
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.candidates, nil
}

func (m *mockFAQRepoForDecay) IncrementNegativeHit(ctx context.Context, id uint) error {
	return nil
}

func (m *mockFAQRepoForDecay) ListWithFilter(ctx context.Context, filter repository.FAQListParams) ([]model.FAQEntry, int64, error) {
	return nil, 0, nil
}

func (m *mockFAQRepoForDecay) GetByID(ctx context.Context, id uint) (*model.FAQEntry, error) {
	return nil, nil
}

func (m *mockFAQRepoForDecay) ListEnabled(ctx context.Context, limit int) ([]model.FAQEntry, error) {
	return nil, nil
}

func (m *mockFAQRepoForDecay) Create(ctx context.Context, entry *model.FAQEntry) error {
	return nil
}

func (m *mockFAQRepoForDecay) Update(ctx context.Context, id uint, entry *model.FAQEntry) error {
	return nil
}

func (m *mockFAQRepoForDecay) Delete(ctx context.Context, id uint) error {
	return nil
}

type fixedClock struct{ T time.Time }

func (f fixedClock) Now() time.Time { return f.T }

// TestFAQService_WeekDecay 测试周度质量衰减
//
// 验证:
//  1. cutoff = now - 7d
//  2. 候选列表由 ListDecayCandidates 返回, 逐条调用 DecayQuality(0.1)
//  3. 返回值为实际衰减条数
//  4. 时钟通过 mock 注入
func TestFAQService_WeekDecay(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	clock := fixedClock{T: now}
	repo := &mockFAQRepoForDecay{
		candidates: []model.FAQEntry{
			{ID: 1, HitCount: 2, LastHitAt: ptrTimeUniq(now.Add(-8 * 24 * time.Hour))},
			{ID: 2, HitCount: 0, LastHitAt: ptrTimeUniq(now.Add(-30 * 24 * time.Hour))},
			{ID: 3, HitCount: 4, LastHitAt: ptrTimeUniq(now.Add(-10 * 24 * time.Hour))},
		},
	}
	svc := NewFAQServiceWithRepo(repo)
	svc.SetClock(clock)

	decayed, err := svc.WeekDecay(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decayed != 3 {
		t.Errorf("expected decayed=3, got %d", decayed)
	}
	if len(repo.decayCalls) != 3 {
		t.Fatalf("expected 3 DecayQuality calls, got %d", len(repo.decayCalls))
	}
	for i, c := range repo.decayCalls {
		if c.ID != uint(i+1) {
			t.Errorf("call[%d].ID = %d, want %d", i, c.ID, i+1)
		}
		if c.Decay != faqDecayPerWeek {
			t.Errorf("call[%d].Decay = %f, want %f", i, c.Decay, faqDecayPerWeek)
		}
	}
	wantCutoff := now.Add(-faqDecayDays)
	if !repo.cutoffSeen.Equal(wantCutoff) {
		t.Errorf("cutoff mismatch: got %v, want %v", repo.cutoffSeen, wantCutoff)
	}
}

// TestFAQService_WeekDecay_NilRepo nil repo 安全
func TestFAQService_WeekDecay_NilRepo(t *testing.T) {
	svc := &FAQService{repo: nil, clock: fixedClock{T: time.Now()}}
	decayed, err := svc.WeekDecay(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if decayed != 0 {
		t.Errorf("expected decayed=0, got %d", decayed)
	}
}

// TestFAQService_WeekDecay_ListErr list 报错时返回错误
func TestFAQService_WeekDecay_ListErr(t *testing.T) {
	repo := &mockFAQRepoForDecay{listErr: fmt.Errorf("db down")}
	svc := NewFAQServiceWithRepo(repo)
	_, err := svc.WeekDecay(context.Background())
	if err == nil {
		t.Error("expected error when ListDecayCandidates fails")
	}
}

func ptrTimeUniq(t time.Time) *time.Time { return &t }

// TestFAQService_MatchByAgent_AgentIDZero 验证 agentID=0 直接返回 nil (移除"空数组=全局"分支)
func TestFAQService_MatchByAgent_AgentIDZero(t *testing.T) {
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 1, Question: "q", Answer: "a", Enabled: ptrBoolUniq(true)},
				{ID: 2, Question: "q2", Answer: "a2", Enabled: ptrBoolUniq(true)}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 0, "test", 3)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for agentID=0, got %v", matches)
	}
	if repo.agentIDSeen != 0 {
		t.Errorf("repo should not be called for agentID=0, but agentIDSeen=%d", repo.agentIDSeen)
	}
}

// TestFAQService_MatchByAgent_Bound 验证 agentID>0 走 MatchByAgent
func TestFAQService_MatchByAgent_Bound(t *testing.T) {
	a1 := uint(1)
	a2 := uint(2)
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {
				{ID: 10, Question: "韵达发货吗", Answer: "韵达不发的哦", Intent: "logistics", Confidence: 0.9, Enabled: &enabled, AgentID: &a1},
			},
			2: {
				{ID: 20, Question: "可以优惠价吗", Answer: "200 把起优惠", Intent: "pricing", Confidence: 0.85, Enabled: &enabled, AgentID: &a2},
			},
		},
	}
	svc := NewFAQServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "韵达发货吗", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for agent=1, got %d", len(matches))
	}
	if matches[0].Entry == nil || matches[0].Entry.ID != 10 {
		t.Errorf("expected match ID=10, got %v", matches[0].Entry)
	}
	if matches[0].MatchType != "agent_bound" {
		t.Errorf("expected match_type=agent_bound, got %q", matches[0].MatchType)
	}
	if repo.agentIDSeen != 1 {
		t.Errorf("expected agentIDSeen=1, got %d", repo.agentIDSeen)
	}
}

// TestFAQService_MatchByAgent_AgentIDMismatch 验证 agentID 不匹配时返回空
func TestFAQService_MatchByAgent_AgentIDMismatch(t *testing.T) {
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 10, Question: "q", Answer: "a", Enabled: &enabled}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 99, "test", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for agent=99, got %d", len(matches))
	}
}

// TestFAQService_MatchByAgent_NilRepo 验证 nil repo 安全
func TestFAQService_MatchByAgent_NilRepo(t *testing.T) {
	svc := &FAQService{repo: nil}
	matches, err := svc.MatchByAgent(context.Background(), 1, "test", 3)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for nil repo, got %v", matches)
	}
}

// TestFAQService_MatchByAgent_RepoError 验证 repo 报错时透传
func TestFAQService_MatchByAgent_RepoError(t *testing.T) {
	repo := &mockFAQRepoForDecay{matchByAgentErr: fmt.Errorf("db down")}
	svc := NewFAQServiceWithRepo(repo)

	_, err := svc.MatchByAgent(context.Background(), 1, "test", 3)
	if err == nil {
		t.Error("expected error when MatchByAgent fails")
	}
}

// TestFAQService_MatchByAgent_DisabledEntries 验证 enabled=false 被过滤
func TestFAQService_MatchByAgent_DisabledEntries(t *testing.T) {
	enabled := true
	disabled := false
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {
				{ID: 1, Question: "q1", Answer: "a1", Enabled: &enabled},
				{ID: 2, Question: "q2", Answer: "a2", Enabled: &disabled},
			},
		},
	}
	svc := NewFAQServiceWithRepo(repo)

	matches, err := svc.MatchByAgent(context.Background(), 1, "q", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 enabled match, got %d", len(matches))
	}
	if matches[0].Entry != nil && matches[0].Entry.ID != 1 {
		t.Errorf("expected ID=1, got %d", matches[0].Entry.ID)
	}
}

// TestFAQService_Create_RequireAgentID Task 15: Create 必填 AgentID
func TestFAQService_Create_RequireAgentID(t *testing.T) {
	repo := &mockFAQRepoForDecay{}
	svc := NewFAQServiceWithRepo(repo)

	err := svc.Create(context.Background(), &model.FAQEntry{
		Question: "q",
		Answer:   "a",
	})
	if err == nil {
		t.Error("expected error when AgentID is nil")
	}

	zero := uint(0)
	err = svc.Create(context.Background(), &model.FAQEntry{
		Question: "q",
		Answer:   "a",
		AgentID:  &zero,
	})
	if err == nil {
		t.Error("expected error when AgentID=0")
	}

	one := uint(1)
	err = svc.Create(context.Background(), &model.FAQEntry{
		Question: "q",
		Answer:   "a",
		AgentID:  &one,
	})
	if err != nil {
		t.Errorf("unexpected error for valid AgentID, got %v", err)
	}
}

// TestFAQService_Create_NilEntry 验证 nil entry 拒绝
func TestFAQService_Create_NilEntry(t *testing.T) {
	repo := &mockFAQRepoForDecay{}
	svc := NewFAQServiceWithRepo(repo)
	err := svc.Create(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil entry")
	}
}

// TestFAQService_WarmupCache_PerAgent Task 15: 按 agentID 分片预热
func TestFAQService_WarmupCache_PerAgent(t *testing.T) {
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 1, Question: "q1", Answer: "a1", Enabled: &enabled}},
			2: {{ID: 2, Question: "q2", Answer: "a2", Enabled: &enabled}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)

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

// TestFAQService_InvalidateCache_PerAgent Task 15: 精确失效单 agent 桶
func TestFAQService_InvalidateCache_PerAgent(t *testing.T) {
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 1, Enabled: &enabled}},
			2: {{ID: 2, Enabled: &enabled}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)
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

// TestFAQService_InvalidateAllCache 验证全量失效
func TestFAQService_InvalidateAllCache(t *testing.T) {
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 1, Enabled: &enabled}},
			2: {{ID: 2, Enabled: &enabled}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)
	_ = svc.WarmupCache(context.Background(), 1)
	_ = svc.WarmupCache(context.Background(), 2)

	svc.InvalidateAllCache()

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if len(svc.cache) != 0 {
		t.Errorf("expected 0 cache buckets, got %d", len(svc.cache))
	}
}

// TestFAQService_MatchByAgent_EmptyMsg 验证空消息直接返回 nil
func TestFAQService_MatchByAgent_EmptyMsg(t *testing.T) {
	repo := &mockFAQRepoForDecay{}
	svc := NewFAQServiceWithRepo(repo)
	matches, err := svc.MatchByAgent(context.Background(), 1, "", 3)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if matches != nil {
		t.Errorf("expected nil matches for empty msg, got %v", matches)
	}
	if repo.agentIDSeen != 0 {
		t.Errorf("repo should not be called for empty msg, but agentIDSeen=%d", repo.agentIDSeen)
	}
}

// TestFAQService_MatchByAgent_DefaultTopK 验证 topK<=0 走默认
func TestFAQService_MatchByAgent_DefaultTopK(t *testing.T) {
	enabled := true
	repo := &mockFAQRepoForDecay{
		entriesByAgent: map[uint][]model.FAQEntry{
			1: {{ID: 1, Question: "q1", Answer: "a1", Enabled: &enabled}},
		},
	}
	svc := NewFAQServiceWithRepo(repo)
	matches, err := svc.MatchByAgent(context.Background(), 1, "q", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches == nil {
		t.Error("expected non-nil matches for valid input")
	}
}

func ptrBoolUniq(b bool) *bool { return &b }
