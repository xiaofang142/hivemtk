package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/ops/model"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// FA-SCREEN-01：公开大屏读取组件必须继承分享码解析出的租户，
// 客户端不能通过匿名 ctx 或请求参数绕过租户绑定；查询故障不能伪装成空白成功。
func TestFAScreen_PublicWidgetsInheritResolvedTenant(t *testing.T) {
	database := setupDashboardScreenTestDB(t)
	if database == nil {
		t.Skip("测试库不可用")
	}
	tenant.SetMode(tenant.ModeOn)
	defer tenant.SetMode(tenant.ModeOff)

	ctxA := tenant.WithTenantID(context.Background(), 101)
	ctxB := tenant.WithTenantID(context.Background(), 202)

	// 租户 A：公开屏 pub-a + 两个组件。
	screenA := &model.DashboardScreen{Name: "A屏", Code: "pub-a", IsPublic: true}
	if err := database.WithContext(ctxA).Create(screenA).Error; err != nil {
		t.Fatalf("create screenA: %v", err)
	}
	wa1 := &model.DashboardWidget{ScreenID: screenA.ID, WidgetType: "chart", Title: "A-组件1", SortOrder: 1}
	wa2 := &model.DashboardWidget{ScreenID: screenA.ID, WidgetType: "table", Title: "A-组件2", SortOrder: 2}
	if err := database.WithContext(ctxA).Create(wa1).Error; err != nil {
		t.Fatalf("create wa1: %v", err)
	}
	if err := database.WithContext(ctxA).Create(wa2).Error; err != nil {
		t.Fatalf("create wa2: %v", err)
	}

	// 租户 B：公开屏 pub-b + 一个组件。
	screenB := &model.DashboardScreen{Name: "B屏", Code: "pub-b", IsPublic: true}
	if err := database.WithContext(ctxB).Create(screenB).Error; err != nil {
		t.Fatalf("create screenB: %v", err)
	}
	wb := &model.DashboardWidget{ScreenID: screenB.ID, WidgetType: "chart", Title: "B-组件", SortOrder: 1}
	if err := database.WithContext(ctxB).Create(wb).Error; err != nil {
		t.Fatalf("create wb: %v", err)
	}

	// 私有屏（租户 A）：不应通过公开码访问。
	private := &model.DashboardScreen{Name: "私有屏", Code: "private-a", IsPublic: false}
	if err := database.WithContext(ctxA).Create(private).Error; err != nil {
		t.Fatalf("create private: %v", err)
	}

	svc := NewDashboardScreenService()

	// 匿名 ctx 访问 pub-a：用解析出的租户 A 读 screen + widgets，零跨租户串染。
	gotScreen, gotWidgets, err := svc.GetPublicScreenWithWidgetsCtx(context.Background(), "pub-a")
	if err != nil {
		t.Fatalf("GetPublicScreenWithWidgetsCtx(pub-a): %v", err)
	}
	if gotScreen.ID != screenA.ID || gotScreen.TenantID != 101 {
		t.Fatalf("screen 归属错误: id=%d tenant=%d (want id=%d tenant=101)", gotScreen.ID, gotScreen.TenantID, screenA.ID)
	}
	if len(gotWidgets) != 2 {
		t.Fatalf("widgets 数量错误: got %d want 2", len(gotWidgets))
	}
	for _, w := range gotWidgets {
		if w.TenantID != 101 || w.ScreenID != screenA.ID {
			t.Fatalf("跨租户组件串染: widget=%+v", w)
		}
	}

	// 匿名 ctx 访问 pub-b：应得租户 B 的 screen + B 的组件。
	gotScreenB, gotWidgetsB, err := svc.GetPublicScreenWithWidgetsCtx(context.Background(), "pub-b")
	if err != nil {
		t.Fatalf("GetPublicScreenWithWidgetsCtx(pub-b): %v", err)
	}
	if gotScreenB.TenantID != 202 || len(gotWidgetsB) != 1 || gotWidgetsB[0].TenantID != 202 {
		t.Fatalf("pub-b 归属错误: screen=%+v widgets=%+v", gotScreenB, gotWidgetsB)
	}

	// 未知码 / 私有码：明确错误，不返回空白成功。
	if _, _, err := svc.GetPublicScreenWithWidgetsCtx(context.Background(), "no-such-code"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("未知码应 ErrRecordNotFound，实际 %v", err)
	}
	if _, _, err := svc.GetPublicScreenWithWidgetsCtx(context.Background(), "private-a"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("私有码应 ErrRecordNotFound，实际 %v", err)
	}
}
