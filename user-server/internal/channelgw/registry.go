package channelgw

import (
	"sort"
	"sync"

	"hivemtk-user/internal/model"
)

// ChannelSpec 渠道注册表条目：声明一个渠道的身份与支持传输。
//
// 渠道白名单单源化：此前 IsBridgeChannel（bridge 包）与 model 渠道常量各自维护，
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

// Default 全局默认注册表：九个桥接渠道（HTTP + WebSocket 双传输）。
// 成熟度两级以 ChannelSpec.Experimental 为准（CS-07 支持等级口径）：
//   - 正式（未标实验）：douyin/xiaohongshu/xianyu/tiktok——2026-07~08 真机校准与风控实测记录在案；
//   - 实验性（Experimental: true）：kuaishou（B8 纯模式猜测零真机）与 jd/bilibili/weibo/personal_wx
//     （2026-11 六渠道统一接待，选择器未真机校准/伴生未交付）。
//
// 实验集合与扩展侧 EXPERIMENTAL_CHANNELS（user-web/bridge/src/core/constants.js）同步维护，
// 由 registry_parity_test.go 双向钉住；注册但缺扩展适配器的渠道不得宣传为全支持。
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
		// 2026-11 六渠道统一接待新增：
		//   - jd/bilibili/weibo：网页私信选择器已有公开页面结构证据，但未真机校准；
		//   - personal_wx：服务端协议就绪，桌面伴生尚未交付（外部工程）。
		// 四者一律 Experimental，链路可跑但不宣称生产可用。
		{Name: model.ChannelJD, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "京东", Experimental: true},
		{Name: model.ChannelBilibili, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "B站", Experimental: true},
		{Name: model.ChannelWeibo, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "微博", Experimental: true},
		{Name: model.ChannelPersonalWX, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "个人微信", Experimental: true},
	} {
		Default.Register(spec)
	}
}
