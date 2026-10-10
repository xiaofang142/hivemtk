package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// mcp_key_admin_test.go 钥匙生命周期（1:N + 状态机 + reveal + 级联）的落库用例。
//
// 与 router 层用例同判据：NewTestDB 连不上测试库时整个包按设计跳过（CI 上 Fatal）。

func newMCPKeySvcDB(t *testing.T) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.MCPCredential{}, &model.MCPCredentialKey{}, &model.MCPCredentialAuditLog{})
	dbutil.SetTestDB(db)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })
}

func seedMCPCredential(t *testing.T, clientID string) *model.MCPCredential {
	t.Helper()
	cred := &model.MCPCredential{ClientID: clientID, Name: "钥匙用例", Enabled: true}
	if err := dbutil.GetDB().Create(cred).Error; err != nil {
		t.Fatalf("铺凭证失败: %v", err)
	}
	return cred
}

func TestMCPKey_IssueRevealStatusDelete(t *testing.T) {
	newMCPKeySvcDB(t)
	ctx := context.Background()
	cred := seedMCPCredential(t, "mcp-keytest-1")
	svc := NewMcpKeyAdminService()
	if !svc.Ready() {
		t.Fatal("钥匙服务未就绪（测试库句柄缺失）")
	}

	// ① 签发：明文口径 wgk_+32hex，双轨落库（hash + 密文），状态 active。
	k1, plain1, err := svc.IssueKey(ctx, cred.ID, "主钥匙", nil)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if !strings.HasPrefix(plain1, "wgk_") || len(plain1) != 4+32 {
		t.Errorf("明文口径应为 wgk_+32hex，got %q(len=%d)", plain1, len(plain1))
	}
	if k1.Status != model.MCPKeyStatusActive || k1.KeyEnc == "" || k1.KeyPrefix == "" {
		t.Errorf("钥匙行形状错: %+v", k1)
	}
	// 第一把钥匙镜像进凭证 legacy 列（回滚窗口可用性）。
	after, _ := svc.GetCredential(ctx, cred.ID)
	if after.APIKeyHash != k1.KeyHash {
		t.Errorf("第一把钥匙的 hash 应镜像进 legacy 列，got %q want %q", after.APIKeyHash, k1.KeyHash)
	}

	// ② reveal：明文可再取出（1:N 之后"显示/复制"的依据）。
	got, err := svc.RevealKey(ctx, cred.ID, k1.ID)
	if err != nil || got != plain1 {
		t.Errorf("reveal 应还原明文，got %q err=%v", got, err)
	}

	// ③ 1:N：同一凭证可再签发，第二把不再镜像 legacy 列。
	k2, _, err := svc.IssueKey(ctx, cred.ID, "副钥匙", nil)
	if err != nil {
		t.Fatalf("第二把签发失败: %v", err)
	}
	after, _ = svc.GetCredential(ctx, cred.ID)
	if after.APIKeyHash != k1.KeyHash {
		t.Errorf("第二把钥匙不应改写 legacy 镜像")
	}

	// ④ 状态机：disable → enable 往返；suspend（欠费/违规）可 enable 恢复；
	// revoke 终态后 enable 必须被拒。
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusDisabled); err != nil {
		t.Fatalf("disable 失败: %v", err)
	}
	if k, _ := svc.GetForCredential(ctx, cred.ID, k2.ID); k.Status != model.MCPKeyStatusDisabled {
		t.Errorf("disable 后状态应 disabled，got %s", k.Status)
	}
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusActive); err != nil {
		t.Fatalf("enable 失败: %v", err)
	}
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusSuspended); err != nil {
		t.Fatalf("suspend 失败: %v", err)
	}
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusActive); err != nil {
		t.Fatalf("suspended 后应可 enable 恢复: %v", err)
	}
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusRevoked); err != nil {
		t.Fatalf("revoke 失败: %v", err)
	}
	if _, err := svc.SetKeyStatus(ctx, cred.ID, k2.ID, model.MCPKeyStatusActive); !errors.Is(err, ErrMCPKeyRevokedTerminal) {
		t.Errorf("吊销后 enable 必须报终态错误，got %v", err)
	}

	// ⑤ 越权/不存在：别的凭证 id 查钥匙 = NotFound（不泄露存在性）。
	other := seedMCPCredential(t, "mcp-keytest-2")
	if _, err := svc.GetForCredential(ctx, other.ID, k1.ID); !errors.Is(err, ErrMCPKeyNotFound) {
		t.Errorf("跨凭证取钥匙应 NotFound，got %v", err)
	}

	// ⑥ 删除镜像基准钥匙 → legacy 列清空（回滚后的旧二进制对该凭证 fail-closed，
	// 好过放行一把已删除的钥匙）。
	if _, err := svc.DeleteKey(ctx, cred.ID, k1.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	after, _ = svc.GetCredential(ctx, cred.ID)
	if after.APIKeyHash != "" {
		t.Errorf("删除镜像钥匙后 legacy 列应清空，got %q", after.APIKeyHash)
	}

	// ⑥b 软删除：行还在（deleted_at 打标）供审计对账；guard 查询自动排除。
	var softRows int64
	dbutil.GetDB().Unscoped().Model(&model.MCPCredentialKey{}).
		Where("credential_id = ? AND deleted_at IS NOT NULL", cred.ID).Count(&softRows)
	if softRows == 0 {
		t.Errorf("删除应为软删除（deleted_at 打标），got 0 行")
	}

	// ⑦ 管理审计有账：签发/状态/删除都该留痕。
	auditSvc := NewMCPCredentialAuditLogAdminService()
	rows, total, err := auditSvc.List(ctx, "", "", &cred.ID, "", "", "", 0, 1, 50)
	if err != nil || total < 3 {
		t.Fatalf("管理审计应≥3条（签发/状态/删除），got total=%d err=%v", total, err)
	}
	for _, r := range rows {
		if strings.Contains(r.Detail, plain1) {
			t.Errorf("管理审计不得落明文钥匙: %s", r.Detail)
		}
	}
}

func TestMCPKey_CapAndCascade(t *testing.T) {
	newMCPKeySvcDB(t)
	ctx := context.Background()
	cred := seedMCPCredential(t, "mcp-keytest-cap")
	svc := NewMcpKeyAdminService()

	// 签发到上限，再签一把必须被拦（防无限签发把审计表与缓存撑爆）。
	for i := 0; i < maxKeysPerCredential; i++ {
		if _, _, err := svc.IssueKey(ctx, cred.ID, "k", nil); err != nil {
			t.Fatalf("第 %d 把签发不应失败: %v", i+1, err)
		}
	}
	if _, _, err := svc.IssueKey(ctx, cred.ID, "k", nil); !errors.Is(err, ErrMCPKeyCapReached) {
		t.Errorf("超过上限必须报 CapReached，got %v", err)
	}

	// 凭证删除级联清钥匙：不留孤儿行（hash 唯一索引还在，孤儿行管理端看不见却占着坑）。
	admin := NewMcpCredentialAdminService()
	if err := admin.Delete(ctx, cred.ID); err != nil {
		t.Fatalf("凭证删除失败: %v", err)
	}
	var cnt int64
	dbutil.GetDB().Model(&model.MCPCredentialKey{}).Where("credential_id = ?", cred.ID).Count(&cnt)
	if cnt != 0 {
		t.Errorf("凭证删除后钥匙应级联清空，got %d 行残留", cnt)
	}
	if err := dbutil.GetDB().First(&model.MCPCredential{}, cred.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("凭证应已删除，got err=%v", err)
	}
}

func TestMCPKey_BackfillIdempotent(t *testing.T) {
	// 启动期回填（db 包 postMigrateBackfillMCPCredentialKeys 的业务语义等价验证在
	// internal/pkg/db/migrate_backfill_test.go）：这里补的是 service 视角——
	// 回填行（KeyEnc 空）不可 reveal，只能"新建+删除"轮换。
	newMCPKeySvcDB(t)
	ctx := context.Background()
	cred := seedMCPCredential(t, "mcp-keytest-legacy")
	legacyHash := strings.Repeat("a", 64)
	dbutil.GetDB().Model(&model.MCPCredential{}).Where("id = ?", cred.ID).
		Update("api_key_hash", legacyHash)
	legacy := &model.MCPCredentialKey{CredentialID: cred.ID, Name: "初始密钥（回填）",
		KeyHash: legacyHash, Status: model.MCPKeyStatusActive}
	if err := dbutil.GetDB().Create(legacy).Error; err != nil {
		t.Fatalf("铺回填行失败: %v", err)
	}
	svc := NewMcpKeyAdminService()
	if _, err := svc.RevealKey(ctx, cred.ID, legacy.ID); !errors.Is(err, ErrMCPKeyNoPlaintext) {
		t.Errorf("回填行（无明文）reveal 必须报 NoPlaintext，got %v", err)
	}
	// 轮换路径：新签一把（明文可 reveal），删除回填行 → legacy 镜像清空。
	_, plain, err := svc.IssueKey(ctx, cred.ID, "轮换钥匙", nil)
	if err != nil {
		t.Fatalf("轮换签发失败: %v", err)
	}
	if got, err := svc.RevealKey(ctx, cred.ID, legacy.ID+1); err != nil || got != plain {
		t.Errorf("新钥匙应可 reveal，got %q err=%v", got, err)
	}
	if _, err := svc.DeleteKey(ctx, cred.ID, legacy.ID); err != nil {
		t.Fatalf("删除回填行失败: %v", err)
	}
	after, _ := svc.GetCredential(ctx, cred.ID)
	if after.APIKeyHash != "" {
		t.Errorf("回填行被删后 legacy 镜像应清空，got %q", after.APIKeyHash)
	}
}
