package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

type dispatchConversationRepoStub struct {
	conv *model.InboxConversation
	err  error
}

func (s *dispatchConversationRepoStub) FindLatestByPlatformAndPeer(
	context.Context,
	uint,
	string,
	string,
	string,
) (*model.InboxConversation, error) {
	return s.conv, s.err
}

func TestChannelDispatchService_WithReposUsesInjectedDependencies(t *testing.T) {
	account := model.ChannelAccount{
		ID:           7,
		TenantID:     1,
		ChannelID:    1,
		Name:         "injected-account",
		IdentifyCode: "injected-code",
		Status:       1,
	}
	svc := NewChannelDispatchServiceWithRepos(
		&dispatchChannelRepoStub{},
		&dispatchConversationRepoStub{
			conv: &model.InboxConversation{
				TenantID:       1,
				Platform:       "wx",
				AccountID:      strconv.FormatUint(uint64(account.ID), 10),
				ConversationID: "conv-injected",
			},
		},
		&dispatchAccountRepoStub{account: &account},
	)

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{
		TenantID:       1,
		ChannelID:      1,
		Channel:        "wx",
		ConversationID: "conv-injected",
	})
	if err != nil || got == nil {
		t.Fatalf("注入仓储后应命中粘性账号: got=%+v err=%v", got, err)
	}
	if got.AccountID != account.ID || got.Reason != "sticky" || got.IdentifyCode != account.IdentifyCode {
		t.Fatalf("必须使用注入的账号仓储: got=%+v", got)
	}
}

func TestChannelDispatchService_NilDatabaseRepositoriesFailClosed(t *testing.T) {
	svc := NewChannelDispatchServiceWithRepos(
		repository.NewChannelRepositoryWithDB(nil),
		repository.NewInboxConversationRepositoryWithDB(nil),
		repository.NewChannelAccountRepositoryWithDB(nil),
	)

	got, err := svc.PickAccount(context.Background(), DispatchPickInput{
		TenantID: 1,
		Channel:  "wx",
	})
	if got != nil || !errors.Is(err, repository.ErrChannelStoreUnavailable) {
		t.Fatalf("无库仓储必须 fail-closed 且不得返回账号: got=%+v err=%v", got, err)
	}
}
