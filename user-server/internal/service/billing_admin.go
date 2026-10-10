package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// billing_admin.go 计费管理服务（SaaS P2 首批，2026-10-11）。
//
// 口径：
//   - 方案可增删改查；删除前置校验"无订阅引用"（有引用走下架 retired）；
//   - 订阅一团队一行：指派/续期都走 Upsert（续期 = EndsAt 顺延，含跨期叠加）；
//   - 账单人工记账：创建=应收(pending)、标记支付=paid、作废=void；
//   - 配额：成员调拨（进团队）时校验方案 MaxUsers——未订阅团队不设限（存量兼容）。
//
// 执法边界：MaxMessagesMonthly 的消息量闸在 P2 后续接 ingest 链路（本期只存储与展示）。

var (
	ErrBillingPlanNotFound  = errors.New("计费方案不存在")
	ErrBillingPlanInUse     = errors.New("计费方案已被订阅引用，请先下架（retired）而非删除")
	ErrBillingQuotaExceeded = errors.New("团队成员数已达订阅方案上限")
)

// BillingAdminService 计费管理。
type BillingAdminService struct {
	plans   *repository.BillingPlanRepo
	subs    *repository.TeamSubscriptionRepo
	records *repository.BillingRecordRepo
	teams   *repository.TeamRepository
}

func NewBillingAdminService() *BillingAdminService {
	p := repository.NewBillingPlanRepo()
	if !p.Available() {
		return &BillingAdminService{}
	}
	return &BillingAdminService{
		plans: p, subs: repository.NewTeamSubscriptionRepo(),
		records: repository.NewBillingRecordRepo(), teams: repository.NewTeamRepository(),
	}
}

func (s *BillingAdminService) Ready() bool { return s.plans != nil }

// ---- 方案 CRUD ----

func (s *BillingAdminService) ListPlans(ctx context.Context) ([]model.BillingPlan, error) {
	return s.plans.List(ctx)
}

func (s *BillingAdminService) CreatePlan(ctx context.Context, p *model.BillingPlan) error {
	if p.Code == "" || p.Name == "" {
		return errors.New("code/name 必填")
	}
	if p.Status == "" {
		p.Status = model.BillingPlanStatusActive
	}
	if existing, _ := s.plans.GetByCode(ctx, p.Code); existing != nil {
		return fmt.Errorf("方案编码 %q 已存在", p.Code)
	}
	return s.plans.Create(ctx, p)
}

func (s *BillingAdminService) UpdatePlan(ctx context.Context, id uint, updates map[string]any) error {
	if _, err := s.plans.GetByID(ctx, id); err != nil {
		return ErrBillingPlanNotFound
	}
	if code, ok := updates["code"]; ok {
		if existing, _ := s.plans.GetByCode(ctx, fmt.Sprint(code)); existing != nil && existing.ID != id {
			return fmt.Errorf("方案编码 %v 已存在", code)
		}
	}
	return s.plans.Update(ctx, id, updates)
}

func (s *BillingAdminService) DeletePlan(ctx context.Context, id uint) error {
	if _, err := s.plans.GetByID(ctx, id); err != nil {
		return ErrBillingPlanNotFound
	}
	cnt, err := s.plans.CountSubscriptions(ctx, id)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return ErrBillingPlanInUse
	}
	return s.plans.Delete(ctx, id)
}

// ---- 订阅 ----

func (s *BillingAdminService) ListSubscriptions(ctx context.Context) ([]model.TeamSubscription, error) {
	return s.subs.List(ctx)
}

func (s *BillingAdminService) GetTeamSubscription(ctx context.Context, teamID uint) (*model.TeamSubscription, error) {
	return s.subs.GetByTeam(ctx, teamID)
}

// AssignPlan 指派/变更订阅（months=覆盖月数，0 按 1 个月算）。
func (s *BillingAdminService) AssignPlan(ctx context.Context, teamID, planID uint, months int, autoRenew bool) (*model.TeamSubscription, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return nil, ErrBillingPlanNotFound
	}
	if _, err := s.teams.GetByID(ctx, teamID); err != nil {
		return nil, errors.New("团队不存在")
	}
	if months <= 0 {
		months = 1
	}
	now := time.Now()
	sub := &model.TeamSubscription{
		TeamID: teamID, PlanID: plan.ID, PlanCode: plan.Code,
		Status:   model.TeamSubStatusActive,
		StartsAt: now, EndsAt: now.AddDate(0, months, 0), AutoRenew: autoRenew,
	}
	// 续期叠加：已有未到期订阅 → 从其 EndsAt 起算
	if existing, err := s.subs.GetByTeam(ctx, teamID); err == nil && existing.EndsAt.After(now) {
		sub.StartsAt = existing.EndsAt
		sub.EndsAt = existing.EndsAt.AddDate(0, months, 0)
	}
	if err := s.subs.Upsert(ctx, sub); err != nil {
		return nil, err
	}
	// teams.plan 展示档位跟随
	_ = s.teams.Update(ctx, teamID, map[string]any{"plan": plan.Code})
	return s.subs.GetByTeam(ctx, teamID)
}

func (s *BillingAdminService) CancelSubscription(ctx context.Context, teamID uint) error {
	sub, err := s.subs.GetByTeam(ctx, teamID)
	if err != nil {
		return errors.New("该团队无订阅")
	}
	return s.subs.Upsert(ctx, &model.TeamSubscription{
		TeamID: teamID, PlanID: sub.PlanID, PlanCode: sub.PlanCode,
		Status: model.TeamSubStatusExpired, StartsAt: sub.StartsAt, EndsAt: sub.EndsAt,
		AutoRenew: false,
	})
}

// ---- 账单 ----

func (s *BillingAdminService) ListRecords(ctx context.Context, teamID uint, status string, page, size int) ([]model.BillingRecord, int64, error) {
	return s.records.List(ctx, teamID, status, page, size)
}

// CreateRecord 应收账单（金额可从方案月价 × 月数推导，也允许手填）。
func (s *BillingAdminService) CreateRecord(ctx context.Context, teamID, planID uint, months int, amountCents int64, remark string) (*model.BillingRecord, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return nil, ErrBillingPlanNotFound
	}
	if months <= 0 {
		months = 1
	}
	if amountCents == 0 {
		amountCents = plan.PriceCents * int64(months)
	}
	row := &model.BillingRecord{
		TeamID: teamID, PlanCode: plan.Code, AmountCents: amountCents,
		Months: months, Status: model.BillingStatusPending, Remark: remark,
	}
	if err := s.records.Create(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *BillingAdminService) MarkRecordPaid(ctx context.Context, id uint) error {
	return s.records.MarkPaid(ctx, id, time.Now())
}

func (s *BillingAdminService) VoidRecord(ctx context.Context, id uint) error {
	return s.records.Void(ctx, id)
}

// ---- 配额 ----

// CheckMemberQuota 成员进团队的配额闸（MoveMember 调用）。
// 无订阅 = 不限（存量兼容）；有订阅按方案 MaxUsers（0=不限）。
func (s *BillingAdminService) CheckMemberQuota(ctx context.Context, teamID uint) error {
	sub, err := s.subs.GetByTeam(ctx, teamID)
	if err != nil || sub.Status != model.TeamSubStatusActive {
		return nil // 未订阅：不设限
	}
	plan, err := s.plans.GetByID(ctx, sub.PlanID)
	if err != nil || plan.MaxUsers <= 0 {
		return nil
	}
	cnt, err := s.teams.CountMembers(ctx, teamID)
	if err != nil {
		return nil // 查询故障不阻塞业务（fail-open，配额不是安全边界）
	}
	if int64(cnt) >= int64(plan.MaxUsers) {
		return fmt.Errorf("%w（%s 方案上限 %d 人，当前 %d 人）", ErrBillingQuotaExceeded, plan.Name, plan.MaxUsers, cnt)
	}
	return nil
}
