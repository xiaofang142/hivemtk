// webhook_wiring_default_test.go —— webhook 装配收口与根域五文件 Default 构造回归
// （AV-04c 批次7 刀1）。
//
// 批次7 把根域五文件的 service 层取句柄收口为仓储 Default/Shared 构造：
//   - NewWebhookService：删「构造器取 repository.GetDB() 再逐仓 SetDB」的同值冗余
//     （各仓无参构造已自取同一 dbstate 全局句柄），消息三仓改 repository.DBReady()
//     探针保持「无库留 nil」；ensureReposFromDB 改仓储 Shared 构造，连接与
//     eventRepo 同源，句柄不再在 service 层过手。
//   - knowledge_base/openapi/wecom/cards/tiktok_card：装配改 Default 构造。
//
// 本文件锁装配侧契约：有库即就绪、无库不建仓（nil 语义不变）、Shared 与
// eventRepo 同连接；仓储侧连接语义由各仓储既有契约测试覆盖。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 有库：webhook 全部下游仓储就绪且连着测试库（原「GetDB+SetDB」装配的等价形态）。
func TestNewWebhookService_WithDB_WiresAllRepos(t *testing.T) {
	_ = testutil.NewTestDB(t)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	if !svc.connected() {
		t.Fatal("有库时 webhook 服务应判 connected")
	}
	if svc.messageHubRepo == nil || svc.inboxConvRepo == nil || svc.unifiedMsgRepo == nil {
		t.Fatal("有库时消息三仓应在装配期建好（DBReady 探针路径）")
	}
	if svc.delayedRepo == nil {
		t.Fatal("有库时 delayedRepo 应非 nil")
	}

	// clueRepo 与原装配一致：不在构造期建，懒装配首用时建。
	if svc.clueRepo != nil {
		t.Fatal("clueRepo 与原装配一致：构造期不建（懒装配语义）")
	}
	// ensureReposFromDB 幂等：已建好的仓不被重建（指针保持原样）。
	before := svc.messageHubRepo
	svc.ensureReposFromDB(context.Background())
	if svc.messageHubRepo != before {
		t.Fatal("ensureReposFromDB 不应重建已就绪的 messageHubRepo")
	}
	if svc.clueRepo == nil {
		t.Fatal("有库时懒装配应建出 clueRepo（Shared 构造与 eventRepo 同连接）")
	}
}

// 无库：消息三仓保持 nil（DBReady 探针语义），懒装配也不建仓，构造不 panic。
func TestNewWebhookService_WithoutDB_KeepsNilSemantics(t *testing.T) {
	t.Cleanup(func() { db.SetTestDB(nil) })
	db.SetTestDB(nil)

	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	if svc.connected() {
		t.Fatal("无库时 webhook 服务不应判 connected")
	}
	if svc.messageHubRepo != nil || svc.inboxConvRepo != nil || svc.unifiedMsgRepo != nil {
		t.Fatal("无库时消息三仓应保持 nil（无库留 nil 的原装配语义）")
	}
	if svc.delayedRepo == nil {
		t.Fatal("delayedRepo 与原装配一致：无条件构造（库句柄为 nil，调用时按错误路径返回）")
	}
	svc.ensureReposFromDB(context.Background())
	if svc.messageHubRepo != nil || svc.inboxConvRepo != nil || svc.unifiedMsgRepo != nil || svc.clueRepo != nil {
		t.Fatal("无库时懒装配不应建仓（Shared 构造对缺句柄来源返回 nil）")
	}
}

// Shared 构造：来源 nil 或缺句柄 → nil 接口值（不是包着 nil 的 typed nil）；
// 来源就绪 → 与 eventRepo 同连接（写读同库可证）。
func TestWebhookSharedConstructors_SourceSemantics(t *testing.T) {
	if repository.NewMessageHubRepositoryShared(nil) != nil ||
		repository.NewInboxConversationRepositoryShared(nil) != nil ||
		repository.NewUnifiedMessageRepositoryShared(nil) != nil ||
		repository.NewClueRepositoryShared(nil) != nil {
		t.Fatal("Shared 构造对 nil 来源应返回 nil")
	}

	_ = testutil.NewTestDB(t)
	evRepo := repository.NewWebhookEventRepository() // 无参构造自取全局句柄（此时即测试库）
	hub := repository.NewMessageHubRepositoryShared(evRepo)
	conv := repository.NewInboxConversationRepositoryShared(evRepo)
	unified := repository.NewUnifiedMessageRepositoryShared(evRepo)
	clue := repository.NewClueRepositoryShared(evRepo)
	if hub == nil || conv == nil || unified == nil || clue == nil {
		t.Fatal("来源仓储有句柄时 Shared 构造应全部非 nil")
	}
	if !evRepo.Connected() {
		t.Fatal("eventRepo 应连着测试库")
	}
}

// 根域其余四文件装配：Default 构造与原「service 取句柄传仓储」同连接。
func TestRootDomainDefaultConstructors_WithDB(t *testing.T) {
	_ = testutil.NewTestDB(t)

	kb := NewKnowledgeBaseService()
	if kb.repo == nil || kb.bindingRepo == nil || kb.itemRepo == nil {
		t.Fatal("NewKnowledgeBaseService 三仓应全部就绪")
	}

	svc := NewTikTokCardService()
	if svc == nil {
		t.Fatal("NewTikTokCardService 应就绪")
	}
	wc := NewWeComService()
	if wc == nil || wc.tagRepo == nil {
		t.Fatal("NewWeComService 与 tagRepo 应就绪")
	}
	oa := NewOpenAPIService()
	if oa == nil || oa.srcRepo == nil || oa.docRepo == nil {
		t.Fatal("NewOpenAPIService 两仓应就绪")
	}
}
