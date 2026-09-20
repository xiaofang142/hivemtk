package pagination

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func base64Encode(raw string) string {
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

type cursorRow struct {
	CreatedAt time.Time
	ID        uint64
	Name      string
}

func TestCursorQueryDryRun(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatalf("open dummy db: %v", err)
	}
	ctx := context.Background()

	t.Run("非法 order_by fail-closed", func(t *testing.T) {
		var rows []cursorRow
		if _, err := CursorQuery(ctx, db, CursorQueryOpts{
			Table: "t", OrderBy: "id; DROP TABLE t", Dest: &rows,
		}); err == nil {
			t.Fatal("注入式 order_by 应报错")
		}
	})

	t.Run("非法 cursor 报错", func(t *testing.T) {
		var rows []cursorRow
		if _, err := CursorQuery(ctx, db, CursorQueryOpts{
			Table: "t", Cursor: Cursor("!!!not-base64!!!"), Dest: &rows,
		}); err == nil {
			t.Fatal("非法 cursor 应报错")
		}
	})

	t.Run("预填充 dest 走 hasMore+截断+游标提取", func(t *testing.T) {
		t0 := time.Unix(1700, 0).UTC()
		rows := []cursorRow{
			{CreatedAt: t0.Add(3 * time.Second), ID: 3},
			{CreatedAt: t0.Add(2 * time.Second), ID: 2},
			{CreatedAt: t0.Add(1 * time.Second), ID: 1},
		}
		res, err := CursorQuery(ctx, db, CursorQueryOpts{
			Table: "t", PageSize: 2, Dest: &rows,
		})
		if err != nil {
			t.Fatalf("CursorQuery: %v", err)
		}
		if !res.HasMore {
			t.Fatal("3>2 应 HasMore")
		}
		got := *(res.Items.(*[]cursorRow))
		if len(got) != 2 {
			t.Fatalf("应截断至 2 条，得 %d", len(got))
		}
		ts, id, ok := DecodeCursor(res.NextCursor)
		if !ok || id != 2 || !ts.Equal(t0.Add(2*time.Second)) {
			t.Fatalf("NextCursor 应为末条 (t0+2s,2)，得 (%v,%d,%v)", ts, id, ok)
		}
	})

	t.Run("默认页大小不截断", func(t *testing.T) {
		rows := []cursorRow{{CreatedAt: time.Now(), ID: 1}}
		res, err := CursorQuery(ctx, db, CursorQueryOpts{Table: "t", Dest: &rows})
		if err != nil {
			t.Fatalf("CursorQuery: %v", err)
		}
		if res.HasMore || res.NextCursor != "" {
			t.Fatalf("1 条 < 默认 100 不应 HasMore: %+v", res)
		}
	})
}

func TestNextCursor(t *testing.T) {
	t0 := time.Unix(1600, 0).UTC()
	rows := []cursorRow{
		{CreatedAt: t0, ID: 10},
		{CreatedAt: t0.Add(time.Second), ID: 11},
	}
	if c := NextCursor(rows, 2); c != "" {
		ts, id, ok := DecodeCursor(c)
		if !ok || id != 11 || !ts.Equal(t0.Add(time.Second)) {
			t.Fatalf("满页游标应为 (t0+1s,11)，得 (%v,%d)", ts, id)
		}
	} else {
		t.Fatal("满页应返回游标")
	}
	if c := NextCursor(rows, 3); c != "" {
		t.Fatalf("末页应返回空游标，得 %q", c)
	}
	var empty []cursorRow
	if c := NextCursor(empty, 2); c != "" {
		t.Fatalf("空结果应返回空游标，得 %q", c)
	}
}

func TestExtractCursorFromItem(t *testing.T) {
	t0 := time.Unix(1500, 0).UTC()
	if c := extractCursorFromItem(nil); c != "" {
		t.Errorf("nil 应为空游标")
	}
	if c := extractCursorFromItem(42); c != "" {
		t.Errorf("非结构体应为空游标")
	}
	type noFields struct{ X int }
	if c := extractCursorFromItem(noFields{}); c != "" {
		t.Errorf("无 CreatedAt 应为空游标")
	}
	if c := extractCursorFromItem(cursorRow{CreatedAt: t0, ID: 7}); c == "" {
		t.Fatal("uint64 ID 应可提取")
	} else if _, id, ok := DecodeCursor(c); !ok || id != 7 {
		t.Fatalf("游标解码不符: %d %v", id, ok)
	}
	type uintID struct {
		CreatedAt time.Time
		ID        uint
	}
	if c := extractCursorFromItem(uintID{CreatedAt: t0, ID: 8}); c == "" {
		t.Fatal("uint ID 应可提取")
	} else if _, id, _ := DecodeCursor(c); id != 8 {
		t.Fatalf("uint ID 解码错误: %d", id)
	}
	type idAlias struct {
		CreatedAt time.Time
		Id        uint64
	}
	if c := extractCursorFromItem(&idAlias{CreatedAt: t0, Id: 9}); c == "" {
		t.Fatal("指针 + Id 字段应可提取")
	}
	type badTime struct {
		CreatedAt string
		ID        uint64
	}
	// 当前实现：CreatedAt 类型不符时以零时间继续编码。
	// 注：零时间经 UnixNano 往返会溢出（UnixNano 仅 1678–2262 有效），
	// 因此只断言 id 与可解码性，不断言时间等同。
	if c := extractCursorFromItem(badTime{ID: 5}); c == "" {
		t.Fatal("非 time.Time 的 CreatedAt 应按零时间编码（现状行为）")
	} else if _, id, ok := DecodeCursor(c); !ok || id != 5 {
		t.Fatalf("零时间游标解码不符: id=%d ok=%v", id, ok)
	}
	type noID struct{ CreatedAt time.Time }
	if c := extractCursorFromItem(noID{CreatedAt: t0}); c != "" {
		t.Errorf("无 ID 字段应为空游标")
	}
}

func TestReflectHelpers(t *testing.T) {
	s := []int{1, 2, 3}
	if v := sliceValue(&s); v.Kind() != reflect.Slice {
		t.Fatal("sliceValue 应解引用指针")
	}
	if got := sliceLen(s); got != 3 {
		t.Fatalf("非指针切片 sliceLen=%d", got)
	}
	if got := sliceLen(nil); got != 0 {
		t.Fatalf("nil sliceLen=%d", got)
	}
	if got := sliceLen(42); got != 0 {
		t.Fatalf("非切片 sliceLen=%d", got)
	}

	ptr := &s
	sliceTruncate(ptr, 2)
	if len(s) != 2 {
		t.Fatalf("截断后应为 2，得 %d", len(s))
	}
	sliceTruncate(&s, 10) // n >= len，no-op
	if len(s) != 2 {
		t.Fatalf("越界截断应 no-op，得 %d", len(s))
	}
	sliceTruncate(42, 1) // 非切片 no-op
	if v := sliceAt(&s, 0); v != 1 {
		t.Fatalf("sliceAt(0)=%v", v)
	}
	if v := sliceAt(&s, 5); v != nil {
		t.Fatalf("越界 sliceAt 应为 nil，得 %v", v)
	}
	if v := sliceAt(&s, -1); v != nil {
		t.Fatalf("负索引 sliceAt 应为 nil，得 %v", v)
	}
	if v := sliceAt("str", 0); v != nil {
		t.Fatalf("非切片 sliceAt 应为 nil，得 %v", v)
	}
}

func TestDecodeCursorMalformedPayloads(t *testing.T) {
	cases := []Cursor{
		Cursor(base64Encode("no-colon")),
		Cursor(base64Encode("abc:def")),
		Cursor(base64Encode("1:2:3")),
		Cursor("not base64 at all!"),
	}
	for _, c := range cases {
		if _, _, ok := DecodeCursor(c); ok {
			t.Errorf("DecodeCursor(%q) 应失败", c)
		}
	}
}
