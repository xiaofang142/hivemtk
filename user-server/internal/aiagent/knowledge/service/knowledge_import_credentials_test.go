package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/secrets"
)

func TestKnowledgeImportCredentials_RepositoryBoundary(t *testing.T) {
	source, err := os.ReadFile("knowledge_import_credentials.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), `"hivemtk-user/internal/repository"`) {
		t.Fatal("知识service不得跨域直接引用根仓储")
	}
}

func TestKnowledgeImportCredentials_TenantEncryptionAndInput(t *testing.T) {
	database := testutil.NewTestDB(t, &model.SystemConfigKV{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	secrets.ResetForTest()
	t.Cleanup(secrets.ResetForTest)
	t.Setenv("MASTER_KEY", strings.Repeat("k", 32))
	if err := secrets.InitFromEnv(); err != nil {
		t.Fatal(err)
	}
	ctx := tenant.OnlyTenant(context.Background(), 18)
	original := &KnowledgeImportCredentials{FeishuAppID: "synthetic-id", FeishuAppSecret: "synthetic-feishu", NotionAPIKey: "synthetic-notion"}
	if err := SaveKnowledgeImportCredentials(ctx, original); err != nil {
		t.Fatal(err)
	}
	var row model.SystemConfigKV
	if err := database.WithContext(ctx).Where("tenant_id = ? AND key = ?", 18, kvKnowledgeImportCredentials).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	var encrypted KnowledgeImportCredentials
	if err := json.Unmarshal([]byte(row.Value), &encrypted); err != nil {
		t.Fatal(err)
	}
	if !secrets.IsCiphertextFormat(encrypted.FeishuAppSecret) || !secrets.IsCiphertextFormat(encrypted.NotionAPIKey) {
		t.Fatal("两个秘密字段必须密文落库")
	}
	loaded, err := LoadKnowledgeImportCredentials(ctx)
	if err != nil || loaded == nil || *loaded != *original {
		t.Fatal("正确租户必须透明读取原凭证")
	}
	if original.FeishuAppSecret != "synthetic-feishu" {
		t.Fatal("保存不能改写调用方配置")
	}
	other, err := LoadKnowledgeImportCredentials(tenant.OnlyTenant(context.Background(), 19))
	if err != nil || other == nil || *other != (KnowledgeImportCredentials{}) {
		t.Fatal("另一租户不能借用已存凭证")
	}
	t.Run("空配置", func(t *testing.T) {
		defer func() {
			if recover() != nil {
				t.Error("空配置必须返回错误而非panic")
			}
		}()
		if err := SaveKnowledgeImportCredentials(ctx, nil); err == nil {
			t.Fatal("空配置不得保存")
		}
	})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"缺上下文", nil}, {"缺租户", context.Background()},
		{"系统豁免", tenant.WithSystemScope(ctx)}, {"取消", canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := SaveKnowledgeImportCredentials(tc.ctx, original); err == nil {
				t.Fatal("未确认上下文不能写凭证")
			}
			if _, err := LoadKnowledgeImportCredentials(tc.ctx); err == nil {
				t.Fatal("未确认上下文不能读凭证")
			}
		})
	}
}
