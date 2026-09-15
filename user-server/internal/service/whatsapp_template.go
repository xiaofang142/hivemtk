package service

import (
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"

	"context"

	"gorm.io/gorm"
)

type WhatsAppTemplateService struct {
	repo *repository.WhatsappTemplateRepository
}

func NewWhatsAppTemplateService(db *gorm.DB) *WhatsAppTemplateService {
	if db != nil {
		return &WhatsAppTemplateService{repo: repository.NewWhatsappTemplateRepositoryWithDB(db)}
	}
	return &WhatsAppTemplateService{repo: repository.NewWhatsappTemplateRepository()}
}

func (ts *WhatsAppTemplateService) CreateTemplate(ctx context.Context, template *model.WhatsappMessageTemplate) (*model.WhatsappMessageTemplate, error) {
	if template.ID == "" {
		template.ID = generateTemplateID()
	}
	now := time.Now()
	template.CreatedAt = now
	template.UpdatedAt = now

	if err := ts.repo.Create(context.Background(), template); err != nil {
		return nil, err
	}
	return template, nil
}

func (ts *WhatsAppTemplateService) UpdateTemplate(ctx context.Context, template *model.WhatsappMessageTemplate) (*model.WhatsappMessageTemplate, error) {
	template.UpdatedAt = time.Now()
	if err := ts.repo.Save(context.Background(), template); err != nil {
		return nil, err
	}
	return template, nil
}

func (ts *WhatsAppTemplateService) GetTemplate(ctx context.Context, templateID string) (*model.WhatsappMessageTemplate, error) {
	template, err := ts.repo.GetByID(context.Background(), templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("模板不存在")
		}
		return nil, err
	}
	return template, nil
}

func (ts *WhatsAppTemplateService) GetTemplates(ctx context.Context, category string, isActive *bool) ([]*model.WhatsappMessageTemplate, error) {
	return ts.repo.ListByFilters(context.Background(), category, isActive)
}

func (ts *WhatsAppTemplateService) DeleteTemplate(ctx context.Context, templateID string) error {
	rowsAffected, err := ts.repo.DeleteByID(context.Background(), templateID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("模板不存在")
	}
	return nil
}

func generateTemplateID() string {
	return fmt.Sprintf("tmpl_%d", time.Now().UnixNano())
}
