package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	_type "hivemtk-user/internal/pkg/utils/type"
	"hivemtk-user/internal/repository"
)

func TestRFMScoreRecency(t *testing.T) {
	buckets := []int{7, 30, 90, 180}
	cases := []struct {
		days int
		want int
	}{
		{1, 5},
		{7, 5},
		{8, 4},
		{30, 4},
		{31, 3},
		{90, 3},
		{91, 2},
		{180, 2},
		{181, 1},
		{9999, 1},
	}
	for _, c := range cases {
		if got := rfmScoreRecency(c.days, buckets); got != c.want {
			t.Errorf("days=%d expected %d, got %d", c.days, c.want, got)
		}
	}
}

func TestRFMScoreFrequency(t *testing.T) {
	buckets := []int{10, 5, 3, 1}
	cases := []struct {
		freq int
		want int
	}{
		{15, 5},
		{10, 5},
		{9, 4},
		{5, 4},
		{4, 3},
		{3, 3},
		{2, 2},
		{1, 2},
		{0, 1},
	}
	for _, c := range cases {
		if got := rfmScoreFrequency(c.freq, buckets); got != c.want {
			t.Errorf("freq=%d expected %d, got %d", c.freq, c.want, got)
		}
	}
}

func TestRFMScoreMonetary(t *testing.T) {
	buckets := []int64{500000, 100000, 30000, 5000}
	cases := []struct {
		m    int64
		want int
	}{
		{600000, 5},
		{500000, 5},
		{499999, 4},
		{100000, 4},
		{99999, 3},
		{30000, 3},
		{29999, 2},
		{5000, 2},
		{4999, 1},
		{0, 1},
	}
	for _, c := range cases {
		if got := rfmScoreMonetary(c.m, buckets); got != c.want {
			t.Errorf("m=%d expected %d, got %d", c.m, c.want, got)
		}
	}
}

func TestDetermineSegment_Churn(t *testing.T) {
	if got := determineSegment(1, 1, 1, 200, 180); got != model.RFMSegmentChurn {
		t.Errorf("expected churn, got %s", got)
	}
	if got := determineSegment(1, 1, 5, 100, 180); got != model.RFMSegmentChurn {
		t.Errorf("expected churn (R=1 F=1), got %s", got)
	}
}

func TestDetermineSegment_Champion(t *testing.T) {
	if got := determineSegment(5, 4, 5, 1, 180); got != model.RFMSegmentChampion {
		t.Errorf("expected champion, got %s", got)
	}
}

func TestDetermineSegment_Loyal(t *testing.T) {
	if got := determineSegment(4, 3, 3, 30, 180); got != model.RFMSegmentLoyal {
		t.Errorf("expected loyal, got %s", got)
	}
}

func TestDetermineSegment_AtRisk(t *testing.T) {
	if got := determineSegment(2, 2, 2, 60, 180); got != model.RFMSegmentAtRisk {
		t.Errorf("expected at_risk, got %s", got)
	}
}

func TestDetermineSegment_Potential(t *testing.T) {
	if got := determineSegment(3, 2, 2, 60, 180); got != model.RFMSegmentPotential {
		t.Errorf("expected potential, got %s", got)
	}
}

func TestCalcChurnRisk(t *testing.T) {
	cfg := DefaultRFMConfig()
	level, score := calcChurnRisk(200, 0, 0, cfg)
	if level != "high" || score < 70 {
		t.Errorf("expected high/score>=70, got %s/%d", level, score)
	}
	level, score = calcChurnRisk(0, 10, 200000, cfg)
	if level != "low" {
		t.Errorf("expected low, got %s/%d", level, score)
	}
	level, score = calcChurnRisk(120, 1, 0, cfg)
	if level != "medium" {
		t.Errorf("expected medium, got %s/%d", level, score)
	}
	level, _ = calcChurnRisk(0, 10, 200000, cfg)
	if level != "low" {
		t.Errorf("expected low, got %s", level)
	}
}

type stubRFMRepo struct {
	CustomerRFMSaver func(ctx context.Context, rfm *model.CustomerRFM) error
}

func (s *stubRFMRepo) Upsert(ctx context.Context, rfm *model.CustomerRFM) error {
	if s.CustomerRFMSaver != nil {
		return s.CustomerRFMSaver(ctx, rfm)
	}
	return nil
}
func (s *stubRFMRepo) GetByCustomerID(ctx context.Context, id string) (*model.CustomerRFM, error) {
	return nil, nil
}
func (s *stubRFMRepo) ListBySegment(ctx context.Context, seg string, p, ps int) ([]*model.CustomerRFM, int64, error) {
	return nil, 0, nil
}
func (s *stubRFMRepo) ListChurnCandidates(ctx context.Context, th, lim int) ([]*model.CustomerRFM, error) {
	return nil, nil
}
func (s *stubRFMRepo) CountBySegment(ctx context.Context) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (s *stubRFMRepo) DeleteByCustomerID(ctx context.Context, id string) error { return nil }

type stubOrderRepo struct {
	byAcct  []*model.Order
	byTgID  []*model.Order
	gotTgID int64
}

func newStubOrderRepo(byAcct, byTgID []*model.Order) *stubOrderRepo {
	return &stubOrderRepo{byAcct: byAcct, byTgID: byTgID}
}
func (s *stubOrderRepo) Create(ctx context.Context, o *model.Order) error { return nil }
func (s *stubOrderRepo) GetByTgID(ctx context.Context, tgID int64) ([]*model.Order, error) {
	s.gotTgID = tgID
	return s.byTgID, nil
}
func (s *stubOrderRepo) ListByAccountIDs(ctx context.Context, ids []string) ([]*model.Order, error) {
	return s.byAcct, nil
}
func (s *stubOrderRepo) GetByID(ctx context.Context, id uint) (*model.Order, error) { return nil, nil }
func (s *stubOrderRepo) GetByStringID(ctx context.Context, id string) (*model.Order, error) {
	return nil, nil
}
func (s *stubOrderRepo) GetOrderList(ctx context.Context, page, limit int) ([]*model.Order, int64, error) {
	return nil, 0, nil
}
func (s *stubOrderRepo) Delete(ctx context.Context, id string) error { return nil }
func (s *stubOrderRepo) GetGetLastOrder(ctx context.Context, account_ID string, tgID int64) (*model.Order, error) {
	return nil, nil
}
func (s *stubOrderRepo) UpdateOrderStatusById(ctx context.Context, id string, st _type.OrderStatusType) error {
	return nil
}
func (s *stubOrderRepo) GetRecentOrderList(ctx context.Context) ([]*model.Order, error) {
	return nil, nil
}
func (s *stubOrderRepo) GetDistinctPaidTgIDs(ctx context.Context) ([]int64, error) { return nil, nil }
func (s *stubOrderRepo) Update(ctx context.Context, order *model.Order) error      { return nil }

// TestComputeRawMetrics_PhoneNotTreatedAsTgID 回归：手机号不得被 ParseInt 当 TgID 查订单。
// 历史缺陷：accountID=cust.Phone → ParseInt("138...")>0 → GetByTgID(13800138000)
// → 恒查不到订单 → recency=9999，全量手机客户被误判流失。
func TestComputeRawMetrics_PhoneNotTreatedAsTgID(t *testing.T) {
	orders := []*model.Order{
		{AccountID: "cust-rfm-1", Price: "199", CreateTime: time.Now().Add(-24 * time.Hour).Unix()},
	}
	orderStub := newStubOrderRepo(orders, nil)
	svc := &CustomerRFMService{
		rfmRepo:      &stubRFMRepo{},
		customerRepo: repository.NewCustomerRepository(),
		orderRepo:    orderStub,
		recoveryRepo: nil,
		nowFunc:      time.Now,
	}
	cust := &model.Customer{
		ID:             "cust-rfm-1",
		Phone:          "13800138000",
		TelegramChatID: 0,
	}
	r, f, m, lastActive, err := svc.computeRawMetricsForCustomer(context.Background(), cust)
	if err != nil {
		t.Fatalf("computeRawMetricsForCustomer 失败: %v", err)
	}
	if orderStub.gotTgID != 0 {
		t.Errorf("无 TelegramChatID 的客户不应触发 TgID 订单查询, got tg_id=%d", orderStub.gotTgID)
	}
	if f != 1 || m != 199 {
		t.Errorf("应按 account_id 命中订单: frequency=%d monetary=%d", f, m)
	}
	if r != 1 || lastActive == nil {
		t.Errorf("recency 应为 1 天而非 9999, got %d", r)
	}
}

// TestComputeRawMetrics_UsesRealTelegramChatID 客户真实持有 TelegramChatID 时才走 TG 查询并合并去重
func TestComputeRawMetrics_UsesRealTelegramChatID(t *testing.T) {
	tgOrder := &model.Order{ID: "o-tg-1", AccountID: "cust-rfm-2", Price: "50", CreateTime: time.Now().Unix()}
	acctOrder := &model.Order{ID: "o-acct-1", AccountID: "cust-rfm-2", Price: "100"}
	orderStub := newStubOrderRepo([]*model.Order{acctOrder}, []*model.Order{tgOrder})
	svc := &CustomerRFMService{
		rfmRepo:      &stubRFMRepo{},
		customerRepo: repository.NewCustomerRepository(),
		orderRepo:    orderStub,
		nowFunc:      time.Now,
	}
	cust := &model.Customer{ID: "cust-rfm-2", Phone: "", TelegramChatID: 777888}
	_, f, m, _, err := svc.computeRawMetricsForCustomer(context.Background(), cust)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if orderStub.gotTgID != 777888 {
		t.Errorf("应按真实 TelegramChatID 查询, got %d", orderStub.gotTgID)
	}
	if f != 2 || m != 150 {
		t.Errorf("TG订单与账户订单应合并去重: f=%d m=%d", f, m)
	}
}
