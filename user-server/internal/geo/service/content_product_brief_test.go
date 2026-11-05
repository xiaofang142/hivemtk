package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"
)

func TestProductBriefPromptBlock(t *testing.T) {
	empty := ProductBrief{}
	if empty.promptBlock() != "" || !empty.empty() {
		t.Fatal("空 brief 不应产生 prompt 片段")
	}

	b := ProductBrief{
		ProductCategory: "AI 销售与智能客服平台",
		TargetAudience:  "需要私域增长的中小企业",
		KeyFeatures:     "AI 销售引擎、RAG 知识库",
		GenerationNote:  "相比上一代增加多模型验证",
		VersionLabel:    "v2.5（2026-11 更新）",
	}
	got := b.promptBlock()
	for _, want := range []string{
		"产品类别：", "核心功能：", "适合人群：", "代际/竞品差异：", "当前版本/时效：",
		"这是什么产品", "哪些信息是最新版本",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt 片段缺少 %q:\n%s", want, got)
		}
	}
}

func TestApplyProductBriefToSchema(t *testing.T) {
	b := ProductBrief{
		ProductCategory: "BusinessApplication",
		TargetAudience:  "中小企业销售团队",
		VersionLabel:    "v2.5",
	}

	app := map[string]any{"@context": "https://schema.org", "@type": "SoftwareApplication", "name": "HiveMtk"}
	applyProductBriefToSchema(app, b)
	if app["softwareVersion"] != "v2.5" || app["applicationCategory"] != "BusinessApplication" {
		t.Fatalf("SoftwareApplication 未补版本/类别: %+v", app)
	}
	if _, ok := app["audience"]; !ok {
		t.Fatalf("SoftwareApplication 未补 audience: %+v", app)
	}
	if _, ok := app["dateModified"]; !ok {
		t.Fatalf("SoftwareApplication 未补 dateModified: %+v", app)
	}

	article := map[string]any{"@type": "Article", "dateModified": "2025-01-01"}
	applyProductBriefToSchema(article, b)
	if article["dateModified"] != "2025-01-01" {
		t.Fatalf("不应覆盖已有 dateModified: %+v", article)
	}
	if _, ok := article["audience"]; !ok {
		t.Fatalf("Article 未补 audience: %+v", article)
	}

	org := map[string]any{"@type": "Organization", "name": "HiveMtk"}
	applyProductBriefToSchema(org, b)
	if len(org) != 2 {
		t.Fatalf("Organization 不应被塞入不适用的产品字段: %+v", org)
	}
}

func TestConfigServiceUpdateProductBrief(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoConfig{})
	if db == nil {
		t.Fatal("测试库不可达：配置合并更新判不了")
	}
	repo := repository.NewGeoConfigRepositoryWithDB(db)
	svc := NewConfigService(repo, nil)
	ctx := context.Background()

	productCategory, targetAudience, keyFeatures := "AI 销售平台", "中小企业", "RAG、SOP"
	generationNote, versionLabel := "相比上一代增加多模型验证", "v2.5"
	if err := svc.UpdateConfig(ctx, ConfigUpdateInput{
		Brand: "HiveMtk", ProductCategory: &productCategory, TargetAudience: &targetAudience,
		KeyFeatures: &keyFeatures, GenerationNote: &generationNote, VersionLabel: &versionLabel,
	}); err != nil {
		t.Fatalf("更新配置失败: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("回查配置失败: %v", err)
	}
	if got.BrandName != "HiveMtk" || got.ProductCategory != "AI 销售平台" ||
		got.TargetAudience != "中小企业" || got.KeyFeatures != "RAG、SOP" ||
		got.GenerationNote != "相比上一代增加多模型验证" || got.VersionLabel != "v2.5" {
		t.Fatalf("产品叙事字段未落库: %+v", got)
	}

	// 合并更新：未提供的字段保留。
	if err := svc.UpdateConfig(ctx, ConfigUpdateInput{Brand: "HiveMtk 2"}); err != nil {
		t.Fatalf("二次更新失败: %v", err)
	}
	got, _ = repo.Get(ctx)
	if got.BrandName != "HiveMtk 2" || got.VersionLabel != "v2.5" {
		t.Fatalf("合并更新丢了字段: %+v", got)
	}
}

// 清空语义：显式传空值必须覆盖旧值，未传字段保留。
func TestConfigServiceUpdateClearsExplicitEmpty(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoConfig{})
	if db == nil {
		t.Fatal("测试库不可达：配置清空语义判不了")
	}
	repo := repository.NewGeoConfigRepositoryWithDB(db)
	svc := NewConfigService(repo, nil)
	ctx := context.Background()

	desc, adv, domain := "旧描述", "旧优势", "old.example.com"
	comps := []string{"甲", "乙"}
	negs := []string{"差评", "骗局"}
	if err := svc.UpdateConfig(ctx, ConfigUpdateInput{
		Brand: "HiveMtk", BrandDescription: &desc, Advantages: &adv,
		Domain: &domain, Competitors: &comps, NegativeKeywords: &negs,
	}); err != nil {
		t.Fatalf("首次更新失败: %v", err)
	}

	empty, emptyList := "", []string{}
	if err := svc.UpdateConfig(ctx, ConfigUpdateInput{
		BrandDescription: &empty, Domain: &empty, Competitors: &emptyList, NegativeKeywords: &emptyList,
	}); err != nil {
		t.Fatalf("清空更新失败: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.BrandDescription != "" || got.Domain != "" || got.Competitors != "" || got.NegativeKeywords != "" {
		t.Fatalf("显式空值未清空: %+v", got)
	}
	if got.BrandName != "HiveMtk" || got.Advantages != "旧优势" {
		t.Fatalf("未传字段应保留: %+v", got)
	}
}

// 默认生成模型必须真的被内容链路读取（此前只存不用）。
func TestContentServiceResolveProvider(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoConfig{})
	if db == nil {
		t.Fatal("测试库不可达：默认模型读取判不了")
	}
	repo := repository.NewGeoConfigRepositoryWithDB(db)
	cfgSvc := NewConfigService(repo, nil)
	contentSvc := NewContentService(nil, nil, nil, nil, repo, nil)
	ctx := context.Background()

	if got := contentSvc.resolveProvider(ctx, ""); got != "" {
		t.Fatalf("未配置时应返回空 provider，得到 %q", got)
	}
	if got := contentSvc.resolveProvider(ctx, " explicit-model "); got != "explicit-model" {
		t.Fatalf("显式指定应优先，得到 %q", got)
	}
	def := "deepseek"
	if err := cfgSvc.UpdateConfig(ctx, ConfigUpdateInput{Brand: "B", DefaultModel: &def}); err != nil {
		t.Fatalf("保存默认模型失败: %v", err)
	}
	if got := contentSvc.resolveProvider(ctx, ""); got != "deepseek" {
		t.Fatalf("应回落到 default_model=deepseek，得到 %q", got)
	}
}
