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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/connector"
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

// validChainServiceIntents service 家族意图词表（CS 主链路 intent.Type 消费方，
// TB-03 扩容；编译期引用 service 常量）。重叠键（greeting/complaint/unknown）语义一致。
var validChainServiceIntents = map[string]bool{
	IntentPriceInquiry:        true,
	IntentObjectionPrice:      true,
	IntentObjectionNeed:       true,
	IntentObjectionTrust:      true,
	IntentObjectionCompetitor: true,
	IntentObjectionTiming:     true,
	IntentPurchase:            true,
	IntentAskProduct:          true,
	IntentAskService:          true,
	IntentAfterSale:           true,
	IntentChurn:               true,
	IntentSocial:              true,
	IntentGreeting:            true,
	IntentComplaint:           true,
	IntentUnknown:             true,
	IntentClarify:             true,
}

// isValidChainIntent 双家族校验：任一家族命中即可绑定。
func isValidChainIntent(code string) bool {
	return validChainIntents[code] || validChainServiceIntents[code]
}

// ChainIntentVocabulary 返回可绑定意图清单（升序、双家族并集；供管理端 UI 与 vocab 端点）。
func ChainIntentVocabulary() []string {
	names := make([]string, 0, len(validChainIntents)+len(validChainServiceIntents))
	for name := range validChainIntents {
		names = append(names, name)
	}
	for name := range validChainServiceIntents {
		if !validChainIntents[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ChainIntentFamily 键→家族归属（"runtime"/"service"/"both"；vocab 端点分组展示用）。
func ChainIntentFamily(code string) string {
	inRuntime := validChainIntents[code]
	inService := validChainServiceIntents[code]
	switch {
	case inRuntime && inService:
		return "both"
	case inRuntime:
		return "runtime"
	case inService:
		return "service"
	default:
		return ""
	}
}

// IntentToolChainService 意图→工具链绑定服务。
type IntentToolChainService struct {
	repo repository.IntentToolChainRepository

	// customIntents 自定义意图词条服务（T-09 动态放行面）；nil=仅静态双家族词表。
	customIntents *CustomIntentService

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
	// 缓存命中后仍复验可绑定面（T-09 动态放行）：自定义词条被删除时立即 fail-closed
	// （其缓存随词条写失效）；新建词条的绑定行反向生效走 30s TTL（缓存装载时放行）。
	if spec, ok := s.cachedSpec(tenantID, intent); ok && s.isBindableIntent(ctx, intent) {
		return spec, nil
	}
	rows, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	specs := make(map[string]*agent_runtime.IntentChainSpec, len(rows))
	for i := range rows {
		spec := s.chainRowToSpec(ctx, &rows[i])
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
// chainRowToSpec 行→runtime 契约映射；解析失败/意图不在可绑定面返回 nil（fail-closed 跳过）。
// 可绑定面=静态双家族 ∪ 本租户启用中的自定义词条（T-09 动态放行）。
func (s *IntentToolChainService) chainRowToSpec(ctx context.Context, rec *model.IntentToolChain) *agent_runtime.IntentChainSpec {
	if rec == nil || !s.isBindableIntent(ctx, rec.IntentCode) {
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
	for _, sl := range unmarshalChainSlots(rec.SlotsJSON) {
		spec.Slots = append(spec.Slots, agent_runtime.IntentChainSlot{
			Slot:      sl.Slot,
			Question:  sl.Question,
			SourceVar: sl.SourceVar,
			Required:  sl.Required,
		})
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
	// Slots 必填槽位（TB-08）：集齐 required 前严格模式 0 工具调用+追问客户。
	Slots     []model.IntentSlotConfig `json:"slots"`
	ReplyHint string                   `json:"reply_hint"`
	Enabled   *bool                    `json:"enabled"`
}

// Create 新建绑定。校验：意图词表/步骤 1~N/工具名非空/模板变量白名单预检；
// 同租户同意图唯一（软删行不占用：写侧显式查重）。
func (s *IntentToolChainService) Create(ctx context.Context, in ChainUpsertInput) (*model.IntentToolChain, error) {
	if err := s.validateChainUpsert(ctx, in); err != nil {
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
		SlotsJSON:  marshalChainSlots(in.Slots),
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
	if err := s.validateChainUpsert(ctx, in); err != nil {
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
	rec.SlotsJSON = marshalChainSlots(in.Slots)
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

func (s *IntentToolChainService) validateChainUpsert(ctx context.Context, in ChainUpsertInput) error {
	if !s.isBindableIntent(ctx, in.IntentCode) {
		return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("intent_code %q 不在可绑定意图词表内（可用: %s）",
			in.IntentCode, joinChainVocab()))
	}
	if len(in.Steps) == 0 || len(in.Steps) > agent_runtime.ChainMaxSteps {
		return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("工具链步骤数须在 1~%d 之间", agent_runtime.ChainMaxSteps))
	}
	seen := make(map[string]bool, len(in.Steps))
	if err := validateChainSlots(in.Slots); err != nil {
		return err
	}
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

// ChainVocabulary vocab 端点数据：意图词表（含家族归属）+ 模板变量白名单 + 注册中心工具名清单
// +连接器方言清单（TB-06b：插件清单面）。
type ChainVocabulary struct {
	Intents           []string          `json:"intents"`
	IntentFamilies    map[string]string `json:"intent_families"`
	TemplateVars      []string          `json:"template_vars"`
	Tools             []string          `json:"tools"`
	ConnectorDialects []string          `json:"connector_dialects"`
}

// GetChainVocabulary 管理端配置页取词表/变量/工具三项（单一事实源出口）。
// 意图清单 = 统一意图目录（TB-10）优先：catalog 行 family 列直出（custom 行
// family=custom）；目录未装配/未播种/读失败 → 回落静态双家族 ∪ 本租户启用中的
// 自定义词条（family=custom，T-09）。
func (s *IntentToolChainService) GetChainVocabulary(ctx context.Context) ChainVocabulary {
	tools := []string{}
	if reg := tooluse.GetGlobalRegistry(); reg != nil {
		tools = reg.ListNames()
	}
	families := make(map[string]string)
	var intents []string
	if cat := GetIntentCatalogService(); cat != nil {
		if rows := cat.loadCatalogForVocab(ctx); len(rows) > 0 {
			for _, row := range rows {
				intents = append(intents, row.IntentKey)
				families[row.IntentKey] = row.Family
			}
		}
	}
	if len(intents) == 0 {
		intents = ChainIntentVocabulary()
		for _, code := range intents {
			families[code] = ChainIntentFamily(code)
		}
		if s.customIntents != nil {
			for _, def := range s.customIntents.EffectiveCustomIntentDefs(ctx) {
				if _, dup := families[def.Type]; dup {
					continue
				}
				intents = append(intents, def.Type)
				families[def.Type] = "custom"
			}
		}
	}
	return ChainVocabulary{
		Intents:           intents,
		IntentFamilies:    families,
		TemplateVars:      agent_runtime.ChainTemplateVarNames(),
		Tools:             tools,
		ConnectorDialects: connectorList(),
	}
}

// ---- CS 主链路消费（TB-03）：工具晋升 + 调用流提示 ----

// promoteIntentChainTools 绑定命中的工具 stable 前置（其余保序），保证过 maxTools
// 截断后绑定链仍完整在列；不删任何既有工具，仅重排。
func promoteIntentChainTools(tools []AgentToolDef, spec *agent_runtime.IntentChainSpec) []AgentToolDef {
	if spec == nil || len(spec.Steps) == 0 || len(tools) == 0 {
		return tools
	}
	rank := make(map[string]int, len(spec.Steps))
	order := 0
	for _, st := range spec.Steps {
		if st.Enabled && st.ToolName != "" {
			if _, seen := rank[st.ToolName]; !seen {
				rank[st.ToolName] = order
				order++
			}
		}
	}
	if len(rank) == 0 {
		return tools
	}
	promoted := make([]AgentToolDef, 0, len(tools))
	rest := make([]AgentToolDef, 0, len(tools))
	for _, t := range tools {
		if _, hit := rank[t.Name]; hit {
			promoted = append(promoted, t)
		} else {
			rest = append(rest, t)
		}
	}
	sort.SliceStable(promoted, func(i, j int) bool {
		return rank[promoted[i].Name] < rank[promoted[j].Name]
	})
	return append(promoted, rest...)
}

// renderIntentChainHint 绑定命中时注入系统提示的"按链优先调用"指引。
// 只引导不强制：LLM 仍自主决策；未命中=空串（提示零变化）。
func renderIntentChainHint(spec *agent_runtime.IntentChainSpec) string {
	if spec == nil || len(spec.Steps) == 0 {
		return ""
	}
	names := make([]string, 0, len(spec.Steps))
	for _, st := range spec.Steps {
		if st.Enabled && st.ToolName != "" {
			names = append(names, st.ToolName)
		}
	}
	if len(names) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n- 已识别的客户意图绑定了工具调用流（按此顺序优先调用，拿到结果后再综合作答；工具不适用时可跳过并说明原因）：")
	sb.WriteString(strings.Join(names, " → "))
	sb.WriteString("\n")
	return sb.String()
}

// ---- 默认种子（TB-03）：只读链 Enabled=true；写链类动作禁止出现在种子里 ----

// defaultIntentChainSeeds 只读查询链种子：快递/物流查询与售后查询主载。
// 步骤全部是 RiskReadonly 工具（order.lookup/logistics.track），无写动作。
func defaultIntentChainSeeds() []ChainUpsertInput {
	enabled := true
	return []ChainUpsertInput{
		{
			Name:       "快递/物流查询调用流（默认）",
			IntentCode: IntentAskService,
			Steps: []model.IntentChainStepConfig{
				{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 1, Enabled: true},
				{ToolName: "logistics.track", Args: map[string]any{"channel_type": "{{channel_type}}"}, Priority: 2, Enabled: true},
			},
			ReplyHint: "客户询问快递/物流。优先调 order.lookup 关联客户订单，再调 logistics.track 查询轨迹；查不到运单号时礼貌引导客户提供，不要编造轨迹。",
			Enabled:   &enabled,
		},
		{
			Name:       "售后查询调用流（默认）",
			IntentCode: IntentAfterSale,
			Steps: []model.IntentChainStepConfig{
				{ToolName: "order.lookup", Args: map[string]any{"customer_id": "{{customer_id}}"}, Priority: 1, Enabled: true},
			},
			ReplyHint: "客户有售后诉求。先调 order.lookup 关联订单掌握上下文，再按售后政策答复；无法处理时按转人工口径引导。",
			Enabled:   &enabled,
		},
	}
}

// SeedDefaultIntentChains 幂等种子：同租户同意图已存在任何绑定（含禁用）即跳过；
// 只写 DefaultTenantID（多租户其他租户经 UI 自配）。返回新建条数。
func (s *IntentToolChainService) SeedDefaultIntentChains(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	created := 0
	for _, in := range defaultIntentChainSeeds() {
		existing, err := s.repo.GetByIntent(ctx, in.IntentCode)
		if err != nil {
			return created, err
		}
		if existing != nil {
			continue
		}
		rec := &model.IntentToolChain{
			Name:       in.Name,
			IntentCode: in.IntentCode,
			StepsJSON:  marshalChainSteps(in.Steps),
			ReplyHint:  in.ReplyHint,
			Enabled:    in.Enabled != nil && *in.Enabled,
		}
		if err := s.repo.Create(ctx, rec); err != nil {
			return created, err
		}
		created++
	}
	if created > 0 {
		s.invalidateCache(chainTenantID(ctx))
	}
	return created, nil
}

// SetCustomIntentService 注入自定义意图词条服务（T-09）：绑定校验与行映射对
// service 家族的自定义意图 type 动态放行；nil=仅静态词表（TB-01 行为不变）。
func (s *IntentToolChainService) SetCustomIntentService(svc *CustomIntentService) {
	s.customIntents = svc
}

// isBindableIntent 统一意图目录（TB-10）优先放行；目录未装配/未播种/读失败
// 回落：静态双家族 OR 动态自定义词条（T-09）。
func (s *IntentToolChainService) isBindableIntent(ctx context.Context, code string) bool {
	if cat := GetIntentCatalogService(); cat != nil {
		if cat.IsCatalogIntent(ctx, code) {
			return true
		}
	}
	if isValidChainIntent(code) {
		return true
	}
	if s.customIntents != nil {
		return s.customIntents.IsCustomIntentType(ctx, code)
	}
	return false
}

// ---- TB-08 槽位：校验与映射 ----

// chainSlotSourceContextVars 槽位来源变量的上下文白名单（entity.* 之外的形态）。
var chainSlotSourceContextVars = map[string]bool{"customer_id": true, "session_id": true, "content": true}

// validateChainSlots 槽位形态校验：slot/question 非空、SourceVar = entity.<key> 或白名单变量、
// slot 名不重复、步数上限复用（≤8）。空 Slots = 无槽位门（既有行为）。
func validateChainSlots(slots []model.IntentSlotConfig) error {
	if len(slots) == 0 {
		return nil
	}
	if len(slots) > agent_runtime.ChainMaxSteps {
		return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("槽位数上限 %d", agent_runtime.ChainMaxSteps))
	}
	seen := make(map[string]bool, len(slots))
	for i, sl := range slots {
		slot := trimChainToolName(sl.Slot)
		if slot == "" {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("第 %d 个槽位 slot 不能为空", i+1))
		}
		if seen[slot] {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("槽位 %q 重复", slot))
		}
		seen[slot] = true
		if strings.TrimSpace(sl.Question) == "" {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("槽位 %q 缺少追问话术", slot))
		}
		if !isValidSlotSourceVar(sl.SourceVar) {
			return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("槽位 %q 的 source_var %q 不合法（entity.<key> 或 %s）",
				slot, sl.SourceVar, joinSlotContextVars()))
		}
	}
	return nil
}

func isValidSlotSourceVar(sv string) bool {
	if chainSlotSourceContextVars[sv] {
		return true
	}
	if strings.HasPrefix(sv, "entity.") && len(sv) > len("entity.") {
		return customIntentTypeRe.MatchString(strings.TrimPrefix(sv, "entity.")) || entityKeyRe.MatchString(strings.TrimPrefix(sv, "entity."))
	}
	return false
}

// entityKeyRe 实体键形态（字母/数字/下划线）。
var entityKeyRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,49}$`)

func joinSlotContextVars() string {
	keys := make([]string, 0, len(chainSlotSourceContextVars))
	for k := range chainSlotSourceContextVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, "/") + "/entity.<key>"
}

func marshalChainSlots(slots []model.IntentSlotConfig) string {
	if len(slots) == 0 {
		return ""
	}
	buf, err := json.Marshal(slots)
	if err != nil {
		return ""
	}
	return string(buf)
}

// unmarshalChainSlots 行→槽位（解析失败 fail-closed 跳过=无槽位门）。
func unmarshalChainSlots(raw string) []model.IntentSlotConfig {
	if raw == "" {
		return nil
	}
	var slots []model.IntentSlotConfig
	if err := json.Unmarshal([]byte(raw), &slots); err != nil {
		return nil
	}
	return slots
}

// connectorList 连接器方言清单（TB-06b 插件清单面）；connector 包未装配时返回空。
func connectorList() []string {
	return connector.List()
}
