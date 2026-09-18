package etl

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	rag_core "hivemtk-user/internal/aiagent/rag/core"
)

// DocumentProcessor 文档处理器
type DocumentProcessor struct {
	config *ProcessingConfig
}

// ProcessingConfig 处理配置
type ProcessingConfig struct {
	ChunkSize           int
	ChunkOverlap        int
	MinChunkSize        int
	SimilarityThreshold float64
	MaxLengthPerChunk   int
	MinLengthPerChunk   int
}

// NewDocumentProcessor 创建新的文档处理器
func NewDocumentProcessor(config *ProcessingConfig) *DocumentProcessor {
	if config == nil {
		config = &ProcessingConfig{
			ChunkSize:           512,
			ChunkOverlap:        50,
			MinChunkSize:        100,
			SimilarityThreshold: 0.8,
			MaxLengthPerChunk:   1000,
			MinLengthPerChunk:   50,
		}
	}
	return &DocumentProcessor{config: config}
}

// ProcessDocument 处理单个文档
func (dp *DocumentProcessor) ProcessDocument(ctx context.Context, doc rag_core.Document) ([]rag_core.Chunk, error) {
	text := doc.Content

	text = dp.preprocessText(text)

	var chunks []rag_core.Chunk
	switch {
	case dp.shouldUseHeadingSplit(text):
		chunks = dp.splitByHeadings(text, doc)
	case dp.shouldUseSentenceSplit(text):
		chunks = dp.splitBySentences(text, doc)
	default:
		chunks = dp.splitByFixedLength(text, doc)
	}

	chunks = dp.postProcessChunks(chunks)

	return chunks, nil
}

func (dp *DocumentProcessor) preprocessText(text string) string {
	text = regexp.MustCompile(`[ \t]+`).ReplaceAllString(text, " ")
	text = regexp.MustCompile(`\n[ \t]*\n`).ReplaceAllString(text, "\n\n")
	text = strings.TrimSpace(text)
	return text
}

func (dp *DocumentProcessor) shouldUseHeadingSplit(text string) bool {
	headings := regexp.MustCompile(`(?m)^#+\s.*$`).FindAllString(text, -1)
	return len(headings) > 2
}

func (dp *DocumentProcessor) shouldUseSentenceSplit(text string) bool {
	sentences := regexp.MustCompile(`[.!?。！？]\s+`).FindAllString(text, -1)
	return len(sentences) > 5
}

func (dp *DocumentProcessor) splitByHeadings(text string, doc rag_core.Document) []rag_core.Chunk {
	re := regexp.MustCompile(`(?m)^#+\s.*$`)
	matches := re.FindAllStringIndex(text, -1)

	if len(matches) == 0 {
		return dp.splitByFixedLength(text, doc)
	}

	var chunks []rag_core.Chunk
	start := 0

	for i, match := range matches {
		if i > 0 {
			chunkText := strings.TrimSpace(text[start:match[0]])
			if len(chunkText) >= dp.config.MinLengthPerChunk {
				chunk := dp.createChunk(doc, chunkText, fmt.Sprintf("%s_heading_%d", doc.ID, i-1))
				chunks = append(chunks, chunk)
			}
		}
		start = match[0]
	}

	if start < len(text) {
		chunkText := strings.TrimSpace(text[start:])
		if len(chunkText) >= dp.config.MinLengthPerChunk {
			chunk := dp.createChunk(doc, chunkText, fmt.Sprintf("%s_heading_%d", doc.ID, len(matches)))
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}

func (dp *DocumentProcessor) splitBySentences(text string, doc rag_core.Document) []rag_core.Chunk {
	// RE2 不支持 (?= 前瞻；"标点+换行"场景已由 \s+ 覆盖
	sentencePattern := `[.!?。！？]\s+|\n\s*\n`
	re := regexp.MustCompile(sentencePattern)
	sentences := re.Split(text, -1)

	var chunks []rag_core.Chunk
	currentChunk := ""

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		testChunk := currentChunk + " " + sentence
		if utf8.RuneCountInString(testChunk) > dp.config.MaxLengthPerChunk && currentChunk != "" {
			if utf8.RuneCountInString(currentChunk) >= dp.config.MinLengthPerChunk {
				chunk := dp.createChunk(doc, strings.TrimSpace(currentChunk), fmt.Sprintf("%s_sentence_%d", doc.ID, len(chunks)+1))
				chunks = append(chunks, chunk)
			}
			currentChunk = sentence
		} else {
			if currentChunk == "" {
				currentChunk = sentence
			} else {
				currentChunk = testChunk
			}
		}
	}

	if currentChunk != "" && utf8.RuneCountInString(currentChunk) >= dp.config.MinLengthPerChunk {
		chunk := dp.createChunk(doc, strings.TrimSpace(currentChunk), fmt.Sprintf("%s_sentence_%d", doc.ID, len(chunks)+1))
		chunks = append(chunks, chunk)
	}

	return chunks
}

func (dp *DocumentProcessor) splitByFixedLength(text string, doc rag_core.Document) []rag_core.Chunk {
	var chunks []rag_core.Chunk

	runes := []rune(text)
	totalRunes := len(runes)

	if totalRunes <= dp.config.ChunkSize {
		chunk := dp.createChunk(doc, text, fmt.Sprintf("%s_single", doc.ID))
		return append(chunks, chunk)
	}

	start := 0
	for start < totalRunes {
		end := start + dp.config.ChunkSize

		if end > totalRunes {
			end = totalRunes
		}

		actualEnd := dp.findBestCutPoint(runes, start, end)
		if actualEnd <= start {
			actualEnd = end
		}

		chunkText := string(runes[start:actualEnd])
		chunk := dp.createChunk(doc, chunkText, fmt.Sprintf("%s_fixed_%d", doc.ID, len(chunks)+1))
		chunks = append(chunks, chunk)

		prevStart := start
		start = actualEnd - dp.config.ChunkOverlap
		if start <= prevStart {
			start = actualEnd
		}
		if start <= prevStart {
			start = prevStart + 1
		}
	}

	return chunks
}

func (dp *DocumentProcessor) findBestCutPoint(runes []rune, start, suggestedEnd int) int {
	totalRunes := len(runes)
	if suggestedEnd >= totalRunes {
		return totalRunes
	}

	sentenceBoundaries := []rune{'.', '!', '?', '。', '！', '？'}
	for i := suggestedEnd; i > start; i-- {
		if runes[i-1] == '\n' || runes[i-1] == '\t' {
			return i
		}
		for _, boundary := range sentenceBoundaries {
			if runes[i-1] == boundary {
				return i
			}
		}
	}

	for i := suggestedEnd; i > start; i-- {
		if runes[i-1] == ' ' {
			return i
		}
	}

	return suggestedEnd
}

func (dp *DocumentProcessor) createChunk(doc rag_core.Document, content, chunkID string) rag_core.Chunk {
	return rag_core.Chunk{
		ID:         chunkID,
		DocumentID: doc.ID,
		Content:    content,
		Metadata:   doc.Metadata,
		TokenCount: len(strings.Fields(content)),
	}
}

func (dp *DocumentProcessor) postProcessChunks(chunks []rag_core.Chunk) []rag_core.Chunk {

	var filteredChunks []rag_core.Chunk
	for _, chunk := range chunks {
		if len(strings.TrimSpace(chunk.Content)) >= dp.config.MinChunkSize {
			filteredChunks = append(filteredChunks, chunk)
		}
	}

	if len(filteredChunks) == 0 && len(chunks) > 0 {
		best := chunks[0]
		for _, c := range chunks[1:] {
			if len(c.Content) > len(best.Content) {
				best = c
			}
		}
		filteredChunks = append(filteredChunks, best)
	}

	return filteredChunks
}

// BatchProcessDocuments 批量处理文档
func (dp *DocumentProcessor) BatchProcessDocuments(ctx context.Context, docs []rag_core.Document) ([]rag_core.Chunk, error) {
	var allChunks []rag_core.Chunk

	for _, doc := range docs {
		chunks, err := dp.ProcessDocument(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("failed to process document %s: %w", doc.ID, err)
		}
		allChunks = append(allChunks, chunks...)
	}

	return allChunks, nil
}

// UpdateConfig 更新处理配置
func (dp *DocumentProcessor) UpdateConfig(config *ProcessingConfig) error {
	if config.ChunkSize <= 0 {
		return fmt.Errorf("chunk size must be positive")
	}
	if config.ChunkOverlap >= config.ChunkSize {
		return fmt.Errorf("chunk overlap must be less than chunk size")
	}
	if config.MinChunkSize > config.ChunkSize {
		return fmt.Errorf("min chunk size must be less than or equal to chunk size")
	}
	if config.SimilarityThreshold < 0 || config.SimilarityThreshold > 1 {
		return fmt.Errorf("similarity threshold must be between 0 and 1")
	}

	dp.config = config
	return nil
}

// GetConfig 获取当前配置
func (dp *DocumentProcessor) GetConfig() *ProcessingConfig {
	return dp.config
}
