package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

type fakeCustRepo struct {
	mu    sync.Mutex
	byID  map[string]*model.Customer
	byUID map[string]*model.Customer
	seq   int
}

func newFakeCustRepo() *fakeCustRepo {
	return &fakeCustRepo{byID: map[string]*model.Customer{}, byUID: map[string]*model.Customer{}}
}

func (f *fakeCustRepo) Create(_ context.Context, c *model.Customer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	c.ID = "cust-" + lmItoa(f.seq)
	f.byID[c.ID] = c
	if c.UnifiedID != "" {
		f.byUID[c.UnifiedID] = c
	}
	return nil
}
func (f *fakeCustRepo) GetByID(_ context.Context, id string) (*model.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id], nil
}
func (f *fakeCustRepo) GetByUnifiedID(_ context.Context, uid string) (*model.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byUID[uid], nil
}
func (f *fakeCustRepo) Update(_ context.Context, c *model.Customer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[c.ID] = c
	if c.UnifiedID != "" {
		f.byUID[c.UnifiedID] = c
	}
	return nil
}
func (f *fakeCustRepo) GetByPhone(context.Context, string) (*model.Customer, error) { return nil, nil }
func (f *fakeCustRepo) GetByEmail(context.Context, string) (*model.Customer, error) { return nil, nil }
func (f *fakeCustRepo) GetByWechatOpenID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) GetByDouyinOpenID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) GetByXiaohongshuID(context.Context, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) Delete(context.Context, string) error { return nil }
func (f *fakeCustRepo) List(context.Context, int, int, string) ([]*model.Customer, int64, error) {
	return nil, 0, nil
}
func (f *fakeCustRepo) ListKeyset(context.Context, string, int, string) ([]*model.Customer, int64, string, error) {
	return nil, 0, "", nil
}
func (f *fakeCustRepo) FindByIdentity(context.Context, string, string, string, string, string) (*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) FindByIdentityAll(context.Context, string, string, string, string, string) ([]*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) ReassignSessionOneID(context.Context, string, string) error {
	return nil
}
func (f *fakeCustRepo) ReassignOneID(context.Context, string, string, string) error { return nil }
func (f *fakeCustRepo) ReassignDNCOneID(context.Context, string, string) error      { return nil }
func (f *fakeCustRepo) CountNotEmpty(context.Context, string) (int64, error)        { return 0, nil }
func (f *fakeCustRepo) CountMultiIdentity(context.Context) (int64, error)           { return 0, nil }
func (f *fakeCustRepo) ListByIDs(context.Context, []string) (map[string]*model.Customer, error) {
	return nil, nil
}
func (f *fakeCustRepo) SearchByFilter(context.Context, repository.CustomerSearchFilter) ([]*model.Customer, int64, error) {
	return nil, 0, nil
}
func (f *fakeCustRepo) WithTransaction(_ context.Context, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

// 线索测试替身不模拟客户合并事务；合并契约由真实 PostgreSQL 回归覆盖。
func (f *fakeCustRepo) LockMergeCustomers(context.Context, string, string) (*model.Customer, *model.Customer, error) {
	return nil, nil, lmErrNotFound
}
func (f *fakeCustRepo) LinkConsentIdentities(context.Context, string, string) error {
	return lmErrNotFound
}

type fakeClueRepo struct {
	mu         sync.Mutex
	byID       map[string]*model.Clue
	byTypeAcct map[string]*model.Clue
	seq        int
	createCnt  int
	updateCnt  int
}

func newFakeClueRepo() *fakeClueRepo {
	return &fakeClueRepo{byID: map[string]*model.Clue{}, byTypeAcct: map[string]*model.Clue{}}
}

func (f *fakeClueRepo) Create(_ context.Context, c *model.Clue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := lmItoa(int(c.Type)) + ":" + c.Account
	if _, ok := f.byTypeAcct[key]; ok {
		return lmErrDuplicate
	}
	f.seq++
	c.ID = "clue-" + lmItoa(f.seq)
	f.byID[c.ID] = c
	f.byTypeAcct[key] = c
	f.createCnt++
	return nil
}

// GetByIDs 按 ID 批量取（群发按 ID 精确取人，见 bulkMessaging 修复）
func (f *fakeClueRepo) GetByIDs(_ context.Context, ids []string) ([]*model.Clue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*model.Clue, 0, len(ids))
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeClueRepo) GetByID(_ context.Context, id string) (*model.Clue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id], nil
}
func (f *fakeClueRepo) FindByTypeAndAccount(_ context.Context, t int64, acct string) (*model.Clue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byTypeAcct[lmItoa(int(t))+":"+acct], nil
}
func (f *fakeClueRepo) UpdateByID(_ context.Context, id string, _ map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[id]; !ok {
		return lmErrNotFound
	}
	f.updateCnt++
	return nil
}
func (f *fakeClueRepo) ExistsByTypeAndAccount(_ context.Context, t int64, acct string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.byTypeAcct[lmItoa(int(t))+":"+acct]
	return ok, nil
}
func (f *fakeClueRepo) GetClueList(context.Context, int, int) ([]*model.Clue, int64, error) {
	return nil, 0, nil
}
func (f *fakeClueRepo) ListByAccount(context.Context, string, int, int) ([]*model.Clue, int64, error) {
	return nil, 0, nil
}
func (f *fakeClueRepo) Delete(context.Context, string) error { return nil }
func (f *fakeClueRepo) GetClueStatistics(context.Context) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeClueRepo) GetClueStatisticsWithRange(context.Context, int64, int64) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeClueRepo) GetLeadMiningStats(context.Context) (map[string]any, error) {
	return map[string]any{"total": 0, "by_lead_role": []map[string]any{}, "by_status": []map[string]any{}, "by_channel": []map[string]any{}, "by_conversation": []map[string]any{}}, nil
}
func (f *fakeClueRepo) GetLeadMiningStatsWithRange(context.Context, int64, int64) (map[string]any, error) {
	return map[string]any{"total": 0, "by_lead_role": []map[string]any{}, "by_status": []map[string]any{}, "by_channel": []map[string]any{}, "by_conversation": []map[string]any{}}, nil
}
func (f *fakeClueRepo) GetClueAllList(context.Context, int64) ([]*model.Clue, int64, error) {
	return nil, 0, nil
}
func (f *fakeClueRepo) ListByAccounts(context.Context, []string) ([]*model.Clue, error) {
	return nil, nil
}
func (f *fakeClueRepo) BatchUpdateInTx(context.Context, []string, map[string]any) (int, error) {
	return 0, nil
}
func (f *fakeClueRepo) BatchCreateWithDedup(context.Context, []*model.Clue) (int64, int64, error) {
	return 0, 0, nil
}
func (f *fakeClueRepo) GetWhatsappClues(context.Context) ([]*model.Clue, int64, error) {
	return nil, 0, nil
}
func (f *fakeClueRepo) ListWithQuery(context.Context, repository.ClueQuery, int, int) ([]*model.Clue, int64, error) {
	return nil, 0, nil
}
func (f *fakeClueRepo) CountWithQuery(context.Context, repository.ClueQuery) (int64, error) {
	return 0, nil
}

type fakeCfgRepo struct {
	cfg *model.LeadMiningConfig
}

func (f *fakeCfgRepo) GetSingleton(context.Context) (*model.LeadMiningConfig, error) {
	if f.cfg == nil {
		return &model.LeadMiningConfig{ID: 1, MinIntentScore: 50}, nil
	}
	return f.cfg, nil
}
func (f *fakeCfgRepo) Save(context.Context, *model.LeadMiningConfig) error { return nil }

type fakeJudge struct {
	calls int
	resp  *LeadJudgement
}

func (f *fakeJudge) Judge(context.Context, *model.LeadMiningConfig, []llm.ChatMessage) (*LeadJudgement, error) {
	f.calls++
	return f.resp, nil
}

func lmItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var (
	lmErrDuplicate = &lmRepoError{"重复数据"}
	lmErrNotFound  = &lmRepoError{"not found"}
)

type lmRepoError struct{ msg string }

func (e *lmRepoError) Error() string { return e.msg }

func cannedHistory() func(context.Context, *model.MessageHub) []llm.ChatMessage {
	return func(_ context.Context, _ *model.MessageHub) []llm.ChatMessage {
		return []llm.ChatMessage{
			{Role: "user", Content: "你好，在吗？"},
			{Role: "user", Content: "我想了解一下你们的产品和报价"},
		}
	}
}

func newTestService(t *testing.T, cfg *model.LeadMiningConfig, j *fakeJudge) *Service {
	t.Helper()
	setupLeadHistoryTest(t)
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })
	return &Service{
		channelResolver: NewChannelResolverWithRepos(nil, nil),
		judge:           j,
		historyFetcher:  cannedHistory(),
		custRepo:        newFakeCustRepo(),
		clueRepo:        newFakeClueRepo(),
		cfgRepo:         &fakeCfgRepo{cfg: cfg},
		lastJudge:       map[string]time.Time{},
	}
}

func sampleHub(platform, senderID, content string) *model.MessageHub {
	return &model.MessageHub{
		ID:             1,
		Platform:       platform,
		AccountID:      "test-account",
		SenderID:       senderID,
		SenderName:     "张三",
		Direction:      "inbound",
		Content:        content,
		ConversationID: "conv-" + senderID,
	}
}

func TestMergeTags(t *testing.T) {
	got := mergeTags([]string{"A", "B"}, []string{"b", "C", ""})
	want := []string{"A", "B", "C"}
	if len(got) != len(want) {
		t.Fatalf("mergeTags=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mergeTags[%d]=%s want %s", i, got[i], want[i])
		}
	}
}

func TestChannelEnabled(t *testing.T) {
	// P0-c：默认全纳入（Channels 空 = 不表达关闭），显式名单 = 关闭名单。
	cfg := &model.LeadMiningConfig{}
	for _, platform := range []string{"douyin", "telegram", "wechat", "qq"} {
		if !channelEnabled(cfg, platform) {
			t.Fatalf("空渠道配置应放行全部渠道，实际拦截 %s", platform)
		}
	}
	cfg2 := &model.LeadMiningConfig{Channels: []string{"telegram", "douyin"}}
	if channelEnabled(cfg2, "telegram") {
		t.Fatal("telegram 在显式关闭名单内，应拦截")
	}
	if channelEnabled(cfg2, "douyin") {
		t.Fatal("douyin 在显式关闭名单内，应拦截")
	}
	if !channelEnabled(cfg2, "wechat") {
		t.Fatal("wechat 不在关闭名单内，应默认放行")
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	cfg := &model.LeadMiningConfig{
		Keywords:    []string{"购买", "代理"},
		Tags:        []string{"高意向"},
		Requirement: "客户明确表达购买意向",
	}
	p := buildSystemPrompt(cfg)
	for _, need := range []string{"购买", "代理", "高意向", "客户明确表达购买意向"} {
		if !strings.Contains(p, need) {
			t.Fatalf("系统提示词缺少 %q", need)
		}
	}
}

func TestProcess_LeadDetected(t *testing.T) {
	cfg := &model.LeadMiningConfig{
		Enabled:        true,
		Keywords:       []string{"购买"},
		Tags:           []string{"高意向"},
		MinIntentScore: 50,
	}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80, MatchedTags: []string{"AI兴趣"}}}
	s := newTestService(t, cfg, j)
	fcr := s.custRepo.(*fakeCustRepo)
	fkr := s.clueRepo.(*fakeClueRepo)

	s.process(context.Background(), sampleHub("telegram", "u1", "我想购买"))

	if j.calls != 1 {
		t.Fatalf("期望判定 1 次，实际 %d", j.calls)
	}
	// 身份归一口径已演进：telegram 非数字 sender 走规范化 UnifiedID；
	// LM-04 起 SourceAccountID 写真实接入账号（hub.AccountID），不是联系人 sender。
	var c *model.Customer
	for _, candidate := range fcr.byUID {
		if candidate.SourceChannel == "telegram" && candidate.SourceAccountID == "test-account" {
			c = candidate
			break
		}
	}
	if c == nil {
		t.Fatal("未创建/解析客户")
	}
	var tags []string
	_ = json.Unmarshal([]byte(c.Tags), &tags)
	if !lmContains(tags, "高意向") || !lmContains(tags, "AI兴趣") {
		t.Fatalf("客户标签不正确: %v", tags)
	}
	clue := fkr.byTypeAcct[lmItoa(int(ClueTypeLeadMining))+":telegram:u1"]
	if clue == nil {
		t.Fatal("未写入线索库存")
	}
	if clue.Type != ClueTypeLeadMining {
		t.Fatalf("通用 LLM 发掘路径线索类型应为 ClueTypeLeadMining(8), 实际 %d", clue.Type)
	}
	if clue.IntentScore != 80 || clue.SourceID != "lead_mining" {
		t.Fatalf("线索字段异常: %+v", clue)
	}
	if fkr.createCnt != 1 {
		t.Fatalf("期望 Create 1 次，实际 %d", fkr.createCnt)
	}
}

func TestProcess_BelowThreshold(t *testing.T) {
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 90}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 50}}
	s := newTestService(t, cfg, j)
	fkr := s.clueRepo.(*fakeClueRepo)

	s.process(context.Background(), sampleHub("telegram", "u2", "随便聊聊"))

	if fkr.createCnt != 0 {
		t.Fatalf("低于阈值不应写线索，实际 Create %d", fkr.createCnt)
	}
}

func TestProcess_NotLead(t *testing.T) {
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: false, IntentScore: 10}}
	s := newTestService(t, cfg, j)
	fkr := s.clueRepo.(*fakeClueRepo)

	s.process(context.Background(), sampleHub("telegram", "u3", "你好"))

	if fkr.createCnt != 0 {
		t.Fatalf("非线索不应写线索，实际 Create %d", fkr.createCnt)
	}
}

func TestProcess_ChannelDisabled(t *testing.T) {
	// P0-c：Channels 是显式关闭名单。douyin 被显式关闭 ⇒ 不触发判定。
	cfg := &model.LeadMiningConfig{Enabled: true, Channels: []string{"douyin"}, MinIntentScore: 50}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 90}}
	s := newTestService(t, cfg, j)

	s.process(context.Background(), sampleHub("douyin", "u4", "我想购买"))

	if j.calls != 0 {
		t.Fatalf("显式关闭渠道不应触发判定，实际判定 %d 次", j.calls)
	}
}

func TestProcess_Disabled(t *testing.T) {
	cfg := &model.LeadMiningConfig{Enabled: false, MinIntentScore: 50}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 90}}
	s := newTestService(t, cfg, j)

	s.process(context.Background(), sampleHub("telegram", "u5", "我想购买"))

	if j.calls != 0 {
		t.Fatalf("未启用不应触发判定，实际 %d", j.calls)
	}
}

func TestProcess_Debounce(t *testing.T) {
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80}}
	s := newTestService(t, cfg, j)
	fkr := s.clueRepo.(*fakeClueRepo)

	s.process(context.Background(), sampleHub("telegram", "u6", "在吗"))
	s.process(context.Background(), sampleHub("telegram", "u6", "我想购买"))

	if j.calls != 1 {
		t.Fatalf("去抖后应只判定 1 次，实际 %d", j.calls)
	}
	if fkr.createCnt != 1 {
		t.Fatalf("去抖后应只写 1 条线索，实际 %d", fkr.createCnt)
	}
}

func TestProcess_DedupAcrossTime(t *testing.T) {
	cfg := &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}
	j := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80}}
	s := newTestService(t, cfg, j)
	fkr := s.clueRepo.(*fakeClueRepo)

	s.process(context.Background(), sampleHub("telegram", "u7", "第一次咨询"))
	s.lastJudge = map[string]time.Time{}
	s.process(context.Background(), sampleHub("telegram", "u7", "第二次咨询"))

	if j.calls != 2 {
		t.Fatalf("跨窗口应判定 2 次，实际 %d", j.calls)
	}
	if fkr.createCnt != 1 {
		t.Fatalf("同账号线索应只 Create 1 次（其余 Update），实际 %d", fkr.createCnt)
	}
	if fkr.updateCnt != 1 {
		t.Fatalf("第二次应走 UpdateByID，实际 %d", fkr.updateCnt)
	}
}

func lmContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
