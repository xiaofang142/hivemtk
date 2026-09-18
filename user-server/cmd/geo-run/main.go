package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/config"
	geodto "hivemtk-user/internal/geo/dto"
	geomodel "hivemtk-user/internal/geo/model"
	georepo "hivemtk-user/internal/geo/repository"
	geoservice "hivemtk-user/internal/geo/service"
	"hivemtk-user/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func must(err error, what string) {
	if err != nil {
		fmt.Printf("❌ %s 失败: %v\n", what, err)
		os.Exit(1)
	}
}

type providerSeed struct {
	name, display, base, model, apiKey string
	quality                            float64
	cost                               float64
}

func main() {
	brand := "HiveMtk"
	advantages := []string{
		"私域营销 AI 操作系统，数据 100% 本地部署不出域",
		"ReAct 智能体 + 41 工具直接驱动成交",
		"13 渠道触达：企微/WhatsApp/TG/抖音/小红书等",
		"RAG 三级检索 + SOP 自动化",
		"开源 AGPL-3.0，支持私有化交付",
	}

	db, err := gorm.Open(postgres.Open(envOr("GEO_DB_DSN", "")), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	must(err, "连接 PG")
	must(db.AutoMigrate(
		&model.LLMProvider{},
		&geomodel.GeoKeyword{}, &geomodel.GeoKeywordGroup{},
		&geomodel.GeoArticle{}, &geomodel.GeoOptimization{},
		&geomodel.GeoVerifyResult{}, &geomodel.GeoAPICall{}, &geomodel.GeoConfig{},
		&geomodel.GeoWorkflow{}, &geomodel.GeoWorkflowExecution{}, &geomodel.GeoWorkflowTemplate{},
		&geomodel.GeoQueryChain{}, &geomodel.GeoContentTask{},
		// ⬇️ 新增 GEO v2 全链路模型
		&geomodel.GeoPushRecord{},
		&geomodel.GeoSite{},
		&geomodel.GeoPusherConfig{},
		&geomodel.GeoIndexTracking{},
		&geomodel.GeoSchemaTemplate{},
	), "AutoMigrate")

	seeds := []providerSeed{
		{"deepseek", "DeepSeek", "https://api.deepseek.com", "deepseek-chat", os.Getenv("DEEPSEEK_API_KEY"), 0.88, 0.001},
		{"qwen", "通义千问", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen-plus", os.Getenv("QWEN_API_KEY"), 0.85, 0.002},
		{"doubao", "豆包", "https://ark.cn-beijing.volces.com/api/v3", "doubao-pro-32k", os.Getenv("DOUBAO_API_KEY"), 0.84, 0.002},
		{"ernie", "文心一言", "https://qianfan.baidubce.com/v2", "ernie-4.0-8k-latest", os.Getenv("ERNIE_API_KEY"), 0.86, 0.004},
	}
	for i, sd := range seeds {
		if sd.apiKey == "" {
			continue
		}
		row := model.LLMProvider{
			Name: sd.name, DisplayName: sd.display, BaseURL: sd.base,
			Model: sd.model, APIKey: sd.apiKey, APIType: "openai",
			Enabled: true, QualityScore: sd.quality, CostPer1k: sd.cost,
			MaxRPM: 60, AvgLatencyMs: 2000,
		}

		var existing model.LLMProvider
		if err := db.Where("name = ?", sd.name).First(&existing).Error; err == nil {
			row.ID = existing.ID
			if uerr := db.Model(&row).Updates(map[string]any{
				"base_url": row.BaseURL, "model": row.Model, "api_key": row.APIKey,
				"enabled": true, "quality_score": row.QualityScore, "cost_per_1k": row.CostPer1k,
			}).Error; uerr != nil {
				must(uerr, "更新 provider "+sd.name)
			}
		} else {
			must(db.Create(&row).Error, "创建 provider "+sd.name)
		}
		_ = i
	}
	fmt.Printf("== [A] llm_providers 已就绪（DB 为真相，%d 个厂商种子） ==\n", len(seeds))

	cfg := config.AppConfig{}
	cfg.Inference.LLM.Mode = config.InferenceModeRemote
	cfg.Inference.LLM.BaseURL = "https://api.deepseek.com"
	cfg.Inference.LLM.Model = "deepseek-chat"
	dispatcher := llm.NewDispatcherFromConfig(cfg)
	llm.InitGlobalDispatcherWithDB(dispatcher, db)
	app.SetGlobalDispatcher(dispatcher)
	must(dispatcher.LoadProvidersFromDB(), "LoadProvidersFromDB")
	fallbacks := []string{"qwen", "doubao", "ernie"}
	for _, sc := range []llm.DispatchScenario{
		llm.ScenarioIntentRecognize, llm.ScenarioSOPReply, llm.ScenarioObjection,
		llm.ScenarioFriendlyChat, llm.ScenarioLongSummary,
		llm.ScenarioHighQuality, llm.ScenarioLowCost,
	} {
		dispatcher.SetRoute(llm.ScenarioRoute{
			Scenario: sc, Provider: "deepseek", Fallbacks: fallbacks,
			CostWeight: 1, MaxLatency: 120000, MinQuality: 0.7,
		})
	}
	fmt.Println("== [B] Dispatcher 已从 DB 加载提供商并配置路由 ==")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	kwSvc := geoservice.NewKeywordService(
		georepo.NewGeoKeywordGroupRepositoryWithDB(db),
		georepo.NewGeoKeywordRepositoryWithDB(db),
		georepo.NewGeoAPICallRepositoryWithDB(db),
		geoservice.NewLLMAdapter(),
	)

	configSvc := geoservice.NewConfigService(
		georepo.NewGeoConfigRepositoryWithDB(db), geoservice.NewLLMAdapter())
	_ = configSvc.UpdateConfig(ctx,
		"HiveMtk",
		"开源自私域营销 AI 操作系统：ReAct 销冠智能体(41工具)+13渠道触达(企微/WhatsApp/TG/抖音/小红书/TikTok等)+RAG三级检索+SOP自动化，数据100%本地部署不出域，AGPL-3.0 开源",
		"数据不出域;AI直接驱动成交而非仅客服应答;开源可自部署;多渠道统一收件箱;决策链GEO优化内置",
		[]string{"微伴助手", "探马SCRM", "尘锋SCRM"},
		"https://hivemtk.com", "deepseek",
		[]string{"deepseek", "qwen"}, nil)
	cfgRow, _ := configSvc.GetConfig(ctx)
	fmt.Printf("== [U1] 品牌配置已保存 == 品牌=%s 竞品=%s\n", cfgRow.BrandName, cfgRow.Competitors)

	keywords, err := kwSvc.MineKeywords(ctx,
		[]string{"企业微信 SCRM", "私域营销 AI", "营销自动化系统", "开源 SCRM"},
		"longtail", brand, advantages)
	must(err, "关键词挖掘")
	fmt.Printf("== [U2] 关键词：%d 个 ==\n", len(keywords))
	topKeyword := brand + " 私域营销系统怎么选"
	if len(keywords) > 0 && strings.TrimSpace(keywords[0].Keyword) != "" {
		topKeyword = keywords[0].Keyword
	}

	intentQueries := []struct{ intent, query, kw string }{
		{"疑问", "HiveMtk 是什么？支持哪些渠道？", "HiveMtk 功能介绍"},
		{"对比", "HiveMtk 和微伴助手、探马SCRM 对比哪个好？", "HiveMtk 与微伴助手对比"},
		{"推荐", "有没有开源、支持私有化部署的私域营销 AI 系统推荐？", "开源私域营销 AI 系统"},
	}
	verifySvc := geoservice.NewVerificationService(
		georepo.NewGeoVerifyResultRepositoryWithDB(db),
		georepo.NewGeoAPICallRepositoryWithDB(db),
		georepo.NewGeoQueryChainRepository(db),
		georepo.NewGeoContentTaskRepository(db),
		geoservice.NewLLMAdapter(),
		geoservice.NewDefaultSearchProbe(),
	)
	contentSvc := geoservice.NewContentService(
		georepo.NewGeoArticleRepositoryWithDB(db),
		georepo.NewGeoOptimizationRepositoryWithDB(db),
		georepo.NewGeoAPICallRepositoryWithDB(db),
		georepo.NewGeoKnowledgeDocumentRepositoryWithDB(db),
		geoservice.NewLLMAdapter(),
	)

	fmt.Println("== [U3] 决策链三阶段内容+验证 ==")
	for _, iq := range intentQueries {
		art, gerr := contentSvc.GenerateContent(ctx, "", iq.kw, brand, advantages, 800, "professional")
		if gerr != nil {
			fmt.Printf("  [%s] 生成失败: %v\n", iq.intent, gerr)
			continue
		}
		score, serr := contentSvc.ScoreContent(ctx, art.ID, art.Content, brand, iq.kw)
		total := "?"
		if serr == nil {
			if v, ok := score["total_score"].(float64); ok {
				total = fmt.Sprintf("%.0f", v)
			} else if b, _ := json.Marshal(score); serr == nil {
				total = truncate(string(b), 60)
			}
		}
		vr, verr := verifySvc.VerifyArticle(ctx, geodto.VerifyRequest{
			ArticleID: art.ID, Query: iq.query, BrandName: brand,
		})
		vline := "验证失败"
		if verr == nil {
			vline = fmt.Sprintf("提及=%v 次数=%d 情感=%s 位置=%q",
				vr.BrandMentioned, vr.MentionCount, vr.Sentiment, vr.Position)
		} else {
			vline += ": " + verr.Error()
		}
		fmt.Printf("  [%s] 文章《%s》len=%d 评分=%s | 用户问:%q → %s\n",
			iq.intent, nonEmpty(art.Title, "(无题)"), len([]rune(art.Content)), total, iq.query, vline)
	}

	wfSvc := geoservice.NewWorkflowService(
		georepo.NewGeoWorkflowRepositoryWithDB(db),
		georepo.NewGeoWorkflowExecutionRepositoryWithDB(db),
		georepo.NewGeoWorkflowTemplateRepositoryWithDB(db),
		georepo.NewGeoQueryChainRepository(db),
		georepo.NewGeoContentTaskRepository(db),
		geoservice.NewLLMAdapter(),
	)
	wf, err := wfSvc.Create(ctx, &geodto.SaveWorkflowRequest{
		Name: "HiveMtk 决策链内容流水线",
		Steps: []geodto.WorkflowStep{
			{Name: "生成", Type: "content_generate", Params: map[string]interface{}{
				"topic": "企业选择私域营销 AI 系统的决策指南", "brand": brand,
				"keyword": topKeyword, "platform": "技术博客",
				"advantages": strings.Join(advantages, "；"),
			}},
			{Name: "评分", Type: "content_score", Params: map[string]interface{}{}},
			{Name: "EEAT增强", Type: "eeat_enhance", Params: map[string]interface{}{}},
			{Name: "事实密度", Type: "fact_density_enhance", Params: map[string]interface{}{}},
			{Name: "验证", Type: "verify", Params: map[string]interface{}{
				"brand": brand, "query": topKeyword,
			}},
		},
	})
	must(err, "创建工作流")
	runResp, err := wfSvc.Run(ctx, wf.ID)
	must(err, "运行工作流")

	elapsed := runResp.CompletedAt.Sub(runResp.StartedAt).Milliseconds()
	fmt.Printf("== [C2] 工作流执行完成（execution=%s status=%s 用时%dms） ==\n",
		runResp.ID, runResp.Status, elapsed)
	for _, sr := range runResp.Result {
		status := sr.Status
		line := fmt.Sprintf("  • [%s] %-10s %s", status, sr.StepType, sr.StepName)
		if sr.Error != "" {
			line += " err=" + truncate(sr.Error, 80)
		}
		fmt.Println(line)
		if sr.Result != "" && (sr.StepType == "content_score") {
			fmt.Println("     ", truncate(sr.Result, 220))
		}
	}

	var latestArt geomodel.GeoArticle
	if err := db.Order("created_at DESC").First(&latestArt).Error; err == nil {
		verifySvc := geoservice.NewVerificationService(
			georepo.NewGeoVerifyResultRepositoryWithDB(db),
			georepo.NewGeoAPICallRepositoryWithDB(db),
			georepo.NewGeoQueryChainRepository(db),
			georepo.NewGeoContentTaskRepository(db),
			geoservice.NewLLMAdapter(),
			geoservice.NewDefaultSearchProbe(),
		)
		if _, verr := verifySvc.VerifyArticle(ctx, geodto.VerifyRequest{
			ArticleID: latestArt.ID, Query: topKeyword, BrandName: brand,
		}); verr != nil {
			fmt.Println("  ⚠️ 验证服务失败:", verr)
		}
	}

	fmt.Println("\n== [D] GEO 数据看板 ==")
	dumpCounts(db)
	dumpLatestArticleAndVerify(db, brand)

	batchN := 9
	if v := os.Getenv("GEO_BATCH"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &batchN)
	}
	intents := []struct{ label, query, kw string }{
		{"疑问", "HiveMtk 是什么？支持哪些渠道和功能？", "HiveMtk 功能介绍"},
		{"疑问", "HiveMtk 怎么部署？数据真的不出域吗？", "HiveMtk 部署指南"},
		{"疑问", "HiveMtk 的 AI 销冠是怎么工作的？", "HiveMtk AI 销售原理"},
		{"对比", "HiveMtk 和微伴助手对比哪个更适合企业微信私域？", "HiveMtk vs 微伴助手"},
		{"对比", "HiveMtk 与传统 SCRM 系统的核心区别是什么？", "HiveMtk vs 传统SCRM"},
		{"对比", "HiveMtk 和探马 SCRM 的优缺点分别是什么？", "HiveMtk vs 探马SCRM"},
		{"推荐", "有没有开源、支持私有化部署的营销自动化平台推荐？", "开源营销自动化推荐"},
		{"推荐", "2026 年最值得选的 AI 客服系统有哪些？", "AI 客服系统排行"},
		{"推荐", "预算有限的情况下哪款私域运营工具性价比最高？", "性价比私域工具"},
	}
	auditSvc := geoservice.NewTechConfigService()
	mentioned, auditTotal := 0, 0
	for i, iq := range intents {
		if i >= batchN {
			break
		}
		art, gerr := contentSvc.GenerateContent(ctx, "", iq.kw, brand, advantages, 800, "professional")
		if gerr != nil {
			continue
		}
		vr, verr := verifySvc.VerifyArticle(ctx, geodto.VerifyRequest{ArticleID: art.ID, Query: iq.query, BrandName: brand})
		audit := auditSvc.RunGEOAudit(iq.kw, art.Title, art.Content, "", "")
		auditTotal += audit.Score
		st := "✗"
		if verr == nil && vr != nil && vr.BrandMentioned {
			st = "✓"
			mentioned++
		}
		fmt.Printf("  [%s] %q -> 提及=%s 审计=%d分 len=%d\n", iq.label, truncate(iq.query, 40), st, audit.Score, len([]rune(art.Content)))
	}
	if batchN > 0 && batchN <= len(intents) {
		fmt.Printf("== [D2] 批量生成：%d/%d 提及 | 平均审计分 %.0f ==\n", mentioned, batchN, float64(auditTotal)/float64(batchN))
	}

	fmt.Println("\n✅ GEO 自动化执行完成")
	_ = context.Background()
}

func dumpCounts(db *gorm.DB) {
	type cnt struct {
		label string
		q     string
		args  []any
	}
	items := []cnt{
		{"llm_providers(启用)", "SELECT COUNT(*) FROM llm_providers WHERE enabled = ?", []any{true}},
		{"geo_keywords", "SELECT COUNT(*) FROM geo_keywords", nil},
		{"geo_articles", "SELECT COUNT(*) FROM geo_articles", nil},
		{"geo_verify_results", "SELECT COUNT(*) FROM geo_verify_results", nil},
		{"geo_workflow_executions", "SELECT COUNT(*) FROM geo_workflow_executions", nil},
		{"geo_content_tasks(pending)", "SELECT COUNT(*) FROM geo_content_tasks WHERE status='pending'", nil},
		{"geo_query_chains(probe)", "SELECT COUNT(*) FROM geo_query_chains WHERE source='probe'", nil},
	}
	for _, it := range items {
		var n int64
		if err := db.Raw(it.q, it.args...).Scan(&n).Error; err != nil {
			fmt.Printf("  %-28s 查询失败: %v\n", it.label, err)
			continue
		}
		fmt.Printf("  %-28s %d\n", it.label, n)
	}
}

func dumpLatestArticleAndVerify(db *gorm.DB, brand string) {
	var art geomodel.GeoArticle
	if err := db.Order("created_at DESC").First(&art).Error; err == nil {
		fmt.Printf("\n  最新文章: %s\n    ID=%s 长度=%d字\n    预览: %s\n",
			nonEmpty(art.Title, "(无标题)"), art.ID, len([]rune(art.Content)), preview(art.Content, 120))
	}
	var vr geomodel.GeoVerifyResult
	if err := db.Where("brand_name = ?", brand).Order("created_at DESC").First(&vr).Error; err == nil {
		fmt.Printf("  最新验证: mentioned=%v count=%d sentiment=%s position=%q query=%q\n",
			vr.BrandMentioned, vr.MentionCount, vr.Sentiment, vr.Position, truncate(vr.Query, 40))
	}
	var ex geomodel.GeoWorkflowExecution
	if err := db.Order("created_at DESC").First(&ex).Error; err == nil {
		b, _ := json.Marshal(ex)
		_ = b
		fmt.Printf("  最近执行: id=%s status=%s\n", ex.ID, ex.Status)
	}
}

func nonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func preview(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
