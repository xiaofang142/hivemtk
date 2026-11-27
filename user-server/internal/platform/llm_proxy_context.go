package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// LLMProxyContractVersion 标记携带上下文头的代理契约版本；平台侧按同一版本验签。
const LLMProxyContractVersion = "llm-context-v2"

const (
	llmProxyContractHeader      = "X-Llm-Contract-Version"
	llmProxySessionHeader       = "X-Llm-Session-Key"
	llmProxyScenarioHeader      = "X-Llm-Execution-Scenario"
	llmProxyIdempotencyHeader   = "X-Llm-Idempotency-Key"
	llmProxyCachedTokensHeader  = "X-Llm-Cached-Tokens"
	llmProxyCacheCreationHeader = "X-Llm-Cache-Creation-Tokens"
	llmProxyOutcomeHeader       = "X-Llm-Outcome"
)

var llmProxyContextSignedHeaders = []string{
	llmProxyContractHeader,
	llmProxySessionHeader,
	llmProxyScenarioHeader,
	llmProxyIdempotencyHeader,
}

// buildLLMProxyContextHeaders 只上行不可逆 opaque session 标识，不把 canonical bus key 交给平台。
func buildLLMProxyContextHeaders(meta LLMCallMeta) map[string]string {
	headers := map[string]string{}
	if meta.ContractVersion == "" || meta.ContractVersion != LLMProxyContractVersion {
		return headers
	}
	headers[llmProxyContractHeader] = meta.ContractVersion
	if meta.OpaqueSessionKey != "" {
		sum := sha256.Sum256([]byte("hivemtk:platform-llm-session:v1\x00" + meta.OpaqueSessionKey))
		headers[llmProxySessionHeader] = hex.EncodeToString(sum[:])
	}
	if meta.ExecutionScenarioID != "" {
		headers[llmProxyScenarioHeader] = meta.ExecutionScenarioID
	}
	if meta.IdempotencyKey != "" {
		headers[llmProxyIdempotencyHeader] = meta.IdempotencyKey
	}
	return headers
}

// LLMProxySignatureV2 在 v1 的 method/uri/ts/nonce/body 摘要之外绑定上下文字段。
func LLMProxySignatureV2(secret, method, requestURI string, base []string, headers map[string]string) string {
	parts := make([]string, 0, len(base)+len(headers)+2)
	parts = append(parts, llmProxySignDomain+"-context-v2", method, requestURI)
	parts = append(parts, base...)
	normalized := make(map[string]string, len(headers))
	keys := make([]string, 0, len(headers))
	for key, value := range headers {
		lower := strings.ToLower(key)
		normalized[lower] = value
		keys = append(keys, lower)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+strings.TrimSpace(normalized[key]))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseLLMProxyCachedUsage(header http.Header) (int64, int64) {
	return parseIntHeader(header.Get(llmProxyCachedTokensHeader)), parseIntHeader(header.Get(llmProxyCacheCreationHeader))
}

func isLLMProxyOutcomeUnknown(resp *http.Response) bool {
	if resp == nil || resp.StatusCode != http.StatusConflict {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(resp.Header.Get(llmProxyOutcomeHeader)), "outcome_unknown")
}

func parseInt64HeaderValue(v string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	return n
}
