package tooluse

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrToolNotFound 工具未找到
var ErrToolNotFound = errors.New("tool not found")

// ErrToolAlreadyExists 工具已存在
var ErrToolAlreadyExists = errors.New("tool already exists")

// ToolRegistry 工具注册中心
type ToolRegistry struct {
	tools      map[string]Tool
	versions   map[string]uint64
	generation uint64
	mu         sync.RWMutex
}

// toolSnapshot 将实例、来源与登记代际在同一读锁内绑定。
// 工具登记后按只读对象使用；更新配置需要重新登记，不能并发原地修改。
type toolSnapshot struct {
	tool       Tool
	registry   *ToolRegistry
	name       string
	generation uint64
}

// NewToolRegistry 创建新的工具注册中心
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:    make(map[string]Tool),
		versions: make(map[string]uint64),
	}
}

// Register 注册工具
// 同名工具重复注册返回 ErrToolAlreadyExists
func (r *ToolRegistry) Register(t Tool) error {
	return r.register(t, false)
}

// Replace 原子登记当前工具代际，重发现不会暴露注销与重建之间的窗口。
func (r *ToolRegistry) Replace(t Tool) error {
	return r.register(t, true)
}

// register 在一个写锁内完成同名替换；删除重建也不会复用旧代际。
func (r *ToolRegistry) register(t Tool, replace bool) error {
	if t == nil {
		return errors.New("cannot register nil tool")
	}
	name := t.Name()
	if name == "" {
		return errors.New("tool name cannot be empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists && !replace {
		return fmt.Errorf("%w: %s", ErrToolAlreadyExists, name)
	}
	r.generation++
	r.tools[name] = t
	r.versions[name] = r.generation
	return nil
}

// MustRegister 注册工具，出错 panic（用于初始化时）
func (r *ToolRegistry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(fmt.Sprintf("注册工具 %s 失败：%v", t.Name(), err))
	}
}

// Unregister 注销工具
func (r *ToolRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; !exists {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	delete(r.tools, name)
	delete(r.versions, name)
	return nil
}

// Get 按 name 获取工具
func (r *ToolRegistry) Get(name string) (Tool, error) {
	snapshot, err := r.getSnapshot(name)
	return snapshot.tool, err
}

func (r *ToolRegistry) getSnapshot(name string) (toolSnapshot, error) {
	if r == nil {
		return toolSnapshot{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, exists := r.tools[name]
	if !exists {
		return toolSnapshot{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return toolSnapshot{tool: t, registry: r, name: name, generation: r.versions[name]}, nil
}

// Has 判断工具是否存在
func (r *ToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.tools[name]
	return exists
}

// List 列出所有工具（按 name 排序）
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name() < list[j].Name()
	})
	return list
}

// ListByCategory 按 category 列出工具（按 name 排序）
func (r *ToolRegistry) ListByCategory(category ToolCategory) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Tool, 0)
	for _, t := range r.tools {
		if t.Category() == category {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name() < list[j].Name()
	})
	return list
}

// Count 返回已注册工具数量
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// CountByCategory 按 category 统计工具数量
func (r *ToolRegistry) CountByCategory(category ToolCategory) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, t := range r.tools {
		if t.Category() == category {
			count++
		}
	}
	return count
}

// ToLLMFunctions 导出所有工具为 LLM Function Calling 格式
func (r *ToolRegistry) ToLLMFunctions() []LLMFunction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]LLMFunction, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, ToLLMFunction(t))
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// ---------- 场景→工具类别白名单 ----------
//
// 场景裁剪映射表集中在此一处维护：
//   - intent_recognize 类场景只暴露 knowledge/customer 类工具（降低 prompt 体积+误触面）
//   - sales/cs 等业务场景全量暴露
//   - 未列出的场景默认全量（向后兼容）
const ScenarioIntentRecognize = "intent_recognize"

var scenarioToolWhitelist = map[string][]ToolCategory{
	ScenarioIntentRecognize: {CategoryKnowledge, CategoryCustomer},
}

// ScenarioAllowedCategories 返回场景允许的工具类别。
// restricted=false 表示该场景无裁剪配置（调用方应全量放行）。
func ScenarioAllowedCategories(scenario string) ([]ToolCategory, bool) {
	cats, ok := scenarioToolWhitelist[scenario]
	return cats, ok && len(cats) > 0
}

// ScenarioAllowsTool 判断指定工具是否允许在场景中暴露
// （无类别映射的工具按"不在白名单即隐藏"处理；场景未配置则全量放行）
func ScenarioAllowsTool(scenario string, toolName string) bool {
	cats, restricted := ScenarioAllowedCategories(scenario)
	if !restricted {
		return true
	}
	t, err := GetGlobalRegistry().Get(toolName)
	if err != nil {
		return false
	}
	for _, c := range cats {
		if t.Category() == c {
			return true
		}
	}
	return false
}

// ToLLMFunctionsForScenario 按场景白名单裁剪导出 LLM Function Calling 格式。
// 场景未配置白名单时等价于 ToLLMFunctions（全量，向后兼容）。
func (r *ToolRegistry) ToLLMFunctionsForScenario(scenario string) []LLMFunction {
	cats, restricted := ScenarioAllowedCategories(scenario)
	if !restricted {
		return r.ToLLMFunctions()
	}
	allowed := make(map[ToolCategory]bool, len(cats))
	for _, c := range cats {
		allowed[c] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]LLMFunction, 0)
	for _, t := range r.tools {
		if allowed[t.Category()] {
			list = append(list, ToLLMFunction(t))
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// ToLLMFunctionsByCategory 按 category 导出 LLM Function Calling 格式
func (r *ToolRegistry) ToLLMFunctionsByCategory(category ToolCategory) []LLMFunction {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]LLMFunction, 0)
	for _, t := range r.tools {
		if t.Category() == category {
			list = append(list, ToLLMFunction(t))
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

// ListNames 列出所有工具名称（按 name 排序）
func (r *ToolRegistry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var (
	globalRegistry     *ToolRegistry
	globalRegistryOnce sync.Once
)

// GetGlobalRegistry 获取全局工具注册中心
func GetGlobalRegistry() *ToolRegistry {
	globalRegistryOnce.Do(func() {
		globalRegistry = NewToolRegistry()
	})
	return globalRegistry
}

// RegisterGlobal 注册到全局注册中心
func RegisterGlobal(t Tool) error {
	return GetGlobalRegistry().Register(t)
}

// MustRegisterGlobal 注册到全局注册中心，出错 panic
func MustRegisterGlobal(t Tool) {
	GetGlobalRegistry().MustRegister(t)
}
