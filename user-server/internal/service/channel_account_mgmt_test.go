package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ---------- CH-02 凭据脱敏/映射纯函数反例 ----------

func TestChannelAccountCredential_MaskAndMerge(t *testing.T) {
	// 空值保持空；短值全遮蔽；长值保留前后各 2 位
	if got := MaskCredential(""); got != "" {
		t.Fatalf("空凭据应保持空，实得 %q", got)
	}
	if got := MaskCredential("abc"); got != "***" {
		t.Fatalf("短凭据应全遮蔽，实得 %q", got)
	}
	got := MaskCredential("1234567890")
	if got != "12****90" {
		t.Fatalf("长凭据脱敏格式错误，实得 %q", got)
	}
	// 编辑时前端回传脱敏值不得覆盖真实 secret
	old := ChannelAccountCredential{Platform: "telegram", ExternalID: "ext-1", Secret: "real-secret-token"}
	merged, err := MergeCredential(old, ChannelAccountCredential{Secret: got}, false)
	if err != nil || merged.Secret != "real-secret-token" {
		t.Fatalf("脱敏占位不得覆盖真实值，merged=%+v err=%v", merged, err)
	}
	// 真实新值应覆盖；明确清空应清空
	merged, err = MergeCredential(old, ChannelAccountCredential{Secret: "new-token"}, false)
	if err != nil || merged.Secret != "new-token" {
		t.Fatalf("新值应覆盖，merged=%+v err=%v", merged, err)
	}
	merged, err = MergeCredential(old, ChannelAccountCredential{}, true)
	if err != nil || merged.Secret != "" {
		t.Fatalf("明确清空应清空，merged=%+v err=%v", merged, err)
	}
	// 回显绝不出现明文
	view := toCredentialView(old)
	if view.Secret == old.Secret || !view.Verified {
		t.Fatalf("回显必须脱敏且标记已配置，view=%+v", view)
	}
}

func TestChannelAccountCredential_ValidatePlatform(t *testing.T) {
	valid := map[string]ChannelAccountCredential{
		"telegram": {ExternalID: "bot1", Secret: "token"},
		"qq":       {ExternalID: "12345"},
		"feishu":   {ExternalID: "app1"},
		"wecom":    {ExternalID: "corp1"},
		"whatsapp": {ExternalID: "wa1"},
	}
	for p, c := range valid {
		if err := ValidateCredentialForPlatform(p, "", c); err != nil {
			t.Fatalf("%s 合法配置应通过: %v", p, err)
		}
	}
	// 反例：未知平台、缺外部 ID、TG 缺 token
	if err := ValidateCredentialForPlatform("unknown", "", ChannelAccountCredential{ExternalID: "x"}); err == nil {
		t.Fatalf("未知平台必须拒绝")
	}
	if err := ValidateCredentialForPlatform("qq", "", ChannelAccountCredential{}); err == nil {
		t.Fatalf("缺平台账号 ID 必须拒绝")
	}
	if err := ValidateCredentialForPlatform("telegram", "", ChannelAccountCredential{ExternalID: "x"}); err == nil {
		t.Fatalf("TG 缺 token 必须拒绝")
	}
}

func TestChannelAccountCredential_MigrationStableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	kv := &memCredentialKV{values: map[string]string{}}
	svc := NewChannelAccountCredentialService(kv)
	records := []ChannelAccountMigrationRecord{
		{TenantID: 2, Platform: "qq", LegacyID: "old-2", AccountID: 20, ChannelID: 2},
		{TenantID: 1, Platform: "telegram", LegacyID: "old-1", AccountID: 10, ChannelID: 1},
	}
	// 反向创建顺序 + 重复条目：预览恒同、去重
	preview, err := svc.MigratePreview(ctx, 1, append(records, records[1]))
	if err != nil || len(preview) != 1 {
		t.Fatalf("预览应只含租户 1 一条去重记录，preview=%+v err=%v", preview, err)
	}
	previewAll, err := svc.MigratePreview(ctx, 1, records)
	if err != nil || len(previewAll) != 1 {
		t.Fatalf("跨租户记录不得混入，preview=%+v err=%v", previewAll, err)
	}
	// 冲突：同一旧 ID 映射到两个账号必须拒绝
	conflict := append([]ChannelAccountMigrationRecord{}, records...)
	conflict = append(conflict, ChannelAccountMigrationRecord{TenantID: 1, Platform: "telegram", LegacyID: "old-1", AccountID: 99})
	if _, err := svc.MigratePreview(ctx, 1, conflict); err == nil {
		t.Fatalf("同一旧 ID 冲突映射必须拒绝")
	}
	// 应用幂等：第一次新建 1，第二次零新建
	created, err := svc.MigrateApply(ctx, 1, records)
	if err != nil || created != 1 {
		t.Fatalf("首次迁移应建 1 条，created=%d err=%v", created, err)
	}
	created2, err := svc.MigrateApply(ctx, 1, records)
	if err != nil || created2 != 0 {
		t.Fatalf("重复迁移应零新建，created=%d err=%v", created2, err)
	}
}

type memCredentialKV struct {
	values map[string]string
	err    error
}

func (m *memCredentialKV) GetRaw(_ context.Context, tenantID uint, key string) (string, error) {
	return m.values[strconv.FormatUint(uint64(tenantID), 10)+":"+key], m.err
}
func (m *memCredentialKV) SetRaw(_ context.Context, tenantID uint, key, value string) error {
	m.values[strconv.FormatUint(uint64(tenantID), 10)+":"+key] = value
	return m.err
}

// ---------- CH-03 管理列表/备注/停用找回 ----------

type memAccountRepo struct {
	repository.ChannelAccountRepository
	rows   []model.ChannelAccount
	nextID uint
	err    error
}

func (m *memAccountRepo) Available() bool { return true }

// GetByIdentifyCode 精确匹配冗余码；多条命中歧义拒绝（不取首条）。
func (m *memAccountRepo) GetByIdentifyCode(_ context.Context, code string) (*model.ChannelAccount, error) {
	var found []model.ChannelAccount
	for _, row := range m.rows {
		if row.IdentifyCode == code {
			found = append(found, row)
		}
	}
	if len(found) > 1 {
		return nil, repository.ErrAmbiguousIdentifyCode
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

func (m *memAccountRepo) GetByID(_ context.Context, tid, id uint) (*model.ChannelAccount, error) {
	if m.err != nil {
		return nil, m.err
	}
	for i := range m.rows {
		if m.rows[i].TenantID == tid && m.rows[i].ID == id {
			c := m.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}

func (m *memAccountRepo) Update(_ context.Context, tid, id uint, updates map[string]any) error {
	if m.err != nil {
		return m.err
	}
	for i := range m.rows {
		if m.rows[i].TenantID != tid || m.rows[i].ID != id {
			continue
		}
		if v, ok := updates["status"].(int); ok {
			m.rows[i].Status = v
		}
		if v, ok := updates["remark"].(string); ok {
			m.rows[i].Remark = v
		}
		if v, ok := updates["name"].(string); ok {
			m.rows[i].Name = v
		}
		if v, ok := updates["self_identify_name"].(string); ok {
			m.rows[i].SelfIdentifyName = v
		}
		return nil
	}
	return nil
}

type memManageRepo struct {
	repository.ChannelAccountManageRepository
	rows []model.ChannelAccount
	err  error
}

func (m *memManageRepo) Available() bool { return true }

func filterAccounts(rows []model.ChannelAccount, opts repository.ChannelAccountListOptions, paginate bool) []model.ChannelAccount {
	var out []model.ChannelAccount
	for _, row := range rows {
		if row.TenantID != opts.TenantID {
			continue
		}
		if opts.Status != nil && row.Status != *opts.Status {
			continue
		}
		out = append(out, row)
	}
	if paginate {
		start, end := opts.Offset, opts.Offset+opts.Limit
		if start > len(out) {
			start = len(out)
		}
		if end > len(out) || opts.Limit <= 0 {
			end = len(out)
		}
		out = out[start:end]
	}
	return out
}

func (m *memManageRepo) ListManaged(_ context.Context, opts repository.ChannelAccountListOptions) ([]model.ChannelAccount, error) {
	if m.err != nil {
		return nil, m.err
	}
	return filterAccounts(m.rows, opts, true), nil
}
func (m *memManageRepo) CountManaged(_ context.Context, opts repository.ChannelAccountListOptions) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return int64(len(filterAccounts(m.rows, opts, false))), nil
}

func seedManageAccounts() (*memAccountRepo, *memManageRepo) {
	rows := []model.ChannelAccount{
		{TenantID: 1, ID: 1, ChannelID: 1, Name: "acc-1", Remark: "带备注", SelfIdentifyName: "自己", Type: "dm", Status: 1},
		{TenantID: 1, ID: 2, ChannelID: 1, Name: "acc-2", Remark: "停用备注", SelfIdentifyName: "自己2", Type: "group", Status: 0},
		{TenantID: 1, ID: 3, ChannelID: 1, Name: "acc-3", Status: 1},
		{TenantID: 2, ID: 4, ChannelID: 1, Name: "other-tenant", Status: 1},
	}
	a := &memAccountRepo{rows: append([]model.ChannelAccount{}, rows...), nextID: 5}
	// 管理仓储与写仓储共享同一行切片，模拟同一张表（更新后列表可见）
	m := &memManageRepo{rows: a.rows}
	return a, m
}

func TestChannelAccountManage_ListFilterAndPaging(t *testing.T) {
	ctx := context.Background()
	a, m := seedManageAccounts()
	svc := NewChannelAccountManageServiceWithRepos(a, m)

	// 默认全部：含停用、含备注、正确 total
	rows, total, err := svc.ListManaged(ctx, 1, dto.ChannelAccountListQuery{})
	if err != nil || total != 3 || len(rows) != 3 {
		t.Fatalf("全部过滤应 3 条，total=%d len=%d err=%v", total, len(rows), err)
	}
	if rows[0].Remark != "带备注" || rows[1].Status != 0 {
		t.Fatalf("管理列表必须保留备注与停用账号，rows=%+v", rows)
	}
	// 仅停用
	off := 0
	rows, total, err = svc.ListManaged(ctx, 1, dto.ChannelAccountListQuery{Status: &off})
	if err != nil || total != 1 || rows[0].ID != 2 {
		t.Fatalf("停用过滤应只返回 acc-2，total=%d rows=%+v err=%v", total, rows, err)
	}
	// 第二页：limit=1 offset=1，total 不受分页影响
	rows, total, err = svc.ListManaged(ctx, 1, dto.ChannelAccountListQuery{Limit: 1, Offset: 1})
	if err != nil || total != 3 || len(rows) != 1 || rows[0].ID != 2 {
		t.Fatalf("第二页总数应为 3 且取到 acc-2，total=%d rows=%+v err=%v", total, rows, err)
	}
	// 跨租户不可见
	rows, total, err = svc.ListManaged(ctx, 2, dto.ChannelAccountListQuery{})
	if err != nil || total != 1 || rows[0].ID != 4 {
		t.Fatalf("租户隔离失败，total=%d rows=%+v err=%v", total, rows, err)
	}
}

func TestChannelAccountManage_UpdateKeepsUnloadedFields(t *testing.T) {
	ctx := context.Background()
	a, m := seedManageAccounts()
	svc := NewChannelAccountManageServiceWithRepos(a, m)

	// 详情完整加载后才提交：仅改名称，备注/自他名称/类型保留
	detail, err := svc.GetDetail(ctx, 1, 1)
	if err != nil || detail.Remark != "带备注" {
		t.Fatalf("详情必须完整回读备注，detail=%+v err=%v", detail, err)
	}
	name := "new-name"
	if err := svc.UpdateManaged(ctx, 1, 1, &ChannelAccountManagedSave{Name: &name}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	after := a.rows[0]
	if after.Name != "new-name" || after.Remark != "带备注" || after.SelfIdentifyName != "自己" || after.Type != "dm" {
		t.Fatalf("仅改名称不得清空未改字段，after=%+v", after)
	}
	// 空提交（详情失败场景）：不写任何字段
	if err := svc.UpdateManaged(ctx, 1, 1, &ChannelAccountManagedSave{}); err != nil {
		t.Fatalf("空提交应成功但不写库: %v", err)
	}
	if a.rows[0].Name != "new-name" {
		t.Fatalf("空提交不得改变任何字段")
	}
	// 明确清空备注
	empty := ""
	if err := svc.UpdateManaged(ctx, 1, 1, &ChannelAccountManagedSave{Remark: &empty}); err != nil {
		t.Fatalf("清空备注失败: %v", err)
	}
	if a.rows[0].Remark != "" {
		t.Fatalf("明确清空必须生效")
	}
	// 详情加载失败：不得提交（仓储错误原样上抛）
	a.err = errors.New("db down")
	if err := svc.UpdateManaged(ctx, 1, 1, &ChannelAccountManagedSave{Name: &name}); err == nil {
		t.Fatalf("详情失败必须阻止提交")
	}
	a.err = nil
}

func TestChannelAccountManage_DisabledAccountRecoverable(t *testing.T) {
	ctx := context.Background()
	a, m := seedManageAccounts()
	svc := NewChannelAccountManageServiceWithRepos(a, m)
	if err := svc.SetStatus(ctx, 1, 2, 1); err != nil {
		t.Fatalf("停用账号重新启用失败: %v", err)
	}
	on := 1
	rows, total, err := svc.ListManaged(ctx, 1, dto.ChannelAccountListQuery{Status: &on})
	if err != nil || total != 3 {
		t.Fatalf("重新启用后应有 3 个启用账号，total=%d err=%v", total, err)
	}
	found := false
	for _, r := range rows {
		if r.ID == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("重新启用的账号应可找回")
	}
	// 反例：非法状态拒绝
	if err := svc.SetStatus(ctx, 1, 1, 9); err == nil {
		t.Fatalf("非法状态必须拒绝")
	}
}
