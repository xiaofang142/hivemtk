// obs_config_default_batchm_test.go 钉"默认存储选取面"的七条不变量（N-26 批M）。
//
// 为什么全在真库上跑而不是 mock：这几条缺陷的成因恰好都在 SQL 落地的那一层
// （WHERE 少一个谓词、两条语句中间没有事务、整行 UPDATE 把陈旧列写回），
// mock 只会复现我对实现的想象。
//
// 与仓库里既有那份 obs_config_test.go 的分工：那份按"方法逐个能用"写，
// 本文件按"选取结果必须是什么"写 —— 默认行只有一条、必须是 active、
// 切换默认不许留下零默认、不许被库级守卫挡住、计数器写回不许动到选取列，
// 而多条并存（存量）时取哪一条由 created_at 定死。
// 第六条是"除白名单里的可编辑列之外，任何一条写语句都不许经口写回选取列、状态列与用量列"
// —— R6/R6b/R6c 三腿管的是**编辑口**那份整行 Save（N-26③ 的另一半）。
package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

func batchMNewRepo(t *testing.T) (ObsConfigRepository, *gorm.DB) {
	t.Helper()
	gdb := setupObsConfigTestDB(t)
	return NewObsConfigRepository(), gdb
}

// batchMCountDefault 直接按列统计，不走 GetDefault —— 否则"两条默认"会被
// GetDefault 的 First 语义藏成一条，用例就成了自证。
func batchMCountDefault(t *testing.T, gdb *gorm.DB) (int64, []string) {
	t.Helper()
	var n int64
	if err := gdb.Model(&model.ObsConfig{}).Where("is_default = ?", true).Count(&n).Error; err != nil {
		t.Fatalf("统计默认行失败: %v", err)
	}
	var names []string
	if err := gdb.Model(&model.ObsConfig{}).Where("is_default = ?", true).
		Order("name").Pluck("name", &names).Error; err != nil {
		t.Fatalf("读取默认行名称失败: %v", err)
	}
	return n, names
}

func batchMCreate(t *testing.T, repo ObsConfigRepository, name string, provider model.ObsProvider, status model.ObsStatus, isDefault bool) *model.ObsConfig {
	t.Helper()
	c := &model.ObsConfig{
		Name:      name,
		Provider:  provider,
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "b",
		Status:    status,
		MaxSize:   104857600,
		MaxCount:  1000,
		IsDefault: isDefault,
	}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatalf("创建 %s 失败: %v", name, err)
	}
	return c
}

// batchMCreateWithID 与 batchMCreate 同，只是主键由用例写死（GORM 只在主键为空时生成 uuid）。
// R5 需要"id 字典序"与"创建序"两把尺子指向不同行，才能把 Order 那一句钉实
// —— GORM 的 First 在没有显式 Order 时按主键升序，删掉 Order 就会选中另一台。
func batchMCreateWithID(t *testing.T, repo ObsConfigRepository, id, name string, provider model.ObsProvider, status model.ObsStatus, isDefault bool) *model.ObsConfig {
	t.Helper()
	c := &model.ObsConfig{
		ID:        id,
		Name:      name,
		Provider:  provider,
		AccessKey: "ak",
		SecretKey: "sk",
		Bucket:    "b",
		Status:    status,
		MaxSize:   104857600,
		MaxCount:  1000,
		IsDefault: isDefault,
	}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatalf("创建 %s(id=%s) 失败: %v", name, id, err)
	}
	if c.ID != id {
		t.Fatalf("主键没被用：落库为 %s，夹具要求 %s（GORM 生成规则变了 ⇒ R5 的取序判据失去着力点）", c.ID, id)
	}
	return c
}

// R1: 选取必须带 status，且**不许降级到别的行**。
//
// 两个方向都要钉，因为漏掉任何一个都是"上传静默落到另一台存储"：
//   - 默认行被停用 ⇒ 必须报 record not found。旧实现把停用行直接交出去，
//     调用方拿到一台已经被判坏的存储继续投文件；
//   - 此时库里另有 active 的**非默认**行 ⇒ 也不能自动改用它。
//     "默认"是运维显式选的那台，悄悄换一台会把文件写进另一个桶/目录，
//     而两边的公开 URL 都成立 ⇒ 只有取回原文件时才露馅。
//
// 为什么这一条不铺"两条 is_default=true"：那是生产库里不该存在的状态 ——
// 建库后由偏唯一索引 idx_obs_config_single_default 挡死（索引本身归 db 包那四条
// TestBatchM_ObsDefaultIndex_* 腿管；测试库只 AutoMigrate、不跑 post-migrate 钩子，
// 在这儿铺双默认测的是生产到不了的形状）。
// 唯一例外是 R5：它铺双默认不是为了测"读侧兜住脏状态"，而是为了把取序口径
// 钉成与钩子清重语句同一把尺子。
func TestBatchM_GetDefaultRequiresActiveStatus(t *testing.T) {
	ctx := context.Background()
	repo, _ := batchMNewRepo(t)

	disabled := batchMCreate(t, repo, "batchm-default-inactive", model.ObsProviderLocal, model.ObsStatusInactive, true)
	other := batchMCreate(t, repo, "batchm-active-not-default", model.ObsProviderQiniu, model.ObsStatusActive, false)

	got, err := repo.GetDefault(ctx)
	// (nil, nil) 单独一支：产码是 `return &config, err` 到不了这里，但变异一旦造出它，
	// `got.Name` 会当场 nil 解引用带走整个测试二进制，其余腿一条都不交卷（判据会把"崩"
	// 记成"杀"）。
	switch {
	case err == nil && got == nil:
		t.Errorf("默认行已停用，GetDefault 却给了 (nil, nil) —— 期望 record not found")
	case err == nil:
		t.Errorf("默认行已停用，却返回了 %s(status=%s) —— 期望 record not found", got.Name, got.Status)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		t.Errorf("错误类型 = %v，期望 gorm.ErrRecordNotFound", err)
	}

	// 把另一行标成 error 态再取一次：任何非 active 行都不该被选中。
	if err := repo.UpdateStatus(ctx, other.ID, model.ObsStatusError); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	if got, err := repo.GetDefault(ctx); err == nil {
		if got == nil {
			t.Errorf("库里只剩非 active 行，GetDefault 却给了 (nil, nil)")
		} else {
			t.Errorf("库里只剩非 active 行，仍返回 %s(status=%s)", got.Name, got.Status)
		}
	}

	// 把默认行恢复 active：它必须被选中（本条同时防"过滤写反"把 active 当 inactive 筛掉）。
	if err := repo.UpdateStatus(ctx, disabled.ID, model.ObsStatusActive); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	got2, err := repo.GetDefault(ctx)
	if err != nil {
		t.Fatalf("恢复 active 后 GetDefault 报错: %v", err)
	}
	if got2 == nil {
		t.Fatalf("GetDefault 既没报错也没给行（下面 `got2.ID` 解引用会带走整包测试二进制）")
	}
	if got2.ID != disabled.ID {
		t.Errorf("选中了 %s，期望那台默认的 %s", got2.Name, disabled.Name)
	}
}

// R2: 切换默认必须原子 —— 目标 id 不存在时**一条都不改**，且要报错。
//
// 旧实现是 ClearDefault 先提交、再 UPDATE 目标行：目标行被并发删掉时第二句影响 0 行
// 却返回 nil，库里留下"零条默认"，此后所有上传/媒体转存都从"未找到默认存储配置"起步，
// 且没有任何自愈路径（默认行没了，后台也就没有"取消默认"可点）。
func TestBatchM_SetDefaultRejectsMissingIDAndKeepsPrevious(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	before := batchMCreate(t, repo, "batchm-prev", model.ObsProviderLocal, model.ObsStatusActive, true)

	if err := repo.SetDefault(ctx, "batchm-no-such-id"); err == nil {
		t.Error("对不存在的 id 设默认返回了 nil（调用方以为成功）")
	}
	after, err := repo.GetDefault(ctx)
	if err != nil {
		t.Fatalf("切换失败后默认行应仍在，实际 GetDefault 报错: %v", err)
	}
	if after == nil {
		t.Fatalf("GetDefault 既没报错也没给行")
	}
	if after.ID != before.ID {
		t.Errorf("默认行变成 %s，期望仍是 %s", after.Name, before.Name)
	}
	if n, names := batchMCountDefault(t, gdb); n != 1 {
		t.Errorf("默认行条数 = %d(%v)，期望 1", n, names)
	}
}

// R2b: 目标行存在时，切换后全表**恰好一条**默认，且就是目标行。
func TestBatchM_SetDefaultLeavesExactlyOneDefault(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	batchMCreate(t, repo, "batchm-a", model.ObsProviderLocal, model.ObsStatusActive, true)
	target := batchMCreate(t, repo, "batchm-b", model.ObsProviderQiniu, model.ObsStatusActive, false)

	if err := repo.SetDefault(ctx, target.ID); err != nil {
		t.Fatalf("SetDefault 失败: %v", err)
	}
	n, names := batchMCountDefault(t, gdb)
	if n != 1 {
		t.Fatalf("默认行条数 = %d(%v)，期望恰好 1 条", n, names)
	}
	got, err := repo.GetDefault(ctx)
	if err != nil {
		t.Fatalf("GetDefault 失败: %v", err)
	}
	// 产码是 `return &config, err` ⇒ (nil, nil) 到不了这里；但变异一旦造出它，
	// `got.ID` 会当场 nil 解引用带走整个测试二进制，其余腿一条都不交卷。
	if got == nil {
		t.Fatalf("GetDefault 既没报错也没给行")
	}
	if got.ID != target.ID {
		t.Fatalf("默认行 = %s(%+v)，期望 %s", got.Name, got, target.Name)
	}
}

// R3: 计数写回不得触碰选取列。
//
// 旧实现是 UploadFile 用整行 Save 写 file_count/total_size —— 它写回的是
// **上传开始时读到的那份快照**：期间管理员把默认切到别的配置，这次 Save 会把旧的
// is_default=true 原样写回，于是库里出现两条默认行，之后每次选取都由 First 的
// 隐式主键序（uuid 随机）决定，媒体落到哪台存储变成抛硬币。
// 新入口 IncrementUsage 只写两列，本条把它钉住：跑完之后默认行仍只有 B，
// 而 A 的计数确实涨了。
func TestBatchM_IncrementUsageDoesNotTouchSelectionColumns(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	a := batchMCreate(t, repo, "batchm-r3-a", model.ObsProviderLocal, model.ObsStatusActive, true)
	b := batchMCreate(t, repo, "batchm-r3-b", model.ObsProviderQiniu, model.ObsStatusActive, false)

	snapshot, err := repo.GetDefault(ctx) // 上传路径拿到的那一行（此刻是 A）
	if err != nil {
		t.Fatalf("GetDefault 失败: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("GetDefault 既没报错也没给行")
	}
	if snapshot.ID != a.ID {
		t.Fatalf("前置不成立：默认行是 %s，期望 %s", snapshot.Name, a.Name)
	}
	if err := repo.SetDefault(ctx, b.ID); err != nil {
		t.Fatalf("SetDefault 失败: %v", err)
	}

	if err := repo.IncrementUsage(ctx, snapshot.ID, 4096); err != nil {
		t.Fatalf("IncrementUsage 失败: %v", err)
	}
	if err := repo.IncrementUsage(ctx, snapshot.ID, 1024); err != nil {
		t.Fatalf("IncrementUsage 失败: %v", err)
	}

	back, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("读回 A 失败: %v", err)
	}
	if back == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `back.` 的解引用会带走整包测试二进制）")
	}
	if back.IsDefault {
		t.Error("计数写回把陈旧快照的 is_default=true 又落回了 A（会出现两条默认行）")
	}
	if back.FileCount != 2 {
		t.Errorf("file_count = %d，期望 2（两次累加）", back.FileCount)
	}
	if back.TotalSize != 5120 {
		t.Errorf("total_size = %d，期望 5120", back.TotalSize)
	}
	if back.Status != model.ObsStatusActive {
		t.Errorf("status 被改成 %s，计数写回不该动它", back.Status)
	}
	if n, names := batchMCountDefault(t, gdb); n != 1 || len(names) != 1 || names[0] != "batchm-r3-b" {
		t.Errorf("默认行 = %d 条 %v，期望 1 条 [batchm-r3-b]", n, names)
	}
}

// R3 的另一半：计数写回也不得顶掉 updated_at。
//
// 为什么单独断这一列：IncrementUsage 用 UpdateColumns 而不是 Updates，差别就在这一列 ——
// 后者会让 GORM 自动刷 updated_at，管理页上的"最后修改时间"于是变成"最后一次有人传了
// 个文件"。真要把配置改坏时看时间戳定位是谁改的，这条时间线就被上传淹掉了。
// 这一断同时是 Updates↔UpdateColumns 那格变异的杀手。
func TestBatchM_IncrementUsageDoesNotTouchUpdatedAt(t *testing.T) {
	ctx := context.Background()
	repo, _ := batchMNewRepo(t)

	row := batchMCreate(t, repo, "batchm-r3c", model.ObsProviderLocal, model.ObsStatusActive, true)
	before, err := repo.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if before == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `before.` 的解引用会带走整包测试二进制）")
	}
	if before.FileCount != 0 || before.TotalSize != 0 {
		t.Fatalf("前置不成立：新建行的用量 = %d/%d，期望 0/0", before.FileCount, before.TotalSize)
	}

	if err := repo.IncrementUsage(ctx, row.ID, 2048); err != nil {
		t.Fatalf("IncrementUsage 失败: %v", err)
	}

	after, err := repo.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if after == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `after.UpdatedAt` 的解引用会带走整包测试二进制）")
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at 被计数写回顶掉了：%v → %v", before.UpdatedAt, after.UpdatedAt)
	}
}

// R4: 库级守卫在场时，切换默认仍然走得通，且留下的恰好一条是新默认。
//
// 为什么要在 repository 包里重跑一遍那道 DDL：SetDefault 里"先摘别人、后置自己"
// 的顺序**只有**在偏唯一索引存在时才是唯一可行顺序（反序会当场唯一冲突，切不动）。
// repository 的测试库默认不跑 post-migrate 钩子（testutil 只做 AutoMigrate），
// 不补这一格，把两句 UPDATE 换顺序这种"看着更直观"的改法就没人拦。
// DDL 与 db.postMigrateObsDefaultUniqueIndex 同源；两处若漂移，db 包那四条索引腿会先红。
func TestBatchM_SetDefaultSucceedsWithSingleDefaultGuard(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	if err := gdb.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_config_single_default
		ON obs_config (is_default) WHERE is_default`).Error; err != nil {
		t.Fatalf("铺库级守卫失败: %v", err)
	}

	batchMCreate(t, repo, "batchm-r4-old", model.ObsProviderLocal, model.ObsStatusActive, true)
	target := batchMCreate(t, repo, "batchm-r4-new", model.ObsProviderQiniu, model.ObsStatusActive, false)

	if err := repo.SetDefault(ctx, target.ID); err != nil {
		t.Fatalf("带守卫切换默认失败（先摘后置的顺序不成立？）: %v", err)
	}
	if n, names := batchMCountDefault(t, gdb); n != 1 || names[0] != "batchm-r4-new" {
		t.Errorf("切换后默认行 = %d 条 %v，期望 1 条 [batchm-r4-new]", n, names)
	}
}

// R5: 库里已存在双默认（存量迁移期）时，选取必须由 created_at 升序定死为最早那条。
//
// 这是本文件唯一**故意**铺双默认的腿，理由与 R1 的取舍不冲突：这里断的不是
// "双默认是个能长期存在的合法状态"（生产里那道索引不让），而是**取序口径**——
// 钩子的清重语句保留的就是"GetDefault 本来会选中的那一条"（见 migrate.go 里
// 那句 ORDER BY created_at ASC, id ASC），存量文件实际落在那台上。两处取序一旦
// 各说各话，清重就会把默认从"文件在的那台"切到"文件不在的那台"，比双默认更糟。
// 删掉或反着写 Order 都会被这一格逮住。
func TestBatchM_GetDefaultPicksOldestWhenDuplicated(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	base := time.Now().Add(-time.Hour)
	// 故意让"主键序"与"创建序"相反：newer 先创建但拿到一个字典序更小的固定 id。
	// 这样 GORM First 的默认行为（无显式 Order 时按主键升序）必然选中错的那台，
	// "把 Order 删掉"这种改法才会红，而不是靠物理存储顺序侥幸绿。
	newer := batchMCreateWithID(t, repo, "aaa-batchm-r5-newer", "batchm-r5-newer", model.ObsProviderQiniu, model.ObsStatusActive, true)
	older := batchMCreateWithID(t, repo, "zzz-batchm-r5-older", "batchm-r5-older", model.ObsProviderLocal, model.ObsStatusActive, true)
	if n, _ := batchMCountDefault(t, gdb); n != 2 {
		t.Fatalf("前置不成立：默认行有 %d 条，期望 2", n)
	}
	// 显式拉开 created_at，不让"谁先创建"取决于本机时钟精度。
	if err := gdb.Model(&model.ObsConfig{}).Where("id = ?", older.ID).
		UpdateColumn("created_at", base).Error; err != nil {
		t.Fatalf("设置 created_at 失败: %v", err)
	}
	if err := gdb.Model(&model.ObsConfig{}).Where("id = ?", newer.ID).
		UpdateColumn("created_at", base.Add(10*time.Minute)).Error; err != nil {
		t.Fatalf("设置 created_at 失败: %v", err)
	}

	got, err := repo.GetDefault(ctx)
	if err != nil {
		t.Fatalf("GetDefault 失败: %v", err)
	}
	if got == nil {
		t.Fatalf("GetDefault 既没报错也没给行")
	}
	if got.ID != older.ID {
		t.Errorf("双默认时选中了 %s(id=%s)，期望最早创建的 %s(id=%s)", got.Name, got.ID, older.Name, older.ID)
	}
}

// R3b: 计数写回不得凭空造出行 —— 目标 id 不存在时返回错误而不是"成功"。
//
// 为什么单独一条：UpdateColumns/Update 对 0 行影响都返回 nil，"没报错"不等于"写进去了"。
// 上传路径把它当非阻塞 Warn，所以这里只要求它**可判定**（RowsAffected==0 ⇒ error），
// 否则计数与真实文件数会长期对不上而无人知。
func TestBatchM_IncrementUsageMissingIDIsError(t *testing.T) {
	ctx := context.Background()
	repo, _ := batchMNewRepo(t)

	batchMCreate(t, repo, "batchm-r3b", model.ObsProviderLocal, model.ObsStatusActive, true)

	if err := repo.IncrementUsage(ctx, "batchm-no-such-id", 10); err == nil {
		t.Error("对不存在的 id 自增计数返回了 nil")
	}
}

// R6: 管理页编辑一台配置，不许把快照里的**选取列、状态列与用量列**写回。
//
// 这是 N-26③ 的另一半。③ 说的是"整行 Save 把陈旧快照写回"：本批把**上传**那条
// 路径换成了 IncrementUsage，但**编辑**这条路径（`Update` 的唯一生产调用方 =
// service.UpdateConfig，全仓再无第二处）用的仍是 `db.Save(整个 struct)`，
// 而快照里带着 `is_default / status / file_count / total_size` —— 这三组列各有各的
// 属主（分别是 SetDefault＋库级索引、UpdateStatus、IncrementUsage），编辑口一张
// 都不该碰。同一份陈旧快照因此有三个危害方向：
//   - 读快照与写回之间发生默认切换 ⇒ 刚被摘掉的那台复活成第二条默认；
//   - 读快照与写回之间发生上传计数 ⇒ 计数器被快照里的旧值覆盖（少算，且无人知晓）；
//   - 读快照与写回之间探测把这台判成 error ⇒ 编辑把故障标记抹回 active，
//     而运维以为它健康（这一格今天在生产里做不出来：`UpdateStatus` 零调用方，
//     与 §23.1 对 ① 的打折同一条，判据先钉住、接线那天不用再补测试）。
//
// 修法用**白名单**而不是 Omit 黑名单：黑名单会随模型以后加列而静默失守，
// 白名单让"新列默认不可经编辑口写入"。与 IncrementUsage 只点两列同一个理由。
func TestBatchM_UpdateDoesNotResurrectSelectionColumns(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	// 本腿刻意**不建**索引：让"复活成第二条默认"以数据形状暴露出来，而不是先撞
	// 唯一冲突。索引在场时的那一份形状由 R6b 管。
	old := batchMCreate(t, repo, "batchm-r6-old", model.ObsProviderLocal, model.ObsStatusActive, true)
	other := batchMCreate(t, repo, "batchm-r6-other", model.ObsProviderQiniu, model.ObsStatusActive, false)

	snapshot, err := repo.GetByID(ctx, old.ID)
	if err != nil {
		t.Fatalf("读快照失败: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `snapshot.` 的解引用会带走整包测试二进制）")
	}
	if !snapshot.IsDefault {
		t.Fatalf("前置不成立：快照 is_default = false，期望 true（否则这一腿根本测不到复活）")
	}

	// 快照之后发生三件事：默认切走、用量涨上来、这一台被探测判成故障。
	if err := repo.SetDefault(ctx, other.ID); err != nil {
		t.Fatalf("切换默认失败: %v", err)
	}
	if err := repo.IncrementUsage(ctx, old.ID, 4096); err != nil {
		t.Fatalf("IncrementUsage 失败: %v", err)
	}
	if err := repo.UpdateStatus(ctx, old.ID, model.ObsStatusError); err != nil {
		t.Fatalf("UpdateStatus 失败: %v", err)
	}
	if n, _ := batchMCountDefault(t, gdb); n != 1 {
		t.Fatalf("前置不成立：切换后默认行有 %d 条，期望 1", n)
	}

	// 管理员只是改个名字，写回的却是整份快照。
	snapshot.Name = "batchm-r6-renamed"
	if err := repo.Update(ctx, snapshot); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}

	if n, names := batchMCountDefault(t, gdb); n != 1 || names[0] != "batchm-r6-other" {
		t.Errorf("编辑之后默认行 = %d 条 %v，期望 1 条 [batchm-r6-other]（快照里的 is_default=true 被写回了）", n, names)
	}
	edited, err := repo.GetByID(ctx, old.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if edited == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `edited.` 的解引用会带走整包测试二进制）")
	}
	if edited.Name != "batchm-r6-renamed" {
		t.Errorf("编辑没落地：name = %q，期望 batchm-r6-renamed", edited.Name)
	}
	if edited.FileCount != 1 || edited.TotalSize != 4096 {
		t.Errorf("用量被快照覆盖：file_count/total_size = %d/%d，期望 1/4096", edited.FileCount, edited.TotalSize)
	}
	if edited.Status != model.ObsStatusError {
		t.Errorf("status 被快照复活成 %s，期望 error（status 归探测/UpdateStatus 所有，编辑口不许写回）", edited.Status)
	}
	// 白名单不许把 autoUpdateTime 一起筛掉：管理员改了配置，时间线就该动。
	if !edited.UpdatedAt.After(snapshot.UpdatedAt) {
		t.Errorf("编辑没刷 updated_at：%v → %v", snapshot.UpdatedAt, edited.UpdatedAt)
	}
}

// R6b: 同一份陈旧快照在库级守卫**在场**时必须仍然编辑得动。
//
// 为什么单独一条：修 ③ 只要方向不对，就会把"复活成双默认"改成"撞唯一冲突报 500"——
// 数据是对的，但管理员在页面上改个名字会收到一句 duplicate key，且改名根本不生效。
// 这一腿把"编辑口不再碰 is_default"钉成**用户可感知**的形状：有索引、期间发生过默认
// 切换，Update 返回 nil、改名生效、默认仍只有一条且是新那台。
func TestBatchM_UpdateSucceedsWithSingleDefaultGuard(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	// 与 R4 同源：DDL 抄 postMigrateObsDefaultUniqueIndex，两处漂移由 db 包那五条腿先红。
	if err := gdb.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_config_single_default
		ON obs_config (is_default) WHERE is_default`).Error; err != nil {
		t.Fatalf("铺库级守卫失败: %v", err)
	}

	old := batchMCreate(t, repo, "batchm-r6b-old", model.ObsProviderLocal, model.ObsStatusActive, true)
	other := batchMCreate(t, repo, "batchm-r6b-other", model.ObsProviderQiniu, model.ObsStatusActive, false)

	snapshot, err := repo.GetByID(ctx, old.ID)
	if err != nil {
		t.Fatalf("读快照失败: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `snapshot.` 的解引用会带走整包测试二进制）")
	}
	if !snapshot.IsDefault {
		t.Fatalf("前置不成立：快照 is_default = false，期望 true")
	}
	if err := repo.SetDefault(ctx, other.ID); err != nil {
		t.Fatalf("切换默认失败: %v", err)
	}

	snapshot.Name = "batchm-r6b-renamed"
	if err := repo.Update(ctx, snapshot); err != nil {
		t.Fatalf("带守卫时编辑一台非默认行失败（编辑口仍在写 is_default？）: %v", err)
	}

	edited, err := repo.GetByID(ctx, old.ID)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if edited == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `edited.Name` 的解引用会带走整包测试二进制）")
	}
	if edited.Name != "batchm-r6b-renamed" {
		t.Errorf("编辑没落地：name = %q", edited.Name)
	}
	if n, names := batchMCountDefault(t, gdb); n != 1 || names[0] != "batchm-r6b-other" {
		t.Errorf("默认行 = %d 条 %v，期望 1 条 [batchm-r6b-other]", n, names)
	}
}

// R6c: 编辑口对"行已经不在了"必须报错，而不是回一句成功。
//
// 与 R2 同一条纪律，只是落在另一条语句上：服务层 `UpdateConfig` 先 `GetByID` 再
// `Update`，两句之间那台存储被删掉时，整行写回影响 0 行 —— 而 `Save`/`Updates`
// 对 0 行都返回 nil，界面就显示"保存成功"，管理员以为改过的配置其实一条没落。
// 顺带把"编辑口是纯更新"这件事说明白：旧实现用 `Save`，主键为零值时它会**插入**，
// 那是编辑口绝不该有的第二身份（建配置走 `Create`）。
func TestBatchM_UpdateMissingIDIsError(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	row := batchMCreate(t, repo, "batchm-r6c", model.ObsProviderLocal, model.ObsStatusActive, false)
	snapshot, err := repo.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("读快照失败: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("GetByID 既没报错也没给行（下面 `snapshot.Name` 的解引用会带走整包测试二进制）")
	}
	if err := repo.DeleteNonDefault(ctx, row.ID); err != nil {
		t.Fatalf("删行失败: %v", err)
	}

	snapshot.Name = "batchm-r6c-ghost"
	if err := repo.Update(ctx, snapshot); err == nil {
		t.Error("对已删除的 id 执行 Update 返回了 nil（界面会显示保存成功）")
	}

	// 主键为空的 struct 不许经编辑口落库。
	if err := repo.Update(ctx, &model.ObsConfig{Name: "batchm-r6c-no-id"}); err == nil {
		t.Error("无主键的 struct 经编辑口返回了 nil")
	}
	var n int64
	if err := gdb.Model(&model.ObsConfig{}).Where("name = ?", "batchm-r6c-no-id").Count(&n).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if n != 0 {
		t.Errorf("编辑口把无主键的 struct 插进了库（count=%d，期望 0）", n)
	}
}

// R7: 删除口删不动唯一默认行 —— 判据在那条 DELETE 的 WHERE 里，不在调用方的预读里。
//
// 服务层是先 GetByID 看 is_default、再删。两句之间管理页上另一次"设为默认"提交，
// 被删的那一行就已经是全站唯一默认了 —— 两次点击界面都回"成功"，库里留下零条默认：
// 两条上传口一起断，入站媒体静默改落本地盘（§5 N-32）。而
// `InitDefaultStorageIfEmpty` 只在**表为空**时兜底 seed，表里还有别的行就不会自愈。
// 所以这一格与 SetDefault 同口径：把判据写进写语句本身，预读只负责把错误话说清楚。
func TestBatchM_DeleteNonDefaultRejectsTheOnlyDefaultRow(t *testing.T) {
	ctx := context.Background()
	repo, gdb := batchMNewRepo(t)

	def := batchMCreate(t, repo, "batchm-r7-default", model.ObsProviderLocal, model.ObsStatusActive, true)
	other := batchMCreate(t, repo, "batchm-r7-other", model.ObsProviderQiniu, model.ObsStatusActive, false)

	if err := repo.DeleteNonDefault(ctx, def.ID); err == nil {
		t.Error("删除唯一默认行返回了 nil（并发越过服务层预读后，库里会留下零条默认）")
	}
	if n, names := batchMCountDefault(t, gdb); n != 1 || names[0] != "batchm-r7-default" {
		t.Errorf("试着删除默认行之后，库里默认行 = %d 条 %v，期望 1 条 [batchm-r7-default]", n, names)
	}

	// 对照腿：非默认行照常删得掉，而且是真删（不是"报成功但行还在"）。
	if err := repo.DeleteNonDefault(ctx, other.ID); err != nil {
		t.Fatalf("删除非默认行失败: %v", err)
	}
	if _, err := repo.GetByID(ctx, other.ID); err == nil {
		t.Error("非默认行没被删掉")
	}

	// 不存在的 id 必须报错：删除语句影响 0 行时 GORM 返回 nil，
	// 界面会把"其实什么都没删"显示成"已删除"。（与 R6c 同一条纪律，落在另一条语句上。）
	if err := repo.DeleteNonDefault(ctx, "batchm-r7-no-such-id"); err == nil {
		t.Error("删除不存在的 id 返回了 nil")
	}
}
