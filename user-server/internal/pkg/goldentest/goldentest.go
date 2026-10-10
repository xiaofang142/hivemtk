// Package goldentest 确定性测试基建（借鉴 catbus rand.ts/vm.ts 模式）：
// 种子随机数 + 假时钟 + 黄金文件回放，让含随机数/时间/外部请求的代码
// 可字节级复现。
package goldentest

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// SeededRand mulberry32 种子随机源：同种子同序列，替代 Math.random 式不确定性。
type SeededRand struct {
	state uint32
}

// NewSeededRand 以给定种子构造。
func NewSeededRand(seed uint32) *SeededRand { return &SeededRand{state: seed} }

// Next 返回 [0,1) 确定性伪随机数。
func (r *SeededRand) Next() float64 {
	r.state += 0x6D2B79F5
	t := r.state
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + 7*(t^(t>>7)) | 61
	return float64((t^(t>>14))&0xFFFFFFFF) / 4294967296
}

// Intn 返回 [0,n) 整数。
func (r *SeededRand) Intn(n int) int { return int(r.Next() * float64(n)) }

// FakeClock 可手动推进的假时钟。
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock 从给定时刻构造。
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

// Now 实现取时。
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 手动推进。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// Golden 对比 value 与黄金文件；golden 缺失或 UPDATE_GOLDEN=1 时刷新并跳过断言。
//
// 用法：先写断言跑一遍生成黄金文件，人工 diff 确认后入库；此后任何格式/语义
// 漂移都会被测试抓住（字节级）。
func Golden(t *testing.T, name string, value []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if dir := os.Getenv("GOLDEN_DIR"); dir != "" {
		path = filepath.Join(dir, name+".golden")
	}
	want, err := os.ReadFile(path)
	if err != nil {
		refresh(path, value)
		t.Skipf("golden %s 不存在，已生成基线（请人工 diff 后入库）: %v", path, err)
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		refresh(path, value)
		t.Skipf("golden %s 已更新（UPDATE_GOLDEN=1），请 diff 后入库", path)
	}
	if string(want) != string(value) {
		t.Fatalf("golden %s 不匹配：\n--- want ---\n%s\n--- got ---\n%s\n（确认属预期改进后，UPDATE_GOLDEN=1 重新生成）", path, want, value)
	}
}

func refresh(path string, value []byte) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, value, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "goldentest: 写黄金文件失败 %v\n", err)
	}
}
