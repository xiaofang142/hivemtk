package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// AUD-03：响应面 AK/SK 同口径掩码。此前 AK 明文回显而 SK 掩码，不对称——
// 列表/详情接口不该出现任何凭据原文；TestConnection 走控制器直收请求体
// DTO，不经过本转换，不受影响。
func TestObsConfigDTOMasksCredentials(t *testing.T) {
	s := &obsConfigService{}
	resp := s.convertToDTO(context.Background(), &model.ObsConfig{
		AccessKey: "ak-live-plaintext",
		SecretKey: "sk-live-plaintext",
	})
	if resp.AccessKey != "***" {
		t.Errorf("AccessKey 应掩码为 ***, got %q", resp.AccessKey)
	}
	if resp.SecretKey != "***" {
		t.Errorf("SecretKey 应掩码为 ***, got %q", resp.SecretKey)
	}
}
