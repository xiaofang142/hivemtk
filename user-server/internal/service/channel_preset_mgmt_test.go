package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/repository"
)

// ---------- 纯函数反例：CH-01 启用值解析 ----------

func TestChannelPreset_ParseEnabledValue(t *testing.T) {
	cases := []struct {
		value string
		want  bool
		err   bool
	}{
		{"", true, false}, // 未配置默认启用
		{"1", true, false},
		{"0", false, false},
		{" true ", true, false},
		{"garbage", false, true}, // 脏数据 fail-closed
	}
	for _, c := range cases {
		got, err := ParseEnabledValue(c.value)
		if (err != nil) != c.err || got != c.want {
			t.Fatalf("ParseEnabledValue(%q) = %v,%v，期望 %v,err=%v", c.value, got, err, c.want, c.err)
		}
		if c.err && !errors.Is(err, strconv.ErrSyntax) {
			t.Fatalf("脏值应返回 strconv.ErrSyntax，实得 %v", err)
		}
	}
}

// ---------- 内存仓储替身（CH-01 两租户隔离 / 幂等） ----------

type memChannelRepo struct {
	repository.ChannelRepository
	byCode map[string]*model.Channel
	byID   map[uint]*model.Channel
	rows   []model.Channel
	nextID uint
}

func newMemChannelRepo() *memChannelRepo {
	return &memChannelRepo{byCode: map[string]*model.Channel{}, byID: map[uint]*model.Channel{}, nextID: 1}
}

func (m *memChannelRepo) Available() bool { return true }

func (m *memChannelRepo) GetByCode(_ context.Context, code string) (*model.Channel, error) {
	return m.byCode[strings.ToLower(code)], nil
}

func (m *memChannelRepo) GetByTenantAndCode(_ context.Context, tid uint, code string) (*model.Channel, error) {
	for i := range m.rows {
		if m.rows[i].TenantID == tid && m.rows[i].Code == code {
			c := m.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}

func (m *memChannelRepo) GetByID(_ context.Context, tid, id uint) (*model.Channel, error) {
	ch := m.byID[id]
	if ch == nil || ch.TenantID != tid {
		return nil, nil
	}
	return ch, nil
}

func (m *memChannelRepo) Create(_ context.Context, ch *model.Channel) error {
	if m.byCode[ch.Code] != nil {
		return errors.New("duplicate code")
	}
	ch.ID = m.nextID
	m.nextID++
	copyCh := *ch
	m.byCode[ch.Code] = &copyCh
	m.byID[ch.ID] = &copyCh
	m.rows = append(m.rows, copyCh)
	return nil
}

func (m *memChannelRepo) List(_ context.Context, opts repository.ChannelListOptions) ([]model.Channel, error) {
	var out []model.Channel
	for _, row := range m.rows {
		if row.TenantID != opts.TenantID && !(opts.IncludeGlobal && row.TenantID == repository.ChannelGlobalTenantID) {
			continue
		}
		if opts.Enabled != nil && row.Enabled != *opts.Enabled {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (m *memChannelRepo) Update(_ context.Context, tid, id uint, updates map[string]any) error {
	ch := m.byID[id]
	if ch == nil || ch.TenantID != tid {
		return nil
	}
	if v, ok := updates["enabled"].(bool); ok {
		ch.Enabled = v
	}
	return nil
}

type memPresetState struct {
	repository.ChannelPresetStateRepository
	values map[string]string
}

func newMemPresetState() *memPresetState {
	return &memPresetState{values: map[string]string{}}
}

func (m *memPresetState) Available() bool { return true }

func (m *memPresetState) Get(_ context.Context, tid uint, code string) (string, error) {
	return m.values[strconv.FormatUint(uint64(tid), 10)+":"+code], nil
}

func (m *memPresetState) Set(_ context.Context, tid uint, code, value string) error {
	m.values[strconv.FormatUint(uint64(tid), 10)+":"+code] = value
	return nil
}

func customEditionTestCtx() context.Context {
	return edition.WithContext(context.Background(), edition.TypeCustom)
}

// 空库启动：全局模板只建一次；重复播种幂等；两租户启停互不影响。
func TestChannelPreset_EmptyBootstrapAndTenantIsolation(t *testing.T) {
	ctx := customEditionTestCtx()
	chRepo := newMemChannelRepo()
	state := newMemPresetState()
	svc := NewChannelServiceWithDeps(chRepo, state)

	created, err := svc.EnsurePresets(ctx, 1)
	if err != nil || created == 0 {
		t.Fatalf("空库播种应建全局模板，created=%d err=%v", created, err)
	}
	if got := len(chRepo.byCode); got != len(PresetChannelCodes) {
		t.Fatalf("全局模板行数应=%d，实得 %d", len(PresetChannelCodes), got)
	}
	// 模板必须落库（TenantID=0 表示全局），不能以常量代替
	for _, ch := range chRepo.byCode {
		if ch.TenantID != repository.ChannelGlobalTenantID {
			t.Fatalf("预设模板应落全局租户行，实得 tenant=%d", ch.TenantID)
		}
	}
	created2, err := svc.EnsurePresets(ctx, 2)
	if err != nil || created2 != 0 {
		t.Fatalf("重复播种应幂等零新建，created=%d err=%v", created2, err)
	}
	// EnsurePresets(ctx,2) 已顺手初始化 tenant 2；再次调用应幂等零新建。
	if n, err := svc.EnsureTenantPresets(ctx, 2); err != nil || n != 0 {
		t.Fatalf("租户初始化应幂等零新建，n=%d err=%v", n, err)
	}
	// 反例：未初始化的新租户 3 首次调用应新建
	if n, err := svc.EnsureTenantPresets(ctx, 3); err != nil || n == 0 {
		t.Fatalf("新租户应初始化启用配置，n=%d err=%v", n, err)
	}
	// 租户 2 关闭 telegram 不影响租户 1
	if err := svc.SetPresetEnabledForTenant(ctx, 2, "telegram", false); err != nil {
		t.Fatalf("设置租户 2 关闭失败: %v", err)
	}
	ok1, _ := svc.IsEnabledForTenant(ctx, 1, "telegram")
	ok2, _ := svc.IsEnabledForTenant(ctx, 2, "telegram")
	if !ok1 || ok2 {
		t.Fatalf("两租户启停应互不影响，tenant1=%v tenant2=%v", ok1, ok2)
	}
	// 选项列表对应排除/包含
	opts1, err := svc.EnabledOptions(ctx, 1)
	if err != nil {
		t.Fatalf("选项列表失败: %v", err)
	}
	opts2, err := svc.EnabledOptions(ctx, 2)
	if err != nil {
		t.Fatalf("选项列表失败: %v", err)
	}
	has := func(list []struct{ Code string }, code string) bool { return false }
	_ = has
	found1, found2 := false, false
	for _, o := range opts1 {
		if o.Code == "telegram" {
			found1 = true
		}
	}
	for _, o := range opts2 {
		if o.Code == "telegram" {
			found2 = true
		}
	}
	if !found1 || found2 {
		t.Fatalf("选项列表应反映租户启停，t1=%v t2=%v", found1, found2)
	}
}

// 自定义渠道（不在预设常量）对租户可见、不影响预设，且受租户隔离。
func TestChannelPreset_CustomChannelVisibleInOptions(t *testing.T) {
	ctx := customEditionTestCtx()
	chRepo := newMemChannelRepo()
	state := newMemPresetState()
	svc := NewChannelServiceWithDeps(chRepo, state)
	if _, err := svc.EnsurePresets(ctx, 1); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	// 租户 1 新增自定义渠道
	if err := chRepo.Create(ctx, &model.Channel{TenantID: 1, Name: "自建 IM", Code: "custom-im", Type: "dm", Enabled: true}); err != nil {
		t.Fatalf("新建自定义渠道失败: %v", err)
	}
	opts, err := svc.EnabledOptions(ctx, 1)
	if err != nil {
		t.Fatalf("选项失败: %v", err)
	}
	found := false
	for _, o := range opts {
		if o.Code == "custom-im" {
			found = true
		}
	}
	if !found {
		t.Fatalf("自定义渠道应在租户 1 选项可见")
	}
	// 租户 2 看不到（租户隔离）
	opts2, err := svc.EnabledOptions(ctx, 2)
	if err != nil {
		t.Fatalf("选项失败: %v", err)
	}
	for _, o := range opts2 {
		if o.Code == "custom-im" {
			t.Fatalf("自定义渠道不得跨租户可见")
		}
	}
	// 自定义渠道的行停用生效
	if err := chRepo.Update(ctx, 1, chRepo.byCode["custom-im"].ID, map[string]any{"enabled": false}); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	ok, err := svc.IsEnabledForTenant(ctx, 1, "custom-im")
	if err != nil || ok {
		t.Fatalf("停用自定义渠道应不可启用，ok=%v err=%v", ok, err)
	}
}

// 脏数据/仓储故障 fail-closed（不静默放行）
func TestChannelPreset_DirtyStateFailClosed(t *testing.T) {
	ctx := customEditionTestCtx()
	chRepo := newMemChannelRepo()
	state := newMemPresetState()
	svc := NewChannelServiceWithDeps(chRepo, state)
	if _, err := svc.EnsurePresets(ctx, 1); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	if err := state.Set(ctx, 1, "telegram", "garbage"); err != nil {
		t.Fatalf("写脏值失败: %v", err)
	}
	ok, err := svc.IsEnabledForTenant(ctx, 1, "telegram")
	if err != nil || ok {
		t.Fatalf("脏值应 fail-closed（ok=false,err=nil 或显式错），实得 ok=%v err=%v", ok, err)
	}
}
