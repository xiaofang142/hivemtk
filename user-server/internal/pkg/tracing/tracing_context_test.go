package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
)

func TestTracingContext_DirectWriteHonorsDatabaseDeadline(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageTrace{})
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	lockCtx, cancelLock := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelLock()
	tx, err := pool.BeginTx(lockCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(lockCtx, "LOCK TABLE message_trace IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(tenant.OnlyTenant(context.Background(), 18), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	started := time.Now()
	// 直接路径单独验证，不依赖全包其它测试是否已初始化异步sink。
	utils.SafeGo(ctx, "tracing.test.deadline", func(workerCtx context.Context) {
		done <- writeNodeContext(workerCtx, pendingSpan{tenantID: 18, node: NodeIngest, status: StatusOk})
	})
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("数据库等待必须传播截止错误，而不是成功或吞错：%v", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = tx.Rollback()
		<-done
		t.Fatal("追踪兼容直写丢弃了调用方截止时间")
	}
	if time.Since(started) >= 500*time.Millisecond {
		t.Fatal("数据库锁不应让有截止时间的观测无限等待")
	}
	_ = tx.Rollback()
	var count int64
	if err := database.WithContext(tenant.OnlyTenant(context.Background(), 18)).Model(&model.MessageTrace{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("已超时的写入不得在释放锁后继续落库")
	}
}

func TestTracingContext_RejectsUnconfirmedScope(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.OnlyTenant(context.Background(), 18)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "缺上下文"},
		{name: "缺租户", ctx: context.Background()},
		{name: "已取消", ctx: canceled},
		{name: "冲突载体", ctx: WithCarrier(ctx, &Carrier{TenantID: 19})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := RecordNodeContext(tc.ctx, NodeSpan{Node: NodeIngest}); err == nil {
				t.Fatal("受控观测不得借用未知或其它租户的上下文")
			}
		})
	}
}
