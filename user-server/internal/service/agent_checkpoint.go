// Agent Runtime 阶段级 Checkpoint（D06，三表自研方案 v2——DBOS 试点评估后延后：
// 引入外部 durable 运行时需验证与现有连接池共存且团队无 Go durable 经验，
// 三表 schema（LangGraph Checkpointer 单表简化）+ 阶段边界恢复语义已满足五阶段流程）。
//
// 恢复纪律（LangGraph superstep 语义）：
//   - 只能从阶段边界恢复；阶段内失败整体重跑该阶段；
//   - 每 stage 完成 upsert (thread_id, stage, state)；
//   - 恢复 = LoadLatest(thread_id) → 从下一阶段继续；无记录则从感知阶段起跑。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// AgentStageNames 五阶段顺序（恢复游标依据）
var AgentStageNames = []string{"perception", "alignment", "gatekeeper", "planner", "reviewer"}

// AgentCheckpoint 单条记录（别名，存储结构已收敛到 repository 层）
type AgentCheckpoint = repository.AgentCheckpoint

// NewAgentCheckpointRepository 构造（gorm 收敛在仓储，service 不直连 DB）
func NewAgentCheckpointRepository(db *gorm.DB) *repository.AgentCheckpointRepository {
	return repository.NewAgentCheckpointRepository(db)
}

// SaveCheckpoint 阶段完成即落 checkpoint（upsert，幂等）
func SaveCheckpoint(ctx context.Context, repo *repository.AgentCheckpointRepository, threadID, stage string, state json.RawMessage) error {
	return repo.Save(ctx, threadID, stage, state)
}

// LoadLatestCheckpoint 取该 thread 最新 checkpoint，无记录返回 nil
func LoadLatestCheckpoint(ctx context.Context, repo *repository.AgentCheckpointRepository, threadID string) (*AgentCheckpoint, error) {
	return repo.LoadLatest(ctx, threadID)
}

// ResumeStage 返回应从哪个阶段续跑（无 checkpoint → 从第一阶段起跑）。
// 已完成阶段数 = checkpoint 在 AgentStageNames 中的位置 + 1。
func ResumeStage(latest *AgentCheckpoint) string {
	if latest == nil {
		return AgentStageNames[0]
	}
	for i, name := range AgentStageNames {
		if name == latest.Stage {
			if i+1 >= len(AgentStageNames) {
				return AgentStageNames[len(AgentStageNames)-1]
			}
			return AgentStageNames[i+1]
		}
	}
	return AgentStageNames[0]
}

// NewThreadID 生成 checkpoint 会话标识
func NewThreadID(sessionID string) string {
	if sessionID == "" {
		return "thr_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return sessionID
}

var _ = gorm.ErrRecordNotFound
var _ = context.Background
