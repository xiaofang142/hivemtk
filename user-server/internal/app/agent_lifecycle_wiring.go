package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"hivemtk-user/internal/aiagent/agent/lifecycle"
	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 本文件兑现「双模式」的另一半：把 lifecycle 装配起来，并**按 model.AgentMode 分派**（W-4）。
//
// 在此之前，`lifecycle.Resolver` 与 `AgentMode` 两侧字段都存在，但
//   - `AIAgentService.LoadContext` 组装上下文时不带模式（读出来恒为空串），
//   - 全项目没有任何一处按模式选过实现，
//
// 于是"active/passive"在运行期是零分派、零入口的名义字段。本文件把这两截接上：
// 上下文带模式（service 侧，见 `TestAIAgentService_LoadContextCarriesAgentMode`）＋
// 这里的分派与 HTTP 入口（`POST /api/agent/lifecycle/run`）。
//
// 注意：本文件**不接管**既有渠道入站链路。渠道消息仍走 `SmartCSOrchestrator` →
// `SalesEngine.HandleWithAgent`（AC③「Passive 行为零变化」靠的就是这一点：被动只是
// 多了一个可选入口，原有那条路一行未动）。

// AgentContextLoader 按智能体 ID 读执行上下文（`*service.AIAgentService.LoadContext` 天然满足）。
//
// 契约里最容易踩的一格：智能体不存在或已禁用时回的是 `(nil, nil)` 而不是错误，
// 所以分派前必须自己判空 —— 没有上下文就没有模式。
type AgentContextLoader interface {
	LoadContext(ctx context.Context, agentID uint) (*dto.AgentContext, error)
}

// AgentLifecycleRuntime 双模式运行时：一台智能体的一次运行该走哪条生命周期。
//
// 选择函数用的是 `lifecycle.Resolver` —— 它自写下起一直零调用方，本卡就是来接它的
// （模式比对的字面量因此只有一处定义，不在 app 层再抄一遍）。
type AgentLifecycleRuntime struct {
	loader  AgentContextLoader
	resolve func(mode string) lifecycle.AgentLifecycle
}

// NewAgentLifecycleRuntime 构造运行时。active 允许为 nil（依赖没配齐时按被动跑，见 LifecycleFor）。
func NewAgentLifecycleRuntime(loader AgentContextLoader, passive, active lifecycle.AgentLifecycle) *AgentLifecycleRuntime {
	return &AgentLifecycleRuntime{loader: loader, resolve: lifecycle.Resolver(passive, active)}
}

// LifecycleFor 按 agent_mode 选实现：只有明确写着 active（忽略大小写与空白）且主动已装配时
// 才走 Active，其余一切 —— passive、空串、历史脏值、拼错的模式 —— 回退 Passive。
//
// 回退方向是有意的：被动只答一条消息，主动会对外发消息。反过来（未知即主动）等于
// 运营在一个字段上填错字就对外群发。
func (r *AgentLifecycleRuntime) LifecycleFor(mode string) lifecycle.AgentLifecycle {
	if r == nil || r.resolve == nil {
		return nil
	}
	// Resolver 比的是字面量，脏值先归一（'  Active ' 与 'ACTIVE' 都是历史行可能长成的样子）。
	return r.resolve(strings.ToLower(strings.TrimSpace(mode)))
}

// Run 读上下文拿模式 → 分派 → 执行，并把**实际走了哪条**写回结果。
//
// Mode 必须回显：运营要能分辨"它真跑了主动"与"它悄悄回退成被动了"，
// 这两件事在只返回成功/失败时长得一模一样。
func (r *AgentLifecycleRuntime) Run(ctx context.Context, agentID uint, req *lifecycle.LifecycleRequest) (*lifecycle.LifecycleResult, error) {
	if r == nil || r.loader == nil {
		return nil, errors.New("agent lifecycle: 上下文加载器未装配")
	}
	agentCtx, err := r.loader.LoadContext(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent lifecycle: 读取智能体 %d 上下文失败: %w", agentID, err)
	}
	if agentCtx == nil {
		return nil, fmt.Errorf("agent lifecycle: 智能体 %d 不存在或已禁用，没有 agent_mode 可分派", agentID)
	}
	lc := r.LifecycleFor(agentCtx.AgentMode)
	if lc == nil {
		return nil, fmt.Errorf("agent lifecycle: 智能体 %d 的模式 %q 无可用的生命周期实现（被动也未装配）", agentID, agentCtx.AgentMode)
	}
	res, err := lc.Run(ctx, agentCtx, req)
	if err != nil {
		// 原样带上走的是哪条，但不吞掉原因：主动入口的每一条拒绝都要让运营读得懂，
		// 因为它的反面是一次已经发出去的外联。
		return nil, fmt.Errorf("agent lifecycle(%s): %w", lc.Mode(), err)
	}
	if res == nil {
		return nil, fmt.Errorf("agent lifecycle(%s): 返回了空结果", lc.Mode())
	}
	if res.Mode == "" {
		res.Mode = lc.Mode()
	}
	return res, nil
}

var (
	// 编译期锁：真实的三个依赖必须满足 lifecycle 包自己声明的窄接口。
	// 一旦签名漂移（例如仓层 GetByID 改了返回形状），这里先编译不过，
	// 而不是等到运行期"装配失败"退成被动、日志里才看得见。
	_ lifecycle.ConversationRunner = (*service.SalesEngine)(nil)
	_ lifecycle.SOPExecutor        = (*service.SOPService)(nil)
	_ lifecycle.CustomerLookup     = (repository.CustomerRepository)(nil)
)

// InitAgentLifecycles 装配全局双模式运行时。
//
// 调用方：router.Setup()，在 `BuildSalesEngine` 之后（被动那条复用同一个会话引擎实例）。
// 依赖为空时不 panic、也不塞一个半空运行时进去：只记一条 WARN，全局保持未装配，
// 于是下面那个端点回 503，而不是回一个看起来成功的空结果。
func InitAgentLifecycles(gormDB *gorm.DB, engine *service.SalesEngine) {
	if gormDB == nil || engine == nil {
		logger.Warn("[agent-lifecycle] ⚠️ DB 句柄或会话引擎为空，双模式运行时未装配（/api/agent/lifecycle/run 将回 503）")
		return
	}
	sopSvc := service.GetSOPService()
	if sopSvc == nil {
		// 优先复用全局单例（与 /api/sop* 路由同一份状态）；启动顺序上拿不到时自建一份，
		// 与 service_routes.go 里既有的 NewSOPService(db, nil) 同一口径。
		sopSvc = service.NewSOPService(gormDB, llm.GetGlobalDispatcher())
	}
	rt := NewAgentLifecycleRuntime(
		service.NewAIAgentServiceWithDB(gormDB),
		lifecycle.NewPassiveAgentLifecycle(engine),
		lifecycle.NewActiveAgentLifecycle(sopSvc, repository.NewCustomerRepository()),
	)
	SetAgentLifecycleRuntime(rt)
	logger.Info("[agent-lifecycle] ✅ 双模式生命周期已装配（passive=会话引擎 / active=SOP 编排），按 agent_mode 分派")
}

var globalAgentLifecycleRuntime *AgentLifecycleRuntime

// SetAgentLifecycleRuntime 注入全局双模式运行时；传 nil 撤销装配（测试还原用）。
func SetAgentLifecycleRuntime(rt *AgentLifecycleRuntime) {
	globalAgentLifecycleRuntime = rt
}

// GetAgentLifecycleRuntime 取全局运行时，未装配时为 nil。
func GetAgentLifecycleRuntime() *AgentLifecycleRuntime {
	return globalAgentLifecycleRuntime
}

// agentLifecycleRunRequest 触发一次生命周期运行。
//
// content 故意不要求：主动模式没有入站消息（它按客户开工），要求它就把 Active 挡在门外了。
type agentLifecycleRunRequest struct {
	AgentID    uint   `json:"agent_id" binding:"required"`
	CustomerID string `json:"customer_id" binding:"required"`
	Content    string `json:"content"`
	SessionID  string `json:"session_id"`
	Channel    string `json:"channel"`
	OneID      string `json:"one_id"`
	TraceID    string `json:"trace_id"`
}

// SetupAgentLifecycleRoutes 注册智能体双模式运行入口。
//
// 端点：
//   - POST /api/agent/lifecycle/run  按 agent_id 读出 agent_mode 并分派一次运行
//
// 调用方：router.Setup() 的 auth 路由组（与 SetupInferenceRoutes 同一位置）。
func SetupAgentLifecycleRoutes(auth *gin.RouterGroup) {
	auth.POST("/agent/lifecycle/run", handleAgentLifecycleRun)
}

func handleAgentLifecycleRun(c *gin.Context) {
	rt := GetAgentLifecycleRuntime()
	if rt == nil {
		response.Error(c, http.StatusServiceUnavailable, "agent lifecycle runtime not assembled（双模式运行时未装配）")
		return
	}
	var body agentLifecycleRunRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	res, err := rt.Run(c.Request.Context(), body.AgentID, &lifecycle.LifecycleRequest{
		Channel:    body.Channel,
		CustomerID: body.CustomerID,
		Content:    body.Content,
		SessionID:  body.SessionID,
		OneID:      body.OneID,
		TraceID:    body.TraceID,
	})
	if err != nil {
		logger.Errorf("[agent-lifecycle] run failed agent=%d customer=%s: %v", body.AgentID, body.CustomerID, err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"mode":         res.Mode,
		"execution_id": res.ExecutionID,
		"one_id":       res.OneID,
		"reply":        res.ReplyContent,
		"handoff":      res.Handoff,
		"stop_reason":  res.StopReason,
		"tools_called": res.ToolsCalled,
	}, "ok")
}
