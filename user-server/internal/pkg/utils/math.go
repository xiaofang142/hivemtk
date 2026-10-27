package utils

import "math"

// Round2 四舍五入到两位小数。
//
// 原先只是 service/email_tracking.go 里的私有 round2，被短信/邮件/看板多处复制；
// 按域拆包（service/sms）时子包不能反向依赖根 service，故下沉到 pkg。
func Round2(v float64) float64 { return math.Round(v*100) / 100 }
