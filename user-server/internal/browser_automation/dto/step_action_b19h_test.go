package dto

// 契约锁：Brain 链路的动作白名单必须与 REST 的 oneof 同源、且缺判据时 fail-closed。
//
// 为什么会有一条没人管的动作名通道：REST 建任务走 binding 校验，Action 上的
// `oneof=open_tab click …` 把不认识的动作挡在库里；Brain 模式却是 LLM 输出 JSON →
// executor `json.Unmarshal(stepsJSON, &[]dto.StepItem)` —— 这条路上 **binding 一行都不跑**
// （gin 只在 HTTP 入口做校验，反序列化本身不看 tag）。于是模型幻觉出来的
// `{"action":"hover"}` 会被当成合法步骤落库，再在 dispatchStep 的 default 分支炸成
// 「未知动作」：库里多一条永远失败的步骤行，用户侧看到一场莫名其妙的红。
// 修法只能有一个来源：白名单从同一个 tag 里读出来，两处共用一份事实，
// 否则「REST 认得但执行器不认」这种裂脑会随每次新增动作重新长出来。
//
// 反面形状也要锁：如果实现退化成「tag 里没有 oneof 就全部放行」，那把 tag 改名/删掉的
// 那一刻，闸门就静默变成常开——比没有闸门更坏，因为它看起来像有。所以解析不出 oneof
// 必须返回空集（什么都不放行），而不是返回 nil 让调用方各自理解。

import (
	"reflect"
	"testing"
)

// b19hActions 人手抄一份动作表：故意不从 tag 反推。
// 若实现与 tag 一起漂移（比如 tag 误删了 click），这里会单独红一次——只比 tag 的话，
// 两边一起错就永远绿。
var b19hActions = []string{
	"open_tab", "click", "type", "click_near", "post_comment",
	"snapshot", "markdown", "screenshot", "wait", "wait_for_selector",
	"scroll", "extract", "assert", "query", "close_tab",
}

func TestB19HKnownStepActionSet(t *testing.T) {
	if len(KnownStepActions) != len(b19hActions) {
		t.Errorf("白名单 %d 个动作，want %d：%v", len(KnownStepActions), len(b19hActions), KnownStepActions)
	}
	for _, a := range b19hActions {
		if !KnownStepActions[a] {
			t.Errorf("动作 %q 不在白名单里：REST 的 oneof 与 Brain 闸门就此分家", a)
		}
	}
	// 反向：白名单里不许多出表里没有的动作（多了=执行器没有对应 case 的纸面动作）
	for a := range KnownStepActions {
		found := false
		for _, want := range b19hActions {
			if want == a {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("白名单多出未登记动作 %q：oneof 里写了但动作表/执行器没有它", a)
		}
	}
}

func TestB19HIsKnownStepActionExactMatch(t *testing.T) {
	for _, a := range b19hActions {
		if !IsKnownStepAction(a) {
			t.Errorf("IsKnownStepAction(%q)=false，want true", a)
		}
	}
	// 每一个「差一点就一样」的形态都必须判否：闸门放行了而 dispatchStep 按精确字符串 switch，
	// 等于把「永远失败的步骤行」重新放回库里，只是换了个触发条件。
	for _, bad := range []string{
		"", " ", "click ", " click", "Click", "CLICK", "click\nrm -rf",
		"hover", "drop_pin", "click; await foo()", "screenshot ", "open_tab extra",
	} {
		if IsKnownStepAction(bad) {
			t.Errorf("IsKnownStepAction(%q)=true，want false：dispatchStep 的 switch 认不了这个字符串", bad)
		}
	}
}

// b19hSynthetic 三种 tag 形状，验证「解析不出 oneof 就什么都不放行」。
type b19hSynthetic struct {
	Action string `binding:"required,oneof=alpha beta"`
}

type b19hNoOneof struct {
	Action string `binding:"required"`
}

type b19hEmptyOneof struct {
	Action string `binding:"required,oneof="`
}

type b19hNoActionField struct {
	Kind string `binding:"required,oneof=alpha"`
}

func TestB19HWhitelistParserFailClosed(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want int
	}{
		{"正常 oneof", reflect.TypeOf(b19hSynthetic{}), 2},
		{"tag 里根本没有 oneof", reflect.TypeOf(b19hNoOneof{}), 0},
		{"oneof 是空的", reflect.TypeOf(b19hEmptyOneof{}), 0},
		{"没有 Action 字段", reflect.TypeOf(b19hNoActionField{}), 0},
		{"传进来的根本不是 struct", reflect.TypeOf(""), 0},
	}
	for _, c := range cases {
		got := parseStepActionWhitelist(c.typ)
		if len(got) != c.want {
			t.Errorf("%s：解析出 %d 个动作，want %d（%v）——解析不出时必须返回空集，"+
				"不能默认放行，否则 tag 一丢闸门就静默常开", c.name, len(got), c.want, got)
		}
	}
	if m := parseStepActionWhitelist(reflect.TypeOf(b19hSynthetic{})); !m["alpha"] || !m["beta"] {
		t.Errorf("oneof=alpha beta 解析错：%v", m)
	}
}
