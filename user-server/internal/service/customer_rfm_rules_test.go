package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type rfmRuleCustomers struct {
	repository.CustomerRepository
	items  []*model.Customer
	onList func()
}

func (r *rfmRuleCustomers) GetByID(_ context.Context, id string) (*model.Customer, error) {
	return &model.Customer{ID: id}, nil
}

func (r *rfmRuleCustomers) List(_ context.Context, page, size int, _ string) ([]*model.Customer, int64, error) {
	if r.onList != nil {
		r.onList()
	}
	start := (page - 1) * size
	if start >= len(r.items) {
		return nil, int64(len(r.items)), nil
	}
	end := start + size
	if end > len(r.items) {
		end = len(r.items)
	}
	return r.items[start:end], int64(len(r.items)), nil
}

// 两个真实计算入口使用相同的零到一百分映射，保存值也必须一致。
func TestRFMComposite_RealCalculationPaths(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		r, f, m, want int
	}{
		{"最低", 1, 1, 1, 0}, {"最高", 5, 5, 5, 100}, {"中间", 3, 3, 3, 50},
		{"仅最近消费提高", 5, 1, 1, 30}, {"仅频次提高", 1, 5, 1, 30}, {"仅金额提高", 1, 1, 5, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultRFMConfig()
			cfg.AutoEnqueueRecovery = false
			cfg.RecencyBuckets = make([]int, tc.r-1)
			for i := range cfg.RecencyBuckets {
				cfg.RecencyBuckets[i] = 400
			}
			cfg.FrequencyBuckets = make([]int, tc.f-1)
			cfg.MonetaryBuckets = make([]int64, tc.m-1)
			var saved *model.CustomerRFM
			svc := NewCustomerRFMServiceWithRepos(&stubRFMRepo{CustomerRFMSaver: func(_ context.Context, row *model.CustomerRFM) error { saved = row; return nil }}, &rfmRuleCustomers{}, newStubOrderRepo([]*model.Order{{ID: "order", Price: "100", CreateTime: now.Add(-24 * time.Hour).Unix()}}, nil), nil)
			svc.nowFunc = func() time.Time { return now }
			for _, loaded := range []bool{false, true} {
				var row *model.CustomerRFM
				var err error
				if loaded {
					row, err = svc.computeForCustomerLoaded(context.Background(), &model.Customer{ID: "customer"}, cfg)
				} else {
					row, err = svc.ComputeForCustomer(context.Background(), "customer", cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				if row.CompositeScore != tc.want || saved.CompositeScore != tc.want {
					t.Fatalf("loaded=%v 综合分=%d 保存值=%d，期望%d", loaded, row.CompositeScore, saved.CompositeScore, tc.want)
				}
			}
		})
	}
}

// 原始指标应服从注入时钟，无消费不能伪装为当天消费。
func TestRFMRawMetrics_FrozenClockAndNoOrders(t *testing.T) {
	now := time.Date(2020, 1, 10, 12, 0, 0, 0, time.UTC)
	svc := NewCustomerRFMServiceWithRepos(&stubRFMRepo{}, nil, newStubOrderRepo([]*model.Order{{ID: "order", Price: "100", CreateTime: now.Add(-48 * time.Hour).Unix()}}, nil), nil)
	svc.nowFunc = func() time.Time { return now }
	r, _, _, _, err := svc.computeRawMetricsForCustomer(context.Background(), &model.Customer{ID: "customer"})
	if err != nil || r != 2 {
		t.Fatalf("固定时钟应得2天，实际%d err=%v", r, err)
	}
	svc.orderRepo = newStubOrderRepo(nil, nil)
	r, _, _, last, err := svc.computeRawMetricsForCustomer(context.Background(), &model.Customer{ID: "customer"})
	if err != nil || r != 9999 || last != nil {
		t.Fatalf("无消费应无日期且9999天，实际%d %v err=%v", r, last, err)
	}
}

// 在真实规则仓储中读取当前租户规则，一轮计算固定快照，故障不得降级为默认值。
func TestRFMComputeAll_ActiveTenantRuleAndReadFailure(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	database := testutil.NewTestDB(t, &model.RFMRule{})
	previous := db.GetDB()
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(previous) })
	ctxA := tenant.WithTenantID(context.Background(), 41)
	ctxB := tenant.WithTenantID(context.Background(), 42)
	ruleRepo := repository.NewRFMRuleRepository()
	ruleA := &model.RFMRule{Name: "A", RDays1: 1, RDays2: 2, RDays3: 3, RDays4: 4, FCount2: 2, FCount3: 3, FCount4: 4, FCount5: 5, MAmount1: 1, MAmount2: 200, MAmount3: 300, MAmount4: 400, MAmount5: 500, IsActive: true}
	ruleB := &model.RFMRule{Name: "B", RDays1: 100, RDays2: 200, RDays3: 300, RDays4: 400, FCount2: 1, FCount3: 1, FCount4: 1, FCount5: 1, MAmount1: 1, MAmount2: 1, MAmount3: 1, MAmount4: 1, MAmount5: 1, IsActive: true}
	if err := ruleRepo.Create(ctxA, ruleA); err != nil {
		t.Fatal(err)
	}
	if err := ruleRepo.Create(ctxB, ruleB); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	customers := &rfmRuleCustomers{items: []*model.Customer{{ID: "customer"}}}
	var saved []*model.CustomerRFM
	svc := NewCustomerRFMServiceWithRepos(&stubRFMRepo{CustomerRFMSaver: func(_ context.Context, row *model.CustomerRFM) error { saved = append(saved, row); return nil }}, customers, newStubOrderRepo([]*model.Order{{ID: "order", Price: "100", CreateTime: now.Add(-48 * time.Hour).Unix()}}, nil), nil)
	svc.nowFunc = func() time.Time { return now }
	svc.rfmRuleRepo = ruleRepo
	for _, tc := range []struct {
		ctx  context.Context
		want int
	}{{ctxA, 23}, {ctxB, 100}} {
		saved = nil
		n, err := svc.ComputeAll(tc.ctx, 0)
		if err != nil || n != 1 || len(saved) != 1 {
			t.Fatalf("重算失败 n=%d saved=%d err=%v", n, len(saved), err)
		}
		if saved[0].CompositeScore != tc.want {
			t.Fatalf("租户规则未生效，综合分=%d，期望%d", saved[0].CompositeScore, tc.want)
		}
	}
	ruleA.FCount5 = 1
	ruleA.MAmount5 = 1
	if err := ruleRepo.Update(ctxA, ruleA); err != nil {
		t.Fatal(err)
	}
	saved = nil
	if _, err := svc.ComputeAll(ctxA, 0); err != nil {
		t.Fatal(err)
	}
	if saved[0].CompositeScore != 93 {
		t.Fatalf("规则更新后未重算: %d", saved[0].CompositeScore)
	}
	queries := 0
	if err := database.Callback().Query().Before("gorm:query").Register("rfm_rule_count", func(tx *gorm.DB) {
		if tx.Statement.Table == "rfm_rules" {
			queries++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove("rfm_rule_count") })
	customers.items = make([]*model.Customer, 501)
	for i := range customers.items {
		customers.items[i] = &model.Customer{ID: "customer"}
	}
	if n, err := svc.ComputeAll(ctxA, 0); err != nil || n != 501 {
		t.Fatalf("分页重算 n=%d err=%v", n, err)
	}
	if queries != 1 {
		t.Fatalf("一轮规则读取%d次，期望1", queries)
	}
	readErr := errors.New("合成规则查询故障")
	if err := database.Callback().Query().Before("gorm:query").Register("rfm_rule_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "rfm_rules" {
			_ = tx.AddError(readErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove("rfm_rule_fail") })
	saved = nil
	if n, err := svc.ComputeAll(ctxA, 0); !errors.Is(err, readErr) || n != 0 || len(saved) != 0 {
		t.Fatalf("读规则失败必须零写入 n=%d saved=%d err=%v", n, len(saved), err)
	}
}
