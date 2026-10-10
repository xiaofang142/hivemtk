package core

import (
	"testing"
	"time"
)

// TestHTTPTimeoutSeam 覆盖外部依赖默认 HTTP 超时的 seam 四格。
// 构造期读一次（NewBaseClient），所以这条参数在种子里标了 Restart: true。
func TestHTTPTimeoutSeam(t *testing.T) {
	// 必须直接写包内变量：生产 setter 传 nil 是「不注入」的空操作（防装配顺序错
	// 把兜底值顶掉），复位不了。同包测试是唯一能真正摘掉 provider 的地方。
	t.Cleanup(func() { httpTimeoutProvider = nil })

	if got := HTTPTimeoutEffective(); got != DefaultHTTPTimeout {
		t.Errorf("未注入时应等于兜底常量 %v, got %v", DefaultHTTPTimeout, got)
	}

	SetHTTPTimeoutProvider(func() time.Duration { return 45 * time.Second })
	if got := HTTPTimeoutEffective(); got != 45*time.Second {
		t.Errorf("注入 45s 后应读到 45s, got %v", got)
	}

	// 超时 0 会让 http.Client 认为"永不超时"，外部依赖挂起时 goroutine 永不返回。
	SetHTTPTimeoutProvider(func() time.Duration { return 0 })
	if got := HTTPTimeoutEffective(); got != DefaultHTTPTimeout {
		t.Errorf("注入 0 应回落兜底 %v, got %v", DefaultHTTPTimeout, got)
	}

	SetHTTPTimeoutProvider(func() time.Duration { return -time.Second })
	if got := HTTPTimeoutEffective(); got != DefaultHTTPTimeout {
		t.Errorf("注入负值应回落兜底 %v, got %v", DefaultHTTPTimeout, got)
	}

	// setter 传 nil 是空操作，不是复位。
	SetHTTPTimeoutProvider(func() time.Duration { return 90 * time.Second })
	SetHTTPTimeoutProvider(nil)
	if got := HTTPTimeoutEffective(); got != 90*time.Second {
		t.Errorf("setter 传 nil 后应保持 90s, got %v", got)
	}
}

// TestNewBaseClientPicksUpTimeoutSeam 钉住两个构造期读点真的走 seam：
// 一个是 http.Client.Timeout，一个是 BaseClient.Timeout 字段。
func TestNewBaseClientPicksUpTimeoutSeam(t *testing.T) {
	t.Cleanup(func() { httpTimeoutProvider = nil })

	bc := NewBaseClient()
	if bc.HTTPClient.Timeout != DefaultHTTPTimeout {
		t.Errorf("未注入时 HTTPClient.Timeout 应为 %v, got %v", DefaultHTTPTimeout, bc.HTTPClient.Timeout)
	}
	if bc.Timeout != DefaultHTTPTimeout {
		t.Errorf("未注入时 BaseClient.Timeout 应为 %v, got %v", DefaultHTTPTimeout, bc.Timeout)
	}

	SetHTTPTimeoutProvider(func() time.Duration { return 7 * time.Second })
	bc = NewBaseClient()
	if bc.HTTPClient.Timeout != 7*time.Second {
		t.Errorf("注入 7s 后 HTTPClient.Timeout 应为 7s, got %v", bc.HTTPClient.Timeout)
	}
	if bc.Timeout != 7*time.Second {
		t.Errorf("注入 7s 后 BaseClient.Timeout 应为 7s, got %v", bc.Timeout)
	}

	// 显式传 option 仍必须赢过参数中心：个别渠道（如长轮询）需要自己的超时。
	bc = NewBaseClient(WithTimeout(120 * time.Second))
	if bc.Timeout != 120*time.Second {
		t.Errorf("WithTimeout(120s) 应赢过注入值, got %v", bc.Timeout)
	}
}
