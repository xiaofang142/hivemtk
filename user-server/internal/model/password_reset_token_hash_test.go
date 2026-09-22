// password_reset_token_hash_test.go 钉住"库里那一列不是可直接重放的凭证"。
//
// 旧实现把 forgot-password 令牌原样写进 password_reset_tokens.token（uuid+uuid，72 字符），
// 于是备份文件、SQL 日志、只读副本里任何一处泄露都等于交出改密能力（24h 窗口内）。
// 现在这列只存明文的 SHA-256；明文只在签发那一刻存在于 RawToken，用于拼邮件里的重置链接。
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func hashResetTokenForTest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestPasswordResetTokenStoresHashOfRawToken(t *testing.T) {
	tok := &PasswordResetToken{UserID: "77", ExpiresAt: time.Now().Add(time.Hour)}
	if err := tok.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate 报错: %v", err)
	}

	if len(tok.RawToken) != 72 {
		t.Errorf("RawToken 长度=%d，期望 72（uuid+uuid，签发侧要靠它拼重置链接）⇒ 值=%q",
			len(tok.RawToken), tok.RawToken)
	}
	if tok.RawToken == "" {
		t.Fatal("RawToken 未填充 ⇒ 邮件里的重置链接无从生成")
	}
	if tok.Token != hashResetTokenForTest(tok.RawToken) {
		t.Errorf("token 列不是明文的 SHA-256：存的是 %q（%d 字符）", tok.Token, len(tok.Token))
	}
	if tok.Token == tok.RawToken || strings.Contains(tok.Token, tok.RawToken) {
		t.Errorf("明文仍原样落库：token=%q", tok.Token)
	}
}

// 调用方预置 Token 也不得成为"把明文塞进库里"的逃生口：先哈希，再落库。
func TestPasswordResetTokenBeforeCreateNeverStoresCallerSuppliedPlaintext(t *testing.T) {
	const plaintext = "caller-supplied-plaintext-token"
	tok := &PasswordResetToken{UserID: "77", Token: plaintext, ExpiresAt: time.Now().Add(time.Hour)}
	if err := tok.BeforeCreate(nil); err != nil {
		t.Fatalf("BeforeCreate 报错: %v", err)
	}
	if tok.Token == plaintext {
		t.Fatalf("调用方传的原文被原样保留 ⇒ 哈希化有逃生口：token=%q", tok.Token)
	}
	if tok.Token != hashResetTokenForTest(tok.RawToken) {
		t.Errorf("token 列不等于新生成明文的 SHA-256：存 %q，RawToken %q", tok.Token, tok.RawToken)
	}
}
