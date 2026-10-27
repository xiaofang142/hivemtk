package sms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/identity"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// SmsBlockType 黑名单类型
type SmsBlockType string

const (
	SmsBlockCarrier    SmsBlockType = "carrier"
	SmsBlockBusiness   SmsBlockType = "business"
	SmsBlockRegulatory SmsBlockType = "regulatory"
	SmsBlockContent    SmsBlockType = "content"
)

// SmsDeliveryTrackerService 短信到达率追踪服务
type SmsDeliveryTrackerService struct {
	tracking     *SmsTrackingService
	repo         repository.SmsTrackingRepository
	deliveryRepo repository.SmsDeliveryRepository
	smsRepo      repository.SmsRepository

	carrierMu        sync.RWMutex
	carrierCache     map[string]model.SmsCarrier
	carrierLoaded    bool
	carrierLoadErrAt time.Time
}

// NewSmsDeliveryTrackerService 创建短信到达率追踪服务
//
// 句柄由 repository 自取全局库（测试用 db.SetTestDB／testutil 换库）；repo 可显式注入。
func NewSmsDeliveryTrackerService(tracking *SmsTrackingService, repo repository.SmsTrackingRepository) *SmsDeliveryTrackerService {
	if tracking == nil {
		tracking = NewSmsTrackingService(repo)
	}
	if repo == nil {
		repo = repository.NewSmsTrackingRepositoryDefault()
	}
	return &SmsDeliveryTrackerService{
		tracking:      tracking,
		repo:          repo,
		deliveryRepo:  repository.NewSmsDeliveryRepositoryDefault(),
		smsRepo:       repository.NewSmsRepository(),
		carrierCache:  make(map[string]model.SmsCarrier),
		carrierLoaded: false,
	}
}

// DetectCarrierFromPhone 简单规则识别运营商（前 7 位号段）
//
// 真实生产应使用第三方号段库（如 https://github.com/ls0f/phone）
// 此处为不引入新依赖的简化版本，覆盖主流号段。
func DetectCarrierFromPhone(phone string) model.SmsCarrier {
	phone = identity.NormalizePhone(phone)
	if len(phone) < 7 {
		return model.SmsCarrierUnknown
	}
	prefix := phone[:7]
	switch {
	case hasPrefix(prefix, []string{"134", "135", "136", "137", "138", "139", "150", "151", "152", "157", "158", "159", "182", "183", "184", "187", "188", "198"}):
		return model.SmsCarrierMobile
	case hasPrefix(prefix, []string{"130", "131", "132", "155", "156", "166", "175", "176", "185", "186"}):
		return model.SmsCarrierUnicom
	case hasPrefix(prefix, []string{"133", "149", "153", "173", "174", "177", "180", "181", "189", "199"}):
		return model.SmsCarrierTelecom
	}
	return model.SmsCarrierUnknown
}

func hasPrefix(prefix string, candidates []string) bool {
	for _, c := range candidates {
		if len(c) <= len(prefix) && prefix[:len(c)] == c {
			return true
		}
	}
	return false
}

// DetectAndRecordPortability 检测并记录携号转网
//
// 流程：
//  1. 读取运营商 webhook 推送的 carrier（权威）
//  2. 与内存缓存的"上一次运营商"对比
//  3. 若发生变化 → 写入 sms_number_portability_logs
func (s *SmsDeliveryTrackerService) DetectAndRecordPortability(ctx context.Context, phone, webhookCarrier string) error {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return errors.New("phone 不能为空")
	}

	var newCarrier model.SmsCarrier
	switch strings.ToLower(strings.TrimSpace(webhookCarrier)) {
	case "mobile", "中国移动", "cmcc", "yidong":
		newCarrier = model.SmsCarrierMobile
	case "unicom", "中国联通", "cu", "liantong":
		newCarrier = model.SmsCarrierUnicom
	case "telecom", "中国电信", "ct", "dianxin":
		newCarrier = model.SmsCarrierTelecom
	default:
		newCarrier = DetectCarrierFromPhone(phone)
	}
	if newCarrier == model.SmsCarrierUnknown {
		return nil
	}

	// 基线：内存缓存优先，取不到才回库补一份。
	//
	// 顺序要留意 —— 不能先把本号码写进缓存再去回库：`loadCarrierCache` 只填空缺
	// （`if _, ok := s.carrierCache[r.Phone]; !ok`），预写会把库里的基线挡掉，
	// 回库之后读回来的就是自己刚写进去的 newCarrier ⇒ `original == newCarrier`
	// 恒成立 ⇒ 「本进程第一次见到这个号码」永远不会被记成一条转网流水。
	// 库里的基线（上一次回执的 carrier）因此只在「同进程内第二次遇到」时才起作用。
	s.carrierMu.RLock()
	original, exists := s.carrierCache[phone]
	s.carrierMu.RUnlock()

	if !exists {
		s.loadCarrierCache(ctx)
		s.carrierMu.RLock()
		original, exists = s.carrierCache[phone]
		s.carrierMu.RUnlock()
	}

	s.carrierMu.Lock()
	s.carrierCache[phone] = newCarrier
	s.carrierMu.Unlock()

	// 一条基线都没有（缓存里没有、库里也没有）⇒ 无法判定「变了」。
	// 不写流水：一行 OriginalCarrier 为空的「转网」事件只会在报表里混进没有前值的噪音。
	if !exists {
		return nil
	}
	if original == newCarrier {
		return nil
	}

	rec := &model.SmsNumberPortabilityRecord{
		Phone:           phone,
		OriginalCarrier: original,
		CurrentCarrier:  newCarrier,
		DetectedAt:      time.Now(),
		Source:          "webhook",
		RawPayload:      fmt.Sprintf(`{"phone":%q,"carrier":%q}`, phone, webhookCarrier),
		CreatedAt:       time.Now(),
	}
	if s.deliveryRepo == nil {
		return nil
	}
	// 携号转网流水是「事后出报表」用的旁路凭据，不是回执主流程：落库失败不该
	// 让运营商重发整条回执（`RecordDeliveryReport` 那次写入已经成功并返回 nil）。
	// 但**必须留下日志** —— 原来的写法是仓储句柄缺失时 `return nil`，调用方把这个
	// nil 读成「已记录」，于是这张表可以一直是空的而日志里一行错都没有：等要出
	// 「携号转网失败率」报表时才发现没有数据，且无从判断是「没发生过」还是「一直
	// 写不进去」。唯一的生产调用方 `RecordDeliveryReport` 的 goroutine 里就是
	// `_ = s.DetectAndRecordPortability(...)`（错误被直接丢掉），所以在这里
	// 显式记 ERROR 是这条旁路唯一能被看见的地方。
	if err := s.deliveryRepo.CreatePortability(ctx, rec); err != nil {
		logger.Errorf("[SmsDeliveryTracker] 携号转网流水落库失败 phone=%s %s→%s: %v（该条不重试，报表将缺这一行）",
			phone, original, newCarrier, err)
	}
	return nil
}

func (s *SmsDeliveryTrackerService) loadCarrierCache(ctx context.Context) {
	s.carrierMu.RLock()
	loaded := s.carrierLoaded
	errRecent := !s.carrierLoadErrAt.IsZero() && time.Since(s.carrierLoadErrAt) < utils.LongTimeout
	s.carrierMu.RUnlock()
	if loaded || s.deliveryRepo == nil || errRecent {
		return
	}
	rows, err := s.deliveryRepo.LoadLatestPortability(ctx, 10000)
	if err != nil {
		logger.Errorf("[SmsDeliveryTracker] load carrier cache: %v", err)
		s.carrierMu.Lock()
		s.carrierLoadErrAt = time.Now()
		s.carrierMu.Unlock()
		return
	}
	s.carrierMu.Lock()
	for _, r := range rows {
		if _, ok := s.carrierCache[r.Phone]; !ok {
			s.carrierCache[r.Phone] = r.CurrentCarrier
		}
	}
	s.carrierLoaded = true
	s.carrierMu.Unlock()
}

// GetCurrentCarrier 查询号码当前归属运营商
func (s *SmsDeliveryTrackerService) GetCurrentCarrier(ctx context.Context, phone string) model.SmsCarrier {
	phone = identity.NormalizePhone(phone)
	s.carrierMu.RLock()
	if c, ok := s.carrierCache[phone]; ok {
		s.carrierMu.RUnlock()
		return c
	}
	s.carrierMu.RUnlock()

	return DetectCarrierFromPhone(phone)
}

// ListPortabilityRecords 列出携号转网记录（分页 + 手机号过滤）
//
// 设计：直接查询 sms_number_portability_logs，不走内存缓存。
// 限流：limit 上限 200，避免一次拉全表。
func (s *SmsDeliveryTrackerService) ListPortabilityRecords(ctx context.Context, phone string, page, limit int) ([]model.SmsNumberPortabilityRecord, int64, error) {
	if s == nil || s.deliveryRepo == nil {
		return nil, 0, errors.New("service or delivery repo is nil")
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}

	rows, total, err := s.deliveryRepo.ListPortability(ctx, identity.NormalizePhone(phone), page, limit)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// SmsBlacklistRecord 黑名单记录（聚合）
type SmsBlacklistRecord struct {
	Phone     string       `json:"phone"`
	BlockType SmsBlockType `json:"block_type"`
	Reason    string       `json:"reason"`
	ErrorCode string       `json:"error_code"`
	BlockedAt time.Time    `json:"blocked_at"`
	JobID     string       `json:"job_id"`
	MessageID string       `json:"message_id"`
}

// RecordBlacklistEvent 记录黑名单事件
//
// 触发场景：
//   - 运营商回执 ERR_4002 → carrier 级黑名单
//   - 用户回复 TD 退订   → business 级黑名单
//   - 内容审核违规       → content / regulatory 级
func (s *SmsDeliveryTrackerService) RecordBlacklistEvent(ctx context.Context, phone, errorCode, errorMsg, jobID, messageID string) {
	phone = identity.NormalizePhone(phone)
	if phone == "" {
		return
	}
	rec := SmsBlacklistRecord{
		Phone:     phone,
		ErrorCode: errorCode,
		Reason:    errorMsg,
		BlockedAt: time.Now(),
		JobID:     jobID,
		MessageID: messageID,
	}
	switch {
	case strings.HasPrefix(errorCode, "ERR_4002"):
		rec.BlockType = SmsBlockCarrier
	case strings.HasPrefix(errorCode, "ERR_4003"):
		rec.BlockType = SmsBlockContent
	case strings.HasPrefix(errorCode, "ERR_4004"):
		rec.BlockType = SmsBlockRegulatory
	default:
		rec.BlockType = SmsBlockBusiness
	}
	logger.Infof("[SmsDeliveryTracker] blacklist: %+v", rec)
}

// DeliveryRateMetrics 到达率指标
type DeliveryRateMetrics struct {
	WindowStart  time.Time              `json:"window_start"`
	WindowEnd    time.Time              `json:"window_end"`
	TotalSent    int64                  `json:"total_sent"`
	Delivered    int64                  `json:"delivered"`
	Failed       int64                  `json:"failed"`
	Retryable    int64                  `json:"retryable"`
	Blacklisted  int64                  `json:"blacklisted"`
	Portability  int64                  `json:"portability"`
	DeliveryRate float64                `json:"delivery_rate"`
	FailureRate  float64                `json:"failure_rate"`
	ByCarrier    map[string]CarrierStat `json:"by_carrier"`
}

// CarrierStat 单运营商统计
type CarrierStat struct {
	Total        int64   `json:"total"`
	Delivered    int64   `json:"delivered"`
	Failed       int64   `json:"failed"`
	DeliveryRate float64 `json:"delivery_rate"`
}

// GetDeliveryRateMetrics 聚合时间窗口内的到达率
func (s *SmsDeliveryTrackerService) GetDeliveryRateMetrics(ctx context.Context, start, end time.Time) (*DeliveryRateMetrics, error) {
	if start.IsZero() || end.IsZero() {
		return nil, errors.New("start / end 不能为空")
	}
	if end.Before(start) {
		return nil, errors.New("end 必须大于 start")
	}
	if s.deliveryRepo == nil {
		return nil, errors.New("delivery repo is nil")
	}

	m := &DeliveryRateMetrics{
		WindowStart: start,
		WindowEnd:   end,
		ByCarrier:   make(map[string]CarrierStat),
	}

	row, err := s.deliveryRepo.GetDeliveryAggregate(ctx, start, end)
	if err != nil {
		return nil, err
	}
	m.TotalSent = row.Total
	m.Delivered = row.Delivered
	m.Failed = row.Failed
	m.Retryable = row.Retryable
	if m.TotalSent > 0 {
		m.DeliveryRate = utils.Round2(float64(m.Delivered) / float64(m.TotalSent) * 100)
		m.FailureRate = utils.Round2(float64(m.Failed+m.Retryable) / float64(m.TotalSent) * 100)
	}

	blacklisted, _ := s.deliveryRepo.CountBlacklisted(ctx, start, end)
	m.Blacklisted = blacklisted

	port, _ := s.deliveryRepo.CountPortabilityFailure(ctx, start, end)
	m.Portability = port

	crows, err := s.deliveryRepo.GetCarrierStats(ctx, start, end)
	if err == nil {
		for _, cr := range crows {
			cs := CarrierStat{
				Total:     cr.Total,
				Delivered: cr.Delivered,
				Failed:    cr.Failed,
			}
			if cr.Total > 0 {
				cs.DeliveryRate = utils.Round2(float64(cr.Delivered) / float64(cr.Total) * 100)
			}
			m.ByCarrier[cr.Provider] = cs
		}
	}

	return m, nil
}

// ProviderDeliveryReport 兼容的运营商回执统一格式
type ProviderDeliveryReport struct {
	MessageID   string `json:"messageId"`
	Phone       string `json:"phone"`
	JobID       string `json:"jobId"`
	Provider    string `json:"provider"`
	Carrier     string `json:"carrier"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode"`
	ErrorMsg    string `json:"errorMsg"`
	SentAt      string `json:"sentAt"`
	DeliveredAt string `json:"deliveredAt"`
	RawPayload  string `json:"rawPayload"`
}

// RecordFromProvider 统一的运营商回执接收入口
//
// 自动：
//  1. 转换为内部 DeliveryReportRequest 走原 tracking 路径
//  2. 检测并记录携号转网
//  3. 记录黑名单事件
func (s *SmsDeliveryTrackerService) RecordFromProvider(ctx context.Context, r *ProviderDeliveryReport) error {
	if r == nil {
		return errors.New("report is nil")
	}
	if r.MessageID == "" {
		return errors.New("messageId 不能为空")
	}
	ctx, err := s.bindReportTenant(ctx, r.MessageID)
	if err != nil {
		return err
	}

	req := &DeliveryReportRequest{
		MessageID:   r.MessageID,
		Phone:       r.Phone,
		JobID:       r.JobID,
		Provider:    r.Provider,
		Status:      r.Status,
		ErrorCode:   r.ErrorCode,
		ErrorMsg:    r.ErrorMsg,
		SentAt:      r.SentAt,
		DeliveredAt: r.DeliveredAt,
	}
	if err := s.tracking.RecordDeliveryReport(ctx, req); err != nil {
		logger.Errorf("[SmsDeliveryTracker] record delivery report: %v", err)
		// 回执落库失败必须让运营商看到失败（否则状态报告永久丢失，
		// on 模式下租户未解析时更是静默 200 + 零写入）
		return err
	}

	s.markLedgerByMsgID(ctx, r)

	go func(phone, carrier string) {
		bgCtx, cancel := context.WithTimeout(tenant.BackgroundWithTenant(ctx), utils.ShortTimeout)
		defer cancel()
		_ = s.DetectAndRecordPortability(bgCtx, phone, carrier)
	}(r.Phone, r.Carrier)

	if r.ErrorCode != "" {
		s.RecordBlacklistEvent(ctx, r.Phone, r.ErrorCode, r.ErrorMsg, r.JobID, r.MessageID)
	}

	return nil
}

// markLedgerByMsgID 回执终态按单号回写外发台账（best-effort：失败只记日志，不拦回执 200）。
//
// 送达表只认 message_id，而台账此前没有这个键——回执到了也对不上外发行，这是
// 送达率链的最后一公里。现在 SendSms/ResendSms 落 MsgID，这里闭回另一半环。
// 只有 delivered/failed 两种终态动台账：retryable 是送达表侧的语义（可重试），
// 台账侧与 failed 同落 failed 并保留渠道错误码；pending/sent 中间态没有要推进的东西。
func (s *SmsDeliveryTrackerService) markLedgerByMsgID(ctx context.Context, r *ProviderDeliveryReport) {
	if s.smsRepo == nil || r.MessageID == "" {
		return
	}
	ledgerStatus := ""
	switch normalizeSmsStatus(r.Status) {
	case model.SmsStatusDelivered:
		ledgerStatus = model.SmsStatusDelivered
	case model.SmsStatusFailed:
		ledgerStatus = model.SmsStatusFailed
	default:
		return
	}
	n, err := s.smsRepo.MarkDeliveryByMsgID(ctx, r.MessageID, ledgerStatus, r.ErrorCode, r.ErrorMsg)
	if err != nil {
		logger.Errorf("[SmsDeliveryTracker] 回执回写台账失败 message_id=%s: %v", r.MessageID, err)
		return
	}
	if n > 0 {
		logger.Infof("[SmsDeliveryTracker] 回执已回写台账 message_id=%s status=%s rows=%d", r.MessageID, ledgerStatus, n)
	}
}

// bindReportTenant 运营商回执没有登录态，租户线索只有两处：
// 调用方显式绑定（webhook 上的 tenant_code），或发送侧预落的 message_id 行自证。
// 都拿不到就保持无绑定——on 模式下由隔离 callback fail-closed，
// 绝不回落到默认租户（那会把回执写进别的租户）。
func (s *SmsDeliveryTrackerService) bindReportTenant(ctx context.Context, messageID string) (context.Context, error) {
	if !tenant.IsMultiTenant() {
		return ctx, nil
	}
	if _, bound := tenant.FromContext(ctx); bound {
		return ctx, nil
	}
	id, ok, err := s.tracking.TenantIDByMessageID(ctx, messageID)
	if err != nil {
		return ctx, fmt.Errorf("解析短信回执租户失败: %w", err)
	}
	if !ok {
		return ctx, nil
	}
	return tenant.OnlyTenant(ctx, id), nil
}

// MarshalReport 序列化原始回执（用于持久化到 raw_payload 字段）
func MarshalReport(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
