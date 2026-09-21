// email_runtime_wiring_test.go 排水循环必须装配到进程上（R21 的那条"有方法没调用方"）。
package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// TestInitEmailRuntimeWithoutDB 无库句柄时不装配（排水会在没有落库对象的情况下认领邮件）。
func TestInitEmailRuntimeWithoutDB(t *testing.T) {
	t.Cleanup(StopEmailRuntime)
	if rt := InitEmailRuntime(nil); rt != nil {
		t.Errorf("无库时装配出了运行时（%+v）", rt)
	}
}

// TestInitEmailRuntimeReplacesPreviousWorker 重复装配不得攒出第二个并发排水器。
//
// 后果不是"多跑几轮"而是两个节拍器抢同一批到期行：SKIP LOCKED 让它们各拿一份，
// 于是同一封邮件的投递时刻取决于哪条协程先跑 —— 那是没法复现的行为。
func TestInitEmailRuntimeReplacesPreviousWorker(t *testing.T) {
	database := testutil.NewTestDB(t, &model.EmailSend{})
	db.SetTestDB(database)
	t.Cleanup(StopEmailRuntime)

	first := InitEmailRuntime(database)
	if first == nil {
		t.Fatal("有库时没有装配出运行时")
	}
	second := InitEmailRuntime(database)
	if second == nil {
		t.Fatal("第二次装配返回 nil")
	}
	if first.DrainWorker() == second.DrainWorker() {
		t.Error("两次装配复用了同一台 worker ⇒ 前一台的节拍无法单独停掉")
	}

	// 节拍是 30s ⇒ 用 RunOnce 催，不靠等（等固定时长在负载高的机器上只会红）。
	if err := first.DrainWorker().RunOnce(context.Background()); err != nil {
		t.Fatalf("第一台 worker 手动催轮失败: %v", err)
	}
	if first.DrainWorker().Rounds() != 1 {
		t.Errorf("第一台轮次 = %d，期望 1", first.DrainWorker().Rounds())
	}
	StopEmailRuntime()

	// 停机之后两台的轮次都必须冻结：没被 Stop 的旧协程是这里唯一真正的泄漏形状。
	frozenFirst, frozenSecond := first.DrainWorker().Rounds(), second.DrainWorker().Rounds()
	time.Sleep(120 * time.Millisecond)
	if got := first.DrainWorker().Rounds(); got != frozenFirst {
		t.Errorf("StopEmailRuntime 之后旧 worker 轮次仍在增长（%d → %d）", frozenFirst, got)
	}
	if got := second.DrainWorker().Rounds(); got != frozenSecond {
		t.Errorf("StopEmailRuntime 之后当前 worker 轮次仍在增长（%d → %d）", frozenSecond, got)
	}
}

// TestStopEmailRuntimeIdempotent 没装配过也要能停（装配层可重复调用）。
func TestStopEmailRuntimeIdempotent(t *testing.T) {
	StopEmailRuntime()
	StopEmailRuntime()
}

// TestEmailRuntimeWiredIntoRouter 排水器必须由 router 起起来。
//
// 这是一条静态锁：判据本身（ProcessPendingEmails）与节拍（EmailDrainWorker）都有行为用例，
// 唯独"进程里到底有没有人跑它"只能在装配点看到 —— 而它恰恰是 R21 的原病灶
// （ProcessPendingEmails 早就存在，非测试调用点是 0）。摘掉 router.go 里那一行必须让本用例红。
//
// 判据按"行"而不是按"整段子串出现"：第一版写的是 strings.Count(body, "app.InitEmailRuntime(")，
// 反向测试（把那行注释掉）实测仍然绿 —— 注释行 `// app.InitEmailRuntime(gormDB)` 里同样含这个
// 子串。没有那一次反向测试这条锁就是零牙齿的装饰。
func TestEmailRuntimeWiredIntoRouter(t *testing.T) {
	src, err := os.ReadFile("../../internal/router/router.go")
	if err != nil {
		t.Fatalf("读 router.go 失败: %v", err)
	}
	const wiring = "app.InitEmailRuntime("
	lineIdx, seen := -1, 0
	for i, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if strings.Contains(line, wiring) {
			lineIdx = i
			seen++
		}
	}
	if seen == 0 {
		t.Fatalf("router.go 里没有包含 %s 的非注释语句行 ⇒ 排期邮件会退回「没人排水」的状态", wiring)
	}
	if seen > 1 {
		t.Errorf("装配语句出现 %d 次，期望 1 次 ⇒ 两台排水器会抢同一批到期行", seen)
	}
	// 装配必须晚于 DB 句柄就绪：Setup 的形参 gormDB 在函数签名那行就已出现，
	// 所以这里比的是"签名行的行号 < 装配行的行号"，而不是字符下标 —— 下标会把
	// 文件头注释里提到的 gormDB 也算成"句柄已就绪"。
	if sigIdx := strings.Index(string(src), "func Setup(r *gin.Engine, gormDB *gorm.DB)"); sigIdx < 0 {
		t.Fatal("找不到 Setup 签名，无法判定装配点的先后")
	} else if sigLine := strings.Count(string(src)[:sigIdx], "\n"); sigLine >= lineIdx {
		t.Errorf("排水装配（第 %d 行）不在 Setup 签名（第 %d 行）之后 ⇒ 认领会打在一个还没建好的连接上",
			lineIdx+1, sigLine+1)
	}
}
