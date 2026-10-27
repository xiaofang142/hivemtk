package egolite

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"

	hrepo "hivemtk-user/internal/repository"
)

// ego-runner 通道 token 机制（调研 §6.5 定案：照抄 browser_automation/service/host_token.go，
// 前缀 eg_ 与 bh_ 隔离——两通道 token 不通用；KV 双候选 _prev 轮换 + 常量时间比较 + fail-closed）。
//
// 轮换语义与 host_token 一致：当前值滚入 _prev，旧 token 活到下一次重置；撤销=重置两次。
// 轮换提级 P0 的含义（形态乙补偿控制②）：此文件随 runner-ws 上线即生效，不后置。

const (
	egoTokenKVKey     = "egolite_runner_token"
	egoTokenKVPrevKey = "egolite_runner_token_prev"
)

// GenerateEgoToken admin 生成绑定用户的 runner token
func GenerateEgoToken(userID uint) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("eg_%d_%s", userID, hex.EncodeToString(buf)), nil
}

// RotateEgoToken 轮换：旧值滚入 _prev（保留归属用户），写新值
func RotateEgoToken(ctx context.Context, kvRepo hrepo.PlatformConfigKVRepository, userID uint) (string, error) {
	old, err := kvRepo.Get(ctx, egoTokenKVKey)
	if err != nil {
		return "", err
	}
	// 未指定时保留旧 token 的 userID 归属（host_token 同语义）
	if userID == 0 && old != "" {
		if id, ok := parseEgoTokenUser(old); ok {
			userID = id
		}
	}
	if userID == 0 {
		userID = 1
	}
	newToken, err := GenerateEgoToken(userID)
	if err != nil {
		return "", err
	}
	if old != "" {
		if _, err := kvRepo.Upsert(ctx, egoTokenKVPrevKey, old); err != nil {
			return "", err
		}
	}
	if _, err := kvRepo.Upsert(ctx, egoTokenKVKey, newToken); err != nil {
		return "", err
	}
	return newToken, nil
}

func parseEgoTokenUser(token string) (uint, bool) {
	parts := strings.SplitN(token, "_", 3)
	if len(parts) != 3 || parts[0] != "eg" {
		return 0, false
	}
	id, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil || id == 0 {
		return 0, false
	}
	return uint(id), true
}

// ValidateEgoToken 校验（双候选+常量时间），返回归属 userID；fail-closed
func ValidateEgoToken(ctx context.Context, kvRepo hrepo.PlatformConfigKVRepository, got string) (uint, bool) {
	candidates := make([]string, 0, 2)
	if v, err := kvRepo.Get(ctx, egoTokenKVKey); err == nil && strings.TrimSpace(v) != "" {
		candidates = append(candidates, strings.TrimSpace(v))
	}
	if v, err := kvRepo.Get(ctx, egoTokenKVPrevKey); err == nil && strings.TrimSpace(v) != "" {
		candidates = append(candidates, strings.TrimSpace(v))
	}
	if len(candidates) == 0 {
		return 0, false
	}
	for _, want := range candidates {
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			id, ok := parseEgoTokenUser(want)
			if !ok {
				return 0, false
			}
			return id, true
		}
	}
	return 0, false
}

// EnsureEgoTokenExists 进程启动兜底（避免首次部署 fail-closed 卡死，host_token 同型）
func EnsureEgoTokenExists(ctx context.Context, kvRepo hrepo.PlatformConfigKVRepository) {
	v, err := kvRepo.Get(ctx, egoTokenKVKey)
	if err == nil && strings.TrimSpace(v) != "" {
		return
	}
	tok, err := GenerateEgoToken(1)
	if err != nil {
		return
	}
	_, _ = kvRepo.Upsert(ctx, egoTokenKVKey, tok)
}

// isLoopbackIP 回环判定（host.go 同判据：只认 RemoteAddr 解析结果，畸形一律非回环）
func isLoopbackIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	return ip != nil && ip.IsLoopback()
}
