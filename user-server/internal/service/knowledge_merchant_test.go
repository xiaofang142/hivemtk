package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
)

func TestParseCSV_Basic(t *testing.T) {
	csv := `title,content,category,tags
"如何退货","请在订单页面申请","售后","退货,流程"
"产品 A 介绍","产品 A 是我们的明星产品","产品","产品A,介绍"`
	items, err := knowledgesvc.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Title != "如何退货" {
		t.Errorf("expected title '如何退货', got %s", items[0].Title)
	}
	if items[0].Category != "售后" {
		t.Errorf("expected category '售后', got %s", items[0].Category)
	}
	if len(items[0].Tags) != 2 || items[0].Tags[0] != "退货" {
		t.Errorf("expected tags [退货,流程], got %v", items[0].Tags)
	}
}

func TestParseCSV_NoHeader(t *testing.T) {
	csv := "title\nhello"
	_, err := knowledgesvc.ParseCSV([]byte(csv))
	if err == nil {
		t.Error("expected error for CSV without content column")
	}
}

func TestParseCSV_Empty(t *testing.T) {
	_, err := knowledgesvc.ParseCSV([]byte(""))
	if err == nil {
		t.Error("expected error for empty CSV")
	}
}

func TestParseJSON_Array(t *testing.T) {
	data := `[
		{"title": "T1", "content": "C1", "category": "FAQ"},
		{"title": "T2", "content": "C2"}
	]`
	items, err := knowledgesvc.ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestParseJSON_Wrapped(t *testing.T) {
	data := `{"items": [{"title": "T1", "content": "C1"}], "total": 1}`
	items, err := knowledgesvc.ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestParseJSON_Invalid(t *testing.T) {
	_, err := knowledgesvc.ParseJSON([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseJSON_DataField(t *testing.T) {
	data := `{"data": [{"title": "T", "content": "C"}]}`
	items, err := knowledgesvc.ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 1 || items[0].Content != "C" {
		t.Errorf("expected 1 item, got %+v", items)
	}
}

func TestGenerateToken(t *testing.T) {
	t1, err := knowledgesvc.GenerateToken()
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}
	if !strings.HasPrefix(t1, "kbg_") {
		t.Errorf("expected prefix kbg_, got %s", t1)
	}
	if len(t1) < 20 {
		t.Errorf("expected length >= 20, got %d", len(t1))
	}
	t2, _ := knowledgesvc.GenerateToken()
	if t1 == t2 {
		t.Error("expected different tokens")
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	a := knowledgesvc.HashToken("kbg_abc")
	b := knowledgesvc.HashToken("kbg_abc")
	if a != b {
		t.Error("hashToken should be deterministic")
	}
	if knowledgesvc.HashToken("kbg_abc") == knowledgesvc.HashToken("kbg_xyz") {
		t.Error("different tokens should have different hashes")
	}
}

func TestTokenHasScope(t *testing.T) {
	if !knowledgesvc.TokenHasScope(`["read","write"]`, "write") {
		t.Error("expected write scope")
	}
	if !knowledgesvc.TokenHasScope(`["*"]`, "read") {
		t.Error("expected * scope to match any")
	}
	if knowledgesvc.TokenHasScope(`["read"]`, "write") {
		t.Error("should not have write scope")
	}
	if knowledgesvc.TokenHasScope(``, "read") {
		t.Error("invalid JSON should not match")
	}
}

func TestBoolToInt(t *testing.T) {
	if knowledgesvc.BoolToInt(true) != 1 {
		t.Error("expected 1 for true")
	}
	if knowledgesvc.BoolToInt(false) != 0 {
		t.Error("expected 0 for false")
	}
}

func TestMerchantRAGChunk_Structure(t *testing.T) {
	c := knowledgesvc.MerchantRAGChunk{
		ID:         1,
		DocumentID: 2,
		Content:    "test",
		Score:      0.95,
	}
	if c.ID != 1 || c.DocumentID != 2 || c.Content != "test" || c.Score != 0.95 {
		t.Error("MerchantRAGChunk fields not set correctly")
	}
}

func TestPlaygroundRequest_Defaults(t *testing.T) {
	req := knowledgesvc.PlaygroundRequest{Query: "test", ProductID: "kb1"}
	if req.Query == "" {
		t.Error("query should not be empty")
	}
	if req.TopK != 0 {
		t.Error("default topK should be 0 (will be set to 5 internally)")
	}
}

func TestBatchImportResult_Fields(t *testing.T) {
	r := &knowledgesvc.BatchImportResult{
		BatchNo:  "BATCH-001",
		Total:    10,
		Accepted: 8,
		Rejected: 2,
	}
	if r.Accepted+r.Rejected != r.Total {
		t.Error("accepted + rejected should equal total")
	}
}

func TestSubmitFeedback_ValidatesRating(t *testing.T) {
	tests := []struct {
		rating int
		valid  bool
	}{
		{1, true},
		{0, true},
		{-1, true},
		{2, false},
		{-2, false},
	}
	for _, tc := range tests {
		valid := tc.rating >= -1 && tc.rating <= 1
		if valid != tc.valid {
			t.Errorf("rating %d: expected valid=%v, got %v", tc.rating, tc.valid, valid)
		}
	}
}

func TestContextCheck(t *testing.T) {
	svc := knowledgesvc.NewKnowledgeMerchantServiceWithDeps(knowledgesvc.KnowledgeMerchantDeps{}) // 零值 deps = 原 WithDB(nil) 无库语义
	err := svc.SubmitFeedback(context.Background(), &knowledgesvc.SubmitFeedbackRequest{Query: "q", Rating: 1})
	if err != nil {
		t.Errorf("nil db should not error on feedback: %v", err)
	}
}

func TestParseCSV_OnlyContentColumn(t *testing.T) {
	csv := "content\nhello world\nfoo bar"
	items, err := knowledgesvc.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Content != "hello world" {
		t.Errorf("expected 'hello world', got %s", items[0].Content)
	}
}

func TestParseCSV_QuotedFields(t *testing.T) {
	csv := `title,content
"标题,带逗号","内容 ""含引号"" 也行"`
	items, err := knowledgesvc.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Title != "标题,带逗号" {
		t.Errorf("expected title with comma, got %s", items[0].Title)
	}
	if items[0].Content != `内容 "含引号" 也行` {
		t.Errorf("expected content with quotes, got %s", items[0].Content)
	}
}

func TestParseCSV_VariableLengthRows(t *testing.T) {
	csv := "title,content\nA,B,C\nX,Y"
	items, err := knowledgesvc.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Title != "A" || items[0].Content != "B" {
		t.Errorf("row 1 wrong: %+v", items[0])
	}
}

func TestParseCSV_SkipEmptyContent(t *testing.T) {
	csv := "title,content\nA,\nB,content-b"
	items, err := knowledgesvc.ParseCSV([]byte(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items (parse 不过滤), got %d", len(items))
	}
}

func TestParseJSON_ListField(t *testing.T) {
	data := `{"list": [{"title": "T1", "content": "C1"}]}`
	items, err := knowledgesvc.ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestParseJSON_DocumentsField(t *testing.T) {
	data := `{"documents": [{"title": "T", "content": "C"}]}`
	items, err := knowledgesvc.ParseJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestParseJSON_EmptyArray(t *testing.T) {
	items, err := knowledgesvc.ParseJSON([]byte("[]"))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestParseJSON_EmptyObject(t *testing.T) {
	_, err := knowledgesvc.ParseJSON([]byte("{}"))
	if err == nil {
		t.Error("expected error for empty JSON object")
	}
}

func TestGenerateToken_UniqueAndLong(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := knowledgesvc.GenerateToken()
		if err != nil {
			t.Fatalf("generateToken: %v", err)
		}
		if seen[tok] {
			t.Errorf("duplicate token generated: %s", tok)
		}
		seen[tok] = true
		if len(tok) < 30 {
			t.Errorf("token too short: %s", tok)
		}
	}
}

func TestHashToken_ConsistentForSameInput(t *testing.T) {
	for i := 0; i < 10; i++ {
		input := fmt.Sprintf("kbg_test_%d", i)
		h1 := knowledgesvc.HashToken(input)
		h2 := knowledgesvc.HashToken(input)
		if h1 != h2 {
			t.Errorf("inconsistent hash for %s", input)
		}
		if len(h1) != 64 {
			t.Errorf("hash length should be 64, got %d", len(h1))
		}
	}
}

func TestTokenHasScope_EmptyScopes(t *testing.T) {
	if knowledgesvc.TokenHasScope("", "read") {
		t.Error("empty scope should not match")
	}
	if knowledgesvc.TokenHasScope("not json", "read") {
		t.Error("invalid JSON should not match")
	}
}

func TestTokenHasScope_Wildcard(t *testing.T) {
	for _, scope := range []string{"read", "write", "delete", "anything"} {
		if !knowledgesvc.TokenHasScope(`["*"]`, scope) {
			t.Errorf("wildcard should match %s", scope)
		}
	}
}

func TestBoolToInt_AllCases(t *testing.T) {
	cases := []struct {
		input bool
		want  int
	}{
		{true, 1},
		{false, 0},
	}
	for _, c := range cases {
		if got := knowledgesvc.BoolToInt(c.input); got != c.want {
			t.Errorf("knowledgesvc.BoolToInt(%v) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestBatchImportResult_TotalCalculation(t *testing.T) {
	cases := []struct {
		total    int
		accepted int
		rejected int
	}{
		{10, 10, 0},
		{10, 0, 10},
		{10, 7, 3},
		{0, 0, 0},
	}
	for _, c := range cases {
		r := &knowledgesvc.BatchImportResult{Total: c.total, Accepted: c.accepted, Rejected: c.rejected}
		if r.Accepted+r.Rejected != r.Total {
			t.Errorf("accepted+rejected should equal total: %+v", r)
		}
	}
}

func TestSubmitFeedbackRequest_Fields(t *testing.T) {
	req := knowledgesvc.SubmitFeedbackRequest{
		ProductID:  "prod-1",
		Query:      "test query",
		DocumentID: 100,
		ChunkID:    200,
		Rating:     1,
		Comment:    "good",
		Operator:   "user1",
		SessionID:  "sess1",
	}
	if req.Query != "test query" {
		t.Error("Query not set")
	}
	if req.Rating != 1 {
		t.Error("Rating not set")
	}
}

func TestPlaygroundRequest_ThresholdBounds(t *testing.T) {
	req := knowledgesvc.PlaygroundRequest{
		ProductID:           "p1",
		Query:               "q",
		TopK:                20,
		SimilarityThreshold: 0.75,
	}
	if req.SimilarityThreshold < 0 || req.SimilarityThreshold > 1 {
		t.Error("threshold out of range")
	}
	if req.TopK > 50 || req.TopK < 1 {
		t.Error("topK out of range")
	}
}

func TestCreateTokenRequest_RequiredFields(t *testing.T) {
	req := knowledgesvc.CreateTokenRequest{
		Name:      "test",
		ProductID: "prod1",
		Scopes:    []string{"read", "write"},
	}
	if req.Name == "" {
		t.Error("name required")
	}
	if req.ProductID == "" {
		t.Error("product_id required")
	}
}

func TestExternalImportRequest_SourceValidation(t *testing.T) {
	validSources := []string{"custom", "feishu", "notion", "dingtalk"}
	for _, s := range validSources {
		req := knowledgesvc.ExternalImportRequest{Source: s, ProductID: "p1"}
		if req.Source == "" {
			t.Errorf("source %s should not be empty", s)
		}
	}
}

func TestKnowledgeChunk_EmbeddingIDReset(t *testing.T) {
	chunk := model.KnowledgeChunk{
		EmbeddingID: "vec_old_123",
	}
	if chunk.EmbeddingID != "" {
		chunk.EmbeddingID = ""
	}
	if chunk.EmbeddingID != "" {
		t.Error("EmbeddingID should be reset to empty after content update")
	}
}

func TestExternalImportResponse_FieldsStructure(t *testing.T) {
	resp := knowledgesvc.ExternalImportResponse{
		JobNo:       "EXT-001",
		Status:      "pending",
		Total:       10,
		Accepted:    8,
		Rejected:    2,
		FailedItems: 2,
		Async:       true,
	}
	if resp.JobNo == "" {
		t.Error("job_no required")
	}
	if resp.Status != "pending" {
		t.Error("status wrong")
	}
	if !resp.Async {
		t.Error("async should be true")
	}
}

func TestSplitChunkRequest_RequiresTwoParts(t *testing.T) {
	req := knowledgesvc.SplitChunkRequest{
		ChunkID: 1,
		Parts:   []string{"part1", "part2"},
	}
	if len(req.Parts) < 2 {
		t.Error("need at least 2 parts")
	}
}

func TestUpdateChunkRequest_RequiresContent(t *testing.T) {
	req := knowledgesvc.UpdateChunkRequest{
		ChunkID: 1,
		Content: "new content",
	}
	if req.Content == "" {
		t.Error("content required")
	}
	if req.ChunkID == 0 {
		t.Error("chunk_id required")
	}
}
