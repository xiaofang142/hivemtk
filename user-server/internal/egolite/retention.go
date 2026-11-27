package egolite

import (
	"context"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// retention.go —— T19 观测留存策略（M3 治理）。
//
// 对象是 egolite_rounds.observation（页面 targets/text 的 JSON 快照——观测面里
// 唯一带"内容"的列）与 result 里的 extract 摘要。留存期后把**内容列**置空，
// 账本列（ok/duration_ms/actions/error_code/round）原样保留——过期清理不误删
// 账本，是本文件的验收红线：审计链（何时跑过、跑没跑成、耗时多少）永在，
// 丢的只是"当时页面上写了什么"。
//
// 默认保留 observation_retention_days=30（config_params 可调；≤0 = 永不清理，
// 留给合规要求更长的部署）。产物文件（截图）不在本任务面：文件挂在用户本机
// runner 与 storage，清理走各自的生命周期，不在这里越界。

// RetentionSweep 清理窗口外的观测内容列（observation/targets/text 快照面）。
// 账本列（ok/duration_ms/actions/error_code/request/result 的结构面）原样保留。
// 幂等：重复执行对已清理行是 no-op（where observation is not null）。
func RetentionSweep(ctx context.Context, db *gorm.DB, keepDays int) (int64, error) {
	if keepDays <= 0 {
		return 0, nil // 永不清理档
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepDays) * 24 * time.Hour)
	// 分批清理（架构审计轮优化）：egolite_rounds 是高频写入表（每轮一行 +
	// observation JSON 快照可达数十 KB），留存窗口首扫可能是几十万行的大事务
	// （长时间行锁 + WAL 峰值）。改为每批 2000 行循环清到清空为止——单批事务
	// 短、锁窗口小；幂等不变（where observation is not null 对已清行 no-op）。
	const batchSize = 2000
	var total int64
	for {
		res := db.WithContext(ctx).Model(&model.EgoliteRound{}).
			Where("observation IS NOT NULL AND created_at < ?", cutoff).
			// 子查询必须同样带 ctx：裸 db.Model 的子查询没有租户绑定，
			// 多租户模式下被隔离 callback 拒绝后 SQL 为空，外层退化成 id IN () 语法错误。
			Where("id IN (?)", db.WithContext(ctx).Model(&model.EgoliteRound{}).
				Select("id").
				Where("observation IS NOT NULL AND created_at < ?", cutoff).
				Order("id ASC").Limit(batchSize)).
			UpdateColumn("observation", gorm.Expr("NULL"))
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < batchSize {
			return total, nil
		}
	}
}

// ObserveRetentionDays 从 config_params 读保留天数（键不存在/非法 → 默认 30）。
// 治理参数进参数中心（007 同口径），不散落 env。
func ObserveRetentionDays(ctx context.Context, db *gorm.DB) int {
	var v string
	err := db.WithContext(ctx).Model(&model.ConfigParam{}).
		Select("param_value").
		Where("param_group = ? AND key = ?", "egolite", "observation_retention_days").
		Limit(1).
		Scan(&v).Error
	if err != nil || v == "" {
		return 30
	}
	n := 0
	for _, r := range v {
		if r < '0' || r > '9' {
			return 30
		}
		n = n*10 + int(r-'0')
	}
	return n
}
