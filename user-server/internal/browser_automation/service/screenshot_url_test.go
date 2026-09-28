package service

// 最终截图的落库值必须是「浏览器能直接取到的公开 URL」。
//
// 写入口只有一处（SaveFinalScreenshot），但它一度把 UploadReader 的两个返回值解构反了，
// 于是库里存的是存储相对路径（browser_automation/2026/09/….png）。这种值不是 URL：
// 相对当前页面解析后打到 SPA 的 index.html（实测 http=200、content-type=text/html），
// 监控页的截图卡片于是恒为裂图——全部带截图的历史会话无一例外。
//
// 判据因此同时钉两侧，单钉任一侧都拦不住这个缺陷：
//   - 只断「以 /files/ 开头」→ 把 storagePath 拼上前缀再返回也能过；
//   - 只断「磁盘有文件」→ 解构写反同样成立（文件本来就落在那儿）。
// 两侧一起比，才等价于「库里那个值指向的就是本次上传的那份字节」这条不变量。

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 1x1 PNG（67 字节，base64 已用 `base64 -D | cmp` 与原文件往返比对过），
// 保证 decodeBase64 走真实解码而不是空串短路。
const onePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAACklEQVR4nGMAAQAABQABDQottAAAAABJRU5ErkJggg=="

func TestSaveFinalScreenshotReturnsPublicURLNotStoragePath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)

	f := NewFeedbackService(nil, nil)
	url, err := f.SaveFinalScreenshot(context.Background(), 327, onePixelPNGBase64)
	if err != nil {
		t.Fatalf("SaveFinalScreenshot 报错：%v", err)
	}
	if url == "" {
		t.Fatal("返回空 URL——截图没有任何可访问出口")
	}
	if !strings.HasPrefix(url, "/files/browser_automation/") {
		t.Errorf("落库值必须是可访问的公开 URL（前缀 /files/），实得 %q", url)
	}
	if strings.Contains(url, "//") {
		t.Errorf("URL 里出现重复斜杠，前端会 404：%q", url)
	}
	if strings.HasPrefix(url, "browser_automation/") {
		t.Errorf("返回的是 storagePath 而不是 publicURL（解构顺序反了）：%q", url)
	}

	// 另一侧：URL 去掉 /files/ 前缀必须正好命中本次落盘的那份字节。
	rel := strings.TrimPrefix(url, "/files/")
	full := filepath.Join(base, filepath.FromSlash(rel))
	got, err := os.ReadFile(full)
	if err != nil {
		t.Errorf("URL %q 指向的位置没有文件（%s）：%v", url, full, err)
	}
	want, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatalf("夹具本身不是合法 base64：%v", err)
	}
	if string(got) != string(want) {
		t.Errorf("URL 指向的文件内容与上传内容不一致（got %d 字节 / want %d 字节）", len(got), len(want))
	}
	// 落盘名是 UUID（generatePath 只从文件名取扩展名），所以归属靠会话记录这一行而不是文件名；
	// 这里钉住扩展名保留为 .png——被中和成 .bin 的话 <img> 直接不渲染。
	if ext := filepath.Ext(rel); ext != ".png" {
		t.Errorf("截图扩展名应保留 .png，实得 %q（路径 %q）", ext, rel)
	}
}

func TestSaveFinalScreenshotEmptyInputIsNoop(t *testing.T) {
	t.Setenv("STORAGE_LOCAL_BASE_DIR", t.TempDir())
	f := NewFeedbackService(nil, nil)
	url, err := f.SaveFinalScreenshot(context.Background(), 1, "")
	if err != nil || url != "" {
		t.Fatalf("空截图应是 (\"\", nil) 静默跳过，实得 (%q, %v)", url, err)
	}
}

func TestSaveFinalScreenshotAcceptsDataURLPrefix(t *testing.T) {
	t.Setenv("STORAGE_LOCAL_BASE_DIR", t.TempDir())
	f := NewFeedbackService(nil, nil)
	// 扩展上报的是 data URL（chrome.tabs.captureVisibleTab 的原始形态）
	url, err := f.SaveFinalScreenshot(context.Background(), 5, "data:image/png;base64,"+onePixelPNGBase64)
	if err != nil {
		t.Fatalf("data URL 前缀应被兼容，实得错误：%v", err)
	}
	if !strings.HasPrefix(url, "/files/") {
		t.Errorf("data URL 分支也必须返回公开 URL，实得 %q", url)
	}
}

func TestSaveFinalScreenshotRejectsGarbageBase64(t *testing.T) {
	base := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", base)
	f := NewFeedbackService(nil, nil)
	if _, err := f.SaveFinalScreenshot(context.Background(), 5, "@@not-base64@@"); err == nil {
		t.Fatal("非法 base64 必须报错——否则用户看到的是「截图成功但库里没东西」")
	}
	// 失败路径不许留半个文件：监控页没有截图时该显示「未采集」，而不是指向一张不存在的图
	matches, err := filepath.Glob(filepath.Join(base, "*", "*", "*"))
	if err != nil {
		t.Fatalf("夹具扫描失败：%v", err)
	}
	if len(matches) != 0 {
		t.Errorf("解码失败后 base 目录必须仍是空的，实得 %v", matches)
	}
}
