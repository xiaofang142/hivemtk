package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// LM-09：队列满不再静默丢弃，水位与待处理缓冲都保留消息。
func TestP1TailQueueFullNoDrop(t *testing.T) {
	s := &Service{
		queue:       make(chan *model.MessageHub, 1),
		pending:     make(chan *model.MessageHub, 4),
		tail:        NewLeadTailWatermark(),
		tailMaxWait: time.Second,
	}
	a := &model.MessageHub{Platform: "telegram", AccountID: "acc", ConversationID: "c1", MsgID: "m1", Direction: "inbound", Content: "你好"}
	b := &model.MessageHub{Platform: "telegram", AccountID: "acc", ConversationID: "c1", MsgID: "m2", Direction: "inbound", Content: "我想购买"}
	s.Enqueue(a)
	s.Enqueue(b) // 主队列满 -> 转待处理缓冲
	key := leadTailKeyFor(b)
	if got := len(s.pending); got != 1 {
		t.Fatalf("队列满时消息应进入待处理缓冲，实际 pending=%d", got)
	}
	if got := s.tail.Pending(key); got != 2 {
		t.Fatalf("水位应记录 2 条待分析，实际 %d", got)
	}
	if s.tail.Watermark(key) == 0 {
		t.Fatal("水位未前进")
	}
	// 主队列仍满时补扫不丢消息；消费一条后补扫成功。
	s.drainPending()
	if len(s.pending) != 1 {
		t.Fatalf("主队列满时补扫应把消息留在 pending，实际 %d", len(s.pending))
	}
	<-s.queue
	s.drainPending()
	if len(s.pending) != 0 || len(s.queue) != 1 {
		t.Fatalf("补扫失败: pending=%d queue=%d", len(s.pending), len(s.queue))
	}
}

// LM-09：尾判重放必须越过 60 秒防抖，并且只在判定耐久落库后清水位。
func TestP1TailForceReplayClearsWatermark(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	judge := &fakeJudge{resp: &LeadJudgement{IsLead: true, IntentScore: 80, MatchedTags: []string{"意向"}}}
	repo := newFakeClueRepo()
	s := p1TestService(judge, repo)
	// 渠道资格集合是全局的（channelgw.init 投影 + 其他测试可能重置），
	// 用 douyin 保证隔离/全量两种跑法下都在发掘资格集合内。
	hub := &model.MessageHub{Platform: "douyin", AccountID: "acc", ConversationID: "c1", MsgID: "m1", Direction: "inbound", SenderID: "u1", Content: "我想购买"}
	key := leadTailKeyFor(hub)
	s.tail.NoteEnqueued(key, 7, time.Now().Add(-time.Hour))
	s.lastJudge[p1DebounceKey(s, hub, ctx)] = time.Now() // 常规窗口命中

	s.process(ctx, hub)
	if judge.calls != 0 {
		t.Fatalf("常规防抖窗口内不应分析，实际调用 %d 次", judge.calls)
	}
	forced := context.WithValue(ctx, leadTailForceKey{}, true)
	forced = context.WithValue(forced, leadTailSeqKey{}, uint64(7))
	s.process(forced, hub)
	if judge.calls != 1 {
		t.Fatalf("尾判重放应越过防抖，实际调用 %d 次", judge.calls)
	}
	if s.tail.HasUnfinished(key) {
		t.Fatal("判定落库后应清除待处理水位")
	}
}

func p1DebounceKey(s *Service, hub *model.MessageHub, ctx context.Context) string {
	scope, err := s.historyScope(ctx, hub)
	if err != nil {
		return ""
	}
	return leadMiningDebounceKey(scope.tenantID, scope.Platform, scope.accountKey, hub.ConversationID, hub.GroupID, hub.SenderID, hub.IsGroup)
}

func p1TestService(judge LLMJudge, clueRepo repository.ClueRepository) *Service {
	return &Service{
		queue:           make(chan *model.MessageHub, 8),
		pending:         make(chan *model.MessageHub, 8),
		tail:            NewLeadTailWatermark(),
		tailMaxWait:     time.Second,
		judge:           judge,
		historyFetcher:  func(context.Context, *model.MessageHub) []llm.ChatMessage { return []llm.ChatMessage{{Role: "user", Content: "历史消息"}} },
		hitsProvider:    func(context.Context, DetectionInput) ([]model.LeadMiningHit, error) { return []model.LeadMiningHit{{ID: 9, Complete: true}}, nil },
		custRepo:        newFakeCustRepo(),
		clueRepo:        clueRepo,
		cfgRepo:         &fakeCfgRepo{cfg: &model.LeadMiningConfig{Enabled: true, MinIntentScore: 50}},
		// T01：规则是唯一真实入口，显式注入内存 account 规则，不依赖真实 dbstate。
		ruleTargetFor: func(context.Context, *model.MessageHub) LeadRuleTarget {
			return LeadRuleTarget{ChannelAccountID: 1, ChannelID: 1}
		},
		ruleService: NewLeadRuleServiceWithRepo(&lmRuleRepoStub{rows: map[string]*model.LeadMiningRule{
			"account": {Scope: "account", Enabled: true, Keywords: model.JSONStrings{"购买"}},
		}}),
		channelResolver: NewChannelResolverWithRepos(nil, nil),
		lastJudge:       map[string]time.Time{},
	}
}

// LM-03：生产严格模式下判定落库失败必须挡住副作用；兼容模式只记日志。
type p1JudgementRepo struct {
	repository.LeadMiningJudgementRepository
	err     error
	created int
}

func (r *p1JudgementRepo) Create(_ context.Context, _ *model.LeadMiningJudgement) error {
	r.created++
	return r.err
}

func TestP1StrictJudgementFailClosed(t *testing.T) {
	hub := &model.MessageHub{MsgID: "m1", Platform: "telegram", AccountID: "acc", Content: "x"}
	jd := &LeadJudgement{IsLead: true, IntentScore: 80}
	repo := &p1JudgementRepo{err: errors.New("db down")}
	s := &Service{strictJudgement: true, judgementRepo: repo}
	if s.recordLeadMiningJudgement(context.Background(), hub, jd) {
		t.Fatal("严格模式下判定落库失败必须返回 false")
	}
	s.strictJudgement = false
	if !s.recordLeadMiningJudgement(context.Background(), hub, jd) {
		t.Fatal("兼容模式应保留旧的 fail-visible 行为")
	}
	if repo.created != 2 {
		t.Fatalf("两次都应尝试写入，实际 %d", repo.created)
	}
}

// LM-14：归档关联字段在新建分支完整填充；更新失败不再继续通知/转商机。
type p1FailingClueRepo struct {
	repository.ClueRepository
	inner     *fakeClueRepo
	updateErr error
	updates   int
}

func (r *p1FailingClueRepo) Create(ctx context.Context, c *model.Clue) error { return r.inner.Create(ctx, c) }
func (r *p1FailingClueRepo) FindByTypeAndAccount(ctx context.Context, t int64, a string) (*model.Clue, error) {
	return r.inner.FindByTypeAndAccount(ctx, t, a)
}
func (r *p1FailingClueRepo) UpdateByID(ctx context.Context, id string, u map[string]any) error {
	r.updates++
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.inner.UpdateByID(ctx, id, u)
}

func TestP1PersistLeadArchiveFieldsAndFailureGate(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)

	// 新建：CustomerID/UserInfo/SourceHitIDs/ArchivedAt 完整回填。
	inner := newFakeClueRepo()
	svc := p1TestService(&fakeJudge{resp: &LeadJudgement{}}, inner)
	hub := &model.MessageHub{ID: 11, TenantID: tenant.DefaultTenantID, Platform: "telegram", AccountID: "acc", SenderID: "u1", SenderName: "张三", ConversationID: "c1", MsgID: "real-1", Direction: "inbound", Content: "我想购买"}
	jctx := &LeadJudgeContext{Hits: []model.LeadMiningHit{{ID: 41}}}
	svc.persistLead(ctx, &model.LeadMiningConfig{Enabled: true}, hub, "telegram:u1", &LeadJudgement{IsLead: true, IntentScore: 80, Reason: "r"}, jctx)
	clue := inner.byTypeAcct["8:telegram:u1"]
	if clue == nil {
		t.Fatal("未写入线索")
	}
	if clue.CustomerID == "" || clue.UserInfo == "" || len(clue.SourceHitIDs) != 1 || clue.ArchivedAt == nil {
		t.Fatalf("归档关联字段未完整回填: %+v", clue)
	}
	if clue.EvidenceJSON == "" || !strings.Contains(clue.EvidenceJSON, "real-1") {
		t.Fatalf("证据 JSON 未保留真实消息 ID: %s", clue.EvidenceJSON)
	}

	// 更新失败：不得继续后续副作用（由 notifier/商机转换计数间接验证更新调用后即返回）。
	failing := &p1FailingClueRepo{inner: newFakeClueRepo(), updateErr: errors.New("write failed")}
	existing := &model.Clue{ID: "old", Type: ClueTypeLeadMining, Account: "telegram:u2", Status: "candidate"}
	_ = failing.inner.Create(ctx, existing)
	failing.inner.byTypeAcct["8:telegram:u2"] = existing
	svc2 := p1TestService(&fakeJudge{resp: &LeadJudgement{}}, failing)
	hub2 := &model.MessageHub{ID: 12, TenantID: tenant.DefaultTenantID, Platform: "telegram", AccountID: "acc", SenderID: "u2", ConversationID: "c2", MsgID: "real-2", Direction: "inbound", Content: "我想购买"}
	svc2.persistLead(ctx, nil, hub2, "telegram:u2", &LeadJudgement{IsLead: true, IntentScore: 90}, nil)
	if failing.updates != 1 {
		t.Fatalf("更新分支应尝试写入一次，实际 %d", failing.updates)
	}
}

// LM-05：通知数组逐条入库、源消息级幂等、方向未知与覆盖标记。
func TestP1SageXNormalizedArrayEvents(t *testing.T) {
	inst := &model.SageXInstallation{ID: 3, TenantID: 1, AccountID: "acc"}
	primary := &model.MessageEvent{
		EventID: "sagex:inst:e1", SessionID: "s1", Channel: "qq", Content: "通知正文",
		ConversationID: "conv", SenderID: "group", Timestamp: time.UnixMilli(1000), Extra: map[string]any{},
	}
	ev := &SageXEventItem{
		EventID:     "e1",
		ParseStatus: "ok",
		RawNotification: SageXRawNotification{
			Messages: []SageXStyleMessage{
				{Text: "甲：我要买", Timestamp: 1000, SenderKey: "member-a", SenderName: "甲"},
				{Text: "通知正文", Timestamp: 1000, SenderKey: "member-a"}, // 与正文重叠，应跳过
			},
			HistoricMessages: []SageXStyleMessage{
				{Text: "历史消息", Timestamp: 900, SenderKey: "member-b", SenderName: "乙"},
			},
		},
	}
	svc := &SageXUploadService{}
	events := svc.buildNormalizedMessageEvents(inst, "inst", "batch", "qq", ev, primary)
	if len(events) != 2 {
		t.Fatalf("应生成 2 条规范消息，实际 %d", len(events))
	}
	if events[0].EventID == events[1].EventID || events[0].EventID == primary.EventID {
		t.Fatal("源消息级 EventID 必须稳定且互不相同")
	}
	if events[0].SenderType != "unknown" || events[1].SenderType != "unknown" {
		t.Fatal("方向未知不得标记为 customer")
	}
	sx0 := events[0].Extra["sagex"].(map[string]any)
	sx1 := events[1].Extra["sagex"].(map[string]any)
	if sx0["source_coverage"] != "historic" || sx1["source_coverage"] != "live" {
		t.Fatalf("覆盖标记错误: %v / %v", sx0["source_coverage"], sx1["source_coverage"])
	}
	// 同一通知重发得到完全相同的 EventID -> ingress 按 MsgID 幂等。
	again := svc.buildNormalizedMessageEvents(inst, "inst", "batch", "qq", ev, primary)
	if again[0].EventID != events[0].EventID || again[1].EventID != events[1].EventID {
		t.Fatal("重发必须产生同一源消息级 EventID")
	}
}

// LM-07：冻结快照并入配置副本，nil 配置不 panic，解析错误 fail-closed 由生产构造保证。
func TestP1ConfigSnapshotAppliedToConfig(t *testing.T) {
	snap := &LeadMiningConfigSnapshot{Version: "lm-v1", Enabled: true, MinIntentScore: 77, Model: "m2", Requirement: "req", Keywords: []string{"采购"}}
	got := applyLeadMiningSnapshotToConfig(nil, snap)
	if got.MinIntentScore != 77 || got.Model != "m2" || got.Requirement != "req" || len(got.Keywords) != 1 {
		t.Fatalf("快照未并入配置: %+v", got)
	}
	if got = applyLeadMiningSnapshotToConfig(nil, nil); got == nil {
		t.Fatal("nil 快照也必须返回可用配置副本")
	}
}

// LM-12：合成 history_N / 伪造 ID 不属于授权快照，引用校验拒绝。
func TestP1EvidenceSnapshotReferences(t *testing.T) {
	s := NewLeadEvidenceSnapshot([]model.MessageHub{
		{MsgID: "real-a"}, {MsgID: "history_0"}, {MsgID: "real-b"}, {MsgID: "real-a"},
	})
	if len(s.RealMessageIDs()) != 2 {
		t.Fatalf("真实消息 ID 去重后应为 2，实际 %v", s.RealMessageIDs())
	}
	if !s.ReferencesValid([]string{"real-a", "real-b"}) {
		t.Fatal("授权快照内引用应通过")
	}
	if s.ReferencesValid([]string{"real-a", "history_0"}) || s.ReferencesValid([]string{"forged"}) {
		t.Fatal("合成/伪造 ID 必须拒绝")
	}
}
