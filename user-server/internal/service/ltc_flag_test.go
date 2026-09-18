// ltc_flag_test.go 接线开关解析的口径测试（T-P1-01 / T-P1-02 共用）。
//
// 之所以单独立一个文件：envFlagEnabled 同时决定 checkpoint 与 compensation 两条
// 生产装配路径，判"开"过宽 = 未经灰度就把行为切到现网。默认必须判关。
package service

import "testing"

func TestEnvFlagEnabledDefaultsOff(t *testing.T) {
	offs := []string{"", " ", "0", "false", "FALSE", "no", "n", "off", "OFF"}
	for _, v := range offs {
		t.Setenv("FF_LTC_FLAG_TEST", v)
		if envFlagEnabled("FF_LTC_FLAG_TEST") {
			t.Errorf("env=%q 应判关（未显式打开的一律不接线）", v)
		}
	}

	ons := []string{"1", "true", "TRUE", "True", "yes", "y", "Y", "on", "On"}
	for _, v := range ons {
		t.Setenv("FF_LTC_FLAG_TEST", v)
		if !envFlagEnabled("FF_LTC_FLAG_TEST") {
			t.Errorf("env=%q 应判开", v)
		}
	}

	// 无法识别的值按关处理： typo（trure / 1a）不得静默变成"开"
	for _, v := range []string{"tru", "ture", "1a", "yesno", "2", "-1", "开启"} {
		t.Setenv("FF_LTC_FLAG_TEST", v)
		if envFlagEnabled("FF_LTC_FLAG_TEST") {
			t.Errorf("env=%q 是不可识别值，必须判关", v)
		}
	}

	// 不存在的变量名 = 关（缺省不接线）
	if envFlagEnabled("FF_LTC_FLAG_TEST_UNSET_XYZ") {
		t.Error("未设置的开关必须判关")
	}
}
