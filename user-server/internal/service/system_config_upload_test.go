package service

import (
	"context"
	"errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
	"testing"
)

// 阶段 0.5：ResolveUploadMaxBytes 的口径必须是"库里真实存在且 > 0 的值"，
// 拿不到就报 0 让调用方兜底。绝不能跟着 GetConfig 的 defaultConfig 走——
// 那条路在读不到行时会拿 50MB 顶上，等于把存量站点从 10MB 悄悄放宽 5 倍。

type stubSystemConfigRepo struct {
	cfg *model.SystemConfig
	err error
}

func (s *stubSystemConfigRepo) GetConfig(context.Context) (*model.SystemConfig, error) {
	return s.cfg, s.err
}

func (s *stubSystemConfigRepo) SaveConfig(_ context.Context, cfg *model.SystemConfig) (*model.SystemConfig, error) {
	return cfg, nil
}

func (s *stubSystemConfigRepo) CountUsers(context.Context) (int64, error) { return 0, nil }
func (s *stubSystemConfigRepo) PingDB(context.Context) bool               { return true }

func svcWithRepo(repo repository.SystemConfigRepository) *SystemConfigService {
	return &SystemConfigService{repo: repo}
}

func TestResolveUploadMaxBytesUsesPersistedValue(t *testing.T) {
	svc := svcWithRepo(&stubSystemConfigRepo{cfg: &model.SystemConfig{MaxUploadSizeMB: 200}})

	if got := svc.ResolveUploadMaxBytes(context.Background()); got != 200*1024*1024 {
		t.Fatalf("want %d, got %d", 200*1024*1024, got)
	}
}

// 反向钉死：GetConfig 在同样场景下会给 50MB，而 ResolveUploadMaxBytes 必须给 0。
// 两者不一致是故意的——让任何"图省事改用 GetConfig"的改动在用例里立刻现形。
func TestResolveUploadMaxBytesDoesNotInheritGetConfigDefault(t *testing.T) {
	svc := svcWithRepo(&stubSystemConfigRepo{err: errors.New("no row")})

	cfg, err := svc.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig 不该返回 error：%v", err)
	}
	if cfg.MaxUploadSizeMB != 50 {
		t.Fatalf("前提变了：GetConfig 的兜底应是 50，实际 %d", cfg.MaxUploadSizeMB)
	}
	if got := svc.ResolveUploadMaxBytes(context.Background()); got != 0 {
		t.Fatalf("读不到配置时应报 0（不是 defaultConfig 的 50MB）：got %d", got)
	}
}

func TestResolveUploadMaxBytesTreatsNonPositiveAsUnset(t *testing.T) {
	for _, v := range []int{0, -1, -50} {
		svc := svcWithRepo(&stubSystemConfigRepo{cfg: &model.SystemConfig{MaxUploadSizeMB: v}})
		if got := svc.ResolveUploadMaxBytes(context.Background()); got != 0 {
			t.Fatalf("MaxUploadSizeMB=%d 应视为未配置：got %d", v, got)
		}
	}
}

// 句柄没装配 / 服务体是 nil 时不许崩：上传是每个请求都会走一遍的链路，
// 崩在这里等于把"读不到配置"伪装成"进程挂了"。
func TestResolveUploadMaxBytesNilSafe(t *testing.T) {
	var nilSvc *SystemConfigService
	if got := nilSvc.ResolveUploadMaxBytes(context.Background()); got != 0 {
		t.Fatalf("nil 服务体应报 0：got %d", got)
	}
	if got := svcWithRepo(nil).ResolveUploadMaxBytes(context.Background()); got != 0 {
		t.Fatalf("nil 仓储应报 0：got %d", got)
	}
}

func TestResolveUploadMaxBytesNilConfigRow(t *testing.T) {
	svc := svcWithRepo(&stubSystemConfigRepo{cfg: nil})
	if got := svc.ResolveUploadMaxBytes(context.Background()); got != 0 {
		t.Fatalf("(nil, nil) 应报 0 而非崩：got %d", got)
	}
}
