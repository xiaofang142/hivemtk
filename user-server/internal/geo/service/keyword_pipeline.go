package service

import (
	"context"
	"fmt"
	"strings"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	pkgdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	baseservice "hivemtk-user/internal/service"

	"gorm.io/gorm"
)

const (
	// geoPipelineSuggestSeedCap 下拉抓取的种子子集上限（5 引擎 × N 种子 = HTTP 请求数，限流保护）
	geoPipelineSuggestSeedCap = 30
	// geoPipelineDefaultSEOBatch 每轮 LLM SEO（生成→评分→验证→探针）的词数上限。
	// 待 SEO 队列按 last_mined_at 轮转拉取，每日一轮逐步消化 backlog、无遗留；
	// 20 词约消耗单轮超时（60min）的一半预算，兼顾成本与清零速度，可经 agent_llm.geo_pipeline_batch_per_run 调整
	geoPipelineDefaultSEOBatch = 20
	// geoPipelineSEO WordCount 生成字数
	geoPipelineSEOWordCount = 800
)

func geoPipelineBatchPerRun(ctx context.Context) int {
	return baseservice.GlobalConfigParam().GetInt(ctx, "agent_llm", "geo_pipeline_batch_per_run", geoPipelineDefaultSEOBatch)
}

// keywordPipelineJob 关键词批量管线定时任务（4 业务线 × 技术/概念 × 品牌 → 万词级 → 队列轮转 LLM SEO，清零 backlog 不遗留）
//
// 阶段（零重试、无外部 LLM 直调，全部走 GEO 站内服务）：
//  1. 种子矩阵：BuildBusinessSeedMatrix（离线确定性，4 业务线一级类目）
//  2. 长尾展开：CombineLongtail（23 模板离线组合，零 LLM 成本）
//  3. 下拉抓取：CrawlSuggest（5 引擎，限 30 种子，失败仅日志不断链）
//  4. 意图补全：SaveMiningResults 内启发式 ClassifyIntent（零成本）
//  5. 落库站位：SaveMiningResults（种子站位 + ParentID 回填 + 合并更新）
//  6. 业务线回填：Cluster = 一级类目（结果词内存标记 + 种子词 post-save UPDATE）
//  7. 漏斗统计：BuildFunnel
//  8. 有界 SEO：取 N 个无文章长尾/下拉词 → GenerateContent → ScoreContent →
//     VerifyArticle（含探针 + 多模型对比），逐词容错，N 可配 geo_pipeline_batch_per_run
func keywordPipelineJob(ctx context.Context) (string, error) {
	keywordRepo := repository.NewGeoKeywordRepository()
	gormDB := pkgdb.GetDB()
	llmAdapter := NewLLMAdapter()
	miningSvc := NewKeywordMiningService(keywordRepo, gormDB, llmAdapter)

	// 1. 种子矩阵（DB 优先：geo_seed_terms 表；空表自动播种，DB 故障回硬编码）
	seeds, seedLines := NewSeedTermService(repository.NewGeoSeedTermRepositoryWithDB(gormDB)).LoadSeedMatrix(ctx)
	if len(seeds) == 0 {
		return "种子矩阵为空，跳过本轮", nil
	}

	// 2. 离线长尾展开
	longtails, err := miningSvc.CombineLongtail(ctx, seeds, nil)
	if err != nil {
		return "", fmt.Errorf("长尾展开失败: %w", err)
	}

	// 3. 下拉抓取（限子集，best-effort）
	suggestSeeds := seeds
	if len(suggestSeeds) > geoPipelineSuggestSeedCap {
		suggestSeeds = suggestSeeds[:geoPipelineSuggestSeedCap]
	}
	suggestOutcome := miningSvc.CrawlSuggest(ctx, suggestSeeds, nil)
	if len(suggestOutcome.Errors) > 0 {
		logger.Warnf("[GEO Job keyword_pipeline] 下拉抓取 %d 组（引擎×种子）报错，仅日志不断链: %v",
			len(suggestOutcome.Errors), suggestOutcome.Errors)
	}
	suggests := suggestOutcome.Keywords

	// 4. 业务线回填 Cluster（结果词：ParentKeyword → seedLines）
	all := make([]*model.GeoKeyword, 0, len(longtails)+len(suggests))
	for _, r := range append(append([]*model.GeoKeyword{}, longtails...), suggests...) {
		if r == nil || r.Keyword == "" {
			continue
		}
		if line := seedLines[r.ParentKeyword]; line != "" {
			r.Cluster = line
		}
		all = append(all, r)
	}

	// 5. 落库站位
	saved, err := miningSvc.SaveMiningResults(ctx, seeds, all)
	if err != nil {
		return "", fmt.Errorf("挖掘结果落库失败: %w", err)
	}

	// 6. 种子词 Cluster 回填（SaveMiningResults 建的 seed 行无 Cluster，按业务线分 4 批 UPDATE）
	seedClustered := 0
	byLine := map[string][]string{}
	for _, s := range seeds {
		if line := seedLines[s]; line != "" {
			byLine[line] = append(byLine[line], s)
		}
	}
	kwTable := (&model.GeoKeyword{}).TableName()
	for line, words := range byLine {
		if ctx.Err() != nil {
			break
		}
		res := gormDB.WithContext(ctx).Table(kwTable).
			Where("keyword IN ? AND (cluster = '' OR cluster IS NULL)", words).
			Update("cluster", line)
		if res.Error != nil {
			logger.Warnf("[GEO Job keyword_pipeline] 种子词业务线回填失败 line=%s: %v", line, res.Error)
			continue
		}
		seedClustered += int(res.RowsAffected)
	}

	// 7. 漏斗统计
	funnel, err := miningSvc.BuildFunnel(ctx)
	if err != nil {
		return "", fmt.Errorf("漏斗统计失败: %w", err)
	}

	// 8. 有界 LLM SEO 批次
	seoOK, seoFail, seoNote := runPipelineSEOBatch(ctx, gormDB, llmAdapter)

	summary := fmt.Sprintf(
		"种子=%d 长尾=%d 下拉=%d 落库=%d 种子业务线回填=%d 漏斗总量=%d(suggest=%d/longtail=%d) SEO批次成功=%d失败=%d%s",
		len(seeds), len(longtails), len(suggests), saved, seedClustered,
		funnel.Total, funnel.SuggestCount, funnel.LongtailCount, seoOK, seoFail, seoNote,
	)
	return summary, nil
}

// runPipelineSEOBatch 有界 LLM SEO：无文章的长尾/下拉词 → 生成 → 评分 → 验证（含探针）
// 返回 (成功数, 失败数, 备注)
func runPipelineSEOBatch(ctx context.Context, gormDB *gorm.DB, llmAdapter *LLMAdapter) (int, int, string) {
	batchN := geoPipelineBatchPerRun(ctx)
	if batchN <= 0 {
		return 0, 0, "（SEO批次已关闭 geo_pipeline_batch_per_run<=0）"
	}

	// 品牌配置：无品牌则跳过 SEO（只做挖掘落库），避免生成无主内容
	cfgRepo := repository.NewGeoConfigRepository()
	cfg, err := cfgRepo.Get()
	if err != nil || cfg == nil || strings.TrimSpace(cfg.BrandName) == "" {
		return 0, 0, "（未配置品牌名，跳过SEO批次）"
	}
	brand := strings.TrimSpace(cfg.BrandName)
	lang := strings.TrimSpace(cfg.Language)
	if lang == "" {
		lang = "zh"
	}
	advs := splitAdvantages(cfg.Advantages)
	models := splitVerifyModels(cfg.VerifyModels)

	// 取无文章的长尾/下拉词（ORDER BY last_mined_at 保证轮转推进）
	kwTable := (&model.GeoKeyword{}).TableName()
	artTable := (&model.GeoArticle{}).TableName()
	var pending []model.GeoKeyword
	if err := gormDB.WithContext(ctx).Table(kwTable+" k").
		Select("k.*").
		Joins(fmt.Sprintf("LEFT JOIN %s a ON a.keyword = k.keyword", artTable)).
		Where("k.status = ? AND k.layer IN ? AND a.id IS NULL", "active", []string{"longtail", "suggest"}).
		Order("k.last_mined_at ASC NULLS LAST, k.keyword ASC").
		Limit(batchN).
		Scan(&pending).Error; err != nil {
		logger.Warnf("[GEO Job keyword_pipeline] 拉取待SEO词失败: %v", err)
		return 0, 0, fmt.Sprintf("（拉取待SEO词失败: %v）", err)
	}
	if len(pending) == 0 {
		return 0, 0, "（无待SEO词）"
	}

	contentSvc := NewContentService(
		repository.NewGeoArticleRepository(),
		repository.NewGeoOptimizationRepository(),
		repository.NewGeoAPICallRepository(),
		repository.NewGeoKnowledgeDocumentRepository(),
		llmAdapter,
	)
	verifySvc := NewVerificationService(
		repository.NewGeoVerifyResultRepository(),
		repository.NewGeoAPICallRepository(),
		repository.NewGeoQueryChainRepository(gormDB),
		repository.NewGeoContentTaskRepository(gormDB),
		llmAdapter,
		nil,
	)

	ok, fail := 0, 0
	for i := range pending {
		if ctx.Err() != nil {
			logger.Warnf("[GEO Job keyword_pipeline] SEO批次超时中止，进度 %d/%d", i, len(pending))
			break
		}
		kw := strings.TrimSpace(pending[i].Keyword)
		if kw == "" {
			continue
		}
		if serr := seoOneWord(ctx, contentSvc, verifySvc, lang, kw, brand, advs, models); serr != nil {
			fail++
			logger.Warnf("[GEO Job keyword_pipeline] SEO词 %q 失败: %v", kw, serr)
			continue
		}
		ok++
	}
	return ok, fail, ""
}

// seoOneWord 单词全链路：生成 → 评分 → 验证（含探针 + 多模型对比）
func seoOneWord(ctx context.Context, contentSvc *ContentService, verifySvc *VerificationService, lang, keyword, brand string, advs, models []string) error {
	art, err := contentSvc.GenerateContent(ctx, lang, keyword, brand, advs, geoPipelineSEOWordCount, "professional")
	if err != nil {
		return fmt.Errorf("内容生成失败: %w", err)
	}
	if _, err := contentSvc.ScoreContent(ctx, art.ID, art.Content, brand, keyword); err != nil {
		return fmt.Errorf("内容评分失败: %w", err)
	}
	if _, err := verifySvc.VerifyArticle(ctx, dto.VerifyRequest{
		ArticleID: art.ID,
		Query:     keyword,
		BrandName: brand,
		Models:    models,
	}); err != nil {
		return fmt.Errorf("验证失败: %w", err)
	}
	return nil
}

// splitAdvantages 优势文本切分为数组（品牌配置 Advantages 为自由文本）
func splitAdvantages(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	rep := strings.NewReplacer("，", "、", ",", "、", ";", "、", "；", "、", "\n", "、")
	parts := strings.Split(rep.Replace(s), "、")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitVerifyModels 验证模型列表（配置以 "、" 分隔存储，与 UpdateConfig 一致）
func splitVerifyModels(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "、")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
