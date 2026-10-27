package service

// task_capability_c21_test.go —— C21：能力声明必须有执行面（编排校验这一层）。
//
// 被锁的行为：平台没声明 post_comment 时，**创建/编辑/发布**就必须红，而不是让人一路
// 跑到执行期收到一句 comment_verify 报错。旧形态里 Capabilities() 只进 /platforms 接口
// 与 Brain prompt，执行面一处不读——「声明了能力」与「执行面按能力拦」之间没有连接，
// P4（未声明能力即报错）等于纯文档。
//
// 现形态：platform.RequiredCapability 是唯一的原语→能力强制表（只映射 post_comment），
// validateStepsCapabilities 在 Create/Update/Publish 三处把它落到编排上。
//
// 判据要点（都是真机踩过的形状）：
//   - 拦在**声明**层，不拦在实现层：douyin 未声明 post_comment（发评论列 M3），
//     拿一条带 post_comment 的编排去建任务必须当场被拒；
//   - 文案要点名「第几步 / 什么原语 / 要什么能力 / 平台已声明什么」——只说「不支持」
//     会让人去翻代码；
//   - Brain 模式放行（编排运行期才生成，靠执行面同一张表 + CommentLocatorsFor 拦）；
//   - 拿不到适配器不当场红（注册校验是 Create/Update 的职责，历史行/工具造行不能
//     在这里被第二次判死）。

import (
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"

	"gorm.io/datatypes"
)

const c21StepsWithComment = `[{"action":"open_tab","target":"https://www.douyin.com/x"},
{"action":"snapshot"},
{"action":"post_comment","value":"给作者大大的点赞"}]`

// ① 未声明 post_comment 的平台 + 编排里有 post_comment → 必须被拒，且文案可归因。
func TestValidateStepsCapabilitiesRejectsUndeclaredComment(t *testing.T) {
	err := validateStepsCapabilities(&model.BrowserTask{
		Platform: "douyin",
		Steps:    datatypes.JSON(c21StepsWithComment),
	})
	if err == nil {
		t.Fatal("douyin 未声明 post_comment，带 post_comment 的编排必须被拒")
	}
	msg := err.Error()
	for _, want := range []string{"第 3 步", "post_comment", "能力", "douyin", "已声明"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误文案缺 %q（人要靠它定位到哪一步/哪一项能力）: %s", want, msg)
		}
	}
	// 序号 1 起：编排面板与执行日志都按「第几步」读
	if strings.Contains(msg, "第 0 步") {
		t.Errorf("步序号应为 1 起（0 起会与面板对不上号）: %s", msg)
	}
}

// ② 声明了 post_comment 的平台（小红书）走同一条编排 → 放行。
func TestValidateStepsCapabilitiesAcceptsDeclaredComment(t *testing.T) {
	if err := validateStepsCapabilities(&model.BrowserTask{
		Platform: "xiaohongshu",
		Steps:    datatypes.JSON(c21StepsWithComment),
	}); err != nil {
		t.Fatalf("xiaohongshu 已声明 post_comment，不该被拒: %v", err)
	}
}

// ③ 通用原语不涉及平台能力：任何已注册平台都放行（基座不得对通用动作做平台分支）。
func TestValidateStepsCapabilitiesAllowsGenericStepsOnAnyPlatform(t *testing.T) {
	steps := datatypes.JSON(`[{"action":"open_tab","target":"https://www.douyin.com/x"},
{"action":"snapshot"},{"action":"click","target":"#q"},{"action":"type","target":"#q","value":"x"},
{"action":"query","query_kind":"text","target":"#t"},{"action":"close_tab"}]`)
	for _, p := range []string{"douyin", "xianyu", "xiaohongshu"} {
		if err := validateStepsCapabilities(&model.BrowserTask{Platform: p, Steps: steps}); err != nil {
			t.Errorf("%s 上跑纯通用编排不该被拒: %v", p, err)
		}
	}
}

// ④ Brain 模式放行：编排运行期生成，拦在这里会变成「Brain 任务一律建不出来」。
func TestValidateStepsCapabilitiesSkipsBrainMode(t *testing.T) {
	if err := validateStepsCapabilities(&model.BrowserTask{
		Platform:  "douyin",
		BrainMode: true,
		Steps:     datatypes.JSON(c21StepsWithComment),
	}); err != nil {
		t.Fatalf("Brain 模式的 steps 是运行期产物，不该在创建期按平台能力拦: %v", err)
	}
}

// ⑤ 空编排放行（只建任务的草稿态）；注册校验不归这层管（见 b21 的 ⑤）。
func TestValidateStepsCapabilitiesSkipsEmptyAndUnregistered(t *testing.T) {
	if err := validateStepsCapabilities(&model.BrowserTask{Platform: "xiaohongshu"}); err != nil {
		t.Errorf("空编排不该被拒: %v", err)
	}
	if err := validateStepsCapabilities(&model.BrowserTask{
		Platform: "fixture_placeholder",
		Steps:    datatypes.JSON(c21StepsWithComment),
	}); err != nil {
		t.Errorf("未注册平台名不在这层判死（注册校验属 Create/Update，执行面 platform.Get 会报）: %v", err)
	}
}

// ⑥ 坏 JSON 照样红：能力校验不能把「编排读不出来」当成「没问题」。
func TestValidateStepsCapabilitiesPropagatesParseError(t *testing.T) {
	if err := validateStepsCapabilities(&model.BrowserTask{
		Platform: "xiaohongshu",
		Steps:    datatypes.JSON(`[{"action":`),
	}); err == nil {
		t.Fatal("steps JSON 坏了必须报错，不能默默放行")
	}
}
