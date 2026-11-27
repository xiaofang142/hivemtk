package platform

import (
	"net/http"
	"testing"
)

func TestLLMProxyOpaqueHeaders(t *testing.T) {
	meta := LLMCallMeta{
		ContractVersion:     LLMProxyContractVersion,
		OpaqueSessionKey:    "llmsess_canonical_secret",
		ExecutionScenarioID: "sales_generate",
		IdempotencyKey:      "idem-1",
	}
	headers := buildLLMProxyContextHeaders(meta)
	if headers[llmProxyContractHeader] != LLMProxyContractVersion {
		t.Fatalf("契约版本头错误：%q", headers[llmProxyContractHeader])
	}
	if headers[llmProxySessionHeader] == "" || headers[llmProxySessionHeader] == meta.OpaqueSessionKey {
		t.Fatal("会话必须经不可逆 opaque 映射后上行")
	}
	if got := buildLLMProxyContextHeaders(meta)[llmProxySessionHeader]; got != headers[llmProxySessionHeader] {
		t.Fatal("同一 canonical key 必须产生稳定 opaque header")
	}
	if headers[llmProxyScenarioHeader] != "sales_generate" || headers[llmProxyIdempotencyHeader] != "idem-1" {
		t.Fatalf("执行场景/幂等键丢失：%#v", headers)
	}
}

func TestLLMProxySignatureV2BindsOpaqueHeaders(t *testing.T) {
	secret := "secret"
	uri := "/llm-api/v1/chat/completions?provider=openai"
	body := []byte(`{"model":"m"}`)
	base := []string{"10", "nonce", string(body)}
	a := LLMProxySignatureV2(secret, "POST", uri, base, map[string]string{llmProxySessionHeader: "opaque-a"})
	b := LLMProxySignatureV2(secret, "POST", uri, base, map[string]string{llmProxySessionHeader: "opaque-b"})
	if a == b {
		t.Fatal("opaque session 头必须进入签名，不能只签 URI 和 body")
	}
	if LLMProxySignatureV2(secret, "POST", uri, base, nil) == LLMProxySignature(secret, "POST", uri, "10", "nonce", body) {
		t.Fatal("无额外头时 v2 不应与 v1 相同")
	}
}

func TestParseLLMProxyCachedUsage(t *testing.T) {
	header := http.Header{}
	header.Set(llmProxyCachedTokensHeader, "120")
	header.Set(llmProxyCacheCreationHeader, "30")
	cached, creation := parseLLMProxyCachedUsage(header)
	if cached != 120 || creation != 30 {
		t.Fatalf("缓存用量解析错误：cached=%d creation=%d", cached, creation)
	}
}

func TestLLMProxyOutcomeUnknownDetection(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusConflict, Header: http.Header{}}
	resp.Header.Set(llmProxyOutcomeHeader, "outcome_unknown")
	if !isLLMProxyOutcomeUnknown(resp) {
		t.Fatal("409 + outcome_unknown 必须识别为未确定结果")
	}
	resp.StatusCode = http.StatusOK
	if isLLMProxyOutcomeUnknown(resp) {
		t.Fatal("成功响应不能当成未确定结果")
	}
}
