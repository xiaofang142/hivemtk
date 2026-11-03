// Package service T4 知识库冷启动：爬取源管理 + 爬取批次管道 + 变更检测。
//
// 设计文档：docs/architecture/design/T4-kb-coldstart.md
//
// 管道：来源配置 → 调度触发（manual/cron）→ 建批次 → robots 检查 → sitemap/链接
// 发现 → 限速抓取 → goquery 正文抽取 → 内容哈希变更判定 → 入库（IngestCrawledPage，
// 复用 KnowledgeBaseService 分段→向量化→索引链路）→ QA 拆分异步任务（kb_qa_split.go）。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	kbcrawler "hivemtk-user/internal/aiagent/knowledge/kbcrawler"
	knowledgeModel "hivemtk-user/internal/aiagent/knowledge/model"
	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// —— T4 配额（§8.2）——

const (
	MaxSourcesPerTenant     = 10   // 每租户源数
	DefaultMaxPages         = 200  // 单源单批次页面数
	MaxDailyCrawlPages      = 2000 // 每租户每日爬取页面数
	MaxConsecutiveFailBatch = 3    // 连续失败批次数 → error
	MaxRetryPerBatch        = 3    // failed 批次最多重试次数
	defaultIntervalHours    = 168  // 7 天
)

// CrawlerService 爬取服务（对齐 KnowledgeBaseService 风格）
type CrawlerService struct {
	db     *gorm.DB
	kb     *KnowledgeBaseService
	http   *http.Client
	extr   kbcrawler.Extractor
	qa     *QASplitService // 可选：qa_split 开启时注入
	mu     sync.Mutex
	hashes map[string]map[string]string // source_id → url → contentHash
}

// NewCrawlerService 构造
func NewCrawlerService(db *gorm.DB, kb *KnowledgeBaseService) *CrawlerService {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
			// SSRF 防护（§8.1）：建连前校验解析出的 IP，内网段一律拒绝
			// （RFC1918/loopback/link-local）。Control 回调签名带 RawConn（Go 1.20+）。
			Control: func(_ string, address string, _ syscall.RawConn) error {
				return checkPublicAddr(address)
			},
		}).DialContext,
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("重定向次数过多")
			}
			// 重定向跨 host 一律终止该 URL（§8.1）
			if req.URL.Host != via[0].URL.Host {
				return errors.New("重定向跨域已终止")
			}
			return nil
		},
	}
	return &CrawlerService{
		db:     db,
		kb:     kb,
		http:   client,
		extr:   kbcrawler.NewGoqueryExtractor(),
		hashes: map[string]map[string]string{},
	}
}

// SetQASplitService 注入 QA 拆分服务（qa_split 开启时装配）
func (s *CrawlerService) SetQASplitService(qa *QASplitService) { s.qa = qa }

// KB 返回底层 KnowledgeBaseService（controller publish 链路用）
func (s *CrawlerService) KB() *KnowledgeBaseService { return s.kb }

// checkPublicAddr SSRF：解析地址属内网段则拒绝建连
func checkPublicAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return fmt.Errorf("域名解析失败: %s", host)
		}
		ip = net.ParseIP(addrs[0])
		if ip == nil {
			return fmt.Errorf("无效地址: %s", host)
		}
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("禁止访问内网地址: %s", ip)
	}
	return nil
}

// —— 源管理（§6.1 校验顺序：参数绑定 → 枚举 → scheme → 归属 → 配额 → 落库）——

// CreateSourceInput 创建源参数
type CreateSourceInput struct {
	KBID            uint   `json:"kb_id"`
	SourceType      string `json:"source_type"` // url / sitemap
	StartURL        string `json:"start_url"`
	IncludePatterns string `json:"include_patterns"`
	MaxPages        int    `json:"max_pages"`
	IntervalHours   int    `json:"interval_hours"`
	AutoResync      *bool  `json:"auto_resync"`
	QASplitEnabled  *bool  `json:"qa_split_enabled"`
}

// CreateSource 创建爬取源（先校验后落库，对齐 ImportDocument 惯例）。
// 站点验证 token 建源时生成：租户放到目标站点根路径 hivemtk-verify.txt 或
// 首页 <meta name="hivemtk-site-verify"> —— 未验证的源不发起任何请求（§8.1 硬门槛）。
func (s *CrawlerService) CreateSource(ctx context.Context, in *CreateSourceInput) (*model.KBCrawlerSource, error) {
	if in.SourceType != "url" && in.SourceType != "sitemap" {
		return nil, errors.New("source_type 必须是 url 或 sitemap")
	}
	u, err := url.Parse(in.StartURL)
	if err != nil || u.Host == "" {
		return nil, errors.New("start_url 非法")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("start_url 仅支持 http/https")
	}
	// KB 归属租户校验（租户隔离 callback 在 on 模式下已注入 WHERE tenant_id）
	var kbRow model.KnowledgeBase
	if err := s.db.WithContext(ctx).First(&kbRow, in.KBID).Error; err != nil {
		return nil, errors.New("目标知识库不存在")
	}
	// 配额：每租户源数
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.KBCrawlerSource{}).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt >= MaxSourcesPerTenant {
		return nil, fmt.Errorf("爬取源数量已达上限（%d 个）", MaxSourcesPerTenant)
	}

	if in.MaxPages <= 0 {
		in.MaxPages = DefaultMaxPages
	}
	if in.MaxPages > DefaultMaxPages*5 {
		in.MaxPages = DefaultMaxPages * 5 // 硬上限 1000
	}
	if in.IntervalHours <= 0 {
		in.IntervalHours = defaultIntervalHours
	}
	if in.IntervalHours > 30*24 {
		in.IntervalHours = 30 * 24 // 1~30 天（§1.1 Chatbase 对齐）
	}

	src := &model.KBCrawlerSource{
		KBID:            in.KBID,
		SourceType:      in.SourceType,
		StartURL:        in.StartURL,
		IncludePatterns: in.IncludePatterns,
		MaxPages:        in.MaxPages,
		IntervalHours:   in.IntervalHours,
		AutoResync:      in.AutoResync,
		QASplitEnabled:  in.QASplitEnabled,
		Status:          model.KBCrawlerSourceStatusActive,
		SiteVerifyToken: verifyToken(),
	}
	if err := s.db.WithContext(ctx).Create(src).Error; err != nil {
		return nil, fmt.Errorf("保存爬取源失败: %w", err)
	}
	return src, nil
}

// verifyToken 生成站点验证 token（时间+随机熵，无需加密强度）
func verifyToken() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("hivemtk-verify-%d", time.Now().UnixNano())))
	return hex.EncodeToString(h[:])[:32]
}

// UpdateSource 更新源（改 start_url 不支持——视为新建源重建）
func (s *CrawlerService) UpdateSource(ctx context.Context, id uint, fields map[string]any) error {
	var src model.KBCrawlerSource
	if err := s.db.WithContext(ctx).First(&src, id).Error; err != nil {
		return errors.New("爬取源不存在")
	}
	allowed := map[string]bool{"include_patterns": true, "max_pages": true, "interval_hours": true, "auto_resync": true, "qa_split_enabled": true}
	for k := range fields {
		if !allowed[k] {
			delete(fields, k)
		}
	}
	if len(fields) == 0 {
		return errors.New("无可更新字段（start_url 不允许修改，请新建源）")
	}
	return s.db.WithContext(ctx).Model(&src).Updates(fields).Error
}

// TransitionSource 源状态跃迁（active⇄paused，error→active 人工恢复）
func (s *CrawlerService) TransitionSource(ctx context.Context, id uint, to string) error {
	var src model.KBCrawlerSource
	if err := s.db.WithContext(ctx).First(&src, id).Error; err != nil {
		return errors.New("爬取源不存在")
	}
	if !model.KBCrawlerSourceTransitionAllowed(src.Status, to) {
		return fmt.Errorf("非法状态跃迁: %s → %s", src.Status, to)
	}
	return s.db.WithContext(ctx).Model(&src).Updates(map[string]any{"status": to, "error_msg": ""}).Error
}

// DeleteSource 软删源（不删已入库文档）
func (s *CrawlerService) DeleteSource(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&model.KBCrawlerSource{}, id).Error
}

// ListSources 列出源。kbID=0 不限知识库；status 为空不限状态（active/paused/error）。
//
// 状态筛选必须落在 SQL 上：前端“状态”下拉此前只是把 status 发上来，
// 服务层忽略后即使筛“已暂停”也照旧返回全部行，看起来像筛选坏了。
func (s *CrawlerService) ListSources(ctx context.Context, kbID uint, status string) ([]model.KBCrawlerSource, error) {
	var out []model.KBCrawlerSource
	q := s.db.WithContext(ctx).Order("id DESC")
	if kbID > 0 {
		q = q.Where("kb_id = ?", kbID)
	}
	if status = strings.TrimSpace(status); status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Find(&out).Error
	return out, err
}

// VerifySite 域名验证（§8.1 硬门槛）：抓根路径 hivemtk-verify.txt 或首页，
// 检查验证 token（<meta name="hivemtk-site-verify" content="token">）。
func (s *CrawlerService) VerifySite(ctx context.Context, id uint) error {
	var src model.KBCrawlerSource
	if err := s.db.WithContext(ctx).First(&src, id).Error; err != nil {
		return errors.New("爬取源不存在")
	}
	if src.SiteVerified {
		return nil
	}
	u, err := url.Parse(src.StartURL)
	if err != nil {
		return errors.New("start_url 非法")
	}
	// 根路径校验文件优先
	if body, status, err := s.fetch(ctx, u.Scheme+"://"+u.Host+"/hivemtk-verify.txt"); err == nil &&
		status == http.StatusOK && strings.Contains(body, src.SiteVerifyToken) {
		return s.db.WithContext(ctx).Model(&src).Updates(map[string]any{"site_verified": true}).Error
	}
	// 首页 meta 标签兜底
	if body, status, err := s.fetch(ctx, src.StartURL); err == nil &&
		status == http.StatusOK && strings.Contains(body, src.SiteVerifyToken) {
		return s.db.WithContext(ctx).Model(&src).Updates(map[string]any{"site_verified": true}).Error
	}
	return errors.New("站点验证失败：请在目标站点根路径放置 hivemtk-verify.txt（内容为验证 token）或首页加 <meta name=\"hivemtk-site-verify\" content=\"...\"> 标签")
}

// fetch 抓取 URL，返回 body（≤5MB）与 status
func (s *CrawlerService) fetch(ctx context.Context, rawURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", kbcrawler.UserAgent)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var sb strings.Builder
	tmp := make([]byte, 32<<10)
	total := 0
	for total < 5<<20 {
		n, err := resp.Body.Read(tmp)
		sb.Write(tmp[:n])
		total += n
		if err != nil {
			break
		}
	}
	return sb.String(), resp.StatusCode, nil
}

// —— 批次（§6.2）——

// CreateBatch 手动/定时触发一次同步：建 pending 批次行。
// 未通过站点验证的源不发起任何请求（硬门槛）——直接报错。
func (s *CrawlerService) CreateBatch(ctx context.Context, sourceID uint, trigger string) (*model.KBCrawlBatch, error) {
	var src model.KBCrawlerSource
	if err := s.db.WithContext(ctx).First(&src, sourceID).Error; err != nil {
		return nil, errors.New("爬取源不存在")
	}
	if src.Status != model.KBCrawlerSourceStatusActive {
		return nil, fmt.Errorf("源状态为 %s，不可触发同步", src.Status)
	}
	if !src.SiteVerified {
		return nil, errors.New("源未通过站点验证，不能同步（先调用验证接口）")
	}
	// 每租户每日页面配额（§8.2）
	var todayPages int64
	s.db.WithContext(ctx).Model(&model.KBCrawlBatch{}).
		Where("created_at >= ?", time.Now().Format("2006-01-02")).
		Select("COALESCE(SUM(pages_found),0)").Scan(&todayPages)
	if todayPages >= MaxDailyCrawlPages {
		return nil, errors.New("今日爬取页面配额已用完，批次推迟到次日")
	}
	batch := &model.KBCrawlBatch{
		SourceID: src.ID,
		BatchNo:  fmt.Sprintf("crawl_%d", time.Now().UnixNano()),
		Trigger:  trigger,
		Status:   model.KBCrawlBatchStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(batch).Error; err != nil {
		return nil, err
	}
	return batch, nil
}

// RunBatch 同步执行一个 pending 批次（cron worker / 手动触发后异步执行）。
// 单源串行限速（间隔 ≥ max(Crawl-delay, 1s)），单批次并发 1。
func (s *CrawlerService) RunBatch(ctx context.Context, batchID uint64) error {
	var batch model.KBCrawlBatch
	if err := s.db.WithContext(ctx).First(&batch, batchID).Error; err != nil {
		return errors.New("批次不存在")
	}
	if batch.Status != model.KBCrawlBatchStatusPending {
		return fmt.Errorf("批次状态为 %s，不可执行", batch.Status)
	}
	var src model.KBCrawlerSource
	if err := s.db.WithContext(ctx).First(&src, batch.SourceID).Error; err != nil {
		return errors.New("爬取源不存在")
	}

	now := time.Now()
	s.db.WithContext(ctx).Model(&batch).Updates(map[string]any{
		"status": model.KBCrawlBatchStatusRunning, "started_at": now,
	})

	batchCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	counters, runErr := s.crawl(batchCtx, &src, &batch)

	finished := time.Now()
	updates := map[string]any{
		"pages_found":     counters.found,
		"pages_new":       counters.newPages,
		"pages_changed":   counters.changed,
		"pages_unchanged": counters.unchanged,
		"pages_failed":    counters.failed,
		"finished_at":     finished,
	}
	status := model.KBCrawlBatchStatusSucceeded
	switch {
	case runErr != nil:
		status = model.KBCrawlBatchStatusFailed
		updates["error_msg"] = runErr.Error()
	case counters.failed > 0:
		status = model.KBCrawlBatchStatusPartiallyFailed
	}
	updates["status"] = status
	if err := s.db.WithContext(ctx).Model(&batch).Updates(updates).Error; err != nil {
		return err
	}

	// 更新源：last_crawl_at / last_batch_id；连续 3 批失败自动置 error
	srcUpdates := map[string]any{"last_crawl_at": finished, "last_batch_id": batch.BatchNo}
	if status == model.KBCrawlBatchStatusFailed {
		var consecFail int64
		s.db.WithContext(ctx).Model(&model.KBCrawlBatch{}).
			Where("source_id = ? AND status = ? AND created_at >= ?",
				src.ID, model.KBCrawlBatchStatusFailed, time.Now().AddDate(0, 0, -7)).
			Count(&consecFail)
		if consecFail >= MaxConsecutiveFailBatch && src.Status == model.KBCrawlerSourceStatusActive {
			srcUpdates["status"] = model.KBCrawlerSourceStatusError
			srcUpdates["error_msg"] = fmt.Sprintf("连续 %d 批失败，已自动暂停；请排查后人工恢复", consecFail)
		}
	}
	return s.db.WithContext(ctx).Model(&src).Updates(srcUpdates).Error
}

// RetryBatch 重试 failed 批次：新建批次行（旧行保持 failed 不覆盖，只增事件流）。
func (s *CrawlerService) RetryBatch(ctx context.Context, batchID uint64) (*model.KBCrawlBatch, error) {
	var old model.KBCrawlBatch
	if err := s.db.WithContext(ctx).First(&old, batchID).Error; err != nil {
		return nil, errors.New("批次不存在")
	}
	if old.Status != model.KBCrawlBatchStatusFailed {
		return nil, errors.New("仅失败批次可重试")
	}
	if old.RetryCount >= MaxRetryPerBatch {
		return nil, fmt.Errorf("已重试 %d 次，达到上限", MaxRetryPerBatch)
	}
	batch := &model.KBCrawlBatch{
		SourceID:   old.SourceID,
		BatchNo:    fmt.Sprintf("crawl_%d", time.Now().UnixNano()),
		Trigger:    old.Trigger,
		Status:     model.KBCrawlBatchStatusPending,
		RetryCount: old.RetryCount + 1,
	}
	if err := s.db.WithContext(ctx).Create(batch).Error; err != nil {
		return nil, err
	}
	return batch, nil
}

// ListBatches 批次列表
func (s *CrawlerService) ListBatches(ctx context.Context, sourceID uint, page, pageSize int) ([]model.KBCrawlBatch, int64, error) {
	var out []model.KBCrawlBatch
	var total int64
	q := s.db.WithContext(ctx).Model(&model.KBCrawlBatch{})
	if sourceID > 0 {
		q = q.Where("source_id = ?", sourceID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&out).Error
	return out, total, err
}

// —— 爬取核心 ——

type crawlCounters struct {
	found, newPages, changed, unchanged, failed int
}

// crawl 单批次管道执行
func (s *CrawlerService) crawl(ctx context.Context, src *model.KBCrawlerSource, batch *model.KBCrawlBatch) (crawlCounters, error) {
	var counters crawlCounters

	// robots.txt 检查（§4.3）：失败 fail-open；401/403 全站禁爬
	rules, err := kbcrawler.FetchRobots(ctx, s.http, src.StartURL)
	if err != nil {
		rules = nil
	}
	limiter := kbcrawler.NewHostLimiter(rules.Delay())

	// URL 发现
	var targets []string
	if src.SourceType == "sitemap" {
		var lastmod *time.Time
		if src.LastCrawlAt != nil {
			lastmod = src.LastCrawlAt
		}
		entries, err := kbcrawler.ParseSitemap(ctx, s.http, src.StartURL, map[string]bool{}, lastmod)
		if err != nil {
			return counters, fmt.Errorf("sitemap 解析失败: %w", err)
		}
		for _, e := range entries {
			if rules.Allowed(e.Loc) {
				targets = append(targets, e.Loc)
			}
		}
	} else {
		discovered, err := s.discoverByLinks(ctx, rules, limiter, src)
		if err != nil && len(discovered) == 0 {
			return counters, err
		}
		targets = discovered
	}

	if len(targets) > src.MaxPages {
		targets = targets[:src.MaxPages]
	}
	counters.found = len(targets)

	include := strings.Split(src.IncludePatterns, ",")
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		if !rules.Allowed(target) || !kbcrawler.MatchInclude(target, include) {
			continue
		}
		if !limiter.Acquire(ctx) {
			break
		}
		if err := s.crawlOne(ctx, src, target, &counters); err != nil {
			counters.failed++
		}
	}
	return counters, nil
}

// discoverByLinks url 型源：起始页 BFS，深度 ≤3，总量 ≤ MaxPages
func (s *CrawlerService) discoverByLinks(ctx context.Context, rules *kbcrawler.RobotsRules, limiter *kbcrawler.HostLimiter, src *model.KBCrawlerSource) ([]string, error) {
	include := strings.Split(src.IncludePatterns, ",")
	type item struct {
		url   string
		depth int
	}
	visited := map[string]bool{src.StartURL: true}
	queue := []item{{src.StartURL, 0}}
	var out []string
	for len(queue) > 0 && len(out) < src.MaxPages {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur.url)
		if cur.depth >= 3 || len(out) >= src.MaxPages {
			continue
		}
		if !limiter.Acquire(ctx) {
			break
		}
		body, status, err := s.fetch(ctx, cur.url)
		if err != nil || status != http.StatusOK {
			continue
		}
		for _, link := range kbcrawler.ExtractLinks(body, cur.url, include) {
			if visited[link] || !rules.Allowed(link) {
				continue
			}
			visited[link] = true
			queue = append(queue, item{link, cur.depth + 1})
		}
	}
	return out, nil
}

// crawlOne 单页：抓取 → 抽取 → 哈希变更判定 → 入库/跳过
func (s *CrawlerService) crawlOne(ctx context.Context, src *model.KBCrawlerSource, pageURL string, counters *crawlCounters) error {
	body, status, err := s.fetch(ctx, pageURL)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("抓取失败 %s: HTTP %d", pageURL, status)
	}
	page, err := s.extr.Extract(body, pageURL)
	if err != nil {
		return err // ErrEmptyPage（JS 壳/导航页）等：计入 failed，批次日志可见
	}
	hash := ContentHash(page.Title, page.Content)

	if s.prevHash(src.ID, pageURL) == hash {
		counters.unchanged++
		return nil // 哈希命中：跳过 ImportDocument 全链路（Chatbase 成本控制点）
	}

	docID, isNew, err := s.IngestCrawledPage(ctx, src, pageURL, page)
	if err != nil {
		return fmt.Errorf("入库失败 %s: %w", pageURL, err)
	}
	s.setHash(src.ID, pageURL, hash)
	if isNew {
		counters.newPages++
	} else {
		counters.changed++
	}
	// QA 拆分（源级开关，默认开）
	if s.qa != nil && (src.QASplitEnabled == nil || *src.QASplitEnabled) {
		s.qa.EnqueueDocument(docID, src.KBID)
	}
	return nil
}

// prevHash 读上次内容哈希（内存快照；进程重启后首爬全量视为变更，走一次重建不丢数据）
func (s *CrawlerService) prevHash(sourceID uint, pageURL string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.hashes[fmt.Sprint(sourceID)]
	if m == nil {
		return ""
	}
	return m[pageURL]
}

func (s *CrawlerService) setHash(sourceID uint, pageURL, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprint(sourceID)
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	s.hashes[key][pageURL] = hash
}

// IngestCrawledPage 爬得页面入库：落 kb_documents（file_type=".html"、content 直存、
// title 存 URL 作变更检测约定键）→ ProcessDocumentByID 复用分段→向量化→索引全链路。
// 变更页：旧 chunks 标 superseded 后重建。返回 (docID, isNew, err)。
func (s *CrawlerService) IngestCrawledPage(ctx context.Context, src *model.KBCrawlerSource, pageURL string, page *kbcrawler.Page) (uint, bool, error) {
	var existing model.KBDocument
	err := s.db.WithContext(ctx).Where("title = ? AND file_type = ?", pageURL, ".html").First(&existing).Error
	if err == nil {
		// 变更重建：旧 chunks 标 superseded
		s.db.WithContext(ctx).Table("kb_document_chunks").
			Where("document_id = ? AND status = ?", existing.ID, "active").
			Update("status", "superseded")
		s.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"content": page.Content, "status": knowledgeModel.KBDocumentStatusPending, "error_msg": "",
		})
		go s.kb.ProcessDocumentByID(context.Background(), existing.ID)
		return existing.ID, false, nil
	}

	doc := &model.KBDocument{
		TenantID: src.TenantID,
		Title:    pageURL,
		Content:  page.Content,
		FileType: ".html",
		FileSize: int64(len(page.Content)),
		Status:   knowledgeModel.KBDocumentStatusPending,
	}
	if err := s.db.WithContext(ctx).Create(doc).Error; err != nil {
		return 0, true, fmt.Errorf("保存爬取文档失败: %w", err)
	}
	go s.kb.ProcessDocumentByID(context.Background(), doc.ID)
	return doc.ID, true, nil
}

// ContentHash 页面内容哈希：sha256(title + "\n" + content)
func ContentHash(title, content string) string {
	h := sha256.Sum256([]byte(title + "\n" + content))
	return hex.EncodeToString(h[:])
}
