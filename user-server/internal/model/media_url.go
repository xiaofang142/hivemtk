package model

import "unicode/utf8"

// MaxMediaURLLen media_url 列宽（message_hub / session_messages / unified_messages /
// unified_replies 四处均为 varchar(500)，PG 按**字符**计数）。
//
// 2026-11-10 审计 F2：平台签名 URL（抖音 p3-sign…&x-expires=… 等）常超 500 字符，
// 无截断时 INSERT 直接 22001 整条消息落库失败，uplink 重传永不收敛 —— 入库前必须统一收口。
const MaxMediaURLLen = 500

// TruncateMediaURL 把媒体 URL 截断到列宽。varchar(500) 按 rune 计数，故按 rune 截；
// 截断会破坏签名参数（链接失效），但保住消息本体——链接失效可观测，消息蒸发不可观测。
// 空串原样返回；非 URL 文本不做语义校验（本函数只管列宽这一件事）。
func TruncateMediaURL(u string) string {
	if utf8.RuneCountInString(u) <= MaxMediaURLLen {
		return u
	}
	r := []rune(u)
	return string(r[:MaxMediaURLLen])
}
