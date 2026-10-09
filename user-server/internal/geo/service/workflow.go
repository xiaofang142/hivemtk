package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"hivemtk-user/internal/geo/dto"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"
)

// StepExecutor 工作流步骤执行器类型
type StepExecutor func(ctx context.Context, step map[string]interface{}) (string, error)

// ProgressCallback 进度回调
type ProgressCallback func(workflowID string, stepName string, status string, progress int, message string)

// WorkflowService GEO 工作流自动化服务（迁移自 AIGEOTOOLS workflow/service.go）
type WorkflowService struct {
	wfRepo     repository.GeoWorkflowRepository
	execRepo   repository.GeoWorkflowExecutionRepository
	tplRepo    repository.GeoWorkflowTemplateRepository
	llm        *LLMAdapter
	executors  map[string]StepExecutor
	onProgress ProgressCallback
}

// NewWorkflowService 创建工作流服务
func NewWorkflowService(
	wfRepo repository.GeoWorkflowRepository,
	execRepo repository.GeoWorkflowExecutionRepository,
	tplRepo repository.GeoWorkflowTemplateRepository,
	chainRepo repository.GeoQueryChainRepository,
	taskRepo repository.GeoContentTaskRepository,
	adapter *LLMAdapter,
) *WorkflowService {
	s := &WorkflowService{
		wfRepo:    wfRepo,
		execRepo:  execRepo,
		tplRepo:   tplRepo,
		llm:       adapter,
		executors: make(map[string]StepExecutor),
	}
	s.registerBuiltinExecutors()

	deps := DecisionChainDeps{
		Probe:     NewDefaultSearchProbe(),
		ChainRepo: chainRepo,
		TaskRepo:  taskRepo,
	}
	s.RegisterDecisionChainExecutors(deps)
	return s
}

// RegisterExecutor 注册自定义步骤执行器
func (s *WorkflowService) RegisterExecutor(stepType string, exec StepExecutor) {
	s.executors[stepType] = exec
}

// OnProgressCallback 设置进度回调
func (s *WorkflowService) OnProgressCallback(cb ProgressCallback) {
	s.onProgress = cb
}

func (s *WorkflowService) setSteps(wf *model.GeoWorkflow, steps []dto.WorkflowStep) error {
	b, err := json.Marshal(steps)
	if err != nil {
		return err
	}
	wf.Steps = string(b)
	return nil
}

func (s *WorkflowService) toWorkflowResponse(wf *model.GeoWorkflow) (*dto.WorkflowResponse, error) {
	steps, err := wf.GetSteps()
	if err != nil {
		return nil, err
	}
	conditions, err := wf.GetConditions()
	if err != nil {
		return nil, err
	}
	resp := &dto.WorkflowResponse{
		ID:         wf.ID,
		Name:       wf.Name,
		Schedule:   wf.Schedule,
		Enabled:    wf.Enabled,
		Conditions: conditions,
		CreatedAt:  wf.CreatedAt,
		UpdatedAt:  wf.UpdatedAt,
	}

	stepDTOs := make([]dto.WorkflowStep, 0, len(steps))
	for _, st := range steps {
		b, _ := json.Marshal(st)
		var ws dto.WorkflowStep
		_ = json.Unmarshal(b, &ws)
		stepDTOs = append(stepDTOs, ws)
	}
	resp.Steps = stepDTOs
	return resp, nil
}

// Create 创建工作流
func (s *WorkflowService) Create(ctx context.Context, req *dto.SaveWorkflowRequest) (*dto.WorkflowResponse, error) {
	wf := &model.GeoWorkflow{
		Name:     req.Name,
		Schedule: req.Schedule,
		Enabled:  req.Enabled,
	}
	if err := s.setSteps(wf, req.Steps); err != nil {
		return nil, err
	}
	if err := wf.SetConditions(req.Conditions); err != nil {
		return nil, err
	}
	if err := s.wfRepo.Create(wf); err != nil {
		return nil, err
	}
	return s.toWorkflowResponse(wf)
}

// Update 更新工作流
func (s *WorkflowService) Update(ctx context.Context, id string, req *dto.SaveWorkflowRequest) (*dto.WorkflowResponse, error) {
	wf, err := s.wfRepo.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("工作流不存在: %w", err)
	}
	wf.Name = req.Name
	wf.Schedule = req.Schedule
	wf.Enabled = req.Enabled
	if err := s.setSteps(wf, req.Steps); err != nil {
		return nil, err
	}
	if err := wf.SetConditions(req.Conditions); err != nil {
		return nil, err
	}
	if err := s.wfRepo.Update(wf); err != nil {
		return nil, err
	}
	return s.toWorkflowResponse(wf)
}

// Delete 删除工作流
func (s *WorkflowService) Delete(ctx context.Context, id string) error {
	return s.wfRepo.Delete(id)
}

// Get 获取工作流
func (s *WorkflowService) Get(ctx context.Context, id string) (*dto.WorkflowResponse, error) {
	wf, err := s.wfRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	return s.toWorkflowResponse(wf)
}

// List 工作流列表
func (s *WorkflowService) List(ctx context.Context) ([]*dto.WorkflowResponse, error) {
	wfs, err := s.wfRepo.GetList()
	if err != nil {
		return nil, err
	}
	res := make([]*dto.WorkflowResponse, 0, len(wfs))
	for _, wf := range wfs {
		resp, err := s.toWorkflowResponse(wf)
		if err != nil {
			continue
		}
		res = append(res, resp)
	}
	return res, nil
}

// ListExecutions 获取执行记录
func (s *WorkflowService) ListExecutions(ctx context.Context, workflowID string) ([]*model.GeoWorkflowExecution, error) {
	return s.execRepo.GetByWorkflowID(workflowID)
}

// CreateTemplate 创建模板
func (s *WorkflowService) CreateTemplate(ctx context.Context, req *dto.SaveWorkflowTemplateRequest) (*dto.WorkflowTemplateResponse, error) {
	tpl := &model.GeoWorkflowTemplate{
		Name:        req.Name,
		Description: req.Description,
	}
	steps, _ := json.Marshal(req.Steps)
	tpl.Steps = string(steps)
	if err := s.tplRepo.Create(tpl); err != nil {
		return nil, err
	}
	return s.toTemplateResponse(tpl)
}

// ListTemplates 模板列表
func (s *WorkflowService) ListTemplates(ctx context.Context) ([]*dto.WorkflowTemplateResponse, error) {
	tpls, err := s.tplRepo.GetList()
	if err != nil {
		return nil, err
	}
	res := make([]*dto.WorkflowTemplateResponse, 0, len(tpls))
	for _, tpl := range tpls {
		resp, err := s.toTemplateResponse(tpl)
		if err != nil {
			continue
		}
		res = append(res, resp)
	}
	return res, nil
}

func (s *WorkflowService) toTemplateResponse(tpl *model.GeoWorkflowTemplate) (*dto.WorkflowTemplateResponse, error) {
	steps, err := tpl.GetSteps()
	if err != nil {
		return nil, err
	}
	stepDTOs := make([]dto.WorkflowStep, 0, len(steps))
	for _, st := range steps {
		b, _ := json.Marshal(st)
		var ws dto.WorkflowStep
		_ = json.Unmarshal(b, &ws)
		stepDTOs = append(stepDTOs, ws)
	}
	return &dto.WorkflowTemplateResponse{
		ID:          tpl.ID,
		Name:        tpl.Name,
		Description: tpl.Description,
		Steps:       stepDTOs,
		CreatedAt:   tpl.CreatedAt,
		UpdatedAt:   tpl.UpdatedAt,
	}, nil
}

// Run 执行工作流（核心引擎：条件判断 + 步骤执行 + jump 跳转）
func (s *WorkflowService) Run(ctx context.Context, id string) (*dto.RunWorkflowResponse, error) {
	wf, err := s.wfRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	steps, err := wf.GetSteps()
	if err != nil {
		return nil, err
	}

	exec := &model.GeoWorkflowExecution{
		WorkflowID: id,
		Status:     "running",
		StartedAt:  time.Now(),
	}
	if err := s.execRepo.Create(exec); err != nil {
		return nil, err
	}

	defer func() {
		if exec.Status == "running" {
			completedAt := time.Now()
			exec.CompletedAt = &completedAt
			exec.Status = "failed"
			if exec.Error == "" {
				exec.Error = "工作流执行异常中断"
			}
		}
		if err := s.execRepo.Update(exec); err != nil {
			logger.Errorf("工作流执行记录落库失败 exec_id=%s: %v", exec.ID, err)
		}
	}()

	results := make([]dto.StepResult, 0, len(steps))
	stepResults := map[string]*dto.StepResult{}
	totalSteps := len(steps)
	if totalSteps == 0 {
		totalSteps = 1
	}

	emitProgress := func(stepName, status string, progress int, message string) {
		if s.onProgress != nil {
			s.onProgress(id, stepName, status, progress, message)
		}
	}

	evalCondition := func(expr string, sr map[string]*dto.StepResult) bool {
		if expr == "" {
			return true
		}
		for _, op := range []string{"==", ">=", "<=", ">", "<"} {
			parts := strings.SplitN(expr, op, 2)
			if len(parts) == 2 {
				lhs := strings.TrimSpace(parts[0])
				rhs := strings.TrimSpace(parts[1])
				if r, ok := sr[lhs]; ok {
					if op == "==" {
						return r.Result == rhs || r.Status == rhs
					}
					val, err1 := strconv.ParseFloat(strings.TrimSpace(r.Result), 64)
					th, err2 := strconv.ParseFloat(rhs, 64)
					if err1 == nil && err2 == nil {
						switch op {
						case ">=":
							return val >= th
						case "<=":
							return val <= th
						case ">":
							return val > th
						case "<":
							return val < th
						}
					}
				}
				return false
			}
		}
		if r, ok := sr[expr]; ok && r.Status == "success" {
			return true
		}
		return false
	}

	jumpMap := map[string]int{}
	for i, step := range steps {
		if name, ok := step["name"].(string); ok && name != "" {
			jumpMap[name] = i
		}
	}

	var anyErr error
	idx := 0

	const maxIterations = 1000
	iterations := 0
	for idx < len(steps) {
		iterations++
		if iterations > maxIterations {
			anyErr = fmt.Errorf("工作流执行超过最大迭代次数 %d（检查 jump_to 是否构成死循环）", maxIterations)
			break
		}
		step := steps[idx]
		stepName := stepString(step, "name")
		stepType, _ := step["type"].(string)

		conditionKey, _ := step["condition"].(string)
		if conditionKey != "" && !evalCondition(conditionKey, stepResults) {
			idx++
			continue
		}

		sr := &dto.StepResult{
			StepName:  stepName,
			StepType:  stepType,
			Status:    "running",
			StartedAt: time.Now(),
		}
		stepResults[stepName] = sr

		progress := int(float64(idx+1) / float64(totalSteps) * 100)
		emitProgress(stepName, "running", progress, "执行步骤: "+stepName)

		executor, ok := s.executors[stepType]
		if !ok {
			sr.Status = "failed"
			sr.Error = "no executor for step type: " + stepType
			emitProgress(stepName, "failed", progress, sr.Error)
			results = append(results, *sr)
			anyErr = fmt.Errorf("step %s: %s", stepName, sr.Error)
			break
		}

		stepWithResults := make(map[string]interface{}, len(step)+2)
		for k, v := range step {
			stepWithResults[k] = v
		}

		if params, ok := step["params"].(map[string]interface{}); ok {
			for k, v := range params {
				if _, exists := stepWithResults[k]; !exists {
					stepWithResults[k] = v
				}
			}
		}
		prevResults := map[string]string{}
		for name, r := range stepResults {
			prevResults[name] = r.Result
		}
		stepWithResults["_step_results"] = prevResults

		result, err := executor(context.WithoutCancel(ctx), stepWithResults)
		now := time.Now()
		sr.CompletedAt = &now
		if err != nil {
			sr.Status = "failed"
			sr.Error = err.Error()
			emitProgress(stepName, "failed", progress, err.Error())
			results = append(results, *sr)
			anyErr = fmt.Errorf("step %s: %w", stepName, err)
			break
		}
		sr.Status = "success"
		sr.Result = result
		emitProgress(stepName, "success", progress, "步骤 "+stepName+" 完成")
		results = append(results, *sr)

		if jumpTarget, ok := step["jump_to"].(string); ok && jumpTarget != "" {
			if targetIdx, exists := jumpMap[jumpTarget]; exists {
				idx = targetIdx
				continue
			}
		}
		idx++
	}

	completedAt := time.Now()
	exec.CompletedAt = &completedAt
	if anyErr != nil {
		exec.Status = "failed"
		exec.Error = anyErr.Error()
	} else {
		exec.Status = "success"
	}
	b, _ := json.Marshal(results)
	exec.Result = string(b)

	resp := &dto.RunWorkflowResponse{
		ID:          exec.ID,
		WorkflowID:  id,
		Status:      exec.Status,
		Result:      results,
		Error:       exec.Error,
		StartedAt:   exec.StartedAt,
		CompletedAt: exec.CompletedAt,
	}
	return resp, anyErr
}

func (s *WorkflowService) registerBuiltinExecutors() {
	gen := s.llm

	s.executors["content_generate"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		topic, _ := step["topic"].(string)
		brand, _ := step["brand"].(string)
		advantages, _ := step["advantages"].(string)
		keyword, _ := step["keyword"].(string)
		platform, _ := step["platform"].(string)
		if topic == "" {
			topic = "general"
		}
		if brand == "" {
			brand = "our brand"
		}
		prompt := fmt.Sprintf("请撰写一篇关于\"%s\"的高质量文章，要求：\n1. 品牌自然融入：%s\n2. 核心优势：%s\n3. 目标平台：%s\n4. 关键词：%s\n5. 字数800-1200字，结构清晰，包含标题、正文、结语\n6. 使用专业但亲切的语调", topic, brand, advantages, platform, keyword)

		prompt = EnhancePromptWithIntent(prompt, keyword)
		if gen != nil {
			resp, err := gen.Generate(ctx, "", prompt, 0.7, 3000)
			if err != nil {
				logger.Warn(fmt.Sprintf("[GEO Workflow] content_generate LLM 不可用，降级模板内容: %v", err))
				return heuristicWorkflowContent(topic, brand, advantages, keyword, platform), nil
			}
			return resp.Content, nil
		}
		return heuristicWorkflowContent(topic, brand, advantages, keyword, platform), nil
	}

	s.executors["content_score"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		content := extractStepContent(step)
		brand, _ := step["brand"].(string)
		minScore := 70.0
		if v, ok := step["min_score"].(float64); ok && v > 0 {
			minScore = v
		}
		if gen != nil {
			prompt := fmt.Sprintf("请对以下内容进行评分（满分100分），评分维度包括：结构完整性(25分)、品牌提及自然度(25分)、权威性信号(25分)、引用与数据支撑(25分)。\n品牌：%s\n内容：\n---\n%s\n---\n请仅返回一个数字分数。", brand, content)
			resp, err := gen.Generate(ctx, "", prompt, 0.3, 500)
			if err != nil {
				logger.Warn(fmt.Sprintf("[GEO Workflow] content_score LLM 不可用，降级最低分 %.0f: %v", minScore, err))
				return fmt.Sprintf("%.0f", minScore), nil
			}
			scoreStr := strings.TrimSpace(resp.Content)
			scoreStr = strings.ReplaceAll(scoreStr, "分", "")
			scoreStr = strings.ReplaceAll(scoreStr, "score:", "")
			scoreStr = strings.ReplaceAll(scoreStr, "Score:", "")
			scoreStr = strings.TrimSpace(scoreStr)
			if score, err := strconv.ParseFloat(scoreStr, 64); err == nil {
				if score < minScore {
					return fmt.Sprintf("%.0f", score), fmt.Errorf("score %.0f below threshold %.0f", score, minScore)
				}
				return fmt.Sprintf("%.0f", score), nil
			}
		}
		return fmt.Sprintf("%.0f", minScore), nil
	}

	s.executors["eeat_enhance"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		content := extractStepContent(step)
		brand, _ := step["brand"].(string)
		if brand == "" {
			brand = "unknown"
		}
		if gen != nil {
			prompt := fmt.Sprintf("请对以下内容进行E-E-A-T（经验、专业、权威、可信）增强。\n品牌：%s\n原始内容：\n---\n%s\n---\n请在不改变核心观点的前提下：\n1. 添加作者资质和专业背景说明\n2. 增加具体案例和实践经验描述\n3. 引用权威来源和行业数据\n4. 强化信任感和可靠性信号\n5. 保持内容流畅自然\n返回增强后的完整内容。", brand, content)
			resp, err := gen.Generate(ctx, "", prompt, 0.5, 4000)
			if err != nil {
				logger.Warn("[GEO Workflow] eeat_enhance LLM 不可用，降级返回原文")
				return content, nil
			}
			return resp.Content, nil
		}
		return content, nil
	}

	s.executors["fact_density_enhance"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		content := extractStepContent(step)
		targetDensity := 0.8
		if v, ok := step["target_density"].(float64); ok && v > 0 {
			targetDensity = v
		}
		if gen != nil {
			prompt := fmt.Sprintf("请对以下内容进行事实密度增强（目标密度 %.0f%%）。\n原始内容：\n---\n%s\n---\n请在保持原有结构和风格的基础上：\n1. 增加具体的数据、统计数字和百分比\n2. 添加具体的案例名称、产品型号、人物姓名\n3. 引用具体的时间、地点、机构名称\n4. 使用精确的数值替代模糊描述\n5. 确保新增事实与内容主题相关\n返回增强后的完整内容。", targetDensity*100, content)
			resp, err := gen.Generate(ctx, "", prompt, 0.5, 4000)
			if err != nil {
				logger.Warn("[GEO Workflow] fact_density_enhance LLM 不可用，降级返回原文")
				return content, nil
			}
			return resp.Content, nil
		}
		return content, nil
	}

	s.executors["verify"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		brand, _ := step["brand"].(string)
		queriesAny, _ := step["queries"].([]interface{})
		queries := make([]string, 0)
		for _, q := range queriesAny {
			if v, ok := q.(string); ok {
				queries = append(queries, v)
			}
		}
		if brand == "" {
			brand = "unknown"
		}
		if len(queries) == 0 {
			queries = []string{brand + " 怎么样", brand + " 评测", brand + " 推荐", "使用" + brand + "的体验", brand + " vs 竞品"}
		}
		if gen != nil {
			mentionCount := 0
			for _, q := range queries {
				prompt := fmt.Sprintf("搜索查询：\"%s\"\n请基于你的知识回答该搜索查询，判断品牌\"%s\"是否在结果中被提及。\n回答\"提及\"或\"未提及\"，并简要说明。", q, brand)
				resp, err := gen.Generate(ctx, "", prompt, 0.3, 500)
				if err != nil {
					continue
				}
				if strings.Contains(resp.Content, "提及") || strings.Contains(resp.Content, brand) {
					mentionCount++
				}
			}
			rate := float64(mentionCount) / float64(len(queries))
			return fmt.Sprintf("%.4f", rate), nil
		}
		return "0.5000", nil
	}

	s.executors["trigger"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		return "trigger_fired", nil
	}
	s.executors["action"] = func(ctx context.Context, step map[string]interface{}) (string, error) {

		if prev, ok := step["_step_results"].(map[string]string); ok {
			for _, v := range prev {
				if v != "" {
					return v, nil
				}
			}
		}
		name, _ := step["name"].(string)
		return "action_done:" + name, nil
	}
	s.executors["decision"] = func(ctx context.Context, step map[string]interface{}) (string, error) {

		cond, _ := step["condition"].(string)
		jumpTo, _ := step["jump_to"].(string)
		if jumpTo != "" {
			return "decision_jump_to:" + jumpTo, nil
		}
		return "decision_evaluated:" + cond, nil
	}
	s.executors["end"] = func(ctx context.Context, step map[string]interface{}) (string, error) {
		return "workflow_complete", nil
	}
}

func stepString(step map[string]interface{}, key string) string {
	if v, ok := step[key].(string); ok {
		return v
	}
	return ""
}

func extractStepContent(step map[string]interface{}) string {
	if c, ok := step["content"].(string); ok && c != "" {
		return c
	}
	if c, ok := step["article"].(string); ok && c != "" {
		return c
	}
	if pr, ok := step["_step_results"].(map[string]string); ok {
		for _, result := range pr {
			if result != "" {
				return result
			}
		}
	}
	return ""
}

// heuristicWorkflowContent LLM 不可用时的降级内容生成
func heuristicWorkflowContent(topic, brand, advantages, keyword, platform string) string {
	if topic == "" || topic == "general" {
		topic = "行业解决方案"
	}
	if brand == "" || brand == "our brand" {
		brand = "HiveMTK"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s：%s 的%s\n\n", brand, keyword, topic))
	sb.WriteString(fmt.Sprintf("## 引言\n\n%s 长期深耕%s领域，凭借扎实的行业经验与持续的技术创新，为企业和用户提供可靠、高效的%s。", brand, topic, topic))
	if advantages != "" {
		sb.WriteString(fmt.Sprintf("核心差异优势包括：%s。", advantages))
	}
	sb.WriteString(fmt.Sprintf("\n\n## 产品与服务\n\n我们围绕%s构建了完整的解决方案体系：\n\n1. 行业理解：深入洞察目标客群的真实需求与使用场景\n2. 产品能力：功能完备、易于上手、持续迭代\n3. 服务体系：专业团队提供从部署到运维的全周期支持", topic))
	sb.WriteString(fmt.Sprintf("\n\n## 客户价值\n\n选择%s意味着获得：\n\n- 可量化的效率提升与成本优化\n- 稳定可靠的产品与服务保障\n- 面向未来的持续升级能力", brand))
	sb.WriteString(fmt.Sprintf("\n\n## 结语\n\n如果你正在评估%s方向的产品，欢迎深入了解%s，我们一起把选择变成成果。", topic, brand))
	if platform != "" {
		sb.WriteString(fmt.Sprintf("（本文发布于 %s）", platform))
	}
	return sb.String()
}
