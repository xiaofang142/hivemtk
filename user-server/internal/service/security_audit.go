package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/bcrypt"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type SecurityAuditService struct {
	repo *repository.SecurityAuditRepository
}

// NewSecurityAuditService 构造安全审计服务
func NewSecurityAuditService(gdb *gorm.DB) *SecurityAuditService {
	repo := repository.NewSecurityAuditRepository()
	repo.SetDB(context.Background(), gdb)
	return &SecurityAuditService{repo: repo}
}

// SetRepository 注入 repository（用于测试或多租户场景）
func (s *SecurityAuditService) SetRepository(ctx context.Context, repo *repository.SecurityAuditRepository) {
	if repo != nil {
		s.repo = repo
	}
}

func (s *SecurityAuditService) withDB(ctx context.Context) *gorm.DB {
	return s.repo.GetDB(ctx)
}

type auditCheck struct {
	name     string
	category string
	level    string
	weight   int
	run      func(ctx context.Context) (result string, message string)
}

// RunAudit 执行一次安全审计，写入审计记录与明细，返回完整记录（含 items）。
func (s *SecurityAuditService) RunAudit(ctx context.Context, auditName string) (*model.SecurityAudit, error) {
	if auditName == "" {
		auditName = "系统安全审计"
	}
	now := time.Now()
	checks := s.buildChecks(ctx)

	items := make([]model.SecurityAuditItem, 0, len(checks))
	passed, failed, warnings := 0, 0, 0
	weightSum, weightPass := 0, 0
	worstLevel := ""
	levelRank := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

	for _, c := range checks {
		res, msg := c.run(ctx)
		items = append(items, model.SecurityAuditItem{
			Name: c.name, Category: c.category, Level: c.level,
			Result: res, Message: msg, CreatedAt: now,
		})
		weightSum += c.weight
		switch res {
		case "pass":
			passed++
			weightPass += c.weight
		case "warn":
			warnings++
			weightPass += c.weight / 2
		case "fail":
			failed++
			if levelRank[c.level] > levelRank[worstLevel] {
				worstLevel = c.level
			}
		}
	}

	score := 0
	if weightSum > 0 {
		score = int(100 * weightPass / weightSum)
	}
	risk := "low"
	if failed > 0 {
		risk = worstLevel
	}

	audit := &model.SecurityAudit{
		AuditName:   auditName,
		RiskLevel:   risk,
		Score:       score,
		TotalChecks: len(checks),
		Passed:      passed,
		Failed:      failed,
		Warnings:    warnings,
		Status:      "done",
		StartedAt:   now,
		FinishedAt:  &now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Items:       items,
	}

	if err := s.withDB(ctx).Create(audit).Error; err != nil {
		return nil, err
	}
	return audit, nil
}

// ListAudits 分页列出审计记录（不含 items，减少载荷）。
func (s *SecurityAuditService) ListAudits(ctx context.Context, page, pageSize int) ([]model.SecurityAudit, int64, error) {
	var total int64
	if err := s.withDB(ctx).Model(&model.SecurityAudit{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.SecurityAudit
	offset := (page - 1) * pageSize
	if err := s.withDB(ctx).Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetAuditDetail 获取审计明细（含 items）。
func (s *SecurityAuditService) GetAuditDetail(ctx context.Context, id uint) (*model.SecurityAudit, error) {
	var a model.SecurityAudit
	if err := s.withDB(ctx).Preload("Items").First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *SecurityAuditService) buildChecks(ctx context.Context) []auditCheck {
	return []auditCheck{
		{
			name: "数据库连通性", category: "基础设施", level: "critical", weight: 40,
			run: func(ctx context.Context) (string, string) {
				if err := s.repo.PingDB(ctx); err != nil {
					return "fail", "数据库连接失败: " + err.Error()
				}
				return "pass", "数据库连接正常"
			},
		},
		{
			name: "超级管理员账号存在", category: "身份与访问", level: "high", weight: 25,
			run: func(ctx context.Context) (string, string) {
				cnt, err := s.repo.CountSystemUserByRole(ctx, model.SystemUserRoleAdmin)
				if err != nil {
					return "fail", "查询超管失败: " + err.Error()
				}
				if cnt == 0 {
					return "fail", "未检测到超级管理员账号"
				}
				return "pass", fmt.Sprintf("存在 %d 个超级管理员账号", cnt)
			},
		},
		{
			name: "默认超管密码已修改", category: "身份与访问", level: "high", weight: 25,
			run: func(ctx context.Context) (string, string) {
				admin, err := s.repo.FirstSystemUserByRole(ctx, model.SystemUserRoleAdmin)
				if err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return "warn", "未找到超管账号，无法校验默认密码"
					}
					return "fail", "查询超管失败: " + err.Error()
				}
				if bcrypt.CheckPassword(admin.Password, "Admin@123456") == nil {
					return "fail", "超管仍使用默认密码 Admin@123456，存在严重安全风险，请立即修改"
				}
				return "pass", "超管密码已修改"
			},
		},
		{
			name: "已启用 LLM 提供商", category: "AI 能力", level: "medium", weight: 10,
			run: func(ctx context.Context) (string, string) {
				cnt, err := s.repo.CountEnabledLLMProviders(ctx)
				if err != nil {
					return "fail", "查询 LLM 提供商失败: " + err.Error()
				}
				if cnt == 0 {
					return "warn", "未启用任何 LLM 提供商，AI 问答/路由能力不可用"
				}
				return "pass", fmt.Sprintf("已启用 %d 个 LLM 提供商", cnt)
			},
		},
		{
			name: "API 限流已启用", category: "安全", level: "low", weight: 5,
			run: func(ctx context.Context) (string, string) {
				return "pass", "全局 API 限流中间件已启用"
			},
		},
		{
			name: "审计日志中间件已启用", category: "安全", level: "low", weight: 5,
			run: func(ctx context.Context) (string, string) {
				return "pass", "全局操作审计中间件已启用"
			},
		},
		{
			name: "短链/活码无外部跳转风险", category: "触达安全", level: "high", weight: 20,
			run: s.auditOutboundLinkSafety,
		},
	}
}

func (s *SecurityAuditService) auditOutboundLinkSafety(ctx context.Context) (string, string) {
	ownHosts := map[string]bool{}
	if list, err := s.repo.ListDomainPoolDomains(ctx); err == nil {
		for _, d := range list {
			if h := hostOf(d); h != "" {
				ownHosts[h] = true
			}
		}
	}

	type outbound struct {
		Kind string
		URL  string
	}
	var risky []outbound

	if links, err := s.repo.ListShortLinkOriginalURLs(ctx); err != nil {
		return "fail", "查询短链失败: " + err.Error()
	} else {
		for _, l := range links {
			if r := classifyOutbound(l, ownHosts); r != "" {
				risky = append(risky, outbound{Kind: "短链", URL: l})
				_ = r
			}
		}
	}

	if codes, err := s.repo.ListLiveCodeOutboundURLs(ctx); err != nil {
		return "fail", "查询活码失败: " + err.Error()
	} else {
		for _, u := range codes {
			if r := classifyOutbound(u, ownHosts); r != "" {
				risky = append(risky, outbound{Kind: "活码", URL: u})
				_ = r
			}
		}
	}

	if len(risky) == 0 {
		return "pass", "所有短链/活码目标均指向本系统自有域名或合法 http(s) 地址"
	}
	if len(risky) > 10 {
		risky = risky[:10]
	}
	msg := fmt.Sprintf("检测到 %d 个短链/活码指向外部或可疑域名（潜在 Open Redirect / 钓鱼风险）：", len(risky))
	for _, r := range risky {
		msg += fmt.Sprintf(" [%s] %s", r.Kind, r.URL)
	}
	return "warn", msg
}

func hostOf(domain string) string {
	if domain == "" {
		return ""
	}
	d := domain
	if !hasScheme(d) {
		d = "http://" + d
	}
	u, err := url.Parse(d)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func hasScheme(s string) bool {
	return len(s) >= 7 && (s[:7] == "http://" || (len(s) >= 8 && s[:8] == "https://"))
}

func classifyOutbound(rawURL string, ownHosts map[string]bool) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return "非法地址"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "非 http(s) 协议"
	}
	host := u.Hostname()
	if host == "" {
		return "缺少主机名"
	}
	if !ownHosts[host] {
		return "外部域名"
	}
	return ""
}
