package db

import (
	"fmt"
	"regexp"

	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/utils/logger"
)

// normalizeTenantIDColumns 归一化 tenant_id 的列类型（必须在 AutoMigrate **之前**执行）。
//
// 事故验证：GEO 模块的模型已声明 `TenantID uint`，而遗留库里
// 8 张 geo_* 表的 tenant_id 仍是 varchar。后果不是"迁移告警"，是**功能直接失败**：
//
//	ERROR: operator does not exist: character varying = integer
//	LINE 1: SELECT count(*) FROM geo_config WHERE tenant_id = 1;
//
// 任何按 tenant_id 过滤的 GEO 查询都报这个错 —— 用户端 `/api/geo/config`
// 返回 INTERNAL_ERROR_6002 正是它。
//
// 为什么不靠 AutoMigrate：它想把 varchar 改成 bigint 时，列上的
// DEFAULT ”::character varying 无法自动转换 → 42804 → **整表迁移中断**
// （该表后续所有列变更都不生效）。修法必须显式：
// DROP DEFAULT → ALTER TYPE … USING → SET DEFAULT。
//
// 为什么按库发现而不是写死表名：写死 8 张 geo_* 表今天够用，但下一次漂移会
// 出现在别的模块，而那时没人记得这里有一份清单。凡 tenant_id 是字符类型的表
// 都进候选。
//
// 为什么 fail-safe 而不是一律强转：`tenant_id::bigint` 遇到非数字值会让整条
// 语句失败；若某张表真的存了语义化租户标识（如 "acme"），强转会把那张表打坏。
// 所以判据是**该列当前的值全部可安全解释为默认租户**（NULL 或空串）才转；
// 发现非空非数字值就跳过并告警，把决定权留给人工。
// tenantTableIdentRe 限制能拼进 DDL 的表名。
//
// 表名来自 `information_schema`（不是用户输入），但本仓已有一条同族规矩：从库元数据
// 取出的标识符拼进 DDL 前仍要校验（见 v3.65.0 迁移里的 identRe）。理由是漂移库里
// 存在何种表名不由我们决定，而这里的每条语句都是 `ALTER TABLE <name>`。
var tenantTableIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)

func normalizeTenantIDColumns(db *gorm.DB) {
	// 用 []string 而不是结构体：GORM 把 table_name 列映射到 TableName 字段
	// （不是 Table），字段名对不上时 Scan **静默给零值** —— 实测表现为
	// "候选为空、循环不执行、日志一条没有"，功能没跑而程序看起来一切正常。
	// tenantIDStringModelTables —— tenant_id **刻意**用字符串租户键的证据链表
	//（specs/024 B2：channel_event_audits / channel_interactions。租户键语义 =
	// 审计证据域，与 operation_logs 的 uint 分离；隔离 callback 已按登记表列类型
	// 绑定字符串形态，见 tenancyidx tenantBindValueFor）。这些表是「人工决定保留」
	// 的豁免口径——归一化把它们转成 bigint 会砸掉整个设计，且模型字段仍是 string。
	var tenantIDStringModelTables = map[string]bool{
		"channel_event_audits": true,
		"channel_interactions": true,
	}
	var tables []string
	if err := db.Raw(`SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name = 'tenant_id'
		  AND data_type IN ('character varying', 'text')
		ORDER BY table_name`).Scan(&tables).Error; err != nil {
		logger.Warnf("[migrate] 枚举 varchar 型 tenant_id 失败，跳过归一化: %v", err)
		return
	}
	if len(tables) == 0 {
		return
	}
	logger.Warnf("[migrate] 发现 %d 张表的 tenant_id 类型漂移（varchar 应为 bigint），开始归一化", len(tables))

	for _, table := range tables {
		if tenantIDStringModelTables[table] {
			logger.Infof("[migrate] %s.tenant_id 为字符串租户键（specs/024 B2 设计），跳过归一化", table)
			continue
		}
		if !tenantTableIdentRe.MatchString(table) {
			logger.Warnf("[migrate] 表名 %q 不符合标识符规范，跳过归一化（需人工处理）", table)
			continue
		}
		// 值体检：只允许 NULL / 空串（语义上即"默认租户"）
		var bad int64
		if err := db.Raw(fmt.Sprintf(
			`SELECT COUNT(*) FROM %s WHERE tenant_id IS NOT NULL AND tenant_id <> ''`, table),
		).Scan(&bad).Error; err != nil {
			logger.Warnf("[migrate] 体检 %s.tenant_id 失败，跳过: %v", table, err)
			continue
		}
		if bad > 0 {
			logger.Warnf("[migrate] %s.tenant_id 有 %d 行非空值，无法自动转 bigint —— "+
				"该列可能承载语义化租户标识，需人工确认后再迁移", table, bad)
			continue
		}

		stmts := []string{
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN tenant_id DROP DEFAULT`, table),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN tenant_id TYPE bigint USING `+
				`(CASE WHEN tenant_id IS NULL OR tenant_id = '' THEN 1 ELSE tenant_id::bigint END)`, table),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN tenant_id SET DEFAULT 1`, table),
			fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN tenant_id SET NOT NULL`, table),
		}
		ok := true
		for _, st := range stmts {
			if err := db.Exec(st).Error; err != nil {
				logger.Warnf("[migrate] %s.tenant_id 归一化失败（已跳过，留待人工）: %v", table, err)
				ok = false
				break
			}
		}
		if ok {
			logger.Warnf("[migrate] 已归一化 %s.tenant_id：varchar → bigint"+
				"（历史值全为空，语义上等于默认租户 1）", table)
		}
	}
}
