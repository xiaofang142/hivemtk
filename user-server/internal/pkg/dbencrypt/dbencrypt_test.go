package dbencrypt

import (
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/secrets"
)

// initMasterKey 用指定密钥重置全局 secrets 状态并初始化。
func initMasterKey(t *testing.T, key string) {
	t.Helper()
	secrets.ResetForTest()
	t.Cleanup(secrets.ResetForTest)
	if key == "" {
		os.Unsetenv("MASTER_KEY")
	} else {
		t.Setenv("MASTER_KEY", key)
	}
	_ = secrets.InitFromEnv()
}

// TestEncryptDecryptRoundTrip 验证加解密往返一致，且密文带 enc:v1: 前缀。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	initMasterKey(t, strings.Repeat("a", 32))

	for _, plain := range []string{"192.168.1.1", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0)", "中文-UA/1.0"} {
		enc := Encrypt(plain)
		if enc == plain {
			t.Fatalf("Encrypt(%q) 应产生密文而非原样返回", plain)
		}
		if !strings.HasPrefix(enc, ciphertextPrefix) {
			t.Fatalf("密文格式缺少前缀: %q", enc)
		}
		if got := Decrypt(enc); got != plain {
			t.Fatalf("roundtrip 失败: want %q got %q", plain, got)
		}
	}
}

// TestEncryptEmptyPassThrough 空串原样返回，不产生密文。
func TestEncryptEmptyPassThrough(t *testing.T) {
	initMasterKey(t, strings.Repeat("a", 32))

	if got := Encrypt(""); got != "" {
		t.Fatalf("Encrypt(\"\") = %q, want 空串", got)
	}
	if got := Decrypt(""); got != "" {
		t.Fatalf("Decrypt(\"\") = %q, want 空串", got)
	}
}

// TestEncryptIdempotent 已加密内容再次 Encrypt 不二次加密。
func TestEncryptIdempotent(t *testing.T) {
	initMasterKey(t, strings.Repeat("a", 32))

	once := Encrypt("10.0.0.1")
	twice := Encrypt(once)
	if twice != once {
		t.Fatalf("Encrypt 应幂等: once=%q twice=%q", once, twice)
	}
}

// TestDecryptPlaintextPassThrough 存量明文（无 enc:v1: 前缀）Decrypt 原样返回，向后兼容。
func TestDecryptPlaintextPassThrough(t *testing.T) {
	initMasterKey(t, strings.Repeat("a", 32))

	for _, plain := range []string{"192.168.1.1", "curl/8.0", "not-base64-at-all"} {
		if got := Decrypt(plain); got != plain {
			t.Fatalf("Decrypt(明文 %q) = %q, want 原样返回", plain, got)
		}
	}
}

// TestDecryptBrokenCiphertextPassThrough 密文损坏时原样返回而不 panic/清空。
func TestDecryptBrokenCiphertextPassThrough(t *testing.T) {
	initMasterKey(t, strings.Repeat("a", 32))

	broken := ciphertextPrefix + "###not-base64###"
	if got := Decrypt(broken); got != broken {
		t.Fatalf("Decrypt(损坏密文) = %q, want 原样返回", got)
	}
}

// TestFallbackWhenNotInitialized MASTER_KEY 缺失时 Encrypt 降级明文，Decrypt 密文原样返回。
func TestFallbackWhenNotInitialized(t *testing.T) {
	initMasterKey(t, "")
	if secrets.Ready() {
		t.Fatal("MASTER_KEY 缺失时 secrets 不应 Ready")
	}

	plain := "172.16.0.9"
	if got := Encrypt(plain); got != plain {
		t.Fatalf("未初始化时 Encrypt 应降级明文: got %q", got)
	}
	cipherText := ciphertextPrefix + "YWJjZGVmZ2hpamtsbW5vcA=="
	if got := Decrypt(cipherText); got != cipherText {
		t.Fatalf("未初始化时 Decrypt 应原样返回密文: got %q", got)
	}
}
