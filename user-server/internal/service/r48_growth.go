package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// WebhookSubscription 出站 Webhook 订阅（别名，模型已收敛到 model 层）
type WebhookSubscription = model.WebhookSubscription

// Webhook 事件常量
const (
	WebhookEventMessageCreated = "message.created"
	WebhookEventSessionCreated = "session.created"
	WebhookEventSessionClosed  = "session.closed"
)

var webhookOutClient = &http.Client{Timeout: 5 * time.Second}

// PublishWebhookEvent 事件发布（fire-and-forget，失败仅日志，绝不阻塞主链路）
// 便捷入口：使用全局单例 WebhookSubService
func PublishWebhookEvent(ctx context.Context, event string, payload map[string]any) {
	NewWebhookSubServiceFromGlobal().PublishEvent(ctx, event, payload)
}

// WebhookSubService CRUD 服务
type WebhookSubService struct {
	repo *repository.WebhookSubscriptionRepository
}

// NewWebhookSubService 构造
func NewWebhookSubService(gdb *gorm.DB) *WebhookSubService {
	return &WebhookSubService{repo: repository.NewWebhookSubscriptionRepository(gdb)}
}

// NewWebhookSubServiceFromGlobal 便捷构造
func NewWebhookSubServiceFromGlobal() *WebhookSubService { return NewWebhookSubService(db.GetDB()) }

// Create 创建订阅（URL 必须 http(s)，secret 自动生成）
func (s *WebhookSubService) Create(ctx context.Context, url, events string) (*model.WebhookSubscription, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("url 必须以 http(s):// 开头")
	}
	if strings.TrimSpace(events) == "" {
		events = "all"
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("failed to generate webhook secret: %w", err)
	}
	secret := "whsec_" + base64.RawURLEncoding.EncodeToString(b)
	sub := &model.WebhookSubscription{URL: url, Events: events, Secret: secret, Enabled: true}
	if err := s.repo.Create(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// List 订阅列表
func (s *WebhookSubService) List(ctx context.Context) ([]*model.WebhookSubscription, error) {
	return s.repo.List(ctx)
}

// Delete 删除
func (s *WebhookSubService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

// PublishEvent 事件发布（fire-and-forget，失败仅日志，绝不阻塞主链路）
func (s *WebhookSubService) PublishEvent(ctx context.Context, event string, payload map[string]any) {
	subs, err := s.repo.ListEnabledByEvent(ctx, event)
	if err != nil || len(subs) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"timestamp": time.Now().Format(time.RFC3339),
		"data":      payload,
	})
	for _, sub := range subs {
		go func(sub model.WebhookSubscription, body []byte) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[panic-recover] %T: %v\n%s", r, r, string(debug.Stack()))
				}
			}()
			reqCtx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, sub.URL, strings.NewReader(string(body)))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			mac := hmac.New(sha256.New, []byte(sub.Secret))
			mac.Write(body)
			req.Header.Set("X-Hivemtk-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			resp, err := webhookOutClient.Do(req)
			if err != nil {
				slog.Warn("[WebhookOut] 投递失败", "url", sub.URL, "event", event, "err", err)
				return
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			if resp.StatusCode >= 400 {
				slog.Warn("[WebhookOut] 远端非 2xx", "url", sub.URL, "status", resp.StatusCode)
			}
		}(sub, body)
	}
}

// SetCustomAttributes 更新客户自定义属性（JSONB merge）
func (s *CustomerServicePlusService) SetCustomAttributes(ctx context.Context, customerID string, attrs map[string]any) (map[string]any, error) {
	curStr, err := s.opsRepo.GetCustomerCustomAttributes(ctx, customerID)
	if err != nil {
		return nil, err
	}
	merged := map[string]any{}
	_ = json.Unmarshal([]byte(curStr), &merged)
	for k, v := range attrs {
		merged[k] = v
	}
	raw, _ := json.Marshal(merged)
	ok, err := s.opsRepo.UpdateCustomerCustomAttributes(ctx, customerID, string(raw))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return merged, nil
}

// SavedView 保存视图（别名，模型已收敛到 model 层）
type SavedView = model.SavedView

// CreateSavedView 创建视图（同名覆盖）
func (s *CustomerServicePlusService) CreateSavedView(ctx context.Context, userID uint, name, route, filter string) (*model.SavedView, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(route) == "" {
		return nil, fmt.Errorf("name/route 必填")
	}
	s.savedViewRepo.DeleteByName(ctx, userID, name, route)
	v := &model.SavedView{UserID: userID, Name: name, Route: route, Filter: filter}
	if err := s.savedViewRepo.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// ListSavedViews 视图列表（按用户+路由）
func (s *CustomerServicePlusService) ListSavedViews(ctx context.Context, userID uint, route string) ([]*model.SavedView, error) {
	return s.savedViewRepo.ListByUserAndRoute(ctx, userID, route)
}

// DeleteSavedView 删除视图
func (s *CustomerServicePlusService) DeleteSavedView(ctx context.Context, id, userID uint) error {
	ok, err := s.savedViewRepo.DeleteOwned(ctx, id, userID)
	if err != nil {
		return err
	}
	if !ok {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ReportSubscription 报表订阅（别名，模型已收敛到 model 层）
type ReportSubscription = model.ReportSubscription

// CreateReportSubscription 订阅报表
func (s *CustomerServicePlusService) CreateReportSubscription(ctx context.Context, email, schedule string) (*model.ReportSubscription, error) {
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("邮箱格式无效")
	}
	if schedule != "daily" && schedule != "weekly" {
		schedule = "daily"
	}

	s.reportSubRepo.DeleteByEmail(ctx, email)
	sub := &model.ReportSubscription{Email: email, Schedule: schedule, Enabled: true}
	if err := s.reportSubRepo.Create(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// ListReportSubscriptions 订阅列表
func (s *CustomerServicePlusService) ListReportSubscriptions(ctx context.Context) ([]*model.ReportSubscription, error) {
	return s.reportSubRepo.List(ctx)
}

// DeleteReportSubscription 退订
func (s *CustomerServicePlusService) DeleteReportSubscription(ctx context.Context, id uint) error {
	return s.reportSubRepo.Delete(ctx, id)
}

// SendScheduledReports cron 入口：给全部启用订阅发送昨日汇总（汇总 CSV 内存生成→SMTP）
func (s *CustomerServicePlusService) SendScheduledReports(ctx context.Context) (int, error) {
	subs, err := s.reportSubRepo.ListEnabled(ctx)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rows, err := s.reportSubRepo.DailyReportSummary(ctx, yesterday)
	if err != nil {
		return 0, err
	}
	var csv strings.Builder
	csv.WriteString("指标,数量\n")
	for _, r := range rows {
		csv.WriteString(fmt.Sprintf("%s,%d\n", r.Metric, r.Value))
	}

	emailSvc := s.emailSvc
	sent := 0
	for _, sub := range subs {
		subject := fmt.Sprintf("每日数据报表 %s", yesterday)
		html := "<pre style='font-family:monospace'>" + csv.String() + "</pre>"
		if _, err := emailSvc.Send(ctx, 0, sub.Email, subject, html, nil); err != nil {
			slog.Warn("[ReportCron] 报表发送失败", "email", sub.Email, "err", err)
			continue
		}
		s.reportSubRepo.MarkSent(ctx, sub.ID, time.Now())
		sent++
	}
	return sent, nil
}

// SessionTranscript 导出转录（csv=true 时返回 CSV 两列）
func (s *CustomerServicePlusService) SessionTranscript(ctx context.Context, sessionID string, csv bool) (string, string, error) {
	msgs, err := s.reportSubRepo.ListSessionTranscriptMessages(ctx, sessionID)
	if err != nil {
		return "", "", err
	}
	if len(msgs) == 0 {
		return "", "", gorm.ErrRecordNotFound
	}
	if csv {
		var b strings.Builder
		b.WriteString("时间,发送方,内容\n")
		for _, m := range msgs {
			line := strings.ReplaceAll(m.Content, "\"", "\"\"")
			line = strings.ReplaceAll(line, "\n", " ")
			fmt.Fprintf(&b, "%s,%s,\"%s\"\n", m.CreatedAt.Format("2006-01-02 15:04:05"), whoOf2(m), line)
		}
		return "text/csv", b.String(), nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("会话转录 %s\n导出时间 %s\n====================\n\n",
		sessionID, time.Now().Format("2006-01-02 15:04")))
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s] %s:\n%s\n\n", m.CreatedAt.Format("15:04:05"), whoOf2(m), m.Content)
	}
	return "text/plain", b.String(), nil
}

func whoOf2(m repository.TranscriptMessageRow) string {
	if m.SenderName != "" {
		return m.SenderName
	}
	switch m.SenderType {
	case "customer", "user":
		return "客户"
	case "staff", "agent":
		return "坐席"
	case "ai", "bot":
		return "AI"
	}
	return m.SenderType
}

// AIPerformanceResult 自动化率漏斗
type AIPerformanceResult struct {
	Window        string           `json:"window"`
	TotalSessions int64            `json:"total_sessions"`
	AIHandled     int64            `json:"ai_handled"`
	HumanHandled  int64            `json:"human_handled"`
	ClosedByAI    int64            `json:"closed_by_ai"`
	AutoRate      float64          `json:"automation_rate"`
	LLMCalls      int64            `json:"llm_calls"`
	LLMCost       float64          `json:"llm_cost"`
	Breakdown     map[string]int64 `json:"llm_by_scenario,omitempty"`
}

// AIPerformance AI 代理绩效（窗口天）
func (s *EmailGapService) AIPerformance(ctx context.Context, days int) (*AIPerformanceResult, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	repo := repository.NewAIPerformanceRepository(s.db)
	since := time.Now().AddDate(0, 0, -days)
	res := &AIPerformanceResult{Window: fmt.Sprintf("%dd", days)}
	res.TotalSessions, _ = repo.CountSessionsSince(ctx, since, "")
	res.ClosedByAI, _ = repo.CountSessionsSince(ctx, since, "status = 'closed'")

	res.AIHandled, _ = repo.CountSessionsSince(ctx, since, "(agent_id IS NULL OR agent_id = '')")
	res.HumanHandled = res.TotalSessions - res.AIHandled
	if res.TotalSessions > 0 {
		res.AutoRate = float64(res.AIHandled) * 100 / float64(res.TotalSessions)
	}

	if lrs, err := repo.SumLLMRoutingByScenario(ctx, since); err == nil {
		res.Breakdown = map[string]int64{}
		for _, r := range lrs {
			res.LLMCalls += r.Cnt
			res.LLMCost += r.Cost
			res.Breakdown[r.Scenario] = r.Cnt
		}
	}
	return res, nil
}
