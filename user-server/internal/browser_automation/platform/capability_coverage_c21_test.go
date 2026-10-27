// C21：能力声明必须有执行面。
//
// 背景（一次性修完的三件事之一）：Capabilities() 此前只被 /platforms 接口和 Brain prompt
// 读，**执行面一处都不看**。于是「平台声明了 read_comment 却没声明 post_comment」这件事
// 要到执行期才以「comment_verify 报错」的形式暴露；而「平台声明了 post_comment 却没实现
// CommentPoster」这类矛盾，也只能靠人读代码发现。声明与执行面之间没有连接，P4（未声明
// 能力即报错）就成了纯文档。
//
// 本文件锁三件事：
//  1. 覆盖不变量：每个 Capability 要么被 RequiredCapability 消费（执行面拦），要么在
//     AdvisoryCapabilities 里写明为什么只服务 Brain。**新增能力而不接执行面 = 测试红**。
//  2. 声明自洽：声明了 post_comment 的适配器必须能给出四元组齐全的 CommentLocators；
//     没声明的平台不许「悄悄能发」（CommentLocatorsFor 必须报错）。
//  3. 反例：四元组缺 comment_item_text 时 CommentLocatorsFor 必须报错（不许静默降级成
//     「页面上有没有这句话」——那正是 session644 的假绿入口）。
package platform_test

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/platform"
)

// 能力值域里每个值都必须有明确的执行面归属（消费 or 显式免责）。
func TestCapabilityCoverageInvariant(t *testing.T) {
	advisory := platform.AdvisoryCapabilities()
	consumed := map[platform.Capability]bool{}
	for _, c := range platform.RequiredCapabilities() {
		consumed[c] = true
	}
	for _, c := range platform.AllCapabilities() {
		_, excused := advisory[c]
		switch {
		case consumed[c] && excused:
			t.Errorf("能力 %q 同时在「执行面拦截」与「免责清单」里：两处口径互相抵消，删一处", c)
		case !consumed[c] && !excused:
			t.Errorf("能力 %q 既没被 RequiredCapability 消费、也不在 AdvisoryCapabilities 里：\n"+
				"  声明了却没人读=声明的执行面又断了一处（C21）。要么接进执行面，要么在\n"+
				"  AdvisoryCapabilities 写明「只服务 Brain/UI、为什么没有可拦的执行点」", c)
		case excused && strings.TrimSpace(advisory[c]) == "":
			t.Errorf("能力 %q 在免责清单里但没写理由：无条件免责等于没接执行面", c)
		}
	}
}

// 原语→能力映射的正面表：唯一强制项是 post_comment。通用原语绝不能出现映射
// （出现即意味着基座开始按平台拆分支，与「基座零平台知识」相反）。
func TestRequiredCapabilityOnlyMapsPostComment(t *testing.T) {
	cap, ok := platform.RequiredCapability("post_comment")
	if !ok || cap != platform.CapPostComment {
		t.Fatalf("post_comment 必须映射到 CapPostComment，实际 (%q,%v)", cap, ok)
	}
	if got := platform.RequiredCapabilities(); len(got) != 1 || got[0] != platform.CapPostComment {
		t.Fatalf("被消费的能力集合=%v want [post_comment]：多一个都必须先想清楚「拦它的执行点在哪」", got)
	}
	for _, action := range []string{
		"open_tab", "snapshot", "markdown", "click", "click_near", "type", "scroll",
		"wait", "query", "assert", "extract", "navigate", "close_tab", "screenshot",
		// 发评论的三段子命令：由 post_comment 这一个原语统一拦（拦在编排层，
		// 别在子命令层再拦一次——那会让「谁负责拦」有两处答案）。
		"comment_prep", "comment_send", "comment_verify",
	} {
		if c, need := platform.RequiredCapability(action); need {
			t.Errorf("通用原语 %q 被映射到能力 %q：基座不应对通用动作做平台分支", action, c)
		}
	}
}

// 声明了 post_comment 的适配器必须交出齐全四元组；没声明的平台不许「悄悄能发」。
func TestDeclaredPostCommentHasCompleteLocators(t *testing.T) {
	for _, id := range []string{"xiaohongshu", "douyin", "xianyu"} {
		p, err := platform.Get(id)
		if err != nil {
			t.Fatalf("平台 %s 未注册: %v", id, err)
		}
		declared := platform.HasCapability(p, platform.CapPostComment)
		locs, locErr := platform.CommentLocatorsFor(context.Background(), id)
		switch {
		case declared && locErr != nil:
			t.Errorf("%s 声明了 post_comment 却取不到选择器：%v", id, locErr)
		case declared:
			if missing := locs.Missing(); len(missing) > 0 {
				t.Errorf("%s 选择器不全，缺 %v", id, missing)
			}
		case locErr == nil:
			t.Errorf("%s 未声明 post_comment 却拿到了发评论选择器：未声明能力必须 fails-loudly", id)
		}
	}
}

// Missing() 逐项可数：缺哪个报哪个，顺序稳定（错误文案要能被人一眼读懂）。
func TestCommentLocatorsMissingNamesEveryField(t *testing.T) {
	var zero platform.CommentLocators
	want := []string{"input_selector", "send_button_text", "comment_container", "comment_item_text"}
	got := zero.Missing()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Missing()=%v want %v", got, want)
	}
	full := platform.CommentLocators{
		InputSelector: "#i", SendButtonText: "发送", CommentContainer: "#c", CommentItemText: "#it",
	}
	if m := full.Missing(); len(m) != 0 {
		t.Fatalf("四元组齐全仍报缺 %v", m)
	}
}

// 缺 comment_item_text 必须报错：它是新旧判据的载体。少了它，回查只能回答
// 「页面上有没有这句话」——同账号对同一条笔记重复发同一句话时，提交前就已经是 true，
// 于是任何一次「点没点中都不知道」的提交都能拿到 verified（session644 真机假绿）。
func TestMissingItemSelectorFailsLoudly(t *testing.T) {
	p := &c21Poster{base: c21Platform{id: "c21-fake-missing-item", caps: []platform.Capability{platform.CapPostComment}},
		locs: platform.CommentLocators{
			InputSelector:    "textarea",
			SendButtonText:   "发送",
			CommentContainer: ".comments",
		}}
	platform.Register(p)
	defer platform.Deregister(p.Identifier())
	_, err := platform.CommentLocatorsFor(context.Background(), p.Identifier())
	if err == nil {
		t.Fatal("四元组缺 comment_item_text 时 CommentLocatorsFor 必须报错（不许静默降级）")
	}
	if !strings.Contains(err.Error(), "comment_item_text") {
		t.Errorf("错误文案必须点名缺哪一项，实际: %v", err)
	}
}

// 声明了 post_comment 却没实现 CommentPoster：同样 fails-loudly（不是「没选择器就跳过」）。
func TestDeclaredWithoutPosterFailsLoudly(t *testing.T) {
	p := &c21Platform{id: "c21-fake-no-poster", caps: []platform.Capability{platform.CapPostComment}}
	platform.Register(p)
	defer platform.Deregister(p.Identifier())
	if _, err := platform.CommentLocatorsFor(context.Background(), p.Identifier()); err == nil {
		t.Fatal("声明 post_comment 但未实现 CommentPoster 必须报错")
	}
}

// c21Platform 最小适配器：只驱动注册表/四元组校验，不实现 CommentPoster（不参与真实执行）。
type c21Platform struct {
	id   string
	caps []platform.Capability
}

func (p *c21Platform) Identifier() string                    { return p.id }
func (p *c21Platform) Capabilities() []platform.Capability   { return p.caps }
func (p *c21Platform) MaxConcurrentJobs() int                { return 1 }
func (p *c21Platform) Locators() map[string]string           { return map[string]string{} }
func (p *c21Platform) DetectBlock(string, string) bool       { return false }
func (p *c21Platform) BlockMarkers() []string                { return nil }
func (p *c21Platform) BlockURLPatterns() []string            { return nil }
func (p *c21Platform) ClassifyError(string) platform.ErrType { return platform.ErrUnknown }

// c21Poster 在最小适配器上补 CommentPoster（Go 的接口判定看方法集，
// 「实现/不实现」只能靠两个类型分开表达）。
type c21Poster struct {
	base c21Platform
	locs platform.CommentLocators
}

func (p *c21Poster) Identifier() string                    { return p.base.id }
func (p *c21Poster) Capabilities() []platform.Capability   { return p.base.caps }
func (p *c21Poster) MaxConcurrentJobs() int                { return 1 }
func (p *c21Poster) Locators() map[string]string           { return map[string]string{} }
func (p *c21Poster) DetectBlock(string, string) bool       { return false }
func (p *c21Poster) BlockMarkers() []string                { return nil }
func (p *c21Poster) BlockURLPatterns() []string            { return nil }
func (p *c21Poster) ClassifyError(string) platform.ErrType { return platform.ErrUnknown }
func (p *c21Poster) CommentLocators() platform.CommentLocators {
	return p.locs
}
