package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type leadMiningTenantConfigRepo struct {
	configs map[uint]*model.LeadMiningConfig
	calls   map[uint]int
}

func (r *leadMiningTenantConfigRepo) GetSingleton(ctx context.Context) (*model.LeadMiningConfig, error) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, lmErrNotFound
	}
	r.calls[tenantID]++
	return cloneLeadMiningConfig(r.configs[tenantID]), nil
}

func (r *leadMiningTenantConfigRepo) Save(context.Context, *model.LeadMiningConfig) error { return nil }

type leadMiningTenantCustomerRepo struct {
	repository.CustomerRepository
	byTenant map[uint]*fakeCustRepo
	seen     []uint
}

func (r *leadMiningTenantCustomerRepo) scoped(ctx context.Context) (uint, *fakeCustRepo) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		r.seen = append(r.seen, 0)
		return 0, nil
	}
	r.seen = append(r.seen, tenantID)
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = newFakeCustRepo()
	}
	return tenantID, r.byTenant[tenantID]
}

func (r *leadMiningTenantCustomerRepo) GetByUnifiedID(ctx context.Context, id string) (*model.Customer, error) {
	_, repo := r.scoped(ctx)
	if repo == nil {
		return nil, lmErrNotFound
	}
	return repo.GetByUnifiedID(ctx, id)
}

func (r *leadMiningTenantCustomerRepo) Create(ctx context.Context, customer *model.Customer) error {
	tenantID, repo := r.scoped(ctx)
	if repo == nil {
		return lmErrNotFound
	}
	customer.TenantID = tenantID
	return repo.Create(ctx, customer)
}

type leadMiningTenantClueRepo struct {
	repository.ClueRepository
	byTenant map[uint]*fakeClueRepo
	seen     []uint
}

func (r *leadMiningTenantClueRepo) scoped(ctx context.Context) (uint, *fakeClueRepo) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok {
		r.seen = append(r.seen, 0)
		return 0, nil
	}
	r.seen = append(r.seen, tenantID)
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = newFakeClueRepo()
	}
	return tenantID, r.byTenant[tenantID]
}

func (r *leadMiningTenantClueRepo) FindByTypeAndAccount(ctx context.Context, clueType int64, account string) (*model.Clue, error) {
	_, repo := r.scoped(ctx)
	if repo == nil {
		return nil, lmErrNotFound
	}
	return repo.FindByTypeAndAccount(ctx, clueType, account)
}

func (r *leadMiningTenantClueRepo) Create(ctx context.Context, clue *model.Clue) error {
	tenantID, repo := r.scoped(ctx)
	if repo == nil {
		return lmErrNotFound
	}
	clue.TenantID = tenantID
	return repo.Create(ctx, clue)
}

type leadMiningTenantJudge struct {
	calls   int
	isLead  bool
	configs []*model.LeadMiningConfig
}

func (j *leadMiningTenantJudge) Judge(_ context.Context, cfg *model.LeadMiningConfig, _ []llm.ChatMessage) (*LeadJudgement, error) {
	j.calls++
	j.configs = append(j.configs, cloneLeadMiningConfig(cfg))
	return &LeadJudgement{IsLead: j.isLead, IntentScore: cfg.MinIntentScore, Summary: "tenant scoped"}, nil
}

func newTenantScopedLeadMiningService(repo *leadMiningTenantConfigRepo, judge LLMJudge) *Service {
	return newTenantScopedLeadMiningServiceWithRepos(
		repo,
		judge,
		&leadMiningTenantCustomerRepo{byTenant: make(map[uint]*fakeCustRepo)},
		&leadMiningTenantClueRepo{byTenant: make(map[uint]*fakeClueRepo)},
	)
}

func newTenantScopedLeadMiningServiceWithRepos(repo *leadMiningTenantConfigRepo, judge LLMJudge, customerRepo repository.CustomerRepository, clueRepo repository.ClueRepository) *Service {
	return &Service{
		judge:          judge,
		historyFetcher: cannedHistory(),
		custRepo:       customerRepo,
		clueRepo:       clueRepo,
		cfgRepo:        repo,
		lastJudge:      make(map[string]time.Time),
		cfgCache:       make(map[uint]leadMiningConfigCacheEntry),
		cfgGeneration:  make(map[uint]uint64),
	}
}

func TestLeadMiningTenant_ConfigCacheAndDebounceAreIsolated(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })

	repo := &leadMiningTenantConfigRepo{
		configs: map[uint]*model.LeadMiningConfig{
			41: {TenantID: 41, Enabled: true, MinIntentScore: 31},
			42: {TenantID: 42, Enabled: true, MinIntentScore: 82},
		},
		calls: make(map[uint]int),
	}
	judge := &leadMiningTenantJudge{isLead: true}
	customerRepo := &leadMiningTenantCustomerRepo{byTenant: make(map[uint]*fakeCustRepo)}
	clueRepo := &leadMiningTenantClueRepo{byTenant: make(map[uint]*fakeClueRepo)}
	svc := newTenantScopedLeadMiningServiceWithRepos(repo, judge, customerRepo, clueRepo)

	hubA := sampleHub("douyin", "shared-sender", "A 消息")
	hubA.TenantID = 41
	hubB := sampleHub("douyin", "shared-sender", "B 消息")
	hubB.TenantID = 42

	svc.process(context.Background(), hubA)
	svc.process(context.Background(), hubB)
	svc.process(context.Background(), hubA)
	svc.process(context.Background(), hubB)

	if repo.calls[41] != 1 || repo.calls[42] != 1 {
		t.Fatalf("配置应按租户各读取一次并命中各自缓存，实际 A=%d B=%d", repo.calls[41], repo.calls[42])
	}
	if judge.calls != 2 {
		t.Fatalf("相同外部身份的不同租户都应各判定一次、同租户窗口内抑制重复，实际 %d 次", judge.calls)
	}
	if len(judge.configs) != 2 || judge.configs[0].MinIntentScore != 31 || judge.configs[1].MinIntentScore != 82 {
		t.Fatalf("Judge 未收到各租户自己的配置，实际 %+v", judge.configs)
	}
	if len(customerRepo.seen) < 4 || len(clueRepo.seen) < 4 {
		t.Fatalf("A/B 消息各执行两次时应触达客户/线索仓储，客户=%v 线索=%v", customerRepo.seen, clueRepo.seen)
	}
	for _, scopedReads := range [][]uint{customerRepo.seen, clueRepo.seen} {
		counts := make(map[uint]int)
		for _, tenantID := range scopedReads {
			counts[tenantID]++
		}
		if counts[41] == 0 || counts[42] == 0 || counts[41] != counts[42] || len(counts) != 2 {
			t.Fatalf("客户与线索读写必须始终带所属租户且两租户均衡，作用域记录=%v", scopedReads)
		}
	}
	for tenantID, repo := range customerRepo.byTenant {
		for _, customer := range repo.byID {
			if customer.TenantID != tenantID {
				t.Fatalf("客户归属错误: map tenant=%d row tenant=%d", tenantID, customer.TenantID)
			}
		}
	}
	for tenantID, repo := range clueRepo.byTenant {
		for _, clue := range repo.byID {
			if clue.TenantID != tenantID {
				t.Fatalf("线索归属错误: map tenant=%d row tenant=%d", tenantID, clue.TenantID)
			}
		}
	}
}

func TestLeadMiningTenant_TargetedReloadOnlyInvalidatesSelectedTenant(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })

	repo := &leadMiningTenantConfigRepo{
		configs: map[uint]*model.LeadMiningConfig{
			51: {TenantID: 51, Enabled: true, MinIntentScore: 30},
			52: {TenantID: 52, Enabled: true, MinIntentScore: 80},
		},
		calls: make(map[uint]int),
	}
	svc := newTenantScopedLeadMiningService(repo, &leadMiningTenantJudge{})
	previousSingleton := singleton
	singleton = svc
	t.Cleanup(func() { singleton = previousSingleton })
	ctxA := tenant.OnlyTenant(context.Background(), 51)
	ctxB := tenant.OnlyTenant(context.Background(), 52)

	cfgA := svc.loadConfig(ctxA)
	cfgB := svc.loadConfig(ctxB)
	cfgA.Keywords = append(cfgA.Keywords, "caller mutation")
	cfgB.MinIntentScore = -1
	if cfg := svc.loadConfig(ctxA); len(cfg.Keywords) != 0 || cfg.MinIntentScore != 30 {
		t.Fatalf("调用方修改配置不应污染租户 A 缓存，实际 %+v", cfg)
	}

	repo.configs[51] = &model.LeadMiningConfig{TenantID: 51, Enabled: true, MinIntentScore: 45}
	ReloadConfigCacheForTenant(51)

	if cfg := svc.loadConfig(ctxB); cfg.MinIntentScore != 80 {
		t.Fatalf("租户 A 失效不得改变租户 B 缓存，实际 %+v", cfg)
	}
	if cfg := svc.loadConfig(ctxA); cfg.MinIntentScore != 45 {
		t.Fatalf("租户 A 定向失效后应读到新配置，实际 %+v", cfg)
	}
	if repo.calls[51] != 2 || repo.calls[52] != 1 {
		t.Fatalf("定向失效读数不符，实际 A=%d B=%d", repo.calls[51], repo.calls[52])
	}
}

func TestLeadMiningTenant_MissingTenantRejectedBeforeConfigRead(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })

	repo := &leadMiningTenantConfigRepo{
		configs: make(map[uint]*model.LeadMiningConfig),
		calls:   make(map[uint]int),
	}
	judge := &leadMiningTenantJudge{}
	svc := newTenantScopedLeadMiningService(repo, judge)

	svc.process(context.Background(), sampleHub("douyin", "unknown-sender", "无租户消息"))

	if len(repo.calls) != 0 || judge.calls != 0 {
		t.Fatalf("缺少租户归属必须在读取配置和调用 Judge 前拒绝，配置读取=%v Judge=%d", repo.calls, judge.calls)
	}
}

func TestLeadMiningTenant_DebounceKeyIncludesTenant(t *testing.T) {
	keyA := leadMiningDebounceKey(101, "douyin", "", "same-sender", false)
	keyB := leadMiningDebounceKey(202, "douyin", "", "same-sender", false)
	if keyA == keyB {
		t.Fatalf("不同租户的相同平台/发送者必须得到不同去抖键: %q", keyA)
	}
	if got := leadMiningDebounceKey(tenant.DefaultTenantID, "douyin", "", "same-sender", false); got != "douyin:same-sender" {
		t.Fatalf("默认租户应保持历史单租户键以兼容现有调用与测试，实际 %q", got)
	}
}
