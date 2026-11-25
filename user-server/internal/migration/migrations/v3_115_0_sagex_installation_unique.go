package migrations

import (
	"context"
	"fmt"
	"strings"

	"hivemtk-user/internal/migration"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// SageXInstallationUniqueMigration v3.115.0：为非空安装标识建立租户内唯一键。
// 重复存量数据必须先由运维修复；迁移只体检并 fail-closed，不静默删改数据。
type SageXInstallationUniqueMigration struct {
	db           *gorm.DB
	createdIndex bool
}

var _ migration.Migration = (*SageXInstallationUniqueMigration)(nil)

func NewSageXInstallationUniqueMigration(db *gorm.DB) *SageXInstallationUniqueMigration {
	return &SageXInstallationUniqueMigration{db: db}
}

func (*SageXInstallationUniqueMigration) Version() string { return "v3.115.0" }

func (*SageXInstallationUniqueMigration) Name() string {
	return "SageX 安装标识租户内唯一"
}

func (*SageXInstallationUniqueMigration) Description() string {
	return "对非空 (tenant_id, installation_id) 建 partial unique index；存量重复 fail-closed，幂等可重入；Down 仅撤销本次实际新建的索引"
}

func (m *SageXInstallationUniqueMigration) Up(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("sagex installation unique migration: db is nil")
	}
	db := m.db.WithContext(tenant.WithSystemScope(ctx))
	if !db.Migrator().HasTable("sagex_installations") {
		return nil
	}
	preexistingIndex := db.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id")
	return db.Transaction(func(tx *gorm.DB) error {
		var duplicates []struct {
			TenantID       uint
			InstallationID string
			Count          int64
		}
		if err := tx.Raw(`
			SELECT tenant_id, installation_id, COUNT(*) AS count
			FROM sagex_installations
			WHERE installation_id <> ''
			GROUP BY tenant_id, installation_id
			HAVING COUNT(*) > 1
			ORDER BY tenant_id, installation_id
			LIMIT 10
		`).Scan(&duplicates).Error; err != nil {
			return fmt.Errorf("sagex installation duplicate preflight: %w", err)
		}
		if len(duplicates) > 0 {
			details := make([]string, 0, len(duplicates))
			for _, duplicate := range duplicates {
				details = append(details, fmt.Sprintf(
					"tenant_id=%d installation_id=%q count=%d",
					duplicate.TenantID,
					duplicate.InstallationID,
					duplicate.Count,
				))
			}
			return fmt.Errorf(
				"sagex installation duplicate data blocks unique index (operator remediation required): %s",
				strings.Join(details, "; "),
			)
		}
		if preexistingIndex {
			return nil
		}
		if tx.Migrator().HasIndex("sagex_installations", "uq_sagex_installation_tenant_id") {
			return nil
		}
		if err := tx.Exec(`
			CREATE UNIQUE INDEX uq_sagex_installation_tenant_id
			ON sagex_installations (tenant_id, installation_id)
			WHERE installation_id <> ''
		`).Error; err != nil {
			return fmt.Errorf("create sagex installation unique index: %w", err)
		}
		m.createdIndex = true
		return nil
	})
}

func (m *SageXInstallationUniqueMigration) Down(ctx context.Context) error {
	if m.db == nil {
		return fmt.Errorf("sagex installation unique migration: db is nil")
	}
	db := m.db.WithContext(tenant.WithSystemScope(ctx))
	if !db.Migrator().HasTable("sagex_installations") || !m.createdIndex {
		return nil
	}
	if err := db.Exec(`DROP INDEX IF EXISTS uq_sagex_installation_tenant_id`).Error; err != nil {
		return fmt.Errorf("drop sagex installation unique index: %w", err)
	}
	m.createdIndex = false
	return nil
}
