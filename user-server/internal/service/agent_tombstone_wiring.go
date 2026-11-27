package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// NewRecallTombstoneService 为撤回/编辑入口装配墓碑服务。
// 摘要与 native generation 的失效钩子在此保持幂等空实现：墓碑已先持久化，
// 后续消费端按 SourceCursor 与 TombstoneInvalidatesCursor 自行失效；真正的
// 摘要重建/native generation 递增由各自的消费任务完成，避免撤回端点同步阻塞。
func NewRecallTombstoneService(repo *repository.MessageTombstoneRepository) *AgentContextTombstoneService {
	return NewAgentContextTombstoneService(MessageTombstoneHooks{
		Persist: func(ctx context.Context, item model.MessageTombstone) error {
			if repo == nil {
				return ErrAgentContextTombstoneHooks
			}
			return repo.Upsert(ctx, &item)
		},
		InvalidateSummary: func(context.Context, model.MessageTombstone) error { return nil },
		InvalidateNative:  func(context.Context, model.MessageTombstone) error { return nil },
	})
}
