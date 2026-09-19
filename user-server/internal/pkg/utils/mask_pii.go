package utils

import "strings"

// MaskPhone 返回可安全落日志的手机号形态：仅按数字计数，保留前 3 后 4，
// 中间以 * 填充（如 11 位 → 138****5678）；数字位数 ≤6 时全量打码，
// 避免留下可定位片段。空格/+/-/括号等分隔符在结果中丢弃。
func MaskPhone(phone string) string {
	digits := make([]byte, 0, len(phone))
	for i := 0; i < len(phone); i++ {
		if c := phone[i]; c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	n := len(digits)
	switch {
	case n == 0:
		return ""
	case n <= 7:
		// 7 位及以下若仍保留前3后4 就等于原样输出，必须整体打码
		return strings.Repeat("*", n)
	default:
		return string(digits[:3]) + strings.Repeat("*", n-7) + string(digits[n-4:])
	}
}

// MaskEmail 保留本地部分首字符与完整域名（域名通常为公司/公共域，定位性远低于
// 用户名），如 zhangsan@qq.com → z*******@qq.com；本地部分 ≤1 字符时整体打码。
func MaskEmail(email string) string {
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		// 非标准邮箱一律整体打码，不做部分回显
		if email == "" {
			return ""
		}
		return strings.Repeat("*", len([]rune(email)))
	}
	local, domain := email[:at], email[at+1:]
	r := []rune(local)
	if len(r) <= 1 {
		return "*@" + domain
	}
	return string(r[0]) + strings.Repeat("*", len(r)-1) + "@" + domain
}
