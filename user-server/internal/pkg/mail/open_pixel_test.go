// open_pixel_test.go 打开追踪像素的正文出口：签不出来就一个字节都不许多，签出来恰好一枚 1×1。
package mail

import (
	"strings"
	"testing"
)

const pixelURL = "https://crm.example.com/api/email/track/open/abc.def.png"

// TestAppendOpenPixelLeavesBodyAloneWithoutURL 空串与带换行的 URL 都不进正文。
//
// 换行那一格不是洁癖：像素 src 取自 SERVER_BASE_URL，而运维把它写成带换行的值时，
// 正文里就会出现一枚指向攻击者可控地址的 img（收件人的邮件客户端会替我们发那个请求）。
func TestAppendOpenPixelLeavesBodyAloneWithoutURL(t *testing.T) {
	body := "<p>正文</p>"
	for _, u := range []string{"", "https://x.example/a.png\r\n<img src=https://evil.example/e.png>", "https://x.example/a.png\n"} {
		if got := AppendOpenPixel(body, u); got != body {
			t.Errorf("URL %q 本不该动正文，得到 %q", u, got)
		}
	}
}

// TestAppendOpenPixelAppendsExactlyOnePixel 有链接 ⇒ 正文尾部恰好一枚 1×1 透明图，原正文一字不改。
func TestAppendOpenPixelAppendsExactlyOnePixel(t *testing.T) {
	body := "<p>正文</p>"
	got := AppendOpenPixel(body, pixelURL)
	if !strings.HasPrefix(got, body) {
		t.Errorf("像素改掉了原正文：%q", got)
	}
	if n := strings.Count(got, "<img"); n != 1 {
		t.Errorf("<img> 出现 %d 次，期望恰好 1 次：%q", n, got)
	}
	if !strings.Contains(got, pixelURL) {
		t.Errorf("像素没指向签发的那条链接：%q", got)
	}
	if !strings.Contains(got, `width="1"`) || !strings.Contains(got, `height="1"`) {
		t.Errorf("像素不是 1×1，会在某些客户端显形：%q", got)
	}
}

// TestAppendOpenPixelCannotBreakOutOfSrcAttribute 引号必须被转义：URL 里带一个 " 就能
// 往正文里塞任意属性（onload 之类），而这段字符串是系统追加的、运营看不出来源。
func TestAppendOpenPixelCannotBreakOutOfSrcAttribute(t *testing.T) {
	evil := `https://x.example/a.png" onload="alert(1)`
	got := AppendOpenPixel("<p>正文</p>", evil)
	if strings.Contains(got, `onload="alert`) {
		t.Errorf("URL 里的引号逃出了 src 属性：%q", got)
	}
}
