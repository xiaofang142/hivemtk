package egolite

// EgoRunnerAdminService runner 通道的查询/管理接缝（2026-11-05 二次审计）。
// 把 controller 原先直连的 tokenRepo / kvRepo / registry 操作收进服务层。

import (
	"context"

	"hivemtk-user/internal/model"
	hrepo "hivemtk-user/internal/repository"
)

// EgoRunnerAdminService runner 令牌、鉴权与在线状态。
type EgoRunnerAdminService struct {
	tokens   EgoRunnerTokenRepository
	kv       hrepo.PlatformConfigKVRepository
	registry *EgoRunnerRegistry
}

func NewEgoRunnerAdminService(tokens EgoRunnerTokenRepository, kv hrepo.PlatformConfigKVRepository, registry *EgoRunnerRegistry) *EgoRunnerAdminService {
	return &EgoRunnerAdminService{tokens: tokens, kv: kv, registry: registry}
}

// Authenticate 校验 runner 明文令牌（行表优先，KV 双候选兜底）。
func (s *EgoRunnerAdminService) Authenticate(ctx context.Context, plain string) (uint, bool) {
	return NewEgoTokenAuthenticator(s.tokens, s.kv).Authenticate(ctx, plain)
}

// StatusFor 用户可见的 runner 状态（admin 看全量）。
func (s *EgoRunnerAdminService) StatusFor(userID uint, isAdmin bool) any {
	return s.registry.StatusFor(userID, isAdmin)
}

// BroadcastUpgrade 向全部在线 runner 下发升级指令，返回通知数。
func (s *EgoRunnerAdminService) BroadcastUpgrade() int {
	return s.registry.BroadcastUpgrade()
}

func (s *EgoRunnerAdminService) ListTokens(ctx context.Context, userID uint) ([]*model.EgoliteRunnerToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *EgoRunnerAdminService) IssueToken(ctx context.Context, userID uint, label string) (string, *model.EgoliteRunnerToken, error) {
	return s.tokens.Issue(ctx, userID, label)
}

func (s *EgoRunnerAdminService) RevokeToken(ctx context.Context, id, userID uint) error {
	return s.tokens.Revoke(ctx, id, userID)
}

// RotateFallback 轮换 KV 兜底 token（老部署/单机自用；调用方负责 admin 闸）。
func (s *EgoRunnerAdminService) RotateFallback(ctx context.Context) (string, error) {
	return RotateEgoToken(ctx, s.kv, 0)
}
