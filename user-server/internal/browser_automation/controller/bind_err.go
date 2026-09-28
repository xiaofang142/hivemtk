package controller

// 请求参数绑定错误的统一出口（替换七个入口上的 `"参数错误: "+err.Error()`）。
//
// 那行的两个毛病，都在真实入口上实测过：
//  1. **泄露**：validator 原文带着 Go 结构体名、字段名和 tag 内容
//     （实测 `Key: 'CreateBrowserTaskReq.Steps' Error:Field validation for 'Steps' failed on the 'max' tag`）。
//     本仓 response.BindJSON 的注释把这条写成了规矩：这类细节等于给攻击者画数据模型，
//     内部原文只该进日志。这一族入口没走那个 helper，是漏网的第七处而不是新口径。
//  2. **没用**：前端只做 `ElMessage.error(res.message)` 透传，用户看到一句英文内部术语，
//     不知道改哪个输入框。拒了却不可操作，比不拒更耗人。
//
// 所以这里的判据是：字段用**对外名字**（json/form tag，也就是请求体和输入框里那个名字），
// 约束翻成中文短语，原文只进日志；HTTP 码与业务码维持 400 / INVALID_PARAM_1001 不变。

import (
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// bindErrToResponse 写出 400；调用方紧接着 return 即可（它不返回值，七个入口都在 void handler 里）。
// req 必须就是刚才传给 ShouldBindJSON/ShouldBindQuery 的那个结构体——对外字段名从它的 tag 上取。
func bindErrToResponse(ctx *gin.Context, err error, req any) {
	logger.Errorf("参数绑定失败 %s %s: %v", ctx.Request.Method, ctx.Request.URL.Path, err)

	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) || len(verrs) == 0 {
		// 走到这里的是「请求体不是合法 JSON / 类型对不上」：没有字段可点名，
		// 也不许把 encoding/json 的原文回显出去（UnmarshalTypeError 里同样带着结构体名）。
		response.Error(ctx, http.StatusBadRequest, "请求参数无法解析，请检查请求体格式")
		return
	}

	names := apiFieldNames(req)
	const maxPoints = 3 // 文案有界：一次最多点三个字段，全量清单已经在日志里
	var field string
	points := make([]string, 0, maxPoints)
	for _, fe := range verrs {
		path := apiFieldPath(fe.Namespace(), names)
		if field == "" {
			field = path
		}
		points = append(points, path+constraintText(fe))
		if len(points) == maxPoints {
			break
		}
	}
	response.InvalidParameterError(ctx, field, "参数不合法："+strings.Join(points, "；"))
}

// constraintText 把 binding tag 翻成人话。fe.Param() 是我们自己写在 tag 里的值，回显它不泄露
// 任何东西，而它是这条文案里唯一能指导用户改什么的部分（上限是多少、合法取值有哪些）。
func constraintText(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return " 为必填项"
	case "max":
		return " 上限 " + boundText(fe)
	case "min":
		if isNumeric(fe) {
			// 「下限 取值 30」是把两种读法缝在一起；数值型直接说人该怎么改
			return " 不得小于 " + fe.Param()
		}
		return " 至少 " + boundText(fe)
	case "oneof":
		return " 取值需为 " + strings.Join(strings.Fields(fe.Param()), " / ") + " 之一"
	default:
		return " 不满足约束 " + fe.ActualTag()
	}
}

// boundText 同一个界在不同类型上读法不同：slice 上是步数、string 上是字数、数值上就是那个数。
// 按 Kind 分流，免得把「steps 上限 200 个字符」「retry_count 上限 取值 10」这种话发给用户。
func boundText(fe validator.FieldError) string {
	switch fe.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return fe.Param() + " 项"
	case reflect.String:
		return fe.Param() + " 个字符"
	default:
		return fe.Param()
	}
}

func isNumeric(fe validator.FieldError) bool {
	switch fe.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// pathToken 拆 validator 的 Namespace()：标识符段与 [下标] 段各成一枚 token
var pathToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*|\[\d+\]`)

// apiFieldPath "CreateBrowserTaskReq.Steps[1].Action" → "steps[1].action"。
// 首 token 是 Go 结构体名（对外不存在），丢掉；其余标识符逐个查表换名，下标原样保留、前不加圆点。
// 查不到的名字按原样带出：那是「文案不够顺」，不是「泄露」，宁可多印一个 PascalCase
// 也不给用户一条点不出字段的空消息。
func apiFieldPath(namespace string, names map[string]string) string {
	toks := pathToken.FindAllString(namespace, -1)
	var b strings.Builder
	for i, tok := range toks {
		if i == 0 && !strings.HasPrefix(tok, "[") {
			continue // 结构体名
		}
		if strings.HasPrefix(tok, "[") {
			b.WriteString(tok)
			continue
		}
		if b.Len() > 0 {
			b.WriteString(".")
		}
		if name, ok := names[tok]; ok {
			tok = name
		}
		b.WriteString(tok)
	}
	if b.Len() == 0 {
		return namespace
	}
	return b.String()
}

// apiFieldNames 收集 req 可达结构体（≤3 层）的「Go 字段名 → 对外字段名」：
// 先看 json tag（请求体里的名字），没有再看 form tag（query 参数里的名字），都没有则跳过。
// 只取 tag 逗号前的部分，忽略 omitempty 之类的选项。
func apiFieldNames(req any) map[string]string {
	out := map[string]string{}
	if req == nil {
		return out
	}
	collectFieldNames(reflect.TypeOf(req), out, 3)
	return out
}

func collectFieldNames(t reflect.Type, out map[string]string, depth int) {
	if depth <= 0 || t == nil {
		return
	}
	switch t.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Map:
		collectFieldNames(t.Elem(), out, depth)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // 非导出字段不参与序列化，对外不存在
			}
			if name := apiTagName(f.Tag); name != "" {
				out[f.Name] = name
			}
			collectFieldNames(f.Type, out, depth-1)
		}
	}
}

func apiTagName(tag reflect.StructTag) string {
	for _, key := range []string{"json", "form"} {
		if v, ok := tag.Lookup(key); ok {
			v = strings.Split(v, ",")[0]
			if v != "" && v != "-" {
				return v
			}
		}
	}
	return ""
}
