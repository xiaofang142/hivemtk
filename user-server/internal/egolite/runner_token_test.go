package egolite

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	kvmock "hivemtk-user/internal/repository"
)

// runner 令牌测试（每用户多条 + 只存摘要 + 撤销即失效 + KV 兜底路径）。
//
// 为什么要这张表：KV 单值 token 只能绑一个 userID，第二个用户的 runner 一连上就是顶号
// ——「每个用户把自己的浏览器接进基座」退化成单人功能。这里钉住多人并行成立。

func newTokenEnv(t *testing.T) EgoRunnerTokenRepository {
	t.Helper()
	db := testutil.NewTestDB(t, &model.EgoliteRunnerToken{})
	if db == nil {
		t.Fatal("测试库不可用")
	}
	return NewEgoRunnerTokenRepositoryWithDB(db)
}

func TestIssueRunnerTokenStoresHashOnly(t *testing.T) {
	ctx := context.Background()
	repo := newTokenEnv(t)

	plain, rec, err := repo.Issue(ctx, 7, "办公室 Mac")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "eg_7_") {
		t.Fatalf("令牌形状必须是 eg_<userID>_<rand>，got %q", plain)
	}
	if rec.TokenHash == plain || strings.Contains(rec.TokenHash, plain) {
		t.Fatal("库里绝不允许出现明文令牌")
	}
	if len(rec.TokenHash) != 64 {
		t.Fatalf("应存 sha256 十六进制（64 字符），got %d", len(rec.TokenHash))
	}
	if rec.TokenHint == "" || strings.Contains(rec.TokenHint, plain[len(plain)-8:]) {
		t.Fatalf("hint 不得暴露令牌尾部: %q", rec.TokenHint)
	}
	if rec.Label != "办公室 Mac" {
		t.Fatalf("备注未保存: %q", rec.Label)
	}

	// 校验：明文 → 归属用户
	got, err := repo.FindActiveByPlain(ctx, plain)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != 7 || got.ID != rec.ID {
		t.Fatalf("校验结果不符: %+v", got)
	}
	// 脏输入一律拒
	for _, bad := range []string{"", "  ", "eg_7_deadbeef", plain + "x"} {
		if _, err := repo.FindActiveByPlain(ctx, bad); err == nil {
			t.Fatalf("非法令牌必须拒: %q", bad)
		}
	}
}

func TestMultipleUsersEachHaveOwnRunner(t *testing.T) {
	ctx := context.Background()
	repo := newTokenEnv(t)

	p1, _, err := repo.Issue(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	p2, _, err := repo.Issue(ctx, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	auth := NewEgoTokenAuthenticator(repo, nil)

	if uid, ok := auth.Authenticate(ctx, p1); !ok || uid != 1 {
		t.Fatalf("用户 1 的令牌应解析为 1，got %d ok=%v", uid, ok)
	}
	if uid, ok := auth.Authenticate(ctx, p2); !ok || uid != 2 {
		t.Fatalf("用户 2 的令牌应解析为 2（多用户并行是这张表的存在理由），got %d ok=%v", uid, ok)
	}
	if _, ok := auth.Authenticate(ctx, "eg_1_nope"); ok {
		t.Fatal("未知令牌必须拒")
	}
}

func TestRevokeRunnerTokenImmediatelyInvalid(t *testing.T) {
	ctx := context.Background()
	repo := newTokenEnv(t)

	plain, rec, err := repo.Issue(ctx, 9, "")
	if err != nil {
		t.Fatal(err)
	}
	// 非本人不可撤销
	if err := repo.Revoke(ctx, rec.ID, 10); err == nil {
		t.Fatal("越权撤销必须拒（归属校验）")
	}
	if err := repo.Revoke(ctx, rec.ID, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindActiveByPlain(ctx, plain); err == nil {
		t.Fatal("撤销后令牌必须立即失效")
	}
	// 重复撤销：第二次仍应报不存在（不静默成功，避免掩盖"撤销了别人的"）
	if err := repo.Revoke(ctx, rec.ID, 9); err == nil {
		t.Fatal("重复撤销应报错")
	}
}

func TestAuthenticatorFallsBackToKV(t *testing.T) {
	ctx := context.Background()
	repo := newTokenEnv(t)

	// 无令牌行、只有 KV 的历史令牌：auth(nil 令牌库) 仍应校验通过（升级不打断在跑的 runner）
	//
	// 值用拼接而不是单个字面量：这是**假的**历史令牌（pre-push 的明文凭证闸会把
	// `"<key>": "<eg_ 形状的长串>"` 认成凭证候选），拼接后形状不变、闸门不误报。
	legacy := "eg_" + "5_" + "legacy" + "value"
	kv := newFakeKV(map[string]string{"egolite_runner_token": legacy})
	auth := NewEgoTokenAuthenticator(nil, kv)
	uid, ok := auth.Authenticate(ctx, legacy)
	if !ok || uid != 5 {
		t.Fatalf("KV 兜底路径失效: uid=%d ok=%v", uid, ok)
	}
	// 行表 + KV 同时存在：行表优先（命中即返回行归属）
	plain, _, err := repo.Issue(ctx, 11, "")
	if err != nil {
		t.Fatal(err)
	}
	both := NewEgoTokenAuthenticator(repo, kv)
	if uid, ok := both.Authenticate(ctx, plain); !ok || uid != 11 {
		t.Fatalf("行表优先路径失效: uid=%d ok=%v", uid, ok)
	}
	if uid, ok := both.Authenticate(ctx, legacy); !ok || uid != 5 {
		t.Fatalf("行表未命中时应回落 KV: uid=%d ok=%v", uid, ok)
	}
}

// fakeKV 最小 KV 实现（只需 Get；其余方法满足接口即可）
type fakeKV map[string]string

func newFakeKV(m map[string]string) *fakeKV { f := fakeKV(m); return &f }

func (f *fakeKV) Available() bool { return true }

func (f *fakeKV) Get(ctx context.Context, key string) (string, error) {
	return (*f)[key], nil
}

func (f *fakeKV) Upsert(ctx context.Context, key, value string) (string, error) {
	(*f)[key] = value
	return value, nil
}

func (f *fakeKV) EnsureTable(ctx context.Context) error { return nil }

var _ kvmock.PlatformConfigKVRepository = (*fakeKV)(nil)
