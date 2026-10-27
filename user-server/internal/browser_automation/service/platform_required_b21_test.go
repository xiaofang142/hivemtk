package service

// platform_required_b21_test.go —— B21：平台必填（fail-loudly）。
//
// 被锁的行为：一个忘了填 platform 的任务**不得**静默按小红书执行。
// 旧形态是 fail-open 的三层叠加：
//  1. model 列默认 'xiaohongshu'（GORM 零值省略 → DB 默认生效，任务凭空变成小红书任务）；
//  2. service.Create 把空值补成 "xiaohongshu"；
//  3. executor_helpers.taskPlatformID 空值回落 "xiaohongshu"。
//
// 后果不是「少了个字段」，而是跨平台写操作落在错误的定位表上：抖音页面上点小红书
// 的 `p.content-input`、用小红书按钮文案找「发送」，最终归因成「定位失效」，
// 「平台选错了」这件事在证据面里彻底消失——与 P4「未声明能力即报错」相反。
//
// 现在：入口（Create/Update/Publish/RunTask）一律报错，执行面 platform.Get("") 也报错，
// 列默认改成 ''（不再自带渠道字面量）。

import (
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
)

// ① 执行面：空平台不得被兜底成任何平台。
func TestTaskPlatformIDIsNotBackfilled(t *testing.T) {
	if got := taskPlatformID(&model.BrowserTask{}); got != "" {
		t.Fatalf("空 platform 被兜底成 %q——这正是 fail-open 的形状", got)
	}
}

// ② 入口门：空 platform 必须红，且文案点明「不再缺省」并给出已注册平台清单
// （只说「必填」会让人去翻文档；换了个地方静默仍然静默）。
func TestValidateTaskPlatformRejectsEmpty(t *testing.T) {
	err := validateTaskPlatform(&model.BrowserTask{})
	if err == nil {
		t.Fatal("空 platform 应被拒绝")
	}
	msg := err.Error()
	if !strings.Contains(msg, "platform 必填") {
		t.Errorf("错误文案应点名 platform 必填: %s", msg)
	}
	if !strings.Contains(msg, "不再缺省") {
		t.Errorf("错误文案应说明不再缺省 xiaohongshu: %s", msg)
	}
	if !strings.Contains(msg, "xiaohongshu") || !strings.Contains(msg, "douyin") {
		t.Errorf("错误文案应列出已注册平台名，实际: %s", msg)
	}
}

// ③ 未注册平台仍然红（这条是既有契约，防 B21 改坏注册表入口）
func TestValidateTaskPlatformRejectsUnregistered(t *testing.T) {
	if err := validateTaskPlatform(&model.BrowserTask{Platform: "not_registered_platform_zz"}); err == nil {
		t.Fatal("未注册平台应被拒绝")
	}
}

// ④ 合法平台通过，并被去空白（落库值可与注册表 identifier 逐字相等）
func TestValidateTaskPlatformTrimsRegistered(t *testing.T) {
	task := &model.BrowserTask{Platform: "  xiaohongshu  "}
	if err := validateTaskPlatform(task); err != nil {
		t.Fatalf("已注册平台不应被拒: %v", err)
	}
	if task.Platform != "xiaohongshu" {
		t.Errorf("platform 应去空白，got %q", task.Platform)
	}
}

// ⑤ 执行前的门（Publish/RunTask 用 requireTaskPlatform）只堵空值，不查注册：
// 历史行/工具造的行里可能有占位平台名，注册校验由执行面 platform.Get 负责——
// 两层的分工不能混，否则「补一个空值洞」会把一批历史行变成不可执行。
func TestRequireTaskPlatformOnlyChecksEmptiness(t *testing.T) {
	if err := requireTaskPlatform(&model.BrowserTask{}); err == nil {
		t.Fatal("空 platform 应被拒绝")
	}
	if err := requireTaskPlatform(&model.BrowserTask{Platform: "fixture"}); err != nil {
		t.Errorf("占位平台名不该在这一层被拒（注册校验属执行面）: %v", err)
	}
}
