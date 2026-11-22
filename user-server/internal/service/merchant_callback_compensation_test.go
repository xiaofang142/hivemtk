package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"github.com/stretchr/testify/require"
)

type callbackCompensationRepository struct {
	repository.MerchantCallbackRepository
	page        repository.MerchantCallbackCompensationPage
	err         error
	reads, acks int
}

func (r *callbackCompensationRepository) ReadCompensationPage(ctx context.Context, subscriptionID uint, limit int) (repository.MerchantCallbackCompensationPage, error) {
	r.reads++
	return r.page, r.err
}

func (r *callbackCompensationRepository) AcknowledgeCompensationPage(ctx context.Context, page repository.MerchantCallbackCompensationPage, ids []uint) error {
	r.acks++
	return r.err
}

// 补偿读取和确认必须经过服务边界，拒绝无租户及跨租户快照。
func TestMerchantCallbackInbox_ServiceCompensationBoundary(t *testing.T) {
	ctx := tenant.WithTenantID(t.Context(), 31)
	page := repository.MerchantCallbackCompensationPage{TenantID: 31, SubscriptionID: 7, Events: []model.MerchantCallbackEvent{{ID: 9, TenantID: 31}}}
	repo := &callbackCompensationRepository{page: page}
	svc := NewMerchantCallbackConsumer(repo)
	got, err := svc.ReadCompensationPage(ctx, 7, 50)
	require.NoError(t, err)
	require.Equal(t, page, got)
	require.NoError(t, svc.AcknowledgeCompensationPage(ctx, got, []uint{9}))
	require.Equal(t, 1, repo.reads)
	require.Equal(t, 1, repo.acks)
	for _, invalid := range []context.Context{t.Context(), tenant.WithSystemScope(ctx), tenant.WithTenantID(t.Context(), 32)} {
		require.ErrorIs(t, svc.AcknowledgeCompensationPage(invalid, page, []uint{9}), repository.ErrMerchantCallbackBinding)
	}
	_, err = svc.ReadCompensationPage(t.Context(), 7, 50)
	require.ErrorIs(t, err, repository.ErrMerchantCallbackBinding)
	_, err = svc.ReadCompensationPage(tenant.WithSystemScope(ctx), 7, 50)
	require.ErrorIs(t, err, repository.ErrMerchantCallbackBinding)
	require.Equal(t, 1, repo.reads)
	require.Equal(t, 1, repo.acks)
	want := errors.New("fixture unavailable")
	repo.err = want
	_, err = svc.ReadCompensationPage(ctx, 7, 50)
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, svc.AcknowledgeCompensationPage(ctx, page, []uint{9}), want)
}

// 缺失仓储配置不能在恢复路径触发 panic 或返回成功。
func TestMerchantCallbackInbox_ServiceCompensationUnconfigured(t *testing.T) {
	svc := NewMerchantCallbackConsumer(nil)
	ctx := tenant.WithTenantID(t.Context(), 31)
	_, err := svc.ReadCompensationPage(ctx, 7, 50)
	require.ErrorIs(t, err, repository.ErrMerchantCallbackStorage)
	require.ErrorIs(t, svc.AcknowledgeCompensationPage(ctx, repository.MerchantCallbackCompensationPage{TenantID: 31, SubscriptionID: 7}, nil), repository.ErrMerchantCallbackStorage)
}
