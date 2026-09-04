//go:build windows

package service

import (
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// sampleCPUOnce Windows 实现：GetProcessTimes 采样进程用户态+内核态 CPU 时间，
// 语义与 Unix getrusage 版本对齐（见 system_monitor_unix.go）。
func sampleCPUOnce() float64 {
	h := windows.CurrentProcess()
	var creation, exit, kernel1, user1, kernel2, user2 windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel1, &user1); err != nil {
		return 0
	}
	t1 := time.Now()
	time.Sleep(100 * time.Millisecond)
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel2, &user2); err != nil {
		return 0
	}
	t2 := time.Now()

	cpu1 := time.Duration(kernel1.Nanoseconds() + user1.Nanoseconds())
	cpu2 := time.Duration(kernel2.Nanoseconds() + user2.Nanoseconds())
	return cpuUsagePercent(cpu1, cpu2, t1, t2)
}

// sampleDiskOnce Windows 实现：GetDiskFreeSpaceEx 统计工作目录所在卷的使用率。
func sampleDiskOnce() float64 {
	cwd, err := os.Getwd()
	if err != nil {
		return 0
	}
	root := filepath.VolumeName(cwd) + `\`
	var free, total, avail uint64
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(root), &avail, &total, &free); err != nil {
		return 0
	}
	if total == 0 {
		return 0
	}
	used := total - free
	usage := float64(used) / float64(total) * 100
	return clampPercent(usage)
}
