package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"github.com/google/uuid"
)

const emailTrackingSecretEnv = "EMAIL_TRACKING_SECRET"

// EmailTrackingClaim 追踪 token 携带的声明
//
// 每个收件人 + 每个 job 独立 token，避免泄露后影响其他收件人
type EmailTrackingClaim struct {
	Email  string `json:"email"`
	JobID  string `json:"job_id"`
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
	Expire int64  `json:"expire"`
	Nonce  string `json:"nonce"`
}

// EmailTrackingService 邮件追踪服务
type EmailTrackingService struct {
	repo repository.EmailTrackingRepository
}

// NewEmailTrackingService 创建邮件追踪服务
func NewEmailTrackingService(repo repository.EmailTrackingRepository) *EmailTrackingService {
	if repo == nil {
		repo = repository.NewEmailTrackingRepository(nil)
	}
	return &EmailTrackingService{repo: repo}
}

// GenerateTrackingPixelToken 生成打开追踪 token（每个收件人 + 每个 job 独立）
func (s *EmailTrackingService) GenerateTrackingPixelToken(ctx context.Context, email, jobID string) (string, error) {
	return s.generateToken(ctx, email, jobID, model.EmailEventTypeOpen, "")
}

// GenerateClickTrackingLink 生成点击追踪链接
//
// 链接格式：{baseURL}/api/email/track/click/{token}?url={原始 URL}
// 重定向时读取 token 内 target（如缺失则取 query url 参数）
func (s *EmailTrackingService) GenerateClickTrackingLink(ctx context.Context, email, jobID, targetURL string) (string, error) {
	token, err := s.generateToken(ctx, email, jobID, model.EmailEventTypeClick, targetURL)
	if err != nil {
		return "", err
	}
	base := s.baseURL(ctx)
	return fmt.Sprintf("%s/api/email/track/click/%s?url=%s",
		strings.TrimRight(base, "/"), token, url.QueryEscape(targetURL)), nil
}

func (s *EmailTrackingService) generateToken(ctx context.Context, email, jobID, tokenType, target string) (string, error) {
	email = normalizeEmail(email)
	if email == "" {
		return "", errors.New("email 不能为空")
	}
	claim := EmailTrackingClaim{
		Email:  email,
		JobID:  jobID,
		Type:   tokenType,
		Target: target,
		Expire: time.Now().Add(90 * 24 * time.Hour).Unix(),
		Nonce:  fmt.Sprintf("track-%s", uuid.NewString()),
	}
	payload, err := json.Marshal(claim)
	if err != nil {
		return "", fmt.Errorf("marshal claim failed: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	sig := s.sign(ctx, []byte(payloadB64))
	return payloadB64 + "." + sig, nil
}

// VerifyTrackingToken 校验追踪 token
func (s *EmailTrackingService) VerifyTrackingToken(ctx context.Context, token string) (*EmailTrackingClaim, error) {
	if token == "" {
		return nil, errors.New("token 不能为空")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("token 格式错误")
	}
	payloadB64, sig := parts[0], parts[1]
	expectedSig := s.sign(ctx, []byte(payloadB64))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, errors.New("token 签名校验失败")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("token payload 解码失败: %w", err)
	}
	var claim EmailTrackingClaim
	if err := json.Unmarshal(payload, &claim); err != nil {
		return nil, fmt.Errorf("token payload 反序列化失败: %w", err)
	}
	if time.Now().Unix() > claim.Expire {
		return nil, errors.New("token 已过期")
	}
	return &claim, nil
}

// RecordOpenEvent 记录打开事件（追踪像素触发）
// eventID 幂等：相同 eventID 不重复记录
func (s *EmailTrackingService) RecordOpenEvent(ctx context.Context, token, ip, ua string) error {
	claim, err := s.VerifyTrackingToken(ctx, token)
	if err != nil {
		return err
	}
	return s.recordEvent(ctx, claim.Email, claim.JobID, model.EmailEventTypeOpen, "", ip, ua)
}

// RecordClickEvent 记录点击事件（链接重定向触发）
// 返回目标 URL 供控制器 302 跳转
func (s *EmailTrackingService) RecordClickEvent(ctx context.Context, token, ip, ua string) (string, error) {
	claim, err := s.VerifyTrackingToken(ctx, token)
	if err != nil {
		return "", err
	}
	if err := s.recordEvent(ctx, claim.Email, claim.JobID, model.EmailEventTypeClick, claim.Target, ip, ua); err != nil {
		return "", err
	}
	if claim.Target != "" {
		return claim.Target, nil
	}
	return "", nil
}

// RecordBounceEvent 记录退信事件（SMTP/邮件网关回调）
func (s *EmailTrackingService) RecordBounceEvent(ctx context.Context, email, jobID, ip, ua string) error {
	return s.recordEvent(ctx, email, jobID, model.EmailEventTypeBounce, "", ip, ua)
}

// RecordUnsubscribeEvent 记录退订事件（与邮件退订服务联动）
func (s *EmailTrackingService) RecordUnsubscribeEvent(ctx context.Context, email, jobID, ip, ua string) error {
	return s.recordEvent(ctx, email, jobID, model.EmailEventTypeUnsubscribe, "", ip, ua)
}

func (s *EmailTrackingService) recordEvent(ctx context.Context, email, jobID, eventType, target, ip, ua string) error {
	email = normalizeEmail(email)
	if email == "" {
		return errors.New("email 不能为空")
	}
	if eventType == "" {
		return errors.New("event_type 不能为空")
	}

	eventID := deriveTrackingEventID(email, jobID, eventType, target)
	exists, err := s.repo.EventExists(ctx, eventID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	event := &model.EmailTrackingEvent{
		EventID:   eventID,
		Email:     email,
		JobID:     jobID,
		EventType: eventType,
		UserAgent: ua,
		IP:        ip,
		Timestamp: time.Now(),
	}
	if err := s.repo.CreateEvent(ctx, event); err != nil {
		if exists2, e2 := s.repo.EventExists(ctx, eventID); e2 == nil && exists2 {
			return nil
		}
		logger.Errorf("记录邮件追踪事件失败 email=%s type=%s: %v", email, eventType, err)
		return err
	}
	return nil
}

func deriveTrackingEventID(email, jobID, eventType, target string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s", email, jobID, eventType, target)))
	return hex.EncodeToString(sum[:])[:32]
}

// GetJobMetrics 返回该批次邮件的完整指标（实时聚合，不依赖定时任务）
func (s *EmailTrackingService) GetJobMetrics(ctx context.Context, jobID string) (*model.EmailJobMetric, error) {
	if jobID == "" {
		return nil, errors.New("job_id 不能为空")
	}

	metric, err := s.repo.GetJobMetric(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if metric == nil {
		metric = &model.EmailJobMetric{JobID: jobID}
	}

	opened, err := s.repo.CountUniqueEmailsByJob(ctx, jobID, model.EmailEventTypeOpen)
	if err != nil {
		return nil, err
	}
	clicked, err := s.repo.CountUniqueEmailsByJob(ctx, jobID, model.EmailEventTypeClick)
	if err != nil {
		return nil, err
	}
	bounced, err := s.repo.CountEventsByJob(ctx, jobID, model.EmailEventTypeBounce)
	if err != nil {
		return nil, err
	}
	unsub, err := s.repo.CountEventsByJob(ctx, jobID, model.EmailEventTypeUnsubscribe)
	if err != nil {
		return nil, err
	}

	metric.TotalOpened = opened
	metric.TotalClicked = clicked
	metric.TotalBounced = bounced
	metric.TotalUnsubscribed = unsub

	if metric.TotalSent > 0 {
		metric.OpenRate = round2(float64(opened) / float64(metric.TotalSent) * 100)
		metric.ClickRate = round2(float64(clicked) / float64(metric.TotalSent) * 100)
	} else {
		metric.OpenRate = 0
		metric.ClickRate = 0
	}

	return metric, nil
}

// GetEmailMetrics 聚合时间区间内的邮件指标
func (s *EmailTrackingService) GetEmailMetrics(ctx context.Context, start, end time.Time) (*model.EmailJobMetric, error) {
	if start.IsZero() || end.IsZero() {
		return nil, errors.New("start / end 不能为空")
	}
	if end.Before(start) {
		return nil, errors.New("end 必须大于 start")
	}

	opened, err := s.repo.CountEventsByRange(ctx, start, end, model.EmailEventTypeOpen)
	if err != nil {
		return nil, err
	}
	clicked, err := s.repo.CountEventsByRange(ctx, start, end, model.EmailEventTypeClick)
	if err != nil {
		return nil, err
	}
	bounced, err := s.repo.CountEventsByRange(ctx, start, end, model.EmailEventTypeBounce)
	if err != nil {
		return nil, err
	}
	unsub, err := s.repo.CountEventsByRange(ctx, start, end, model.EmailEventTypeUnsubscribe)
	if err != nil {
		return nil, err
	}
	sent, err := s.repo.CountEventsByRange(ctx, start, end, "")
	if err != nil {
		return nil, err
	}

	metric := &model.EmailJobMetric{
		JobID:             "range",
		TotalSent:         sent,
		TotalOpened:       opened,
		TotalClicked:      clicked,
		TotalBounced:      bounced,
		TotalUnsubscribed: unsub,
	}
	if sent > 0 {
		metric.OpenRate = round2(float64(opened) / float64(sent) * 100)
		metric.ClickRate = round2(float64(clicked) / float64(sent) * 100)
	}
	return metric, nil
}

// RefreshJobMetrics 刷新任务指标（定时任务调用，每 10 分钟一次）
func (s *EmailTrackingService) RefreshJobMetrics(ctx context.Context, jobID string, totalSent int64) error {
	metric, err := s.GetJobMetrics(ctx, jobID)
	if err != nil {
		return err
	}
	if totalSent > 0 {
		metric.TotalSent = totalSent
		if metric.TotalOpened > 0 {
			metric.OpenRate = round2(float64(metric.TotalOpened) / float64(totalSent) * 100)
		}
		if metric.TotalClicked > 0 {
			metric.ClickRate = round2(float64(metric.TotalClicked) / float64(totalSent) * 100)
		}
	}
	return s.repo.UpsertJobMetric(ctx, metric)
}

// ListJobEvents 分页查询任务的追踪事件
func (s *EmailTrackingService) ListJobEvents(ctx context.Context, jobID string, page, limit int) ([]*model.EmailTrackingEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 500 {
		limit = 20
	}
	return s.repo.ListEventsByJob(ctx, jobID, page, limit)
}

func (s *EmailTrackingService) sign(ctx context.Context, data []byte) string {
	mac := hmac.New(sha256.New, []byte(s.secret(ctx)))
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *EmailTrackingService) secret(ctx context.Context) string {
	return os.Getenv(emailTrackingSecretEnv)
}

func (s *EmailTrackingService) baseURL(ctx context.Context) string {
	return config.GetServerBaseURL()
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
