package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

const (
	abnormalHourStart = 2
	abnormalHourEnd   = 5

	frequentFailureThreshold = 5

	frequentFailureWindow = 1 * time.Hour

	locationDistanceThreshold = 1000.0

	deviceFingerprintChangeWindow = 7 * 24 * time.Hour
)

// LoginRiskContext 登录风险检测上下文
// 由 controller 在登录入口收集，传给 LoginRiskService.Evaluate
type LoginRiskContext struct {
	UserID            uint
	Username          string
	IP                string
	UserAgent         string
	DeviceFingerprint string
	Success           bool
	LoginAt           time.Time
	Reason            string
}

// LoginRiskResult 风险评估结果
type LoginRiskResult struct {
	RiskLevel        model.RiskLevel
	ShouldAlert      bool
	ShouldForceMFA   bool
	AlertType        string
	AlertTitle       string
	AlertDescription string
	Reasons          []string
	Location         string
	LoginEventID     uint
	SecurityAlertID  uint
}

// LoginRiskService 登录风险评估服务
type LoginRiskService struct {
	repo repository.LoginRiskRepository
}

// NewLoginRiskService 创建登录风险服务
func NewLoginRiskService() *LoginRiskService {
	return &LoginRiskService{repo: repository.NewLoginRiskRepository()}
}

// NewLoginRiskServiceWithRepo 通过 repository 注入创建服务（便于测试）
func NewLoginRiskServiceWithRepo(repo repository.LoginRiskRepository) *LoginRiskService {
	return &LoginRiskService{repo: repo}
}

// Evaluate 评估登录风险并写入事件 / 告警
//
// 评估维度：
//  1. 异常时段（凌晨 2-5 点）
//  2. 频次异常（1 小时内 ≥5 次失败）
//  3. 异地登录（IP 与上次登录地距离 > 1000km）
//  4. 设备指纹变更（7 天内首次出现的新指纹）
//
// 输出：risk_level ∈ {low, medium, high, critical}
//   - low: 无异常
//   - medium: 单一中等风险信号（异常时段 / 设备指纹变更）
//   - high: 异地登录 / 频繁失败
//   - critical: 多维度同时触发（如异地 + 频繁失败）
func (s *LoginRiskService) Evaluate(ctx context.Context, riskCtx *LoginRiskContext) (*LoginRiskResult, error) {
	if riskCtx.LoginAt.IsZero() {
		riskCtx.LoginAt = time.Now()
	}

	result := &LoginRiskResult{
		RiskLevel: model.RiskLevelLow,
	}

	rctx := ctx

	result.Location = s.resolveLocation(rctx, riskCtx.IP)

	if s.isAbnormalHour(rctx, riskCtx.LoginAt) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("异常时段登录（%02d:00-%02d:00）", abnormalHourStart, abnormalHourEnd))
		if result.RiskLevel == model.RiskLevelLow {
			result.RiskLevel = model.RiskLevelMedium
		}
	}

	failureCount, err := s.countRecentFailures(rctx, riskCtx.UserID, riskCtx.Username, riskCtx.LoginAt)
	if err != nil {
		logger.Errorf("查询最近失败次数失败: %v", err)
	}
	if failureCount >= frequentFailureThreshold {
		result.Reasons = append(result.Reasons, fmt.Sprintf("1 小时内失败 %d 次（阈值 %d）", failureCount, frequentFailureThreshold))
		result.RiskLevel = s.upgradeRisk(rctx, result.RiskLevel, model.RiskLevelHigh)
		result.AlertType = model.AlertTypeFrequentFailure
	}

	prevLocation, prevIP, hasPrev := s.getPreviousLoginLocation(rctx, riskCtx.UserID)
	if hasPrev {
		distance := s.calculateDistance(rctx, result.Location, prevLocation)
		if distance > locationDistanceThreshold {
			result.Reasons = append(result.Reasons, fmt.Sprintf("异地登录：本次=%s, 上次=%s(%s), 距离≈%.0fkm",
				result.Location, prevLocation, prevIP, distance))
			result.RiskLevel = s.upgradeRisk(rctx, result.RiskLevel, model.RiskLevelHigh)
			if result.AlertType == "" {
				result.AlertType = model.AlertTypeAbnormalLogin
			}
		}
	}

	if riskCtx.DeviceFingerprint != "" {
		if s.isDeviceFingerprintChanged(rctx, riskCtx.UserID, riskCtx.DeviceFingerprint, riskCtx.LoginAt) {
			result.Reasons = append(result.Reasons, "设备指纹变更（7 天内首次出现）")
			if result.RiskLevel == model.RiskLevelLow {
				result.RiskLevel = model.RiskLevelMedium
			}
			if result.AlertType == "" {
				result.AlertType = model.AlertTypeDeviceChange
			}
		}
	}

	if len(result.Reasons) >= 2 && result.RiskLevel == model.RiskLevelHigh {
		result.RiskLevel = model.RiskLevelCritical
	}

	if result.RiskLevel == model.RiskLevelHigh || result.RiskLevel == model.RiskLevelCritical {
		result.ShouldAlert = true
		result.ShouldForceMFA = true
		if result.AlertType == "" {
			result.AlertType = model.AlertTypeAbnormalLogin
		}
		result.AlertTitle = s.buildAlertTitle(ctx, riskCtx, result.RiskLevel)
		result.AlertDescription = s.buildAlertDescription(ctx, riskCtx, result)
	}

	loginEvent := &model.LoginEvent{
		UserID:            riskCtx.UserID,
		Username:          riskCtx.Username,
		IP:                riskCtx.IP,
		UserAgent:         riskCtx.UserAgent,
		DeviceFingerprint: riskCtx.DeviceFingerprint,
		LoginAt:           riskCtx.LoginAt,
		Success:           riskCtx.Success,
		RiskLevel:         result.RiskLevel,
		Location:          result.Location,
		Reason:            strings.Join(result.Reasons, "; "),
	}
	created, err := s.repo.CreateLoginEvent(rctx, loginEvent)
	if err != nil {
		logger.Errorf("写入 login_events 失败: %v", err)
		return result, fmt.Errorf("写入登录事件失败: %w", err)
	}
	result.LoginEventID = created.ID

	if result.ShouldAlert {
		alert := &model.SecurityAlert{
			UserID:       riskCtx.UserID,
			Username:     riskCtx.Username,
			AlertType:    result.AlertType,
			RiskLevel:    result.RiskLevel,
			Title:        result.AlertTitle,
			Description:  result.AlertDescription,
			IP:           riskCtx.IP,
			Location:     result.Location,
			LoginEventID: created.ID,
			Status:       model.SecurityAlertStatusOpen,
		}
		savedAlert, err := s.repo.CreateSecurityAlert(rctx, alert)
		if err != nil {
			logger.Errorf("写入 security_alerts 失败: %v", err)
		} else {
			result.SecurityAlertID = savedAlert.ID
			s.pushNotification(rctx, savedAlert)
		}
	}

	return result, nil
}

func (s *LoginRiskService) isAbnormalHour(ctx context.Context, t time.Time) bool {
	hour := t.Hour()
	return hour >= abnormalHourStart && hour < abnormalHourEnd
}

func (s *LoginRiskService) countRecentFailures(ctx context.Context, userID uint, username string, now time.Time) (int64, error) {
	if s.repo == nil {
		return 0, errors.New("repo 为空")
	}
	return s.repo.CountRecentFailures(ctx, userID, username, now.Add(-frequentFailureWindow))
}

func (s *LoginRiskService) getPreviousLoginLocation(ctx context.Context, userID uint) (location, ip string, hasPrev bool) {
	if s.repo == nil || userID == 0 {
		return "", "", false
	}
	loc, ipAddr, found, err := s.repo.GetLastSuccessLocation(ctx, userID)
	if err != nil {
		logger.Errorf("查询上次登录地点失败: %v", err)
		return "", "", false
	}
	return loc, ipAddr, found
}

func (s *LoginRiskService) isDeviceFingerprintChanged(ctx context.Context, userID uint, fingerprint string, now time.Time) bool {
	if s.repo == nil || userID == 0 || fingerprint == "" {
		return false
	}
	since := now.Add(-deviceFingerprintChangeWindow)
	count, err := s.repo.CountDeviceFingerprintSince(ctx, userID, fingerprint, since)
	if err != nil {
		logger.Errorf("查询设备指纹失败: %v", err)
		return false
	}
	return count == 0
}

func (s *LoginRiskService) resolveLocation(ctx context.Context, ip string) string {
	if ip == "" {
		return "unknown"
	}
	if ip == "127.0.0.1" || ip == "::1" {
		return "localhost"
	}
	if strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "172.") {
		return "private-network"
	}
	h := sha256.Sum256([]byte(ip))
	hashStr := hex.EncodeToString(h[:])
	lat := float64(int(hashStr[0])<<8|int(hashStr[1]))/65535.0*180.0 - 90.0
	lon := float64(int(hashStr[2])<<8|int(hashStr[3]))/65535.0*360.0 - 180.0
	return fmt.Sprintf("geo(%.4f,%.4f)", lat, lon)
}

func (s *LoginRiskService) calculateDistance(ctx context.Context, loc1, loc2 string) float64 {
	lat1, lon1, ok1 := s.parseGeo(ctx, loc1)
	lat2, lon2, ok2 := s.parseGeo(ctx, loc2)
	if !ok1 || !ok2 {
		return 0
	}

	const earthRadiusKm = 6371.0

	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

func (s *LoginRiskService) parseGeo(ctx context.Context, s_ string) (float64, float64, bool) {
	var lat, lon float64
	if _, err := fmt.Sscanf(s_, "geo(%f,%f)", &lat, &lon); err != nil {
		return 0, 0, false
	}
	return lat, lon, true
}

func toRadians(deg float64) float64 {
	return deg * math.Pi / 180.0
}

func (s *LoginRiskService) upgradeRisk(ctx context.Context, current, target model.RiskLevel) model.RiskLevel {
	order := map[model.RiskLevel]int{
		model.RiskLevelLow:      0,
		model.RiskLevelMedium:   1,
		model.RiskLevelHigh:     2,
		model.RiskLevelCritical: 3,
	}
	if order[target] > order[current] {
		return target
	}
	return current
}

func (s *LoginRiskService) buildAlertTitle(ctx context.Context, riskCtx *LoginRiskContext, level model.RiskLevel) string {
	username := riskCtx.Username
	if username == "" {
		username = fmt.Sprintf("user#%d", riskCtx.UserID)
	}
	return fmt.Sprintf("[%s] 异常登录告警: %s", strings.ToUpper(string(level)), username)
}

func (s *LoginRiskService) buildAlertDescription(ctx context.Context, riskCtx *LoginRiskContext, result *LoginRiskResult) string {
	parts := []string{
		fmt.Sprintf("用户: %s", riskCtx.Username),
		fmt.Sprintf("IP: %s", riskCtx.IP),
		fmt.Sprintf("地点: %s", result.Location),
		fmt.Sprintf("时间: %s", riskCtx.LoginAt.Format(time.RFC3339)),
	}
	if len(result.Reasons) > 0 {
		parts = append(parts, "风险原因: "+strings.Join(result.Reasons, "; "))
	}
	return strings.Join(parts, "\n")
}

func (s *LoginRiskService) pushNotification(ctx context.Context, alert *model.SecurityAlert) {
	if alert == nil {
		return
	}
	if s.repo == nil {
		return
	}
	notif := &model.Notification{
		UserID:  alert.UserID,
		Type:    model.NotificationTypeWarning,
		Title:   alert.Title,
		Content: alert.Description,
	}
	if err := s.repo.CreateNotification(ctx, notif); err != nil {
		logger.Errorf("推送安全告警通知失败: %v", err)
	}

	if err := s.repo.MarkAlertNotified(ctx, alert.ID); err != nil {
		logger.Errorf("更新告警 notified 状态失败: %v", err)
	}
}

// ComputeDeviceFingerprint 从 User-Agent + IP 计算设备指纹
// 简化版：SHA-256(UA + "|" + IP 前 3 段)
// 真实场景应加入更多维度：屏幕分辨率、时区、语言、Canvas 指纹等
func ComputeDeviceFingerprint(userAgent, ip string) string {
	ipPrefix := ip
	parts := strings.Split(ip, ".")
	if len(parts) >= 3 {
		ipPrefix = strings.Join(parts[:3], ".")
	}

	data := userAgent + "|" + ipPrefix
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:32]
}

// ListLoginEvents 查询用户登录事件列表（分页）
func (s *LoginRiskService) ListLoginEvents(ctx context.Context, userID uint, page, pageSize int) ([]*model.LoginEvent, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("repo 未初始化")
	}
	return s.repo.ListLoginEvents(ctx, userID, page, pageSize)
}

// ListSecurityAlerts 查询安全告警列表
func (s *LoginRiskService) ListSecurityAlerts(ctx context.Context, userID uint, status string, page, pageSize int) ([]*model.SecurityAlert, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("repo 未初始化")
	}
	return s.repo.ListSecurityAlerts(ctx, userID, status, page, pageSize)
}

// ResolveSecurityAlert 处理安全告警
func (s *LoginRiskService) ResolveSecurityAlert(ctx context.Context, alertID, resolverUserID uint, note string) error {
	if s.repo == nil {
		return errors.New("repo 未初始化")
	}
	return s.repo.ResolveSecurityAlert(ctx, alertID, resolverUserID, note, time.Now(), string(model.SecurityAlertStatusResolved))
}

// IgnoreSecurityAlert 忽略告警
func (s *LoginRiskService) IgnoreSecurityAlert(ctx context.Context, alertID, resolverUserID uint, note string) error {
	if s.repo == nil {
		return errors.New("repo 未初始化")
	}
	return s.repo.ResolveSecurityAlert(ctx, alertID, resolverUserID, note, time.Now(), string(model.SecurityAlertStatusIgnored))
}
