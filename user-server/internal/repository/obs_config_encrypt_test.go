package repository

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/secrets"
)

// 本文件锁 obs_config 凭据加密契约（AUD-03 / 迁移 v3_88_0）：
// 写出口落 enc:v1: 密文、读出口归一成明文、存量明文行仍可读、
// 调用方内存快照不被写侧加密污染。断言形态与 config_param_audit_encrypt_test.go
// 同源：不依赖 MASTER_KEY 是否就绪也能跑（未就绪时 Encrypt 降级明文），
// 但「就绪时落库必为密文」由 ensureSecretsReady 真正执行。

func setupObsConfigEncryptRepo(t *testing.T) (*obsConfigRepo, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.ObsConfig{})
	return NewObsConfigRepositoryWithDB(db).(*obsConfigRepo), context.Background()
}

// ① 就绪时落库必为密文 ② 读出来等于写进去的 ③ 内存快照不被污染。
func TestObsConfigCredentialsEncryptedAtRestAndDecryptedOnRead(t *testing.T) {
	ensureSecretsReady(t)
	repo, ctx := setupObsConfigEncryptRepo(t)
	const ak = "ak-live-plaintext-should-never-persist"
	const sk = "sk-live-plaintext-should-never-persist"

	cfg := &model.ObsConfig{
		Name:      "aud-03",
		Provider:  model.ObsProviderQiniu,
		AccessKey: ak,
		SecretKey: sk,
		Bucket:    "bucket-aud",
		Endpoint:  "https://s3.example.com",
	}
	if err := repo.Create(ctx, cfg); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if cfg.AccessKey != ak || cfg.SecretKey != sk {
		t.Fatalf("写出口污染了调用方内存快照: ak=%q sk=%q", cfg.AccessKey, cfg.SecretKey)
	}

	var raw model.ObsConfig
	if err := repo.db.Where("id = ?", cfg.ID).First(&raw).Error; err != nil {
		t.Fatalf("读原始行失败: %v", err)
	}
	if !secrets.IsCiphertextFormat(raw.AccessKey) || !secrets.IsCiphertextFormat(raw.SecretKey) {
		t.Fatalf("secrets 已就绪但凭据列仍是明文: ak=%q sk=%q", raw.AccessKey, raw.SecretKey)
	}
	if strings.Contains(raw.AccessKey, ak) || strings.Contains(raw.SecretKey, sk) {
		t.Fatal("密文里仍能搜到凭据明文")
	}

	got, err := repo.GetByID(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if got.AccessKey != ak || got.SecretKey != sk {
		t.Fatalf("读出口往返不一致: ak=%q sk=%q", got.AccessKey, got.SecretKey)
	}

	// 更新腿：部分更新后落库仍密文化，读取仍回新明文。
	const ak2, sk2 = "ak-rotated", "sk-rotated"
	got.AccessKey, got.SecretKey = ak2, sk2
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	var raw2 model.ObsConfig
	if err := repo.db.Where("id = ?", cfg.ID).First(&raw2).Error; err != nil {
		t.Fatalf("读更新后原始行失败: %v", err)
	}
	if !secrets.IsCiphertextFormat(raw2.AccessKey) || !secrets.IsCiphertextFormat(raw2.SecretKey) {
		t.Fatalf("Update 后凭据列仍是明文: ak=%q", raw2.AccessKey)
	}
	if strings.Contains(raw2.AccessKey, ak2) || strings.Contains(raw2.SecretKey, sk2) {
		t.Fatal("更新后的密文里仍能搜到凭据明文")
	}
	got2, err := repo.GetByID(ctx, cfg.ID)
	if err != nil {
		t.Fatalf("更新后 GetByID 失败: %v", err)
	}
	if got2.AccessKey != ak2 || got2.SecretKey != sk2 {
		t.Fatalf("更新后往返不一致: ak=%q sk=%q", got2.AccessKey, got2.SecretKey)
	}

	// 列表出口同样归一。
	list, _, err := repo.GetList(ctx, 1, 20, "", "")
	if err != nil {
		t.Fatalf("GetList 失败: %v", err)
	}
	if len(list) != 1 || list[0].AccessKey != ak2 || list[0].SecretKey != sk2 {
		t.Fatalf("GetList 读出口未归一: len=%d ak=%q", len(list), list[0].AccessKey)
	}
}

// 存量明文行（迁移回填跳过/未就绪时写入）经读出口原样透传，
// dbencrypt.Decrypt 对非 enc:v1: 前缀不改写。
func TestObsConfigLegacyPlaintextRowStillReadable(t *testing.T) {
	repo, ctx := setupObsConfigEncryptRepo(t)
	legacy := &model.ObsConfig{
		Name:      "legacy-plain",
		Provider:  model.ObsProviderAWS,
		AccessKey: "AKIA-legacy-plain",
		SecretKey: "legacy-sk-plain",
		Bucket:    "bucket-legacy",
	}
	if err := repo.db.Create(legacy).Error; err != nil {
		t.Fatalf("播种存量明文行失败: %v", err)
	}

	got, err := repo.GetByID(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if got.AccessKey != "AKIA-legacy-plain" || got.SecretKey != "legacy-sk-plain" {
		t.Fatalf("存量明文行读取被改写: ak=%q sk=%q", got.AccessKey, got.SecretKey)
	}
}
