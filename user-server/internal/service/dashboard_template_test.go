// dashboard_template_service_test.go R39-K8：大屏模板 publish（快照）/ clone（还原）。
//
// 判据挂在副作用上：PublishFromScreen 之后模板行里必须有大屏+widgets 的发布时刻
// 快照；clone 之后库里必须多一条真实 dashboard_screens 行和成套 dashboard_widgets
// 行——不是"代码里有没有那个调用"。clone 出的屏走 ops 侧服务（code 生成、默认
// theme 兜底都在那条路上），模板侧不复制任何一条业务规则。
package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	opssvc "hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func setupDashboardTemplateService(t *testing.T) (*DashboardTemplateService, *opssvc.DashboardScreenService, context.Context) {
	t.Helper()
	testutil.NewTestDB(t, &model.DashboardTemplate{}, &opsmodel.DashboardScreen{}, &opsmodel.DashboardWidget{})
	svc := NewDashboardTemplateService(
		repository.NewDashboardTemplateRepository(),
		opssvc.NewDashboardScreenService())
	return svc, opssvc.NewDashboardScreenService(), context.Background()
}

// seedScreen 建一块带两条 widget 的真实大屏，供 publish 快照。
func seedScreen(t *testing.T, screenSvc *opssvc.DashboardScreenService, ctx context.Context) *opsmodel.DashboardScreen {
	t.Helper()
	widgets := []opssvc.WidgetConfig{
		{WidgetType: "line", Title: "线索趋势", Config: map[string]any{"metric": "clues"}, X: 0, Y: 0, Width: 6, Height: 3},
		{WidgetType: "gauge", Title: "转化率", Config: map[string]any{"metric": "cvr"}, X: 6, Y: 0, Width: 3, Height: 3},
	}
	screen, err := screenSvc.CreateScreenWithWidgets(ctx, 9, &opssvc.CreateScreenRequest{
		Name:   "运营总屏",
		Layout: map[string]any{"cols": 12},
		Theme:  "light",
	}, widgets)
	if err != nil {
		t.Fatalf("建源大屏失败：%v", err)
	}
	return screen
}

func TestDashboardTemplate_PublishFromScreen_Snapshots(t *testing.T) {
	svc, screenSvc, ctx := setupDashboardTemplateService(t)
	screen := seedScreen(t, screenSvc, ctx)

	tpl, err := svc.PublishFromScreen(ctx, screen.ID, "运营总屏模板", "一键复用", 7)
	if err != nil {
		t.Fatalf("发布模板失败：%v", err)
	}
	if tpl.SourceScreenID != screen.ID {
		t.Fatalf("source_screen_id 未回填：%d", tpl.SourceScreenID)
	}
	if tpl.Theme != "light" {
		t.Fatalf("theme 快照丢失：%s", tpl.Theme)
	}
	var snap []model.DashboardTemplateWidget
	if err := json.Unmarshal([]byte(tpl.Widgets), &snap); err != nil {
		t.Fatalf("widgets 快照不是合法 JSON：%v", err)
	}
	if len(snap) != 2 {
		t.Fatalf("widgets 快照条数错误：want 2 got %d", len(snap))
	}
	if snap[0].WidgetType != "line" || snap[0].Title != "线索趋势" {
		t.Fatalf("widget 快照内容错位：%+v", snap[0])
	}
	if snap[0].SortOrder != 0 || snap[1].SortOrder != 1 {
		t.Fatalf("sort_order 未保留：%d/%d", snap[0].SortOrder, snap[1].SortOrder)
	}
}

func TestDashboardTemplate_Clone_RestoresRealScreen(t *testing.T) {
	svc, screenSvc, ctx := setupDashboardTemplateService(t)
	screen := seedScreen(t, screenSvc, ctx)

	tpl, err := svc.PublishFromScreen(ctx, screen.ID, "可克隆模板", "", 7)
	if err != nil {
		t.Fatalf("发布模板失败：%v", err)
	}

	cloned, err := svc.Clone(ctx, tpl.ID, 8, &DashboardTemplateCloneReq{Name: "从模板复制"})
	if err != nil {
		t.Fatalf("clone 失败：%v", err)
	}
	if cloned.ID == 0 || cloned.ID == screen.ID {
		t.Fatalf("clone 未产生新大屏：id=%d", cloned.ID)
	}
	if cloned.Theme != "light" {
		t.Fatalf("theme 未还原：%s", cloned.Theme)
	}
	widgetRows, err := screenSvc.GetScreenWidgetsCtx(ctx, cloned.ID)
	if err != nil {
		t.Fatalf("读 clone 屏 widgets 失败：%v", err)
	}
	if len(widgetRows) != 2 {
		t.Fatalf("clone 屏 widgets 条数错误：want 2 got %d", len(widgetRows))
	}
	if widgetRows[0].WidgetType != "line" || widgetRows[0].Title != "线索趋势" {
		t.Fatalf("widget 未按快照还原：%+v", widgetRows[0])
	}
	updated, err := svc.GetByID(ctx, tpl.ID)
	if err != nil {
		t.Fatalf("回读模板失败：%v", err)
	}
	if updated.CloneCount != 1 {
		t.Fatalf("clone_count 未回填：%d", updated.CloneCount)
	}
}

func TestDashboardTemplate_Clone_DraftRejected_AndNameRequired(t *testing.T) {
	svc, _, ctx := setupDashboardTemplateService(t)

	draft, err := svc.Create(ctx, 7, &DashboardTemplateUpsertReq{Name: "手工模板", Layout: `{"cols":12}`})
	if err != nil {
		t.Fatalf("建 draft 模板失败：%v", err)
	}
	if _, err := svc.Clone(ctx, draft.ID, 8, &DashboardTemplateCloneReq{Name: "x"}); err == nil {
		t.Fatal("draft 模板 clone 未拒绝")
	}

	// 手工模板走 Publish 转正后可 clone；缺 name 拒绝
	if err := svc.Publish(ctx, draft.ID); err != nil {
		t.Fatalf("publish 失败：%v", err)
	}
	if _, err := svc.Clone(ctx, draft.ID, 8, &DashboardTemplateCloneReq{Name: " "}); err == nil {
		t.Fatal("clone 缺 name 未拒绝")
	}
	cloned, err := svc.Clone(ctx, draft.ID, 8, &DashboardTemplateCloneReq{Name: "从手工模板复制"})
	if err != nil {
		t.Fatalf("published 模板 clone 失败：%v", err)
	}
	if cloned.Code == "" || !strings.HasPrefix(cloned.Code, "screen_") {
		t.Fatalf("clone 屏 code 未按 ops 规则生成：%q", cloned.Code)
	}
}

func TestDashboardTemplate_Rate_BoundsAndAverage(t *testing.T) {
	svc, _, ctx := setupDashboardTemplateService(t)

	tpl, err := svc.Create(ctx, 7, &DashboardTemplateUpsertReq{Name: "评分屏模板"})
	if err != nil {
		t.Fatalf("建模板失败：%v", err)
	}
	if _, err := svc.Rate(ctx, tpl.ID, 4); err != nil {
		t.Fatalf("评分失败：%v", err)
	}
	rating, err := svc.Rate(ctx, tpl.ID, 2)
	if err != nil {
		t.Fatalf("第二次评分失败：%v", err)
	}
	if rating != 3.0 {
		t.Fatalf("运行均值错误：want 3.0 got %v", rating)
	}
	if _, err := svc.Rate(ctx, tpl.ID, 0); err == nil {
		t.Fatal("越界评分未拒绝")
	}
}
