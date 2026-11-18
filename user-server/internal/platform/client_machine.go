package platform

import (
	"sync"
	"time"
)

// ClientDeviceSnapshot 商户正在使用的电脑（浏览器端）设备快照。
//
// 背景与口径（任务三）：心跳原本只携带 user-server 所在机器的信息——
// 本地部署形态下那台机器就是商户的部署机，口径正确；但 SaaS 形态下
// user-server 跑在云端服务器，商户操作用的是自己的浏览器电脑，
// 平台侧看到的"设备"就不是商户在用的那台。
//
// 因此 SaaS 形态下由 user-web 在登录态发一帧轻量设备信标
// （POST /api/client-device/report），user-server 侧从请求中采集
// 出口 IP + User-Agent，连同浏览器自报的屏幕/时区/语言合并成本快照；
// 心跳上报时作为 client_machine 字段随帧带给平台。
//
// 只保留"最近一帧"（单个部署实例同一时刻只关心商户当前在用的电脑），
// 带时间戳与新鲜度 TTL：商户电脑信息过旧（超过 clientDeviceTTL）时
// 心跳不再附带，避免把几天前某台访客电脑当成"正在使用"。
type ClientDeviceSnapshot struct {
	IP       string `json:"ip"`
	UA       string `json:"ua"`
	Platform string `json:"platform,omitempty"`
	Screen   string `json:"screen,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Language string `json:"language,omitempty"`

	CapturedAt time.Time `json:"captured_at"`
}

// clientDeviceTTL 设备快照的新鲜度：超过此时长的快照视为过期，
// 心跳不再附带（浏览器信标随每次页面加载刷新，正常使用下远小于该值）。
const clientDeviceTTL = 24 * time.Hour

var (
	clientDeviceMu     sync.RWMutex
	clientDeviceLatest *ClientDeviceSnapshot
)

// SetClientDeviceSnapshot 记录最近一帧商户电脑设备快照（信标入口调用）。
func SetClientDeviceSnapshot(snap *ClientDeviceSnapshot) {
	if snap == nil {
		return
	}
	if snap.CapturedAt.IsZero() {
		snap.CapturedAt = time.Now()
	}
	clientDeviceMu.Lock()
	defer clientDeviceMu.Unlock()
	clientDeviceLatest = snap
}

// FreshClientDeviceSnapshot 返回未过期的最近设备快照；无记录或已过期返回 nil。
func FreshClientDeviceSnapshot() *ClientDeviceSnapshot {
	clientDeviceMu.RLock()
	defer clientDeviceMu.RUnlock()
	if clientDeviceLatest == nil {
		return nil
	}
	if time.Since(clientDeviceLatest.CapturedAt) > clientDeviceTTL {
		return nil
	}
	// 返回副本，避免调用方持有读锁期间修改内部状态
	cp := *clientDeviceLatest
	return &cp
}

// ResetClientDeviceSnapshotForTest 清空快照缓存（仅测试用）
func ResetClientDeviceSnapshotForTest() {
	clientDeviceMu.Lock()
	defer clientDeviceMu.Unlock()
	clientDeviceLatest = nil
}
