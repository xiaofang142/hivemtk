package service

// intent_tool_chain.go TB-01：意图→工具链绑定层业务面。
//
// 职责：CRUD + 写侧校验（意图词表/步骤形态/模板变量白名单预检）+ 进程内缓存
// （TTL 30s + 本进程写失效；跨副本一致性靠 TTL 兜底，绑定配置变更频率极低）+
// 实现 agent_runtime.IntentChainLookup 供规划器消费。
//
// 词表单一事实源：意图枚举直接引用 agent runtime 常量（编译期绑定，漂移即编译
// 失败）；模板变量白名单与运行期渲染共用 agent_runtime.ChainTemplateVarNames /
// ValidateChainArgs（fail-closed 双防线：写侧拒配 + 运行期整链拒用）。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// chainCacheTTL 绑定缓存 TTL：跨副本写失效的兜底窗口。
const chainCacheTTL = 30 * time.Second

// validChainIntents 可绑定的意图词表（编译期引用 runtime 常量，两套词表不允许漂移）。
var validChainIntents = map[string]bool{
	string(agent_runtime.IntentChitchat):       true,
	string(agent_runtime.IntentInquiry):        true,
	string(agent_runtime.IntentOrderStatus):    true,
	string(agent_runtime.IntentComplaint):      true,
	string(agent_runtime.IntentRefund):         true,
	string(agent_runtime.IntentAfterSales):     true,
	string(agent_runtime.IntentSalesLead):      true,
	string(agent_runtime.IntentGreeting):       true,
	string(agent_runtime.IntentFarewell):       true,
	string(agent_runtime.IntentHandoffToHuman): true,
	string(agent_runtime.IntentFAQ):            true,
	string(agent_runtime.IntentUnknown):        true,
}

// ChainIntentVocabulary 返回可绑定意图清单（升序；供管理端 UI 与 vocab 端点）。
func ChainIntentVocabulary() []string {
	names := make([]string, 0, len(validChainIntents))
	for name := range validChainIntents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IntentToolChainService 意图→工具链绑定服务。
type IntentToolChainService struct {
	repo repository.IntentToolChainRepository

	mu            sync.RWMutex
	cache         map[uint]map[string]*agent_runtime.IntentChainSpec
	cacheLoadedAt map[uint]time.Time
}

// NewIntentToolChainService 构造（repo 由装配层显式注入句柄版仓储）。
func NewIntentToolChainService(repo repository.IntentToolChainRepository) *IntentToolChainService {
	return &IntentToolChainService{
		repo:          repo,
		cache:         make(map[uint]map[string]*agent_runtime.IntentChainSpec),
		cacheLoadedAt: make(map[uint]time.Time),
	}
}

// ---- LookupIntentChain 实现 agent_runtime.IntentChainLookup（planner 消费） ----

// LookupIntentChain 查租户级意图绑定；未命中/禁用/解析失败返回 (nil, nil)
// （planner 回落硬编码）。租户从 ctx 解析，无租户上下文按 DefaultTenantID 收敛。
func (s *IntentToolChainService) LookupIntentChain(ctx context.Context, intent string) (*agent_runtime.IntentChainSpec, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	tenantID := chainTenantID(ctx)
	if spec, ok := s.cachedSpec(tenantID, intent); ok {
		return spec, nil
	}
	rows, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	specs := make(map[string]*agent_runtime.IntentChainSpec, len(rows))
	for i := range rows {
		spec := chainRowToSpec(&rows[i])
		if spec == nil {
			continue
		}
		specs[spec.IntentCode] = spec
	}
	s.storeCache(tenantID, specs)
	return specs[intent], nil
}

func (s *IntentToolChainService) cachedSpec(tenantID uint, intent string) (*agent_runtime.IntentChainSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	loadedAt, loaded := s.cacheLoadedAt[tenantID]
	if !loaded || time.Since(loadedAt) > chainCacheTTL {
		return nil, false
	}
	spec, ok := s.cache[tenantID][intent]
	return spec, ok
}

func (s *IntentToolChainService) storeCache(tenantID uint, specs map[string]*agent_runtime.IntentChainSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[tenantID] = specs
	s.cacheLoadedAt[tenantID] = time.Now()
}

func (s *IntentToolChainService) invalidateCache(tenantID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, tenantID)
	delete(s.cacheLoadedAt, tenantID)
}

// chainRowToSpec 行→runtime 契约映射；解析失败/意图不在词表返回 nil（fail-closed 跳过）。
func chainRowToSpec(rec *model.IntentToolChain) *agent_runtime.IntentChainSpec {
	if rec == nil || !validChainIntents[rec.IntentCode] {
		return nil
	}
	var steps []model.IntentChainStepConfig
	if err := json.Unmarshal([]byte(rec.StepsJSON), &steps); err != nil {
		logger.Warnf("[intent_chain] 绑定行 steps_json 解析失败 id=%d err=%v", rec.ID, err)
		return nil
	}
	if len(steps) == 0 || len(steps) > agent_runtime.ChainMaxSteps {
		return nil
	}
	spec := &agent_runtime.IntentChainSpec{
		IntentCode: rec.IntentCode,
		ReplyHint:  rec.ReplyHint,
		Steps:      make([]agent_runtime.IntentChainStep, 0, len(steps)),
	}
	for _, st := range steps {
		spec.Steps = append(spec.Steps, agent_runtime.IntentChainStep{
			ToolName: st.ToolName,
			Args:     st.Args,
			Priority: st.Priority,
			Enabled:  st.Enabled,
		})
	}
	return spec
}

func chainTenantID(ctx context.Context) uint {
	if id, ok := tenant.FromContext(ctx); ok && id > 0 {
		return id
	}
	return tenant.DefaultTenantID
}

// ---- CRUD（管理端） ----

// ChainUpsertInput 创建/更新入参。
type ChainUpsertInput struct {
	Name       string                        `json:"name"`
	IntentCode string                        `json:"intent_code"`
	Steps      []model.IntentChainStepConfig `json:"steps"`
	ReplyHint  string                        `json:"reply_hint"`
	Enabled    *bool                         `json:"enabled"`
}

// Create 新建绑定。校验：意图词表/步骤 1~N/工具名非空/模板变量白名单预检；
// 同租户同意图唯一（软删行不占用：写侧显式查重）。
func (s *IntentToolChainService) Create(ctx context.Context, in ChainUpsertInput) (*model.IntentToolChain, error) {
	if err := validateChainUpsert(in); err != nil {
		return nil, err
	}
	if existing, err := s.repo.GetByIntent(ctx, in.IntentCode); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("意图 %q 已绑定工具链，请编辑既有配置", in.IntentCode))
	}
	rec := &model.IntentToolChain{
		Name:       in.Name,
		IntentCode: in.IntentCode,
		StepsJSON:  marshalChainSteps(in.Steps),
		ReplyHint:  in.ReplyHint,
		Enabled:    in.Enabled == nil || *in.Enabled,
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		return nil, err
	}
	s.invalidateCache(rec.TenantID)
	return rec, nil
}

// Update 更新绑定（按 id；租户作用域在仓储层）。
func (s *IntentToolChainService) Update(ctx context.Context, id uint, in ChainUpsertInput) (*model.IntentToolChain, error) {
	if err := validateChainUpsert(in); err != nil {
		return nil, err
	}
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.IntentCode != in.IntentCode {
		if existing, err := s.repo.GetByIntent(ctx, in.IntentCode); err != nil {
			return nil, err
		} else if existing != nil && existing.ID != rec.ID {
			return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("意图 %q 已绑定工具链，请编辑既有配置", in.IntentCode))
		}
	}
	rec.Name = in.Name
	rec.IntentCode = in.IntentCode
	rec.StepsJSON = marshalChainSteps(in.Steps)
	rec.ReplyHint = in.ReplyHint
	if in.Enabled != nil {
		rec.Enabled = *in.Enabled
	}
	if err := s.repo.Update(ctx, rec); err != nil {
		return nil, err
	}
	s.invalidateCache(rec.TenantID)
	return rec, nil
}

// Delete 删除绑定（软删）。
func (s *IntentToolChainService) Delete(ctx context.Context, id uint) error {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateCache(rec.TenantID)
	return nil
}

// Get 单条详情。
func (s *IntentToolChainService) Get(ctx context.Context, id uint) (*model.IntentToolChain, error) {
	return s.repo.GetByID(ctx, id)
}

// List 分页清单（intentCode 可选过滤）。
func (s *IntentToolChainService) List(ctx context.Context, intentCode string, page, pageSize int) ([]model.IntentToolChain, int64, error) {
	return s.repo.List(ctx, intentCode, page, pageSize)
}

// ---- 校验与序列化 ----

func validateChainUpsert(in ChainUpsertInput) error {
	if !validChainIntents[in.IntentCode] {
		return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("intent_code %q 不在可绑定意图词表内（可用: %s）",
			in.IntentCode, joinChainVocab()))
	}
	if len(in.Steps) == 0 || len(in.Steps) > agent_runtime.ChainMaxSteps {
		return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("工具链步骤数须在 1~%d 之间", agent_runtime.ChainMaxSteps))
	}
	seen := make(map[string]bool, len(in.Steps))
	for i, st := range in.Steps {
		name := trimChainToolName(st.ToolName)
		if name == "" {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("第 %d 步 tool_name 不能为空", i+1))
		}
		if len(name) > 100 {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("第 %d 步 tool_name 超长（≤100）", i+1))
		}
		if seen[name] {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("工具 %q 在链中重复", name))
		}
		seen[name] = true
		if err := agent_runtime.ValidateChainArgs(st.Args); err != nil {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("第 %d 步: %v", i+1, err))
		}
	}
	return nil
}

func marshalChainSteps(steps []model.IntentChainStepConfig) string {
	buf, err := json.Marshal(steps)
	if err != nil {
		// validateChainUpsert 已保证结构合法；序列化失败按参数错误处理
		return "[]"
	}
	return string(buf)
}

func trimChainToolName(s string) string {
	// 与 planner_chain.go 的 TrimSpace 判定同源：空工具名整链拒用，这里写侧先拒
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func joinChainVocab() string {
	out := ""
	for i, v := range ChainIntentVocabulary() {
		if i > 0 {
			out += "/"
		}
		out += v
	}
	return out
}

// ChainVocabulary vocab 端点数据：意图词表 + 模板变量白名单 + 注册中心工具名清单。
type ChainVocabulary struct {
	Intents      []string `json:"intents"`
	TemplateVars []string `json:"template_vars"`
	Tools        []string `json:"tools"`
}

// GetChainVocabulary 管理端配置页取词表/变量/工具三项（单一事实源出口）。
func (s *IntentToolChainService) GetChainVocabulary() ChainVocabulary {
	tools := []string{}
	if reg := tooluse.GetGlobalRegistry(); reg != nil {
		tools = reg.ListNames()
	}
	return ChainVocabulary{
		Intents:      ChainIntentVocabulary(),
		TemplateVars: agent_runtime.ChainTemplateVarNames(),
		Tools:        tools,
	}
}
