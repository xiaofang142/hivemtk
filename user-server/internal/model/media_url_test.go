package model

import (
	"strings"
	"testing"
)

// 2026-11-10 审计 F2：media_url 列宽 varchar(500) 的入库前收口。
// 平台签名 URL 常超 500 字符，无截断时 INSERT 22001 整条消息落库失败。
func TestTruncateMediaURL(t *testing.T) {
	if got := TruncateMediaURL(""); got != "" {
		t.Fatalf("空串原样返回: %q", got)
	}
	short := "https://p3-sign.douyinpic.com/img/x.png?x-expires=1"
	if got := TruncateMediaURL(short); got != short {
		t.Fatalf("短 URL 原样返回: %q", got)
	}

	// 500 字符整（ASCII）恰好等于列宽，不截
	exact := strings.Repeat("a", MaxMediaURLLen)
	if got := TruncateMediaURL(exact); got != exact {
		t.Fatalf("恰好 500 字符不得截断")
	}
	// 501 字符截到 500
	long := strings.Repeat("a", MaxMediaURLLen+1)
	if got := TruncateMediaURL(long); len([]rune(got)) != MaxMediaURLLen {
		t.Fatalf("超长应截到 %d 字符，实际 %d", MaxMediaURLLen, len([]rune(got)))
	}

	// 多字节字符按 rune 计数（varchar(500) 按字符不按字节）：501 个中文字符截到 500
	cjk := strings.Repeat("图", MaxMediaURLLen+1)
	got := TruncateMediaURL(cjk)
	if len([]rune(got)) != MaxMediaURLLen {
		t.Fatalf("CJK 应按 rune 截到 %d，实际 %d", MaxMediaURLLen, len([]rune(got)))
	}
	if len(got) > MaxMediaURLLen*4 {
		t.Fatalf("截断结果字节长度异常: %d", len(got))
	}
}
