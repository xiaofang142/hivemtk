package service

import (
	"context"
	"errors"
	"testing"
)

// TestSageXUnifiedProtocolVersion 闸 12（G-04）：SageX 上报必须接受统一协议标识
// hivemtk.channel.v1，并保留 schema_version=1.0 别名；未知版本仍 fail-closed。
func TestSageXUnifiedProtocolVersion(t *testing.T) {
	s := &SageXUploadService{}
	base := func() *SageXUploadRequest {
		return &SageXUploadRequest{BatchID: "b1", Events: []SageXEventItem{{EventID: "e1"}}}
	}

	legacy := base()
	legacy.SchemaVersion = SageXSchemaVersion
	_, err := s.HandleUpload(context.Background(), "", legacy, 0)
	if !errors.Is(err, ErrSageXUnauthorized) {
		t.Errorf("旧 schema_version 应被接受（走到鉴权阶段），实际 err=%v", err)
	}

	canonical := base()
	canonical.ProtocolVersion = SageXProtocolVersion
	_, err = s.HandleUpload(context.Background(), "", canonical, 0)
	if !errors.Is(err, ErrSageXUnauthorized) {
		t.Errorf("统一 protocol_version 应被接受（走到鉴权阶段），实际 err=%v", err)
	}

	unknown := base()
	unknown.ProtocolVersion = "hivemtk.channel.v9"
	_, err = s.HandleUpload(context.Background(), "", unknown, 0)
	if !errors.Is(err, ErrSageXUnsupportedSchema) {
		t.Errorf("未知协议版本必须拒绝，实际 err=%v", err)
	}

	if SageXProtocolVersion != "hivemtk.channel.v1" {
		t.Errorf("统一协议标识漂移: %q", SageXProtocolVersion)
	}
}
