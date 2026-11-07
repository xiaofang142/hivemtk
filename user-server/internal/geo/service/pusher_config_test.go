package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/geo/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type stubPusherConfigRepo struct {
	existing  *model.GeoPusherConfig
	list      []*model.GeoPusherConfig
	created   *model.GeoPusherConfig
	updated   *model.GeoPusherConfig
	deletedID string
}

func (r *stubPusherConfigRepo) List(context.Context, int, int) ([]*model.GeoPusherConfig, int64, error) {
	return r.list, int64(len(r.list)), nil
}

func (r *stubPusherConfigRepo) GetByID(_ context.Context, id string) (*model.GeoPusherConfig, error) {
	if r.existing == nil || r.existing.ID != id {
		return nil, gorm.ErrRecordNotFound
	}
	return r.existing, nil
}

func (r *stubPusherConfigRepo) Create(_ context.Context, cfg *model.GeoPusherConfig) error {
	r.created = cfg
	return nil
}

func (r *stubPusherConfigRepo) Update(_ context.Context, cfg *model.GeoPusherConfig) error {
	r.updated = cfg
	return nil
}

func (r *stubPusherConfigRepo) Delete(_ context.Context, id string) error {
	r.deletedID = id
	return nil
}

func (r *stubPusherConfigRepo) ListAll(context.Context) ([]*model.GeoPusherConfig, error) {
	return r.list, nil
}

func (r *stubPusherConfigRepo) UpdateUsage(context.Context, string, int) error { return nil }

func TestPusherConfigService_Create校验(t *testing.T) {
	svc := NewPusherConfigService(&stubPusherConfigRepo{})
	ctx := context.Background()

	if _, err := svc.Create(ctx, PusherConfigInput{}); err == nil {
		t.Fatal("缺 platform 应报错")
	}
	platform := "baidu"
	if _, err := svc.Create(ctx, PusherConfigInput{Platform: &platform}); err == nil {
		t.Fatal("缺 config_json 应报错")
	}
	// 数组/标量不是对象：装配期按 key 取值会静默失效，必须拒绝。
	if _, err := svc.Create(ctx, PusherConfigInput{
		Platform: &platform, ConfigJSON: datatypes.JSON(`["x"]`),
	}); err == nil {
		t.Fatal("config_json 非对象应报错")
	}

	// 未知平台必须在入口拒绝：loadState 对未知平台静默跳过，
	// 否则调用方拿到 code=0 却一条记录都不会推送（D-22）。
	bogus := "bogus"
	if _, err := svc.Create(ctx, PusherConfigInput{
		Platform: &bogus, ConfigJSON: datatypes.JSON(`{"token":"x"}`),
	}); err == nil {
		t.Fatal("未知 platform 应报错")
	}

	repo := &stubPusherConfigRepo{}
	svc = NewPusherConfigService(repo)
	cfg, err := svc.Create(ctx, PusherConfigInput{
		Platform:   &platform,
		ConfigJSON: datatypes.JSON(`{"token":"x"}`),
	})
	if err != nil {
		t.Fatalf("合法入参创建失败: %v", err)
	}
	if cfg.Platform != "baidu" || !cfg.Active {
		t.Fatalf("默认值不符: %+v", cfg)
	}
	if repo.created != cfg {
		t.Fatal("Create 未落库")
	}
}

func TestPusherConfigService_Update只改可编辑字段(t *testing.T) {
	existing := &model.GeoPusherConfig{
		ID:         "cfg-1",
		TenantID:   42,
		Platform:   "baidu",
		ConfigJSON: datatypes.JSON(`{"token":"old"}`),
		DailyLimit: 100,
		UsedToday:  37,
		Active:     true,
	}
	repo := &stubPusherConfigRepo{existing: existing}
	svc := NewPusherConfigService(repo)

	limit := 9
	cfg, err := svc.Update(context.Background(), "cfg-1", PusherConfigInput{DailyLimit: &limit})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// 合并语义：未传的字段保留原值；运行时状态不被覆盖。
	if cfg.DailyLimit != 9 || cfg.Platform != "baidu" || cfg.UsedToday != 37 || cfg.TenantID != 42 {
		t.Fatalf("合并语义不符: %+v", cfg)
	}
	if string(cfg.ConfigJSON) != `{"token":"old"}` {
		t.Fatalf("未传 config_json 不应被改写: %s", cfg.ConfigJSON)
	}
	if repo.updated != cfg {
		t.Fatal("Update 未落库")
	}
	// 无效 config_json 必须在落库前拒绝。
	if _, err := svc.Update(context.Background(), "cfg-1", PusherConfigInput{
		ConfigJSON: datatypes.JSON(`{oops`),
	}); err == nil {
		t.Fatal("非法 config_json 应报错")
	}
}
