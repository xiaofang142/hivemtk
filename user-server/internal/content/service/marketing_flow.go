package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	cdprepo "hivemtk-user/internal/repository"
	"runtime/debug"
	"strings"
	"time"
)

var smsSenderFunc func(ctx context.Context, phone, content string) error

func SetSmsSender(fn func(ctx context.Context, phone, content string) error) {
	smsSenderFunc = fn
}

type MarketingFlowService struct {
	flowRepo         *repository.MarketingFlowRepository
	executionRepo    *repository.FlowExecutionRepository
	userTagRepo      cdprepo.UserTagRepository
	orderRepo        cdprepo.OrderRepository
	clueRepo         cdprepo.ClueRepository
	sessionRepo      *cdprepo.CustomerSessionRepository
	agentRepo        *cdprepo.AgentStatusRepository
	operationLogRepo cdprepo.OperationLogRepository
}

// MarketingFlowDeps 营销流程服务的仓储依赖。
// 装配（仓储构造）由装配处完成：internal/router 直接填本结构体，service 层
// 不再持 *gorm.DB 形参（AV-04c）。
type MarketingFlowDeps struct {
	FlowRepo         *repository.MarketingFlowRepository
	ExecutionRepo    *repository.FlowExecutionRepository
	UserTagRepo      cdprepo.UserTagRepository
	OrderRepo        cdprepo.OrderRepository
	ClueRepo         cdprepo.ClueRepository
	SessionRepo      *cdprepo.CustomerSessionRepository
	AgentRepo        *cdprepo.AgentStatusRepository
	OperationLogRepo cdprepo.OperationLogRepository
}

// NewMarketingFlowService 创建营销流程服务（依赖由装配处注入）。
func NewMarketingFlowService(deps MarketingFlowDeps) *MarketingFlowService {
	return &MarketingFlowService{
		flowRepo:         deps.FlowRepo,
		executionRepo:    deps.ExecutionRepo,
		userTagRepo:      deps.UserTagRepo,
		orderRepo:        deps.OrderRepo,
		clueRepo:         deps.ClueRepo,
		sessionRepo:      deps.SessionRepo,
		agentRepo:        deps.AgentRepo,
		operationLogRepo: deps.OperationLogRepo,
	}
}

type CreateFlowRequest struct {
	Name          string                `json:"name" binding:"required"`
	Description   string                `json:"description"`
	TriggerType   model.TriggerType     `json:"trigger_type" binding:"required"`
	TriggerConfig map[string]any        `json:"trigger_config"`
	FlowData      *model.FlowDefinition `json:"flow_data"`
}

func (s *MarketingFlowService) CreateFlow(ctx context.Context, createdBy uint, req *CreateFlowRequest) (*model.MarketingFlow, error) {
	flowData, err := json.Marshal(req.FlowData)
	if err != nil {
		return nil, fmt.Errorf("%w: 流程定义序列化失败", utils.ErrInvalidInput)
	}

	if err := s.validateFlowDefinition(string(flowData)); err != nil {
		return nil, err
	}

	triggerConfig, _ := json.Marshal(req.TriggerConfig)

	flow := &model.MarketingFlow{
		Name:          req.Name,
		Description:   req.Description,
		Status:        model.FlowStatusDraft,
		TriggerType:   req.TriggerType,
		TriggerConfig: string(triggerConfig),
		FlowData:      string(flowData),
		Version:       1,
		CreatedBy:     createdBy,
	}

	if err := s.flowRepo.Create(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

func (s *MarketingFlowService) GetFlowList(ctx context.Context, page, pageSize int) ([]*model.MarketingFlow, int64, error) {
	return s.flowRepo.GetAll(ctx, page, pageSize)
}

func (s *MarketingFlowService) GetFlowByID(ctx context.Context, id uint) (*model.MarketingFlow, error) {
	return s.flowRepo.GetByID(ctx, id)
}

type UpdateFlowRequest struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	TriggerType   model.TriggerType     `json:"trigger_type"`
	TriggerConfig map[string]any        `json:"trigger_config"`
	FlowData      *model.FlowDefinition `json:"flow_data"`
}

func (s *MarketingFlowService) UpdateFlow(ctx context.Context, id uint, userID uint, isAdmin bool, req *UpdateFlowRequest) (*model.MarketingFlow, error) {
	flow, err := s.flowRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return nil, fmt.Errorf("%w: 无权限修改此营销流程", utils.ErrForbidden)
	}

	if req.Name != "" {
		flow.Name = req.Name
	}
	if req.Description != "" {
		flow.Description = req.Description
	}
	if req.TriggerType != "" {
		flow.TriggerType = req.TriggerType
	}
	if req.TriggerConfig != nil {
		triggerConfig, _ := json.Marshal(req.TriggerConfig)
		flow.TriggerConfig = string(triggerConfig)
	}
	if req.FlowData != nil {
		flowData, err := json.Marshal(req.FlowData)
		if err != nil {
			return nil, fmt.Errorf("%w: 流程定义序列化失败", utils.ErrInvalidInput)
		}
		if err := s.validateFlowDefinition(string(flowData)); err != nil {
			return nil, err
		}
		flow.FlowData = string(flowData)
	}

	flow.Version++
	if err := s.flowRepo.Update(ctx, flow); err != nil {
		return nil, err
	}

	return flow, nil
}

func (s *MarketingFlowService) DeleteFlow(ctx context.Context, id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return fmt.Errorf("%w: 无权限删除此营销流程", utils.ErrForbidden)
	}

	return s.flowRepo.Delete(ctx, id)
}

func (s *MarketingFlowService) ActivateFlow(ctx context.Context, id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return fmt.Errorf("%w: 无权限激活此营销流程", utils.ErrForbidden)
	}

	if err := s.validateFlowDefinition(flow.FlowData); err != nil {
		return err
	}

	return s.flowRepo.UpdateStatus(ctx, id, model.FlowStatusActive)
}

func (s *MarketingFlowService) PauseFlow(ctx context.Context, id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return fmt.Errorf("%w: 无权限暂停此营销流程", utils.ErrForbidden)
	}

	return s.flowRepo.UpdateStatus(ctx, id, model.FlowStatusPaused)
}

func (s *MarketingFlowService) StopFlow(ctx context.Context, id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return fmt.Errorf("%w: 无权限停止此营销流程", utils.ErrForbidden)
	}

	return s.flowRepo.UpdateStatus(ctx, id, model.FlowStatusInactive)
}

func (s *MarketingFlowService) validateFlowDefinition(flowData string) error {
	_, err := parseMarketingFlowGraph(flowData)
	return err
}

// GetExecutionList 某流程的执行记录列表。
//
// 先按父流程解一次租户：flow_executions 虽然自带 tenant_id，但「父流程不属于本租户」
// 与「流程不存在」必须走同一条 404，否则对别租户流程读子记录会回 200 空列表，
// 把越权探测的成败差异抹平。
func (s *MarketingFlowService) GetExecutionList(ctx context.Context, flowID uint, page, pageSize int) ([]*model.FlowExecution, int64, error) {
	if _, err := s.flowRepo.GetByID(ctx, flowID); err != nil {
		return nil, 0, err
	}
	return s.executionRepo.GetByFlowID(ctx, flowID, page, pageSize)
}

// GetExecutionStats 某流程的执行统计，租户口径同上。
func (s *MarketingFlowService) GetExecutionStats(ctx context.Context, flowID uint) (map[string]int64, error) {
	if _, err := s.flowRepo.GetByID(ctx, flowID); err != nil {
		return nil, err
	}
	return s.executionRepo.GetStats(ctx, flowID)
}

// EC-24 不变量（服务事件先行，营销后置）：营销流程只能消费「已持久化的服务事实」作为触发输入；
//   - 调用方必须先完成订单/售后/资金等服务链路并落库，再以标准字段（customer_id/event_type/...）
//     触发本流程；营销不得先于服务、不得重做/回写服务状态；触发失败不得反向影响服务链路；
//   - data 仅接受标准字段，额外/私有字段不得进入营销事件链；审计按 flow_id/execution_id 可回溯。
func (s *MarketingFlowService) TriggerFlow(ctx context.Context, flow *model.MarketingFlow, triggerID, userID string, data map[string]any) (*model.FlowExecution, error) {

	if flow == nil || flow.Status != model.FlowStatusActive {
		return nil, fmt.Errorf("%w: 流程未激活", utils.ErrInvalidInput)
	}
	if err := s.validateFlowDefinition(flow.FlowData); err != nil {
		return nil, err
	}
	snapshot, err := cloneMarketingFlowData(data)
	if err != nil {
		return nil, err
	}

	key, err := flowTriggerKey(ctx, flow, triggerID, userID)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	execution := &model.FlowExecution{
		FlowID: flow.ID, TriggerID: triggerID, UserID: userID, Status: "running", StartedAt: time.Now(),
		FlowVersion: flow.Version, GraphSnapshot: flow.FlowData, InputSnapshot: string(encoded), TriggerKey: &key,
	}
	created, err := s.executionRepo.CreateDurable(ctx, execution)
	if err != nil {
		return nil, err
	}
	if created {
		workerExecution := *execution
		utils.SafeGo(tenant.BackgroundWithTenant(ctx), "marketing-flow", func(workerCtx context.Context) {
			if err := s.executeDurableFlow(workerCtx, &workerExecution); err != nil {
				logger.Errorf("[MarketingFlow] durable execution %d requires recovery", workerExecution.ID)
			}
		})
	}

	return execution, nil
}

func (s *MarketingFlowService) executeFlow(ctx context.Context, execution *model.FlowExecution, flow *model.MarketingFlow, data map[string]any) { //nolint:unused // 保留旧执行器行为测试；生产 TriggerFlow 使用 executeDurableFlow。
	// FA-20：把执行中的流程挂上 ctx，发送型动作在发送时点据此复核流程状态——
	// 流程在执行中途被暂停/停用时，后续发送动作零外发（不能只在触发入口拦一次）。
	ctx = WithMarketingFlowRef(ctx, flow)
	persist := func() {
		if err := s.executionRepo.Update(ctx, execution); err != nil {
			logger.Errorf("[MarketingFlow] 执行状态落库失败 executionID=%d status=%s: %v", execution.ID, execution.Status, err)
		}
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[MarketingFlow] 执行 panic: %v\n%s", r, debug.Stack())
			execution.Status = "failed"
			execution.ErrorMessage = fmt.Sprintf("流程执行异常：%v", r)
			persist()
		}
	}()

	graph, err := parseMarketingFlowGraph(flow.FlowData)
	if err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = err.Error()
		persist()
		return
	}
	executionData, err := graph.run(ctx, data, func(ctx context.Context, node model.FlowNode, snapshot map[string]any) (map[string]any, error) {
		return s.executeNode(ctx, node, execution.UserID, snapshot)
	}, func(nodeID string) {
		execution.CurrentNode = nodeID
	})
	if err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = err.Error()
		persist()
		return
	}

	execution.Status = "completed"
	execution.CompletedAt = func() *time.Time { t := time.Now(); return &t }()
	executionDataBytes, _ := json.Marshal(executionData)
	execution.ExecutionData = string(executionDataBytes)
	persist()
}

func (s *MarketingFlowService) executeNode(ctx context.Context, node model.FlowNode, userID string, data map[string]any) (map[string]any, error) {
	switch node.Type {
	case "trigger":

		return data, nil

	case "action":

		return s.executeAction(ctx, node, userID, data)

	case "condition":

		return s.evaluateCondition(node, data)

	case "delay":

		return s.handleDelay(ctx, node)

	default:
		return nil, fmt.Errorf("未知的节点类型：%s", node.Type)
	}
}

func evaluateOperator(fieldValue any, operator, value string) (bool, error) {
	switch operator {
	case "eq":
		return evalEq(fieldValue, value)
	case "ne":
		return evalNe(fieldValue, value)
	case "gt":
		return evalGt(fieldValue, value)
	case "lt":
		return evalLt(fieldValue, value)
	case "gte":
		return evalGte(fieldValue, value)
	case "lte":
		return evalLte(fieldValue, value)
	case "contains":
		return evalContains(fieldValue, value)
	case "in":
		return evalIn(fieldValue, value)
	default:
		return false, fmt.Errorf("不支持的运算符：%s", operator)
	}
}

// EvaluateOperator 公开版 evaluateOperator(供跨包调用,如 service/sop_condition.go)
func EvaluateOperator(fieldValue any, operator, value string) (bool, error) {
	return evaluateOperator(fieldValue, operator, value)
}

func evalContains(fieldValue any, value string) (bool, error) {
	strValue := fmt.Sprintf("%v", fieldValue)
	return strings.Contains(strings.ToLower(strValue), strings.ToLower(value)), nil
}

func evalIn(fieldValue any, value string) (bool, error) {

	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return false, errors.New("in 运算符的列表格式错误，应该为 [a,b,c]")
	}

	listStr := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	items := strings.Split(listStr, ",")

	for _, item := range items {
		item = strings.TrimSpace(item)
		matched, err := evalEq(fieldValue, item)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}

	return false, nil
}

// DefaultMaxFlowDelaySeconds 营销流程 delay 节点单次等待时长的上限秒数（参数中心未配置时的兜底）
const DefaultMaxFlowDelaySeconds = 300

var maxFlowDelaySecondsProvider = func() int { return DefaultMaxFlowDelaySeconds }

// SetMaxFlowDelaySecondsProvider 由装配层注入参数中心读取口（nil 视为不注入）
func SetMaxFlowDelaySecondsProvider(fn func() int) {
	if fn != nil {
		maxFlowDelaySecondsProvider = fn
	}
}

// maxFlowDelaySeconds 非正值一律回落兜底：上限 ≤0 会让所有 delay 节点的时长被 clamp 成 0，
// 配置的等待完全不发生，流程看起来跑通但等待步骤全部被跳过。
func maxFlowDelaySeconds() int {
	v := maxFlowDelaySecondsProvider()
	if v <= 0 {
		return DefaultMaxFlowDelaySeconds
	}
	return v
}

func (s *MarketingFlowService) handleDelay(ctx context.Context, node model.FlowNode) (map[string]any, error) {
	duration, _ := node.Config["duration"].(float64)

	maxDelay := maxFlowDelaySeconds()
	if duration > float64(maxDelay) {
		duration = float64(maxDelay)
	}
	if duration > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(duration) * time.Second):
		}
	}
	return nil, nil
}

// TriggerFlowByID 按 ID 触发营销流程（封装 GetByID + TriggerFlow）。
//
// 直接回新建的执行记录，不再回查「最新一条」：回查既可能因为并发触发返回别人的
// 执行记录，也可能在查空时交出 (nil, nil)，让出口层把「没触发成」说成成功。
func (s *MarketingFlowService) TriggerFlowByID(ctx context.Context, flowID uint, triggerID, userID string, data map[string]any) (*model.FlowExecution, error) {
	flow, err := s.flowRepo.GetByID(ctx, flowID)
	if err != nil {
		return nil, err
	}
	return s.TriggerFlow(ctx, flow, triggerID, userID, data)
}

// ProbeMaxFlowDelaySeconds 暴露读取口当前值，供装配层测试断言接线确实生效。
func ProbeMaxFlowDelaySeconds() int { return maxFlowDelaySeconds() }
