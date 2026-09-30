package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubProbe 可控失败次数的假探针
type stubProbe struct {
	name     string
	failLeft int
	calls    int
	err      error
}

func (s *stubProbe) Name() string { return s.name }

func (s *stubProbe) Probe(_ context.Context, _ string) (*ProbeResult, error) {
	s.calls++
	if s.failLeft > 0 {
		s.failLeft--
		return nil, s.err
	}
	return &ProbeResult{Engine: s.name, Response: "ok"}, nil
}

func TestProbeWithRetry_EventualSuccess(t *testing.T) {
	old := probeRetryBackoffs
	probeRetryBackoffs = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { probeRetryBackoffs = old }()

	p := &stubProbe{name: "flaky", failLeft: 2, err: errors.New("status 502: bad gateway")}
	pr, err := probeWithRetry(context.Background(), p, "q")
	if err != nil {
		t.Fatalf("expect success after retries, got %v", err)
	}
	if pr == nil || p.calls != 3 {
		t.Fatalf("expect 3 calls, got %d", p.calls)
	}
}

func TestProbeWithRetry_NoRetryOnCredentialError(t *testing.T) {
	p := &stubProbe{name: "bad", failLeft: 99, err: errors.New("status 401: Invalid token")}
	_, err := probeWithRetry(context.Background(), p, "q")
	if err == nil {
		t.Fatal("expect error")
	}
	if p.calls != 1 {
		t.Fatalf("401 must not retry, calls=%d", p.calls)
	}
}

func TestMatchBrandNames(t *testing.T) {
	names := []string{"HiveMtk", "微蜂", "HiveMTK"}
	cases := []struct {
		resp string
		want bool
	}{
		{"HiveMtk 是开源客服系统", true},
		{"微蜂客服值得推荐", true},
		{"HIVEMTK 部署很方便", true},
		{"某竞品客服系统不错", false},
		{"", false},
	}
	for _, c := range cases {
		if got := matchBrandNames(c.resp, names); got != c.want {
			t.Errorf("resp=%q want %v got %v", c.resp, c.want, got)
		}
	}
	if matchBrandNames("HiveMtk", nil) {
		t.Error("empty names must not match")
	}
}
