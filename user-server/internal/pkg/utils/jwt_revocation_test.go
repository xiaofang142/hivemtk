package utils

import (
	"context"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/cache"

	"github.com/golang-jwt/jwt/v5"
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
