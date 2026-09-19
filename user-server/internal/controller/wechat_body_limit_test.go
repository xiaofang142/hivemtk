package controller

import (
	"bytes"
	"testing"
)

// TestReadWechatBody_Capped 第十六轮回归：超限 body 截断为上限长度
// （截断后的非法 XML 让调用方走 400，fail-closed），正常小 body 原样读全。
func TestReadWechatBody_Capped(t *testing.T) {
	big := bytes.Repeat([]byte("x"), int(maxWechatBody)+4096)
	got, err := readWechatBody(bytes.NewReader(big))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if int64(len(got)) != maxWechatBody {
		t.Fatalf("应截断到 %d 字节，got %d", maxWechatBody, len(got))
	}
	small := []byte("<xml>ok</xml>")
	got, err = readWechatBody(bytes.NewReader(small))
	if err != nil || string(got) != string(small) {
		t.Fatalf("小 body 应原样读出: %q err=%v", got, err)
	}
}
