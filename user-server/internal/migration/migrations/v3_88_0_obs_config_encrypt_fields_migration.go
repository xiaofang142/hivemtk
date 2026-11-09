package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbencrypt"

	"gorm.io/gorm"
)

// ObsConfigEncryptFieldsMigration ：obs_config 凭据列（access_key / secret_key）
// 落库加密配套的列宽扩展 + 存量明文回填。
//
// dbencrypt.Encrypt 产出 `enc:v1:{base64(AES-256-GCM)}` 密文：AK/SK 最长 255
// 字符时密文约 380 字符，超过 varchar(255)；与 operation_logs/v3_41_0 同策略扩列至 text。
//
// 存量明文行**回填**（与 v3_41_0 不同）：凭据是密钥级数据，留明文行等于把历史
// 密钥抄进运维只读视图；回填在 MASTER_KEY 未就绪时由 dbencrypt 降级写明文并
// warn 一次（等于暂不回填），dbencrypt.Decrypt 对非 enc:v1: 前缀原样透传，
// 双轨读取兜底，运维补齐密钥后新写即密文，未回填历史行不会读坏。
type ObsConfigEncryptFieldsMigration struct {
	db *gorm.DB
}

func NewObsConfigEncryptFieldsMigration(db *gorm.DB) *ObsConfigEncryptFieldsMigration {
	return &ObsConfigEncryptFieldsMigration{db: db}
}

func (m *ObsConfigEncryptFieldsMigration) Version() string { return "v3.88.0" }

func (m *ObsConfigEncryptFieldsMigration) Name() string {
	return "obs_config access_key/secret_key 扩列至 text 并回填密文"
}

func (m *ObsConfigEncryptFieldsMigration) Description() string {
	return "OPT-SEC: obs_config.access_key/secret_key varchar(255) 扩展为 text 容纳 enc:v1 AES-GCM 密文，并回填存量明文凭据"
}

func (m *ObsConfigEncryptFieldsMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("obs_config") {
		return nil
	}
	for _, col := range []string{"access_key", "secret_key"} {
		if !m.db.Migrator().HasColumn("obs_config", col) {
			continue
		}
		sql := fmt.Sprintf("ALTER TABLE obs_config ALTER COLUMN %s TYPE text", col)
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("扩展 obs_config.%s 为 text 失败: %w", col, err)
		}
	}

	// 存量明文回填：任一列已是 enc:v1: 的行（双轨写入期）按列各自判断，
	// NOT LIKE 谓词天然跳过已密文列，不重复加密。
	var configs []model.ObsConfig
	if err := m.db.WithContext(ctx).
		Where("access_key NOT LIKE ?", "enc:v1:%").
		Or("secret_key NOT LIKE ?", "enc:v1:%").
		Find(&configs).Error; err != nil {
		return fmt.Errorf("查询 obs_config 存量明文凭据失败: %w", err)
	}
	for i := range configs {
		configs[i].AccessKey = dbencrypt.Encrypt(configs[i].AccessKey)
		configs[i].SecretKey = dbencrypt.Encrypt(configs[i].SecretKey)
		if err := m.db.WithContext(ctx).Model(&model.ObsConfig{}).
			Where("id = ?", configs[i].ID).
			Updates(map[string]any{
				"access_key": configs[i].AccessKey,
				"secret_key": configs[i].SecretKey,
			}).Error; err != nil {
			return fmt.Errorf("回填 obs_config %s 凭据密文失败: %w", configs[i].ID, err)
		}
	}
	return nil
}

func (m *ObsConfigEncryptFieldsMigration) Down(ctx context.Context) error {
	// 不回滚：列宽放宽无害；密文明文化会把密钥重新铺回库内。
	return nil
}

var _ migration.Migration = (*ObsConfigEncryptFieldsMigration)(nil)
