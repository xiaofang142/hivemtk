package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

func TestChannelOption_PreservesRemark(t *testing.T) {
	view := toChannelOption(&model.Channel{ID: 1, Name: "QQ", Code: "qq", Remark: "业务账号接入"})
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil || got["remark"] != "业务账号接入" {
		t.Fatalf("列表回填不得丢失渠道备注：%s err=%v", raw, err)
	}
}

type channelAccountUpdateRecorder struct {
	memAccountRepo
	updates map[string]any
}

func (r *channelAccountUpdateRecorder) Update(_ context.Context, _, _ uint, updates map[string]any) error {
	r.updates = updates
	return nil
}

func TestChannelAccountUpdate_OmittedTypePreservesLegacyValue(t *testing.T) {
	repo := &channelAccountUpdateRecorder{memAccountRepo: memAccountRepo{rows: []model.ChannelAccount{{ID: 1, TenantID: 18, ChannelID: 2, Name: "业务账号", Type: "group"}}}}
	svc := NewChannelAccountServiceWithRepo(repo)
	if err := svc.Update(context.Background(), 18, 1, &dto.ChannelAccountSave{ChannelID: 2, Name: "业务账号"}); err != nil {
		t.Fatal(err)
	}
	if _, changed := repo.updates["type"]; changed {
		t.Fatalf("新表单省略旧类型字段时不得覆盖历史兼容值：%v", repo.updates["type"])
	}
}

func TestChannelPresets_PlatformsExcludeConnectionServices(t *testing.T) {
	codes := map[string]bool{}
	for _, row := range PresetChannelCodes {
		codes[row.Code] = true
	}
	if !codes[model.ChannelQQ] || !codes[model.ChannelPersonalWX] || codes["sagex"] || codes["magex"] {
		t.Fatalf("预设应包含 QQ 和个人微信，接入服务不是渠道：%v", codes)
	}
	for _, code := range []string{"weixin", "dingtalk", "email", "web_embed"} {
		if !codes[code] {
			t.Fatalf("迁移已播种的全局预设不得在服务清单遗漏：%s", code)
		}
	}
}

// 管理列表必须合并全局模板与当前租户自定义行，并按租户 KV 展示启停，不得泄漏其他租户。
func TestChannelMasterList_GlobalTemplateAndTenantIsolation(t *testing.T) {
	ctx := customEditionTestCtx()
	channels := newMemChannelRepo()
	states := newMemPresetState()
	for _, row := range []*model.Channel{
		{TenantID: repository.ChannelGlobalTenantID, Name: "Telegram", Code: "telegram", Type: "group", Enabled: true},
		{TenantID: 1, Name: "租户一自建", Code: "custom-tenant-one", Type: "dm", Enabled: true},
		{TenantID: 2, Name: "租户二自建", Code: "custom-tenant-two", Type: "dm", Enabled: true},
	} {
		if err := channels.Create(ctx, row); err != nil {
			t.Fatalf("播种渠道失败: %v", err)
		}
	}
	if err := states.Set(ctx, 1, "telegram", "0"); err != nil {
		t.Fatalf("设置租户一开关失败: %v", err)
	}
	svc := NewChannelMasterServiceWithDeps(channels, nil, nil, states)

	rows1, err := svc.List(ctx, 1, dto.ChannelListQuery{})
	if err != nil {
		t.Fatalf("读取租户一渠道失败: %v", err)
	}
	rows2, err := svc.List(ctx, 2, dto.ChannelListQuery{})
	if err != nil {
		t.Fatalf("读取租户二渠道失败: %v", err)
	}
	byCode := func(rows []dto.ChannelOption) map[string]dto.ChannelOption {
		out := make(map[string]dto.ChannelOption, len(rows))
		for _, row := range rows {
			out[row.Code] = row
		}
		return out
	}
	got1, got2 := byCode(rows1), byCode(rows2)
	if len(rows1) != 2 || len(rows2) != 2 {
		t.Fatalf("每个租户应看到一条全局模板和自己的自定义渠道: tenant1=%+v tenant2=%+v", rows1, rows2)
	}
	if got1["telegram"].Enabled || !got2["telegram"].Enabled {
		t.Fatalf("同一全局模板应按租户开关分别显示: tenant1=%+v tenant2=%+v", got1["telegram"], got2["telegram"])
	}
	if _, ok := got1["custom-tenant-one"]; !ok {
		t.Fatalf("租户一自定义渠道缺失: %+v", rows1)
	}
	if _, ok := got1["custom-tenant-two"]; ok {
		t.Fatalf("租户二自定义渠道不得泄漏给租户一: %+v", rows1)
	}
	if _, ok := got2["custom-tenant-one"]; ok {
		t.Fatalf("租户一自定义渠道不得泄漏给租户二: %+v", rows2)
	}
	disabled := false
	disabledRows, err := svc.List(ctx, 1, dto.ChannelListQuery{Enabled: &disabled})
	if err != nil || len(disabledRows) != 1 || disabledRows[0].Code != "telegram" {
		t.Fatalf("状态过滤必须在租户状态合成后执行: rows=%+v err=%v", disabledRows, err)
	}
	detail, err := svc.Get(ctx, 1, got1["telegram"].ID)
	if err != nil || detail == nil || detail.Enabled {
		t.Fatalf("全局模板详情必须可读且反映租户停用: detail=%+v err=%v", detail, err)
	}
	if other, err := svc.Get(ctx, 1, got2["custom-tenant-two"].ID); other != nil || !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("详情不得跨租户读取自定义渠道: other=%+v err=%v", other, err)
	}
}

// 全局模板开关只写当前租户 KV，不改模板行，也不影响其他租户。
func TestChannelCatalog_GlobalTemplateToggleUsesTenantState(t *testing.T) {
	ctx := customEditionTestCtx()
	channels := newMemChannelRepo()
	states := newMemPresetState()
	global := &model.Channel{TenantID: repository.ChannelGlobalTenantID, Name: "邮件", Code: "email", Type: "dm", Enabled: true}
	if err := channels.Create(ctx, global); err != nil {
		t.Fatalf("播种全局邮件渠道失败: %v", err)
	}
	channelSvc := NewChannelServiceWithDeps(channels, states)
	masterSvc := NewChannelMasterServiceWithDeps(channels, nil, nil, states)
	catalogSvc := NewChannelCatalogServiceWithDeps(channelSvc, nil, masterSvc)
	if err := catalogSvc.SetChannelEnabledForTenant(ctx, 1, global.ID, false); err != nil {
		t.Fatalf("停用租户一邮件渠道失败: %v", err)
	}
	if got := states.values["1:email"]; got != "0" {
		t.Fatalf("停用必须写租户一 KV，实得 %q", got)
	}
	if !channels.byID[global.ID].Enabled {
		t.Fatal("租户开关不得修改全局模板行")
	}
	enabled1, err := channelSvc.IsEnabledForTenant(ctx, 1, "email")
	if err != nil || enabled1 {
		t.Fatalf("租户一邮件渠道应停用: enabled=%v err=%v", enabled1, err)
	}
	enabled2, err := channelSvc.IsEnabledForTenant(ctx, 2, "email")
	if err != nil || !enabled2 {
		t.Fatalf("租户二邮件渠道应保持启用: enabled=%v err=%v", enabled2, err)
	}
}
