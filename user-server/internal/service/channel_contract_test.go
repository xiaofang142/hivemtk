package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// ---------- CH-05 状态契约：渠道+账号同时启用 ----------

type memStatusReader struct {
	ch  *model.Channel
	acc *model.ChannelAccount
	err error
}

func (m *memStatusReader) Available() bool { return true }
func (m *memStatusReader) ChannelByID(context.Context, uint, uint) (*model.Channel, error) {
	return m.ch, m.err
}
func (m *memStatusReader) AccountByID(context.Context, uint, uint) (*model.ChannelAccount, error) {
	return m.acc, m.err
}

type stubPresetService struct {
	ChannelService
	enabled bool
	err     error
}

func (s *stubPresetService) IsEnabledForTenant(context.Context, uint, string) (bool, error) {
	return s.enabled, s.err
}

type ambiguousIdentifyAccountRepo struct {
	repository.ChannelAccountRepository
	listCalls int
}

type legacyMissAccountRepo struct {
	repository.ChannelAccountRepository
}

type namedAccountRepo struct {
	repository.ChannelAccountRepository
	account model.ChannelAccount
	rows    []model.ChannelAccount
}

type resolverChannelRepoStub struct {
	repository.ChannelRepository
	available bool
	channel   *model.Channel
	err       error
}

func (r *legacyMissAccountRepo) Available() bool { return true }
func (r *legacyMissAccountRepo) GetByIdentifyCode(context.Context, string) (*model.ChannelAccount, error) {
	return nil, nil
}
func (r *legacyMissAccountRepo) GetByID(context.Context, uint, uint) (*model.ChannelAccount, error) {
	return nil, nil
}
func (r *legacyMissAccountRepo) List(context.Context, repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return nil, nil
}

func (r *namedAccountRepo) Available() bool { return true }
func (r *namedAccountRepo) GetByIdentifyCode(context.Context, string) (*model.ChannelAccount, error) {
	return nil, nil
}
func (r *namedAccountRepo) List(context.Context, repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	if r.rows != nil {
		return r.rows, nil
	}
	return []model.ChannelAccount{r.account}, nil
}

func (r *resolverChannelRepoStub) Available() bool { return r.available }
func (r *resolverChannelRepoStub) GetByID(context.Context, uint, uint) (*model.Channel, error) {
	return r.channel, r.err
}

func (r *ambiguousIdentifyAccountRepo) Available() bool { return true }
func (r *ambiguousIdentifyAccountRepo) GetByIdentifyCode(context.Context, string) (*model.ChannelAccount, error) {
	return nil, repository.ErrAmbiguousIdentifyCode
}
func (r *ambiguousIdentifyAccountRepo) List(context.Context, repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	r.listCalls++
	return []model.ChannelAccount{{ID: 10, TenantID: 1, Name: "cid_ambiguous"}}, nil
}

func TestChannelResolver_ResolveByAccountRefDoesNotFallbackAfterAmbiguousIdentifyCode(t *testing.T) {
	repo := &ambiguousIdentifyAccountRepo{}
	resolver := NewChannelResolverWithRepos(repo, nil)
	ctx := tenant.WithTenantID(context.Background(), 1)

	_, err := resolver.ResolveByAccountRef(ctx, "", "cid_ambiguous")
	if !errors.Is(err, ErrChannelIdentityAmbiguous) {
		t.Fatalf("识别码歧义必须原样拒绝，实际 err=%v", err)
	}
	if repo.listCalls != 0 {
		t.Fatalf("识别码歧义后不得继续账号名回退，List 调用次数=%d", repo.listCalls)
	}
}

func TestChannelResolver_ResolveByAccountRefClassifiesOnlyHealthyMissAsLegacy(t *testing.T) {
	resolver := NewChannelResolverWithRepos(&legacyMissAccountRepo{}, nil)
	ctx := tenant.WithTenantID(context.Background(), 1)

	_, err := resolver.ResolveByAccountRef(ctx, "", "legacy-external-name")
	if !errors.Is(err, ErrChannelIdentityLegacyUnmatched) {
		t.Fatalf("健康查询确认无匹配必须返回 legacy miss，实际 err=%v", err)
	}
}

func TestChannelResolver_ResolveByAccountRefIncompleteSearchDoesNotClaimLegacyMiss(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), 1)
	rows := make([]model.ChannelAccount, 50)
	for i := range rows {
		rows[i] = model.ChannelAccount{ID: uint(i + 1), TenantID: 1, Name: "other-account"}
	}
	resolver := NewChannelResolverWithRepos(&namedAccountRepo{rows: rows}, nil)
	if _, err := resolver.ResolveByAccountRef(ctx, "", "name"); !errors.Is(err, ErrChannelIdentityUnresolved) {
		t.Fatalf("满页查询不能证明无匹配账号，实际 err=%v", err)
	}
}

func TestChannelResolver_ResolveByAccountRefDoesNotTreatChannelMismatchAsLegacyMiss(t *testing.T) {
	accountRepo := &namedAccountRepo{account: model.ChannelAccount{
		ID: 10, TenantID: 1, ChannelID: 2, Name: "store-account", Status: 1,
	}}
	channelRepo := &resolverChannelRepoStub{
		available: true,
		channel:   &model.Channel{ID: 2, TenantID: 1, Code: "qq", Type: "chat"},
	}
	resolver := NewChannelResolverWithRepos(accountRepo, channelRepo)
	ctx := tenant.WithTenantID(context.Background(), 1)

	_, err := resolver.ResolveByAccountRef(ctx, "telegram", "store-account")
	if !errors.Is(err, ErrChannelIdentityUnresolved) {
		t.Fatalf("命中账号但渠道不符必须标记为身份不确定，不能作为legacy未命中放行，实际 err=%v", err)
	}
}

func TestChannelResolver_ResolveByAccountRefChannelStoreFailureIsNotLegacyMiss(t *testing.T) {
	accountRepo := &namedAccountRepo{account: model.ChannelAccount{
		ID: 10, TenantID: 1, ChannelID: 2, Name: "store-account", Status: 1,
	}}
	resolver := NewChannelResolverWithRepos(accountRepo, &resolverChannelRepoStub{available: false})
	ctx := tenant.WithTenantID(context.Background(), 1)

	_, err := resolver.ResolveByAccountRef(ctx, "telegram", "store-account")
	if !errors.Is(err, repository.ErrChannelStoreUnavailable) {
		t.Fatalf("渠道仓储不可用必须保留故障，不得判为legacy未命中，实际 err=%v", err)
	}
}

func TestChannelResolver_ResolveByAccountRefRejectsMissingTenantInMultiTenantMode(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })

	resolver := NewChannelResolverWithRepos(&legacyMissAccountRepo{}, nil)
	_, err := resolver.ResolveByAccountRef(context.Background(), "", "999")
	if !errors.Is(err, ErrChannelTenantUnresolved) {
		t.Fatalf("多租户模式缺少租户必须拒绝，实际 err=%v", err)
	}
}

func TestChannelStatus_RequireRunnableBothSides(t *testing.T) {
	ctx := context.Background()
	ch := &model.Channel{ID: 1, TenantID: 1, Code: "telegram", Enabled: true}
	acc := &model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 1, Status: 1, IdentifyCode: "cid_x"}
	reader := &memStatusReader{ch: ch, acc: acc}
	svc := NewChannelStatusServiceWithDeps(reader, &stubPresetService{enabled: true})

	got, err := svc.RequireRunnable(ctx, 1, 1, 10)
	if err != nil || got.AccountID != 10 {
		t.Fatalf("启用的渠道+账号应放行，got=%+v err=%v", got, err)
	}
	// 渠道停用 → 拒绝；账号停用 → 拒绝；跨渠道账号 → 拒绝；缺 ID → 拒绝
	ch.Enabled = false
	if _, err := svc.RequireRunnable(ctx, 1, 1, 10); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("渠道停用必须拒绝，err=%v", err)
	}
	ch.Enabled = true
	acc.Status = 0
	if _, err := svc.RequireRunnable(ctx, 1, 1, 10); !errors.Is(err, ErrChannelAccountDisabled) {
		t.Fatalf("账号停用必须拒绝，err=%v", err)
	}
	acc.Status = 1
	acc.ChannelID = 2
	if _, err := svc.RequireRunnable(ctx, 1, 1, 10); !errors.Is(err, ErrChannelAccountDisabled) {
		t.Fatalf("账号属于错误渠道必须拒绝，err=%v", err)
	}
	if _, err := svc.RequireRunnable(ctx, 0, 1, 10); err == nil {
		t.Fatalf("缺租户必须拒绝")
	}
	// 查询故障原样上抛，不静默放行
	reader.err = errors.New("db down")
	if _, err := svc.RequireRunnable(ctx, 1, 1, 10); err == nil {
		t.Fatalf("查询故障必须上抛")
	}
}

// ---------- CH-07 可信身份：ctx/渠道/账号冲突拒绝 ----------

func TestChannelResolver_ResolveTrustedConflictRejects(t *testing.T) {
	ctx := customEditionTestCtx()
	chRepo := newMemChannelRepo()
	_ = chRepo.Create(ctx, &model.Channel{ID: 1, TenantID: 1, Code: "telegram", Enabled: true})
	// 同渠道两账号：权威码解析命中后缺消歧信息时仓储会拒绝
	accA := model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 1, Name: "a", IdentifyCode: "cid_a", Status: 1}
	accB := model.ChannelAccount{ID: 11, TenantID: 1, ChannelID: 1, Name: "b", Status: 1}
	accRepo := &memAccountRepo{rows: []model.ChannelAccount{accA, accB}, nextID: 12}
	r := NewChannelResolverWithRepos(accRepo, chRepo)

	// 账号不存在/缺码 → 拒绝
	if _, err := r.ResolveTrusted(ctx, "", "telegram", ""); err == nil {
		t.Fatalf("空识别码必须拒绝")
	}
	if _, err := r.ResolveTrusted(ctx, "cid_a", "qq", ""); err == nil {
		t.Fatalf("识别码渠道与请求渠道冲突必须拒绝")
	}
	// 显式账号 ID 不一致 → 拒绝（不能换号）
	if _, err := r.ResolveTrusted(ctx, "cid_a", "telegram", "11"); err == nil {
		t.Fatalf("显式账号引用冲突必须拒绝")
	}
	// 渠道停用 → 拒绝
	chRepo.byID[1].Enabled = false
	if _, err := r.ResolveTrusted(ctx, "cid_a", "telegram", "10"); !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("渠道停用必须拒绝，err=%v", err)
	}
}

// ---------- CH-08 自他识别：稳定 ID + unknown 不猜测 ----------

func TestChannelIdentity_StableSelfAndUnknown(t *testing.T) {
	ctx := context.Background()
	acc := &model.ChannelAccount{ID: 10, TenantID: 1, ChannelID: 1, Type: "dm", IdentifyCode: "cid_a", SelfIdentifyName: "显示名"}
	accRepo := &memAccountRepo{rows: []model.ChannelAccount{*acc}, nextID: 11}
	r := &ChannelResolver{accountRepo: accRepo}
	ir := NewChannelIdentityResolver(r)

	// 稳定 self ID 命中 → self
	got, err := ir.IdentifySpeaker(ctx, 1, 10, "dm", "stable-self-1", "stable-self-1", nil)
	if err != nil || !got.Self || got.Role != "self" {
		t.Fatalf("稳定 self ID 应判自己，got=%+v err=%v", got, err)
	}
	// 同名/仅显示名 → unknown，不能靠识别码/账号 ID 猜 self
	got, err = ir.IdentifySpeaker(ctx, 1, 10, "dm", "cid_a", "display-name", nil)
	if err != nil || got.Self || got.Role != "unknown" || !IsUnknownIdentity(got) {
		t.Fatalf("识别码/显示名不得当 self，got=%+v err=%v", got, err)
	}
	// 群多成员：明确角色 → 正确；角色冲突 → 歧义拒绝
	members := []GroupMemberRole{{PlatformUserID: "u1", Role: "customer"}, {PlatformUserID: "u1", Role: "agent"}}
	if _, err := ir.IdentifySpeaker(ctx, 1, 10, "group", "u1", "self-id", members); err == nil {
		t.Fatalf("同一成员角色冲突必须拒绝")
	}
	got, err = ir.IdentifySpeaker(ctx, 1, 10, "group", "u2", "self-id", []GroupMemberRole{{PlatformUserID: "u2", Role: "customer"}})
	if err != nil || got.Self || got.Role != "customer" {
		t.Fatalf("群成员角色应生效，got=%+v err=%v", got, err)
	}
}

// ---------- CH-09 统一入站：重放/乱序/失败可查 ----------

type memInboundArchive struct {
	seen    map[string]bool
	last    int64
	rows    []*model.MessageHub
	failErr error
}

func (m *memInboundArchive) Available() bool { return true }
func (m *memInboundArchive) Exists(_ context.Context, _ uint, event string) (bool, error) {
	return m.seen[event], m.failErr
}
func (m *memInboundArchive) LastTimestamp(context.Context, uint, string) (int64, error) {
	return m.last, m.failErr
}
func (m *memInboundArchive) Archive(_ context.Context, hub *model.MessageHub) error {
	if m.failErr != nil {
		return m.failErr
	}
	if m.seen[hub.MsgID] {
		return ErrInboundDuplicate
	}
	m.seen[hub.MsgID] = true
	m.rows = append(m.rows, hub)
	return nil
}

type memInboundTrigger struct{ calls int }

func (m *memInboundTrigger) Trigger(context.Context, *model.MessageHub, *ChannelIdentity) (string, error) {
	m.calls++
	return "triggered", nil
}

func TestChannelInbound_ReplayOutOfOrderAndPersistFailure(t *testing.T) {
	ctx := context.Background()
	archive := &memInboundArchive{seen: map[string]bool{}}
	trigger := &memInboundTrigger{}
	o := NewChannelInboundOrchestrator(archive, nil, trigger)
	base := &model.InboundMessage{EventID: "e1", Source: "webhook", Channel: "telegram", TenantID: 1, ConversationID: "c1", SenderID: "u1", Content: "hi", Timestamp: 100}
	out, err := o.DeliverAndArchive(ctx, base)
	if err != nil || out.Status != "archived" || out.Automation != "skipped_unknown" || trigger.calls != 0 {
		t.Fatalf("无身份应归档但跳过业务，out=%+v err=%v calls=%d", out, err, trigger.calls)
	}
	// 重放：显式拒绝，不二次触发
	out, err = o.DeliverAndArchive(ctx, base)
	if !errors.Is(err, ErrInboundDuplicate) || out.Status != "duplicate" || trigger.calls != 0 {
		t.Fatalf("重放必须显式拒绝，out=%+v err=%v", out, err)
	}
	// 乱序：显式标记
	archive.last = 200
	out, err = o.DeliverAndArchive(ctx, &model.InboundMessage{EventID: "e2", Source: "webhook", TenantID: 1, ConversationID: "c1", Timestamp: 150})
	if !errors.Is(err, ErrInboundOutOfOrder) || out.Status != "out_of_order" {
		t.Fatalf("乱序必须显式标记，out=%+v err=%v", out, err)
	}
	// 持久化失败：不报业务成功
	archive.failErr = errors.New("db down")
	out, err = o.DeliverAndArchive(ctx, &model.InboundMessage{EventID: "e3", Source: "webhook", TenantID: 1})
	if !errors.Is(err, ErrInboundPersistFailed) || out.Status != "persist_failed" {
		t.Fatalf("持久化失败必须显式返回，out=%+v err=%v", out, err)
	}
}

// ---------- CH-10 调度：粘性跨渠道拒绝 + 分页不漏候选 ----------

type pagingAccountRepo struct {
	repository.ChannelAccountRepository
	rows       []model.ChannelAccount
	listCalled int
}

func (p *pagingAccountRepo) Available() bool { return true }
func (p *pagingAccountRepo) GetByID(_ context.Context, tid, id uint) (*model.ChannelAccount, error) {
	for i := range p.rows {
		if p.rows[i].TenantID == tid && p.rows[i].ID == id {
			c := p.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}
func (p *pagingAccountRepo) List(_ context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	p.listCalled++
	var out []model.ChannelAccount
	for _, r := range p.rows {
		if r.TenantID != opts.TenantID || r.ChannelID != opts.ChannelID || r.Status != 1 {
			continue
		}
		out = append(out, r)
	}
	start, end := opts.Offset, opts.Offset+opts.Limit
	if start > len(out) {
		start = len(out)
	}
	if end > len(out) || opts.Limit <= 0 {
		end = len(out)
	}
	return out[start:end], nil
}

func TestChannelDispatch_StickyChannelAndPagination(t *testing.T) {
	ctx := context.Background()
	rows := []model.ChannelAccount{
		{TenantID: 1, ID: 1, ChannelID: 1, Name: "a", Status: 1},
		{TenantID: 1, ID: 2, ChannelID: 2, Name: "b", Status: 1},
	}
	accRepo := &pagingAccountRepo{rows: rows}
	conv := &dispatchConversationRepoStub{conv: &model.InboxConversation{TenantID: 1, Platform: "telegram", AccountID: "2", ConversationID: "conv-1"}}
	svc := NewChannelDispatchServiceWithRepos(nil, conv, accRepo)

	// 粘性记录属于渠道 2，但请求渠道 1：必须拒绝，不能串发/换号
	got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram", ConversationID: "conv-1"})
	if err == nil {
		t.Fatalf("跨渠道粘性记录必须拒绝，got=%+v", got)
	}
	// 600 个候选：分页循环必须全部拿到，轮转覆盖 >500 的账号
	rows = rows[:1]
	for i := 3; i <= 602; i++ {
		rows = append(rows, model.ChannelAccount{TenantID: 1, ID: uint(i), ChannelID: 1, Name: "acc" + strconv.Itoa(i), Status: 1})
	}
	accRepo = &pagingAccountRepo{rows: rows}
	svc = NewChannelDispatchServiceWithRepos(nil, &dispatchConversationRepoStub{}, accRepo)
	picked := map[uint]bool{}
	for i := 0; i < 20; i++ {
		got, err = svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram"})
		if err != nil {
			t.Fatalf("选号失败: %v", err)
		}
		picked[got.AccountID] = true
	}
	if accRepo.listCalled < 2 {
		t.Fatalf("超过 500 候选应触发分页，实际调用次数=%d", accRepo.listCalled)
	}
	// 最末账号（ID=602）必须有机会被选中（分页不漏）
	var sawLast bool
	for i := 0; i < 700; i++ {
		got, err = svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "telegram"})
		if err != nil {
			t.Fatalf("选号失败: %v", err)
		}
		if got.AccountID == 602 {
			sawLast = true
			break
		}
	}
	if !sawLast || len(picked) < 2 {
		t.Fatalf("分页后应覆盖全部候选，sawLast=%v picked=%d", sawLast, len(picked))
	}
}
