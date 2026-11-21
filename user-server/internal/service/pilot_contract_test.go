package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/system/install"
)

// 缺失/未初始化安装不能形成启动基线；回读必须重新核对当前部署身份。
func TestPilotContract_InstallationRechecked(t *testing.T) {
	svc, _, ctx := pilotService(t)
	if _, err := svc.Save(ctx, pilotFixture(), 7); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		lock    *install.Lock
		blocker string
	}{
		{"missing", nil, "installation_not_initialized"},
		{"uninitialized", &install.Lock{InstallID: "ins-fake"}, "installation_not_initialized"},
		{"moved", &install.Lock{InstallID: "ins-other", Initialized: true}, "installation_mapping_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc.installationLoad = func() (*install.Lock, error) { return tc.lock, nil }
			got, err := svc.Load(ctx)
			if err != nil || got.Readiness != "blocked" || !slices.Contains(got.Blockers, tc.blocker) {
				t.Fatalf("安装状态未重新核对: %+v %v", got, err)
			}
		})
	}
}

// 即使持久化草稿被改成 live_validated，也不能提升生产许可。
func TestPilotContract_PersistedLiveCannotAuthorize(t *testing.T) {
	svc, store, ctx := pilotService(t)
	in := pilotFixture()
	in.ProviderMode = "live_validated"
	raw, err := json.Marshal(pilotContractDocument{Contract: in})
	if err != nil {
		t.Fatal(err)
	}
	store.values[42] = string(raw)
	got, err := svc.Load(ctx)
	if err != nil || got.ProductionGate != "blocked" || got.Readiness != "blocked" || !slices.Contains(got.Blockers, "provider_mode_invalid") {
		t.Fatalf("持久化字段放行生产: %+v %v", got, err)
	}
}

// fakePilotStore 只模拟租户 KV，不访问平台或商户。
type fakePilotStore struct {
	values map[uint]string
	err    error
	calls  int
}

func (f *fakePilotStore) Load(ctx context.Context) (string, error) {
	f.calls++
	return f.values[tenant.WriteTenantID(ctx)], f.err
}

func (f *fakePilotStore) Save(ctx context.Context, previous, updated string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	id := tenant.WriteTenantID(ctx)
	if f.values[id] != previous {
		return ErrPilotConflict
	}
	f.values[id] = updated
	return nil
}

func pilotFixture() dto.PilotContract {
	zero := int64(0)
	return dto.PilotContract{
		Version: "pilot-contract.v1", InstallID: "ins-fake", TenantID: 42,
		MerchantType: "simulated_merchant", ProviderMode: "fake",
		Shop:      dto.PilotShop{Platform: "douyin_shop", ShopID: "fake-shop", OwnerRef: "fake-owner", AuthorizationRef: "fake:authorization"},
		ReadScope: []string{"orders.read"}, WriteScope: []string{"orders.address.write"},
		FundsLimit:     &dto.PilotFundsLimit{Currency: "CNY", AmountMinor: &zero},
		TestAuthorizer: "fake-authorizer", ObservationMinutes: 60, RollbackOwner: "fake-rollback",
		Action: dto.PilotAction{Name: "single_order_address_change", OrderID: "fake-order", Postconditions: []string{"fake:address_matches", "fake:no_payment"}, ResponsibleOwner: "fake-owner"},
		Dependencies: []dto.PilotDependency{
			{ID: "FA", CloseCommit: "fake:fa-commit", TestEvidence: "fake:fa-tests"},
			{ID: "AR", CloseCommit: "fake:ar-commit", TestEvidence: "fake:ar-tests"},
			{ID: "CH", CloseCommit: "fake:ch-commit", TestEvidence: "fake:ch-tests"},
		}, EngineeringMinutes: 90, PlatformApprovalWait: "pending_external_approval",
	}
}

func pilotService(t *testing.T) (*pilotContractService, *fakePilotStore, context.Context) {
	t.Helper()
	store := &fakePilotStore{values: make(map[uint]string)}
	svc := NewPilotContractService(store, func() (*install.Lock, error) {
		return &install.Lock{InstallID: "ins-fake", Version: "fake-build", Initialized: true}, nil
	}).(*pilotContractService)
	return svc, store, tenant.OnlyTenant(context.Background(), 42)
}

// 00-A：锁定单笔模拟订单与负责人；本地闭环绝不等于生产放行。
func TestPilotContract_00A(t *testing.T) {
	svc, _, ctx := pilotService(t)
	got, err := svc.Save(ctx, pilotFixture(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Readiness != "ready_for_simulated_validation" || len(got.Blockers) != 0 {
		t.Fatalf("本地契约未闭环: %+v", got)
	}
	if got.ProductionGate != "blocked" || !got.Simulated || got.ValidationSignal != "pending_live_validation" {
		t.Fatalf("模拟信号丢失: %+v", got)
	}
	if got.Contract.Action.OrderID != "fake-order" || got.UpdatedBy != 7 || got.Contract.Revision != 1 {
		t.Fatalf("版本/负责人未锁定: %+v", got)
	}
	loaded, err := svc.Load(ctx)
	if err != nil || loaded.Contract.Revision != 1 || !loaded.Configured || loaded.UpdatedBy != 7 {
		t.Fatalf("回读失败: %+v %v", loaded, err)
	}
}

// 金额零必须在真实 JSON 解码后仍区分显式零、缺失和 null；不完整草稿可保存但不能就绪。
func TestPilotContract_FundsLimitAmountPresenceFromJSON(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fundsJSON   string
		wantReady   string
		wantBlocked bool
		wantInvalid bool
	}{
		{name: "omitted", fundsJSON: `{"currency":"CNY"}`, wantReady: "blocked", wantBlocked: true},
		{name: "null", fundsJSON: `{"currency":"CNY","amount_minor":null}`, wantReady: "blocked", wantBlocked: true},
		{name: "explicit_zero", fundsJSON: `{"currency":"CNY","amount_minor":0}`, wantReady: "ready_for_simulated_validation"},
		{name: "negative", fundsJSON: `{"currency":"CNY","amount_minor":-1}`, wantInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(pilotFixture())
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			fields["funds_limit"] = json.RawMessage(tc.fundsJSON)
			encoded, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}

			var in dto.PilotContract
			if err := json.Unmarshal(encoded, &in); err != nil {
				t.Fatalf("契约 JSON 解码失败: %v", err)
			}
			svc, _, ctx := pilotService(t)
			got, err := svc.Save(ctx, in, 7)
			if tc.wantInvalid {
				if !errors.Is(err, ErrPilotInvalid) {
					t.Fatalf("负金额应拒绝保存，得到响应=%+v 错误=%v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不完整/有效草稿应可保存: %v", err)
			}
			if got.Readiness != tc.wantReady {
				t.Fatalf("金额 JSON %s 的 readiness=%q，期望 %q", tc.name, got.Readiness, tc.wantReady)
			}
			if got.ProductionGate != "blocked" {
				t.Fatalf("契约不能开放生产执行: %+v", got)
			}
			if tc.wantBlocked != slices.Contains(got.Blockers, "funds_limit_missing") {
				t.Fatalf("金额 JSON %s 的 funds_limit_missing blocker 不符合预期: %v", tc.name, got.Blockers)
			}
		})
	}
}

// 00-B：没有店铺仅保留 fake 草稿，不能提升生产许可。
func TestPilotContract_00B(t *testing.T) {
	svc, _, ctx := pilotService(t)
	in := pilotFixture()
	in.Shop = dto.PilotShop{}
	got, err := svc.Save(ctx, in, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Readiness != "blocked" || !slices.Contains(got.Blockers, "shop_authorization_missing") || got.ProductionGate != "blocked" {
		t.Fatalf("无店铺被放行: %+v", got)
	}
}

// 00-C：FA 行是否存在不参与判定，关闭提交和测试证据缺一不可。
func TestPilotContract_00C(t *testing.T) {
	for _, field := range []string{"commit", "test", "row"} {
		t.Run(field, func(t *testing.T) {
			svc, _, ctx := pilotService(t)
			in := pilotFixture()
			switch field {
			case "commit":
				in.Dependencies[0].CloseCommit = ""
			case "test":
				in.Dependencies[0].TestEvidence = ""
			case "row":
				in.Dependencies = in.Dependencies[1:]
			}
			got, err := svc.Validate(ctx, in)
			if err != nil || !slices.Contains(got.Blockers, "dependency_FA_closure_missing") {
				t.Fatalf("行消失被当成关闭: %+v %v", got, err)
			}
		})
	}
}

func TestPilotContract_UnconfiguredBaselineAndIsolation(t *testing.T) {
	svc, store, ctx := pilotService(t)
	got, err := svc.Load(ctx)
	if err != nil || got.Configured || !got.BaselineAvailable || got.Installation.InstallID != "ins-fake" ||
		got.Contract.InstallID != "ins-fake" || got.Contract.TenantID != 42 || got.Contract.ProviderMode != "" {
		t.Fatalf("启动基线依赖业务配置: %+v %v", got, err)
	}
	if _, err := svc.Save(ctx, pilotFixture(), 7); err != nil {
		t.Fatal(err)
	}
	other := tenant.OnlyTenant(context.Background(), 43)
	got, err = svc.Load(other)
	if err != nil || got.Configured {
		t.Fatalf("跨租户读到契约: %+v %v", got, err)
	}
	before := store.calls
	if _, err := svc.Load(context.Background()); !errors.Is(err, ErrPilotTenant) || store.calls != before {
		t.Fatalf("无租户仍访问仓储: %v", err)
	}
	if _, err := svc.Save(ctx, pilotFixture(), 7); !errors.Is(err, ErrPilotConflict) {
		t.Fatalf("旧修订覆盖新修订: %v", err)
	}
}

func TestPilotContract_InvalidBoundary(t *testing.T) {
	for _, field := range []string{"version", "tenant", "install", "provider", "funds", "actor", "effort"} {
		t.Run(field, func(t *testing.T) {
			svc, store, ctx := pilotService(t)
			in := pilotFixture()
			actor := uint(7)
			switch field {
			case "version":
				in.Version = "v2"
			case "tenant":
				in.TenantID = 43
			case "install":
				in.InstallID = "other"
			case "provider":
				in.ProviderMode = "live_validated"
			case "funds":
				negative := int64(-1)
				in.FundsLimit.AmountMinor = &negative
			case "actor":
				actor = 0
			case "effort":
				in.EngineeringMinutes = -1
			}
			if _, err := svc.Save(ctx, in, actor); err == nil || len(store.values) != 0 {
				t.Fatalf("非法边界被保存: %v", err)
			}
		})
	}
}

func TestPilotContract_StoreAndInstallFailuresAreNotUnconfigured(t *testing.T) {
	svc, store, ctx := pilotService(t)
	store.err = errors.New("fake unavailable")
	if _, err := svc.Load(ctx); err == nil {
		t.Fatal("仓储故障被当作未配置")
	}
	store.err = nil
	store.values[42] = "broken-json"
	if _, err := svc.Load(ctx); err == nil {
		t.Fatal("坏配置被当作未配置")
	}
	store.values[42] = ""
	svc.installationLoad = func() (*install.Lock, error) { return nil, errors.New("fake unreadable lock") }
	if _, err := svc.Load(ctx); err == nil {
		t.Fatal("安装读取失败被吞")
	}
}
