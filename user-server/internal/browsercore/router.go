package browsercore

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Router 统一执行端口：按 engine 选腿。
//
// 设计纪律：
//   - auto 的优先级固定可测（ego 优先，nm 兜底），不做"猜"；
//   - 两腿都不可用 → 明确错误，绝不静默换腿（否则用户以为跑在 ego，实际跑了别的浏览器）；
//   - Router 只持有 Executor 接口，不知道任何一腿的内部类型。
type Router struct {
	mu    sync.RWMutex
	execs map[string]Executor
	order []string
}

// NewRouter 创建路由器（默认 auto 顺序 ego → nm）
func NewRouter() *Router {
	return &Router{execs: make(map[string]Executor), order: []string{EngineEgo, EngineNM}}
}

// Register 注册一条执行腿（同 engine 重复注册视为装配错误）。
func (r *Router) Register(e Executor) error {
	if e == nil || e.Engine() == "" {
		return fmt.Errorf("注册执行腿失败：engine 为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.execs[e.Engine()]; ok {
		return fmt.Errorf("执行腿重复注册: %s", e.Engine())
	}
	r.execs[e.Engine()] = e
	return nil
}

// Pick 按 engine 取执行腿；engine=auto 按 order 取第一个已注册的。
func (r *Router) Pick(engine string) (Executor, error) {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		engine = EngineAuto
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if engine != EngineAuto {
		if e, ok := r.execs[engine]; ok {
			return e, nil
		}
		return nil, fmt.Errorf("%w: engine=%s", ErrEngineUnavailable, engine)
	}
	for _, name := range r.order {
		if e, ok := r.execs[name]; ok {
			return e, nil
		}
	}
	return nil, fmt.Errorf("%w: 没有任何已注册的执行腿", ErrEngineUnavailable)
}

// Engines 已注册引擎（健康检查/能力探测用）
func (r *Router) Engines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.execs))
	for name := range r.execs {
		out = append(out, name)
	}
	return out
}

// NormalizeOpen 归一 engine/mode/space_name，并给策略填缺省。
func NormalizeOpen(req *OpenRequest) {
	req.Engine = strings.ToLower(strings.TrimSpace(req.Engine))
	switch req.Engine {
	case EngineEgo, EngineNM, EngineAuto:
	default:
		req.Engine = EngineAuto
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != ModeScript {
		req.Mode = ModeOps
	}
	if strings.TrimSpace(req.SpaceName) == "" {
		req.SpaceName = fmt.Sprintf("hivemtk-%d-%d", req.UserID, time.Now().UnixNano())
	}
	if req.CreatedFrom == "" {
		req.CreatedFrom = "api"
	}
	req.Policy.Normalize()
}
