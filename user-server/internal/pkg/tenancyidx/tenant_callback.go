package tenancyidx

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// 租户隔离 GORM callback（specs/001-saas-multi-tenant T005）
//
// 实现位于 tenancyidx（其依赖仅 gorm+tenant）：testutil 与 pkg/db 经本包注册，
// 断开 testutil→db 的 import 环（pkg/db 测试依赖 testutil 的链路会成环）。
//
// 隔离的唯一执行者，业务代码零入侵：
//   - off 模式（TENANT_MODE=off，默认）：全部 callback 直通，SQL 形状保持不变
//   - on 模式：Query/Update/Delete 自动注入 `WHERE tenant_id = ?`；Create 自动填充
//     tenant_id 列；上下文无租户 ⇒ fail-closed 报错（绝不默认全量）
//   - Raw/Exec（预构建 SQL）：off 直通；on 模式引用租户表即拒绝——数据访问唯一形态
//     是 GORM API（硬闸防回归），运行时这里兜第二道闸
//
// 注册点：db.AutoMigrate()（生产全局 DB）与 testutil.NewTestDB（测试连接）；
// 同名重复注册等价替换，幂等。

// identPattern 匹配裸标识符、双引号标识符与反引号标识符（PG 仅双引号，兼容遗留写法）
const identPattern = `(?:"[^"]+"|` + "`[^`]+`" + `|[A-Za-z_][A-Za-z0-9_$]*)`

// tableRefRe 从预构建 SQL / JOIN 片段中提取引用表（FROM/JOIN/INTO/UPDATE 等关键字后）
var tableRefRe = regexp.MustCompile(`(?i)\b(?:from|join|into|update|truncate(?:\s+table)?|alter\s+table|drop\s+table|copy)\s+(?:only\s+)?(?:if\s+exists\s+)?(` + identPattern + `(?:\s*\.\s*` + identPattern + `)?)`)

// tableExprRe 解析 GORM 的 TableExpr（如 `sop_executions AS se` / `"tenants"` / `public.users u`）
var tableExprRe = regexp.MustCompile(`(?i)^\s*(?:only\s+)?(` + identPattern + `(?:\s*\.\s*` + identPattern + `)?)(?:\s+(?:as\s+)?(` + identPattern + `))?\s*$`)

var aliasRe = regexp.MustCompile(`(?i)^\s*(?:as\s+)?(` + identPattern + `)`)

var joinOnRe = regexp.MustCompile(`(?i)\bON\b`)

// aliasKeyword 不能当作表别名的 SQL 关键字（FROM t WHERE / JOIN t ON ...）
var aliasKeyword = map[string]bool{
	"ON": true, "USING": true, "WHERE": true, "LEFT": true, "RIGHT": true,
	"INNER": true, "OUTER": true, "CROSS": true, "NATURAL": true, "JOIN": true,
	"SET": true, "VALUES": true, "RETURNING": true, "GROUP": true, "ORDER": true,
	"LIMIT": true, "OFFSET": true, "FOR": true, "WINDOW": true, "UNION": true,
	"EXCEPT": true, "INTERSECT": true, "FETCH": true, "TABLESAMPLE": true,
	"AS": true, "ONLY": true, "FROM": true, "INTO": true, "UPDATE": true,
	"TRUNCATE": true, "ALTER": true, "DROP": true, "COPY": true, "ADD": true,
	"COLUMN": true, "DEFAULT": true, "CHECK": true, "CONSTRAINT": true,
	"PRIMARY": true, "UNIQUE": true, "REFERENCES": true, "NOT": true,
	"NULL": true, "TRUE": true, "FALSE": true, "AND": true, "OR": true,
	"IS": true, "IN": true, "BETWEEN": true, "LIKE": true, "ILIKE": true,
	"SIMILAR": true, "DO": true, "NOTHING": true, "CONFLICT": true,
	"SELECT": true, "WITH": true, "OVER": true, "PARTITION": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"ASC": true, "DESC": true, "NULLS": true, "FIRST": true, "LAST": true,
	"LATERAL": true, "QUALIFY": true, "EXISTS": true, "IF": true,
}

// tableRef 目标表引用：注册表按 table 判定，SQL 条件用 qualifier（别名优先）
type tableRef struct {
	table string
	alias string
}

func (r tableRef) qualifier() string {
	if r.alias != "" {
		return r.alias
	}
	return r.table
}

// normalizeIdent 归一化标识符：schema.table → table；去引号；裸标识符按 PG 语义转小写
func normalizeIdent(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "."); i >= 0 {
		raw = strings.TrimSpace(raw[i+1:])
	}
	if len(raw) >= 2 {
		switch {
		case raw[0] == '"' && raw[len(raw)-1] == '"':
			return strings.ReplaceAll(raw[1:len(raw)-1], `""`, `"`)
		case raw[0] == '`' && raw[len(raw)-1] == '`':
			return strings.ReplaceAll(raw[1:len(raw)-1], "``", "`")
		}
	}
	return strings.ToLower(raw)
}

// parseTableExpr 解析 TableExpr 原文，返回基础表名与别名。
// 解析不了（派生表 `(?) AS u`、多表 FROM）返回 ok=false——绝不把别名当表名。
func parseTableExpr(raw string) (tableRef, bool) {
	m := tableExprRe.FindStringSubmatch(raw)
	if m == nil {
		return tableRef{}, false
	}
	table := normalizeIdent(m[1])
	if table == "" {
		return tableRef{}, false
	}
	ref := tableRef{table: table}
	if m[2] != "" {
		if aliasKeyword[strings.ToUpper(m[2])] {
			return tableRef{}, false
		}
		ref.alias = normalizeIdent(m[2])
	}
	return ref, true
}

// scanTableRefs 提取 SQL 片段中的全部表引用（含别名）
func scanTableRefs(sqlText string) []tableRef {
	matches := tableRefRe.FindAllStringSubmatchIndex(sqlText, -1)
	out := make([]tableRef, 0, len(matches))
	for _, m := range matches {
		table := normalizeIdent(sqlText[m[2]:m[3]])
		if table == "" {
			continue
		}
		out = append(out, tableRef{table: table, alias: parseTableAlias(sqlText[m[1]:])})
	}
	return out
}

// parseTableAlias 从表名之后的剩余文本解析别名；关键字/标点/括号一律视为无别名
func parseTableAlias(rest string) string {
	s := strings.TrimSpace(rest)
	if s == "" || s[0] == '(' || s[0] == ',' || s[0] == ';' {
		return ""
	}
	m := aliasRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	if aliasKeyword[strings.ToUpper(m[1])] {
		return ""
	}
	return normalizeIdent(m[1])
}

// resolveTenantRef 解析 Statement 的目标表：显式 TableExpr 优先（其中 Table 字段是
// 别名本身，不能直接查登记表），其次 Schema/Table。解析不出返回 ok=false。
func resolveTenantRef(stmt *gorm.Statement) (tableRef, bool) {
	if stmt == nil {
		return tableRef{}, false
	}
	if stmt.TableExpr != nil {
		return parseTableExpr(stmt.TableExpr.SQL)
	}
	if stmt.Schema == nil && stmt.Model != nil {
		_ = stmt.Parse(stmt.Model)
	}
	if stmt.Table != "" {
		return tableRef{table: normalizeIdent(stmt.Table)}, true
	}
	if stmt.Schema != nil {
		return tableRef{table: normalizeIdent(stmt.Schema.Table)}, true
	}
	return tableRef{}, false
}

// RegisterTenantCallbacks 在给定连接上注册六类租户隔离 callback（幂等）
func RegisterTenantCallbacks(gdb *gorm.DB) {
	if gdb == nil {
		return
	}
	cb := gdb.Callback()
	_ = cb.Query().Before("gorm:query").Register("tenant:scope_query", scopeQueryForTenant)
	_ = cb.Create().Before("gorm:before_create").Register("tenant:fill_create", fillCreateForTenant)
	_ = cb.Update().Before("gorm:update").Register("tenant:scope_update", scopeUpdateForTenant)
	_ = cb.Delete().Before("gorm:delete").Register("tenant:scope_delete", scopeDeleteForTenant)
	// Scan()/Rows() 走 Row 链（callbacks.RowQuery→BuildQuerySQL），不是 Query 链：
	// 只在 Query 链注入会让所有 .Scan() 查询（大量 Table+Joins 仓储）整体绕过隔离。
	_ = cb.Row().Before("gorm:row").Register("tenant:scope_row", scopeQueryForTenant)
	_ = cb.Raw().Before("gorm:raw").Register("tenant:guard_raw", guardRawForTenant)
}

// tenantInjectionActive 是否处于注入生效状态（on 模式）。off 一律直通。
func tenantInjectionActive() bool { return tenant.IsMultiTenant() }

// tenantGuardFail fail-closed：注入失败即报错，绝不放行为全量查询
func tenantGuardFail(db *gorm.DB, table, reason string) {
	_ = db.AddError(fmt.Errorf("[tenant] 表 %s 需要租户上下文但 ctx 未绑定（%s）；"+
		"多租户模式下拒绝执行，请经中间件/TenantRunner/行自证提供租户", table, reason))
}

// scopeQueryForTenant Query/Row 前置：自动注入 WHERE tenant_id = ?
func scopeQueryForTenant(db *gorm.DB) {
	if db == nil || db.Statement == nil || !tenantInjectionActive() {
		return
	}
	stmt := db.Statement
	// 预构建 SQL（Raw().Scan 形态）：不走子句注入，交由 raw 守卫裁决
	if stmt.SQL.Len() > 0 {
		guardRawSQLForTenant(db)
		return
	}
	ctx := stmt.Context
	if tenant.IsSystemScope(ctx) {
		return
	}
	if !ensureRegistryForTenant(db, "query") {
		return
	}
	ref, known := resolveTenantRef(stmt)
	id, bound := tenant.FromContext(ctx)
	if known && IsTenantTable(ref.table) {
		if !bound {
			tenantGuardFail(db, ref.table, "query")
			return
		}
		stmt.AddClause(clauseWhereTenant(stmt, ref, id))
	}
	if err := scopeTenantJoins(stmt, bound, id); err != nil {
		_ = db.AddError(err)
	}
}

// fillCreateForTenant Create 前置：自动填充 tenant_id 列
func fillCreateForTenant(db *gorm.DB) {
	if db == nil || db.Statement == nil || !tenantInjectionActive() {
		return
	}
	stmt := db.Statement
	ctx := stmt.Context
	if tenant.IsSystemScope(ctx) {
		return // 系统操作：交由列默认值（1）
	}
	if !ensureRegistryForTenant(db, "create") {
		return
	}
	ref, known := resolveTenantRef(stmt)
	id, bound := tenant.FromContext(ctx)
	table := ref.table
	isTenant := known && IsTenantTable(table)
	if isTenant {
		if err := guardOnConflictTenantAssignments(stmt, id, bound); err != nil {
			_ = db.AddError(err)
			return
		}
	}

	// schema-less（map）Create：没有 Model 字段可填，INSERT 也不会带 tenant_id。
	// 直接 return ⇒ 行落列默认值 1（非默认租户的数据整批串进租户 1）。
	if stmt.Schema == nil {
		if !isTenant {
			return
		}
		fillMapCreateForTenant(db, stmt, table, id, bound)
		return
	}
	field := stmt.Schema.LookUpField("tenant_id")
	if field == nil {
		// 目标是租户表但模型没声明 tenant_id：INSERT 会静默落列默认值 1，必须拒绝
		if isTenant {
			tenantGuardFail(db, table, "create：模型未声明 tenant_id 字段，无法填充")
		}
		return
	}
	// INSERT 必须真的带上 tenant_id 列。
	//
	// 为什么必须查：Omit("tenant_id") / Select("name") 这类写法会让 GORM 的
	// ConvertToCreateValues **不把 tenant_id 放进 INSERT**（Selects 非空即 restricted，
	// 只有 selectedColumns 里为 true 的列进 SQL）。于是回调填进内存的值根本没进 SQL，
	// 行落**列默认值 1** —— 也就是默认租户：on 模式下这是一次静默的跨租户写。
	// 对抗探针验证：INSERT INTO "tenancy_probe" ("name") VALUES ($1)，
	// 行 tenant_id=1，而发起方 ctx 租户是 91。
	if isTenant && !tenantIDWillBeInserted(stmt) {
		tenantGuardFail(db, table, "create：本次 INSERT 不含 tenant_id 列（Omit/Select 掉了），"+
			"行会落列默认值 1（默认租户）= 跨租户写，拒绝执行")
		return
	}

	// 批量 Create(&[]*Model / []Model)：stmt.ReflectValue 是**切片**。
	// 若对整片直接 ValueOf/Set —— reflect 在切片上调 Field 直接 panic
	// （on 模式 + 有租户绑定 ctx + 批量写租户表 = 进程级崩溃；单行路径不受
	// 影响，所以长期没暴露）。逐元素执行与单行完全相同的填充/校验语义。
	rv := stmt.ReflectValue
	switch rv.Kind() {
	case reflect.Map:
		// Model 与 map 混用（Schema 非空、Dest 是 map）：同样走 map 填充
		if isTenant {
			fillMapCreateForTenant(db, stmt, table, id, bound)
		}
		return
	case reflect.Slice:
		if rv.Len() > 0 && derefKind(rv.Index(0)) == reflect.Map {
			if isTenant {
				fillMapCreateForTenant(db, stmt, table, id, bound)
			}
			return
		}
		for i := 0; i < rv.Len(); i++ {
			elem := rv.Index(i)
			for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Interface {
				if elem.IsNil() {
					break
				}
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Struct {
				continue
			}
			if !fillOneForTenant(db, field, elem, id, bound, table) {
				return // 已 AddError：停止处理剩余行（与单行 fail-closed 同后果）
			}
		}
		return
	case reflect.Struct:
		fillOneForTenant(db, field, rv, id, bound, table)
	default:
		// 目标形态无法填充（非结构体/切片/map）：绝不能静默落列默认值
		if isTenant {
			tenantGuardFail(db, table, "create：目标行形态不支持 tenant_id 填充")
		}
	}
}

func derefKind(v reflect.Value) reflect.Kind {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return v.Kind()
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return reflect.Invalid
	}
	return v.Kind()
}

// fillMapCreateForTenant 为 map[string]interface{}（或切片）逐行填充/校验 tenant_id。
// 直接改写 map 本身：GORM ConvertToCreateValues 读的是同一份 Dest。
func fillMapCreateForTenant(db *gorm.DB, stmt *gorm.Statement, table string, id uint, bound bool) {
	// 与结构体路径同一道闸：Omit/Select 掉 tenant_id 时 INSERT 缺列 ⇒ 行落默认租户。
	if !tenantIDWillBeInserted(stmt) {
		tenantGuardFail(db, table, "create：本次 INSERT 不含 tenant_id 列（Omit/Select 掉了），"+
			"行会落列默认值 1（默认租户）= 跨租户写，拒绝执行")
		return
	}
	rv := stmt.ReflectValue
	switch rv.Kind() {
	case reflect.Map:
		if !fillOneMapForTenant(db, rv, table, id, bound) {
			return
		}
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			elem := rv.Index(i)
			for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Interface {
				if elem.IsNil() {
					tenantGuardFail(db, table, "create：map 批量行含 nil")
					return
				}
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Map {
				tenantGuardFail(db, table, "create：批量行不是 map")
				return
			}
			if !fillOneMapForTenant(db, elem, table, id, bound) {
				return
			}
		}
	default:
		tenantGuardFail(db, table, "create：map 目标形态不支持 tenant_id 填充")
	}
}

// fillOneMapForTenant 单行 map 填充：bound 时强制==上下文租户，未绑定仅接受行自证。
func fillOneMapForTenant(db *gorm.DB, m reflect.Value, table string, id uint, bound bool) bool {
	if m.Type().Key().Kind() != reflect.String {
		tenantGuardFail(db, table, "create：map 键类型不是字符串")
		return false
	}
	if m.IsNil() {
		tenantGuardFail(db, table, "create：map 为 nil")
		return false
	}
	stringTenant := IsStringTenantTable(table)
	key := reflect.ValueOf("tenant_id").Convert(m.Type().Key())
	existing := m.MapIndex(key)
	if existing.IsValid() && !isNilableNil(existing) {
		value := existing.Interface()
		if bound {
			if sv, isStr := value.(string); isStr && stringTenant {
				if n, err := strconv.ParseUint(sv, 10, 64); err == nil && n != 0 && n != uint64(id) {
					_ = db.AddError(fmt.Errorf("[tenant] 写入 tenant_id=%q 与上下文租户 %d 不一致，拒绝跨租户写", sv, id))
					return false
				}
			} else if n, ok := asUint(value); !ok || (n != 0 && n != id) {
				_ = db.AddError(fmt.Errorf("[tenant] 写入 tenant_id=%v 与上下文租户 %d 不一致，拒绝跨租户写", value, id))
				return false
			}
		} else {
			valid := false
			if sv, isStr := value.(string); isStr && stringTenant {
				if n, err := strconv.ParseUint(sv, 10, 64); err == nil && n != 0 {
					valid = true
				}
			} else if n, ok := asUint(value); ok && n != 0 {
				valid = true
			}
			if !valid {
				tenantGuardFail(db, table, "create：无租户上下文且 tenant_id 不是有效自证值")
				return false
			}
		}
	} else if !bound {
		tenantGuardFail(db, table, "create：无租户上下文且 map 未携带 tenant_id")
		return false
	}
	elemType := m.Type().Elem()
	var tenantVal reflect.Value
	if stringTenant {
		tenantVal = reflect.ValueOf(strconv.FormatUint(uint64(id), 10))
	} else {
		tenantVal = reflect.ValueOf(id)
	}
	if !tenantVal.Type().AssignableTo(elemType) {
		if !tenantVal.Type().ConvertibleTo(elemType) {
			tenantGuardFail(db, table, "create：map 值类型无法承载 tenant_id")
			return false
		}
		tenantVal = tenantVal.Convert(elemType)
	}
	m.SetMapIndex(key, tenantVal)
	return true
}

func isNilableNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// fillOneForTenant 对**一个结构体行**执行 tenant_id 填充/校验。
// 返回 true=可继续处理后续行；false=已 fail-closed（db 已挂错误）。
func fillOneForTenant(db *gorm.DB, field *schema.Field, v reflect.Value, id uint, bound bool, table string) bool {
	if !bound {
		// 字段已有显式非零值（行自证写入路径）：放行
		if fv, zero := field.ValueOf(db.Statement.Context, v); !zero {
			if fvUint, isUint := fv.(uint); isUint && fvUint != 0 {
				return true
			}
		}
		tenantGuardFail(db, table, "create")
		return false
	}
	if fv, zero := field.ValueOf(db.Statement.Context, v); !zero {
		if fvUint, isUint := fv.(uint); isUint && fvUint != 0 && fvUint != id {
			_ = db.AddError(fmt.Errorf("[tenant] 写入 tenant_id=%d 与上下文租户 %d 不一致，拒绝跨租户写", fvUint, id))
			return false
		}
		if fvStr, isStr := fv.(string); isStr && fvStr != "" {
			if n, err := strconv.ParseUint(fvStr, 10, 64); err == nil && n != 0 && n != uint64(id) {
				_ = db.AddError(fmt.Errorf("[tenant] 写入 tenant_id=%q 与上下文租户 %d 不一致，拒绝跨租户写", fvStr, id))
				return false
			}
		}
	}
	if !v.CanAddr() {
		// 不可寻址就填不进去：宁可 fail-closed 报错，也不能静默落列默认值 1
		// （那会把行写到默认租户 = 串租户）。单行 Create 的 Dest 恒可寻址。
		tenantGuardFail(db, table, "create：目标行不可寻址，无法填充 tenant_id")
		return false
	}
	if field.FieldType.Kind() == reflect.String {
		// 字符串租户键（specs/024 B2 证据链表）：填字符串形态，pgx 才能编码进 varchar 列
		_ = field.Set(db.Statement.Context, v, strconv.FormatUint(uint64(id), 10))
		return true
	}
	_ = field.Set(db.Statement.Context, v, id)
	return true
}

// scopeUpdateForTenant / scopeDeleteForTenant：与 query 同一注入语义
func scopeUpdateForTenant(db *gorm.DB) { scopeLikeForTenant(db, "update") }
func scopeDeleteForTenant(db *gorm.DB) { scopeLikeForTenant(db, "delete") }

func scopeLikeForTenant(db *gorm.DB, op string) {
	if db == nil || db.Statement == nil || !tenantInjectionActive() {
		return
	}
	stmt := db.Statement
	if stmt.SQL.Len() > 0 {
		guardRawSQLForTenant(db)
		return
	}
	ctx := stmt.Context
	if tenant.IsSystemScope(ctx) {
		return
	}
	if !ensureRegistryForTenant(db, op) {
		return
	}
	ref, known := resolveTenantRef(stmt)
	if !known || !IsTenantTable(ref.table) {
		return
	}
	id, bound := tenant.FromContext(ctx)
	if !bound {
		tenantGuardFail(db, ref.table, op)
		return
	}
	if op == "update" {
		if err := guardUpdateTenantAssignments(stmt, id); err != nil {
			_ = db.AddError(err)
			return
		}
	}
	stmt.AddClause(clauseWhereTenant(stmt, ref, id))
}

// guardRawForTenant Row/Raw 前置：预构建 SQL 守卫
func guardRawForTenant(db *gorm.DB) {
	if db == nil || db.Statement == nil || !tenantInjectionActive() {
		return
	}
	guardRawSQLForTenant(db)
}

// guardRawSQLForTenant 预构建 SQL 守卫：on 模式引用租户表即拒绝。
// 数据访问唯一形态是 GORM API；此处是运行时第二道闸。
func guardRawSQLForTenant(db *gorm.DB) {
	if db == nil || db.Statement == nil {
		return
	}
	sqlText := db.Statement.SQL.String()
	if sqlText == "" || tenant.IsSystemScope(db.Statement.Context) {
		return
	}
	if !ensureRegistryForTenant(db, "raw") {
		return
	}
	for _, ref := range scanTableRefs(sqlText) {
		if IsTenantTable(ref.table) {
			_ = db.AddError(fmt.Errorf("[tenant] 预构建 SQL 引用租户表 %s 被拒绝：数据访问必须走 GORM API"+
				"（specs/001-saas-multi-tenant D2 / check-gorm-only 门禁）", ref.table))
			return
		}
	}
}

// ensureRegistryForTenant 登记表未装配时 on 模式拒绝一切非系统操作：
// nil 登记表会把任何表都判成"非租户表" = 全量放行。
func ensureRegistryForTenant(db *gorm.DB, op string) bool {
	if RegistryReady() {
		return true
	}
	_ = db.AddError(fmt.Errorf("[tenant] 租户登记表未初始化，无法判定表归属（%s）；"+
		"多租户模式拒绝执行，请在装配期先调用 InitTenantRegistry", op))
	return false
}

// clauseWhereTenant 构造 `tenant_id = ?` WHERE 子句；
// 有别名时限定别名，避免 JOIN 场景下 tenant_id 列歧义。
// tenantBindValueFor 把上下文租户转换为目标表 tenant_id 的绑定表示。
//
// specs/024 B2 起，证据链表（channel_event_audits / channel_interactions）的租户键
// **刻意**用字符串（审计域语义，与 operation_logs 的 uint 分离，见
// recordRouteDiscard 头注释）。隔离 callback 恒绑 uint 会让 pgx 以 text OID 收到
// uint 值 → "cannot find encode plan"（MODE=on 读/写路径全断，2026-11-18 实测）。
// 绑定表示按「登记表列类型 + 模型字段类型」双保险判定：字符串租户表绑字符串形态。
func tenantBindValueFor(stmt *gorm.Statement, table string, id uint) any {
	if IsStringTenantTable(table) {
		return strconv.FormatUint(uint64(id), 10)
	}
	if stmt != nil && stmt.Schema != nil {
		if f := stmt.Schema.FieldsByDBName["tenant_id"]; f != nil && f.FieldType.Kind() == reflect.String {
			return strconv.FormatUint(uint64(id), 10)
		}
	}
	return id
}

func clauseWhereTenant(stmt *gorm.Statement, ref tableRef, id uint) clause.Where {
	col := clause.Column{Name: "tenant_id"}
	if q := ref.qualifier(); q != "" {
		col.Table = q
	}
	return clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: col, Value: tenantBindValueFor(stmt, ref.table, id)},
	}}
}

// scopeTenantJoins 给 JOIN 到的租户表追加租户条件。
// 条件必须进 ON 子句（而不是 WHERE）：LEFT JOIN 的未匹配父行必须保留。
func scopeTenantJoins(stmt *gorm.Statement, bound bool, id uint) error {
	for i := range stmt.Joins {
		j := &stmt.Joins[i]
		if !strings.ContainsAny(j.Name, " \t\r\n") {
			// 关联式 Joins("Relation")：无法可靠推导 SQL 别名，目标为租户表即拒绝
			if stmt.Schema != nil {
				if target, ok := associationTable(stmt.Schema, j.Name); ok && IsTenantTable(target) {
					return fmt.Errorf("[tenant] 关联式 JOIN %s 指向租户表 %s，无法自动附加租户条件；"+
						"请改用带 ON 的显式 JOIN", j.Name, target)
				}
			}
			continue
		}
		var tenantRefs []tableRef
		for _, ref := range scanTableRefs(j.Name) {
			if IsTenantTable(ref.table) {
				tenantRefs = append(tenantRefs, ref)
			}
		}
		if len(tenantRefs) == 0 {
			continue
		}
		if !bound {
			return fmt.Errorf("[tenant] JOIN 引用租户表 %s 但 ctx 未绑定租户；多租户模式拒绝执行", tenantRefs[0].table)
		}
		if !joinOnRe.MatchString(j.Name) {
			return fmt.Errorf("[tenant] JOIN 租户表 %s 未使用 ON 子句，无法附加租户条件，拒绝执行", tenantRefs[0].table)
		}
		for _, ref := range tenantRefs {
			q := ref.qualifier()
			if q == "" {
				return fmt.Errorf("[tenant] JOIN 租户表 %s 无法解析别名", ref.table)
			}
			// 条件必须落进 **ON 子句**。
			//
			// 为什么不能简单追加到 join 串末尾：join 串自带 WHERE 时（"JOIN x ON … WHERE …"），
			// 追加的 AND 会落进 WHERE 侧 ⇒ LEFT JOIN 退化成 INNER JOIN ⇒ 未匹配的父行被挤掉。
			// 对抗探针实测，修前 SQL：
			//   … LEFT JOIN tenancy_probe AS tp ON tp.id = g.id + 1000
			//     WHERE g.id = 1 AND "tp"."tenant_id" = $1
			// 期望：租户条件在 ON 里，父行保留（0 行 = 语义被破坏）。
			//
			// 租户 id 是 ctx 里的 uint（不是外部输入），直接内联成整数字面量：
			// 免去"往 join 串中间插占位符 ⇒ 破坏 ? 与 args 的位置对应"这个坑
			// （GORM 的 NamedExpr 按位置绑定 Vars，插中间就得同步挪 args，计错一位就静默绑错值）。
			// 字符串租户表的内联形态必须带引号（varchar = integer 直接报 operator 不存在，
			// geo_* 事故同族）。id 是 ctx 里的 uint，%d 内联无注入面。
			joinCond := fmt.Sprintf(` AND %s.%s = %d`, quoteIdent(q), quoteIdent("tenant_id"), id)
			if IsStringTenantTable(ref.table) {
				joinCond = fmt.Sprintf(` AND %s.%s = '%d'`, quoteIdent(q), quoteIdent("tenant_id"), id)
			}
			j.Name = insertJoinOnCondition(j.Name, joinCond)
		}
	}
	return nil
}

// insertJoinOnCondition 把 cond 插进 join 串的 **ON 子句**：
// 若 join 串自带顶层 WHERE，则插在 WHERE 之前；否则追加到末尾。
//
// 只认顶层（括号深度 0、且不在字符串字面量里）的 WHERE：
// "JOIN x ON (a = b) WHERE c" 里的括号先配平，WHERE 仍在深度 0；
// "JOIN x ON a = 'WHERE'" 里的引号内文本不算关键字。
func insertJoinOnCondition(joinSQL, cond string) string {
	idx := topLevelKeywordIndex(joinSQL, "where")
	if idx < 0 {
		return joinSQL + cond
	}
	// 补一个空格：cond 以数字结尾，直接接 "WHERE" 会拼成 "91WHERE"（PG 语法错）。
	return joinSQL[:idx] + cond + " " + joinSQL[idx:]
}

// topLevelKeywordIndex 找 SQL 串里第一个顶层关键字（大小写不敏感、单词边界）。
// 顶层 = 括号深度 0 且不在 '...' / "..." 字面量里。找不到返回 -1。
func topLevelKeywordIndex(sqlText, keyword string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(sqlText); i++ {
		ch := sqlText[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			continue
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 {
			continue
		}
		if i+len(keyword) > len(sqlText) {
			break
		}
		if !strings.EqualFold(sqlText[i:i+len(keyword)], keyword) {
			continue
		}
		// 单词边界：前一个字符不是标识符字符，后一个字符也不是
		if i > 0 && isIdentChar(sqlText[i-1]) {
			continue
		}
		if i+len(keyword) < len(sqlText) && isIdentChar(sqlText[i+len(keyword)]) {
			continue
		}
		return i
	}
	return -1
}

// isIdentChar 标识符字符（字母数字下划线 $）
func isIdentChar(ch byte) bool {
	return ch == '_' || ch == '$' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// associationTable 解析 Joins("A.B") 关系链的最终表名
func associationTable(s *schema.Schema, name string) (string, bool) {
	cur := s
	for _, part := range strings.Split(name, ".") {
		rel, ok := cur.Relationships.Relations[part]
		if !ok {
			return "", false
		}
		cur = rel.FieldSchema
	}
	return cur.Table, true
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// tenantIDWillBeInserted 判断本次 INSERT 是否真的带上 tenant_id 列。
//
// GORM 的 ConvertToCreateValues 规则（gorm v1.31.2 statement.go:715）：
//   - Selects 为空 ⇒ 不受限，所有模型列都进 INSERT
//   - Selects 非空且含 "*" ⇒ 不受限，所有模型列都进 INSERT
//   - Selects 非空且不含 "*"（restricted）⇒ 只有 selectedColumns 里为 true 的列进 INSERT
//   - Omits 里的列一律 false ⇒ 不进 INSERT
//
// 于是 Omit("tenant_id") / Select("name") 会让 INSERT **缺 tenant_id 列**：
// 回调填进内存的值根本没进 SQL，行落**列默认值 1**（默认租户）——
// on 模式下这是一次静默的跨租户写（验证：INSERT INTO "tenancy_probe" ("name") VALUES ($1)，
// 行 tenant_id=1，而发起方 ctx 租户是 91）。
func tenantIDWillBeInserted(stmt *gorm.Statement) bool {
	if stmt == nil {
		return false
	}
	selected, restricted := stmt.SelectAndOmitColumns(true, false)
	v, ok := selected["tenant_id"]
	// 与 GORM ConvertToCreateValues 的入选判据同式：(ok && v) || (!ok && !restricted)
	//   - ok && v      ⇒ 显式选中且未被 Omit ⇒ 进 INSERT
	//   - !ok && !restricted ⇒ 没进 selectedColumns 且无 Selects 限制 ⇒ 进 INSERT
	//   - Omit 掉的列会以 v=false 落在 map 里 ⇒ ok=true ⇒ 不进 INSERT
	//   （注意：Selects 为空时 restricted=false，但 Omit 仍然生效——别用
	//     "restricted 为假 ⇒ 一定有列" 这种捷径，Omit-only 情形会被漏掉。）
	return (ok && v) || (!ok && !restricted)
}

// guardUpdateTenantAssignments 更新不得改写 tenant_id：
// Update 列式 / Updates map / Updates struct / Clauses(Assignments) 四种形态全覆盖。
func guardUpdateTenantAssignments(stmt *gorm.Statement, id uint) error {
	reject := func(v interface{}) error {
		if n, ok := asUint(v); ok && n == id {
			return nil
		}
		return fmt.Errorf("[tenant] 更新不可改写 tenant_id（上下文租户 %d，SET 值 %v）", id, v)
	}
	if stmt.Schema != nil && stmt.Dest != nil {
		rv := reflect.ValueOf(stmt.Dest)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				rv = reflect.Value{}
				break
			}
			rv = rv.Elem()
		}
		if rv.IsValid() && rv.Kind() == reflect.Struct {
			if field := stmt.Schema.LookUpField("tenant_id"); field != nil {
				v, zero := field.ValueOf(stmt.Context, rv)
				// 本次 SET 到底会不会写 tenant_id 列？GORM ConvertToAssignments 的规则：
				//   selected["tenant_id"]==true ⇒ **一定写**（Selects 含 "*" 时零值也照写，
				//   这就是 Save 的行为）——于是值必须等于上下文租户，零值也不放过；
				//   未选中且不受限（Selects 为空）⇒ 仅当值非零才写 ⇒ 沿用"非零必须等于租户"。
				// 只判"非零且不等于租户"时， Save 一个没加载过的结构体
				// （TenantID 零值）会把行的 tenant_id 覆写成 0 —— 行掉出本租户。
				// 对抗探针验证：
				//   UPDATE "tenancy_probe" SET "tenant_id"=$1,"name"=$2 WHERE ... AND "id" = $4
				// $1 = 0，守卫未拦。
				selected, restricted := stmt.SelectAndOmitColumns(false, true)
				alwaysWritten, inMap := selected["tenant_id"]
				switch {
				case inMap && alwaysWritten:
					if err := reject(v); err != nil {
						return err
					}
				case !inMap && !restricted:
					if !zero {
						if err := reject(v); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if dest, ok := stmt.Dest.(map[string]interface{}); ok {
		for key, v := range dest {
			if isTenantColumnName(key) {
				if err := reject(v); err != nil {
					return err
				}
			}
		}
	}
	if c, ok := stmt.Clauses["SET"]; ok {
		if set, ok := c.Expression.(clause.Set); ok {
			for _, a := range set {
				if isTenantColumnName(a.Column.Name) {
					if err := reject(a.Value); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// guardOnConflictTenantAssignments upsert 的 DO UPDATE 不得把 tenant_id 改成别的值。
// excluded.tenant_id 是本次插入值（已填充为上下文租户），放行。
func guardOnConflictTenantAssignments(stmt *gorm.Statement, id uint, bound bool) error {
	c, ok := stmt.Clauses["ON CONFLICT"]
	if !ok {
		return nil
	}
	oc, ok := c.Expression.(clause.OnConflict)
	if !ok {
		return nil
	}
	for _, a := range oc.DoUpdates {
		if !isTenantColumnName(a.Column.Name) {
			continue
		}
		if col, ok := a.Value.(clause.Column); ok && strings.EqualFold(col.Table, "excluded") && isTenantColumnName(col.Name) {
			continue
		}
		if n, ok := asUint(a.Value); bound && ok && n == id {
			continue
		}
		return fmt.Errorf("[tenant] upsert 的 ON CONFLICT DO UPDATE 不可改写 tenant_id（上下文租户 %d）", id)
	}
	return nil
}

func isTenantColumnName(name string) bool {
	return strings.EqualFold(normalizeIdent(name), "tenant_id")
}

// asUint 把常见数值形态归一为 uint；表达式/字符串等一律 false（fail-closed）
func asUint(v interface{}) (uint, bool) {
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
		if n >= 0 {
			return uint(n), true
		}
	case int8:
		if n >= 0 {
			return uint(n), true
		}
	case int16:
		if n >= 0 {
			return uint(n), true
		}
	case int32:
		if n >= 0 {
			return uint(n), true
		}
	case int64:
		if n >= 0 {
			return uint(n), true
		}
	case float32:
		return floatToUint(float64(n))
	case float64:
		return floatToUint(n)
	}
	return 0, false
}

func floatToUint(f float64) (uint, bool) {
	if f < 0 || f != math.Trunc(f) || f > math.MaxUint32 {
		return 0, false
	}
	return uint(f), true
}
