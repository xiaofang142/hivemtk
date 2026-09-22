package config

import "testing"

// TestWebsiteBaseURLOverrideAndTrim 官网基址只有一个来源，且结尾永远不带 '/'。
//
// 带 '/' 会让拼接出 "//features"，不带边界判断会让 "/hivemtk" 命中 "/hivemtkevil"。
func TestWebsiteBaseURLOverrideAndTrim(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "")
	if got := WebsiteBaseURL(); got != DefaultWebsiteBaseURL {
		t.Fatalf("未设置覆盖变量时应取默认基址，实际=%q", got)
	}
	t.Setenv("GEO_SITE_BASE_URL", "https://example.com/mysite///")
	if got := WebsiteBaseURL(); got != "https://example.com/mysite" {
		t.Fatalf("结尾斜杠必须全部裁掉，实际=%q", got)
	}
}

func TestIsSelfSiteURLUsesBasePrefixWithBoundary(t *testing.T) {
	t.Setenv("GEO_SITE_BASE_URL", "https://xiaofang142.github.io/hivemtk")

	cases := []struct {
		url  string
		want bool
		why  string
	}{
		{"https://xiaofang142.github.io/hivemtk/", true, "基址首页"},
		{"https://xiaofang142.github.io/hivemtk/features", true, "基址下深链"},
		{"https://xiaofang142.github.io/hivemtk", true, "无路径的基址本身"},
		{"https://xiaofang142.github.io/someone-else/", false, "同 host 不同项目页不得算自家"},
		{"https://xiaofang142.github.io/hivemtkevil/x", false, "前缀必须落在路径边界上"},
		{"https://weibanzhushou.com/", false, "竞品站"},
		{"", false, "空串"},
	}
	for _, tc := range cases {
		if got := IsSelfSiteURL(tc.url); got != tc.want {
			t.Errorf("IsSelfSiteURL(%q)=%v，期望 %v（%s）", tc.url, got, tc.want, tc.why)
		}
	}
}
