// objection_handler_fallback_version_test.go 异议处理兜底版本号异步回填回归
// （AV-04c 批次6 刀2）。
//
// fallbackVersionOf 的句柄解析收口为 repository.NewScriptLibraryRepositoryFromGlobal
// （spawning 之前同步在仓储包内取全局句柄）。本组用例锁三件事：
//  1. 同步契约：调用即返回 1（回填只发生在后台 goroutine）；
//  2. 回填契约：句柄有效时 version_cache 最终被 DB 里的 MAX(version) 填充；
//  3. 无库安全：仓储句柄为 nil 时 MaxScriptVersion 报错被吞，缓存不填充、不 panic。
package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func pollVersionCache(t *testing.T, scriptID uint, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		versionCacheMu.RLock()
		got := versionCache[scriptID]
		versionCacheMu.RUnlock()
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	versionCacheMu.RLock()
	got := versionCache[scriptID]
	versionCacheMu.RUnlock()
	t.Fatalf("version_cache[%d] 未在 %s 内回填为 %d, got %d", scriptID, timeout, want, got)
}

func dropVersionCache(t *testing.T, scriptID uint) {
	t.Helper()
	versionCacheMu.Lock()
	delete(versionCache, scriptID)
	versionCacheMu.Unlock()
}

func TestFallbackVersionOf_BackfillsFromDB(t *testing.T) {
	db := testutil.NewTestDB(t, &model.ScriptVersion{})
	for _, v := range []int{1, 3} {
		if err := db.Create(&model.ScriptVersion{ScriptID: 777, Version: v, Title: "v", Content: "c"}).Error; err != nil {
			t.Fatalf("seed script version %d: %v", v, err)
		}
	}
	dropVersionCache(t, 777)
	defer dropVersionCache(t, 777)

	// 同步契约：立即返回 1，回填交给异步体。
	if got := fallbackVersionOf(context.Background(), 777); got != 1 {
		t.Fatalf("同步返回应恒为 1, got %d", got)
	}
	pollVersionCache(t, 777, 3, 3*time.Second)
}

func TestFallbackVersionOf_ZeroTemplateNoop(t *testing.T) {
	if got := fallbackVersionOf(context.Background(), 0); got != 1 {
		t.Fatalf("templateID=0 应回退 1, got %d", got)
	}
}

// 无库：FromGlobal 得到 db=nil 仓储，MaxScriptVersion 守卫报错被吞——缓存不填充、不 panic。
func TestFallbackVersionOf_NilDB_NoPanicNoCache(t *testing.T) {
	if repository.DBReady() {
		t.Skip("全局句柄被在途测试占用，无库语义由其余 NilDB 用例覆盖")
	}
	dropVersionCache(t, 888)
	defer dropVersionCache(t, 888)
	if got := fallbackVersionOf(context.Background(), 888); got != 1 {
		t.Fatalf("同步返回应恒为 1, got %d", got)
	}
	time.Sleep(150 * time.Millisecond) // 给异步体留出「若实现错误会 panic/回填」的窗口
	versionCacheMu.RLock()
	got := versionCache[888]
	versionCacheMu.RUnlock()
	if got != 0 {
		t.Fatalf("无库不应回填缓存, got %d", got)
	}
}
