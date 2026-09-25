package imconnect

import (
	"strings"
	"testing"
)

func TestTakeParagraphBatch_WaitsForBlankLine(t *testing.T) {
	ready, rest, ok := takeParagraphBatch("hello world", maxTextBatchRunes)
	if ok || ready != "" || rest != "hello world" {
		t.Fatalf("incomplete paragraph: ready=%q rest=%q ok=%v", ready, rest, ok)
	}
}

func TestTakeParagraphBatch_OneParagraph(t *testing.T) {
	ready, rest, ok := takeParagraphBatch("第一段\n\n第二段还没完", maxTextBatchRunes)
	if !ok || ready != "第一段" || rest != "第二段还没完" {
		t.Fatalf("got ready=%q rest=%q ok=%v", ready, rest, ok)
	}
}

func TestTakeParagraphBatch_CoalescesShortParagraphs(t *testing.T) {
	in := "a\n\nb\n\nc still open"
	ready, rest, ok := takeParagraphBatch(in, 100)
	if !ok {
		t.Fatal("expected a batch")
	}
	if ready != "a\n\nb" {
		t.Fatalf("ready=%q", ready)
	}
	if rest != "c still open" {
		t.Fatalf("rest=%q", rest)
	}
}

func TestTakeParagraphBatch_SkipsExtraBlankLines(t *testing.T) {
	ready, rest, ok := takeParagraphBatch("\n\n第一段\n\n\n\n第二段未完", maxTextBatchRunes)
	if !ok || ready != "第一段" || rest != "第二段未完" {
		t.Fatalf("got ready=%q rest=%q ok=%v", ready, rest, ok)
	}
}

func TestTakeParagraphBatch_RespectsSoftMax(t *testing.T) {
	long := strings.Repeat("字", 50)
	in := long + "\n\n" + long + "\n\n" + "tail"
	ready, rest, ok := takeParagraphBatch(in, 60)
	if !ok {
		t.Fatal("expected a batch")
	}
	if ready != long {
		t.Fatalf("should ship first paragraph alone when next would exceed max, got len=%d", runeLen(ready))
	}
	if rest != long+"\n\n"+"tail" {
		t.Fatalf("rest=%q", rest)
	}
}
