package egolite

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// 资产签名链测试（决策 8 验收：篡改拒收/正确签名通过/形状校验联动）

func TestAssetSignatureRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)

	build := func(sig string) []byte {
		m := map[string]any{
			"id":      "github-search",
			"version": "1.0.0",
			"domains": []string{"github.com"},
			"hivemtk": map[string]any{
				"entry": "tools/search.js", "compat_min": "0.6.0",
				"writes": []string{}, "sig_alg": "ed25519",
				"signature": sig, "pubkey_fingerprint": PubKeyFingerprint(pub),
			},
		}
		b, _ := json.Marshal(m)
		return b
	}

	// 发布侧：signature="" 的字节 → SHA-256 → 签名 → 回填
	canonical := build("")
	sum := sha256.Sum256(canonical)
	sig := ed25519.Sign(priv, sum[:])
	signed := build(base64.StdEncoding.EncodeToString(sig))

	// 验签侧
	if err := VerifyAssetSignature(signed, pub); err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}

	// 篡改 domains → 拒收
	var m map[string]any
	_ = json.Unmarshal(signed, &m)
	m["domains"] = []string{"evil.com"}
	tampered, _ := json.Marshal(m)
	if err := VerifyAssetSignature(tampered, pub); err == nil {
		t.Fatal("篡改后的包必须拒收")
	}

	// 换公钥 → 拒收
	_, _, _ = ed25519.GenerateKey(nil)
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	if err := VerifyAssetSignature(signed, wrongPub); err == nil {
		t.Fatal("公钥不匹配必须拒收")
	}

	// signature 缺失 → 拒收
	if err := VerifyAssetSignature(canonical, pub); err == nil {
		t.Fatal("无签名必须拒收")
	}
}

func TestAssetSignatureShapeGateChained(t *testing.T) {
	// 验签通过的包还要过 manifest 形状门（缺 domains 拒绝）——两层闸串联
	pub, priv, _ := ed25519.GenerateKey(nil)
	m := map[string]any{
		"id":      "no-domains",
		"domains": []string{},
		"hivemtk": map[string]any{
			"entry": "a.js", "compat_min": "0.6", "sig_alg": "ed25519", "signature": "",
		},
	}
	b, _ := json.Marshal(m)
	sum := sha256.Sum256(b)
	sig := ed25519.Sign(priv, sum[:])
	// 重签（domains 变化后 canonical 不变——signature 是对 b 的）
	m["hivemtk"].(map[string]any)["signature"] = base64.StdEncoding.EncodeToString(sig)
	b2, _ := json.Marshal(m)
	if err := VerifyAssetSignature(b2, pub); err == nil {
		t.Fatal("缺 domains 的包即使签名正确也应被形状门拒收")
	}
}

func TestPubKeyFingerprintStable(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	f1 := PubKeyFingerprint(pub)
	f2 := PubKeyFingerprint(pub)
	if f1 != f2 || len(f1) != 16 {
		t.Fatalf("指纹应为稳定 16 位 hex，got %q", f1)
	}
	for _, r := range f1 {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("指纹应全 hex，got %q", f1)
		}
	}
}
