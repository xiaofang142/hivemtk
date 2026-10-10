package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultVisitorTokenTTL visitor token 默认有效期（7 天）
//
// 它同时是参数中心 `misc.visitor_token_ttl` 的代码兜底值：两者必须一致，
// 否则「运维改了参数」与「没配参数」会走出两种 TTL，而页面上看不出区别。
const DefaultVisitorTokenTTL = 7 * 24 * time.Hour

// visitorTokenTTLProvider 由装配层（internal/app/misc_params_wiring.go）在
// SeedConfigParams 之后注入；未注入时回落 DefaultVisitorTokenTTL。
//
// 做成函数而不是直接读全局单例：这个包是叶子包，不该知道参数中心的存在。
var visitorTokenTTLProvider = func() time.Duration { return DefaultVisitorTokenTTL }

// GetVisitorTokenTTL 返回当前生效的访客 token 有效期。
func GetVisitorTokenTTL() time.Duration { return visitorTokenTTLProvider() }

// SetVisitorTokenTTLProvider 注入读取函数（参数中心装配层调用；测试可注入桩）。
// nil 视为不注入，避免装配顺序出错时把兜底值顶掉。
func SetVisitorTokenTTLProvider(fn func() time.Duration) {
	if fn != nil {
		visitorTokenTTLProvider = fn
	}
}

// GenerateVisitorToken 使用 HMAC-SHA256 为访客生成签名 token
//
// 组成：hex(hmac_sha256(secret, channelID|visitorID|sessionID|expire_ts)) + "." + expire_ts
// 验证时比对 token 与 (channelID, visitorID, sessionID) 绑定关系，
// 防止攻击者通过枚举 visitor_id 越权读取他人会话历史（IDOR 修复）。
//
// Parameters:
//   - secret: HMAC 密钥（建议 32+ 字符随机字符串）
//   - channelID: 渠道 ID
//   - visitorID: 访客 ID
//   - sessionID: 会话 ID
//   - ttl: 有效期，0 表示使用默认 7 天
func GenerateVisitorToken(secret, channelID, visitorID, sessionID string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("secret 不能为空")
	}
	if channelID == "" || visitorID == "" || sessionID == "" {
		return "", errors.New("channel_id、visitor_id、session_id 均不能为空")
	}
	if ttl <= 0 {
		ttl = GetVisitorTokenTTL()
	}
	expireTS := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s|%s|%s|%d", channelID, visitorID, sessionID, expireTS)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s.%d", sig, expireTS), nil
}

// ValidateVisitorToken 验证 visitor token
//
// 校验规则：
//  1. token 格式合法（含 . 分隔符）
//  2. HMAC 签名匹配
//  3. 未过期
//  4. 与传入的 (channelID, visitorID, sessionID) 一致
//
// Parameters:
//   - secret: HMAC 密钥，必须与生成时使用的一致
//   - token: 待验证的 token
//   - channelID: 渠道 ID
//   - visitorID: 访客 ID
//   - sessionID: 会话 ID
func ValidateVisitorToken(secret, token, channelID, visitorID, sessionID string) error {
	if secret == "" {
		return errors.New("secret 未配置")
	}
	if token == "" {
		return errors.New("缺少 visitor_token")
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return errors.New("visitor_token 格式无效")
	}
	sig := parts[0]
	var expireTS int64
	if _, err := fmt.Sscanf(parts[1], "%d", &expireTS); err != nil {
		return errors.New("visitor_token 过期时间解析失败")
	}
	if time.Now().Unix() > expireTS {
		return errors.New("visitor_token 已过期")
	}
	payload := fmt.Sprintf("%s|%s|%s|%d", channelID, visitorID, sessionID, expireTS)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return errors.New("visitor_token 签名无效")
	}
	return nil
}
