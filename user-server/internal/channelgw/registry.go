package channelgw

import (
	"sort"
	"sync"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

// OutboundPath 出站路径能力位（B5 单一 sender 能力位，CH-37b刀3 批准决议）：
// 声明该渠道的 AI 出站走哪条下发路径。webhook_outbound 的桥接分支据此判定
// （能力查询驱动，分支里不再维护第二份渠道清单），人工/触达路径的白名单
// 同源自本注册表（bridge 包 SetBridgeChannels 装配）。
const (
	// OutboundPathBridgeOutbox 桥接出库队列：AI 回复落 message_hub(status=pending)，
	// 由浏览器扩展/桌面伴生经 GET /api/bridge/outbox（或 WS outbound_reply 帧）领取投递。
	OutboundPathBridgeOutbox = "bridge_outbox"
	// OutboundPathNone 无服务端出站路径（只入站/暂不出站的渠道预留）。
	OutboundPathNone = "none"
	// direct_api（平台直连接口下发）将来预留，本刀不实现。
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
	// OutboundPath 出站路径能力位（B5）：取值 OutboundPathBridgeOutbox /
	// OutboundPathNone。空串 = 未声明（语义同 none，但没有出站分支会认它）。
	// 九个桥接渠道登记 bridge_outbox；webhook 直发渠道（wecom/telegram/whatsapp/
	// qq/feishu/dingtalk/weixin）不在本注册表，不受影响。
	OutboundPath string
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

// OutboundPathOf 查询渠道的出站路径能力位（B5）。
// 未注册或未声明的渠道返回空串（与 OutboundPathNone 的差别：none 是显式声明
// 「无出站路径」，空串是「登记时没写这一位」）。
func (r *Registry) OutboundPathOf(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.specs[name].OutboundPath
}

// OutboundChannels 返回登记了指定出站路径的渠道名（排序）。
// B5：OutboundPathBridgeOutbox 的返回值即 AI 桥接出站分支的能力集合
// （经 SetBridgeOutboxChannels 装配给 service，新增渠道只登记这里即自动获得出站路径）。
func (r *Registry) OutboundChannels(path string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.specs))
	for n, s := range r.specs {
		if s.OutboundPath == path {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
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
// 出站路径（B5 单一 sender 能力位）：九渠道一律 OutboundPath=bridge_outbox——AI 回复与
// 人工/触达消息都经桥接出库队列下发，SenderID ≡ accountID。webhook 直发渠道
// （wecom/telegram/whatsapp/qq/feishu/dingtalk/weixin）不在本注册表，不受影响。
//
// 实验集合与扩展侧 EXPERIMENTAL_CHANNELS（user-web/bridge/src/core/constants.js）同步维护，
// 由 registry_parity_test.go 双向钉住；注册但缺扩展适配器的渠道不得宣传为全支持。
// OutboundPath 能力集合与 webhook_outbound 桥接分支的恒等亦由 registry_parity_test.go
// 双向钉住（registry 有能力但出站不认 = 红；出站认但 registry 无能力 = 红）。
//
// HTTP 传输：bridge 扩展三通道（POST /api/bridge/ingest、GET /api/bridge/outbox、
// POST /api/bridge/outbox/ack）。
// WebSocket 传输：GET /api/ws/channel（register → inbound → ack；出站推帧）。
var Default = NewRegistry()

func init() {
	for _, spec := range []ChannelSpec{
		{Name: model.ChannelDouyin, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "抖音", OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelXHS, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "小红书", OutboundPath: OutboundPathBridgeOutbox},
		// B8：快手网页 IM 选择器是 2026-08 观察的 [class*=…] 模式猜测，无真机
		// 校准、无回归测试——标 experimental（能力如实降级，链路保留）。
		{Name: model.ChannelKuaishou, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "快手", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelXianyu, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "闲鱼", OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelTikTok, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "TikTok", OutboundPath: OutboundPathBridgeOutbox},
		// 2026-11 六渠道统一接待新增：
		//   - jd/bilibili/weibo：网页私信选择器已有公开页面结构证据，但未真机校准；
		//   - personal_wx：服务端协议就绪，桌面伴生尚未交付（外部工程）。
		// 四者一律 Experimental，链路可跑但不宣称生产可用。
		{Name: model.ChannelJD, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "京东", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelBilibili, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "B站", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelWeibo, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "微博", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
		{Name: model.ChannelPersonalWX, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "个人微信", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
		// CH-16（2026-11-09 首切片，CH-37a 批准选定渠道）：拼多多商家网页聊天。
		// 入口 https://mms.pinduoduo.com/chat-merchant/index.html（A+V 实证，登录墙后）；
		// 选择器为 X 级历史源码实证（ChatGPT-On-CS v1.4.5 静态源：.msg-list/.onemsg/
		// .buyer-item/.cs-item/#replyTextarea/.reply-footer .send-btn 等，2026-11-06
		// 评估报告 §5.1 复读），本方真机未校准——一律 Experimental，能力如实降级。
		{Name: model.ChannelPDD, Transports: []Transport{TransportHTTP, TransportWebSocket}, Label: "拼多多", Experimental: true, OutboundPath: OutboundPathBridgeOutbox},
	} {
		Default.Register(spec)
	}

	// B5 运行时单源装配：AI 出站桥接分支（service/webhook_outbound.go sendOutbound）
	// 的可出站判定集合取自本注册表的能力位。新增渠道只登记 registry（带
	// OutboundPath=bridge_outbox）即自动获得出站路径，无需改任何第二处。
	// 与 bridge/channel.go 对人工/触达白名单的 SetBridgeChannels 同一装配模式；
	// 恒等由 TestBridgeOutboxParityWithWebhookOutbound 双向钉住。
	service.SetBridgeOutboxChannels(Default.OutboundChannels(OutboundPathBridgeOutbox))
}
