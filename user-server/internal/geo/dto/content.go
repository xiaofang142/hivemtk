package dto

import "time"

// GenerateContentRequest 内容生成请求
type GenerateContentRequest struct {
	Keyword    string   `json:"keyword"`
	BrandName  string   `json:"brand_name"`
	Advantages []string `json:"advantages"`
	Model      string   `json:"model"`
	WordCount  int      `json:"word_count" binding:"omitempty,min=100,max=20000"`
	Style      string   `json:"style"`
	Lang       string   `json:"lang"`
}

// BatchGenerateRequest 批量生成请求
type BatchGenerateRequest struct {
	Keywords   []string `json:"keywords" binding:"required,min=1"`
	BrandName  string   `json:"brand_name"`
	Model      string   `json:"model"`
	Advantages []string `json:"advantages"`
	WordCount  int      `json:"word_count" binding:"omitempty,min=100,max=20000"`
	Style      string   `json:"style"`
	Lang       string   `json:"lang"`
}

// OptimizeContentRequest 内容优化请求
type OptimizeContentRequest struct {
	ArticleID  string   `json:"article_id"`
	Content    string   `json:"content"`
	BrandName  string   `json:"brand_name"`
	Advantages []string `json:"advantages"`
	Model      string   `json:"model"`
}

// ScoreContentRequest 内容评分请求
type ScoreContentRequest struct {
	ArticleID string `json:"article_id"`
	Content   string `json:"content"`
	BrandName string `json:"brand_name"`
	Keyword   string `json:"keyword"`
}

// ContentResponse 内容响应
type ContentResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Keyword   string    `json:"keyword"`
	Model     string    `json:"model"`
	WordCount int       `json:"word_count"`
	Status    string    `json:"status"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

// OptimizationResponse 优化响应
type OptimizationResponse struct {
	ID               string    `json:"id"`
	ArticleID        string    `json:"article_id"`
	ScoreBefore      float64   `json:"score_before"`
	ScoreAfter       float64   `json:"score_after"`
	Suggestions      string    `json:"suggestions"`
	OptimizedContent string    `json:"optimized_content"`
	CreatedAt        time.Time `json:"created_at"`
}
