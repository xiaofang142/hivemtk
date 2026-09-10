package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestNew_HasSaneDefaults(t *testing.T) {
	c := New()
	if c.Timeout != 15*time.Second {
		t.Fatalf("default timeout = %v, want 15s", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", c.Transport)
	}
	if tr.MaxIdleConns != 100 || tr.MaxIdleConnsPerHost != 20 {
		t.Fatalf("pool defaults wrong: maxIdle=%d perHost=%d", tr.MaxIdleConns, tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("idle conn timeout = %v, want 90s", tr.IdleConnTimeout)
	}
}

func TestNewWithTimeout_OverridesOnlyTimeout(t *testing.T) {
	c := NewWithTimeout(30 * time.Second)
	if c.Timeout != 30*time.Second {
		t.Fatalf("override timeout = %v, want 30s", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.MaxIdleConns != 100 {
		t.Fatalf("pool config should stay unchanged after timeout override")
	}
}
