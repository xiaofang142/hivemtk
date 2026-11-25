package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"hivemtk-user/internal/pkg/protocolhash"
)

// FromClient 由请求侧 User-Agent 与来源 IP 计算设备指纹（sha256 hex）。
//
// 与 GenerateFingerprint（宿主机指纹，心跳上报用）区分：本函数算的是**发起
// 注册/初始化请求的客户端**指纹，用于合规留痕（003 T010 静默采集）。
// 只哈希不存原文：UA 明文可拼出浏览器指纹细节，留痕场景无此必要。
func FromClient(userAgent, clientIP string) string {
	sum := sha256.Sum256([]byte(userAgent + "|" + clientIP))
	return hex.EncodeToString(sum[:])
}

// HashHex 返回任意字符串的 sha256 hex（init_ua 脱敏落库用）
func HashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// GenerateFingerprint 生成设备指纹。
// MD5 沿用既有心跳上报指纹口径（非安全用途，不参与鉴权），通过受控封装标注用途；
// 换算法会让同一台宿主机被识别为新设备，破坏心跳连续性。
func GenerateFingerprint() (string, error) {
	info := collectDeviceInfo()

	hash := protocolhash.LegacyProtocolMD5([]byte(info))
	return hex.EncodeToString(hash[:]), nil
}

func collectDeviceInfo() string {
	var info strings.Builder

	info.WriteString(fmt.Sprintf("os:%s,%s\n", runtime.GOOS, runtime.GOARCH))

	hostname, err := os.Hostname()
	if err == nil {
		info.WriteString(fmt.Sprintf("hostname:%s\n", hostname))
	}

	if os.Getenv("USER") != "" {
		info.WriteString(fmt.Sprintf("user:%s\n", os.Getenv("USER")))
	}

	cwd, err := os.Getwd()
	if err == nil {
		info.WriteString(fmt.Sprintf("cwd:%s\n", cwd))
	}

	return info.String()
}
