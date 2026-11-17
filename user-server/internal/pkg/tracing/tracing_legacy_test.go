package tracing

import (
	"encoding/json"
	"fmt"
)

// 旧序列化帮助函数只用于兼容历史单测；生产使用凭证脱敏且有界的 tracePayloadJSON。
func toJSON(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
