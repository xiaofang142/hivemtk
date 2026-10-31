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
	// UpdateColumn：与原 UPDATE 语义一致——只置 observation 为 NULL，不触碰
	// updated_at，也不走 hooks（幂等重跑对已清理行是 no-op）。
	res := db.WithContext(ctx).Model(&model.EgoliteRound{}).
		Where("observation IS NOT NULL AND created_at < ?", cutoff).
		UpdateColumn("observation", gorm.Expr("NULL"))
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
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
