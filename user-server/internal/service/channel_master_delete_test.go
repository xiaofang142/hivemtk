package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// channelMasterDeleteCall 记录级联删除收到的租户/渠道实参，防止「删错渠道」回归。
type channelMasterDeleteCall struct {
	tenantID  uint
	channelID uint
}

type channelMasterFakeChannelRepo struct {
	deleteErr error
	calls     []channelMasterDeleteCall
}

func (f *channelMasterFakeChannelRepo) Available() bool { return true }
func (f *channelMasterFakeChannelRepo) Create(_ context.Context, _ *model.Channel) error {
	return nil
}
func (f *channelMasterFakeChannelRepo) GetByID(_ context.Context, _, _ uint) (*model.Channel, error) {
	return nil, nil
}
func (f *channelMasterFakeChannelRepo) GetByCode(_ context.Context, _ string) (*model.Channel, error) {
	return nil, nil
}
func (f *channelMasterFakeChannelRepo) GetByTenantAndCode(_ context.Context, _ uint, _ string) (*model.Channel, error) {
	return nil, nil
}
func (f *channelMasterFakeChannelRepo) List(_ context.Context, _ repository.ChannelListOptions) ([]model.Channel, error) {
	return nil, nil
}
func (f *channelMasterFakeChannelRepo) Update(_ context.Context, _, _ uint, _ map[string]any) error {
	return nil
}
func (f *channelMasterFakeChannelRepo) Delete(_ context.Context, tenantID, id uint) error {
	f.calls = append(f.calls, channelMasterDeleteCall{tenantID: tenantID, channelID: id})
	return f.deleteErr
}

type channelMasterFakeCodeRepo struct {
	deleteErr error
	calls     []channelMasterDeleteCall
}

func (f *channelMasterFakeCodeRepo) Available() bool { return true }
func (f *channelMasterFakeCodeRepo) GetByChannel(_ context.Context, _, _ uint) (*model.ChannelIdentifyCode, error) {
	return nil, nil
}
func (f *channelMasterFakeCodeRepo) GetByCode(_ context.Context, _ string) (*model.ChannelIdentifyCode, error) {
	return nil, nil
}
func (f *channelMasterFakeCodeRepo) ListByTenant(_ context.Context, _ uint) ([]model.ChannelIdentifyCode, error) {
	return nil, nil
}
func (f *channelMasterFakeCodeRepo) ValidCodes(_ context.Context) ([]string, error) { return nil, nil }
func (f *channelMasterFakeCodeRepo) Upsert(_ context.Context, _ *model.ChannelIdentifyCode) error {
	return nil
}
func (f *channelMasterFakeCodeRepo) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
func (f *channelMasterFakeCodeRepo) DeleteByChannel(_ context.Context, tenantID, channelID uint) error {
	f.calls = append(f.calls, channelMasterDeleteCall{tenantID: tenantID, channelID: channelID})
	return f.deleteErr
}

type channelMasterFakeAccountRepo struct {
	deleteErr error
	calls     []channelMasterDeleteCall
}

func (f *channelMasterFakeAccountRepo) Available() bool { return true }
func (f *channelMasterFakeAccountRepo) Create(_ context.Context, _ *model.ChannelAccount) error {
	return nil
}
func (f *channelMasterFakeAccountRepo) GetByID(_ context.Context, _, _ uint) (*model.ChannelAccount, error) {
	return nil, nil
}
func (f *channelMasterFakeAccountRepo) List(_ context.Context, _ repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	return nil, nil
}
func (f *channelMasterFakeAccountRepo) GetByIDs(_ context.Context, _ uint, _ []uint) ([]model.ChannelAccount, error) {
	return nil, nil
}
func (f *channelMasterFakeAccountRepo) GetByIdentifyCode(_ context.Context, _ string) (*model.ChannelAccount, error) {
	return nil, nil
}
func (f *channelMasterFakeAccountRepo) Update(_ context.Context, _, _ uint, _ map[string]any) error {
	return nil
}
func (f *channelMasterFakeAccountRepo) Delete(_ context.Context, _, _ uint) error { return nil }
func (f *channelMasterFakeAccountRepo) DeleteByChannel(_ context.Context, tenantID, channelID uint) error {
	f.calls = append(f.calls, channelMasterDeleteCall{tenantID: tenantID, channelID: channelID})
	return f.deleteErr
}
func (f *channelMasterFakeAccountRepo) SetIdentifyCodeByChannel(_ context.Context, _, _ uint, _ string) error {
	return nil
}

func assertChannelMasterDeleteCall(t *testing.T, name string, calls []channelMasterDeleteCall, tenantID, channelID uint) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("%s 调用次数 = %d，期望 1（calls=%+v）", name, len(calls), calls)
	}
	if calls[0].tenantID != tenantID || calls[0].channelID != channelID {
		t.Fatalf("%s 实参 = %+v，期望 {tenantID:%d channelID:%d}", name, calls[0], tenantID, channelID)
	}
}

// 删除渠道必须级联清除识别码（BridgeIngressGuard 的权威候选源）与账号上的冗余码，
// 否则已删渠道的旧识别码仍然有效（fail-open）。
func TestChannelMasterDelete_CascadesCodeAndAccount(t *testing.T) {
	chRepo := &channelMasterFakeChannelRepo{}
	codeRepo := &channelMasterFakeCodeRepo{}
	accRepo := &channelMasterFakeAccountRepo{}
	svc := NewChannelMasterServiceWithDeps(chRepo, codeRepo, accRepo)

	if err := svc.Delete(context.Background(), 42, 7); err != nil {
		t.Fatalf("Delete 返回错误: %v", err)
	}
	assertChannelMasterDeleteCall(t, "channelRepo.Delete", chRepo.calls, 42, 7)
	assertChannelMasterDeleteCall(t, "identifyCodeRepo.DeleteByChannel", codeRepo.calls, 42, 7)
	assertChannelMasterDeleteCall(t, "accountRepo.DeleteByChannel", accRepo.calls, 42, 7)
}

func TestChannelMasterDelete_IdentifyCodeCascadeError(t *testing.T) {
	wantErr := errors.New("识别码级联删除失败")
	codeRepo := &channelMasterFakeCodeRepo{deleteErr: wantErr}
	accRepo := &channelMasterFakeAccountRepo{}
	svc := NewChannelMasterServiceWithDeps(&channelMasterFakeChannelRepo{}, codeRepo, accRepo)

	got := svc.Delete(context.Background(), 42, 7)
	if !errors.Is(got, wantErr) {
		t.Fatalf("Delete 错误 = %v，期望透传 %v", got, wantErr)
	}
	assertChannelMasterDeleteCall(t, "identifyCodeRepo.DeleteByChannel", codeRepo.calls, 42, 7)
}

func TestChannelMasterDelete_AccountCascadeError(t *testing.T) {
	wantErr := errors.New("账号级联删除失败")
	codeRepo := &channelMasterFakeCodeRepo{}
	accRepo := &channelMasterFakeAccountRepo{deleteErr: wantErr}
	svc := NewChannelMasterServiceWithDeps(&channelMasterFakeChannelRepo{}, codeRepo, accRepo)

	got := svc.Delete(context.Background(), 42, 7)
	if !errors.Is(got, wantErr) {
		t.Fatalf("Delete 错误 = %v，期望透传 %v", got, wantErr)
	}
	assertChannelMasterDeleteCall(t, "identifyCodeRepo.DeleteByChannel", codeRepo.calls, 42, 7)
	assertChannelMasterDeleteCall(t, "accountRepo.DeleteByChannel", accRepo.calls, 42, 7)
}

// 渠道本身删除失败时不得继续级联，避免产生「码删了但渠道还在」的不一致状态。
func TestChannelMasterDelete_ChannelDeleteErrorSkipsCascade(t *testing.T) {
	wantErr := errors.New("渠道删除失败")
	chRepo := &channelMasterFakeChannelRepo{deleteErr: wantErr}
	codeRepo := &channelMasterFakeCodeRepo{}
	accRepo := &channelMasterFakeAccountRepo{}
	svc := NewChannelMasterServiceWithDeps(chRepo, codeRepo, accRepo)

	got := svc.Delete(context.Background(), 42, 7)
	if !errors.Is(got, wantErr) {
		t.Fatalf("Delete 错误 = %v，期望透传 %v", got, wantErr)
	}
	assertChannelMasterDeleteCall(t, "channelRepo.Delete", chRepo.calls, 42, 7)
	if len(codeRepo.calls) != 0 || len(accRepo.calls) != 0 {
		t.Fatalf("渠道删除失败后不应级联，实际 code=%+v account=%+v", codeRepo.calls, accRepo.calls)
	}
}
