package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"
	"hivemtk-user/internal/repository"
)

// mcp_key_admin.go MCP 凭证钥匙（1:N）生命周期与管理审计服务（L3 controller → L4 service → L5 repository）。
//
// 状态机（管理端与 guard 共用一套口径，字面量在 model 层）：
//
//	active ──disable──▶ disabled ──enable──▶ active
//	   │  ▲                  │
//	   └──┴────revoke───────▼──▶ revoked（终态，仅可删除）
//
// expired 不是存储状态：由 ExpiresAt 推导，guard 判过期优先于状态。
// 删除是物理删除（管理审计账本留痕），吊销是逻辑终态（保留行供审计对账）。

// 钥匙生命周期业务错误（controller 按类型映射 404/409/400）。
var (
	// ErrMCPKeyNotFound 钥匙不存在或不属于该凭证（越权按不存在处理，不泄露存在性）。
	ErrMCPKeyNotFound = errors.New("mcp key not found")
	// ErrMCPKeyRevokedTerminal 吊销是终态：只能重新签发，不能复活。
	ErrMCPKeyRevokedTerminal = errors.New("钥匙已停用（吊销为终态，不可重新启用）")
	// ErrMCPKeyNoPlaintext 迁移回填行没有明文，reveal 不可用。
	ErrMCPKeyNoPlaintext = errors.New("该密钥明文未存储（旧凭证迁移行），请新建密钥完成轮换")
	// ErrMCPKeyCapReached 单凭证钥匙数上限（防无限签发把审计表与缓存撑爆）。
	ErrMCPKeyCapReached = errors.New("该凭证名下密钥数量已达上限（20 把），请先删除不再使用的密钥")
	// ErrMCPKeyStatusInvalid 目标状态字面量非法（防脏值入库）。
	ErrMCPKeyStatusInvalid = errors.New("目标状态非法（active/disabled/revoked）")
)

// maxKeysPerCredential 单凭证名下钥匙上限。
const maxKeysPerCredential = 20

// McpKeyAdminService 钥匙生命周期管理。
type McpKeyAdminService struct {
	repo     *repository.MCPCredentialKeyRepo
	credRepo *repository.MCPCredentialRepo
}

func NewMcpKeyAdminService() *McpKeyAdminService {
	kr := repository.NewMCPCredentialKeyRepo()
	cr := repository.NewMCPCredentialRepo()
	if !kr.Available() || !cr.Available() {
		// 未装配：返回空服务，控制器 Ready()=false 回 503（分诊纪律同 McpCredentialAdminService）。
		return &McpKeyAdminService{}
	}
	return &McpKeyAdminService{repo: kr, credRepo: cr}
}

func (s *McpKeyAdminService) Ready() bool { return s.repo != nil && s.credRepo != nil }

// GetCredential 暴露凭证行（管理端归属展示与审计标签用；不筛 enabled）。
func (s *McpKeyAdminService) GetCredential(ctx context.Context, id uint) (*model.MCPCredential, error) {
	return s.credRepo.GetByIDAny(ctx, id)
}

// IssueKey 为凭证签发一把新钥匙，返回钥匙行与**只出现这一次**的明文。
// 明文落库两轨：sha256（校验）+ dbencrypt AES-GCM 密文（管理端显示/复制）。
// 第一把钥匙的 hash 同时镜像进凭证的 legacy 列（旧二进制回滚窗口的可用性）。
func (s *McpKeyAdminService) IssueKey(ctx context.Context, credentialID uint, name string, expiresAt *time.Time) (*model.MCPCredentialKey, string, error) {
	cred, err := s.credRepo.GetByIDAny(ctx, credentialID)
	if err != nil {
		return nil, "", fmt.Errorf("凭证不存在: %w", err)
	}
	cnt, err := s.repo.CountByCredential(ctx, credentialID)
	if err != nil {
		return nil, "", err
	}
	if cnt >= maxKeysPerCredential {
		return nil, "", ErrMCPKeyCapReached
	}
	plaintext, err := generateMCPKeyPlaintext()
	if err != nil {
		return nil, "", err
	}
	key := &model.MCPCredentialKey{
		CredentialID: credentialID,
		Name:         name,
		KeyHash:      mcpKeySha256Hex(plaintext),
		KeyEnc:       dbencrypt.Encrypt(plaintext),
		KeyPrefix:    plaintext[:12],
		KeySuffix:    plaintext[len(plaintext)-4:],
		Status:       model.MCPKeyStatusActive,
		ExpiresAt:    expiresAt,
	}
	if err := s.repo.Create(ctx, key); err != nil {
		return nil, "", err
	}
	// legacy 镜像：仅当凭证还没有基准 hash 时写入（1:N 之后不再主动维护本列，
	// 清空时机见 DeleteKey——"签发成功=回滚后的旧二进制也能认"只对第一把钥匙成立）。
	if cred.APIKeyHash == "" {
		_ = s.credRepo.Update(ctx, credentialID, map[string]any{"api_key_hash": key.KeyHash})
	}
	return key, plaintext, nil
}

// ListByCredential 凭证名下全部钥匙（管理端列表；掩码形由 model.MaskedKey 组装）。
func (s *McpKeyAdminService) ListByCredential(ctx context.Context, credentialID uint) ([]model.MCPCredentialKey, error) {
	return s.repo.ListByCredential(ctx, credentialID)
}

// GetForCredential 取一把钥匙，校验归属（不属于该凭证 = 不存在）。
func (s *McpKeyAdminService) GetForCredential(ctx context.Context, credentialID, keyID uint) (*model.MCPCredentialKey, error) {
	key, err := s.repo.GetByID(ctx, keyID)
	if err != nil || key.CredentialID != credentialID {
		return nil, ErrMCPKeyNotFound
	}
	return key, nil
}

// SetKeyStatus 状态流转。no-op（重复 enable/disable/revoke 同状态）按成功返回，
// 管理端重复点击不必报错；唯一硬约束是 revoked 不可离开（终态语义）。
func (s *McpKeyAdminService) SetKeyStatus(ctx context.Context, credentialID, keyID uint, to string) (*model.MCPCredentialKey, error) {
	if !model.MCPKeyStatusValid(to) {
		return nil, ErrMCPKeyStatusInvalid
	}
	key, err := s.GetForCredential(ctx, credentialID, keyID)
	if err != nil {
		return nil, err
	}
	if key.Status == model.MCPKeyStatusRevoked && to != model.MCPKeyStatusRevoked {
		return nil, ErrMCPKeyRevokedTerminal
	}
	if key.Status == to {
		return key, nil
	}
	if err := s.repo.Update(ctx, keyID, map[string]any{"status": to}); err != nil {
		return nil, err
	}
	key.Status = to
	return key, nil
}

// DeleteKey 物理删除一把钥匙。若删除的是 legacy 镜像基准行，同步清空凭证的
// api_key_hash——清空后回滚的旧二进制对该凭证 fail-closed（401），
// 好过放行一把已经删除的钥匙（fail-closed 是两害相权取其轻）。
func (s *McpKeyAdminService) DeleteKey(ctx context.Context, credentialID, keyID uint) (*model.MCPCredentialKey, error) {
	key, err := s.GetForCredential(ctx, credentialID, keyID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, keyID); err != nil {
		return nil, err
	}
	cred, err := s.credRepo.GetByIDAny(ctx, credentialID)
	if err == nil && cred.APIKeyHash == key.KeyHash {
		_ = s.credRepo.Update(ctx, credentialID, map[string]any{"api_key_hash": ""})
	}
	return key, nil
}

// RevealKey 解出明文钥匙（管理端"显示/复制"）。只有落库时存了密文的行可用；
// 迁移回填行（只有 sha256）永远返回 ErrMCPKeyNoPlaintext。
func (s *McpKeyAdminService) RevealKey(ctx context.Context, credentialID, keyID uint) (string, error) {
	key, err := s.GetForCredential(ctx, credentialID, keyID)
	if err != nil {
		return "", err
	}
	if key.KeyEnc == "" {
		return "", ErrMCPKeyNoPlaintext
	}
	plain := dbencrypt.Decrypt(key.KeyEnc)
	if plain == "" {
		// dbencrypt 永不返回空串给非空输入；走到这里说明密文被截断/损坏
		//（密钥轮换或半截数据），把垃圾还给前端没有意义，按不可 reveal 处理。
		return "", ErrMCPKeyNoPlaintext
	}
	return plain, nil
}

// generateMCPKeyPlaintext 生成 "wgk_" + 32hex 明文（与既有签发口径一致）。
func generateMCPKeyPlaintext() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "wgk_" + hex.EncodeToString(b), nil
}

func mcpKeySha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// MCPCredentialAuditLogAdminService 管理面操作审计（记账 + 分页查询）。
type MCPCredentialAuditLogAdminService struct {
	repo *repository.MCPCredentialAuditLogRepo
}

func NewMCPCredentialAuditLogAdminService() *MCPCredentialAuditLogAdminService {
	r := repository.NewMCPCredentialAuditLogRepo()
	if !r.Available() {
		return &MCPCredentialAuditLogAdminService{}
	}
	return &MCPCredentialAuditLogAdminService{repo: r}
}

func (s *MCPCredentialAuditLogAdminService) Ready() bool { return s.repo != nil }

// Record 追加一条管理审计（best-effort：记账失败不影响管理操作本身）。
func (s *MCPCredentialAuditLogAdminService) Record(ctx context.Context, row *model.MCPCredentialAuditLog) {
	if s.repo == nil {
		return
	}
	_ = s.repo.Create(ctx, row)
}

func (s *MCPCredentialAuditLogAdminService) List(ctx context.Context, action, targetType string, credentialID *uint, page, size int) ([]model.MCPCredentialAuditLog, int64, error) {
	return s.repo.List(ctx, action, targetType, credentialID, page, size)
}
