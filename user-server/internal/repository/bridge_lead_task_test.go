package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
)

func TestBridgeLeadTasks_RecoveryAndExecutionFence(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	repo := NewBridgeLeadTaskRepository(database)
	for i, eligible := range []bool{true, true, false} {
		hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "lead-queue-account", ConversationID: "lead-queue-conv",
			MsgID: string(rune('a' + i)), Direction: "inbound", Status: "received", MsgType: "text", Content: "合成采购消息", BridgeLeadEligible: eligible}
		if err := database.WithContext(ctx).Create(hub).Error; err != nil {
			t.Fatal(err)
		}
	}
	if count, err := repo.Reconcile(ctx, 10); err != nil || count != 2 {
		t.Fatalf("必须从资格恢复两个任务：%d %v", count, err)
	}
	if count, err := repo.Reconcile(ctx, 10); err != nil || count != 0 {
		t.Fatalf("任务补齐必须幂等：%d %v", count, err)
	}
	now := time.Now()
	first, err := repo.Claim(ctx, now, time.Second, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("首次认领失败：%v %v", first, err)
	}
	second, err := repo.Claim(ctx, now, time.Second, 10)
	if err != nil || len(second) != 1 || second[0].ID == first[0].ID {
		t.Fatalf("活租约不得双认领：%v %v", second, err)
	}
	if _, err := repo.Expire(ctx, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := repo.Claim(ctx, now.Add(2*time.Second), time.Second, 10)
	if err != nil || len(reclaimed) != 2 {
		t.Fatalf("未执行租约应恢复：%v %v", reclaimed, err)
	}
	if started, err := repo.Begin(ctx, &first[0], now); err != nil || started {
		t.Fatalf("旧令牌不得执行：%v %v", started, err)
	}
	for _, task := range reclaimed {
		if started, err := repo.Begin(ctx, &task, now.Add(2*time.Second)); err != nil || !started {
			t.Fatalf("新租约必须开始：%v %v", started, err)
		}
	}
	if _, err := repo.Expire(ctx, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.Claim(ctx, now.Add(4*time.Second), time.Second, 10); err != nil || len(rows) != 0 {
		t.Fatalf("结果未知不能重放：%v %v", rows, err)
	}
	if completed, err := repo.Finish(ctx, &reclaimed[0], model.BridgeLeadCompleted, now.Add(4*time.Second), ""); err != nil || completed {
		t.Fatalf("过期worker不得覆盖未知终态：%v %v", completed, err)
	}
	var tasks []model.BridgeLeadTask
	if err := database.WithContext(ctx).Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Status != model.BridgeLeadUnknown {
			t.Fatalf("运行过的过期任务必须保留未知证据：%+v", task)
		}
	}
}

func TestBridgeLeadTasks_TenantAndHubScope(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	repo := NewBridgeLeadTaskRepository(database)
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	hubs := []model.MessageHub{
		{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "a", ConversationID: "c", MsgID: "one", Direction: "inbound", MsgType: "text", BridgeLeadEligible: true},
		{TenantID: 19, Platform: "douyin", AccountID: "a", ConversationID: "c", MsgID: "two", Direction: "inbound", MsgType: "text", BridgeLeadEligible: true},
		{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "", ConversationID: "c", MsgID: "bad", Direction: "inbound", MsgType: "text", BridgeLeadEligible: true},
	}
	for i := range hubs {
		if err := database.Create(&hubs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := repo.Reconcile(ctx, 10); err != nil || n != 1 {
		t.Fatalf("补齐必须限定租户及完整身份：%d %v", n, err)
	}
	claimed, err := repo.Claim(ctx, time.Now(), time.Minute, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("认领异常：%v %v", claimed, err)
	}
	wrong := claimed[0]
	wrong.AccountID = "other"
	if hub, err := repo.LoadHub(ctx, &wrong); err == nil || hub != nil {
		t.Fatalf("消息引用不能借用另一账号：%v %v", hub, err)
	}
	if _, err := repo.Claim(tenant.OnlyTenant(context.Background(), 0), time.Now(), time.Minute, 10); err == nil {
		t.Fatal("缺租户必须拒绝后台认领")
	}
}

func TestBridgeLeadTasks_ConcurrentInstancesDoNotDoubleClaim(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{}, &model.BridgeLeadTask{})
	ctx, cancel := context.WithTimeout(tenant.ContextWithDefault(context.Background()), 5*time.Second)
	defer cancel()
	repo := NewBridgeLeadTaskRepository(database)
	for i := range 6 {
		hub := &model.MessageHub{TenantID: tenant.DefaultTenantID, Platform: "douyin", AccountID: "concurrent-account", ConversationID: "concurrent-conv", MsgID: fmt.Sprintf("concurrent-%d", i), Direction: "inbound", BridgeLeadEligible: true}
		if err := database.WithContext(ctx).Create(hub).Error; err != nil {
			t.Fatal(err)
		}
	}
	if n, err := repo.Reconcile(ctx, 10); err != nil || n != 6 {
		t.Fatalf("必须准备六个持久任务：%d %v", n, err)
	}
	type result struct {
		tasks []model.BridgeLeadTask
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		utils.SafeGo(ctx, "test.bridge-lead-concurrent-claim", func(ctx context.Context) {
			<-start
			tasks, err := NewBridgeLeadTaskRepository(database).Claim(ctx, time.Now(), time.Minute, 3)
			results <- result{tasks: tasks, err: err}
		})
	}
	close(start)
	seen := make(map[uint]bool)
	for range 2 {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatal(r.err)
			}
			for _, task := range r.tasks {
				if seen[task.ID] || task.LeaseToken == "" {
					t.Fatal("两个实例不能认领同一任务或返回无令牌任务")
				}
				seen[task.ID] = true
			}
		case <-ctx.Done():
			t.Fatal("并发认领必须在受限时间内结束")
		}
	}
	if len(seen) != 6 {
		t.Fatalf("两个有界实例应独立取得六个不同任务：%d", len(seen))
	}
}
