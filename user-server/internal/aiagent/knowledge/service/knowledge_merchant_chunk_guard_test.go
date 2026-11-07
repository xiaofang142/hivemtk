package service

import (
	"context"
	"strings"
	"testing"
)

// D-31 回归：拆分分段传全空白内容时，必须在触碰数据库之前就拒绝。
//
// 原实现先删原分段、再在插入循环里 continue 跳过空白段：传 ["  ", ""] 时
// 原分段被删掉、新分段一个都没插，接口却回"拆分成功"，文档就此静默丢光分段。
// 这里用零值 service（chunkRepo 为 nil）断言"拒绝发生在删除之前"——
// 只要代码走到仓储就会 panic，测试立刻失败。
func TestSplitChunk_RejectsBlankPartsBeforeDelete(t *testing.T) {
	svc := &KnowledgeMerchantService{}
	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{name: "全空白", parts: []string{"   ", ""}, want: "拆分内容不能为空"},
		{name: "混合空白", parts: []string{"第一段", "  "}, want: "拆分内容不能为空"},
		{name: "段数不足", parts: []string{"只有一段"}, want: "至少需要 2 段"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.SplitChunk(context.Background(), &SplitChunkRequest{ChunkID: 1, Parts: tc.parts})
			if err == nil {
				t.Fatal("应拒绝该拆分请求，实际返回成功")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误文案应包含 %q，实际 %q", tc.want, err.Error())
			}
		})
	}
}
