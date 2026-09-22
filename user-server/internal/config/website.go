package config

import (
	"os"
	"strings"
)

// WebsiteBaseURL 官网基址（运行期唯一出口，拼接站内路径都走这里）。
//
// 覆盖：GEO_SITE_BASE_URL；未设置时取 DefaultWebsiteBaseURL。
// 结尾恒不带 '/'，供调用方直接 + "/features" 而不出 "//features"。
func WebsiteBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("GEO_SITE_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return strings.TrimRight(DefaultWebsiteBaseURL, "/")
}

// IsSelfSiteURL 判断一个 URL 是否指向本系统自己的官网。
//
// 判据是"基址 + 路径边界"而不是 host：GitHub Pages 项目页形态下，同一 host 上
// 还住着无数别人的项目（xiaofang142.github.io/someone-else/），只比 host 会把它们
// 全算成自家；而 "hivemtkevil" 这类同前缀不同路径又会躲过裸前缀匹配。
func IsSelfSiteURL(rawURL string) bool {
	base := WebsiteBaseURL()
	if base == "" || rawURL == "" {
		return false
	}
	return rawURL == base || strings.HasPrefix(rawURL, base+"/")
}
