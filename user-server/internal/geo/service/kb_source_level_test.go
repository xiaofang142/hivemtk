package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/geo/dto"

	"hivemtk-user/internal/pkg/utils"
)

// #499 回归：source_level 落库列是 varchar(2)，历史实现不做长度校验，
// 超长值（如 'official'）直达 PG 报错 → controller 兜成 500「保存文档失败」。
// 修复后 service 在触碰数据库前拒绝，ErrorFromDB 据 utils.ErrInvalidInput 翻 400。
// 用 nil repo：合法值域校验必须在任何 IO 之前发生，用例失败即说明校验缺失。
func TestKBService_Save_RejectsInvalidSourceLevel(t *testing.T) {
	svc := NewKBService(nil, nil)
	ctx := context.Background()

	for _, level := range []string{"official", "AB", "E", "a ", "A1"} {
		_, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
			Title:       "品牌FAQ",
			Content:     "Q:什么是GEO？A:生成式引擎优化",
			SourceLevel: level,
		})
		if err == nil {
			t.Fatalf("source_level=%q 应被拒绝", level)
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Fatalf("source_level=%q 应报 ErrInvalidInput（可映射 400），实际: %v", level, err)
		}
	}
}

// 合法值（A-D 与空）必须放行到仓储层；nil repo 下放行后会撞 nil 指针，
// 用 recover 捕获——只要 panic 值不是"校验拒绝"，就证明校验层已放行。
func TestKBService_Save_AllowsValidSourceLevel(t *testing.T) {
	svc := NewKBService(nil, nil)
	ctx := context.Background()

	for _, level := range []string{"", "A", "B", "C", "D"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// nil repo 的 nil 指针 panic = 已越过校验抵达仓储 = 放行成功。
					return
				}
			}()
			_, err := svc.Save(ctx, &dto.SaveKnowledgeDocumentRequest{
				Title:       "品牌FAQ",
				Content:     "Q:什么是GEO？A:生成式引擎优化",
				SourceLevel: level,
			})
			if errors.Is(err, utils.ErrInvalidInput) {
				t.Errorf("source_level=%q 是合法值，不应被校验拒绝: %v", level, err)
			}
		}()
	}
}
