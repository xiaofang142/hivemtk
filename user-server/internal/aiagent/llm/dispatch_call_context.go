package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	agentcontext "hivemtk-user/internal/aiagent/context"
)

// defaultExecutionScenarioForRoute 给尚未显式声明执行场景的存量调用点一个确定映射。
// 它不影响显式 ExecutionScenarioID；显式值仍必须属于 agentcontext 的 12 个合法值。
func defaultExecutionScenarioForRoute(route DispatchScenario) string {
	switch route {
	case ScenarioIntentRecognize:
		return string(agentcontext.ExecutionScenarioIntent)
	case ScenarioObjection:
		return string(agentcontext.ExecutionScenarioObjection)
	case ScenarioLongSummary:
		return string(agentcontext.ExecutionScenarioSummary)
	case ScenarioLowCost:
		return string(agentcontext.ExecutionScenarioLeadDetect)
	case ScenarioSOPReply, ScenarioFriendlyChat, ScenarioHighQuality, ScenarioJevChoice:
		return string(agentcontext.ExecutionScenarioSalesGenerate)
	default:
		return ""
	}
}

func fillLegacyExecutionScenario(req *DispatchRequest) {
	if req == nil || req.ExecutionScenarioID != "" {
		return
	}
	req.ExecutionScenarioID = defaultExecutionScenarioForRoute(req.Scenario)
}

func applyDispatchCallContext(config *LLMConfig, req DispatchRequest) error {
	if config == nil {
		return errors.New("LLM 配置为空")
	}
	executionScenario, err := agentcontext.ParseExecutionScenarioID(req.ExecutionScenarioID)
	if err != nil {
		return fmt.Errorf("执行场景非法：%w", err)
	}
	config.ExecutionScenarioID = string(executionScenario)
	config.LLMSessionRef = req.LLMSessionRef
	config.RequestIdempotencyKey = req.RequestIdempotencyKey
	config.PromptPrefixHash = req.PromptPrefixHash
	return nil
}

// applyProviderCapabilitiesToConfig 把已解析的供应商能力落到实际请求：
// 输出预算按模型上限夹取，canonical session key 在 stateless 协议下只作为
// 本地关联键保留，不支持的字段记入报告而不是伪造厂商能力。
func applyProviderCapabilitiesToConfig(config *LLMConfig, provider *ProviderConfig) error {
	if config == nil || provider == nil {
		return errors.New("LLM 配置或提供商为空")
	}
	caps := ResolveProviderCapabilities(*provider)
	shape, err := BuildProviderRequestShapeForDispatch(*config, caps)
	if err != nil {
		return err
	}
	config.MaxTokens = shape.MaxTokens
	return nil
}

// BuildRequestIdempotencyKey 生成单次调用幂等键：消息 + 执行场景 + 步骤。
func BuildRequestIdempotencyKey(messageID, executionScenarioID, step string) string {
	sum := sha256.Sum256([]byte(messageID + "|" + executionScenarioID + "|" + step))
	return hex.EncodeToString(sum[:])
}
