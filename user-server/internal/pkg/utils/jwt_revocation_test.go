package utils

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// 第十八轮（2026-09-19）：按用户维度的令牌即时吊销。
// 水位线以**秒级 unix** 存储，因此单测直接写死水位线值来构造边界，
// 不用 time.Now() 对拍（会随秒边界抖动）。

func setRevocationMarker(t *testing.T, userID uint, cutoff int64) {
	t.Helper()
	key := revokeKey(userID)
	if err := cache.GetGlobalCache().Set(context.Background(), key, strconv.FormatInt(cutoff, 10), time.Hour); err != nil {
		t.Fatalf("set marker: %v", err)
	}
}

func TestIsTokenRevoked_NoMarker(t *testing.T) {
	// 从未触发吊销事件的用户：任何 iat 都不应判失效
	if IsTokenRevoked(context.Background(), 900001, jwt.NewNumericDate(time.Unix(1000, 0))) {
		t.Fatalf("无水位线时不应判为吊销")
	}
}

func TestIsTokenRevoked_CutoffBoundary(t *testing.T) {
	const uid = 900002
	setRevocationMarker(t, uid, 1_000_000)

	cases := []struct {
		name string
		iat  int64
		want bool
	}{
		{"早于水位线一秒", 999_999, true},
		{"早于水位线很久", 1, true},
		{"与水位线同秒（严格 < 边界，不判失效）", 1_000_000, false},
		{"晚于水位线", 1_000_001, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Unix(tc.iat, 0))); got != tc.want {
				t.Fatalf("iat=%d 期望 revoked=%v，实际 %v", tc.iat, tc.want, got)
			}
		})
	}
}

func TestIsTokenRevoked_MissingIssuedAtFailsClosed(t *testing.T) {
	const uid = 900003
	setRevocationMarker(t, uid, time.Now().Unix())
	// 没有 iat 就无法自证"签发于吊销之后" ⇒ 判失效
	if !IsTokenRevoked(context.Background(), uid, nil) {
		t.Fatalf("缺 iat 且有水位线时必须 fail-closed 判吊销")
	}
	// 无水位线时缺 iat 也放行（不影响存量正常流量）
	if IsTokenRevoked(context.Background(), 900099, nil) {
		t.Fatalf("无水位线时缺 iat 不应判吊销")
	}
}

func TestIsTokenRevoked_CorruptMarkerFailsClosed(t *testing.T) {
	const uid = 900004
	if err := cache.GetGlobalCache().Set(context.Background(), revokeKey(uid), "not-a-number", time.Hour); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	if !IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Now())) {
		t.Fatalf("水位线值非法时必须 fail-closed 拒绝令牌")
	}
}

func TestIsTokenRevoked_ZeroUserIDIgnored(t *testing.T) {
	// user_id=0 不是合法账号 id，不参与吊销判定（避免误伤匿名/系统内部令牌）
	if IsTokenRevoked(context.Background(), 0, jwt.NewNumericDate(time.Unix(1, 0))) {
		t.Fatalf("user_id=0 应直接放行")
	}
}

func TestRevokeUserTokens_WritesNowWatermark(t *testing.T) {
	const uid = 900005
	before := time.Now().Unix()
	RevokeUserTokens(context.Background(), uid)
	after := time.Now().Unix()

	val, err := cache.GetGlobalCache().Get(context.Background(), revokeKey(uid))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	cutoff, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		t.Fatalf("parse marker %q: %v", val, err)
	}
	if cutoff < before || cutoff > after {
		t.Fatalf("水位线应落在调用时刻附近，期望 [%d,%d]，实际 %d", before, after, cutoff)
	}
	// 旧令牌必被拒；新令牌（下一秒起）放行
	if !IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Unix(before-1, 0))) {
		t.Fatalf("吊销前的令牌必须判失效")
	}
	if IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Unix(cutoff+1, 0))) {
		t.Fatalf("吊销后签发的令牌不应判失效")
	}
}

func TestRevokeUserTokens_MovesWatermarkForward(t *testing.T) {
	const uid = 900006
	setRevocationMarker(t, uid, 1_000_000)
	RevokeUserTokens(context.Background(), uid) // 写入 now（远晚于 1_000_000）
	// 旧水位线已被覆盖：iat=1_000_000 依然早于新水位线 ⇒ 仍判失效
	if !IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Unix(1_000_000, 0))) {
		t.Fatalf("覆盖后旧令牌仍须判失效")
	}
}

func TestRevokeUserTokens_ZeroUserIDNoop(t *testing.T) {
	RevokeUserTokens(context.Background(), 0)
	if _, err := cache.GetGlobalCache().Get(context.Background(), revokeKey(0)); err == nil {
		t.Fatalf("user_id=0 不应写入水位线")
	}
}

// ── 客户端取消不得被判成"缓存故障"──────────────────────────────────────
//
// 真机上的失效形态：浏览器在 SPA 路由切换时 abort 掉受保护请求 ⇒ Gin 的 request ctx
// 立刻 context.Canceled ⇒ 水位线读失败落进 IsTokenRevoked 的 default 分支 fail-closed
// ⇒ 一把合法令牌被 401「账号状态已变更，请重新登录」⇒ 前端清 token 打回登录页。
// 写侧同一件事更糟：禁用/改密请求被取消 ⇒ 水位线没落下去 ⇒ 该用户旧令牌最长 24h 仍可用。
//
// MemoryCache 根本不看 ctx，所以这条腿在默认后端上是**量不到**的（改前改后都绿＝假测试）。
// 这里换一个"看 ctx 脸色"的缓存替身：走真实 Redis 会把用例绑在本地端口上（CI 没有 Redis），
// 因此用 go-redis 的 hook 造一个**永不联网**的替身——ctx 已 Done 就原样回 ctx.Err()
// （与真客户端在同一时刻的行为一致），否则从内存表应答；从不调用 next ⇒ 不发 TCP 连接。

type ctxAwareCache struct {
	mu   sync.Mutex
	data map[string]string
}

func (h *ctxAwareCache) Name() string { return "ctx-aware-redis" }

func (h *ctxAwareCache) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *ctxAwareCache) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return h.respond(ctx, cmd)
	}
}

func (h *ctxAwareCache) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if err := h.respond(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	}
}

func (h *ctxAwareCache) respond(ctx context.Context, cmd redis.Cmder) error {
	if err := ctx.Err(); err != nil {
		cmd.SetErr(err)
		return err
	}
	args := cmd.Args()
	if len(args) < 2 {
		err := fmt.Errorf("ctxAwareCache: 未预期的命令参数 %v", args)
		cmd.SetErr(err)
		return err
	}
	key, _ := args[1].(string)
	h.mu.Lock()
	defer h.mu.Unlock()
	switch cmd.Name() {
	case "get":
		val, ok := h.data[key]
		if !ok {
			cmd.SetErr(redis.Nil)
			return redis.Nil
		}
		if c, ok := cmd.(*redis.StringCmd); ok {
			c.SetVal(val)
		}
		return nil
	case "set":
		h.data[key] = fmt.Sprint(args[2])
		if c, ok := cmd.(*redis.StatusCmd); ok {
			c.SetVal("OK")
		}
		return nil
	default:
		err := fmt.Errorf("ctxAwareCache: 未实现该命令 %q（夹具覆盖面不足）", cmd.Name())
		cmd.SetErr(err)
		return err
	}
}

// installCtxAwareCache 把全局缓存换成看 ctx 脸色的替身，测试结束还原。
// 返回夹具本体，供断言"夹具确实生效"。
func installCtxAwareCache(t *testing.T) *ctxAwareCache {
	t.Helper()
	fake := &ctxAwareCache{data: map[string]string{}}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	client.AddHook(fake)
	cache.InitGlobalCache(client)
	t.Cleanup(func() {
		_ = client.Close()
		cache.InitGlobalCache(nil)
	})
	return fake
}

// canceledCallerCtx 造一个"客户端已经 abort"的 ctx，并当场自证它确实已 Done——
// 夹具没生效时这条腿等于没测，绝不能默默走过去。
func canceledCallerCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ctx.Err() == nil {
		t.Fatal("夹具没生效：ctx 未被取消")
	}
	return ctx
}

func assertCacheHonorsCtx(t *testing.T) {
	t.Helper()
	// 直接把已取消的 ctx 交给缓存读：必须拿到 context.Canceled（真 Redis 客户端同刻行为）。
	// 拿到 ErrCacheMiss 之类的就说明后端不看 ctx（MemoryCache），下面的断言全是假的。
	_, err := cache.GetGlobalCache().Get(canceledCallerCtx(t), "probe:not-a-key")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("夹具没生效：已取消的 ctx 读缓存得到 %v（want context.Canceled）——这条腿等于没测", err)
	}
}

// TestIsTokenRevoked_CanceledCallerCtxDoesNotRejectToken 客户端 abort 不得把合法令牌判成已吊销。
func TestIsTokenRevoked_CanceledCallerCtxDoesNotRejectToken(t *testing.T) {
	installCtxAwareCache(t)
	assertCacheHonorsCtx(t)
	canceled := canceledCallerCtx(t)

	const uid = 900008
	// 从未吊销的账号：调用方 ctx 已死也必须放行（这正是被踢回登录页的那一类请求）
	if IsTokenRevoked(canceled, uid, jwt.NewNumericDate(time.Unix(1000, 0))) {
		t.Fatalf("客户端取消被当成了吊销：无水位线时合法令牌被判失效")
	}

	// 解绑不许反过来削弱真吊销：水位线仍在时，吊销前的令牌照旧判失效，吊销后的照旧放行
	setRevocationMarker(t, uid, 1_000_000)
	if !IsTokenRevoked(canceled, uid, jwt.NewNumericDate(time.Unix(999_999, 0))) {
		t.Fatalf("有水位线时吊销前的令牌必须判失效（解绑取消链不能变成放行）")
	}
	if IsTokenRevoked(canceled, uid, jwt.NewNumericDate(time.Unix(1_000_001, 0))) {
		t.Fatalf("有水位线时吊销后签发的令牌不应判失效")
	}
}

// TestRevokeUserTokens_WatermarkLandsDespiteCanceledCallerCtx 吊销写入必须活过客户端取消，
// 否则"禁用/改密"请求被 abort 就等于没发生（旧令牌继续可用最长 24h）。
func TestRevokeUserTokens_WatermarkLandsDespiteCanceledCallerCtx(t *testing.T) {
	installCtxAwareCache(t)
	assertCacheHonorsCtx(t)

	const uid = 900009
	RevokeUserTokens(canceledCallerCtx(t), uid)

	val, err := cache.GetGlobalCache().Get(context.Background(), revokeKey(uid))
	if err != nil {
		t.Fatalf("调用方 ctx 取消后水位线没落下去（该用户旧令牌仍可用到自然过期）：%v", err)
	}
	cutoff, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		t.Fatalf("水位线值非法 %q: %v", val, err)
	}
	if !IsTokenRevoked(context.Background(), uid, jwt.NewNumericDate(time.Unix(cutoff-1, 0))) {
		t.Fatalf("落库的水位线没能作废吊销前的令牌")
	}
}

// TestIsTokenRevoked_GenuineCacheOutageStillFailsClosed 解绑只针对"调用方取消"，
// 真正的缓存不可用（这里是网络层拒绝连接，不是 ctx 派生错误）仍必须 fail-closed。
func TestIsTokenRevoked_GenuineCacheOutageStillFailsClosed(t *testing.T) {
	// 无人监听的端口 + 关掉重试：得到的是 dial 失败，与 context.Canceled 同类不同义
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	cache.InitGlobalCache(client)
	t.Cleanup(func() {
		_ = client.Close()
		cache.InitGlobalCache(nil)
	})

	if _, err := cache.GetGlobalCache().Get(context.Background(), "probe:not-a-key"); err == nil {
		t.Fatal("夹具没生效：不存在的 Redis 竟读成功")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("夹具走的是取消路径而非故障路径：%v", err)
	}
	if !IsTokenRevoked(context.Background(), 900010, jwt.NewNumericDate(time.Now())) {
		t.Fatalf("缓存真的不可用时必须 fail-closed 拒绝令牌")
	}
}

// TestDetachedRevocationCtx_ShedsCallerCancellationAndKeepsBudget
// 解绑后的 ctx 必须既不吃调用方的取消、又带着自己的硬超时（否则缓存挂了就一直挂着）。
func TestDetachedRevocationCtx_ShedsCallerCancellationAndKeepsBudget(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	detached, detachedCancel := detachedRevocationCtx(parent)
	defer detachedCancel()

	if err := detached.Err(); err != nil {
		t.Fatalf("解绑失败：派生 ctx 继承了调用方的取消（%v）⇒ 合法令牌会被判成已吊销", err)
	}
	if _, ok := detached.Deadline(); !ok {
		t.Fatalf("派生 ctx 没有超时预算：缓存不可用时会无限期占住受保护请求")
	}
}
