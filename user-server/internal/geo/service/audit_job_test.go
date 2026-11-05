package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestSiteAuditJob_PersistsAndAlertsOnDrop 月度复查：
//  1. 第一次给启用站点留一条审计历史；
//  2. 站点退化后第二次分数下降 >8，写 audit_score_drop 告警。
func TestSiteAuditJob_PersistsAndAlertsOnDrop(t *testing.T) {
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.ResetRegistryForTest(); tenant.SetMode(tenant.ModeOff) })

	database := testutil.NewTestDB(t,
		&model.GeoSite{}, &model.GeoConfig{}, &model.GeoAuditRun{}, &model.GeoAuditItem{}, &model.GeoAlert{})
	if database == nil {
		t.Fatal("测试库不可达：月度复查任务判不了")
	}
	db.SetTestDB(database)

	var bad atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if bad.Load() {
			fmt.Fprint(w, `<html><body><h1>临时维护</h1><p>页面正在调整。</p></body></html>`)
			return
		}
		fmt.Fprint(w, `<!DOCTYPE html><html lang="zh-CN"><head>
<title>HiveMtk 官网</title>
<meta name="description" content="HiveMtk 是面向企业的 AI 销售与智能客服平台，适合需要私域增长的中小团队。">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Organization","name":"HiveMtk"}</script>
</head><body><h1>HiveMtk</h1><p>我们的客户案例覆盖 13 个渠道，增长 200%。</p>
<ul><li>AI 销售</li><li>私域 SCRM</li></ul><p><strong>Q: 适合谁？</strong></p><p>A: 中小团队。</p>
<p><a href="https://example.com/docs">文档</a> <a href="/pricing">价格</a></p></body></html>`)
	})
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "User-agent: *\nAllow: /\nUser-agent: OAI-SearchBot\nAllow: /\nSitemap: https://example.com/sitemap.xml\n")
	})
	mux.HandleFunc("/llms.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "# HiveMtk\n\n> AI 销售与智能客服平台\n\n## 产品\n- [GEO](https://example.com/geo)\n\n## 文档\n- [指南](https://example.com/docs)\n\n## 联系\n- hello@example.com")
	})
	site := httptest.NewServer(mux)
	defer site.Close()

	cfg := &model.GeoConfig{BrandName: "HiveMtk", BrandDescription: "AI 销售与智能客服平台"}
	if err := database.Create(cfg).Error; err != nil {
		t.Fatalf("写品牌配置失败: %v", err)
	}
	geoSite := &model.GeoSite{Domain: site.URL, Provider: "cloudflare", Active: true}
	if err := database.Create(geoSite).Error; err != nil {
		t.Fatalf("写站点失败: %v", err)
	}

	summary, err := siteAuditJob(context.Background())
	if err != nil {
		t.Fatalf("第一次月度复查失败: %v", err)
	}
	if summary == "" {
		t.Fatal("第一次月度复查应返回摘要")
	}
	repo := repository.NewGeoAuditRepositoryWithDB(database)
	runs, total, err := repo.List(context.Background(), repository.AuditRunQuery{Kind: model.GeoAuditKindSite})
	if err != nil || total != 1 {
		t.Fatalf("第一次应落 1 条审计: total=%d err=%v", total, err)
	}
	firstScore := runs[0].Score
	if firstScore <= 0 {
		t.Fatalf("第一次审计分数异常: %d", firstScore)
	}

	bad.Store(true)
	if _, err := siteAuditJob(context.Background()); err != nil {
		t.Fatalf("第二次月度复查失败: %v", err)
	}
	runs, total, err = repo.List(context.Background(), repository.AuditRunQuery{Kind: model.GeoAuditKindSite, URL: site.URL})
	if err != nil || total != 2 {
		t.Fatalf("第二次后应有 2 条审计: total=%d err=%v", total, err)
	}
	// List 按 created_at DESC：runs[0] 是第二次，runs[1] 是第一次。
	secondScore := runs[0].Score
	if secondScore >= firstScore-8 {
		t.Fatalf("夹具退化后分数应至少下降 8 分: first=%d second=%d", firstScore, secondScore)
	}

	var alerts []*model.GeoAlert
	if err := database.Where("type = ?", "audit_score_drop").Find(&alerts).Error; err != nil {
		t.Fatalf("查告警失败: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("应写 1 条分数下降告警，实际 %d", len(alerts))
	}
	if alerts[0].Query != site.URL || alerts[0].Engine != "geo-audit" {
		t.Fatalf("告警字段错误: %+v", alerts[0])
	}
}
