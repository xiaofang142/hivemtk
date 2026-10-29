package egolite

// params_schema_test.go —— S-11 参数契约（纯函数用例，不依赖 PG）。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateEgoParams_PassCases(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		params string
	}{
		{"无 schema 即无契约", ``, `{"anything":1}`},
		{"schema 为 null", `null`, `{"anything":1}`},
		{"参数可省略（required 为空）", `{"type":"object"}`, ``},
		{"空 schema 对象放行", `{}`, `{"k":"v"}`},
		{"全字段合规", `{
			"type":"object",
			"required":["keyword"],
			"properties":{
				"keyword":{"type":"string","minLength":1,"maxLength":10},
				"limit":{"type":"integer","minimum":1,"maximum":20},
				"mode":{"type":"string","enum":["fast","deep"]},
				"tags":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":3}
			}
		}`, `{"keyword":"蜂巢","limit":5,"mode":"deep","tags":["a"]}`},
		{"未知关键字忽略（不因声明了未支持的能力就拒跑）",
			`{"type":"object","properties":{"k":{"type":"string","format":"uri","title":"K"}}}`, `{"k":"x"}`},
		{"枚举里 1 与 1.0 等价", `{"type":"object","properties":{"n":{"enum":[1,2]}}}`, `{"n":1.0}`},
		{"pattern 命中", `{"type":"object","properties":{"code":{"type":"string","pattern":"^[A-Z]{2}[0-9]+$"}}}`, `{"code":"AB12"}`},
		{"显式 null 不判类型（交给脚本）", `{"type":"object","properties":{"k":{"type":"string"}}}`, `{"k":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateEgoParams([]byte(c.schema), []byte(c.params)); err != nil {
				t.Fatalf("应当放行，却报错: %v", err)
			}
		})
	}
}

func TestValidateEgoParams_RejectCases(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		params string
		want   string
	}{
		{"schema 不是合法 JSON", `{`, `{}`, "params_schema 不是合法 JSON"},
		{"schema.type 非 object", `{"type":"string"}`, `{}`, "只支持 object"},
		{"properties 形状错", `{"properties":[]}`, `{}`, "properties 必须是对象"},
		{"required 形状错", `{"required":"keyword"}`, `{}`, "required 必须是字符串数组"},
		{"缺必填参数", `{"required":["keyword"],"properties":{"keyword":{"type":"string"}}}`, `{}`, "缺少必填参数 params.keyword"},
		{"参数不是对象（数组）", `{"type":"object"}`, `[1,2]`, "params 必须是 JSON 对象"},
		{"参数不是合法 JSON", `{"type":"object"}`, `{"a":`, "params 不是合法 JSON"},
		{"类型不符 string→number", `{"properties":{"k":{"type":"string"}}}`, `{"k":1}`, "params.k 类型必须是 string"},
		{"整数位不符", `{"properties":{"k":{"type":"integer"}}}`, `{"k":1.5}`, "类型必须是 integer"},
		{"枚举外取值", `{"properties":{"mode":{"enum":["fast"]}}}`, `{"mode":"slow"}`, "不在允许范围内"},
		{"低于下限", `{"properties":{"k":{"type":"number","minimum":10}}}`, `{"k":9}`, "不能小于 10"},
		{"高于上限", `{"properties":{"k":{"type":"number","maximum":10}}}`, `{"k":11}`, "不能大于 10"},
		{"长度下限（按字符计，中文算 1）", `{"properties":{"k":{"type":"string","minLength":3}}}`, `{"k":"蜂巢"}`, "至少 3 个字符"},
		{"长度上限", `{"properties":{"k":{"type":"string","maxLength":1}}}`, `{"k":"ab"}`, "最多 1 个字符"},
		{"pattern 不匹配", `{"properties":{"k":{"type":"string","pattern":"^[0-9]+$"}}}`, `{"k":"ab"}`, "不匹配要求的格式"},
		{"pattern 非法（schema 坏掉就 fail-close）", `{"properties":{"k":{"type":"string","pattern":"["}}}`, `{"k":"x"}`, "pattern 不是合法正则"},
		{"数组元素类型错", `{"properties":{"t":{"type":"array","items":{"type":"string"}}}}`, `{"t":["a",1]}`, "params.t[1] 类型必须是 string"},
		{"数组元素过少", `{"properties":{"t":{"type":"array","minItems":2}}}`, `{"t":["a"]}`, "至少 2 个元素"},
		{"数组元素过多", `{"properties":{"t":{"type":"array","maxItems":1}}}`, `{"t":["a","b"]}`, "最多 1 个元素"},
		{"未声明参数（additionalProperties:false）", `{"properties":{"a":{"type":"string"}},"additionalProperties":false}`, `{"b":1}`, "不接受未声明的参数 params.b"},
		{"嵌套对象缺必填", `{"properties":{"cfg":{"type":"object","required":["url"]}}}`, `{"cfg":{}}`, "缺少必填参数 params.cfg.url"},
		{"未知 type 关键字值", `{"properties":{"k":{"type":"email"}}}`, `{"k":"a@b.c"}`, `type 不支持 "email"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateEgoParams([]byte(c.schema), []byte(c.params))
			if err == nil {
				t.Fatalf("应当拒跑，却放行了")
			}
			if !errors.Is(err, ErrParamsInvalid) {
				t.Errorf("错误要能被 errors.Is(ErrParamsInvalid) 认出（controller 归 400），got=%v", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误文案要包含 %q，got=%q", c.want, err.Error())
			}
		})
	}
}

// 契约是 JSON 语义：数字用 json.Number 走，不能被 float64 精度带偏。
func TestValidateEgoParams_BigIntegerNotTruncated(t *testing.T) {
	schema := []byte(`{"properties":{"id":{"type":"integer","maximum":9007199254740993}}}`)
	if err := ValidateEgoParams(schema, []byte(`{"id":9007199254740993}`)); err != nil {
		t.Fatalf("大整数应当通过: %v", err)
	}
	if err := ValidateEgoParams(schema, []byte(`{"id":9007199254740999}`)); err == nil {
		t.Fatalf("超过上限的大整数必须拒（精度丢失会让它看起来合规）")
	}
}

// 参数原样透传：校验只看不改（脚本拿到的还是调用方给的那份）
func TestValidateEgoParams_DoesNotMutateInput(t *testing.T) {
	params := []byte(`{"k":"v"}`)
	schema := []byte(`{"properties":{"k":{"type":"string"}}}`)
	if err := ValidateEgoParams(schema, params); err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(params) != `{"k":"v"}` {
		t.Errorf("params 被改了：%s", string(params))
	}
	var raw map[string]any
	if err := json.Unmarshal(params, &raw); err != nil || raw["k"] != "v" {
		t.Errorf("params 仍是合法 JSON 且值不变")
	}
}
