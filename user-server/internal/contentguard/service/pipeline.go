// pipeline.go 异步审查流水线：入队 → 认领 → 审查 → 留痕 → 处置。
//
// 「不阻碍前端任务」的落地方式（三句话）：
//  1. 业务链路只调 Submit：一次单行插入 + 一次非阻塞通知，**不扫描、不调外部 API、
//     不等待处置**。首响与 LLM 流式输出因此与审查完全解耦。
//  2. 审查在 worker 里跑：认领一批 → 本地词库/策略 → 可选外部 API → 写记录 → 处置。
//     慢的被隔离在这里，最多把审查自己的积压拖长，不会回压到 HTTP 请求。
//  3. 队列落库而非纯内存：进程重启/发布不丢在途任务（内存 channel 只做"有新活"的通知，
//     任务真相在 guard_tasks 表里，worker 每轮都从表里认领，重启后自动续跑）。
//
// 与 trace 日志的关系：trace_events 是**旁路可观测**数据（best-effort、队列满即丢、
// metadata 不含全文），所以它不能当审查队列用；本流水线在任务/记录上都带 trace_id，
// 用 trace 做"这条审查属于哪次请求"的关联与回溯，而不是用它承载审查本身。
package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/contentguard/matcher"
	"hivemtk-user/internal/contentguard/model"
	"hivemtk-user/internal/contentguard/provider"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// PipelineConfig 流水线参数。
type PipelineConfig struct {
	// Workers 并发 worker 数（<=0 取 2）。
	Workers int
	// BatchSize 单轮认领条数（<=0 取 32）。
	BatchSize int
	// PollInterval 空转轮询间隔（<=0 取 2s）。
	PollInterval time.Duration
	// MaxAttempts 单任务最大尝试次数（<=0 取 5）。
	MaxAttempts int
	// RetryDelay 失败重试间隔（<=0 取 30s）。
	RetryDelay time.Duration
	// APITimeout 单次外部审核超时（<=0 取 3s）。
	APITimeout time.Duration
	// QueueLen 通知 channel 容量（<=0 取 256）。
	QueueLen int
}

func (c PipelineConfig) withDefaults() PipelineConfig {
	if c.Workers <= 0 {
		c.Workers = 2
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = 30 * time.Second
	}
	if c.APITimeout <= 0 {
		c.APITimeout = 3 * time.Second
	}
	if c.QueueLen <= 0 {
		c.QueueLen = 256
	}
	return c
}

// PipelineStats 流水线计数（运维观测）。
type PipelineStats struct {
	Submitted int64
	Enqueued  int64
	Deduped   int64
	Dropped   int64
	Processed int64
	Failed    int64
	Blocked   int64
	Reviewed  int64
}

// Pipeline 异步审查流水线。
type Pipeline struct {
	engine    *Engine
	tasks     TaskStore
	records   RecordStore
	sanctions *SanctionService
	auditor   provider.Auditor
	cfg       PipelineConfig
	now       func() time.Time

	notify chan struct{}
	stopCh chan struct{}
	doneCh chan struct{}

	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	submitted int64
	enqueued  int64
	deduped   int64
	dropped   int64
	processed int64
	failed    int64
	blocked   int64
	reviewed  int64
}

// NewPipeline 构造流水线（不自动启动，调用方决定 Start/Stop）。
func NewPipeline(engine *Engine, tasks TaskStore, records RecordStore, sanctions *SanctionService, auditor provider.Auditor, cfg PipelineConfig) *Pipeline {
	cfg = cfg.withDefaults()
	return &Pipeline{
		engine:    engine,
		tasks:     tasks,
		records:   records,
		sanctions: sanctions,
		auditor:   auditor,
		cfg:       cfg,
		now:       time.Now,
		notify:    make(chan struct{}, cfg.QueueLen),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Config 生效配置（只读）。
func (p *Pipeline) Config() PipelineConfig {
	if p == nil {
		return PipelineConfig{}
	}
	return p.cfg
}

// Submit 提交一条待审内容。
//
// 返回 false 表示"没有新任务"（内容为空、或幂等键已存在）。**不阻塞**：
// 唯一的同步 IO 是一次插入；插入失败会如实返回 error（调用方据此决定是否记录/重试），
// 但绝不因为审查链路出问题而拒绝业务请求——审查是旁路，业务可用性优先。
func (p *Pipeline) Submit(ctx context.Context, req ScanRequest) (bool, error) {
	if p == nil || p.tasks == nil {
		return false, ErrRuntimeUnavailable
	}
	atomic.AddInt64(&p.submitted, 1)
	if strings.TrimSpace(req.Content) == "" {
		atomic.AddInt64(&p.dropped, 1)
		return false, nil
	}
	if err := ValidateSubjectType(req.SubjectType); err != nil {
		return false, err
	}
	if err := ValidateScene(req.Scene); err != nil {
		return false, err
	}
	digest := ContentDigest(req.SubjectType, req.SubjectID, req.Scene, req.Content)
	tenantID, _ := tenant.FromContext(ctx)
	now := p.now()
	task := &model.GuardTask{
		TenantID:      tenantID,
		TaskKey:       digest,
		TraceID:       req.TraceID,
		SubjectType:   req.SubjectType,
		SubjectID:     req.SubjectID,
		Scene:         req.Scene,
		Content:       req.Content,
		ContentDigest: digest,
		Status:        model.TaskStatusPending,
		NextRunAt:     now,
	}
	created, err := p.tasks.Enqueue(ctx, task)
	if err != nil {
		atomic.AddInt64(&p.dropped, 1)
		return false, err
	}
	if !created {
		atomic.AddInt64(&p.deduped, 1)
		return false, nil
	}
	atomic.AddInt64(&p.enqueued, 1)
	p.kick()
	return true, nil
}

func (p *Pipeline) kick() {
	select {
	case p.notify <- struct{}{}:
	default:
		// 通知只是"有新活"的提示，丢了也不影响正确性：worker 每轮都会从表里认领，
		// 最坏情况延迟一个 PollInterval。这里绝不阻塞调用方。
	}
}

// Start 启动 worker。
func (p *Pipeline) Start() {
	if p == nil {
		return
	}
	p.startOnce.Do(func() {
		for i := 0; i < p.cfg.Workers; i++ {
			p.wg.Add(1)
			go p.loop()
		}
		logger.Infof("[content-guard] 审查流水线已启动：workers=%d batch=%d poll=%s auditor=%v",
			p.cfg.Workers, p.cfg.BatchSize, p.cfg.PollInterval, p.auditor != nil)
	})
}

// Stop 优雅停止：关闭信号 → 等当前轮跑完（不再接受新任务）。
func (p *Pipeline) Stop(ctx context.Context) {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		close(p.stopCh)
		done := make(chan struct{})
		go func() { // gospawn-ok: 等待型 helper（wg.Wait→close done，CC-01 白名单），无业务 panic 面
			p.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			logger.Warnf("[content-guard] 停止超时，仍有 worker 在跑（在途任务已落库，下次启动续跑）")
		}
		close(p.doneCh)
	})
}

func (p *Pipeline) loop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()
	for {
		p.drain(context.Background())
		select {
		case <-p.stopCh:
			return
		case <-p.notify:
		case <-ticker.C:
		}
	}
}

func (p *Pipeline) drain(parent context.Context) {
	for {
		select {
		case <-p.stopCh:
			return
		default:
		}
		n, err := p.ProcessDue(parent, p.cfg.BatchSize)
		if err != nil {
			logger.Warnf("[content-guard] 认领/处理失败：%v", err)
			return
		}
		if n < p.cfg.BatchSize {
			return
		}
	}
}

// ProcessDue 处理一轮到期任务，返回处理条数（供 worker 与测试直接调用）。
func (p *Pipeline) ProcessDue(ctx context.Context, limit int) (int, error) {
	if p == nil || p.tasks == nil {
		return 0, ErrRuntimeUnavailable
	}
	if limit <= 0 {
		limit = p.cfg.BatchSize
	}
	claimCtx := p.claimContext(ctx)
	claimed, err := p.tasks.ClaimDue(claimCtx, limit, p.now())
	if err != nil {
		return 0, err
	}
	for i := range claimed {
		p.processOne(ctx, &claimed[i])
	}
	return len(claimed), nil
}

// claimContext 认领是**跨租户**的系统动作（worker 不属于任何租户）。
//
// 为什么不直接裸 ctx：TENANT_MODE=on 时隔离 callback 会拒绝未绑定租户的查询
// （fail-closed），后台清扫类任务必须显式走系统作用域这一条豁免通道。
func (p *Pipeline) claimContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tenant.IsMultiTenant() {
		return tenant.WithSystemScope(ctx)
	}
	return tenant.ContextWithDefault(ctx)
}

func (p *Pipeline) taskContext(ctx context.Context, task *model.GuardTask) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if task.TenantID != 0 {
		return tenant.WithTenantID(ctx, task.TenantID)
	}
	return tenant.ContextWithDefault(ctx)
}

func (p *Pipeline) processOne(parent context.Context, task *model.GuardTask) {
	ctx := p.taskContext(parent, task)
	req := ScanRequest{
		SubjectType: task.SubjectType,
		SubjectID:   task.SubjectID,
		Scene:       task.Scene,
		Content:     task.Content,
		TraceID:     task.TraceID,
	}
	assessment, err := p.engine.Scan(ctx, req)
	if err != nil {
		p.failTask(ctx, task, err)
		return
	}

	// evalHits 是"参与裁决的命中集合"，比 assessment.RawHits 多一条可能来自外部 API 的合成命中。
	//
	// 为什么要单独拿一个变量：下面的升级重算必须用**与产生当前结论相同**的命中集合。
	// 若重算时只喂 RawHits，那么"本地词库没命中、外部 API 判高风险"这条最需要拦的
	// 情况会被重算成 pass（MaxSeverity 仍是 3 而 Verdict 变 pass，记录自相矛盾）。
	evalHits := assessment.RawHits

	// 外部审核（L3）：仅当策略开启且抽样命中；失败一律降级为仅本地结论。
	if p.auditor != nil && assessment.Policy != nil && ShouldSampleAPI(assessment.Policy, assessment.Digest) {
		apiCtx, cancel := context.WithTimeout(ctx, p.cfg.APITimeout)
		res, apiErr := p.auditor.Audit(apiCtx, provider.Request{
			TenantID:    task.TenantID,
			SubjectType: task.SubjectType,
			SubjectID:   task.SubjectID,
			Scene:       task.Scene,
			Content:     task.Content,
			TraceID:     task.TraceID,
		})
		cancel()
		if apiErr != nil {
			// 失败不留"api 已参与"的假象：sources 只记 dict，错误进日志。
			logger.Warnf("[content-guard] 外部审核失败，降级本地结论：subject=%s/%s err=%v",
				task.SubjectType, task.SubjectID, apiErr)
		} else {
			assessment.Sources = appendUnique(assessment.Sources, "api")
			if len(res.Labels) > 0 {
				assessment.Hits = append(assessment.Hits, model.GuardHit{
					Word:     strings.Join(res.Labels, ","),
					Category: "api",
					Severity: res.RiskLevel,
				})
			}
			if res.RiskLevel > assessment.MaxSeverity {
				// 外部结论更严时以其为准：本地词库永远落后于攻防，L3 的价值就在这里。
				evalHits = append(append(make([]matcher.Hit, 0, len(assessment.RawHits)+1), assessment.RawHits...), syntheticHit(res.RiskLevel))
				assessment.Decision = Evaluate(assessment.Policy, evalHits, 0)
				if res.RiskLevel > 0 {
					assessment.MaxSeverity = res.RiskLevel
				}
			}
		}
	}

	// 升级判定需要"窗口内该主体此前被拦截几次"，只有拿到 block 结论时才付这次查询。
	if assessment.Verdict == model.VerdictBlock && p.records != nil && assessment.Policy != nil && !assessment.Truncated {
		since := p.now().Add(-time.Duration(assessment.Policy.WindowMinutes) * time.Minute)
		prior, cErr := p.records.CountBlocksSince(ctx, task.SubjectType, task.SubjectID, since)
		if cErr == nil {
			assessment.Decision = Evaluate(assessment.Policy, evalHits, prior)
		}
	}

	record := &model.GuardRecord{
		TenantID:       task.TenantID,
		TaskID:         task.ID,
		TraceID:        task.TraceID,
		SubjectType:    task.SubjectType,
		SubjectID:      task.SubjectID,
		Scene:          task.Scene,
		Verdict:        assessment.Verdict,
		Action:         actionOrDefault(assessment.Decision),
		MaxSeverity:    assessment.MaxSeverity,
		Hits:           assessment.Hits,
		Sources:        strings.Join(nonEmpty(assessment.Sources, "none"), ","),
		ContentDigest:  task.ContentDigest,
		ContentSnippet: assessment.Snippet,
		LatencyMs:      assessment.LatencyMs,
		ManualStatus:   model.ManualStatusNone,
	}
	if assessment.Verdict == model.VerdictReview {
		record.ManualStatus = model.ManualStatusPending
	}
	if err := p.records.Create(ctx, record); err != nil {
		p.failTask(ctx, task, err)
		return
	}

	// 处置：只在 block 且策略给出冻结档位时施加。
	if assessment.Verdict == model.VerdictBlock && assessment.FreezeLevel != "" && p.sanctions != nil {
		reason := buildReason(assessment)
		duration := 0
		if assessment.Policy != nil {
			duration = assessment.Policy.TempFreezeMinutes
		}
		sanction, sErr := p.sanctions.Apply(ctx, ApplyInput{
			SubjectType:     task.SubjectType,
			SubjectID:       task.SubjectID,
			Kind:            model.SanctionKindFreeze,
			Level:           assessment.FreezeLevel,
			Reason:          reason,
			Source:          "auto",
			RecordID:        record.ID,
			TraceID:         task.TraceID,
			DurationMinutes: duration,
		})
		if sErr != nil {
			logger.Warnf("[content-guard] 处置施加失败：subject=%s/%s level=%s err=%v",
				task.SubjectType, task.SubjectID, assessment.FreezeLevel, sErr)
		} else if sanction != nil {
			record.SanctionID = sanction.ID
			if err := p.records.SetSanctionID(ctx, record.ID, sanction.ID); err != nil {
				logger.Warnf("[content-guard] 回填处置号失败 record=%d sanction=%d err=%v",
					record.ID, sanction.ID, err)
			}
		}
	}

	if err := p.tasks.MarkDone(ctx, task.ID); err != nil {
		logger.Warnf("[content-guard] 任务标记完成失败 task=%d err=%v", task.ID, err)
	}
	atomic.AddInt64(&p.processed, 1)
	switch assessment.Verdict {
	case model.VerdictBlock:
		atomic.AddInt64(&p.blocked, 1)
	case model.VerdictReview:
		atomic.AddInt64(&p.reviewed, 1)
	}
}

func (p *Pipeline) failTask(ctx context.Context, task *model.GuardTask, cause error) {
	atomic.AddInt64(&p.failed, 1)
	attempts := task.Attempts + 1
	var retryAt time.Time
	if attempts < p.cfg.MaxAttempts {
		retryAt = p.now().Add(p.cfg.RetryDelay)
	}
	if err := p.tasks.MarkFailed(ctx, task.ID, cause.Error(), retryAt); err != nil {
		logger.Warnf("[content-guard] 任务失败留痕失败 task=%d err=%v", task.ID, err)
	}
}

func syntheticHit(severity int) matcher.Hit {
	return matcher.Hit{Severity: severity}
}

// Stats 计数快照。
func (p *Pipeline) Stats() PipelineStats {
	if p == nil {
		return PipelineStats{}
	}
	return PipelineStats{
		Submitted: atomic.LoadInt64(&p.submitted),
		Enqueued:  atomic.LoadInt64(&p.enqueued),
		Deduped:   atomic.LoadInt64(&p.deduped),
		Dropped:   atomic.LoadInt64(&p.dropped),
		Processed: atomic.LoadInt64(&p.processed),
		Failed:    atomic.LoadInt64(&p.failed),
		Blocked:   atomic.LoadInt64(&p.blocked),
		Reviewed:  atomic.LoadInt64(&p.reviewed),
	}
}

func actionOrDefault(d Decision) string {
	if d.Action == "" {
		return model.ActionNone
	}
	return d.Action
}

func buildReason(a *Assessment) string {
	var b strings.Builder
	b.WriteString("命中违规内容（")
	b.WriteString(a.Verdict)
	b.WriteString("，最高级别 ")
	b.WriteString(itoa(uint64(a.MaxSeverity)))
	b.WriteString("）：")
	if len(a.Hits) > 0 {
		words := make([]string, 0, len(a.Hits))
		for _, h := range a.Hits {
			if h.Word == "" {
				continue
			}
			words = append(words, h.Word)
			if len(words) >= 5 {
				break
			}
		}
		b.WriteString(strings.Join(words, "、"))
	}
	return b.String()
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func nonEmpty(list []string, fallback string) []string {
	if len(list) == 0 {
		return []string{fallback}
	}
	return list
}

// IsClosed 是否已停止（健康检查用）。
func (p *Pipeline) IsClosed() bool {
	if p == nil {
		return true
	}
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
}
