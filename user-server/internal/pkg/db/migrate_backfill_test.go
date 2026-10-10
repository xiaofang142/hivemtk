// migrate_backfill_test.go 钉住"存量 1:1 凭证对必须真的迁成钥匙行，且可重跑"。
//
// 1:N 拆表（2026-10-11）后认证真值源是 mcp_credential_keys：回填钩子漏跑或半跑，
// 升级瞬间所有存量凭证都会掉进 legacy 兜底列——能用，但 last_used/缓存口径分裂，
// 管理端也看不见那把钥匙。三条腿：
//  1. 有 legacy hash 的凭证回填出钥匙行（状态 active、过期继承凭证）；
//  2. 钩子重跑不产生第二行（幂等，启动期可反复执行）；
//  3. 没有 legacy hash 的凭证（新口径只由 IssueKey 造钥匙）不回填出空 hash 行。
package db

import (
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func TestStartupBackfillsMCPCredentialKeys(t *testing.T) {
	gdb := testutil.NewTestDB(t, &model.MCPCredential{}, &model.MCPCredentialKey{})
	if gdb == nil {
		t.Skip("测试库不可达")
	}

	legacyHash := "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c"
	cred := &model.MCPCredential{ClientID: "mcp-backfill-1", Name: "回填用例", APIKeyHash: legacyHash, Enabled: true}
	if err := gdb.Create(cred).Error; err != nil {
		t.Fatalf("植入存量凭证失败: %v", err)
	}
	// 新口径凭证：无 legacy hash（钥匙由 IssueKey 签发，不写本列）。
	fresh := &model.MCPCredential{ClientID: "mcp-backfill-2", Name: "新口径", Enabled: true}
	if err := gdb.Create(fresh).Error; err != nil {
		t.Fatalf("植入新口径凭证失败: %v", err)
	}

	postMigrateBackfillMCPCredentialKeys(gdb)

	var keyRows []model.MCPCredentialKey
	if err := gdb.Where("credential_id = ?", cred.ID).Find(&keyRows).Error; err != nil {
		t.Fatalf("查钥匙行失败: %v", err)
	}
	if len(keyRows) != 1 {
		t.Fatalf("存量凭证应回填出恰好 1 把钥匙，got %d", len(keyRows))
	}
	if keyRows[0].KeyHash != legacyHash || keyRows[0].Status != model.MCPKeyStatusActive {
		t.Errorf("回填行形状错: %+v", keyRows[0])
	}
	if keyRows[0].KeyEnc != "" {
		t.Errorf("回填行没有明文来源，KeyEnc 必须为空（不可 reveal）: %+v", keyRows[0])
	}

	// 幂等：重跑不产生第二行。
	postMigrateBackfillMCPCredentialKeys(gdb)
	var cnt int64
	gdb.Model(&model.MCPCredentialKey{}).Where("credential_id = ?", cred.ID).Count(&cnt)
	if cnt != 1 {
		t.Errorf("回填必须幂等，重跑后行数=%d", cnt)
	}

	// 新口径凭证不回填（没有 hash 可迁）。
	var freshCnt int64
	gdb.Model(&model.MCPCredentialKey{}).Where("credential_id = ?", fresh.ID).Count(&freshCnt)
	if freshCnt != 0 {
		t.Errorf("无 legacy hash 的凭证不应回填出钥匙行，got %d", freshCnt)
	}
}
