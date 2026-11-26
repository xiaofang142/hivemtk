package service

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
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
}
