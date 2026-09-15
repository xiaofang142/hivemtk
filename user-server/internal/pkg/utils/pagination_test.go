package utils

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newPaginationCtx(query string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/?"+query, nil)
	c.Request = req
	return c
}

func TestParsePagination_Defaults(t *testing.T) {
	page, pageSize, err := ParsePagination(newPaginationCtx(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 1 {
		t.Errorf("page = %d, want 1", page)
	}
	if pageSize != defaultDefaultPageSize {
		t.Errorf("pageSize = %d, want %d", pageSize, defaultDefaultPageSize)
	}
}

func TestParsePagination_PageNonNumeric_DefaultsToOne(t *testing.T) {
	page, _, err := ParsePagination(newPaginationCtx("page=abc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 1 {
		t.Errorf("page = %d, want 1", page)
	}
}

func TestParsePagination_PageNegative_DefaultsToOne(t *testing.T) {
	page, _, err := ParsePagination(newPaginationCtx("page=-3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 1 {
		t.Errorf("page = %d, want 1", page)
	}
}

func TestParsePagination_PageSizeNonNumeric_FallbackToDefault(t *testing.T) {
	_, pageSize, err := ParsePagination(newPaginationCtx("page_size=xyz"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != defaultDefaultPageSize {
		t.Errorf("pageSize = %d, want %d", pageSize, defaultDefaultPageSize)
	}
}

func TestParsePagination_PageSizeBelowMin_Clamped(t *testing.T) {

	_, pageSize, err := ParsePagination(newPaginationCtx("page_size=2"),
		WithMinSize(5),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != 5 {
		t.Errorf("pageSize = %d, want 5 (clamped to minSize)", pageSize)
	}
}

func TestParsePagination_PageSizeZero_FallbackToDefault(t *testing.T) {

	_, pageSize, err := ParsePagination(newPaginationCtx("page_size=0"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != defaultDefaultPageSize {
		t.Errorf("pageSize = %d, want %d (defaultSize fallback)", pageSize, defaultDefaultPageSize)
	}
}

func TestParsePagination_PageSizeNegative_FallbackToDefault(t *testing.T) {
	_, pageSize, err := ParsePagination(newPaginationCtx("page_size=-10"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != defaultDefaultPageSize {
		t.Errorf("pageSize = %d, want %d", pageSize, defaultDefaultPageSize)
	}
}

func TestParsePagination_PageSizeExceedsMax_Rejected(t *testing.T) {
	_, _, err := ParsePagination(newPaginationCtx("page_size=999"))
	if !IsInvalidPageSize(err) {
		t.Errorf("err = %v, want ErrInvalidPageSize", err)
	}
}

func TestParsePagination_PageSizeExceedsMax_ClampedWhenAllowed(t *testing.T) {

	_, pageSize, err := ParsePagination(newPaginationCtx("page_size=2000"),
		WithMaxSize(MaxPageSizeAdmin),
		WithAllowOverMax(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != MaxPageSizeAdmin {
		t.Errorf("pageSize = %d, want %d", pageSize, MaxPageSizeAdmin)
	}
}

func TestParsePagination_DefaultSizeOption(t *testing.T) {
	_, pageSize, err := ParsePagination(newPaginationCtx(""),
		WithDefaultSize(50),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != 50 {
		t.Errorf("pageSize = %d, want 50", pageSize)
	}
}

func TestParsePagination_AdminScenario(t *testing.T) {

	page, pageSize, err := ParsePagination(newPaginationCtx("page=2&page_size=500"),
		WithMaxSize(MaxPageSizeAdmin),
		WithAllowOverMax(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page != 2 {
		t.Errorf("page = %d, want 2", page)
	}
	if pageSize != 500 {
		t.Errorf("pageSize = %d, want 500", pageSize)
	}

	_, pageSize, err = ParsePagination(newPaginationCtx("page_size=2000"),
		WithMaxSize(MaxPageSizeAdmin),
		WithAllowOverMax(true),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != MaxPageSizeAdmin {
		t.Errorf("pageSize = %d, want %d", pageSize, MaxPageSizeAdmin)
	}
}

func TestParsePagination_LimitAlias(t *testing.T) {
	_, pageSize, err := ParsePagination(newPaginationCtx("limit=33"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != 33 {
		t.Errorf("pageSize = %d, want 33", pageSize)
	}
}

func TestParsePagination_CamelCaseAlias(t *testing.T) {
	_, pageSize, err := ParsePagination(newPaginationCtx("pageSize=27"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageSize != 27 {
		t.Errorf("pageSize = %d, want 27", pageSize)
	}
}

func TestParsePaginationOffset(t *testing.T) {
	offset, limit, err := ParsePaginationOffset(newPaginationCtx("page=3&page_size=15"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offset != 30 {
		t.Errorf("offset = %d, want 30", offset)
	}
	if limit != 15 {
		t.Errorf("limit = %d, want 15", limit)
	}
}

func TestParseCursorParams(t *testing.T) {
	cases := []struct {
		name          string
		query         string
		defaultLimit  int
		wantCursor    string
		wantLimit     int
		wantUseCursor bool
	}{
		{
			name:         "no cursor no mode falls back to offset",
			query:        "page=2&limit=30",
			defaultLimit: 20,

			wantLimit: 30,
		},
		{
			name:          "mode=keyset triggers cursor mode",
			query:         "mode=keyset&limit=15",
			defaultLimit:  20,
			wantLimit:     15,
			wantUseCursor: true,
		},
		{
			name:          "cursor present triggers cursor mode",
			query:         "cursor=abc123",
			defaultLimit:  20,
			wantCursor:    "abc123",
			wantLimit:     20,
			wantUseCursor: true,
		},
		{
			name:          "limit alias page_size",
			query:         "mode=keyset&page_size=50",
			defaultLimit:  20,
			wantLimit:     50,
			wantUseCursor: true,
		},
		{
			name:          "limit alias size",
			query:         "mode=keyset&size=7",
			defaultLimit:  20,
			wantLimit:     7,
			wantUseCursor: true,
		},
		{
			name:          "limit precedence over page_size",
			query:         "mode=keyset&limit=9&page_size=50",
			defaultLimit:  20,
			wantLimit:     9,
			wantUseCursor: true,
		},
		{
			name:          "limit over max clamped to cursor page size",
			query:         "mode=keyset&limit=9999",
			defaultLimit:  20,
			wantLimit:     100,
			wantUseCursor: true,
		},
		{
			name:          "non numeric limit falls back to default",
			query:         "mode=keyset&limit=xyz",
			defaultLimit:  25,
			wantLimit:     25,
			wantUseCursor: true,
		},
		{
			name:          "non positive limit falls back to default",
			query:         "mode=keyset&limit=0",
			defaultLimit:  30,
			wantLimit:     30,
			wantUseCursor: true,
		},
		{
			name:          "default limit non positive falls back to package default",
			query:         "mode=keyset",
			defaultLimit:  0,
			wantLimit:     defaultDefaultPageSize,
			wantUseCursor: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cursor, limit, useCursor := ParseCursorParams(newPaginationCtx(tc.query), tc.defaultLimit)
			if cursor != tc.wantCursor {
				t.Errorf("cursor = %q, want %q", cursor, tc.wantCursor)
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
			if useCursor != tc.wantUseCursor {
				t.Errorf("useCursor = %v, want %v", useCursor, tc.wantUseCursor)
			}
		})
	}
}

func TestStringIDCursor_RoundTrip(t *testing.T) {
	ts := time.Unix(1700000000, 123456789).UTC()
	id := "550e8400-e29b-41d4-a716-446655440000"

	enc := EncodeStringIDCursor(ts, id)
	gotTS, gotID, ok := DecodeStringIDCursor(enc)
	if !ok {
		t.Fatalf("DecodeStringIDCursor returned ok=false")
	}
	if gotTS.UnixNano() != ts.UnixNano() {
		t.Errorf("ts = %v, want %v", gotTS.UnixNano(), ts.UnixNano())
	}
	if gotID != id {
		t.Errorf("id = %q, want %q", gotID, id)
	}
}

func TestStringIDCursor_Invalid(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cursor string
	}{
		{"empty", ""},
		{"not base64", "!!!not-base64!!!"},
		{"missing separator", base64.URLEncoding.EncodeToString([]byte("no-separator"))},
		{"empty ts", base64.URLEncoding.EncodeToString([]byte(":id-only"))},
		{"empty id", base64.URLEncoding.EncodeToString([]byte("1700000000:"))},
		{"non numeric ts", base64.URLEncoding.EncodeToString([]byte("abc:id"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := DecodeStringIDCursor(tc.cursor); ok {
				t.Errorf("expected decode failure for %q", tc.cursor)
			}
		})
	}
}
