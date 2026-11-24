package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hivemtk-user/internal/pkg/utils/logger"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tracing"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	WorkflowNodeTypeTrigger   = "trigger"
	WorkflowNodeTypeAction    = "action"
	WorkflowNodeTypeCondition = "condition"
	WorkflowNodeTypeSubflow   = "subflow"
)

// 工作流编排服务的 sentinel error。
//
// 注意：与 workflow_orchestrator_deep_test.go 中 TestWO_GetLatestPublished_NoPublished
// 的断言保持兼容 —— 该用例使用 errors.Is(err, service.ErrNoPublishedVersion) 判定。
var (
	ErrNoPublishedVersion  = errors.New("no published version found")
	ErrExecutionNotRunning = errors.New("execution is not in running state")
	// ErrPublishedVersionImmutable 已发布/已归档版本的 definition 是执行绑定快照，
	// 不可原位覆盖；改稿必须新建版本（FA-02）。
	ErrPublishedVersionImmutable = errors.New("published version definition is immutable; create a new version instead")
)

// WorkflowOrchestratorService 工作流编排服务
type WorkflowOrchestratorService struct {
	versionRepo  *repository.WorkflowVersionRepository
	execRepo     *repository.WorkflowExecutionRepository
	nodeExecRepo *repository.WorkflowNodeExecutionRepository
	dispatcher   *WorkflowDispatcher
	// nodeRegistry 用于创建/更新/发布三道闸的节点类型校验（装配点注入，FA-02）。
	nodeRegistry *WorkflowNodeExecutorRegistry
}

// NewWorkflowOrchestratorService 创建工作流编排服务
func NewWorkflowOrchestratorService(
	versionRepo *repository.WorkflowVersionRepository,
	execRepo *repository.WorkflowExecutionRepository,
	nodeExecRepo *repository.WorkflowNodeExecutionRepository,
) *WorkflowOrchestratorService {
	return &WorkflowOrchestratorService{
		versionRepo:  versionRepo,
		execRepo:     execRepo,
		nodeExecRepo: nodeExecRepo,
	}
}

// SetDispatcher 注入工作流执行调度器（避免循环依赖，启动期装配后调用）
func (s *WorkflowOrchestratorService) SetDispatcher(d *WorkflowDispatcher) {
	if s == nil {
		return
	}
	s.dispatcher = d
}

// SetNodeRegistry 注入节点执行器注册表，供定义校验判断节点类型是否已注册（FA-02）。
func (s *WorkflowOrchestratorService) SetNodeRegistry(r *WorkflowNodeExecutorRegistry) {
	if s == nil {
		return
	}
	s.nodeRegistry = r
}

// CreateVersion 创建工作流版本
func (s *WorkflowOrchestratorService) CreateVersion(ctx context.Context, req *dto.WorkflowVersionCreateRequest) (*model.WorkflowVersion, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	if req == nil || req.WorkflowID == "" {
		return nil, utils.ErrInvalidInput
	}
	if req.Definition == nil {
		return nil, utils.ErrInvalidInput
	}
	// 创建闸：未知节点类型/未知动作/非法图在入口即拒绝，不放进草稿（FA-02）。
	// FA-WF-08：草稿允许空定义/缺 trigger，但未知节点类型/未知动作/非法图入口即拒 FA-02；
	// 严格完整性（≥1 节点 + trigger）留给发布闸 PublishVersion。
	if err := ValidateWorkflowDefinitionForDraft(req.Definition, s.nodeRegistry); err != nil {
		return nil, fmt.Errorf("%w: %v", utils.ErrInvalidInput, err)
	}

	count, err := s.versionRepo.CountByWorkflowID(ctx, req.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("count versions failed: %w", err)
	}

	version := &model.WorkflowVersion{
		WorkflowID:  req.WorkflowID,
		Version:     int(count) + 1,
		Name:        req.Name,
		Description: req.Description,
		Definition:  req.Definition,
		Status:      model.WorkflowStatusDraft,
		Changelog:   req.Changelog,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := s.versionRepo.Save(ctx, version); err != nil {
		return nil, fmt.Errorf("save version failed: %w", err)
	}

	return version, nil
}

// GetVersion 获取工作流版本详情
func (s *WorkflowOrchestratorService) GetVersion(ctx context.Context, id uint) (*model.WorkflowVersion, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	return s.versionRepo.GetByID(ctx, id)
}

// GetLatestPublished 获取最新已发布版本
//
// 当无已发布版本时返回 ErrNoPublishedVersion sentinel error（而非 gorm.ErrRecordNotFound），
// 便于上层/controller 用 errors.Is 精确判定。
func (s *WorkflowOrchestratorService) GetLatestPublished(ctx context.Context, workflowID string) (*model.WorkflowVersion, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	if workflowID == "" {
		return nil, utils.ErrInvalidInput
	}
	v, err := s.versionRepo.GetLatestPublished(ctx, workflowID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoPublishedVersion
		}
		return nil, err
	}
	return v, nil
}

// ListVersions 列出工作流版本
func (s *WorkflowOrchestratorService) ListVersions(ctx context.Context, workflowID string) ([]model.WorkflowVersion, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	return s.versionRepo.ListVersions(ctx, workflowID)
}

// ListAll 列出所有工作流版本（用于列表页，支持按 workflow_id 和 status 过滤）
func (s *WorkflowOrchestratorService) ListAll(ctx context.Context, workflowID, status, keyword string, page, pageSize int) ([]model.WorkflowVersion, int64, error) {
	if s == nil {
		return nil, 0, utils.ErrServiceNotInit
	}
	return s.versionRepo.ListAll(ctx, workflowID, status, keyword, page, pageSize)
}

// UpdateVersion 更新工作流版本
//
// draft 版本允许改稿（definition 仍需过校验闸）；published/archived 版本的
// definition 是执行绑定快照，拒绝原位覆盖（FA-02），仅元数据可更新。
func (s *WorkflowOrchestratorService) UpdateVersion(ctx context.Context, id uint, req *dto.WorkflowVersionUpdateRequest) error {
	if s == nil {
		return utils.ErrServiceNotInit
	}
	if id == 0 || req == nil || req.Definition == nil {
		return utils.ErrInvalidInput
	}
	version, err := s.versionRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if version.Status != model.WorkflowStatusDraft && !definitionEqual(version.Definition, req.Definition) {
		return ErrPublishedVersionImmutable
	}
	if err := ValidateWorkflowDefinition(req.Definition, s.nodeRegistry); err != nil {
		return fmt.Errorf("%w: %v", utils.ErrInvalidInput, err)
	}

	// FA-WF-07：用 Revision 整数作乐观锁 CAS，避免"读 draft → 发布者置 published
	// → 旧草稿整行 Save 覆盖新状态/定义"的交错。Revision 随每次改稿递增（仓储层 +1）。
	// 时间戳精度不足（并发更新可能同秒同值），故改用单调 Revision。
	expectedRevision := version.Revision
	version.Name = req.Name
	version.Description = req.Description
	version.Definition = req.Definition
	version.Changelog = req.Changelog

	// 草稿改稿用 status=draft CAS（拦住"读到 draft→别人发布→旧草稿覆盖"）；
	// 已发布/归档版本只允许同定义元数据更新（definitionEqual 已在上面校验），用 revision CAS。
	var rows int64
	if version.Status == model.WorkflowStatusDraft {
		rows, err = s.versionRepo.UpdateDraft(ctx, version, expectedRevision)
	} else {
		rows, err = s.versionRepo.UpdateMetadataCAS(ctx, version, expectedRevision)
	}
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}

// definitionEqual 判断两份定义 JSON 是否内容一致（键序无关）。
// 用于放行"已发布版本提交了与现网完全相同 definition"的幂等更新。
func definitionEqual(a, b model.JSONMap) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	na, nerr := ParseWorkflowDefinition(a)
	nb, berr := ParseWorkflowDefinition(b)
	if nerr != nil || berr != nil {
		// 解析失败时退回字节级比较，宁可拒绝不可误放。
		aj, err1 := jsonMarshal(a)
		bj, err2 := jsonMarshal(b)
		return err1 == nil && err2 == nil && string(aj) == string(bj)
	}
	aj, err1 := jsonMarshal(na)
	bj, err2 := jsonMarshal(nb)
	return err1 == nil && err2 == nil && string(aj) == string(bj)
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// PublishVersion 发布工作流版本
//
// 发布闸：definition 未通过服务端校验的版本不允许进入 published 状态（FA-02）。
func (s *WorkflowOrchestratorService) PublishVersion(ctx context.Context, id uint) error {
	if s == nil {
		return utils.ErrServiceNotInit
	}
	if id == 0 {
		return utils.ErrInvalidInput
	}
	version, err := s.versionRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := ValidateWorkflowDefinition(version.Definition, s.nodeRegistry); err != nil {
		return fmt.Errorf("%w: %v", utils.ErrInvalidInput, err)
	}
	// FA-WF-07：仅从 draft→published  CAS 提交；若已被并发更新/发布，rows==0 返回冲突。
	rows, err := s.versionRepo.TransitionStatus(ctx, id, model.WorkflowStatusDraft, model.WorkflowStatusPublished)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}

// ArchiveVersion 归档工作流版本
func (s *WorkflowOrchestratorService) ArchiveVersion(ctx context.Context, id uint) error {
	if s == nil {
		return utils.ErrServiceNotInit
	}
	if id == 0 {
		return utils.ErrInvalidInput
	}
	// FA-WF-07：先读当前状态作 CAS 的 from，避免整行覆盖与并发更新/发布交错。
	version, err := s.versionRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	// 已归档版本再次归档：from=to=archived 会让状态 CAS 恒匹配，需显式拒绝，
	// 否则重复归档静默成功（前端误以为状态变化）。
	if version.Status == model.WorkflowStatusArchived {
		return ErrVersionConflict
	}
	rows, err := s.versionRepo.TransitionStatus(ctx, id, version.Status, model.WorkflowStatusArchived)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrVersionConflict
	}
	return nil
}

// ErrVersionConflict FA-WF-07：版本在读取后被并发修改（状态变更或另一次改稿），
// 本次 CAS 提交未命中期望状态/时间戳。调用方应回读最新状态后重试，不能把旧草稿
// 覆盖到已发布/已归档版本上。
var ErrVersionConflict = errors.New("workflow version: 版本已被并发修改，请重试")

// ErrVersionReferenced FA-36：版本被执行引用（在途或历史），不可硬删。
var ErrVersionReferenced = errors.New("workflow version: 已被执行引用，不可删除（在途与历史执行依赖其不可变定义）")

// DeleteVersion 删除工作流版本
//
// FA-36：删除前检查执行引用——workflow_executions 只存 workflow_id+version
// （无外键，dispatcher 每次启动/重试都按这两个值重载定义），被引用版本的
// 硬删会让恢复无法加载定义、历史失去可重放定义。有引用一律拒绝（可诊断）。
func (s *WorkflowOrchestratorService) DeleteVersion(ctx context.Context, id uint) error {
	if s == nil {
		return utils.ErrServiceNotInit
	}
	if id == 0 {
		return utils.ErrInvalidInput
	}
	version, err := s.versionRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	n, err := s.execRepo.CountByWorkflowVersion(ctx, version.WorkflowID, version.Version)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w：当前 %d 条执行（含在途/历史）引用 version %d", ErrVersionReferenced, n, version.Version)
	}
	return s.versionRepo.DeleteByID(ctx, id)
}

// Execute 执行工作流
//
// 创建执行实例后，若已注入 WorkflowDispatcher 则通过 go d.Run(ctx, exec.ID, traceID)
// 异步驱动全部节点执行；dispatcher 为 nil 时仅创建记录（保持旧行为，便于单测）。
func (s *WorkflowOrchestratorService) Execute(ctx context.Context, req *dto.WorkflowExecuteRequest) (*model.WorkflowExecution, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	if req == nil || req.WorkflowID == "" {
		return nil, utils.ErrInvalidInput
	}
	if req.TriggerPayload == nil {
		req.TriggerPayload = model.JSONMap{}
	}

	version, err := s.versionRepo.GetLatestPublished(ctx, req.WorkflowID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoPublishedVersion
		}
		return nil, fmt.Errorf("get latest published version failed: %w", err)
	}

	exec := &model.WorkflowExecution{
		WorkflowID:     req.WorkflowID,
		Version:        version.Version,
		TriggerPayload: req.TriggerPayload,
		Status:         model.WorkflowExecRunning,
		CurrentNodeID:  "",
		Context:        model.JSONMap{},
		StartedAt:      time.Now(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.execRepo.Create(ctx, exec); err != nil {
		return nil, fmt.Errorf("create execution failed: %w", err)
	}

	if s.dispatcher != nil {
		traceID := tracing.TraceIDFromContext(ctx)
		if traceID == "" {
			traceID = fmt.Sprintf("wf-%d-%d", exec.ID, time.Now().UnixNano())
		}
		// FA-WF-06：受理与后台执行生命周期分离——执行已落库（受理完成），
		// 后续 Run 的读写不能随 HTTP 请求结束被取消（客户端断开 ⇒ 首次读库
		// 即 canceled，执行永久停在无节点的 running）。WithoutCancel 保留
		// 租户/追踪取值、只摘掉取消信号；traceID 已在上方显式提取。
		runCtx := context.WithoutCancel(ctx)
		s.dispatcher.Run(runCtx, exec.ID, traceID)
	}

	return exec, nil
}

// RecoverStuckExecutions FA-WF-06：恢复扫描——把超过阈值的卡死 running 执行
// 用 FA-WF-04 的栅栏写标为明确 failed（可查询、可重试的终态），不盲目重跑
// （无 per-node 副作用幂等前，重跑等于重复副作用）。返回标记条数。
func (s *WorkflowOrchestratorService) RecoverStuckExecutions(ctx context.Context, threshold time.Time, limit int) (int64, error) {
	if s == nil {
		return 0, utils.ErrServiceNotInit
	}
	stuck, err := s.execRepo.FindStuck(ctx, threshold, limit)
	if err != nil {
		return 0, err
	}
	var marked int64
	for i := range stuck {
		applied, err := s.execRepo.UpdateStatusFenced(ctx, stuck[i].ID, map[string]any{
			"status":      model.WorkflowExecFailed,
			"error":       "recovery: 执行中断（疑似受理后崩溃/请求取消），由恢复扫描标记为失败；重试请重新触发",
			"finished_at": time.Now(),
		}, []string{model.WorkflowExecRunning})
		if err != nil {
			logger.Ctx(ctx).Error().Uint("execution_id", stuck[i].ID).Err(err).Msg("stuck execution recovery write failed")
			continue
		}
		if applied {
			marked++
			logger.Ctx(ctx).Warn().Uint("execution_id", stuck[i].ID).Msg("stuck running execution marked failed by recovery sweep")
		}
	}
	return marked, nil
}

// GetExecution 获取执行详情
func (s *WorkflowOrchestratorService) GetExecution(ctx context.Context, execID uint) (*model.WorkflowExecution, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	if execID == 0 {
		return nil, utils.ErrInvalidInput
	}
	exec, err := s.execRepo.GetByID(ctx, execID)
	if err != nil {
		return nil, err
	}

	nodeExecs, err := s.nodeExecRepo.ListByExecutionID(ctx, execID)
	if err != nil {
		return nil, err
	}

	_ = nodeExecs
	return exec, nil
}

// GetNodeExecutions 获取节点执行明细
func (s *WorkflowOrchestratorService) GetNodeExecutions(ctx context.Context, execID uint) ([]model.WorkflowNodeExecution, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	if execID == 0 {
		return nil, utils.ErrInvalidInput
	}
	return s.nodeExecRepo.ListByExecutionID(ctx, execID)
}

// ListExecutions 列出执行实例
func (s *WorkflowOrchestratorService) ListExecutions(ctx context.Context, workflowID, status string, page, pageSize int) ([]model.WorkflowExecution, int64, error) {
	if s == nil {
		return nil, 0, utils.ErrServiceNotInit
	}
	return s.execRepo.List(ctx, workflowID, status, page, pageSize)
}

// StopExecution 停止执行
//
// 仅当执行处于 running 状态时方可停止，否则返回 ErrExecutionNotRunning sentinel error。
// 终止后 dispatcher 的 runExecution 循环会在下一轮检测到 status 变化自动退出。
func (s *WorkflowOrchestratorService) StopExecution(ctx context.Context, execID uint) error {
	if s == nil {
		return utils.ErrServiceNotInit
	}
	if execID == 0 {
		return utils.ErrInvalidInput
	}
	if _, err := s.execRepo.GetByID(ctx, execID); err != nil {
		// 行不存在透传仓储错误；存在性校验先行，CAS 里 0 行=非 running。
		return err
	}

	// FA-WF-04：停止用 CAS（running → terminated）原子迁移——先读后整行写的窗口里
	// 执行可能已终态，旧实现会把 completed/failed 覆写成 terminated。
	// 0 行受影响 = 行不存在或已非 running，两者都按既有哨兵回报。
	applied, err := s.execRepo.UpdateStatusFenced(ctx, execID, map[string]any{
		"status":      model.WorkflowExecTerminated,
		"finished_at": time.Now(),
		"error":       "manual stopped",
	}, []string{model.WorkflowExecRunning})
	if err != nil {
		return err
	}
	if !applied {
		return ErrExecutionNotRunning
	}
	return nil
}

// FindStuckExecutions 查找卡死的执行
func (s *WorkflowOrchestratorService) FindStuckExecutions(ctx context.Context, threshold time.Time, limit int) ([]model.WorkflowExecution, error) {
	if s == nil {
		return nil, utils.ErrServiceNotInit
	}
	return s.execRepo.FindStuck(ctx, threshold, limit)
}
