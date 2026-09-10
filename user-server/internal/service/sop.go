package service

import (
	"context"

	"encoding/json"

	"errors"

	"fmt"

	"sync"

	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/llm"

	"hivemtk-user/internal/dto"

	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils/logger"

	"hivemtk-user/internal/repository"
)

type SOPService struct {
	db         *gorm.DB
	agentRepo  *repository.SopAgentRepository
	execRepo   *repository.SopExecutionRepository
	dispatcher *llm.Dispatcher
}

const (
	SOPStatusPending = "pending"

	SOPStatusRunning = "running"

	SOPStatusSuccess = "success"

	SOPStatusFailed = "failed"

	SOPStatusPaused = "paused"

	SOPStatusCanceled = "canceled"

	SOPNodeTypeStart = "start"

	SOPNodeTypeMessage = "message"

	SOPNodeTypeBranch = "branch"

	SOPNodeTypeWait = "wait"

	SOPNodeTypeAction = "action"

	SOPNodeTypeEnd = "end"

	SOPNodeTypeAIDecide = "ai_decide"

	SOPNodeTypeSendOffer = "send_offer"

	SOPNodeTypeGreeting = "greeting"

	SOPNodeTypeInquire = "inquire"

	SOPNodeTypeIntroduce = "introduce"

	SOPNodeTypeHandle = "handle"

	SOPNodeTypeClose = "close"

	SOPNodeTypeInvite = "invite"

	SOPNodeTypeFollowUp = "follow_up"

	SOPNodeTypeActivate = "activate"

	SOPNodeTypeNurture = "nurture"

	SOPNodeTypeCondition = "condition"

	SOPNodeTypeLLM = "llm"

	SOPTriggerManual = "manual"

	SOPTriggerAuto = "auto"

	SOPTriggerIntent = "intent"

	SOPTriggerSchedule = "schedule"
)

var SOPNodeSupportedTypes = map[string]bool{
	SOPNodeTypeStart: true, SOPNodeTypeMessage: true, SOPNodeTypeBranch: true,
	SOPNodeTypeWait: true, SOPNodeTypeAction: true, SOPNodeTypeEnd: true,
	SOPNodeTypeAIDecide: true, SOPNodeTypeSendOffer: true,
	SOPNodeTypeGreeting: true, SOPNodeTypeInquire: true, SOPNodeTypeIntroduce: true,
	SOPNodeTypeHandle: true, SOPNodeTypeClose: true, SOPNodeTypeInvite: true,
	SOPNodeTypeFollowUp: true, SOPNodeTypeActivate: true, SOPNodeTypeNurture: true,
	SOPNodeTypeCondition: true, SOPNodeTypeLLM: true,
}

var (
	ErrSOPNotFound = errors.New("sop not found")

	ErrSOPInvalidGraph = errors.New("invalid sop graph")

	ErrSOPNoStart = errors.New("sop graph has no start node")

	ErrSOPExecNotFound = errors.New("execution not found")

	ErrSOPExecNotRunning = errors.New("execution is not running")
)

const (
	maxNodesPerSOP  = 100
	maxWaitNodesSOP = 10
)

type SOPNode = dto.SOPNode

type SOPConditionBranch = dto.SOPConditionBranch

type SOPPosition = dto.SOPPosition

type SOPGraph = dto.SOPGraph

type SOPEdge = dto.SOPEdge

func NewSOPService(db *gorm.DB, dispatcher *llm.Dispatcher) *SOPService {
	return &SOPService{
		db:         db,
		agentRepo:  repository.NewSopAgentRepository(db),
		execRepo:   repository.NewSopExecutionRepository(db),
		dispatcher: dispatcher,
	}
}

type CreateRequest = dto.CreateRequest

func (s *SOPService) TemplateFromActiveAsset(ctx context.Context, scenario string) (*CreateRequest, bool) {
	if r := GetAssetResolver(); r != nil {
		if sop, ok := r.GetActiveSOP(ctx); ok && sop != nil {

			return sop.ToCreateRequest(ctx, scenario), true
		}
	}
	return nil, false
}

func (s *SOPService) Create(ctx context.Context, req *CreateRequest) (*model.SOPAgent, error) {
	if len(req.SOPGraph.Nodes) == 0 {
		if tpl, ok := s.TemplateFromActiveAsset(ctx, req.Scenario); ok && tpl != nil {
			req = tpl
		}
	}
	if err := s.validateGraph(ctx, &req.SOPGraph); err != nil {
		return nil, err
	}
	if !req.ABTestConfig.Enabled && len(req.ABTestConfig.Variants) == 0 {
		if r := GetAssetResolver(); r != nil {
			if plan, ok := r.GetActiveABPlan(ctx); ok && plan != nil {

				if cfg := plan.ToSOPABTestConfig(ctx); ValidateSOPABTestConfig(cfg) == nil {
					req.ABTestConfig = cfg
				}
			}
		}
	}
	if err := ValidateSOPABTestConfig(req.ABTestConfig); err != nil {
		return nil, fmt.Errorf("A/B 测试配置非法：%w", err)
	}

	if err := ValidateSOPTriggerConfigEntryPolicy(model.JSONMap(req.TriggerConfig)); err != nil {
		return nil, err
	}
	graphData, _ := json.Marshal(req.SOPGraph)
	if req.TriggerType == "" {
		req.TriggerType = SOPTriggerAuto
	}
	triggerMap := toJSONMap(req.TriggerConfig)
	abMap := model.JSONMap{}
	if req.ABTestConfig.Enabled {

		abData, err := json.Marshal(req.ABTestConfig)
		if err != nil {
			return nil, fmt.Errorf("marshal ABTestConfig: %w", err)
		}
		if err := json.Unmarshal(abData, &abMap); err != nil {
			return nil, fmt.Errorf("unmarshal ABTestConfig: %w", err)
		}
	}
	agent := &model.SOPAgent{

		Name:          req.Name,
		Scenario:      req.Scenario,
		Description:   req.Description,
		TriggerType:   req.TriggerType,
		TriggerConfig: triggerMap,
		SOPGraph:      toJSONMapBytes(graphData),
		Priority:      req.Priority,
		ABTestConfig:  abMap,
		CreatedBy:     req.CreatedBy,
		IsActive:      true,
	}
	if agent.TriggerConfig == nil {
		agent.TriggerConfig = model.JSONMap{}
	}
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func (s *SOPService) Update(ctx context.Context, id uint, req *CreateRequest) (*model.SOPAgent, error) {
	if err := s.validateGraph(ctx, &req.SOPGraph); err != nil {
		return nil, err
	}
	if err := ValidateSOPABTestConfig(req.ABTestConfig); err != nil {
		return nil, fmt.Errorf("A/B 测试配置非法：%w", err)
	}

	if err := ValidateSOPTriggerConfigEntryPolicy(model.JSONMap(req.TriggerConfig)); err != nil {
		return nil, err
	}
	agent, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSOPNotFound
		}
		return nil, err
	}
	graphData, err := json.Marshal(req.SOPGraph)
	if err != nil {
		return nil, fmt.Errorf("marshal SOPGraph: %w", err)
	}
	abMap := model.JSONMap{}
	if req.ABTestConfig.Enabled {
		abData, err := json.Marshal(req.ABTestConfig)
		if err != nil {
			return nil, fmt.Errorf("marshal ABTestConfig: %w", err)
		}
		if err := json.Unmarshal(abData, &abMap); err != nil {
			return nil, fmt.Errorf("unmarshal ABTestConfig: %w", err)
		}
	}
	agent.Name = req.Name
	agent.Scenario = req.Scenario
	agent.Description = req.Description
	agent.TriggerType = req.TriggerType
	agent.TriggerConfig = toJSONMap(req.TriggerConfig)
	agent.SOPGraph = toJSONMapBytes(graphData)
	agent.Priority = req.Priority
	agent.ABTestConfig = abMap
	agent.Version++
	if err := s.agentRepo.Save(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func (s *SOPService) Get(ctx context.Context, id uint) (*model.SOPAgent, error) {
	agent, err := s.agentRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSOPNotFound
		}
		return nil, err
	}
	return agent, nil
}

func (s *SOPService) List(ctx context.Context, scenario string, page, pageSize int) ([]model.SOPAgent, int64, error) {
	return s.agentRepo.List(ctx, scenario, page, pageSize)
}

func (s *SOPService) Delete(ctx context.Context, id uint) error {
	rowsAffected, err := s.agentRepo.DeleteByID(ctx, id)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrSOPNotFound
	}
	return nil
}

func (s *SOPService) Activate(ctx context.Context, id uint) error {
	rowsAffected, err := s.agentRepo.UpdateActive(ctx, id, true)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrSOPNotFound
	}
	return nil
}

func (s *SOPService) Deactivate(ctx context.Context, id uint) error {
	rowsAffected, err := s.agentRepo.UpdateActive(ctx, id, false)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrSOPNotFound
	}
	return nil
}

func (s *SOPService) Execute(ctx context.Context, req *dto.ExecuteRequest) (*model.SOPExecution, error) {
	agent, err := s.Get(ctx, req.SOPID)
	if err != nil {
		return nil, err
	}
	if !agent.IsActive {
		return nil, errors.New("sop is not active")
	}

	policy := ParseSOPEntryPolicy(agent.TriggerConfig)
	if !s.entryAllowedByPolicy(ctx, req.SOPID, req.CustomerID, policy) {
		logger.Infof("[SOP] entry_policy 拦截重复进入 sop=%d customer=%s mode=%s", req.SOPID, req.CustomerID, policy.Mode)
		return nil, ErrSOPEntrySuppressed
	}

	variantName, variantGraphID, err := s.resolveABTestVariant(context.Background(), agent, req.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("A/B 测试分流失败：%w", err)
	}

	traceID := tracing.TraceIDFromContext(ctx)
	if traceID == "" {
		traceID = logger.GenerateTraceID()
	}

	exec := &model.SOPExecution{
		SOPID:          req.SOPID,
		CustomerID:     req.CustomerID,
		SessionID:      req.SessionID,
		Status:         SOPStatusRunning,
		CurrentNodeIdx: 0,
		StartedAt:      time.Now(),
		ExecutionData:  model.JSONMap(req.Input),
		Variant:        variantName,
		TraceID:        traceID,
	}
	if exec.ExecutionData == nil {
		exec.ExecutionData = model.JSONMap{}
	}
	if err := s.execRepo.Create(ctx, exec); err != nil {
		return nil, err
	}

	graph, err := s.loadSOPGraph(ctx, agent, variantGraphID)
	if err != nil {
		return nil, err
	}
	startNode := findStartNode(&graph)
	if startNode == nil {
		exec.Status = SOPStatusFailed
		exec.ErrorMessage = ErrSOPNoStart.Error()
		_ = s.execRepo.Save(ctx, exec)
		return exec, ErrSOPNoStart
	}
	exec.CurrentNode = startNode.ID
	if err := s.execRepo.Save(ctx, exec); err != nil {
		return nil, err
	}
	_ = s.agentRepo.IncrementExecutionCount(ctx, agent.ID)

	if dispatcher := GetSOPExecutionDispatcher(); dispatcher != nil {
		dispatcher.DispatchOrLog(&dispatchTask{
			ExecutionID: exec.ID,
			NodeID:      startNode.ID,
			Attempt:     0,
			TraceID:     traceID,
		})
	}
	return exec, nil
}

func (s *SOPService) Step(ctx context.Context, req *dto.StepRequest) (*model.SOPExecution, error) {
	exec, err := s.execRepo.GetByID(ctx, req.ExecutionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSOPExecNotFound
		}
		return nil, err
	}
	if exec.Status != SOPStatusRunning {
		return nil, ErrSOPExecNotRunning
	}

	if exec.ExecutionData == nil {
		exec.ExecutionData = model.JSONMap{}
	}
	for k, v := range req.Output {
		exec.ExecutionData[k] = v
	}

	if exec.WaitEvent != "" {
		exec.WaitEvent = ""
	}

	if err := s.execRepo.Save(ctx, exec); err != nil {
		return nil, err
	}

	if dispatcher := GetSOPExecutionDispatcher(); dispatcher != nil {
		traceID := exec.TraceID
		if traceID == "" {
			traceID = logger.GenerateTraceID()
		}
		dispatcher.DispatchOrLog(&dispatchTask{
			ExecutionID: exec.ID,
			NodeID:      exec.CurrentNode,
			Attempt:     0,
			TraceID:     traceID,
		})
		return exec, nil
	}

	agent, err := s.Get(ctx, exec.SOPID)
	if err != nil {
		return nil, err
	}

	var variantGraphID uint
	if exec.Variant != "" {
		cfg := ParseSOPABTestConfig(agent.ABTestConfig)
		if cfg.Enabled {
			for _, v := range cfg.Variants {
				if v.Name == exec.Variant {
					variantGraphID = v.SOPGraphID
					break
				}
			}
		}
	}

	graph, err := s.loadSOPGraph(ctx, agent, variantGraphID)
	if err != nil {
		return nil, err
	}
	current := findNodeByID(&graph, exec.CurrentNode)
	if current == nil {
		exec.Status = SOPStatusFailed
		exec.ErrorMessage = "current node not found"
		_ = s.execRepo.Save(ctx, exec)
		return exec, nil
	}
	next := nextNode(&graph, current, exec.ExecutionData)
	if next == nil {
		exec.Status = SOPStatusSuccess
		now := time.Now()
		exec.CompletedAt = &now
		if err := s.execRepo.Save(ctx, exec); err != nil {
			return nil, err
		}
		_ = s.agentRepo.IncrementSuccessCount(ctx, exec.SOPID)
		return exec, nil
	}
	exec.CurrentNode = next.ID
	for i, n := range graph.Nodes {
		if n.ID == next.ID {
			exec.CurrentNodeIdx = i
			break
		}
	}
	if err := s.execRepo.Save(ctx, exec); err != nil {
		return nil, err
	}
	return exec, nil
}

func (s *SOPService) Pause(ctx context.Context, execID uint) error {
	exec, err := s.execRepo.GetByID(ctx, execID)
	if err != nil {
		return err
	}
	if exec == nil {
		return ErrSOPNotFound
	}
	return s.execRepo.UpdateStatus(ctx, execID, SOPStatusPaused)
}

func (s *SOPService) Resume(ctx context.Context, execID uint) error {
	exec, err := s.execRepo.GetByID(ctx, execID)
	if err != nil {
		return err
	}
	if exec == nil {
		return ErrSOPNotFound
	}
	return s.execRepo.UpdateStatus(ctx, execID, SOPStatusRunning)
}

func (s *SOPService) Cancel(ctx context.Context, execID uint) error {
	now := time.Now()
	return s.execRepo.UpdateFields(ctx, execID, map[string]any{
		"status":       SOPStatusCanceled,
		"completed_at": now,
	})
}

func (s *SOPService) GetExecution(ctx context.Context, execID uint) (*model.SOPExecution, error) {
	exec, err := s.execRepo.GetByID(ctx, execID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSOPExecNotFound
		}
		return nil, err
	}
	return exec, nil
}

func (s *SOPService) ListExecutions(ctx context.Context, customerID string, status string, page, pageSize int) ([]model.SOPExecution, int64, error) {
	return s.execRepo.List(ctx, customerID, status, page, pageSize)
}

func (s *SOPService) MatchByIntent(ctx context.Context, intentType string) ([]model.SOPAgent, error) {
	list, err := s.agentRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	matched := []model.SOPAgent{}
	for _, a := range list {
		if a.TriggerType != SOPTriggerIntent {
			continue
		}
		if intents, ok := a.TriggerConfig["intents"].([]any); ok {
			for _, i := range intents {
				if s, ok := i.(string); ok && s == intentType {
					matched = append(matched, a)
					break
				}
			}
		}
	}
	return matched, nil
}

func (s *SOPService) Stats(ctx context.Context) (map[string]int64, error) {
	stats := map[string]int64{
		"total":   0,
		"active":  0,
		"running": 0,
		"success": 0,
		"failed":  0,
	}
	totalAgents, err := s.agentRepo.CountAll(ctx)
	if err != nil {
		logger.Warnf("[SOP Stats] CountAll 失败，记 0: %v", err)
	} else {
		stats["total"] = totalAgents
	}
	activeAgents, err := s.agentRepo.CountActive(ctx)
	if err != nil {
		logger.Warnf("[SOP Stats] CountActive 失败，记 0: %v", err)
	} else {
		stats["active"] = activeAgents
	}
	runningExecs, err := s.execRepo.CountByStatus(ctx, SOPStatusRunning)
	if err != nil {
		logger.Warnf("[SOP Stats] CountByStatus(running) 失败，记 0: %v", err)
	} else {
		stats["running"] = runningExecs
	}
	successExecs, err := s.execRepo.CountByStatus(ctx, SOPStatusSuccess)
	if err != nil {
		logger.Warnf("[SOP Stats] CountByStatus(success) 失败，记 0: %v", err)
	} else {
		stats["success"] = successExecs
	}
	failedExecs, err := s.execRepo.CountByStatus(ctx, SOPStatusFailed)
	if err != nil {
		logger.Warnf("[SOP Stats] CountByStatus(failed) 失败，记 0: %v", err)
	} else {
		stats["failed"] = failedExecs
	}

	stats["total"] = totalAgents
	stats["active"] = activeAgents
	stats["running"] = runningExecs
	stats["success"] = successExecs
	stats["failed"] = failedExecs
	return stats, nil
}

// ValidateGraphForTest 暴露给测试用：跳过 DB 检查，只跑图结构验证
func (s *SOPService) ValidateGraphForTest(ctx context.Context, graph *SOPGraph) error {
	return s.validateGraph(ctx, graph)
}

func (s *SOPService) validateGraph(ctx context.Context, graph *SOPGraph) error {
	if graph == nil {
		return ErrSOPInvalidGraph
	}
	if len(graph.Nodes) == 0 {
		return ErrSOPInvalidGraph
	}
	if len(graph.Nodes) > maxNodesPerSOP {
		return fmt.Errorf("sop exceeds max nodes (limit=%d)", maxNodesPerSOP)
	}
	waitCount := 0
	for _, n := range graph.Nodes {
		if n.Type == SOPNodeTypeWait {
			waitCount++
		}
	}
	if waitCount > maxWaitNodesSOP {
		return fmt.Errorf("sop exceeds max wait nodes (limit=%d)", maxWaitNodesSOP)
	}
	hasStart := false
	ids := map[string]bool{}
	for _, n := range graph.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node has empty id")
		}
		if ids[n.ID] {
			return fmt.Errorf("duplicate node id: %s", n.ID)
		}
		ids[n.ID] = true
		if !SOPNodeSupportedTypes[n.Type] {
			return fmt.Errorf("node %s has unsupported type: %s", n.ID, n.Type)
		}
		if n.Type == SOPNodeTypeStart {
			hasStart = true
		}
		if n.Type == SOPNodeTypeCondition {
			for _, br := range n.Conditions {
				if br.Next == "" {
					return fmt.Errorf("condition node %s has a branch with empty next", n.ID)
				}
			}
		}
	}
	if !hasStart {
		return ErrSOPNoStart
	}
	for _, n := range graph.Nodes {
		for _, nextID := range n.Next {
			if !ids[nextID] {
				return fmt.Errorf("node %s references missing node %s", n.ID, nextID)
			}
		}
		if n.Type == SOPNodeTypeCondition {
			for _, br := range n.Conditions {
				if !ids[br.Next] {
					return fmt.Errorf("condition node %s branch [%s] references missing node %s", n.ID, br.Label, br.Next)
				}
			}
		}
	}
	for _, e := range graph.Edges {
		if !ids[e.From] {
			return fmt.Errorf("edge from missing node %s", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("edge to missing node %s", e.To)
		}
	}

	if err := detectSOPCycles(graph); err != nil {
		return err
	}
	return nil
}

func detectSOPCycles(graph *SOPGraph) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(graph.Nodes))
	for _, n := range graph.Nodes {
		color[n.ID] = white
	}

	adj := make(map[string][]string, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nexts := append([]string{}, n.Next...)
		if n.Type == SOPNodeTypeCondition {
			for _, br := range n.Conditions {
				nexts = append(nexts, br.Next)
			}
		}
		adj[n.ID] = nexts
	}

	var dfs func(id string) error
	dfs = func(id string) error {
		color[id] = gray
		for _, next := range adj[id] {
			switch color[next] {
			case gray:
				return fmt.Errorf("SOP 存在环：%s -> %s（环检测拒绝保存）", id, next)
			case white:
				if err := dfs(next); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for _, n := range graph.Nodes {
		if color[n.ID] == white {
			if err := dfs(n.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SOPService) entryAllowedByPolicy(ctx context.Context, sopID uint, customerID string, policy SOPEntryPolicy) bool {
	if policy.Mode == SOPEntryModeAlways {
		return true
	}
	last, err := s.execRepo.LatestBySOPAndCustomer(ctx, sopID, customerID)
	if err != nil || last == nil {
		// 无执行记录或查询失败均放行（与原 ErrRecordNotFound 语义一致）
		return true
	}
	switch policy.Mode {
	case SOPEntryModeCooldown:
		window := policy.cooldownWindow()
		if window <= 0 {
			return true
		}
		return time.Since(last.CreatedAt) >= window
	default:
		return false
	}
}

func findStartNode(graph *SOPGraph) *SOPNode {
	for i, n := range graph.Nodes {
		if n.Type == SOPNodeTypeStart {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func findNodeByID(graph *SOPGraph, id string) *SOPNode {
	for i, n := range graph.Nodes {
		if n.ID == id {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func deepCopySOPNode(n *SOPNode) *SOPNode {
	if n == nil {
		return nil
	}
	cp := &SOPNode{
		ID:          n.ID,
		Type:        n.Type,
		Name:        n.Name,
		Condition:   n.Condition,
		Description: n.Description,
		Prompt:      n.Prompt,
		Position:    n.Position,
	}
	if n.Config != nil {
		cp.Config = make(map[string]any, len(n.Config))
		for k, v := range n.Config {
			cp.Config[k] = v
		}
	}
	if n.Next != nil {
		cp.Next = make([]string, len(n.Next))
		copy(cp.Next, n.Next)
	}
	if n.Tools != nil {
		cp.Tools = make([]string, len(n.Tools))
		copy(cp.Tools, n.Tools)
	}
	if n.Conditions != nil {
		cp.Conditions = make([]SOPConditionBranch, len(n.Conditions))
		copy(cp.Conditions, n.Conditions)
	}
	if n.Metadata != nil {
		cp.Metadata = make(map[string]any, len(n.Metadata))
		for k, v := range n.Metadata {
			cp.Metadata[k] = v
		}
	}
	return cp
}

func nextNode(graph *SOPGraph, current *SOPNode, data model.JSONMap) *SOPNode {
	if current == nil {
		return nil
	}
	if current.Type == SOPNodeTypeEnd {
		return nil
	}

	if current.Type == SOPNodeTypeCondition {
		if len(current.Conditions) > 0 {
			br, err := SOPEvaluateConditionBranches(current.Conditions, data)
			if err == nil && br.Matched && br.NextNode != "" {
				if n := findNodeByID(graph, br.NextNode); n != nil {
					return deepCopySOPNode(n)
				}
			}
			if len(current.Next) > 0 {
				return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
			}
			return nil
		}

		condStr := current.Condition
		if condStr == "" {
			if c, ok := current.Config["condition"].(string); ok {
				condStr = c
			}
		}
		if condStr != "" {
			result, err := SOPEvaluateNodeCondition(current, data)
			if err == nil {
				if nextID, ok := result["_next_node"].(string); ok && nextID != "" {
					if n := findNodeByID(graph, nextID); n != nil {
						return deepCopySOPNode(n)
					}
				}
			}
		}
		if len(current.Next) > 0 {
			return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
		}
		return nil
	}

	if current.Type == SOPNodeTypeLLM {
		if decision, ok := data["_llm_decision"].(string); ok && decision != "" {
			if n := findNodeByID(graph, decision); n != nil {
				return deepCopySOPNode(n)
			}
		}
		if nextID, ok := current.Config["next"].(string); ok && nextID != "" {
			if n := findNodeByID(graph, nextID); n != nil {
				return deepCopySOPNode(n)
			}
		}
		if len(current.Next) > 0 {
			return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
		}
		return nil
	}

	if current.Type == SOPNodeTypeBranch {
		if len(current.Conditions) > 0 {
			br, err := SOPEvaluateConditionBranches(current.Conditions, data)
			if err == nil && br.Matched && br.NextNode != "" {
				if n := findNodeByID(graph, br.NextNode); n != nil {
					return deepCopySOPNode(n)
				}
			}
		}
		for _, e := range graph.Edges {
			if e.From == current.ID {
				if v, ok := data["_branch_result"].(string); ok {
					if v == e.When || (v == "true" && e.When == "true") || (v == "false" && e.When == "false") {
						return deepCopySOPNode(findNodeByID(graph, e.To))
					}
				}
			}
		}
		if len(current.Next) > 0 {
			return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
		}
		return nil
	}

	if current.Type == SOPNodeTypeAIDecide {
		if decision, ok := data["_ai_decision"].(string); ok && decision != "" {
			if n := findNodeByID(graph, decision); n != nil {
				return deepCopySOPNode(n)
			}
		}
		if decision, ok := data["_llm_decision"].(string); ok && decision != "" {
			if n := findNodeByID(graph, decision); n != nil {
				return deepCopySOPNode(n)
			}
		}
		if len(current.Next) > 0 {
			return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
		}
		return nil
	}

	if len(current.Next) > 0 {
		return deepCopySOPNode(findNodeByID(graph, current.Next[0]))
	}
	return nil
}

var (
	sopOnce sync.Once

	sopInstance *SOPService
)

func GetSOPService() *SOPService {
	return sopInstance
}

func InitSOPService(db *gorm.DB, dispatcher *llm.Dispatcher) *SOPService {
	sopOnce.Do(func() {
		sopInstance = NewSOPService(db, dispatcher)
	})
	return sopInstance
}

func NewWelcomeSOP() *CreateRequest {
	return &CreateRequest{
		Name:        "客户欢迎 SOP",
		Scenario:    "welcome",
		Description: "新客户接入时的标准欢迎流程（14 节点类型示范）",
		TriggerType: SOPTriggerAuto,
		SOPGraph: SOPGraph{
			Name:     "welcome_graph",
			Scenario: "welcome",
			Version:  "2.0",
			Entry:    "start",
			Exits:    []string{"end"},
			Nodes: []SOPNode{
				{ID: "start", Type: SOPNodeTypeStart, Name: "开始", Next: []string{"greeting"}},
				{
					ID:          "greeting",
					Type:        SOPNodeTypeGreeting,
					Name:        "问候",
					Description: "标准化客户问候",
					Prompt:      "您好，欢迎咨询，我是您的专属顾问",
					Next:        []string{"inquire"},
				},
				{
					ID:          "inquire",
					Type:        SOPNodeTypeInquire,
					Name:        "询问需求",
					Description: "了解客户核心诉求",
					Prompt:      "请问您想了解什么产品或服务？",
					Next:        []string{"end"},
				},
				{ID: "end", Type: SOPNodeTypeEnd, Name: "结束"},
			},
		},
	}
}
