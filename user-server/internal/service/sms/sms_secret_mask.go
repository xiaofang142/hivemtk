package sms

// 短信通道密钥的「掩码」口径（读侧脱敏 / 写侧认掩码）。
//
// 读侧（controller/sms.go 的 maskSMSConfig）把 AccessKeyId / AccessKeySecret 等
// 换成"掩码前缀 + 末 4 位"再给前端；写侧必须认得这个掩码，把它当"前端没改这个字段"
// ⇒ 保留库里原值。
//
// 为什么两件事必须成对：前端配置页把 GET 回来的整份配置直接塞进表单
// （user-web/src/views/sms/Config.vue：`smsConfig.value = response`，
//  表单项 `v-model="smsConfig.aliyun.accessKeyId"`），保存时**原样提交掩码串**。
// 只脱敏不认掩码 ⇒ 用户打开配置页点一次「保存」，真凭证就被覆盖成 "****ALUE"，
// 之后所有短信发送都回 "Specified access key is not found." ——
// 而页面上看不出任何异常，配置"保存成功"。
//
// 与企业微信那条（service/wecom_secret_mask.go）是同一族修法。

// SmsSecretMask 短信通道密钥出参的掩码前缀。
const SmsSecretMask = "****"

// isMaskedSmsSecret 判断一个值是不是掩码串（写侧据此保留原值）。
//
// 判据是**前缀**匹配：脱敏产出的是 "****ALUE" 这种"掩码+末4位"，
// 全等比对只认得 "****" 一个串，写侧就会把 "****ALUE" 当成用户新填的密钥。
//
// 误判方向要想清楚：把真密钥误认成掩码 ⇒ 保留原值（本次编辑没改它，结果正确）；
// 把掩码误认成真密钥 ⇒ 覆盖成掩码（通道废掉）。宁可前者。
func isMaskedSmsSecret(v string) bool {
	return len(v) >= len(SmsSecretMask) && v[:len(SmsSecretMask)] == SmsSecretMask
}
