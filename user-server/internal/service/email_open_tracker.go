package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

var EmailOpenPixel = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

// EmailOpenPixelContentType 像素 HTTP Content-Type
const EmailOpenPixelContentType = "image/png"

// EmailOpenPixelMaxAge 像素缓存时间（避免邮件客户端反复 GET 触发重复计数）
//
// 长缓存：30 天。30 天后追踪 token 早已过期（默认 90 天），不会与业务冲突。
// 短缓存：邮件客户端对同一 URL 1 天内可能拉多次。
const EmailOpenPixelMaxAge = 30 * 24 * 3600

// EmailOpenTrackerService 邮件打开追踪服务
type EmailOpenTrackerService struct {
	tracking *EmailTrackingService
	repo     repository.EmailTrackingRepository
}

// NewEmailOpenTrackerService 创建邮件打开追踪服务
func NewEmailOpenTrackerService(tracking *EmailTrackingService, repo repository.EmailTrackingRepository) *EmailOpenTrackerService {
	if tracking == nil {
		tracking = NewEmailTrackingService(repo)
	}
	if repo == nil {
		repo = repository.NewEmailTrackingRepository(nil)
	}
	return &EmailOpenTrackerService{tracking: tracking, repo: repo}
}

// GenerateOpenPixelURL 生成邮件打开追踪的 1×1 像素 URL
//
// 链接格式：{baseURL}/api/email/track/open/{token}.png
// 选 .png 后缀是 Postmark / Mailchimp 等业内标准做法，方便部分邮件
// 客户端按 Content-Type 缓存或屏蔽。
func (s *EmailOpenTrackerService) GenerateOpenPixelURL(ctx context.Context, email, jobID string) (string, error) {
	token, err := s.tracking.GenerateTrackingPixelToken(ctx, email, jobID)
	if err != nil {
		return "", err
	}
	base := s.tracking.baseURL(ctx)
	return fmt.Sprintf("%s/api/email/track/open/%s.png", strings.TrimRight(base, "/"), token), nil
}

// RenderPixel 处理打开追踪 GET 请求
//
// 行为：
//  1. 校验 token 签名与有效期
//  2. 异步记录打开事件（不影响像素返回速度）
//  3. 立即返回 1×1 透明 PNG + Cache-Control: max-age=2592000, immutable
func (s *EmailOpenTrackerService) RenderPixel(ctx context.Context, token, ip, ua string) ([]byte, string, int, error) {
	if token == "" {
		return nil, "", 0, errors.New("token 不能为空")
	}
	if !MarkOpenSeen(token) {
		return EmailOpenPixel, EmailOpenPixelContentType, EmailOpenPixelMaxAge, nil
	}
	go func(t, ipAddr, userAgent string) {
		bgCtx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
		defer cancel()
		if err := s.tracking.RecordOpenEvent(bgCtx, t, ipAddr, userAgent); err != nil {
			logger.Warnf("[email_open] 打开事件记录失败 token=%s: %v", t, err)
		}
	}(token, ip, ua)

	return EmailOpenPixel, EmailOpenPixelContentType, EmailOpenPixelMaxAge, nil
}

// PostmarkOpenEvent Postmark 邮件打开 webhook payload
//
// 兼容 Postmark MessageEvent API：https://postmarkapp.com/developer/webhooks/message-event-webhook
// 我们只关心 Open / Click / Bounce / SpamComplaint 4 类事件。
type PostmarkOpenEvent struct {
	RecordType    string `json:"RecordType"`
	MessageID     string `json:"MessageID"`
	Recipient     string `json:"Recipient"`
	ReceivedAt    string `json:"DeliveredAt"`
	UserAgent     string `json:"UserAgent"`
	IP            string `json:"IP"`
	ClickLocation string `json:"ClickLocation,omitempty"`
	OriginalLink  string `json:"OriginalLink,omitempty"`
}

// RecordPostmarkEvent 记录 Postmark 风格事件
//
// 自动映射 RecordType → 内部事件类型（open/click/bounce/unsubscribe）
func (s *EmailOpenTrackerService) RecordPostmarkEvent(ctx context.Context, evt *PostmarkOpenEvent) error {
	if evt == nil {
		return errors.New("event is nil")
	}
	switch strings.ToLower(evt.RecordType) {
	case "open":
		return s.recordOpenByEmail(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent)
	case "click":
		return s.recordClickByEmail(ctx, evt.Recipient, evt.MessageID, evt.OriginalLink, evt.IP, evt.UserAgent)
	case "bounce", "hardbounce", "softbounce":
		if err := s.tracking.RecordBounceEvent(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent); err != nil {
			return err
		}

		rt := strings.ToLower(evt.RecordType)
		if rt == "bounce" || rt == "hardbounce" {
			s.autoBlockOnBounce(ctx, evt.Recipient)
		}
		return nil
	case "spamcomplaint":
		if err := s.tracking.RecordUnsubscribeEvent(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent); err != nil {
			return err
		}

		s.autoBlockOnBounce(ctx, evt.Recipient)
		return nil
	default:
		return fmt.Errorf("%w: unsupported RecordType: %s", utils.ErrInvalidInput, evt.RecordType)
	}
}

func (s *EmailOpenTrackerService) recordOpenByEmail(ctx context.Context, email, messageID, ip, ua string) error {
	email = normalizeEmail(email)
	if email == "" {
		return errors.New("email 不能为空")
	}
	eventID := "pm-open-" + fmt.Sprintf("%x", sha256.Sum256([]byte(email+"|"+messageID)))[:24]
	if s.repo != nil {
		if exists, err := s.repo.EventExists(ctx, eventID); err == nil && exists {
			return nil
		}
	}
	evt := &model.EmailTrackingEvent{
		EventID:   eventID,
		Email:     email,
		JobID:     messageID,
		EventType: model.EmailEventTypeOpen,
		UserAgent: ua,
		IP:        ip,
		Timestamp: time.Now(),
	}
	if s.repo == nil {
		return errors.New("email tracking repository 未初始化")
	}
	return s.repo.CreateEvent(ctx, evt)
}

// SendCloudOpenEvent SendCloud webhook payload
//
// 文档：https://www.sendcloud.net/doc/email_v2/webhook/
// 关键字段：event（delivered / open / click / bounce / spam_report / unsubscribe）
type SendCloudOpenEvent struct {
	Event     string `json:"event"`
	Recipient string `json:"recipient"`
	MessageID string `json:"message_id"`
	IP        string `json:"ip"`
	UserAgent string `json:"useragent"`
	URL       string `json:"url,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Timestamp string `json:"timestamp"`
}

// RecordSendCloudEvent 记录 SendCloud 风格事件
func (s *EmailOpenTrackerService) RecordSendCloudEvent(ctx context.Context, evt *SendCloudOpenEvent) error {
	if evt == nil {
		return errors.New("event is nil")
	}
	switch strings.ToLower(evt.Event) {
	case "delivered", "open":
		return s.recordOpenByEmail(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent)
	case "click":
		return s.recordClickByEmail(ctx, evt.Recipient, evt.MessageID, evt.URL, evt.IP, evt.UserAgent)
	case "bounce", "spam_report":
		return s.tracking.RecordBounceEvent(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent)
	case "unsubscribe":
		return s.tracking.RecordUnsubscribeEvent(ctx, evt.Recipient, evt.MessageID, evt.IP, evt.UserAgent)
	default:
		return fmt.Errorf("%w: unsupported event: %s", utils.ErrInvalidInput, evt.Event)
	}
}

func (s *EmailOpenTrackerService) recordClickByEmail(ctx context.Context, email, messageID, targetURL, ip, ua string) error {
	email = normalizeEmail(email)
	if email == "" {
		return errors.New("email 不能为空")
	}
	eventID := "sc-click-" + fmt.Sprintf("%x", sha256.Sum256([]byte(email+"|"+messageID+"|"+targetURL)))[:24]
	if s.repo != nil {
		if exists, _ := s.repo.EventExists(ctx, eventID); exists {
			return nil
		}
	}
	evt := &model.EmailTrackingEvent{
		EventID:   eventID,
		Email:     email,
		JobID:     messageID,
		EventType: model.EmailEventTypeClick,
		UserAgent: ua,
		IP:        ip,
		Timestamp: time.Now(),
	}
	if targetURL != "" {
		evt.UserAgent = ua + " | target=" + targetURL
	}
	if s.repo == nil {
		return errors.New("email tracking repository 未初始化")
	}
	return s.repo.CreateEvent(ctx, evt)
}

// OpenRateMetrics 打开率指标
type OpenRateMetrics struct {
	JobID           string  `json:"job_id"`
	TotalSent       int64   `json:"total_sent"`
	UniqueOpened    int64   `json:"unique_opened"`
	TotalOpens      int64   `json:"total_opens"`
	OpenRate        float64 `json:"open_rate"`
	AvgOpensPerUser float64 `json:"avg_opens_per_user"`
}

// GetOpenRateMetrics 获取指定任务的打开率指标
func (s *EmailOpenTrackerService) GetOpenRateMetrics(ctx context.Context, jobID string, totalSent int64) (*OpenRateMetrics, error) {
	if jobID == "" {
		return nil, errors.New("job_id 不能为空")
	}
	uniqueOpened, err := s.repo.CountUniqueEmailsByJob(ctx, jobID, model.EmailEventTypeOpen)
	if err != nil {
		return nil, err
	}
	totalOpens, err := s.repo.CountEventsByJob(ctx, jobID, model.EmailEventTypeOpen)
	if err != nil {
		return nil, err
	}
	m := &OpenRateMetrics{
		JobID:        jobID,
		TotalSent:    totalSent,
		UniqueOpened: uniqueOpened,
		TotalOpens:   totalOpens,
	}
	if totalSent > 0 {
		m.OpenRate = round2(float64(uniqueOpened) / float64(totalSent) * 100)
	}
	if uniqueOpened > 0 {
		m.AvgOpensPerUser = round2(float64(totalOpens) / float64(uniqueOpened))
	}
	return m, nil
}

var (
	pixelCacheMu sync.Mutex
	pixelCache   = make(map[string]time.Time)
)

// MarkOpenSeen 标记一次打开事件已记录（防重）
//
// 同一 token 30 秒内只记一次，避免 Gmail / Outlook 预览面板
// 自动 GET 像素 URL 导致的重复计数。
func MarkOpenSeen(token string) bool {
	if token == "" {
		return false
	}
	pixelCacheMu.Lock()
	defer pixelCacheMu.Unlock()
	if last, ok := pixelCache[token]; ok && time.Since(last) < utils.DefaultHTTPTimeout {
		return false
	}
	pixelCache[token] = time.Now()
	if len(pixelCache) > 4096 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, t := range pixelCache {
			if t.Before(cutoff) {
				delete(pixelCache, k)
			}
		}
	}
	return true
}

// EmailEventSummary 邮件事件摘要（用于日志）
func EmailEventSummary(evtType, email string) string {
	return fmt.Sprintf("[%s] %s", evtType, email)
}

// PrettyPrintJSON 调试用：序列化 webhook payload
func PrettyPrintJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *EmailOpenTrackerService) autoBlockOnBounce(ctx context.Context, email string) {
	email = normalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return
	}
	phone := strings.Split(email, "@")[0]
	dnc := NewDoNotContactService(nil)
	_ = dnc.BlockFromPhone(ctx, phone, "email_hard_bounce")
}
