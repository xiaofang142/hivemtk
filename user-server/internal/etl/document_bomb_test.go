package etl

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildDocx 用给定的 document.xml 内容打包一个最小 docx（内存构造，落盘零副作用）
func buildDocx(t *testing.T, documentXML []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(documentXML); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtractDocx_ZipBombRefused 第十六轮回归：压缩比 ~1000:1 的炸弹 docx
// （40MB 重复字节 deflate 后远小于 1MB）必须在解压阶段熔断而非全量 ReadAll。
// 反向测试：还原为 io.ReadAll(rc) ⇒ 本用例会因炸弹被完整解压而失败/耗内存。
func TestExtractDocx_ZipBombRefused(t *testing.T) {
	bomb := bytes.Repeat([]byte("<w:t>AAAAAAAAAAAAAAAA</w:t>"), 2<<20) // ~40MB 压缩前
	docx := buildDocx(t, bomb)
	if len(docx) > 8<<20 {
		t.Fatalf("夹具应远小于解压产物以体现放大比，got zip=%d", len(docx))
	}
	_, err := ExtractText("evil.docx", docx)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("炸弹 docx 应被解压上限熔断，got err=%v", err)
	}
}

// TestExtractDocx_NormalDocStillExtracts 正常文档回归：熔断不得误伤常规导入。
func TestExtractDocx_NormalDocStillExtracts(t *testing.T) {
	xmlBody := []byte(`<?xml version="1.0"?><w:document><w:body><w:p><w:r><w:t>你好 hello</w:t></w:r></w:p></w:body></w:document>`)
	got, err := ExtractText("normal.docx", buildDocx(t, xmlBody))
	if err != nil {
		t.Fatalf("正常 docx 提取失败: %v", err)
	}
	if !strings.Contains(got, "你好 hello") {
		t.Fatalf("文本内容丢失: %q", got)
	}
}
