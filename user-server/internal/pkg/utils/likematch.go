package utils

import "strings"

// EscapeLike 转义 LIKE/ILIKE 模式的三个通配元字符（\ % _）。
//
// 背景（架构师深度审计轮）：全仓 10+ 处 "col ILIKE '%'+用户输入+'%'"——参数化
// 保证了不构成 SQL 注入，但用户值里的 %/_ 会作为通配符生效：单个 % 使条件
// 退化为全表匹配（搜索框输个 % 就能枚举/拖库），\ 在部分拼接形态下会报错。
// 使用约定：SQL 侧 ILIKE 必须搭配 ESCAPE '\'（PG 默认转义符即反斜杠，
// 显式声明以不受 standard_conforming_strings 变化影响）。
// 单源先例：repository/tenant.go escapeLike（本函数提取自该实现）。
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
