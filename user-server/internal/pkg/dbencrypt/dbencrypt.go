// Package dbencrypt 提供数据库敏感字段的落库加密出口（OPT-SEC-04）。
//
// 设计要点：
//   - 复用 internal/secrets 的 AES-256-GCM 原语，密文格式 `enc:v1:{base64}`，
//     与 LLM provider APIKey 等既有加密字段同格式同密钥。
//   - 字符串出入口均为“永不失败”语义：Encrypt 失败降级明文（仅 warn 一次），
//     Decrypt 非密文前缀透传、解密失败原样返回。保证任何环境（含 MASTER_KEY 缺失的
//     开发环境、密钥轮换过渡期）都不会阻断日志写入或查询链路。
//   - 对存量明文行向后兼容：历史数据无 enc:v1: 前缀，Decrypt 原样返回，
//     无需一次性全表改写（详见 migrations/057_api_logs_encrypt_fields.sql）。
//   - 调用位置约定：写入出口（Repository Create 前）调 Encrypt，
//     读取出口（Repository Find 后）调 Decrypt；不修改 model 字段类型。
package dbencrypt

import (
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/secrets"
)

// ciphertextPrefix 与 secrets 内部 stringEncPrefix 保持一致的持久化格式前缀。
const ciphertextPrefix = "enc:v1:"

var notReadyWarnOnce sync.Once

// Encrypt 加密落库字段。
//   - 空串原样返回（避免把“无值”变成一段密文）；
//   - 已是 enc:v1: 密文格式原样返回（幂等，防止二次加密）；
//   - secrets 未初始化（MASTER_KEY 缺失）时降级明文并进程内 warn 一次；
//   - 加密失败同样降级明文。
func Encrypt(plain string) string {
	if plain == "" {
		return plain
	}
	if secrets.IsCiphertextFormat(plain) {
		return plain
	}
	if !secrets.Ready() {
		warnNotReadyOnce()
		return plain
	}
	enc, err := secrets.EncryptString(plain)
	if err != nil {
		warnNotReadyOnce()
		return plain
	}
	return enc
}

// Decrypt 读取出口解密。
//   - 非 enc:v1: 前缀（含存量明文行）原样返回，保证向后兼容；
//   - secrets 未初始化或解密失败（密钥轮换过渡期可能遇到旧密钥密文）时原样返回，
//     不阻断列表接口。
func Decrypt(cipherText string) string {
	if cipherText == "" {
		return cipherText
	}
	if !secrets.IsCiphertextFormat(cipherText) {
		return cipherText
	}
	if !secrets.Ready() {
		return cipherText
	}
	plain, err := secrets.DecryptString(cipherText)
	if err != nil {
		logger.Warnf("[dbencrypt] 字段解密失败（密钥轮换/MASTER_KEY 不一致？），原样返回密文: %v", err)
		return cipherText
	}
	return plain
}

// warnNotReadyOnce 仅在进程生命周期内提示一次降级警告，避免日志刷爆。
func warnNotReadyOnce() {
	notReadyWarnOnce.Do(func() {
		logger.Warn("[dbencrypt] secrets 未初始化（MASTER_KEY 缺失），敏感字段降级明文存储；生产环境请配置 MASTER_KEY")
	})
}
