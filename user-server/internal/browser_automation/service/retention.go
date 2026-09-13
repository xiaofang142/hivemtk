package service

import (
	"context"
	"os"
	"strconv"
	"time"

	"hivemtk-user/internal/browser_automation/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// retention.go — G19 数据保留治理（主文档 v1.1 §5.2）。
// 背景：command_log（命令帧+回包全文）与 llm_plans（快照/摘要大字段）随 cron 常态运行无界增长。
// 策略（保留审计价值、裁剪体积）：终态超保留期的 command_log 行整行删除（append-only 的
// "不可改"契约约束的是执行路径写后修改；治理性定期裁剪是显式设计，不破坏重放事实——
// 保留期内的重放/审计完整性不受影响）；llm_plans 仅清空 snapshot 大文本、保留 token 成本账。
// 保留天数 env BROWSER_AUDIT_RETENTION_DAYS（默认 90，0=禁用）。

func auditRetentionDays() int {
	if v := os.Getenv("BROWSER_AUDIT_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 90
}

// StartAuditRetention 每日裁剪扫描（进程生命周期内一个 goroutine，每小时检查是否到当日 03:40 批次）。
// 简化为每小时执行一次小批量删除（分批避免长事务锁），天然幂等。
func StartAuditRetention(ctx context.Context, cmdLogRepo repository.BrowserCommandLogRepository, planRepo repository.BrowserLLMPlanRepository) {
	days := auditRetentionDays()
	if days <= 0 || cmdLogRepo == nil || planRepo == nil {
		return
	}
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cutoff := time.Now().AddDate(0, 0, -days)
				deleted, err := cmdLogRepo.PruneBefore(ctx, cutoff)
				if err != nil {
					logger.Warnf("[BrowserRetention] command_log 裁剪失败: %v", err)
				} else if deleted > 0 {
					logger.Infof("[BrowserRetention] command_log 裁剪 %d 行（保留 %d 天）", deleted, days)
				}
				cleared, err := planRepo.PruneSnapshotText(ctx, cutoff)
				if err != nil {
					logger.Warnf("[BrowserRetention] llm_plans 快照裁剪失败: %v", err)
				} else if cleared > 0 {
					logger.Infof("[BrowserRetention] llm_plans 清空旧快照文本 %d 行", cleared)
				}
			}
		}
	}()
}
