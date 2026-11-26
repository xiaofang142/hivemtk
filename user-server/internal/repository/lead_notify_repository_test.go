package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func leadNotifyRepoContext(t *testing.T) context.Context {
	t.Helper()
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	return tenant.WithTenantID(context.Background(), 11)
}

// TestLeadNotifyRepositoryEnqueueIsIdempotentAcrossRebuild 同一幂等键只占一行，重建 repository 后仍可读回。
func TestLeadNotifyRepositoryEnqueueIsIdempotentAcrossRebuild(t *testing.T) {
	ctx := leadNotifyRepoContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	repo := NewLeadNotifyRepository(db)
	now := time.Now().UTC()
	first := &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: "event-clue-1-user-42-in-app", ClueID: "clue-1", EventID: "clue-1",
		Channel: "in_app", Recipient: "42", Status: model.LeadNotifyStatusPending, NextAttemptAt: now,
	}
	created, err := repo.Enqueue(ctx, first)
	if err != nil || !created || first.ID == 0 {
		t.Fatalf("首次入队应成功并分配 ID：created=%v item=%+v err=%v", created, first, err)
	}
	duplicate := &model.LeadNotifyOutbox{TenantID: 11, IdempotencyKey: first.IdempotencyKey}
	created, err = repo.Enqueue(ctx, duplicate)
	if err != nil || created || duplicate.ID != first.ID {
		t.Fatalf("重复幂等键应返回既有意图：created=%v item=%+v err=%v", created, duplicate, err)
	}
	rebuilt := NewLeadNotifyRepository(db)
	got, err := rebuilt.GetOutbox(ctx, 11, first.ID)
	if err != nil || got.IdempotencyKey != first.IdempotencyKey || got.Status != model.LeadNotifyStatusPending {
		t.Fatalf("重建 repository 后投递意图丢失：got=%+v err=%v", got, err)
	}
}

// TestLeadNotifyRepositoryConcurrentClaimHasSingleWinner 两个 worker 抢同一行只能有一个赢家。
func TestLeadNotifyRepositoryConcurrentClaimHasSingleWinner(t *testing.T) {
	ctx := leadNotifyRepoContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	repo := NewLeadNotifyRepository(db)
	now := time.Now().UTC()
	if _, err := repo.Enqueue(ctx, &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: "claim-one", EventID: "claim-event",
		Channel: "in_app", Recipient: "42", Status: model.LeadNotifyStatusPending, NextAttemptAt: now,
	}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}

	type result struct {
		rows []*model.LeadNotifyOutbox
		err  error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := repo.ClaimDueForUpdate(tenant.WithSystemScope(context.Background()), now, 1)
			results <- result{rows: rows, err: err}
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("并发认领失败：%v", r.err)
		}
		winners += len(r.rows)
		for _, row := range r.rows {
			if row.Status != model.LeadNotifyStatusSending || row.ClaimToken == "" || row.LeaseUntil == nil || !row.LeaseUntil.After(now) {
				t.Fatalf("认领行缺少 sending/claim token/有效 lease：%+v", row)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("同一 outbox 行被认领 %d 次，want 1", winners)
	}
}

// TestLeadNotifyRepositoryRetryStaleReclaimAndFence 重试退避、stale receipt_unknown、人工重试与 token 栅栏必须同时成立。
func TestLeadNotifyRepositoryRetryStaleReclaimAndFence(t *testing.T) {
	ctx := leadNotifyRepoContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	repo := NewLeadNotifyRepository(db)
	now := time.Now().UTC()
	item := &model.LeadNotifyOutbox{
		TenantID: 11, IdempotencyKey: "retry-key", EventID: "retry-event", Channel: "in_app",
		Recipient: "42", Status: model.LeadNotifyStatusPending, NextAttemptAt: now, MaxAttempts: 3,
	}
	if _, err := repo.Enqueue(ctx, item); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	rows, err := repo.ClaimDueForUpdate(tenant.WithSystemScope(context.Background()), now, 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("认领失败：rows=%d err=%v", len(rows), err)
	}
	if err := repo.MarkDelivered(tenant.WithSystemScope(context.Background()), rows[0].ID, "wrong-token", now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("错误 claim token 不得推进终态，实际 err=%v", err)
	}
	next := now.Add(time.Minute)
	if err := repo.ScheduleRetry(tenant.WithSystemScope(context.Background()), rows[0].ID, rows[0].ClaimToken, next, "temporary failure"); err != nil {
		t.Fatalf("重试改排失败：%v", err)
	}
	got, err := repo.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusRetry || got.Attempts != 1 || got.NextAttemptAt.Before(next) {
		t.Fatalf("重试状态不正确：got=%+v err=%v", got, err)
	}

	if err := db.WithContext(tenant.WithSystemScope(ctx)).Model(&model.LeadNotifyOutbox{}).
		Where("id = ?", item.ID).Updates(map[string]any{
		"status": model.LeadNotifyStatusSending,
		"lease_until": now.Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("预置 stale sending 失败：%v", err)
	}
	n, err := repo.ReclaimStaleSending(tenant.WithSystemScope(context.Background()), now.Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("stale sending 回收失败：n=%d err=%v", n, err)
	}
	got, err = repo.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusReceiptUnknown {
		t.Fatalf("stale sending 应转 receipt_unknown 防自动重发，实际 %+v err=%v", got, err)
	}
	if err := repo.RetryManually(ctx, item.ID); err != nil {
		t.Fatalf("人工重试失败：%v", err)
	}
	got, err = repo.GetOutbox(ctx, 11, item.ID)
	if err != nil || got.Status != model.LeadNotifyStatusPending || got.Attempts != 0 {
		t.Fatalf("人工重试应重置 pending/attempts，实际 %+v err=%v", got, err)
	}
}

// TestLeadNotifyRepositoryDeliveryHistorySurvivesRouteDeleteAndTenantScope 独立历史不随路由删除，且跨租户不可读。
func TestLeadNotifyRepositoryDeliveryHistorySurvivesRouteDeleteAndTenantScope(t *testing.T) {
	ctx := leadNotifyRepoContext(t)
	db := testutil.NewTestDB(t, &model.LeadMiningNotifyRoute{}, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	repo := NewLeadNotifyRepository(db)
	route := &model.LeadMiningNotifyRoute{TenantID: 11, RouteID: "route-to-delete", Scope: "tenant"}
	if err := db.WithContext(ctx).Create(route).Error; err != nil {
		t.Fatalf("创建路由失败：%v", err)
	}
	record := &model.LeadNotifyDelivery{
		TenantID: 11, DeliveryKey: "attempt:1:1", ClueID: "clue-history", EventID: "clue-history",
		Channel: "in_app", Recipient: "42", RouteID: route.RouteID, Success: true, Status: model.LeadNotifyStatusDelivered, Attempt: 1,
	}
	if err := repo.RecordDelivery(ctx, record); err != nil {
		t.Fatalf("写独立历史失败：%v", err)
	}
	if err := db.WithContext(ctx).Delete(&model.LeadMiningNotifyRoute{}, route.ID).Error; err != nil {
		t.Fatalf("删除路由失败：%v", err)
	}
	rows, err := repo.ListDeliveries(ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-history", Limit: 20})
	if err != nil || len(rows) != 1 || rows[0].RouteID != route.RouteID {
		t.Fatalf("删路由后历史应仍可查：rows=%+v err=%v", rows, err)
	}
	other := tenant.WithTenantID(context.Background(), 22)
	rows, err = repo.ListDeliveries(other, model.LeadNotifyDeliveryQuery{ClueID: "clue-history", Limit: 20})
	if err != nil || len(rows) != 0 {
		t.Fatalf("跨租户不得读到独立历史：rows=%+v err=%v", rows, err)
	}
}

// TestLeadNotifyRepositorySummaryCountsPartialSuccess 汇总必须区分成功、失败与待重试，不能把空结果当成功。
func TestLeadNotifyRepositorySummaryCountsPartialSuccess(t *testing.T) {
	ctx := leadNotifyRepoContext(t)
	db := testutil.NewTestDB(t, &model.LeadNotifyOutbox{}, &model.LeadNotifyDelivery{})
	repo := NewLeadNotifyRepository(db)
	for i, status := range []string{model.LeadNotifyStatusDelivered, "failed", model.LeadNotifyStatusRetry} {
		if err := repo.RecordDelivery(ctx, &model.LeadNotifyDelivery{
			TenantID: 11, DeliveryKey: fmt.Sprintf("summary-%d", i), ClueID: "clue-summary", EventID: "clue-summary",
			Channel: "in_app", Recipient: fmt.Sprintf("%d", i+1), Status: status, Success: status == model.LeadNotifyStatusDelivered, Attempt: 1,
		}); err != nil {
			t.Fatalf("写历史失败：%v", err)
		}
	}
	rows, err := repo.SummarizeDeliveries(ctx, model.LeadNotifyDeliveryQuery{ClueID: "clue-summary"})
	if err != nil || len(rows) != 3 {
		t.Fatalf("汇总行数不正确：rows=%+v err=%v", rows, err)
	}
	total := 0
	succeeded := 0
	for _, row := range rows {
		total += row.Total
		succeeded += row.Succeeded
	}
	if total != 3 || succeeded != 1 {
		t.Fatalf("部分成功汇总不正确：total=%d succeeded=%d rows=%+v", total, succeeded, rows)
	}
}
