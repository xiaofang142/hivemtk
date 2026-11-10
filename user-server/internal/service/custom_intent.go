package service

// custom_intent.go T-09：自定义意图词条业务面。
//
// 职责：CRUD+校验（type 与默认词典/既有词条冲突 4009 拒绝）+进程内缓存（30s TTL
// +写失效，与绑定层同模式）+ 合并供给（EffectiveCustomIntentDefs → intentsForRule
// / recognizeByLLM / 词典展示消费）。
//
// 冻结契约：
//   - 默认词典不可覆盖（type 冲突拒绝）；同租户同意图 type 唯一（软删不占位）；
//   - enabled=false 不进合并结果但占位（同意图不可重建）；
//   - embedding 锚点 v1 不合并（自定义意图走规则+LLM 两级，显式边界）；
//   - 词面匹配沿 intentsForRule 既有计分语义（默认词典在前、自定义追加在后）。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// customIntentCacheTTL 跨副本写失效兜底窗口（与绑定层同口径）。
const customIntentCacheTTL = 30 * time.Second

// customIntentTypeRe type 形态约束（小写字母/数字/下划线，区别于中文显示名）。
var customIntentTypeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// ---- 进程级单例（识别器消费入口；沿 GetIntentRecognizer 同款装配形态） ----

var (
	globalCustomIntentSvc *CustomIntentService
)

// SetCustomIntentService 装配层注入（app.InitCustomIntentRuntime；nil 不生效）。
func SetCustomIntentService(svc *CustomIntentService) {
	globalCustomIntentSvc = svc
}

// GetCustomIntentService 取单例（未装配返回 nil；消费方 nil-safe）。
func GetCustomIntentService() *CustomIntentService {
	return globalCustomIntentSvc
}

// CustomIntentService 自定义意图词条服务。
type CustomIntentService struct {
	repo repository.CustomIntentRepository
	// catalog TB-10 刀4：custom_intents 写面同步写统一目录（source=custom 行）；
	// nil 容忍（单测/未装配场景跳过同步）。
	catalog *IntentCatalogService

	mu            sync.RWMutex
	cache         map[uint][]IntentDef
	cacheLoadedAt map[uint]time.Time
}

// SetIntentCatalogDependency 装配层注入目录服务（刀4 写面同步；nil 跳过同步）。
func (s *CustomIntentService) SetIntentCatalogDependency(catalog *IntentCatalogService) {
	s.catalog = catalog
}

// syncCatalogUpsert 把 custom_intents 行同步 upsert 到统一目录（source=custom）。
// 同步失败不阻塞主写（catalog 有 30s TTL + 回落链兜底），只记日志。
func (s *CustomIntentService) syncCatalogUpsert(ctx context.Context, rec *model.CustomIntent) {
	if s.catalog == nil || s.catalog.repo == nil {
		return
	}
	existing, err := s.catalog.repo.GetByKeyAny(ctx, rec.Type)
	if err != nil {
		logger.Warnf("[custom_intent] ⚠️ 目录同步读失败（type=%s）: %v", rec.Type, err)
		return
	}
	family := intentCatalogFamilyCustom
	if existing != nil && existing.Source == intentCatalogSourceCustom {
		family = existing.Family
	}
	row := &model.IntentCatalog{
		TenantID:     rec.TenantID,
		IntentKey:    rec.Type,
		Source:       intentCatalogSourceCustom,
		Family:       family,
		Name:         rec.Name,
		KeywordsJSON: rec.KeywordsJSON,
		ExamplesJSON: rec.ExamplesJSON,
		Description:  rec.Description,
		Enabled:      rec.Enabled,
	}
	if existing != nil && existing.Source == intentCatalogSourceCustom {
		row.ID = existing.ID
		if err := s.catalog.repo.Update(ctx, row); err != nil {
			logger.Warnf("[custom_intent] ⚠️ 目录同步更新失败（type=%s）: %v", rec.Type, err)
			return
		}
	} else {
		if err := s.catalog.repo.Create(ctx, row); err != nil {
			logger.Warnf("[custom_intent] ⚠️ 目录同步写入失败（type=%s）: %v", rec.Type, err)
			return
		}
	}
	s.catalog.invalidate(tenantIDOfCtx(ctx))
}

// syncCatalogDelete 把目录中 source=custom 的同键行软删（fail-open）。
func (s *CustomIntentService) syncCatalogDelete(ctx context.Context, intentType string) {
	if s.catalog == nil || s.catalog.repo == nil {
		return
	}
	existing, err := s.catalog.repo.GetByKeyAny(ctx, intentType)
	if err != nil || existing == nil || existing.Source != intentCatalogSourceCustom {
		return
	}
	if err := s.catalog.repo.Delete(ctx, existing.ID); err != nil {
		logger.Warnf("[custom_intent] ⚠️ 目录同步删除失败（type=%s）: %v", intentType, err)
		return
	}
	s.catalog.invalidate(tenantIDOfCtx(ctx))
}

func NewCustomIntentService(repo repository.CustomIntentRepository) *CustomIntentService {
	return &CustomIntentService{
		repo:          repo,
		cache:         make(map[uint][]IntentDef),
		cacheLoadedAt: make(map[uint]time.Time),
	}
}

// CustomIntentUpsertInput 创建/更新入参。
type CustomIntentUpsertInput struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Keywords    []string `json:"keywords"`
	Examples    []string `json:"examples"`
	Description string   `json:"description"`
	Enabled     *bool    `json:"enabled"`
}

// Create 新建词条。校验：type 格式（小写字母数字下划线）/keywords 非空/type 冲突。
func (s *CustomIntentService) Create(ctx context.Context, in CustomIntentUpsertInput) (*model.CustomIntent, error) {
	if err := s.validateUpsert(ctx, in, 0); err != nil {
		return nil, err
	}
	rec := &model.CustomIntent{
		Type:         in.Type,
		Name:         in.Name,
		KeywordsJSON: marshalStringList(in.Keywords),
		ExamplesJSON: marshalStringList(in.Examples),
		Description:  in.Description,
		Enabled:      in.Enabled == nil || *in.Enabled,
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		return nil, err
	}
	s.syncCatalogUpsert(ctx, rec)
	s.invalidate(tenantIDOfCtx(ctx))
	return rec, nil
}

// Update 更新词条（type 不可改：改 type=先删后建，避免词典键漂移的半态）。
func (s *CustomIntentService) Update(ctx context.Context, id uint, in CustomIntentUpsertInput) (*model.CustomIntent, error) {
	if in.Type == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "type 不能为空")
	}
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Type != in.Type {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "type 不可修改（如需换意图键请删除后重建）")
	}
	if err := s.validateUpsert(ctx, in, rec.ID); err != nil {
		return nil, err
	}
	rec.Name = in.Name
	rec.KeywordsJSON = marshalStringList(in.Keywords)
	rec.ExamplesJSON = marshalStringList(in.Examples)
	rec.Description = in.Description
	if in.Enabled != nil {
		rec.Enabled = *in.Enabled
	}
	if err := s.repo.Update(ctx, rec); err != nil {
		return nil, err
	}
	s.syncCatalogUpsert(ctx, rec)
	s.invalidate(tenantIDOfCtx(ctx))
	return rec, nil
}

// Delete 删除词条（软删）。
func (s *CustomIntentService) Delete(ctx context.Context, id uint) error {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.syncCatalogDelete(ctx, rec.Type)
	s.invalidate(tenantIDOfCtx(ctx))
	return nil
}

// Get 单条。
func (s *CustomIntentService) Get(ctx context.Context, id uint) (*model.CustomIntent, error) {
	return s.repo.GetByID(ctx, id)
}

// List 分页。
func (s *CustomIntentService) List(ctx context.Context, page, pageSize int) ([]model.CustomIntent, int64, error) {
	return s.repo.List(ctx, page, pageSize)
}

// IsCustomIntentType 该 type 是否为本租户启用中的自定义意图（绑定层动态放行面）。
func (s *CustomIntentService) IsCustomIntentType(ctx context.Context, intentType string) bool {
	if s == nil || s.repo == nil {
		return false
	}
	for _, def := range s.EffectiveCustomIntentDefs(ctx) {
		if def.Type == intentType {
			return true
		}
	}
	return false
}

// EffectiveCustomIntentDefs 启用中的自定义词条 → IntentDef 合并形态（缓存读）。
func (s *CustomIntentService) EffectiveCustomIntentDefs(ctx context.Context) []IntentDef {
	if s == nil || s.repo == nil {
		return nil
	}
	id := tenantIDOfCtx(ctx)
	if defs, ok := s.cached(id); ok {
		return defs
	}
	rows, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil
	}
	defs := make([]IntentDef, 0, len(rows))
	for i := range rows {
		d := customIntentToDef(&rows[i])
		if d != nil {
			defs = append(defs, *d)
		}
	}
	s.store(id, defs)
	return defs
}

func (s *CustomIntentService) cached(id uint) ([]IntentDef, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	loadedAt, ok := s.cacheLoadedAt[id]
	if !ok || time.Since(loadedAt) > customIntentCacheTTL {
		return nil, false
	}
	return s.cache[id], true
}

func (s *CustomIntentService) store(id uint, defs []IntentDef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[id] = defs
	s.cacheLoadedAt[id] = time.Now()
}

func (s *CustomIntentService) invalidate(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, id)
	delete(s.cacheLoadedAt, id)
}

// customIntentToDef 行→词典形态；JSON 解析失败 fail-closed 跳过。
func customIntentToDef(rec *model.CustomIntent) *IntentDef {
	if rec == nil {
		return nil
	}
	var keywords, examples []string
	if err := json.Unmarshal([]byte(rec.KeywordsJSON), &keywords); err != nil || len(keywords) == 0 {
		return nil
	}
	_ = json.Unmarshal([]byte(rec.ExamplesJSON), &examples)
	desc := rec.Description
	if strings.TrimSpace(desc) == "" {
		desc = rec.Name
	}
	return &IntentDef{
		Type:        rec.Type,
		Name:        rec.Name,
		Keywords:    keywords,
		Examples:    examples,
		Description: desc,
	}
}

func (s *CustomIntentService) validateUpsert(ctx context.Context, in CustomIntentUpsertInput, selfID uint) error {
	if !customIntentTypeRe.MatchString(in.Type) {
		return bizerr.New(bizerr.CodeParamInvalid, "type 仅允许小写字母/数字/下划线（如 return_policy）")
	}
	if strings.TrimSpace(in.Name) == "" {
		return bizerr.New(bizerr.CodeParamInvalid, "name 不能为空")
	}
	filtered := make([]string, 0, len(in.Keywords))
	for _, kw := range in.Keywords {
		if kw = strings.TrimSpace(kw); kw != "" {
			filtered = append(filtered, kw)
		}
	}
	if len(filtered) == 0 {
		return bizerr.New(bizerr.CodeParamInvalid, "keywords 至少一个非空词面")
	}
	in.Keywords = filtered
	// type 冲突：默认词典 || 既有词条（含禁用态占位，软删不占位由查重自然保证）
	for _, def := range DefaultIntents {
		if def.Type == in.Type {
			return bizerr.New(bizerr.CodeConflict, fmt.Sprintf("type %q 与内置意图冲突，内置词典不可覆盖", in.Type))
		}
	}
	existing, err := s.repo.GetByType(ctx, in.Type)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != selfID {
		return bizerr.New(bizerr.CodeConflict, fmt.Sprintf("type %q 已存在自定义词条（编辑既有配置）", in.Type))
	}
	return nil
}

func marshalStringList(list []string) string {
	if len(list) == 0 {
		return "[]"
	}
	buf, _ := json.Marshal(list)
	return string(buf)
}

func tenantIDOfCtx(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

// effectiveCustomIntentDefs 包级便捷读取（识别器消费；未装配返回 nil）。
func effectiveCustomIntentDefs(ctx context.Context) []IntentDef {
	if svc := GetCustomIntentService(); svc != nil {
		return svc.EffectiveCustomIntentDefs(ctx)
	}
	return nil
}
