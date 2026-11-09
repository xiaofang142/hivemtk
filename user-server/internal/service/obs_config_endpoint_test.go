package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/dto"
)

// AUD-01：local Endpoint 必须是本地目录路径，URL 形态在创建入口即被拒，
// 不得落库后由 storage Factory 兜底拦截。
func TestObsConfigCreateEndpointRejectsURLShape(t *testing.T) {
	s := &obsConfigService{}
	req := &dto.CreateObsConfigRequest{
		Name:     "audit-obs",
		Provider: "local",
		Endpoint: "https://qiniu.example.com/materials",
		Domain:   "https://d.example.com",
	}
	if err := s.validateCreateRequest(context.Background(), req); err == nil {
		t.Error("URL 形态 Endpoint 必须被 validateCreateRequest 拒绝")
	}
}

func TestObsConfigCreateEndpointLocalPathAccepted(t *testing.T) {
	s := &obsConfigService{}
	req := &dto.CreateObsConfigRequest{Name: "audit-obs", Provider: "local", Endpoint: "/data/uploads"}
	if err := s.validateCreateRequest(context.Background(), req); err != nil {
		t.Errorf("合法本地路径被拒: %v", err)
	}
	if req.Domain == "" {
		t.Error("Domain 缺省应被 validateCreateRequest 填上默认值")
	}
}
