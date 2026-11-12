package ragretrieval

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func requireLocalEmbedding(t *testing.T) {
	t.Helper()
	// 2026-11-12 §3.13：embedding 连接配置已迁移至参数中心 embedding.global_override，
	// EMBEDDING_ALLOW_FALLBACK env 已移除；依赖真实 embedding 的测试默认跳过。
	t.Skip("集成测试默认跳过：embedding 连接配置已迁移至参数中心 embedding.global_override")
}

// TestVectorizer_NewVectorizer tests the constructor
func TestVectorizer_NewVectorizer(t *testing.T) {
	v := NewVectorizer(128, nil)
	assert.NotNil(t, v)
	assert.Equal(t, 128, v.dimension)

	vDefault := NewVectorizer(0, nil)
	assert.NotNil(t, vDefault)
	assert.Equal(t, 1024, vDefault.dimension)

	vNegative := NewVectorizer(-100, nil)
	assert.NotNil(t, vNegative)
	assert.Equal(t, 1024, vNegative.dimension)
}

// TestVectorizer_EmbedText tests text embedding
func TestVectorizer_EmbedText(t *testing.T) {
	requireLocalEmbedding(t)
	v := NewVectorizer(128, nil)

	tests := []struct {
		name        string
		text        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty_text",
			text:        "",
			expectError: true,
			errorMsg:    "text cannot be empty",
		},
		{
			name:        "short_text",
			text:        "hello",
			expectError: false,
		},
		{
			name:        "long_text",
			text:        "This is a longer text that should produce a different embedding than a short text.",
			expectError: false,
		},
		{
			name:        "chinese_text",
			text:        "这是一段中文文本",
			expectError: false,
		},
		{
			name:        "mixed_text",
			text:        "Hello 世界 123",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedding, err := v.EmbedText(tt.text)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, embedding)
				assert.Len(t, embedding, 128)

				var sum float64
				for _, val := range embedding {
					sum += float64(val) * float64(val)
				}
				assert.InDelta(t, 1.0, sum, 0.001)
			}
		})
	}
}

// TestVectorizer_EmbedText_Deterministic tests that same text produces same embedding
func TestVectorizer_EmbedText_Deterministic(t *testing.T) {
	requireLocalEmbedding(t)
	v := NewVectorizer(128, nil)
	text := "deterministic test"

	embedding1, err := v.EmbedText(text)
	assert.NoError(t, err)

	embedding2, err := v.EmbedText(text)
	assert.NoError(t, err)

	assert.Equal(t, embedding1, embedding2)
}

// TestVectorizer_EmbedText_DifferentTexts tests that different texts produce different embeddings
func TestVectorizer_EmbedText_DifferentTexts(t *testing.T) {
	requireLocalEmbedding(t)
	v := NewVectorizer(128, nil)

	embedding1, err := v.EmbedText("text one")
	assert.NoError(t, err)

	embedding2, err := v.EmbedText("text two")
	assert.NoError(t, err)

	assert.NotEqual(t, embedding1, embedding2)
}

// TestVectorizer_EmbedBatch tests batch embedding
func TestVectorizer_EmbedBatch(t *testing.T) {
	requireLocalEmbedding(t)
	v := NewVectorizer(128, nil)

	tests := []struct {
		name        string
		texts       []string
		expectError bool
		expectedLen int
	}{
		{
			name:        "empty_batch",
			texts:       []string{},
			expectError: false,
			expectedLen: 0,
		},
		{
			name:        "single_text",
			texts:       []string{"hello"},
			expectError: false,
			expectedLen: 1,
		},
		{
			name:        "multiple_texts",
			texts:       []string{"hello", "world", "test"},
			expectError: false,
			expectedLen: 3,
		},
		{
			name:        "contains_empty_text",
			texts:       []string{"hello", "", "world"},
			expectError: true,
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embeddings, err := v.EmbedBatch(tt.texts)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, embeddings, tt.expectedLen)

				for i, emb := range embeddings {
					assert.Len(t, emb, 128, "embedding %d has wrong dimension", i)
				}
			}
		})
	}
}

// TestVectorizer_GetDimension tests dimension getter
func TestVectorizer_GetDimension(t *testing.T) {
	v := NewVectorizer(256, nil)
	assert.Equal(t, 256, v.GetDimension())
}

// TestVectorizer_ValidateEmbedding tests embedding validation
func TestVectorizer_ValidateEmbedding(t *testing.T) {
	v := NewVectorizer(3, nil)

	tests := []struct {
		name        string
		embedding   []float32
		expectValid bool
	}{
		{
			name:        "correct_dimension",
			embedding:   []float32{0.1, 0.2, 0.3},
			expectValid: true,
		},
		{
			name:        "wrong_dimension",
			embedding:   []float32{0.1, 0.2},
			expectValid: false,
		},
		{
			name:        "empty_embedding",
			embedding:   []float32{},
			expectValid: false,
		},
		{
			name:        "contains_nan",
			embedding:   []float32{0.1, float32(math.NaN()), 0.3},
			expectValid: false,
		},
		{
			name:        "contains_inf",
			embedding:   []float32{0.1, float32(math.Inf(1)), 0.3},
			expectValid: false,
		},
		{
			name:        "contains_neg_inf",
			embedding:   []float32{0.1, float32(math.Inf(-1)), 0.3},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := v.ValidateEmbedding(tt.embedding)
			assert.Equal(t, tt.expectValid, valid)
		})
	}
}

// TestCosineSimilarity tests the cosine similarity function
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		vecA     []float32
		vecB     []float32
		expected float64
	}{
		{
			name:     "empty_vectors",
			vecA:     []float32{},
			vecB:     []float32{},
			expected: 0,
		},
		{
			name:     "different_lengths",
			vecA:     []float32{0.1, 0.2, 0.3},
			vecB:     []float32{0.1, 0.2},
			expected: 0,
		},
		{
			name:     "identical_unit_vectors",
			vecA:     []float32{1, 0, 0},
			vecB:     []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal_vectors",
			vecA:     []float32{1, 0, 0},
			vecB:     []float32{0, 1, 0},
			expected: 0,
		},
		{
			name:     "opposite_vectors",
			vecA:     []float32{1, 0, 0},
			vecB:     []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "normalized_vectors",
			vecA:     []float32{0.5, 0.5, 0.5, 0.5},
			vecB:     []float32{0.5, 0.5, 0.5, 0.5},
			expected: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			similarity := CosineSimilarity(tt.vecA, tt.vecB)
			assert.InDelta(t, tt.expected, similarity, 0.0001)
		})
	}
}

// TestJaccardSimilarity tests the Jaccard similarity function
func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		text1    string
		text2    string
		expected float64
	}{
		{
			name:     "empty_texts",
			text1:    "",
			text2:    "",
			expected: 0,
		},
		{
			name:     "identical_texts",
			text1:    "hello world",
			text2:    "hello world",
			expected: 1.0,
		},
		{
			name:     "completely_different",
			text1:    "apple banana",
			text2:    "orange grape",
			expected: 0,
		},
		{
			name:     "partial_overlap",
			text1:    "apple banana cherry",
			text2:    "banana cherry date",
			expected: 0.5,
		},
		{
			name:     "case_insensitive",
			text1:    "Hello World",
			text2:    "hello world",
			expected: 1.0,
		},
		{
			name:     "with_punctuation",
			text1:    "hello, world!",
			text2:    "hello world",
			expected: 1.0,
		},
		{
			name:     "one_empty",
			text1:    "hello",
			text2:    "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			similarity := JaccardSimilarity(tt.text1, tt.text2)
			assert.InDelta(t, tt.expected, similarity, 0.01)
		})
	}
}
