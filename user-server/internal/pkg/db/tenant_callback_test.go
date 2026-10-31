package db

import (
	"context"
	"sort"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenancyidx"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// tenancyProbeModel 回调测试专用模型（独立表，不进 allModels）
type tenancyProbeModel struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	TenantID uint   `gorm:"not null;default:1;index" json:"tenant_id"`
	Name     string `gorm:"type:varchar(60)" json:"name"`
}

func (tenancyProbeModel) TableName() string { return "tenancy_probe" }

// tenancyGlobalModel 无 TenantID 的全局表探针
type tenancyGlobalModel struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"type:varchar(60)" json:"name"`
}

func (tenancyGlobalModel) TableName() string { return "tenancy_global_probe" }

func setupTenantCallbackDB(t *testing.T) *gorm.DB {
	t.Helper()
	database := testutil.NewTestDB(t, &tenancyProbeModel{}, &tenancyGlobalModel{})
	// 登记表由 testdb 钩子内省装配（覆盖本夹具两张表）；生产由 db.AutoMigrate 装配
	tenancyidx.RegisterTenantCallbacks(database)
	t.Cleanup(func() {
		tenant.SetMode(tenant.ModeOff)
		tenancyidx.ResetRegistryForTest()
	})
	return database
}

func TestTenantCallback_OffModePassthrough(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOff)

	ctx := tenant.WithTenantID(context.Background(), 42)
	if err := database.WithContext(ctx).Create(&tenancyProbeModel{Name: "a"}).Error; err != nil {
		t.Fatalf("off 模式创建失败: %v", err)
	}
	var got tenancyProbeModel
	if err := database.WithContext(ctx).First(&got, "name = ?", "a").Error; err != nil {
		t.Fatalf("off 模式查询失败: %v", err)
	}
	if got.TenantID != 1 {
		t.Fatalf("off 模式 tenant_id 应为列默认值 1，实际 %d", got.TenantID)
	}
	// off 模式无租户 ctx 也不报错（直通）
	if err := database.WithContext(context.Background()).First(&got, "name = ?", "a").Error; err != nil {
		t.Fatalf("off 模式无租户 ctx 查询应直通: %v", err)
	}
}

func TestTenantCallback_OnModeFillAndScope(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 42)
	ctxB := tenant.WithTenantID(context.Background(), 43)

	// A 写两行：tenant_id 由 callback 自动填充
	for _, name := range []string{"a1", "a2"} {
		if err := database.WithContext(ctxA).Create(&tenancyProbeModel{Name: name}).Error; err != nil {
			t.Fatalf("on 模式创建失败: %v", err)
		}
	}
	// B 写一行
	if err := database.WithContext(ctxB).Create(&tenancyProbeModel{Name: "b1"}).Error; err != nil {
		t.Fatalf("on 模式创建失败: %v", err)
	}

	var nA, nB int64
	if err := database.WithContext(ctxA).Model(&tenancyProbeModel{}).Count(&nA).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctxB).Model(&tenancyProbeModel{}).Count(&nB).Error; err != nil {
		t.Fatal(err)
	}
	if nA != 2 || nB != 1 {
		t.Fatalf("租户隔离计数错误：A=%d(期望2) B=%d(期望1)", nA, nB)
	}

	// A 按 B 的行名查不到（不存在的行）
	var got tenancyProbeModel
	if err := database.WithContext(ctxA).First(&got, "name = ?", "b1").Error; err == nil {
		t.Fatal("租户 A 不应看到租户 B 的数据")
	}

	// 非租户表（无 TenantID）不受隔离：全局表 A/B 均可见
	if err := database.WithContext(ctxA).Create(&tenancyGlobalModel{Name: "g"}).Error; err != nil {
		t.Fatalf("全局表创建失败: %v", err)
	}
	if err := database.WithContext(ctxB).First(&tenancyGlobalModel{}, "name = ?", "g").Error; err != nil {
		t.Fatalf("非租户表不应被隔离拦截: %v", err)
	}
}

// TestTenantCallback_OnModeBatchCreateFill 批量 Create(&[]*Model) 的逐行填充。
//
// 回归钉的是一个真实 panic：fillCreateForTenant 曾直接对**切片**形态的
// stmt.ReflectValue 取 Field —— on 模式 + 有租户绑定 ctx + 批量写租户表
// = "reflect: call of reflect.Value.Field on slice Value" 进程崩溃。
// 模块激活（Sync 落点归并批量落下单租户）就是这条路径的真实调用方。
func TestTenantCallback_OnModeBatchCreateFill(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)
	ctxA := tenant.WithTenantID(context.Background(), 51)
	ctxB := tenant.WithTenantID(context.Background(), 52)

	// 批量写：每行的 tenant_id 都由绑定 ctx 填充
	rowsA := []*tenancyProbeModel{{Name: "ba1"}, {Name: "ba2"}}
	if err := database.WithContext(ctxA).Create(&rowsA).Error; err != nil {
		t.Fatalf("on 模式批量创建失败: %v", err)
	}
	rowsB := []*tenancyProbeModel{{Name: "bb1", TenantID: 52}} // 行自证值与绑定一致
	if err := database.WithContext(ctxB).Create(&rowsB).Error; err != nil {
		t.Fatalf("on 模式批量创建（显式一致值）失败: %v", err)
	}
	// 混入与绑定不一致的显式值：整批 fail-closed
	rowsBad := []*tenancyProbeModel{{Name: "bad", TenantID: 99}}
	if err := database.WithContext(ctxA).Create(&rowsBad).Error; err == nil {
		t.Fatal("批量写入中 tenant_id 与上下文不一致的行必须 fail-closed")
	}

	var got []tenancyProbeModel
	if err := database.WithContext(ctxA).Order("name").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].TenantID != 51 || got[1].TenantID != 51 {
		t.Fatalf("批量行应全部落租户 51，实际 %+v", got)
	}

	// 无绑定的批量创建：只要存在零值行就 fail-closed（显式值行的自证放行
	// 不改变整批报错的结论）
	rowsUnbound := []*tenancyProbeModel{{Name: "ub1"}, {Name: "ub2", TenantID: 53}}
	if err := database.WithContext(context.Background()).Create(&rowsUnbound).Error; err == nil {
		t.Fatal("on 模式无租户绑定的批量创建必须 fail-closed")
	}
}

func TestTenantCallback_OnModeFailClosed(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	// 无租户 ctx：查询/创建/更新/删除全部 fail-closed
	if err := database.WithContext(context.Background()).Find(&[]tenancyProbeModel{}).Error; err == nil {
		t.Fatal("on 模式无租户 ctx 查询必须 fail-closed")
	}
	if err := database.WithContext(context.Background()).Create(&tenancyProbeModel{Name: "x"}).Error; err == nil {
		t.Fatal("on 模式无租户 ctx 创建必须 fail-closed")
	}
	if err := database.WithContext(context.Background()).Model(&tenancyProbeModel{}).Where("1=1").Update("name", "y").Error; err == nil {
		t.Fatal("on 模式无租户 ctx 更新必须 fail-closed")
	}
	if err := database.WithContext(context.Background()).Where("1=1").Delete(&tenancyProbeModel{}).Error; err == nil {
		t.Fatal("on 模式无租户 ctx 删除必须 fail-closed")
	}

	// 行自证写入：显式携带 tenant_id 的无 ctx 写入放行（消费端读行取租户的前提）
	row := tenancyProbeModel{TenantID: 42, Name: "self-proof"}
	if err := database.WithContext(context.Background()).Create(&row).Error; err != nil {
		t.Fatalf("行自证写入（显式 tenant_id）应放行: %v", err)
	}

	// 系统作用域豁免：无租户 ctx 系统操作放行（列默认值兜底）
	sysCtx := tenant.WithSystemScope(context.Background())
	if err := database.WithContext(sysCtx).Create(&tenancyProbeModel{Name: "sys"}).Error; err != nil {
		t.Fatalf("系统作用域创建应放行: %v", err)
	}
	if err := database.WithContext(sysCtx).Find(&[]tenancyProbeModel{}).Error; err != nil {
		t.Fatalf("系统作用域查询应放行: %v", err)
	}
}

func TestTenantCallback_OnModeCrossTenantWriteRejected(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 42)
	// 上下文租户 42，行显式声明 tenant_id=43 → 跨租户写拒绝
	if err := database.WithContext(ctxA).Create(&tenancyProbeModel{TenantID: 43, Name: "evil"}).Error; err == nil {
		t.Fatal("跨租户写入必须被拒绝")
	}
	// 一致声明放行
	if err := database.WithContext(ctxA).Create(&tenancyProbeModel{TenantID: 42, Name: "ok"}).Error; err != nil {
		t.Fatalf("租户一致的显式写入应放行: %v", err)
	}
}

func TestTenantCallback_RawGuard(t *testing.T) {
	database := setupTenantCallbackDB(t)

	// off 模式裸 SQL 直通
	tenant.SetMode(tenant.ModeOff)
	if err := database.WithContext(context.Background()).Exec("SELECT 1").Error; err != nil {
		t.Fatalf("off 模式裸 SQL 应直通: %v", err)
	}

	// on 模式裸 SQL 引用租户表 → 拒绝（GORM-only 终态的运行时第二道闸）
	tenant.SetMode(tenant.ModeOn)
	if err := database.WithContext(context.Background()).Exec("SELECT count(*) FROM tenancy_probe").Error; err == nil {
		t.Fatal("on 模式裸 SQL 引用租户表必须被拒绝")
	}
	// 引用非租户表 → 放行
	if err := database.WithContext(context.Background()).Exec("SELECT count(*) FROM tenancy_global_probe").Error; err != nil {
		t.Fatalf("on 模式裸 SQL 引用非租户表应放行: %v", err)
	}
	// 系统作用域豁免
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Exec("SELECT count(*) FROM tenancy_probe").Error; err != nil {
		t.Fatalf("系统作用域裸 SQL 应放行: %v", err)
	}
}

func TestTenantRegistry_InitializeAndProbe(t *testing.T) {
	// 夹具显式建两张探针表，登记表内省覆盖它们
	database := testutil.NewTestDB(t, &tenancyProbeModel{}, &tenancyGlobalModel{})
	tenancyidx.InitTenantRegistry(database)
	t.Cleanup(func() { tenancyidx.ResetRegistryForTest() })

	if !tenancyidx.IsTenantTable("tenancy_probe") {
		t.Fatal("声明 TenantID 的模型表必须进登记表")
	}
	if tenancyidx.IsTenantTable("tenancy_global_probe") {
		t.Fatal("未声明 TenantID 的模型表不应进登记表")
	}
	if tenancyidx.IsTenantTable("") || tenancyidx.IsTenantTable("nonexistent") {
		t.Fatal("空表名/未知表必须返回 false")
	}
	// 共享槽位库会积累其他测试的租户表：只断言相对事实，不锁死数量
	snap := tenancyidx.RegistrySnapshot()
	if !snap["tenancy_probe"] {
		t.Fatalf("登记表快照缺 tenancy_probe: %v", snap)
	}
	_ = database
}

func TestTenantRegistry_ModelDeclarations(t *testing.T) {
	// 对账不变量（模型级，DB 无关）：
	//   ① 核心业务模型必须声明 TenantID（行级隔离的建模前提）
	//   ② 全局/系统模型必须不声明（tenants 自身/参数中心/功能开关/租约等）
	// DB 侧的列存在性由 T007 加列迁移 + AutoMigrate 保证（全链路迁移测试覆盖）。
	models := append(allModels(), ExtraModels()...)
	// ExtraModels 注册点在 service 等包的 init()，db 包测试不 import 它们：
	// 这几个模型直接点名断言（与 RegisterExtraModels 清单人工对账）。
	models = append(models,
		&model.ReachComplianceLog{},
		&model.DelayedOutboundReply{},
	)
	cache := &sync.Map{}

	tableHasTenant := map[string]bool{}
	structOfTable := map[string]string{}
	for _, m := range models {
		s, err := schema.Parse(m, cache, schema.NamingStrategy{})
		if err != nil {
			continue
		}
		has := false
		for _, f := range s.Fields {
			if f.DBName == "tenant_id" {
				has = true
				break
			}
		}
		tableHasTenant[s.Table] = has
		structOfTable[s.Table] = s.Name
	}

	for _, table := range []string{
		"customers", "system_users", "llm_providers", "sop_timers", "short_links",
		"chat_channels", "bridge_accounts", "reach_delayed_outbound",
		"aggregation_watermarks", "msg_hourly_summary",
		"system_config", "system_config_kv",
		"feature_flags", "feature_flag_audit_logs", "feature_flag_eval_logs",
	} {
		if !tableHasTenant[table] {
			t.Errorf("核心业务表 %s（模型 %s）必须声明 TenantID（specs/001-saas-multi-tenant）", table, structOfTable[table])
		}
	}
	globalTables := []string{
		"tenants", "migration_records", "migration_checkpoints",
		"config_params", "config_param_groups", "config_param_audit_logs",
		"platform_config_kv",
		"cron_job_leases", "dictionaries", "dictionary_transitions",
		"backups", "restore_records",
		// 平台级公开目录：模板/技能市场目录条目全租户共享（行不归属租户，
		// 提交人/审核人是运营侧归属，下单归属在 tenant_orders 一侧），故全局。
		"market_catalog_entries",
	}
	global := make(map[string]bool, len(globalTables))
	for _, table := range globalTables {
		global[table] = true
		if tableHasTenant[table] {
			t.Errorf("全局/系统表 %s 不应声明 TenantID（应与迁移豁免清单一致）", table)
		}
	}

	// 反向不变量：未声明 TenantID 的模型必须恰好是上面的全局清单。
	// 少了这一条，新增模型漏写 TenantID 时测试仍是绿的（只会静默落列默认值 1）。
	var missing []string
	for table, has := range tableHasTenant {
		if !has && !global[table] {
			missing = append(missing, table)
		}
	}
	sort.Strings(missing)
	for _, table := range missing {
		t.Errorf("模型表 %s（%s）未声明 TenantID 且不在全局豁免清单内："+
			"租户表请补 TenantID，确为全局表则把表名与理由加入本测试的 globalTables",
			table, structOfTable[table])
	}
	for _, table := range globalTables {
		if _, ok := tableHasTenant[table]; !ok {
			t.Errorf("全局豁免清单里的 %s 未出现在 allModels/ExtraModels 中，清单已过期", table)
		}
	}
}

// aliasedProbeRow 联合查询扫描目标（无 GORM 模型，避免把 JOIN 结果套进探针模型）
type aliasedProbeRow struct {
	ID        uint
	TenantID  uint
	Name      string
	ChildName *string
}

// TestTenantCallback_AliasedTableIsolation 别名表必须按表名而非别名判定租户表。
//
// gorm.Table("x AS alias") 把 Statement.Table 设成 **alias** 本身
// （gorm/chainable_api.go:66-74）；回调若直接拿它查登记表必然 miss，
// 于是 SELECT/UPDATE/DELETE 全部无 WHERE tenant_id —— 跨租户读写。
func TestTenantCallback_AliasedTableIsolation(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 61)
	ctxB := tenant.WithTenantID(context.Background(), 62)
	for _, tc := range []struct {
		ctx  context.Context
		name string
	}{{ctxA, "a1"}, {ctxB, "b1"}} {
		if err := database.WithContext(tc.ctx).Create(&tenancyProbeModel{Name: tc.name}).Error; err != nil {
			t.Fatalf("创建 %s 失败: %v", tc.name, err)
		}
	}

	// 查询：AS 别名与裸别名两种写法
	for _, from := range []string{"tenancy_probe AS tp", "tenancy_probe tp"} {
		var rows []tenancyProbeModel
		if err := database.WithContext(ctxA).Table(from).Order("name").Find(&rows).Error; err != nil {
			t.Fatalf("别名查询 %q 失败: %v", from, err)
		}
		if len(rows) != 1 || rows[0].Name != "a1" {
			t.Fatalf("别名查询 %q 泄漏跨租户行: %+v", from, rows)
		}
	}

	// Scan()/Rows() 走 Row 链而非 Query 链：必须同样注入隔离
	var scanned []tenancyProbeModel
	if err := database.WithContext(ctxA).Table("tenancy_probe AS tp").
		Where("tp.name IN ?", []string{"a1", "b1"}).Scan(&scanned).Error; err != nil {
		t.Fatalf("别名 Scan 失败: %v", err)
	}
	if len(scanned) != 1 || scanned[0].Name != "a1" {
		t.Fatalf("Scan 路径泄漏跨租户行: %+v", scanned)
	}

	// 更新：B 不能借别名改 A 的行
	res := database.WithContext(ctxB).Table("tenancy_probe AS tp").
		Where("tp.name = ?", "a1").Update("name", "hacked")
	if res.Error != nil {
		t.Fatalf("别名更新返回错误: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Fatalf("别名更新跨租户命中 %d 行（应为 0）", res.RowsAffected)
	}
	var got tenancyProbeModel
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		First(&got, "name = ?", "a1").Error; err != nil {
		t.Fatalf("A 的行被跨租户改名/删除: %v", err)
	}

	// 删除：B 不能借别名删 A 的行
	res = database.WithContext(ctxB).Table("tenancy_probe AS tp").
		Where("tp.name = ?", "a1").Delete(&tenancyProbeModel{})
	if res.Error != nil {
		t.Fatalf("别名删除返回错误: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Fatalf("别名删除跨租户命中 %d 行（应为 0）", res.RowsAffected)
	}
	if err := database.WithContext(ctxA).First(&got, "name = ?", "a1").Error; err != nil {
		t.Fatalf("A 的行被跨租户删除: %v", err)
	}
}

// TestTenantCallback_MapCreateFill schema-less（map）Create 必须同样填充/校验 tenant_id。
//
// GORM 的 map Create 没有 Schema，旧实现 `if stmt.Schema == nil { return }`
// 直接放行：INSERT 不含 tenant_id ⇒ 列默认值 1，非默认租户的数据整批落进租户 1。
func TestTenantCallback_MapCreateFill(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 71)
	ctxB := tenant.WithTenantID(context.Background(), 72)

	if err := database.WithContext(ctxA).Table("tenancy_probe").
		Create(map[string]interface{}{"name": "ma"}).Error; err != nil {
		t.Fatalf("map 创建失败: %v", err)
	}

	// 显式一致值放行
	if err := database.WithContext(ctxA).Table("tenancy_probe").
		Create(map[string]interface{}{"name": "ma-ok", "tenant_id": uint(71)}).Error; err != nil {
		t.Fatalf("map 创建（显式一致 tenant_id）失败: %v", err)
	}
	// 显式冲突值拒绝
	if err := database.WithContext(ctxA).Table("tenancy_probe").
		Create(map[string]interface{}{"name": "evil", "tenant_id": uint(72)}).Error; err == nil {
		t.Fatal("map 创建携带其他租户 tenant_id 必须被拒绝")
	}
	// 无租户上下文拒绝
	if err := database.WithContext(context.Background()).Table("tenancy_probe").
		Create(map[string]interface{}{"name": "unbound"}).Error; err == nil {
		t.Fatal("on 模式无租户 ctx 的 map 创建必须 fail-closed")
	}
	// 批量 map 创建逐行填充
	batch := []map[string]interface{}{{"name": "mb1"}, {"name": "mb2"}}
	if err := database.WithContext(ctxA).Table("tenancy_probe").Create(&batch).Error; err != nil {
		t.Fatalf("批量 map 创建失败: %v", err)
	}

	var rows []tenancyProbeModel
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Order("name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("map 创建行数 = %d，期望 4: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row.TenantID != 71 {
			t.Fatalf("map 创建行 %s 落错租户: tenant_id=%d（期望 71）", row.Name, row.TenantID)
		}
	}

	// B 读不到 A 的 map 行
	var bCount int64
	if err := database.WithContext(ctxB).Table("tenancy_probe").Count(&bCount).Error; err != nil {
		t.Fatal(err)
	}
	if bCount != 0 {
		t.Fatalf("租户 B 看到了 A 的 map 创建行：%d", bCount)
	}
}

// TestTenantCallback_UpdateCannotChangeTenantID 更新语句不得改写 tenant_id。
//
// Update/Updates/Assignments 都能把 SET tenant_id=其他租户；仅注入 WHERE
// 拦不住"把自己的行搬去/搬来别的租户"——搬走后原租户仍可读到已归属他人的行，
// 或把他人行搬进自己租户（若 WHERE 被绕过）。一律拒绝。
func TestTenantCallback_UpdateCannotChangeTenantID(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 81)
	ctxB := tenant.WithTenantID(context.Background(), 82)
	if err := database.WithContext(ctxA).Create(&tenancyProbeModel{Name: "a1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctxB).Create(&tenancyProbeModel{Name: "b1"}).Error; err != nil {
		t.Fatal(err)
	}

	// 非租户列更新不能命中他人行
	res := database.WithContext(ctxA).Model(&tenancyProbeModel{}).
		Where("name = ?", "b1").Update("name", "hacked")
	if res.Error != nil {
		t.Fatalf("普通更新返回错误: %v", res.Error)
	}
	if res.RowsAffected != 0 {
		t.Fatalf("租户 A 改到了 B 的行（命中 %d）", res.RowsAffected)
	}

	for name, exec := range map[string]func() error{
		"Update 列式": func() error {
			return database.WithContext(ctxA).Model(&tenancyProbeModel{}).
				Where("name = ?", "a1").Update("tenant_id", 82).Error
		},
		"Updates map": func() error {
			return database.WithContext(ctxA).Model(&tenancyProbeModel{}).
				Where("name = ?", "a1").Updates(map[string]interface{}{"tenant_id": uint(82)}).Error
		},
		"Updates struct": func() error {
			return database.WithContext(ctxA).Model(&tenancyProbeModel{}).
				Where("name = ?", "a1").Updates(tenancyProbeModel{TenantID: 82}).Error
		},
		"Clauses Assignments": func() error {
			return database.WithContext(ctxA).Model(&tenancyProbeModel{}).
				Clauses(clause.Assignments(map[string]interface{}{"tenant_id": uint(82)})).
				Where("name = ?", "a1").Updates(map[string]interface{}{}).Error
		},
	} {
		if err := exec(); err == nil {
			t.Fatalf("%s 改写 tenant_id 必须被拒绝", name)
		}
	}

	// 一致性写入（同租户）放行
	if err := database.WithContext(ctxA).Model(&tenancyProbeModel{}).
		Where("name = ?", "a1").Update("tenant_id", 81).Error; err != nil {
		t.Fatalf("同租户 tenant_id 更新应放行: %v", err)
	}
	var got tenancyProbeModel
	if err := database.WithContext(ctxA).First(&got, "name = ?", "a1").Error; err != nil {
		t.Fatalf("租户 A 的行归属被改写: %v", err)
	}
	if got.TenantID != 81 {
		t.Fatalf("tenant_id 被改写为 %d", got.TenantID)
	}
}

// TestTenantCallback_JoinToTenantTableScoped JOIN 中的租户表必须同样受隔离。
//
// 主表是全局表时旧实现只检查主表 → JOIN 上的租户表整表可见（跨租户 union）。
// LEFT JOIN 的过滤条件必须进 ON 子句：进 WHERE 会把未匹配的父行一起吞掉，
// 改变左连接语义。
func TestTenantCallback_JoinToTenantTableScoped(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	ctxA := tenant.WithTenantID(context.Background(), 91)
	ctxB := tenant.WithTenantID(context.Background(), 92)
	if err := database.WithContext(ctxA).Create(&tenancyProbeModel{Name: "ja"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.WithContext(ctxB).Create(&tenancyProbeModel{Name: "jb"}).Error; err != nil {
		t.Fatal(err)
	}
	// 全局表两行，id 与租户表行对齐
	for _, id := range []uint{1, 2} {
		if err := database.WithContext(ctxA).Create(&tenancyGlobalModel{ID: id, Name: "g"}).Error; err != nil {
			t.Fatal(err)
		}
	}

	var rows []aliasedProbeRow
	if err := database.WithContext(ctxA).Table("tenancy_global_probe AS g").
		Select("g.id, tp.tenant_id, tp.name").
		Joins("JOIN tenancy_probe AS tp ON tp.id = g.id").
		Order("g.id").Scan(&rows).Error; err != nil {
		t.Fatalf("JOIN 查询失败: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "ja" || rows[0].TenantID != 91 {
		t.Fatalf("JOIN 泄漏跨租户行: %+v", rows)
	}

	// 无租户 ctx：JOIN 到租户表必须 fail-closed，而不是放行全量
	var leak []aliasedProbeRow
	if err := database.WithContext(context.Background()).Table("tenancy_global_probe AS g").
		Select("g.id, tp.name").
		Joins("JOIN tenancy_probe AS tp ON tp.id = g.id").
		Scan(&leak).Error; err == nil {
		t.Fatalf("无租户 ctx 的 JOIN 必须 fail-closed，实际返回 %+v", leak)
	}

	// LEFT JOIN 语义：过滤进 ON，不匹配的子行仍保留父行
	var left []aliasedProbeRow
	if err := database.WithContext(ctxA).Table("tenancy_probe AS parent").
		Select("parent.id, parent.tenant_id, parent.name, child.name AS child_name").
		Joins("LEFT JOIN tenancy_probe AS child ON child.id = parent.id + 1000").
		Scan(&left).Error; err != nil {
		t.Fatalf("LEFT JOIN 查询失败: %v", err)
	}
	if len(left) != 1 || left[0].Name != "ja" || left[0].ChildName != nil {
		t.Fatalf("LEFT JOIN 语义被 WHERE 过滤破坏: %+v", left)
	}
}

// TestTenantCallback_RawGuardIdentifierForms 预构建 SQL 的租户表识别必须覆盖
// 引号标识符 / ONLY / schema 限定 / TRUNCATE 等真实 SQL 形态。
func TestTenantCallback_RawGuardIdentifierForms(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)

	for _, sqlText := range []string{
		`SELECT count(*) FROM "tenancy_probe"`,
		"SELECT count(*) FROM `tenancy_probe`",
		`SELECT count(*) FROM ONLY tenancy_probe`,
		`SELECT count(*) FROM public.tenancy_probe`,
		`UPDATE "tenancy_probe" SET name = 'x'`,
		`DELETE FROM ONLY "tenancy_probe"`,
		`TRUNCATE tenancy_probe`,
		`TRUNCATE TABLE public."tenancy_probe"`,
		`INSERT INTO "tenancy_probe" (name) VALUES ('x')`,
		`COPY tenancy_probe FROM STDIN`,
		`ALTER TABLE "tenancy_probe" ADD COLUMN x int`,
	} {
		if err := database.WithContext(context.Background()).Exec(sqlText).Error; err == nil {
			t.Fatalf("裸 SQL 引用租户表必须被拒绝: %s", sqlText)
		}
	}
	// 非租户表与系统作用域不受影响
	if err := database.WithContext(context.Background()).Exec(`SELECT count(*) FROM "tenancy_global_probe"`).Error; err != nil {
		t.Fatalf("非租户表裸 SQL 应放行: %v", err)
	}
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).Exec(`SELECT count(*) FROM "tenancy_probe"`).Error; err != nil {
		t.Fatalf("系统作用域裸 SQL 应放行: %v", err)
	}
}

// TestTenantCallback_RegistryUninitializedFailClosed 登记表未装配时 on 模式
// 必须整体 fail-closed：nil 登记表查什么都是"非租户表"= 全量放行。
func TestTenantCallback_RegistryUninitializedFailClosed(t *testing.T) {
	database := setupTenantCallbackDB(t)
	tenant.SetMode(tenant.ModeOn)
	tenancyidx.ResetRegistryForTest()
	t.Cleanup(func() {
		tenancyidx.InitTenantRegistry(database)
	})

	ctx := tenant.WithTenantID(context.Background(), 1)
	if err := database.WithContext(ctx).Find(&[]tenancyProbeModel{}).Error; err == nil {
		t.Fatal("登记表未初始化时 on 模式查询必须 fail-closed")
	}
	if err := database.WithContext(ctx).Create(&tenancyProbeModel{Name: "x"}).Error; err == nil {
		t.Fatal("登记表未初始化时 on 模式创建必须 fail-closed")
	}
	if err := database.WithContext(ctx).Exec("SELECT 1").Error; err == nil {
		t.Fatal("登记表未初始化时 on 模式裸 SQL 无法判定表归属，必须 fail-closed")
	}
	// 系统作用域仍可用（迁移/备份装配期）
	if err := database.WithContext(tenant.WithSystemScope(context.Background())).
		Find(&[]tenancyProbeModel{}).Error; err != nil {
		t.Fatalf("系统作用域不应受登记表就绪检查影响: %v", err)
	}
}

// 防止 sync import 在未来重构中丢失（登记表并发语义依赖它）
var _ = sync.RWMutex{}
