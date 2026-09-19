package storage

import (
	"context"
	"strings"
	"testing"
)

// TestGeneratePath_NeutralizesWebExecutableExts 落盘层中和：
// 同源可执行/可渲染扩展名一律改写为 .bin（素材库任意扩展名、WhatsApp
// 媒体嗅探出 text/html 补 .html 两条真实路径的共同收口）。
func TestGeneratePath_NeutralizesWebExecutableExts(t *testing.T) {
	d := newTestDriver(t)
	dangerous := []string{
		"evil.html", "evil.HTM", "x.xhtml", "legacy.svg",
		"icon.svgz", "app.js", "mod.mjs", "data.xml", "upper.HTML", "dot.SVG",
	}
	for _, name := range dangerous {
		got := d.generatePath("materials", name)
		if strings.HasSuffix(got, ".bin") {
			continue
		}
		t.Errorf("危险扩展名应中和为 .bin: filename=%q -> %q", name, got)
	}
	safe := []string{"photo.png", "doc.pdf", "notes.txt", "archive.zip", "noext"}
	for _, name := range safe {
		got := d.generatePath("materials", name)
		if strings.HasSuffix(got, ".bin") && name != "noext" {
			t.Errorf("正常扩展名不应被误伤: filename=%q -> %q", name, got)
		}
	}
}

// TestUploadReader_HtmlPayloadStoredAsBin 端到端：以 evil.html 名义上传
// HTML 内容，落盘路径与公开 URL 都不允许保留 .html 扩展名。
func TestUploadReader_HtmlPayloadStoredAsBin(t *testing.T) {
	d := newTestDriver(t)
	payload := "<!DOCTYPE html><script>alert(1)</script>"
	url, storagePath, err := d.UploadReader(context.Background(), strings.NewReader(payload), int64(len(payload)), "channels/whatsapp/2026/09", "invoice.html")
	if err != nil {
		t.Fatalf("UploadReader: %v", err)
	}
	if strings.HasSuffix(storagePath, ".html") || strings.HasSuffix(url, ".html") {
		t.Fatalf("HTML 载荷落盘保留了可执行扩展名: path=%q url=%q", storagePath, url)
	}
	if !strings.HasSuffix(storagePath, ".bin") {
		t.Fatalf("应中和为 .bin: %q", storagePath)
	}
}

func TestIsDangerousWebExt(t *testing.T) {
	for _, e := range []string{".html", ".htm", ".xhtml", ".svg", ".js", ".mjs", ".xml", ".HTML", ".Svg"} {
		if !IsDangerousWebExt(e) {
			t.Errorf("应判危险: %q", e)
		}
	}
	for _, e := range []string{".png", ".jpg", ".pdf", ".txt", ".zip", ".bin", ""} {
		if IsDangerousWebExt(e) {
			t.Errorf("不应判危险: %q", e)
		}
	}
}
