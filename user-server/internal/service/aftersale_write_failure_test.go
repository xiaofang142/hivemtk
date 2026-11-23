package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
)

// fakeAfterSaleRepo 持久化边界假实现，用于注入远端回写状态持久化失败。
type fakeAfterSaleRepo struct {
	createErr, updateErr error
	created, updated     *model.AfterSale
}

func (r *fakeAfterSaleRepo) Create(_ context.Context, as *model.AfterSale) error {
	if r.createErr != nil {
		return r.createErr
	}
	as.ID = 42
	copied := *as
	r.created = &copied
	return nil
}

func (r *fakeAfterSaleRepo) Update(_ context.Context, as *model.AfterSale) error {
	copied := *as
	r.updated = &copied
	return r.updateErr
}

func (r *fakeAfterSaleRepo) ListByOrder(context.Context, string, string) ([]model.AfterSale, error) {
	return nil, nil
}

func (r *fakeAfterSaleRepo) ListByCustomer(context.Context, string) ([]model.AfterSale, error) {
	return nil, nil
}

// 远端已受理并返回资金成功态，但本地持久化失败：不得对外宣称资金成功，须回退本地 pending。
func TestApplyRemoteWriteBack_WriteFails_KeepsLocalPending(t *testing.T) {
	repo := &fakeAfterSaleRepo{updateErr: errors.New("db write failed")}
	svc := &AfterSaleService{repo: repo}
	as := &model.AfterSale{Status: portcontract.AfterSalePending}
	res := &AfterSaleExternalResult{ExternalID: "EXT-1", Status: "refunded"}

	svc.applyRemoteWriteBack(context.Background(), as, res)

	if as.Status != portcontract.AfterSalePending {
		t.Fatalf("持久化失败时须回退本地 pending，实际=%q", as.Status)
	}
	if as.ExternalID != "EXT-1" {
		t.Fatalf("外部单号须留痕 EXT-1，实际=%q", as.ExternalID)
	}
	if repo.updated == nil {
		t.Fatal("应尝试持久化远端回写状态")
	}
}

// 远端受理且本地持久化成功：对外显示远端资金成功态。
func TestApplyRemoteWriteBack_WriteOK_ShowsRemoteStatus(t *testing.T) {
	repo := &fakeAfterSaleRepo{}
	svc := &AfterSaleService{repo: repo}
	as := &model.AfterSale{Status: portcontract.AfterSalePending}
	res := &AfterSaleExternalResult{ExternalID: "EXT-2", Status: "refunded"}

	svc.applyRemoteWriteBack(context.Background(), as, res)

	if as.Status != "refunded" {
		t.Fatalf("持久化成功时应显示远端状态 refunded，实际=%q", as.Status)
	}
	if as.ExternalID != "EXT-2" {
		t.Fatalf("外部单号须为 EXT-2，实际=%q", as.ExternalID)
	}
}
