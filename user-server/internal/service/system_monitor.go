package service

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

var processStartTime = time.Now()

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	return fmt.Sprintf("%dh %dm %ds", h, m, s)
}

var (
	cpuSnapshot    atomic.Value
	memSnapshot    atomic.Value
	diskSnapshot   atomic.Value
	smInitOnce     sync.Once
	smSamplingDone chan struct{}
)

type resourceSnapshot struct {
	CPU  float64
	Mem  float64
	Disk float64
}

func initResourceSnapshots() {
	cpuSnapshot.Store(float64(0))
	memSnapshot.Store(float64(0))
	diskSnapshot.Store(float64(0))
}

func startResourceSampling() {
	smInitOnce.Do(func() {
		initResourceSnapshots()
		smSamplingDone = make(chan struct{})
		go func() {
			defer close(smSamplingDone)
			sampleResourceUsage()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				sampleResourceUsage()
			}
		}()
	})
}

func sampleResourceUsage() {
	cpuSnapshot.Store(sampleCPUOnce())
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.Sys > 0 {
		memSnapshot.Store(float64(m.Alloc) / float64(m.Sys) * 100)
	} else {
		memSnapshot.Store(float64(0))
	}
	diskSnapshot.Store(sampleDiskOnce())
}

// sampleCPUOnce / sampleDiskOnce 为平台相关实现，按构建标签拆分：
// Unix 见 system_monitor_unix.go（getrusage / statfs），
// Windows 见 system_monitor_windows.go（GetProcessTimes / GetDiskFreeSpaceEx）。

// cpuUsagePercent 由两次 CPU 时间快照与墙钟间隔计算归一化 CPU 使用率（%）。
func cpuUsagePercent(cpu1, cpu2 time.Duration, t1, t2 time.Time) float64 {
	cpuDelta := cpu2 - cpu1
	wallDelta := t2.Sub(t1)
	if wallDelta <= 0 {
		return 0
	}
	numCPU := runtime.NumCPU()
	if numCPU < 1 {
		numCPU = 1
	}
	return clampPercent(float64(cpuDelta) / float64(wallDelta) * 100 / float64(numCPU))
}

// clampPercent 将百分比截断到 [0, 100]。
func clampPercent(usage float64) float64 {
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func readResourceSnapshot() (cpu, mem, disk float64) {
	if v := cpuSnapshot.Load(); v != nil {
		cpu = v.(float64)
	}
	if v := memSnapshot.Load(); v != nil {
		mem = v.(float64)
	}
	if v := diskSnapshot.Load(); v != nil {
		disk = v.(float64)
	}
	return
}

type SystemMonitorService struct {
	statsRepo repository.SystemStatsRepository
}

func NewSystemMonitorService() *SystemMonitorService {
	startResourceSampling()
	return &SystemMonitorService{
		statsRepo: repository.NewSystemStatsRepository(),
	}
}

func NewSystemMonitorServiceWithRepo(repo repository.SystemStatsRepository) *SystemMonitorService {
	startResourceSampling()
	return &SystemMonitorService{statsRepo: repo}
}

func (s *SystemMonitorService) GetSystemStats(ctx context.Context) (map[string]any, error) {
	totalUsers, _ := s.statsRepo.CountSystemUsers(ctx)
	totalOrders, _ := s.statsRepo.CountOrders(ctx)
	totalCards, _ := s.statsRepo.CountCards(ctx)
	totalShortLinks, _ := s.statsRepo.CountShortLinks(ctx)

	todayStart := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.Now().Location())
	todayVisits, _ := s.statsRepo.CountTodayVisits(ctx, todayStart.Unix())

	uptime := formatUptime(time.Since(processStartTime))

	cpuUsage, memUsage, diskUsage := readResourceSnapshot()

	stats := map[string]any{
		"total_users":       totalUsers,
		"total_orders":      totalOrders,
		"total_cards":       totalCards,
		"total_short_links": totalShortLinks,
		"today_visits":      todayVisits,
		"system_uptime":     uptime,
		"cpu_usage":         cpuUsage,
		"memory_usage":      memUsage,
		"disk_usage":        diskUsage,
		"timestamp":         time.Now(),
	}

	return stats, nil
}

func (s *SystemMonitorService) GetDetailedSystemStats(ctx context.Context) (map[string]any, error) {
	todayStart := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, time.Now().Location())
	activeUsers, _ := s.statsRepo.CountActiveSystemUsers(ctx, todayStart.Unix())

	totalMerchants, _ := s.statsRepo.CountSystemUsersByRole(ctx, model.SystemUserRoleAdmin)

	totalEmailLists, _ := s.statsRepo.CountEmailLists(ctx)
	totalEmailJobs, _ := s.statsRepo.CountEmailJobs(ctx)

	totalMaterials, _ := s.statsRepo.CountMaterials(ctx)

	totalUsers, _ := s.statsRepo.CountSystemUsers(ctx)
	totalOrders, _ := s.statsRepo.CountOrders(ctx)
	totalCards, _ := s.statsRepo.CountCards(ctx)
	totalShortLinks, _ := s.statsRepo.CountShortLinks(ctx)

	todayVisits, _ := s.statsRepo.CountTodayVisits(ctx, todayStart.Unix())

	systemMetrics, _ := s.statsRepo.ListRecentSystemMetrics(ctx, 10)

	cpuUsage, memUsage, diskUsage := readResourceSnapshot()

	detailedStats := map[string]any{
		"basic_stats": map[string]any{
			"total_users":        totalUsers,
			"total_orders":       totalOrders,
			"total_cards":        totalCards,
			"total_short_links":  totalShortLinks,
			"today_visits":       todayVisits,
			"active_users_today": activeUsers,
			"total_merchants":    totalMerchants,
		},
		"business_stats": map[string]any{
			"total_email_lists": totalEmailLists,
			"total_email_jobs":  totalEmailJobs,
			"total_materials":   totalMaterials,
		},
		"system_resources": map[string]any{
			"cpu_usage":    cpuUsage,
			"memory_usage": memUsage,
			"disk_usage":   diskUsage,
		},
		"system_metrics": systemMetrics,
		"timestamp":      time.Now(),
	}

	return detailedStats, nil
}
