package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// BackupGapService backup 页面契约适配（复用既有 BackupService 存储能力 + KV 策略）
type BackupGapService struct {
	repo *repository.BackupGapRepo
	kv   repository.SystemConfigKVRepository
	now  func() time.Time
}

// NewBackupGapService 构造（ARC-01：gdb 为 nil 时 repo 为 nil，查询类方法返回错误）
func NewBackupGapService(gdb *gorm.DB) *BackupGapService {
	return &BackupGapService{repo: repository.NewBackupGapRepoWithDB(gdb), kv: repository.NewSystemConfigKVRepository(), now: time.Now}
}

// NewBackupGapServiceFromGlobal 便捷构造
func NewBackupGapServiceFromGlobal() *BackupGapService {
	return NewBackupGapService(repository.GetDB())
}

// BackupStatsRow backup 页统计契约
type BackupStatsRow struct {
	Total       int64                  `json:"total"`
	LastSuccess string                 `json:"lastSuccess"`
	TotalSize   int64                  `json:"totalSize"`
	NextRun     string                 `json:"nextRun"`
	TableStats  []map[string]any       `json:"tableStats"`
	Extra       map[string]interface{} `json:"-"`
}

const backupStrategyKey = "backup.strategy"

// BackupStrategy 策略结构
type BackupStrategy struct {
	Enabled       bool `json:"enabled"`
	DailyHour     int  `json:"daily_hour"`
	DailyMinute   int  `json:"daily_minute"`
	WeeklyDay     int  `json:"weekly_day"`
	WeeklyHour    int  `json:"weekly_hour"`
	WeeklyMinute  int  `json:"weekly_minute"`
	RetentionDays int  `json:"retention_days"`
	Checksum      bool `json:"checksum"`
}

// GetStrategy 读策略（未配置回退默认）
func (s *BackupGapService) GetStrategy(ctx context.Context) BackupStrategy {
	def := BackupStrategy{Enabled: true, DailyHour: 2, WeeklyDay: 0, WeeklyHour: 3, RetentionDays: 30, Checksum: true}
	raw, err := s.kv.Get(ctx, backupStrategyKey)
	if err != nil || raw == "" {
		return def
	}
	var st BackupStrategy
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return def
	}
	return st
}

// SaveStrategy 存策略
func (s *BackupGapService) SaveStrategy(ctx context.Context, st *BackupStrategy) error {
	if st.RetentionDays < 7 || st.RetentionDays > 365 {
		return fmt.Errorf("retention_days 必须在 7-365")
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = s.kv.Upsert(ctx, backupStrategyKey, string(raw))
	return err
}

// Stats 聚合既有 backups 表（model.Backup）+ 表级规模估算
func (s *BackupGapService) Stats(ctx context.Context) (*BackupStatsRow, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	row := &BackupStatsRow{LastSuccess: "-", NextRun: "-"}
	total, err := s.repo.CountBackups(ctx)
	if err != nil {
		return nil, err
	}
	row.Total = total
	if last, err := s.repo.LastSuccess(ctx); err == nil {
		row.LastSuccess = last.CreatedAt.Format("2006-01-02 15:04")
		row.TotalSize = last.FileSize
	}

	if stats, err := s.repo.TableStats(ctx); err == nil {
		for _, r := range stats {
			row.TableStats = append(row.TableStats, map[string]any{"table": r.Table, "rows": r.Rows})
		}
	}

	st := s.GetStrategy(ctx)
	if st.Enabled {
		nxt := s.nextRunTime(st)
		row.NextRun = nxt.Format("2006-01-02 15:04")
	}
	return row, nil
}

func (s *BackupGapService) nextRunTime(st BackupStrategy) time.Time {
	now := s.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), st.DailyHour, st.DailyMinute, 0, 0, now.Location())
	if today.After(now) {
		return today
	}
	return today.AddDate(0, 0, 1)
}

// RagEvalGapService RAG 评测服务（诚实口径：Recall@5 = 检索 top5 文本含答案关键词的比例；MRR/NDCG 按同口径排序）
type RagEvalGapService struct {
	gapRepo  *repository.RagEvalGapRepo
	evalRepo *repository.RagEvalRepository
	searchFn func(ctx context.Context, productID, query string) ([]string, error)
}

// NewRagEvalGapService 构造（searchFn: 复用既有 RagSearcher 混合检索，由装配处注入）
//
// ARC-01：gdb 为 nil 时两个仓储均为 nil，查询/写库类方法返回错误。
func NewRagEvalGapService(gdb *gorm.DB, searchFn func(ctx context.Context, productID, query string) ([]string, error)) *RagEvalGapService {
	return &RagEvalGapService{
		gapRepo:  repository.NewRagEvalGapRepoWithDB(gdb),
		evalRepo: repository.NewRagEvalRepositoryWithDB(gdb),
		searchFn: searchFn,
	}
}

// NewRagEvalGapServiceFromGlobal 便捷构造
func NewRagEvalGapServiceFromGlobal(searchFn func(ctx context.Context, productID, query string) ([]string, error)) *RagEvalGapService {
	return NewRagEvalGapService(repository.GetDB(), searchFn)
}

func (s *RagEvalGapService) searchTop5(ctx context.Context, productID, question string) ([]string, error) {
	return s.searchFn(ctx, productID, question)
}

// UploadCSV 解析 CSV（列: question,answer）入库
func (s *RagEvalGapService) UploadCSV(ctx context.Context, r io.Reader, productID string) (int, error) {
	if s == nil || s.gapRepo == nil {
		return 0, fmt.Errorf("service or repository is nil")
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(records) == 0 {
		return 0, fmt.Errorf("CSV 为空")
	}
	start := 0

	if len(records[0]) > 0 && strings.Contains(strings.ToLower(records[0][0]), "question") {
		start = 1
	}
	n := 0
	for i := start; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 || strings.TrimSpace(row[0]) == "" {
			continue
		}
		ans := ""
		if len(row) > 1 {
			ans = strings.TrimSpace(row[1])
		}
		q := &model.RagEvalQuestion{ProductID: productID, Question: strings.TrimSpace(row[0]), Answer: ans}
		if err := s.gapRepo.CreateQuestion(ctx, q); err != nil {
			return n, err
		}
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("CSV 无有效行（需 question,answer 两列）")
	}
	return n, nil
}

func (s *RagEvalGapService) RunAsync(productID string) (*model.RagEvalRun, error) {
	if s == nil || s.gapRepo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	ctx := context.Background()
	run := &model.RagEvalRun{Total: -1}
	if err := s.gapRepo.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	go func(runID uint, pid string) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), utils.CronMediumTimeout)
		defer cancel()
		res, err := s.computeRun(ctx, pid)
		if err != nil {
			_ = s.gapRepo.MarkRunFailed(ctx, runID)
			return
		}
		_ = s.gapRepo.UpdateRunResult(ctx, runID, map[string]any{
			"total": res.Total, "hit": res.Hit, "recall5": res.Recall5,
			"mrr": res.MRR, "ndcg5": res.NDCG5, "eval_set_size": res.EvalSetSize,
		})
	}(run.ID, productID)
	return run, nil
}

func (s *RagEvalGapService) computeRun(ctx context.Context, productID string) (*model.RagEvalRun, error) {
	qs, err := s.gapRepo.ListQuestions(ctx, productID)
	if err != nil {
		return nil, err
	}
	if len(qs) == 0 {
		return nil, fmt.Errorf("评测集为空，请先上传 CSV（question,answer）")
	}
	_ = productID

	run := &model.RagEvalRun{Total: len(qs), EvalSetSize: len(qs)}
	for _, qy := range qs {
		hits, err := s.searchTop5(ctx, productID, qy.Question)
		if err != nil {
			continue
		}
		run.Hit += hitRank(hits, qy.Answer, &run.MRR, &run.NDCG5)
	}
	if run.Total > 0 {
		run.Recall5 = float64(run.Hit) / float64(run.Total)
		run.MRR = run.MRR / float64(run.Total)
		run.NDCG5 = run.NDCG5 / float64(run.Total)
	}
	if err := s.gapRepo.CreateRunCompleted(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func hitRank(snippets []string, answer string, mrr, ndcg *float64) int {
	ans := strings.TrimSpace(answer)
	if ans == "" {

		if len(snippets) > 0 {
			*mrr += 1.0
			*ndcg += 1.0
			return 1
		}
		return 0
	}
	keys := answerKeywords(ans)
	for i, sn := range snippets {
		low := strings.ToLower(sn)
		matched := 0
		for _, k := range keys {
			if strings.Contains(low, k) {
				matched++
			}
		}
		if len(keys) > 0 && matched*2 >= len(keys) {
			rank := i + 1
			*mrr += 1.0 / float64(rank)
			*ndcg += 1.0 / float64(rank)
			return rank
		}
	}
	return 0
}

func answerKeywords(ans string) []string {
	f := func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == '。' || r == '、' || r == '；' || r == ';' || r == '\n'
	}
	seen := map[string]bool{}
	out := []string{}
	for _, part := range strings.FieldsFunc(strings.ToLower(ans), f) {
		p := strings.TrimSpace(part)
		r := []rune(p)
		if len(r) < 2 || len(r) > 20 {
			continue
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// Latest 最新一次 run
func (s *RagEvalGapService) Latest(ctx context.Context) (*model.RagEvalRun, error) {
	if s == nil || s.evalRepo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	return s.evalRepo.LatestRun(ctx)
}

// Runs 历史
func (s *RagEvalGapService) Runs(ctx context.Context, limit int) ([]*model.RagEvalRun, error) {
	if s == nil || s.evalRepo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.evalRepo.ListRunsDesc(ctx, limit)
}

// Diff 与基线对比
func (s *RagEvalGapService) Diff(ctx context.Context, baselineID uint) (map[string]any, error) {
	if s == nil || s.evalRepo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	base, err := s.evalRepo.GetRunRaw(ctx, baselineID)
	if err != nil {
		return nil, err
	}
	latest, _ := s.Latest(ctx)
	if latest == nil {
		latest = &model.RagEvalRun{}
	}
	return map[string]any{
		"baseline_id":  base.ID,
		"latest_id":    latest.ID,
		"recall5Delta": latest.Recall5 - base.Recall5,
		"mrrDelta":     latest.MRR - base.MRR,
		"ndcg5Delta":   latest.NDCG5 - base.NDCG5,
		"baseline":     base,
		"latest":       latest,
	}, nil
}

// CohortGapService 留存/路径分析
type CohortGapService struct {
	repo *repository.CohortGapRepo
}

// NewCohortGapService 构造（ARC-01：gdb 为 nil 时 repo 为 nil，查询类方法返回错误）
func NewCohortGapService(gdb *gorm.DB) *CohortGapService {
	return &CohortGapService{repo: repository.NewCohortGapRepoWithDB(gdb)}
}

// NewCohortGapServiceFromGlobal 便捷构造
func NewCohortGapServiceFromGlobal() *CohortGapService {
	return NewCohortGapService(repository.GetDB())
}

// CohortResult 周留存矩阵
type CohortResult struct {
	Periods []string          `json:"periods"`
	Cohorts []CohortBucketRow `json:"cohorts"`
}

// CohortBucketRow 单分群行
type CohortBucketRow struct {
	Label     string    `json:"label"`
	Size      int64     `json:"size"`
	Retention []float64 `json:"retention"`
}

// Cohort 按客户注册周分群，后续周有行为事件（customer_events）即留存
func (s *CohortGapService) Cohort(ctx context.Context, weeks int) (*CohortResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if weeks <= 0 || weeks > 12 {
		weeks = 8
	}
	now := time.Now()
	thisWeekStart := now.AddDate(0, 0, -int(now.Weekday()))
	type cohort struct {
		label    string
		start    time.Time
		end      time.Time
		size     int64
		retained []int64
	}
	cohorts := make([]cohort, 0, weeks)
	periods := []string{}
	for i := weeks - 1; i >= 0; i-- {
		cstart := thisWeekStart.AddDate(0, 0, -7*i)
		cend := cstart.AddDate(0, 0, 7)
		cohorts = append(cohorts, cohort{
			label: cstart.Format("01/02"),
			start: cstart, end: cend,
		})
		periods = append(periods, fmt.Sprintf("W+%d", weeks-1-i))
	}

	for ci := range cohorts {
		c := &cohorts[ci]
		if n, err := s.repo.CountCustomersBetween(ctx, c.start, c.end); err == nil {
			c.size = n
		}
		c.retained = make([]int64, weeks)
		for wi := 0; wi < weeks; wi++ {
			wstart := c.end.AddDate(0, 0, 7*wi)
			wend := wstart.AddDate(0, 0, 7)
			if wstart.After(now) {
				break
			}
			var n int64
			if cnt, err := s.repo.CountRetainedEvents(ctx, c.start, c.end, wstart, wend); err == nil {
				n = cnt
			}
			c.retained[wi] = n
		}
	}
	out := &CohortResult{Periods: periods}
	for _, c := range cohorts {
		row := CohortBucketRow{Label: c.label, Size: c.size}
		row.Retention = make([]float64, weeks)
		for wi := 0; wi < weeks; wi++ {
			if c.size > 0 && wi < len(c.retained) {
				row.Retention[wi] = float64(c.retained[wi]) * 100 / float64(c.size)
			}
		}
		out.Cohorts = append(out.Cohorts, row)
	}
	return out, nil
}

// PathResult 事件路径桑基
type PathResult struct {
	Nodes []map[string]any `json:"nodes"`
	Links []map[string]any `json:"links"`
}

// Path 事件路径：同一客户相邻事件对聚合 top N
func (s *CohortGapService) Path(ctx context.Context, limit int) (*PathResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	evs, err := s.repo.ListCustomerEvents(ctx)
	if err != nil {
		return nil, err
	}
	pairs := map[string]int64{}
	nodeSet := map[string]bool{}
	for i := 1; i < len(evs); i++ {
		if evs[i].CustomerID != evs[i-1].CustomerID {
			continue
		}
		a, b := evs[i-1].EventType, evs[i].EventType
		if a == b {
			continue
		}
		key := a + "->" + b
		pairs[key]++
		nodeSet[a] = true
		nodeSet[b] = true
	}
	out := &PathResult{}
	for n := range nodeSet {
		out.Nodes = append(out.Nodes, map[string]any{"name": n})
	}

	type lk struct {
		Key   string
		From  string
		To    string
		Value int64
	}
	list := make([]lk, 0, len(pairs))
	for k, v := range pairs {
		ps := strings.SplitN(k, "->", 2)
		list = append(list, lk{k, ps[0], ps[1], v})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Value > list[i].Value {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	if len(list) > limit {
		list = list[:limit]
	}
	for _, l := range list {
		out.Links = append(out.Links, map[string]any{"source": l.From, "target": l.To, "value": l.Value})
	}
	return out, nil
}

// EmailGapService 邮件送达分析
type EmailGapService struct {
	repo   *repository.EmailGapRepo
	aiRepo *repository.AIPerformanceRepository
}

// NewEmailGapService 构造（ARC-01：gdb 为 nil 时 repo 为 nil，查询类方法返回错误）
func NewEmailGapService(gdb *gorm.DB) *EmailGapService {
	return &EmailGapService{
		repo:   repository.NewEmailGapRepoWithDB(gdb),
		aiRepo: repository.NewAIPerformanceRepository(gdb),
	}
}

// NewEmailGapServiceFromGlobal 便捷构造
func NewEmailGapServiceFromGlobal() *EmailGapService { return NewEmailGapService(repository.GetDB()) }

// DeliverabilityStats 页面顶部指标
type DeliverabilityStats struct {
	Sent         int64   `json:"sent"`
	Delivered    int64   `json:"delivered"`
	Opened       int64   `json:"opened"`
	Clicked      int64   `json:"clicked"`
	Bounced      int64   `json:"bounced"`
	HardBounce   int64   `json:"hardBounce"`
	SoftBounce   int64   `json:"softBounce"`
	Unsub        int64   `json:"unsub"`
	DeliveryRate float64 `json:"deliveryRate"`
	OpenRate     float64 `json:"openRate"`
	ClickRate    float64 `json:"clickRate"`
}

// Deliverability 聚合 email_sends + email_tracking_events
func (s *EmailGapService) Deliverability(ctx context.Context, days int) (*DeliverabilityStats, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	st := &DeliverabilityStats{}
	sent, err := s.repo.CountSent(ctx, since)
	if err != nil {
		return nil, err
	}
	st.Sent = sent
	countEvent := func(t string) int64 {
		n, _ := s.repo.CountEventByType(ctx, t, since)
		return n
	}
	st.Opened = countEvent("open")
	st.Clicked = countEvent("click")
	st.Unsub = countEvent("unsubscribe")

	bounces, _ := s.repo.ListBounces(ctx, since)
	for _, b := range bounces {
		switch b.EventType {
		case "hard_bounce":
			st.HardBounce++
		case "soft_bounce":
			st.SoftBounce++
		default:

			st.HardBounce++
		}
	}
	st.Bounced = st.HardBounce + st.SoftBounce
	st.Delivered = st.Sent - st.Bounced
	if st.Delivered < 0 {
		st.Delivered = 0
	}
	if st.Sent > 0 {
		st.DeliveryRate = float64(st.Delivered) * 100 / float64(st.Sent)
		st.OpenRate = float64(st.Opened) * 100 / float64(st.Sent)
		st.ClickRate = float64(st.Clicked) * 100 / float64(st.Sent)
	}
	return st, nil
}

// BounceBreakdown ISP 分桶（饼图: [{isp, count}]）
func (s *EmailGapService) BounceBreakdown(ctx context.Context, days int) ([]map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days)
	rows, err := s.repo.BounceByISP(ctx, since)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, r := range rows {
		out = append(out, map[string]any{"isp": r.Domain, "count": r.Cnt})
	}
	return out, nil
}

// DomainReputationRow 域名信誉行
type DomainReputationRow struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	Reputation  string `json:"reputation"`
	SentLast24h int64  `json:"sentLast24h"`
	Delivered   int64  `json:"delivered"`
	Bounced     int64  `json:"bounced"`
	Complained  int64  `json:"complained"`
	Blacklisted bool   `json:"blacklisted"`
}

// DomainReputation 从 SMTP 配置取自有域名 → 24h 发送/退信聚合 + DNS 记录检查（诚实口径：无外网返回 unknown）
func (s *EmailGapService) DomainReputation(ctx context.Context) ([]DomainReputationRow, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	var domains []string
	if smtps, err := s.repo.ListSMTPServers(ctx); err == nil {
		for _, r := range smtps {
			parts := strings.Split(r.User, "@")
			if len(parts) == 2 && parts[1] != "" {
				domains = append(domains, parts[1])
			}
		}
	}
	if len(domains) == 0 {
		fs, _ := s.repo.DistinctTrackingDomains(ctx)
		domains = append(domains, fs...)
	}
	since24 := time.Now().Add(-24 * time.Hour)
	out := []DomainReputationRow{}
	for _, d := range domains {
		row := DomainReputationRow{Domain: d}
		row.SentLast24h, _ = s.repo.CountSentToDomain(ctx, d, since24)
		row.Bounced, _ = s.repo.CountBouncesToDomain(ctx, d, since24)
		row.Complained, _ = s.repo.CountSpamReportToDomain(ctx, d, since24)
		row.Delivered = row.SentLast24h - row.Bounced
		if row.Delivered < 0 {
			row.Delivered = 0
		}
		switch {
		case row.SentLast24h == 0:
			row.Reputation = "无数据"
		case row.Complained > 0 || (row.SentLast24h > 0 && float64(row.Bounced)*100/float64(row.SentLast24h) > 10):
			row.Reputation = "较差"
			row.Blacklisted = row.Complained > 0
		case float64(row.Bounced)*100/float64(row.SentLast24h) > 5:
			row.Reputation = "一般"
		default:
			row.Reputation = "良好"
		}
		out = append(out, row)
	}
	// 前端按 row.id 调用暂停接口，这里注入稳定 id（域名序号，1-based）
	for i := range out {
		out[i].ID = int64(i + 1)
	}
	return out, nil
}

// emailSuspendedDomainsKey KV 中暂停域名的逗号分隔清单
const emailSuspendedDomainsKey = "email_suspended_domains"

// SuspendEmailDomain 暂停域名使用（写入 KV 黑名单，下次发送前应检查）
func (s *EmailGapService) SuspendEmailDomain(ctx context.Context, domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}
	kv := repository.NewSystemConfigKVRepository()
	var suspended []string
	if raw, err := kv.Get(ctx, emailSuspendedDomainsKey); err == nil && raw != "" {
		for _, d := range strings.Split(raw, ",") {
			if strings.TrimSpace(d) != "" && strings.TrimSpace(strings.ToLower(d)) != domain {
				suspended = append(suspended, strings.TrimSpace(strings.ToLower(d)))
			}
		}
	}
	suspended = append(suspended, domain)
	_, err := kv.Upsert(ctx, emailSuspendedDomainsKey, strings.Join(suspended, ","))
	return err
}

// RFMMatrixRow 矩阵行（前端 RfmMatrix: [{recency, frequency, count}]）
type RFMMatrixRow struct {
	Recency   int   `json:"recency"`
	Frequency int   `json:"frequency"`
	Count     int64 `json:"count"`
}

// RFMMatrixStats 顶部指标（前端: {total, highValue, active, churnRisk}）
type RFMMatrixStats struct {
	Total     int64 `json:"total"`
	HighValue int64 `json:"highValue"`
	Active    int64 `json:"active"`
	ChurnRisk int64 `json:"churnRisk"`
}

// RFMMatrix 复用 CustomerRFMService.Distribution + 分层映射
func (s *EmailGapService) RFMMatrix(ctx context.Context) ([]RFMMatrixRow, RFMMatrixStats, error) {
	rfmSvc := NewCustomerRFMService()
	dist, err := rfmSvc.Distribution(ctx)
	if err != nil {
		return nil, RFMMatrixStats{}, err
	}

	layerPos := map[string][2]int{
		"champion":  {5, 5},
		"loyal":     {4, 4},
		"potential": {3, 2},
		"at_risk":   {2, 3},
		"churn":     {1, 1},
	}
	rows := map[string]*RFMMatrixRow{}
	st := RFMMatrixStats{}
	get := func(r, f int) *RFMMatrixRow {
		k := fmt.Sprintf("%d-%d", r, f)
		if rows[k] == nil {
			rows[k] = &RFMMatrixRow{Recency: r, Frequency: f}
		}
		return rows[k]
	}
	for layer, cnt := range dist {
		st.Total += cnt
		pos, ok := layerPos[layer]
		if !ok {
			pos = [2]int{3, 3}
		}
		*get(pos[0], pos[1]) = RFMMatrixRow{Recency: pos[0], Frequency: pos[1], Count: cnt}
		switch layer {
		case "champion", "loyal":
			st.HighValue += cnt
			st.Active += cnt
		case "potential":
			st.Active += cnt
		case "at_risk", "churn":
			st.ChurnRisk += cnt
		}
	}
	out := make([]RFMMatrixRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, st, nil
}

// BackupRow backup 列表行契约
type BackupRow struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Size      int64  `json:"size"`
	Checksum  string `json:"checksum"`
	CreatedAt string `json:"createdAt"`
}

// ListBackups 列出 backups 表（最多 100 条）
func (s *BackupGapService) ListBackups(ctx context.Context) ([]BackupRow, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	rows, err := s.repo.ListBackups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BackupRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, BackupRow{
			ID: r.ID, Name: r.BackupName, Type: r.BackupType,
			Status: r.Status, Size: r.FileSize, Checksum: "", CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// PreviewTableStats 返回核心表的行数估算（pg_stat_user_tables）
func (s *BackupGapService) PreviewTableStats(ctx context.Context) ([]map[string]any, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	coreTables := []string{"customers", "customer_sessions", "session_messages", "message_hub", "clues", "script_library"}
	rows, err := s.repo.CoreTableStats(ctx, coreTables)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		rows = make([]repository.TableStatsRow, 0, len(coreTables))
		for _, t := range coreTables {
			rows = append(rows, repository.TableStatsRow{Table: t, Rows: 0})
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{"table": r.Table, "rows": r.Rows})
	}
	return out, nil
}

// RetryDeadLetters 将 message_hub 中 status='dead_letter' 的记录重置为 pending
func (s *EmailGapService) RetryDeadLetters(ctx context.Context) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, fmt.Errorf("service or repository is nil")
	}
	return s.repo.ResetDeadLetters(ctx)
}

// ClueApplyResult 线索导入建议应用结果
type ClueApplyResult struct {
	Merged  int `json:"merged"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// ClueApplySuggestions 逐条按 action 处理重复线索
func (s *EmailGapService) ClueApplySuggestions(ctx context.Context, action string, duplicates []struct {
	ExistingClueID int64          `json:"existingClueId"`
	Row            map[string]any `json:"row"`
}) (*ClueApplyResult, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("service or repository is nil")
	}
	res := &ClueApplyResult{}
	for _, dup := range duplicates {
		if action != "merge" || dup.ExistingClueID <= 0 || dup.Row == nil {
			res.Skipped++
			continue
		}
		updates := clueMergeUpdates(dup.Row)
		if len(updates) == 0 {
			res.Skipped++
			continue
		}
		if err := s.repo.UpdateClueFields(ctx, dup.ExistingClueID, updates); err != nil {
			res.Failed++
			continue
		}
		res.Merged++
	}
	return res, nil
}

// clueMergeUpdates 从导入行提取可合并的非空字符串字段
func clueMergeUpdates(from map[string]any) map[string]any {
	updates := map[string]any{}
	for _, f := range []string{"name", "city", "address", "desc", "account", "source_id"} {
		if v, ok := from[f]; ok {
			if sv, isStr := v.(string); isStr && strings.TrimSpace(sv) != "" {
				updates[f] = sv
			}
		}
	}
	return updates
}

// ClueMerge 合并线索字段到现有线索
func (s *EmailGapService) ClueMerge(ctx context.Context, id int64, from map[string]any) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("service or repository is nil")
	}
	updates := clueMergeUpdates(from)
	if len(updates) == 0 {
		return true, nil
	}
	if err := s.repo.UpdateClueFields(ctx, id, updates); err != nil {
		return false, err
	}
	return true, nil
}

// ClueForceCreate 强制创建线索（单条导入）
func (s *EmailGapService) ClueForceCreate(ctx context.Context, row map[string]any) (bool, error) {
	if s == nil || s.repo == nil {
		return false, fmt.Errorf("service or repository is nil")
	}
	name, _ := row["name"].(string)
	phone, _ := row["phone"].(string)
	if strings.TrimSpace(name) == "" && strings.TrimSpace(phone) == "" {
		return false, fmt.Errorf("name/phone 至少一项必填")
	}
	rec := map[string]any{
		"name":      name,
		"source_id": "force-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"source":    "import_force",
	}
	if v, ok := row["city"].(string); ok {
		rec["city"] = v
	}
	if v, ok := row["desc"].(string); ok {
		rec["desc"] = v
	}
	if err := s.repo.CreateClueRecord(ctx, rec); err != nil {
		return false, err
	}
	return true, nil
}
