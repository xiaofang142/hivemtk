// Package service 提供 Telegram Polling 分布式锁的 service 门面。
//
// 背景（修复 S3-6）：
//   - Telegram Bot API 同一 token 全局只允许一个进程做 getUpdates long polling
//   - 多实例同时 polling 立即触发 409 Conflict，本地只能退避但无法消除
//   - 通过 telegram_accounts.polling_owner / polling_heartbeat_at 字段实现
//     （实际 SQL 由 repository.TelegramPollingLockRepository 负责，本文件仅做
//     service 层门面 + workerID 解析 + 调用转发）
//
// 五层架构：
//   - L5 repository/telegram_polling_lock.go：所有 SQL（UPDATE telegram_accounts），
//     持有 *gorm.DB，service 不得直接调 db.GetDB()（架构文档 §三.4）
//   - L4 service（本文件）：导出 TryAcquirePollingLock / HeartbeatPollingLock
//     / ReleasePollingLock / IsPollingLockHeldByMe 供其他 service 复用；
//     内部统一调 repository，不持有 *gorm.DB，不写 SQL
package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"hivemtk-user/internal/repository"
)

// Polling 锁相关常量（service 层复用）
const (
	PollingLockHeartbeatInterval = 30 * time.Second
)

var (
	pollingWorkerID     string
	pollingWorkerIDOnce sync.Once
)

// GetPollingWorkerID 获取当前进程的稳定 worker 标识（hostname:pid）
// 用于分布式锁的 owner 字段，方便日志 / 监控区分是哪个实例持有锁。
func GetPollingWorkerID() string {
	pollingWorkerIDOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		pollingWorkerID = fmt.Sprintf("%s:%d", host, os.Getpid())
	})
	return pollingWorkerID
}

// pollingLockRepo/pollingLockRepoOnce 的读写只走紧随其后的两扇 accessor
// （pollingLockMu 守，包内其余位置直读 0 处）。要上锁的理由同 tgSeamMu：polling 降级链在
// SafeGo 协程里经 TryAcquirePollingLock 取这个仓储，而用例逐格装桩/还原。
//
// 装桩刻意不原地改 `*pollingLockRepoOnce = sync.Once{}`：那是**透过指针**写一个别的协程
// 正在 Do 的对象，锁住指针变量本身也拦不住那种写。resetPollingLockRepoForTest 换成换指针，
// 于是锁内的一次赋值就是全部并发可见的边界。
var (
	pollingLockMu sync.Mutex

	pollingLockRepo     *repository.TelegramPollingLockRepository
	pollingLockRepoOnce = &sync.Once{}
)

func getPollingLockRepo() *repository.TelegramPollingLockRepository {
	pollingLockMu.Lock()
	defer pollingLockMu.Unlock()
	pollingLockRepoOnce.Do(func() {
		pollingLockRepo = repository.NewTelegramPollingLockRepository()
	})
	return pollingLockRepo
}

// resetPollingLockRepoForTest 测试装桩：锁内一次完成「换仓储 + 让下一次取用看到它」，
// 返回还原函数（同 pkg/db 的 SetTestDB 一条路走 accessor 的口径）。
func resetPollingLockRepoForTest(repo *repository.TelegramPollingLockRepository) func() {
	pollingLockMu.Lock()
	defer pollingLockMu.Unlock()
	prev, prevOnce := pollingLockRepo, pollingLockRepoOnce
	pollingLockRepo = repo
	pollingLockRepoOnce = &sync.Once{}
	pollingLockRepoOnce.Do(func() { pollingLockRepo = repo })
	return func() {
		pollingLockMu.Lock()
		defer pollingLockMu.Unlock()
		pollingLockRepo, pollingLockRepoOnce = prev, prevOnce
	}
}

// TryAcquirePollingLock 原子抢占 Telegram 账号的 polling 锁（service 门面）
//
// 转发到 repository.TelegramPollingLockRepository.TryAcquirePollingLock。
//
// 返回值：
//   - acquired=true → 抢占成功，调用方应启动 worker 并周期性调用 HeartbeatPollingLock
//   - acquired=false → 抢占失败（被其他进程持有且未过期），调用方应放弃启动 polling
func TryAcquirePollingLock(ctx context.Context, _ interface{}, accountID uint) (acquired bool, owner string, lastHeartbeat *time.Time, err error) {
	repo := getPollingLockRepo()
	acq, info, repoErr := repo.TryAcquirePollingLock(ctx, GetPollingWorkerID(), accountID)
	if repoErr != nil {
		if repoErr == repository.ErrPollingLockDBNil {
			return false, "", nil, fmt.Errorf("polling lock: db is nil")
		}
		return false, "", nil, fmt.Errorf("polling lock acquire: %w", repoErr)
	}
	return acq, info.Owner, info.LastHeartbeat, nil
}

// HeartbeatPollingLock 续约锁：仅当 owner 仍是本进程时才更新心跳（service 门面）
//
// 转发到 repository.TelegramPollingLockRepository.HeartbeatPollingLock。
// lockLost=true 表示锁已被其他进程抢占，调用方应停止 worker。
func HeartbeatPollingLock(ctx context.Context, _ interface{}, accountID uint) (lockLost bool, err error) {
	lockLost, err = getPollingLockRepo().HeartbeatPollingLock(ctx, GetPollingWorkerID(), accountID)
	if err != nil {
		if err == repository.ErrPollingLockDBNil {
			return true, fmt.Errorf("polling lock heartbeat: db is nil")
		}
		return true, fmt.Errorf("polling lock heartbeat: %w", err)
	}
	return lockLost, nil
}

// ReleasePollingLock 释放锁：仅当 owner 仍是本进程时才清空（service 门面）
//
// 转发到 repository.TelegramPollingLockRepository.ReleasePollingLock。
func ReleasePollingLock(ctx context.Context, _ interface{}, accountID uint) error {
	if err := getPollingLockRepo().ReleasePollingLock(ctx, GetPollingWorkerID(), accountID); err != nil {
		if err == repository.ErrPollingLockDBNil {
			return fmt.Errorf("polling lock release: db is nil")
		}
		return err
	}
	return nil
}

// IsPollingLockHeldByMe 检查某账号的 polling 锁是否由本进程持有（service 门面）
//
// 用于状态端点 / 调试。
func IsPollingLockHeldByMe(ctx context.Context, _ interface{}, accountID uint) bool {
	return getPollingLockRepo().IsPollingLockHeldByMe(ctx, GetPollingWorkerID(), accountID)
}
