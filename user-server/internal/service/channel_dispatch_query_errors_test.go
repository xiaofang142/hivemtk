package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type dispatchChannelRepoStub struct {
	repository.ChannelRepository
	channel *model.Channel
	err     error
}

func (s *dispatchChannelRepoStub) GetByTenantAndCode(context.Context, uint, string) (*model.Channel, error) {
	return s.channel, s.err
}

type dispatchAccountRepoStub struct {
	repository.ChannelAccountRepository
	account *model.ChannelAccount
	err     error
	list    []model.ChannelAccount
	listErr error
}

func (s *dispatchAccountRepoStub) GetByID(context.Context, uint, uint) (*model.ChannelAccount, error) {
	return s.account, s.err
}

func (s *dispatchAccountRepoStub) List(context.Context, repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return s.list, s.listErr
}

func newTestChannelDispatchService(database *gorm.DB) *ChannelDispatchService {
	return NewChannelDispatchServiceWithRepos(
		repository.NewChannelRepositoryWithDB(database),
		repository.NewInboxConversationRepositoryWithDB(database),
		repository.NewChannelAccountRepositoryWithDB(database),
	)
}

func TestChannelDispatchService_ChannelQueryErrorIsNotSwallowed(t *testing.T) {
	queryErr := errors.New("channel query failed")
	svc := &ChannelDispatchService{
		channelRepo: &dispatchChannelRepoStub{err: queryErr},
		accRepo:     &dispatchAccountRepoStub{},
	}

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{TenantID: 1, Channel: "wx"})
	if got != nil || !errors.Is(err, queryErr) {
		t.Fatalf("渠道查询运行错误必须上抛且不得返回账号: got=%+v err=%v", got, err)
	}
}

func TestChannelDispatchService_ConversationQueryErrorIsNotSwallowed(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChannelAccount{}, &model.InboxConversation{})
	account := model.ChannelAccount{TenantID: 1, ChannelID: 1, Name: "active", IdentifyCode: "code-1", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	svc := newTestChannelDispatchService(database)
	queryErr := errors.New("conversation query failed")
	const callback = "test:channel-dispatch-conversation-query-error"
	if err := database.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "inbox_conversations" {
			tx.AddError(queryErr)
		}
	}); err != nil {
		t.Fatalf("register query fault: %v", err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove(callback) })

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{
		TenantID: 1, ChannelID: 1, Channel: "wx", ConversationID: "conv-1",
	})
	if got != nil || !errors.Is(err, queryErr) {
		t.Fatalf("会话查询运行错误必须上抛且不得降级选号: got=%+v err=%v", got, err)
	}
}

func TestChannelDispatchService_StickyAccountQueryErrorIsNotSwallowed(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChannelAccount{}, &model.InboxConversation{})
	account := model.ChannelAccount{TenantID: 1, ChannelID: 1, Name: "active", IdentifyCode: "code-1", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	conv := model.InboxConversation{TenantID: 1, Platform: "wx", AccountID: strconv.FormatUint(uint64(account.ID), 10), CustomerID: "c1", ConversationID: "conv-1"}
	if err := database.Create(&conv).Error; err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	svc := newTestChannelDispatchService(database)
	queryErr := errors.New("sticky account query failed")
	svc.accRepo = &dispatchAccountRepoStub{
		ChannelAccountRepository: svc.accRepo,
		err:                      queryErr,
		list:                     []model.ChannelAccount{account},
	}

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{
		TenantID: 1, ChannelID: 1, Channel: "wx", ConversationID: "conv-1",
	})
	if got != nil || !errors.Is(err, queryErr) {
		t.Fatalf("粘性账号查询运行错误必须上抛且不得 round-robin: got=%+v err=%v", got, err)
	}
}

func TestChannelDispatchService_PreferredAccountQueryErrorIsNotSwallowed(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ChannelAccount{})
	account := model.ChannelAccount{TenantID: 1, ChannelID: 1, Name: "active", IdentifyCode: "code-1", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}}
	if err := database.Create(&account).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	svc := newTestChannelDispatchService(database)
	queryErr := errors.New("preferred account query failed")
	svc.accRepo = &dispatchAccountRepoStub{
		ChannelAccountRepository: svc.accRepo,
		err:                      queryErr,
		list:                     []model.ChannelAccount{account},
	}

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{
		TenantID: 1, ChannelID: 1, Channel: "wx", PreferredAccountID: account.ID,
	})
	if got != nil || !errors.Is(err, queryErr) {
		t.Fatalf("首选账号查询运行错误必须上抛且不得 round-robin: got=%+v err=%v", got, err)
	}
}

func TestChannelDispatchService_RoundRobinListErrorIsNotSwallowed(t *testing.T) {
	queryErr := errors.New("account list failed")
	svc := &ChannelDispatchService{
		channelRepo: &dispatchChannelRepoStub{channel: &model.Channel{ID: 1}},
		accRepo:     &dispatchAccountRepoStub{listErr: queryErr},
	}

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{TenantID: 1, ChannelID: 1, Channel: "wx"})
	if got != nil || !errors.Is(err, queryErr) {
		t.Fatalf("账号列表运行错误必须上抛且不得返回账号: got=%+v err=%v", got, err)
	}
}

func TestChannelDispatchService_SelectionMatrix(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.InboxConversation{})
	channels := []model.Channel{
		{TenantID: 1, Name: "wx", Code: "dispatch-matrix-wx", Type: "dm", Enabled: true},
		{TenantID: 1, Name: "tg", Code: "dispatch-matrix-tg", Type: "dm", Enabled: true},
		{TenantID: 1, Name: "empty", Code: "dispatch-matrix-empty", Type: "dm", Enabled: true},
	}
	for i := range channels {
		if err := database.Create(&channels[i]).Error; err != nil {
			t.Fatalf("seed channel %d: %v", i, err)
		}
	}
	accounts := []model.ChannelAccount{
		{TenantID: 1, ChannelID: channels[0].ID, Name: "active-1", IdentifyCode: "matrix-code-1", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: channels[0].ID, Name: "active-2", IdentifyCode: "matrix-code-2", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: channels[0].ID, Name: "disabled", IdentifyCode: "matrix-code-disabled", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 1, ChannelID: channels[1].ID, Name: "other-channel", IdentifyCode: "matrix-code-tg", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
		{TenantID: 2, ChannelID: channels[0].ID, Name: "other-tenant", IdentifyCode: "matrix-code-tenant2", Status: 1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{}},
	}
	for i := range accounts {
		if err := database.Create(&accounts[i]).Error; err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
	}
	if err := database.Model(&model.ChannelAccount{}).Where("id = ?", accounts[2].ID).Update("status", 0).Error; err != nil {
		t.Fatalf("seed disabled status: %v", err)
	}
	accounts[2].Status = 0
	svc := newTestChannelDispatchService(database)
	ctx := context.Background()

	t.Run("sticky", func(t *testing.T) {
		conv := model.InboxConversation{TenantID: 1, Platform: "wx", AccountID: strconv.FormatUint(uint64(accounts[0].ID), 10), CustomerID: "sticky-c", ConversationID: "sticky-conv"}
		if err := database.Create(&conv).Error; err != nil {
			t.Fatalf("seed sticky conversation: %v", err)
		}
		got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", ConversationID: conv.ConversationID})
		if err != nil || got == nil || got.AccountID != accounts[0].ID || got.Reason != "sticky" {
			t.Fatalf("sticky 选择错误: got=%+v err=%v", got, err)
		}
	})

	t.Run("preferred", func(t *testing.T) {
		got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", PreferredAccountID: accounts[1].ID})
		if err != nil || got == nil || got.AccountID != accounts[1].ID || got.Reason != "preferred" {
			t.Fatalf("preferred 选择错误: got=%+v err=%v", got, err)
		}
	})

	for _, tc := range []struct {
		name string
		in   DispatchPickInput
	}{
		{name: "round-robin", in: DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx"}},
		{name: "record-not-found-falls-through", in: DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", ConversationID: "missing-conv"}},
		{name: "disabled-preferred-falls-through", in: DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", PreferredAccountID: accounts[2].ID}},
		{name: "cross-channel-preferred-falls-through", in: DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", PreferredAccountID: accounts[3].ID}},
		{name: "cross-tenant-preferred-falls-through", in: DispatchPickInput{TenantID: 1, ChannelID: channels[0].ID, Channel: "wx", PreferredAccountID: accounts[4].ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.PickAccount(ctx, tc.in)
			if err != nil || got == nil || got.Reason != "round_robin" {
				t.Fatalf("应降级到 round-robin: got=%+v err=%v", got, err)
			}
			if got.AccountID != accounts[0].ID && got.AccountID != accounts[1].ID {
				t.Fatalf("不得选到禁用/跨渠道/跨租户账号: got=%+v", got)
			}
		})
	}

	t.Run("no-active-account", func(t *testing.T) {
		got, err := svc.PickAccount(ctx, DispatchPickInput{TenantID: 1, ChannelID: channels[2].ID, Channel: "empty"})
		if got != nil || err == nil {
			t.Fatalf("无启用账号必须拒绝: got=%+v err=%v", got, err)
		}
	})
}
