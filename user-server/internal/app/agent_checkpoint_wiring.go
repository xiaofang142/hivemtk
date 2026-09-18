// agent_checkpoint_wiring.go 阶段边界检查点的装配层（新规划 T-P1-01 / W-7 接线）。
//
// 五层归属：运行时只认 agent_runtime.StageCheckpointStore 端口，持久化在
// repository，恢复游标语义在 service，本文件负责把三者接起来（DI 装配在 main.go
// 触发，见 cmd/api/main.go 的 InitAgentCheckpointStore 调用点）。
package app

import (
	"context"
	"encoding/json"
	"sync"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// agentCheckpointStore agent_runtime.StageCheckpointStore 的生产实现。
type agentCheckpointStore struct {
	repo *repository.AgentCheckpointRepository
}

func (s *agentCheckpointStore) Enabled() bool { return service.CheckpointEnabled() }

func (s *agentCheckpointStore) Save(ctx context.Context, threadID, stage string, state json.RawMessage) error {
	return service.SaveCheckpoint(ctx, s.repo, threadID, stage, state)
}

// LoadResume 取恢复点：游标行由 service 按阶段序号（而非写入时间）判定，
// 续跑阶段名 = ResumeStage(游标)。
func (s *agentCheckpointStore) LoadResume(ctx context.Context, threadID string) (string, json.RawMessage, error) {
	latest, err := service.LoadLatestCheckpoint(ctx, s.repo, threadID)
	if err != nil {
		return "", nil, err
	}
	if latest == nil {
		return "", nil, nil
	}
	return service.ResumeStage(latest), latest.State, nil
}

var (
	globalAgentCheckpointStore     agent_runtime.StageCheckpointStore
	globalAgentCheckpointStoreOnce sync.Once
)

// InitAgentCheckpointStore 装配阶段检查点存储。
//
// 调用方：cmd/api/main.go（必须先于 router.Setup → InitInferenceOrchestrator，
// 否则推理闭环构造时拿不到 store）。db 为 nil 时不装配，链路保持挂载前行为。
func InitAgentCheckpointStore(db *gorm.DB) {
	if db == nil {
		logger.Warn("[checkpoint] ⚠️ db 为 nil，阶段边界断点续跑未装配")
		return
	}
	globalAgentCheckpointStoreOnce.Do(func() {
		globalAgentCheckpointStore = &agentCheckpointStore{
			repo: service.NewAgentCheckpointRepository(db),
		}
		state := "off"
		if service.CheckpointEnabled() {
			state = "on"
		}
		logger.Infof("[checkpoint] ✅ 阶段边界检查点已装配（开关 FF_LTC_CHECKPOINT=%s，off 时推理链路行为与挂载前一致）", state)
	})
}

// GetAgentCheckpointStore 返回已装配的检查点存储；未装配返回 nil。
func GetAgentCheckpointStore() agent_runtime.StageCheckpointStore {
	return globalAgentCheckpointStore
}
