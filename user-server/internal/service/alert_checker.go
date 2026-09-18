package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/metrics"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// MetricSnapshot 当前指标快照
type MetricSnapshot struct {
	Name  string
	Value float64
}

// MetricProvider 指标取数接口，由 metrics 包 / 业务侧实现
type MetricProvider interface {
	// Snapshot 返回当前所有指标；checker 按规则 source 名称匹配
	Snapshot(ctx context.Context) ([]MetricSnapshot, error)
}

// AlertNotifier 告警通知器接口（与 ReachSender 解耦）
type AlertNotifier interface {
	Notify(ctx context.Context, rule *model.AlertRule, history *model.AlertHistory) error
}

// AlertChecker 告警规则检查器
//
// 设计要点（plan v3.1 §T8）：
//   - 定时扫描启用的规则，比对指标值与阈值
//   - 冷却期内不重复触发（last_triggered_at + cooldown_seconds）
//   - 触发即写 AlertHistory，记录快照
//   - 恢复检测：若历史中存在 firing 状态且当前未触发，则写恢复时间
//
// 不依赖外部 Alertmanager；通知由注入的 AlertNotifier 实现（邮件 / 钉钉 / webhook）。
type AlertChecker struct {
	ruleRepo repository.AlertRuleRepository
	histRepo repository.AlertHistoryRepository
	provider MetricProvider
	notifier AlertNotifier
	interval time.Duration
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewAlertChecker 构造
func NewAlertChecker(provider MetricProvider, notifier AlertNotifier, interval time.Duration) *AlertChecker {
	return &AlertChecker{
		ruleRepo: repository.NewAlertRuleRepository(),
		histRepo: repository.NewAlertHistoryRepository(),
		provider: provider,
		notifier: notifier,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

// Start 启动后台检查器
func (c *AlertChecker) Start() {
	c.wg.Add(1)
	go c.loop()
	logger.Infof("[AlertChecker] 已启动，间隔 %v", c.interval)
}

// Stop 停止
func (c *AlertChecker) Stop() {
	close(c.stop)
	c.wg.Wait()
	logger.Info("[AlertChecker] 已停止")
}

func (c *AlertChecker) loop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.checkOnce(context.Background())
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.checkOnce(context.Background())
		}
	}
}

// CheckOnce 单次执行（暴露给定时任务/手动触发）
func (c *AlertChecker) CheckOnce(ctx context.Context) (int, error) {
	return c.checkOnce(ctx)
}

func (c *AlertChecker) checkOnce(ctx context.Context) (int, error) {
	rules, err := c.ruleRepo.ListEnabled(ctx)
	if err != nil {
		logger.Error(err, "[AlertChecker] 拉取规则失败")
		return 0, err
	}
	if len(rules) == 0 {
		return 0, nil
	}
	if c.provider == nil {
		return 0, fmt.Errorf("MetricProvider 未注入")
	}
	snapshots, err := c.provider.Snapshot(ctx)
	if err != nil {
		logger.Error(err, "[AlertChecker] 拉取指标失败")
		return 0, err
	}
	snapMap := make(map[string]float64, len(snapshots))
	for _, s := range snapshots {
		snapMap[s.Name] = s.Value
	}

	fired := 0
	now := time.Now()
	for _, r := range rules {

		if r.LastTriggeredAt != nil && now.Sub(*r.LastTriggeredAt) < time.Duration(r.CooldownSeconds)*time.Second {
			continue
		}
		val, ok := snapMap[r.Source]
		if !ok {

			_ = c.tryResolve(ctx, r.ID)
			continue
		}
		if !evaluateOperator(r.Operator, val, r.Threshold) {
			_ = c.tryResolve(ctx, r.ID)
			continue
		}
		if err := c.fire(ctx, r, val, now); err != nil {
			logger.Error(err, "[AlertChecker] 触发告警失败: "+r.Name)
			continue
		}
		fired++
	}
	return fired, nil
}

func (c *AlertChecker) fire(ctx context.Context, rule *model.AlertRule, val float64, at time.Time) error {
	msg := fmt.Sprintf("[%s] %s: %s 当前值 %.4f %s 阈值 %.4f（窗口 %ds）",
		strings.ToUpper(string(rule.Severity)), rule.Source, rule.Name, val, rule.Operator, rule.Threshold, rule.WindowSeconds)
	h := &model.AlertHistory{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Source:      rule.Source,
		Value:       val,
		Threshold:   rule.Threshold,
		Severity:    rule.Severity,
		Message:     msg,
		Status:      model.AlertHistoryFiring,
		Channels:    rule.Channels,
		TriggeredAt: at,
	}
	if err := c.histRepo.Create(ctx, h); err != nil {
		return fmt.Errorf("写入告警历史失败: %w", err)
	}
	if c.notifier != nil {
		notifyFn := func() {
			if err := c.notifier.Notify(ctx, rule, h); err != nil {
				logger.Error(err, "[AlertChecker] 通知下发失败: "+rule.Name)
				h.NotifyResult = "notify_error: " + err.Error()
			} else {
				h.NotifyResult = "ok"
			}
			_ = updateNotifyResult(ctx, c.histRepo, h)
		}

		if d := GetAlertDispatcher(); d != nil {
			d.Dispatch("alertrule:"+rule.Name, notifyFn)
		} else {
			notifyFn()
		}
	}
	if err := c.ruleRepo.UpdateLastTriggered(ctx, rule.ID, at); err != nil {
		logger.Error(err, "[AlertChecker] 更新 last_triggered_at 失败")
	}
	logger.Warnf("[AlertChecker] TRIGGER %s | src=%s val=%.4f thr=%.4f", rule.Name, rule.Source, val, rule.Threshold)
	return nil
}

func (c *AlertChecker) tryResolve(ctx context.Context, ruleID uint) error {
	return c.histRepo.ResolveFiring(ctx, ruleID, time.Now())
}

func evaluateOperator(op string, val, threshold float64) bool {
	switch strings.ToLower(op) {
	case "gt":
		return val > threshold
	case "ge":
		return val >= threshold
	case "lt":
		return val < threshold
	case "le":
		return val <= threshold
	case "eq":
		return val == threshold
	case "ne":
		return val != threshold
	}
	return false
}

func updateNotifyResult(ctx context.Context, repo repository.AlertHistoryRepository, h *model.AlertHistory) error {
	type updater interface {
		UpdateNotify(context.Context, uint, string) error
	}
	if u, ok := repo.(updater); ok {
		return u.UpdateNotify(ctx, h.ID, h.NotifyResult)
	}
	return nil
}

type metricsProvider struct{}

// NewMetricsAlertProvider 构造基于 metrics 包的 Provider
func NewMetricsAlertProvider() MetricProvider { return &metricsProvider{} }

func (p *metricsProvider) Snapshot(ctx context.Context) ([]MetricSnapshot, error) {
	_ = ctx
	snap := metrics.Snapshot()
	out := make([]MetricSnapshot, 0, len(snap))
	for name, v := range snap {
		out = append(out, MetricSnapshot{Name: name, Value: v})
	}
	return out, nil
}

type logNotifier struct{}

// NewLogAlertNotifier 构造
func NewLogAlertNotifier() AlertNotifier { return &logNotifier{} }

func (n *logNotifier) Notify(ctx context.Context, rule *model.AlertRule, h *model.AlertHistory) error {
	_ = ctx
	var chans []string
	_ = json.Unmarshal([]byte(rule.Channels), &chans)
	logger.Warnf("[AlertNotify] rule=%s severity=%s channels=%v msg=%s", rule.Name, rule.Severity, chans, h.Message)
	return nil
}

// MultiNotifier 遍历调用所有子 Notifier，单个失败不中断，返回首个 error
type MultiNotifier struct {
	notifiers []AlertNotifier
}

func NewMultiNotifier(notifiers ...AlertNotifier) AlertNotifier {
	filtered := make([]AlertNotifier, 0, len(notifiers))
	for _, n := range notifiers {
		if n != nil {
			filtered = append(filtered, n)
		}
	}
	return &MultiNotifier{notifiers: filtered}
}

func (m *MultiNotifier) Notify(ctx context.Context, rule *model.AlertRule, h *model.AlertHistory) error {
	var firstErr error
	for _, n := range m.notifiers {
		if err := n.Notify(ctx, rule, h); err != nil {
			logger.Warnf("[MultiNotifier] 子通知器失败: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// EmailSender 抽象 EmailService 避免循环依赖，语义与 EmailService.Send 对齐
type EmailSender interface {
	Send(ctx context.Context, accountID uint, to, subject, content string, attachments []string) (string, error)
}

type EmailAlertNotifier struct {
	sender     EmailSender
	recipients []string
	accountID  uint
}

func NewEmailAlertNotifier(sender EmailSender, recipients []string, accountID uint) AlertNotifier {
	return &EmailAlertNotifier{sender: sender, recipients: recipients, accountID: accountID}
}

func (n *EmailAlertNotifier) Notify(ctx context.Context, rule *model.AlertRule, h *model.AlertHistory) error {
	if n.sender == nil || len(n.recipients) == 0 {
		return nil
	}
	subject := fmt.Sprintf("[HiveMTK 告警] %s (%s)", rule.Name, rule.Severity)
	var firstErr error
	for _, to := range n.recipients {
		to = strings.TrimSpace(to)
		if to == "" {
			continue
		}
		if _, err := n.sender.Send(ctx, n.accountID, to, subject, h.Message, nil); err != nil {
			logger.Warnf("[EmailAlertNotifier] 发送告警给 %s 失败: %v", to, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

type WebhookAlertNotifier struct {
	url string
}

func NewWebhookAlertNotifier(url string) AlertNotifier {
	return &WebhookAlertNotifier{url: url}
}

func (n *WebhookAlertNotifier) Notify(ctx context.Context, rule *model.AlertRule, h *model.AlertHistory) error {
	if n.url == "" {
		return nil
	}
	payload := map[string]any{
		"rule_name":    rule.Name,
		"severity":     rule.Severity,
		"source":       rule.Source,
		"message":      h.Message,
		"value":        h.Value,
		"threshold":    h.Threshold,
		"status":       h.Status,
		"triggered_at": h.TriggeredAt.Format(time.RFC3339),
		"channels":     rule.Channels,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("webhook marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook 返回非 2xx: %d", resp.StatusCode)
	}
	return nil
}
