package dto

// step_action.go — 步骤动作白名单。
//
// 为什么需要一份「代码里的动作集合」：StepItem.Action 的 oneof 只在 gin 绑定 HTTP 请求时生效，
// 而 Brain 模式的 steps 是 LLM 输出 JSON 直接 json.Unmarshal 出来的，同一条 tag 在那条路上
// 一行都不跑。闸门要和 REST 判得一模一样，就不能再抄一份名单——抄的那份第一天就对、
// 第一次新增动作就错。所以白名单从 tag 本身读出来：一处声明，两处消费。
//
// 解析不出 oneof 时返回**空集**（什么都不放行），不是「全放行」：tag 被改名或删掉是
// 一次无声的降级，若默认放行，闸门会在没人看管的那天变成常开，比从来没有闸门更坏。

import (
	"reflect"
	"strings"
)

// KnownStepActions 合法动作集合，来源是 StepItem.Action 的 binding oneof。
var KnownStepActions = parseStepActionWhitelist(reflect.TypeOf(StepItem{}))

// IsKnownStepAction 动作名是否被服务端认识。精确匹配、不 trim 不折叠大小写——
// dispatchStep 的 switch 也是精确匹配字符串，两边判据必须一致，
// 否则「闸门放行了而执行器认不出」的失败步骤行又会落库。
func IsKnownStepAction(action string) bool { return KnownStepActions[action] }

func parseStepActionWhitelist(typ reflect.Type) map[string]bool {
	out := map[string]bool{}
	if typ.Kind() != reflect.Struct {
		return out
	}
	field, ok := typ.FieldByName("Action")
	if !ok || field.Type.Kind() != reflect.String {
		return out
	}
	value, ok := bindingOneof(field.Tag.Get("binding"))
	if !ok {
		return out
	}
	for _, action := range strings.Fields(value) {
		out[action] = true
	}
	return out
}

// bindingOneof 从 validator 的 binding 标签里取 oneof 的取值段（不含引号形态）。
// 只按逗号切段：本项目所有 oneof 都用空格分隔候选（含空格的取值需要 '…' 引号，
// 那种写法会在这里被截断——所以动作名一律不许带空格，StepItem 里三个 oneof 同口径）。
func bindingOneof(binding string) (string, bool) {
	for _, part := range strings.Split(binding, ",") {
		if strings.HasPrefix(part, "oneof=") {
			return strings.TrimPrefix(part, "oneof="), true
		}
	}
	return "", false
}
