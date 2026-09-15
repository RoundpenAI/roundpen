package sysagent

import (
	"io"
	"testing"
)

// eofWithDataReader returns the whole payload together with io.EOF on the
// first Read, mimicking a flushed SSE body whose final chunk carries data and
// EOF in one call.
type eofWithDataReader struct {
	data []byte
	done bool
}

func (r *eofWithDataReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	n := copy(p, r.data)
	return n, io.EOF
}

func TestLineReaderSplitsFinalChunkWithEOF(t *testing.T) {
	r := &eofWithDataReader{data: []byte("data: one\n\ndata: two\n\ndata: [DONE]\n\n")}
	lr := newLineReader(r)
	var lines []string
	for {
		line, err := lr.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	want := []string{"data: one", "", "data: two", "", "data: [DONE]", ""}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestLineReaderTrailingChunkWithoutNewline(t *testing.T) {
	lr := newLineReader(&eofWithDataReader{data: []byte("data: one\ntail")})
	line, err := lr.ReadLine()
	if err != nil || line != "data: one" {
		t.Fatalf("first = %q, %v", line, err)
	}
	line, err = lr.ReadLine()
	if err != nil || line != "tail" {
		t.Fatalf("tail = %q, %v", line, err)
	}
	if _, err = lr.ReadLine(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestLineReaderCRLF(t *testing.T) {
	lr := newLineReader(&eofWithDataReader{data: []byte("a\r\nb\n")})
	line, _ := lr.ReadLine()
	if line != "a" {
		t.Fatalf("crlf line = %q", line)
	}
	line, err := lr.ReadLine()
	if err != nil || line != "b" {
		t.Fatalf("second = %q, %v", line, err)
	}
}
