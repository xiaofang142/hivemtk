package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// commerceFixtureAdapter is intentionally test-only: no production registration and no network.
type commerceFixtureAdapter struct {
	mu                sync.Mutex
	snapshot          CommerceSnapshot
	submits, observes int
	result            CommerceAdapterResult
	submitErr         error
	onRead            func()
	onSubmit          func()
}

func (f *commerceFixtureAdapter) Read(_ context.Context, _ CommerceAdapterRequest) (CommerceSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onRead != nil {
		f.onRead()
	}
	x := f.snapshot
	x.FetchedAt = time.Now()
	return x, nil
}
func (f *commerceFixtureAdapter) Preflight(ctx context.Context, r CommerceAdapterRequest) (CommerceQuote, error) {
	x, e := f.Read(ctx, r)
	return CommerceQuote{Snapshot: x, FeesKnown: true, PolicyVersion: "fixture-1"}, e
}
func (f *commerceFixtureAdapter) Submit(_ context.Context, _ CommerceAdapterRequest) (CommerceAdapterResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submits++
	if f.onSubmit != nil {
		f.onSubmit()
	}
	return f.result, f.submitErr
}
func (f *commerceFixtureAdapter) Observe(_ context.Context, _ CommerceAdapterRequest) (CommerceAdapterResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observes++
	return f.result, nil
}

type commerceFixture struct {
	db      *gorm.DB
	svc     CommerceService
	adapter *commerceFixtureAdapter
	ctx     context.Context
	target  dto.CommerceTarget
	actor   uint
	p       dto.CommerceParameters
	action  string
}

func newCommerceFixture(t *testing.T, profile, action string) *commerceFixture {
	t.Helper()
	database := testutil.NewTestDB(t, &model.SystemUser{}, &model.ExternalOrder{}, &model.AfterSale{}, &model.OrderChangeRequest{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{}, &model.MerchantConnection{}, &model.CommerceOperation{}, &model.CommerceApproval{}, &model.CommerceAttempt{}, &model.CommerceResource{}, &model.CommerceOutbox{}, &model.CommercePolicy{}, &model.CommerceMember{}, &model.CommerceEvidence{})
	old := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(old) })
	ctx := tenant.WithTenantID(t.Context(), 701)
	db := database.WithContext(tenant.WithSystemScope(t.Context()))
	p, _ := commerceProfile(profile)
	shop := "shop-" + uuid.NewString()
	actor := uint(812)
	u := model.SystemUser{ID: actor, TenantID: 701, Username: uuid.NewString(), Password: "fix" + "ture", Role: "admin", Enabled: true, Status: 1}
	if e := db.Create(&u).Error; e != nil {
		t.Fatal(e)
	}
	exp := time.Now().Add(time.Hour)
	c := model.MerchantConnection{TenantID: 701, Provider: p.Product, ShopID: shop, Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "fixture", CredentialCiphertext: "never-export-secret", Scopes: []string{"commerce." + action}, ExpiresAt: &exp}
	if e := db.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	order := model.ExternalOrder{TenantID: 701, Platform: p.Product, OrderID: uuid.NewString(), Status: "paid"}
	if e := db.Create(&order).Error; e != nil {
		t.Fatal(e)
	}
	b := model.CapabilitySourceBinding{TenantID: 701, Source: "order", Product: p.Product, ResourceID: order.OrderID, ShopID: shop, Region: "CN", ConnectionID: c.ID, ConsentVersion: 1}
	if e := db.Create(&b).Error; e != nil {
		t.Fatal(e)
	}
	ev := model.CapabilityEvidence{TenantID: 701, ShopID: shop, Product: p.Product, Region: "CN", Action: action, Carrier: commerceAPI, ConnectionID: c.ID, ConnectionVersion: 1, CredentialVersion: 1, ConsentVersion: 1, ContractVersion: p.Version, DocumentationVersion: p.Version, Documentation: true, Implementation: true, Simulated: false, ExpiresAt: exp}
	if e := db.Create(&ev).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Model(&model.CapabilityEvidence{}).Where("id = ?", ev.ID).Update("simulated", false).Error; e != nil {
		t.Fatal(e)
	}
	policy := model.CommercePolicy{ID: commerceDigest([]any{701, profile, shop}), TenantID: 701, Profile: profile, ShopID: shop, Version: 1, WriteEnabled: true, WriteOwner: "native", ApprovalMode: "single_owner", OwnerID: actor, RecoveryEpoch: 1}
	if e := db.Create(&policy).Error; e != nil {
		t.Fatal(e)
	}
	member := model.CommerceMember{ID: uuid.NewString(), TenantID: 701, Profile: profile, ShopID: shop, ActorID: actor, Version: 1, CanRead: true, CanApprove: true, CanExecute: true, CanReview: true, LimitMinor: 100000, Currency: "CNY"}
	if e := db.Create(&member).Error; e != nil {
		t.Fatal(e)
	}
	params := dto.CommerceParameters{Recipient: "测试收件人", Phone: "13100000000", Address: "测试地址", Province: "省", City: "市", District: "区", AmountMinor: 0, Currency: "CNY", CaseRef: "case-1", LineID: "line-1", PaymentPath: "cash", WarehouseRef: "warehouse-1", SKU: "sku-1", LogisticsRef: "tracking-1", Reason: "fixture"}
	ac, _ := commerceAction(p, action)
	params.Acknowledgements = ac.Obligations
	if action == "refund" || action == "refund.partial" || action == "refund.difference" {
		params.AmountMinor = 4000
	}
	if action == "return.approve" || action == "exchange.ship" || action == "reship" {
		params.Quantity = 1
	}
	f := &commerceFixtureAdapter{snapshot: CommerceSnapshot{Version: "v1", NativeState: "paid", Currency: "CNY", RefundableMinor: 6000, AvailableQuantity: 2, AllowedActions: []string{action}, Source: "platform"}, result: CommerceAdapterResult{State: "succeeded", NativeState: "completed", Action: action, Authoritative: true, Postcondition: true, EvidenceSource: "platform", EvidenceRef: "platform-proof", ExternalRef: "transaction-1", FundsState: "succeeded", AmountMinor: params.AmountMinor, Currency: "CNY", AddressDigest: commerceDigest([]string{params.Recipient, params.Phone, params.Address, params.Province, params.City, params.District})}}
	if action == "return.approve" {
		f.result.NativeState = "return_authorized"
	}
	if action == "exchange.ship" || action == "reship" {
		f.result.NativeState = "shipment_created"
	}
	svc := NewCommerceService(repository.NewCommerceRepository(database), map[string]CommerceAdapter{profile: f})
	return &commerceFixture{db: db, svc: svc, adapter: f, ctx: ctx, target: dto.CommerceTarget{Profile: profile, ShopID: shop, OrderID: order.ID}, actor: actor, p: params, action: action}
}
func (f *commerceFixture) propose(t *testing.T, intent string) dto.CommerceOperationResponse {
	t.Helper()
	o, e := f.svc.Propose(f.ctx, f.actor, dto.CommerceProposeRequest{CommerceTarget: f.target, Action: f.action, IntentID: intent, Revision: 1, ExpectedVersion: "v1", Parameters: f.p})
	if e != nil {
		t.Fatalf("创建意图失败: %v", e)
	}
	return o
}
func (f *commerceFixture) transition(t *testing.T, o dto.CommerceOperationResponse, verb string) dto.CommerceOperationResponse {
	t.Helper()
	x, e := f.svc.Transition(f.ctx, f.actor, o.ID, verb, dto.CommerceTransitionRequest{ExpectedVersion: o.Version, PreflightHash: o.PreflightHash})
	if e != nil {
		t.Fatalf("%s 失败: %v", verb, e)
	}
	return x
}
func (f *commerceFixture) ready(t *testing.T, intent string) dto.CommerceOperationResponse {
	o := f.propose(t, intent)
	o = f.transition(t, o, "preflight")
	return f.transition(t, o, "approve")
}

// 验证所有固定profile分别跑主路径、业务拒绝与结果未知恢复，托管路径不提交写。
func TestCommerce_ProfileExecutionMatrix(t *testing.T) {
	for _, profile := range commerceProfiles() {
		t.Run(profile.ID, func(t *testing.T) {
			action := profile.Actions[0].Action
			f := newCommerceFixture(t, profile.ID, action)
			if profile.Mode == commerceWorkbench {
				o := f.ready(t, "manual-main")
				o = f.transition(t, o, "submit")
				o = f.transition(t, o, "claim")
				o, e := f.svc.Evidence(f.ctx, f.actor, o.ID, false, dto.CommerceEvidenceRequest{ExpectedVersion: o.Version, Reference: "official-proof", Outcome: "completed", NativeState: "completed", FundsState: "succeeded"})
				if e != nil {
					t.Fatal(e)
				}
				if o.EvidenceLevel != "manual_attested" || o.State == "succeeded" {
					t.Fatalf("人工证据错误升级: %+v", o)
				}
				var ev model.CommerceEvidence
				if e = f.db.Where("operation_id = ?", o.ID).First(&ev).Error; e != nil {
					t.Fatal(e)
				}
				o, e = f.svc.Evidence(f.ctx, f.actor, o.ID, true, dto.CommerceEvidenceRequest{ExpectedVersion: o.Version, EvidenceID: ev.ID})
				if e != nil {
					t.Fatal(e)
				}
				if o.EvidenceLevel != "manual_reviewed" || o.State != "manual_review" {
					t.Fatal("人工复核不应宣称平台核验")
				}
				f.adapter.result.State = "succeeded"
				o = f.transition(t, o, "observe")
				if o.State != "succeeded" || o.EvidenceLevel != "platform_verified" {
					t.Fatalf("权威回查未完成: %+v", o)
				}
			} else {
				o := f.ready(t, "main")
				o = f.transition(t, o, "submit")
				if o.State != "succeeded" {
					t.Fatalf("主路径未完成: %+v", o)
				}
			}
			if profile.Mode == commerceManaged && f.adapter.submits != 0 {
				t.Fatal("托管模式产生额外写")
			}
			// Native business rejection remains failure, and each profile preserves unknown until a readback.
			o := f.ready(t, "reject")
			f.adapter.result.State = "failed"
			if profile.Mode == commerceWorkbench {
				o = f.transition(t, o, "submit")
				o = f.transition(t, o, "claim")
				o = f.transition(t, o, "observe")
			} else {
				o = f.transition(t, o, "submit")
			}
			if o.State != "failed" {
				t.Fatalf("原生拒绝未收口: %+v", o)
			}
			o = f.ready(t, "unknown")
			f.adapter.result.State = "succeeded"
			f.adapter.result.Authoritative = false
			if profile.Mode == commerceWorkbench {
				o = f.transition(t, o, "submit")
				o = f.transition(t, o, "claim")
				o = f.transition(t, o, "observe")
			} else {
				o = f.transition(t, o, "submit")
			}
			if o.State != "outcome_unknown" {
				t.Fatalf("无证据不得成功: %+v", o)
			}
			before := f.adapter.submits
			f.adapter.result.Authoritative = true
			o = f.transition(t, o, "observe")
			if o.State != "succeeded" || f.adapter.submits != before {
				t.Fatal("未知恢复重复写或未核明")
			}
		})
	}
}

// 抖店需要买家复核和写后断线，恢复只查询，订单守卫不因时间释放。
func TestCommerce_DouyinAwaitingAndCrashReadback(t *testing.T) {
	for _, mode := range []string{"customer", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			f := newCommerceFixture(t, "douyin_shop_cn", "address.change")
			if mode == "customer" {
				f.adapter.result.CustomerReview = true
			} else {
				f.adapter.submitErr = errors.New("fixture lost acknowledgement")
			}
			o := f.ready(t, "address")
			o = f.transition(t, o, "submit")
			if mode == "customer" && o.State != "awaiting_customer" {
				t.Fatal(o.State)
			}
			if mode == "timeout" && o.State != "outcome_unknown" {
				t.Fatal(o.State)
			}
			blocked := f.ready(t, "other-intent")
			_, e := f.svc.Transition(f.ctx, f.actor, blocked.ID, "submit", dto.CommerceTransitionRequest{ExpectedVersion: blocked.Version, PreflightHash: blocked.PreflightHash})
			if !errors.Is(e, ErrCommerceBusy) {
				t.Fatalf("应阻止同订单新意图: %v", e)
			}
			f.adapter.submitErr = nil
			f.adapter.result.CustomerReview = false
			o = f.transition(t, o, "observe")
			if o.State != "succeeded" || f.adapter.submits != 1 {
				t.Fatalf("断线恢复不应重写: state=%s writes=%d", o.State, f.adapter.submits)
			}
		})
	}
}

// 同键同参复用、异参冲突，跨租户/店铺和空scope均拒绝。
func TestCommerce_IdempotencyAndIsolation(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	o := f.propose(t, "stable")
	again := f.propose(t, "stable")
	if o.ID != again.ID {
		t.Fatal("重放新建操作")
	}
	f.p.AmountMinor++
	_, e := f.svc.Propose(f.ctx, f.actor, dto.CommerceProposeRequest{CommerceTarget: f.target, Action: f.action, IntentID: "stable", Revision: 1, ExpectedVersion: "v1", Parameters: f.p})
	if !errors.Is(e, ErrCommerceConflict) {
		t.Fatal(e)
	}
	for _, ctx := range []context.Context{context.Background(), tenant.WithTenantID(t.Context(), 702)} {
		if _, e = f.svc.Get(ctx, f.actor, o.ID); e == nil {
			t.Fatal("跨租户读取放行")
		}
	}
	wrong := f.target
	wrong.ShopID = "other-shop"
	if _, e = f.svc.Read(f.ctx, f.actor, wrong); e == nil {
		t.Fatal("跨店读取放行")
	}
	data, e := f.svc.Read(f.ctx, f.actor, f.target)
	if e != nil || data.Source != "platform" {
		t.Fatalf("实时读取失败: %v", e)
	}
}

// 审批后权限/连接/停写/余额发生变化，真实submit调用次数必须为零。
func TestCommerce_CurrentDispatchGuards(t *testing.T) {
	for _, kind := range []string{"limit", "role", "credential", "scope", "pause", "approval_expired", "balance", "version"} {
		t.Run(kind, func(t *testing.T) {
			f := newCommerceFixture(t, "douyin_shop_cn", "refund")
			o := f.ready(t, "guard")
			switch kind {
			case "limit":
				f.db.Model(&model.CommerceMember{}).Where("actor_id = ?", f.actor).Updates(map[string]any{"limit_minor": 0, "version": 2})
			case "role":
				f.db.Model(&model.SystemUser{}).Where("id = ?", f.actor).Update("enabled", false)
			case "credential":
				f.db.Model(&model.MerchantConnection{}).Where("shop_id = ?", f.target.ShopID).Update("credential_version", 2)
			case "scope":
				f.db.Model(&model.MerchantConnection{}).Where("shop_id = ?", f.target.ShopID).Update("scopes", "[]")
			case "pause":
				f.db.Model(&model.CommercePolicy{}).Where("shop_id = ?", f.target.ShopID).Updates(map[string]any{"write_enabled": false, "version": 2})
			case "approval_expired":
				f.db.Model(&model.CommerceApproval{}).Where("operation_id = ?", o.ID).Update("expires_at", time.Now().Add(-time.Minute))
			case "balance":
				f.adapter.snapshot.RefundableMinor = 100
			case "version":
				f.adapter.snapshot.Version = "v2"
			}
			out, e := f.svc.Transition(f.ctx, f.actor, o.ID, "submit", dto.CommerceTransitionRequest{ExpectedVersion: o.Version, PreflightHash: o.PreflightHash})
			if e == nil && out.State != "awaiting_approval" {
				t.Fatalf("变化未阻止写: %+v", out)
			}
			if f.adapter.submits != 0 {
				t.Fatal("失效授权产生外部写")
			}
		})
	}
}

// 资金原币后置条件、退货/换货独立成功条件，不以案件受理充当退款。
func TestCommerce_FinancialAndReversePostconditions(t *testing.T) {
	for _, action := range []string{"refund", "return.approve", "exchange.ship"} {
		t.Run(action, func(t *testing.T) {
			f := newCommerceFixture(t, "douyin_shop_cn", action)
			o := f.ready(t, "flow")
			o = f.transition(t, o, "submit")
			if o.State != "succeeded" {
				t.Fatalf("独立动作失败: %+v", o)
			}
			if action == "refund" {
				f.adapter.result.Currency = "USD"
				x := f.ready(t, "currency-mismatch")
				x = f.transition(t, x, "submit")
				if x.State != "outcome_unknown" {
					t.Fatal("跨币种结果假成功")
				}
			}
		})
	}
}

// 旧租约失效后的恢复先Observe，旧worker的fence不能覆盖新结果。
func TestCommerce_LeaseRecoveryAndOutboxRollback(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	o := f.ready(t, "lease")
	now := time.Now().Add(-time.Minute)
	if e := f.db.Model(&model.CommerceOperation{}).Where("id = ?", o.ID).Updates(map[string]any{"state": "executing", "dispatched_at": now, "lease_until": now, "lease_owner": "crashed", "fence": 1}).Error; e != nil {
		t.Fatal(e)
	}
	if n, e := f.svc.RunDue(f.ctx, 10); e != nil || n != 1 {
		t.Fatalf("恢复失败 n=%d err=%v", n, e)
	}
	if f.adapter.submits != 0 || f.adapter.observes != 1 {
		t.Fatal("租约恢复重复提交")
	}
	var count int64
	f.db.Model(&model.CommerceOutbox{}).Where("operation_id = ?", o.ID).Count(&count)
	if count < 4 {
		t.Fatalf("缺少事务事件: %d", count)
	}
	repo := repository.NewCommerceRepository(f.db)
	before := count
	e := repo.Transact(f.ctx, repository.CommerceKey{ID: o.ID, ActorID: f.actor}, func(a *repository.CommerceAggregate) error { commerceMutate(a); return errors.New("rollback") })
	if e == nil {
		t.Fatal("预期事务回滚")
	}
	f.db.Model(&model.CommerceOutbox{}).Where("operation_id = ?", o.ID).Count(&count)
	if before != count {
		t.Fatal("回滚仍产生outbox")
	}
}

// 双请求抢占同一订单至多一个派发，合法新intent不能绕过未知占用。
func TestCommerce_ConcurrentIntentReservation(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	first := f.ready(t, "first")
	second := f.ready(t, "second")
	f.adapter.result.Authoritative = false
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, o := range []dto.CommerceOperationResponse{first, second} {
		wg.Add(1)
		go func(o dto.CommerceOperationResponse) {
			defer wg.Done()
			_, e := f.svc.Transition(f.ctx, f.actor, o.ID, "submit", dto.CommerceTransitionRequest{ExpectedVersion: o.Version, PreflightHash: o.PreflightHash})
			errs <- e
		}(o)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 || f.adapter.submits != 1 {
		t.Fatalf("并发余额守卫错误 success=%d submits=%d", success, f.adapter.submits)
	}
	var resources []model.CommerceResource
	if e := f.db.Find(&resources).Error; e != nil {
		t.Fatal(e)
	}
	if len(resources) != 1 || resources[0].AmountMinor != 4000 {
		t.Fatalf("未知预占丢失: %+v", resources)
	}
}

func TestCommerce_ProductionProviderMissing(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "address.change")
	svc := NewCommerceService(repository.NewCommerceRepository(f.db), nil)
	o := f.propose(t, "no-provider")
	_, e := svc.Transition(f.ctx, f.actor, o.ID, "preflight", dto.CommerceTransitionRequest{ExpectedVersion: o.Version})
	if !errors.Is(e, ErrCommerceProvider) {
		t.Fatal(e)
	}
	profiles, e := svc.Profiles(f.ctx, f.actor)
	if e != nil || len(profiles) != 29 {
		t.Fatalf("profile数量错误: %d %v", len(profiles), e)
	}
	for _, p := range profiles {
		if p.ProviderConfigured || p.LiveStatus != "pending_live_validation" {
			t.Fatal(fmt.Sprint(p))
		}
	}
}

// 远端已成功而本地保存失败，租约恢复先Observe且submit次数仍为一。
func TestCommerce_RemoteSuccessLocalPersistenceFailure(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	o := f.ready(t, "remote-success")
	injected := false
	f.adapter.onSubmit = func() {
		f.db.Callback().Update().Before("gorm:update").Register("commerce_fixture_fail_result", func(tx *gorm.DB) {
			if tx.Statement.Table == "commerce_operations" && !injected {
				injected = true
				tx.AddError(errors.New("fixture persistence failure"))
			}
		})
	}
	_, e := f.svc.Transition(f.ctx, f.actor, o.ID, "submit", dto.CommerceTransitionRequest{ExpectedVersion: o.Version, PreflightHash: o.PreflightHash})
	if e == nil || !injected {
		t.Fatal("应命中远端写后本地保存失败")
	}
	f.db.Callback().Update().Remove("commerce_fixture_fail_result")
	f.adapter.onSubmit = nil
	past := time.Now().Add(-time.Minute)
	if e = f.db.Model(&model.CommerceOperation{}).Where("id = ?", o.ID).Updates(map[string]any{"lease_until": past, "due_at": past}).Error; e != nil {
		t.Fatal(e)
	}
	n, e := f.svc.RunDue(f.ctx, 10)
	if e != nil || n != 1 {
		t.Fatalf("恢复失败: %d %v", n, e)
	}
	recovered, e := f.svc.Get(f.ctx, f.actor, o.ID)
	if e != nil || recovered.State != "succeeded" || f.adapter.submits != 1 || f.adapter.observes != 1 {
		t.Fatalf("恢复重复写或未核明: %+v err=%v writes=%d", recovered, e, f.adapter.submits)
	}
}

// 双worker并发恢复同一已出站操作，至多一条有效观察租约且不会再Submit。
func TestCommerce_DualWorkerRecovery(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	o := f.ready(t, "workers")
	past := time.Now().Add(-time.Minute)
	if e := f.db.Model(&model.CommerceOperation{}).Where("id = ?", o.ID).Updates(map[string]any{"state": "executing", "dispatched_at": past, "lease_until": past, "due_at": past}).Error; e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = f.svc.RunDue(f.ctx, 10) }()
	}
	wg.Wait()
	recovered, e := f.svc.Get(f.ctx, f.actor, o.ID)
	if e != nil || recovered.State != "succeeded" || f.adapter.submits != 0 || f.adapter.observes != 1 {
		t.Fatalf("双worker恢复错误: %+v %v observe=%d", recovered, e, f.adapter.observes)
	}
}

// ERP执行与平台权威回读分开：ERP受理不能假报平台完成或再走原生写。
func TestCommerce_ERPSingleWriteOwner(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	if e := f.db.Model(&model.CommercePolicy{}).Where("shop_id = ?", f.target.ShopID).Update("write_owner", "erp:approved-connector").Error; e != nil {
		t.Fatal(e)
	}
	f.adapter.result.State = "accepted"
	f.adapter.result.EvidenceSource = "erp"
	f.adapter.result.Authoritative = false
	o := f.ready(t, "erp")
	o = f.transition(t, o, "submit")
	if o.State != "accepted" || o.WriteOwner != "erp:approved-connector" {
		t.Fatal("ERP受理错误")
	}
	if _, e := f.svc.ConfigurePolicy(f.ctx, f.actor, dto.CommercePolicyRequest{Profile: f.target.Profile, ShopID: f.target.ShopID, ExpectedVersion: 1, WriteEnabled: true, WriteOwner: "native", ApprovalMode: "single_owner", OwnerID: f.actor, RecoveryEpoch: 1}); e != nil {
		t.Fatal(e)
	}
	second := f.propose(t, "native-intent")
	_, e := f.svc.Transition(f.ctx, f.actor, second.ID, "preflight", dto.CommerceTransitionRequest{ExpectedVersion: second.Version})
	if e != nil {
		t.Fatal(e)
	}
	f.adapter.result.State = "succeeded"
	f.adapter.result.Authoritative = true
	f.adapter.result.EvidenceSource = "platform"
	o = f.transition(t, o, "observe")
	if o.State != "succeeded" || f.adapter.submits != 1 {
		t.Fatal("ERP未知恢复重复写")
	}
}

// 关联现有售后/改单只能属于同租户同订单，不迁移历史executed为平台成功。
func TestCommerce_LinkExistingRecords(t *testing.T) {
	f := newCommerceFixture(t, "douyin_shop_cn", "refund")
	var order model.ExternalOrder
	if e := f.db.First(&order, f.target.OrderID).Error; e != nil {
		t.Fatal(e)
	}
	as := model.AfterSale{TenantID: 701, Platform: order.Platform, OrderID: order.OrderID, Type: "refund", Status: "pending"}
	if e := f.db.Create(&as).Error; e != nil {
		t.Fatal(e)
	}
	req := dto.CommerceProposeRequest{CommerceTarget: f.target, Action: f.action, IntentID: "linked", Revision: 1, ExpectedVersion: "v1", Parameters: f.p, AfterSaleID: as.ID}
	o, e := f.svc.Propose(f.ctx, f.actor, req)
	if e != nil || o.AfterSaleID != as.ID {
		t.Fatalf("未复用已有售后: %+v %v", o, e)
	}
	if e = f.db.Model(&model.AfterSale{}).Where("id = ?", as.ID).Update("order_id", "other-order").Error; e != nil {
		t.Fatal(e)
	}
	req.IntentID = "foreign-case"
	if _, e = f.svc.Propose(f.ctx, f.actor, req); !errors.Is(e, ErrCommerceDenied) {
		t.Fatalf("错订单case被接受: %v", e)
	}
}

// 工作台已经出站后不允许再次认领并点击同一退款，unknown仍持资源守卫。
func TestCommerce_WorkbenchCannotReclaimDispatched(t *testing.T) {
	f := newCommerceFixture(t, "etsy", "refund")
	o := f.ready(t, "workbench")
	o = f.transition(t, o, "submit")
	o = f.transition(t, o, "claim")
	_, e := f.svc.Transition(f.ctx, f.actor, o.ID, "claim", dto.CommerceTransitionRequest{ExpectedVersion: o.Version, PreflightHash: o.PreflightHash})
	if !errors.Is(e, ErrCommerceConflict) {
		t.Fatalf("已出站任务再次领取: %v", e)
	}
}
