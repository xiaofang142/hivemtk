// kb_template_service_test.go R39-K7：知识库模板 CRUD / apply / rate。
//
// apply 的判据挂在副作用上：模板 apply 之后库里多一条真实 KnowledgeBase
// （kb_code 派生规则、apply_count 回填），而不是"代码里有没有那个调用"。
// 未发布模板拒绝 apply、评分越界拒绝，两条负路径都钉住——一键建 KB 的
// 门槛松了，模板目录会变成绕过 KB 校验的后门。
package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupKbTemplateService(t *testing.T) (*KbTemplateService, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.KbTemplate{}, &model.KnowledgeBase{})
	svc := NewKbTemplateService(
		repository.NewKbTemplateRepository(),
		NewKnowledgeBaseServiceWithRepos(
			repository.NewKnowledgeBaseRepository(db),
			repository.NewAgentKBBindingRepository(db),
			repository.NewKBItemBindingRepository(db)),
	)
	return svc, context.Background()
}

func TestKbTemplate_Apply_CreatesRealKB(t *testing.T) {
	svc, ctx := setupKbTemplateService(t)

	tpl, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{
		Name:        "电商售后 FAQ",
		Category:    "ecommerce",
		Description: "退换货/物流 FAQ 起步库",
		KBType:      "faq",
		Structure:   `{"categories":["退换货","物流"]}`,
	})
	if err != nil {
		t.Fatalf("建模板失败：%v", err)
	}

	kb, err := svc.Apply(ctx, tpl.ID, &KbTemplateApplyReq{})
	if err != nil {
		t.Fatalf("apply 失败：%v", err)
	}
	if kb.ID == 0 {
		t.Fatal("apply 后 KB ID 为 0：KB 没落库")
	}
	if kb.Type != "faq" || kb.Name != "电商售后 FAQ" {
		t.Fatalf("KB 字段未按模板派生：type=%s name=%s", kb.Type, kb.Name)
	}
	// kb_code 缺省时按模板派生，且可回查
	if !strings.HasPrefix(kb.KBCode, "kb_tpl") {
		t.Fatalf("kb_code 未按规则派生：%s", kb.KBCode)
	}
	updated, err := svc.GetByID(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("回读模板失败：%v", err)
	}
	if updated.ApplyCount != 1 {
		t.Fatalf("apply_count 未回填：%d", updated.ApplyCount)
	}
}

func TestKbTemplate_Apply_DraftRejected(t *testing.T) {
	svc, ctx := setupKbTemplateService(t)

	tpl, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{Name: "草稿模板", KBType: "rag", Status: "draft"})
	if err != nil {
		t.Fatalf("建模板失败：%v", err)
	}
	if _, err := svc.Apply(ctx, tpl.ID, &KbTemplateApplyReq{}); err == nil {
		t.Fatal("draft 模板 apply 未拒绝")
	}
}

func TestKbTemplate_Rate_RunningAverage(t *testing.T) {
	svc, ctx := setupKbTemplateService(t)

	tpl, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{Name: "评分模板", KBType: "sop"})
	if err != nil {
		t.Fatalf("建模板失败：%v", err)
	}
	if _, err := svc.Rate(ctx, tpl.ID, 5); err != nil {
		t.Fatalf("评分失败：%v", err)
	}
	rating, err := svc.Rate(ctx, tpl.ID, 3)
	if err != nil {
		t.Fatalf("第二次评分失败：%v", err)
	}
	if rating != 4.0 {
		t.Fatalf("运行均值错误：want 4.0 got %v", rating)
	}
	if _, err := svc.Rate(ctx, tpl.ID, 6); err == nil {
		t.Fatal("越界评分未拒绝")
	}
}

func TestKbTemplate_UpsertValidation(t *testing.T) {
	svc, ctx := setupKbTemplateService(t)

	if _, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{Name: " ", KBType: "faq"}); err == nil {
		t.Fatal("空 name 未拒绝")
	}
	if _, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{Name: "x", KBType: "wiki"}); err == nil {
		t.Fatal("非法 kb_type 未拒绝")
	}
	if _, err := svc.Create(ctx, 7, &KbTemplateUpsertReq{Name: "x", KBType: "faq", Status: "hidden"}); err == nil {
		t.Fatal("非法 status 未拒绝")
	}
}
