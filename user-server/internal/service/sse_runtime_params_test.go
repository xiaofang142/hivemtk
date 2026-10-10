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

// newSSERuntimeParamFixture 起一个只带 config_params 的库，seed 后把全局参数
// 服务换成指向它的实例；返回后可用 UpdateValue 改参数值。
// 第二个返回值是底层 *gorm.DB，供需要绕过 service 校验的用例直接用 repository 写。
func newSSERuntimeParamFixture(t *testing.T) (*ConfigParamService, *gorm.DB) {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	if database == nil {
		t.Skip("no test DB")
	}
	if err := SeedConfigParams(context.Background(), database); err != nil {
		t.Fatalf("SeedConfigParams 失败：%v", err)
	}
	svc := NewConfigParamService(database)
	SetGlobalForTest(svc)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	return svc, database
}

func setSSEParam(t *testing.T, svc *ConfigParamService, group, key, val string) {
	t.Helper()
	if err := svc.UpdateValue(context.Background(), group, key, val, 1); err != nil {
		t.Fatalf("UpdateValue(%s.%s=%s) 失败：%v", group, key, val, err)
	}
}

// TestSSERuntimeParamsReadParamCenter 证明三条参数真的从库里读，且改完立即生效
// （除构造期那条要重启）。
func TestSSERuntimeParamsReadParamCenter(t *testing.T) {
	svc, _ := newSSERuntimeParamFixture(t)
	ctx := context.Background()

	if got := runtimeSSEHeartbeatInterval(ctx); got != SSEHeartbeatInterval {
		t.Errorf("未改参数时应等于兜底 %v, got %v", SSEHeartbeatInterval, got)
	}
	setSSEParam(t, svc, "sse", "heartbeat_interval", "42")
	if got := runtimeSSEHeartbeatInterval(ctx); got != 42*time.Second {
		t.Errorf("改 42s 后应读到 42s, got %v", got)
	}

	if got := runtimeSSEMaxConnPerIP(ctx); got != SSEMaxConnPerIP {
		t.Errorf("未改参数时应等于兜底 %d, got %d", SSEMaxConnPerIP, got)
	}
	setSSEParam(t, svc, "sse", "max_conn_per_ip", "9")
	if got := runtimeSSEMaxConnPerIP(ctx); got != 9 {
		t.Errorf("改 9 后应读到 9, got %d", got)
	}

	if got := runtimeSSEClientBufferSize(ctx); got != SSEClientBufferSize {
		t.Errorf("未改参数时应等于兜底 %d, got %d", SSEClientBufferSize, got)
	}
	setSSEParam(t, svc, "sse", "client_buffer_size", "256")
	if got := runtimeSSEClientBufferSize(ctx); got != 256 {
		t.Errorf("改 256 后应读到 256, got %d", got)
	}
}

// TestSSERuntimeParamsRejectNonPositive 钉住三类非正值各自的后果：
// 心跳 0 → time.NewTicker(0) panic；连接数 0 → 所有访客被挡；缓冲 0 → 同步信道堵死广播。
//
// 写入走 repository 而不是 service.UpdateValue：后者有 min/max 校验（阶段 0.2 修的
// 那个口子），越界值根本进不了库。runtimeSSE* 的守卫防的是**库里已经有坏值**的
// 情况——历史数据、直接改库、或将来有人绕过 service 写。这正是守卫存在的理由，
// 所以必须用能造出坏值的方式来测它。
func TestSSERuntimeParamsRejectNonPositive(t *testing.T) {
	_, db := newSSERuntimeParamFixture(t)
	ctx := context.Background()
	repo := repository.NewConfigParamRepository(db)

	writeRaw := func(group, key, val string) {
		t.Helper()
		if err := repo.UpdateValue(ctx, group, key, val, 1); err != nil {
			t.Fatalf("repo.UpdateValue(%s.%s=%s) 失败：%v", group, key, val, err)
		}
	}

	writeRaw("sse", "heartbeat_interval", "0")
	if got := runtimeSSEHeartbeatInterval(ctx); got != SSEHeartbeatInterval {
		t.Errorf("心跳 0 应回落兜底 %v（否则 Ticker panic）, got %v", SSEHeartbeatInterval, got)
	}
	writeRaw("sse", "max_conn_per_ip", "0")
	if got := runtimeSSEMaxConnPerIP(ctx); got != SSEMaxConnPerIP {
		t.Errorf("连接数 0 应回落兜底 %d（否则挡掉所有访客）, got %d", SSEMaxConnPerIP, got)
	}
	writeRaw("sse", "client_buffer_size", "0")
	if got := runtimeSSEClientBufferSize(ctx); got != SSEClientBufferSize {
		t.Errorf("缓冲 0 应回落兜底 %d（否则同步信道堵死广播）, got %d", SSEClientBufferSize, got)
	}

	writeRaw("sse", "heartbeat_interval", "-5")
	if got := runtimeSSEHeartbeatInterval(ctx); got != SSEHeartbeatInterval {
		t.Errorf("心跳负值应回落兜底, got %v", got)
	}
	writeRaw("sse", "max_conn_per_ip", "-1")
	if got := runtimeSSEMaxConnPerIP(ctx); got != SSEMaxConnPerIP {
		t.Errorf("连接数负值应回落兜底, got %d", got)
	}
	writeRaw("sse", "client_buffer_size", "-100")
	if got := runtimeSSEClientBufferSize(ctx); got != SSEClientBufferSize {
		t.Errorf("缓冲负值应回落兜底, got %d", got)
	}

	// 坏值恢复后必须又能读到新值（证明守卫不是把这一键永久钉死）。
	writeRaw("sse", "max_conn_per_ip", "12")
	// 必须换一个**冷缓存**的 service：参数服务按 group.key 缓存 60s
	// （configParamTTL），上一个 service 里 sse.max_conn_per_ip 还停在 0，
	// 直接读只会拿到缓存的旧值。
	SetGlobalForTest(NewConfigParamService(db))
	if got := runtimeSSEMaxConnPerIP(ctx); got != 12 {
		t.Errorf("坏值恢复后应读到 12, got %d", got)
	}
}

// TestSSERuntimeParamsWithoutParamCenter 无库/无全局服务时三条都必须回落兜底。
func TestSSERuntimeParamsWithoutParamCenter(t *testing.T) {
	SetGlobalForTest(nil)
	t.Cleanup(func() { SetGlobalForTest(nil) })
	ctx := context.Background()

	if got := runtimeSSEHeartbeatInterval(ctx); got != SSEHeartbeatInterval {
		t.Errorf("无参数服务时心跳应回落兜底, got %v", got)
	}
	if got := runtimeSSEMaxConnPerIP(ctx); got != SSEMaxConnPerIP {
		t.Errorf("无参数服务时连接数应回落兜底, got %d", got)
	}
	if got := runtimeSSEClientBufferSize(ctx); got != SSEClientBufferSize {
		t.Errorf("无参数服务时缓冲应回落兜底, got %d", got)
	}
}
