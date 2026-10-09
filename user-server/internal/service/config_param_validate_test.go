package service

import (
	"testing"
	"time"
)

// validateValue 与 parseDurationSeconds 的行为钉死用例。
//
// 这组用例是为堵一个真实绕过口写的：历史实现里 duration 分支在
// time.ParseDuration / strconv.ParseFloat 双双失败时直接 `return nil`，
// 于是「非法 duration + 设了 min/max」这一格被静默放行 ——
// 运维能往 max=3600s 的参数里写进 "999999999s"，保存成功、读取侧却解析失败
// 悄悄回落到编译期兜底值。这跟「登记了没人读」是同一种病，只是更隐蔽。

func sp(s string) *string { return &s }

func TestValidateValueDurationNoLongerSkipsBounds(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		min     *string
		max     *string
		wantErr bool
	}{
		// —— 原来的绕过口：非法 duration 曾经被 return nil 放行 ——
		{"非法 duration + 有 min/max → 必须拒绝", "abc", sp("10"), sp("3600"), true},
		{"带单位但超上界 → 必须拒绝", "999999999s", sp("10"), sp("3600"), true},
		{"带单位且合法 → 接受", "30s", sp("10"), sp("3600"), false},
		{"带单位低于下界 → 必须拒绝", "5s", sp("10"), sp("3600"), true},
		{"裸秒数超上界 → 必须拒绝", "99999", sp("10"), sp("3600"), true},
		{"裸秒数合法 → 接受", "30", sp("10"), sp("3600"), false},
		// 单位混写也按秒折算：30s == 30 秒，与 max=60 比较是通过的
		{"秒数与 duration 字面量单位对齐", "30s", nil, sp("60"), false},
		{"分钟字面量按秒折算后超界", "90m", sp("10"), sp("3600"), true},
		{"复合字面量按秒折算后超界（1h30m=5400s > 3600s）", "1h30m", sp("10"), sp("3600"), true},
		{"复合字面量按秒折算后合法（30m=1800s）", "30m", sp("10"), sp("3600"), false},
		// 没有 min/max 时也必须做类型校验（不能因为没边界就什么都放）
		{"无边界 + 非法值 → 必须拒绝", "abc", nil, nil, true},
		{"无边界 + 合法值 → 接受", "30", nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateValue("duration", c.value, c.min, c.max)
			if c.wantErr && err == nil {
				t.Fatalf("validateValue(duration, %q, %v, %v) = nil, 期望报错（min/max 被绕过了）", c.value, c.min, c.max)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("validateValue(duration, %q, %v, %v) = %v, 期望通过", c.value, c.min, c.max, err)
			}
		})
	}
}

func TestValidateValueIntAndFloatBoundsAreEnforced(t *testing.T) {
	cases := []struct {
		valueType string
		value     string
		min       *string
		max       *string
		wantErr   bool
	}{
		{"int", "50", sp("1"), sp("100"), false},
		{"int", "101", sp("1"), sp("100"), true},
		{"int", "0", sp("0"), sp("100"), false},
		// 没有 min/max 时类型校验依然生效（旧实现在这里直接 return nil）
		{"int", "notanint", nil, nil, true},
		{"int", "42", nil, nil, false},
		{"float", "0.9", sp("0.5"), sp("1.0"), false},
		{"float", "1.1", sp("0.5"), sp("1.0"), true},
		{"float", "notafloat", nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.valueType+"/"+c.value, func(t *testing.T) {
			err := validateValue(c.valueType, c.value, c.min, c.max)
			if c.wantErr && err == nil {
				t.Fatalf("validateValue(%s, %q, %v, %v) = nil, 期望报错", c.valueType, c.value, c.min, c.max)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("validateValue(%s, %q, %v, %v) = %v, 期望通过", c.valueType, c.value, c.min, c.max, err)
			}
		})
	}
}

// 库里存了个解析不了的边界（min="abc"）必须报错，不能静默当 0 处理。
// 静默当 0 的后果是：所有值都「通过校验」，比直接报错难查得多。
func TestValidateValueRejectsBrokenBounds(t *testing.T) {
	cases := []struct {
		name      string
		valueType string
		min       *string
		max       *string
	}{
		{"int 的 min 是坏的", "int", sp("abc"), nil},
		{"int 的 max 是坏的", "int", nil, sp("abc")},
		{"float 的 min 是坏的", "float", sp("xyz"), nil},
		{"float 的 max 是坏的", "float", nil, sp("xyz")},
		{"duration 的 min 是坏的", "duration", sp("abc"), nil},
		{"duration 的 max 是坏的", "duration", nil, sp("abc")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateValue(c.valueType, "1", c.min, c.max); err == nil {
				t.Fatalf("validateValue(%s, \"1\", %v, %v) = nil, 期望对坏边界报错", c.valueType, c.min, c.max)
			}
		})
	}
}

// bool / string 不做值域校验，不能因为 switch 没有对应分支就把一切判红。
func TestValidateValueIgnoresNonNumericTypes(t *testing.T) {
	for _, vt := range []string{"bool", "string"} {
		if err := validateValue(vt, "随便什么", sp("1"), sp("2")); err != nil {
			t.Fatalf("validateValue(%s, ...) = %v, 期望 nil（该类型不参与数值校验）", vt, err)
		}
	}
}

// 读取侧与校验侧必须共用同一个解析口径。
// 这条是本文件存在的理由：两侧一旦分叉，就会裂成
// 「写得进库、读出来是 fallback」或者「校验形同虚设」两种静默失效。
func TestParseDurationSecondsSharedByReadAndValidate(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"30", 30 * time.Second},
		{"0.5", 500 * time.Millisecond},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"0", 0},
	}
	for _, c := range cases {
		got, err := parseDurationSeconds(c.raw)
		if err != nil {
			t.Fatalf("parseDurationSeconds(%q) 返回错误 %v", c.raw, err)
		}
		if d := time.Duration(got * float64(time.Second)); d != c.want {
			t.Fatalf("parseDurationSeconds(%q) = %v, 期望 %v", c.raw, d, c.want)
		}
	}

	for _, bad := range []string{"", "abc", "30 s", "-", "1x"} {
		if _, err := parseDurationSeconds(bad); err == nil {
			t.Fatalf("parseDurationSeconds(%q) = nil error, 期望报错", bad)
		}
	}
}

// GetDuration 与 parseDurationSeconds 口径一致：能校验通过的写法，读取侧也必须认。
func TestGetDurationAcceptsWhatValidateAccepts(t *testing.T) {
	svc := &ConfigParamService{
		cache:  map[string]paramEntry{},
		loaded: map[string]bool{},
	}
	// 直接往缓存里塞值，绕开 DB，专注验证解析口径。
	svc.cache["g.k"] = paramEntry{value: "45s", expiresAt: time.Now().Add(time.Minute)}

	got := svc.GetDuration(t.Context(), "g", "k", 7*time.Second)
	if got != 45*time.Second {
		t.Fatalf("GetDuration = %v, 期望 45s", got)
	}
	if err := validateValue("duration", "45s", sp("10"), sp("60")); err != nil {
		t.Fatalf("validateValue(duration, \"45s\") = %v, 期望通过（与 GetDuration 口径一致）", err)
	}
}

// 每条种子的 DefaultValue 必须能通过它自己声明的类型与边界。
//
// 这条守卫的存在是因为「ResetToDefault / BulkResetGroup」会把值写回 DefaultValue：
// 一条 DefaultValue 越界的种子，等于埋了一颗「运维点一下重置就报错」的地雷，
// 而且要到线上被点才会炸。接线量上来后手工维护 300+ 条种子，靠人眼扫 min/max 是守不住的。
func TestSeedDefaultsPassTheirOwnValidation(t *testing.T) {
	for _, d := range DefaultParamDefs() {
		if d.DefaultValue == "" {
			continue
		}
		if err := validateValue(d.ValueType, d.DefaultValue, d.Min, d.Max); err != nil {
			t.Errorf("种子 %s/%s 的 DefaultValue=%q 通不过自己的校验（type=%s min=%v max=%v）：%v",
				d.Group, d.Key, d.DefaultValue, d.ValueType, d.Min, d.Max, err)
		}
	}
}