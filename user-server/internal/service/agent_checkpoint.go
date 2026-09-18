// Agent Runtime 阶段级 Checkpoint（D06，三表自研方案 v2——DBOS 试点评估后延后：
// 引入外部 durable 运行时需验证与现有连接池共存且团队无 Go durable 经验，
// 三表 schema（LangGraph Checkpointer 单表简化）+ 阶段边界恢复语义已满足五阶段流程）。
//
// 恢复纪律（LangGraph superstep 语义）：
//   - 只能从阶段边界恢复；阶段内失败整体重跑该阶段；
//   - 每 stage 完成 upsert (thread_id, stage, state)；
//   - 游标 = 阶段序号最大的一行（不是 updated_at 最新，见 LatestByStageOrder）；
//   - 恢复 = LoadLatest(thread_id) → 从下一阶段继续；无记录则从感知阶段起跑；
//   - thread_id 按载荷内容寻址（见 agent_runtime.CheckpointThreadID），同一逻辑运行
//     重试才命中；state 里带指纹与 done 标，已完成/不匹配的运行一律整体重跑，
//     不会把上一轮的阶段产出喂给新消息。
package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// AgentStageNames 五阶段顺序（恢复游标依据）
var AgentStageNames = []string{"perception", "alignment", "gatekeeper", "planner", "reviewer"}

// checkpointEnvVar 断点续跑挂载开关（T-P1-01）。默认关闭 = 现网行为零变化。
const checkpointEnvVar = "FF_LTC_CHECKPOINT"

// checkpointEnabledFn 判定入口，测试可替换（先例：aiReplyQuietHoursFn）。
//
// 有意不走 pkg/featureflag：那里是 5s 后台轮询的缓存值，一次 RunOnce 里
// "存点"与"取点"可能落在缓存刷新两侧，出现只写不读（或反之）的半开状态。
// checkpoint 的读写必须来自同一个判定，故每次直接读 env（进程级常量，纳秒级开销）。
var checkpointEnabledFn = func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(checkpointEnvVar))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// CheckpointEnabled 报告阶段级 checkpoint 挂载是否开启。
//
// 调用方：internal/app 的 StageCheckpointStore 适配器（每次 RunOnce 快照一次）。
func CheckpointEnabled() bool { return checkpointEnabledFn() }

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

// LoadLatestCheckpoint 取该 thread 的恢复游标（阶段序号最大的一条），无记录返回 nil。
//
// 不用"updated_at 最新"：阶段重跑会顶高早期阶段的时间戳，按时间取会让游标倒退。
func LoadLatestCheckpoint(ctx context.Context, repo *repository.AgentCheckpointRepository, threadID string) (*AgentCheckpoint, error) {
	rows, err := repo.LoadAllByThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	return LatestByStageOrder(rows), nil
}

// LatestByStageOrder 按 AgentStageNames 序号取最靠后的阶段；未知阶段名的行忽略，
// 全部未知（或空集）返回 nil = 从第一阶段起跑。
func LatestByStageOrder(rows []AgentCheckpoint) *AgentCheckpoint {
	best, bestIdx := (*AgentCheckpoint)(nil), -1
	for i := range rows {
		idx := stageIndex(rows[i].Stage)
		if idx > bestIdx {
			best, bestIdx = &rows[i], idx
		}
	}
	return best
}

func stageIndex(stage string) int {
	for i, name := range AgentStageNames {
		if name == stage {
			return i
		}
	}
	return -1
}

// ResumeStage 返回应从哪个阶段续跑（无 checkpoint → 从第一阶段起跑）。
// 已完成阶段数 = checkpoint 在 AgentStageNames 中的位置 + 1。
func ResumeStage(latest *AgentCheckpoint) string {
	if latest == nil {
		return AgentStageNames[0]
	}
	if i := stageIndex(latest.Stage); i >= 0 {
		if i+1 >= len(AgentStageNames) {
			return AgentStageNames[len(AgentStageNames)-1]
		}
		return AgentStageNames[i+1]
	}
	return AgentStageNames[0]
}
