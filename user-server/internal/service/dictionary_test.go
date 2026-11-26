package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

// dictRepoStub 内存态仓储桩，用来验证「DB 没有这一批就回落种子」这条口径——
// 真库版本的同一断言在 dictionary_db_test.go 里。
type dictRepoStub struct {
	items     map[string][]model.Dictionary // parent -> items
	trans     map[string][]model.DictionaryTransition
	parents   []string
	upsertErr error
}

func newDictRepoStub() *dictRepoStub {
	return &dictRepoStub{
		items: map[string][]model.Dictionary{},
		trans: map[string][]model.DictionaryTransition{},
	}
}

func (s *dictRepoStub) ListByParent(_ context.Context, parent string) ([]model.Dictionary, error) {
	return s.items[parent], nil
}
func (s *dictRepoStub) ListParents(_ context.Context) ([]string, error) { return s.parents, nil }
func (s *dictRepoStub) GetByCode(_ context.Context, parent, code string) (*model.Dictionary, error) {
	for _, d := range s.items[parent] {
		if d.Code == code {
			cp := d
			return &cp, nil
		}
	}
	return nil, nil
}
func (s *dictRepoStub) Upsert(_ context.Context, d *model.Dictionary, _ uint) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	for i, ex := range s.items[d.ParentCode] {
		if ex.Code == d.Code {
			s.items[d.ParentCode][i] = *d
			return nil
		}
	}
	s.items[d.ParentCode] = append(s.items[d.ParentCode], *d)
	return nil
}
func (s *dictRepoStub) ListTransitions(_ context.Context, parent string) ([]model.DictionaryTransition, error) {
	return s.trans[parent], nil
}
func (s *dictRepoStub) ReplaceTransitions(_ context.Context, parent string, trans []model.DictionaryTransition, _ uint) error {
	s.trans[parent] = trans
	return nil
}

// TestGetDictionaryFallsBackToSeed DB 没有这一批时回落 Go 种子。
// 这条口径不能反：字典描述的是**存量行正在用的值域**，反向会让管理端改了不生效。
func TestGetDictionaryFallsBackToSeed(t *testing.T) {
	svc := NewDictionaryServiceWithRepo(newDictRepoStub())
	items, trans, err := svc.GetDictionary(context.Background(), "approval_status")
	if err != nil {
		t.Fatalf("GetDictionary err=%v", err)
	}
	if len(items) != 4 {
		t.Fatalf("approval_status 应有 4 项种子, got %d", len(items))
	}
	if len(trans) != 3 {
		t.Errorf("approval_status 应有 3 条跃迁种子, got %d", len(trans))
	}
	// 种子项必须是锁定态：管理端不能改名或停用（代码是权威的那一侧）。
	for _, d := range items {
		if !d.SystemLocked {
			t.Errorf("种子项 %s 应 SystemLocked", d.Code)
		}
	}
}

// TestGetDictionaryUnknownParent 未知域返回 ErrDictionaryParentUnknown
// （不是空列表——空列表会让管理端以为"这个域就是空的"）。
func TestGetDictionaryUnknownParent(t *testing.T) {
	svc := NewDictionaryServiceWithRepo(newDictRepoStub())
	_, _, err := svc.GetDictionary(context.Background(), "no_such_domain")
	if err != ErrDictionaryParentUnknown {
		t.Errorf("want ErrDictionaryParentUnknown, got %v", err)
	}
}

// TestGetDictionaryPrefersDB DB 有这一批时以 DB 为准，种子不再下发。
func TestGetDictionaryPrefersDB(t *testing.T) {
	stub := newDictRepoStub()
	stub.items["approval_status"] = []model.Dictionary{
		{ParentCode: "approval_status", Code: "pending", Name: "待审批(改过名)"},
		{ParentCode: "approval_status", Code: "custom", Name: "自定义状态", Enabled: true},
	}
	stub.trans["approval_status"] = []model.DictionaryTransition{
		{ParentCode: "approval_status", FromCode: "pending", ToCode: "custom"},
	}
	svc := NewDictionaryServiceWithRepo(stub)
	items, trans, err := svc.GetDictionary(context.Background(), "approval_status")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(items) != 2 || items[0].Name != "待审批(改过名)" {
		t.Errorf("应以 DB 为准: %+v", items)
	}
	if len(trans) != 1 || trans[0].ToCode != "custom" {
		t.Errorf("跃迁应以 DB 为准: %+v", trans)
	}
}

// TestUpsertItemRejectsSystemLocked 种子锁定项不能改名/停用。
func TestUpsertItemRejectsSystemLocked(t *testing.T) {
	stub := newDictRepoStub()
	stub.items["approval_status"] = []model.Dictionary{
		{ParentCode: "approval_status", Code: "pending", Name: "待审批", SystemLocked: true},
	}
	svc := NewDictionaryServiceWithRepo(stub)
	err := svc.UpsertItem(context.Background(), &model.Dictionary{
		ParentCode: "approval_status", Code: "pending", Name: "改成别的名字",
	}, 1)
	if err != ErrDictionarySystemLocked {
		t.Errorf("want ErrDictionarySystemLocked, got %v", err)
	}
}

// TestUpsertItemAllowsNewAndUnlocked 非锁定项可建可改。
func TestUpsertItemAllowsNewAndUnlocked(t *testing.T) {
	stub := newDictRepoStub()
	svc := NewDictionaryServiceWithRepo(stub)
	if err := svc.UpsertItem(context.Background(), &model.Dictionary{
		ParentCode: "x", Code: "a", Name: "A",
	}, 1); err != nil {
		t.Fatalf("新建失败: %v", err)
	}
	if err := svc.UpsertItem(context.Background(), &model.Dictionary{
		ParentCode: "x", Code: "a", Name: "A2",
	}, 1); err != nil {
		t.Fatalf("改建失败: %v", err)
	}
	if stub.items["x"][0].Name != "A2" {
		t.Errorf("改建未生效: %+v", stub.items["x"])
	}
}

// TestUpsertItemRejectsEmptyKeys code/parent 为空必须拒。
func TestUpsertItemRejectsEmptyKeys(t *testing.T) {
	svc := NewDictionaryServiceWithRepo(newDictRepoStub())
	for _, c := range []struct{ parent, code string }{
		{"", ""}, {"x", ""}, {"", "a"},
	} {
		err := svc.UpsertItem(context.Background(), &model.Dictionary{
			ParentCode: c.parent, Code: c.code, Name: "n",
		}, 1)
		if err == nil {
			t.Errorf("parent=%q code=%q 应被拒", c.parent, c.code)
		}
	}
}

// TestListParentsMergesDBAndSeed 目录是 DB 与种子的并集（去重排序）：
// 只列 DB 会让未落库的种子域在管理端看不见，只列种子会让管理端新建的域不出现。
func TestListParentsMergesDBAndSeed(t *testing.T) {
	stub := newDictRepoStub()
	stub.parents = []string{"bad_case_status", "custom_domain"}
	svc := NewDictionaryServiceWithRepo(stub)
	got, err := svc.ListParents(context.Background())
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	want := []string{"approval_status", "bad_case_status", "crm_business_type", "crm_industry",
		"crm_requirement_category", "custom_domain", "human_task_status", "opportunity_stage", "payment_status"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i].Code != want[i] {
			t.Errorf("[%d] want %s, got %s", i, want[i], got[i].Code)
		}
	}
}

// TestDictionarySeedsMirrorModelConstants 种子的 code 必须与 model 侧常量逐字对应。
// 这条是防漂移的主锚：种子描述存量行的值域，与代码里的常量分叉就意味着
// 「前端按种子翻译、后端按常量判定」，两边对同一行的结论会不一致。
func TestDictionarySeedsMirrorModelConstants(t *testing.T) {
	items, _, ok := lookupDictionarySeed("approval_status")
	if !ok {
		t.Fatal("approval_status 种子缺失")
	}
	wantCodes := map[string]bool{
		"pending": false, "approved": false, "rejected": false, "expired": false,
	}
	for _, d := range items {
		if _, tracked := wantCodes[d.Code]; tracked {
			wantCodes[d.Code] = true
		}
	}
	for code, seen := range wantCodes {
		if !seen {
			t.Errorf("approval_status 种子缺 %s（与 model.ApprovalStatuses 不一致）", code)
		}
	}
	if len(items) != len(wantCodes) {
		t.Errorf("approval_status 应恰好 %d 项, got %d", len(wantCodes), len(items))
	}
}

// TestDictionarySeedParentCodesAreConsistent 每个种子项的 parent_code 必须已回填
// （registerDictionarySeed 负责回填；漏了会让仓储按空 parent 查）。
func TestDictionarySeedParentCodesAreConsistent(t *testing.T) {
	for parent, seed := range dictionarySeedIndex {
		for i, d := range seed.items {
			if d.ParentCode != parent {
				t.Errorf("%s.items[%d].ParentCode=%q，应为 %q", parent, i, d.ParentCode, parent)
			}
		}
		for i, tr := range seed.trans {
			if tr.ParentCode != parent {
				t.Errorf("%s.trans[%d].ParentCode=%q，应为 %q", parent, i, tr.ParentCode, parent)
			}
		}
	}
}

// TestDictionarySeedCodesUniqueWithinParent 同一域内 code 不能重复
// （复合唯一 (parent_code, code) 的种子侧预检，落库时报的唯一索引错更难读）。
func TestDictionarySeedCodesUniqueWithinParent(t *testing.T) {
	for parent, seed := range dictionarySeedIndex {
		seen := map[string]bool{}
		for _, d := range seed.items {
			if seen[d.Code] {
				t.Errorf("%s 域内 code %q 重复", parent, d.Code)
			}
			seen[d.Code] = true
		}
		tseen := map[string]bool{}
		for _, tr := range seed.trans {
			k := tr.FromCode + "->" + tr.ToCode
			if tseen[k] {
				t.Errorf("%s 域内跃迁 %q 重复", parent, k)
			}
			tseen[k] = true
		}
	}
}

// TestDictionarySeedTransitionsReferenceKnownCodes 跃迁两端必须在项里存在，
// 否则前端画状态机会出现悬空箭头。
func TestDictionarySeedTransitionsReferenceKnownCodes(t *testing.T) {
	for parent, seed := range dictionarySeedIndex {
		known := map[string]bool{}
		for _, d := range seed.items {
			known[d.Code] = true
		}
		for _, tr := range seed.trans {
			if !known[tr.FromCode] {
				t.Errorf("%s: 跃迁起点 %q 不在项里", parent, tr.FromCode)
			}
			if !known[tr.ToCode] {
				t.Errorf("%s: 跃迁终点 %q 不在项里", parent, tr.ToCode)
			}
		}
	}
}
