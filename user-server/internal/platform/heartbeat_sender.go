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
	// 走 GetStatus 而不是裸 Load：Load 在文件坏着时返回错误，旧写法直接 return，
	// 于是这台实例从此一帧心跳都不发且不留日志；GetStatus 会先把坏文件自愈再拿身份。
	st := install.GetStatus()
	if st.InstallID == "" {
		return
	}
	if st.Reminted {
		// 磁盘上没有可用身份、库里却装着超管：这台机器的 install.lock 丢了，
		// 刚才那次回填铸的是全新 install_id。平台侧纯按 install_id 记商户，
		// 于是同一台机器会多出一个新装商户、旧历史再也接不上——必须说出来。
		// 最常见成因是换目录启动：默认路径 ./install.lock 相对的是进程 CWD，
		// 生产应显式设 INSTALL_LOCK_PATH 为绝对路径（见 .env-example）。
		logger.Warnf("install.lock 缺失，本次回填铸了新安装身份 install_id=%s；"+
			"平台侧会把它记成一个新装商户。若这是同一台机器在换启动目录后重启，"+
			"请把原 install.lock 找回并把 INSTALL_LOCK_PATH 指向它", st.InstallID)
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

	version := st.Version
	if version == "" {
		version = "unknown"
	}

	req := &ReportHeartbeatReq{
		InstallID:         st.InstallID,
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
