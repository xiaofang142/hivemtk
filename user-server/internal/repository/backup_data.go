package repository

import (
	"context"
	"encoding/json"
	"fmt"

	_db "hivemtk-user/internal/pkg/db"

	"gorm.io/gorm"
)

type BackupDataRepository interface {
	DumpClues(ctx context.Context) (json.RawMessage, error)
	DumpUsers(ctx context.Context, limit, offset int) (json.RawMessage, error)
	DumpShortLinks(ctx context.Context, limit, offset int) (json.RawMessage, error)
	DumpTable(ctx context.Context, table string) (json.RawMessage, error)
	ClueExists(ctx context.Context, tenantID uint, id string) (bool, error)
	RestoreClue(ctx context.Context, row map[string]any) error
	UserExistsByUsername(ctx context.Context, tenantID uint, username string) (bool, error)
	RestoreUser(ctx context.Context, row map[string]any) error
	ShortLinkExistsByCode(ctx context.Context, tenantID uint, code string) (bool, error)
	RestoreShortLink(ctx context.Context, row map[string]any) error
	RestoreTable(ctx context.Context, tableName string, rows []map[string]any) error
}

type backupDataRepo struct {
	db *gorm.DB
}

func NewBackupDataRepository() BackupDataRepository {
	return &backupDataRepo{db: _db.GetDB()}
}

// NewBackupDataRepositoryWithDB 通过 *gorm.DB 创建(用于测试)
func NewBackupDataRepositoryWithDB(gormDB *gorm.DB) BackupDataRepository {
	return &backupDataRepo{db: gormDB}
}

func (r *backupDataRepo) DumpClues(ctx context.Context) (json.RawMessage, error) {
	type clueRow struct {
		TenantID uint   `json:"tenant_id"`
		ID       string `json:"id"`
		SourceID string `json:"source_id"`
		Account  string `json:"account"`
		Type     int64  `json:"type"`
		IsVerify int64  `json:"is_verify"`
		Name     string `json:"name"`
		City     string `json:"city"`
		Address  string `json:"address"`
		Desc     string `json:"desc"`
	}
	// G-6：分批拉取（对齐 DumpUsers 的 limit/offset 口径）。clues 全表单次 Find
	// 会随数据增长变内存炸弹；批大小 1000，空批即终止，签名与返回口径不变。
	const batchSize = 1000
	rows := make([]clueRow, 0, batchSize)
	for offset := 0; ; offset += batchSize {
		var batch []clueRow
		if err := r.db.WithContext(ctx).Table("clues").Order("id ASC").Limit(batchSize).Offset(offset).Find(&batch).Error; err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(batch) < batchSize {
			break
		}
	}
	return json.Marshal(rows)
}

func (r *backupDataRepo) DumpUsers(ctx context.Context, limit, offset int) (json.RawMessage, error) {
	type userRow struct {
		TenantID uint   `json:"tenant_id"`
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	var rows []userRow
	if err := r.db.WithContext(ctx).Table("user").
		Order("id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, err
	}
	return json.Marshal(rows)
}

func (r *backupDataRepo) DumpShortLinks(ctx context.Context, limit, offset int) (json.RawMessage, error) {
	type row struct {
		TenantID    uint   `json:"tenant_id"`
		ID          uint   `json:"id"`
		ShortCode   string `json:"short_code"`
		OriginalURL string `gorm:"column:original_url" json:"original_url"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).Table("short_links").
		Order("id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, err
	}
	return json.Marshal(rows)
}

// DumpTable 通用整表导出（按 id 升序单次查询）。扩表均为配置/记录类小表，
// 整表读入可接受；users 等大表不走此路径（仍有专用分页 dumper）。
func (r *backupDataRepo) DumpTable(ctx context.Context, table string) (json.RawMessage, error) {
	if !allowedBackupTables[table] {
		return nil, fmt.Errorf("表 %s 不在备份白名单内", table)
	}
	var rows []map[string]any
	if err := r.db.WithContext(ctx).Table(table).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(rows)
}

func (r *backupDataRepo) ClueExists(ctx context.Context, tenantID uint, id string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Table("clues").
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *backupDataRepo) RestoreClue(ctx context.Context, row map[string]any) error {
	return r.db.WithContext(ctx).Table("clues").Create(row).Error
}

func (r *backupDataRepo) UserExistsByUsername(ctx context.Context, tenantID uint, username string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Table("user").
		Where("tenant_id = ? AND username = ?", tenantID, username).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *backupDataRepo) RestoreUser(ctx context.Context, row map[string]any) error {
	return r.db.WithContext(ctx).Table("user").Create(row).Error
}

func (r *backupDataRepo) ShortLinkExistsByCode(ctx context.Context, tenantID uint, code string) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Table("short_links").
		Where("tenant_id = ? AND short_code = ?", tenantID, code).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *backupDataRepo) RestoreShortLink(ctx context.Context, row map[string]any) error {
	return r.db.WithContext(ctx).Table("short_links").Create(row).Error
}

// allowedBackupTables 备份/恢复白名单，key 为数据库真实表名。
// 与 service.backupExtraTables 同源（导出/恢复两端共用一份键→表清单），
// 任何一端新增表必须同步另一端，否则恢复端 fail-closed 探针会拦下残缺备份。
var allowedBackupTables = map[string]bool{
	"user_mfa":                true,
	"obs_config":              true,
	"email_accounts":          true,
	"email_jobs":              true,
	"customer_do_not_contact": true,
	"system_config":           true,
	"webhook_subscriptions":   true,
	"password_history":        true,
	"customer_sessions":       true,
	"message":                 true,
	"csat_surveys":            true,
	"customers":               true,
	"agent_statuses":          true,
	"alert_rules":             true,
	"automation_rules":        true,
	"bridge_accounts":         true,
	"operation_logs":          true,
	"login_events":            true,
	"security_alerts":         true,
	"password_reset_tokens":   true,
	"prompts":                 true,
	"ai_agents":               true,
	"feature_flags":           true,
}

// RestoreTable 恢复单表：DELETE 备份覆盖到的租户 + 逐行 Create 包在同一个事务里，
// 任一行失败整体回滚，绝不留下"清空了但没恢复完"的半恢复状态。
//
// DELETE 必须带租户范围：这里跑在 SystemScope 下，无 WHERE 的整表清空会连
// 备份未覆盖的租户一起清掉。备份来自旧版本时行内可能没有 tenant_id，
// 这类行按默认租户（1）归属，与 Create 落库时的列默认值一致。
func (r *backupDataRepo) RestoreTable(ctx context.Context, tableName string, rows []map[string]any) error {
	if !allowedBackupTables[tableName] {
		return fmt.Errorf("表 %s 不在恢复白名单内", tableName)
	}
	if len(rows) == 0 {
		return nil
	}

	tenantIDs := backupRowTenantIDs(rows)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(tableName).
			Where("tenant_id IN ?", tenantIDs).
			Delete(nil).Error; err != nil {
			return fmt.Errorf("清空 %s 失败: %w", tableName, err)
		}
		for i, row := range rows {
			if err := tx.Table(tableName).Create(row).Error; err != nil {
				return fmt.Errorf("恢复 %s 第 %d 行失败: %w", tableName, i+1, err)
			}
		}
		return nil
	})
}

// backupRowTenantIDs 收集备份行覆盖的租户集合；缺省归属默认租户。
func backupRowTenantIDs(rows []map[string]any) []uint {
	seen := map[uint]bool{}
	ids := make([]uint, 0, 1)
	for _, row := range rows {
		id := backupRowTenantID(row)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func backupRowTenantID(row map[string]any) uint {
	for _, key := range []string{"tenant_id", "TenantID"} {
		if v, ok := row[key]; ok {
			if n, ok := backupUint(v); ok && n != 0 {
				return n
			}
		}
	}
	return 1
}

func backupUint(v any) (uint, bool) {
	switch n := v.(type) {
	case uint:
		return n, true
	case uint8:
		return uint(n), true
	case uint16:
		return uint(n), true
	case uint32:
		return uint(n), true
	case uint64:
		return uint(n), true
	case int:
		return uint(n), n >= 0
	case int32:
		return uint(n), n >= 0
	case int64:
		return uint(n), n >= 0
	case float64:
		return uint(n), n >= 0 && n == float64(uint(n))
	}
	return 0, false
}

var _ BackupDataRepository = (*backupDataRepo)(nil)
