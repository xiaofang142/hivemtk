// bad_case_wiring_test.go T-P8-03：Bad Case 竖装配层的实跑。
//
// 沿用 T-P2-06 / T-P3-03 那条教训：服务层单测全绿而生产装配点没人调用，是"实现了但
// 一行都不写"的共同病灶。所以这里断的不是 BadCaseService 的逻辑（服务层用例已锁死），
// 而是只有装配层能答的四件事：
//  1. 拿不到 DB 句柄时全局是不是**被清空**（不是"什么都没做"）；
//  2. 装配出的全局实例是不是真能用（Available），且路由与编排器拿到的是同一对象；
//  3. 挂上编排器的那一份闭包是不是 badCaseMarkerFor() 这一个构造口造的，
//     而不是另搓一份等价闭包 —— 判据是它真往 bad_cases 写行；
//  4. 装配之后撤装配（InitBadCaseRuntime(nil)）时，已经挂上去的标记器行为不变：
//     它绑的是服务实例（内含活的 *gorm.DB），不是每次回读全局登记处。
//
// 全局句柄一律 t.Cleanup 还原：本包用例共享进程，留在指针上会串到后续用例
// 对"未装配"的断言那里去。表名与列名都是真的（NewTestDB 走进程私有库），
// 所以每个用例各自取独立 session_id 前缀，不靠"这库里只有我的行"这种侥幸。
package app

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func badCaseTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.BadCase{})
}

// cleanupGlobalBadCase 记住当前全局实例并在用例结束后还原。
func cleanupGlobalBadCase(t *testing.T) {
	t.Helper()
	prev := service.GlobalBadCaseService()
	t.Cleanup(func() { service.SetGlobalBadCaseService(prev) })
}

// countBadCasesBySession 按会话数行。**不走**被测链路的读出口（svc.List），
// 否则"装配好的服务恰好读不到自己刚写的行"这类失效会被同一份代码自证。
func countBadCasesBySession(t *testing.T, db *gorm.DB, sessionID string) []*model.BadCase {
	t.Helper()
	var rows []*model.BadCase
	if err := db.Where("session_id = ?", sessionID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("读 bad_cases 失败: %v", err)
	}
	return rows
}

func TestInitBadCaseRuntime_NilDBClearsGlobal(t *testing.T) {
	cleanupGlobalBadCase(t)
	database := badCaseTestDB(t)
	if InitBadCaseRuntime(database) == nil {
		t.Fatal("有 DB 句柄时应装配")
	}
	// 关键的一半：第二次拿到 nil 句柄时必须把上一份**撤掉**。只 return nil 不清全局的话，
	// 路由会继续对着"未装配"的启动日志回 200，编排器也会继续挂着上一份标记器。
	if got := InitBadCaseRuntime(nil); got != nil {
		t.Errorf("db=nil 时返回 %v，期望 nil", got)
	}
	if service.GlobalBadCaseService() != nil {
		t.Error("db=nil 后全局仍留有实例 ⇒ 端点与编排器都会以为底座可用")
	}
}

func TestInitBadCaseRuntime_RegistersUsableGlobal(t *testing.T) {
	cleanupGlobalBadCase(t)
	database := badCaseTestDB(t)
	svc := InitBadCaseRuntime(database)
	if svc == nil {
		t.Fatal("应装配")
	}
	if !svc.Available() {
		t.Error("装配出的实例 Available=false ⇒ 全部端点会回 503")
	}
	// 两个消费方各取一次全局，必须是同一对象：否则"路由以为装了、编排器以为没装"。
	if service.GlobalBadCaseService() != svc {
		t.Error("全局那份与返回的那份不是同一对象")
	}
	if got := InitBadCaseRuntime(database); got == svc {
		t.Error("重复装配没有换新实例 ⇒ 后写覆盖这条语义没生效")
	}
	if !service.GlobalBadCaseService().Available() {
		t.Error("重复装配后全局实例不可用")
	}
}

// 未装配时构造出来的是 nil：这一条是"本卡零改动"的装配侧凭据 ——
// 挂 nil 与不挂，对对话主链路是同一个形状（低质回答照常发出，只是不留痕）。
func TestBadCaseMarkerFor_NilWhenNotAssembled(t *testing.T) {
	cleanupGlobalBadCase(t)
	service.SetGlobalBadCaseService(nil)
	if m := badCaseMarkerFor(); m != nil {
		t.Error("底座未装配时不该造出标记器")
	}
	if attachBadCaseMarker(newBareOrchestrator(t)) {
		t.Error("底座未装配时 attach 不该报成功")
	}
	if attachBadCaseMarker(nil) {
		t.Error("编排器为 nil 时 attach 不该报成功")
	}
}

// 装配后走一次**生产闭包**：真表里必须出现坏例行，且来源、状态、门槛三列都是服务层
// 算出来的那份（装配层不自己判门槛，否则两处口径会漂）。
func TestBadCaseMarkerFor_WritesThroughProductionClosure(t *testing.T) {
	cleanupGlobalBadCase(t)
	database := badCaseTestDB(t)
	if InitBadCaseRuntime(database) == nil {
		t.Fatal("应装配")
	}
	if !attachBadCaseMarker(newBareOrchestrator(t)) {
		t.Fatal("已装配时 attach 应成功")
	}
	marker := badCaseMarkerFor()
	if marker == nil {
		t.Fatal("已装配时标记器不该为 nil")
	}
	ctx := context.Background()

	// ① 低置信度这一路：同一条 (session, message) 投两次只落一行（幂等键在装配之后仍成立）。
	lowConf := service.BadCaseMarkInput{
		SessionID: "sess_bc_wire_low", MessageID: "msg_bc_wire_low_1",
		QueryText: "退订要多久生效", AnswerText: "您好，已收到。",
		Confidence: 0.10, Threshold: 0.70, RetrievedCount: 3,
	}
	for i := 0; i < 2; i++ {
		marker(ctx, lowConf)
	}
	rows := countBadCasesBySession(t, database, "sess_bc_wire_low")
	if len(rows) != 1 {
		t.Fatalf("同一轮投两次落库 %d 行，期望 1 行", len(rows))
	}
	if rows[0].Source != model.BadCaseSourceLowConfidence {
		t.Errorf("来源判成 %s，期望 %s", rows[0].Source, model.BadCaseSourceLowConfidence)
	}
	if rows[0].Status != model.BadCaseStatusPending {
		t.Errorf("落库状态 %s，期望 %s（装配层不该替人判完）", rows[0].Status, model.BadCaseStatusPending)
	}

	// ② 零命中这一路：置信度高过阈值也必须记，且来源是 zero_hit。
	//    这条断的是"两条判据并列"这一决定在装配后的生产闭包里仍然成立。
	zeroHit := service.BadCaseMarkInput{
		SessionID: "sess_bc_wire_zero", MessageID: "msg_bc_wire_zero_1",
		QueryText: "你们老板是谁", AnswerText: "抱歉，暂时没有相关资料。",
		Confidence: 0.99, Threshold: 0.70, RetrievedCount: 0,
	}
	marker(ctx, zeroHit)
	rows = countBadCasesBySession(t, database, "sess_bc_wire_zero")
	if len(rows) != 1 {
		t.Fatalf("零命中轮次落库 %d 行，期望 1 行", len(rows))
	}
	if rows[0].Source != model.BadCaseSourceZeroHit {
		t.Errorf("来源判成 %s，期望 %s", rows[0].Source, model.BadCaseSourceZeroHit)
	}
	if rows[0].Threshold <= 0 {
		t.Errorf("落库 threshold=%v，门槛那一列必须留在现场（事后要能判这条算不算低质）", rows[0].Threshold)
	}
	if rows[0].MarkReason == "" {
		t.Error("mark_reason 为空 ⇒ 队列里看不出这一条为什么进来")
	}

	// ③ 够好的那一轮一条都不记：闭包不是"每次都写"，门槛仍在。
	marker(ctx, service.BadCaseMarkInput{
		SessionID: "sess_bc_wire_ok", MessageID: "msg_bc_wire_ok_1",
		Confidence: 0.95, Threshold: 0.70, RetrievedCount: 5,
	})
	if got := countBadCasesBySession(t, database, "sess_bc_wire_ok"); len(got) != 0 {
		t.Errorf("高质回答被记了 %d 行，期望 0 行", len(got))
	}
}

// 已挂上的标记器绑的是服务实例，不是每次回读全局登记处。
//
// 断这一条不是为了"就该这样设计"，而是因为不写清楚会被误读：撤装配（全局清空）
// **不会**让在途的编排器停下来。它手里的实例自带 DB 句柄，所以还会继续写；
// 真要停本竖得靠不装配（起始那一步）或进程重启。
func TestBadCaseMarkerFor_BoundAtAttachTimeNotPerCall(t *testing.T) {
	cleanupGlobalBadCase(t)
	database := badCaseTestDB(t)
	if InitBadCaseRuntime(database) == nil {
		t.Fatal("应装配")
	}
	marker := badCaseMarkerFor()
	if marker == nil {
		t.Fatal("已装配时标记器不该为 nil")
	}
	InitBadCaseRuntime(nil) // 全局撤掉
	if service.GlobalBadCaseService() != nil {
		t.Fatal("前置不成立：全局没被清空")
	}
	marker(context.Background(), service.BadCaseMarkInput{
		SessionID: "sess_bc_wire_stale", MessageID: "msg_bc_wire_stale_1",
		Confidence: 0.10, Threshold: 0.70, RetrievedCount: 1,
	})
	if got := countBadCasesBySession(t, database, "sess_bc_wire_stale"); len(got) != 1 {
		t.Errorf("全局清空后旧标记器落了 %d 行，期望 1 行（绑实例而非绑登记处）", len(got))
	}
	// 清空**之后**再取的构造口必须回 nil：这才是"装配决定挂不挂"的那半条语义。
	if m := badCaseMarkerFor(); m != nil {
		t.Error("全局已清空时仍造出标记器 ⇒ 关闸失效（未装配=零改动这条不成立）")
	}
}
