// ltc_flag.go LTC 接线类变更的进程级 env 开关共用解析（T-P1-01 / T-P1-02）。
//
// 有意不走 pkg/featureflag：那里是 5s 后台轮询的缓存值，一次 RunOnce 里
// "存点"与"取点"可能落在缓存刷新两侧，出现只写不读（或反之）的半开状态。
// 接线开关的读写必须来自同一个判定，故每次直接读 env（进程级常量，纳秒级开销）。
package service

import (
	"os"
	"strconv"
	"strings"
)

// envFlagEnabled 解析 FF_* 开关：1/true/yes/on 为开（大小写不敏感），
// 其余（含未设置、无法解析）为关。
//
// 认不出的值一律判关而不是判开 —— 灰度开关上写错值时，安全侧是"不启用"。
func envFlagEnabled(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if v == "" {
		return false
	}
	if ok, err := strconv.ParseBool(v); err == nil {
		return ok
	}
	switch v {
	case "yes", "y", "on":
		return true
	default:
		return false
	}
}
