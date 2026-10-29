package sop

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestSOPScheduler_Tick_FansOutPerTenant 钉住 T020：on 模式下 tick 的三个子调用
// （cleanupStuckExecutions / dispatchAutoSOPs / dispatchScheduledSOPs）必须经
// tenant.Fanout 逐租户执行，而不是拿一个无租户 ctx 直接跑。
//
// 为什么这道判据必要：sop_agents / sop_executions 都是租户表（模型首列 TenantID），
// on 模式下 tenancyidx 隔离 callback 对**无租户 ctx** 的查询 fail-closed 拒绝。
// tick 原实现用 context.Background() 派生的无租户 ctx 直接调三个子方法 ⇒ 生产上
// （TENANT_MODE=on）每轮扫描都会被 fail-closed 吞掉（当前单租户数据未见报错日志，
// 但一开多租户就结构性必炸）。修法是 Fanout（逐活跃租户注入 ctx），**不是** system
// scope —— tryExecute→svc.Execute 是跨模块执行路径，system scope 会把执行写进错误租户。
func TestSOPScheduler_Tick_FansOutPerTenant(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	tenant.SetActiveTenantLoader(func(ctx context.Context) ([]uint, error) {
		return []uint{11}, nil
	})
	t.Cleanup(func() {
		// activeTenantLoader 是进程级 atomic.Value，用例间必须复位（见 fanout.go 注释）。
		tenant.SetActiveTenantLoader(nil)
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})

	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{})

	// 以租户 11 身份 seed 一个 auto SOP（静态名单通道，无需圈选确认）。
	ctx11 := tenant.WithTenantID(context.Background(), 11)
	agent := &model.SOPAgent{
		Name:          "fanout",
		Scenario:      "fanout",
		TriggerType:   SOPTriggerAuto,
		TriggerConfig: model.JSONMap{"customer_ids": []any{"c-1"}},
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     1,
	}
	if err := db.WithContext(ctx11).Create(agent).Error; err != nil {
		t.Fatalf("以租户 11 seed agent 失败: %v", err)
	}

	s := NewSOPScheduler(NewSOPService(nil), time.Hour)
	// 关键：tick 收到的是**无租户** ctx；Fanout 必须把它扩成租户 11 的 ctx，
	// 否则 dispatchAutoSOPs 里的 ListActiveByTriggerType 会被 fail-closed 吞掉。
	s.tick(context.Background())

	// 断言：租户 11 名下的 agent 被列出并开工了一条执行（即查询未被 fail-closed）。
	var execs []model.SOPExecution
	if err := db.WithContext(ctx11).
		Where("sop_id = ?", agent.ID).
		Find(&execs).Error; err != nil {
		t.Fatalf("读执行记录失败（若为 [tenant] fail-closed 错误即 Fanout 未生效）: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("on 模式 tick 应经 Fanout 开工 1 条执行（租户 11），实际 %d 条", len(execs))
	}
	if execs[0].CustomerID != "c-1" {
		t.Errorf("执行客户应为 c-1，实际 %s", execs[0].CustomerID)
	}
}

// TestSOPScheduler_Tick_OffModeFanoutDefault 钉住 off 模式退化：Fanout 单默认租户，
// tick 行为与历史版本等价（不注入、不 fail-closed、数据归默认租户）。
func TestSOPScheduler_Tick_OffModeFanoutDefault(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SOPAgent{}, &model.SOPExecution{})

	agent := &model.SOPAgent{
		Name:          "off-fanout",
		Scenario:      "off-fanout",
		TriggerType:   SOPTriggerAuto,
		TriggerConfig: model.JSONMap{"customer_ids": []any{"c-1"}},
		SOPGraph:      model.JSONMap{"nodes": []any{map[string]any{"id": "start", "type": "start"}}},
		IsActive:      true,
		CreatedBy:     1,
	}
	if err := db.Create(agent).Error; err != nil {
		t.Fatalf("seed agent 失败: %v", err)
	}

	s := NewSOPScheduler(NewSOPService(nil), time.Hour)
	s.tick(context.Background())

	var execs []model.SOPExecution
	if err := db.Where("sop_id = ?", agent.ID).Find(&execs).Error; err != nil {
		t.Fatalf("读执行记录失败: %v", err)
	}
	if len(execs) != 1 {
		t.Fatalf("off 模式 tick 应开工 1 条执行，实际 %d 条", len(execs))
	}
	if execs[0].TenantID != tenant.DefaultTenantID {
		t.Errorf("off 模式执行应归默认租户 %d，实际 %d", tenant.DefaultTenantID, execs[0].TenantID)
	}
}
