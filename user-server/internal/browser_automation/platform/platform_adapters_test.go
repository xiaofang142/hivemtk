package platform_test

import (
	"testing"

	"hivemtk-user/internal/browser_automation/platform"
	_ "hivemtk-user/internal/browser_automation/platform/douyin"
	_ "hivemtk-user/internal/browser_automation/platform/xianyu"
	_ "hivemtk-user/internal/browser_automation/platform/xiaohongshu"
)

// D5 补测：三平台适配器纯函数（DetectBlock/ClassifyError/Locators/Capabilities）——
// 零依赖表驱动（主文档 §5.2 G8 补测优先级 ③）。

func TestRegistryHasThreePlatforms(t *testing.T) {
	for _, id := range []string{"xiaohongshu", "douyin", "xianyu"} {
		if _, err := platform.Get(id); err != nil {
			t.Fatalf("平台 %s 未注册: %v", id, err)
		}
	}
}

func TestDetectBlock(t *testing.T) {
	cases := []struct {
		platform string
		snapshot string
		want     bool
	}{
		{"xiaohongshu", "跳转 https://www.xiaohongshu.com/website-login/error?error_code=300012", true},
		{"xiaohongshu", "当前环境异常，请完成验证后继续", true},
		{"xiaohongshu", "笔记标题 真好吃 评论区 12 条", false},
		{"douyin", "rmc.bytedance.com/verify 安全验证", true},
		{"douyin", "video-title 好吃", false},
		{"xianyu", "RGV587 滑块验证 punish", true},
		{"xianyu", "非法访问", true},
		{"xianyu", "二手 iPhone 3999 元", false},
	}
	for _, c := range cases {
		p, err := platform.Get(c.platform)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.DetectBlock(c.snapshot); got != c.want {
			t.Errorf("%s.DetectBlock(%q)=%v want %v", c.platform, c.snapshot[:min(20, len(c.snapshot))], got, c.want)
		}
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		platform string
		errText  string
		want     platform.ErrType
	}{
		{"xiaohongshu", "未登录或 web_session 过期", platform.ErrRefreshToken},
		{"xiaohongshu", "内容包含敏感词被拒绝", platform.ErrBadBody},
		{"xiaohongshu", "触发验证码 461", platform.ErrDisconnect},
		{"xiaohongshu", "IP存在风险 300012", platform.ErrDisconnect},
		{"xiaohongshu", "element_not_found: #x", platform.ErrRetry},
		{"xiaohongshu", "完全未知的错误", platform.ErrRetry}, // 默认归 retry
		{"douyin", "触发验证码 rmc.bytedance.com", platform.ErrDisconnect},
		{"douyin", "a_bogus 签名失效", platform.ErrDisconnect},
		{"xianyu", "cookie2 失效", platform.ErrRefreshToken},
	}
	for _, c := range cases {
		p, err := platform.Get(c.platform)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.ClassifyError(c.errText); got != c.want {
			t.Errorf("%s.ClassifyError(%q)=%s want %s", c.platform, c.errText, got, c.want)
		}
	}
}

func TestLocatorsKeyPresent(t *testing.T) {
	// blocked_marker 是 Brain 平台知识注入的必需键（brain_prompts.go 直读该键）
	for _, id := range []string{"xiaohongshu", "douyin", "xianyu"} {
		p, err := platform.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Locators()["blocked_marker"] == "" {
			t.Errorf("%s 缺 blocked_marker", id)
		}
	}
}

func TestCapabilitiesHonest(t *testing.T) {
	// 能力矩阵诚实性（铁律 1）：闲鱼/抖音不得声明 post_comment（写链路未收口）
	xys, _ := platform.Get("xianyu")
	for _, c := range xys.Capabilities() {
		if c == platform.CapPostComment {
			t.Error("xianyu 不得声明 post_comment")
		}
	}
	dy, _ := platform.Get("douyin")
	for _, c := range dy.Capabilities() {
		if c == platform.CapPostComment {
			t.Error("douyin 不得声明 post_comment（M3 前）")
		}
	}
	xhs, _ := platform.Get("xiaohongshu")
	if !platform.HasCapability(xhs, platform.CapPostComment) {
		t.Error("xiaohongshu 应声明 post_comment")
	}
	// CommentPoster 契约：声明了就必须实现（fails-loudly 由 CommentLocatorsFor 执行）
	if _, err := platform.CommentLocatorsFor(nil, "xiaohongshu"); err != nil {
		t.Errorf("xiaohongshu CommentLocatorsFor 应成功: %v", err)
	}
	if _, err := platform.CommentLocatorsFor(nil, "xianyu"); err == nil {
		t.Error("xianyu CommentLocatorsFor 应 fails-loudly 报错")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
