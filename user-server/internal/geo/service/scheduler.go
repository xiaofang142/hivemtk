package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/timeutil"
	"hivemtk-user/internal/pkg/utils/logger"
)

func runeTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// citationDriftJob 引用漂移快照：聚合当日探针引用 URL 计数。
// 租户行（geo_probe_runs / geo_citation_snapshots 都是租户表）⇒ 与 sovRefreshJob
// 同一执行形态：JobManager 保证按 (任务, 租户) 注册，进来时 ctx 已绑定本租户。
func citationDriftJob(ctx context.Context) (string, error) {
	svc := NewCitationDriftService(
		repository.NewGeoProbeRunRepository(),
		repository.NewGeoCitationSnapshotRepository(),
	)
	snap, err := svc.TakeSnapshot(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("快照 %s：URL %d 个 / 引用 %d 次",
		snap.SnapshotDate, snap.DistinctURLs, snap.TotalCitations), nil
}

func sovRefreshJob(ctx context.Context) (string, error) {
	// 触达租户表 —— geo_keywords（采样）、geo_probe_runs（逐词
	// 探针落行）、geo_daily_stats（聚合写入），且采样对象是**本租户的词表** ⇒ 必须
	// 逐租户执行，不能"执行一次 + 全平台聚合"（探针行/聚合行是租户行，跨租户 union
	// 采样会把 A 租户的词探完记到 B 名下）。执行形态由 JobManager 保证：cron 条目按
	// (任务, 租户) 注册、手动触发经 Fanout 扇出，进来时 ctx 已绑定本租户（execute
	// 另叠加 def.Timeout）。这里**不再**包 system scope —— 那正是串租户的根源；裸
	// ctx 在 on 模式会被隔离 callback fail-closed 拒绝（那是漏装配的兜底，不是常态）。
	// 代价说明：跨租户重叠的关键词会被各租户各探一次（外部 LLM 调用 N 次）——
	// 探针结果是租户行，无法共享，属可接受成本。

	keywordRepo := repository.NewGeoKeywordRepository()
	probeRepo := repository.NewGeoProbeRunRepository()
	probes := NewEngineProbes()
	probeSvc := NewProbeService(probes, probeRepo)

	const sovKeywordSample = 60
	var allKW []string
	page, limit := 1, sovKeywordSample
	list, _, err := keywordRepo.GetList(ctx, "", "", "", "", "", "", page, limit)
	if err != nil {
		return "", fmt.Errorf("拉取关键词失败(页=%d): %w", page, err)
	}
	for _, k := range list {
		if strings.TrimSpace(k.Keyword) == "" {
			continue
		}
		allKW = append(allKW, k.Keyword)
	}
	if len(allKW) == 0 {
		return "无关键词可探测，跳过本轮", nil
	}
	logger.Info(fmt.Sprintf("[GEO Job sov_refresh] SOV 刷新覆盖关键词数=%d", len(allKW)))

	success, partial, failed := 0, 0, 0
	for i, kw := range allKW {
		if ctx.Err() != nil {
			return fmt.Sprintf("超时中止，进度 %d/%d (成功=%d, 部分成功=%d, 全失败=%d)", i, len(allKW), success, partial, failed),
				fmt.Errorf("执行超时中止")
		}
		runs, errs := probeSvc.ProbeAllEnginesConcurrent(ctx, kw)
		switch {
		case len(runs) > 0 && len(errs) == 0:
			success++
		case len(runs) > 0:
			// 至少一个引擎持久化了探针行：算部分成功，不再因单个坏引擎毒化整词。
			partial++
			errMsgs := make([]string, 0, len(errs))
			for _, e := range errs {
				errMsgs = append(errMsgs, runeTruncate(e.Error(), 120))
			}
			logger.Error(fmt.Errorf("%s", strings.Join(errMsgs, " | ")), fmt.Sprintf("[GEO Job sov_refresh] SOV 刷新关键词 %q 探针部分成功(成功引擎=%d, 失败引擎=%d)", kw, len(runs), len(errs)))
		default:
			failed++
			errMsgs := make([]string, 0, len(errs))
			for _, e := range errs {
				errMsgs = append(errMsgs, runeTruncate(e.Error(), 120))
			}
			logger.Error(fmt.Errorf("%s", strings.Join(errMsgs, " | ")), fmt.Sprintf("[GEO Job sov_refresh] SOV 刷新关键词 %q 探针全失败", kw))
		}
		if (i+1)%50 == 0 {
			logger.Info(fmt.Sprintf("[GEO Job sov_refresh] SOV 刷新进度 %d/%d (成功=%d, 部分成功=%d, 全失败=%d)", i+1, len(allKW), success, partial, failed))
		}
	}

	aggErr := aggregateDailyStats(ctx, probeRepo)
	summary := fmt.Sprintf("覆盖关键词=%d 探针成功=%d 部分成功=%d 全失败=%d", len(allKW), success, partial, failed)
	if aggErr != nil {
		return summary, fmt.Errorf("daily_stats 聚合失败: %w", aggErr)
	}
	if success+partial == 0 {
		// 全轮零持久化：显式报错，避免假成功掩盖探针通道故障。
		return summary, fmt.Errorf("全部关键词探针失败(覆盖=%d)，请检查 LLM 引擎通道", len(allKW))
	}
	return summary, nil
}

func aggregateDailyStats(ctx context.Context, probeRepo repository.GeoProbeRunRepository) error {
	dailyRepo := repository.NewGeoDailyStatRepository()
	// 业务日口径：stat_date 与窗口首都用 timeutil 业务日 helpers（Asia/Shanghai），
	// 与探针行 timestamptz 一致，避免宿主时区漂移导致聚合写错键或漏算。
	todayStart := timeutil.StartOfBusinessDay(time.Now())
	today := timeutil.BusinessDate(todayStart)
	type aggKey struct {
		engine string
		intent string
	}
	agg := map[aggKey]*model.GeoDailyStat{}

	recentRuns, err := probeRepo.ListSince(ctx, todayStart, 0)
	if err != nil {
		return fmt.Errorf("拉取今日探针记录失败: %w", err)
	}
	for _, r := range recentRuns {
		if timeutil.BusinessDate(r.CreatedAt) != today {
			continue
		}
		k := aggKey{engine: r.Engine, intent: runeTruncate(r.Query, 40)}
		if agg[k] == nil {
			agg[k] = &model.GeoDailyStat{
				Date:   today,
				Engine: r.Engine,
				Intent: runeTruncate(r.Query, 40),
			}
		}
		if r.BrandMentioned {
			agg[k].BrandMentionedCount++
		}
		if r.Sentiment == "negative" {
			agg[k].NegativeCount++
		}
		agg[k].ProbeCount++

		var cits []map[string]interface{}
		if err := json.Unmarshal(r.Citations, &cits); err == nil {
			agg[k].CitationCount += int(len(cits))
		}
	}

	stats := make([]*model.GeoDailyStat, 0, len(agg))
	for _, v := range agg {
		stats = append(stats, v)
	}
	if len(stats) == 0 {
		logger.Info("[GEO Job sov_refresh] daily_stats 今日无探针数据，跳过聚合")
		return nil
	}
	if err := dailyRepo.BatchUpsert(ctx, stats); err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("[GEO Job sov_refresh] daily_stats 聚合完成 写入=%d", len(stats)))
	return nil
}

var negativeSeeds = []string{"差评", "投诉", "骗局", "失败", "坑"}

func loadNegativeKeywords(ctx context.Context, config *model.GeoConfig) []string {
	raw := strings.TrimSpace(config.NegativeKeywords)
	if raw == "" {
		// 配置为空时走字典：DB 优先（geo_dicts/negative_seeds.words，缺行自动播种），
		// 故障或空表时 fail-open 回内置 negativeSeeds
		var words []string
		if err := DictJSON(ctx, model.DictCategoryNegativeSeeds, "words", `["差评","投诉","骗局","失败","坑"]`, &words); err != nil || len(words) == 0 {
			return negativeSeeds
		}
		return words
	}
	out := make([]string, 0, 8)
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return negativeSeeds
	}
	return out
}

func negativeMonitorJob(ctx context.Context) (string, error) {
	// 触达租户表 —— geo_config（读**本租户**品牌名/负面词）、
	// geo_probe_runs（探针落行）、geo_alerts（命中写告警）。品牌名是逐租户配置，
	// system scope 下 cfgRepo.Get 会读成跨租户 union 的首行（拿 A 租户的品牌去探、
	// 告警落默认租户）⇒ 必须逐租户执行。ctx 已由 JobManager 绑定本租户（见
	// job_manager.go execute）；这里不再包 system scope。

	cfgRepo := repository.NewGeoConfigRepository()
	probeRepo := repository.NewGeoProbeRunRepository()
	alertRepo := repository.NewGeoAlertRepository()
	config, err := cfgRepo.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("读取 GeoConfig 失败: %w", err)
	}

	brandName := strings.TrimSpace(config.BrandName)
	if brandName == "" {
		return "未配置品牌名，跳过本轮", nil
	}
	negativeWords := loadNegativeKeywords(ctx, config)

	probes := NewEngineProbes()
	probeSvc := NewProbeService(probes, probeRepo)

	queries := make([]string, 0, len(negativeWords))
	for _, neg := range negativeWords {
		queries = append(queries, fmt.Sprintf("%s %s", brandName, neg))
	}

	hit := 0
	failedQueries := 0
	for _, q := range queries {
		if ctx.Err() != nil {
			return fmt.Sprintf("已检查部分查询命中=%d", hit), fmt.Errorf("执行超时中止")
		}
		runs, errs := probeSvc.ProbeAllEngines(ctx, q)
		if len(runs) == 0 {
			// 全引擎失败：该查询无任何探针数据，不能视为"零负面"。
			// 计数失败查询，整轮全失败则返回 error，避免 success+命中=0 的静默降级。
			failedQueries++
			errMsgs := make([]string, 0, len(errs))
			for _, e := range errs {
				errMsgs = append(errMsgs, e.Error())
			}
			logger.Warn(fmt.Sprintf("[GEO Job negative_monitor] 查询 %q 全引擎探针失败(%d): %s", q, len(errs), strings.Join(errMsgs, " | ")))
			continue
		}
		for _, r := range runs {
			resp := strings.ToLower(r.Response)
			for _, neg := range negativeWords {
				if strings.Contains(resp, strings.ToLower(neg)) && strings.Contains(resp, strings.ToLower(brandName)) {
					hit++
					logger.Info(fmt.Sprintf("[GEO Job negative_monitor] 命中！brand=%s engine=%s query=%s snippet=%s",
						brandName, r.Engine, q, truncateForLog(r.Response, 120)))
					alert := &model.GeoAlert{
						Type:      "negative_monitor",
						Level:     "warning",
						BrandName: brandName,
						Query:     q,
						Engine:    r.Engine,
						Snippet:   truncateForLog(r.Response, 300),
						Details:   r.Response,
						Notified:  false,
					}
					if err := alertRepo.Create(ctx, alert); err != nil {
						logger.Error(err, "[GEO Job negative_monitor] 写入 geo_alerts 失败")
					}
					break
				}
			}
		}
	}
	summary := fmt.Sprintf("品牌=%s 负面词=%d 检查查询=%d 命中=%d 失败查询=%d", brandName, len(negativeWords), len(queries), hit, failedQueries)
	if failedQueries == len(queries) {
		return summary, fmt.Errorf("全部 %d 个查询探针失败，本轮无有效数据", len(queries))
	}
	return summary, nil
}

func sourceCatalogSyncJob(ctx context.Context) (string, error) {
	// 触达租户表 geo_source_catalog（读**本租户**种子 URL +
	// 回写 last_checked）。信源种子是逐租户配置 ⇒ 必须逐租户执行；ctx 已由
	// JobManager 绑定本租户，这里不再包 system scope（跨租户 union 读会把别的
	// 租户的种子拿来探、回写也落错行）。

	sourceRepo := repository.NewGeoSourceCatalogRepository()
	crawlerSvc := NewCrawlerService(sourceRepo)

	seeds, err := sourceRepo.ListSeedURLs(ctx)
	if err != nil {
		return "", fmt.Errorf("读取种子 URL 失败: %w", err)
	}
	if len(seeds) == 0 {
		return "信源目录无种子 URL，跳过本轮", nil
	}
	logger.Info(fmt.Sprintf("[GEO Job source_sync] 信源目录同步：扫描种子数=%d", len(seeds)))

	ok, fail := 0, 0
	for _, s := range seeds {
		if ctx.Err() != nil {
			return fmt.Sprintf("超时中止，可达=%d 失败=%d", ok, fail), fmt.Errorf("执行超时中止")
		}
		if err := crawlerSvc.RecordVisit(ctx, s.SourceURL); err != nil {
			fail++
			logger.Error(err, fmt.Sprintf("[GEO Job source_sync] URL=%s 访问失败", s.SourceURL))
			continue
		}
		_ = sourceRepo.UpdateLastChecked(ctx, s.ID, time.Now())
		ok++
	}
	return fmt.Sprintf("种子总数=%d 可达=%d 失败=%d", len(seeds), ok, fail), nil
}

func crawlerMonitorJob(ctx context.Context) (string, error) {
	// CrawlerMonitorCronWithContext 读 geo_keywords/geo_competitors
	// （**本租户**词表与竞品清单）、写 geo_crawler_visits（均带 tenant_id）⇒ 必须
	// 逐租户执行。ctx 已由 JobManager 绑定本租户，这里不再包 system scope。
	// 注意：手动同步入口 CrawlerMonitorCronSync 是另一条不经 JobManager 的路径，
	// 在那里自行 Fanout —— 两条路径互不嵌套，不会双重扇出。

	n, err := CrawlerMonitorCronWithContext(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("写入爬虫访问记录=%d 条", n), nil
}

// siteAuditJob 月度站点体检：给启用中的 geo_sites 各跑一次 rubric v2，
// 结果落审计历史；与上一次同 URL/同 rubric 的分数相比下降 >8 分时写告警。
//
// 执行形态与其它 GEO 任务一致：JobManager 按 (任务, 租户) 注册，进来时
// ctx 已绑定本租户；geo_sites / geo_audit_runs 都是租户表，不能跨租户聚合。
func siteAuditJob(ctx context.Context) (string, error) {
	siteRepo := repository.NewGeoSiteRepository()
	sites, err := listAllGeoSites(ctx, siteRepo)
	if err != nil {
		return "", fmt.Errorf("拉取站点失败: %w", err)
	}
	active := make([]*model.GeoSite, 0, len(sites))
	for _, s := range sites {
		if s != nil && s.Active && strings.TrimSpace(s.Domain) != "" {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return "无启用站点，跳过本轮", nil
	}

	brand := ""
	if cfg, cfgErr := repository.NewGeoConfigRepository().Get(ctx); cfgErr == nil && cfg != nil {
		brand = cfg.BrandName
	}
	alertSvc := NewAlertService(repository.NewGeoAlertRepository())
	auditRepo := repository.NewGeoAuditRepository()
	auditSvc := NewAuditService(auditRepo, alertSvc)

	success, failed := 0, 0
	for _, site := range active {
		if ctx.Err() != nil {
			return fmt.Sprintf("超时中止：已审计 %d 个站点（成功=%d 失败=%d）", success+failed, success, failed),
				fmt.Errorf("执行超时中止")
		}
		rawURL := normalizeSiteAuditURL(site.Domain)
		run, _, runErr := auditSvc.RunSiteAudit(ctx, rawURL, brand, model.GeoAuditSourceSchedule, "job")
		if runErr != nil {
			failed++
			logger.Warn(fmt.Sprintf("[GEO Job site_audit] 站点 %s 审计失败: %v", rawURL, runErr))
			continue
		}
		success++
		prev, prevErr := auditRepo.Previous(ctx, run)
		if prevErr != nil || prev == nil {
			continue
		}
		delta := run.Score - prev.Score
		if delta > -8 {
			continue
		}
		details, _ := json.Marshal(map[string]any{
			"url":            run.URL,
			"rubric_version": run.RubricVersion,
			"from_score":     prev.Score,
			"to_score":       run.Score,
			"from_run":       prev.ID,
			"to_run":         run.ID,
		})
		alert := &model.GeoAlert{
			Type:      "audit_score_drop",
			Level:     "warning",
			BrandName: brand,
			Query:     run.URL,
			Engine:    "geo-audit",
			Snippet:   fmt.Sprintf("站点审计分数从 %d 降到 %d（%d 分）", prev.Score, run.Score, delta),
			Details:   string(details),
			Notified:  false,
		}
		if err := alertSvc.CreateAlert(ctx, alert); err != nil {
			logger.Error(err, "[GEO Job site_audit] 写入分数下降告警失败")
		}
	}
	summary := fmt.Sprintf("站点审计完成：成功=%d 失败=%d", success, failed)
	if failed == len(active) {
		return summary, fmt.Errorf("全部 %d 个站点审计失败，本轮无有效数据", len(active))
	}
	return summary, nil
}

func listAllGeoSites(ctx context.Context, repo repository.GeoSiteRepository) ([]*model.GeoSite, error) {
	const limit = 200
	page := 1
	out := make([]*model.GeoSite, 0)
	for {
		list, total, err := repo.List(ctx, page, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
		if len(list) == 0 || int64(len(out)) >= total {
			return out, nil
		}
		page++
	}
}

func normalizeSiteAuditURL(domain string) string {
	d := strings.TrimSpace(domain)
	if strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") {
		return d
	}
	return "https://" + strings.TrimPrefix(d, "//")
}
