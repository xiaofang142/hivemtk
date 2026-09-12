package websocket

import (
	"testing"
	"time"

	"hivemtk-user/internal/cache"
)

// 降级路径：seqIsRedis=false → 本地 atomic 递增
func TestD15_FallbackLocalSeq(t *testing.T) {
	oldIsRedis, oldCache := seqIsRedis, seqCache
	defer func() { seqIsRedis, seqCache = oldIsRedis, oldCache }()
	seqIsRedis = func() bool { return false }
	seqCache = nil
	redisDegraded.Store(false)

	a, b := NextSeq(), NextSeq()
	if b != a+1 {
		t.Errorf("降级路径应本地递增, got %d→%d", a, b)
	}
	redisDegraded.Store(false)
}

// Redis 模式：MemoryCache 替身走 INCR 递增
func TestD15_RedisSeqIncr(t *testing.T) {
	oldIsRedis, oldCache := seqIsRedis, seqCache
	defer func() { seqIsRedis, seqCache = oldIsRedis, oldCache }()
	mc := cache.NewMemoryCache()
	seqIsRedis = func() bool { return true }
	seqCache = mc
	redisDegraded.Store(false)

	a, b, c := NextSeq(), NextSeq(), NextSeq()
	if !(b == a+1 && c == b+1) {
		t.Errorf("Redis INCR 应连续递增: %d %d %d", a, b, c)
	}
	if PeekSeq() != c {
		t.Errorf("PeekSeq 应=最新值 %d, got %d", c, PeekSeq())
	}
	redisDegraded.Store(false)
}

// 撞号保护：降级期间本地发号超 Redis 水位，恢复后自动跳过
func TestD15_RecoverySkipsLocalPeak(t *testing.T) {
	oldIsRedis, oldCache := seqIsRedis, seqCache
	defer func() { seqIsRedis, seqCache = oldIsRedis, oldCache }()
	mc := cache.NewMemoryCache()
	seqIsRedis = func() bool { return true }
	seqCache = mc
	redisDegraded.Store(false)

	redisN := NextSeq()

	redisDegraded.Store(true)
	local := NextSeq()
	for i := 0; i < 4; i++ {
		local = NextSeq()
	}

	got := NextSeq()
	if got <= uint64(redisN) || got <= local {
		t.Errorf("恢复后应跳过本地峰值, redis=%d local=%d got=%d", redisN, local, got)
	}
	redisDegraded.Store(false)
}

// epoch：同进程内稳定、与 NewEnvelope 自动填充一致
func TestD15_EpsilonStableAndFilled(t *testing.T) {
	if CurrentEpoch() == "" {
		t.Fatal("epoch 不应为空")
	}
	if CurrentEpoch() != epochStr {
		t.Error("CurrentEpoch 应= epochStr")
	}
	env := MustEnvelope(NextSeq(), "msg", nil)
	if env.Epoch != epochStr {
		t.Errorf("Envelope.Epoch 应自动填充, got %q", env.Epoch)
	}
}

// PendingSince 合并远端（MemoryCache 写穿同进程即时可见）
func TestD15_PendingSinceMergesRemote(t *testing.T) {
	oldIsRedis, oldCache := seqIsRedis, seqCache
	defer func() { seqIsRedis, seqCache = oldIsRedis, oldCache }()
	seqIsRedis = func() bool { return true }
	seqCache = cache.NewMemoryCache()
	redisDegraded.Store(false)

	p := NewPendingAck()
	p.Track("s1", 5)
	p.Track("s1", 7)

	pendingRedis.asyncSetJSON("s1", map[uint64]time.Time{9: time.Now()})

	// asyncSetJSON 是异步 goroutine 写同一 seqCache；等待其落盘后再覆盖快照，
	// 否则后台写会与本行的确定性写入竞态，偶发把快照覆盖回 {9} 导致合并断言失败。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var probe map[uint64]time.Time
		if _ = seqCache.GetJSON(nil, pendingKey("s1"), &probe); len(probe) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	_ = seqCache.SetJSON(nil, pendingKey("s1"), map[uint64]time.Time{5: time.Now(), 9: time.Now()}, 0)

	got := p.PendingSince("s1", 6)

	if len(got) != 2 {
		t.Errorf("应合并本地 7 + 远端 9 共 2 条, got %v", got)
	}
	for _, want := range []uint64{7, 9} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("缺 seq=%d, got %v", want, got)
		}
	}
	redisDegraded.Store(false)
}
