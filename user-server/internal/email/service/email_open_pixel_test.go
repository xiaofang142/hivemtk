// email_open_pixel_test.go 外发正文的打开追踪像素出口：签发器默认可用、签不出来不阻断发信。
//
// 这一格存在的理由本身：追踪链路的消费侧（路由 + RenderPixel + 事件落库）早就装配齐了，
// 唯独"把像素放进正文"的签发侧全仓非测试调用点为 0 ⇒ 打开事件永远录不到第一条，
// 而管理端读到的计数是 0 而不是"没接线"。
package email

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubOpenPixelLinker struct {
	url    string
	err    error
	calls  int
	gotTo  string
	gotJob string
}

func (s *stubOpenPixelLinker) GenerateOpenPixelURL(_ context.Context, email, jobID string) (string, error) {
	s.calls++
	s.gotTo = email
	s.gotJob = jobID
	return s.url, s.err
}

const issuedPixel = "https://crm.example.com/api/email/track/open/abc.def.png"

// TestBuildEmailMessageSignsPixelForThisRecipient 像素按"本封的收件人 + 本封的发送记录"签发。
//
// 复用别人的 token 等于把打开事件记到别的客户头上，所以 jobID 必须是这一条 EmailSend 的 ID。
func TestBuildEmailMessageSignsPixelForThisRecipient(t *testing.T) {
	linker := &stubOpenPixelLinker{url: issuedPixel}
	svc := NewEmailSendService()
	svc.SetOpenPixelLinker(linker)

	svc.buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("a@b.example"))

	if linker.calls != 1 {
		t.Fatalf("签发调用 %d 次，期望 1 次", linker.calls)
	}
	if linker.gotTo != "a@b.example" {
		t.Errorf("像素是为 %q 签的，期望收件人 a@b.example", linker.gotTo)
	}
	if linker.gotJob != "send-1" {
		t.Errorf("像素归属 jobID = %q，期望本条发送记录 ID send-1", linker.gotJob)
	}
}

// TestEmailBodyAppendsPixelOnlyWhenIssued 有 URL 才有 img，没有就一个字节都不许多。
func TestEmailBodyAppendsPixelOnlyWhenIssued(t *testing.T) {
	svc := NewEmailSendService()
	send := newHeaderTestSend("a@b.example")

	withPixel := svc.emailBody(send, unsubLink, issuedPixel)
	if !strings.Contains(withPixel, "<img") || !strings.Contains(withPixel, issuedPixel) {
		t.Errorf("有链接却没塞进像素：%q", withPixel)
	}
	if !strings.Contains(withPixel, "退订此类邮件") {
		t.Errorf("加了像素把退订页脚挤掉了：%q", withPixel)
	}

	noPixel := svc.emailBody(send, unsubLink, "")
	if strings.Contains(noPixel, "<img") {
		t.Errorf("没链接仍塞了像素：%q", noPixel)
	}
	if strings.Contains(svc.emailBody(send, "", ""), "<img") ||
		strings.Contains(svc.emailBody(send, "", ""), "退订") {
		t.Error("两个出口都没有时正文被系统改动了")
	}
}

// TestBuildEmailMessageFailOpenWhenPixelSigningFails 签发失败（典型是 EMAIL_TRACKING_SECRET 未配置）
// ⇒ 信照发、正文不带像素。
//
// 与退订头同一档取舍：追踪是可选能力，缺密钥只该让这一封信没有统计，不该让整批发不出去。
func TestBuildEmailMessageFailOpenWhenPixelSigningFails(t *testing.T) {
	linker := &stubOpenPixelLinker{err: errors.New("EMAIL_TRACKING_SECRET 未配置")}
	svc := NewEmailSendService()
	svc.SetOpenPixelLinker(linker)

	m := svc.buildEmailMessage(context.Background(), stubSmtp(), newHeaderTestSend("a@b.example"))
	if got := headerOrEmpty(m, "To"); got != "a@b.example" {
		t.Errorf("像素签发失败把信本身也带坏了，To = %q", got)
	}
	if got := headerOrEmpty(m, "Subject"); got == "" {
		t.Error("Subject 丢失")
	}
}

// TestOpenPixelURLEmptyWithoutLinker 没注入签发器 ⇒ 不调用、不出 URL。
func TestOpenPixelURLEmptyWithoutLinker(t *testing.T) {
	svc := NewEmailSendService()
	svc.SetOpenPixelLinker(nil)
	if got := svc.openPixelURL(context.Background(), newHeaderTestSend("a@b.example")); got != "" {
		t.Errorf("无签发器却给出像素 URL：%q", got)
	}
}

// TestNewEmailSendServiceDefaultsOpenPixelLinker 构造函数必须自带像素签发器。
//
// 与退订签发器同一条理由：NewEmailSendService 有三个调用点，只在装配层注入的话
// 走 HTTP 即时发送那条路永远没有统计，而缺的那一半恰恰看不出来。
func TestNewEmailSendServiceDefaultsOpenPixelLinker(t *testing.T) {
	if NewEmailSendService().openPixel == nil {
		t.Error("默认构造的 EmailSendService 没有像素签发器 ⇒ 打开追踪链路仍是死的")
	}
}
