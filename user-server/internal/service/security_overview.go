// security_overview.go 安全总览（R39-K3 的 overview 腿）。
//
// 聚合四路数据源给管理员一块"谁在打我"的仪表：
//  1. login_events 近 24h 总量/高危量（异常登录腿，anomaly detector 已在写）；
//  2. security_alerts open 分布（登录风控产生的告警）；
//  3. 最近一次 security_audit 的成绩与弱密码抽样结论（scan 腿的缓存读）；
//  4. platform_config_kv 里 JWT 401 聚合（middleware/security_recorder 写入）——
//     近 7 天按形态/路径前缀的分布，"没数据"与"没装配"分开报。
//
// 弱密码结论取自最近一次审计项（名称「常见弱密码抽样探测」），总览不现场重跑
// bcrypt 抽样——那是 POST /security/audit 的职责，总览必须是毫秒级读。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/securitytelemetry"
	"hivemtk-user/internal/repository"
)

type SecurityOverviewService struct {
	auditRepo *repository.SecurityAuditRepository
	kvRepo    repository.PlatformConfigKVRepository
}

// NewSecurityOverviewService 构造。kvRepo 为 nil 时 401 段报 not_installed。
func NewSecurityOverviewService(auditRepo *repository.SecurityAuditRepository, kvRepo repository.PlatformConfigKVRepository) *SecurityOverviewService {
	return &SecurityOverviewService{auditRepo: auditRepo, kvRepo: kvRepo}
}

// SecurityUnauthorizedAccess 401 聚合段。
type SecurityUnauthorizedAccess struct {
	Installed bool             `json:"installed"`      // recorder 是否装配过（有 kv 行即视为装配）
	Today     map[string]int64 `json:"today"`          // 今日按失败形态计数
	TopPaths  []string         `json:"top_paths"`      // 近 7 天被打最多的路径前缀（≤5）
	WeekTotal int64            `json:"week_total"`     // 近 7 天总数
	Note      string           `json:"note,omitempty"` // not_installed / no_data 时的说明
}

// SecurityLastAudit 最近一次审计的摘要（无审计时 nil）。
type SecurityLastAudit struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Score      int        `json:"score"`
	Passed     int        `json:"passed"`
	Failed     int        `json:"failed"`
	Warnings   int        `json:"warnings"`
	RiskLevel  string     `json:"risk_level"`
	FinishedAt *time.Time `json:"finished_at"`
	// WeakPassword 弱密码抽样项的结论（result + message）；从未跑过含该项的审计时为 nil。
	WeakPassword *model.SecurityAuditItem `json:"weak_password,omitempty"`
}

// SecurityOverview 总览载荷。
type SecurityOverview struct {
	GeneratedAt            time.Time                   `json:"generated_at"`
	LoginEvents24h         int64                       `json:"login_events_24h"`
	HighRiskLoginEvents24h int64                       `json:"high_risk_login_events_24h"`
	OpenAlerts             map[string]int64            `json:"open_alerts"` // 风险级 → open 数
	OpenAlertsTotal        int64                       `json:"open_alerts_total"`
	LastAudit              *SecurityLastAudit          `json:"last_audit,omitempty"`
	Unauthorized           *SecurityUnauthorizedAccess `json:"unauthorized_access"`
}

// Overview 聚合四路数据源。单路失败不拖垮整块：该段落 error 并继续其余段
// （总览是仪表不是事务，半块仪表好过没有仪表）。
func (s *SecurityOverviewService) Overview(ctx context.Context) (*SecurityOverview, error) {
	if s == nil || s.auditRepo == nil {
		return nil, errors.New("security overview service not initialized")
	}
	out := &SecurityOverview{
		GeneratedAt: time.Now(),
		OpenAlerts:  map[string]int64{},
	}

	if total, high, err := s.auditRepo.CountLoginEventsSince(ctx, time.Now().Add(-24*time.Hour)); err == nil {
		out.LoginEvents24h = total
		out.HighRiskLoginEvents24h = high
	}
	if bySev, err := s.auditRepo.CountOpenAlertsBySeverity(ctx); err == nil {
		out.OpenAlerts = bySev
		for _, n := range bySev {
			out.OpenAlertsTotal += n
		}
	}
	if last, err := s.auditRepo.LatestSecurityAudit(ctx); err == nil && last != nil {
		la := &SecurityLastAudit{
			ID: last.ID, Name: last.AuditName, Score: last.Score,
			Passed: last.Passed, Failed: last.Failed, Warnings: last.Warnings,
			RiskLevel: last.RiskLevel, FinishedAt: last.FinishedAt,
		}
		for i := range last.Items {
			if strings.Contains(last.Items[i].Name, "弱密码") {
				la.WeakPassword = &last.Items[i]
				break
			}
		}
		out.LastAudit = la
	}

	out.Unauthorized = s.unauthorizedAccess(ctx)
	return out, nil
}

func (s *SecurityOverviewService) unauthorizedAccess(ctx context.Context) *SecurityUnauthorizedAccess {
	seg := &SecurityUnauthorizedAccess{Today: map[string]int64{}}
	if s.kvRepo == nil || !s.kvRepo.Available() {
		seg.Note = "recorder 未装配（无 kv 句柄）"
		return seg
	}
	raw, err := s.kvRepo.Get(ctx, model.SecurityAccessFailureKVKey)
	if err != nil {
		seg.Note = "读取失败: " + err.Error()
		return seg
	}
	if raw == "" {
		seg.Note = "尚无记录（recorder 已装配但还没有 401 落账，或进程重启后未产生新失败）"
		return seg
	}
	payload := model.ParseSecurityAccessFailurePayload(raw)
	if len(payload.Days) == 0 {
		seg.Note = "无数据"
		return seg
	}
	seg.Installed = true
	today := time.Now().Format("2006-01-02")
	if day := payload.Days[today]; day != nil {
		seg.Today = day.Kinds
	}
	seg.TopPaths = securitytelemetry.TopPaths(payload, 5)
	for _, day := range payload.Days {
		for _, n := range day.Kinds {
			seg.WeekTotal += n
		}
	}
	return seg
}
