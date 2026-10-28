package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/datatypes"
)

// EgoFlowService 流程域（需求②：用户自建草稿 → 扫描门 → 试跑 → 发布/提交平台审核）。
//
// 纪律（决策 5/6）：
//   - 保存即扫描（ScanScript 唯一实现双端同源）；block 未清零禁止试跑与发布；
//   - 发布产出不可变版本（version 自增），draft 行继续演化；
//   - market 通道（提交平台审核）M2 只到 submitted 态 + 人工审核清单，上架分发在 T3。

type EgoFlowService struct {
	flowRepo EgoFlowRepository
	execSvc  *EgoExecutionService
}

func NewEgoFlowService(flowRepo EgoFlowRepository, execSvc *EgoExecutionService) *EgoFlowService {
	return &EgoFlowService{flowRepo: flowRepo, execSvc: execSvc}
}

// SaveRequest 保存草稿
type SaveRequest struct {
	Name         string          `json:"name"`
	Domains      []string        `json:"domains"`
	Writes       []string        `json:"writes"`
	ParamsSchema json.RawMessage `json:"params_schema"`
	Script       string          `json:"script"`
}

// Save 创建或更新草稿（id=0 创建；否则归属校验后更新）。返回扫描报告。
func (s *EgoFlowService) Save(ctx context.Context, userID, flowID uint, req SaveRequest) (*model.EgoliteFlow, *ScanReport, error) {
	if req.Name == "" {
		return nil, nil, fmt.Errorf("flow 名称必填")
	}
	if len(req.Domains) == 0 {
		return nil, nil, fmt.Errorf("domains 必填（域名白名单，与 manifest 同纪律）")
	}
	for _, d := range req.Domains {
		if d == "*" {
			return nil, nil, fmt.Errorf("用户 flow 不允许整站通配 *（manifest.domains 才可声明）")
		}
	}
	if req.Script == "" {
		return nil, nil, fmt.Errorf("script 必填")
	}

	// 保存即扫描（决策 5：扫描门是必经路径，不是可选审查）
	report := ScanScript(req.Script)

	domainsJSON, _ := json.Marshal(req.Domains)
	writesJSON, _ := json.Marshal(req.Writes)
	sets := map[string]any{
		"name":          req.Name,
		"domains":       domainsJSON,
		"writes":        writesJSON,
		"params_schema": json.RawMessage(req.ParamsSchema),
		"script":        req.Script,
		"scan_passed":   report.Passed,
		"scan_report":   datatypes.JSON(mustJSON(report)),
		"updated_at":    time.Now(),
	}

	var f *model.EgoliteFlow
	if flowID == 0 {
		f = &model.EgoliteFlow{
			UserID:      userID,
			Name:        req.Name,
			DomainsJSON: domainsJSON,
			WritesJSON:  writesJSON,
			Script:      req.Script,
			Status:      model.EgoFlowDraft,
			Version:     1,
			ScanPassed:  report.Passed,
			ScanReport:  datatypes.JSON(mustJSON(report)),
		}
		if err := s.flowRepo.Create(ctx, f); err != nil {
			return nil, nil, err
		}
	} else {
		if _, err := s.flowRepo.GetForUser(ctx, flowID, userID); err != nil {
			return nil, nil, err
		}
		if err := s.flowRepo.Update(ctx, flowID, userID, sets); err != nil {
			return nil, nil, err
		}
		f, _ = s.flowRepo.GetForUser(ctx, flowID, userID)
	}
	return f, &report, nil
}

// DryRun 试跑（draft 通道）：block 未清零直接拒绝（决策 5 的强制点）
func (s *EgoFlowService) DryRun(ctx context.Context, userID, flowID uint, params json.RawMessage, spaceName string, timeoutSec int) (*model.EgoliteExecution, error) {
	f, err := s.flowRepo.GetForUser(ctx, flowID, userID)
	if err != nil {
		return nil, err
	}
	if !f.ScanPassed {
		return nil, fmt.Errorf("扫描门未通过（存在 block 级命中），先修正脚本再试跑")
	}
	if f.Script == "" {
		return nil, fmt.Errorf("flow 脚本为空")
	}
	// 决策 4 同口径（specs/014 T008）：声明了写动作的试跑同样逐步确认——
	// 客户端不可自行关闭；与 market 分支「资产 writes 非空 ⇒ 强制 require」对齐。
	confirmPolicy := "none"
	var writes []string
	if json.Unmarshal(f.WritesJSON, &writes) == nil && len(writes) > 0 {
		confirmPolicy = "require"
	}
	return s.execSvc.Start(ctx, userID, StartRequest{
		Origin:        "draft",
		FlowID:        flowID,
		Params:        params,
		SpaceName:     spaceName,
		ConfirmPolicy: confirmPolicy,
		TimeoutSec:    timeoutSec,
	})
}

// Publish 发布草稿为版本（version 自增；published 不可再改脚本——改动即新 draft）
func (s *EgoFlowService) Publish(ctx context.Context, userID, flowID uint) (*model.EgoliteFlow, error) {
	f, err := s.flowRepo.GetForUser(ctx, flowID, userID)
	if err != nil {
		return nil, err
	}
	if !f.ScanPassed {
		return nil, fmt.Errorf("扫描门未通过，禁止发布")
	}
	if f.Status == model.EgoFlowPublished || f.Status == model.EgoFlowSubmitted {
		// 重复发布 = 版本自增（不可变版本语义）
	}
	return s.republish(ctx, f)
}

func (s *EgoFlowService) republish(ctx context.Context, f *model.EgoliteFlow) (*model.EgoliteFlow, error) {
	newVer := f.Version + 1
	if f.Status == model.EgoFlowDraft {
		newVer = f.Version // 首次发布保持 v1
	}
	err := s.flowRepo.Update(context.Background(), f.ID, f.UserID, map[string]any{
		"status":     model.EgoFlowPublished,
		"version":    newVer,
		"updated_at": time.Now(),
	})
	if err != nil {
		return nil, err
	}
	return s.flowRepo.GetForUser(context.Background(), f.ID, f.UserID)
}

// Submit 提交平台审核（决策 5：上架走 scan gate + 人工审核 + 沙箱复扫；M2 落 submitted 态）
func (s *EgoFlowService) Submit(ctx context.Context, userID, flowID uint) (*model.EgoliteFlow, error) {
	f, err := s.flowRepo.GetForUser(ctx, flowID, userID)
	if err != nil {
		return nil, err
	}
	if f.Status != model.EgoFlowPublished {
		return nil, fmt.Errorf("只有 published 流程可提交审核（先发布再提交）")
	}
	if err := s.flowRepo.Update(ctx, flowID, userID, map[string]any{
		"status":     model.EgoFlowSubmitted,
		"updated_at": time.Now(),
	}); err != nil {
		return nil, err
	}
	return s.flowRepo.GetForUser(ctx, flowID, userID)
}

// ScanRescan 重新扫描（用户改完脚本后主动触发；也供审核工作台复扫）
func (s *EgoFlowService) Rescan(ctx context.Context, userID, flowID uint) (*ScanReport, error) {
	f, err := s.flowRepo.GetForUser(ctx, flowID, userID)
	if err != nil {
		return nil, err
	}
	report := ScanScript(f.Script)
	_ = s.flowRepo.Update(ctx, flowID, userID, map[string]any{
		"scan_passed": report.Passed,
		"scan_report": datatypes.JSON(mustJSON(report)),
		"updated_at":  time.Now(),
	})
	return &report, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func datatypesJSON(b []byte) json.RawMessage {
	return json.RawMessage(b)
}
