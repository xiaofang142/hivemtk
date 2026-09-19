package logger

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// forgedTail：可控文本里塞一条"看起来完全合法"的日志行。
const forgedTail = "\n2026-09-19T00:00:00+08:00 ERR [FORGED] admin login ok user=root"

// useBuffer 把标准输出接缝临时指向缓冲，并留下还原用的 Cleanup。
func useBuffer(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	prev := stdout
	stdout = func() io.Writer { return buf }
	t.Cleanup(func() {
		stdout = prev
		InitLogger(LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	})
}

// assertNoForgedLine：一次日志事件必须只占一条物理行，且没有哪一行以被伪造的时间戳开头。
func assertNoForgedLine(t *testing.T, out string, wantEvents int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != wantEvents {
		t.Fatalf("必须一条事件一行（期望 %d 行），实际 %d 行:\n%s", wantEvents, len(lines), out)
	}
	for i, l := range lines {
		if strings.HasPrefix(l, "2026-09-19T00:00:00") {
			t.Fatalf("第 %d 行是被凭空劈出的假日志行（伪造时间戳开头）: %q", i, l)
		}
	}
}

func TestEscapeInnerLineBreaks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"普通单行原样", "hello\n", "hello\n"},
		{"正文换行转义", "a\nb\n", `a\nb` + "\n"},
		{"回车也转义", "a\rb\n", `a\rb` + "\n"},
		{"CRLF 一起转义", "a\r\nb\n", `a\r\nb` + "\n"},
		{"纯换行不吞掉行尾", "\n", "\n"},
		{"连续两个换行只剩一个行尾", "a\n\n", "a\\n\n"},
		{"无行尾时不补换行", "a\nb", `a\nb`},
		{"空字节流", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(escapeInnerLineBreaks([]byte(tc.in))); got != tc.want {
				t.Fatalf("escapeInnerLineBreaks(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// 走真实的 console 输出路径：InitLogger → consoleWriter → zerolog。
func TestConsoleRendererCannotForgeLogLines(t *testing.T) {
	var buf bytes.Buffer
	useBuffer(t, &buf)
	InitLogger(LoggingConfig{Level: "info", Format: "console", Output: "stdout"})

	Infof("login failed user=%s", "attacker"+forgedTail)
	Info("second event")

	out := buf.String()
	assertNoForgedLine(t, out, 2)
	if !strings.Contains(out, `login failed user=attacker\n2026-09-19T00:00:00+08:00 ERR [FORGED]`) {
		t.Fatalf("注入文本应留在原事件行内并以可见转义呈现，实际: %q", out)
	}
	if !strings.Contains(out, "second event") {
		t.Fatalf("第二条事件丢失: %q", out)
	}
}

// json 路径本就把换行转义在字符串里，不得被再包一层。
func TestJSONWriterIsNotWrapped(t *testing.T) {
	var buf bytes.Buffer
	useBuffer(t, &buf)

	if got := consoleWriter("json"); got != stdout() {
		t.Fatalf("json 分支必须直接返回底层 writer，实际类型 %T", got)
	}
	lw := zerolog.New(consoleWriter("json"))
	lw.Info().Msg("a" + forgedTail)
	assertNoForgedLine(t, buf.String(), 1)
}

// console 分支必须真的挂上接缝，否则上面的断言没有前提。
func TestConsoleWriterUsesLineSafeSeam(t *testing.T) {
	cw, ok := consoleWriter("console").(zerolog.ConsoleWriter)
	if !ok {
		t.Fatalf("console 分支应返回 zerolog.ConsoleWriter，实际 %T", consoleWriter("console"))
	}
	if _, ok := cw.Out.(lineSafeWriter); !ok {
		t.Fatalf("ConsoleWriter.Out 必须是 lineSafeWriter，实际 %T", cw.Out)
	}
}

// 接缝必须在调用时才读 os.Stdout：抓真实日志的用例靠的就是"换 os.Stdout 再重建日志器"，
// 若接缝在包初始化时冻结句柄，那条路径会静默变成假绿。
func TestStdoutSeamReadsHandleAtCallTime(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = pw
	InitLogger(LoggingConfig{Level: "info", Format: "console", Output: "stdout"})
	Infof("real-stdout %s", "x"+forgedTail)
	os.Stdout = prev
	if err := pw.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	defer pr.Close()

	got, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("未抓到任何输出：接缝冻结了包初始化时的 os.Stdout 句柄")
	}
	assertNoForgedLine(t, string(got), 1)
	InitLogger(LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return len(p) / 2, io.ErrShortWrite }

// 接缝必须照实上报底层写失败（返回 (0, err)），否则日志丢失会被当成写入成功。
func TestLineSafeWriterPropagatesFailure(t *testing.T) {
	n, err := lineSafeWriter{w: failingWriter{}}.Write([]byte("a\nb\n"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("应上报 io.ErrShortWrite，实际 %v", err)
	}
	if n != 0 {
		t.Fatalf("写失败时 n 必须为 0，实际 %d", n)
	}
}
