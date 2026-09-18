package agent

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestModeOf(t *testing.T) {
	tests := []struct {
		name  string
		input *model.AIAgent
		want  model.AgentMode
	}{
		{"nil 智能体回退被动", nil, model.AgentModePassive},
		{"空模式回退被动", &model.AIAgent{}, model.AgentModePassive},
		{"显式 passive", &model.AIAgent{AgentMode: "passive"}, model.AgentModePassive},
		{"显式 active", &model.AIAgent{AgentMode: "active"}, model.AgentModeActive},
		{"未知模式原样透传", &model.AIAgent{AgentMode: "turbo"}, model.AgentMode("turbo")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModeOf(tt.input); got != tt.want {
				t.Fatalf("ModeOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsActive(t *testing.T) {
	if IsActive(nil) {
		t.Fatal("IsActive(nil) = true, want false")
	}
	if IsActive(&model.AIAgent{AgentMode: "passive"}) {
		t.Fatal("IsActive(passive) = true, want false")
	}
	if !IsActive(&model.AIAgent{AgentMode: "active"}) {
		t.Fatal("IsActive(active) = false, want true")
	}
	if IsActive(&model.AIAgent{AgentMode: "unknown"}) {
		t.Fatal("IsActive(unknown) = true, want false")
	}
}
