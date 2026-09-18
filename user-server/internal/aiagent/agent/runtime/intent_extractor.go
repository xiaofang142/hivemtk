package agent_runtime

import (
	"encoding/json"
	"regexp"
	"strings"
)

// BusinessIntentResult 业务结算结果（资产包模式 §六 协议）
//
// 字段：
//   - Intent       业务结算标签（faq / lead_capture / human_transfer）
//   - CapturedData 捕获到的业务字段（统一字符串化，便于上层直接落库）
//   - RawJSON      原始 JSON 字符串（用于审计/回放/调试）
type BusinessIntentResult struct {
	Intent       string            `json:"intent"`
	CapturedData map[string]string `json:"captured_data"`
	RawJSON      string            `json:"raw_json,omitempty"`
}

// DefaultIntentExtractor 默认业务结算提取器
//
// 不依赖任何外部状态，纯函数式实现，可单测
type DefaultIntentExtractor struct{}

// Extract 从 LLM 响应文本中提取业务结算 JSON 块
func (DefaultIntentExtractor) Extract(reply string) (*BusinessIntentResult, error) {
	if reply == "" {
		return nil, nil
	}

	if ir := extractFromCodeBlock(reply); ir != nil {
		return ir, nil
	}

	if ir := extractFromBareJSON(reply); ir != nil {
		return ir, nil
	}

	return nil, nil
}

var intentJSONBlockRE = regexp.MustCompile("(?s)```(?:json|JSON)?\\s*(\\{[\\s\\S]*?\\})\\s*```")

var intentBareStartRE = regexp.MustCompile(`\{"intent"\s*:`)

func extractFromCodeBlock(reply string) *BusinessIntentResult {
	matches := intentJSONBlockRE.FindAllStringSubmatch(reply, -1)
	if len(matches) == 0 {
		return nil
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return nil
	}
	rawJSON := strings.TrimSpace(last[1])
	return parseIntentJSON(rawJSON)
}

func extractFromBareJSON(reply string) *BusinessIntentResult {
	idxs := intentBareStartRE.FindAllStringIndex(reply, -1)
	if len(idxs) == 0 {
		return nil
	}
	lastStart := idxs[len(idxs)-1][0]
	depth := 0
	end := -1
	for i := lastStart; i < len(reply); i++ {
		switch reply[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}
	rawJSON := reply[lastStart : end+1]
	return parseIntentJSON(rawJSON)
}

func parseIntentJSON(raw string) *BusinessIntentResult {
	var dec struct {
		Intent       string         `json:"intent"`
		CapturedData map[string]any `json:"captured_data"`
	}
	if err := json.Unmarshal([]byte(raw), &dec); err != nil {
		return nil
	}
	ir := &BusinessIntentResult{
		Intent:       dec.Intent,
		CapturedData: make(map[string]string, len(dec.CapturedData)),
		RawJSON:      raw,
	}
	if ir.Intent == "" {
		ir.Intent = "faq"
	}
	for k, v := range dec.CapturedData {
		switch val := v.(type) {
		case string:
			ir.CapturedData[k] = val
		case float64:
			ir.CapturedData[k] = strings.TrimRight(strings.TrimRight(
				strings.TrimSpace(formatBusinessFloat(val)), "0"), ".")
		case bool:
			if val {
				ir.CapturedData[k] = "true"
			} else {
				ir.CapturedData[k] = "false"
			}
		case nil:
			ir.CapturedData[k] = ""
		default:
			if b, err := json.Marshal(val); err == nil {
				ir.CapturedData[k] = string(b)
			}
		}
	}
	return ir
}

func formatBusinessFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
