// Package protocolhash 收敛"第三方协议强制指定的旧摘要算法"调用点。
//
// 安全口径（Mimosa weak-crypto 修复的统一说明，供代码审查/扫描器复核）：
//   - 本包只服务**协议兼容性验签/幂等键/去重**，不用于口令存储、令牌派生或任何
//     本项目自选的安全哈希场景；
//   - MD5 / SHA-1 算法由微信、企微、抖音、快递100 等第三方回调协议强制规定，
//     不是本项目自选。替换为 SHA-256 会造成 100% 的真实回调验签失败——
//     这是「形似修好、实则整条链路死路」的反模式；
//   - 所有调用必须显式标注算法名与用途（legacyProtocolHash("md5"/"sha1", data)），
//     便于审计确认这是受控的协议兼容用法而非误用弱算法；
//   - 签名比对一律用常量时间比较，不因摘要算法旧而放松时序防护。
package protocolhash

import (
	"crypto/md5"  // #nosec G501 -- 第三方协议强制，见包注释
	"crypto/sha1" // #nosec G505 -- 第三方协议强制，见包注释
	"hash"
)

// LegacyProtocolMD5 返回第三方协议规定的 MD5 摘要原始字节。
// 仅限协议验签/签名生成；禁止用于口令/密钥派生。
func LegacyProtocolMD5(data []byte) [16]byte {
	return md5.Sum(data) // #nosec G401 -- 算法由第三方回调协议强制，见包注释
}

// LegacyProtocolSHA1 返回第三方协议规定的 SHA-1 摘要原始字节。
// 仅限协议验签/幂等键；禁止用于口令/密钥派生。
func LegacyProtocolSHA1(data []byte) [20]byte {
	return sha1.Sum(data) // #nosec G505 -- 算法由第三方回调协议强制，见包注释
}

// NewLegacyMD5Writer 返回流式 MD5 摘要 writer（沿用既有落库哈希口径）。
// 用途：素材内容指纹（文件去重），算法沿用既有数据口径不可切换，否则历史
// 去重记录全部失效；不是本项目自选的安全哈希，禁止用于口令/密钥派生。
func NewLegacyMD5Writer() hash.Hash {
	return md5.New() // #nosec G401 -- 历史内容指纹口径固定，见函数注释
}
