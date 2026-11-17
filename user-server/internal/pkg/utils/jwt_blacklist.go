package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"
)

const (
	jwtBlacklistTTL = 25 * time.Hour
	jwtBlacklistKey = "jwt:blacklist:"
)

func hashJWT(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// 本进程兜底黑名单（2026-11-15 审计 L6）。
//
// 此前 BlacklistJWT 的 cache.Set 失败只打一行 Warn（写侧 fail-open）：Redis 抖动
// 窗口内「登出 / 刷新轮换 / 禁用吊销」的水位线没有落上，被拉黑的令牌在本实例上
// 仍可重放，最长可用 24h（读侧 fail-closed 只在 Redis 读挂时生效，救不了
// 「写丢了、读还在」的窗口）。这里加一层进程内 map 兜底：写失败时把 token 哈希
// 记进本地，读侧并查——本实例上的吊销立即生效。
//
// 已知边界（明确取舍，不做全局一致）：多实例部署时兜底只覆盖本进程（Redis 恢复
// 后全局一致）；进程重启兜底丢失（Redis 与本 map 同时不可用是最坏窗口，残余
// 风险与修复前相同）。兜底只该是「Redis 抖动的缓冲」，不是第二事实源。
var (
	localBlacklistMu sync.RWMutex
	localBlacklist   = map[string]time.Time{}
)

func localBlacklistAdd(hash string, exp time.Time) {
	localBlacklistMu.Lock()
	defer localBlacklistMu.Unlock()
	localBlacklist[hash] = exp
}

func localBlacklistSeen(hash string) bool {
	now := time.Now()
	localBlacklistMu.Lock()
	defer localBlacklistMu.Unlock()
	exp, ok := localBlacklist[hash]
	if !ok {
		return false
	}
	if now.After(exp) {
		// 过期兜底项顺手清掉（登出/刷新触发本查询，天然是清理时机）
		delete(localBlacklist, hash)
		return false
	}
	return true
}

func localBlacklistCleanup() {
	now := time.Now()
	localBlacklistMu.Lock()
	defer localBlacklistMu.Unlock()
	for h, exp := range localBlacklist {
		if now.After(exp) {
			delete(localBlacklist, h)
		}
	}
}

var localBlacklistJanitorOnce sync.Once

func startLocalBlacklistJanitor() {
	localBlacklistJanitorOnce.Do(func() {
		utils_safeGoLocalBlacklistJanitor()
	})
}

// utils_safeGoLocalBlacklistJanitor 单独拆出：SafeGo 在 utils 包本身，直接递归
// 引用会绕，这里走同包的 SafeGo（utils.SafeGo 定义在包内其他文件）。
func utils_safeGoLocalBlacklistJanitor() {
	SafeGo(context.Background(), "jwt_blacklist.janitor", func(ctx context.Context) {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				localBlacklistCleanup()
			}
		}
	})
}

// BlacklistJWT 将指定 JWT 加入黑名单（登出 / 刷新时调用）
func BlacklistJWT(token string) {
	if token == "" {
		return
	}
	hash := hashJWT(token)
	key := jwtBlacklistKey + hash
	startLocalBlacklistJanitor()
	if err := cache.GetGlobalCache().Set(context.Background(), key, "1", jwtBlacklistTTL); err != nil {
		// 写侧兜底（2026-11-15 审计 L6）：Redis 写失败不再只是打日志——本进程内
		// 记一份，保证本实例上的吊销立即生效（见 localBlacklist 注释的边界声明）。
		localBlacklistAdd(hash, time.Now().Add(jwtBlacklistTTL))
		logger.Warnf("BlacklistJWT cache.Set failed, fell back to in-process blacklist (token revoked on this instance; may be replay-able on other instances until Redis recovers): %v", err)
	}
}

func IsJWTBlacklisted(token string) bool {
	if token == "" {
		return false
	}
	hash := hashJWT(token)
	key := jwtBlacklistKey + hash
	exists, err := cache.GetGlobalCache().Exists(context.Background(), key)
	if err != nil {
		logger.Errorf("IsJWTBlacklisted cache.Exists failed (fail-closed, rejecting token): %v", err)
		return true
	}
	return exists || localBlacklistSeen(hash)
}
