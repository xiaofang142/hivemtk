package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/securitytelemetry"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// C-5 总览聚合：四路数据源各喂一点，断言各段落数对得上；单路缺席不拖垮整块。
func TestSecurityOverviewAggregates(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.LoginEvent{},
		&model.SecurityAlert{},
		&model.SecurityAudit{},
		&model.SecurityAuditItem{},
		&model.SystemUser{},
		&model.PlatformConfigKV{},
	)
	db.SetTestDB(database)

	now := time.Now()
	if err := database.Create(&model.LoginEvent{UserID: 1, Username: "u1", IP: "1.2.3.4", LoginAt: now, RiskLevel: model.RiskLevelLow}).Error; err != nil {
		t.Fatalf("seed login: %v", err)
	}
	if err := database.Create(&model.LoginEvent{UserID: 2, Username: "u2", IP: "2.2.2.2", LoginAt: now, RiskLevel: model.RiskLevelCritical, Reason: "暴力尝试"}).Error; err != nil {
		t.Fatalf("seed high-risk login: %v", err)
	}
	if err := database.Create(&model.SecurityAlert{AlertType: "brute_force", RiskLevel: model.RiskLevelHigh, Status: model.SecurityAlertStatusOpen, Title: "高危登录"}).Error; err != nil {
		t.Fatalf("seed alert: %v", err)
	}

	// 最近一次审计（带弱密码项）。
	audit := &model.SecurityAudit{AuditName: "周检", Score: 80, TotalChecks: 8, Passed: 7, Failed: 1, RiskLevel: "medium", Status: "done", StartedAt: now}
	if err := database.Create(audit).Error; err != nil {
		t.Fatalf("seed audit: %v", err)
	}
	if err := database.Create(&model.SecurityAuditItem{AuditID: audit.ID, Name: "常见弱密码抽样探测", Level: "high", Result: "pass", Message: "抽样 10 用户未发现"}).Error; err != nil {
		t.Fatalf("seed audit item: %v", err)
	}

	// 401 遥测 kv 行。
	payload := model.NewSecurityAccessFailurePayload()
	securitytelemetry.Add(payload, now.Format("2006-01-02"), model.SecurityAccessKindTokenInvalid, "")
	securitytelemetry.Add(payload, now.Format("2006-01-02"), model.SecurityAccessKindTokenMissing, "/api/system")
	kvRaw, _ := json.Marshal(payload)
	kvRepo := repository.NewPlatformConfigKVRepositoryWithDB(database)
	if _, err := kvRepo.Upsert(context.Background(), model.SecurityAccessFailureKVKey, string(kvRaw)); err != nil {
		t.Fatalf("seed kv: %v", err)
	}

	auditRepo := repository.NewSecurityAuditRepository()
	auditRepo.SetDB(context.Background(), database)
	svc := NewSecurityOverviewService(auditRepo, kvRepo)
	out, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}

	if out.LoginEvents24h != 2 || out.HighRiskLoginEvents24h != 1 {
		t.Fatalf("登录事件计数错: total=%d high=%d，期望 2/1", out.LoginEvents24h, out.HighRiskLoginEvents24h)
	}
	if out.OpenAlertsTotal != 1 || out.OpenAlerts[string(model.RiskLevelHigh)] != 1 {
		t.Fatalf("open 告警分布错: %v", out.OpenAlerts)
	}
	if out.LastAudit == nil || out.LastAudit.ID != audit.ID {
		t.Fatalf("应取到最近审计: %+v", out.LastAudit)
	}
	if out.LastAudit.WeakPassword == nil || out.LastAudit.WeakPassword.Result != "pass" {
		t.Fatalf("弱密码项应读自最近审计: %+v", out.LastAudit.WeakPassword)
	}
	ua := out.Unauthorized
	if !ua.Installed || ua.Today[model.SecurityAccessKindTokenInvalid] != 1 || ua.Today[model.SecurityAccessKindTokenMissing] != 1 {
		t.Fatalf("401 今日计数错: %+v", ua)
	}
	if ua.WeekTotal != 2 {
		t.Fatalf("周总数应 2，实际 %d", ua.WeekTotal)
	}
	if len(ua.TopPaths) == 0 || ua.TopPaths[0] != "/api/system" {
		t.Fatalf("TopPaths 应含 /api/system: %v", ua.TopPaths)
	}
}

// C-5 弱密码抽样探测：种一个用 "123456" 的活跃用户 ⇒ 审计检查必须点名 fail。
// 明文创建是有意的：SystemUser.BeforeCreate 会对非空 Password 再哈希一次，
// 预置哈希会被双重哈希成探测永远打不中的串——生产写路径传的也是明文。
func TestSecurityAuditWeakPasswordProbe(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SystemUser{})
	if err := database.Create(&model.SystemUser{Username: "weakling", Password: "123456", Status: 1, Email: "w@example.com"}).Error; err != nil {
		t.Fatalf("seed weak user: %v", err)
	}
	if err := database.Create(&model.SystemUser{Username: "strongman", Password: "Str0ng!Passphrase#2026", Status: 1, Email: "s@example.com"}).Error; err != nil {
		t.Fatalf("seed strong user: %v", err)
	}

	repo := repository.NewSecurityAuditRepository()
	repo.SetDB(context.Background(), database)
	svc := NewSecurityAuditService(repo)

	result, msg := svc.auditWeakPasswordSample(context.Background())
	if result != "fail" {
		t.Fatalf("123456 用户必须被点名（result=%s msg=%s）", result, msg)
	}
	if !contains(msg, "weakling") || contains(msg, "strongman") {
		t.Fatalf("应点名 weakling 且不冤枉 strongman: %s", msg)
	}
}
