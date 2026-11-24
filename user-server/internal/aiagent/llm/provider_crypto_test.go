package llm

import (
	"testing"

	"hivemtk-user/internal/secrets"
)

const testMasterKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestProviderCrypto_RoundTrip(t *testing.T) {
	secrets.ResetForTest()
	defer secrets.ResetForTest()
	if err := secrets.InitFromEnv(); err == nil {
		t.Skip("MASTER_KEY 不应在此环境存在")
	}
	t.Setenv("MASTER_KEY", testMasterKey)
	secrets.ResetForTest()
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatalf("init with test key: %v", err)
	}

	stored := encryptAPIKeyForStorage("sk-test-123456")
	if stored == "sk-test-123456" {
		t.Fatal("ready 状态下应加密存储")
	}
	if !secrets.IsCiphertextFormat(stored) {
		t.Fatalf("应为 enc:v1: 密文格式, got %s", stored)
	}
	if got := decryptAPIKeyForUse(stored); got != "sk-test-123456" {
		t.Fatalf("读库应还原明文, got %s", got)
	}
}

func TestProviderCrypto_Idempotent(t *testing.T) {
	t.Setenv("MASTER_KEY", testMasterKey)
	secrets.ResetForTest()
	defer secrets.ResetForTest()
	_ = secrets.InitFromEnv()

	once := encryptAPIKeyForStorage("sk-abc")
	twice := encryptAPIKeyForStorage(once)
	if once != twice {
		t.Fatalf("已密文不应二次加密: %s vs %s", once, twice)
	}
}

func TestProviderCrypto_PlaintextFallbackWhenNotReady(t *testing.T) {
	secrets.ResetForTest()
	defer secrets.ResetForTest()

	got := encryptAPIKeyForStorage("sk-plain")
	if got != "sk-plain" {
		t.Fatalf("未就绪应明文直存, got %s", got)
	}
	if decryptAPIKeyForUse("sk-plain") != "sk-plain" {
		t.Fatal("明文读库应原样返回")
	}
}

func TestProviderCrypto_LegacyPlaintextStillReadable(t *testing.T) {
	t.Setenv("MASTER_KEY", testMasterKey)
	secrets.ResetForTest()
	defer secrets.ResetForTest()
	_ = secrets.InitFromEnv()

	if got := decryptAPIKeyForUse("sk-legacy-plaintext"); got != "sk-legacy-plaintext" {
		t.Fatalf("存量明文应直读, got %s", got)
	}
}

func TestProviderCrypto_RowRoundTrip(t *testing.T) {
	t.Setenv("MASTER_KEY", testMasterKey)
	secrets.ResetForTest()
	defer secrets.ResetForTest()
	_ = secrets.InitFromEnv()

	pc := ProviderConfig{Name: "p1", APIKey: "sk-s" + "ecret", BaseURL: "http://x", Model: "m"}
	row := providerConfigToRow(pc)
	if row.APIKey == "sk-secret" {
		t.Fatal("落库行应为密文")
	}
	if !secrets.IsCiphertextFormat(row.APIKey) {
		t.Fatalf("落库行应为 enc:v1: 格式, got %s", row.APIKey)
	}

	if got := decryptAPIKeyForUse(row.APIKey); got != "sk-secret" {
		t.Fatalf("读库应还原, got %s", got)
	}
}
