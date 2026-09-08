package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/system/install"
)

var heartbeatIntervalGetter = func() time.Duration { return 3 * time.Minute }

func heartbeatInterval() time.Duration { return heartbeatIntervalGetter() }

// SetHeartbeatIntervalGetter 装配层注入 DB 驱动的心跳间隔读取器
func SetHeartbeatIntervalGetter(fn func() time.Duration) {
	heartbeatIntervalGetter = fn
}

var deviceFP = computeDeviceFingerprint()

func computeDeviceFingerprint() string {
	host, _ := os.Hostname()
	var mac string
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			if len(iface.HardwareAddr) > 0 {
				mac = iface.HardwareAddr.String()
				break
			}
		}
	}
	seed := strings.TrimSpace(host) + "|" + mac + "|" + runtime.GOOS + "|" + runtime.GOARCH
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// StartHeartbeat 启动心跳上报协程（开源版：每 3 分钟上报一次，best-effort）。
//
// 仅在已初始化（install.lock 含 InstallID）后才会真正发送；未初始化前静默跳过。
// 启动后延迟 30s 先发送一次，便于平台侧尽快拿到安装在线状态。
//
// 心跳会随请求携带：设备指纹（用户端生成）+ 主机信息 + 运行指标；
// 上报方公网 IP 由平台侧从请求中采集，不依赖用户端自报。
func StartHeartbeat(ctx context.Context) {
	utils.SafeGo(ctx, "platform.heartbeat", func(ctx context.Context) {
		time.Sleep(30 * time.Second)
		sendHeartbeat()
		ticker := time.NewTicker(heartbeatInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sendHeartbeat()
			}
		}
	})
}

func sendHeartbeat() {
	lock, err := install.Load()
	if err != nil || lock == nil || lock.InstallID == "" {
		return
	}

	hostInfo, _ := json.Marshal(map[string]any{
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"hostname":      hostName(),
		"go_version":    runtime.Version(),
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
	})
	metrics, _ := json.Marshal(collectMetrics())

	version := lock.Version
	if version == "" {
		version = "unknown"
	}

	req := &ReportHeartbeatReq{
		InstallID:         lock.InstallID,
		Version:           version,
		HostInfo:          hostInfo,
		Metrics:           metrics,
		DeviceFingerprint: deviceFP,
		Timestamp:         time.Now(),
	}
	if err := ReportHeartbeatDefault(req); err != nil {
		logger.Warnf("心跳上报失败（已忽略）: %v", err)
	}
}

func hostName() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func collectMetrics() map[string]any {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"heap_alloc": m.Alloc,
		"heap_sys":   m.HeapSys,
		"num_gc":     m.NumGC,
		"timestamp":  time.Now().Unix(),
	}
}
