package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type cfgCall struct {
	group, key string
	fbInt      int
	fbFloat    float64
	fbBool     bool
	fbDur      time.Duration
	kind       string
}

// fakeReader 记录每次读取的 (group,key,fallback)，用于锁死 seed 键名与兜底值。
type fakeReader struct {
	calls    []cfgCall
	intVal   int
	floatVal float64
	boolVal  bool
	durVal   time.Duration
}

func (f *fakeReader) record(c cfgCall) {
	f.calls = append(f.calls, c)
}

func (f *fakeReader) GetInt(_ context.Context, group, key string, fallback int) int {
	f.record(cfgCall{group: group, key: key, fbInt: fallback, kind: "int"})
	return f.intVal
}

func (f *fakeReader) GetFloat(_ context.Context, group, key string, fallback float64) float64 {
	f.record(cfgCall{group: group, key: key, fbFloat: fallback, kind: "float"})
	return f.floatVal
}

func (f *fakeReader) GetBool(_ context.Context, group, key string, fallback bool) bool {
	f.record(cfgCall{group: group, key: key, fbBool: fallback, kind: "bool"})
	return f.boolVal
}

func (f *fakeReader) GetDuration(_ context.Context, group, key string, fallback time.Duration) time.Duration {
	f.record(cfgCall{group: group, key: key, fbDur: fallback, kind: "dur"})
	return f.durVal
}

// TestConfigGettersFallBackWithoutReader 未注入读取器时必须回落硬编码默认值。
func TestConfigGettersFallBackWithoutReader(t *testing.T) {
	SetConfigReader(nil)
	t.Cleanup(func() { SetConfigReader(nil) })

	if EmbeddingDim() != 1024 {
		t.Errorf("EmbeddingDim=%d want 1024", EmbeddingDim())
	}
	if AsyncProcessingTimeout() != 15*time.Minute {
		t.Errorf("AsyncProcessingTimeout=%v want 15m", AsyncProcessingTimeout())
	}
	if ExternalImportTimeout() != 30*time.Minute {
		t.Errorf("ExternalImportTimeout=%v want 30m", ExternalImportTimeout())
	}
	if SSRFCheckTimeout() != 5*time.Second {
		t.Errorf("SSRFCheckTimeout=%v want 5s", SSRFCheckTimeout())
	}
	if DefaultTopK() != 5 {
		t.Errorf("DefaultTopK=%d want 5", DefaultTopK())
	}
	if ChunkContentPreview() != 500 {
		t.Errorf("ChunkContentPreview=%d want 500", ChunkContentPreview())
	}
	if MaxSearchListSize() != 1000 {
		t.Errorf("MaxSearchListSize=%d want 1000", MaxSearchListSize())
	}
	if BM25ScanLimit() != 10000 {
		t.Errorf("BM25ScanLimit=%d want 10000", BM25ScanLimit())
	}
	if DefaultSimilarityThreshold() != 0.5 {
		t.Errorf("DefaultSimilarityThreshold=%v want 0.5", DefaultSimilarityThreshold())
	}
	if DefaultTemperature() != 0.7 {
		t.Errorf("DefaultTemperature=%v want 0.7", DefaultTemperature())
	}
	if DefaultMaxTokens() != 1000 {
		t.Errorf("DefaultMaxTokens=%d want 1000", DefaultMaxTokens())
	}
	if DefaultTopP() != 0.9 {
		t.Errorf("DefaultTopP=%v want 0.9", DefaultTopP())
	}
	if DefaultRequestTimeoutSeconds() != 60 {
		t.Errorf("DefaultRequestTimeoutSeconds=%d want 60", DefaultRequestTimeoutSeconds())
	}
	if DefaultMaxRetries() != 3 {
		t.Errorf("DefaultMaxRetries=%d want 3", DefaultMaxRetries())
	}
	if DefaultPageSize != 20 || DefaultFrequencyPenalty != 0.5 || DefaultPresencePenalty != 0.5 {
		t.Error("常量默认值被改动")
	}
}

// TestConfigGettersReadInjectedKeys 注入后必须按 (group,key) 读取，并把兜底值一并传下去。
func TestConfigGettersReadInjectedKeys(t *testing.T) {
	f := &fakeReader{intVal: 77, floatVal: 0.11, boolVal: true, durVal: 90 * time.Second}
	SetConfigReader(f)
	t.Cleanup(func() { SetConfigReader(nil) })

	// 逐个调用全部 getter，顺序必须与下方 cases 完全一致：
	// fakeReader 按调用顺序记录 (group,key,kind,兜底值)，据此逐位比对。
	_ = EmbeddingDim()
	_ = AsyncProcessingTimeout()
	_ = ExternalImportTimeout()
	_ = SSRFCheckTimeout()
	_ = DefaultTopK()
	_ = ChunkContentPreview()
	_ = MaxSearchListSize()
	_ = BM25ScanLimit()
	_ = DefaultSimilarityThreshold()
	_ = DefaultTemperature()
	_ = DefaultMaxTokens()
	_ = DefaultTopP()
	_ = DefaultRequestTimeoutSeconds()
	_ = DefaultMaxRetries()

	cases := []struct {
		group, key, kind string
		fb               string
	}{
		{"knowledge", "embedding_dimension", "int", "1024"},
		{"knowledge", "async_processing_timeout", "dur", "15m0s"},
		{"knowledge", "external_import_timeout", "dur", "30m0s"},
		{"knowledge", "ssrf_check_timeout", "dur", "5s"},
		{"knowledge", "default_top_k", "int", "5"},
		{"knowledge", "chunk_preview_max_len", "int", "500"},
		{"knowledge", "max_search_list_size", "int", "1000"},
		{"knowledge", "bm25_scan_limit", "int", "10000"},
		{"knowledge", "similarity_threshold", "float", "0.5"},
		{"agent_llm", "temperature", "float", "0.7"},
		{"agent_llm", "max_tokens", "int", "1000"},
		{"agent_llm", "top_p", "float", "0.9"},
		{"agent_llm", "request_timeout", "dur", "1m0s"},
		{"agent_llm", "max_retries", "int", "3"},
	}
	if len(f.calls) != len(cases) {
		t.Fatalf("读取次数=%d want %d（getter 数量或读取方式变了）", len(f.calls), len(cases))
	}
	for i, want := range cases {
		got := f.calls[i]
		if got.group != want.group || got.key != want.key || got.kind != want.kind {
			t.Errorf("第 %d 次读取 = (%s,%s,%s) want (%s,%s,%s)",
				i, got.group, got.key, got.kind, want.group, want.key, want.kind)
		}
		var gotFb string
		switch got.kind {
		case "int":
			gotFb = fmt.Sprintf("%v", got.fbInt)
		case "float":
			gotFb = fmt.Sprintf("%v", got.fbFloat)
		case "dur":
			gotFb = got.fbDur.String()
		}
		if gotFb != want.fb {
			t.Errorf("%s.%s 兜底值=%q want %q", want.group, want.key, gotFb, want.fb)
		}
	}

	if EmbeddingDim() != 77 || DefaultTemperature() != 0.11 || SSRFCheckTimeout() != 90*time.Second {
		t.Error("注入值未透传到 getter 返回值")
	}
	if DefaultRequestTimeoutSeconds() != 90 {
		t.Errorf("DefaultRequestTimeoutSeconds 应按秒取整, got %d", DefaultRequestTimeoutSeconds())
	}
	if DefaultMaxTokens() != 77 {
		t.Errorf("DefaultMaxTokens=%d want 77（注入值）", DefaultMaxTokens())
	}
}

// TestSetConfigReaderNilIsIdempotent 重复清空不得 panic，且清空后必须回到硬编码兜底。
func TestSetConfigReaderNilIsIdempotent(t *testing.T) {
	SetConfigReader(nil)
	SetConfigReader(&fakeReader{intVal: 1})
	if EmbeddingDim() != 1 {
		t.Fatalf("注入后应读到 1, got %d", EmbeddingDim())
	}
	SetConfigReader(nil)
	if EmbeddingDim() != 1024 {
		t.Errorf("清空后应回落 1024, got %d", EmbeddingDim())
	}
	SetConfigReader(nil)
	if BM25ScanLimit() != 10000 {
		t.Errorf("重复清空后 BM25ScanLimit=%d want 10000", BM25ScanLimit())
	}
}
