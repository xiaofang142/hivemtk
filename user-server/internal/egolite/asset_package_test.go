package egolite

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func sampleAsset(id, entry, script string) InstallableAsset {
	sum := sha256.Sum256([]byte(script))
	return InstallableAsset{
		ID: id, Name: "资产 " + id, Version: 3, Domains: []string{"github.com"},
		Entry: entry, Script: script, ScriptHash: hex.EncodeToString(sum[:]),
		Source: "pack", AssetID: id, AssetVer: "3",
	}
}

// TestBuildAssetPackage_ShapeAndDeterminism 包内形状 + 同一清单两次打包逐字节一致。
func TestBuildAssetPackage_ShapeAndDeterminism(t *testing.T) {
	assets := []InstallableAsset{
		sampleAsset("beta", "tools/b.js", "console.log('b')"),
		sampleAsset("alpha", "tools/a.js", "console.log('a')"),
	}
	p1, err := BuildAssetPackage(assets)
	if err != nil {
		t.Fatalf("打包失败: %v", err)
	}
	p2, err := BuildAssetPackage([]InstallableAsset{assets[1], assets[0]})
	if err != nil {
		t.Fatalf("打包失败: %v", err)
	}
	if !bytes.Equal(p1, p2) {
		t.Fatal("同一清单应产出逐字节相同的包（顺序不确定）")
	}
	zr, err := zip.NewReader(bytes.NewReader(p1), int64(len(p1)))
	if err != nil {
		t.Fatalf("读包失败: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := []string{"alpha/manifest.json", "alpha/tools/a.js", "beta/manifest.json", "beta/tools/b.js"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("条目 = %v，期望 %v", names, want)
	}
	// manifest 里必须带 script_hash（runner 用它与本地比对）
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "manifest.json") {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		var m struct {
			Hivemtk struct {
				ScriptHash string `json:"script_hash"`
			} `json:"hivemtk"`
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("manifest 解析失败: %v", err)
		}
		if m.Hivemtk.ScriptHash == "" {
			t.Fatalf("%s 缺 script_hash", f.Name)
		}
	}
}

// TestBuildAssetPackage_RejectsBadPaths 路径穿越/绝对路径一律拒（两端各判一次）。
func TestBuildAssetPackage_RejectsBadPaths(t *testing.T) {
	cases := []InstallableAsset{
		sampleAsset("../evil", "tools/a.js", "x"),
		sampleAsset("ok", "/etc/passwd", "x"),
		sampleAsset("ok", "tools/../../x.js", "x"),
		sampleAsset("ok", `tools\a.js`, "x"),
	}
	for _, a := range cases {
		if _, err := BuildAssetPackage([]InstallableAsset{a}); err == nil {
			t.Fatalf("非法路径未被拒: id=%q entry=%q", a.ID, a.Entry)
		}
	}
}

// TestBuildAssetPackage_RejectsOversize 单条超限拒（防 zip bomb 的一半；总量上限见常量）。
func TestBuildAssetPackage_RejectsOversize(t *testing.T) {
	big := strings.Repeat("a", AssetPackageMaxEntryBytes+1)
	if _, err := BuildAssetPackage([]InstallableAsset{sampleAsset("big", "tools/big.js", big)}); err == nil {
		t.Fatal("超限条目未被拒")
	}
}

// TestSafeAssetPath 形状判据本身（被上面三组用例复用）。
func TestSafeAssetPath(t *testing.T) {
	ok := []string{"a", "a/b.js", "tools/x-y_z.js"}
	bad := []string{"", "..", "../a", "/a", "a/../b", `a\b`, "a//b", "./a"}
	for _, p := range ok {
		if !safeAssetPath(p) {
			t.Errorf("%q 应为合法", p)
		}
	}
	for _, p := range bad {
		if safeAssetPath(p) {
			t.Errorf("%q 应判非法", p)
		}
	}
}

// ---------- S-6：签名 ----------

func signAsset(t *testing.T, a InstallableAsset, priv ed25519.PrivateKey) InstallableAsset {
	t.Helper()
	a.SigAlg = "ed25519"
	a.PubKeyFpr = PubKeyFingerprint(priv.Public().(ed25519.PublicKey))
	canonical, err := assetManifestCanonical(a)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	a.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))
	return a
}

// TestVerifyPackageSignatures 正确签名通过；篡改脚本/换密钥/缺公钥一律拒。
func TestVerifyPackageSignatures(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	base := sampleAsset("alpha", "tools/a.js", "console.log(1)")
	signed := signAsset(t, base, priv)

	if err := VerifyPackageSignatures([]InstallableAsset{signed}, pub); err != nil {
		t.Fatalf("正确签名被拒: %v", err)
	}
	// 未签名资产在配置了公钥时放行（旧数据兼容）
	if err := VerifyPackageSignatures([]InstallableAsset{base}, pub); err != nil {
		t.Fatalf("未签名资产被拒: %v", err)
	}
	// 篡改脚本
	tampered := signed
	tampered.Script = "console.log('evil')"
	if err := VerifyPackageSignatures([]InstallableAsset{tampered}, pub); err == nil {
		t.Fatal("篡改后仍验过")
	}
	// 换公钥
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := VerifyPackageSignatures([]InstallableAsset{signed}, otherPub); err == nil {
		t.Fatal("换公钥后仍验过")
	}
	// 已签名但未配置公钥 ⇒ 拒（fail-close）
	if err := VerifyPackageSignatures([]InstallableAsset{signed}, nil); err == nil {
		t.Fatal("缺信任公钥时已签名资产应被拒")
	}
}

// TestLoadAssetTrustPubKeyFromEnv 环境变量解析：空 ⇒ nil；非法 ⇒ 错；合法 ⇒ 32 字节。
func TestLoadAssetTrustPubKeyFromEnv(t *testing.T) {
	t.Setenv(AssetTrustPubKeyEnv, "")
	if k, err := LoadAssetTrustPubKeyFromEnv(); err != nil || k != nil {
		t.Fatalf("空配置 = (%v,%v)，期望 (nil,nil)", k, err)
	}
	t.Setenv(AssetTrustPubKeyEnv, "not-base64")
	if _, err := LoadAssetTrustPubKeyFromEnv(); err == nil {
		t.Fatal("非法公钥未报错")
	}
	pub, _, _ := ed25519.GenerateKey(nil)
	t.Setenv(AssetTrustPubKeyEnv, base64.StdEncoding.EncodeToString(pub))
	if k, err := LoadAssetTrustPubKeyFromEnv(); err != nil || !bytes.Equal(k, pub) {
		t.Fatalf("合法公钥解析 = (%v,%v)", k, err)
	}
}
