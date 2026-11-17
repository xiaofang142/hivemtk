package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/tracing"
)

func TestTraceObservationScope_AllQueriesRequireConfirmedTenant(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	repo := NewTraceObservationRepository(database)
	started := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	for _, tenantID := range []uint{tenant.DefaultTenantID, 18} {
		completed := started.Add(time.Duration(tenantID) * time.Millisecond)
		row := model.MessageTrace{
			TenantID: tenantID, TraceID: "shared-scope-trace", TurnID: "shared-scope-turn",
			Channel: "douyin", AccountID: "shared-account", ConversationID: "shared-conversation",
			MsgID: "shared-inbound", Direction: "inbound", Node: tracing.NodeIngest,
			Status: tracing.StatusOk, DurationMs: int64(tenantID), DurationMeasured: true,
			TimingVersion: 1, StartedAt: &started, CompletedAt: &completed,
		}
		if err := database.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 所有入口都使用真实数据；统计入口也核对范围而不是只检查错误。
	queries := []struct {
		name string
		run  func(context.Context, uint) error
	}{
		{name: "节点测量", run: func(ctx context.Context, expected uint) error {
			rows, err := repo.NodeMeasurements(ctx, started.Add(-time.Second))
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].Total != 1 || rows[0].MeasuredCount != 1 || rows[0].AvgMs == nil || *rows[0].AvgMs != int64(expected) {
				return fmt.Errorf("节点测量读到了错误租户或跨租户聚合")
			}
			return nil
		}},
		{name: "生命周期测量", run: func(ctx context.Context, _ uint) error {
			rows, err := repo.LifecycleMeasurements(ctx, started.Add(-time.Second))
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].TotalTurns != 1 || len(rows[0].Samples) != 0 {
				return fmt.Errorf("生命周期测量不能拼接其它租户或猜测未回执耗时")
			}
			return nil
		}},
		{name: "生命周期行", run: func(ctx context.Context, expected uint) error {
			rows, err := repo.LifecycleRows(ctx, "shared-conversation", "shared-scope-trace", 10)
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].TenantID != expected {
				return fmt.Errorf("生命周期行租户不符")
			}
			return nil
		}},
		{name: "轮次树", run: func(ctx context.Context, expected uint) error {
			rows, err := repo.TraceTreeRows(ctx, "shared-scope-trace", "shared-conversation", "shared-inbound")
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].TenantID != expected {
				return fmt.Errorf("定位轮次不能借用其它租户的最近行")
			}
			return nil
		}},
		{name: "轮次摘要", run: func(ctx context.Context, expected uint) error {
			rows, err := repo.Summaries(ctx, 10)
			if err != nil {
				return err
			}
			if len(rows) != 1 || rows[0].NodeCount != 1 || rows[0].LastAt == nil || rows[0].LastAt.Sub(started).Milliseconds() != int64(expected) {
				return fmt.Errorf("轮次摘要租户不符")
			}
			return nil
		}},
	}

	tenant.SetMode(tenant.ModeOn)
	confirmed := tenant.OnlyTenant(context.Background(), 18)
	canceled, cancel := context.WithCancel(confirmed)
	cancel()
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			for _, scope := range []struct {
				name string
				ctx  context.Context
			}{
				{name: "空上下文"},
				{name: "未绑定租户", ctx: context.Background()},
				{name: "已取消", ctx: canceled},
				{name: "系统豁免", ctx: tenant.WithSystemScope(confirmed)},
			} {
				t.Run(scope.name, func(t *testing.T) {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Fatalf("缺少上下文应明确拒绝而不是崩溃：%v", recovered)
						}
					}()
					// 仅仓储返回的错误才代表拒绝，不能把本测试的结果校验错误当授权失败。
					var err error
					switch query.name {
					case "节点测量":
						_, err = repo.NodeMeasurements(scope.ctx, started)
					case "生命周期测量":
						_, err = repo.LifecycleMeasurements(scope.ctx, started)
					case "生命周期行":
						_, err = repo.LifecycleRows(scope.ctx, "shared-conversation", "shared-scope-trace", 10)
					case "轮次树":
						_, err = repo.TraceTreeRows(scope.ctx, "shared-scope-trace", "shared-conversation", "shared-inbound")
					case "轮次摘要":
						_, err = repo.Summaries(scope.ctx, 10)
					}
					if err == nil {
						t.Fatal("未知或系统豁免上下文不得查询租户追踪数据")
					}
				})
			}
			if err := query.run(confirmed, 18); err != nil {
				t.Fatal(err)
			}
		})
	}
	tenant.SetMode(tenant.ModeOff)
	for _, query := range queries {
		t.Run("单租户默认/"+query.name, func(t *testing.T) {
			if err := query.run(context.Background(), tenant.DefaultTenantID); err != nil {
				t.Fatal(err)
			}
			if err := query.run(confirmed, 18); err != nil {
				t.Fatal(err)
			}
		})
	}
}
