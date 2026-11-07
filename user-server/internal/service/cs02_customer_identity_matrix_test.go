// cs02_customer_identity_matrix_test.go CS-02 客户身份/绑定矩阵补格（真实 PG 8217 受控变量）。
//
// 既有电池已钉：归一化词表、IdentifyOrCreate 优先级、LinkIdentity 冲突（phone/email）、
// 会话仓储 OneID 跨渠道合并（S3-1）、CS-01 样本① douyin open_id → customer 档案、
// CS-01 跨租户负例（消息行/认领隔离）。本文件补三格：
//
//	① A1/A2/B1 同 sender_name、可区分 sender_id：sender_name 不是身份字段——
//	   同名不得并客户、不得并会话（身份只认 open_id），同名仅落 UserName 展示位。
//	② 同租户跨账号合并**仅在既有 OneID 规则成立时**发生：同一 open_id 经 A1/A2 两账号
//	   进入 → GetActiveByUserID 按用户身份并回同一会话；群聊走 "group:<gid>" 独立
//	   命名空间——群/私作用域保留，不得互串。
//	③ 跨租户同平台 open_id 不得串客户：租户隔离下同 open_id 各建各档，ResolveIdentity
//	   只见本租户的行。
//
// AI 腿直接走生产实体 SmartCSOrchestrator.HandleIncoming（会话+身份格的共用入口，
// 与 runAIGeneration 的调用同源）；cs02- 前缀字段全量可区分。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/identity"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// cs02In 会话矩阵的入站上下文（平台/账号/发送者/内容冻结）。
func cs02In(accountID, senderID, senderName, content string) *IncomingContext {
	return &IncomingContext{
		Platform:   model.PlatformDouyin,
		AccountID:  accountID,
		SenderID:   senderID,
		SenderName: senderName,
		Content:    content,
		MessageID:  "cs02-evt-" + senderID,
	}
}

// TestCS02_CustomerIdentity_SameSenderNameDistinctOpenIDs 格①：
// 同 sender_name、不同 open_id → 两个客户档案；sender_name 不参与身份判定，
// 只落展示位；open_id 已被他人持有则 LinkIdentity 拒绝（不静默合并）。
func TestCS02_CustomerIdentity_SameSenderNameDistinctOpenIDs(t *testing.T) {
	_ = testutil.NewTestDB(t, &model.Customer{})
	svc := NewCustomerIdentityService()
	ctx := context.Background()

	const sameName = "CS02同名客户"
	custA, err := svc.IdentifyOrCreate(ctx, identity.Identifiers{DouyinOpenID: "cs02-openid-name-a"})
	if err != nil {
		t.Fatalf("open_id A 建档失败: %v", err)
	}
	custB, err := svc.IdentifyOrCreate(ctx, identity.Identifiers{DouyinOpenID: "cs02-openid-name-b"})
	if err != nil {
		t.Fatalf("open_id B 建档失败: %v", err)
	}
	if custA.ID == custB.ID {
		t.Fatal("不同 open_id 不得并成同一客户（sender_name 不是身份字段，同名校验不该存在）")
	}

	// 幂等：同 open_id 重复识别回到同一档案。
	custA2, err := svc.IdentifyOrCreate(ctx, identity.Identifiers{DouyinOpenID: "cs02-openid-name-a"})
	if err != nil || custA2.ID != custA.ID {
		t.Fatalf("同 open_id 应幂等回同一档案: %+v err=%v", custA2, err)
	}

	// LinkIdentity：把 B 档案的 douyin open_id 改成 A 已持有的 → 拒绝（不静默合并）。
	if err := svc.LinkIdentity(ctx, custB.ID, "", "", "", "cs02-openid-name-a", ""); err == nil {
		t.Fatal("open_id 已被其他客户持有时 LinkIdentity 必须拒绝（跨客户静默合并=串档案）")
	}

	// ResolveIdentity 按身份回档案，与 sender_name 无关。
	got, err := svc.ResolveIdentity(ctx, identity.Identifiers{DouyinOpenID: "cs02-openid-name-b"})
	if err != nil || len(got) != 1 || got[0].ID != custB.ID {
		t.Fatalf("ResolveIdentity(open_id B) 应恰回 B 档案: %+v err=%v", got, err)
	}
}

// TestCS02_Sessions_SameSenderNameDistinctSenderID_A1A2Scopes 格①+②（AI 腿会话矩阵）：
//
//	同 sender_name（A1/A2 两账号）不同 sender_id → 两个会话、两个客户档案；
//	同一 sender_id 经 A1 → A2（同租户跨账号）→ 并回**同一**会话（既有规则：
//	按用户身份 GetActiveByUserID 合并，前提是 OneID 规则成立——同一 open_id）；
//	同一 sender_id 的群聊消息走 "group:<gid>" 命名空间——群/私作用域保留不互并。
func TestCS02_Sessions_SameSenderNameDistinctSenderID_A1A2Scopes(t *testing.T) {
	_, orch, db := c01SetupAI(t, &fakeRAGSearcher{}, fakeIntentRecognizer{})
	ctx := context.Background()

	const (
		accA1      = c01AccountA1 // "cs01-acc-a1"
		accA2      = "cs02-acc-a2"
		sameName   = "CS02同名买家"
		openX1     = "cs02-openid-x1"
		openX2     = "cs02-openid-x2"
		groupID    = "cs02-group-001"
		contentPrv = "私聊问：支持七天无理由吗"
		contentGrp = "群里问：发货时间"
	)

	// ① X1 经 A1（私聊）→ 会话 S1。
	r1, err := orch.HandleIncoming(ctx, cs02In(accA1, openX1, sameName, contentPrv))
	if err != nil {
		t.Fatalf("X1@A1 入会失败: %v", err)
	}
	// ② X2 经 A2（私聊，同名）→ 会话 S2，与 S1 不同。
	r2, err := orch.HandleIncoming(ctx, cs02In(accA2, openX2, sameName, contentPrv))
	if err != nil {
		t.Fatalf("X2@A2 入会失败: %v", err)
	}
	if r1.SessionID == r2.SessionID {
		t.Fatal("同名不同 open_id 不得并会话（身份只认 sender_id）")
	}
	var s1, s2 model.CustomerSession
	if err := db.Where("session_id = ?", r1.SessionID).First(&s1).Error; err != nil {
		t.Fatalf("读 S1: %v", err)
	}
	if err := db.Where("session_id = ?", r2.SessionID).First(&s2).Error; err != nil {
		t.Fatalf("读 S2: %v", err)
	}
	if s1.OneID != "douyin:"+openX1 || s2.OneID != "douyin:"+openX2 {
		t.Errorf("OneID 派生规则（douyin:open_id）: S1=%q S2=%q", s1.OneID, s2.OneID)
	}
	if s1.AccountID != accA1 || s2.AccountID != accA2 {
		t.Errorf("会话账号归属: S1=%q S2=%q，期望 %q/%q", s1.AccountID, s2.AccountID, accA1, accA2)
	}
	if s1.UserName != sameName || s2.UserName != sameName {
		t.Errorf("sender_name 只落展示位 UserName: %q/%q", s1.UserName, s2.UserName)
	}

	// ③ X1 经 A2 再进（同租户跨账号）→ 并回 S1（既有 OneID/用户身份规则成立时的合并）。
	r3, err := orch.HandleIncoming(ctx, cs02In(accA2, openX1, sameName, contentPrv+"（第二条）"))
	if err != nil {
		t.Fatalf("X1@A2 第二次入会失败: %v", err)
	}
	if r3.SessionID != r1.SessionID {
		t.Errorf("同租户跨账号、同一 open_id 应并回同一会话（既有规则），实际新会话 %q ≠ %q", r3.SessionID, r1.SessionID)
	}

	// ④ 同一 open_id 的群聊消息 → "group:<gid>" 独立作用域，不与私聊互并。
	gin := cs02In(accA1, openX1, sameName, contentGrp)
	gin.IsGroup = true
	gin.GroupID = groupID
	r4, err := orch.HandleIncoming(ctx, gin)
	if err != nil {
		t.Fatalf("群聊入会失败: %v", err)
	}
	if r4.SessionID == r1.SessionID {
		t.Fatal("群聊与私聊作用域必须保留（group: 命名空间独立），不得互并")
	}
	var s4 model.CustomerSession
	if err := db.Where("session_id = ?", r4.SessionID).First(&s4).Error; err != nil {
		t.Fatalf("读 S4: %v", err)
	}
	if s4.OneID != "group:"+groupID {
		t.Errorf("群聊 OneID = %q，期望 group:%s", s4.OneID, groupID)
	}

	// 计数对账：三把 OneID 恰三个会话；客户档案恰两条（X1/X2）。
	var sessCount, custCount int64
	db.Model(&model.CustomerSession{}).Where("one_id IN ?", []string{"douyin:" + openX1, "douyin:" + openX2, "group:" + groupID}).Count(&sessCount)
	if sessCount != 3 {
		t.Errorf("三把 OneID 应恰 3 个会话行，实际 %d", sessCount)
	}
	db.Model(&model.Customer{}).Where("douyin_open_id IN ?", []string{openX1, openX2}).Count(&custCount)
	if custCount != 2 {
		t.Errorf("两个 open_id 应恰 2 条客户档案，实际 %d（同名不得建第三档/并档）", custCount)
	}
}

// TestCS02_CustomerIdentity_CrossTenantSameOpenID_NoCrossCustomer 格③：
// 租户隔离下同一平台 open_id 各租户各建各档；ResolveIdentity 只见本租户的行。
func TestCS02_CustomerIdentity_CrossTenantSameOpenID_NoCrossCustomer(t *testing.T) {
	database := testutil.NewTestDB(t, &model.Customer{})
	prev := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(prev) })

	svc := NewCustomerIdentityService()
	const shared = "cs02-openid-cross-tenant"

	ctxA := tenant.WithTenantID(context.Background(), c01TenantA)
	ctxB := tenant.WithTenantID(context.Background(), c01TenantB)

	rowA, err := svc.IdentifyOrCreate(ctxA, identity.Identifiers{DouyinOpenID: shared})
	if err != nil {
		t.Fatalf("租户 A 建档失败: %v", err)
	}
	rowB, err := svc.IdentifyOrCreate(ctxB, identity.Identifiers{DouyinOpenID: shared})
	if err != nil {
		t.Fatalf("租户 B 建档失败: %v", err)
	}
	if rowA.ID == rowB.ID {
		t.Fatal("跨租户同 open_id 不得串客户（各租户独立档案）")
	}
	if rowA.TenantID != c01TenantA || rowB.TenantID != c01TenantB {
		t.Errorf("档案租户落章: A=%d B=%d，期望 %d/%d", rowA.TenantID, rowB.TenantID, c01TenantA, c01TenantB)
	}

	// 解析隔离：A 的上下文查不到 B 的行，反之亦然。
	gotA, err := svc.ResolveIdentity(ctxA, identity.Identifiers{DouyinOpenID: shared})
	if err != nil || len(gotA) != 1 || gotA[0].ID != rowA.ID {
		t.Fatalf("租户 A 解析应恰见 A 档案: %+v err=%v", gotA, err)
	}
	gotB, err := svc.ResolveIdentity(ctxB, identity.Identifiers{DouyinOpenID: shared})
	if err != nil || len(gotB) != 1 || gotB[0].ID != rowB.ID {
		t.Fatalf("租户 B 解析应恰见 B 档案: %+v err=%v", gotB, err)
	}

	var total int64
	// on 模式下裸读租户表被隔离守卫拒收（fail-closed 正确行为）——审计计数走系统作用域。
	database.WithContext(tenant.WithSystemScope(context.Background())).
		Model(&model.Customer{}).Where("douyin_open_id = ?", shared).Count(&total)
	if total != 2 {
		t.Errorf("同 open_id 跨租户应恰 2 行（各租户一行），实际 %d", total)
	}
}
