// db_race_test.go —— pkg/db 那把锁自己的回归腿：并发跑 GetDB／SetTestDB 不得报数据竞争。
//
// 为什么钉在 accessor 上、而不是多补几条站点探针：async_db_handle_probe_test.go 那三条钉的是
// "某个 fire-and-forget 的异步体别裸读全局句柄"，而竞争已改在 accessor 里收口——把 dbMu 删掉时
// 那三条照样绿（异步体已经不读全局了），只有这一条会红。它补的是收口之后新露出来的那一面。
//
// 不连 PG、不碰业务表：写进去的是个空壳句柄，被测的只是同一个指针地址上的并发存取，所以判据
// 是"有没有竞争"。那句值比对只在**写方只写这一个值**、且 spawn 之前先同步落一次的前提下才成立：
// 第一版把它写成"读到的必须等于刚写进去的"，而"刚写进去"由四条协程去兑现 ⇒ 第 0 次读就可能赶在
// 它们之前（实测 got=0x0 / 上一轮的指针），那是断言自己造的时序，不是竞争。
package db

import (
	"sync"
	"testing"

	"gorm.io/gorm"
)

func TestConcurrentGetDBAndSetTestDBAreRaceFree(t *testing.T) {
	saved := GetDB()
	t.Cleanup(func() { SetTestDB(saved) })

	const writers = 4
	const reads = 20_000

	probe := &gorm.DB{}
	// 先由本协程把句柄写进去再放写方：写方全程只写这同一个指针，于是"读到的必须等于它"是一句
	// 有时序保证的断言。（少了这一句首发就变成"第一次读要能看到还没被任何协程写过的值"——
	// 实测 -count=5 第一轮就 Fatalf `got=0x0`，那是断言自己错了，不是锁错了。）
	SetTestDB(probe)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					SetTestDB(probe)
				}
			}
		}()
	}
	for i := 0; i < reads; i++ {
		if got := GetDB(); got != probe {
			t.Fatalf("第 %d 次读到的不是刚写进去的那个句柄：got=%p want=%p", i, got, probe)
		}
	}
	close(stop)
	wg.Wait()
}
