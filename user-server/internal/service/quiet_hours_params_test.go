package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// newQuietHoursParamFixture 起一个只带 config_params 的库，seed 后把全局参数服务换成
// 指向它的实例（与 webhook_runtime_params_test.go 的 fixture 同源）。
func newQuietHoursParamFixture(t *testing.T) *ConfigParamService {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no test DB")
	}
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := NewConfigParamService()
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc
}

// TestQuietHoursDefaultsMatchCodeConsts 参数未被改时，读取口必须返回代码默认
// （reach 22-8 / webhook 23-7），行为零变更。
func TestQuietHoursDefaultsMatchCodeConsts(t *testing.T) {
	newQuietHoursParamFixture(t)
	ctx := context.Background()
	if got := reachQuietHoursStart(ctx); got != 22 {
		t.Errorf("reachQuietHoursStart 默认 = %d, 期望 22", got)
	}
	if got := reachQuietHoursEnd(ctx); got != 8 {
		t.Errorf("reachQuietHoursEnd 默认 = %d, 期望 8", got)
	}
	if got := aiReplyQuietHoursStart(ctx); got != 23 {
		t.Errorf("aiReplyQuietHoursStart 默认 = %d, 期望 23", got)
	}
	if got := aiReplyQuietHoursEnd(ctx); got != 7 {
		t.Errorf("aiReplyQuietHoursEnd 默认 = %d, 期望 7", got)
	}
}

// newQuietHoursParamFixtureRaw 与 newQuietHoursParamFixture 同，但额外返回底层 *gorm.DB，
// 供「绕过 service 守卫直接写坏值」的用例使用。
func newQuietHoursParamFixtureRaw(t *testing.T) (*ConfigParamService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no test DB")
	}
	if err := SeedConfigParams(context.Background()); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := NewConfigParamService()
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc, database
}

// writeQuietRaw 绕过 service 的 min/max 守卫直接写库，构造「库里已有越界值」。
func writeQuietRaw(t *testing.T, db *gorm.DB, group, key, val string) {
	t.Helper()
	if err := repository.NewConfigParamRepository(db).UpdateValue(context.Background(),
		group, key, val, 1); err != nil {
		t.Fatalf("直接写库 %s.%s=%s 失败：%v", group, key, val, err)
	}
}

// TestQuietHoursReadersPickUpParams 改库之后读取口必须返回新值（两条管道各改各的，
// 不互相串）。
func TestQuietHoursReadersPickUpParams(t *testing.T) {
	svc := newQuietHoursParamFixture(t)
	ctx := context.Background()
	if err := svc.UpdateValue(ctx, "reach", "quiet_hours_start", "21", 1); err != nil {
		t.Fatalf("UpdateValue(reach.quiet_hours_start) 失败：%v", err)
	}
	if err := svc.UpdateValue(ctx, "webhook", "ai_quiet_hours_end", "6", 1); err != nil {
		t.Fatalf("UpdateValue(webhook.ai_quiet_hours_end) 失败：%v", err)
	}
	if got := reachQuietHoursStart(ctx); got != 21 {
		t.Errorf("reachQuietHoursStart = %d, 期望 21（改库后应生效）", got)
	}
	// reach.quiet_hours_end 未改，仍是默认 8 —— 两条键独立。
	if got := reachQuietHoursEnd(ctx); got != 8 {
		t.Errorf("reachQuietHoursEnd = %d, 期望 8（未改的键保持默认）", got)
	}
	if got := aiReplyQuietHoursEnd(ctx); got != 6 {
		t.Errorf("aiReplyQuietHoursEnd = %d, 期望 6（改库后应生效）", got)
	}
	// webhook.ai_quiet_hours_start 未改，仍是默认 23。
	if got := aiReplyQuietHoursStart(ctx); got != 23 {
		t.Errorf("aiReplyQuietHoursStart = %d, 期望 23（未改的键保持默认）", got)
	}
}

// TestQuietHoursOutOfRangeFallsBack 绕过 service 守卫直接写库的越界值（<0 或 >23），
// 读取口必须回落代码默认而不是把坏值放出去。
func TestQuietHoursOutOfRangeFallsBack(t *testing.T) {
	_, db := newQuietHoursParamFixtureRaw(t)
	ctx := context.Background()
	writeQuietRaw(t, db, "reach", "quiet_hours_start", "30") // >23
	writeQuietRaw(t, db, "reach", "quiet_hours_end", "-1")   // <0
	writeQuietRaw(t, db, "webhook", "ai_quiet_hours_start", "99")
	writeQuietRaw(t, db, "webhook", "ai_quiet_hours_end", "-5")

	if got := reachQuietHoursStart(ctx); got != 22 {
		t.Errorf("reachQuietHoursStart 越界应回落 22, got %d", got)
	}
	if got := reachQuietHoursEnd(ctx); got != 8 {
		t.Errorf("reachQuietHoursEnd 越界应回落 8, got %d", got)
	}
	if got := aiReplyQuietHoursStart(ctx); got != 23 {
		t.Errorf("aiReplyQuietHoursStart 越界应回落 23, got %d", got)
	}
	if got := aiReplyQuietHoursEnd(ctx); got != 7 {
		t.Errorf("aiReplyQuietHoursEnd 越界应回落 7, got %d", got)
	}
}

// TestInQuietHoursWindow 跨午夜与当日窗口的判定（形态不变，参数化不动它）。
func TestInQuietHoursWindow(t *testing.T) {
	cst, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	mk := func(h int) time.Time { return time.Date(2026, 1, 1, h, 0, 0, 0, cst) }
	// 22-8 跨午夜：23 点在窗内，10 点不在。
	if !inQuietHoursWindow(mk(23), 22, 8, cst) {
		t.Error("23 点应落在 22-8 窗口内")
	}
	if inQuietHoursWindow(mk(10), 22, 8, cst) {
		t.Error("10 点不应落在 22-8 窗口内")
	}
	// 9-18 当日窗口。
	if !inQuietHoursWindow(mk(12), 9, 18, cst) {
		t.Error("12 点应落在 9-18 窗口内")
	}
	if inQuietHoursWindow(mk(20), 9, 18, cst) {
		t.Error("20 点不应落在 9-18 窗口内")
	}
}
