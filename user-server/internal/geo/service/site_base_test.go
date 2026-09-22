package service

import (
	"strings"
	"testing"

	"hivemtk-user/internal/config"
)

// websiteRoutes 官网真实路由表（与 website/src/router/index.js 的静态路由一一对应）。
//
// landing 里的每个路径都必须能在这里找到，否则爬虫就是在向一个 404 页投 AI 引擎，
// 测试跑在旧域名死链上从来不会红——因为没人校验过目标存在。
var websiteRoutes = map[string]bool{
	"/":          true,
	"/features":  true,
	"/toolchain": true,
	"/workflow":  true,
	"/docs":      true,
	"/faq":       true,
	"/deploy":    true,
}

// TestLandingPathsAllResolveToRealRoutes 关键词落地路径必须全部落在官网真实路由上。
func TestLandingPathsAllResolveToRealRoutes(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "https://example.com/hivemtk")

	for kw, paths := range keywordToLandings {
		if len(paths) == 0 {
			t.Errorf("关键词 %q 的 landing 列表为空", kw)
		}
		for _, p := range paths {
			if !strings.HasPrefix(p, "/") {
				t.Errorf("关键词 %q 的 landing %q 必须是站内路径（以 / 开头），不能是绝对 URL", kw, p)
				continue
			}
			if !websiteRoutes[p] {
				t.Errorf("关键词 %q 的 landing %q 在官网路由表里不存在（死链）", kw, p)
			}
		}
	}
}

// TestLandingURLsCarryConfiguredBase 拼出的绝对 URL 必须来自配置的基址，且旧域名零残留。
func TestLandingURLsCarryConfiguredBase(t *testing.T) {
	base := "https://example.com/hivemtk"
	t.Setenv("GEO_SITE_BASE_URL", base)

	got := landingURLs("GEO优化")
	if len(got) == 0 {
		t.Fatal("已知关键词必须返回 landing URL")
	}
	for _, u := range got {
		if !strings.HasPrefix(u, base+"/") {
			t.Errorf("landing URL %q 未挂在基址 %q 下", u, base)
		}
		if strings.Contains(u, "xapptool") {
			t.Errorf("landing URL %q 仍含已停用的旧域名", u)
		}
	}
}

// TestLandingURLsUnknownKeywordFallsBackToRoot 未配关键词的落地兜底是自家首页，不是旧域名。
func TestLandingURLsUnknownKeywordFallsBackToRoot(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "")

	got := landingURLs("一个没配过的关键词")
	want := []string{config.WebsiteBaseURL() + "/"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("未知关键词应兜底到官网首页 %v，实际 %v", want, got)
	}
	if strings.Contains(got[0], "xapptool") {
		t.Errorf("兜底 URL %q 仍指向旧域名", got[0])
	}
}

// TestLandingURLsNeverEmitEmptyList 任何关键词都必须至少给出一个 landing，
// 否则该关键词在自家站上的爬虫访问会整轮消失，可见度统计静默少一块。
func TestLandingURLsNeverEmitEmptyList(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "https://example.com/hivemtk")

	for kw := range keywordToLandings {
		if len(landingURLs(kw)) == 0 {
			t.Errorf("关键词 %q 返回零 landing", kw)
		}
	}
	for _, kw := range defaultSeedKeywords() {
		if len(landingURLs(kw)) == 0 {
			t.Errorf("种子关键词 %q 返回零 landing", kw)
		}
	}
}
