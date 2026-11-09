package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// ---------- fake 仓储 ----------

type fakeOrderChangeRepo struct {
	mu   sync.Mutex
	rows map[uint][]*model.OrderChangeRequest
	seq  uint
}

func newFakeOrderChangeRepo() *fakeOrderChangeRepo {
	return &fakeOrderChangeRepo{rows: map[uint][]*model.OrderChangeRequest{}}
}

func (f *fakeOrderChangeRepo) tenantOf(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

func (f *fakeOrderChangeRepo) Create(ctx context.Context, rec *model.OrderChangeRequest) error {
	if rec.TenantID == 0 {
		rec.TenantID = f.tenantOf(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	rec.ID = f.seq
	cp := *rec
	f.rows[rec.TenantID] = append(f.rows[rec.TenantID], &cp)
	return nil
}

func (f *fakeOrderChangeRepo) UpdateStatus(ctx context.Context, id uint, patch repository.OrderChangeStatusPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ID == id {
			r.Status = patch.Status
			r.ApprovedBy, r.ApprovedAt = patch.ApprovedBy, patch.ApprovedAt
			r.ExecutorNote, r.ExecutedBy, r.ExecutedAt = patch.ExecutorNote, patch.ExecutedBy, patch.ExecutedAt
			r.RejectedBy, r.RejectReason, r.RejectedAt = patch.RejectedBy, patch.RejectReason, patch.RejectedAt
			r.CancelledAt = patch.CancelledAt
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeOrderChangeRepo) GetByID(ctx context.Context, id uint) (*model.OrderChangeRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ID == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOrderChangeRepo) GetByChangeNo(ctx context.Context, changeNo string) (*model.OrderChangeRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if r.ChangeNo == changeNo {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeOrderChangeRepo) List(ctx context.Context, status string, page, pageSize int) ([]model.OrderChangeRequest, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []model.OrderChangeRequest{}
	for _, r := range f.rows[f.tenantOf(ctx)] {
		if status == "" || r.Status == status {
			out = append(out, *r)
		}
	}
	return out, int64(len(out)), nil
}

// ---------- 能力路由（创建期冻结） ----------

func TestOrderChange_CapabilityRouting(t *testing.T) {
	cases := map[string]string{
		"jd":      CapabilityDirectAPI,
		"douyin":  CapabilityBuyerInitiate,
		"xianyu":  CapabilityBuyerInitiate,
		"grasp":   CapabilityViaERP,
		"shopify": CapabilityManual, // 未知平台 manual 兜底
	}
	for platform, want := range cases {
		if got := capabilityForPlatform(platform); got != want {
			t.Fatalf("平台 %s 路由应 %s, got %s", platform, want, got)
		}
	}
}

// ---------- 状态机 + 镜像校验（fake repo） ----------

func newOrderChangeSvcForTest() *OrderChangeService {
	return NewOrderChangeService(newFakeOrderChangeRepo(), nil) // orderRepo nil=跳过镜像校验（fake 面）
}

func TestOrderChangeStateMachine_Fake(t *testing.T) {
	svc := newOrderChangeSvcForTest()
	ctx := tenant.ContextWithDefault(context.Background())

	rec, err := svc.Create(ctx, OrderChangeCreateInput{
		Platform: "douyin", OrderID: "DY-1", ChangeType: "address",
		Payload: map[string]any{"address": "上海市浦东新区"}, Reason: "客户要求改地址",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Status != "pending" || rec.Capability != CapabilityBuyerInitiate {
		t.Fatalf("初始态/路由不符: %+v", rec)
	}

	// 非法迁移：executed 前拒绝 execute
	if _, err := svc.Execute(ctx, rec.ID, "op1"); err == nil {
		t.Fatal("pending 直接 execute 应被拒")
	}
	if _, err := svc.Approve(ctx, rec.ID, "审批人"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := svc.Reject(ctx, rec.ID, "op", "改主意"); err == nil {
		t.Fatal("approved 后 reject 应被拒")
	}
	// buyer_initiated 通路可执行
	rec2, err := svc.Execute(ctx, rec.ID, "op2")
	if err != nil {
		t.Fatalf("execute buyer_initiated: %v", err)
	}
	if rec2.Status != "executed" || rec2.ExecutorNote == "" {
		t.Fatalf("执行形态不符: %+v", rec2)
	}
	// 终态不可逆
	if _, err := svc.Cancel(ctx, rec.ID, "op3"); err == nil {
		t.Fatal("executed 终态不可取消")
	}
}

func TestOrderChange_ApproveOperatorRequired(t *testing.T) {
	svc := newOrderChangeSvcForTest()
	ctx := tenant.ContextWithDefault(context.Background())
	rec, _ := svc.Create(ctx, OrderChangeCreateInput{Platform: "jd", OrderID: "JD-1", ChangeType: "address", Payload: map[string]any{"address": "x"}})
	if _, err := svc.Approve(ctx, rec.ID, ""); err == nil {
		t.Fatal("空操作者审批应拒（留痕 fail-closed）")
	}
	if _, err := svc.Reject(ctx, rec.ID, "op", ""); err == nil {
		t.Fatal("拒绝无原因应拒")
	}
}

// ---------- 真实 PG：镜像校验 + 越租户 + direct_api 显式拒绝 ----------

func TestOrderChange_PG_MirrorValidationAndRouting(t *testing.T) {
	db := testutil.NewTestDB(t, &model.OrderChangeRequest{}, &model.ExternalOrder{})
	ctx := tenant.ContextWithDefault(context.Background())

	if err := db.Create(&model.ExternalOrder{
		TenantID: 1, Platform: "jd", OrderID: "JD-PG-1", UserName: "u", UserPhone: "138", Status: "等待出库",
	}).Error; err != nil {
		t.Fatalf("预置镜像失败: %v", err)
	}

	svc := NewOrderChangeService(repository.NewOrderChangeRequestRepositoryWithDB(db), repository.NewExternalOrderRepository())

	// 镜像未找到 → 拒绝（不编造订单）
	_, err := svc.Create(ctx, OrderChangeCreateInput{Platform: "jd", OrderID: "JD-NOT-EXIST", ChangeType: "address", Payload: map[string]any{"address": "x"}})
	if be, ok := err.(*bizerr.BizError); !ok || be.Code != bizerr.CodeNotFound {
		t.Fatalf("镜像未找到应 4004, got %v", err)
	}

	// 镜像命中 → direct_api 冻结 + 审批后执行显式不可用（凭证🔵未接）
	rec, err := svc.Create(ctx, OrderChangeCreateInput{Platform: "jd", OrderID: "JD-PG-1", ChangeType: "address", Payload: map[string]any{"address": "上海市"}, Reason: "客户改址"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Capability != CapabilityDirectAPI {
		t.Fatalf("jd 应冻结 direct_api, got %s", rec.Capability)
	}
	if _, err := svc.Approve(ctx, rec.ID, "审批人A"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	_, err = svc.Execute(ctx, rec.ID, "执行人B")
	if err == nil || !errors.Is(err, ErrCapabilityNotAvailable) && !stringContainsCB(err.Error(), "未接入") {
		t.Fatalf("direct_api 未接入应显式拒绝, got %v", err)
	}
	// 显式拒绝不翻转状态（仍 approved，等待通路接入）
	recNow, _ := svc.Get(ctx, rec.ID)
	if recNow.Status != "approved" {
		t.Fatalf("拒绝执行后状态应保持 approved, got %s", recNow.Status)
	}

	// 越租户：B 租户看不到 A 的申请单（仓储租户过滤）
	ctxB := tenant.WithTenantID(context.Background(), 502)
	if _, err := svc.Get(ctxB, rec.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("越租户读取应 NotFound, got %v", err)
	}
}

func stringContainsCB(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOfCB(s, sub) >= 0
}

func indexOfCB(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------- 工具适配：AI 侧只建申请单 ----------

func TestOrderChange_PortAdapter_ToolFace(t *testing.T) {
	svc := newOrderChangeSvcForTest()
	adapter := NewOrderChangeRequestPortAdapter(svc)
	ctx := tenant.ContextWithDefault(context.Background())

	out, err := adapter.CreateChangeRequest(ctx, tooluse.OrderChangeRequestInput{
		Platform: "douyin", OrderID: "DY-1", CustomerID: "c-9", SessionID: "s-9",
		ChangeType: "address", Payload: map[string]any{"address": "上海市"}, Reason: "改地址",
	})
	if err != nil {
		t.Fatalf("adapter create: %v", err)
	}
	if out["status"] != "pending" || out["capability"] == nil {
		t.Fatalf("摘要形态不符: %v", out)
	}
}
