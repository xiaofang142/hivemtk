package main

// specs/017①：服务端批准快照 ↔ 本地 manifest 交叉核对（verifySpecConsistency）。
// 拦"本地工作区滞留旧版本资产照旧组装"——完整性校验（verifyAssetAt）管"装完被改"，
// 这里管"装进来的就是旧版"。

import (
	"strings"
	"testing"
)

func specConsistencyManifest(hash, ver, entry string) []byte {
	return []byte(`{"hivemtk":{"entry":"` + entry + `","asset_ver":"` + ver + `","script_hash":"` + hash + `"}}`)
}

func TestVerifySpecConsistency(t *testing.T) {
	const hash = "abc123"
	const ver = "3"
	const entry = "tools/main.js"
	manifest := specConsistencyManifest(hash, ver, entry)

	t.Run("三项全空跳过(旧服务端/重放旧spec不误杀)", func(t *testing.T) {
		if err := verifySpecConsistency(manifest, taskSpec{}); err != nil {
			t.Fatalf("空 spec 应跳过核对，实得 %v", err)
		}
	})

	t.Run("同源放行", func(t *testing.T) {
		spec := taskSpec{ScriptHash: hash, AssetVersion: ver, Entry: entry}
		if err := verifySpecConsistency(manifest, spec); err != nil {
			t.Fatalf("同源 spec 应放行，实得 %v", err)
		}
	})

	t.Run("hash不一致拒绝(滞留旧版本资产)", func(t *testing.T) {
		spec := taskSpec{ScriptHash: strings.Repeat("f", 64)}
		err := verifySpecConsistency(manifest, spec)
		if err == nil || !strings.Contains(err.Error(), "批准 hash 不一致") {
			t.Fatalf("应报批准 hash 不一致，实得 %v", err)
		}
	})

	t.Run("版本不一致拒绝", func(t *testing.T) {
		spec := taskSpec{AssetVersion: "2"}
		err := verifySpecConsistency(manifest, spec)
		if err == nil || !strings.Contains(err.Error(), "资产版本不一致") {
			t.Fatalf("应报资产版本不一致，实得 %v", err)
		}
	})

	t.Run("入口不一致拒绝", func(t *testing.T) {
		spec := taskSpec{Entry: "tools/other.js"}
		err := verifySpecConsistency(manifest, spec)
		if err == nil || !strings.Contains(err.Error(), "入口不一致") {
			t.Fatalf("应报入口不一致，实得 %v", err)
		}
	})

	t.Run("manifest损坏拒绝", func(t *testing.T) {
		spec := taskSpec{ScriptHash: hash}
		if err := verifySpecConsistency([]byte("not-json{"), spec); err == nil {
			t.Fatal("manifest 解析失败应拒绝")
		}
	})
}
