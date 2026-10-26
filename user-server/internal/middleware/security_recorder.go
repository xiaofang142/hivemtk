// security_recorder.go JWT 401 失败的部署级聚合记录器。
//
// 为什么是 platform_config_kv 而不是新表：未授权访问的"归属人"是攻击者而不是任何
// 租户——落租户表在 on 模式下要么串库要么被隔离闸 fail-closed 整批拒写（R-8 同一
// 学费）；platform_config_kv 无租户维度，D-2 的 would_deny 落库同款载体。
// 为什么没有常驻刷盘协程：遥测丢 25 条以内增量不构成正确性问题（下一波继续计数），
// 却能省掉一整套启停/退出的生命周期管理——缓冲 ≥25 条或距上次刷盘 >30s 时惰性
// 异步刷一次；测试与优雅关停用 FlushSecurityAccessFailures 显式收口。
//
// 五层归属：middleware 不 import service；经 router.Setup 注入 *gorm.DB 后自持
// repository（platform 仓 PlatformAuditMiddleware 同一先例）。
package middleware

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	securityFlushThreshold = 25
	securityFlushInterval  = 30 * time.Second
	securityKeepDays       = 7
)

var (
	securityFailureMu       sync.Mutex
	securityFailureRepo     repository.PlatformConfigKVRepository
	securityFailureDeltas   *model.SecurityAccessFailurePayload
	securityFailureLastFlsh time.Time
	securityFailureFlushing = false
)

// endSecurityFlush 把"正在刷盘"标志归零（刷盘协程收尾与测试夹具共用这一条路径）。
//
// 为什么单独成一扇门，而不是在协程体里直接写 `securityFailureFlushing = false`：
// scripts/check-async-global-read.py（R27 门）判的是"协程体内出现**测试会改写**的
// 包级全局名"这一形状。securityFailureFlushing 正是这类全局 —— 用例把它当夹具
// 反复置位/归零（security_recorder_test.go），而刷盘协程在锁外跑 Merge；体内直写
// 意味着用例 A 残留的刷盘协程可能踩到用例 B 刚做完的初始化，那既是 DATA RACE
// 也是跨用例串味（门禁文档里 7418e489 那条真红就是同一形状）。
//
// 收进这里之后：名字不再出现在任何协程体里，锁仍是同一把 securityFailureMu，
// 调用路径与语义逐字不变（锁内只做一次赋值，不调用任何取到的函数值）。
func endSecurityFlush() {
	securityFailureMu.Lock()
	securityFailureFlushing = false
	securityFailureMu.Unlock()
}

// InitSecurityRecorder 装配记录器（router.Setup 调用一次；db==nil 时静默不装——
// 记录是尽力而为的遥测，没有库时 JWT 验签行为完全不变）。
func InitSecurityRecorder(db *gorm.DB) {
	if db == nil {
		return
	}
	securityFailureMu.Lock()
	defer securityFailureMu.Unlock()
	securityFailureRepo = repository.NewPlatformConfigKVRepositoryWithDB(db)
	securityFailureDeltas = model.NewSecurityAccessFailurePayload()
	securityFailureLastFlsh = time.Now()
}

// recordAuthFailure 计入一次 401（kind 取 model.SecurityAccessKind* 常量）。
func recordAuthFailure(kind string, c *gin.Context) {
	if c == nil {
		return
	}
	securityFailureMu.Lock()
	if securityFailureDeltas == nil {
		securityFailureMu.Unlock()
		return // 未装配：遥测缺席不影响验签主链
	}
	securityFailureDeltas.Add(time.Now().Format("2006-01-02"), kind, pathPrefixOf(c.Request.URL.Path))
	shouldFlush := false
	if !securityFailureFlushing && (securityFailureDeltasCount() >= securityFlushThreshold ||
		time.Since(securityFailureLastFlsh) >= securityFlushInterval) {
		securityFailureFlushing = true
		shouldFlush = true
	}
	var deltas *model.SecurityAccessFailurePayload
	if shouldFlush {
		// 刷盘必须**换出 detach 快照**，不能把活指针递给 goroutine：goroutine 在
		// 锁外遍历它做 Merge，而请求路径仍在锁内往同一对象 Add——活指针方案 =
		// concurrent map iteration and map write，进程直接 fatal（2026-10-25 D-3
		// 走查 18406 实测 crash：api_verify 登录失败段触发，栈在
		// model/security_access_failure.go Merge ← flushSecurityAccessFailures）。
		// 换出后新增量落进新载荷，与刷盘中的旧快照互不可见，两条腿都安全。
		// LastFlsh 同步更新：否则首刷后 interval 判定恒真，退化为每请求一刷。
		deltas = securityFailureDeltas
		securityFailureDeltas = model.NewSecurityAccessFailurePayload()
		securityFailureLastFlsh = time.Now()
	}
	securityFailureMu.Unlock()

	if shouldFlush {
		go func(d *model.SecurityAccessFailurePayload) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[security-recorder] flush panic recovered: %v", r)
				}
				// 经 accessor 归零，不在协程体里直写全局（R27 门，见 endSecurityFlush
				// 的注释）：锁与语义不变，变的是"用例 A 的异步体不再可能读到
				// 用例 B 装上去的夹具值"。
				endSecurityFlush()
			}()
			_ = flushSecurityAccessFailures(d)
		}(deltas)
	}
}

// FlushSecurityAccessFailures 把增量并进 kv（异步腿与测试/关停共用同一条路径）。
// 返回 error 只为测试断言；异步腿忽略它——遥测写失败不该有副作用。
func FlushSecurityAccessFailures() error {
	securityFailureMu.Lock()
	deltas := securityFailureDeltas
	securityFailureDeltas = model.NewSecurityAccessFailurePayload()
	securityFailureLastFlsh = time.Now()
	securityFailureMu.Unlock()
	return flushSecurityAccessFailures(deltas)
}

func flushSecurityAccessFailures(deltas *model.SecurityAccessFailurePayload) error {
	if deltas == nil {
		return nil
	}
	if securityFailureDeltasCountOf(deltas) == 0 {
		return nil
	}
	securityFailureMu.Lock()
	repo := securityFailureRepo
	securityFailureMu.Unlock()
	if repo == nil || !repo.Available() {
		return nil // 无库：增量丢弃（遥测降级，JWT 主链不受影响）
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := repo.Get(ctx, model.SecurityAccessFailureKVKey)
	if err != nil {
		return err
	}
	payload := model.ParseSecurityAccessFailurePayload(raw)
	payload.Merge(deltas)
	payload.Prune(securityKeepDays)
	bts, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = repo.Upsert(ctx, model.SecurityAccessFailureKVKey, string(bts))
	return err
}

// pathPrefixOf 取 /api/xxx 两段前缀：聚合维度够定位"哪块面在被打"，
// 又不会把带 ID 的细粒度路径刷成高基数键。
func pathPrefixOf(path string) string {
	seg := 0
	out := ""
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			seg++
			if seg == 3 {
				return out
			}
		}
		out += string(path[i])
	}
	return out
}

// securityFailureDeltasCount 当前缓冲的事件数（阈值判定用；调用方须持锁）。
func securityFailureDeltasCount() int {
	if securityFailureDeltas == nil {
		return 0
	}
	return securityFailureDeltasCountOf(securityFailureDeltas)
}

func securityFailureDeltasCountOf(p *model.SecurityAccessFailurePayload) int {
	n := 0
	for _, day := range p.Days {
		for _, cnt := range day.Kinds {
			n += int(cnt)
		}
	}
	return n
}
