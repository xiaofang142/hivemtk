package service

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/aiagent/knowledge/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/secrets"
)

// 知识库外部导入凭证（SaaS 配置面改造 B4，2026-11-17）。
//
// FEISHU_APP_ID/FEISHU_APP_SECRET/NOTION_API_KEY 环境变量已退役：
// 它们是全局单值，SaaS 多租户下既装不下「每个商户各自的飞书/Notion 应用」，
// 也会把 A 租户的凭证漏给 B 租户。唯一来源改为租户级 system_config_kv
// （key = knowledge.import_credentials，租户归属由隔离 callback 限定），
// 凭证字段经 secrets 包 AES-256-GCM 加密落库（与 ToolIntegrationConfig 同一三态口径）。
//
// 读写入口：GET/PUT /api/knowledge-merchant/external/credentials（租户管理员）。

// kvKnowledgeImportCredentials 租户 KV 存储键。
const kvKnowledgeImportCredentials = "knowledge.import_credentials"

func knowledgeImportCredentialContext(ctx context.Context) (context.Context, error) {
	if ctx == nil || tenant.IsSystemScope(ctx) {
		return nil, errors.New("knowledge import credentials require a tenant context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := tenant.WriteTenantID(ctx)
	if id == 0 {
		return nil, errors.New("knowledge import credentials require a confirmed tenant")
	}
	return tenant.OnlyTenant(ctx, id), nil
}

// KnowledgeImportCredentials 外部导入凭证聚合体。
type KnowledgeImportCredentials struct {
	FeishuAppID     string `json:"feishu_app_id"`
	FeishuAppSecret string `json:"feishu_app_secret"`
	NotionAPIKey    string `json:"notion_api_key"`
}

// credentialFields 返回需要加密的凭证字段（AppID 是公开标识，不加密）。
func (c *KnowledgeImportCredentials) credentialFields() []*string {
	return []*string{&c.FeishuAppSecret, &c.NotionAPIKey}
}

func (c *KnowledgeImportCredentials) hasPlaintextCredentials() bool {
	for _, p := range c.credentialFields() {
		if *p != "" && !secrets.IsCiphertextFormat(*p) {
			return true
		}
	}
	return false
}

// LoadKnowledgeImportCredentials 读取当前租户的导入凭证（凭证字段透明解密）。
// 没配过返回全零值结构，不返回 error —— 调用方按空值给「未配置」业务错误。
func LoadKnowledgeImportCredentials(ctx context.Context) (*KnowledgeImportCredentials, error) {
	cfg := &KnowledgeImportCredentials{}
	ctx, err := knowledgeImportCredentialContext(ctx)
	if err != nil {
		return cfg, err
	}
	raw, err := repository.NewImportCredentialsRepository().Get(ctx, kvKnowledgeImportCredentials)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return cfg, err
	}

	if !secrets.Ready() {
		return cfg, nil // MASTER_KEY 缺失：按明文处理（与 tool_integration 同口径，secrets 包已有启动 WARN）
	}
	for _, p := range cfg.credentialFields() {
		if *p == "" || !secrets.IsCiphertextFormat(*p) {
			continue
		}
		plain, derr := secrets.DecryptString(*p)
		if derr != nil {
			continue // 解密失败回退原值（密钥轮换过渡期），不阻断读取
		}
		*p = plain
	}
	if cfg.hasPlaintextCredentials() {
		_ = saveEncryptedKnowledgeImportCredentials(ctx, cfg) // 明文透明升级，失败不阻断读取
	}
	return cfg, nil
}

// SaveKnowledgeImportCredentials 写入当前租户的导入凭证（凭证字段加密落库）。
func SaveKnowledgeImportCredentials(ctx context.Context, cfg *KnowledgeImportCredentials) error {
	return saveEncryptedKnowledgeImportCredentials(ctx, cfg)
}

func saveEncryptedKnowledgeImportCredentials(ctx context.Context, cfg *KnowledgeImportCredentials) error {
	ctx, err := knowledgeImportCredentialContext(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("knowledge import credentials cannot be nil")
	}
	store := *cfg
	if secrets.Ready() {
		for _, p := range store.credentialFields() {
			if *p == "" || secrets.IsCiphertextFormat(*p) {
				continue
			}
			e, err := secrets.EncryptString(*p)
			if err != nil {
				return err
			}
			*p = e
		}
	}
	raw, err := json.Marshal(&store)
	if err != nil {
		return err
	}
	return repository.NewImportCredentialsRepository().Upsert(ctx, kvKnowledgeImportCredentials, string(raw))
}
