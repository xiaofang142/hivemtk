package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"hivemtk-user/internal/storage"
)

// newLocalDriverFromEnv 从环境构造 LocalDriver（与 router.go 的 uploadDir 配置保持一致）
func newLocalDriverFromEnv() (*storage.LocalDriver, error) {
	baseDir := os.Getenv("STORAGE_LOCAL_BASE_DIR")
	if baseDir == "" {
		baseDir = "./uploads"
	}
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, err
	}
	// publicBaseURL 留空时 LocalDriver 自带 "/files" 默认值
	return storage.NewLocalDriver(baseDir, ""), nil
}

func decodeBase64(s string) ([]byte, error) {
	// 兼容 data URL 前缀（data:image/png;base64,....）
	if i := strings.Index(s, "base64,"); i >= 0 {
		s = s[i+len("base64,"):]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("screenshot base64 解码失败: " + err.Error())
	}
	return raw, nil
}

func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
