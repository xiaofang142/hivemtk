package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
)

// CreateTokenRequest 创建 Token
type CreateTokenRequest struct {
	Name      string     `json:"name"`
	ProductID string     `json:"product_id"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedBy string     `json:"created_by"`
}

// normalizeScopes 归一 Token 权限。
// 最小权限原则：不显式声明权限时只给 read（历史行为是默认 read+write，
// 等于"什么都不选就拿到写权限"，对推送文档这种高权限接口风险过大）；
// 未识别的取值直接丢弃，避免拼错字符串反而保留在库里。
func normalizeScopes(in []string) []string {
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, s := range in {
		v := strings.ToLower(strings.TrimSpace(s))
		if v != "read" && v != "write" {
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{"read"}
	}
	return out
}

// CreateToken 创建 Token（返回明文 token 仅此一次）
func (s *KnowledgeMerchantService) CreateToken(ctx context.Context, req *CreateTokenRequest) (*model.KnowledgeAPIToken, error) {
	if req.Name == "" {
		return nil, errors.New("name 不能为空")
	}
	if req.ProductID == "" {
		return nil, errors.New("product_id 不能为空")
	}
	s.ensureReposFromDB()
	plain, err := generateToken()
	if err != nil {
		return nil, err
	}
	hashed := hashToken(plain)
	scopes := normalizeScopes(req.Scopes)
	scopesJSON, _ := json.Marshal(scopes)
	tok := &model.KnowledgeAPIToken{
		Name:       req.Name,
		Token:      hashed,
		TokenPlain: plain,
		Scopes:     string(scopesJSON),
		ProductID:  req.ProductID,
		Enabled:    1,
		ExpiresAt:  req.ExpiresAt,
		CreatedBy:  req.CreatedBy,
	}
	if err := s.tokenRepo.Create(ctx, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// ListTokens 列出 Token
func (s *KnowledgeMerchantService) ListTokens(ctx context.Context, productID string) ([]model.KnowledgeAPIToken, error) {
	s.ensureReposFromDB()
	list, err := s.tokenRepo.ListByProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		list[i].Token = ""
		list[i].TokenPlain = ""
	}
	return list, nil
}

// RevokeToken 吊销 Token
func (s *KnowledgeMerchantService) RevokeToken(ctx context.Context, id uint64) error {
	s.ensureReposFromDB()
	return s.tokenRepo.DisableByID(ctx, id, 0)
}

// ValidateToken 校验 Token（外部系统使用）
func (s *KnowledgeMerchantService) ValidateToken(ctx context.Context, plain string) (*model.KnowledgeAPIToken, error) {
	if plain == "" {
		return nil, fmt.Errorf("%w: token 不能为空", utils.ErrUnauthorized)
	}
	if !s.dbReady() {
		return nil, errors.New("数据库未初始化")
	}
	s.ensureReposFromDB()
	hashed := hashToken(plain)
	// token 摘要索引是全局唯一的引导键：调用方是公开入口，ctx 里还没有租户；
	// 必须系统作用域反查，拿到 token 行自带的 tenant_id 后再绑定业务上下文。
	tok, err := s.tokenRepo.FindByToken(tenant.WithSystemScope(ctx), hashed)
	if err != nil {
		return nil, fmt.Errorf("%w: token 无效或不存在", utils.ErrUnauthorized)
	}
	if tok.Enabled != 1 {
		return nil, fmt.Errorf("%w: token 已禁用", utils.ErrUnauthorized)
	}
	if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("%w: token 已过期", utils.ErrUnauthorized)
	}
	tokID, tokTenant := tok.ID, tok.TenantID
	go func(id uint64, tenantID uint) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("knowledge_merchant: async IncrementUsage recovered from panic: %v\n%s", r, debug.Stack())
			}
		}()
		bg := context.Background()
		if tenantID != 0 {
			bg = tenant.WithTenantID(bg, tenantID)
		} else if !tenant.IsMultiTenant() {
			bg = tenant.ContextWithDefault(bg)
		}
		if err := s.tokenRepo.IncrementUsage(bg, id); err != nil {
			logger.Errorf("knowledge_merchant: async IncrementUsage failed, token_id=%d: %v", id, err)
		}
	}(tokID, tokTenant)
	return tok, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "kbg_" + hex.EncodeToString(b), nil
}

func hashToken(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

// GenerateToken 公开版 generateToken(供跨包测试使用)
func GenerateToken() (string, error) {
	return generateToken()
}

// HashToken 公开版 hashToken(供跨包测试使用)
func HashToken(plain string) string {
	return hashToken(plain)
}

func tokenHasScope(scopes string, target string) bool {
	var arr []string
	if err := json.Unmarshal([]byte(scopes), &arr); err != nil {
		return false
	}
	for _, s := range arr {
		if s == target || s == "*" {
			return true
		}
	}
	return false
}

// TokenHasScope 公开版 tokenHasScope(供跨包测试使用)
func TokenHasScope(scopes string, target string) bool {
	return tokenHasScope(scopes, target)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// BoolToInt 公开版 boolToInt(供跨包测试使用)
func BoolToInt(b bool) int {
	return boolToInt(b)
}
