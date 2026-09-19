// human_task_wiring_test.go T-P3-03：待办竖装配层的实跑。
//
// 与 T-P2-06 同一条教训：service 侧单测全绿，生产装配点却没人调用，是本卡开工前
// 那些"实现了但一行都不写"的表的共同病灶。所以这里断的不是 HumanTaskService 的逻辑
// （服务层用例已经锁死），而是三件只有装配层能答的问题：
//  1. 拿不到 DB 句柄时全局是不是**被清空**（不是"什么都没做"）；
//  2. 装配之后的全局实例是不是真能用（Available）；
//  3. 挂到编排器上的那份闭包，是不是真的会往 human_tasks 写行 —— 用的是
//     humanTaskProducerFor() 这一个构造口，不是另搓一份等价闭包。
//
// 全局句柄的还原一律走 t.Cleanup：本包的用例共享同一个进程，全局待办服务留在
// 指针上会串到后续用例（以及它们对"未装配"的断言）那里去。
package app

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func humanTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{})
}

// cleanupGlobalHumanTask 记住当前全局实例并在用例结束后还原。
func cleanupGlobalHumanTask(t *testing.T) {
	t.Helper()
	prev := service.GlobalHumanTaskService()
	t.Cleanup(func() { service.SetGlobalHumanTaskService(prev) })
}

func TestInitHumanTaskRuntime_NilDBClearsGlobal(t *testing.T) {
	cleanupGlobalHumanTask(t)
	database := humanTaskTestDB(t)
	if InitHumanTaskRuntime(database) == nil {
		t.Fatal("有 DB 句柄时应装配")
	}
	// 关键的一半：第二次拿到 nil 句柄时必须把上一份**撤掉**。
	// 只 return nil 不清全局的话，路由会继续对着"未装配"的启动日志回 200。
	if got := InitHumanTaskRuntime(nil); got != nil {
		t.Errorf("db=nil 时返回 %v，期望 nil", got)
	}
	if service.GlobalHumanTaskService() != nil {
		t.Error("db=nil 后全局仍留有实例 ⇒ 端点与编排器都会以为底座可用")
	}
}

func TestInitHumanTaskRuntime_RegistersUsableGlobal(t *testing.T) {
	cleanupGlobalHumanTask(t)
	database := humanTaskTestDB(t)
	svc := InitHumanTaskRuntime(database)
	if svc == nil {
		t.Fatal("应装配")
	}
	if !svc.Available() {
		t.Error("装配出的实例 Available=false ⇒ 全部端点会回 503")
	}
	if service.GlobalHumanTaskService() != svc {
		t.Error("全局那份与返回的那份不是同一对象（路由与编排器会各自看到一套状态）")
	}
}

// 未装配时构造出来的是 nil：这一条是"本卡零改动"的装配侧凭据 ——
// 挂 nil 与不挂，对 transferToHuman 是同一个形状。
func TestHumanTaskProducerFor_NilWhenNotAssembled(t *testing.T) {
	cleanupGlobalHumanTask(t)
	service.SetGlobalHumanTaskService(nil)
	if p := humanTaskProducerFor(); p != nil {
		t.Error("底座未装配时不该造出生产者")
	}
	if attachHumanTaskProducer(newBareOrchestrator(t)) {
		t.Error("底座未装配时 attach 不该报成功")
	}
	if attachHumanTaskProducer(nil) {
		t.Error("编排器为 nil 时 attach 不该报成功")
	}
}

// 装配后走一次生产闭包：真表里必须出现一条 conversation_handoff，且同一会话第二次
// 投递不许多出一条（幂等判据在装配之后仍然成立 = AC② 的装配侧半边）。
func TestHumanTaskProducerFor_WritesThroughProductionClosure(t *testing.T) {
	cleanupGlobalHumanTask(t)
	database := humanTaskTestDB(t)
	if InitHumanTaskRuntime(database) == nil {
		t.Fatal("应装配")
	}
	o := newBareOrchestrator(t)
	if !attachHumanTaskProducer(o) {
		t.Fatal("已装配时 attach 应成功")
	}

	produce := humanTaskProducerFor()
	if produce == nil {
		t.Fatal("已装配时生产闭包不该为 nil")
	}
	ctx := context.Background()
	session := &model.CustomerSession{SessionID: "sess_app_wire", OneID: "oneid_app_wire"}
	for i := 0; i < 2; i++ {
		if err := produce(ctx, session, "装配层实跑投递"); err != nil {
			t.Fatalf("第 %d 次投递失败: %v", i+1, err)
		}
	}

	var rows []*model.HumanTask
	if err := database.Where("subject_id = ?", "sess_app_wire").Find(&rows).Error; err != nil {
		t.Fatalf("读待办失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("两次投递落库 %d 行，期望 1 行", len(rows))
	}
	if rows[0].Kind != model.HumanTaskKindConversationHandoff || rows[0].Status != model.HumanTaskStatusPending {
		t.Errorf("落库形状不符：kind=%s status=%s", rows[0].Kind, rows[0].Status)
	}
	if rows[0].SlaFirstResponseAt == nil {
		t.Error("首响截止未落列 ⇒ 这类待办不会进逾期读数（AC① 的会话档）")
	}
}
