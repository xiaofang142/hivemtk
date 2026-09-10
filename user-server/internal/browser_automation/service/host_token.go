package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	hrepo "hivemtk-user/internal/repository"
)

// Host 通道 token 机制（参照 BridgeIngressGuard：KV 存储 + fail-closed + _prev 轮换）。
// token 形如 "bh_<userID>_<rand>"——userID 内嵌使 WS 握手即完成 连接↔用户 绑定，
// 命令只路由到归属 Host，绝不跨用户投递（多租户边界，设计文档 §10）。

const (
	hostTokenKey     = "browser_host_token"
	hostTokenPrevKey = "browser_host_token_prev"
)

// GenerateHostToken admin 生成绑定用户的 Host token
func GenerateHostToken(userID uint) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("bh_%d_%s", userID, hex.EncodeToString(buf)), nil
}

// RotateHostToken 轮换 token：当前值滚入 _prev（保持旧 token 平滑失效），写入新值。
// 注意：轮换不改变 token 归属用户（新 token 仍按原 userID 生成语义由调用方决定；
// MVP 简化：轮换时为 admin 指定/默认 user=1 生成，或保留原 token 的 userID）。
func RotateHostToken(ctx context.Context, kvRepo hrepo.SystemConfigKVRepository) (string, error) {
	old, err := kvRepo.Get(ctx, hostTokenKey)
	if err != nil {
		return "", err
	}
	// 保留旧 token 的 userID 归属
	userID := uint(1)
	if old != "" {
		if id, ok := parseHostTokenUser(old); ok {
			userID = id
		}
	}
	newToken, err := GenerateHostToken(userID)
	if err != nil {
		return "", err
	}
	if old != "" {
		if _, err := kvRepo.Upsert(ctx, hostTokenPrevKey, old); err != nil {
			return "", err
		}
	}
	if _, err := kvRepo.Upsert(ctx, hostTokenKey, newToken); err != nil {
		return "", err
	}
	return newToken, nil
}

// parseHostTokenUser 从 token 解出归属 userID
func parseHostTokenUser(token string) (uint, bool) {
	parts := strings.SplitN(token, "_", 3)
	if len(parts) != 3 || parts[0] != "bh" {
		return 0, false
	}
	id, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

// ValidateHostToken 校验 token（常量时间比较，双 token 候选），返回归属 userID。
// fail-closed：KV 未配置任何 token 时拒绝。
func ValidateHostToken(ctx context.Context, kvRepo hrepo.SystemConfigKVRepository, got string) (uint, bool) {
	candidates := make([]string, 0, 2)
	if v, err := kvRepo.Get(ctx, hostTokenKey); err == nil && strings.TrimSpace(v) != "" {
		candidates = append(candidates, strings.TrimSpace(v))
	}
	if v, err := kvRepo.Get(ctx, hostTokenPrevKey); err == nil && strings.TrimSpace(v) != "" {
		candidates = append(candidates, strings.TrimSpace(v))
	}
	if len(candidates) == 0 {
		return 0, false
	}
	for _, want := range candidates {
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			id, ok := parseHostTokenUser(want)
			if !ok {
				return 0, false
			}
			return id, true
		}
	}
	return 0, false
}

// EnsureHostTokenExists 进程启动时若 KV 无 token 则自动为 admin 生成一个（避免首次部署 fail-closed 卡死）。
// 返回值仅记日志用。
func EnsureHostTokenExists(ctx context.Context, kvRepo hrepo.SystemConfigKVRepository) {
	v, err := kvRepo.Get(ctx, hostTokenKey)
	if err == nil && strings.TrimSpace(v) != "" {
		return
	}
	tok, err := GenerateHostToken(1)
	if err != nil {
		return
	}
	if _, err := kvRepo.Upsert(ctx, hostTokenKey, tok); err != nil {
		return
	}
}
