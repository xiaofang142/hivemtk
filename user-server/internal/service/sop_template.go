package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"hivemtk-user/internal/pkg/utils"

	"gorm.io/gorm"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

const (
	sopCacheTTL         = 5 * time.Minute
	sopCacheMaxN        = 2000
	sopTopK             = 5
	sopAgentShared uint = 0
)

var sopTemplateWhitelist = map[string]struct{}{
	"customer_id":  {},
	"intent":       {},
	"stage":        {},
	"agent_name":   {},
	"product_name": {},
	"intent_name":  {},
}

type sopRepoIface interface {
	Create(ctx context.Context, tpl *model.SOPTemplate) error
	GetByID(ctx context.Context, id uint) (*model.SOPTemplate, error)
	ListEnabled(ctx context.Context, limit int) ([]model.SOPTemplate, error)
	MatchByIntent(ctx context.Context, intent string) ([]model.SOPTemplate, error)
	MatchByIntentStage(ctx context.Context, intent, stage string) ([]model.SOPTemplate, error)
	MatchByAgent(ctx context.Context, agentID uint, intent, stage string) ([]model.SOPTemplate, error)
	ListByAgent(ctx context.Context, agentID uint, limit int) ([]model.SOPTemplate, error)
	IncrementHitCount(ctx context.Context, id uint) error
	ListWithFilter(ctx context.Context, filter repository.SOPTemplateListParams) ([]model.SOPTemplate, int64, error)
	Update(ctx context.Context, id uint, tpl *model.SOPTemplate) error
	Delete(ctx context.Context, id uint) error
}

type sopBindingRepoIface interface {
	ListByAgent(ctx context.Context, agentID uint, kbType string) ([]model.AgentKBBinding, error)
}

// SOPTemplateService SOP 模板业务服务
//
// Task 16 变更:
//   - 增加 bindingRepo 字段 (注入绑定仓库, 用于校验 agent ↔ KB 关系)
//   - cache 由单片 []model.SOPTemplate 改为 map[uint][]model.SOPTemplate (按 agentID 分片)
//   - 新增 sharedCache 概念: agentID=0 的桶存储"共享池" SOP (向后兼容旧 Match API)
type SOPTemplateService struct {
	repo        sopRepoIface
	bindingRepo sopBindingRepoIface

	mu     sync.RWMutex
	cache  map[uint][]model.SOPTemplate
	loaded map[uint]time.Time
}

// NewSOPTemplateServiceDefault 使用全局 DB 创建 SOP Service（controller 层入口，避免 controller 持有 gorm.DB）。
func NewSOPTemplateServiceDefault() *SOPTemplateService {
	return NewSOPTemplateService(dbUtil.GetDB(), nil)
}

// NewSOPTemplateService 创建 SOP Service
//
// Task 16: 第二个参数保留 *repository.SOPTemplateRepository 兼容旧调用, bindingRepo 走 SetBindingRepo 注入。
func NewSOPTemplateService(db *gorm.DB, repo *repository.SOPTemplateRepository) *SOPTemplateService {
	var iface sopRepoIface
	if repo == nil && db != nil {
		repo = repository.NewSOPTemplateRepository(db)
	}
	if repo != nil {
		iface = repo
	}
	var bindingIface sopBindingRepoIface
	if db != nil {
		bindingIface = repository.NewAgentKBBindingRepository(db)
	}
	return &SOPTemplateService{
		repo:        iface,
		bindingRepo: bindingIface,
		cache:       make(map[uint][]model.SOPTemplate),
		loaded:      make(map[uint]time.Time),
	}
}

// NewSOPTemplateServiceWithRepo 用任意实现 sopRepoIface 的 repo 创建 (单测用)
func NewSOPTemplateServiceWithRepo(repo sopRepoIface) *SOPTemplateService {
	return &SOPTemplateService{
		repo:   repo,
		cache:  make(map[uint][]model.SOPTemplate),
		loaded: make(map[uint]time.Time),
	}
}

// NewSOPTemplateServiceWithRepos 用 repo + bindingRepo 同时注入 (Task 16: 单元测试/上层装配)
func NewSOPTemplateServiceWithRepos(repo sopRepoIface, bindingRepo sopBindingRepoIface) *SOPTemplateService {
	return &SOPTemplateService{
		repo:        repo,
		bindingRepo: bindingRepo,
		cache:       make(map[uint][]model.SOPTemplate),
		loaded:      make(map[uint]time.Time),
	}
}

// SetBindingRepo 注入 binding 仓库 (供 layer / controller 装配时使用)
func (s *SOPTemplateService) SetBindingRepo(r sopBindingRepoIface) {
	if r != nil {
		s.bindingRepo = r
	}
}

// MatchByIntent 按意图匹配 (旧 API, 全局共享 — 仅用于管理界面 / 调试)
func (s *SOPTemplateService) MatchByIntent(ctx context.Context, intent string) ([]model.SOPTemplate, error) {
	if s.repo == nil || intent == "" {
		return nil, nil
	}
	return s.repo.MatchByIntent(ctx, intent)
}

// MatchByIntentStage 按 (intent, stage) 精确匹配 (旧 API, 全局共享 — 仅用于管理界面 / 调试)
func (s *SOPTemplateService) MatchByIntentStage(ctx context.Context, intent, stage string) ([]model.SOPTemplate, error) {
	if s.repo == nil {
		return nil, nil
	}
	return s.repo.MatchByIntentStage(ctx, intent, stage)
}

// MatchByAgent 强 1对1 匹配 SOP 模板 (Task 16: 严格 1:1 改造)
//
// 新签名: (ctx, agentID uint, intent, stage string, topK int)
//
// 行为:
//   - agentID == 0: 不再走"空数组=全局"回退, 直接返回 (nil, nil)
//   - agentID > 0: 仅匹配 enabled = true AND agent_id = ? 的 SOP
//   - intent/stage 为空时该过滤项不应用
//   - topK <= 0 时走 sopTopK (5)
//
// 调用方 (LayerRouter / SOPTemplateController) 必须显式传入 agentID。
func (s *SOPTemplateService) MatchByAgent(ctx context.Context, agentID uint, intent, stage string, topK int) ([]model.SOPTemplate, error) {
	if s.repo == nil {
		return nil, nil
	}
	if agentID == 0 {
		return nil, nil
	}
	if topK <= 0 {
		topK = sopTopK
	}
	all, err := s.repo.MatchByAgent(ctx, agentID, intent, stage)
	if err != nil {
		return nil, err
	}
	if len(all) > topK {
		all = all[:topK]
	}
	return all, nil
}

// List 列表查询
func (s *SOPTemplateService) List(ctx context.Context, filter dto.SOPTemplateFilter) ([]dto.SOPTemplate, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	params := repository.SOPTemplateListParams{
		Keyword:  filter.Keyword,
		Intent:   filter.Intent,
		Stage:    filter.Stage,
		Enabled:  filter.Enabled,
		AgentID:  filter.AgentID,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	tpls, total, err := s.repo.ListWithFilter(ctx, params)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.SOPTemplate, 0, len(tpls))
	for i := range tpls {
		out = append(out, *sopToDTO(&tpls[i]))
	}
	return out, total, nil
}

// GetByID 按 ID 查询
func (s *SOPTemplateService) GetByID(ctx context.Context, id uint) (*dto.SOPTemplate, error) {
	if s.repo == nil {
		return nil, nil
	}
	tpl, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return sopToDTO(tpl), nil
}

// Create 新增 SOP 模板
//
// Task 16 改造: AgentID 必填 (强 1对1, 所有 SOP 都必须归属于某个智能体或共享池)
func (s *SOPTemplateService) Create(ctx context.Context, tpl *model.SOPTemplate) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	if tpl == nil {
		return errors.New("tpl is nil")
	}
	if tpl.AgentID == nil || *tpl.AgentID == 0 {
		return errors.New("agent_id 必填 (Task 16 强 1对1: 不允许全局匿名 SOP 模板)")
	}
	if strings.TrimSpace(tpl.Intent) == "" {
		return errors.New("intent 必填")
	}
	if strings.TrimSpace(tpl.Stage) == "" {
		return errors.New("stage 必填")
	}
	if tpl.Enabled == nil {
		t := true
		tpl.Enabled = &t
	}
	if err := s.repo.Create(ctx, tpl); err != nil {
		return err
	}
	s.InvalidateCache(*tpl.AgentID)
	return nil
}

// Update 更新 SOP 模板
func (s *SOPTemplateService) Update(ctx context.Context, id uint, tpl *model.SOPTemplate) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	tpl.ID = id
	if err := s.repo.Update(ctx, id, tpl); err != nil {
		return err
	}
	if tpl != nil && tpl.AgentID != nil {
		s.InvalidateCache(*tpl.AgentID)
	} else {
		s.InvalidateAllCache()
	}
	return nil
}

// Delete 删除 SOP 模板
func (s *SOPTemplateService) Delete(ctx context.Context, id uint) error {
	if s.repo == nil {
		return fmt.Errorf("repo not initialized")
	}
	existing, _ := s.repo.GetByID(ctx, id)
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if existing != nil && existing.AgentID != nil {
		s.InvalidateCache(*existing.AgentID)
	} else {
		s.InvalidateAllCache()
	}
	return nil
}

func sopToDTO(t *model.SOPTemplate) *dto.SOPTemplate {
	if t == nil {
		return nil
	}
	enabled := false
	if t.Enabled != nil {
		enabled = *t.Enabled
	}
	return &dto.SOPTemplate{
		ID:         t.ID,
		Name:       t.Name,
		Intent:     t.Intent,
		Stage:      t.Stage,
		Template:   t.Template,
		Vars:       t.Vars,
		Priority:   t.Priority,
		Confidence: t.Confidence,
		HitCount:   t.HitCount,
		Enabled:    &enabled,
	}
}

// Render 渲染模板 (Go text/template)
//
//   - rawTpl: 含 {{.var_name}} 占位符的字符串
//
// vars: map[string]any 变量 (: 仅白名单字段透传到模板, 防止 SSTI)
//
// 安全:
//   - 白名单外的字段 (特别是 user_message) 会被过滤, 不会到达模板
//   - 即使模板作者写了 {{.user_message}}, 渲染时该 key 也不存在 (missingkey=zero)
//   - 白名单 sopTemplateWhitelist 是 const map, 不可运行时篡改
func (s *SOPTemplateService) Render(rawTpl string, vars map[string]any) (string, error) {
	if rawTpl == "" {
		return "", nil
	}
	safe := filterWhitelistVars(vars)
	tpl, err := template.New("sop").Option("missingkey=zero").Parse(rawTpl)
	if err != nil {
		return "", fmt.Errorf("sop template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, safe); err != nil {
		return "", fmt.Errorf("sop template execute: %w", err)
	}
	return buf.String(), nil
}

func filterWhitelistVars(vars map[string]any) map[string]any {
	if vars == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(sopTemplateWhitelist))
	for k := range sopTemplateWhitelist {
		if v, ok := vars[k]; ok {
			out[k] = v
		}
	}
	return out
}

// IncrementHitCount 命中计数 (异步)
func (s *SOPTemplateService) IncrementHitCount(ctx context.Context, id uint) {
	if s.repo == nil || id == 0 {
		return
	}

	utils.SafeGo(context.Background(), "sop_template.IncrementHitCount", func(bgCtx context.Context) {
		bgCtx, cancel := context.WithTimeout(bgCtx, utils.ShortTimeout)
		defer cancel()
		utils.WarnErrKV("sop_template.IncrementHitCount.repo",
			s.repo.IncrementHitCount(bgCtx, id),
			"id", strconv.FormatUint(uint64(id), 10))
	})
}

// WarmupCache 预热缓存 (Task 16: 按 agentID 分片)
//
// 行为:
//   - agentID == 0: 预热共享池 (走 ListEnabled)
//   - agentID > 0: 仅预热该智能体的 SOP (走 ListByAgent)
//   - TTL = 5 min, 重复调用会覆盖旧缓存
func (s *SOPTemplateService) WarmupCache(ctx context.Context, agentID uint) error {
	if s.repo == nil {
		return nil
	}
	var tpls []model.SOPTemplate
	var err error
	if agentID == sopAgentShared {
		tpls, err = s.repo.ListEnabled(ctx, sopCacheMaxN)
	} else {
		tpls, err = s.repo.ListByAgent(ctx, agentID, sopCacheMaxN)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[agentID] = tpls
	s.loaded[agentID] = time.Now()
	s.mu.Unlock()
	return nil
}

// WarmupAll 预热所有已知 agent 的缓存 (启动时调用, 由调用方传入 agentID 列表)
//
// 用途: 已知全量 agent 列表时, 一次预热避免运行时冷启动。
// 实现: 简单串行预热每个 agent, 失败不阻塞。
func (s *SOPTemplateService) WarmupAll(ctx context.Context, agentIDs []uint) {
	for _, id := range agentIDs {

		utils.WarnErrKV("sop_template.WarmupCache",
			s.WarmupCache(ctx, id),
			"id", strconv.FormatUint(uint64(id), 10))
	}
}

// InvalidateCache 失效指定 agentID 的缓存 (Task 16: 精确失效)
//
// agentID == 0: 失效共享池
// agentID > 0: 失效该 agent 私有桶
func (s *SOPTemplateService) InvalidateCache(agentID uint) {
	s.mu.Lock()
	delete(s.cache, agentID)
	delete(s.loaded, agentID)
	s.mu.Unlock()
}

// InvalidateAllCache 失效全部缓存 (Task 16 兼容入口)
//
// 用于: 业务不确定受影响 agentID 时 (例如全局配置变更)。
func (s *SOPTemplateService) InvalidateAllCache() {
	s.mu.Lock()
	s.cache = make(map[uint][]model.SOPTemplate)
	s.loaded = make(map[uint]time.Time)
	s.mu.Unlock()
}

// BuildLayer1Reply 构造 Layer1 SOP 模板回复
func (s *SOPTemplateService) BuildLayer1Reply(tpl *model.SOPTemplate, vars map[string]any) (string, error) {
	if tpl == nil {
		return "", nil
	}
	rendered, err := s.Render(tpl.Template, vars)
	if err != nil {
		return "", err
	}
	return rendered, nil
}

// ShouldSkipLLM SOP 模板是否足以跳过 LLM
func (s *SOPTemplateService) ShouldSkipLLM(tpl *model.SOPTemplate) bool {
	if tpl == nil {
		return false
	}
	return tpl.Confidence >= 0.7
}
