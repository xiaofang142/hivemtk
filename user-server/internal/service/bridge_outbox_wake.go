package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"hivemtk-user/internal/pkg/tenant"
)

var ErrBridgeOutboxWakeScope = errors.New("outbox wake scope unavailable")

type bridgeWakeKey struct {
	tenantID  uint
	channel   string
	accountID string
}

// BridgeOutboxWakeBroker 只保存容量为一的唤醒信号，不持有消息、数据库或认领器。
type BridgeOutboxWakeBroker struct {
	mu           sync.Mutex
	subs         map[bridgeWakeKey]map[uint64]chan struct{}
	nextID       uint64
	count        int
	tenantCounts map[uint]int
}

func NewBridgeOutboxWakeBroker() *BridgeOutboxWakeBroker {
	return &BridgeOutboxWakeBroker{subs: make(map[bridgeWakeKey]map[uint64]chan struct{})}
}

var GlobalBridgeOutboxWake = NewBridgeOutboxWakeBroker()

func bridgeOutboxWakeKey(ctx context.Context, channel, accountID string) (bridgeWakeKey, error) {
	if ctx == nil || ctx.Err() != nil || tenant.IsSystemScope(ctx) {
		return bridgeWakeKey{}, ErrBridgeOutboxWakeScope
	}
	id, bound := tenant.FromContext(ctx)
	invalidAccount := accountID == "" || accountID == "default" || len(accountID) > 100 || strings.TrimSpace(accountID) != accountID
	if !bound || !isBridgeChannel(channel) || invalidAccount || strings.IndexFunc(accountID, unicode.IsControl) >= 0 {
		return bridgeWakeKey{}, ErrBridgeOutboxWakeScope
	}
	return bridgeWakeKey{tenantID: id, channel: channel, accountID: accountID}, nil
}

func (b *BridgeOutboxWakeBroker) Subscribe(ctx context.Context, channel, accountID string) (<-chan struct{}, func(), error) {
	key, err := bridgeOutboxWakeKey(ctx, channel, accountID)
	if err != nil || b == nil {
		return nil, nil, ErrBridgeOutboxWakeScope
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count >= 2048 {
		return nil, nil, errors.New("outbox wake subscription capacity reached")
	}
	if b.tenantCounts[key.tenantID] >= 256 {
		return nil, nil, errors.New("outbox wake tenant subscription capacity reached")
	}
	if b.tenantCounts == nil {
		b.tenantCounts = make(map[uint]int)
	}
	if b.subs == nil {
		b.subs = make(map[bridgeWakeKey]map[uint64]chan struct{})
	}
	if b.subs[key] == nil {
		b.subs[key] = make(map[uint64]chan struct{})
	}
	b.nextID++
	id := b.nextID
	signal := make(chan struct{}, 1)
	b.subs[key][id] = signal
	b.count++
	b.tenantCounts[key.tenantID]++
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subs[key], id)
			if len(b.subs[key]) == 0 {
				delete(b.subs, key)
			}
			b.count--
			b.tenantCounts[key.tenantID]--
			if b.tenantCounts[key.tenantID] == 0 {
				delete(b.tenantCounts, key.tenantID)
			}
			close(signal)
		})
	}
	return signal, cancel, nil
}

func (b *BridgeOutboxWakeBroker) Publish(ctx context.Context, channel, accountID string) bool {
	key, err := bridgeOutboxWakeKey(ctx, channel, accountID)
	if err != nil || b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	found := false
	for _, signal := range b.subs[key] {
		found = true
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	return found
}

// Stream 的初次信号也不补拉或认领；断流后客户端继续使用原轮询。
func (b *BridgeOutboxWakeBroker) Stream(ctx context.Context, channel, accountID string, writer http.ResponseWriter, heartbeat, lifetime time.Duration) error {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return errors.New("outbox wake streaming unavailable")
	}
	signals, cancel, err := b.Subscribe(ctx, channel, accountID)
	if err != nil {
		return err
	}
	defer cancel()
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	if lifetime <= 0 || lifetime > 5*time.Minute {
		lifetime = 5 * time.Minute
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Accel-Buffering", "no")
	writeSignal := func() error {
		_, err := fmt.Fprint(writer, "event: outbox_ready\ndata: {}\n\n")
		if err == nil {
			flusher.Flush()
		}
		return err
	}
	if err := writeSignal(); err != nil {
		return err
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	deadline := time.NewTimer(lifetime)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-signals:
			if err := writeSignal(); err != nil {
				return err
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(writer, ": heartbeat\n\n"); err != nil {
				return err
			}
			flusher.Flush()
		}
	}
}
