package dto

// 契约锁：重试三件套的区间必须**两个入口同一份字面量**。
//
// 为什么行为用例（controller/retry_update_b20e_test.go 那 8 条腿）不够：它们只跑 PUT 一侧。
// 若有人哪天把 Create 的 min=30 改成 min=1（或反过来只改 PUT），PUT 的行为腿照样全绿——
// 「同一个值建任务合法、改任务非法」这种裂脑正是本批要消灭的东西的**下一个版本**，
// 所以这里直接比两处的 tag 字符串，谁动谁红。
//
// 手法沿用本包 b19h：走 reflect 读 tag，不读源码文本——把 tag 改名/删掉都会在这里红，
// 而 grep 源码文本的写法会跟着重构一起失效。

import (
	"reflect"
	"testing"
)

var b20eRetryFields = []string{"RetryOnFail", "RetryDelaySec", "MaxRetryTimes"}

// b20eTag 取某结构体字段的 binding tag；字段不存在返回 ok=false（两入口都得有，缺一即红）。
func b20eTag(typ reflect.Type, field string) (string, bool) {
	f, exists := typ.FieldByName(field)
	if !exists {
		return "", false
	}
	return f.Tag.Get("binding"), true
}

func TestB20ERetryFieldsIdenticalOnCreateAndUpdate(t *testing.T) {
	cre := reflect.TypeOf(CreateBrowserTaskReq{})
	upd := reflect.TypeOf(UpdateBrowserTaskReq{})
	for _, name := range b20eRetryFields {
		ct, cok := b20eTag(cre, name)
		ut, uok := b20eTag(upd, name)
		if !cok {
			t.Errorf("CreateBrowserTaskReq 缺字段 %s：建任务都配不了重试了", name)
			continue
		}
		if !uok {
			t.Errorf("UpdateBrowserTaskReq 缺字段 %s：本批的修复被整块撤掉，PUT 又开始吞这个键", name)
			continue
		}
		// PUT 一侧是指针，validator 的 omitempty 语义与非指针不同，所以不比整串，
		// 只比**区间数字**——那才是"哪个值合法"的实际口径。
		if got := b20eRanges(ct); !reflect.DeepEqual(got, b20eRanges(ut)) {
			t.Errorf("%s 两侧区间不一致：create=%q update=%q（同一份配置走两个入口得出两种答案）",
				name, ct, ut)
		}
	}
}

// b20eRanges 抽出 min=/max= 的数值对；无区间字段（bool）返回空集，两侧同为空即一致。
func b20eRanges(tag string) []string {
	out := []string{}
	for _, kv := range splitTag(tag) {
		if len(kv) > 4 && (kv[:4] == "min=" || kv[:4] == "max=") {
			out = append(out, kv)
		}
	}
	return out
}

func splitTag(tag string) []string {
	var out []string
	cur := ""
	for _, r := range tag {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
