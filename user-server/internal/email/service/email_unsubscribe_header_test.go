// email_unsubscribe_header_test.go 单封投递侧的退订出口（#58）：头由链接决定，缺密钥不阻断发信。
package email

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"

	"gopkg.in/gomail.v2"
)

type stubUnsubLinker struct {
	link  string
	err   error
	calls int
	gotTo string
}

func (s *stubUnsubLinker) GenerateUnsubscribeLink(_ context.Context, email, _ string) (string, error) {
	s.calls++
	s.gotTo = email
	return s.link, s.err
}

const unsubLink = "https://crm.example.com/api/email/unsubscribe?token=abc"

func headerOrEmpty(m *gomail.Message, field string) string {
	got := m.GetHeader(field)
	if len(got) == 0 {
		return ""
	}
	return got[0]
}

func newHeaderTestSend(to string) *model.EmailSend {
	return &model.EmailSend{
		ID:      "send-1",
		To:      to,
		Subject: "本月新品",
		Content: "<p>正文</p>",
	}
}

func stubSmtp() *model.EmailSmtp {
	return &model.EmailSmtp{Server: "smtp.example.com", Port: 465, Username: "noreply@example.com"}
}

// TestBuildEmailMessageCarriesOneClickHeaders 有链接 ⇒ 两个头必须一起上，且值就是那条链接。
func TestBuildEmailMessageCarriesOneClickHeaders(t *testing.T) {
	linker := &stubUnsubLinker{link: unsubLink}
	svc := NewEmailSendService()
	svc.SetUnsubscribeLinker(linker)

	m := svc.buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("a@b.example"))

	if want := "<" + unsubLink + ">"; headerOrEmpty(m, "List-Unsubscribe") != want {
		t.Errorf("List-Unsubscribe = %q，期望 %q", headerOrEmpty(m, "List-Unsubscribe"), want)
	}
	if got := headerOrEmpty(m, "List-Unsubscribe-Post"); got != "List-Unsubscribe=One-Click" {
		t.Errorf("List-Unsubscribe-Post = %q，一键退订声明缺失或写错", got)
	}
	if linker.calls != 1 {
		t.Errorf("签发调用 %d 次，期望 1 次", linker.calls)
	}
	if linker.gotTo != "a@b.example" {
		t.Errorf("退订链接是为 %q 签的，期望收件人 a@b.example", linker.gotTo)
	}
}

// TestBuildEmailMessageOmitsHeadersWithoutLinker 没注入 linker ⇒ 头一个都不许出现。
//
// 只出现 List-Unsubscribe-Post 而没有链接，是向 Gmail/Yahoo 声明了一个本邮件不存在的
// 一键退订能力 —— 那比两个头都没有更糟（前者会被判定为承诺未兑现）。
func TestBuildEmailMessageOmitsHeadersWithoutLinker(t *testing.T) {
	svc := NewEmailSendService()
	svc.SetUnsubscribeLinker(nil)

	m := svc.buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("a@b.example"))
	if got := headerOrEmpty(m, "List-Unsubscribe"); got != "" {
		t.Errorf("无 linker 却有退订头：%q", got)
	}
	if got := headerOrEmpty(m, "List-Unsubscribe-Post"); got != "" {
		t.Errorf("无 linker 却声明了一键退订：%q", got)
	}
}

// TestBuildEmailMessageFailOpenWhenSigningFails 签发失败（典型是 EMAIL_UNSUBSCRIBE_SECRET 未配置）
// 时信照发、头不上。
//
// 反向选择（拒发）把"配置缺失"放大成"整批营销邮件停摆"，而缺退订出口只是不合规；
// 两者的可归因性差很多，所以这里选 fail-open，并由 unsubscribeLink 出声一次。
func TestBuildEmailMessageFailOpenWhenSigningFails(t *testing.T) {
	linker := &stubUnsubLinker{err: errors.New("EMAIL_UNSUBSCRIBE_SECRET 未配置")}
	svc := NewEmailSendService()
	svc.SetUnsubscribeLinker(linker)

	m := svc.buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("a@b.example"))
	if got := headerOrEmpty(m, "List-Unsubscribe"); got != "" {
		t.Errorf("签发失败仍写了退订头：%q", got)
	}
	if got := headerOrEmpty(m, "To"); got != "a@b.example" {
		t.Errorf("签发失败把信本身也带坏了，To = %q", got)
	}
	if got := headerOrEmpty(m, "Subject"); got == "" {
		t.Error("Subject 丢失")
	}
}

// TestEmailBodyAppendsFooterOnlyWithLink 正文页脚与头部链接同源：链接为空时两边都不许出现。
func TestEmailBodyAppendsFooterOnlyWithLink(t *testing.T) {
	svc := NewEmailSendService()
	body := "<p>正文</p>"
	if got := svc.emailBody(newHeaderTestSend("a@b.example"), unsubLink); got == body {
		t.Errorf("有链接却没追加退订页脚：%q", got)
	}
	if got := svc.emailBody(newHeaderTestSend("a@b.example"), ""); got != body {
		t.Errorf("无链接仍塞了页脚：%q", got)
	}
}

// TestNewEmailSendServiceDefaultsLinker 构造函数必须自带退订签发器。
//
// 本仓有三个 NewEmailSendService() 调用点（排水装配、HTTP controller、reach 适配器）。
// 如果只在装配层注入，走 HTTP 即时发送的那条路就没有退订出口 —— 而它恰恰是
// 面向客户的第一条路。默认值写在构造函数里，注入只用于替换（测试与未来的多租户）。
func TestNewEmailSendServiceDefaultsLinker(t *testing.T) {
	if NewEmailSendService().unsubLinker == nil {
		t.Error("默认构造的 EmailSendService 没有退订签发器 ⇒ 即时发送路径无 List-Unsubscribe")
	}
}

// TestBuildEmailMessageFromComesFromSmtpConfig From 头取自本次投递选中的 SMTP 配置，
// 不是环境变量里的默认账号 —— 一封信用 A 账号签的退订链接、从 B 账号发出，
// 退订落库时就没有可靠的归属。
func TestBuildEmailMessageFromComesFromSmtpConfig(t *testing.T) {
	m := NewEmailSendService().buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("x@y.example"))
	if got := headerOrEmpty(m, "From"); got != "noreply@example.com" {
		t.Errorf("From = %q，期望取自 SMTP 配置的 Username", got)
	}
}
