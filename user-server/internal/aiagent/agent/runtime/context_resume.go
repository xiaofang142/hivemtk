package agent_runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
)

// RuntimeLLMCallContext 是 Agent Runtime 单次模型调用的执行链标识。
// 它不改变 OpenAI 兼容 body，只通过 header 承载，避免污染其他厂商协议。
type RuntimeLLMCallContext struct {
	ExecutionScenarioID string
	SessionRef          string
	IdempotencyKey      string
	SnapshotID          string
}

// ContextAwareLLMChatPort 是可选能力；未实现的调用方继续走原 Chat 通道。
type ContextAwareLLMChatPort interface {
	ChatWithContext(ctx context.Context, messages []model.AssetBundleMessage, model string, temperature float64, callCtx RuntimeLLMCallContext) (string, error)
}

// CheckpointContextIdentity 固定单次入站的快照身份；恢复时任何字段不匹配都必须整体重跑。
type CheckpointContextIdentity struct {
	ContextSchemaVersion string
	SnapshotID           string
	MessageWatermark     string
	SessionRef           string
}

var errStaleCheckpointContext = errors.New("stale checkpoint context")

// checkpointContextIdentity 从不可变的入站载荷派生身份，不读取可变配置或 trace。
func checkpointContextIdentity(p CustomerMessagePayload) CheckpointContextIdentity {
	key := strings.Join([]string{p.MessageID, p.SessionID, p.ChannelType, p.AccountID, p.Content}, "\x00")
	sum := sha256.Sum256([]byte(key))
	return CheckpointContextIdentity{
		ContextSchemaVersion: agentcontext.ContextSchemaVersion,
		SnapshotID:           "snap_" + hex.EncodeToString(sum[:])[:24],
		MessageWatermark:     strings.TrimSpace(p.MessageID),
		SessionRef:           strings.TrimSpace(p.SessionID),
	}
}

// ValidateCheckpointContext 只在 checkpoint 带有新身份字段时做严格校验。
// 旧格式 checkpoint 保持源兼容，由版本/载荷指纹门禁处理。
func ValidateCheckpointContext(st checkpointState, expected CheckpointContextIdentity) error {
	if st.ContextSchemaVersion == "" && st.SnapshotID == "" && st.MessageWatermark == "" {
		return nil
	}
	if st.ContextSchemaVersion != expected.ContextSchemaVersion {
		return fmt.Errorf("%w: schema", errStaleCheckpointContext)
	}
	if st.SnapshotID != expected.SnapshotID {
		return fmt.Errorf("%w: snapshot", errStaleCheckpointContext)
	}
	if st.MessageWatermark != expected.MessageWatermark {
		return fmt.Errorf("%w: watermark", errStaleCheckpointContext)
	}
	return nil
}
