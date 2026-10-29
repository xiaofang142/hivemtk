package service

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	pkgdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ─────────────────────────────────────────────────────────────────────────────
// AutopilotService —— GEO 一键自动驾驶（傻瓜式单关键词全链路）。
//
// 用户输入一个关键词，本服务按预设顺序复用既有 GEO 能力跑完：
// 品牌检查 → LLM 语义扩展 → 五引擎下拉词 → 长尾展开 → 落库 → 文章(生成/评分/Schema)
// → 可选蜘蛛推送 → 可选站点审计 → 汇总。
//
// 设计纪律：
//   - 文章只生成不发布（PlatformPublish 不触碰）；
//   - 出站请求与既有能力同源同量（下拉抓取/推送/站点审计均为既有行为）；
//   - 除 brand_check / save_keywords 外全部 best-effort，单步失败不断链；
//   - 运行体脱离请求 ctx（WithoutCancel + 15min 超时），前端轮询驱动；
//   - 同租户并发闸：HasRunning 命中即拒绝。
// ─────────────────────────────────────────────────────────────────────────────

// autopilot 总时长上限（N 篇文章的 LLM 生成是主要耗时）
const autopilotRunTimeout = 15 * time.Minute

// autopilotArticleMax 单次运行文章数上限（成本护栏）
const autopilotArticleMax = 5

// ErrAutopilotRunning 同租户已有进行中的 run（controller 映射 409 语义）
var ErrAutopilotRunning = errors.New("已有一键任务正在进行中，请等待完成后再启动")

// AutopilotService 一键自动驾驶编排服务
type AutopilotService struct {
	runRepo repository.GeoAutopilotRunRepository
}

// NewAutopilotService 创建自动驾驶服务
func NewAutopilotService(runRepo repository.GeoAutopilotRunRepository) *AutopilotService {
	return &AutopilotService{runRepo: runRepo}
}

// Create 校验参数、落 running 行并异步启动执行。返回运行记录（含 ID）。
func (s *AutopilotService) Create(ctx context.Context, req *dto.AutopilotCreateRequest) (*model.GeoAutopilotRun, error) {
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, errors.New("关键词不能为空")
	}
	if len([]rune(keyword)) > 100 {
		return nil, errors.New("关键词过长（≤100 字）")
	}
	siteURL := strings.TrimSpace(req.SiteURL)
	if siteURL != "" && !strings.HasPrefix(siteURL, "http://") && !strings.HasPrefix(siteURL, "https://") {
		return nil, errors.New("站点 URL 必须以 http(s):// 开头")
	}
	n := req.ArticleCount
	if n <= 0 {
		n = 3
	}
	if n > autopilotArticleMax {
		n = autopilotArticleMax
	}

	running, err := s.runRepo.HasRunning(ctx)
	if err != nil {
		return nil, fmt.Errorf("并发检查失败: %w", err)
	}
	if running {
		return nil, ErrAutopilotRunning
	}

	run := &model.GeoAutopilotRun{
		Keyword:      keyword,
		SiteURL:      siteURL,
		ArticleCount: n,
		Status:       "running",
	}
	run.SetSteps(autopilotPlanSteps(siteURL))
	if err := s.runRepo.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("运行记录创建失败: %w", err)
	}

	// 运行体脱离请求 ctx：前端断开/轮询超时不能杀死任务；DB 写仍带租户绑定
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), autopilotRunTimeout)
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[GEO Autopilot] panic recovered: %v\n%s", r, debug.Stack())
				s.finishRun(runCtx, run, "failed", fmt.Sprintf("内部异常: %v", r))
			}
		}()
		s.execute(runCtx, run)
	}()
	return run, nil
}

// autopilotPlanSteps 依据输入预生成步骤骨架（后续逐步回填状态）
func autopilotPlanSteps(siteURL string) []model.AutopilotStep {
	steps := []model.AutopilotStep{
		{Name: "brand_check", Label: "品牌配置检查", Status: "running"},
		{Name: "keyword_expand", Label: "关键词语义扩展", Status: "running"},
		{Name: "suggest_crawl", Label: "下拉词抓取（5 引擎）", Status: "running"},
		{Name: "longtail_expand", Label: "长尾词展开", Status: "running"},
		{Name: "save_keywords", Label: "关键词落库", Status: "running"},
		{Name: "content_generate", Label: "文章生成/评分/Schema", Status: "running"},
	}
	if siteURL != "" {
		steps = append(steps,
			model.AutopilotStep{Name: "push_urls", Label: "蜘蛛推送", Status: "running"},
			model.AutopilotStep{Name: "site_audit", Label: "站点审计（rubric v2）", Status: "running"},
		)
	}
	steps = append(steps, model.AutopilotStep{Name: "summary", Label: "汇总报告", Status: "running"})
	return steps
}

// execute 步骤状态机：逐步执行、逐步落库（进度可见），终态 success/partial/failed。
// 除 brand_check / save_keywords 外全部 best-effort。
func (s *AutopilotService) execute(ctx context.Context, run *model.GeoAutopilotRun) {
	steps := run.GetSteps()
	// 跨步骤产出收集
	var expanded []string            // 语义扩展词（含原词）
	var suggests []*model.GeoKeyword // 下拉词
	var longtails []*model.GeoKeyword
	saved := 0
	articles := []*model.GeoArticle{}
	scores := []float64{}
	auditScore := -1

	hardFail := func(stepName, msg string) {
		s.markStep(ctx, run, steps, stepName, "failed", msg, nil)
		s.finishRun(ctx, run, "failed", msg)
	}

	// 1. brand_check（硬依赖）
	cfgRepo := repository.NewGeoConfigRepository()
	cfg, err := cfgRepo.Get(ctx)
	if err != nil || cfg == nil || strings.TrimSpace(cfg.BrandName) == "" {
		hardFail("brand_check", "未配置品牌名：请先到「GEO 工具 → 品牌与域名」完成品牌配置后再使用一键功能")
		return
	}
	brand := strings.TrimSpace(cfg.BrandName)
	lang := strings.TrimSpace(cfg.Language)
	if lang == "" {
		lang = "zh"
	}
	advs := splitAdvantages(cfg.Advantages)
	s.markStep(ctx, run, steps, "brand_check", "success", "品牌："+brand, map[string]any{"brand": brand})

	// 2. keyword_expand（LLM，失败降级为只用原词）
	{
		kwSvc := NewKeywordService(
			repository.NewGeoKeywordGroupRepository(),
			repository.NewGeoKeywordRepository(),
			repository.NewGeoAPICallRepository(),
			NewLLMAdapter(),
		)
		expandedKWs, err := kwSvc.SemanticExpand(ctx, []string{run.Keyword}, brand)
		if err != nil || len(expandedKWs) == 0 {
			expanded = []string{run.Keyword}
			detail := "LLM 扩展失败，降级为仅用原词"
			if err == nil {
				detail = "LLM 扩展无产出，降级为仅用原词"
			}
			s.markStep(ctx, run, steps, "keyword_expand", "skipped", detail, map[string]any{"words": expanded})
		} else {
			seen := map[string]bool{run.Keyword: true}
			expanded = []string{run.Keyword}
			for _, k := range expandedKWs {
				w := strings.TrimSpace(k.Keyword)
				if w != "" && !seen[w] {
					seen[w] = true
					expanded = append(expanded, w)
				}
			}
			if len(expanded) > 20 {
				expanded = expanded[:20]
			}
			s.markStep(ctx, run, steps, "keyword_expand", "success",
				fmt.Sprintf("扩展出 %d 个相关词", len(expanded)-1), map[string]any{"words": expanded})
		}
	}

	// 3. suggest_crawl（下拉词，best-effort）
	{
		miningSvc := NewKeywordMiningService(repository.NewGeoKeywordRepository(), pkgdb.GetDB(), NewLLMAdapter())
		outcome := miningSvc.CrawlSuggest(ctx, []string{run.Keyword}, nil)
		suggests = outcome.Keywords
		detail := fmt.Sprintf("下拉词 %d 个", len(suggests))
		if len(outcome.Errors) > 0 {
			detail += fmt.Sprintf("（%d 组引擎请求失败：best-effort 跳过）", len(outcome.Errors))
		}
		status := "success"
		if len(suggests) == 0 && len(outcome.Errors) > 0 {
			status = "skipped"
		}
		s.markStep(ctx, run, steps, "suggest_crawl", status, detail,
			map[string]any{"count": len(suggests), "per_engine": outcome.PerEngine})
	}

	// 4. longtail_expand（离线模板，零成本）
	{
		miningSvc := NewKeywordMiningService(repository.NewGeoKeywordRepository(), pkgdb.GetDB(), NewLLMAdapter())
		lt, err := miningSvc.CombineLongtail(ctx, []string{run.Keyword}, nil)
		if err != nil {
			s.markStep(ctx, run, steps, "longtail_expand", "skipped", "长尾展开失败: "+err.Error(), nil)
		} else {
			longtails = lt
			s.markStep(ctx, run, steps, "longtail_expand", "success",
				fmt.Sprintf("长尾词 %d 个", len(longtails)), map[string]any{"count": len(longtails)})
		}
	}

	// 5. save_keywords（落库 + 意图分类，硬依赖：不落库下游无从查询）
	{
		miningSvc := NewKeywordMiningService(repository.NewGeoKeywordRepository(), pkgdb.GetDB(), NewLLMAdapter())
		all := make([]*model.GeoKeyword, 0, len(expanded)+len(suggests)+len(longtails))
		for _, w := range expanded[1:] { // 原词由 SaveMiningResults 作种子站位
			all = append(all, &model.GeoKeyword{Keyword: w, ParentKeyword: run.Keyword, Source: "ai"})
		}
		all = append(all, suggests...)
		all = append(all, longtails...)
		n, err := miningSvc.SaveMiningResults(ctx, []string{run.Keyword}, all)
		if err != nil {
			hardFail("save_keywords", "关键词落库失败: "+err.Error())
			return
		}
		saved = n
		s.markStep(ctx, run, steps, "save_keywords", "success",
			fmt.Sprintf("落库 %d 条（含种子站位）", saved), map[string]any{"saved": saved})
	}

	// 6. content_generate ×N（逐篇容错；只生成不发布）
	{
		contentSvc := NewContentService(
			repository.NewGeoArticleRepository(),
			repository.NewGeoOptimizationRepository(),
			repository.NewGeoAPICallRepository(),
			repository.NewGeoKnowledgeDocumentRepository(),
			NewLLMAdapter(),
		)
		okN, failN := 0, 0
		for i, w := range expanded {
			if i >= run.ArticleCount || ctx.Err() != nil {
				break
			}
			art, err := contentSvc.GenerateContent(ctx, lang, w, brand, advs, 1200, "professional")
			if err != nil {
				failN++
				continue
			}
			if sc, err := contentSvc.ScoreContent(ctx, art.ID, art.Content, brand, w); err == nil {
				// ScoreContent 形态：{"scores":{"total":N,...}}（LLM 与启发式同构）
				if scObj, ok := sc["scores"].(map[string]any); ok {
					if v, ok := scObj["total"].(float64); ok && v > 0 {
						scores = append(scores, v)
					}
				}
			}
			_, _ = contentSvc.GenerateSchema(ctx, art.ID, brand, firstRunes(art.Content, 120), cfg.Domain)
			articles = append(articles, art)
			okN++
			// 每篇写一次进度（前端轮询粒度）
			s.markStep(ctx, run, steps, "content_generate", "running",
				fmt.Sprintf("已生成 %d/%d 篇", okN, run.ArticleCount),
				map[string]any{"done": okN, "fail": failN})
		}
		if okN == 0 {
			s.markStep(ctx, run, steps, "content_generate", "failed",
				fmt.Sprintf("0 篇成功（失败 %d）", failN), map[string]any{"done": 0, "fail": failN})
		} else {
			detail := fmt.Sprintf("生成 %d 篇", okN)
			if failN > 0 {
				detail += fmt.Sprintf("，失败 %d 篇", failN)
			}
			s.markStep(ctx, run, steps, "content_generate", "success", detail,
				map[string]any{"done": okN, "fail": failN})
		}
	}

	// 7. push_urls（可选：给了站点 URL；未配置推送器时 PushURLs 为 no-op，如实提示）
	if run.SiteURL != "" {
		pushSvc := NewPushService(
			repository.NewGeoPusherConfigRepositoryWithDB(pkgdb.GetDB()),
			repository.NewGeoPushRecordRepositoryWithDB(pkgdb.GetDB()),
			repository.NewGeoArticleRepository(),
		)
		urls := make([]string, 0, len(articles))
		for _, a := range articles {
			u := a.SiteURL
			if u == "" && strings.TrimSpace(cfg.Domain) != "" {
				u = fmt.Sprintf("https://%s%s", strings.Trim(strings.TrimSpace(cfg.Domain), "/"), a.SitePath)
			}
			if u != "" {
				urls = append(urls, u)
			}
		}
		if len(urls) == 0 {
			s.markStep(ctx, run, steps, "push_urls", "skipped", "无新文章 URL 可推送", nil)
		} else if err := pushSvc.PushURLs(ctx, urls); err != nil {
			s.markStep(ctx, run, steps, "push_urls", "skipped", "推送提交失败: "+err.Error(), nil)
		} else {
			s.markStep(ctx, run, steps, "push_urls", "success",
				fmt.Sprintf("已提交 %d 个 URL（未配置推送器时为 no-op，请到「蜘蛛推送配置」确认）", len(urls)),
				map[string]any{"urls": len(urls)})
		}
	}

	// 8. site_audit（可选：rubric v2 站点审计，见 005-geo-site-audit-v2）
	if run.SiteURL != "" {
		rep, err := NewTechConfigService().RunSiteAudit(ctx, run.SiteURL, brand)
		if err != nil {
			s.markStep(ctx, run, steps, "site_audit", "skipped", "站点审计失败: "+err.Error(), nil)
		} else {
			auditScore = rep.Score
			s.markStep(ctx, run, steps, "site_audit", "success",
				fmt.Sprintf("%d/%d（%s）", rep.Score, rep.MaxScore, rep.Grade),
				map[string]any{"score": rep.Score, "max": rep.MaxScore, "grade": rep.Grade, "fixes": len(rep.Fixes)})
		}
	}

	// 9. summary
	avg := 0.0
	if len(scores) > 0 {
		for _, v := range scores {
			avg += v
		}
		avg = avg / float64(len(scores))
	}
	summary := fmt.Sprintf("关键词 %q：扩展词 %d、下拉词 %d、长尾 %d、落库 %d 条、文章 %d 篇（均分 %.0f）",
		run.Keyword, len(expanded), len(suggests), len(longtails), saved, len(articles), avg)
	if auditScore >= 0 {
		summary += fmt.Sprintf("、站点审计 %d 分", auditScore)
	}
	s.markStep(ctx, run, steps, "summary", "success", summary, map[string]any{
		"keywords": len(expanded), "suggests": len(suggests), "longtails": len(longtails),
		"saved": saved, "articles": len(articles), "avg_score": avg, "audit_score": auditScore,
	})

	// 终态：存在 failed 步 ⇒ partial（brand_check/save_keywords 硬失败已提前 return）
	status := "success"
	for _, st := range steps {
		if st.Status == "failed" {
			status = "partial"
			break
		}
	}
	s.finishRun(ctx, run, status, summary)
}

// markStep 回填某步状态并整体落库（含进度与当前步）。
// 中间态落库失败只记日志（运行继续；终态由 finishRun 再落一次）。
func (s *AutopilotService) markStep(ctx context.Context, run *model.GeoAutopilotRun, steps []model.AutopilotStep, name, status, detail string, result map[string]any) {
	done := 0
	for i := range steps {
		if steps[i].Name == name {
			steps[i].Status = status
			steps[i].Detail = detail
			steps[i].DurationMs = time.Since(run.StartedAt).Milliseconds()
			steps[i].Result = result
		}
		if steps[i].Status != "running" {
			done++
		}
	}
	run.SetSteps(steps)
	run.CurrentStep = name
	run.Progress = done * 100 / len(steps)
	if err := s.runRepo.Update(ctx, run); err != nil {
		logger.Warnf("[GEO Autopilot] 步骤进度落库失败 run=%s step=%s: %v", run.ID, name, err)
	}
}

// finishRun 终态落库（progress 拉满、finished_at 补齐；failed 时 error 字段承接摘要）
func (s *AutopilotService) finishRun(ctx context.Context, run *model.GeoAutopilotRun, status, summary string) {
	run.Status = status
	run.Summary = summary
	if status == "failed" && strings.TrimSpace(run.Error) == "" {
		run.Error = summary
	}
	run.Progress = 100
	now := time.Now()
	run.FinishedAt = &now
	if err := s.runRepo.Update(context.WithoutCancel(ctx), run); err != nil {
		logger.Errorf("[GEO Autopilot] 终态落库失败 run=%s: %v", run.ID, err)
	}
}

func firstRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// List 运行历史
func (s *AutopilotService) List(ctx context.Context, page, limit int) ([]*model.GeoAutopilotRun, int64, error) {
	return s.runRepo.List(ctx, page, limit)
}

// Get 运行详情
func (s *AutopilotService) Get(ctx context.Context, id string) (*model.GeoAutopilotRun, error) {
	return s.runRepo.GetByID(ctx, id)
}
