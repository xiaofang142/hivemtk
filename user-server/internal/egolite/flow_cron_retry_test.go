package egolite

// flow_cron_retry_test.go —— S-23（specs/016 余量）：定时触发「单次触发、不重投」。
//
// 钉的是口径而不是实现细节：失败只在这一次触发上记账 + 记日志，**绝不**在同一次
// 触发里再调一次 fire；补投（错过的点是否补、补几次）属另一套语义，未立 spec 前不做。

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

type fireRecord struct {
	calls   []uint
	userIDs []uint
}

type triggerEnv struct {
	svc      *EgoFlowCronService
	cronRepo EgoFlowCronRepository
	flowRepo EgoFlowRepository
	rec      *fireRecord
	db       *gorm.DB
}

func newTriggerEnv(t *testing.T, fireErr error) *triggerEnv {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteFlow{}, &model.EgoliteFlowCron{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	flowRepo := NewEgoFlowRepositoryWithDB(db)
	cronRepo := NewEgoFlowCronRepositoryWithDB(db)
	rec := &fireRecord{}
	svc := NewEgoFlowCronService(cronRepo, flowRepo, func(_ context.Context, userID, flowID uint) error {
		rec.calls = append(rec.calls, flowID)
		rec.userIDs = append(rec.userIDs, userID)
		return fireErr
	})
	return &triggerEnv{svc: svc, cronRepo: cronRepo, flowRepo: flowRepo, rec: rec, db: db}
}

// mount 建流程 + 挂触发器，并摘掉调度条目（用例只测触发体，不让后台调度插一脚）
func (e *triggerEnv) mount(t *testing.T, userID uint) uint {
	t.Helper()
	ctx := context.Background()
	f := &model.EgoliteFlow{
		UserID: userID, Name: "cron-flow", Status: model.EgoFlowDraft,
		Script: "console.log(1)", ScanPassed: true,
	}
	if err := e.flowRepo.Create(ctx, f); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Upsert(ctx, userID, f.ID, UpsertCronRequest{CronExpr: "0 9 * * *"}); err != nil {
		t.Fatalf("挂载失败: %v", err)
	}
	e.svc.unregister(f.ID)
	return f.ID
}

// 1. fire 失败：只叫一次、不重投，且 next_run_at 照常推进（调度不因失败停摆）
func TestFlowCronFireFailureDoesNotRetry(t *testing.T) {
	env := newTriggerEnv(t, errors.New("在途执行已达上限"))
	flowID := env.mount(t, 7)
	ctx := context.Background()
	before, err := env.cronRepo.GetByFlow(ctx, flowID)
	if err != nil {
		t.Fatal(err)
	}

	env.svc.triggerOnce(flowID)

	if len(env.rec.calls) != 1 {
		t.Fatalf("fire 调用次数=%d want=1（失败不重投）", len(env.rec.calls))
	}
	if env.rec.calls[0] != flowID || env.rec.userIDs[0] != 7 {
		t.Fatalf("触发归属错误: flow=%v user=%v", env.rec.calls, env.rec.userIDs)
	}
	after, err := env.cronRepo.GetByFlow(ctx, flowID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastRunAt == nil {
		t.Fatalf("失败也要记 last_run_at（否则监控面看不出「触发过但跳过」）")
	}
	// 同一分钟内 upsert 与本次触发算出的下一次可能相同（当天剩余时刻相同），
	// 所以判据是「next 不倒退且等于按 last_run_at 重算的结果」，不是严格递增。
	if after.NextRunAt == nil || after.NextRunAt.Before(*before.NextRunAt) {
		t.Fatalf("next_run_at 不该倒退: before=%v after=%v", before.NextRunAt, after.NextRunAt)
	}
	want := nextRunAt(after.CronExpr, after.TimeZone, *after.LastRunAt)
	if want == nil || !want.Equal(*after.NextRunAt) {
		t.Fatalf("next_run_at 应为「上次触发之后的下一次」: want=%v got=%v", want, after.NextRunAt)
	}
}

// 2. fire 成功：同样只叫一次（别把"成功"也做成重试的由头）
func TestFlowCronFireSuccessCallsOnce(t *testing.T) {
	env := newTriggerEnv(t, nil)
	flowID := env.mount(t, 7)
	env.svc.triggerOnce(flowID)
	if len(env.rec.calls) != 1 {
		t.Fatalf("fire 调用次数=%d want=1", len(env.rec.calls))
	}
}

// 3. 触发器被停用/删除：触发体自己复查，不 fire（调度条目与库状态之间的竞态窗口）
func TestFlowCronFireSkipsWhenDisabledOrRemoved(t *testing.T) {
	env := newTriggerEnv(t, nil)
	flowID := env.mount(t, 7)
	ctx := context.Background()

	cur, err := env.cronRepo.GetByFlow(ctx, flowID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := *cur
	disabled.Enabled = false
	if _, err := env.cronRepo.UpsertByFlow(ctx, &disabled); err != nil {
		t.Fatal(err)
	}
	env.svc.triggerOnce(flowID)
	if len(env.rec.calls) != 0 {
		t.Fatalf("停用后不该 fire，got %d 次", len(env.rec.calls))
	}

	// 行被删除：触发体不 panic、不 fire
	if err := env.cronRepo.DeleteByFlow(ctx, flowID, 7); err != nil {
		t.Fatal(err)
	}
	env.svc.triggerOnce(flowID)
	if len(env.rec.calls) != 0 {
		t.Fatalf("行已删除后不该 fire，got %d 次", len(env.rec.calls))
	}
}

// 4. 未注入执行出口（装配缺失）：记日志跳过，不 panic、不 fire
func TestFlowCronFireWithoutEgressIsSkipOnly(t *testing.T) {
	env := newTriggerEnv(t, nil)
	flowID := env.mount(t, 8)
	env.svc.fire = nil
	ctx := context.Background()

	env.svc.triggerOnce(flowID) // 不 panic 即通过

	tr, err := env.cronRepo.GetByFlow(ctx, flowID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.LastRunAt == nil {
		t.Fatalf("装配缺失也要记一次触发时间")
	}
	if tr.NextRunAt == nil || !tr.NextRunAt.After(time.Now().Add(-time.Minute)) {
		t.Fatalf("next_run_at 应继续推进: %v", tr.NextRunAt)
	}
}
