package channelgw

import (
	"sort"
	"sync"

	"hivemtk-user/internal/model"
)

// ChannelSpec 渠道注册表条目：声明一个渠道的身份与支持传输。
//
// 渠道白名单单源化：历史上 IsBridgeChannel（bridge 包）与 model 渠道常量各自维护，
// 现统一收敛到本注册表；新增渠道仅需 Register 一处，HTTP/WS 传输的校验自动生效。
type ChannelSpec struct {
	Name       string
	Transports []Transport
	Label      string
	// Experimental 能力降级声明（B8）：渠道已注册、链路可跑通，但选择器/字段未经
	// 真机校准，不保证生产级正确率。行为不变（不删不拦），仅显式记录成熟度，
	// 供诊断/展示层如实标注——替代「默认即生产可用」的过度声明。
	Experimental bool
}

// Registry 渠道注册表（并发安全，支持运行时追加注册）。
type Registry struct {
	mu    sync.RWMutex
	specs map[string]ChannelSpec
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{specs: make(map[string]ChannelSpec)}
}

// Register 注册渠道（同名覆盖）。
func (r *Registry) Register(spec ChannelSpec) {
	if spec.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[spec.Name] = spec
}

// Spec 查询渠道条目。
func (r *Registry) Spec(name string) (ChannelSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[name]
	return s, ok
}

// IsChannel 判断是否为已注册渠道（取代散落的白名单判断）。
func (r *Registry) IsChannel(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.specs[name]
	return ok
}

// IsExperimental 渠道是否登记为实验性（B8：kuaishou 选择器纯模式猜测、零真机校准）。
func (r *Registry) IsExperimental(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[name]
	return ok && s.Experimental
}

// Supports 判断渠道是否支持指定传输（HTTP / WebSocket）。
func (r *Registry) Supports(name string, t Transport) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[name]
	if !ok {
		return false
	}
	for _, tr := range s.Transports {
		if tr == t {
			return true
		}
	}
	return false
}

// Names 返回已注册渠道名（排序，日志/诊断用）。
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.specs))
	for n := range r.specs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Default 全局默认注册表：5 大社交平台网页桥接渠道（HTTP + WebSocket 双传输）。
//
// HTTP 传输：bridge 扩展三通道（POST /api/bridge/ingest、GET /api/bridge/outbox、
// POST /api/bridge/outbox/ack）。
// WebSocket 传输：GET /api/ws/channel（register → inbound → ack；出站推帧）。
var Default = NewRegistry()

func init() {
	for _, spec := range []ChannelSpec{
		{Name: model.ChannelDouyin, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "抖音"},
		{Name: model.ChannelXHS, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "小红书"},
		// B8：快手网页 IM 选择器是 2026-08 观察的 [class*=…] 模式猜测，无真机
		// 校准、无回归测试——标 experimental（能力如实降级，链路保留）。
		{Name: model.ChannelKuaishou, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "快手", Experimental: true},
		{Name: model.ChannelXianyu, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "闲鱼"},
		{Name: model.ChannelTikTok, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "TikTok"},
	} {
		Default.Register(spec)
	}
}
