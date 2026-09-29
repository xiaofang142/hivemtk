package llm

import (
	"context"
	"testing"
)

// D16: 来源常量与批内同源判定
func TestD16_SourceConstants(t *testing.T) {
	if EmbedSourceTEI != "tei" || EmbedSourceHash != "hash" {
		t.Errorf("来源常量值错误: %s/%s", EmbedSourceTEI, EmbedSourceHash)
	}
}

// D16: EmbedWithSource 空输入快速返回 tei
func TestD16_EmptyInputReturnsTEI(t *testing.T) {
	s := &EmbeddingService{}
	_, src, err := s.EmbedWithSource(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("空输入不应报错: %v", err)
	}
	if src != EmbedSourceTEI {
		t.Errorf("空输入默认 tei, got %s", src)
	}
}
