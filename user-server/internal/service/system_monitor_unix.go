//go:build unix

package service

import (
	"os"
	"syscall"
	"time"
)

// sampleCPUOnce Unix 实现：getrusage 采样进程用户态+内核态 CPU 时间。
func sampleCPUOnce() float64 {
	var r1, r2 syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r1); err != nil {
		return 0
	}
	t1 := time.Now()
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &r2); err != nil {
		return 0
	}
	t2 := time.Now()

	cpu1 := time.Duration(r1.Utime.Nano()) + time.Duration(r1.Stime.Nano())
	cpu2 := time.Duration(r2.Utime.Nano()) + time.Duration(r2.Stime.Nano())
	return cpuUsagePercent(cpu1, cpu2, t1, t2)
}

// sampleDiskOnce Unix 实现：statfs 统计工作目录所在文件系统使用率。
func sampleDiskOnce() float64 {
	cwd, err := os.Getwd()
	if err != nil {
		return 0
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(cwd, &stat); err != nil {
		return 0
	}
	total := uint64(stat.Blocks) * uint64(stat.Bsize)
	if total == 0 {
		return 0
	}
	free := uint64(stat.Bfree) * uint64(stat.Bsize)
	used := total - free
	usage := float64(used) / float64(total) * 100
	return clampPercent(usage)
}
