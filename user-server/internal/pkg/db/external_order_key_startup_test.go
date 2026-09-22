// external_order_key_startup_test.go 钉住"改键之后，旧那把单列唯一索引必须真的从库里消失"。
//
// 为什么必须有这一道（G15 第①条的收尾）：external_orders 的幂等键从 order_id 改成
// (platform, order_id) 只写在模型标签上，而 **AutoMigrate 只加不删** ——
// 已部署的库里那把 `uni_external_orders_order_id` 还在，且还在生效。
// 于是"改键"在存量库上是个**看起来完成了的动作**：新索引建出来了，标签也改了，
// 而两家平台用同一个订单号时第二条回调仍然撞 23505，签名完全合法的推送照样被丢掉。
// 本文件的第 ① 臂就是拿真库把这句话证出来（它同时也是"没有钩子时坏形状真的坏"的反向测试）。
//
// 版本化迁移链在当前启动口径下永不执行（建表真值是 AutoMigrate，见 migrate.go 头部），
// 所以删旧索引挂在 post-migrate，与 clue_id / obs_config / 明文重置令牌那三道同形状。
//
// 四条臂：
//
//	① 旧形状在场时跨平台同号被拒（夹具自证，且证明 AutoMigrate 不删它）；
//	② 钩子跑完：旧索引名在 pg_class 里查不到，复合唯一在库里；
//	③ 跨平台同号两行并存，而**同平台同号**仍然被拒（复合键没被顺手放宽成非唯一）；
//	④ 钩子可重跑，存量行一行不少。
package db

import (
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func extOrderSeed(platform, orderID string) *model.ExternalOrder {
	return &model.ExternalOrder{Platform: platform, OrderID: orderID, Status: "paid", PayAmount: 370}
}

func TestStartupDropsLegacyExternalOrderOrderIDKey(t *testing.T) {
	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	// 收尾把表恢复成正形状：影子库按进程共享，留一个"只有旧索引没有新索引"的现场
	// 会让同二进制里后面的用例读到错的形状。
	defer func() {
		if err := db.Migrator().DropTable(&model.ExternalOrder{}); err != nil {
			t.Logf("收尾删表失败（不影响判据）: %v", err)
		}
		if err := db.AutoMigrate(&model.ExternalOrder{}); err != nil {
			t.Logf("收尾重建表失败（不影响判据）: %v", err)
		}
	}()

	if err := db.AutoMigrate(&model.ExternalOrder{}); err != nil {
		t.Fatalf("按新模型建表失败: %v", err)
	}
	// 造**改键之前**的现场：单列唯一索引，名字就是 GORM 当年按默认规则拼出来的那个。
	if err := db.Exec("CREATE UNIQUE INDEX " + model.ExternalOrderLegacyOrderIDIndex +
		" ON external_orders (order_id)").Error; err != nil {
		t.Fatalf("植入旧索引失败: %v", err)
	}

	if err := db.Create(extOrderSeed("taobao", "88001")).Error; err != nil {
		t.Fatalf("第一行写入失败: %v", err)
	}
	// ① 旧索引此刻仍在拦着跨平台同号 —— 这一条同时是"AutoMigrate 不删旧索引"的证据：
	// 上面已经按新模型跑过 AutoMigrate 了，如果它会删，这里就该写得进去。
	err := db.Create(extOrderSeed("jd", "88001")).Error
	if err == nil {
		t.Fatal("① 旧形状的夹具没咬住：跨平台同号写得进去，那本用例第 ③ 臂就没有判据了")
	}
	if !strings.Contains(err.Error(), "23505") || !strings.Contains(err.Error(), model.ExternalOrderLegacyOrderIDIndex) {
		t.Fatalf("① 撞的不是那把旧索引（错误 %v，期望含 23505 与 %s）：夹具没复现出坏形状",
			err, model.ExternalOrderLegacyOrderIDIndex)
	}

	postMigrateDropLegacyExternalOrderKey(db)

	// ② 旧索引真的不在了（读 pg_class，不读 Migrator().HasIndex：那一层按名字对账，
	// 名字被拼错时它会连着一并错过去）。
	if n := extOrderLegacyIndexRows(t, db); n != 0 {
		t.Fatalf("② 钩子跑完旧索引还在库里（pg_class 命中 %d 行）⇒ 跨平台同号继续被拦", n)
	}
	comp, ok := tableIndexOf(t, db, "external_orders", model.ExternalOrderScopedUniqueIndex)
	if !ok {
		t.Fatalf("② 复合唯一索引 %s 不在库里，实得 %v", model.ExternalOrderScopedUniqueIndex,
			colsOf(tableIndexes(t, db, "external_orders")))
	}
	if !comp.Uniq || comp.Cols != "platform+order_id" {
		t.Errorf("② %s 形状不对：cols=%q uniq=%v，期望 platform+order_id 且唯一", comp.Name, comp.Cols, comp.Uniq)
	}

	// ③ 坏的那一面没了，好的那一面还在。
	if err := db.Create(extOrderSeed("jd", "88001")).Error; err != nil {
		t.Errorf("③ 改键没生效：另一家用同一个订单号仍然记不进来 %v", err)
	}
	if err := db.Create(extOrderSeed("taobao", "88001")).Error; err == nil {
		t.Error("③ 同平台同订单号写进了两行：幂等键被放宽成非唯一，镜像每次重投都多一行")
	}

	// ④ 可重跑，且不动存量。
	before := extOrderRowCount(t, db)
	if before < 2 {
		t.Fatalf("④ 夹具行数只有 %d，后面的幂等判据没有对象", before)
	}
	postMigrateDropLegacyExternalOrderKey(db)
	postMigrateDropLegacyExternalOrderKey(db)
	if after := extOrderRowCount(t, db); after != before {
		t.Errorf("④ 复跑钩子之后行数从 %d 变 %d ⇒ 删旧索引不该动数据", before, after)
	}
	if n := extOrderLegacyIndexRows(t, db); n != 0 {
		t.Errorf("④ 复跑把旧索引建回来了（命中 %d 行）", n)
	}
}

// TestAutoMigrateWiresExternalOrderLegacyKeyDrop 装配点锁：钩子写了没人调，上面四条臂就只是
// 一个没接的函数 —— 而"没人调"在线上表现为存量库永远删不掉旧索引（本文件①那一撞）。
// 按整行匹配计数（定义那一行也含同名子串），并钉住它排在其余 post-migrate 钩子之后。
func TestAutoMigrateWiresExternalOrderLegacyKeyDrop(t *testing.T) {
	src, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("读 migrate.go 失败: %v", err)
	}
	const call = "\tpostMigrateDropLegacyExternalOrderKey(DB)"
	const define = "func postMigrateDropLegacyExternalOrderKey(db *gorm.DB) {"
	const afterLine = "\tpostMigrateDropLegacyPlaintextResetTokens(DB)"
	calls, defines, afterAt, callAt := 0, 0, -1, -1
	for i, line := range strings.Split(string(src), "\n") {
		switch line {
		case call:
			calls++
			callAt = i
		case define:
			defines++
		case afterLine:
			afterAt = i
		}
	}
	if defines != 1 {
		t.Errorf("钩子定义出现 %d 次，期望 1", defines)
	}
	if calls != 1 {
		t.Errorf("AutoMigrate 里那句调用出现 %d 次，期望 1 ⇒ 旧索引删除没接线（或重复接线）", calls)
	}
	if afterAt < 0 {
		t.Fatal("找不到明文令牌那道钩子的调用行 ⇒ 本腿的位置参照消失，需人工复核 migrate.go")
	}
	if callAt >= 0 && callAt < afterAt {
		t.Errorf("调用在第 %d 行、明文令牌钩子在第 %d 行 ⇒ 删旧索引必须排在建表终校验与其余钩子之后", callAt+1, afterAt+1)
	}
	// 索引名只能来自 model 的那两枚常量：在钩子里再写一遍字面量，
	// 常量改名后钩子会去 DROP 一个不存在的名字并**静默成功**。
	body := string(src)
	if i := strings.Index(body, define); i >= 0 {
		j := strings.Index(body[i:], "\nfunc ")
		if j > 0 {
			hook := body[i : i+j]
			for _, lit := range []string{`"uni_external_orders_order_id"`, `"uq_external_orders_platform_order_id"`} {
				if strings.Contains(hook, lit) {
					t.Errorf("钩子里出现了索引名字面量 %s：改常量而没改这里，DROP 会打在空名字上并静默成功", lit)
				}
			}
			for _, sym := range []string{"model.ExternalOrderLegacyOrderIDIndex", "model.ExternalOrderScopedUniqueIndex"} {
				if !strings.Contains(hook, sym) {
					t.Errorf("钩子里没引用 %s：名字必须与模型标签同源", sym)
				}
			}
		}
	}
}

// TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally 钉住钩子的**失败处置**：只 Warn，不 panic。
//
// 为什么这一格值得一条判据而不是 migrate.go:621 那句注释：那条路径上唯一可能的"报错方式"
// 就是让进程起不来，而它拦住的坏情况（跨平台同号仍被旧键拦住）是**改键之前的既有行为**。
// 把存量缺陷升级成全站宕机，代价是"所有域一起停"，而收益是零 —— 运维看到的还是同一句
// "订单回调进不来"，只是这次连别的接口也没了。这是一次**决定**，决定就要有判据。
//
// 两臂各钉一个失败入口：① 未接库的启动形状里钩子照样被调到（nil 句柄守卫）；
// ② 连接已断 ⇒ 三条 Exec 的第一条就失败，那一支必须走 Warn+return。
// 判据形状是 recover：钩子没有返回值，"没炸"只能这样测。
func TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally(t *testing.T) {
	call := func(label string, run func()) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s：钩子 panic ⇒ 启动会被一个没删掉的索引带走：%v", label, r)
			}
		}()
		run()
	}
	call("① nil 句柄", func() { postMigrateDropLegacyExternalOrderKey(nil) })

	db := testutil.NewTestDB(t)
	if db == nil {
		t.Fatal("测试库不可达")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接池失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("关连接池失败: %v", err)
	}
	// 只关**本用例自己这份句柄**：NewTestDB 每次开一个新池，库名才是按进程共用的，
	// 所以这里关掉不会碰到同二进制里别的用例的连接。
	call("② 连接池已关", func() { postMigrateDropLegacyExternalOrderKey(db) })
}

func extOrderLegacyIndexRows(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM pg_class WHERE relname = ?`, model.ExternalOrderLegacyOrderIDIndex).
		Scan(&n).Error; err != nil {
		t.Fatalf("查旧索引失败: %v", err)
	}
	return int(n)
}

func extOrderRowCount(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int64
	if err := db.Table("external_orders").Count(&n).Error; err != nil {
		t.Fatalf("统计 external_orders 失败: %v", err)
	}
	return int(n)
}
