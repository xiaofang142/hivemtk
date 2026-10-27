// dashboard_template_service.go 数据大屏模板服务（R39-K8）。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/model"
	opsmodel "hivemtk-user/internal/ops/model"
	opssvc "hivemtk-user/internal/ops/service"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// DashboardTemplateService 模板 CRUD + publish（从大屏发快照）+ clone（一键复制成真实大屏）+ rate。
// 建屏/落 widgets 走 ops 侧 DashboardScreenService，本服务不直写 dashboard_screens/dashboard_widgets。
type DashboardTemplateService struct {
	repo      *repository.DashboardTemplateRepository
	screenSvc *opssvc.DashboardScreenService
}

func NewDashboardTemplateService(repo *repository.DashboardTemplateRepository, screenSvc *opssvc.DashboardScreenService) *DashboardTemplateService {
	return &DashboardTemplateService{repo: repo, screenSvc: screenSvc}
}

func (s *DashboardTemplateService) List(ctx context.Context, status string) ([]*model.DashboardTemplate, error) {
	return s.repo.List(ctx, status)
}

func (s *DashboardTemplateService) GetByID(ctx context.Context, id uint) (*model.DashboardTemplate, error) {
	return s.repo.GetByID(ctx, id)
}

type DashboardTemplateUpsertReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Layout      string `json:"layout"`
	Theme       string `json:"theme"`
	Widgets     string `json:"widgets"`
}

func (s *DashboardTemplateService) Create(ctx context.Context, createdBy uint, req *DashboardTemplateUpsertReq) (*model.DashboardTemplate, error) {
	tpl, err := s.normalize(req)
	if err != nil {
		return nil, err
	}
	tpl.Status = model.DashboardTemplateStatusDraft
	tpl.CreatedBy = createdBy
	if err := s.repo.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *DashboardTemplateService) Update(ctx context.Context, id uint, req *DashboardTemplateUpsertReq) (*model.DashboardTemplate, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	tpl, err := s.normalize(req)
	if err != nil {
		return nil, err
	}
	cols := map[string]any{
		"name":        tpl.Name,
		"description": tpl.Description,
		"layout":      tpl.Layout,
		"theme":       tpl.Theme,
		"widgets":     tpl.Widgets,
	}
	if err := s.repo.Update(ctx, id, cols); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *DashboardTemplateService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func (s *DashboardTemplateService) normalize(req *DashboardTemplateUpsertReq) (*model.DashboardTemplate, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: 请求体为空", utils.ErrInvalidInput)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name 必填", utils.ErrInvalidInput)
	}
	if ws := strings.TrimSpace(req.Widgets); ws != "" {
		var probe []model.DashboardTemplateWidget
		if err := json.Unmarshal([]byte(ws), &probe); err != nil {
			return nil, fmt.Errorf("%w: widgets 不是合法的 widget 数组 JSON: %v", utils.ErrInvalidInput, err)
		}
	}
	theme := strings.TrimSpace(req.Theme)
	if theme == "" {
		theme = "dark"
	}
	return &model.DashboardTemplate{
		Name:        name,
		Description: req.Description,
		Layout:      req.Layout,
		Theme:       theme,
		Widgets:     req.Widgets,
	}, nil
}

// PublishFromScreen 从既有大屏发布模板：抓屏面 + widgets 的发布时刻快照落模板行。
func (s *DashboardTemplateService) PublishFromScreen(ctx context.Context, screenID uint, name, description string, createdBy uint) (*model.DashboardTemplate, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name 必填", utils.ErrInvalidInput)
	}
	screen, err := s.screenSvc.GetScreenByID(ctx, screenID)
	if err != nil {
		return nil, err
	}
	widgetRows, err := s.screenSvc.GetScreenWidgetsCtx(ctx, screen.ID)
	if err != nil {
		return nil, err
	}
	tpl := &model.DashboardTemplate{
		Name:           name,
		Description:    description,
		Layout:         screen.Layout,
		Theme:          screen.Theme,
		Widgets:        buildWidgetSnapshotJSON(widgetRows),
		Status:         model.DashboardTemplateStatusPublished,
		SourceScreenID: screen.ID,
		CreatedBy:      createdBy,
	}
	if tpl.Theme == "" {
		tpl.Theme = "dark"
	}
	if err := s.repo.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

// buildWidgetSnapshotJSON 从 GORM widget 行组快照 JSON；Config 是逐 widget 的
// 自由 JSON（map），原样透传不改内容。
func buildWidgetSnapshotJSON(rows []*opsmodel.DashboardWidget) string {
	out := make([]map[string]any, 0, len(rows))
	for _, w := range rows {
		var cfg map[string]any
		_ = json.Unmarshal([]byte(w.Config), &cfg)
		out = append(out, map[string]any{
			"widget_type": w.WidgetType,
			"title":       w.Title,
			"config":      cfg,
			"data_source": w.DataSource,
			"x":           w.X,
			"y":           w.Y,
			"width":       w.Width,
			"height":      w.Height,
			"sort_order":  w.SortOrder,
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Publish 把手工创建的 draft 模板转正（快照类模板发布即已 published）。
func (s *DashboardTemplateService) Publish(ctx context.Context, id uint) error {
	return s.repo.Update(ctx, id, map[string]any{"status": model.DashboardTemplateStatusPublished})
}

// DashboardTemplateCloneReq clone 请求：name 必填（新大屏名），is_public 缺省 false。
type DashboardTemplateCloneReq struct {
	Name     string `json:"name"`
	IsPublic bool   `json:"is_public"`
}

// Clone 一键复制：模板快照 → 真实大屏（dashboard_screens 行 + dashboard_widgets 行）。
func (s *DashboardTemplateService) Clone(ctx context.Context, templateID uint, createdBy uint, req *DashboardTemplateCloneReq) (*opsmodel.DashboardScreen, error) {
	if req == nil {
		req = &DashboardTemplateCloneReq{}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name 必填（新大屏名）", utils.ErrInvalidInput)
	}
	tpl, err := s.repo.GetByID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if tpl.Status != model.DashboardTemplateStatusPublished {
		return nil, fmt.Errorf("%w: 模板未发布，不能 clone", utils.ErrInvalidInput)
	}

	var layout map[string]any
	if strings.TrimSpace(tpl.Layout) != "" {
		if err := json.Unmarshal([]byte(tpl.Layout), &layout); err != nil {
			return nil, fmt.Errorf("%w: 模板 layout 快照损坏: %v", utils.ErrInvalidInput, err)
		}
	}
	var widgets []opssvc.WidgetConfig
	if strings.TrimSpace(tpl.Widgets) != "" {
		var snap []model.DashboardTemplateWidget
		if err := json.Unmarshal([]byte(tpl.Widgets), &snap); err != nil {
			return nil, fmt.Errorf("%w: 模板 widgets 快照损坏: %v", utils.ErrInvalidInput, err)
		}
		for _, w := range snap {
			widgets = append(widgets, opssvc.WidgetConfig{
				WidgetType: w.WidgetType,
				Title:      w.Title,
				Config:     w.Config,
				DataSource: w.DataSource,
				X:          w.X,
				Y:          w.Y,
				Width:      w.Width,
				Height:     w.Height,
			})
		}
	}
	screen, err := s.screenSvc.CreateScreenWithWidgets(ctx, createdBy, &opssvc.CreateScreenRequest{
		Name:     name,
		Layout:   layout,
		Theme:    tpl.Theme,
		IsPublic: req.IsPublic,
	}, widgets)
	if err != nil {
		return nil, err
	}
	// 计数是运营遥测：屏已建成，计数失败不推翻主效果（与 kb 模板 apply 同一口径）。
	_ = s.repo.IncrementCloneCount(ctx, templateID)
	return screen, nil
}

// Rate 评分（1-5），运行均值落库并返回新均值。
func (s *DashboardTemplateService) Rate(ctx context.Context, templateID uint, score float64) (float64, error) {
	if score < 1 || score > 5 {
		return 0, fmt.Errorf("%w: 评分范围 1-5", utils.ErrInvalidInput)
	}
	return s.repo.UpdateRating(ctx, templateID, score)
}
