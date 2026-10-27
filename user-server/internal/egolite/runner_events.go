package egolite

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
)

// runner 读循环事件 ingest 接线（把 runner 上行帧交给执行域服务）。
// 分离成独立文件：读循环结构不因业务扩展变形，Service 注入点清晰。

// EventSink 事件接收器（执行域服务实现；nil=骨架行为只维持活性）
type EventSink interface {
	IngestEvent(ctx context.Context, userID uint, ev RunnerEvent) error
}

// AttachEventSink 把执行域事件服务挂到 WS handler
func (h *EgoRunnerWSHandler) AttachEventSink(sink EventSink) {
	h.sink = sink
}

// readEvents 业务读循环：runner 上行两类帧——事件行（ingest）、pong（心跳）。
// 未知帧只忽略不崩（协议向前兼容）。
func (h *EgoRunnerWSHandler) readEvents(conn *websocket.Conn, userID uint) {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		var ev RunnerEvent
		if err := conn.ReadJSON(&ev); err != nil {
			return
		}
		switch ev.Type {
		case "pong":
			// 心跳；deadline 已刷新
		case "action", "snapshot", "artifact", "handoff", "extract", "confirm", "done", "error", "runner":
			if h.sink != nil {
				// 独立 ctx：不绑 WS 请求生命周期——读循环退出（连接断）不能半路
				// 杀掉最后一帧（done/error）的落库；终态写内部另有 WithoutCancel 双保险。
				ctx := context.Background()
				_ = h.sink.IngestEvent(ctx, userID, ev)
			}
		default:
			// 未知帧只忽略（协议向前兼容）
		}
	}
}
