package service

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/secrets"
)

func initLLMSessionCryptoForTest(t *testing.T, masterKey string) {
	t.Helper()
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", masterKey)
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatalf("初始化测试主密钥失败：%v", err)
	}
	t.Cleanup(func() {
		secrets.ResetForTest()
	})
}

func TestLLMSessionCryptoProtectAndDecrypt(t *testing.T) {
	initLLMSessionCryptoForTest(t, "test-master-key-for-llm-session-32bytes")
	crypto, err := NewLLMSessionCrypto([]byte("test-blind-index-key-for-llm-session-32bytes"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := crypto.Protect("llmsess_canonical_1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := crypto.Protect("llmsess_canonical_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Encrypted) == 0 || bytes.Equal(first.Encrypted, second.Encrypted) {
		t.Fatal("AEAD 必须使用随机 nonce，相同明文不得产生相同密文")
	}
	if len(first.BlindIndexes) != 1 || first.BlindIndexes[0] != second.BlindIndexes[0] {
		t.Fatalf("相同 canonical key 的 blind index 必须稳定：%#v vs %#v", first.BlindIndexes, second.BlindIndexes)
	}
	plain, err := crypto.Decrypt(first.Encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "llmsess_canonical_1" {
		t.Fatalf("解密结果错误：%q", plain)
	}
	other, err := crypto.Protect("llmsess_canonical_2")
	if err != nil {
		t.Fatal(err)
	}
	if other.BlindIndexes[0] == first.BlindIndexes[0] {
		t.Fatal("不同 canonical key 不得产生相同 blind index")
	}
}

func TestLLMSessionCryptoRejectsInvalidCanonicalKey(t *testing.T) {
	initLLMSessionCryptoForTest(t, "test-master-key-for-llm-session-32bytes")
	crypto, err := NewLLMSessionCrypto([]byte("test-blind-index-key-for-llm-session-32bytes"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "   ", " leading", "trailing ", "null\x00key"} {
		if _, err := crypto.Protect(key); !errors.Is(err, ErrInvalidLLMSessionKey) {
			t.Fatalf("非法 canonical key %q 必须拒绝，实际 %v", key, err)
		}
	}
}

func TestLLMSessionCryptoRequiresEncryptionKey(t *testing.T) {
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", "")
	t.Cleanup(secrets.ResetForTest)
	crypto, err := NewLLMSessionCrypto([]byte("test-blind-index-key-for-llm-session-32bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Protect("llmsess_canonical_1"); !errors.Is(err, secrets.ErrMasterKeyMissing) {
		t.Fatalf("缺少主密钥必须 fail-closed，实际 %v", err)
	}
}

func TestLLMSessionCryptoRotationDualWritesBlindIndexes(t *testing.T) {
	initLLMSessionCryptoForTest(t, "test-master-key-for-llm-session-32bytes")
	oldKey := []byte("old-blind-index-key-for-llm-session-32bytes")
	newKey := []byte("new-blind-index-key-for-llm-session-32bytes")
	oldCrypto, err := NewLLMSessionCrypto(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := oldCrypto.Rotate(newKey)
	if err != nil {
		t.Fatal(err)
	}
	oldMaterial, err := oldCrypto.Protect("llmsess_canonical_1")
	if err != nil {
		t.Fatal(err)
	}
	newMaterial, err := rotated.Protect("llmsess_canonical_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(newMaterial.BlindIndexes) != 2 {
		t.Fatalf("轮换期必须双写新旧 blind index：%#v", newMaterial.BlindIndexes)
	}
	if newMaterial.BlindIndexes[0] == oldMaterial.BlindIndexes[0] || newMaterial.BlindIndexes[1] != oldMaterial.BlindIndexes[0] {
		t.Fatalf("轮换顺序错误：new=%#v old=%#v", newMaterial.BlindIndexes, oldMaterial.BlindIndexes)
	}
	if !rotated.VerifyBlindIndex("llmsess_canonical_1", oldMaterial.BlindIndexes[0]) || !rotated.VerifyBlindIndex("llmsess_canonical_1", newMaterial.BlindIndexes[0]) {
		t.Fatal("轮换期必须能验证新旧 blind index")
	}
	if rotated.VerifyBlindIndex("llmsess_other", newMaterial.BlindIndexes[0]) {
		t.Fatal("blind index 不得跨 key 验证通过")
	}
}

func TestLLMSessionCryptoRejectsInvalidBlindKeys(t *testing.T) {
	for _, keys := range [][][]byte{nil, {nil}, {[]byte("short")}, {[]byte("valid-key-with-at-least-32-bytes!!"), nil}} {
		if _, err := NewLLMSessionCrypto(keys...); err == nil {
			t.Fatalf("非法 blind key 集合必须拒绝：%#v", keys)
		}
	}
}

func TestLLMSessionCryptoLogRedaction(t *testing.T) {
	initLLMSessionCryptoForTest(t, "test-master-key-for-llm-session-32bytes")
	crypto, err := NewLLMSessionCrypto([]byte("test-blind-index-key-for-llm-session-32bytes"))
	if err != nil {
		t.Fatal(err)
	}
	key := "llmsess_super_private_canonical_value"
	redacted := crypto.Redact(key)
	if redacted == key || strings.Contains(redacted, key) || strings.Contains(redacted, "super_private") {
		t.Fatalf("日志脱敏不得泄露 canonical key：%q", redacted)
	}
	if !strings.HasPrefix(redacted, "llmsess:redacted:") || len(redacted) <= len("llmsess:redacted:") {
		t.Fatalf("脱敏格式错误：%q", redacted)
	}
}
