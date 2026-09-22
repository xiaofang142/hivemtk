package mail

import (
	"html"
	"strings"
)

// AppendOpenPixel 在正文尾部补一枚 1×1 透明像素（打开追踪）。
//
// 与退订页脚同一形状：URL 为空（未配密钥、签发失败、没装配）⇒ 正文一字不改。
// 像素走 <img> 而不是 <a>：绝大多数客户端默认加载图片时才发这个请求，
// 点了链接才算"点击"是另一条链路（本仓未接线，见 docs/DEPLOYMENT_GUIDE.md 的追踪一节）。
func AppendOpenPixel(body, url string) string {
	if !linkReadable(url) {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n<img src=\"")
	b.WriteString(html.EscapeString(url))
	b.WriteString("\" width=\"1\" height=\"1\" alt=\"\" style=\"display:none;border:0\" />")
	return b.String()
}
