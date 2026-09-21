// email_attachments_test.go 单封投递侧的附件出口（#61）：粘贴进来的自家存储 URL 要真挂得上，
// 挂不上的值不许把整封信带坏。
package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/mail"

	"gopkg.in/gomail.v2"
)

// attachmentFixture 造一棵与上传侧同构的附件树，返回附件根。
func attachmentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "attachments")
	if err := os.MkdirAll(filepath.Join(dir, "2026", "09"), 0o750); err != nil {
		t.Fatalf("建附件目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026", "09", "quote.pdf"), []byte("报价单"), 0o600); err != nil {
		t.Fatalf("写附件失败: %v", err)
	}
	return dir
}

func renderMessage(t *testing.T, m *gomail.Message) string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatalf("渲染消息失败: %v", err)
	}
	return buf.String()
}

func TestBuildEmailMessageAttachesOwnStorageURL(t *testing.T) {
	dir := attachmentFixture(t)
	svc := NewEmailSendService()
	svc.attachments = mail.LocalAttachments(dir, "/files")

	email := newHeaderTestSend("a@b.example")
	// 混一条挂不上的值：合法项必须照常附上，不被连坐。
	email.Attachments = "/files/attachments/2026/09/quote.pdf , https://elsewhere.example/files/attachments/x.pdf"

	text := renderMessage(t, svc.buildEmailMessage(context.Background(), stubSmtp(), email))

	if !strings.Contains(text, "quote.pdf") {
		t.Errorf("自家存储 URL 没被当成附件挂上 ⇒ 附件出口仍是死的：%s", text)
	}
	if !strings.Contains(text, base64.StdEncoding.EncodeToString([]byte("报价单"))) {
		t.Errorf("附件字节没进消息：%s", text)
	}
	if !strings.Contains(text, "Content-Type: text/html") {
		t.Errorf("挂附件把正文弄丢了：%s", text)
	}
}

// TestBuildEmailMessageUnresolvableAttachmentsDoNotBlockSend 附件列非空但一项都挂不上时，
// 信本身必须照旧完整 —— 附件是增值项，不该让一次投递失败。
func TestBuildEmailMessageUnresolvableAttachmentsDoNotBlockSend(t *testing.T) {
	dir := attachmentFixture(t)
	svc := NewEmailSendService()
	svc.attachments = mail.LocalAttachments(dir, "/files")

	email := newHeaderTestSend("a@b.example")
	email.Attachments = "/files/attachments/2026/09/gone.pdf,../../etc/passwd"

	m := svc.buildEmailMessage(context.Background(), stubSmtp(), email)
	if got := headerOrEmpty(m, "To"); got != "a@b.example" {
		t.Errorf("To = %q", got)
	}
	if got := headerOrEmpty(m, "Subject"); got == "" {
		t.Error("Subject 丢失")
	}
	if strings.Contains(renderMessage(t, m), "Content-Disposition") {
		t.Error("解析不出的值仍被挂成了附件")
	}
}

// TestAttachmentResolverFallsBackToEnv 默认构造的服务必须自己就有解析器。
//
// 与 unsubLinker 同一形状的理由：NewEmailSendService 有三个调用点，只在装配层注入会让
// 其中某些路径的附件永远挂不上，而那表现是"静默少个附件"，没人会发现。
func TestAttachmentResolverFallsBackToEnv(t *testing.T) {
	svc := NewEmailSendService()
	if svc.attachments != nil {
		t.Fatal("夹具前提不成立：默认构造就带了解析器，兜底那一腿没被走到")
	}
	if svc.attachmentResolver() == nil {
		t.Error("兜底解析器为 nil ⇒ 附件列被整段忽略")
	}
	// 兜底走的是环境变量推出的真实根；这里只要求"解析一个不存在的值不 panic 且返回 false"。
	if _, ok := svc.attachmentResolver()("/files/attachments/2026/09/nope.pdf"); ok {
		t.Error("不存在于附件根的文件被判成可挂 ⇒ 会在外发那一刻让整封信失败")
	}
}
