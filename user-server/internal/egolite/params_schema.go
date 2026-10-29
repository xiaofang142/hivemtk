package egolite

// params_schema.go —— S-11（U1）：执行参数契约。
//
// 为什么服务端必须校验：表单只是「少让人写 JSON」，不是安全边界——同一个
// POST /api/egolite/executions 谁都能直接打，前端渲染得再漂亮，绕过它只值一行 curl。
// 所以参数的唯一判据放在执行域，和扫描门/版本钉扎同一个位置（Start）。
//
// 支持的是 JSON Schema 的一个**明确子集**（资产 params_schema 的实际用面）：
//   type / properties / required / additionalProperties(false) /
//   enum / minimum / maximum / minLength / maxLength / pattern /
//   items / minItems / maxItems
// 其余关键字一律忽略（宽松），避免"声明了但我们不认"变成莫名其妙的拒跑。
// 但 schema 本身坏掉（非对象、type 非法、pattern 编译不了）一律 fail-close——
// 契约坏了不能靠猜过去，那等于没有契约。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrParamsInvalid 参数不符合契约（controller 归为 400：改入参就能好，不是服务端故障）
var ErrParamsInvalid = fmt.Errorf("执行参数不符合参数契约")

// ValidateEgoParams 按 params_schema 校验一次执行参数。
//
// schema 为空 ⇒ 无契约，放行（未声明参数的资产/流程保持原行为）。
// params 为空 ⇒ 按 {} 处理（这样 required 才能真正生效）。
func ValidateEgoParams(schema []byte, params []byte) error {
	if len(schema) == 0 || string(schema) == "null" {
		return nil
	}
	// schema 也用 UseNumber 解：minimum/maximum 一旦先落成 float64，
	// 2^53 以上的整数边界就已经失真，再拿它判参数等于用错的尺子量。
	sdec := json.NewDecoder(bytes.NewReader(schema))
	sdec.UseNumber()
	var s map[string]any
	if err := sdec.Decode(&s); err != nil {
		return fmt.Errorf("%w: params_schema 不是合法 JSON 对象: %v", ErrParamsInvalid, err)
	}
	obj, err := decodeParamsObject(params)
	if err != nil {
		return err
	}
	if err := validateObject("params", s, obj); err != nil {
		return fmt.Errorf("%w: %v", ErrParamsInvalid, err)
	}
	return nil
}

// decodeParamsObject 参数必须是 JSON 对象（脚本拿到的就是一个 map；数组/标量没有意义）。
func decodeParamsObject(params []byte) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(params))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: params 不是合法 JSON: %v", ErrParamsInvalid, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: params 必须是 JSON 对象", ErrParamsInvalid)
	}
	return obj, nil
}

// validateObject 校验一个 object 值（对应一个 schema 节点 + 一个 map 值）。
func validateObject(path string, schema map[string]any, value map[string]any) error {
	if t, ok := schema["type"].(string); ok && t != "" && t != "object" {
		return fmt.Errorf("%s 的 params_schema.type 只支持 object（实得 %q）", path, t)
	}

	if raw, ok := schema["required"]; ok {
		reqs, err := stringList(raw)
		if err != nil {
			return fmt.Errorf("%s.required 必须是字符串数组", path)
		}
		for _, k := range reqs {
			if _, exists := value[k]; !exists {
				return fmt.Errorf("缺少必填参数 %s", joinPath(path, k))
			}
		}
	}

	props := map[string]any{}
	if raw, ok := schema["properties"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.properties 必须是对象", path)
		}
		props = m
	}

	if raw, ok := schema["additionalProperties"]; ok {
		if allow, isBool := raw.(bool); isBool && !allow {
			for _, k := range sortedKeys(value) {
				if _, declared := props[k]; !declared {
					return fmt.Errorf("不接受未声明的参数 %s", joinPath(path, k))
				}
			}
		}
	}

	for _, k := range sortedKeys(props) {
		sub, ok := props[k].(map[string]any)
		if !ok {
			return fmt.Errorf("%s.properties.%s 必须是 schema 对象", path, k)
		}
		v, exists := value[k]
		if !exists {
			continue
		}
		if err := validateValue(joinPath(path, k), sub, v); err != nil {
			return err
		}
	}
	return nil
}

// validateValue 校验一个具体值（递归处理 array/object）。
func validateValue(path string, schema map[string]any, value any) error {
	if value == nil {
		return nil // 显式 null 交给业务脚本判定（子集不实现 nullable 组合子）
	}
	if want, ok := schema["type"].(string); ok && want != "" {
		if err := checkType(path, want, value); err != nil {
			return err
		}
	}
	if raw, ok := schema["enum"]; ok {
		list, isList := raw.([]any)
		if !isList {
			return fmt.Errorf("%s.enum 必须是数组", path)
		}
		matched := false
		for _, want := range list {
			if looseEqual(want, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s 的取值不在允许范围内", path)
		}
	}

	switch v := value.(type) {
	case json.Number:
		return checkNumber(path, schema, v)
	case float64: // 兼容直接 Unmarshal 出来的形态
		return checkNumber(path, schema, json.Number(fmt.Sprintf("%v", v)))
	case string:
		return checkString(path, schema, v)
	case []any:
		return checkArray(path, schema, v)
	case map[string]any:
		return validateObject(path, schema, v)
	}
	return nil
}

func checkType(path, want string, value any) error {
	ok := false
	switch want {
	case "string":
		_, ok = value.(string)
	case "number":
		switch value.(type) {
		case json.Number, float64, int, int64:
			ok = true
		}
	case "integer":
		switch v := value.(type) {
		case json.Number:
			f, err := v.Float64()
			ok = err == nil && math.Trunc(f) == f
		case float64:
			ok = math.Trunc(v) == v
		case int, int64:
			ok = true
		}
	case "boolean":
		_, ok = value.(bool)
	case "array":
		_, ok = value.([]any)
	case "object":
		_, ok = value.(map[string]any)
	case "null":
		ok = value == nil
	default:
		return fmt.Errorf("%s.type 不支持 %q", path, want)
	}
	if !ok {
		return fmt.Errorf("%s 类型必须是 %s", path, want)
	}
	return nil
}

func checkNumber(path string, schema map[string]any, n json.Number) error {
	if _, err := n.Float64(); err != nil {
		return fmt.Errorf("%s 不是合法数字", path)
	}
	if raw, ok := schema["minimum"]; ok {
		if min, ok := toNumber(raw); ok && compareNumbers(n, min) < 0 {
			return fmt.Errorf("%s 不能小于 %s", path, min.String())
		}
	}
	if raw, ok := schema["maximum"]; ok {
		if max, ok := toNumber(raw); ok && compareNumbers(n, max) > 0 {
			return fmt.Errorf("%s 不能大于 %s", path, max.String())
		}
	}
	return nil
}

func checkString(path string, schema map[string]any, s string) error {
	n := utf8.RuneCountInString(s)
	if raw, ok := schema["minLength"]; ok {
		if min, ok := toNumber(raw); ok && compareNumbers(json.Number(fmt.Sprint(n)), min) < 0 {
			return fmt.Errorf("%s 至少 %s 个字符", path, min.String())
		}
	}
	if raw, ok := schema["maxLength"]; ok {
		if max, ok := toNumber(raw); ok && compareNumbers(json.Number(fmt.Sprint(n)), max) > 0 {
			return fmt.Errorf("%s 最多 %s 个字符", path, max.String())
		}
	}
	if raw, ok := schema["pattern"]; ok {
		pattern, isStr := raw.(string)
		if !isStr {
			return fmt.Errorf("%s.pattern 必须是字符串", path)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("%s.pattern 不是合法正则: %v", path, err)
		}
		if !re.MatchString(s) {
			return fmt.Errorf("%s 不匹配要求的格式", path)
		}
	}
	return nil
}

func checkArray(path string, schema map[string]any, arr []any) error {
	if raw, ok := schema["minItems"]; ok {
		if min, ok := toNumber(raw); ok && compareNumbers(json.Number(fmt.Sprint(len(arr))), min) < 0 {
			return fmt.Errorf("%s 至少 %s 个元素", path, min.String())
		}
	}
	if raw, ok := schema["maxItems"]; ok {
		if max, ok := toNumber(raw); ok && compareNumbers(json.Number(fmt.Sprint(len(arr))), max) > 0 {
			return fmt.Errorf("%s 最多 %s 个元素", path, max.String())
		}
	}
	if raw, ok := schema["items"]; ok && raw != nil {
		sub, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.items 必须是 schema 对象", path)
		}
		for i, item := range arr {
			if err := validateValue(fmt.Sprintf("%s[%d]", path, i), sub, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func stringList(raw any) ([]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("not a list")
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("not a string list")
		}
		out = append(out, s)
	}
	return out, nil
}

// toNumber 把 schema 里的数值收敛成 json.Number（保留原始十进制串，不丢精度）。
func toNumber(raw any) (json.Number, bool) {
	switch v := raw.(type) {
	case json.Number:
		return v, true
	case float64:
		return json.Number(strconv.FormatFloat(v, 'g', -1, 64)), true
	case int:
		return json.Number(strconv.Itoa(v)), true
	case int64:
		return json.Number(strconv.FormatInt(v, 10)), true
	}
	return "", false
}

// compareNumbers 精确比较两个 JSON 数字（big.Rat 走十进制有理数，不经 float64）。
func compareNumbers(a, b json.Number) int {
	ra, okA := new(big.Rat).SetString(a.String())
	rb, okB := new(big.Rat).SetString(b.String())
	if !okA || !okB {
		// 指数形态（1e3）SetString 不认——退回 float64，仅此一路失真，且已是最坏情况
		fa, _ := a.Float64()
		fb, _ := b.Float64()
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	}
	return ra.Cmp(rb)
}

// looseEqual 枚举比较：JSON 里 1 与 1.0 是同一个数，字符串/布尔按值比。
func looseEqual(want, got any) bool {
	if wf, ok := toNumber(want); ok {
		if gf, ok := toNumber(got); ok {
			return compareNumbers(wf, gf) == 0
		}
		return false
	}
	return fmt.Sprint(want) == fmt.Sprint(got) && typeClass(want) == typeClass(got)
}

func typeClass(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	}
	return "other"
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
