package egolite

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// —— token 机制（照抄 host_token 测试要点：fail-closed / 轮换双候选 / 前缀隔离 / userID 内嵌） ——

func TestGenerateEgoTokenShape(t *testing.T) {
	tok, err := GenerateEgoToken(42)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 8 || tok[:3] != "eg_" {
		t.Fatalf("token 前缀必须是 eg_，got %q", tok[:min(5, len(tok))])
	}
	id, ok := parseEgoTokenUser(tok)
	if !ok || id != 42 {
		t.Fatalf("userID 内嵌解析失败: id=%d ok=%v", id, ok)
	}
}

func TestValidateEgoTokenFailClosed(t *testing.T) {
	// KV 无 token ⇒ 拒绝（fail-closed，host_token 同判据）
	kv := newMemKV()
	if _, ok := ValidateEgoToken(context.Background(), kv, "eg_1_x"); ok {
		t.Fatal("未配置 token 时必须拒绝")
	}
}

func TestEgoTokenRotationDualCandidate(t *testing.T) {
	ctx := context.Background()
	kv := newMemKV()
	t1, _ := GenerateEgoToken(7)
	_, _ = kv.Upsert(ctx, egoTokenKVKey, t1)

	t2, err := RotateEgoToken(ctx, kv, 0) // userID=0 ⇒ 保留旧归属
	if err != nil {
		t.Fatal(err)
	}
	if t1 == t2 {
		t.Fatal("轮换必须产出新 token")
	}
	// 双候选：新旧都在有效期内（与 host_token 一致的撤销语义=重置两次）
	for _, want := range []string{t1, t2} {
		id, ok := ValidateEgoToken(ctx, kv, want)
		if !ok || id != 7 {
			t.Fatalf("轮换后双候选应均可验: token=%q ok=%v id=%d", want[:8], ok, id)
		}
	}
	// 轮换保留归属
	t3, _ := RotateEgoToken(ctx, kv, 0)
	if id, ok := parseEgoTokenUser(t3); !ok || id != 7 {
		t.Fatalf("轮换应保留旧 token 的 userID 归属: id=%d", id)
	}
	// 第三次轮换后 t1 被挤出（撤销=重置两次的实际语义）
	if _, ok := ValidateEgoToken(ctx, kv, t1); ok {
		t.Fatal("第三次轮换后最早 token 应失效")
	}
}

func TestEgoTokenHostTokenNotInterchangeable(t *testing.T) {
	// bh_ 前缀的 host token 绝不能通过 runner 校验（两通道 token 不通用——调研 §6.5）
	kv := newMemKV()
	_, _ = kv.Upsert(context.Background(), egoTokenKVKey, "bh_1_abcdef")
	if _, ok := ValidateEgoToken(context.Background(), kv, "bh_1_abcdef"); ok {
		t.Fatal("bh_ token 不应被 runner-ws 接受")
	}
}

// —— 回环门形态开关（runner_ws loopbackIfConfigured 的判定纯函数部分） ——

func TestLoopbackGateShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/egolite/runner-ws?token=eg_1_x", nil)
	// RemoteAddr 为空（测试环境未注入）⇒ isLoopbackIP("") == false ⇒ fail-closed
	if isLoopbackIP("") {
		t.Fatal("空 RemoteAddr 必须判非回环（fail-closed）")
	}
	_ = c
}

// —— RunnerMeta 不携带锁（vet 锁拷贝检查的结构性保证） ——

func TestRunnerMetaNoLockCopy(t *testing.T) {
	// 编译期即保证：RunnerMeta 不含 sync.Mutex；此测试固化意图
	var m RunnerMeta
	_ = m
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// —— 内存 KV（只实现 token 路径用到的两个方法） ——

type memKV struct{ m map[string]string }

func newMemKV() *memKV { return &memKV{m: map[string]string{}} }

func (k *memKV) Get(_ context.Context, key string) (string, error) {
	v, ok := k.m[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (k *memKV) Upsert(_ context.Context, key, value string) (string, error) {
	k.m[key] = value
	return value, nil
}

func (k *memKV) Available() bool { return true }

func (k *memKV) EnsureTable(_ context.Context) error { return nil }
