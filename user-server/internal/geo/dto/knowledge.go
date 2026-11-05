package dto

import "time"

// SaveKnowledgeDocumentRequest 保存知识库文档请求
type SaveKnowledgeDocumentRequest struct {
	ID          string            `json:"id"`
	Title       string            `json:"title" binding:"required"`
	Content     string            `json:"content" binding:"required"`
	DocType     string            `json:"doc_type"`
	SourceLevel string            `json:"source_level"`
	SourceURL   string            `json:"source_url"`
	Metadata    map[string]string `json:"metadata"`
}

// KnowledgeDocumentResponse 知识库文档响应
type KnowledgeDocumentResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	DocType   string    `json:"doc_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KBSearchResult 知识库检索结果（含相关性得分）
type KBSearchResult struct {
	KnowledgeDocumentResponse
	Score float64 `json:"score"`
}

// KBAskRequest 知识库问答请求
type KBAskRequest struct {
	Question string `json:"question" binding:"required"`
}

// KBAskResponse 知识库问答响应
type KBAskResponse struct {
	Answer   string   `json:"answer"`
	Sources  []string `json:"sources"`
	Provider string   `json:"provider,omitempty"`
	Model    string   `json:"model,omitempty"`
}
