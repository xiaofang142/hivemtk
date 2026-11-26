package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// senderNameAccountRepo 只实现自他标记用例需要的两个仓储方法；其余方法由内嵌接口占位，
// 一旦新用例意外触达未实现方法会直接 panic，避免静默走通。
type senderNameAccountRepo struct {
	repository.ChannelAccountRepository
	rows []model.ChannelAccount
}

func (m *senderNameAccountRepo) Available() bool { return true }

func (m *senderNameAccountRepo) GetByID(_ context.Context, tid, id uint) (*model.ChannelAccount, error) {
	for i := range m.rows {
		if m.rows[i].TenantID == tid && m.rows[i].ID == id {
			c := m.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}

func (m *senderNameAccountRepo) List(_ context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	out := make([]model.ChannelAccount, 0, len(m.rows))
	for _, row := range m.rows {
		if row.TenantID == opts.TenantID {
			out = append(out, row)
		}
	}
	return out, nil
}

// TestT03_SenderNameMark_Identity 账号标记（上报发送者名称）是自他判断的弱提示：
// 仅在稳定身份缺失时把 unknown 提升为 self，绝不判 customer；空标记/不匹配/歧义一律
// 保持 unknown（fail-closed），强证据优先不被名称覆盖。
func TestT03_SenderNameMark_Identity(t *testing.T) {
	ctx := context.Background()
	const mark = "四川同咨微信"
	base := []model.ChannelAccount{
		{ID: 10, TenantID: 18, ChannelID: 20, Type: "dm", SelfIdentifyName: mark},
		{ID: 11, TenantID: 18, ChannelID: 21, Type: "dm", SelfIdentifyName: "四川同咨QQ"},
	}

	cases := []struct {
		name       string
		rows       []model.ChannelAccount
		speakerID  string
		selfID     string
		senderName string
		wantSelf   bool
		wantRole   string
		wantEv     string
	}{
		{
			name: "上报发送者名称命中本账号标记且无稳定身份", rows: base,
			speakerID: "unknown-platform-id", selfID: "", senderName: " " + mark + " ",
			wantSelf: true, wantRole: "self", wantEv: "sender_name",
		},
		{
			name: "存量账号无标记不猜", rows: []model.ChannelAccount{
				{ID: 10, TenantID: 18, ChannelID: 20, Type: "dm", SelfIdentifyName: ""},
			}, senderName: mark, wantSelf: false, wantRole: "unknown", wantEv: "unknown",
		},
		{
			name: "名称不匹配不猜客户也不猜自己", rows: base,
			senderName: "某个客户昵称", wantSelf: false, wantRole: "unknown", wantEv: "unknown",
		},
		{
			name: "同租户两账号共用同一标记属歧义保持unknown", rows: []model.ChannelAccount{
				base[0], {ID: 12, TenantID: 18, ChannelID: 20, Type: "dm", SelfIdentifyName: mark},
			}, senderName: mark, wantSelf: false, wantRole: "unknown", wantEv: "unknown",
		},
		{
			name: "稳定self ID强证据优先名称不覆盖", rows: base,
			speakerID: "stable-self", selfID: "stable-self", senderName: mark,
			wantSelf: true, wantRole: "self", wantEv: "stable_id_or_role",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := NewChannelIdentityResolver(NewChannelResolverWithRepos(&senderNameAccountRepo{rows: tc.rows}, nil))
			got, err := ir.IdentifySpeakerWithSenderName(
				ctx, 18, 10, "dm", tc.speakerID, tc.senderName, tc.selfID, nil,
			)
			if err != nil {
				t.Fatalf("识别不应报错: %v", err)
			}
			if got.Self != tc.wantSelf || got.Role != tc.wantRole || got.Evidence != tc.wantEv {
				t.Fatalf("got self=%v role=%s evidence=%s; want self=%v role=%s evidence=%s",
					got.Self, got.Role, got.Evidence, tc.wantSelf, tc.wantRole, tc.wantEv)
			}
			if tc.wantEv == "unknown" && !IsUnknownIdentity(got) {
				t.Fatalf("unknown 必须被业务闸拒绝: %+v", got)
			}
		})
	}
}
