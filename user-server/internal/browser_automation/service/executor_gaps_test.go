package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"hivemtk-user/internal/browser_automation/dto"
	"hivemtk-user/internal/browser_automation/platform"

	// 适配器注册（生产经 router 空白导入；service 测试自持）
	_ "hivemtk-user/internal/browser_automation/platform/douyin"
	_ "hivemtk-user/internal/browser_automation/platform/xianyu"
	_ "hivemtk-user/internal/browser_automation/platform/xiaohongshu"
)

// D5 补测（主文档 §5.2 G8）：executor 本轮新增纯函数——
// F2① 写原语禁重试 / F7 错误分类分线 / F6a 页面变化证据 / D2 attr 参数贯通。

func TestIsWriteAction(t *testing.T) {
	if !isWriteAction("post_comment") {
		t.Error("post_comment 必须是写原语（禁重试）")
	}
	for _, a := range []string{"click", "type", "extract", "query", "open_tab"} {
		if isWriteAction(a) {
			t.Errorf("%s 不应是写原语", a)
		}
	}
}

func TestStepErrRetryable(t *testing.T) {
	// xiaohongshu：敏感词=bad_body 不重试；验证码=disconnect 不重试；未知=默认可重试
	if stepErrRetryable("xiaohongshu", "内容包含敏感词") {
		t.Error("bad_body 应不重试")
	}
	if stepErrRetryable("xiaohongshu", "触发验证码 461") {
		t.Error("disconnect 应不重试")
	}
	if !stepErrRetryable("xiaohongshu", "Host 命令超时") {
		t.Error("retry 类应可重试")
	}
	if !stepErrRetryable("nonexistent_platform", "任何错误") {
		t.Error("平台未注册时保守可重试")
	}
}

func TestStepChangedPage(t *testing.T) {
	if !stepChangedPage("open_tab", nil) {
		t.Error("open_tab 必然换页")
	}
	nav, _ := json.Marshal(map[string]any{"navigated": true})
	if !stepChangedPage("click", nav) {
		t.Error("click navigated=true 应换页")
	}
	notNav, _ := json.Marshal(map[string]any{"navigated": false})
	if stepChangedPage("click", notNav) {
		t.Error("click navigated=false 不应换页")
	}
	if stepChangedPage("type", nil) {
		t.Error("type 无证据不换页")
	}
	if stepChangedPage("click", json.RawMessage(`{"result":{}}`)) {
		t.Error("无 navigated 字段不换页")
	}
}

func TestBuildStepParamsAttribute(t *testing.T) {
	// D2：dto.Attribute 必须经 buildStepParams 进入落库参数与下发命令
	step := parsedStep{StepItem: dto.StepItem{
		Action: "query", QueryKind: "attr", Attribute: "href", Target: "a.item",
	}}
	p := buildStepParams(step)
	if p.Attribute != "href" {
		t.Fatalf("StepParams.Attribute 未贯通: %+v", p)
	}
	blob, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(blob, &m); err != nil {
		t.Fatal(err)
	}
	if m["attribute"] != "href" {
		t.Errorf("params JSON 缺 attribute 键: %s", blob)
	}
}

func TestClampKeepsWriteActionSafe(t *testing.T) {
	// P0-4 钳位与 F2① 的组合：即便 clamp 后 retry_count=3，写原语在执行层也被强制 0——
	// 这里验证钳位本身行为不回退
	r, b := clampBrainStepParams(100, 50)
	if r != 3 || b != 50 { // retry 上限 3；backoff 50 在区间内保持不变（<=0 才兜 1000）
		t.Errorf("clamp 边界异常: %d %d", r, b)
	}
	r2, b2 := clampBrainStepParams(-5, 999999)
	if r2 != 0 || b2 != 10000 {
		t.Errorf("clamp 下界/上限异常: %d %d", r2, b2)
	}
}

// F6（G15 余项）历史压缩：滑窗溢出折叠入账，构成信息不静默丢失
func TestAppendHistoryBoundedFolds(t *testing.T) {
	folded := map[string]int{}
	var h []string
	for i := 0; i < historyWindow+5; i++ {
		h = appendHistoryBounded(h, "click #btn", folded)
	}
	if len(h) != historyWindow {
		t.Errorf("窗口容量必须恒定 %d，got %d", historyWindow, len(h))
	}
	if folded["click"] != 5 {
		t.Errorf("溢出 5 条应全部折叠计数: %v", folded)
	}
	hdr := buildFoldHeader(folded)
	if !strings.HasPrefix(hdr, "[已折叠 5 步: click×5]") {
		t.Errorf("台账首行格式异常: %s", hdr)
	}
	if buildFoldHeader(map[string]int{}) != "" {
		t.Error("空台账应返回空串（不注入噪声行）")
	}
	// 溢出折叠的是**最旧条**（滑窗语义）：首轮 push historyWindow+5 条，折叠 5 条
	if folded["click"] != 5 {
		t.Errorf("滑窗应折叠最旧条（click×5）: %v", folded)
	}
	// 最旧条是折叠行（"[" 前缀）时再溢出 → 计 compacted，不误归任何动作
	h2 := append([]string{"[已折叠 1 步: x×1]"}, h...)
	_ = appendHistoryBounded(h2, "query body", folded)
	if folded["compacted"] != 1 {
		t.Errorf("折叠行被挤出应计 compacted: %v", folded)
	}
	// folded=nil（judge 摘要只读路径）不 panic
	_ = appendHistoryBounded(h, "query body", nil)
}

// R-A3（2026-09-19）fail-closed：默认分类由 retry 翻转为 unknown——不重试；
// 可重试集合显式枚举（timeout/超时/未就绪/element_not_found）。
func TestPlatformDefaultErrTypeIsUnknown(t *testing.T) {
	for _, id := range []string{"xiaohongshu", "douyin", "xianyu"} {
		p, err := platform.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.ClassifyError("完全没见过的字符串") != platform.ErrUnknown {
			t.Errorf("%s 默认分类应为 unknown（fail-closed）", id)
		}
		if p.ClassifyError("Host 命令超时: cdp 未回包") != platform.ErrRetry {
			t.Errorf("%s 中文「超时」必须留在显式可重试表（Host 命令超时是最高频瞬态错误）", id)
		}
		if p.ClassifyError("timeout waiting for element") != platform.ErrRetry {
			t.Errorf("%s timeout 应可重试", id)
		}
	}
}

func TestStepErrRetryableFailClosed(t *testing.T) {
	if stepErrRetryable("xiaohongshu", "完全没见过的字符串") {
		t.Error("ErrUnknown 应不重试（R-A3 fail-closed）")
	}
	if !stepErrRetryable("xiaohongshu", "element_not_found: #x") {
		t.Error("显式 retry 类应可重试")
	}
}

func TestRecordResultPayloadExplicitError(t *testing.T) {
	// R-A4：回包经返回值传递；marshal 失败显式抛错而非静默吞
	blob, err := recordResultPayload(nil)
	if err != nil || blob != nil {
		t.Errorf("空 payload 应回 (nil, nil)，得 (%v, %v)", blob, err)
	}
	blob, err = recordResultPayload(map[string]any{"k": "v"})
	if err != nil || string(blob) != `{"k":"v"}` {
		t.Errorf("正常 payload 应回序列化结果，得 %s, %v", blob, err)
	}
	if _, err := recordResultPayload(map[string]any{"bad": make(chan int)}); err == nil {
		t.Error("不可序列化 payload 必须显式抛错（旧实现吞错致 result 静默丢失）")
	}
}

func TestExecutorCarriesNoPerSessionResultField(t *testing.T) {
	// R-A4 结构防回归：Executor（进程级单例）不得再挂 []byte 类型的逐步回包字段。
	ty := reflect.TypeOf(Executor{})
	byteSlice := reflect.TypeOf([]byte(nil))
	for i := 0; i < ty.NumField(); i++ {
		if f := ty.Field(i); f.Type == byteSlice {
			t.Errorf("Executor 不应持有 []byte 字段（曾为 lastStepResult 数据竞态源），发现: %s", f.Name)
		}
	}
}
