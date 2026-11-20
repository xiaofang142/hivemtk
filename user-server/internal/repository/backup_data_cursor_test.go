package repository

import "testing"

func TestBackupCursorAcceptsStringAndIntegerIDs(t *testing.T) {
	// DumpTable 的白名单同时包含字符串主键和整数主键，游标必须保留数据库返回的实际类型。
	tests := []struct {
		name string
		rows []map[string]any
		want any
	}{
		{name: "string id", rows: []map[string]any{{"id": "customer-1000"}}, want: "customer-1000"},
		{name: "integer id", rows: []map[string]any{{"id": int64(1000)}}, want: int64(1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := backupCursor(tt.rows)
			if !ok {
				t.Fatal("有效的 id 应生成下一页游标")
			}
			if got != tt.want {
				t.Fatalf("游标 = %#v，期望 %#v", got, tt.want)
			}
		})
	}
}

func TestBackupCursorRejectsEmptyID(t *testing.T) {
	// 分页批次的最后一行不能使用空主键继续查询，否则游标契约不成立。
	if got, ok := backupCursor([]map[string]any{{"id": ""}}); ok || got != nil {
		t.Fatalf("空 id 不应生成游标：got=%#v, ok=%v", got, ok)
	}
}
