package repository

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/secrets"
)

// 本文件锁的是 OPT-SEC-04 残项的契约：config_params 的变更审计里存着 API Key 一类的
// 敏感参数（分组见 config_param_group_key_unique 的复合唯一键），审计表明文留痕等于
// 把历史密钥全量抄进一张运维只读视图。
//
// 断言写成「不依赖 MASTER_KEY 是否就绪」的形式：Encrypt 在未就绪时按设计降级明文
// （只 warn 一次，绝不阻断写入），所以「落库必为密文」不是本仓的契约；真正的契约是
// ① 读出来永远等于写进去的 ② 就绪时落库不得是明文 ③ 存量明文行仍可读。

func setupConfigParamAuditRepo(t *testing.T) (*ConfigParamRepository, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	return NewConfigParamRepository(db), context.Background()
}

// ensureSecretsReady 在测试进程里把 AEAD 装起来，让「就绪时落库必须是密文」这条
// 断言真正被执行，而不是因环境缺 MASTER_KEY 被整条跳过 —— 那样的测试等于没写。
// 用固定 32 字节测试密钥；secrets.ResetForTest 让每个用例自足，不依赖用例执行顺序。
func ensureSecretsReady(t *testing.T) {
	t.Helper()
	const testKey = "0123456789abcdef0123456789abcdef"
	secrets.ResetForTest()
	t.Setenv("MASTER_KEY", testKey)
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatalf("secrets.InitFromEnv 失败: %v", err)
	}
	if !secrets.Ready() {
		t.Fatal("注入测试密钥后 secrets 仍未就绪")
	}
}

func seedConfigParam(t *testing.T, repo *ConfigParamRepository, ctx context.Context, key, value string) {
	t.Helper()
	if err := dbCreateParam(repo, ctx, key, value); err != nil {
		t.Fatalf("播种参数 %s 失败: %v", key, err)
	}
}

func dbCreateParam(repo *ConfigParamRepository, ctx context.Context, key, value string) error {
	return dbCreateParamWithDefault(repo, ctx, key, value, value)
}

func dbCreateParamWithDefault(repo *ConfigParamRepository, ctx context.Context, key, value, def string) error {
	return repo.db.Create(&model.ConfigParam{
		Group:        "secrets",
		Key:          key,
		Name:         key,
		ValueType:    "string",
		Value:        value,
		DefaultValue: def,
	}).Error
}

// ① 读出来等于写进去的；② 就绪时落库不是明文。
func TestConfigParamAuditValuesRoundTripAndCipherWhenReady(t *testing.T) {
	ensureSecretsReady(t)
	repo, ctx := setupConfigParamAuditRepo(t)
	const secretValue = "sk-live-plaintext-should-never-persist"

	seedConfigParam(t, repo, ctx, "llm_api_key", secretValue)
	if err := repo.UpdateValue(ctx, "secrets", "llm_api_key", "sk-live-rotated", 7); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	var stored model.ConfigParamAuditLog
	if err := repo.db.Where("param_key = ?", "llm_api_key").First(&stored).Error; err != nil {
		t.Fatalf("读审计行失败: %v", err)
	}
	if secrets.Ready() {
		if !secrets.IsCiphertextFormat(stored.OldValue) || !secrets.IsCiphertextFormat(stored.NewValue) {
			t.Fatalf("secrets 已就绪但审计行仍是明文: old=%q new=%q", stored.OldValue, stored.NewValue)
		}
		if strings.Contains(stored.OldValue, secretValue) {
			t.Fatal("密文里仍能搜到明文密钥")
		}
	}

	logs, err := repo.AuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("AuditLogs 失败: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("期望 1 条审计行，实际 %d 条", len(logs))
	}
	if logs[0].OldValue != secretValue {
		t.Fatalf("old_value 往返不一致: got %q want %q", logs[0].OldValue, secretValue)
	}
	if logs[0].NewValue != "sk-live-rotated" {
		t.Fatalf("new_value 往返不一致: got %q", logs[0].NewValue)
	}
	if logs[0].Action != "update" || logs[0].ActorID != 7 {
		t.Fatalf("非敏感字段被误改: action=%q actor=%d", logs[0].Action, logs[0].ActorID)
	}
}

// 重置与整组重置走的是另外两条写入分支，三条都必须加密，否则从 reset 动作就能读到明文密钥。
func TestConfigParamAuditEncryptsResetAndBulkResetBranches(t *testing.T) {
	ensureSecretsReady(t)
	repo, ctx := setupConfigParamAuditRepo(t)

	if err := dbCreateParam(repo, ctx, "llm_api_key", "sk-live-original"); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	if err := dbCreateParamWithDefault(repo, ctx, "llm_base_url", "https://api.example.com", "https://default.example.com"); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	if err := repo.ResetToDefault(ctx, "secrets", "llm_api_key", 9); err != nil {
		t.Fatalf("ResetToDefault 失败: %v", err)
	}
	if err := repo.BulkResetGroup(ctx, "secrets", 9); err != nil {
		t.Fatalf("BulkResetGroup 失败: %v", err)
	}

	// 判据打在**未解密的原始行**上：AuditLogs 是读取出口，返回的是解密后的明文，
	// 拿它去断言「是不是密文」必然失败。
	var raw []model.ConfigParamAuditLog
	if err := repo.db.Order("created_at DESC").Find(&raw).Error; err != nil {
		t.Fatalf("读原始审计行失败: %v", err)
	}
	if len(raw) < 2 {
		t.Fatalf("期望 reset 与 bulk_reset 两条审计行，实际 %d 条", len(raw))
	}
	actions := map[string]bool{}
	for _, l := range raw {
		actions[l.Action] = true
		if !secrets.IsCiphertextFormat(l.OldValue) || !secrets.IsCiphertextFormat(l.NewValue) {
			t.Fatalf("action=%s 未加密: old=%q new=%q", l.Action, l.OldValue, l.NewValue)
		}
	}
	if !actions["reset"] || !actions["bulk_reset"] {
		t.Fatalf("两条写入分支未都被覆盖: actions=%v", actions)
	}

	// 顺带确认读取出口仍能还原明文，否则加密就把审计列表接口废掉了。
	logs, err := repo.AuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("AuditLogs 失败: %v", err)
	}
	for _, l := range logs {
		if strings.Contains(l.OldValue, "enc:v1:") {
			t.Fatalf("读取出口未解密: action=%s old=%q", l.Action, l.OldValue)
		}
	}
}

// ③ 存量明文行仍可读：dbencrypt.Decrypt 对非 enc:v1: 前缀原样返回，
// 这条一旦破了，任何一次密钥轮换都会让整张审计列表接口 500。
func TestConfigParamAuditReadsLegacyPlaintextRows(t *testing.T) {
	repo, ctx := setupConfigParamAuditRepo(t)

	if err := repo.db.Create(&model.ConfigParamAuditLog{
		ParamKey: "llm_api_key",
		OldValue: "legacy-plaintext",
		NewValue: "legacy-plaintext-new",
		Action:   "update",
		ActorID:  1,
	}).Error; err != nil {
		t.Fatalf("播种存量明文行失败: %v", err)
	}

	logs, err := repo.AuditLogs(ctx, 10)
	if err != nil {
		t.Fatalf("AuditLogs 失败: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("期望 1 条，实际 %d 条", len(logs))
	}
	if logs[0].OldValue != "legacy-plaintext" || logs[0].NewValue != "legacy-plaintext-new" {
		t.Fatalf("存量明文行被破坏: %+v", logs[0])
	}
}
