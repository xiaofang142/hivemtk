package service

// llm_provider_crud_test.go P1-4 验收：提供商增删改 + 凭据不明文落库。
//
// 为什么补这一组：`provider_crypto_test` 只验了「加密函数与单行落库」，
// `llm_source_preserve_test` 只验了 source/enabled 语义且**在无库时会静默通过**。
// 端到端「建 → 列表可见 → 改 → 删」这条链在真实库上没有任何断言，
// 而它正是本条的 AC 原文。

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/secrets"
)

func ensureLLMTestSecrets(t *testing.T) {
	t.Helper()
	const testKey = "0123456789abcdef0123456789abcdef"
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", testKey)
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatalf("secrets.InitFromEnv 失败: %v", err)
	}
	t.Cleanup(secrets.ResetForTest)
}

func TestLLMProviderCRUD_EncryptedAtRestAndPersisted(t *testing.T) {
	ensureLLMTestSecrets(t)
	db := testutil.NewTestDB(t, &model.LLMProvider{}, &model.LLMRoutingRule{})
	llm.AttachAuditDB(db)
	t.Cleanup(func() { llm.AttachAuditDB(nil) }) // 复原：悬着的 auditDB 指向已关池，后续用例拿到 "database is closed"

	d := llm.NewDispatcher(llm.NewLLMService())
	svc := &LLMRoutingService{dispatcher: d}
	ctx := context.Background()

	const plainKey = "sk-live-acme-123"
	if err := svc.CreateModel(ctx, LLMProviderInfo{
		Name: "acme", DisplayName: "Acme", BaseURL: "https://api.acme.com/v1",
		Model: "m1", APIKey: plainKey, Enabled: true, QualityScore: 0.7, MaxRPM: 30,
	}); err != nil {
		t.Fatalf("CreateModel 失败: %v", err)
	}

	// ① 落库且凭据为密文
	var row model.LLMProvider
	if err := db.Where("name = ?", "acme").First(&row).Error; err != nil {
		t.Fatalf("行未落库: %v", err)
	}
	if !strings.HasPrefix(row.APIKey, "enc:v1:") {
		t.Fatalf("api_key 未加密: %q", row.APIKey)
	}
	if strings.Contains(row.APIKey, plainKey) {
		t.Fatal("密文里出现明文片段")
	}
	dec, err := secrets.DecryptString(row.APIKey)
	if err != nil || dec != plainKey {
		t.Errorf("解密失败: got %q err=%v", dec, err)
	}

	// ② 列表可见
	names := map[string]bool{}
	for _, m := range svc.ListModels(ctx) {
		names[m.Name] = true
	}
	if !names["acme"] {
		t.Errorf("ListModels 不含新建 provider: %v", names)
	}

	// ③ 修改：只发 model=空 ⇒ 保留原值；改 DisplayName 生效且 api_key 不变
	if err := svc.UpdateModel(ctx, "acme", LLMProviderInfo{Name: "acme", DisplayName: "Acme 改名", Enabled: true}); err != nil {
		t.Fatalf("UpdateModel 失败: %v", err)
	}
	var after model.LLMProvider
	if err := db.Where("name = ?", "acme").First(&after).Error; err != nil {
		t.Fatalf("更新后读行失败: %v", err)
	}
	if after.DisplayName != "Acme 改名" {
		t.Errorf("display_name 未更新: %q", after.DisplayName)
	}
	if after.Model != "m1" {
		t.Errorf("model 空值应保留原值，实际 %q", after.Model)
	}
	// 密文不可按字节比：AES-GCM 每次加密 nonce 不同，同一明文会得到不同密文。
	// 判据取语义：解密后仍是原 Key，且未变成空/新值。
	decAfter, derr := secrets.DecryptString(after.APIKey)
	if derr != nil || decAfter != plainKey {
		t.Errorf("未传新 Key 时应保留原值（解密后）: got %q err=%v", decAfter, derr)
	}

	// ④ 删除
	if err := svc.DeleteModel(ctx, "acme"); err != nil {
		t.Fatalf("DeleteModel 失败: %v", err)
	}
	var n int64
	db.Model(&model.LLMProvider{}).Where("name = ?", "acme").Count(&n)
	if n != 0 {
		t.Errorf("删除后仍有 %d 行", n)
	}
	if svc.dispatcher.GetProvider("acme") != nil {
		t.Error("删除后内存态仍存在 provider")
	}
}

func TestLLMProviderCRUD_ClearKeySentinel(t *testing.T) {
	ensureLLMTestSecrets(t)
	db := testutil.NewTestDB(t, &model.LLMProvider{}, &model.LLMRoutingRule{})
	llm.AttachAuditDB(db)
	t.Cleanup(func() { llm.AttachAuditDB(nil) }) // 复原：悬着的 auditDB 指向已关池，后续用例拿到 "database is closed"
	d := llm.NewDispatcher(llm.NewLLMService())
	svc := &LLMRoutingService{dispatcher: d}
	ctx := context.Background()

	if err := svc.CreateModel(ctx, LLMProviderInfo{
		Name: "clearable", BaseURL: "https://api.x.com/v1", Model: "m", APIKey: "sk-1", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateModel 失败: %v", err)
	}
	if err := svc.UpdateModel(ctx, "clearable", LLMProviderInfo{
		Name: "clearable", Enabled: true, APIKey: APIKeyClearSentinel,
	}); err != nil {
		t.Fatalf("UpdateModel 失败: %v", err)
	}
	var row model.LLMProvider
	if err := db.Where("name = ?", "clearable").First(&row).Error; err != nil {
		t.Fatalf("读行失败: %v", err)
	}
	if row.APIKey != "" {
		t.Errorf("清空标记未生效，api_key 仍为 %q", row.APIKey)
	}
}
