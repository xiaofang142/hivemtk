package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func setupPollingLockTestDB(t *testing.T) *gorm.DB {
	db := testutil.NewTestDB(t, &model.TelegramAccount{})
	if err := db.Exec("DELETE FROM telegram_accounts").Error; err != nil {
		t.Fatalf("清理测试数据失败: %v", err)
	}
	return db
}

func seedTelegramAccount(t *testing.T, db *gorm.DB, id uint, name, token string) *model.TelegramAccount {
	acc := &model.TelegramAccount{
		ID:          id,
		AccountName: name,
		BotToken:    token,
		Status:      1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("插入测试账号失败: %v", err)
	}
	return acc
}

func withPollingLockRepo(t *testing.T, repo *repository.TelegramPollingLockRepository) {
	t.Cleanup(resetPollingLockRepoForTest(repo))
}

// TestPollingLock_AcquireRelease 测试锁的抢占与释放基本流程
func TestPollingLock_AcquireRelease(t *testing.T) {
	dbConn := setupPollingLockTestDB(t)
	seedTelegramAccount(t, dbConn, 100, "test-acc-100", "test-token-100")
	ctx := context.Background()

	repo := repository.NewTelegramPollingLockRepositoryWithDB(dbConn)
	withPollingLockRepo(t, repo)

	acquired, owner, _, err := TryAcquirePollingLock(ctx, nil, 100)
	if err != nil {
		t.Fatalf("TryAcquire 失败: %v", err)
	}
	if !acquired {
		t.Fatalf("初始空闲锁应抢占成功，got acquired=false (owner=%s)", owner)
	}
	if owner == "" {
		t.Errorf("owner 不应为空")
	}
	expectedWorker := GetPollingWorkerID()
	if owner != expectedWorker {
		t.Errorf("owner=%s, want %s", owner, expectedWorker)
	}

	if !IsPollingLockHeldByMe(ctx, nil, 100) {
		t.Errorf("IsPollingLockHeldByMe 应返回 true（刚抢到锁）")
	}

	acquired2, owner2, _, _ := TryAcquirePollingLock(ctx, nil, 100)
	if !acquired2 {
		t.Errorf("同一 worker 再次抢占应成功（续约场景）")
	}
	if owner2 != expectedWorker {
		t.Errorf("owner=%s, want %s", owner2, expectedWorker)
	}

	if rerr := ReleasePollingLock(ctx, nil, 100); rerr != nil {
		t.Fatalf("Release 失败: %v", rerr)
	}
	if IsPollingLockHeldByMe(ctx, nil, 100) {
		t.Errorf("Release 后 IsPollingLockHeldByMe 应返回 false")
	}
}

// TestPollingLock_ConflictBetweenWorkers 测试两个不同 worker ID 的抢占互斥
func TestPollingLock_ConflictBetweenWorkers(t *testing.T) {
	dbConn := setupPollingLockTestDB(t)
	seedTelegramAccount(t, dbConn, 200, "test-acc-200", "test-token-200")
	ctx := context.Background()

	repo := repository.NewTelegramPollingLockRepositoryWithDB(dbConn)
	withPollingLockRepo(t, repo)

	originalID := pollingWorkerID
	workerA := "host-A:11111"
	workerB := "host-B:22222"
	pollingWorkerID = workerA
	t.Cleanup(func() { pollingWorkerID = originalID })

	acquiredA, ownerA, _, _ := TryAcquirePollingLock(ctx, nil, 200)
	if !acquiredA {
		t.Fatalf("Worker A 应抢占成功")
	}
	if ownerA != workerA {
		t.Errorf("owner=%s, want %s", ownerA, workerA)
	}

	pollingWorkerID = workerB
	acquiredB, ownerB, lastHB, _ := TryAcquirePollingLock(ctx, nil, 200)
	if acquiredB {
		t.Errorf("Worker B 在活跃锁上抢占应失败")
	}
	if ownerB != workerA {
		t.Errorf("失败时应返回当前 owner=%s, want %s", ownerB, workerA)
	}
	if lastHB == nil {
		t.Errorf("失败时应返回 lastHB（最近心跳时间）")
	}

	pollingWorkerID = workerA
	if rerr := ReleasePollingLock(ctx, nil, 200); rerr != nil {
		t.Fatalf("Release 失败: %v", rerr)
	}
	pollingWorkerID = workerB
	acquiredB2, ownerB2, _, _ := TryAcquirePollingLock(ctx, nil, 200)
	if !acquiredB2 {
		t.Errorf("Worker A 释放后 Worker B 应抢占成功")
	}
	if ownerB2 != workerB {
		t.Errorf("owner=%s, want %s", ownerB2, workerB)
	}

	pollingWorkerID = workerB
	_ = ReleasePollingLock(ctx, nil, 200)
}

// TestPollingLock_StaleTakeover 测试僵尸锁可被抢占（心跳超时）
func TestPollingLock_StaleTakeover(t *testing.T) {
	dbConn := setupPollingLockTestDB(t)
	seedTelegramAccount(t, dbConn, 300, "test-acc-300", "test-token-300")
	ctx := context.Background()

	repo := repository.NewTelegramPollingLockRepositoryWithDB(dbConn)
	withPollingLockRepo(t, repo)

	originalID := pollingWorkerID
	defer func() { pollingWorkerID = originalID }()

	pollingWorkerID = "host-A:11111"
	acquired, _, _, _ := TryAcquirePollingLock(ctx, nil, 300)
	if !acquired {
		t.Fatalf("Worker A 应抢占成功")
	}
	staleTime := time.Now().Add(-90 * time.Second)
	if err := dbConn.Model(&model.TelegramAccount{}).
		Where("id = ?", 300).
		Update("polling_heartbeat_at", staleTime).Error; err != nil {
		t.Fatalf("设置心跳为过期时间失败: %v", err)
	}

	pollingWorkerID = "host-B:22222"
	acquiredB, ownerB, _, _ := TryAcquirePollingLock(ctx, nil, 300)
	if !acquiredB {
		t.Errorf("Worker A 锁过期时 Worker B 应抢占成功")
	}
	if ownerB != "host-B:22222" {
		t.Errorf("owner=%s, want %s", ownerB, "host-B:22222")
	}

	_ = ReleasePollingLock(ctx, nil, 300)
}

// TestPollingLock_HeartbeatLoss 测试心跳检测到锁丢失
func TestPollingLock_HeartbeatLoss(t *testing.T) {
	dbConn := setupPollingLockTestDB(t)
	seedTelegramAccount(t, dbConn, 400, "test-acc-400", "test-token-400")
	ctx := context.Background()

	repo := repository.NewTelegramPollingLockRepositoryWithDB(dbConn)
	withPollingLockRepo(t, repo)

	originalID := pollingWorkerID
	defer func() { pollingWorkerID = originalID }()

	pollingWorkerID = "host-A:11111"
	acquired, _, _, _ := TryAcquirePollingLock(ctx, nil, 400)
	if !acquired {
		t.Fatalf("Worker A 应抢占成功")
	}

	lockLost, err := HeartbeatPollingLock(ctx, nil, 400)
	if err != nil {
		t.Fatalf("Worker A 心跳失败: %v", err)
	}
	if lockLost {
		t.Errorf("Worker A 心跳不应检测到锁丢失")
	}

	pollingWorkerID = "host-B:22222"
	if err := dbConn.Exec(
		"UPDATE telegram_accounts SET polling_owner = ?, polling_heartbeat_at = ? WHERE id = ?",
		"host-B:22222", time.Now(), 400,
	).Error; err != nil {
		t.Fatalf("模拟 Worker B 抢占失败: %v", err)
	}

	pollingWorkerID = "host-A:11111"
	lockLost2, err2 := HeartbeatPollingLock(ctx, nil, 400)
	if err2 != nil {
		t.Fatalf("Worker A 心跳（被抢占后）失败: %v", err2)
	}
	if !lockLost2 {
		t.Errorf("Worker A 心跳应检测到锁丢失（RowsAffected=0）")
	}

	pollingWorkerID = "host-B:22222"
	_ = ReleasePollingLock(ctx, nil, 400)
}

// TestPollingLock_NilDB 保护性降级：db=nil 不 panic
//
// 五层架构修复后，service 门面的 db 参数已是兼容旧签名的 _ interface{}，
// 传 nil 表示「用全局 pollingLockRepo」。若全局 repo 也未初始化（DB 句柄为 nil），
// 则应返回 error 而非 panic。
func TestPollingLock_NilDB(t *testing.T) {
	defer resetPollingLockRepoForTest(repository.NewTelegramPollingLockRepositoryWithDB(nil))()

	acquired, _, _, err := TryAcquirePollingLock(context.Background(), nil, 999)
	if err == nil {
		t.Errorf("db=nil 应返回 error")
	}
	if acquired {
		t.Errorf("db=nil 应返回 acquired=false")
	}
	if rerr := ReleasePollingLock(context.Background(), nil, 999); rerr == nil {
		t.Errorf("db=nil Release 应返回 error")
	}
	if IsPollingLockHeldByMe(context.Background(), nil, 999) {
		t.Errorf("db=nil IsPollingLockHeldByMe 应返回 false")
	}
}

// TestGetPollingWorkerID 测试 worker ID 稳定性
func TestGetPollingWorkerID(t *testing.T) {
	id1 := GetPollingWorkerID()
	id2 := GetPollingWorkerID()
	if id1 == "" {
		t.Errorf("GetPollingWorkerID 不应返回空")
	}
	if id1 != id2 {
		t.Errorf("worker ID 应稳定: id1=%s id2=%s", id1, id2)
	}
	expected := fmt.Sprintf("%s:%d", mustHostname(), os.Getpid())
	if id1 != expected {
		t.Errorf("worker ID=%s, want %s", id1, expected)
	}
}

func mustHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// TestStopAllTelegramPollingReleasesHeldLock 关停必须把 polling 锁交回给下一个实例
//
// 为什么行为断言不能被 cmd/api 那条门代替：那条门只钉住「main.go 里有
// defer service.StopAllTelegramPolling()」这一行源码形状，而 StopAll 内部是
// cancel → 等 done → 释放锁 三步，**少了最后一步它照样绿**。真漏的那一步后果是外部的：
// DB 里 polling_owner 仍写着这个已死进程，60s 心跳陈旧窗口内启动的新实例会判定
// 「锁被其他实例持有」而整轮不启动 polling —— 重启后 Telegram 一条消息都不进，
// 要再重启一次才恢复（2026-09-28 排查"三人行无回复"时实际撞到两次）。
func TestStopAllTelegramPollingReleasesHeldLock(t *testing.T) {
	dbConn := setupPollingLockTestDB(t)
	seedTelegramAccount(t, dbConn, 500, "test-acc-500", "test-token-500")
	ctx := context.Background()

	repo := repository.NewTelegramPollingLockRepositoryWithDB(dbConn)
	withPollingLockRepo(t, repo)

	// 前置钉成 Fatal：StopAll 会 cancel 并等 done 它看到的每一条 worker，别的用例在这里
	// 留了状态的话，本用例会把它的 worker 一起带走（红因会读成"那个用例挂了"）。
	telegramPollingMu.Lock()
	initial := len(telegramPollingStates)
	telegramPollingMu.Unlock()
	if initial != 0 {
		t.Fatalf("polling 注册表初始非空（%d 条）：先归属那几条是谁留的再重跑", initial)
	}

	workerID := GetPollingWorkerID() // 先固化：直接写 pollingWorkerID 会被 sync.Once 覆盖
	acquired, owner, _, err := TryAcquirePollingLock(ctx, nil, 500)
	if err != nil || !acquired || owner != workerID {
		t.Fatalf("前置抢占失败: acquired=%v owner=%s err=%v", acquired, owner, err)
	}

	wctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		<-wctx.Done()
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("清理时 worker 的 done 通道 2s 内没关闭")
		}
		telegramPollingMu.Lock()
		delete(telegramPollingStates, 500)
		telegramPollingMu.Unlock()
		_ = repo.ReleasePollingLock(ctx, "other-host:9999", 500)
	})

	// 装一条与 StartTelegramPolling 等价的 worker 状态：lockHeld=true 才是释放分支的开关
	telegramPollingMu.Lock()
	telegramPollingStates[500] = &telegramPollingState{cancel: cancel, done: done, lockHeld: true}
	telegramPollingMu.Unlock()

	if !IsPollingLockHeldByMe(ctx, nil, 500) {
		t.Fatal("前置：关停前锁应握在本进程名下")
	}

	StopAllTelegramPolling()

	if IsPollingLockHeldByMe(ctx, nil, 500) {
		t.Errorf("StopAllTelegramPolling 后锁仍在本进程名下 ⇒ 释放那一步没执行")
	}

	// 下一个实例必须**立刻**抢得上：要等心跳陈旧窗口才恢复的话，重启后那 60s 里零消息，
	// 而进程日志一切正常 —— 正是本次报障的形态。
	acquired2, info2, err2 := repo.TryAcquirePollingLock(ctx, "other-host:9999", 500)
	if err2 != nil {
		t.Fatalf("新实例抢占出错: %v", err2)
	}
	if !acquired2 {
		t.Errorf("关停后新实例抢不上锁（owner=%s lastHeartbeat=%v）⇒ 必须等心跳陈旧窗口才能恢复 polling",
			info2.Owner, info2.LastHeartbeat)
	}
}

// TestStartTelegramPollingMarksStateLockHeld 抢占成功时注册的 worker 状态必须写 lockHeld: true
//
// 上一条行为用例自己写了 `lockHeld: true`，所以它证的是「释放分支在 lockHeld=true 时会跑」，
// 不是「StartTelegramPolling 会把它写成 true」。那一半为什么要单独钉，且只按形状钉：
//   - 走真路径要放一个 worker 协程出去打 api.telegram.org（用例不许联网），而且它拿到 401 后
//     会自己退出、顺手把注册表条目删掉并释放锁 —— 那时 StopAll 看到的是空表，
//     释放这条腿根本没被走到，绿是假绿。
//   - 少了这道的后果：polling 正常在跑，关停时一条锁都不放（`if s.lockHeld` 整块被跳过），
//     上一条行为用例照绿（它自带 true）、cmd/api 的形状门也照绿（defer 那行没动），
//     而重启后的实例要在 60s 心跳陈旧窗口之后才起来 —— 正是本次报障里我实际撞到两次的形态。
//
// 判据取 StartTelegramPolling 函数体内 `&telegramPollingState{…}` 那一条字面量，不取全文件：
// 别处（别的构造点、被注释掉的历史代码）出现一个 `lockHeld: true` 不能替这一条顶数。
func TestStartTelegramPollingMarksStateLockHeld(t *testing.T) {
	path := filepath.Join(".", "telegram_polling.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	code := goCodeOnly(t, path, raw)

	start := strings.Index(code, "func StartTelegramPolling(")
	if start < 0 {
		t.Fatal("telegram_polling.go 里已找不到 StartTelegramPolling：判据的参照物消失了")
	}
	rest := code[start:]
	// 函数体止于第一个「行首 }」：内部 if / go 闭包的右括号都在缩进里，不会误伤
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatal("StartTelegramPolling 的花括号未配对，判据本身失效")
	}
	body := rest[:end]

	litAt := strings.Index(body, "&telegramPollingState{")
	if litAt < 0 {
		t.Fatal("StartTelegramPolling 里没有 &telegramPollingState{ 构造 ⇒ 抢占成功后没有可被关停的 worker 状态")
	}
	lit := body[litAt:]
	// 配对到该复合字面量自己的右括号：`make(chan struct{})` 里那对花括号会先出现，
	// 按"第一个 }"截断会在 lockHeld 之前收口（第一次写就红给自己看过）。
	depth := 0
	closed := -1
	for i := 0; i < len(lit); i++ {
		switch lit[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closed = i
			}
		}
		if closed >= 0 {
			break
		}
	}
	if closed < 0 {
		t.Fatal("&telegramPollingState{ 的右花括号未配对，判据本身失效")
	}
	lit = lit[:closed+1]
	if !strings.Contains(lit, "lockHeld: true") {
		t.Errorf("StartTelegramPolling 注册的 worker 状态没写 lockHeld: true（实际字面量：%s）"+
			"⇒ 关停时 ReleasePollingLock 那一步永远走不到，重启后要在心跳陈旧窗口之后才恢复 polling", lit)
	}
}
