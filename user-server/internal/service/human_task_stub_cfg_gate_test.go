// human_task_stub_cfg_gate_test.go —— 测试替身自身的并发门。
//
// 为什么值得单独一条：Submit 的幂等用例会在 8 个 goroutine 里打同一个
// stubHumanTaskCfg，而这个 stub 逐次往 gotGroup/gotKey/calls 上写。CI -race
// 就是这么红的（run 35473414943），但本地按那条用例跑 30 遍都不复现——
// 竞态窗口取决于机器快慢，"CI 偶发红"没法当回归门用。本用例把窗口固定成
// 64 路同时打，摘掉 stub 里的 mu 就必红（反向实测过），红得稳定才好查。
package service

import (
	"context"
	"sync"
	"testing"
)

func TestHumanTaskSvcStubCfgIsRaceFree(t *testing.T) {
	stub := &stubHumanTaskCfg{value: 5}
	const n = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = stub.GetInt(context.Background(), HumanTaskConfigGroup, HumanTaskHandoffSlaKey, 5)
		}()
	}
	close(start)
	wg.Wait()
	// 光"没有 DATA RACE"还不够：无锁自增会丢写，calls 少于 n 就是同一个病。
	// 这里直接读字段是安全的——wg.Wait() 已经把 64 个 goroutine 写在前面。
	if stub.calls != n {
		t.Errorf("calls=%d，期望 %d（丢写＝记账仍不同步）", stub.calls, n)
	}
}
