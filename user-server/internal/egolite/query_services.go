package egolite

// 查询/管理接缝（2026-11-05 二次审计）：
// controller 层只允许调 service，不允许直连 repository。这里把原先散落在
// router 处理器里的仓储读/删调用收进领域服务，保持 HTTP 层零仓储依赖。

import (
	"context"

	"hivemtk-user/internal/model"
)

// ---------------------------------------------------------------- 流程域

func (s *EgoFlowService) ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteFlow, int64, error) {
	return s.flowRepo.ListByUser(ctx, userID, page, pageSize)
}

func (s *EgoFlowService) GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteFlow, error) {
	return s.flowRepo.GetForUser(ctx, id, userID)
}

func (s *EgoFlowService) DeleteForUser(ctx context.Context, id, userID uint) error {
	return s.flowRepo.Delete(ctx, id, userID)
}

// ListByStatus 管理端审核工作台：跨用户按状态列流程（空状态 = 全量）。
func (s *EgoFlowService) ListByStatus(ctx context.Context, status string, page, pageSize int) ([]*model.EgoliteFlow, int64, error) {
	return s.flowRepo.ListByStatus(ctx, status, page, pageSize)
}

// ---------------------------------------------------------------- 执行域

func (s *EgoExecutionService) ListByUser(ctx context.Context, userID uint, page, pageSize int) ([]*model.EgoliteExecution, int64, error) {
	return s.execRepo.ListByUser(ctx, userID, page, pageSize)
}

func (s *EgoExecutionService) GetForUser(ctx context.Context, id, userID uint) (*model.EgoliteExecution, error) {
	return s.execRepo.GetForUser(ctx, id, userID)
}

func (s *EgoExecutionService) ListEventsAfter(ctx context.Context, executionID, afterID uint, limit int) ([]*model.EgoEventLog, error) {
	return s.evRepo.ListByExecutionAfter(ctx, executionID, afterID, limit)
}

// SubscribeEvents 订阅执行变更信号（SSE 数据源）；hub 未装配时返回空通道。
func (s *EgoExecutionService) SubscribeEvents(executionID uint) (<-chan struct{}, func()) {
	return s.hub.Subscribe(executionID)
}

// ---------------------------------------------------------------- 资产目录

// ListInstallableForUser runner 拉清单：已发布目录资产 + 该用户自己的 published 流程。
func (s *EgoAssetCatalog) ListInstallableForUser(ctx context.Context, userID uint) ([]InstallableAsset, error) {
	return ListInstallableForUser(ctx, s.flowRepo, s.assetRepo, userID)
}
