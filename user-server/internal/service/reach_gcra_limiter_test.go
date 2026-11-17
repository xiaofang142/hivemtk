package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func gcraTestClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("本地 Redis 不可达，跳过 GCRA 真实用例: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func gcraTestKey(t *testing.T, client *redis.Client, label string) string {
	t.Helper()
	key := "gcra-test:" + uuid.NewString() + ":" + label
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := client.Del(ctx, "rate:"+key).Err(); err != nil {
			t.Errorf("清理本用例限流键失败：%v", err)
		}
	})
	return key
}

// 与依赖同用Redis TIME及2017纪元；一次只读查询获取时间和本用例TAT。
func gcraTestState(t *testing.T, client *redis.Client, ctx context.Context, key string) (float64, float64) {
	t.Helper()
	values, err := client.Eval(ctx, `local now = redis.call('TIME')
return {now[1], now[2], redis.call('GET', KEYS[1]) or ''}`, []string{"rate:" + key}).Slice()
	if err != nil || len(values) != 3 {
		t.Fatalf("读取Redis测试时间与TAT失败：%v", err)
	}
	number := func(value any) float64 {
		n, err := strconv.ParseFloat(fmt.Sprint(value), 64)
		if err != nil {
			t.Fatalf("Redis时间或TAT不合法：%v", err)
		}
		return n
	}
	now := number(values[0]) - 1483228800 + number(values[1])/1e6
	tat := now
	if values[2] != "" {
		tat = number(values[2])
	}
	return now, tat
}

// TestGCRABasicAllow 单 key 基本放行/拒绝：QPS=5 Burst=5，前 5 个放行、第 6 个拒绝
func TestGCRABasicAllow(t *testing.T) {
	client := gcraTestClient(t)
	l := NewRedisGCRARateLimiter(client, nil)
	ctx := context.Background()

	spec := RateLimitSpec{QPS: 5, Burst: 5}
	allowed := 0
	for i := 0; i < 6; i++ {
		if l.Allow(ctx, gcraTestKey(t, client, fmt.Sprintf("basic-%d", i)), spec) {
			allowed++
		}
	}
	if allowed != 6 {
		t.Fatalf("六个独立键首次应全部放行，实得%d", allowed)
	}

	allowedSame := 0
	sameKey := gcraTestKey(t, client, "basic-same")
	for i := 0; i < 6; i++ {
		if l.Allow(ctx, sameKey, spec) {
			allowedSame++
		}
	}
	if allowedSame != 5 {
		t.Fatalf("同 key 连发 6 次（QPS=5 Burst=5）放行 = %d, want 5", allowedSame)
	}
}

func TestGCRASmoothOutput(t *testing.T) {
	client := gcraTestClient(t)
	l := NewRedisGCRARateLimiter(client, nil)
	ctx := context.Background()
	key := gcraTestKey(t, client, "smooth")
	spec := RateLimitSpec{QPS: 10, Burst: 5}

	for i := 0; i < 5; i++ {
		if !l.Allow(ctx, key, spec) {
			t.Fatalf("burst 阶段第 %d 个被拒（应全放）", i)
		}
	}
	if l.Allow(ctx, key, spec) {
		t.Fatal("burst 耗尽后应拒绝")
	}

	time.Sleep(300 * time.Millisecond)
	refill := 0
	const interval = .1      // 此用例QPS10，服务器每100ms补一个额度。
	const precision = .00001 // Redis浮点序列化误差，不扩大发送额度。
	for i := 0; i < 10; i++ {
		before, tat := gcraTestState(t, client, ctx, key)
		allowed := l.Allow(ctx, key, spec)
		after, nextTat := gcraTestState(t, client, ctx, key)
		eligibleAt := tat - float64(spec.Burst-1)*interval
		if allowed {
			refill++
			if after+precision < eligibleAt {
				t.Fatalf("服务器额度尚未补齐却放行：时间%f，门槛%f", after, eligibleAt)
			}
			lower := math.Max(tat, before) + interval
			upper := math.Max(tat, after) + interval
			if nextTat < lower-precision || nextTat > upper+precision {
				t.Fatalf("放行没有按实际服务器时间推进一个TAT额度：%f，不在[%f,%f]", nextTat, lower, upper)
			}
		} else {
			if before > eligibleAt+precision || math.Abs(nextTat-tat) > precision {
				t.Fatal("拒绝必须发生在服务器补额前，且不能推进TAT")
			}
		}
	}
	if refill == 0 || l.degraded.Load() {
		t.Fatal("真实Redis经过补额窗口后应平滑放行，不能由降级路径伪造通过")
	}
}

func TestGCRAFallbackOnRedisDown(t *testing.T) {
	client := gcraTestClient(t)
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = dead.Close() })

	l := NewRedisGCRARateLimiter(dead, NewMemorySendRateLimiter())
	l.retryAfter = 300 * time.Millisecond
	ctx := context.Background()
	spec := RateLimitSpec{QPS: 5, Burst: 5}
	key := gcraTestKey(t, client, "down")

	ok := 0
	for i := 0; i < 5; i++ {
		if l.Allow(ctx, key, spec) {
			ok++
		}
	}
	if !l.degraded.Load() {
		t.Fatal("Redis 不可达应进入降级态")
	}
	if ok != 5 {
		t.Fatalf("降级后进程内限流应按 burst 放行 5 个, got %d", ok)
	}

	_ = l.Allow(ctx, key, spec)
	if !l.degraded.Load() {
		t.Fatal("降级窗口内不应提前探测恢复")
	}
}

// TestGCRAZeroLimit QPS/Burst 均为 0 时直接放行（与 Memory 语义一致）
func TestGCRAZeroLimit(t *testing.T) {
	client := gcraTestClient(t)
	l := NewRedisGCRARateLimiter(client, nil)
	if !l.Allow(context.Background(), gcraTestKey(t, client, "zero"), RateLimitSpec{}) {
		t.Fatal("空 spec 应放行")
	}
}

// TestGCRAReset 清 key 后重新放行
func TestGCRAReset(t *testing.T) {
	client := gcraTestClient(t)
	l := NewRedisGCRARateLimiter(client, nil)
	ctx := context.Background()
	key := gcraTestKey(t, client, "reset")
	spec := RateLimitSpec{QPS: 1, Burst: 1}
	if !l.Allow(ctx, key, spec) {
		t.Fatal("首次应放行")
	}
	if l.Allow(ctx, key, spec) {
		t.Fatal("burst=1 第二次应拒绝")
	}
	l.Reset(ctx, key)
	if !l.Allow(ctx, key, spec) {
		t.Fatal("Reset 后应重新放行")
	}
}
