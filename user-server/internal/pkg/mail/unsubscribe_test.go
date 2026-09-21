// unsubscribe_test.go 一次性退订出口（RFC 8058）的判据：两个头必须成对在场，且不可信链接不上头。
package mail

import (
	"strings"
	"testing"

	"gopkg.in/gomail.v2"
)

const cleanLink = "https://crm.example.com/api/email/unsubscribe?token=eyJlbWFpbCI6ImEifQ.sig"

// TestUnsubscribeOptionSetsBothHeaders 两个头必须一起出现。
//
// 只发 List-Unsubscribe 而不发 List-Unsubscribe-Post，Gmail/Yahoo 仍按"未提供一键退订"
// 计（2024-02 起对批量发件人是硬性要求）；只发后者则是承诺了一个链接里没实现的能力。
// 所以这里断言的是"成对"，不是"各自存在"。
func TestUnsubscribeOptionSetsBothHeaders(t *testing.T) {
	m := gomail.NewMessage()
	Unsubscribe(cleanLink)(m)

	got := m.GetHeader("List-Unsubscribe")
	if len(got) != 1 {
		t.Fatalf("List-Unsubscribe 头 = %v，期望恰好 1 个值", got)
	}
	if got[0] != "<"+cleanLink+">" {
		t.Errorf("List-Unsubscribe = %q，期望尖括号包裹的链接（RFC 2369 的 URI 形式）", got[0])
	}
	post := m.GetHeader("List-Unsubscribe-Post")
	if len(post) != 1 || post[0] != "List-Unsubscribe=One-Click" {
		t.Errorf("List-Unsubscribe-Post = %v，期望 [List-Unsubscribe=One-Click]", post)
	}
}

// TestUnsubscribeOptionDoesNotDoubleWrap 链接已带尖括号时不再包一层。
func TestUnsubscribeOptionDoesNotDoubleWrap(t *testing.T) {
	m := gomail.NewMessage()
	Unsubscribe("<" + cleanLink + ">")(m)
	if got := m.GetHeader("List-Unsubscribe"); len(got) != 1 || got[0] != "<"+cleanLink+">" {
		t.Errorf("List-Unsubscribe = %v，期望不重复包裹", got)
	}
}

// TestUnsubscribeRejectsUnsafeLinks 头部注入面：链接值里出现 CR/LF 就能劈开发信行。
//
// 退订链接的 base 来自 SERVER_BASE_URL 环境变量，值不由代码产生；把它写进头部之前
// 必须过一遍"换行即弃"的判据 —— 宁可少一个退出口，也不能让一个配置项变成 SMTP 注入点。
func TestUnsubscribeRejectsUnsafeLinks(t *testing.T) {
	unsafe := []string{
		"",
		"https://x.example.com/a?b=1\r\nBcc: victim@example.com",
		"https://x.example.com/a\nSubject: hi",
	}
	for _, link := range unsafe {
		m := gomail.NewMessage()
		Unsubscribe(link)(m)
		if got := m.GetHeader("List-Unsubscribe"); len(got) != 0 {
			t.Errorf("链接 %q 仍被写进 List-Unsubscribe（%v）", link, got)
		}
		if got := m.GetHeader("List-Unsubscribe-Post"); len(got) != 0 {
			t.Errorf("链接 %q 无退出口却声明了 One-Click（%v）", link, got)
		}
		// 同一条判据必须同时管住正文页脚，否则"头没有、正文有个坏链接"会绕过本用例。
		if got := AppendUnsubscribeFooter("<p>hi</p>", link); got != "<p>hi</p>" {
			t.Errorf("链接 %q 仍被追加进正文：%q", link, got)
		}
	}
}

// TestUnsubscribeRejectsEncodedWordLinks 非 ASCII / 带空格的链接一律不上头。
//
// gomail 会把含非 ASCII 的头值编成 RFC 2047 encoded-word（`=?UTF-8?q?...?=`），
// 而 List-Unsubscribe 是结构化头，收件端不认这种形式 ⇒ 头看起来在，一键退订其实没生效。
// 现实形状就是 SERVER_BASE_URL 写成 IDN 域名（或带了个空格）。
func TestUnsubscribeRejectsEncodedWordLinks(t *testing.T) {
	for _, link := range []string{
		"https://例子.com/api/email/unsubscribe?token=x",
		"https://x.example.com/a b",
	} {
		m := gomail.NewMessage()
		Unsubscribe(link)(m)
		if got := m.GetHeader("List-Unsubscribe"); len(got) != 0 {
			t.Errorf("链接 %q 仍被写成头（%q），会落成 encoded-word", link, got)
		}
		if got := m.GetHeader("List-Unsubscribe-Post"); len(got) != 0 {
			t.Errorf("链接 %q 无可用退订头却声明了 One-Click", link)
		}
	}

	// 正文的判据比头部宽一档：页脚不过 RFC 2047 编码，IDN 链接在正文里照样可点。
	// 两条判据不一致是有意为之，写在这里免得后来人把它们"统一"成一条、
	// 让中文域名的部署连人眼可见的退订入口都没有。
	idn := "https://例子.com/api/email/unsubscribe?token=x"
	if got := AppendUnsubscribeFooter("<p>b</p>", idn); !strings.Contains(got, "例子.com") {
		t.Errorf("中文域名链接被正文判据一并拒掉了：%q", got)
	}
}

// TestAppendUnsubscribeFooterKeepsBodyAndCarriesLink 页脚是"看得见"的那半个退出口：
// 只靠头部链接的话，收件人在纯文本阅读器里看不到退订入口。
func TestAppendUnsubscribeFooterKeepsBodyAndCarriesLink(t *testing.T) {
	body := "<p>本月新品已上架</p>"
	got := AppendUnsubscribeFooter(body, cleanLink)
	if !strings.HasPrefix(got, body) {
		t.Errorf("页脚把原正文吃掉了：%q", got)
	}
	if !strings.Contains(got, `href="`+cleanLink+`"`) {
		t.Errorf("页里没有可点链接：%q", got)
	}
	if !strings.Contains(got, "退订") {
		t.Errorf("页脚文案里没有人话可读的退订字样：%q", got)
	}
	if AppendUnsubscribeFooter(body, "") != body {
		t.Error("空链接也要往正文里塞一段指向不明的退订文字")
	}
}

// TestAppendUnsubscribeFooterEscapesLink 链接里的引号/尖括号不得劈开 <a> 标签。
func TestAppendUnsubscribeFooterEscapesLink(t *testing.T) {
	eviltag := `https://x.example.com/a"onmouseover="alert(1)`
	got := AppendUnsubscribeFooter("<p>b</p>", eviltag)
	if strings.Contains(got, `"onmouseover="`) {
		t.Errorf("链接未转义，页脚出现可执行属性：%q", got)
	}
}
