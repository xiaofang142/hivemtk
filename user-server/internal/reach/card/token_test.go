package card

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestXianyuReportToken_RoundTrip(t *testing.T) {
	token, err := GenerateXianyuReportToken(42, time.Hour)
	if err != nil {
		t.Fatalf("GenerateXianyuReportToken failed: %v", err)
	}
	if err := VerifyXianyuReportToken(42, token); err != nil {
		t.Fatalf("正确凭证验签失败: %v", err)
	}
}

func TestXianyuReportToken_RejectsMissing(t *testing.T) {
	if err := VerifyXianyuReportToken(42, "   "); !errors.Is(err, ErrXianyuReportTokenMissing) {
		t.Fatalf("空凭证应返回 ErrXianyuReportTokenMissing，实际 %v", err)
	}
}

func TestXianyuReportToken_RejectsCardIDMismatch(t *testing.T) {
	token, err := GenerateXianyuReportToken(42, time.Hour)
	if err != nil {
		t.Fatalf("GenerateXianyuReportToken failed: %v", err)
	}
	if err := VerifyXianyuReportToken(43, token); !errors.Is(err, ErrXianyuReportTokenInvalid) {
		t.Fatalf("42 的凭证用在 43 上应返回 ErrXianyuReportTokenInvalid，实际 %v", err)
	}
}

func TestXianyuReportToken_RejectsTampered(t *testing.T) {
	token, err := GenerateXianyuReportToken(42, time.Hour)
	if err != nil {
		t.Fatalf("GenerateXianyuReportToken failed: %v", err)
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("凭证格式异常: %q", token)
	}
	tampered := parts[1] + "." + parts[0]
	if err := VerifyXianyuReportToken(42, tampered); !errors.Is(err, ErrXianyuReportTokenInvalid) {
		t.Fatalf("调换段落的凭证应被拒绝，实际 %v", err)
	}
	flippedFirst := byte('0')
	if parts[0][0] == '0' {
		flippedFirst = '1'
	}
	flipped := string(flippedFirst) + parts[0][1:] + "." + parts[1]
	if err := VerifyXianyuReportToken(42, flipped); !errors.Is(err, ErrXianyuReportTokenInvalid) {
		t.Fatalf("篡改签名的凭证应被拒绝，实际 %v", err)
	}
}

func TestXianyuReportToken_RejectsExpired(t *testing.T) {
	expired, err := signXianyuReportToken(42, time.Now().Add(-time.Minute).Unix())
	if err != nil {
		t.Fatalf("signXianyuReportToken failed: %v", err)
	}
	if err := VerifyXianyuReportToken(42, expired); !errors.Is(err, ErrXianyuReportTokenExpired) {
		t.Fatalf("过期凭证应返回 ErrXianyuReportTokenExpired，实际 %v", err)
	}
}

func TestXianyuReportToken_ZeroCardID(t *testing.T) {
	if _, err := GenerateXianyuReportToken(0, time.Hour); !errors.Is(err, ErrXianyuReportTokenInvalid) {
		t.Fatalf("id=0 不应签发凭证，实际 %v", err)
	}
}
