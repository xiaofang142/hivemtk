package migrations

import (
	"context"
	"fmt"

	"hivemtk-user/internal/migration"

	"gorm.io/gorm"
)

// OperationLogEncryptFieldsMigration OPT-SEC-04：operation_logs 敏感字段（IP / User-Agent）
// 落库加密配套的列宽扩展。
//
// dbencrypt.Encrypt 产出 `enc:v1:{base64(AES-256-GCM)}` 密文：
//   - IPv6 明文最长 45 字符 → 密文约 109 字符，远超原 varchar(50)；
//   - User-Agent 明文上限 255 → 密文约 380 字符，超过 varchar(255)。
//
// 若不扩列，配置 MASTER_KEY 的环境写入会触发 SQLSTATE 22001（值过长）。
// 这里把两列放宽到 text（PostgreSQL 变长无上限），与 api_logs 的加密列同处理策略。
//
// 存量明文行无需回填：dbencrypt.Decrypt 对非 enc:v1: 前缀原样透传，
// 双轨（明文 + 密文）在读取出口统一归一。
type OperationLogEncryptFieldsMigration struct {
	db *gorm.DB
}

// NewOperationLogEncryptFieldsMigration 创建迁移实例
func NewOperationLogEncryptFieldsMigration(db *gorm.DB) *OperationLogEncryptFieldsMigration {
	return &OperationLogEncryptFieldsMigration{db: db}
}

// Version 返回版本号
func (m *OperationLogEncryptFieldsMigration) Version() string { return "v3.41.0" }

// Name 返回迁移名称
func (m *OperationLogEncryptFieldsMigration) Name() string {
	return "operation_logs ip/user_agent 扩列至 text（加密前置）"
}

// Description 返回迁移描述
func (m *OperationLogEncryptFieldsMigration) Description() string {
	return "OPT-SEC-04: operation_logs.ip varchar(50)、user_agent varchar(255) 扩展为 text，容纳 enc:v1 AES-GCM 密文"
}

// Up 执行升级
func (m *OperationLogEncryptFieldsMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("db is nil")
	}
	if !m.db.Migrator().HasTable("operation_logs") {
		return nil
	}
	cols := []string{"ip", "user_agent"}
	for _, col := range cols {
		if !m.db.Migrator().HasColumn("operation_logs", col) {
			continue
		}
		sql := fmt.Sprintf("ALTER TABLE operation_logs ALTER COLUMN %s TYPE text", col)
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("扩展 operation_logs.%s 为 text 失败: %w", col, err)
		}
	}
	return nil
}

// Down 执行降级（不收缩列，避免截断已存密文/明文数据）
func (m *OperationLogEncryptFieldsMigration) Down(ctx context.Context) error {
	return nil
}

var _ migration.Migration = (*OperationLogEncryptFieldsMigration)(nil)
