package context

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	logPhonePattern = regexp.MustCompile(`1[3-9]\d{9}`)
	logEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	logOrderPattern = regexp.MustCompile(`ORD-\d{4}-\d{2}|订单号[:：]?\s*[0-9A-Za-z\-]{6,}`)
)

// RedactForLog 把客户会话内容中的 PII 与显式敏感片段替换为不可逆短哈希标记；
// 原始内容只能进入加密限权 ledger，不得进入日志或 trace。
func RedactForLog(text string, sensitive ...string) string {
	out := logPhonePattern.ReplaceAllStringFunc(text, func(s string) string { return logRedactionMarker("phone", s) })
	out = logEmailPattern.ReplaceAllStringFunc(out, func(s string) string { return logRedactionMarker("email", s) })
	out = logOrderPattern.ReplaceAllStringFunc(out, func(s string) string { return logRedactionMarker("order", s) })
	for _, item := range sensitive {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = strings.ReplaceAll(out, item, logRedactionMarker("sensitive", item))
	}
	return out
}

func logRedactionMarker(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return "[REDACTED:" + kind + ":" + hex.EncodeToString(sum[:])[:12] + "]"
}
