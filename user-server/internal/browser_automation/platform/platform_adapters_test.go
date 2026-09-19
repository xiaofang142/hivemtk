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

// TestDetectBlock A2 结构化判据（批2 重写）：
// 旧实现全文 Contains——评论正文含「验证码」必误报；新契约 URL 层只比对 pageURL、
// 文案层只比对快照非 text 角色结构行。验收用例即 spec 表：正文含验证码不误报、真拦截必报。
func TestDetectBlock(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		url      string
		snapshot string
		want     bool
	}{
		{"xhs URL 重定向拦截", "xiaohongshu", "https://www.xiaohongshu.com/website-login/error?error_code=300012", `text "笔记正文" @e1`, true},
		{"xhs 结构行拦截文案", "xiaohongshu", "https://www.xiaohongshu.com/explore/abc", "h1 \"当前环境异常\" @e2", true},
		{"xhs 评论正文含验证码不误报", "xiaohongshu", "https://www.xiaohongshu.com/explore/abc", "text \"亲，收好这份验证码获取教程\" @e7\ntext \"IP存在风险 是什么梗\" @e8", false},
		{"xhs 正常页面", "xiaohongshu", "https://www.xiaohongshu.com/search_result/abc", "button \"登录\" @e1\ntext \"验证码登录\" @e2", false},
		{"xhs 快照无 URL 时结构层仍兜底", "xiaohongshu", "", "button \"当前环境异常，完成验证后即可继续访问\" @e3", true},
		{"douyin 跳转验证域", "douyin", "https://rmc.bytedance.com/verify/web_page/Hbz3CEKM", `text "安全验证" @e1`, true},
		{"douyin 正文含安全验证字样", "douyin", "https://www.douyin.com/video/7", "text \"如何绕过安全验证？聊聊心得\" @e9", false},
		{"douyin 正常页面", "douyin", "https://www.douyin.com/video/7", "button \"点赞\" @e2", false},
		{"xianyu punish URL", "xianyu", "https://www.goofish.com/_____tmd_____/punish?x5secdata=xx", "text \"illegal\" @e1", true},
		{"xianyu 弹层文案进按钮行", "xianyu", "https://www.goofish.com/item?id=1", "button \"非法访问，请滑动验证\" @e4", true},
		{"xianyu 评论聊滑块不误报", "xianyu", "https://www.goofish.com/item?id=1", "text \"这个手指滑块摆件很精致\" @e6", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := platform.Get(c.platform)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.DetectBlock(c.url, c.snapshot); got != c.want {
				t.Errorf("DetectBlock(%q,%q)=%v want %v", c.url, c.snapshot, got, c.want)
			}
		})
	}
}

// TestBlockSelectorsOnlyXianyu A2 容器层声明面：只有具备公开实证容器形态（baxia）的平台
// 才登记 BlockSelectors——防「纯猜选择器」复发（B8 教训）。
func TestBlockSelectorsOnlyXianyu(t *testing.T) {
	xys, _ := platform.Get("xianyu")
	bs, ok := xys.(platform.BlockSelectorDeclarer)
	if !ok || len(bs.BlockSelectors()) == 0 {
		t.Error("xianyu 应声明 baxia 容器选择器")
	}
	for _, id := range []string{"xiaohongshu", "douyin"} {
		p, _ := platform.Get(id)
		if bs, ok := p.(platform.BlockSelectorDeclarer); ok && len(bs.BlockSelectors()) > 0 {
			t.Errorf("%s 无实证容器形态却声明了 BlockSelectors（禁止猜选择器）", id)
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
		// A3（2026-09-19）fail-closed：判据未命中默认 ErrUnknown（契约默认失败，不盲目重试）；
		// 可重试集合显式枚举——中文「超时」必须在显式表内（Host 命令超时 是最高频瞬态错误）。
		{"xiaohongshu", "完全未知的错误", platform.ErrUnknown},
		{"xiaohongshu", "Host 命令超时: cdp 未回包", platform.ErrRetry},
		{"xiaohongshu", "timeout waiting for selector", platform.ErrRetry},
		{"douyin", "Host 命令超时", platform.ErrRetry},
		{"douyin", "无法归因的新错误", platform.ErrUnknown},
		{"xianyu", "Host 命令超时", platform.ErrRetry},
		{"xianyu", "未知异常", platform.ErrUnknown},
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

// TestBlockKnowledgePresent A1/A2：拦截知识独立于定位表（旧 blocked_marker 混在
// Locators 里的形态已废弃），三平台必须各自声明文案与 URL 判据。
func TestBlockKnowledgePresent(t *testing.T) {
	for _, id := range []string{"xiaohongshu", "douyin", "xianyu"} {
		p, err := platform.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.BlockMarkers()) == 0 {
			t.Errorf("%s 缺 BlockMarkers", id)
		}
		if len(p.BlockURLPatterns()) == 0 {
			t.Errorf("%s 缺 BlockURLPatterns", id)
		}
		if _, dup := p.Locators()["blocked_marker"]; dup {
			t.Errorf("%s Locators 仍混装 blocked_marker（应移出）", id)
		}
	}
}

// TestCommentLocatorsSingleSource A1：CommentLocators 四元组必须逐字派生自 Locators 表
// （旧实现同串 CSS 两处字面量重复——改版时只改一处即静默漂移）。
func TestCommentLocatorsSingleSource(t *testing.T) {
	xhs, _ := platform.Get("xiaohongshu")
	l := xhs.Locators()
	cl, err := platform.CommentLocatorsFor(nil, "xiaohongshu")
	if err != nil {
		t.Fatal(err)
	}
	if cl.InputSelector != l["comment_input"] || cl.CommentContainer != l["comment_list"] ||
		cl.CommentItemText != l["comment_item"] || cl.SendButtonText != l["send_button_text"] {
		t.Errorf("CommentLocators 与 Locators 表脱钩: %+v vs %v", cl, l)
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

// TestCapabilitiesWithinVocabulary A5（2026-09-19 调研定论）：读能力（search/open_detail/
// read_comment）走 DOM 原语 + 真机校准选择器，零签名 API 依赖（a_bogus/mtop 只出现在
// 注释与错误分类里）——「过度声明」指控不成立，声明保持不动。本测试锁住真实风险面：
// 执行器动作表没有发帖原语，CapPost 无处落地；拼错的能力位会原样流入 brain prompt。
func TestCapabilitiesWithinVocabulary(t *testing.T) {
	known := map[platform.Capability]bool{
		platform.CapSearch:      true,
		platform.CapOpenDetail:  true,
		platform.CapPostComment: true,
		platform.CapReadComment: true,
	}
	for _, p := range platform.List() {
		for _, c := range p.Capabilities() {
			if !known[c] {
				t.Errorf("%s 声明了白名单外能力 %q（执行器无对应动作支撑）", p.Identifier(), c)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
