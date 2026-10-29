package router

// egolite_stream.go —— S-13：执行事件 SSE（替换 Monitor 抽屉的 3s 轮询）。
//
// 形态：GET /api/egolite/executions/:id/stream?after_id=<cursor>
//   - 先补历史（游标之后最多 200 条/拍），再随广播增量推；
//   - 每拍回库取增量 ⇒ 库仍是唯一事实源，进程内信号丢了只是晚一拍；
//   - 15s 心跳注释帧（`: hb`）保活，同时顺带复查终态（多副本/信号丢失的兜底）；
//   - 执行进终态且没有待推事件 ⇒ 发 `event: end` 后关流，客户端不必自己判断何时停。
//
// 鉴权：走 eg 分组既有 JWT 中间件；归属校验在 GetForUser（越权与不存在同响应 404）。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/model"
)

const (
	egoStreamBatch     = 200
	egoStreamHeartbeat = 15 * time.Second
)

// egoStreamEventPayload 推给前端的帧（只带事件本体，不带请求上下文）
type egoStreamEventPayload struct {
	ID         uint            `json:"id"`
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Ok         *bool           `json:"ok"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	DurationMs int64           `json:"duration_ms"`
	CreatedAt  time.Time       `json:"created_at"`
}

func streamEgoExecution(c *gin.Context, execRepo egolite.EgoExecutionRepository, evRepo egolite.EgoEventRepository, hub *egolite.EgoEventHub, userID, execID uint) {
	ctx := c.Request.Context()
	e, err := execRepo.GetForUser(ctx, execID, userID)
	if err != nil {
		c.JSON(404, gin.H{"code": 1, "message": "执行不存在"})
		return
	}

	var cursor uint
	if raw := c.Query("after_id"); raw != "" {
		_, _ = fmt.Sscanf(raw, "%d", &cursor)
	}

	h := c.Writer.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx 反代：关缓冲，否则"流"会被攒成一次响应
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	signal, cancel := hub.Subscribe(execID)
	defer cancel()
	heartbeat := time.NewTicker(egoStreamHeartbeat)
	defer heartbeat.Stop()

	writeFrame := func(event string, id uint, data any) {
		if id > 0 {
			fmt.Fprintf(c.Writer, "id: %d\n", id)
		}
		if event != "" {
			fmt.Fprintf(c.Writer, "event: %s\n", event)
		}
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		c.Writer.Flush()
	}

	// drain 把游标之后的增量推完，返回是否还有更多（满批 ⇒ 继续拉，不等信号）
	drain := func() bool {
		batch, err := evRepo.ListByExecutionAfter(ctx, execID, cursor, egoStreamBatch)
		if err != nil {
			return false
		}
		for _, ev := range batch {
			writeFrame("", ev.ID, egoStreamEventPayload{
				ID: ev.ID, Seq: ev.Seq, Type: ev.Type, Ok: ev.Ok,
				Payload: json.RawMessage(ev.Payload), DurationMs: ev.DurationMs, CreatedAt: ev.CreatedAt,
			})
			cursor = ev.ID
		}
		return len(batch) == egoStreamBatch
	}

	for {
		for drain() {
			// 满批继续拉：历史积压要一次追平，不能一拍只走 200 条
		}
		// 每拍复查一次状态：终态 + 已推完 ⇒ 收流（客户端不必自己判何时停）
		if cur, err := execRepo.GetForUser(ctx, execID, userID); err == nil {
			e = cur
		}
		if isEgoTerminal(e.State) {
			writeFrame("end", 0, gin.H{"state": e.State, "fail_reason": e.FailReason, "artifacts": json.RawMessage(e.ArtifactsJSON)})
			return
		}

		select {
		case <-ctx.Done():
			return
		case _, ok := <-signal:
			if !ok { // 退订（理论上只有本函数 defer 会关）
				return
			}
		case <-heartbeat.C:
			// 注释帧：浏览器/netty 不会把它当事件，只用来保活 + 每 15s 复查终态
			fmt.Fprint(c.Writer, ": hb\n\n")
			c.Writer.Flush()
		}
	}
}

func isEgoTerminal(state string) bool {
	_, ok := model.EgoExecTerminalStates[state]
	return ok
}
