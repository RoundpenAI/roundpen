package llmgw

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeLogBody_GzipAndInvalidUTF8(t *testing.T) {
	var raw bytes.Buffer
	zw := gzip.NewWriter(&raw)
	_, _ = zw.Write([]byte(`{"ok":true}`))
	_ = zw.Close()

	got := sanitizeLogBody(raw.Bytes())
	if got != `{"ok":true}` {
		t.Fatalf("gunzip log = %q", got)
	}

	invalid := []byte{'h', 'i', 0x8b, 'x'}
	got = sanitizeLogBody(invalid)
	if !utf8.ValidString(got) {
		t.Fatalf("still invalid utf8: %q", got)
	}
	if !strings.Contains(got, "hi") || !strings.Contains(got, "x") {
		t.Fatalf("unexpected sanitize: %q", got)
	}
}

func TestTruncateForLog_Sanitizes(t *testing.T) {
	s, trunc := truncateForLog([]byte{'a', 0xff, 'b'}, 10)
	if trunc {
		t.Fatal("should not truncate")
	}
	if !utf8.ValidString(s) {
		t.Fatalf("invalid: %q", s)
	}
}
