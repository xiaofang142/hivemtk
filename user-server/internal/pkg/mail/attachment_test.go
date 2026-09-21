package mail

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/gomail.v2"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("建 %s 的父目录失败: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写 %s 失败: %v", path, err)
	}
}

// encoded 返回内容在 MIME 里的样子。
//
// 附件正文是 base64 的，直接 in 原文找等于永远找不到 —— 判据要能真的判：
// 断"这个文件的字节在信里"就得断它的 base64 在信里。夹具内容保持短，
// 免得 base64 被折行后跨行找不到。
func encoded(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}

func newTestMessage() *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", "ops@example.com")
	m.SetHeader("To", "lead@example.com")
	m.SetHeader("Subject", "本月新品")
	m.SetBody("text/html", "<p>正文</p>")
	return m
}

// storageFixture 造一个与上传侧同构的附件根：{dir}/{yyyy}/{mm}/{uuid}.{ext}。
//
// 目录形状必须照抄真实落盘形状：本用例要判的正是"外发侧认不认上传接口产出的那个 URL"，
// 而夹具若铺成扁平的 dir/x.pdf，判据就是在验一个生产里不存在的形状。
// 越界文件（root/outside.pdf）放在附件根的上一层：只断"名字被改过"是没牙的，
// 它得真的存在、真的可读，才分得出"被拦住了"和"照原样拼上去了"。
func storageFixture(t *testing.T) (dir, rel, outside string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "attachments")
	rel = "2026/09/19fc1d70.jpg"
	mustWrite(t, filepath.Join(dir, filepath.FromSlash(rel)), "报价单")
	outside = filepath.Join(root, "outside.pdf")
	mustWrite(t, outside, "越界内容")
	// 两个"根内但形状不对"的诱饵：形状那一腿（年/月必须是数字）只有真的能 Stat 到文件
	// 才判得出来 —— 否则"没有形状检查"和"有检查但文件不存在"两种实现看起来一模一样。
	mustWrite(t, filepath.Join(dir, "abcd", "09", "decoy.pdf"), "诱饵")
	mustWrite(t, filepath.Join(dir, "2026", "ab", "decoy.pdf"), "诱饵")
	return dir, rel, outside
}

func TestLocalAttachmentsResolvesOwnStorageRelativeURL(t *testing.T) {
	dir, rel, _ := storageFixture(t)

	path, ok := LocalAttachments(dir, "/files")("/files/attachments/" + rel)
	if !ok {
		t.Fatal("上传接口给出的相对 URL 没被认成附件 ⇒ 附件出口照旧是死的")
	}
	if filepath.Base(path) != filepath.Base(rel) {
		t.Errorf("解析到了别的文件：%s", path)
	}
}

func TestLocalAttachmentsResolvesAbsolutePublicURL(t *testing.T) {
	dir, rel, _ := storageFixture(t)

	// STORAGE_LOCAL_PUBLIC_URL 可以配成带域名的绝对地址；粘贴进邮件正文编辑器的
	// 也常常是绝对地址。前缀比对只看路径段，否则这一腿又变成静默不附。
	resolve := LocalAttachments(dir, "https://cdn.example.com/files")
	for _, value := range []string{
		"https://cdn.example.com/files/attachments/" + rel,
		"http://other-host.example/files/attachments/" + rel,
		"/files/attachments/" + rel,
	} {
		if _, ok := resolve(value); !ok {
			t.Errorf("%s 该解析成附件（前缀只比路径段）", value)
		}
	}

	// 公开地址配成裸源站（不带路径）时，上传接口回的是 host/attachments/…：
	// 前缀也要同样只取路径段，否则这种部署下附件又静默不附。
	bareOrigin := LocalAttachments(dir, "https://crm.example.com")
	if _, ok := bareOrigin("https://crm.example.com/attachments/" + rel); !ok {
		t.Error("裸源站公开地址下解析不出自家附件")
	}
}

func TestLocalAttachmentsRejectsValuesOutsideLayout(t *testing.T) {
	dir, rel, _ := storageFixture(t)
	resolve := LocalAttachments(dir, "/files")

	for _, value := range []string{
		"",
		rel,                              // 少了公开前缀 ⇒ 不猜
		"/files/" + rel,                  // 前缀对但目录段不是附件根
		"/files/attachments/outside.pdf", // 少了 {yyyy}/{mm} 两层 ⇒ 根里别的文件也不给
		"/files/attachments/2026/" + rel, // 多塞一层
		"/files/attachments/../../outside.pdf",
		"/files/attachments/2026/09/../../../outside.pdf",
		"/files/attachments/2026/09/..",        // 形状对、落点是个目录 ⇒ 不是可挂的文件
		"/files/attachments/abcd/09/decoy.pdf", // 年段不是数字 ⇒ 不是本站落盘形状
		"/files/attachments/2026/ab/decoy.pdf", // 月段同上
		"%2ffiles%2fattachments%2f" + rel,      // 没解码就比对：编码过的斜杠不算路径分隔
		"mailto:someone@example.com",
		"https://x.example/files/attachments/",
	} {
		if path, ok := resolve(value); ok {
			t.Errorf("%q 本该被拒，却解析成 %s", value, path)
		}
	}
}

func TestLocalAttachmentsAcceptsQueryAndFragmentButNotTheirPayload(t *testing.T) {
	dir, rel, _ := storageFixture(t)
	resolve := LocalAttachments(dir, "/files")

	// 粘贴时带上的 query/fragment 是常态噪声，截掉后仍该认。
	if _, ok := resolve("/files/attachments/" + rel + "?v=2#top"); !ok {
		t.Error("带 query 的自家 URL 没被认成附件")
	}
	// 截断只发生在第一个 ?/# 之前：藏在后面的 .. 不参与拼接。
	if _, ok := resolve("/files/attachments/?x=../../outside.pdf"); ok {
		t.Error("query 里的目录段参与了拼接")
	}
}

func TestLocalAttachmentsRejectsMissingAndNonRegular(t *testing.T) {
	dir, _, outside := storageFixture(t)
	resolve := LocalAttachments(dir, "/files")

	if _, ok := resolve("/files/attachments/2026/09/gone.jpg"); ok {
		t.Error("没落盘的值被判成可附 ⇒ 会在真正外发时让整封信失败")
	}
	// 目录段对上、最后一段是指向根外文件的软链接：形状判定放过它，必须靠"不跟随链接的
	// Lstat + 常规文件"这一条拦住（os.Stat 会跟到根外那个文件上）。
	link := filepath.Join(dir, "2026", "09", "link.jpg")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatalf("建软链父目录失败: %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境建不了软链接: %v", err)
	}
	if path, ok := resolve("/files/attachments/2026/09/link.jpg"); ok {
		t.Errorf("软链接指向附件根之外的文件也挂上了：%s", path)
	}
}

func TestAttachmentPathsKeepsResolvableAndDropsRest(t *testing.T) {
	dir, rel, _ := storageFixture(t)
	paths := AttachmentPaths(" ,/files/attachments/"+rel+" ,gone.jpg,,/files/attachments/"+rel+"../x",
		LocalAttachments(dir, "/files"))

	if len(paths) != 1 {
		t.Fatalf("期望只解析出 1 个附件，实际 %d 个：%v", len(paths), paths)
	}
	if filepath.Base(paths[0]) != filepath.Base(rel) {
		t.Errorf("解析到了别的文件：%s", paths[0])
	}
}

func TestAttachmentsFromPathsRendersFileBytes(t *testing.T) {
	dir, rel, _ := storageFixture(t)
	paths := AttachmentPaths("/files/attachments/"+rel, LocalAttachments(dir, "/files"))

	m := newTestMessage()
	AttachmentsFromPaths(paths)(m)

	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatalf("渲染消息失败: %v", err)
	}
	parts := buf.String()
	if !strings.Contains(parts, "Content-Disposition") {
		t.Errorf("缺附件的 Content-Disposition：%s", parts)
	}
	if !strings.Contains(parts, encoded("报价单")) {
		t.Errorf("附件内容没被带上：%s", parts)
	}
}

func TestAttachmentsFromPathsWithNothingIsNoop(t *testing.T) {
	dir, _, _ := storageFixture(t)
	empty := AttachmentPaths("", LocalAttachments(dir, "/files"))
	if len(empty) != 0 {
		t.Fatalf("空 CSV 解析出了附件：%v", empty)
	}

	m := newTestMessage()
	AttachmentsFromPaths(empty)(m)

	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatalf("渲染消息失败: %v", err)
	}
	text := buf.String()
	// 逐字节比不了：MIME boundary 每次随机。判"没改动消息"用结构面 ——
	// 没有附件的信必须还是单部件，正文头照旧。
	if strings.Contains(text, "multipart") {
		t.Errorf("没有附件却劈成了多部件：%s", text)
	}
	if !strings.Contains(text, "Content-Type: text/html") {
		t.Errorf("空附件列表把正文弄丢了：%s", text)
	}
}

func TestAttachmentsDroppedOnlyForNonEmptyCSV(t *testing.T) {
	dir, rel, _ := storageFixture(t)
	resolve := LocalAttachments(dir, "/files")
	paths := AttachmentPaths("/files/attachments/"+rel, resolve)
	if len(paths) != 1 {
		t.Fatalf("夹具前提不成立：合法值没解析出来")
	}

	if AttachmentsDropped("", paths) {
		t.Error("没填附件列也判成丢了附件")
	}
	if AttachmentsDropped("/files/attachments/"+rel, paths) {
		t.Error("附件挂上了还判成丢失")
	}
	if !AttachmentsDropped("/files/attachments/2026/09/gone.pdf", AttachmentPaths("/files/attachments/2026/09/gone.pdf", resolve)) {
		t.Error("填了却一项都没挂上，判定没抓到 ⇒ 出声那一格永远不会响")
	}
	// 前端把多行文本按逗号/换行拼列，"空条目"是真会出现的输入，不该被判成丢了附件。
	if AttachmentsDropped(" , ,", AttachmentPaths(" , ,", resolve)) {
		t.Error("一列空白条目也被判成丢了附件")
	}
}
