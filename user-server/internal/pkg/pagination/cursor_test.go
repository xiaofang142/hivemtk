package pagination

import (
	"testing"
	"time"
)

func TestEncodeDecodeCursor(t *testing.T) {
	now := time.Unix(1700000000, 123456789)
	id := uint64(12345)

	cursor := EncodeCursor(now, id)
	if cursor == "" {
		t.Fatal("EncodeCursor returned empty")
	}

	gotTime, gotID, ok := DecodeCursor(cursor)
	if !ok {
		t.Fatal("DecodeCursor failed")
	}
	if !gotTime.Equal(now) {
		t.Errorf("time mismatch: got %v want %v", gotTime, now)
	}
	if gotID != id {
		t.Errorf("id mismatch: got %d want %d", gotID, id)
	}
}

func TestDecodeEmptyCursor(t *testing.T) {
	_, _, ok := DecodeCursor("")
	if ok {
		t.Error("empty cursor should return ok=false")
	}
}

func TestDecodeInvalidCursor(t *testing.T) {
	_, _, ok := DecodeCursor("not-a-valid-base64!@#")
	if ok {
		t.Error("invalid cursor should return ok=false")
	}
}

func TestClampLimit(t *testing.T) {
	cases := []struct {
		input    int
		expected int
	}{
		{0, DefaultCursorPageSize},
		{-1, DefaultCursorPageSize},
		{1, 1},
		{50, 50},
		{DefaultCursorPageSize, DefaultCursorPageSize},
		{DefaultCursorPageSize + 1, DefaultCursorPageSize},
		{9999, DefaultCursorPageSize},
	}
	for _, c := range cases {
		got := ClampLimit(c.input)
		if got != c.expected {
			t.Errorf("ClampLimit(%d) = %d, want %d", c.input, got, c.expected)
		}
	}
}

func TestIsValidLimit(t *testing.T) {
	if !IsValidLimit(50) {
		t.Error("50 should be valid")
	}
	if IsValidLimit(0) {
		t.Error("0 should be invalid")
	}
	if IsValidLimit(DefaultCursorPageSize + 1) {
		t.Error("over max should be invalid")
	}
}

func TestSplitBytes(t *testing.T) {
	got := splitBytes([]byte("a:b:c"), ':')
	if len(got) != 3 || string(got[0]) != "a" || string(got[2]) != "c" {
		t.Errorf("splitBytes failed: %s", got)
	}
}
