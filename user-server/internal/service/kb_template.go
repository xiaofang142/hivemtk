// kb_template_service.go 知识库模板服务（R39-K7）。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// KbTemplateService 模板 CRUD + apply（一键建 KB）+ rate。
// apply 复用 KnowledgeBaseService.CreateKB：kb_code/type/owner 校验与
// agent 绑定都在那条路上，模板侧不复制任何一条业务规则。
type KbTemplateService struct {
	repo  *repository.KbTemplateRepository
	kbSvc *KnowledgeBaseService
}

func NewKbTemplateService(repo *repository.KbTemplateRepository, kbSvc *KnowledgeBaseService) *KbTemplateService {
	return &KbTemplateService{repo: repo, kbSvc: kbSvc}
}

func (s *KbTemplateService) List(ctx context.Context, category, status string) ([]*model.KbTemplate, error) {
	return s.repo.List(ctx, category, status)
}

func (s *KbTemplateService) GetByID(ctx context.Context, id uint) (*model.KbTemplate, error) {
	return s.repo.GetByID(ctx, id)
}

type KbTemplateUpsertReq struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	KBType      string `json:"kb_type"`
	Structure   string `json:"structure"`
	Status      string `json:"status"`
}

var validKBTemplateStatuses = map[string]bool{
	model.KbTemplateStatusDraft:     true,
	model.KbTemplateStatusPublished: true,
}

func (s *KbTemplateService) Create(ctx context.Context, createdBy uint, req *KbTemplateUpsertReq) (*model.KbTemplate, error) {
	tpl, err := s.normalize(req)
	if err != nil {
		return nil, err
	}
	tpl.CreatedBy = createdBy
	if err := s.repo.Create(ctx, tpl); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *KbTemplateService) Update(ctx context.Context, id uint, req *KbTemplateUpsertReq) (*model.KbTemplate, error) {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return nil, err
	}
	tpl, err := s.normalize(req)
	if err != nil {
		return nil, err
	}
	cols := map[string]any{
		"name":        tpl.Name,
		"category":    tpl.Category,
		"description": tpl.Description,
		"kb_type":     tpl.KBType,
		"structure":   tpl.Structure,
		"status":      tpl.Status,
	}
	if err := s.repo.Update(ctx, id, cols); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *KbTemplateService) Delete(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}

func (s *KbTemplateService) normalize(req *KbTemplateUpsertReq) (*model.KbTemplate, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: 请求体为空", utils.ErrInvalidInput)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name 必填", utils.ErrInvalidInput)
	}
	kbType := strings.ToLower(strings.TrimSpace(req.KBType))
	if !IsValidKBType(kbType) {
		return nil, fmt.Errorf("%w: kb_type 非法: %s (faq/rag/sop)", utils.ErrInvalidInput, kbType)
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = model.KbTemplateStatusPublished
	}
	if !validKBTemplateStatuses[status] {
		return nil, fmt.Errorf("%w: status 非法: %s (draft/published)", utils.ErrInvalidInput, status)
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "general"
	}
	return &model.KbTemplate{
		Name:        name,
		Category:    category,
		Description: req.Description,
		KBType:      kbType,
		Structure:   req.Structure,
		Status:      status,
	}, nil
}

// KbTemplateApplyReq apply 请求。name/kb_code 缺省时由模板派生；
// owner_* 原样交给 CreateKB（private 需要 owner_agent_id，shared 必须为空）。
type KbTemplateApplyReq struct {
	Name         string `json:"name"`
	KBCode       string `json:"kb_code"`
	OwnerType    string `json:"owner_type"`
	OwnerAgentID *uint  `json:"owner_agent_id"`
}

// Apply 一键建 KB：创建真实 KnowledgeBase 并回填模板计数。
// 返回创建好的 KB（含 ID）；结构快照在模板行上，前端铺底自行读取。
func (s *KbTemplateService) Apply(ctx context.Context, templateID uint, req *KbTemplateApplyReq) (*model.KnowledgeBase, error) {
	if req == nil {
		req = &KbTemplateApplyReq{}
	}
	tpl, err := s.repo.GetByID(ctx, templateID)
	if err != nil {
		return nil, err
	}
	if tpl.Status != model.KbTemplateStatusPublished {
		return nil, fmt.Errorf("%w: 模板未发布，不能 apply", utils.ErrInvalidInput)
	}

	kb := &model.KnowledgeBase{
		KBCode:       strings.TrimSpace(req.KBCode),
		Type:         tpl.KBType,
		Name:         strings.TrimSpace(req.Name),
		Description:  tpl.Description,
		OwnerType:    req.OwnerType,
		OwnerAgentID: req.OwnerAgentID,
	}
	// 缺省 shared：模板建的是起步库，绑定哪个 agent 是调用方的后续动作；
	// private 语义（建库即绑 agent）必须显式传 owner_agent_id 才成立。
	if kb.OwnerType == "" {
		kb.OwnerType = model.KnowledgeBaseOwnerShared
	}
	if kb.Name == "" {
		kb.Name = tpl.Name
	}
	if kb.KBCode == "" {
		kb.KBCode = fmt.Sprintf("kb_tpl%d_%d", templateID, time.Now().UnixNano())
	}
	if err := s.kbSvc.CreateKB(ctx, kb); err != nil {
		return nil, err
	}
	// 计数是运营遥测不是资金账：KB 已建成的前提下计数失败不推翻主效果，
	// 也不把"建库成功"报成失败。这条是显式取舍，不是吞错——错在这里没有可信的
	// 补救动作（重试同样可能失败，回滚建库会误删用户刚拿到的 KB）。
	_ = s.repo.IncrementApplyCount(ctx, templateID)
	return kb, nil
}

// Rate 评分（1-5），运行均值落库并返回新均值。
func (s *KbTemplateService) Rate(ctx context.Context, templateID uint, score float64) (float64, error) {
	if score < 1 || score > 5 {
		return 0, fmt.Errorf("%w: 评分范围 1-5", utils.ErrInvalidInput)
	}
	return s.repo.UpdateRating(ctx, templateID, score)
}
