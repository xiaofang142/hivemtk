package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// AnomalyLoginAlertChannel 告警通道
type AnomalyLoginAlertChannel string

const (
	AnomalyLoginChannelAudit   AnomalyLoginAlertChannel = "audit"
	AnomalyLoginChannelEmail   AnomalyLoginAlertChannel = "email"
	AnomalyLoginChannelInbox   AnomalyLoginAlertChannel = "inbox"
	AnomalyLoginChannelWebsock AnomalyLoginAlertChannel = "websocket"
)

// AnomalyLoginDetectorConfig 异常登录预警配置
type AnomalyLoginDetectorConfig struct {
	EmailEnabled         bool
	AuditEnabled         bool
	InboxEnabled         bool
	AdminEmails          []string
	EmailSubjectTemplate string
	EmailBodyTemplate    string
}

// DefaultAnomalyLoginDetectorConfig 默认配置
func DefaultAnomalyLoginDetectorConfig() AnomalyLoginDetectorConfig {
	return AnomalyLoginDetectorConfig{
		EmailEnabled:         true,
		AuditEnabled:         true,
		InboxEnabled:         true,
		AdminEmails:          []string{},
		EmailSubjectTemplate: "[%s] 异常登录告警 - %s",
		EmailBodyTemplate: `系统检测到账户 %s 的异常登录：

时间：%s
IP：%s
地点：%s
风险等级：%s
风险原因：%s

请及时确认是否本人操作。如非本人，请立即修改密码。
`,
	}
}

// AnomalyLoginAlertResult 告警分发结果
type AnomalyLoginAlertResult struct {
	AlertID         uint
	LoginEventID    uint
	ChannelsSent    []AnomalyLoginAlertChannel
	ChannelsFailed  []AnomalyLoginAlertChannel
	ShouldForceMFA  bool
	RiskLevel       model.RiskLevel
	EmailDispatched bool
	AuditLogged     bool
	InboxCreated    bool
}

// AnomalyLoginDetector 异常登录预警服务
type AnomalyLoginDetector struct {
	riskService *LoginRiskService
	config      AnomalyLoginDetectorConfig
}

// NewAnomalyLoginDetector 创建异常登录预警服务
func NewAnomalyLoginDetector() *AnomalyLoginDetector {
	return &AnomalyLoginDetector{
		riskService: NewLoginRiskService(),
		config:      DefaultAnomalyLoginDetectorConfig(),
	}
}

// NewAnomalyLoginDetectorWithConfig 使用自定义配置创建服务
func NewAnomalyLoginDetectorWithConfig(cfg AnomalyLoginDetectorConfig) *AnomalyLoginDetector {
	return &AnomalyLoginDetector{
		riskService: NewLoginRiskService(),
		config:      cfg,
	}
}

// SetAdminEmails 设置管理员邮箱
func (d *AnomalyLoginDetector) SetAdminEmails(ctx context.Context, emails []string) {
	d.config.AdminEmails = emails
}

// SetConfig 整体替换配置
func (d *AnomalyLoginDetector) SetConfig(ctx context.Context, cfg AnomalyLoginDetectorConfig) {
	d.config = cfg
}

// DetectAndAlert 主入口：检测 + 告警分发
//
// 流程：
//  1. 委托 LoginRiskService.Evaluate 做 4 项检测（异地/频次/时段/设备指纹）
//  2. 拿到 LoginRiskResult 后，若 ShouldAlert=true，走三通道告警
//  3. 通道：审计日志 + 邮件 + 站内信
//
// 任何通道失败都不影响其他通道（不吞错，逐通道返回成败）
func (d *AnomalyLoginDetector) DetectAndAlert(ctx context.Context, lctx *LoginRiskContext) (*AnomalyLoginAlertResult, error) {
	if lctx == nil {
		return nil, errors.New("login context is nil")
	}

	riskResult, err := d.riskService.Evaluate(ctx, lctx)
	if err != nil {
		return nil, fmt.Errorf("风险评估失败: %w", err)
	}

	result := &AnomalyLoginAlertResult{
		LoginEventID:   riskResult.LoginEventID,
		AlertID:        riskResult.SecurityAlertID,
		ShouldForceMFA: riskResult.ShouldForceMFA,
		RiskLevel:      riskResult.RiskLevel,
		ChannelsSent:   []AnomalyLoginAlertChannel{},
		ChannelsFailed: []AnomalyLoginAlertChannel{},
	}

	if !riskResult.ShouldAlert {
		return result, nil
	}

	if d.config.AuditEnabled {
		if err := d.writeAuditLog(ctx, lctx, riskResult); err != nil {
			logger.Errorf("[anomaly_login] 审计日志写入失败: %v", err)
			result.ChannelsFailed = append(result.ChannelsFailed, AnomalyLoginChannelAudit)
		} else {
			result.AuditLogged = true
			result.ChannelsSent = append(result.ChannelsSent, AnomalyLoginChannelAudit)
		}
	}

	if d.config.InboxEnabled {
		if err := d.writeInboxNotification(ctx, lctx, riskResult); err != nil {
			logger.Errorf("[anomaly_login] 站内信写入失败: %v", err)
			result.ChannelsFailed = append(result.ChannelsFailed, AnomalyLoginChannelInbox)
		} else {
			result.InboxCreated = true
			result.ChannelsSent = append(result.ChannelsSent, AnomalyLoginChannelInbox)
		}
	}

	if d.config.EmailEnabled && len(d.config.AdminEmails) > 0 {
		if err := d.sendEmailAlert(ctx, lctx, riskResult); err != nil {
			logger.Errorf("[anomaly_login] 邮件告警发送失败: %v", err)
			result.ChannelsFailed = append(result.ChannelsFailed, AnomalyLoginChannelEmail)
		} else {
			result.EmailDispatched = true
			result.ChannelsSent = append(result.ChannelsSent, AnomalyLoginChannelEmail)
		}
	}

	return result, nil
}

func (d *AnomalyLoginDetector) writeAuditLog(ctx context.Context, lctx *LoginRiskContext, result *LoginRiskResult) error {
	if result.SecurityAlertID == 0 {
		return errors.New("alert id is 0, alert not persisted")
	}
	repo := repository.NewOperationLogRepository()
	now := time.Now()
	logEntry := &model.OperationLog{
		UserID:     lctx.UserID,
		Username:   lctx.Username,
		Action:     "anomaly_login_detected",
		Module:     "auth",
		Resource:   "login_event",
		ResourceID: fmt.Sprintf("%d", result.LoginEventID),
		Detail: fmt.Sprintf("risk_level=%s, alert_id=%d, reasons=%s",
			result.RiskLevel, result.SecurityAlertID, strings.Join(result.Reasons, ";")),
		IP:        lctx.IP,
		UserAgent: lctx.UserAgent,
		CreatedAt: now,
	}
	return repo.Create(ctx, logEntry)
}

func (d *AnomalyLoginDetector) writeInboxNotification(ctx context.Context, lctx *LoginRiskContext, result *LoginRiskResult) error {
	notif := &model.Notification{
		UserID:  lctx.UserID,
		Type:    model.NotificationTypeWarning,
		Title:   result.AlertTitle,
		Content: result.AlertDescription,
	}
	riskRepo := repository.NewLoginRiskRepository()
	return riskRepo.CreateNotification(ctx, notif)
}

func (d *AnomalyLoginDetector) sendEmailAlert(ctx context.Context, lctx *LoginRiskContext, result *LoginRiskResult) error {
	subject := fmt.Sprintf(d.config.EmailSubjectTemplate,
		strings.ToUpper(string(result.RiskLevel)),
		lctx.Username,
	)
	body := fmt.Sprintf(d.config.EmailBodyTemplate,
		lctx.Username,
		lctx.LoginAt.Format(time.RFC3339),
		lctx.IP,
		result.Location,
		result.RiskLevel,
		strings.Join(result.Reasons, "; "),
	)

	emailRepo := repository.NewEmailSendRepository()
	email := &model.EmailSend{
		To:      strings.Join(d.config.AdminEmails, ","),
		Subject: subject,
		Content: body,
		Status:  0,
	}
	return emailRepo.Create(ctx, email)
}

// ListAlerts 列出告警（供 controller 复用，复用 LoginRiskService 已存在的方法）
func (d *AnomalyLoginDetector) ListAlerts(ctx context.Context, userID uint, status string, page, pageSize int) ([]*model.SecurityAlert, int64, error) {
	return d.riskService.ListSecurityAlerts(ctx, userID, status, page, pageSize)
}

// ListLoginEvents 列出登录事件（供 controller 复用）
func (d *AnomalyLoginDetector) ListLoginEvents(ctx context.Context, userID uint, page, pageSize int) ([]*model.LoginEvent, int64, error) {
	return d.riskService.ListLoginEvents(ctx, userID, page, pageSize)
}

// ResolveAlert 解决告警（带审计日志）
func (d *AnomalyLoginDetector) ResolveAlert(ctx context.Context, alertID, operatorID uint, note string) error {
	if err := d.riskService.ResolveSecurityAlert(ctx, alertID, operatorID, note); err != nil {
		return err
	}
	repo := repository.NewOperationLogRepository()
	_ = repo.Create(ctx, &model.OperationLog{
		UserID:     operatorID,
		Action:     "anomaly_login_resolve",
		Module:     "auth",
		Resource:   "security_alert",
		ResourceID: fmt.Sprintf("%d", alertID),
		Detail:     fmt.Sprintf("note=%s", note),
		CreatedAt:  time.Now(),
	})
	return nil
}

// IgnoreAlert 忽略告警（带审计日志）
func (d *AnomalyLoginDetector) IgnoreAlert(ctx context.Context, alertID, operatorID uint, note string) error {
	if err := d.riskService.IgnoreSecurityAlert(ctx, alertID, operatorID, note); err != nil {
		return err
	}
	repo := repository.NewOperationLogRepository()
	_ = repo.Create(ctx, &model.OperationLog{
		UserID:     operatorID,
		Action:     "anomaly_login_ignore",
		Module:     "auth",
		Resource:   "security_alert",
		ResourceID: fmt.Sprintf("%d", alertID),
		Detail:     fmt.Sprintf("note=%s", note),
		CreatedAt:  time.Now(),
	})
	return nil
}
