package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// 生命周期测试显式提供已确认许可；真实模块隔离由 bridge_lead_module_test 覆盖。
func bridgeLeadFixturePermission(context.Context) (bool, error) { return true, nil }

func TestBridgeLeadWorker_PersistsTerminalOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		process BridgeLeadProcessor
		stale   bool
		status  string
	}{
		{name: "成功仅完成工作流", process: func(context.Context, *model.MessageHub) error { return nil }, status: model.BridgeLeadCompleted},
		{name: "错误结果未知不重放", process: func(context.Context, *model.MessageHub) error { return errors.New("测试执行故障") }, status: model.BridgeLeadUnknown},
		{name: "异常结果未知不重放", process: func(context.Context, *model.MessageHub) error { panic("测试异常") }, status: model.BridgeLeadUnknown},
		{name: "积压旧输入不触达", stale: true, process: func(context.Context, *model.MessageHub) error { t.Error("旧输入不能执行矿工"); return nil }, status: model.BridgeLeadSkipped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{})
			ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
			sent := time.Now()
			if tc.stale {
				sent = sent.Add(-10 * time.Minute)
			}
			hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "worker-account", ConversationID: "worker-conversation", MsgID: "worker-inbound", Direction: "inbound", SenderID: "customer-a", Content: "采购咨询", BridgeLeadEligible: true, SentAt: sent}
			if err := database.WithContext(ctx).Create(hub).Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(ctx context.Context, hub *model.MessageHub) error {
				calls++
				id, ok := tenant.FromContext(ctx)
				if !ok || id != hub.TenantID {
					t.Error("执行必须绑定持久消息的租户")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("执行必须有超时预算")
				}
				return tc.process(ctx, hub)
			}, bridgeLeadFixturePermission)
			_, _ = worker.RunTenant(ctx)
			var task model.BridgeLeadTask
			if err := database.WithContext(ctx).First(&task).Error; err != nil {
				t.Fatal(err)
			}
			if task.Status != tc.status || task.CompletedAt == nil {
				t.Fatalf("必须持久化正确终态：%s", task.Status)
			}
			before := calls
			_, _ = worker.RunTenant(ctx)
			if calls != before {
				t.Fatal("终态任务不能自动重放")
			}
		})
	}
}

func TestBridgeLeadWorker_IsBoundedAndStops(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	ctx, cancel := context.WithTimeout(tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID), 5*time.Second)
	defer cancel()
	for _, id := range []string{"worker-first", "worker-second"} {
		hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "worker-account", ConversationID: "worker-conversation", MsgID: id, Direction: "inbound", SenderID: "customer-a", Content: "采购咨询", BridgeLeadEligible: true, SentAt: time.Now()}
		if err := database.WithContext(ctx).Create(hub).Error; err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	released := make(chan struct{})
	done := make(chan struct{})
	worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(ctx context.Context, _ *model.MessageHub) error {
		close(started)
		select {
		case <-released:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, bridgeLeadFixturePermission)
	utils.SafeGo(ctx, "test.bridge-lead-bounded", func(ctx context.Context) { defer close(done); _, _ = worker.RunTenant(ctx) })
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("worker没有执行已持久任务")
	}
	if n, err := worker.RunTenant(ctx); err != nil || n != 0 {
		t.Fatal("同一worker必须只有一个执行槽位")
	}
	close(released)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("worker未释放执行槽位")
	}
	// 已经进入running的取消不恢复pending；后台服务自身另有唯一关闭生命周期。
	lifecycle := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(ctx context.Context, _ *model.MessageHub) error { <-ctx.Done(); return ctx.Err() }, bridgeLeadFixturePermission)
	if err := lifecycle.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Stop(ctx); err != nil {
		t.Fatal("服务必须能明确关闭：", err)
	}
	if err := lifecycle.Start(ctx); err == nil {
		t.Fatal("停止的实例不能再次启动遗留循环")
	}
}

func TestBridgeLeadWorker_ContextAndStoppedBoundaries(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	for _, operation := range []string{"缺执行上下文", "缺停止上下文", "停止后执行"} {
		t.Run(operation, func(t *testing.T) {
			worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(context.Context, *model.MessageHub) error { return nil }, bridgeLeadFixturePermission)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("无效生命周期调用应返回错误，不能panic：%v", r)
				}
			}()
			var err error
			switch operation {
			case "缺执行上下文":
				_, err = worker.RunTenant(nil)
			case "缺停止上下文":
				err = worker.Stop(nil)
			default:
				if err := worker.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				_, err = worker.RunTenant(tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID))
			}
			if err == nil {
				t.Error("无上下文或已停止实例不得接受执行")
			}
		})
	}
}

func TestBridgeLeadWorker_StopDuringExecutionPersistsUnknown(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	ctx, cancel := context.WithTimeout(tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID), 5*time.Second)
	defer cancel()
	hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "worker-account", ConversationID: "worker-conversation", MsgID: "worker-cancel", Direction: "inbound", SenderID: "customer-a", Content: "采购咨询", BridgeLeadEligible: true, SentAt: time.Now()}
	if err := database.WithContext(ctx).Create(hub).Error; err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	worker := NewBridgeLeadWorker(repository.NewBridgeLeadTaskRepository(database), func(ctx context.Context, _ *model.MessageHub) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, bridgeLeadFixturePermission)
	if err := worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Stop(context.Background()) })
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("必须实际进入执行后再验证取消")
	}
	if err := worker.Stop(ctx); err != nil {
		t.Fatal("执行中停止应等待终态记录：", err)
	}
	var task model.BridgeLeadTask
	if err := database.WithContext(ctx).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.BridgeLeadUnknown || task.CompletedAt == nil {
		t.Fatalf("已进入执行的取消必须保留unknown，而非恢复pending：%s", task.Status)
	}
}
