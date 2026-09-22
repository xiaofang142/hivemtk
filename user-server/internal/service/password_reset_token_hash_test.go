// password_reset_token_hash_test.go 把"重置令牌的往返只在明文那一侧成立"钉成行为判据。
//
// 两条腿各管一头：
//   - 库里那一列必须是明文的 SHA-256（备份/日志/只读副本泄露 ≠ 交出改密能力）；
//   - 拿库内那个值去走校验必须失败（否则哈希化只是换了个写法，攻击者照旧能重放）。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func sha256HexReset(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestResetTokenRoundTripRejectsStoredHashAsCredential(t *testing.T) {
	db := testutil.NewTestDB(t, &model.PasswordResetToken{})
	if db == nil {
		t.Skip("测试库不可达")
	}

	tok := &model.PasswordResetToken{UserID: "88", ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(tok).Error; err != nil {
		t.Fatalf("建令牌行失败: %v", err)
	}
	raw := tok.RawToken
	if raw == "" {
		t.Fatal("Create 后 RawToken 为空 ⇒ 签发侧拿不到明文，重置链接无从生成")
	}

	var stored string
	if err := db.Table("password_reset_tokens").Select("token").
		Where("id = ?", tok.ID).Scan(&stored).Error; err != nil {
		t.Fatalf("读回 token 列失败: %v", err)
	}
	if stored != sha256HexReset(raw) {
		t.Errorf("库里那列不是明文的 SHA-256：%d 字符 %q", len(stored), stored[:min(24, len(stored))])
	}
	if strings.Contains(stored, raw) {
		t.Error("库里那列仍含明文")
	}

	svc := &PasswordResetService{tokenRepo: repository.NewPasswordResetTokenRepository(db)}
	ctx := context.Background()

	if got, err := svc.ValidateResetToken(ctx, raw); err != nil {
		t.Fatalf("用户按邮件链接（明文）校验被拒: %v ⇒ 哈希化把合法链路弄断了", err)
	} else if got.UserID != "88" {
		t.Errorf("校验回来的 user_id=%q，期望 88", got.UserID)
	}

	if _, err := svc.ValidateResetToken(ctx, stored); err != ErrInvalidResetToken {
		t.Errorf("库内值（泄露面）直接重放的结果=%v，期望 %v ⇒ 哈希没挡住偷到库的人", err, ErrInvalidResetToken)
	}
}

// 邮件链接那一头为什么只能静态锁：RequestPasswordReset 把明文拼进 URL 后交给
// EmailService.Send，本包没有 SMTP 桩，行为腿看不到发出去的那串字符。
// 这条锁只覆盖"用哪个字段"这一个决定；它绿着不代表链接可点（可点性由上面的往返腿管）。
func TestResetEmailLinkUsesRawTokenNotStoredHash(t *testing.T) {
	src, err := os.ReadFile("password_reset.go")
	if err != nil {
		t.Fatalf("读 password_reset.go 失败: %v", err)
	}
	if !strings.Contains(string(src), "reset-password?token=%s") {
		t.Fatal("重置链接拼装形状变了，本腿的判据需要一起改")
	}
	line := ""
	for _, l := range strings.Split(string(src), "\n") {
		if strings.Contains(l, "reset-password?token=%s") {
			line = l
		}
	}
	if !strings.Contains(line, "resetToken.RawToken") {
		t.Errorf("重置链接没用 RawToken（用了库里那个哈希的话，用户点开的链接永远无效）: %s", strings.TrimSpace(line))
	}
	if strings.Contains(line, "resetToken.Token") {
		t.Errorf("重置链接仍引用库里那列: %s", strings.TrimSpace(line))
	}
}
