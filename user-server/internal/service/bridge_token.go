package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"

	"hivemtk-user/internal/repository"
)

const (
	bridgeTokenKVKey     = "bridge_ingest_token"
	bridgeTokenPrevKVKey = "bridge_ingest_token_prev"
)

// BridgeTokenService 桥接通道凭证服务
//
// GET  /api/bridge/token/status  —— 查询配置状态，并回显当前明文（便于管理员再次复制到新渠道）
// POST /api/bridge/token/reset   —— 轮换：旧值转 PREV，生成新值返回明文
//
// 存储优先级：DB platform_config_kv（运行时可变）> 环境变量 BRIDGE_INGEST_TOKEN。
// 桥接凭证属于实例级通道凭证（非租户业务配置），必须与租户上下文解耦：
// 入口校验发生在租户解析之前，用租户 KV 在 on 模式下会 fail-closed。
type BridgeTokenService struct {
	kv repository.PlatformConfigKVRepository
}

// NewBridgeTokenService 创建服务（全局 DB 装配）
func NewBridgeTokenService() *BridgeTokenService {
	return &BridgeTokenService{kv: repository.NewPlatformConfigKVRepository()}
}

func generateBridgeTokenValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Status 返回主/灰度 token 是否已配置，并回显当前明文（便于管理员再次复制到新渠道）
func (s *BridgeTokenService) Status(ctx context.Context) map[string]any {
	status := map[string]any{
		"main_configured": false,
		"prev_configured": false,
		"source":          "unset",
		"token":           "",
	}
	if v, err := s.kv.Get(ctx, bridgeTokenKVKey); err == nil && v != "" {
		status["main_configured"] = true
		status["source"] = "db"
		status["token"] = v
	} else if envTok := os.Getenv("BRIDGE_INGEST_TOKEN"); envTok != "" {
		status["main_configured"] = true
		status["source"] = "env"
		status["token"] = envTok
	}
	if v, err := s.kv.Get(ctx, bridgeTokenPrevKVKey); err == nil && v != "" {
		status["prev_configured"] = true
	}
	return status
}

// Reset 轮换凭证：旧 token 转入灰度位（PREV），生成并返回新 token 明文
func (s *BridgeTokenService) Reset(ctx context.Context) (string, error) {
	newTok, err := generateBridgeTokenValue()
	if err != nil {
		return "", err
	}

	if cur, gerr := s.kv.Get(ctx, bridgeTokenKVKey); gerr == nil && cur != "" {
		_, _ = s.kv.Upsert(ctx, bridgeTokenPrevKVKey, cur)
	}
	if _, uerr := s.kv.Upsert(ctx, bridgeTokenKVKey, newTok); uerr != nil {
		return "", uerr
	}
	return newTok, nil
}
