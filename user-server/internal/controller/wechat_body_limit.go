package controller

import "io"

// maxWechatBody 微信回调请求体硬上限（第十六轮审计，2026-09-19）。
// ReceiveMessage 的入口闸门只有 SHA1 签名校验——持有 account token 的一方
// （配置泄露/内部人员/上游被拖）可用任意大小 body 过闸；此前 io.ReadAll
// 不设限，等于把"进程内存"挂在一个公网可写端点上。
// 微信官方消息推送正文远小于 64KB，1MB 已给正常流量留足余量；
// 超限请求会被截断 → XML 解析失败 → 400，不产生副作用。
const maxWechatBody int64 = 1 << 20

func readWechatBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxWechatBody))
}
