package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// ---------- CH-05 状态闸门：租户 KV 启停必须被 RequireRunnable 消费 ----------

func TestChannelStatus_RequireRunnableConsumesTenantKV(t *testing.T) {
	ctx := context.Background()
	ch := &model.Channel{ID: 1, TenantID: 1, Code: "telegram", Enabled: true}
	acc := &model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 1, Status: 1, IdentifyCode: "cid_a"}
	reader := &memStatusReader{ch: ch, acc: acc}

	on := NewChannelStatusServiceWithDeps(reader, &stubPresetService{enabled: true})
	if _, err := on.RequireRunnable(ctx, 1, 1, 10); err != nil {
		t.Fatalf("租户 KV 启用应放行，err=%v", err)
	}

	off := NewChannelStatusServiceWithDeps(reader, &stubPresetService{enabled: false})
	if _, err := off.RequireRunnable(ctx, 1, 1, 10); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("租户 KV 关闭必须拒绝，err=%v", err)
	}

	broken := NewChannelStatusServiceWithDeps(reader, &stubPresetService{err: errors.New("kv down")})
	if _, err := broken.RequireRunnable(ctx, 1, 1, 10); err == nil {
		t.Fatalf("租户 KV 查询故障必须 fail-closed 上抛，不能重新放行")
	}
}

// ---------- CH-05/CH-10 默认行状态闸门：不依赖 system_config_kv ----------

func TestChannelRowStatusGate_NoKVTableDependency(t *testing.T) {
	ctx := context.Background()
	reader := &memStatusReader{
		ch:  &model.Channel{ID: 1, TenantID: 1, Code: "douyin", Enabled: true},
		acc: &model.ChannelAccount{ID: 1, TenantID: 1, ChannelID: 1, Status: 1},
	}
	gate := &channelRowStatusGate{reader: reader}
	got, err := gate.RequireRunnable(ctx, 1, 1, 1)
	if err != nil || got == nil || got.AccountID != 1 {
		t.Fatalf("启用行应放行且不读 KV，got=%+v err=%v", got, err)
	}

	reader.ch.Enabled = false
	if _, err := gate.RequireRunnable(ctx, 1, 1, 1); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("渠道行停用必须拒绝，err=%v", err)
	}
	reader.ch.Enabled = true
	reader.acc.Status = 0
	if _, err := gate.RequireRunnable(ctx, 1, 1, 1); !errors.Is(err, ErrChannelAccountDisabled) {
		t.Fatalf("账号停用必须拒绝，err=%v", err)
	}
}

// ---------- CH-05/CH-10 运行入口：调度闸门在选号处生效 ----------

type stubRunnableGate struct {
	reject map[uint]error
	errAll error
	calls  []uint
}

func (g *stubRunnableGate) RequireRunnable(_ context.Context, tenantID, channelID, accountID uint) (*ResolvedChannelAccount, error) {
	g.calls = append(g.calls, accountID)
	if g.errAll != nil {
		return nil, g.errAll
	}
	if e, ok := g.reject[accountID]; ok {
		return nil, e
	}
	return &ResolvedChannelAccount{TenantID: tenantID, ChannelID: channelID, AccountID: accountID}, nil
}

func TestChannelDispatch_RuntimeGateEnforcedAtPick(t *testing.T) {
	ctx := context.Background()
	accRepo := &pagingAccountRepo{rows: []model.ChannelAccount{
		{TenantID: 1, ID: 1, ChannelID: 1, Name: "a", Status: 1},
		{TenantID: 1, ID: 2, ChannelID: 1, Name: "b", Status: 1},
	}}
	gate := &stubRunnableGate{reject: map[uint]error{1: ErrChannelAccountDisabled}}
	svc := NewChannelDispatchServiceWithGate(nil, &dispatchConversationRepoStub{}, accRepo, gate)

	// 轮转：被停用的账号 1 必须被跳过，连续多轮只落到账号 2。
	for i := 0; i < 6; i++ {
		got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram"})
		if err != nil || got.AccountID != 2 {
			t.Fatalf("停用账号不得参与均衡，第 %d 轮 got=%+v err=%v", i, got, err)
		}
	}

	// 渠道级关闭：全部候选被拒，显式失败而不是静默选出账号。
	gate.errAll = ErrChannelDisabled
	if got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram"}); err == nil {
		t.Fatalf("渠道停用必须拒绝选号，got=%+v", got)
	}

	// 显式会话粘性 / preferred：闸门拒绝必须显式失败，不悄悄换号。
	gate.errAll = nil
	sticky := NewChannelDispatchServiceWithGate(nil, &dispatchConversationRepoStub{
		conv: &model.InboxConversation{TenantID: 1, Platform: "telegram", AccountID: "1", ConversationID: "conv-1"},
	}, accRepo, gate)
	if got, err := sticky.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram", ConversationID: "conv-1"}); err == nil {
		t.Fatalf("粘停用的账号必须拒绝、不得换号，got=%+v", got)
	}
	if got, err := sticky.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram", PreferredAccountID: 1}); err == nil {
		t.Fatalf("preferred 停用的账号必须拒绝、不得换号，got=%+v", got)
	}
}

func TestChannelDispatch_ExplicitOutboundUsesGate(t *testing.T) {
	// 显式账号校验只对桥接出站在役渠道生效，测试临时把 telegram 纳入该集合。
	previous := BridgeOutboxChannels()
	SetBridgeOutboxChannels(append(append([]string{}, previous...), "telegram"))
	t.Cleanup(func() {
		if len(previous) > 0 {
			SetBridgeOutboxChannels(previous)
		}
	})

	ctx := context.Background()
	chRepo := newMemChannelRepo()
	_ = chRepo.Create(ctx, &model.Channel{ID: 1, TenantID: 1, Code: "telegram", Enabled: true})
	accRepo := &memAccountRepo{rows: []model.ChannelAccount{
		{ID: 9, TenantID: 1, ChannelID: 1, Name: "a", Status: 1},
	}, nextID: 10}
	gate := &stubRunnableGate{errAll: ErrChannelDisabled}
	svc := NewChannelDispatchServiceWithGate(chRepo, &dispatchConversationRepoStub{}, accRepo, gate)

	if _, err := svc.ResolveExplicitOutboundAccount(ctx, 1, "telegram", "9"); err == nil {
		t.Fatalf("显式账号也必须过启停闸门，渠道停用时应拒绝")
	}
}

// ---------- CH-08 自他识别：平台稳定 self ID 与群成员角色进入真实编排 ----------

func TestChannelInbound_SelfAndGroupRolesReachBusinessGate(t *testing.T) {
	ctx := context.Background()
	chRepo := newMemChannelRepo()
	_ = chRepo.Create(ctx, &model.Channel{ID: 1, TenantID: 1, Code: "telegram", Enabled: true})
	accRepo := &memAccountRepo{rows: []model.ChannelAccount{
		{ID: 10, TenantID: 1, ChannelID: 1, Name: "a", IdentifyCode: "cid_a", Status: 1},
	}, nextID: 11}
	resolver := NewChannelIdentityResolver(NewChannelResolverWithRepos(accRepo, chRepo))
	archive := &memInboundArchive{seen: map[string]bool{}}
	trigger := &memInboundTrigger{}
	o := NewChannelInboundOrchestrator(archive, resolver, trigger)

	// 单聊：发言人 == 平台 self 稳定 ID → self，允许触发业务。
	out, err := o.DeliverAndArchive(ctx, &model.InboundMessage{
		EventID: "e-self", Source: "webhook", Channel: "telegram", TenantID: 1,
		AccountRef: "10", SenderID: "stable-self", SelfPlatformID: "stable-self", Timestamp: 100,
	})
	if err != nil || out.IdentityRole != "self" || out.Automation != "triggered" || trigger.calls != 1 {
		t.Fatalf("稳定 self ID 应判自己并触发，out=%+v err=%v calls=%d", out, err, trigger.calls)
	}

	// 群聊：成员稳定 ID→customer 角色，由成员证据判定，不靠昵称猜。
	out, err = o.DeliverAndArchive(ctx, &model.InboundMessage{
		EventID: "e-customer", Source: "webhook", Channel: "telegram", TenantID: 1,
		AccountRef: "10", SenderID: "u1", SelfPlatformID: "stable-self", IsGroup: true,
		MemberRoles: []model.InboundMemberRole{{PlatformUserID: "u1", Role: "customer"}}, Timestamp: 110,
	})
	if err != nil || out.IdentityRole != "customer" || out.Automation != "triggered" || trigger.calls != 2 {
		t.Fatalf("群成员角色应进入识别并触发，out=%+v err=%v calls=%d", out, err, trigger.calls)
	}

	// 缺身份：保持 unknown 且业务闸跳过，不触发猜测性自动回复。
	out, err = o.DeliverAndArchive(ctx, &model.InboundMessage{
		EventID: "e-unknown", Source: "webhook", Channel: "telegram", TenantID: 1,
		AccountRef: "10", SenderID: "u-unknown", SelfPlatformID: "stable-self", IsGroup: true, Timestamp: 120,
	})
	if err != nil || out.Automation != "skipped_unknown" || trigger.calls != 2 {
		t.Fatalf("unknown 必须跳过业务，out=%+v err=%v calls=%d", out, err, trigger.calls)
	}
}

// ---------- CH-09 真实归档适配器：查重/乱序/落库全部经仓储 + 租户上下文 ----------

type memChannelArchiveStore struct {
	rows          map[string]*model.MessageHub
	last          *model.MessageHub
	err           error
	tenantsSeen   []uint
	createdTenant uint
}

func (s *memChannelArchiveStore) GetByMsgID(ctx context.Context, msgID string) (*model.MessageHub, error) {
	if tid, ok := tenant.FromContext(ctx); ok {
		s.tenantsSeen = append(s.tenantsSeen, tid)
	}
	if s.err != nil {
		return nil, s.err
	}
	if row := s.rows[msgID]; row != nil {
		return row, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *memChannelArchiveStore) GetLastInboundByConversation(ctx context.Context, _ string) (*model.MessageHub, error) {
	if tid, ok := tenant.FromContext(ctx); ok {
		s.tenantsSeen = append(s.tenantsSeen, tid)
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.last == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return s.last, nil
}

func (s *memChannelArchiveStore) Create(ctx context.Context, hub *model.MessageHub) error {
	if tid, ok := tenant.FromContext(ctx); ok {
		s.createdTenant = tid
	}
	if s.err != nil {
		return s.err
	}
	if s.rows == nil {
		s.rows = map[string]*model.MessageHub{}
	}
	s.rows[hub.MsgID] = hub
	return nil
}

func TestChannelInboundArchive_RealStoreSeam(t *testing.T) {
	ctx := context.Background()
	store := &memChannelArchiveStore{rows: map[string]*model.MessageHub{}}
	adapter := NewInboundArchiveWithStore(store)

	if ok, err := adapter.Exists(ctx, 42, "missing"); err != nil || ok {
		t.Fatalf("未命中应为 false/nil，ok=%v err=%v", ok, err)
	}
	hub := &model.MessageHub{TenantID: 42, MsgID: "e1", Platform: "telegram", Direction: "inbound", SentAt: time.UnixMilli(1234)}
	if err := adapter.Archive(ctx, hub); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	if store.createdTenant != 42 {
		t.Fatalf("归档必须带事件租户上下文，got=%d", store.createdTenant)
	}
	if ok, err := adapter.Exists(ctx, 42, "e1"); err != nil || !ok {
		t.Fatalf("已归档事件应命中，ok=%v err=%v", ok, err)
	}
	store.last = hub
	ts, err := adapter.LastTimestamp(ctx, 42, "conv-1")
	if err != nil || ts != 1234 {
		t.Fatalf("最后时间戳应取同源 SentAt，ts=%d err=%v", ts, err)
	}

	// 仓储故障原样上抛，绝不当作「未命中」放行。
	store.err = errors.New("db down")
	if _, err := adapter.Exists(ctx, 42, "e-x"); err == nil {
		t.Fatalf("查重故障必须上抛")
	}
	for _, seen := range store.tenantsSeen {
		if seen != 42 {
			t.Fatalf("所有仓储读必须带租户上下文，got=%d", seen)
		}
	}
}

// 保证 Archive 的 SentAt 与信封 UnixMilli 同源（CH-09 乱序判据一致）。
func TestChannelInbound_ArchiveSentAtFromEnvelopeMilli(t *testing.T) {
	ctx := context.Background()
	archive := &memInboundArchive{seen: map[string]bool{}}
	o := NewChannelInboundOrchestrator(archive, nil, nil)
	_, err := o.DeliverAndArchive(ctx, &model.InboundMessage{
		EventID: "e-ts", Source: "webhook", Channel: "telegram", TenantID: 1,
		ConversationID: "c1", SenderID: "u1", Timestamp: 1700000000000,
	})
	if err != nil || len(archive.rows) != 1 {
		t.Fatalf("归档应成功，err=%v rows=%d", err, len(archive.rows))
	}
	if got := archive.rows[0].SentAt.UnixMilli(); got != 1700000000000 {
		t.Fatalf("SentAt 应等于信封 UnixMilli，got=%d", got)
	}
}
