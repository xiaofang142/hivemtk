package service

// custom_report_use_template_auth_test.go FA-24：复制模板入口与读取同权。
//
// 钉住的契约：私有模板对无权者不可复制（且零新行、错误不泄露存在性差异——
// 存在但无权与不存在都回报各自既定错误）；公开/本人/管理员按契约成功；
// 复制产物属于复制人且默认私有。使用真实测试库（仓储为具体类型，不可假化）。

import (
	"context"
	"errors"
	"testing"

	opsmodel "hivemtk-user/internal/ops/model"
	opsrepo "hivemtk-user/internal/ops/repository"
	"hivemtk-user/internal/pkg/testutil"
)

func setupUseTemplateAuthService(t *testing.T) *CustomReportService {
	t.Helper()
	db := testutil.NewTestDB(t, &opsmodel.CustomReport{})
	return NewCustomReportServiceWithRepos(
		opsrepo.NewCustomReportRepositoryWithDB(db),
		nil, nil, nil,
	)
}

func TestUseTemplate_CopyFollowsReadAuthorization(t *testing.T) {
	svc := setupUseTemplateAuthService(t)
	ctx := context.Background()

	private := &opsmodel.CustomReport{Name: "A-私有", DataSource: "sessions", ChartType: "bar", IsPublic: false, CreatedBy: 101}
	public := &opsmodel.CustomReport{Name: "A-公开", DataSource: "sessions", ChartType: "bar", IsPublic: true, CreatedBy: 101}
	for _, tpl := range []*opsmodel.CustomReport{private, public} {
		if err := svc.reportRepo.Create(ctx, tpl); err != nil {
			t.Fatalf("预置模板失败：%v", err)
		}
	}

	// 普通 B：他人私有模板拒绝，且零新行。
	_, totalBefore, _ := svc.reportRepo.GetAll(ctx, 1, 100)

	var err error
	_, err = svc.UseTemplate(ctx, private.ID, 202, false)
	if !errors.Is(err, ErrReportForbidden) {
		t.Fatalf("普通用户复制他人私有模板必须拒绝，实际 %v", err)
	}
	_, totalAfter, err := svc.reportRepo.GetAll(ctx, 1, 100)
	if err != nil {
		t.Fatalf("复读行数失败：%v", err)
	}
	if totalAfter != totalBefore {
		t.Errorf("被拒绝的复制不得留下新行：before=%d after=%d", totalBefore, totalAfter)
	}

	// 普通 B：公开模板按契约成功，产物归 B 且私有。
	copied, err := svc.UseTemplate(ctx, public.ID, 202, false)
	if err != nil {
		t.Fatalf("公开模板应可复制：%v", err)
	}
	if copied.CreatedBy != 202 || copied.IsPublic {
		t.Errorf("复制产物应归复制人且默认私有：%+v", copied)
	}

	// 本人：自己的私有模板可复制。
	if _, err := svc.UseTemplate(ctx, private.ID, 101, false); err != nil {
		t.Fatalf("本人复制自己的私有模板应成功：%v", err)
	}

	// 管理员：私有模板可复制。
	if _, err := svc.UseTemplate(ctx, private.ID, 203, true); err != nil {
		t.Fatalf("管理员复制私有模板应成功：%v", err)
	}

	// 不存在：NotFound。
	if _, err := svc.UseTemplate(ctx, 99999, 202, false); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("不存在模板应返回 NotFound，实际 %v", err)
	}
}
