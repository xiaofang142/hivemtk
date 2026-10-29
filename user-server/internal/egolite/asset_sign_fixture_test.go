package egolite

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// testdata/signed_official_manifest.json + official_trust.pub 是 hivemtk-platform
// `cmd/ego-asset-signer`（S-6 平台侧签发 CLI）产出的**真签名单**——本文件把它钉成
// 跨仓契约：CLI 的字节规范（SHA-256 over raw-bytes-with-blank-signature）与
// 服务端 VerifyAssetSignature 必须互认。fixture 换了就得同步换（钥匙对不可再生），
// 改动任何一端规范而不动另一端，这里最先红。

const (
	fixtureSignedManifest = "testdata/signed_official_manifest.json"
	fixtureOfficialTrust  = "testdata/official_trust.pub"
)

func loadFixturePubKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	b, err := os.ReadFile(fixtureOfficialTrust)
	if err != nil {
		t.Fatalf("读 fixture trust 公钥 失败: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, err := base64.StdEncoding.DecodeString(line); err == nil && len(k) == ed25519.PublicKeySize {
			return ed25519.PublicKey(k)
		}
	}
	t.Fatal("fixture trust 公钥 中没有有效公钥")
	return nil
}

func TestCrossRepoSignedManifestVerifies(t *testing.T) {
	raw, err := os.ReadFile(fixtureSignedManifest)
	if err != nil {
		t.Fatalf("读 fixture 失败: %v", err)
	}
	pub := loadFixturePubKey(t)
	if err := VerifyAssetSignature(raw, pub); err != nil {
		t.Fatalf("platform 签发 CLI 产出的清单必须被服务端验签认过（跨仓契约）: %v", err)
	}
}

func TestCrossRepoSignedManifestTamperIsRejected(t *testing.T) {
	raw, err := os.ReadFile(fixtureSignedManifest)
	if err != nil {
		t.Fatal(err)
	}
	pub := loadFixturePubKey(t)
	// 篡改 1：换 domains（业务面变化必须被签名抓住）
	tampered := strings.Replace(string(raw), `"example.com"`, `"evil.example.net"`, 1)
	if tampered == string(raw) {
		t.Fatal("篡改替换未生效（fixture 形状变了？）")
	}
	if err := VerifyAssetSignature([]byte(tampered), pub); err == nil {
		t.Fatal("篡改 domains 后验签必须失败")
	}
	// 篡改 2：错配公钥（另一把钥匙验不过 = 来源认证在起作用）
	_, wrongPriv, err := ed25519.GenerateKey(nil)
	_ = wrongPriv
	if err != nil {
		t.Fatal(err)
	}
	wrongPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAssetSignature(raw, wrongPub); err == nil {
		t.Fatal("非信任公钥验签必须失败（fail-close）")
	}
	_ = wrongPriv
}
