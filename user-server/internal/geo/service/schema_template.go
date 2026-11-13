package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"

	"gorm.io/datatypes"
)

// SchemaTemplateInput Schema 模板的可编辑字段。
//
// 指针区分「未传」与「传了零值」（Update 只覆盖传了的字段）；
// id/tenant_id/timestamps 一律不接受客户端输入。
type SchemaTemplateInput struct {
	PageType     *string
	SchemaType   *string
	TemplateJSON json.RawMessage
	Active       *bool
}

// SchemaTemplateService Schema 模板库读写编排
type SchemaTemplateService struct {
	repo repository.GeoSchemaTemplateRepository
}

func NewSchemaTemplateService(repo repository.GeoSchemaTemplateRepository) *SchemaTemplateService {
	return &SchemaTemplateService{repo: repo}
}

// List 分页查询；limit 上限 200。
func (s *SchemaTemplateService) List(ctx context.Context, page, limit int) ([]*model.GeoSchemaTemplate, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	return s.repo.List(ctx, page, limit)
}

func (s *SchemaTemplateService) Get(ctx context.Context, id string) (*model.GeoSchemaTemplate, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *SchemaTemplateService) Create(ctx context.Context, in SchemaTemplateInput) (*model.GeoSchemaTemplate, error) {
	pageType, err := requiredTrimmed(in.PageType, "page_type")
	if err != nil {
		return nil, err
	}
	schemaType, err := requiredTrimmed(in.SchemaType, "schema_type")
	if err != nil {
		return nil, err
	}
	if err := validateTemplateJSON(in.TemplateJSON); err != nil {
		return nil, err
	}
	tpl := &model.GeoSchemaTemplate{
		PageType:     pageType,
		SchemaType:   schemaType,
		TemplateJSON: datatypes.JSON(in.TemplateJSON),
		Active:       true,
	}
	if in.Active != nil {
		tpl.Active = *in.Active
	}
	if err := s.repo.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *SchemaTemplateService) Update(ctx context.Context, id string, in SchemaTemplateInput) (*model.GeoSchemaTemplate, error) {
	tpl, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.PageType != nil {
		v, err := requiredTrimmed(in.PageType, "page_type")
		if err != nil {
			return nil, err
		}
		tpl.PageType = v
	}
	if in.SchemaType != nil {
		v, err := requiredTrimmed(in.SchemaType, "schema_type")
		if err != nil {
			return nil, err
		}
		tpl.SchemaType = v
	}
	if in.TemplateJSON != nil {
		if err := validateTemplateJSON(in.TemplateJSON); err != nil {
			return nil, err
		}
		tpl.TemplateJSON = datatypes.JSON(in.TemplateJSON)
	}
	if in.Active != nil {
		tpl.Active = *in.Active
	}
	if err := s.repo.Update(ctx, tpl); err != nil {
		return nil, err
	}
	tpl.UpdatedAt = time.Now()
	return tpl, nil
}

func (s *SchemaTemplateService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

func requiredTrimmed(v *string, field string) (string, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return "", fmt.Errorf("%s不能为空", field)
	}
	return strings.TrimSpace(*v), nil
}

// validateTemplateJSON 必须是 JSON 对象：模板按 @type 取键值，数组/标量无法套用。
func validateTemplateJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("模板 JSON（template_json）不能为空")
	}
	if !json.Valid(raw) {
		return errors.New("template_json 不是合法 JSON")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return errors.New("模板 JSON（template_json）必须是 JSON 对象")
	}
	return nil
}
