// system_config_kv_nil_test.go 阶段0.4：nil 句柄必须是 error，不能是 panic。
//
// 为什么单独一格：`NewSystemConfigKVRepository()` 捕获的是**构造那一刻**的
// db.GetDB()，装配顺序不对时它是 nil。而 nil *gorm.DB 走进 Get/Upsert/EnsureTable
// 不是返回 error，是当场崩——一条 panic 会带走整个测试二进制（剩下的用例一条不报），
// 于是"装配顺序写错"这件事在 CI 上表现为一片莫名红，没人往装配顺序上想。
//
// 真实事故已经有前科：internal/router/browser_automation_routes.go 里那条 agent
// 工具链就是拿全局句柄造 KV 仓储的（同一函数里另外 9 个仓储都走传进来的 gormDB），
// 全局句柄为 nil 时崩。（该装配点已改为 WithDB，本文件同时把它钉住。）
//
// 这一族用例全部不需要真库：它们验的是"句柄没了怎么表现"，与表结构无关。
package repository

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	dbutil "hivemtk-user/internal/pkg/db"
)

func TestSystemConfigKVNilHandleReturnsError(t *testing.T) {
	repo := NewSystemConfigKVRepositoryWithDB(nil)
	if repo.Available() {
		t.Fatal("nil 句柄时 Available 应为 false")
	}
	ctx := context.Background()

	// 三个方法都要报错，且报的是同一个哨兵错：调用侧要能 errors.Is 一把抓住，
	// 而不是各自返回一句不同的话（那就得在三处分别判）。
	if _, err := repo.Get(ctx, "k"); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Errorf("nil 句柄下 Get 应返回 ErrKVStoreUnavailable，实际 %v", err)
	}
	if _, err := repo.Upsert(ctx, "k", "v"); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Errorf("nil 句柄下 Upsert 应返回 ErrKVStoreUnavailable，实际 %v", err)
	}
	if err := repo.EnsureTable(ctx); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Errorf("nil 句柄下 EnsureTable 应返回 ErrKVStoreUnavailable，实际 %v", err)
	}
}

// TestSystemConfigKVNilReceiverIsSafe 指针接收者本身为 nil 也不能崩。
//
// 与上面不是一回事：上面是"仓储在、句柄没了"，这里是"仓储也没了"。
// 后者来自服务字段未注入就被调用（service 层的字段多为接口、nil 是合法零值），
// 崩在这里的代价同样是整条请求 500 之外还带走 goroutine。
func TestSystemConfigKVNilReceiverIsSafe(t *testing.T) {
	// 把一个具体类型的 nil 装进接口，制造"仓储指针为 nil"的运行态。
	var repo SystemConfigKVRepository = (*systemConfigKVRepo)(nil)

	if repo.Available() {
		t.Fatal("仓储指针为 nil 时 Available 应为 false")
	}
	ctx := context.Background()
	if _, err := repo.Get(ctx, "k"); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Errorf("仓储指针为 nil 时 Get 应返回 ErrKVStoreUnavailable，实际 %v", err)
	}
}

// TestSystemConfigKVNilGlobalHandleIsNotPanic 走真实构造路径复现装配顺序写错的坏法。
//
// 这一格是上面两格的前因：真实代码不是显式传 nil，而是"构造那一刻全局句柄还没指好"。
// 直接把全局置空再构造，才走的是 NewSystemConfigKVRepository 自己那条路。
func TestSystemConfigKVNilGlobalHandleIsNotPanic(t *testing.T) {
	prev := dbutil.GetDB()
	t.Cleanup(func() { dbutil.SetTestDB(prev) })
	dbutil.SetTestDB(nil)

	repo := NewSystemConfigKVRepository()
	if repo.Available() {
		t.Fatal("全局句柄为空时构造出的仓储不该 Available")
	}
	if _, err := repo.Get(context.Background(), "k"); !errors.Is(err, ErrKVStoreUnavailable) {
		t.Errorf("全局句柄为空时 Get 应返回 ErrKVStoreUnavailable，实际 %v", err)
	}
}

// TestSystemConfigKVMissingKeyIsNotUnavailable 未命中与句柄没了必须能分开。
//
// 这两个态在值上都是"空字符串"，只有 error 不同。如果这里退成 ("", nil)，
// 运维把配置改了不生效时看到的是"没配过"，而真相是"库没指对"——查不到根因。
// 下面这格需要真库，跑不到就按 testutil 的设计跳过。
func TestSystemConfigKVMissingKeyIsNotUnavailable(t *testing.T) {
	database := testutil.NewTestDBOrSkip(t, &model.SystemConfigKV{})
	repo := NewSystemConfigKVRepositoryWithDB(database)

	value, err := repo.Get(context.Background(), "definitely-absent-key")
	if err != nil {
		t.Fatalf("未命中应是 nil error（调用侧据此回落默认值），实际 %v", err)
	}
	if value != "" {
		t.Errorf("未命中的值应为空串，实际 %q", value)
	}
	if errors.Is(err, ErrKVStoreUnavailable) {
		t.Error("未命中绝不能报 ErrKVStoreUnavailable：那是装配问题，不是配置问题")
	}
}
