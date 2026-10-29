package egolite

// event_hub.go —— S-13：执行事件的本进程广播（SSE 数据源）。
//
// 为什么需要它：执行事件是 runner 上行写库的，HTTP 请求侧没有"等事件"的入口，
// 前端只能 3s 轮询。SSE 要么轮库（假流：仍是轮询，只是从浏览器搬到服务端），
// 要么在写库处广播——选后者：IngestEvent 是唯一写入口，广播点跟着它走。
//
// 纪律：
//   - 广播只做「有变化」的信号，不带数据：订阅方收到信号后按自己的游标回库拉增量，
//     保证「数据库是唯一事实源」，进程内信号丢了也不会漏事件（最坏是晚一拍）。
//   - 非阻塞发送（容量 1）：慢订阅者不能拖住 IngestEvent——事件落库优先于推送。
//   - 多副本部署时本 hub 只覆盖本进程：跨副本由客户端游标 + 心跳补拉兜底（见 SSE handler）。

import "sync"

// EgoEventHub 按执行号分组的变更广播。
type EgoEventHub struct {
	mu   sync.Mutex
	subs map[uint]map[chan struct{}]struct{}
}

// NewEgoEventHub 建一个空 hub。
func NewEgoEventHub() *EgoEventHub {
	return &EgoEventHub{subs: make(map[uint]map[chan struct{}]struct{})}
}

// Subscribe 订阅某执行的变更信号，返回信号通道与退订函数。
// 退订函数可重复调用（SSE 断连与 handler 收尾都会调一次）。
func (h *EgoEventHub) Subscribe(execID uint) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	if h == nil {
		return ch, func() {}
	}
	h.mu.Lock()
	if h.subs[execID] == nil {
		h.subs[execID] = make(map[chan struct{}]struct{})
	}
	h.subs[execID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set, ok := h.subs[execID]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(h.subs, execID)
				}
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish 通知某执行有新事件（非阻塞：订阅者没跟上就丢弃这一拍信号）。
//
// 发送在锁内完成：非阻塞发送不会卡住，但必须与「退订时关通道」互斥——
// 先收集通道再放锁去发，就会出现「发到一半订阅方已 close」的 send on closed panic
// （并发 SSE 断连时必现，本文件用例已钉住）。
func (h *EgoEventHub) Publish(execID uint) {
	if h == nil || execID == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[execID] {
		select {
		case ch <- struct{}{}:
		default: // 已有一拍未消费：合并成一次，不排队
		}
	}
}

// SubscriberCount 当前订阅数（测试与排障用）。
func (h *EgoEventHub) SubscriberCount(execID uint) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[execID])
}
