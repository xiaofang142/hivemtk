package textutil

import (
	"fmt"
	"unicode/utf8"
)

// DefaultMaxBytes 通用文本截断上限的代码兜底值，与参数中心
// `misc.text_truncate_max_bytes` 的 DefaultValue 一致。
const DefaultMaxBytes = 8192

// maxBytesProvider 由装配层注入；未注入时回落 DefaultMaxBytes。
var maxBytesProvider = func() int { return DefaultMaxBytes }

// SetMaxBytesProvider 注入读取函数（装配层调用；测试可注入桩）。nil 视为不注入。
func SetMaxBytesProvider(fn func() int) {
	if fn != nil {
		maxBytesProvider = fn
	}
}

// DefaultTruncateMaxBytes 返回当前生效的截断上限（非正数一律回落默认值）。
func DefaultTruncateMaxBytes() int {
	if n := maxBytesProvider(); n > 0 {
		return n
	}
	return DefaultMaxBytes
}

func TruncateText(s string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultTruncateMaxBytes()
	}
	if len(s) <= maxBytes {
		return s
	}
	head := s[:maxBytes]
	for len(head) > 0 {
		if r, _ := utf8.DecodeLastRuneInString(head); r != utf8.RuneError {
			break
		}
		head = head[:len(head)-1]
	}
	return head + fmt.Sprintf("\u2026[truncated %d bytes]", len(s)-len(head))
}
