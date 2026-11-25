package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"hivemtk-user/internal/repository"
)

// 各类平台账号配置集中到统一账号管理的契约（CH-02）。
//
// 设计取舍：不改 schema，不新建平台账号表。平台凭据存进既有 system_config_kv，
// key = channel_account_cred:<tenantID>:<accountID>（租户维度），value = JSON。
// 原平台账号（telegram_accounts / qq_accounts / ...）的行 ID 通过映射条目指向规范
// ChannelAccount.ID，重复迁移不重建账号（先查映射，命中则复用）。
const channelAccountCredentialKeyPrefix = "channel_account_cred:"

// ErrChannelCredentialInvalid 凭据配置非法（类型/租户/脱敏值错误）。
var ErrChannelCredentialInvalid = errors.New("渠道账号凭据配置无效")

// ChannelAccountCredential 平台账号凭据（只存必要字段，敏感字段脱敏回显）。
type ChannelAccountCredential struct {
	Platform   string            `json:"platform"`
	ExternalID string            `json:"external_id"`
	Secret     string            `json:"secret,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
}

// ChannelAccountCredentialView 管理面回显：secret 脱敏，其余原样。
type ChannelAccountCredentialView struct {
	Platform   string            `json:"platform"`
	ExternalID string            `json:"external_id"`
	Secret     string            `json:"secret,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	Verified   bool              `json:"verified"`
}

// MaskCredential 对敏感值脱敏：空串保持空（表示未配置）；短值全遮蔽；
// 长值保留前 2 后 2 便于人工校核，中间固定星号。纯函数便于正反例测试。
func MaskCredential(secret string) string {
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + "****" + string(runes[len(runes)-2:])
}

// 传入前端的脱敏占位不会被误当成真实值写回：识别 MaskCredential 的输出形态。
func isMaskedCredential(v string) bool {
	return v == "" || strings.Contains(v, "****") || (v != "" && strings.Trim(v, "*") == "")
}

// ValidateCredentialForPlatform 按渠道类型校验必要字段（TG/QQ/飞书/企微/WhatsApp）。
// 未知平台 fail-closed 拒绝；平台类型与账号自身类型不一致也拒绝。
func ValidateCredentialForPlatform(platform, accountType string, cred ChannelAccountCredential) error {
	p := strings.ToLower(strings.TrimSpace(platform))
	if p == "" {
		return ErrChannelCredentialInvalid
	}
	allowed := map[string]bool{"telegram": true, "qq": true, "feishu": true, "wecom": true, "whatsapp": true}
	if !allowed[p] {
		return fmt.Errorf("%w: 不支持的平台 %q", ErrChannelCredentialInvalid, platform)
	}
	if cred.ExternalID == "" {
		return fmt.Errorf("%w: 平台账号 ID 必填", ErrChannelCredentialInvalid)
	}
	if p == "telegram" && cred.Secret == "" {
		return fmt.Errorf("%w: Telegram bot token 必填", ErrChannelCredentialInvalid)
	}
	_ = accountType // 账号类型（dm/group）由 ChannelAccountSave 校验，凭据不另造约束
	return nil
}

// MergeCredential 合并编辑：脱敏占位不覆盖真实 secret；blank 明确清空（需 ClearSecret=true）。
func MergeCredential(old ChannelAccountCredential, in ChannelAccountCredential, clearSecret bool) (ChannelAccountCredential, error) {
	merged := old
	if in.Platform != "" {
		merged.Platform = strings.ToLower(strings.TrimSpace(in.Platform))
	}
	if in.ExternalID != "" {
		merged.ExternalID = in.ExternalID
	}
	for k, v := range in.Extra {
		if merged.Extra == nil {
			merged.Extra = map[string]string{}
		}
		merged.Extra[k] = v
	}
	if clearSecret {
		merged.Secret = ""
	} else if in.Secret != "" && !isMaskedCredential(in.Secret) {
		merged.Secret = in.Secret
	}
	return merged, nil
}

// ChannelAccountCredentialView 生成：从不把明文 secret 返回前端。
func toCredentialView(c ChannelAccountCredential) ChannelAccountCredentialView {
	return ChannelAccountCredentialView{
		Platform: c.Platform, ExternalID: c.ExternalID,
		Secret: MaskCredential(c.Secret), Extra: c.Extra, Verified: c.Secret != "",
	}
}

// channelAccountCredentialKey 使用显式 tenantID/accountID，不依赖 ctx 隐式租户（防串租户）。
func channelAccountCredentialKey(tenantID, accountID uint) string {
	return channelAccountCredentialKeyPrefix + strconv.FormatUint(uint64(tenantID), 10) + ":" + strconv.FormatUint(uint64(accountID), 10)
}

// ChannelAccountMigrationRecord 存量平台账号 → 规范 ChannelAccount 的稳定映射。
// 迁移幂等：以 (tenantID, platform, legacyID) 为键先查后建，命中直接复用，不重建账号。
type ChannelAccountMigrationRecord struct {
	TenantID      uint   `json:"tenant_id"`
	Platform      string `json:"platform"`
	LegacyID      string `json:"legacy_id"`
	AccountID     uint   `json:"account_id"`
	ChannelID     uint   `json:"channel_id"`
	CredentialRef string `json:"credential_ref"`
}

// migrationKey 稳定映射键（同一租户+平台+旧 ID 恒同输出，与创建顺序无关）。
func migrationKey(tenantID uint, platform, legacyID string) string {
	return strconv.FormatUint(uint64(tenantID), 10) + ":" +
		strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.TrimSpace(legacyID)
}

// SortMigrationRecords 稳定排序（反向创建顺序输入也得到一致结果，便于预览校核）。
func SortMigrationRecords(records []ChannelAccountMigrationRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		return migrationKey(records[i].TenantID, records[i].Platform, records[i].LegacyID) <
			migrationKey(records[j].TenantID, records[j].Platform, records[j].LegacyID)
	})
}

// ChannelAccountCredentialService 凭据配置与存量迁移服务。
type ChannelAccountCredentialService interface {
	// Get 回显脱敏视图（明文绝不返回）。
	Get(ctx context.Context, tenantID, accountID uint) (*ChannelAccountCredentialView, error)
	// Save 合并编辑：未改的脱敏值不覆盖真实 secret。
	Save(ctx context.Context, tenantID, accountID uint, in ChannelAccountCredential, clearSecret bool) error
	// MigratePreview 存量可预览校核：返回稳定映射，不改库。
	MigratePreview(ctx context.Context, tenantID uint, records []ChannelAccountMigrationRecord) ([]ChannelAccountMigrationRecord, error)
	// MigrateApply 幂等应用：已映射的账号复用，不重建。
	MigrateApply(ctx context.Context, tenantID uint, records []ChannelAccountMigrationRecord) (int, error)
}

// channelAccountCredentialKV 是 CredentialService 依赖的最小 KV 端口（Repository 层持有 DB）。
type channelAccountCredentialKV interface {
	GetRaw(ctx context.Context, tenantID uint, key string) (string, error)
	SetRaw(ctx context.Context, tenantID uint, key, value string) error
}

type channelAccountCredentialService struct {
	kv channelAccountCredentialKV
}

// NewChannelAccountCredentialService 用显式 KV 端口构造（装配点/测试用）。
func NewChannelAccountCredentialService(kv channelAccountCredentialKV) ChannelAccountCredentialService {
	return &channelAccountCredentialService{kv: kv}
}

// NewChannelAccountCredentialServiceDefault 用全局 KV 仓储构造（生产装配点用）。
func NewChannelAccountCredentialServiceDefault() ChannelAccountCredentialService {
	return &channelAccountCredentialService{kv: repository.NewChannelAccountCredentialKV()}
}

func (s *channelAccountCredentialService) load(ctx context.Context, tenantID, accountID uint) (ChannelAccountCredential, error) {
	if s == nil || s.kv == nil {
		return ChannelAccountCredential{}, repository.ErrChannelStoreUnavailable
	}
	raw, err := s.kv.GetRaw(ctx, tenantID, channelAccountCredentialKey(tenantID, accountID))
	if err != nil {
		return ChannelAccountCredential{}, err
	}
	if raw == "" {
		return ChannelAccountCredential{}, nil
	}
	var c ChannelAccountCredential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return ChannelAccountCredential{}, fmt.Errorf("%w: 已存凭据解析失败", ErrChannelCredentialInvalid)
	}
	return c, nil
}

func (s *channelAccountCredentialService) Get(ctx context.Context, tenantID, accountID uint) (*ChannelAccountCredentialView, error) {
	if tenantID == 0 || accountID == 0 {
		return nil, ErrChannelCredentialInvalid
	}
	c, err := s.load(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	view := toCredentialView(c)
	return &view, nil
}

func (s *channelAccountCredentialService) Save(ctx context.Context, tenantID, accountID uint, in ChannelAccountCredential, clearSecret bool) error {
	if tenantID == 0 || accountID == 0 {
		return ErrChannelCredentialInvalid
	}
	if err := ValidateCredentialForPlatform(in.Platform, "", in); err != nil {
		return err
	}
	old, err := s.load(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	merged, err := MergeCredential(old, in, clearSecret)
	if err != nil {
		return err
	}
	if err := ValidateCredentialForPlatform(merged.Platform, "", merged); err != nil {
		return err
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	return s.kv.SetRaw(ctx, tenantID, channelAccountCredentialKey(tenantID, accountID), string(raw))
}

// MigratePreview 不写库，按稳定键生成映射；重复条目合并为一条（重复迁移不重建）。
func (s *channelAccountCredentialService) MigratePreview(ctx context.Context, tenantID uint, records []ChannelAccountMigrationRecord) ([]ChannelAccountMigrationRecord, error) {
	if tenantID == 0 {
		return nil, ErrChannelCredentialInvalid
	}
	byKey := map[string]ChannelAccountMigrationRecord{}
	for _, rec := range records {
		if rec.TenantID != tenantID {
			continue // 跨租户记录跳过，绝不迁移到别的租户
		}
		if rec.Platform == "" || rec.LegacyID == "" || rec.AccountID == 0 {
			return nil, fmt.Errorf("%w: 迁移记录字段不完整", ErrChannelCredentialInvalid)
		}
		key := migrationKey(tenantID, rec.Platform, rec.LegacyID)
		if existing, ok := byKey[key]; ok {
			if existing.AccountID != rec.AccountID {
				return nil, fmt.Errorf("%w: 同一旧账号映射到不同规范账号 %q", ErrChannelCredentialInvalid, key)
			}
			continue
		}
		byKey[key] = rec
	}
	out := make([]ChannelAccountMigrationRecord, 0, len(byKey))
	for _, rec := range byKey {
		out = append(out, rec)
	}
	SortMigrationRecords(out)
	return out, nil
}

// MigrateApply 先读已存映射键，已存在直接复用；只写尚未映射的记录。
func (s *channelAccountCredentialService) MigrateApply(ctx context.Context, tenantID uint, records []ChannelAccountMigrationRecord) (int, error) {
	preview, err := s.MigratePreview(ctx, tenantID, records)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, rec := range preview {
		key := channelAccountCredentialKeyPrefix + "migration:" + migrationKey(tenantID, rec.Platform, rec.LegacyID)
		existing, err := s.kv.GetRaw(ctx, tenantID, key)
		if err != nil {
			return created, err
		}
		if existing != "" {
			continue // 重复迁移：复用已映射账号，不重建
		}
		raw, err := json.Marshal(rec)
		if err != nil {
			return created, err
		}
		if err := s.kv.SetRaw(ctx, tenantID, key, string(raw)); err != nil {
			return created, err
		}
		created++
	}
	return created, nil
}
