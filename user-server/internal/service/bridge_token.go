package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/repository"
)

// ErrLegacyBridgeTokenRetired 旧 Bridge 通道凭证已退役（T058/T063 一次性替换）。
// 管理端旧页面调用 Reset 时收到本错误，应引导改用识别码管理页重置。
var ErrLegacyBridgeTokenRetired = errors.New("旧 Bridge 通道凭证已退役，请在账号识别码管理页重置识别码")

// BridgeTokenService 旧 Bridge 通道凭证的只读退役提示器。
//
// T058 起凭证闸门只认渠道识别码表；T061 停用双写；T063 移除全部 KV 写入与键位。
// 本类型仅保留以兼容既有管理端路由（/api/bridge/token/status|reset），不再写任何凭证，
// 也不再持有 bridge_ingest_token / bridge_ingest_token_prev 这类旧字段。
type BridgeTokenService struct {
	codes repository.ChannelIdentifyCodeRepository
}

// NewBridgeTokenService 创建退役提示器（全局 DB 装配）。
func NewBridgeTokenService() *BridgeTokenService {
	return &BridgeTokenService{codes: repository.NewChannelIdentifyCodeRepository()}
}

// Status 返回退役状态：识别码数量 + 明确提示不再使用 Bridge token。
func (s *BridgeTokenService) Status(ctx context.Context) map[string]any {
	count := 0
	if s != nil && s.codes != nil && s.codes.Available() {
		codes, err := s.codes.ValidCodes(ctx)
		if err == nil {
			count = len(codes)
		}
	}
	return map[string]any{
		"retired":         true,
		"identify_codes":  count,
		"main_configured": count > 0,
		"prev_configured": false,
		"source":          "channel_accounts.identify_code（含存量渠道码）",
		"token":           "",
		"message":         "旧 Bridge token 已退役，请使用账号识别码",
	}
}

// Reset 不再轮换/写入任何凭证——旧写入路径已在 T063 移除。
func (s *BridgeTokenService) Reset(context.Context) (string, error) {
	return "", ErrLegacyBridgeTokenRetired
}
