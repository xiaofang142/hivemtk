package main

// assets_sign_drill_test.go —— S-6「泄露演练」的可执行形态。
//
// 演练场景：发布者私钥泄露后，攻击者用**另一把**私钥签同一份 manifest。
// 期望：runner 侧 trust.pem 仍是原公钥 ⇒ 验签必红、资产不得落盘。
// 反向：换掉 trust.pem 为新公钥 ⇒ 能验过（证明拒绝来自公钥不匹配而非实现恒红）。

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// signManifest 与 internal/egolite 同一规范：signature 置空后取 SHA-256 再签名。
func signManifest(t *testing.T, raw string, sigValue string, priv ed25519.PrivateKey) string {
	t.Helper()
	canonical := strings.Replace(raw, `"`+sigValue+`"`, `""`, 1)
	sum := sha256.Sum256([]byte(canonical))
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))
}

func writeTrust(t *testing.T, dir string, pub ed25519.PublicKey) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "trust.pem"), []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAssetSignDrill_LeakedKeyIsRejected(t *testing.T) {
	victimPub, _, _ := ed25519.GenerateKey(nil)
	leakedPub, leakedPriv, _ := ed25519.GenerateKey(nil)

	// 攻击者签的 manifest（signature 字段先放占位串，再按规范重算）
	raw := `{"id":"alpha","hivemtk":{"entry":"tools/a.js","script_hash":"x","signature":"SIG"}}`
	sig := signManifest(t, raw, "SIG", leakedPriv)
	signed := strings.Replace(raw, "SIG", sig, 1)

	// 场景①：trust.pem 仍是原公钥 ⇒ 必须拒
	dir := t.TempDir()
	writeTrust(t, dir, victimPub)
	if err := verifyAssetManifestAt(dir, []byte(signed)); err == nil {
		t.Fatal("泄露密钥签的 manifest 被验过 —— 演练失败")
	}
	// 场景②：没有 trust.pem ⇒ 必须拒（fail-close，不是"没钥匙就放行"）
	empty := t.TempDir()
	if err := verifyAssetManifestAt(empty, []byte(signed)); err == nil {
		t.Fatal("缺 trust.pem 时应 fail-close")
	}
	// 场景③（反向对照）：trust.pem 换成攻击者公钥 ⇒ 能验过（证明拒绝来自密钥不匹配）
	writeTrust(t, dir, leakedPub)
	if err := verifyAssetManifestAt(dir, []byte(signed)); err != nil {
		t.Fatalf("换公钥后仍被拒，说明判据恒红而非公钥比对: %v", err)
	}
}
