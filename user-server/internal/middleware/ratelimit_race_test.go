package middleware

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestRateLimiter_ConcurrentGetAndCleanup -race 压力：
// 并发 getLimiter/Allow 与 cleanupClients 交叉读写 clients map。
// 修复验证轮（第十五轮）无生产改动，本测试锁住既有锁纪律：
// 任一侧丢锁时 go test -race 立即报 DATA RACE。
func TestRateLimiter_ConcurrentGetAndCleanup(t *testing.T) {
	rl := &RateLimiter{
		clients:  make(map[string]*ClientLimiter),
		config:   RateLimitConfig{RPS: 1000, BucketSize: 100, Enabled: true},
		cleanup:  time.Minute,
		stopChan: make(chan struct{}),
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := "10.0." + strconv.Itoa(w) + "." + strconv.Itoa(i%7)
				_ = rl.getLimiter(key)
				if i%3 == 0 {
					_ = rl.Allow(context.Background(), key)
				}
			}
		}(w)
	}
	for c := 0; c < 3; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rl.cleanupClients()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	close(rl.stopChan) // 不起 cleanupLoop，仅收敛 stopChan 语义
}

// TestVisitorRateLimiter_ConcurrentGetAndCleanup 同上压 visitor 双维度 map。
func TestVisitorRateLimiter_ConcurrentGetAndCleanup(t *testing.T) {
	rl := &visitorRateLimiter{
		ipLimiters:      make(map[string]*visitorLimiterEntry),
		channelLimiters: make(map[string]*visitorLimiterEntry),
		config:          DefaultVisitorRateLimitConfig,
		stopChan:        make(chan struct{}),
	}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = rl.getIPLimiter(fmt.Sprintf("192.168.%d.%d", w, i%11))
				if i%2 == 0 {
					_ = rl.getChannelLimiter("chan-" + strconv.Itoa(i%5))
				}
			}
		}(w)
	}
	for c := 0; c < 3; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rl.cleanup()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}
