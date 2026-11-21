package service

// knowledge_service_parents.go 父子分段的父块构建（P1-1 前置小刀）。
//
// 模型：连续 N 个子块合成一个父块（窗口不重叠、按子块顺序切）；
// 子块记 parent_chunk_id，检索命中子块后由检索侧回拼父块进 prompt。
//
// 行为闸（AC「未启用时与现状逐字节一致」）：
//   - 单块文档不建父块（parent_chunk_id 全 0）——短文档本来就没有上下文可扩；
//   - parentRepo 未装配、或所有窗口都超字符上限时同样返回全 0，调用方行为不变。

import (
	"context"
	"strings"
	"unicode/utf8"

	"hivemtk-user/internal/aiagent/knowledge/model"
)

var (
	// parentWindowSize 一个父块覆盖的子块数。3 是「够成一段上下文、又不至于
	// 把长文的首段与第三段硬拼」的折中；改它要同步复核检索侧 parentMaxChars。
	parentWindowSize = 3
	// parentMaxChars 父块字符上限：超过的窗口不建父块，子块保持自身内容。
	// 用字符而非 token 作判据：token 估算在两处各有实现，字符数无歧义。
	//
	// 取 1800 = etl 默认 ChunkSize(512 字符) × 窗口 3 + 分隔符余量。**低于**这个值会让
	// 绝大多数窗口在默认配置下就被判超限（父块静默不建，功能等于没开）；
	// 改 ChunkSize 或 parentWindowSize 时要同步这里，检索侧 parentStitchMaxChars 同口径。
	parentMaxChars = 1800
)

// createChunkParents 按窗口构建父块并落库，返回与子块等长的父块 ID 表（0=无父块）。
// contentAt 让调用方按索引取内容与 token 估算，避免把 rag_core.Chunk 泄漏进来。
func (s *KnowledgeService) createChunkParents(
	ctx context.Context,
	documentID uint64,
	owner string,
	productID string,
	n int,
	contentAt func(i int) (content string, tokens int),
) ([]uint64, error) {
	ids := make([]uint64, n)
	if n <= 1 || s.parentRepo == nil {
		return ids, nil
	}

	// childToParent[i] = 子块 i 所属父块在 parents 切片中的下标；-1 = 该窗口未建父块。
	// 用逐子块映射而不是「i / windowSize」：中间窗口可能因超上限被跳过，
	// 那之后的下标会整体前移，整除式会算出错位的父块。
	childToParent := make([]int, n)
	for i := range childToParent {
		childToParent[i] = -1
	}

	parents := make([]model.KnowledgeChunkParent, 0, (n+parentWindowSize-1)/parentWindowSize)
	for start := 0; start < n; start += parentWindowSize {
		end := start + parentWindowSize
		if end > n {
			end = n
		}
		var b strings.Builder
		tokens := 0
		chars := 0
		for i := start; i < end; i++ {
			content, t := contentAt(i)
			if b.Len() > 0 {
				b.WriteString("\n")
				chars++ // 分隔符也占一个字符
			}
			b.WriteString(content)
			chars += utf8.RuneCountInString(content)
			tokens += t
		}
		// 按**字符**判上限：strings.Builder.Len() 是字节数，中文一字三字节，
		// 用字节判会把正常窗口误判成超限（父块静默不建，功能等于没开）。
		if chars > parentMaxChars {
			continue // 超上限窗口：不建父块，子块保持自身内容
		}
		idx := len(parents)
		parents = append(parents, model.KnowledgeChunkParent{
			DocumentID: documentID,
			ProductID:  productID,
			Content:    b.String(),
			ChildCount: end - start,
			TokenCount: tokens,
			CharCount:  chars,
		})
		for i := start; i < end; i++ {
			childToParent[i] = idx
		}
	}
	if len(parents) == 0 {
		return ids, nil
	}
	if err := s.parentRepo.BatchCreateForOwner(ctx, documentID, owner, parents); err != nil {
		return nil, err
	}
	for i := 0; i < n; i++ {
		if w := childToParent[i]; w >= 0 && w < len(parents) {
			ids[i] = parents[w].ID
		}
	}
	return ids, nil
}
