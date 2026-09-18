package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/content/model"
	"hivemtk-user/internal/content/repository"
	"hivemtk-user/internal/pkg/utils/logger"
	cdprepo "hivemtk-user/internal/repository"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var smsSenderFunc func(phone, content string) error

func SetSmsSender(fn func(phone, content string) error) {
	smsSenderFunc = fn
}

var workflowAssetResolverFunc func(ctx context.Context) (json.RawMessage, bool)

func SetWorkflowAssetResolver(fn func(ctx context.Context) (json.RawMessage, bool)) {
	workflowAssetResolverFunc = fn
}

type MarketingWorkflow struct {
	ID       string                   `json:"id"`
	Name     string                   `json:"name"`
	Industry string                   `json:"industry"`
	Trigger  map[string]interface{}   `json:"trigger"`
	Steps    []map[string]interface{} `json:"steps"`
	KPI      map[string]interface{}   `json:"kpi"`
}

func ResolveActiveWorkflow(ctx context.Context) (*MarketingWorkflow, bool) {
	if workflowAssetResolverFunc == nil {
		return nil, false
	}
	raw, ok := workflowAssetResolverFunc(ctx)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	var w MarketingWorkflow
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, false
	}
	return &w, true
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

func NewMarketingFlowService() *MarketingFlowService {
	return &MarketingFlowService{
		flowRepo:         repository.NewMarketingFlowRepository(),
		executionRepo:    repository.NewFlowExecutionRepository(),
		userTagRepo:      cdprepo.NewUserTagRepository(),
		orderRepo:        cdprepo.NewOrderRepository(),
		clueRepo:         cdprepo.NewClueRepository(),
		sessionRepo:      cdprepo.NewCustomerSessionRepository(),
		agentRepo:        cdprepo.NewAgentStatusRepository(),
		operationLogRepo: cdprepo.NewOperationLogRepository(),
	}
}

func NewMarketingFlowServiceWithDB(db *gorm.DB) *MarketingFlowService {
	return &MarketingFlowService{
		flowRepo:         repository.NewMarketingFlowRepositoryWithDB(db),
		executionRepo:    repository.NewFlowExecutionRepositoryWithDB(db),
		userTagRepo:      cdprepo.NewUserTagRepositoryWithDB(db),
		orderRepo:        cdprepo.NewOrderRepositoryWithDB(db),
		clueRepo:         cdprepo.NewClueRepositoryWithDB(db),
		sessionRepo:      cdprepo.NewCustomerSessionRepositoryWithDB(db),
		agentRepo:        cdprepo.NewAgentStatusRepositoryWithDB(db),
		operationLogRepo: cdprepo.NewOperationLogRepositoryWithDB(db),
	}
}

type CreateFlowRequest struct {
	Name          string                `json:"name" binding:"required"`
	Description   string                `json:"description"`
	TriggerType   model.TriggerType     `json:"trigger_type" binding:"required"`
	TriggerConfig map[string]any        `json:"trigger_config"`
	FlowData      *model.FlowDefinition `json:"flow_data"`
}

func (s *MarketingFlowService) CreateFlow(createdBy uint, req *CreateFlowRequest) (*model.MarketingFlow, error) {
	flowData, err := json.Marshal(req.FlowData)
	if err != nil {
		return nil, fmt.Errorf("流程定义 JSON 序列化失败：%w", err)
	}

	if err := s.validateFlowDefinition(string(flowData)); err != nil {
		return nil, fmt.Errorf("流程定义无效：%w", err)
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

	if err := s.flowRepo.Create(flow); err != nil {
		return nil, err
	}

	return flow, nil
}

func (s *MarketingFlowService) GetFlowList(page, pageSize int) ([]*model.MarketingFlow, int64, error) {
	return s.flowRepo.GetAll(page, pageSize)
}

func (s *MarketingFlowService) GetFlowByID(id uint) (*model.MarketingFlow, error) {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return flow, nil
}

type UpdateFlowRequest struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	TriggerType   model.TriggerType     `json:"trigger_type"`
	TriggerConfig map[string]any        `json:"trigger_config"`
	FlowData      *model.FlowDefinition `json:"flow_data"`
}

func (s *MarketingFlowService) UpdateFlow(id uint, userID uint, isAdmin bool, req *UpdateFlowRequest) (*model.MarketingFlow, error) {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return nil, errors.New("无权限修改此营销流程")
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
		flowData, _ := json.Marshal(req.FlowData)
		flow.FlowData = string(flowData)
	}

	flow.Version++
	if err := s.flowRepo.Update(flow); err != nil {
		return nil, err
	}

	return flow, nil
}

func (s *MarketingFlowService) DeleteFlow(id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return errors.New("无权限删除此营销流程")
	}

	return s.flowRepo.Delete(id)
}

func (s *MarketingFlowService) ActivateFlow(id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return errors.New("无权限激活此营销流程")
	}

	if err := s.validateFlowDefinition(flow.FlowData); err != nil {
		return fmt.Errorf("流程定义无效：%w", err)
	}

	return s.flowRepo.UpdateStatus(id, model.FlowStatusActive)
}

func (s *MarketingFlowService) PauseFlow(id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return errors.New("无权限暂停此营销流程")
	}

	return s.flowRepo.UpdateStatus(id, model.FlowStatusPaused)
}

func (s *MarketingFlowService) StopFlow(id uint, userID uint, isAdmin bool) error {
	flow, err := s.flowRepo.GetByID(id)
	if err != nil {
		return err
	}

	if !isAdmin && flow.CreatedBy != userID {
		return errors.New("无权限停止此营销流程")
	}

	return s.flowRepo.UpdateStatus(id, model.FlowStatusInactive)
}

func (s *MarketingFlowService) validateFlowDefinition(flowData string) error {
	if flowData == "" {
		return errors.New("流程定义为空")
	}

	var def model.FlowDefinition
	if err := json.Unmarshal([]byte(flowData), &def); err != nil {
		return fmt.Errorf("流程定义 JSON 解析失败：%w", err)
	}

	if len(def.Nodes) == 0 {
		return errors.New("流程至少需要一个节点")
	}

	hasTrigger := false
	for _, node := range def.Nodes {
		if node.Type == "trigger" {
			hasTrigger = true
			break
		}
	}
	if !hasTrigger {
		return errors.New("流程必须有触发器节点")
	}

	return nil
}

func (s *MarketingFlowService) GetExecutionList(flowID uint, page, pageSize int) ([]*model.FlowExecution, int64, error) {
	return s.executionRepo.GetByFlowID(flowID, page, pageSize)
}

func (s *MarketingFlowService) GetExecutionStats(flowID uint) (map[string]int64, error) {
	return s.executionRepo.GetStats(flowID)
}

func (s *MarketingFlowService) TriggerFlow(ctx context.Context, flow *model.MarketingFlow, triggerID, userID string, data map[string]any) error {

	if flow.Status != model.FlowStatusActive {
		return errors.New("流程未激活")
	}

	execution := &model.FlowExecution{
		FlowID:        flow.ID,
		TriggerID:     triggerID,
		UserID:        userID,
		Status:        "running",
		ExecutionData: "",
		StartedAt:     time.Now(),
	}

	if err := s.executionRepo.Create(execution); err != nil {
		return err
	}

	go s.executeFlow(context.Background(), execution, flow, data)

	return nil
}

func (s *MarketingFlowService) executeFlow(ctx context.Context, execution *model.FlowExecution, flow *model.MarketingFlow, data map[string]any) {
	persist := func() {
		if err := s.executionRepo.Update(execution); err != nil {
			logger.Errorf("[MarketingFlow] 执行状态落库失败 executionID=%d status=%s: %v", execution.ID, execution.Status, err)
		}
	}
	defer func() {
		if r := recover(); r != nil {

			execution.Status = "failed"
			execution.ErrorMessage = fmt.Sprintf("流程执行异常：%v", r)
			persist()
		}
	}()

	var flowDef model.FlowDefinition
	if err := json.Unmarshal([]byte(flow.FlowData), &flowDef); err != nil {
		execution.Status = "failed"
		execution.ErrorMessage = fmt.Sprintf("流程定义解析失败：%v", err)
		persist()
		return
	}

	levels := topologicalLevels(flowDef.Nodes)
	if len(levels) == 0 {
		execution.Status = "failed"
		execution.ErrorMessage = "流程无有效节点"
		persist()
		return
	}

	executionData := data
	var execMu sync.Mutex

	for levelIdx, level := range levels {
		var wg sync.WaitGroup
		levelErrs := []error{}
		var levelMu sync.Mutex

		for i := range level {
			node := level[i]
			wg.Add(1)
			go func(n *model.FlowNode) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						levelMu.Lock()
						levelErrs = append(levelErrs, fmt.Errorf("node %s panic: %v", n.ID, r))
						levelMu.Unlock()
					}
				}()

				execution.CurrentNode = n.ID
				result, err := s.executeNode(ctx, *n, execution.UserID, executionData)
				if err != nil {
					levelMu.Lock()
					levelErrs = append(levelErrs, fmt.Errorf("节点 %s 执行失败：%w", n.Name, err))
					levelMu.Unlock()
					return
				}

				if result != nil {
					execMu.Lock()
					for k, v := range result {
						executionData[k] = v
					}
					execMu.Unlock()
				}
			}(node)
		}
		wg.Wait()

		if len(levelErrs) > 0 {
			execution.Status = "failed"
			execution.ErrorMessage = levelErrs[0].Error()
			persist()
			return
		}
		_ = levelIdx
	}

	execution.Status = "completed"
	execution.CompletedAt = func() *time.Time { t := time.Now(); return &t }()
	executionDataBytes, _ := json.Marshal(executionData)
	execution.ExecutionData = string(executionDataBytes)
	persist()
}

func topologicalLevels(nodes []model.FlowNode) [][]*model.FlowNode {
	if len(nodes) == 0 {
		return nil
	}

	nodeMap := make(map[string]*model.FlowNode, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		nodeMap[n.ID] = n
		inDegree[n.ID] = 0
	}

	for i := range nodes {
		n := &nodes[i]
		for _, next := range n.NextNodes {
			if _, ok := inDegree[next]; ok {
				inDegree[next]++
			}
		}
	}

	var levels [][]*model.FlowNode
	currentLevel := []*model.FlowNode{}
	for id, deg := range inDegree {
		if deg == 0 {
			currentLevel = append(currentLevel, nodeMap[id])
		}
	}

	if len(currentLevel) == 0 {
		for i := range nodes {
			currentLevel = append(currentLevel, &nodes[i])
		}
		return [][]*model.FlowNode{currentLevel}
	}

	visited := make(map[string]bool)
	for len(currentLevel) > 0 {
		levels = append(levels, currentLevel)
		nextLevel := []*model.FlowNode{}
		for _, n := range currentLevel {
			visited[n.ID] = true
			for _, next := range n.NextNodes {
				if _, ok := inDegree[next]; !ok {
					continue
				}
				inDegree[next]--
				if inDegree[next] == 0 {
					nextLevel = append(nextLevel, nodeMap[next])
				}
			}
		}
		currentLevel = nextLevel
	}

	unvisited := []*model.FlowNode{}
	for id := range nodeMap {
		if !visited[id] {
			unvisited = append(unvisited, nodeMap[id])
		}
	}
	if len(unvisited) > 0 {
		levels = append(levels, unvisited)
	}

	return levels
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

func (s *MarketingFlowService) handleDelay(ctx context.Context, node model.FlowNode) (map[string]any, error) {
	duration, _ := node.Config["duration"].(float64)

	const maxDelaySeconds = 300
	if duration > maxDelaySeconds {
		duration = maxDelaySeconds
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

// GetActiveFlows 获取所有激活的流程
func (s *MarketingFlowService) GetActiveFlows() ([]*model.MarketingFlow, error) {
	return s.flowRepo.GetByStatus(model.FlowStatusActive)
}

// TriggerFlowByID 按 ID 触发营销流程（封装 GetByID + TriggerFlow）
func (s *MarketingFlowService) TriggerFlowByID(ctx context.Context, flowID uint, triggerID, userID string, data map[string]any) (*model.FlowExecution, error) {
	flow, err := s.flowRepo.GetByID(flowID)
	if err != nil {
		return nil, err
	}
	if err := s.TriggerFlow(ctx, flow, triggerID, userID, data); err != nil {
		return nil, err
	}

	executions, _, err := s.executionRepo.GetByFlowID(flowID, 1, 1)
	if err != nil {
		return nil, err
	}
	if len(executions) > 0 {
		return executions[0], nil
	}
	return nil, nil
}
