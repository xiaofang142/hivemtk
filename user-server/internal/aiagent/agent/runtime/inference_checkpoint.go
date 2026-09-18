// inference_checkpoint.go 推理闭环的阶段边界检查点挂载（D06 / 新规划 T-P1-01）。
//
// 语义：每阶段成功返回后 upsert 一份 (thread, stage) 快照；同一逻辑运行被中断后
// 重试时，从"已完成的最后一个阶段"的下一个阶段续跑，已完成阶段不再执行、其产出
// 从快照回填。开关与持久化本身在 service/repository 层（agent_checkpoint.go），
// 本文件只定义运行时侧的端口与快照编解码，由 internal/app 装配注入。
package agent_runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"
)

// checkpointStateVersion 快照格式版本；不匹配的旧快照一律整体重跑。
const checkpointStateVersion = 1

// StageCheckpointStore 阶段检查点存储端口。
//
// 实现方：internal/app/agent_checkpoint_wiring.go（委托 service.SaveCheckpoint /
// LoadLatestCheckpoint / ResumeStage）。 Enabled 为假时运行时的 checkpoint 路径
// 完全短路，行为与挂载前逐字节一致。
type StageCheckpointStore interface {
	// Enabled 本次运行是否挂载 checkpoint（每次 RunOnce 只判定一次，读写同侧）。
	Enabled() bool

	// Save 落一份阶段快照；state 为 checkpointState 的 JSON。
	Save(ctx context.Context, threadID, stage string, state json.RawMessage) error

	// LoadResume 返回该 thread 的续跑阶段与"该阶段完成时"的快照。
	// 无记录时 resumeStage 为空串。
	LoadResume(ctx context.Context, threadID string) (resumeStage string, state json.RawMessage, err error)
}

// checkpointState 一行 checkpoint 的载荷。
type checkpointState struct {
	Version     int                `json:"v"`
	Fingerprint string             `json:"fingerprint"`
	Done        bool               `json:"done,omitempty"`
	Snapshot    *checkpointContext `json:"snapshot,omitempty"`
}

// checkpointContext 阶段累计产出的可序列化子集。
//
// 有意不含 Payload / AgentCtx / StartTime（恢复时以本次真实入参为准）
// 与 EpisodicMemory（体积是 prompt 级，恢复时由 provider 重新读取）。
type checkpointContext struct {
	Sentiment SentimentScore     `json:"sentiment,omitempty"`
	Intent    IntentResult       `json:"intent,omitempty"`
	Alignment AlignmentScore     `json:"alignment,omitempty"`
	Crisis    CrisisSignal       `json:"crisis,omitempty"`
	Plan      *ActionPlan        `json:"plan,omitempty"`
	Stages    []StageDecision    `json:"stages,omitempty"`
	Decision  *InferenceDecision `json:"decision,omitempty"`
}

// CheckpointThreadID 按载荷内容寻址生成 thread 标识。
//
// 不用 sessionID 作 thread：同一会话的第二条消息会命中第一条的游标、直接跳过感知，
// 把上一轮的阶段产出当成新消息的结果。内容寻址后，只有"同一条消息的重试"才续跑。
// TraceID 不入摘要：重试方常带新的 trace，否则永远命不中。
func CheckpointThreadID(p CustomerMessagePayload) string {
	return "thr_" + payloadFingerprint(p)[:24]
}

func payloadFingerprint(p CustomerMessagePayload) string {
	key := strings.Join([]string{p.ChannelType, p.SessionID, p.CustomerID, p.MessageType, p.Content}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// cycleCheckpoint RunOnce 一侧的挂载状态；nil = 未挂载（关或无 store）。
type cycleCheckpoint struct {
	store       StageCheckpointStore
	thread      string
	fingerprint string
	skip        map[string]bool
	last        string // 最后一个已落点的阶段名
}

// newCycleCheckpoint 读游标并回填 ic；恢复不可行时退化为整体重跑（skip 为空）。
func newCycleCheckpoint(ctx context.Context, store StageCheckpointStore, payload CustomerMessagePayload, ic *InferenceContext, order []string) *cycleCheckpoint {
	if store == nil || !store.Enabled() {
		return nil
	}
	cc := &cycleCheckpoint{
		store:       store,
		thread:      CheckpointThreadID(payload),
		fingerprint: payloadFingerprint(payload),
		skip:        map[string]bool{},
	}
	resumeStage, blob, err := store.LoadResume(ctx, cc.thread)
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("thread", cc.thread).
			Msg("[checkpoint] 读游标失败，本次整体重跑")
		return cc
	}
	if resumeStage == "" || len(blob) == 0 {
		return cc
	}

	var st checkpointState
	if err := json.Unmarshal(blob, &st); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("thread", cc.thread).
			Msg("[checkpoint] 快照不可解析，本次整体重跑")
		return cc
	}
	if st.Version != checkpointStateVersion || st.Fingerprint != cc.fingerprint {
		logger.Ctx(ctx).Info().Str("thread", cc.thread).
			Msg("[checkpoint] 快照版本/载荷指纹不匹配，本次整体重跑")
		return cc
	}
	if st.Done {
		logger.Ctx(ctx).Info().Str("thread", cc.thread).Str("stage", resumeStage).
			Msg("[checkpoint] 该运行已完整结束，重试走整体重跑")
		return cc
	}
	if st.Snapshot != nil {
		applySnapshot(ic, st.Snapshot)
	}
	for _, name := range order {
		if name == resumeStage {
			break
		}
		cc.skip[name] = true
		cc.last = name
	}
	logger.Ctx(ctx).Info().Str("thread", cc.thread).Str("from_stage", resumeStage).
		Int("skipped_stages", len(cc.skip)).Msg("[checkpoint] 从阶段边界续跑")
	return cc
}

func (cc *cycleCheckpoint) skipped(stage string) bool {
	return cc != nil && cc.skip[stage]
}

// complete 阶段成功：落快照，供后续中断后续跑。失败只告警，不影响回复链路。
func (cc *cycleCheckpoint) complete(ctx context.Context, stage string, ic *InferenceContext) {
	if cc == nil {
		return
	}
	cc.last = stage
	cc.save(ctx, stage, ic, false)
}

// finish 整轮结束（正常收尾或提前返回）：把最后一个阶段改写为 done 快照，
// 之后同一载荷的重试不再续跑（否则会把已结束运行的产出喂给新一次推理）。
func (cc *cycleCheckpoint) finish(ctx context.Context, ic *InferenceContext) {
	if cc == nil || cc.last == "" {
		return
	}
	cc.save(ctx, cc.last, ic, true)
}

func (cc *cycleCheckpoint) save(ctx context.Context, stage string, ic *InferenceContext, done bool) {
	blob, err := json.Marshal(checkpointState{
		Version:     checkpointStateVersion,
		Fingerprint: cc.fingerprint,
		Done:        done,
		Snapshot:    snapshotOf(ic),
	})
	if err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("stage", stage).
			Msg("[checkpoint] 快照序列化失败，跳过落点")
		return
	}
	if err := cc.store.Save(ctx, cc.thread, stage, blob); err != nil {
		logger.Ctx(ctx).Warn().Err(err).Str("thread", cc.thread).Str("stage", stage).
			Msg("[checkpoint] 落点失败，跳过（最坏退化为整体重跑）")
	}
}

func snapshotOf(ic *InferenceContext) *checkpointContext {
	if ic == nil {
		return nil
	}
	snap := &checkpointContext{
		Sentiment: ic.Sentiment,
		Intent:    ic.Intent,
		Alignment: ic.Alignment,
		Crisis:    ic.Crisis,
		Plan:      ic.Plan,
		Stages:    append([]StageDecision(nil), ic.Stages...),
		Decision:  &ic.Decision,
	}
	return snap
}

func applySnapshot(ic *InferenceContext, snap *checkpointContext) {
	if ic == nil || snap == nil {
		return
	}
	ic.Sentiment = snap.Sentiment
	ic.Intent = snap.Intent
	ic.Alignment = snap.Alignment
	ic.Crisis = snap.Crisis
	ic.Plan = snap.Plan
	ic.Stages = append([]StageDecision(nil), snap.Stages...)
	if snap.Decision != nil {
		ic.Decision = *snap.Decision
	}
}
