// Package cron T4 知识库冷启动：爬取源到期扫描（§4.2）。
//
// 每 10 分钟扫一次 kb_crawler_sources 中 status=active AND auto_resync=true
// 且到期的源（last_crawl_at + interval_hours <= now()，从未爬过的视为到期），
// 建 pending 批次并由 worker 消费。
//
// 租户扇出：cron 交来的 ctx 不含租户（on 模式未绑定即被隔离守卫拒绝），
// 走 tenant.Fanout 逐租户注入——对齐 cron.go 既有 bridge_offline_replay 等任务口径。
package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	kbservice "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
)

// KBSourceDueScanner 到期源的批次创建 + 执行（scanFn 可注入供测试直接调用）
func KBSourceDueScanner(crawler *kbservice.CrawlerService) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		gdb := db.GetDB()
		if gdb == nil || crawler == nil {
			return nil
		}
		var failures []error
		if err := crawler.RecoverPending(ctx); err != nil {
			failures = append(failures, err)
		}
		var sources []model.KBCrawlerSource
		if err := gdb.WithContext(ctx).
			Where("status = ? AND deleted_at IS NULL", model.KBCrawlerSourceStatusActive).
			Find(&sources).Error; err != nil {
			return fmt.Errorf("扫爬取源失败: %w", err)
		}
		now := time.Now()
		created := 0
		for _, src := range sources {
			if src.AutoResync != nil && !*src.AutoResync {
				continue
			}
			if !src.SiteVerified {
				continue // 硬门槛：未验证不发任何请求
			}
			if src.LastCrawlAt != nil && src.LastCrawlAt.Add(time.Duration(src.IntervalHours)*time.Hour).After(now) {
				continue // 未到期
			}
			batch, err := crawler.CreateBatch(ctx, src.ID, model.KBCrawlTriggerCron)
			if err != nil {
				logger.Warnf("[KBCrawler] 源 %d 建批次失败: %v", src.ID, err)
				failures = append(failures, err)
				continue
			}
			created++
			// 同步执行，SkipIfStillRunning 与数据库租约共同防止任务重叠。
			if err := crawler.RunBatch(ctx, batch.ID); err != nil {
				logger.Warnf("[KBCrawler] 批次 %s 执行失败: %v", batch.BatchNo, err)
				failures = append(failures, err)
			}
		}
		if created > 0 {
			logger.Infof("[KBCrawler] 本轮创建并执行 %d 个到期批次", created)
		}
		return errors.Join(failures...)
	}
}

// RegisterKBCrawlerTask 注册爬取到期扫描任务（每 10 分钟，租户扇出）
func RegisterKBCrawlerTask(crawler *kbservice.CrawlerService) {
	mgr := GetTaskManager()
	_, err := mgr.AddTask("0 */10 * * * *", mgr.goTask("kb_crawler", func(ctx context.Context) {
		if err := fanoutTask(ctx, KBSourceDueScanner(crawler)); err != nil {
			logger.Warnf("[KBCrawler] 到期扫描失败: %v", err)
		}
	}))
	if err != nil {
		logger.Warn(fmt.Sprintf("[KBCrawler] 注册定时任务失败: %v", err))
	}
}
