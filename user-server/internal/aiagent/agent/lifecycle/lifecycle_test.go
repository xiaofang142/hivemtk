package lifecycle

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
)

type stubLifecycle struct{ mode string }

func (s *stubLifecycle) Mode() string { return s.mode }

func (s *stubLifecycle) Run(_ context.Context, _ *dto.AgentContext, _ *LifecycleRequest) (*LifecycleResult, error) {
	return &LifecycleResult{StopReason: s.mode}, nil
}

func TestResolver(t *testing.T) {
	passive := &stubLifecycle{mode: "passive"}
	active := &stubLifecycle{mode: "active"}

	tests := []struct {
		name   string
		active AgentLifecycle
		mode   string
		want   string
	}{
		{"active 模式命中主动实现", active, "active", "active"},
		{"active 实现缺失回退被动", nil, "active", "passive"},
		{"passive 模式走被动", active, "passive", "passive"},
		{"空模式走被动", active, "", "passive"},
		{"未知模式走被动", active, "turbo", "passive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver(passive, tt.active)
			if got := r(tt.mode); got != passive && got != active {
				t.Fatalf("Resolver(%q) 返回了未知实现: %v", tt.mode, got)
			}
			if got := r(tt.mode).Mode(); got != tt.want {
				t.Fatalf("Resolver(%q).Mode() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}

	t.Run("Run 返回各自 StopReason", func(t *testing.T) {
		res, err := passive.Run(context.Background(), nil, nil)
		if err != nil || res.StopReason != "passive" {
			t.Fatalf("passive.Run() = %+v, %v", res, err)
		}
		res, err = active.Run(context.Background(), nil, nil)
		if err != nil || res.StopReason != "active" {
			t.Fatalf("active.Run() = %+v, %v", res, err)
		}
	})
}
