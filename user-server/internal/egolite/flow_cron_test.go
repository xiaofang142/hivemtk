package egolite

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// 流程定时触发器（specs/016 S10）的服务端语义：
//  ① 只挂纯读流程（writes 非空拒绝——决策点 6，无人值守写会被 confirm 闸判停）；
//  ② enabled 显式 false 不被 GORM 零值陷阱翻转（BrowserCronTrigger 同款坑）；
//  ③ 表达式/时区预检（ValidateCronExpr 向量）与归属/摘除语义。

func newCronEnv(t *testing.T) (*EgoFlowCronService, *EgoFlowService, *[]uint) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteFlow{}, &model.EgoliteFlowCron{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	flowRepo := NewEgoFlowRepositoryWithDB(db)
	cronRepo := NewEgoFlowCronRepositoryWithDB(db)
	fired := &[]uint{}
	cronSvc := NewEgoFlowCronService(cronRepo, flowRepo, func(_ context.Context, _, flowID uint) error {
		*fired = append(*fired, flowID)
		return nil
	})
	// Save 只用 flowRepo（不触 execSvc），nil 即可
	return cronSvc, NewEgoFlowService(flowRepo, nil), fired
}

func TestFlowCronUpsertReadFlowOnly(t *testing.T) {
	ctx := context.Background()
	cronSvc, flowSvc, _ := newCronEnv(t)

	// 纯读流程：可挂载，缺省 enabled=true
	fRead, _, err := flowSvc.Save(ctx, 8, 0, SaveRequest{
		Name: "cron 读流", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := cronSvc.Upsert(ctx, 8, fRead.ID, UpsertCronRequest{CronExpr: "0 9 * * *"})
	if err != nil {
		t.Fatalf("纯读流程应可挂载: %v", err)
	}
	if !tr.Enabled || tr.CronExpr != "0 9 * * *" {
		t.Errorf("缺省应 enabled=true 且表达式原样: %+v", tr)
	}

	// 写流程：拒绝（fail-close，决策点 6）
	fWrite, _, err := flowSvc.Save(ctx, 8, 0, SaveRequest{
		Name: "cron 写流", Domains: []string{"github.com"}, Writes: []string{"comment"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cronSvc.Upsert(ctx, 8, fWrite.ID, UpsertCronRequest{CronExpr: "0 9 * * *"}); err == nil ||
		!strings.Contains(err.Error(), "写动作") {
		t.Errorf("写流程必须被拒绝，err=%v", err)
	}
}

func TestFlowCronExplicitDisabledAndOwnership(t *testing.T) {
	ctx := context.Background()
	cronSvc, flowSvc, _ := newCronEnv(t)
	f, _, err := flowSvc.Save(ctx, 9, 0, SaveRequest{
		Name: "停用流", Domains: []string{"github.com"}, Script: benignScript,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 显式 enabled=false 原样落库（GORM 零值陷阱回归：列默认值不得翻成 true）
	disabled := false
	tr, err := cronSvc.Upsert(ctx, 9, f.ID, UpsertCronRequest{CronExpr: "*/10 * * * *", Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Enabled {
		t.Error("显式 enabled=false 被翻转成 true——GORM 零值陷阱复发")
	}
	// 再 Upsert 启用：同一行更新，不产生第二条触发器
	tr2, err := cronSvc.Upsert(ctx, 9, f.ID, UpsertCronRequest{CronExpr: "*/10 * * * *"})
	if err != nil || !tr2.Enabled || tr2.ID != tr.ID {
		t.Errorf("二次挂载应更新同一行并启用: tr2=%+v err=%v", tr2, err)
	}
	// 归属：别人的查询/摘除都是"未挂载"（同响应防枚举）
	if _, err := cronSvc.Get(ctx, 10, f.ID); err == nil {
		t.Error("他人查询应返回未挂载")
	}
	if err := cronSvc.Delete(ctx, 10, f.ID); err == nil {
		t.Error("他人摘除应失败")
	}
	if err := cronSvc.Delete(ctx, 9, f.ID); err != nil {
		t.Fatalf("归属人摘除应成功: %v", err)
	}
	if _, err := cronSvc.Get(ctx, 9, f.ID); err == nil {
		t.Error("摘除后应返回未挂载")
	}
}

func TestValidateCronExprVectors(t *testing.T) {
	valid := []struct{ expr, tz string }{
		{"*/5 * * * *", ""},
		{"0 9 * * *", "Asia/Shanghai"},
		{"30 8 1 * *", "UTC"},
		{"0 */2 * * 1-5", "America/New_York"},
	}
	for _, tc := range valid {
		if err := ValidateCronExpr(tc.expr, tc.tz); err != nil {
			t.Errorf("ValidateCronExpr(%q,%q) 应通过: %v", tc.expr, tc.tz, err)
		}
	}
	invalid := []struct{ expr, tz, note string }{
		{"99 * * * *", "", "分钟越界"},
		{"* * * *", "", "只有 4 段"},
		{"0 9 * * *", "Mars/Phobos", "非法时区"},
		{"", "", "空表达式"},
	}
	for _, tc := range invalid {
		if err := ValidateCronExpr(tc.expr, tc.tz); err == nil {
			t.Errorf("ValidateCronExpr(%q,%q) 应拒绝（%s）", tc.expr, tc.tz, tc.note)
		}
	}
}
