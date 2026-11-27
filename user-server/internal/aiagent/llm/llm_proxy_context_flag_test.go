package llm

import "testing"

func TestPlatformProxyContextV2Flag(t *testing.T) {
	t.Setenv("LLM_PROXY_CONTEXT_V2_ENABLED", "")
	if platformProxyContextV2Enabled() {
		t.Fatalf("v2 contract must be disabled by default")
	}
	t.Setenv("LLM_PROXY_CONTEXT_V2_ENABLED", "1")
	if !platformProxyContextV2Enabled() {
		t.Fatalf("v2 contract must be enabled when explicitly opted in")
	}
}
