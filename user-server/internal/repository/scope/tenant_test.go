package scope

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

type tenantRow struct {
	ID      uint
	OwnerID uint
}

func (tenantRow) TableName() string { return "tenant_rows" }

func newDryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("打开 dry-run gorm 失败: %v", err)
	}
	return db
}

// apply 执行一次 dry-run 查询，返回生成的 WHERE 子句与绑定变量。
func apply(t *testing.T, sc func(db *gorm.DB) *gorm.DB) (string, []any) {
	t.Helper()
	db := newDryDB(t)
	var rows []tenantRow
	tx := db.Scopes(sc).Find(&rows)
	if tx.Error != nil {
		t.Fatalf("dry-run 查询失败: %v", tx.Error)
	}
	sql := tx.Statement.SQL.String()
	i := strings.Index(sql, "WHERE")
	if i < 0 {
		return "", tx.Statement.Vars
	}
	return sql[i:], tx.Statement.Vars
}

func TestTenantScope_AdminUnfiltered(t *testing.T) {
	ctx := WithRole(WithUID(context.Background(), 7), "admin")
	where, _ := apply(t, TenantScope(ctx))
	if where != "" {
		t.Fatalf("admin 不应附加租户过滤, got %q", where)
	}
}

func TestTenantScope_NormalUser(t *testing.T) {
	ctx := WithUID(context.Background(), 42)
	where, vars := apply(t, TenantScope(ctx))
	if !strings.Contains(where, "owner_id") || !strings.Contains(where, "OR") {
		t.Fatalf("普通用户应含 owner_id OR 兜底, got %q", where)
	}
	if len(vars) == 0 || vars[0] != uint(42) {
		t.Fatalf("绑定变量应为 uid=42, got %v", vars)
	}
}

func TestTenantScope_AnonymousSeesSystemOnly(t *testing.T) {
	where, vars := apply(t, TenantScope(context.Background()))
	if !strings.Contains(where, "owner_id") || len(vars) != 1 || vars[0] != 0 {
		t.Fatalf("匿名应仅得 owner_id=0 系统数据, got %q vars=%v", where, vars)
	}
}

func TestStrictTenantScope_Matrix(t *testing.T) {
	adminCtx := WithRole(context.Background(), "admin")
	if w, _ := apply(t, StrictTenantScope(adminCtx)); w != "" {
		t.Fatalf("strict: admin 不应过滤, got %q", w)
	}
	w, vars := apply(t, StrictTenantScope(WithUID(context.Background(), 9)))
	if !strings.Contains(w, "owner_id") || len(vars) != 1 || vars[0] != uint(9) {
		t.Fatalf("strict: 普通用户应只见自己, got %q vars=%v", w, vars)
	}
	if strings.Contains(w, "OR") {
		t.Fatalf("strict: 不应出现 owner_id=0 系统兜底, got %q", w)
	}
	w2, _ := apply(t, StrictTenantScope(context.Background()))
	if !strings.Contains(w2, "1 = 0") {
		t.Fatalf("strict: 匿名应得空集条件 1 = 0, got %q", w2)
	}
}

func TestOwnedBy_EqualsStrict(t *testing.T) {
	ctx := WithUID(context.Background(), 3)
	w1, v1 := apply(t, OwnedBy(ctx))
	w2, v2 := apply(t, StrictTenantScope(ctx))
	if w1 != w2 || len(v1) != len(v2) {
		t.Fatalf("OwnedBy 应与 StrictTenantScope 等价: %q/%v vs %q/%v", w1, v1, w2, v2)
	}
}

func TestNilDB_Passthrough(t *testing.T) {
	if got := TenantScope(context.Background())(nil); got != nil {
		t.Fatal("TenantScope(nil db) 应原样返回 nil")
	}
	if got := StrictTenantScope(context.Background())(nil); got != nil {
		t.Fatal("StrictTenantScope(nil db) 应原样返回 nil")
	}
}

func TestCurrentUID_TypeMatrix(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want uint
	}{
		{"uint", uint(5), 5},
		{"uint64", uint64(6), 6},
		{"int", 7, 7},
		{"int 负数按 0", -3, 0},
		{"int64", int64(8), 8},
		{"int64 负数按 0", int64(-1), 0},
		{"非数值类型按 0", "123", 0},
		{"缺键按 0", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if c.val != nil {
				ctx = context.WithValue(ctx, ginCtxKey(ginUserKey), c.val)
			}
			if got := currentUID(ctx); got != c.want {
				t.Fatalf("currentUID = %d, want %d", got, c.want)
			}
		})
	}
}

func TestFromGinContext_DualKeyCompat(t *testing.T) {
	typed := context.WithValue(context.Background(), ginCtxKey(ginUserKey), uint(1))
	if v, ok := fromGinContext(typed, ginUserKey); !ok || v != uint(1) {
		t.Fatal("类型化键应可读出")
	}
	plain := context.WithValue(context.Background(), ginUserKey, uint(2))
	if v, ok := fromGinContext(plain, ginUserKey); !ok || v != uint(2) {
		t.Fatal("历史裸字符串键应兼容读出")
	}
	if _, ok := fromGinContext(context.Background(), ginUserKey); ok {
		t.Fatal("空 ctx 不应命中")
	}
	if _, ok := fromGinContext(nil, ginUserKey); ok { //nolint:staticcheck // 显式测试 nil ctx 防御
		t.Fatal("nil ctx 应安全返回未命中")
	}
}

// fakeGin 模拟 gin.Context.Get 的最小接口实现。
type fakeGin struct {
	vals map[string]any
}

func (f *fakeGin) Get(key string) (any, bool) {
	v, ok := f.vals[key]
	return v, ok
}

func TestSetGinValuesToCtx(t *testing.T) {
	g := &fakeGin{vals: map[string]any{"user_id": uint(11), "role": "member"}}
	ctx := SetGinValuesToCtx(context.Background(), g)
	if currentUID(ctx) != 11 {
		t.Fatalf("注入后 uid 应为 11, got %d", currentUID(ctx))
	}
	if !isAdmin(WithRole(ctx, "admin")) {
		t.Fatal("admin 标记应生效")
	}
}

func TestSetGinValuesToCtx_NoOverwriteExisting(t *testing.T) {
	g := &fakeGin{vals: map[string]any{"user_id": uint(99), "role": "admin"}}
	base := WithRole(WithUID(context.Background(), 5), "member")
	ctx := SetGinValuesToCtx(base, g)
	if currentUID(ctx) != 5 {
		t.Fatalf("已有 uid 不应被覆盖, got %d", currentUID(ctx))
	}
	if isAdmin(ctx) {
		t.Fatal("已有 role=member 不应被 gin 值覆盖为 admin")
	}
}

func TestSetGinValuesToCtx_NilSafety(t *testing.T) {
	if ctx := SetGinValuesToCtx(nil, &fakeGin{}); ctx != nil {
		t.Fatal("nil ctx 应原样返回")
	}
	base := context.Background()
	if ctx := SetGinValuesToCtx(base, nil); ctx != base {
		t.Fatal("nil gin 应原样返回 ctx")
	}
}

func TestIsAdmin_StringRoleOnly(t *testing.T) {
	if isAdmin(WithRole(context.Background(), "admin")) != true {
		t.Fatal("role=admin 应识别")
	}
	if isAdmin(WithRole(context.Background(), "ADMIN")) {
		t.Fatal("大小写敏感：ADMIN 不应视为 admin（防误提权语义漂移）")
	}
	nonStr := context.WithValue(context.Background(), ginCtxKey(roleAdminKey), 123)
	if isAdmin(nonStr) {
		t.Fatal("非字符串 role 不应视为 admin")
	}
}

func TestWithUIDRole_NilCtxGuard(t *testing.T) {
	if WithUID(nil, 1) != nil { //nolint:staticcheck // 防御路径
		t.Fatal("WithUID(nil) 应返回 nil")
	}
	if WithRole(nil, "admin") != nil { //nolint:staticcheck // 防御路径
		t.Fatal("WithRole(nil) 应返回 nil")
	}
}

func TestGinCtxKey_String(t *testing.T) {
	if got := ginCtxKey("user_id").String(); got != "scope:user_id" {
		t.Fatalf("ginCtxKey.String = %q", got)
	}
}
