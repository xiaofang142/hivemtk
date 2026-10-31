package main

// telemetry.go —— S-9 runner 侧遥测：内存环形缓冲 + 周期批量上报。
//
// 采集纪律（与 internal/egolite/telemetry.go 同一口径，双端各自实现、样例测试钉住）：
//   - 只记低基数枚举与数值：kind / 耗时 / ok / 分类错误码 / skill slug / device_id；
//   - 永不收页面内容：事件构造函数只接受这几个字段，调用方想塞 URL/正文都没有入口；
//   - 服务端入口还有一道默认拒绝白名单（SanitizeTelemetry），这里写歪了也出不去。
//
// 缓冲纪律：环形、有界（满丢最旧）、不上锁热点路径（mutex 足够——事件频率是
// "每执行/每拉取一条"量级）。服务端不可达时缓冲保底，恢复后随下一轮补传；
// 长期断网最多少记（丢最旧），绝不影响执行主流程。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

const (
	// telemetryRingCap 环形容量：512 条 ≈ 断网一天的低频事件量，再多就该告警而不是囤。
	telemetryRingCap = 512
	// telemetryFlushEvery 周期上报间隔。
	telemetryFlushEvery = 30 * time.Second
	// telemetryFlushAt 缓冲到这个条数立即上报（不等周期）。
	telemetryFlushAt = 64
	// telemetryPostTimeout 上报超时（独立于资产拉取：遥测慢不该拖慢别的通道）。
	telemetryPostTimeout = 10 * time.Second
	// telemetryMaxPerPost 单次 POST 条数上限（与服务端 telemetryMaxBatch 对齐）。
	telemetryMaxPerPost = 500
)

// telemetrySample 一条遥测样本（上报前的内存形状 = 服务端白名单键的超集裁剪面）。
// 字段是**闭集**：新增采集面必须改这里（+服务端白名单），编译期可见，不会悄悄扩散。
type telemetrySample struct {
	Kind       string `json:"kind"`
	At         string `json:"at"`
	DurationMS int64  `json:"duration_ms"`
	OK         bool   `json:"ok"`
	SkillID    string `json:"skill_id,omitempty"`
	ErrorClass string `json:"error_class,omitempty"`
	RunnerID   string `json:"runner_id"`
}

// telemetryRing 环形缓冲（满丢最旧）。
type telemetryRing struct {
	mu   sync.Mutex
	buf  []telemetrySample
	head int // 下一写入位
	full bool
}

func newTelemetryRing(cap int) *telemetryRing {
	return &telemetryRing{buf: make([]telemetrySample, cap)}
}

// record 入队；返回被挤掉的最旧样本数（正常为 0，只有长期断网才 >0）。
func (t *telemetryRing) record(s telemetrySample) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.At == "" {
		s.At = time.Now().UTC().Format(time.RFC3339)
	}
	evicted := 0
	if t.full {
		evicted = 1 // 环满：head 位正是最旧样本，写入即覆盖
	}
	t.buf[t.head] = s
	t.head = (t.head + 1) % len(t.buf)
	if t.head == 0 {
		t.full = true
	}
	return evicted
}

// drain 取出全部样本（按入队顺序）并清空。
func (t *telemetryRing) drain() []telemetrySample {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.full {
		out := make([]telemetrySample, t.head)
		copy(out, t.buf[:t.head])
		t.head = 0
		return out
	}
	out := make([]telemetrySample, 0, len(t.buf))
	out = append(out, t.buf[t.head:]...)
	out = append(out, t.buf[:t.head]...)
	t.head = 0
	t.full = false
	return out
}

func (t *telemetryRing) len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.full {
		return len(t.buf)
	}
	return t.head
}

// runner 侧遥测单例（main 初始化；采集点只调 recordTelemetry）。
var telRing = newTelemetryRing(telemetryRingCap)

// recordTelemetry 采集点统一入口：错误码为空的失败样本归为 unknown；
// 缓冲达到阈值立即触发一次上报（突发批量安装场景不等周期）。
func recordTelemetry(kind string, dur time.Duration, ok bool, skillID, errClass string) {
	if !ok && errClass == "" {
		errClass = "unknown"
	}
	telRing.record(telemetrySample{
		Kind:       kind,
		DurationMS: dur.Milliseconds(),
		OK:         ok,
		SkillID:    skillID,
		ErrorClass: errClass,
		RunnerID:   confSnapshot.DeviceID,
	})
	if telRing.len() >= telemetryFlushAt {
		go flushTelemetry(confSnapshot)
	}
}

// confSnapshot main() 装好 conf 后写入（recordTelemetry 读 device_id 用；
// 遥测不需要每次拿锁读全量 conf）。
var confSnapshot runnerConf

// startTelemetryFlusher 周期 + 阈值双触发上报；ctx 取消即停（进程退出语义）。
func startTelemetryFlusher(c runnerConf, ctx <-chan struct{}) {
	confSnapshot = c
	go func() {
		ticker := time.NewTicker(telemetryFlushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx:
				return
			case <-ticker.C:
				flushTelemetry(c)
			}
		}
	}()
}

// flushTelemetryPost 单次上报（独立函数便于测试打桩）。
var flushTelemetryPost = func(c runnerConf, body []byte) error {
	u, err := url.Parse(c.ServerURL)
	if err != nil {
		return err
	}
	scheme := "http"
	if u.Scheme == "wss" || u.Scheme == "https" {
		scheme = "https"
	}
	u.Scheme = scheme
	u.Path = "/api/egolite/runner/telemetry"
	q := u.Query()
	q.Set("token", c.Token)
	u.RawQuery = q.Encode()
	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: telemetryPostTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // 上报后善后关闭，错误无从处置
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// flushTelemetry 取缓冲批量上报；失败整批放回队头（下一次随周期重试，丢最旧兜底）。
func flushTelemetry(c runnerConf) {
	samples := telRing.drain()
	for len(samples) > 0 {
		n := len(samples)
		if n > telemetryMaxPerPost {
			n = telemetryMaxPerPost
		}
		batch := samples[:n]
		samples = samples[n:]
		body, err := json.Marshal(map[string]any{"events": batch})
		if err != nil {
			continue // marshal 不了的是样本本身坏了（不可能路径），丢弃
		}
		if err := flushTelemetryPost(c, body); err != nil {
			// 失败：这一批放回（drain 后缓冲是空的，直接写回不会乱序）
			for i := len(batch) - 1; i >= 0; i-- {
				telRing.record(batch[i])
			}
			fmt.Fprintf(os.Stderr, "[ego-runner] 遥测上报失败（%d 条留缓冲）: %v\n", len(batch), err)
			return
		}
	}
}
