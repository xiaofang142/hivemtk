package agent_runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// ScopedCheckpointThreadID 将恢复地址绑定可信租户、消息、账号及本轮完整智能体配置。
// 时间、trace 和配置加载时间不改变语义；不可序列化的配置不允许复用快照。
// 注意（EC-08 对齐口径）：当前指纹不包含“意图修订”（intent revision）；若后续需要意图修订变更即失效旧恢复，
// 必须在此显式纳入并补齐回归测试，避免沿用不再匹配的阶段产出。
func ScopedCheckpointThreadID(ctx context.Context, payload CustomerMessagePayload, agent *AgentContext) (string, error) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenantID == 0 || agent == nil || agent.Version < 0 {
		return "", errors.New("checkpoint_identity_unavailable")
	}
	for _, value := range []string{payload.MessageID, payload.ChannelType, payload.AccountID, payload.CustomerID, payload.SessionID, agent.AgentCode} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 512 {
			return "", errors.New("checkpoint_identity_unavailable")
		}
	}
	configuration := *agent
	configuration.LoadedAt = time.Time{}
	payload.Timestamp = time.Time{}
	payload.TraceID = ""
	encoded, err := json.Marshal(struct {
		Tenant  uint                   `json:"tenant"`
		Payload CustomerMessagePayload `json:"payload"`
		Agent   AgentContext           `json:"agent"`
	}{tenantID, payload, configuration})
	if err != nil {
		return "", errors.New("checkpoint_identity_unavailable")
	}
	sum := sha256.Sum256(encoded)
	return "thr_v3_" + hex.EncodeToString(sum[:]), nil
}
