package llm

import (
	"errors"
	"testing"
)

func TestCheckedLLMOutputRejectsTruncatedAndFiltered(t *testing.T) {
	tests := []struct {
		name string
		res  GenerateResult
		want error
	}{
		{name: "truncated", res: GenerateResult{Content: "半句", FinishReason: "length"}, want: ErrLLMOutputTruncated},
		{name: "filtered", res: GenerateResult{Content: "违规", FinishReason: "content_filter"}, want: ErrLLMOutputFiltered},
		{name: "empty", res: GenerateResult{Content: "", FinishReason: "stop"}, want: ErrLLMOutputInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CheckGenerateResult(tt.res)
			if !errors.Is(err, tt.want) {
				t.Fatalf("期望 %v，实际 %v", tt.want, err)
			}
		})
	}
}

func TestCheckedLLMOutputPreservesFinishReasonAndUsage(t *testing.T) {
	result := GenerateResult{
		Content:      "完整回复",
		FinishReason: "stop",
		Usage:        TokenUsage{PromptTokens: 1200, CompletionTokens: 80, TotalTokens: 1280},
	}
	checked, err := CheckGenerateResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if checked.FinishReason != "stop" || checked.Usage.PromptTokens != 1200 || checked.Usage.CompletionTokens != 80 {
		t.Fatalf("usage/finish_reason 丢失：%+v", checked)
	}
}

func TestCheckedLLMOutputValidators(t *testing.T) {
	if err := ValidateJSONOutput(`{"intent":"price"}`); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSONOutput(`not-json`); err == nil {
		t.Fatal("JSON 校验失败必须报错")
	}

	allowed := map[string]bool{"hub:42": true}
	if err := ValidateAllowedEvidenceIDs(`依据是 hub:42`, allowed); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllowedEvidenceIDs(`依据是 hub:999`, allowed); err == nil {
		t.Fatal("未授权证据 ID 必须报错")
	}

	checked, err := CheckGenerateResult(
		GenerateResult{Content: `{"text":"方案"}`, FinishReason: "stop"},
		ValidateJSONOutput,
		func(content string) error { return ValidateForbiddenSubstrings(content, []string{"机密"}) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Content != `{"text":"方案"}` {
		t.Fatalf("校验后内容被改写：%q", checked.Content)
	}
	if _, err := CheckGenerateResult(
		GenerateResult{Content: "包含机密信息", FinishReason: "stop"},
		func(content string) error { return ValidateForbiddenSubstrings(content, []string{"机密"}) },
	); err == nil {
		t.Fatal("隐私片段校验失败必须报错")
	}
}

func TestValidateJSONOutputUsesWholeDocument(t *testing.T) {
	if err := ValidateJSONOutput(`{"a":1} trailing`); err == nil {
		t.Fatal("JSON 后附文本必须拒绝")
	}
	if err := ValidateJSONOutput(`[1,2]`); err != nil {
		t.Fatalf("合法 JSON 数组应通过：%v", err)
	}
}
