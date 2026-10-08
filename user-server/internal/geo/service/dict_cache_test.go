package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"

	"gorm.io/gorm"
)

// 字典缓存必须是「一个进程一份」，而不是「一个 DictService 实例一份」。
//
// 起因是长尾接口的实测：管理端走 router 注入的 DictService 实例写库，业务读路径走
// globalDict() 单例。两者各持一份 sync.Map 时，Set 只失效自己那份，单例里的值
// 永不过期 ⇒ 管理员改完字典，线上行为要到进程重启那天才变。字典行本身已经写对了，
// 坏的是失效半径。
//
// 本文件不依赖数据库：用同一份内存 repo 造出两个实例，模拟上面那两条真实链路。

type stubDictRepo struct {
	rows map[string]*model.GeoDict // key: category + "\x00" + key
}

func newStubDictRepo() *stubDictRepo {
	return &stubDictRepo{rows: map[string]*model.GeoDict{}}
}

func (r *stubDictRepo) ck(category, key string) string { return category + "\x00" + key }

func (r *stubDictRepo) Get(category, key string) (*model.GeoDict, error) {
	return r.GetAny(category, key)
}

func (r *stubDictRepo) GetAny(category, key string) (*model.GeoDict, error) {
	if d, ok := r.rows[r.ck(category, key)]; ok {
		return d, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubDictRepo) ListByCategory(category string) ([]*model.GeoDict, error) {
	var list []*model.GeoDict
	for _, d := range r.rows {
		if d.Category == category {
			list = append(list, d)
		}
	}
	return list, nil
}

func (r *stubDictRepo) Upsert(d *model.GeoDict) error {
	cp := *d
	r.rows[r.ck(d.Category, d.Key)] = &cp
	return nil
}

func (r *stubDictRepo) Delete(category, key string) error {
	delete(r.rows, r.ck(category, key))
	return nil
}

func (r *stubDictRepo) CountByCategory(category string) (int64, error) {
	var n int64
	for _, d := range r.rows {
		if d.Category == category {
			n++
		}
	}
	return n, nil
}

const dictTestCategory = "dict_cache_test"

func TestDictSetInvalidatesCacheAcrossInstances(t *testing.T) {
	const key = "templates"
	repo := newStubDictRepo()
	// 两个实例＝管理端（router 注入）与业务读路径（globalDict 单例）的同构替身。
	admin := NewDictService(repo)
	reader := NewDictService(newStubDictRepoWrapped(repo))
	// 缓存是包级共享的，测完必须把自己写进去的键摘掉，否则同包后续用例会读到本用例的值。
	t.Cleanup(func() { DictInvalidate(dictTestCategory, key) })

	if err := admin.Set(context.Background(), dictTestCategory, key, "v1", "", true, 0); err != nil {
		t.Fatalf("Set v1 失败：%v", err)
	}
	// 读一次，把 v1 灌进缓存——失效半径的问题只在「已缓存」之后才暴露。
	if got := reader.GetString(context.Background(), dictTestCategory, key, "fallback"); got != "v1" {
		t.Fatalf("首次读应为 v1，实际=%q", got)
	}

	if err := admin.Set(context.Background(), dictTestCategory, key, "v2", "", true, 0); err != nil {
		t.Fatalf("Set v2 失败：%v", err)
	}
	got := reader.GetString(context.Background(), dictTestCategory, key, "fallback")
	if got != "v2" {
		t.Fatalf("写入端 Set 后，读取端仍拿到旧值 %q（缓存按实例隔离 ⇒ 管理员改字典要等重启才生效）", got)
	}

}

// 反向锁：Set 的失效不能只是「碰巧又读了一次库」——缓存命中路径本身也必须已经空了。
// 这里用 CountByCategory 之外最省事的办法：直接断言 GetAny 的调用次数随 Set 增长。
type countingDictRepo struct {
	*stubDictRepo
	getAnyCalls int
}

func newStubDictRepoWrapped(r *stubDictRepo) *countingDictRepo {
	return &countingDictRepo{stubDictRepo: r}
}

func (c *countingDictRepo) GetAny(category, key string) (*model.GeoDict, error) {
	c.getAnyCalls++
	return c.stubDictRepo.GetAny(category, key)
}

func TestDictSetForcesReaderToReloadFromRepo(t *testing.T) {
	const key = "force_reload"
	repo := newStubDictRepo()
	counted := newStubDictRepoWrapped(repo)
	admin := NewDictService(repo)
	reader := NewDictService(counted)
	t.Cleanup(func() { DictInvalidate(dictTestCategory, key) })

	if err := admin.Set(context.Background(), dictTestCategory, key, "v1", "", true, 0); err != nil {
		t.Fatalf("Set 失败：%v", err)
	}
	reader.GetString(context.Background(), dictTestCategory, key, "fallback")
	before := counted.getAnyCalls

	// 先证明「读第二次走缓存」，否则下面的 +1 断量不出失效：前提不成立时本用例就是空跑。
	reader.GetString(context.Background(), dictTestCategory, key, "fallback")
	if counted.getAnyCalls != before {
		t.Fatalf("缓存未生效（连续两次读都打库），本用例的计数前提不成立")
	}

	if err := admin.Set(context.Background(), dictTestCategory, key, "v2", "", true, 0); err != nil {
		t.Fatalf("Set 失败：%v", err)
	}
	// GetAny 只在真的回源时自增，所以计数要在读完这一次之后再取。
	got := reader.GetString(context.Background(), dictTestCategory, key, "fallback")
	if counted.getAnyCalls != before+1 {
		t.Fatalf("Set 后读取端应回源一次：期望打库 %d 次实际 %d 次（失效没打到读取端 ⇒ 它还在用旧缓存）",
			before+1, counted.getAnyCalls)
	}
	if got != "v2" {
		t.Fatalf("回源后应读到 v2，实际=%q", got)
	}
}

// Delete 同样要跨实例失效（停用/删除字典条目是管理员的常规动作）。
func TestDictDeleteInvalidatesCacheAcrossInstances(t *testing.T) {
	const key = "to_delete"
	repo := newStubDictRepo()
	admin := NewDictService(repo)
	reader := NewDictService(newStubDictRepoWrapped(repo))
	t.Cleanup(func() { DictInvalidate(dictTestCategory, key) })

	if err := admin.Set(context.Background(), dictTestCategory, key, "alive", "", true, 0); err != nil {
		t.Fatalf("Set 失败：%v", err)
	}
	if got := reader.GetString(context.Background(), dictTestCategory, key, "fallback"); got != "alive" {
		t.Fatalf("删除前应读到 alive，实际=%q", got)
	}

	if err := admin.Delete(context.Background(), dictTestCategory, key); err != nil {
		t.Fatalf("Delete 失败：%v", err)
	}
	// 行没了：读侧应回落到缺省值（缺行会 best-effort 播种，这里断言不再返回已删的 alive）
	if got := reader.GetString(context.Background(), dictTestCategory, key, "fallback"); got == "alive" {
		t.Fatalf("Delete 后读取端仍返回已删条目 %q（跨实例失效未生效）", got)
	}

}
