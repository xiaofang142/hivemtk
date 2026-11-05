package service

// 契约锁（实体审查 #16，TENANT_MODE=on）：模板市场服务改前没有任何 ctx 形参，
// 直接把无 ctx 的调用交给不带 ctx 的仓储 ⇒ saas 形态下 8 个 handler 全废；
// 更要命的是三类错误口径：
//   `DownloadTemplate` / `RateTemplate` 把仓储返回的错误一律换成
//     `errors.New("模板不存在")`，于是「[tenant] 表 market_templates 需要租户上下文但 ctx 未绑定」
//     这种配置级故障被 handler 当成业务 404 报出去（改前实测原文见 repository 层锁注释）；
//   `DownloadTemplate(userID, …)` 收了 userID 却从不使用，下载流水没有归属列，
//     「我的下载」回的是本租户全员流水；
//   别租户模板的 id 能走通下载流程吗？改前 GetByID 失败仍回非 nil 结构体，
//     下载流水照写、计数照加（0 行静默当成功）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	contentmodel "hivemtk-user/internal/content/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	mtsTenantA = uint(93)
	mtsTenantB = uint(94)
	mtsUserA   = uint(9101)
	mtsUserB   = uint(9102)
)

func mtsDB(t *testing.T) (*gorm.DB, *TemplateMarketService, context.Context, context.Context) {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	gdb := testutil.NewTestDB(t, &contentmodel.MarketTemplate{}, &contentmodel.MarketTemplateDownload{})
	if gdb == nil {
		t.Fatal("测试库不可达：租户遮挡与归属判定全都判不了（不 Skip，跳过等于没锁）")
	}
	return gdb,
		NewTemplateMarketService(NewTemplateMarketDeps(gdb)),
		tenant.WithTenantID(context.Background(), mtsTenantA),
		tenant.WithTenantID(context.Background(), mtsTenantB)
}

func mtsSeedTemplate(t *testing.T, gdb *gorm.DB, tenantID uint, name string) *contentmodel.MarketTemplate {
	t.Helper()
	row := &contentmodel.MarketTemplate{
		TenantID:   tenantID,
		Name:       name,
		Category:   "dashboard",
		Type:       "flow",
		IsOfficial: true,
	}
	if err := gdb.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("预置 %d 租户模板 %s 失败: %v", tenantID, name, err)
	}
	return row
}

func mtsPhysicalDownloads(t *testing.T, gdb *gorm.DB, templateID uint) []*contentmodel.MarketTemplateDownload {
	t.Helper()
	var rows []*contentmodel.MarketTemplateDownload
	if err := gdb.WithContext(tenant.WithSystemScope(context.Background())).
		Where("template_id = ?", templateID).Find(&rows).Error; err != nil {
		t.Fatalf("物理读取模板(%d) 下载流水失败: %v", templateID, err)
	}
	return rows
}

// TestTemplateMarketServiceReadsUseCallerContext 三条读链必须落在调用方租户上，
// 未绑定 ctx 必须原样报租户闸错。
func TestTemplateMarketServiceReadsUseCallerContext(t *testing.T) {
	gdb, svc, ctxA, ctxB := mtsDB(t)
	mtsSeedTemplate(t, gdb, mtsTenantA, "A 家模板")
	a2 := mtsSeedTemplate(t, gdb, mtsTenantA, "A 家二号")
	b1 := mtsSeedTemplate(t, gdb, mtsTenantB, "B 家模板")

	list, total, err := svc.GetTemplateList(ctxA, "", "", 1, 10)
	if err != nil {
		t.Fatalf("A 租户列表读取失败: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Errorf("A 租户只应看到自己的 2 条，实得 len=%d total=%d", len(list), total)
	}

	official, cnt, err := svc.GetOfficialTemplates(ctxB, 1, 10)
	if err != nil {
		t.Fatalf("B 租户官方模板读取失败: %v", err)
	}
	if cnt != 1 || len(official) != 1 || official[0].ID != b1.ID {
		t.Errorf("B 租户官方模板只应是自己那条，实得 len=%d total=%d", len(official), cnt)
	}

	hits, _, err := svc.SearchTemplates(ctxA, "A 家", 1, 10)
	if err != nil {
		t.Fatalf("A 租户搜索失败: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("A 租户搜「A 家」应命中自己 2 条，实得 len=%d", len(hits))
	}

	got, err := svc.GetTemplateByID(ctxA, b1.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) || got != nil {
		t.Errorf("A 读 B 家模板应报 ErrRecordNotFound 且不回结构体，实得 got=%+v err=%v", got, err)
	}
	if got, err = svc.GetTemplateByID(ctxA, a2.ID); err != nil || got == nil || got.Name != "A 家二号" {
		t.Errorf("A 读本租户模板失败: got=%+v err=%v", got, err)
	}

	_, _, err = svc.GetTemplateList(context.Background(), "", "", 1, 10)
	if err == nil || !strings.Contains(err.Error(), "需要租户上下文") {
		t.Errorf("未绑定 ctx 的列表读取应报租户闸错，实得 %v", err)
	}
}

// TestTemplateMarketServiceDownloadRecordsOwner 下载流水必须带归属，且计数只加本租户行。
func TestTemplateMarketServiceDownloadRecordsOwner(t *testing.T) {
	gdb, svc, ctxA, ctxB := mtsDB(t)
	a1 := mtsSeedTemplate(t, gdb, mtsTenantA, "可下载模板")
	b1 := mtsSeedTemplate(t, gdb, mtsTenantB, "别家模板")

	tmpl, err := svc.DownloadTemplate(ctxA, mtsUserA, a1.ID)
	if err != nil {
		t.Fatalf("A 用户下载本租户模板失败: %v", err)
	}
	if tmpl.ID != a1.ID {
		t.Errorf("下载回显的模板不对，实得 %d", tmpl.ID)
	}
	rows := mtsPhysicalDownloads(t, gdb, a1.ID)
	if len(rows) != 1 || rows[0].UserID != mtsUserA || rows[0].TenantID != mtsTenantA {
		t.Errorf("下载流水应恰好 1 条、归属 A 用户与 A 租户，实得 %+v", rows)
	}

	// 「我的下载」按用户裁剪
	mine, total, err := svc.GetMyDownloads(ctxA, mtsUserA, 1, 10)
	if err != nil {
		t.Fatalf("A 用户读自己的下载流水失败: %v", err)
	}
	if total != 1 || len(mine) != 1 || mine[0].TemplateID != a1.ID {
		t.Errorf("A 用户只应看到自己那 1 条，实得 len=%d total=%d", len(mine), total)
	}
	if _, cnt, err := svc.GetMyDownloads(ctxA, mtsUserB, 1, 10); err != nil || cnt != 0 {
		t.Errorf("同租户他人不该看到 A 的流水，实得 total=%d err=%v", cnt, err)
	}
	if _, cnt, err := svc.GetMyDownloads(ctxB, mtsUserA, 1, 10); err != nil || cnt != 0 {
		t.Errorf("别租户不该看到 A 用户的流水，实得 total=%d err=%v", cnt, err)
	}

	// 下载别租户模板：必须失败，且不给别租户的模板留下流水或计数
	if _, err := svc.DownloadTemplate(ctxA, mtsUserA, b1.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("下载别租户模板应报 ErrRecordNotFound，实得 %v", err)
	}
	if rows := mtsPhysicalDownloads(t, gdb, b1.ID); len(rows) != 0 {
		t.Errorf("别租户模板被写进了下载流水，实得 %+v", rows)
	}

	// 未绑定 ctx 不得伪装成「模板不存在」
	if _, err := svc.DownloadTemplate(context.Background(), mtsUserA, a1.ID); err == nil ||
		!strings.Contains(err.Error(), "需要租户上下文") {
		t.Errorf("未绑定 ctx 的下载应报租户闸错，实得 %v", err)
	}
}

// TestTemplateMarketServiceDoesNotHideGateAsNotFound 建模板/评分两口的错误口径。
func TestTemplateMarketServiceDoesNotHideGateAsNotFound(t *testing.T) {
	gdb, svc, ctxA, _ := mtsDB(t)
	a1 := mtsSeedTemplate(t, gdb, mtsTenantA, "待评分模板")

	if err := svc.CreateTemplate(context.Background(), &contentmodel.MarketTemplate{Name: "闸外模板", Type: "flow"}); err == nil ||
		!strings.Contains(err.Error(), "需要租户上下文") {
		t.Errorf("未绑定 ctx 建模板应报租户闸错，实得 %v", err)
	}
	if err := svc.CreateTemplate(ctxA, &contentmodel.MarketTemplate{Name: "", Type: "flow"}); err == nil ||
		err.Error() != "模板名称不能为空" {
		t.Errorf("空名建模板应被服务层校验挡下，实得 %v", err)
	}

	if err := svc.RateTemplate(ctxA, a1.ID, 6); err == nil || err.Error() != "评分范围 0-5" {
		t.Errorf("越界评分应被挡下，实得 %v", err)
	}
	if err := svc.RateTemplate(ctxA, a1.ID, 4); err != nil {
		t.Errorf("A 租户给自己模板评分失败: %v", err)
	}
	err := svc.RateTemplate(context.Background(), a1.ID, 5)
	if err == nil || !strings.Contains(err.Error(), "需要租户上下文") {
		t.Errorf("未绑定 ctx 的评分应原样报租户闸错，不得折叠成「模板不存在」，实得 %v", err)
	}
}
