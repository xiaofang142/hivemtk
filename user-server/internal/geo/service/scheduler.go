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
		return s
	}
	return string(r[:n])
}

func sovRefreshJob(ctx context.Context) (string, error) {
	keywordRepo := repository.NewGeoKeywordRepository()
	probeRepo := repository.NewGeoProbeRunRepository()
	probes := NewEngineProbes()
	probeSvc := NewProbeService(probes, probeRepo)

	const sovKeywordSample = 60
	var allKW []string
	page, limit := 1, sovKeywordSample
	list, _, err := keywordRepo.GetList("", "", "", "", "", "", page, limit)
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

func loadNegativeKeywords(config *model.GeoConfig) []string {
	raw := strings.TrimSpace(config.NegativeKeywords)
	if raw == "" {
		// 配置为空时走字典：DB 优先（geo_dicts/negative_seeds.words，缺行自动播种），
		// 故障或空表时 fail-open 回内置 negativeSeeds
		var words []string
		if err := DictJSON(model.DictCategoryNegativeSeeds, "words", `["差评","投诉","骗局","失败","坑"]`, &words); err != nil || len(words) == 0 {
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
	cfgRepo := repository.NewGeoConfigRepository()
	probeRepo := repository.NewGeoProbeRunRepository()
	alertRepo := repository.NewGeoAlertRepository()
	config, err := cfgRepo.Get()
	if err != nil {
		return "", fmt.Errorf("读取 GeoConfig 失败: %w", err)
	}

	brandName := strings.TrimSpace(config.BrandName)
	if brandName == "" {
		return "未配置品牌名，跳过本轮", nil
	}
	negativeWords := loadNegativeKeywords(config)

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
	n, err := CrawlerMonitorCronWithContext(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("写入爬虫访问记录=%d 条", n), nil
}
