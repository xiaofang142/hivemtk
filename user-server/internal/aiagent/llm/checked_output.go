package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrLLMOutputTruncated 表示 finish_reason=length，输出可能不完整。
	ErrLLMOutputTruncated = errors.New("模型输出被长度截断")
	// ErrLLMOutputFiltered 表示内容被安全策略过滤。
	ErrLLMOutputFiltered = errors.New("模型输出被过滤")
	// ErrLLMOutputInvalid 表示输出未通过结构或内容校验。
	ErrLLMOutputInvalid = errors.New("模型输出校验失败")
)

var evidenceIDPattern = regexp.MustCompile(`hub:[0-9]+`)

// OutputValidator 在不改写内容的前提下校验一次完整输出。
type OutputValidator func(content string) error

// CheckGenerateResult 保留 finish_reason 与 usage，只在输出完整且通过全部校验时返回。
func CheckGenerateResult(result GenerateResult, validators ...OutputValidator) (GenerateResult, error) {
	switch result.FinishReason {
	case "length":
		return GenerateResult{}, fmt.Errorf("%w: finish_reason=length", ErrLLMOutputTruncated)
	case "content_filter":
		return GenerateResult{}, fmt.Errorf("%w: finish_reason=content_filter", ErrLLMOutputFiltered)
	}
	if strings.TrimSpace(result.Content) == "" {
		return GenerateResult{}, fmt.Errorf("%w: 输出为空", ErrLLMOutputInvalid)
	}
	for _, validate := range validators {
		if validate == nil {
			continue
		}
		if err := validate(result.Content); err != nil {
			return GenerateResult{}, err
		}
	}
	return result, nil
}

// ValidateJSONOutput 要求完整 JSON 文档，拒绝 JSON 后附文本。
func ValidateJSONOutput(content string) error {
	var payload any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return fmt.Errorf("%w: JSON 解析失败", ErrLLMOutputInvalid)
	}
	return nil
}

// ValidateAllowedEvidenceIDs 拒绝引用未授权的真实消息 ID。
func ValidateAllowedEvidenceIDs(content string, allowed map[string]bool) error {
	for _, id := range evidenceIDPattern.FindAllString(content, -1) {
		if !allowed[id] {
			return fmt.Errorf("%w: 未授权证据 ID", ErrLLMOutputInvalid)
		}
	}
	return nil
}

// ValidateForbiddenSubstrings 只返回是否命中，不在错误中回显敏感原文。
func ValidateForbiddenSubstrings(content string, forbidden []string) error {
	for _, fragment := range forbidden {
		if fragment != "" && strings.Contains(content, fragment) {
			return fmt.Errorf("%w: 命中禁止片段", ErrLLMOutputInvalid)
		}
	}
	return nil
}
