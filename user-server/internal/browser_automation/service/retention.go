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
// 界本身要连**来源**一起落库（/ §7.28 八-3，见 `PruneBefore` 的 cutoffSource 参数）：
// 只记 cutoff 时，「运维把 90 天改成 7 天」与「按 90 天正常裁剪」在库里同形。

const (
	auditRetentionEnv     = "BROWSER_AUDIT_RETENTION_DAYS"
	auditRetentionDefault = 90
	// cutoffSourceMaxLen 对齐 prune_runs.cutoff_source 的列宽（VARCHAR(64)，按字节算）。
	// env 值是外部输入，不设界的话一次畸形配置会让留痕行 INSERT 失败，而留痕是 fail-close
	// 的那一侧——裁剪功能会因一个记账字段超长而整体停摆，这个失效面不成立。
	cutoffSourceMaxLen = 64
)

// auditRetention 返回保留天数 + 这个界从哪来的一句话标注。
// 每次扫描现读而非启动时读一次：进程活着时改了 env，界和来源必须一起跟着变，
// 否则留痕行会写着一个已经不是事实的来源（正是这一列要防的那类不可查）。
func auditRetention() (days int, source string) {
	v := os.Getenv(auditRetentionEnv)
	if v == "" {
		return auditRetentionDefault, cutoffSourceLabel("default:" + strconv.Itoa(auditRetentionDefault))
	}
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		return n, cutoffSourceLabel(auditRetentionEnv + "=" + v)
	}
	// 读不出来/为负时用的是默认值，来源就得说「用了默认」——写成 env 的值会指向一次没发生过的裁剪。
	return auditRetentionDefault,
		cutoffSourceLabel(auditRetentionEnv + "=" + v + "(invalid," + strconv.Itoa(auditRetentionDefault) + ")")
}

func cutoffSourceLabel(s string) string {
	if len(s) <= cutoffSourceMaxLen {
		return s
	}
	r := []rune(s)
	if len(r) > cutoffSourceMaxLen {
		r = r[:cutoffSourceMaxLen]
	}
	return string(r)
}

// StartAuditRetention 每日裁剪扫描（进程生命周期内一个 goroutine，每小时检查是否到当日 03:40 批次）。
// 简化为每小时执行一次小批量删除（分批避免长事务锁），天然幂等。
func StartAuditRetention(ctx context.Context, cmdLogRepo repository.BrowserCommandLogRepository, planRepo repository.BrowserLLMPlanRepository) {
	if !retentionEnabled(cmdLogRepo, planRepo) {
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
				pruneAuditOnce(ctx, cmdLogRepo, planRepo)
			}
		}
	}()
}

// retentionEnabled 是启动那道门：保留期禁用或仓储没装配齐就不起 goroutine。
// 单独成函数的理由同 `pruneAuditOnce`：goroutine 里的早退测不到，而它防的是
// 「装配漏一个仓储 ⇒ 一小时后后台 panic 带走整个进程」，这个失效面必须有腿指着。
func retentionEnabled(cmdLogRepo repository.BrowserCommandLogRepository, planRepo repository.BrowserLLMPlanRepository) bool {
	if cmdLogRepo == nil || planRepo == nil {
		return false
	}
	days, _ := auditRetention()
	return days > 0
}

// pruneAuditOnce 跑一轮扫描：界与来源**每次现读现算**。
// 单独成函数是因为「来源到底有没有传给 PruneBefore」这一格必须可断言——goroutine + 1h ticker
// 里的接线测不到，而它正是这条修复的落点（少传一处，库里就只剩 cutoff 没有来源，
// 与修复前的形状逐字节相同，且没有任何一行会红）。
func pruneAuditOnce(ctx context.Context, cmdLogRepo repository.BrowserCommandLogRepository, planRepo repository.BrowserLLMPlanRepository) {
	days, source := auditRetention()
	if days <= 0 {
		return // 运行中被改成 0：本轮起不再裁剪（口径同启动时的那道门）
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	deleted, err := cmdLogRepo.PruneBefore(ctx, cutoff, source)
	if err != nil {
		logger.Warnf("[BrowserRetention] command_log 裁剪失败: %v", err)
	} else if deleted > 0 {
		logger.Infof("[BrowserRetention] command_log 裁剪 %d 行（界=%s）", deleted, source)
	}
	cleared, err := planRepo.PruneSnapshotText(ctx, cutoff)
	if err != nil {
		logger.Warnf("[BrowserRetention] llm_plans 快照裁剪失败: %v", err)
	} else if cleared > 0 {
		logger.Infof("[BrowserRetention] llm_plans 清空旧快照文本 %d 行", cleared)
	}
}
