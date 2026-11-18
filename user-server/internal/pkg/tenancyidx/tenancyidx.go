// Package tenancyidx 提供租户唯一索引规范化：把已租户化表上的单列/多列唯一索引
// 原位重建为 (tenant_id, 原列...)，使业务唯一性升级为租户维度。
//
//   - 唯一事实源：AutoMigrate 后与迁移执行后各调用一次 Normalize，幂等
//   - 排除主键（*_pkey）与约束型 UNIQUE（可能被 ON CONFLICT 显式引用，留人工审计）
//   - 原位重建（不改名）：基线索引名保持稳定，GORM AutoMigrate 仅按名判存在不会回退
//   - 豁免命名约定 global_uniq_*：刻意要求「跨租户全局唯一」的索引（如登录邮箱），
//     复合化会摧毁该语义，故按前缀排除
package tenancyidx

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

type uniqIndex struct {
	table string
	name  string
	def   string
}

// Normalize 幂等重建全部目标唯一索引与约束型 UNIQUE。返回处理数量。
//
// 两类目标同等对待（唯一性语义一致）：
//   - 非约束型唯一索引（GORM uniqueIndex 标签产物）
//   - 约束型 UNIQUE（pg_constraint contype='u'，可能被 ON CONFLICT 显式引用，
//     原位重建保约束名，推断不失配）
func Normalize(db *gorm.DB) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("db is nil")
	}
	// 索引规范化是装配期系统操作：显式系统作用域，避免已被注册的隔离 callback
	// 把 pg_catalog 内省/ALTER TABLE 当成业务 SQL 拒绝（on 模式幂等重入场景）。
	db = db.WithContext(tenant.WithSystemScope(dbContext(db)))
	indexes, err := targets(db)
	if err != nil {
		return 0, err
	}
	for _, idx := range indexes {
		if err := rebuild(db, idx); err != nil {
			return 0, fmt.Errorf("索引 %s（表 %s）复合化失败: %w", idx.name, idx.table, err)
		}
	}
	constraints, err := constraintTargets(db)
	if err != nil {
		return 0, err
	}
	for _, c := range constraints {
		if err := rebuildConstraint(db, c); err != nil {
			return 0, fmt.Errorf("约束 %s（表 %s）复合化失败: %w", c.name, c.table, err)
		}
	}
	return len(indexes) + len(constraints), nil
}

type uniqConstraint struct {
	table string
	name  string
	def   string // pg_get_constraintdef 原文，形如 UNIQUE (a, b)
}

// constraintTargets 枚举约束型 UNIQUE 重建目标：
// 已含 tenant_id 列的表上的 contype='u' 约束（def 未含 tenant_id）
func constraintTargets(db *gorm.DB) ([]uniqConstraint, error) {
	rows, err := db.Raw(`
		SELECT t.relname, c.conname, pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE c.contype = 'u'
		  AND n.nspname = current_schema()
		  AND c.conname NOT LIKE 'global\_uniq\_%'
		  AND pg_get_constraintdef(c.oid) NOT LIKE '%tenant_id%'
		  AND EXISTS (
			SELECT 1 FROM information_schema.columns col
			WHERE col.table_schema = n.nspname
			  AND col.table_name = t.relname
			  AND col.column_name = 'tenant_id'
		  )
		ORDER BY t.relname, c.conname
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("枚举唯一约束失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []uniqConstraint
	for rows.Next() {
		var c uniqConstraint
		if err := rows.Scan(&c.table, &c.name, &c.def); err != nil {
			return nil, fmt.Errorf("扫描约束定义失败: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// rebuildConstraint 约束原位重建：同事务删旧建新（同名），失败整体回滚
func rebuildConstraint(db *gorm.DB, c uniqConstraint) error {
	cols, err := constraintCols(c.def)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf(`ALTER TABLE %q DROP CONSTRAINT %q`, c.table, c.name)).Error; err != nil {
			return fmt.Errorf("删除旧约束失败: %w", err)
		}
		ddl := fmt.Sprintf(`ALTER TABLE %q ADD CONSTRAINT %q UNIQUE (tenant_id, %s)`, c.table, c.name, cols)
		if err := tx.Exec(ddl).Error; err != nil {
			return fmt.Errorf("创建复合约束失败: %w", err)
		}
		return nil
	})
}

// constraintCols 从 pg_get_constraintdef 原文提取列清单：
// 形如 "UNIQUE (a, b)" / "UNIQUE (a, b) WHERE ..."——取第一个配平括号区。
func constraintCols(def string) (string, error) {
	start := strings.Index(def, "(")
	if start < 0 {
		return "", fmt.Errorf("约束定义缺列区: %s", def)
	}
	depth := 0
	end := -1
	for i := start; i < len(def); i++ {
		switch def[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return "", fmt.Errorf("约束定义括号不配平: %s", def)
	}
	cols := strings.TrimSpace(def[start+1 : end])
	if cols == "" {
		return "", fmt.Errorf("约束定义列区为空: %s", def)
	}
	return cols, nil
}

// targets 枚举重建目标：已含 tenant_id 列的表上的非约束型唯一索引（indexdef 未含 tenant_id）
func targets(db *gorm.DB) ([]uniqIndex, error) {
	rows, err := db.Raw(`
		SELECT i.tablename, i.indexname, i.indexdef
		FROM pg_indexes i
		WHERE i.schemaname = current_schema()
		  AND i.indexdef LIKE 'CREATE UNIQUE %'
		  AND i.indexname NOT LIKE '%\_pkey'
		  AND i.indexname NOT LIKE 'global\_uniq\_%'
		  AND i.indexdef NOT LIKE '%tenant_id%'
		  AND NOT EXISTS (
			SELECT 1 FROM pg_constraint c
			WHERE c.conname = i.indexname
		  )
		  AND EXISTS (
			SELECT 1 FROM information_schema.columns col
			WHERE col.table_schema = i.schemaname
			  AND col.table_name = i.tablename
			  AND col.column_name = 'tenant_id'
		  )
		ORDER BY i.tablename, i.indexname
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("枚举唯一索引失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []uniqIndex
	for rows.Next() {
		var idx uniqIndex
		if err := rows.Scan(&idx.table, &idx.name, &idx.def); err != nil {
			return nil, fmt.Errorf("扫描索引定义失败: %w", err)
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

// rebuild 单索引原位重建：同事务删旧建新（同名），失败整体回滚
func rebuild(db *gorm.DB, idx uniqIndex) error {
	newDef, err := compositeIndexDef(idx.def, idx.table, idx.name)
	if err != nil {
		return fmt.Errorf("解析索引定义失败: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS %q`, idx.name)).Error; err != nil {
			return fmt.Errorf("删除旧索引失败: %w", err)
		}
		if err := tx.Exec(newDef).Error; err != nil {
			return fmt.Errorf("创建复合索引失败: %w", err)
		}
		return nil
	})
}

// compositeIndexDef 依据原 indexdef 构造同名复合定义。
// 列区定位：USING 之后的第一个 "(" 起做括号配平（兼容列内表达式 lower(x) 等），
// 平衡收口后的剩余部分若以 WHERE 开头则原样保留（部分索引语义）。
func compositeIndexDef(def, table, name string) (string, error) {
	upper := strings.ToUpper(def)

	colStart := strings.Index(upper, "(")
	if u := strings.Index(upper, " USING "); u >= 0 {
		if p := strings.Index(upper[u:], "("); p >= 0 {
			colStart = u + p
		}
	}
	if colStart < 0 {
		return "", fmt.Errorf("未找到列区起始括号: %s", def)
	}

	depth := 0
	colEnd := -1
	for i := colStart; i < len(def); i++ {
		switch def[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				colEnd = i
			}
		}
		if colEnd >= 0 {
			break
		}
	}
	if colEnd < 0 {
		return "", fmt.Errorf("列区括号不配平: %s", def)
	}

	cols := strings.TrimSpace(def[colStart+1 : colEnd])
	if cols == "" {
		return "", fmt.Errorf("列区为空: %s", def)
	}

	where := ""
	if rest := strings.TrimSpace(def[colEnd+1:]); rest != "" {
		if !strings.HasPrefix(strings.ToUpper(rest), "WHERE") {
			return "", fmt.Errorf("列区后存在无法识别的子句 %q: %s", rest, def)
		}
		where = " " + rest
	}

	return fmt.Sprintf(`CREATE UNIQUE INDEX %q ON %q (tenant_id, %s)%s`,
		name, table, cols, where), nil
}

// ---- 租户表登记表：唯一事实源 = 数据库内省 ----
//
// 凡有 tenant_id 列的 BASE TABLE 即租户表，与动态加列迁移同构——
// AutoMigrate 路径与迁移路径最终都落到"表上有无 tenant_id 列"。

var (
	registryMu sync.RWMutex
	registry   map[string]bool
	// stringTenantTable tenant_id 列为字符类型的租户表（specs/024 B2 证据链表：
	// channel_event_audits / channel_interactions 刻意用字符串租户键）。
	// 隔离 callback 据此绑定字符串形态的租户值（见 tenant_callback.go tenantBindValueFor）。
	stringTenantTable map[string]bool
)

// InitTenantRegistry 装配期调用（AutoMigrate 尾部 / 测试夹具）：
// 内省 current_schema 下全部含 tenant_id 列的 BASE TABLE 构建登记表。
// 内省失败返回 error——调用方在 on 模式必须拒绝启动，绝不能留 nil 登记表
// （nil 意味着任何表都判成"非租户表"= 全量放行）。
func InitTenantRegistry(gdb *gorm.DB) error {
	if gdb == nil {
		return fmt.Errorf("db is nil")
	}
	rows, err := gdb.WithContext(tenant.WithSystemScope(dbContext(gdb))).Raw(`
		SELECT c.table_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema()
		  AND c.column_name = 'tenant_id'
		  AND t.table_type = 'BASE TABLE'
	`).Rows()
	if err != nil {
		return fmt.Errorf("内省租户表登记失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reg := make(map[string]bool)
	strReg := make(map[string]bool)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return fmt.Errorf("扫描租户表名失败: %w", err)
		}
		reg[name] = true
		if dataType == "character varying" || dataType == "text" {
			strReg[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历租户表名失败: %w", err)
	}
	registryMu.Lock()
	registry = reg
	stringTenantTable = strReg
	registryMu.Unlock()
	return nil
}

// IsStringTenantTable 表的 tenant_id 列是否为字符类型（绑定字符串形态的判定源）。
// 登记表未初始化按 uint 口径（off 行为不变）。
func IsStringTenantTable(table string) bool {
	if table == "" {
		return false
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	if stringTenantTable == nil {
		return false
	}
	return stringTenantTable[table]
}

func dbContext(db *gorm.DB) context.Context {
	if db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}

// RegistryReady 租户登记表是否已装配（on 模式的 fail-closed 前置）
func RegistryReady() bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry != nil
}

// IsTenantTable 表是否在租户登记表内（未初始化按"非租户表"，等价 off 行为；
// on 模式是否允许执行由 callback 的 RegistryReady 前置另行裁决）
func IsTenantTable(table string) bool {
	if table == "" {
		return false
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	if registry == nil {
		return false
	}
	return registry[table]
}

// RegistrySnapshot 登记表只读快照（测试对账用）
func RegistrySnapshot() map[string]bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]bool, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

// ResetRegistryForTest 测试专用：清空登记表（生产代码禁止调用）
func ResetRegistryForTest() {
	registryMu.Lock()
	registry = nil
	registryMu.Unlock()
}
